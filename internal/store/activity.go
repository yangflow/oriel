package store

import (
	"context"
	"errors"
	"strings"
	"time"
)

// FetchLogKeep is how many attempts of each source are kept. docs/product.md ("抓取记录") gives the reason.
const FetchLogKeep = 10

// fetchLogError is the longest error text kept, in characters.
const fetchLogError = 300

// ErrVacuumRunning reports that a vacuum is already running.
var ErrVacuumRunning = errors.New("a vacuum is already running")

// FetchAttempt is the result of one fetch of a source.
type FetchAttempt struct {
	SourceID   int64
	At         time.Time // when the fetch ended
	Duration   time.Duration
	OK         bool
	Status     int // the last HTTP status received; 0 when there was none
	NewEntries int
	Error      string
}

// SourceAttempts is a source with its recent attempts, newest first.
type SourceAttempts struct {
	ID         int64
	URL        string
	RSSHubPath string
	Title      string
	Paused     bool
	Attempts   []FetchAttempt
}

// LogFetch adds an attempt and trims the source's older ones beyond FetchLogKeep, in one transaction.
func (s *Store) LogFetch(ctx context.Context, a FetchAttempt) error {
	if runes := []rune(a.Error); len(runes) > fetchLogError {
		a.Error = string(runes[:fetchLogError])
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO fetch_log (source_id, at, duration_ms, ok, status, new_entries, error) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.SourceID, a.At.UnixMilli(), a.Duration.Milliseconds(), a.OK, a.Status, a.NewEntries, a.Error); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM fetch_log WHERE source_id = ?1 AND id NOT IN (
			SELECT id FROM fetch_log WHERE source_id = ?1 ORDER BY id DESC LIMIT ?2)`, a.SourceID, FetchLogKeep); err != nil {
		return err
	}
	return tx.Commit()
}

// RecentFetches returns every source that has attempts, the one fetched last first, each with its attempts newest
// first. Two queries (the attempts, the sources) are merged here, so there is no subquery per source.
func (s *Store) RecentFetches(ctx context.Context) ([]SourceAttempts, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.source_id, l.at, l.duration_ms, l.ok, l.status, l.new_entries, l.error
		FROM fetch_log l JOIN sources s ON s.id = l.source_id WHERE s.deleted_at IS NULL ORDER BY l.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	index := map[int64]int{}
	var list []SourceAttempts
	for rows.Next() {
		var a FetchAttempt
		var at, duration int64
		if err := rows.Scan(&a.SourceID, &at, &duration, &a.OK, &a.Status, &a.NewEntries, &a.Error); err != nil {
			return nil, err
		}
		a.At, a.Duration = time.UnixMilli(at), time.Duration(duration)*time.Millisecond
		i, ok := index[a.SourceID]
		if !ok {
			i = len(list)
			index[a.SourceID] = i
			list = append(list, SourceAttempts{ID: a.SourceID})
		}
		list[i].Attempts = append(list[i].Attempts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// The log is read newest first, so the sources come in the order of their newest attempt.
	return list, s.fillIdentity(ctx, list, index)
}

// fillIdentity adds the name and address of each source in list; index maps a source id to its place in list.
func (s *Store) fillIdentity(ctx context.Context, list []SourceAttempts, index map[int64]int) error {
	if len(list) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, url, rsshub_path, title, paused FROM sources WHERE deleted_at IS NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var url, path, title string
		var paused bool
		if err := rows.Scan(&id, &url, &path, &title, &paused); err != nil {
			return err
		}
		if i, ok := index[id]; ok {
			list[i].URL, list[i].RSSHubPath, list[i].Title, list[i].Paused = url, path, title, paused
		}
	}
	return rows.Err()
}

// SourceIdentities returns the sources with the given ids that still exist, ordered by id, without attempts.
func (s *Store) SourceIdentities(ctx context.Context, ids []int64) ([]SourceAttempts, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, url, rsshub_path, title, paused FROM sources WHERE deleted_at IS NULL AND id IN (?`+strings.Repeat(",?", len(ids)-1)+`) ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []SourceAttempts
	for rows.Next() {
		var src SourceAttempts
		if err := rows.Scan(&src.ID, &src.URL, &src.RSSHubPath, &src.Title, &src.Paused); err != nil {
			return nil, err
		}
		list = append(list, src)
	}
	return list, rows.Err()
}

// Storage is what the database takes up.
type Storage struct {
	DatabaseBytes    int64 // pages in the file times the page size, without the write-ahead log
	ReclaimableBytes int64 // the free pages in it, which a vacuum gives back
	Entries          int64
}

// Storage measures the database.
func (s *Store) Storage(ctx context.Context) (Storage, error) {
	var pages, free, size int64
	for pragma, into := range map[string]*int64{"page_count": &pages, "freelist_count": &free, "page_size": &size} {
		if err := s.db.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(into); err != nil {
			return Storage{}, err
		}
	}
	st := Storage{DatabaseBytes: pages * size, ReclaimableBytes: free * size}
	return st, s.db.QueryRowContext(ctx, "SELECT count(*) FROM entries").Scan(&st.Entries)
}

// Vacuuming reports whether a vacuum is running, so that the fetch loop can leave new fetches until it ends.
func (s *Store) Vacuuming() bool { return s.vacuuming.Load() }

// Vacuum rewrites the database file to give back its free pages, then truncates the write-ahead log so that the file
// really gets smaller. It holds the write lock for as long as it runs and needs free disk space about the size of the
// database; when it fails the database is as it was. Only one runs at a time, a second returns ErrVacuumRunning.
func (s *Store) Vacuum(ctx context.Context) error {
	if !s.vacuuming.CompareAndSwap(false, true) {
		return ErrVacuumRunning
	}
	defer s.vacuuming.Store(false)
	// auto_vacuum only changes through a VACUUM on the same connection, so this one also converts a database made
	// before incremental vacuum was used.
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA auto_vacuum = INCREMENTAL"); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "VACUUM"); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, checkpoint)
	return err
}
