package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// AI tasks, the values of entry_ai.task.
const (
	TaskSummary     = "summary"
	TaskTranslation = "translation"
)

// AICapReached is the error code of an automatic task a cap stopped.
const AICapReached = "ai_cap_reached"

// AIResult is one AI result of an entry. Text keeps the last success while a new attempt is pending or failed.
type AIResult struct {
	Task        string
	Status      string // pending, ok or failed
	Text        string
	Error       string
	Attempts    int
	Requested   bool
	Fingerprint string
	Model       string
	UpdatedAt   time.Time
}

// AIInput is what an AI task reads of an entry and its source.
type AIInput struct {
	Title           string
	Kind            string
	ExtractStatus   string
	ExtractedHTML   string
	ContentHTML     string
	Summary         string
	PromptSummary   string
	PromptTranslate string
}

// AIJob is a result waiting for the AI endpoint.
type AIJob struct {
	EntryID   int64
	Task      string
	Requested bool // the user asked for it; otherwise it was queued automatically
}

// AIInput reads what the AI tasks of an entry need. It returns ErrNotFound for a missing entry.
func (s *Store) AIInput(ctx context.Context, entryID int64) (AIInput, error) {
	var in AIInput
	err := s.db.QueryRowContext(ctx, `
		SELECT e.title, s.kind, e.extract_status, e.extracted_html, e.rss_content_html, e.rss_summary,
			s.prompt_summary, s.prompt_translate
		FROM entries e JOIN sources s ON s.id = e.source_id WHERE e.id = ? AND s.deleted_at IS NULL`, entryID).Scan(
		&in.Title, &in.Kind, &in.ExtractStatus, &in.ExtractedHTML, &in.ContentHTML, &in.Summary,
		&in.PromptSummary, &in.PromptTranslate)
	if errors.Is(err, sql.ErrNoRows) {
		return AIInput{}, ErrNotFound
	}
	return in, err
}

// AIResults returns the results of an entry by task. It returns ErrNotFound for a missing entry.
func (s *Store) AIResults(ctx context.Context, entryID int64) (map[string]AIResult, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT 1 FROM entries WHERE id = ?", entryID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT task, status, text, error, attempts, requested, fingerprint, model, updated_at
		FROM entry_ai WHERE entry_id = ?`, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := map[string]AIResult{}
	for rows.Next() {
		var r AIResult
		var updated int64
		if err := rows.Scan(&r.Task, &r.Status, &r.Text, &r.Error, &r.Attempts, &r.Requested, &r.Fingerprint,
			&r.Model, &updated); err != nil {
			return nil, err
		}
		r.UpdatedAt = time.UnixMilli(updated)
		results[r.Task] = r
	}
	return results, rows.Err()
}

// RequestAI queues a result the user asked for, keeping the text of the last success, and returns it.
func (s *Store) RequestAI(ctx context.Context, entryID int64, task string, now time.Time) (AIResult, error) {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO entry_ai (entry_id, task, status, requested, updated_at) VALUES (?, ?, 'pending', 1, ?)
		ON CONFLICT (entry_id, task) DO UPDATE SET status = 'pending', error = '', requested = 1,
			updated_at = excluded.updated_at`,
		entryID, task, now.UnixMilli()); err != nil {
		return AIResult{}, err
	}
	results, err := s.AIResults(ctx, entryID)
	if err != nil {
		return AIResult{}, err
	}
	return results[task], nil
}

// PromoteAI makes a queued automatic result one the user asked for, so that it is no longer held back by the cap
// or by a wait for the endpoint.
func (s *Store) PromoteAI(ctx context.Context, entryID int64, task string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE entry_ai SET requested = 1 WHERE entry_id = ? AND task = ? AND status = 'pending'", entryID, task)
	return err
}

// aiJobsQuery is the query of AIJobs. The condition on status repeats the one of the partial index entry_ai_open,
// which the planner needs to see to use it. The sort that remains runs over the open results only.
const aiJobsQuery = `
		SELECT a.entry_id, a.task, a.requested FROM entry_ai a
			JOIN entries e ON e.id = a.entry_id JOIN sources s ON s.id = e.source_id
		WHERE a.status <> 'ok' AND s.deleted_at IS NULL AND NOT (s.kind = 'article' AND e.extract_status = 'pending')
			AND (a.status = 'pending'
				OR (a.status = 'failed' AND a.requested = 0 AND a.error <> '` + AICapReached + `' AND a.attempts < ? AND a.updated_at <= ?))
		ORDER BY a.status = 'failed', a.requested DESC, e.created_at DESC, e.id DESC
		LIMIT ?`

