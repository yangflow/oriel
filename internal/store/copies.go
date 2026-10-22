package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// CopyTarget is a starred entry waiting for the local copy of its pictures, with what tells which pictures it has.
type CopyTarget struct {
	ID            int64
	URL           string
	ThumbnailURL  string
	Images        []Image
	ContentHTML   string
	ExtractedHTML string // the full text, "" unless the extraction succeeded
	Attempts      int    // copies already tried and failed
}

// PendingCopies lists starred entries whose copy was never tried, then failed ones whose next attempt is due, oldest
// first. An article still waiting for its full text is left for later, since the full text names the pictures.
func (s *Store) PendingCopies(ctx context.Context, now time.Time, limit int) ([]CopyTarget, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.url, e.thumbnail_url, e.images, e.rss_content_html, CASE e.extract_status WHEN 'ok' THEN e.extracted_html ELSE '' END,
			e.copy_attempts
		FROM entries e JOIN sources s ON s.id = e.source_id
		WHERE e.starred = 1 AND s.deleted_at IS NULL AND NOT (s.kind = 'article' AND e.extract_status = 'pending')
			AND (e.copy_status = 'pending' OR (e.copy_status = 'failed' AND e.next_copy_at <= ?))
		ORDER BY e.copy_status = 'failed', e.id
		LIMIT ?`, now.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var targets []CopyTarget
	for rows.Next() {
		var t CopyTarget
		var images string
		if err := rows.Scan(&t.ID, &t.URL, &t.ThumbnailURL, &images, &t.ContentHTML, &t.ExtractedHTML, &t.Attempts); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(images), &t.Images); err != nil {
			t.Images = nil
		}
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

// CopiedAddresses returns the addresses of the pictures already copied for an entry.
func (s *Store) CopiedAddresses(ctx context.Context, id int64) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT url FROM entry_image_copies WHERE entry_id = ?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var address string
		if err := rows.Scan(&address); err != nil {
			return nil, err
		}
		have[address] = true
	}
	return have, rows.Err()
}

// SaveImageCopy stores a picture for a starred entry. An entry unstarred or removed meanwhile gets nothing, and a
// picture already copied stays as it is.
func (s *Store) SaveImageCopy(ctx context.Context, id int64, address, contentType string, body []byte, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO entry_image_copies (entry_id, url, content_type, body, created_at)
		SELECT id, ?, ?, ?, ? FROM entries WHERE id = ? AND starred = 1
		ON CONFLICT (entry_id, url) DO NOTHING`, address, contentType, body, now.UnixMilli(), id)
	return err
}

// FinishCopy ends one attempt at the copy of a starred entry. With done the entry is complete; otherwise it failed,
// and a non-zero retryAt is when it may be tried again, the zero time giving up until the entry is starred again.
func (s *Store) FinishCopy(ctx context.Context, id int64, done bool, retryAt time.Time) error {
	if done {
		_, err := s.db.ExecContext(ctx, `
			UPDATE entries SET copy_status = 'ok', copy_attempts = copy_attempts + 1, next_copy_at = NULL
			WHERE id = ? AND starred = 1`, id)
		return err
	}
	var next any
	if !retryAt.IsZero() {
		next = retryAt.UnixMilli()
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE entries SET copy_status = 'failed', copy_attempts = copy_attempts + 1, next_copy_at = ?
		WHERE id = ? AND starred = 1`, next, id)
	return err
}

// ImageCopy returns the copy of the picture at address kept for a starred entry, if there is one.
func (s *Store) ImageCopy(ctx context.Context, address string) (contentType string, body []byte, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT content_type, body FROM entry_image_copies WHERE url = ? LIMIT 1", address).Scan(&contentType, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, false, nil
	}
	return contentType, body, err == nil, err
}

// CopyStats counts the copied pictures and the bytes they take.
func (s *Store) CopyStats(ctx context.Context) (count int, bytes int64, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT count(*), coalesce(sum(length(body)), 0) FROM entry_image_copies").Scan(&count, &bytes)
	return count, bytes, err
}
