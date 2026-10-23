package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yangflow/oriel/internal/diag"
	"github.com/yangflow/oriel/internal/importer"
	"github.com/yangflow/oriel/internal/store"
)

// maxImportRequest is the largest request body of POST /api/import/start.
const maxImportRequest = 64 << 10

// maxImportSkipped is how many skipped items a result lists; skippedTotal counts them all.
const maxImportSkipped = 100

// The phases of an import job.
const (
	phaseIdle     = "idle"
	phaseFetching = "fetching"
	phaseReady    = "ready"
	phaseWriting  = "writing"
	phaseDone     = "done"
	phaseFailed   = "failed"
	phaseCanceled = "canceled"
)

type importProgress struct {
	Stage string `json:"stage"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

type importSkipped struct {
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Address string `json:"address"`
	Error   string `json:"error"`
}

type importResult struct {
	AddedSources    int             `json:"addedSources"`
	ExistingSources int             `json:"existingSources"`
	StarredAdded    int             `json:"starredAdded"`
	StarredExisting int             `json:"starredExisting"`
	ReadMarked      int             `json:"readMarked"`
	UnmatchedRead   int             `json:"unmatchedRead"`
	FoldersSkipped  int             `json:"foldersSkipped"`
	Skipped         []importSkipped `json:"skipped"`
	SkippedTotal    int             `json:"skippedTotal"`
}

func (r *importResult) skip(item importSkipped) {
	r.SkippedTotal++
	if len(r.Skipped) < maxImportSkipped {
		r.Skipped = append(r.Skipped, item)
	}
}

// importJob is the one import in progress or just finished. It lives in memory only and holds no credential: that
// stays in the goroutine that fetches, and is gone when the fetch ends.
type importJob struct {
	phase    string
	service  string
	progress importProgress
	counts   importer.Counts
	result   *importResult
	err      string
	snap     *importer.Snapshot
	cancel   context.CancelFunc
}

type importStatus struct {
	Phase    string          `json:"phase"`
	Service  string          `json:"service,omitempty"`
	Progress importProgress  `json:"progress"`
	Counts   importer.Counts `json:"counts"`
	Result   *importResult   `json:"result,omitempty"`
	Error    string          `json:"error,omitempty"`
}

// importState guards the job.
type importState struct {
	mu  sync.Mutex
	job *importJob
}

// status returns the job as the API shows it; s.mu must be held.
func (st *importState) status() importStatus {
	if st.job == nil {
		return importStatus{Phase: phaseIdle}
	}
	j := st.job
	return importStatus{Phase: j.phase, Service: j.service, Progress: j.progress, Counts: j.counts, Result: j.result, Error: j.err}
}

func (j *importJob) busy() bool {
	return j.phase == phaseFetching || j.phase == phaseReady || j.phase == phaseWriting
}

func (s *Server) startImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Service    string `json:"service"`
		BaseURL    string `json:"baseUrl"`
		Username   string `json:"username"`
		Credential string `json:"credential"`
	}
	if !jsonContent(r) {
		fail(w, http.StatusUnsupportedMediaType, "unsupported_media_type")
		return
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxImportRequest)).Decode(&body); err != nil {
		fail(w, http.StatusBadRequest, "invalid_json")
		return
	}
	imp, ok := importer.For(body.Service)
	if !ok {
		fail(w, http.StatusBadRequest, "invalid_service")
		return
	}
	base, err := importer.ParseBase(body.BaseURL, body.Service)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_base_url")
		return
	}
	if strings.TrimSpace(body.Credential) == "" {
		fail(w, http.StatusBadRequest, "missing_credential")
		return
	}
	req := importer.Request{Base: base, Username: strings.TrimSpace(body.Username), Credential: strings.TrimSpace(body.Credential)}
	body.Credential = ""

	s.imports.mu.Lock()
	defer s.imports.mu.Unlock()
	if s.imports.job != nil && s.imports.job.busy() {
		fail(w, http.StatusConflict, "import_busy")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &importJob{phase: phaseFetching, service: body.Service, cancel: cancel}
	s.imports.job = job
	go s.guardImport(job, func() { s.fetchImport(ctx, job, imp, req) })
	reply(w, http.StatusAccepted, s.imports.status())
}

// guardImport runs one step of an import in its own goroutine. A panic in it is logged with its stack and fails the
// import, so the page does not wait for a job that is gone.
func (s *Server) guardImport(job *importJob, step func()) {
	if !diag.Guard("import", step) {
		return
	}
	s.imports.mu.Lock()
	defer s.imports.mu.Unlock()
	if job.busy() {
		job.phase, job.err, job.snap, job.progress = phaseFailed, "internal", nil, importProgress{}
	}
}

// fetchImport reads the snapshot. It is the only place the credential goes, and drops it when the fetch ends.
func (s *Server) fetchImport(ctx context.Context, job *importJob, imp importer.Importer, req importer.Request) {
	progress := func(stage string, done, total int) {
		s.imports.mu.Lock()
		defer s.imports.mu.Unlock()
		if job.phase == phaseFetching {
			job.progress = importProgress{Stage: stage, Done: done, Total: total}
		}
	}
	snap, err := imp.Fetch(ctx, s.importClient(), req, progress)
	req = importer.Request{}
	s.imports.mu.Lock()
	defer s.imports.mu.Unlock()
	if job.phase != phaseFetching {
		return // canceled meanwhile
	}
	if err != nil {
		code := importer.Code(err)
		if code == "" {
			code = "internal"
		}
		job.phase, job.err = phaseFailed, code
		slog.Info("Import failed", "service", job.service, "code", code)
		return
	}
	job.phase, job.snap, job.counts = phaseReady, snap, snap.Counts()
	job.progress = importProgress{}
}

func (s *Server) importStatus(w http.ResponseWriter, r *http.Request) {
	s.imports.mu.Lock()
	defer s.imports.mu.Unlock()
	reply(w, http.StatusOK, s.imports.status())
}

func (s *Server) confirmImport(w http.ResponseWriter, r *http.Request) {
	s.imports.mu.Lock()
	defer s.imports.mu.Unlock()
	job := s.imports.job
	if job == nil || job.phase != phaseReady {
		fail(w, http.StatusConflict, "import_not_ready")
		return
	}
	job.phase = phaseWriting
	job.progress = importProgress{Stage: "writing", Total: len(job.snap.Feeds)}
	snap := job.snap
	job.snap = nil
	ctx, cancel := context.WithCancel(context.Background())
	job.cancel = cancel
	go s.guardImport(job, func() { s.writeImport(ctx, job, snap) })
	reply(w, http.StatusOK, s.imports.status())
}

func (s *Server) cancelImport(w http.ResponseWriter, r *http.Request) {
	s.imports.mu.Lock()
	defer s.imports.mu.Unlock()
	if job := s.imports.job; job != nil {
		if job.busy() {
			job.cancel()
			job.phase, job.snap, job.progress = phaseCanceled, nil, importProgress{}
		} else {
			s.imports.job = nil
		}
	}
	reply(w, http.StatusOK, s.imports.status())
}

// writeImport writes a snapshot: the subscriptions the way an OPML import does, then the entries of each.
func (s *Server) writeImport(ctx context.Context, job *importJob, snap *importer.Snapshot) {
	res, err := s.writeSnapshot(ctx, job, snap)
	s.imports.mu.Lock()
	defer s.imports.mu.Unlock()
	if job.phase != phaseWriting {
		return // canceled meanwhile
	}
	if err != nil {
		slog.Error("Import failed", "service", job.service, "error", err)
		job.phase, job.err = phaseFailed, "internal"
		return
	}
	job.phase, job.result = phaseDone, res
}

func (s *Server) setImportProgress(job *importJob, done int) {
	s.imports.mu.Lock()
	defer s.imports.mu.Unlock()
	if job.phase == phaseWriting {
		job.progress.Done = done
	}
}

func (s *Server) writeSnapshot(ctx context.Context, job *importJob, snap *importer.Snapshot) (*importResult, error) {
	res := &importResult{Skipped: []importSkipped{}, FoldersSkipped: len(snap.Folders)}
	spread := min(importSpreadMax, time.Duration(len(snap.Feeds))*importSpreadEach)
	ids := map[string]int64{}
	for i, f := range snap.Feeds {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		src := store.NewSource{Kind: "article", URL: strings.TrimSpace(f.URL), FetchInterval: defaultInterval, FetchIntervalAuto: true}
		title := strings.TrimSpace(f.Title)
		if title == src.URL {
			title = ""
		}
		if code := checkSource(src); code != "" {
			res.skip(importSkipped{Kind: "source", Title: title, Address: src.URL, Error: code})
			continue
		}
		src.FirstFetchDelay = time.Duration(rand.Int64N(int64(max(spread, 1))))
		created, err := s.store.CreateSource(ctx, src, title, s.now())
		switch {
		case errors.Is(err, store.ErrExists):
			res.ExistingSources++
			var exists *store.ExistsError
			if errors.As(err, &exists) {
				ids[f.URL] = exists.ID
			} else if ids[f.URL], err = s.store.SourceIDByFeedURL(ctx, src.URL); err != nil {
				return nil, err
			}
		case err != nil:
			return nil, err
		default:
			res.AddedSources++
			ids[f.URL] = created.ID
		}
		s.setImportProgress(job, i+1)
	}

	var order []string
	groups := map[string][]importer.Entry{}
	for _, e := range snap.Entries {
		if _, ok := groups[e.FeedURL]; !ok {
			order = append(order, e.FeedURL)
		}
		groups[e.FeedURL] = append(groups[e.FeedURL], e)
	}
	for _, feedURL := range order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entries := groups[feedURL]
		id, ok := ids[feedURL]
		if !ok && feedURL != "" {
			var err error
			switch id, err = s.store.SourceIDByFeedURL(ctx, feedURL); {
			case err == nil:
				ok = true
			case !errors.Is(err, store.ErrNotFound):
				return nil, err
			}
		}
		if !ok {
			for _, e := range entries {
				if e.Starred {
					res.skip(importSkipped{Kind: "starred", Title: e.Title, Address: e.Link, Error: "unknown_source"})
				} else {
					res.UnmatchedRead++
				}
			}
			continue
		}
		in := make([]store.ImportedEntry, len(entries))
		for i, e := range entries {
			in[i] = store.ImportedEntry{Title: e.Title, URL: e.Link, Author: e.Author, PublishedAt: e.Published,
				ContentHTML: e.ContentHTML, Read: e.Read, Starred: e.Starred}
		}
		out, err := s.store.ImportEntries(ctx, id, in, s.now())
		if err != nil {
			return nil, err
		}
		for i, o := range out {
			e := entries[i]
			switch o.Outcome {
			case store.ImportAdded:
				res.StarredAdded++
			case store.ImportExisting:
				if e.Starred {
					res.StarredExisting++
				}
			case store.ImportUnmatched:
				res.UnmatchedRead++
			case store.ImportInvalid:
				if e.Starred {
					res.skip(importSkipped{Kind: "starred", Title: e.Title, Address: e.Link, Error: "invalid_link"})
				}
			}
			if o.ReadSet {
				res.ReadMarked++
			}
		}
	}
	return res, nil
}
