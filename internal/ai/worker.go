package ai

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

const (
	tick        = 15 * time.Second
	concurrency = 2
	callTimeout = 120 * time.Second
	maxAttempts = 3
	retryDelay  = 5 * time.Minute
)

// Worker works through the queue of AI results in the background.
type Worker struct {
	store     *store.Store
	client    *http.Client // the endpoint the user entered, which may be on this machine or the LAN
	userAgent string
	timeout   time.Duration
	now       func() time.Time
	wake      chan struct{}

	mu       sync.Mutex
	inFlight map[store.AIJob]bool
}

// New returns a worker that identifies itself as Oriel/version.
func New(st *store.Store, version string) *Worker {
	return &Worker{
		store:     st,
		client:    &http.Client{},
		userAgent: "Oriel/" + version,
		timeout:   callTimeout,
		now:       time.Now,
		wake:      make(chan struct{}, 1),
		inFlight:  map[store.AIJob]bool{},
	}
}

// Wake makes a running worker look at the queue now.
func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run works through the queue until ctx is canceled, then waits for the calls in flight to stop.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		w.dispatch(ctx, &wg)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-w.wake:
		}
	}
}

// dispatch starts jobs until two are in flight.
func (w *Worker) dispatch(ctx context.Context, wg *sync.WaitGroup) {
	cfg, err := Load(ctx, w.store)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot read the AI settings", "error", err)
		}
		return
	}
	if !cfg.Configured() {
		return
	}
	w.mu.Lock()
	busy := len(w.inFlight)
	w.mu.Unlock()
	if busy >= concurrency {
		return
	}
	jobs, err := w.store.AIJobs(ctx, concurrency+busy, maxAttempts, w.now().Add(-retryDelay))
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot list AI jobs", "error", err)
		}
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, job := range jobs {
		if len(w.inFlight) >= concurrency {
			break
		}
		if w.inFlight[job] {
			continue
		}
		w.inFlight[job] = true
		wg.Go(func() {
			w.process(ctx, cfg, job)
			w.mu.Lock()
			delete(w.inFlight, job)
			w.mu.Unlock()
			w.Wake()
		})
	}
}

// process runs one job and records the outcome. A job cut short by shutdown stays queued.
func (w *Worker) process(ctx context.Context, cfg Config, job store.AIJob) {
	start := w.now()
	in, err := w.store.AIInput(ctx, job.EntryID)
	if errors.Is(err, store.ErrNotFound) {
		return
	}
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot read an entry for AI", "entry", job.EntryID, "error", err)
		}
		return
	}
	prepared, ready := prepare(cfg, in, job.Task)
	if !ready {
		return
	}
	text, model, err := w.run(ctx, cfg, prepared)
	if ctx.Err() != nil {
		return
	}
	var code Code
	if err != nil && !errors.As(err, &code) {
		slog.Error("AI task failed", "entry", job.EntryID, "task", job.Task, "error", err)
		code = ErrUpstream
	}
	if err := w.store.SaveAI(ctx, job.EntryID, job.Task, text, model, prepared.fingerprint, string(code), w.now()); err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot save an AI result", "entry", job.EntryID, "task", job.Task, "error", err)
		}
		return
	}
	elapsed := w.now().Sub(start).Round(time.Millisecond)
	if code != "" {
		slog.Warn("AI task failed", "entry", job.EntryID, "task", job.Task, "model", cfg.Model, "code", string(code), "duration", elapsed)
		return
	}
	slog.Info("AI task done", "entry", job.EntryID, "task", job.Task, "model", model, "duration", elapsed)
}

// run sends every chunk in order and joins the replies.
func (w *Worker) run(ctx context.Context, cfg Config, j job) (string, string, error) {
	if len(j.chunks) == 0 {
		return "", "", ErrEmpty
	}
	var parts []string
	var model string
	for _, chunk := range j.chunks {
		text, m, err := w.call(ctx, cfg, j.prompt, chunk)
		if err != nil {
			return "", "", err
		}
		parts, model = append(parts, text), m
	}
	return strings.Join(parts, "\n\n"), model, nil
}

func (w *Worker) call(ctx context.Context, cfg Config, system, user string) (string, string, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, w.timeout, errCallTimeout)
	defer cancel()
	return complete(ctx, w.client, cfg, w.userAgent, system, user)
}

// Request queues a task the user asked for, unless a result for the same input is done or already queued.
// It returns ErrNotConfigured, and store.ErrNotFound for a missing entry.
func (w *Worker) Request(ctx context.Context, entryID int64, task string) (store.AIResult, error) {
	cfg, err := Load(ctx, w.store)
	if err != nil {
		return store.AIResult{}, err
	}
	if !cfg.Configured() {
		return store.AIResult{}, ErrNotConfigured
	}
	in, err := w.store.AIInput(ctx, entryID)
	if err != nil {
		return store.AIResult{}, err
	}
	results, err := w.store.AIResults(ctx, entryID)
	if err != nil {
		return store.AIResult{}, err
	}
	current, exists := results[task]
	switch {
	case exists && current.Status == "pending":
		return current, nil
	case exists && current.Status == "ok":
		if prepared, ready := prepare(cfg, in, task); ready && prepared.fingerprint == current.Fingerprint {
			return current, nil
		}
	}
	result, err := w.store.RequestAI(ctx, entryID, task, w.now())
	if err != nil {
		return store.AIResult{}, err
	}
	w.Wake()
	return result, nil
}

// Test sends a short request with the current settings and returns the model that answered.
func (w *Worker) Test(ctx context.Context) (string, error) {
	cfg, err := Load(ctx, w.store)
	if err != nil {
		return "", err
	}
	if !cfg.Configured() {
		return "", ErrNotConfigured
	}
	_, model, err := w.call(ctx, cfg, "Reply with OK.", "OK")
	return model, err
}
