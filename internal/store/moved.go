package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// MovedSeenNeeded is how many fetches in a row must reach the same permanent address before it is suggested.
const MovedSeenNeeded = 3

// movedShown is the SQL, with the column prefix prefix, that gives the suggested address of a source row: the one seen MovedSeenNeeded times, else none.
func movedShown(prefix string) string {
	return "CASE WHEN " + prefix + "moved_seen >= " + strconv.Itoa(MovedSeenNeeded) + " THEN " + prefix + "moved_url ELSE '' END"
}

// ErrMoveChanged reports that the address to move to is not the one the caller was shown: it changed, or there is
// none any more.
var ErrMoveChanged = errors.New("the move changed")

// NoteRedirect records what one successful fetch of a source saw. final is the address that only permanent
// redirects (301 and 308) led to, empty when the fetch was not redirected that way. The same final address as the
// last time counts one more; another one restarts the count at one; empty clears it; the address the user dismissed is
// not counted. The source's updated_at changes only when the suggestion appears or goes, so that clients see it.
func (s *Store) NoteRedirect(ctx context.Context, id int64, final string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var moved, dismissed string
	var seen int
	if err := tx.QueryRowContext(ctx, "SELECT moved_url, moved_seen, moved_dismissed FROM sources WHERE id = ?", id).Scan(&moved, &seen, &dismissed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	newMoved, newSeen := nextMoved(moved, seen, dismissed, final)
	if newMoved == moved && newSeen == seen {
		return nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE sources SET moved_url = ?, moved_seen = ? WHERE id = ?", newMoved, newSeen, id); err != nil {
		return err
	}
	if (seen >= MovedSeenNeeded) != (newSeen >= MovedSeenNeeded) {
		if _, err := tx.ExecContext(ctx, "UPDATE sources SET updated_at = ? WHERE id = ?", now.UnixMilli(), id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// NoteFetchRedirect is NoteRedirect for a source as the fetcher read it: when that state shows nothing would change,
// which is the common case of no redirect and no suggestion, it does not touch the database. The state is read again
// inside NoteRedirect's transaction when it does, so a change made in between is not lost.
func (s *Store) NoteFetchRedirect(ctx context.Context, src FeedSource, final string, now time.Time) error {
	if moved, seen := nextMoved(src.MovedURL, src.MovedSeen, src.MovedDismissed, final); moved == src.MovedURL && seen == src.MovedSeen {
		return nil
	}
	return s.NoteRedirect(ctx, src.ID, final, now)
}

// nextMoved is the suggestion state after one fetch that ended at final.
func nextMoved(moved string, seen int, dismissed, final string) (string, int) {
	switch {
	case final == "" || final == dismissed:
		return "", 0
	case final == moved:
		return moved, min(seen+1, MovedSeenNeeded)
	}
	return final, 1
}

// AcceptMove moves a subscription to the address its site moved to: the address becomes shown, the validators are
// forgotten and the source is due at once. Entries and their read and starred state stay. shown is the address the
// caller saw; it returns ErrMoveChanged when that is not the suggested one. When another subscription is the same feed
// as the new address it returns an *ExistsError and changes nothing.
func (s *Store) AcceptMove(ctx context.Context, id int64, shown string, now time.Time) (Source, error) {
	src, err := s.Source(ctx, id)
	if err != nil {
		return Source{}, err
	}
	if src.MovedTo == "" || src.MovedTo != shown {
		return Source{}, ErrMoveChanged
	}
	idx, err := s.DuplicateIndex(ctx)
	if err != nil {
		return Source{}, err
	}
	for _, key := range sourceKeys(shown, "", idx.roots) {
		if have, found := idx.live[key]; found && have != id {
			return Source{}, &ExistsError{ID: have}
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Source{}, err
	}
	defer tx.Rollback()
	// A deleted subscription with this address goes now, as when the feed is added again; if the move then fails,
	// the rollback keeps it.
	for _, gone := range idx.deletedLike(NewSource{URL: shown}) {
		if _, err := tx.ExecContext(ctx, "DELETE FROM sources WHERE id = ?", gone); err != nil {
			return Source{}, err
		}
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE sources SET url = ?1, etag = '', last_modified = '', moved_url = '', moved_seen = 0, next_fetch_at = ?2, updated_at = ?2
		WHERE id = ?3 AND moved_url = ?1 AND moved_seen >= ?4 AND deleted_at IS NULL`, shown, now.UnixMilli(), id, MovedSeenNeeded)
	if err != nil {
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			return Source{}, ErrExists
		}
		return Source{}, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return Source{}, ErrMoveChanged
	}
	if err := tx.Commit(); err != nil {
		return Source{}, err
	}
	return s.Source(ctx, id)
}

// DismissMove turns down the suggested address: it is not suggested again, and not counted, until the site moves
// somewhere else. shown is the address the caller saw; it returns ErrMoveChanged when that is not the suggested one.
func (s *Store) DismissMove(ctx context.Context, id int64, shown string, now time.Time) (Source, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE sources SET moved_dismissed = moved_url, moved_url = '', moved_seen = 0, updated_at = ?1
		WHERE id = ?2 AND moved_url = ?3 AND moved_seen >= ?4 AND deleted_at IS NULL`, now.UnixMilli(), id, shown, MovedSeenNeeded)
	if err != nil {
		return Source{}, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		if _, err := s.Source(ctx, id); err != nil {
			return Source{}, err
		}
		return Source{}, ErrMoveChanged
	}
	return s.Source(ctx, id)
}
