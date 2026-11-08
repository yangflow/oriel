package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// RemovedKeep is how long a removed entry's GUID is remembered after a feed last carried it.
const RemovedKeep = 30 * 24 * time.Hour

// removableRead is what "delete all read entries" removes: read and unstarred, in a subscription that is not waiting
// out its undo window, so that undoing a deletion brings the subscription back as it was.
const removableRead = `is_read = 1 AND starred = 0 AND source_id IN (SELECT id FROM sources WHERE deleted_at IS NULL)`

// ReadEntryCount counts the entries DeleteReadEntries would remove.
func (s *Store) ReadEntryCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM entries WHERE "+removableRead).Scan(&n)
	return n, err
}

// DeleteReadEntries removes the read, unstarred entries and remembers their GUIDs, so that the next fetch does not add
// them again as unread. Like PruneEntries it finds the ids once and works pruneBatch at a time, each batch its own
// transaction that checks the rule again: an entry starred or marked unread meanwhile stays. When it fails or ctx
// ends, the batches already done stay and the count so far is returned.
func (s *Store) DeleteReadEntries(ctx context.Context, now time.Time) (int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM entries WHERE "+removableRead)
	if err != nil {
		return 0, err
	}
	var ids []any
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return 0, err
	}
	batch := s.pruneBatch
	if batch <= 0 {
		batch = pruneBatch
	}
	var total int64
	for len(ids) > 0 {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n := min(batch, len(ids))
		deleted, err := s.deleteReadBatch(ctx, ids[:n], now)
		if err != nil {
			return total, err
		}
		total += deleted
		ids = ids[n:]
		if s.betweenBatches != nil && len(ids) > 0 {
			s.betweenBatches()
		}
	}
	return total, nil
}

func (s *Store) deleteReadBatch(ctx context.Context, ids []any, now time.Time) (int64, error) {
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	remember := append([]any{now.UnixMilli()}, ids...)
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO removed_entries (source_id, guid, seen_at)
		SELECT source_id, guid, ? FROM entries WHERE `+removableRead+" AND id IN ("+marks+")", remember...); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM entries WHERE "+removableRead+" AND id IN ("+marks+")", ids...)
	if err != nil {
		return 0, err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return deleted, tx.Commit()
}

// PruneRemovedEntries forgets the GUIDs of removed entries no feed has carried for RemovedKeep.
func (s *Store) PruneRemovedEntries(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM removed_entries WHERE seen_at < ?", now.Add(-RemovedKeep).UnixMilli())
	return err
}

// removedGUIDs is the GUIDs removed from a source that a fetch must not add again.
func removedGUIDs(ctx context.Context, tx *sql.Tx, source int64) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, "SELECT guid FROM removed_entries WHERE source_id = ?", source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	removed := map[string]bool{}
	for rows.Next() {
		var guid string
		if err := rows.Scan(&guid); err != nil {
			return nil, err
		}
		removed[guid] = true
	}
	return removed, rows.Err()
}
