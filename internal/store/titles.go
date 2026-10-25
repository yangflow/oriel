package store

import (
	"context"
	"strings"
	"time"
)

// TitleJob is a title waiting for translation.
type TitleJob struct {
	ID       int64
	Title    string
	Language string // the entry's language, "" when not known yet
	Attempts int
}

// TitleResult is the outcome for one title of a batch. Text is the translation.
type TitleResult struct {
	ID    int64
	Title string // the title that was sent; a result for an entry whose title changed meanwhile is dropped
	Text  string
}

// PendingTitles lists the titles to translate: pending ones, and failed ones with attempts left that last failed at
// or before retryBefore; pending first, oldest entries first. Entries of a source whose switch is off or that is
// deleted are not listed. The condition on status repeats the one of the partial index entries_title_tr.
func (s *Store) PendingTitles(ctx context.Context, limit, maxAttempts int, retryBefore time.Time) ([]TitleJob, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.title, coalesce(e.language, ''), e.title_tr_attempts
		FROM entries e JOIN sources s ON s.id = e.source_id
		WHERE e.title_tr_status IN ('pending', 'failed') AND s.deleted_at IS NULL AND s.translate_titles = 1
			AND (e.title_tr_status = 'pending' OR (e.title_tr_attempts < ? AND e.title_tr_at <= ?))
		ORDER BY e.title_tr_status = 'failed', e.created_at, e.id
		LIMIT ?`, maxAttempts, retryBefore.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []TitleJob
	for rows.Next() {
		var job TitleJob
		if err := rows.Scan(&job.ID, &job.Title, &job.Language, &job.Attempts); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// SaveTitles stores the translations of a batch. A translation equal to its title, ignoring surrounding space, is
// stored as "" so that no subtitle repeats the title. A title that changed meanwhile is left to its new queue entry.
func (s *Store) SaveTitles(ctx context.Context, results []TitleResult, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range results {
		text := strings.TrimSpace(r.Text)
		if text == strings.TrimSpace(r.Title) {
			text = ""
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE entries SET translated_title = ?, title_tr_status = 'ok', title_tr_attempts = title_tr_attempts + 1, title_tr_at = ?
			WHERE id = ? AND title = ? AND title_tr_status IN ('pending', 'failed')`,
			text, now.UnixMilli(), r.ID, r.Title); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FailTitle records a failed try on one title.
func (s *Store) FailTitle(ctx context.Context, id int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE entries SET title_tr_status = 'failed', title_tr_attempts = title_tr_attempts + 1, title_tr_at = ?
		WHERE id = ? AND title_tr_status IN ('pending', 'failed')`, now.UnixMilli(), id)
	return err
}

// SkipTitles takes titles out of the queue because they need no translation (already in the target language).
func (s *Store) SkipTitles(ctx context.Context, ids []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `
			UPDATE entries SET title_tr_status = '', title_tr_attempts = 0
			WHERE id = ? AND title_tr_status IN ('pending', 'failed')`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// StopTitles ends the wait of the titles still pending because the daily cap is reached: they become failed with no
// attempts left. It returns how many.
func (s *Store) StopTitles(ctx context.Context, maxAttempts int, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE entries SET title_tr_status = 'failed', title_tr_attempts = ?, title_tr_at = ?
		WHERE title_tr_status = 'pending'`, maxAttempts, now.UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
