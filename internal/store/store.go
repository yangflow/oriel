// Package store owns the SQLite database: schema migrations and every query.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrNotFound reports that no row matched.
var ErrNotFound = errors.New("not found")

// ErrNotRetryable reports that an entry's full text cannot be fetched again: it already has it, or it is not an article.
var ErrNotRetryable = errors.New("not retryable")

// ErrLocked reports that another process already serves the same database.
var ErrLocked = errors.New("the database is in use by another Oriel process")

// ErrSchemaNewer reports a database, or a backup, made by a newer Oriel than this one.
var ErrSchemaNewer = errors.New("the data is from a newer version of Oriel")

// Store is safe for concurrent use.
type Store struct {
	db   *sql.DB
	lock *os.File
	path string // the database file; backups and restores keep their scratch files beside it

	vacuuming atomic.Bool // set while Vacuum runs
}

// Open opens or creates the database file, locks it for this process and applies pending migrations.
// The lock is a sibling file ending in .lock; the operating system releases it if the process dies.
func Open(ctx context.Context, file string) (*Store, error) {
	lockFile, err := os.OpenFile(file+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lock(lockFile); err != nil {
		lockFile.Close()
		return nil, err
	}
	// SQLite would create the file with the umask's permissions; sessions and API keys stay private to the user.
	// SQLite gives the -wal and -shm files the same permissions.
	dbFile, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		lockFile.Close()
		return nil, err
	}
	dbFile.Close()
	removeScratch(filepath.Dir(file))
	db, err := sql.Open("sqlite", dsn(file))
	if err != nil {
		lockFile.Close()
		return nil, err
	}
	s := &Store{db: db, lock: lockFile, path: file}
	if err := s.migrate(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// dsn is the connection string of a database file: write-ahead log, foreign keys, and a write lock taken when a
// transaction begins.
func dsn(file string) string {
	query := url.Values{}
	// Every transaction takes the write lock when it begins. A deferred one that reads first and writes later
	// would fail at once with SQLITE_BUSY when it upgrades, and busy_timeout does not apply to that.
	query.Set("_txlock", "immediate")
	for _, pragma := range []string{"journal_mode(WAL)", "foreign_keys(1)", "busy_timeout(5000)", "synchronous(NORMAL)"} {
		query.Add("_pragma", pragma)
	}
	return file + "?" + query.Encode()
}

// Close closes the database, then releases the lock.
func (s *Store) Close() error {
	return errors.Join(s.db.Close(), s.lock.Close())
}

// migrate runs every embedded migration newer than PRAGMA user_version, each in its own transaction.
func (s *Store) migrate(ctx context.Context) error {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	var current int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	if current > len(names) {
		return fmt.Errorf("database schema version %d is newer than this build (%d): %w", current, len(names), ErrSchemaNewer)
	}
	for i, name := range names {
		version := i + 1
		if prefix, _, _ := strings.Cut(path.Base(name), "_"); prefix != fmt.Sprintf("%04d", version) {
			return fmt.Errorf("migration %s is out of sequence", name)
		}
		if version <= current {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(version)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Ping reports whether the database can be read. It takes no write lock.
func (s *Store) Ping(ctx context.Context) error {
	var one int
	return s.db.QueryRowContext(ctx, "SELECT 1").Scan(&one)
}

// Writable reports whether a write transaction can run, without leaving a change behind.
func (s *Store) Writable(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "INSERT INTO settings (key, value) VALUES ('health_probe', '') ON CONFLICT (key) DO NOTHING")
	return err
}

// Session is one signed-in device.
type Session struct {
	ID         int64
	UserAgent  string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// CreateSession stores a session by the hash of its token and returns its ID.
func (s *Store) CreateSession(ctx context.Context, tokenHash []byte, userAgent string, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		"INSERT INTO sessions (token_hash, user_agent, created_at, last_seen_at) VALUES (?, ?, ?, ?)",
		tokenHash, userAgent, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// SessionByToken finds a session by the hash of its token.
func (s *Store) SessionByToken(ctx context.Context, tokenHash []byte) (Session, error) {
	return scanSession(s.db.QueryRowContext(ctx,
		"SELECT id, user_agent, created_at, last_seen_at FROM sessions WHERE token_hash = ?", tokenHash))
}

// TouchSession records that a session was used.
func (s *Store) TouchSession(ctx context.Context, id int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, "UPDATE sessions SET last_seen_at = ? WHERE id = ?", now.UnixMilli(), id)
	return err
}

// Sessions lists sessions used at or after since, most recently used first.
func (s *Store) Sessions(ctx context.Context, since time.Time) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, user_agent, created_at, last_seen_at FROM sessions WHERE last_seen_at >= ? ORDER BY last_seen_at DESC, id DESC",
		since.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := []Session{}
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// DeleteSession revokes a session.
func (s *Store) DeleteSession(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSessionsIdleSince removes sessions last used before cutoff.
func (s *Store) DeleteSessionsIdleSince(ctx context.Context, cutoff time.Time) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE last_seen_at < ?", cutoff.UnixMilli())
	return err
}

func scanSession(row interface{ Scan(...any) error }) (Session, error) {
	var session Session
	var created, seen int64
	if err := row.Scan(&session.ID, &session.UserAgent, &created, &seen); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, ErrNotFound
		}
		return Session{}, err
	}
	session.CreatedAt, session.LastSeenAt = time.UnixMilli(created), time.UnixMilli(seen)
	return session, nil
}
