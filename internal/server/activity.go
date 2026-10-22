package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/yangflow/oriel/internal/store"
)

// imageStats is implemented by an Images that keeps a cache on disk.
type imageStats interface {
	CacheStats() (count int, bytes int64)
}

// vacuumState is what the last (or running) vacuum reports. It lives in memory only (docs/product.md, "活动和存储接口").
type vacuumState struct {
	State      string `json:"state"` // idle, running, done or failed
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt"`
	FreedBytes int64  `json:"freedBytes"`
	Error      string `json:"error"`
}

type attemptJSON struct {
	At         int64  `json:"at"`
	DurationMs int64  `json:"durationMs"`
	OK         bool   `json:"ok"`
	Status     int    `json:"status"`
	NewEntries int    `json:"newEntries"`
	Error      string `json:"error"`
}

type activitySourceJSON struct {
	ID         int64         `json:"id"`
	URL        string        `json:"url"`
	RSSHubPath string        `json:"rsshubPath"`
	Title      string        `json:"title"`
	Paused     bool          `json:"paused"`
	Attempts   []attemptJSON `json:"attempts"`
}

type fetchingJSON struct {
	ID         int64  `json:"id"`
	URL        string `json:"url"`
	RSSHubPath string `json:"rsshubPath"`
	Title      string `json:"title"`
}

// activity answers GET /api/activity: the sources being fetched now and the recent attempts of each source.
func (s *Server) activity(w http.ResponseWriter, r *http.Request) {
	var ids []int64
	if s.fetching != nil {
		ids = s.fetching()
	}
	now, err := s.store.SourceIdentities(r.Context(), ids)
	if err != nil {
		internal(w, err)
		return
	}
	recent, err := s.store.RecentFetches(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	fetching := make([]fetchingJSON, len(now))
	for i, src := range now {
		fetching[i] = fetchingJSON{ID: src.ID, URL: src.URL, RSSHubPath: src.RSSHubPath, Title: src.Title}
	}
	sources := make([]activitySourceJSON, len(recent))
	for i, src := range recent {
		item := activitySourceJSON{ID: src.ID, URL: src.URL, RSSHubPath: src.RSSHubPath, Title: src.Title, Paused: src.Paused,
			Attempts: make([]attemptJSON, len(src.Attempts))}
		for j, a := range src.Attempts {
			item.Attempts[j] = attemptJSON{At: a.At.UnixMilli(), DurationMs: a.Duration.Milliseconds(), OK: a.OK, Status: a.Status,
				NewEntries: a.NewEntries, Error: a.Error}
		}
		sources[i] = item
	}
	reply(w, http.StatusOK, map[string]any{"keep": store.FetchLogKeep, "fetching": fetching, "sources": sources})
}

// storage answers GET /api/storage: the size of the database, the image cache and the state of the vacuum.
func (s *Server) storage(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Storage(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	var images int
	var imageBytes int64
	if stats, ok := s.images.(imageStats); ok {
		images, imageBytes = stats.CacheStats()
	}
	copies, copyBytes, err := s.store.CopyStats(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{
		"copies": copies, "copyBytes": copyBytes,
		"databaseBytes": st.DatabaseBytes, "reclaimableBytes": st.ReclaimableBytes, "entries": st.Entries,
		"images": images, "imageBytes": imageBytes, "vacuum": s.vacuumNow(),
	})
}

func (s *Server) vacuumNow() vacuumState {
	s.vacuumMu.Lock()
	defer s.vacuumMu.Unlock()
	return s.vacuum
}

// startVacuum answers POST /api/storage/vacuum. It starts the vacuum in a goroutine of its own, so that the request
// ending, or timing out, does not interrupt it, and answers 202 at once.
func (s *Server) startVacuum(w http.ResponseWriter, r *http.Request) {
	before, err := s.store.Storage(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	s.vacuumMu.Lock()
	if s.vacuum.State == "running" {
		s.vacuumMu.Unlock()
		fail(w, http.StatusConflict, "vacuum_running")
		return
	}
	s.vacuum = vacuumState{State: "running", StartedAt: s.now().UnixMilli()}
	started := s.vacuum
	s.vacuumMu.Unlock()
	go s.runVacuum(before.DatabaseBytes)
	reply(w, http.StatusAccepted, map[string]any{"vacuum": started})
}

func (s *Server) runVacuum(before int64) {
	err := s.store.Vacuum(context.Background())
	var after store.Storage
	if err == nil {
		after, err = s.store.Storage(context.Background())
	}
	s.vacuumMu.Lock()
	defer s.vacuumMu.Unlock()
	s.vacuum.FinishedAt = s.now().UnixMilli()
	if err != nil {
		if !errors.Is(err, store.ErrVacuumRunning) {
			slog.Error("Cannot vacuum the database", "error", err)
		}
		s.vacuum.State, s.vacuum.Error = "failed", err.Error()
		return
	}
	s.vacuum.State, s.vacuum.FreedBytes = "done", max(before-after.DatabaseBytes, 0)
}