// AIJobs lists results to work on: pending ones, those the user asked for first, then automatic ones that
// failed, have attempts left and last failed at or before retryBefore, but not those a cap stopped; newer entries
// first. Articles still
// waiting for their full text wait too.
func (s *Store) AIJobs(ctx context.Context, limit, maxAttempts int, retryBefore time.Time) ([]AIJob, error) {
	rows, err := s.db.QueryContext(ctx, aiJobsQuery, maxAttempts, retryBefore.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []AIJob
	for rows.Next() {
		var job AIJob
		if err := rows.Scan(&job.EntryID, &job.Task, &job.Requested); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// SaveAI records one attempt: the text on success, or an error code that keeps the last success's text.
// A result whose entry was deleted meanwhile is dropped.
func (s *Store) SaveAI(ctx context.Context, entryID int64, task, text, model, fingerprint, code string, now time.Time) error {
	if code != "" {
		_, err := s.db.ExecContext(ctx, `
			UPDATE entry_ai SET status = 'failed', error = ?, attempts = attempts + 1, updated_at = ?
			WHERE entry_id = ? AND task = ?`, code, now.UnixMilli(), entryID, task)
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE entry_ai SET status = 'ok', text = ?, error = '', attempts = attempts + 1, fingerprint = ?, model = ?,
			updated_at = ?
		WHERE entry_id = ? AND task = ?`, text, fingerprint, model, now.UnixMilli(), entryID, task); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM entry_ai_chunks WHERE entry_id = ? AND task = ?", entryID, task); err != nil {
		return err
	}
	return tx.Commit()
}

// FailAI records that the endpoint, not the entry, made an attempt fail: the result is failed with code and keeps
// the last success's text, but the attempt does not count toward the retries an automatic task has.
func (s *Store) FailAI(ctx context.Context, entryID int64, task, code string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE entry_ai SET status = 'failed', error = ?, updated_at = ? WHERE entry_id = ? AND task = ?`,
		code, now.UnixMilli(), entryID, task)
	return err
}

// StopAI records that a cap stopped an automatic task. Unlike a failed attempt it does not use up a retry. A task
// the user asked for meanwhile is left as it is.
func (s *Store) StopAI(ctx context.Context, entryID int64, task string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE entry_ai SET status = 'failed', error = ?, updated_at = ?
		WHERE entry_id = ? AND task = ? AND requested = 0`,
		AICapReached, now.UnixMilli(), entryID, task)
	return err
}

// stopPendingAIQuery is the update of StopPendingAI. Like aiJobsQuery it repeats the condition of the partial index
// entry_ai_open, so that the update searches the open results and not every row.
const stopPendingAIQuery = `
		UPDATE entry_ai SET status = 'failed', error = ?, updated_at = ?
		WHERE status <> 'ok' AND status = 'pending' AND requested = 0`

// StopPendingAI stops every automatic task still waiting, since the daily cap is reached. It returns how many.
func (s *Store) StopPendingAI(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, stopPendingAIQuery, AICapReached, now.UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// usageDay is the key of the day now falls in, in its own time zone.
func usageDay(now time.Time) string { return now.Format(time.DateOnly) }

// AIUsage is how many requests automatic tasks sent on the day of now.
func (s *Store) AIUsage(ctx context.Context, now time.Time) (int, error) {
	var calls int
	err := s.db.QueryRowContext(ctx, "SELECT calls FROM ai_usage WHERE day = ?", usageDay(now)).Scan(&calls)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return calls, err
}

// ReserveAICall counts one more request of automatic tasks on the day of now, and reports false, counting nothing,
// when limit requests were sent already.
func (s *Store) ReserveAICall(ctx context.Context, limit int, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO ai_usage (day, calls) SELECT ?, 1 WHERE ? > 0
		ON CONFLICT (day) DO UPDATE SET calls = calls + 1 WHERE calls < ?`, usageDay(now), limit, limit)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// AIChunk returns the reply saved for a finished chunk of a task, and false when there is none.
func (s *Store) AIChunk(ctx context.Context, entryID int64, task, key string) (text, model string, found bool, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT text, model FROM entry_ai_chunks WHERE entry_id = ? AND task = ? AND key = ?",
		entryID, task, key).Scan(&text, &model)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	return text, model, err == nil, err
}

// SaveAIChunk keeps the reply for a finished chunk until the whole task succeeds. A deleted entry is dropped.
func (s *Store) SaveAIChunk(ctx context.Context, entryID int64, task, key, text, model string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO entry_ai_chunks (entry_id, task, key, text, model)
		SELECT id, ?, ?, ?, ? FROM entries WHERE id = ?`, task, key, text, model, entryID)
	return err
}
