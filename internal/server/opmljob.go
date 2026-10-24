package server

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"

	"github.com/yangflow/oriel/internal/diag"
)

// The phases of an OPML import job.
const (
	opmlIdle    = "idle"
	opmlRunning = "running"
	opmlDone    = "done"
	opmlFailed  = "failed"
)

type opmlProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

type opmlStatus struct {
	ID       string       `json:"id,omitempty"`
	Phase    string       `json:"phase"`
	Progress opmlProgress `json:"progress"`
	Result   *opmlResult  `json:"result,omitempty"`
	Error    string       `json:"error,omitempty"`
}

// opmlState is the one OPML import in progress, a job or a synchronous request, and the job that ended last. It lives
// in memory only. The job stays until the next one starts.
type opmlState struct {
	mu      sync.Mutex
	syncing bool // a synchronous POST /api/opml/import is running
	job     *opmlStatus
	closed  bool            // Close has begun: no job starts any more
	ctx     context.Context // the context of every job; Close cancels it
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	step    func(done int) // called by a job after each subscription, outside the lock; tests replace it
}

func newOPMLState() *opmlState {
	ctx, cancel := context.WithCancel(context.Background())
	return &opmlState{ctx: ctx, cancel: cancel}
}

// busy reports whether an import is running; st.mu must be held.
func (st *opmlState) busy() bool {
	return st.syncing || (st.job != nil && st.job.Phase == opmlRunning)
}

// begin takes the lock for a synchronous import, or reports false when an import is running.
func (st *opmlState) begin() bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.busy() {
		return false
	}
	st.syncing = true
	return true
}

func (st *opmlState) endSync() {
	st.mu.Lock()
	st.syncing = false
	st.mu.Unlock()
}

// status returns a copy of the job as the API shows it; st.mu must be held.
func (st *opmlState) status() opmlStatus {
	if st.job == nil {
		return opmlStatus{Phase: opmlIdle}
	}
	out := *st.job
	if out.Result != nil {
		res := *out.Result
		out.Result = &res
	}
	return out
}

// startOPML checks the file, then adds its subscriptions in the background and answers with the status of the job.
func (s *Server) startOPML(w http.ResponseWriter, r *http.Request) {
	doc, ok := readOPML(w, r)
	if !ok {
		return
	}
	st := s.opml
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.closed {
		fail(w, http.StatusServiceUnavailable, "server_closing")
		return
	}
	if st.busy() {
		fail(w, http.StatusConflict, "opml_import_busy")
		return
	}
	job := &opmlStatus{ID: strconv.FormatUint(rand.Uint64(), 36), Phase: opmlRunning, Progress: opmlProgress{Total: countOutlines(doc.Body)}}
	st.job = job
	st.wg.Add(1)
	go func() {
		defer st.wg.Done()
		s.runOPML(job, doc)
	}()
	reply(w, http.StatusAccepted, st.status())
}

// runOPML adds the sources of one job. A panic in it is logged with its stack and fails the job, so the page does not
// wait for a job that is gone.
func (s *Server) runOPML(job *opmlStatus, doc opmlDocument) {
	st := s.opml
	var res opmlResult
	var err error
	progress := func(done int) {
		st.mu.Lock()
		job.Progress.Done = done
		st.mu.Unlock()
		if st.step != nil {
			st.step(done)
		}
	}
	panicked := diag.Guard("opml import", func() { res, err = s.addSources(st.ctx, doc, progress) })
	st.mu.Lock()
	defer st.mu.Unlock()
	switch {
	case panicked:
		job.Phase, job.Error = opmlFailed, "import_failed"
	case err != nil:
		slog.Error("OPML import failed", "error", err)
		job.Phase, job.Error, job.Result = opmlFailed, "import_failed", &res
	default:
		job.Phase, job.Result = opmlDone, &res
	}
}

func (s *Server) opmlJobStatus(w http.ResponseWriter, r *http.Request) {
	s.opml.mu.Lock()
	defer s.opml.mu.Unlock()
	reply(w, http.StatusOK, s.opml.status())
}

// Close stops the OPML import in progress and waits until it has stopped.
func (s *Server) Close() {
	s.opml.mu.Lock()
	s.opml.closed = true // no job starts from here on, so wg is not added to while it is waited for
	s.opml.mu.Unlock()
	s.opml.cancel()
	s.opml.wg.Wait()
}
