package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

// backupTimeout is how long one backup or restore may read or write, in place of the server's one-minute timeouts: the
// file can be gigabytes.
const backupTimeout = 30 * time.Minute

// extendDeadlines gives the request the long timeouts. A response writer that cannot set them (a test recorder) keeps
// the ones it has.
func extendDeadlines(w http.ResponseWriter) {
	rc := http.NewResponseController(w)
	deadline := time.Now().Add(backupTimeout)
	rc.SetReadDeadline(deadline)
	rc.SetWriteDeadline(deadline)
}

// lazyWriter starts the response only when the first byte is written, so that a backup that fails before it has any
// bytes can still answer with a JSON error.
type lazyWriter struct {
	w       http.ResponseWriter
	start   func()
	started bool
}

func (l *lazyWriter) Write(p []byte) (int, error) {
	if !l.started {
		l.started = true
		l.start()
	}
	return l.w.Write(p)
}

// backup streams a backup file of all the data.
func (s *Server) backup(w http.ResponseWriter, r *http.Request) {
	extendDeadlines(w)
	out := &lazyWriter{w: w, start: func() {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="Oriel-`+s.now().Format("2006-01-02")+`.oriel-backup"`)
	}}
	if err := s.store.Backup(r.Context(), out, s.version); err != nil {
		if out.started {
			// Part of the file is gone; cutting the connection is the only way to tell the client it is incomplete.
			panic(http.ErrAbortHandler)
		}
		internal(w, err)
		return
	}
}

type restoreJSON struct {
	Sources int `json:"sources"`
	Entries int `json:"entries"`
}

// restore replaces all the data with the uploaded backup, or changes nothing.
func (s *Server) restore(w http.ResponseWriter, r *http.Request) {
	if !s.restoring.TryLock() {
		fail(w, http.StatusConflict, "restore_running")
		return
	}
	defer s.restoring.Unlock()
	extendDeadlines(w)
	prepared, err := s.store.PrepareRestore(r.Context(), http.MaxBytesReader(w, r.Body, store.MaxBackup), store.MaxBackup)
	if err != nil {
		replyRestoreError(w, err)
		return
	}
	defer prepared.Discard()
	// The upload is checked; from here the restore runs to its end even if the client gives up, and either all of it
	// happens or none.
	ctx := context.WithoutCancel(r.Context())
	if s.pause != nil {
		resume, err := s.pause(ctx)
		if err != nil {
			internal(w, err)
			return
		}
		defer resume()
	}
	result, err := prepared.Apply(ctx)
	if err != nil {
		replyRestoreError(w, err)
		return
	}
	reply(w, http.StatusOK, restoreJSON{Sources: result.Sources, Entries: result.Entries})
}

func replyRestoreError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.Is(err, store.ErrBackupTooLarge), errors.As(err, &tooLarge):
		fail(w, http.StatusRequestEntityTooLarge, "backup_too_large")
	case errors.Is(err, store.ErrInvalidBackup):
		fail(w, http.StatusBadRequest, "invalid_backup")
	case errors.Is(err, store.ErrCorruptBackup):
		fail(w, http.StatusUnprocessableEntity, "corrupt_backup")
	case errors.Is(err, store.ErrSchemaNewer):
		fail(w, http.StatusUnprocessableEntity, "backup_newer")
	case errors.Is(err, store.ErrVacuumRunning):
		fail(w, http.StatusConflict, "vacuum_running")
	default:
		internal(w, err)
	}
}
