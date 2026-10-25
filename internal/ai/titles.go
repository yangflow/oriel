package ai

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"

	"github.com/yangflow/oriel/internal/diag"
	"github.com/yangflow/oriel/internal/store"
)

// The title translation of a subscription (see docs/product.md "标题翻译"): titles go to the endpoint in batches.
const (
	// PromptTitles is the built-in prompt of a batch; it is not a setting.
	PromptTitles = "Translate each title in the JSON array into {language}. Reply with a JSON array of strings of the same length and in the same order, and nothing else. Keep a title that is already in {language} as it is."
	// titleBatch is the most titles in one request, and maxTitleRunes the most characters sent of one title.
	titleBatch    = 40
	maxTitleRunes = 300
)

// dispatchTitles starts one batch of queued titles when a slot is free and the endpoint is not waiting. Titles are
// automatic work only, so they wait while the endpoint fails.
func (w *Worker) dispatchTitles(ctx context.Context, wg *sync.WaitGroup, cfg Config, paused bool) {
	if paused {
		return
	}
	w.mu.Lock()
	free := !w.titlesBusy
	w.mu.Unlock()
	if !free {
		return
	}
	jobs, err := w.store.PendingTitles(ctx, titleBatch, maxAttempts, w.now().Add(-retryDelay))
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot list queued titles", "error", err)
		}
		return
	}
	if len(jobs) == 0 {
		return
	}
	w.mu.Lock()
	w.titlesBusy = true
	w.mu.Unlock()
	wg.Go(func() {
		panicked := diag.Guard("ai titles", func() { w.processTitles(ctx, cfg, jobs) })
		if panicked {
			for _, job := range jobs {
				if err := w.store.FailTitle(ctx, job.ID, w.now()); err != nil && ctx.Err() == nil {
					slog.Error("Cannot record a failed title", "error", err)
				}
			}
		}
		w.mu.Lock()
		w.titlesBusy = false
		w.mu.Unlock()
		if !panicked {
			w.Wake()
		}
	})
}

// titleOutcome collects what a batch and the halves it was split into came to.
type titleOutcome struct {
	translated []store.TitleResult
	failed     []int64 // titles that failed alone, the ones that cost an attempt
}

// translateTitles sends the titles of batch as one request and returns the translations in the same order. The error
// is a Code for a failed call, and ErrUpstream for a reply that is not an array of as many non-empty strings.
func (w *Worker) translateTitles(ctx context.Context, cfg Config, batch []store.TitleJob) ([]string, error) {
	titles := make([]string, len(batch))
	for i, job := range batch {
		titles[i] = truncate(strings.TrimSpace(job.Title), maxTitleRunes)
	}
	user, err := json.Marshal(titles)
	if err != nil {
		return nil, err
	}
	reply, _, err := w.call(ctx, cfg, strings.ReplaceAll(PromptTitles, "{language}", Languages[cfg.Language]), string(user))
	if err != nil {
		return nil, err
	}
	return parseTitles(reply, len(batch))
}

// parseTitles reads the reply of a batch: a JSON array of n non-empty strings, possibly inside a Markdown code fence.
func parseTitles(reply string, n int) ([]string, error) {
	reply = strings.TrimSpace(reply)
	if rest, ok := strings.CutPrefix(reply, "```"); ok {
		rest = strings.TrimPrefix(rest, "json")
		reply = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), "```"))
	}
	var texts []string
	if json.Unmarshal([]byte(reply), &texts) != nil || len(texts) != n {
		return nil, ErrUpstream
	}
	for i, text := range texts {
		if texts[i] = strings.TrimSpace(text); texts[i] == "" {
			return nil, ErrUpstream
		}
	}
	return texts, nil
}

// titleRun sends batch, and when the reply is unusable splits it in two and sends the halves, down to single
// titles; a single title that still fails is recorded in out. Each request counts against the daily cap. It stops
// with an error when the endpoint fails, the cap is reached or ctx ends; the titles not recorded in out then stay
// queued.
func (w *Worker) titleRun(ctx context.Context, cfg Config, batch []store.TitleJob, out *titleOutcome) error {
	ok, err := w.store.ReserveAICall(ctx, cfg.AutoDailyLimit, w.now())
	if err != nil {
		return err
	}
	if !ok {
		return ErrCapReached
	}
	start := w.now()
	texts, err := w.translateTitles(ctx, cfg, batch)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var code Code
	if err != nil && !errors.As(err, &code) {
		slog.Error("AI title batch failed", "error", err)
		code = ErrUpstream
	}
	switch {
	case code == "":
		w.succeeded(cfg)
		for i, job := range batch {
			out.translated = append(out.translated, store.TitleResult{ID: job.ID, Title: job.Title, Text: texts[i]})
		}
		return nil
	case endpointFailure(code):
		// A timeout is the endpoint's too: a batch is short, so it is not the titles' doing.
		w.failed(cfg, start, err, code, false)
		return err
	}
	slog.Warn("AI title batch failed", "titles", len(batch), "model", cfg.Model, "code", string(code))
	if len(batch) == 1 {
		out.failed = append(out.failed, batch[0].ID)
		return nil
	}
	half := len(batch) / 2
	if err := w.titleRun(ctx, cfg, batch[:half], out); err != nil {
		return err
	}
	return w.titleRun(ctx, cfg, batch[half:], out)
}

// processTitles works through one batch of queued titles and records the outcome. Titles of an entry already in the
// target language, which can have become known since the title was queued, are taken out of the queue unsent.
func (w *Worker) processTitles(ctx context.Context, cfg Config, jobs []store.TitleJob) {
	var skip []int64
	var batch []store.TitleJob
	for _, job := range jobs {
		if job.Language != "" && job.Language == cfg.Language {
			skip = append(skip, job.ID)
			continue
		}
		batch = append(batch, job)
	}
	if len(skip) > 0 {
		if err := w.store.SkipTitles(ctx, skip); err != nil && ctx.Err() == nil {
			slog.Error("Cannot skip titles", "error", err)
		}
	}
	if len(batch) == 0 {
		return
	}
	var out titleOutcome
	err := w.titleRun(ctx, cfg, batch, &out)
	if ctx.Err() != nil {
		return
	}
	if len(out.translated) > 0 {
		if err := w.store.SaveTitles(ctx, out.translated, w.now()); err != nil {
			slog.Error("Cannot save translated titles", "error", err)
			return
		}
	}
	for _, id := range out.failed {
		if err := w.store.FailTitle(ctx, id, w.now()); err != nil {
			slog.Error("Cannot record a failed title", "error", err)
		}
	}
	switch {
	case errors.Is(err, ErrCapReached):
		slog.Warn("Title translation stopped by the daily cap")
	case err != nil && !endpointFailure(codeOf(err)):
		slog.Error("Title translation failed", "error", err)
	default:
		slog.Info("Titles translated", "translated", len(out.translated), "failed", len(out.failed), "model", cfg.Model)
	}
}

// codeOf is the Code in err, "" when there is none.
func codeOf(err error) Code {
	var code Code
	errors.As(err, &code)
	return code
}
