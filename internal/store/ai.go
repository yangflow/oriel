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
	EntryID int64
	Task    string
}

// AIInput reads what the AI tasks of an entry need. It returns ErrNotFound for a missing entry.
func (s *Store) AIInput(ctx context.Context, entryID int64) (AIInput, error) {
	var in AIInput
	err := s.db.QueryRowContext(ctx, `
		SELECT e.title, s.kind, e.extract_status, e.extracted_html, e.rss_content_html, e.rss_summary,
			s.prompt_summary, s.prompt_translate
		FROM entries e JOIN sources s ON s.id = e.source_id WHERE e.id = ?`, entryID).Scan(
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

// AIJobs lists results to work on: pending ones, those the user asked for first, then automatic ones that
// failed, have attempts left and last failed at or before retryBefore; newer entries first. Articles still
// waiting for their full text wait too.
func (s *Store) AIJobs(ctx context.Context, limit, maxAttempts int, retryBefore time.Time) ([]AIJob, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.entry_id, a.task FROM entry_ai a
			JOIN entries e ON e.id = a.entry_id JOIN sources s ON s.id = e.source_id
		WHERE NOT (s.kind = 'article' AND e.extract_status = 'pending')
			AND (a.status = 'pending'
				OR (a.status = 'failed' AND a.requested = 0 AND a.attempts < ? AND a.updated_at <= ?))
		ORDER BY a.status = 'failed', a.requested DESC, e.created_at DESC, e.id DESC
		LIMIT ?`, maxAttempts, retryBefore.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []AIJob
	for rows.Next() {
		var job AIJob
		if err := rows.Scan(&job.EntryID, &job.Task); err != nil {
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
	_, err := s.db.ExecContext(ctx, `
		UPDATE entry_ai SET status = 'ok', text = ?, error = '', attempts = attempts + 1, fingerprint = ?, model = ?,
			updated_at = ?
		WHERE entry_id = ? AND task = ?`, text, fingerprint, model, now.UnixMilli(), entryID, task)
	return err
}
