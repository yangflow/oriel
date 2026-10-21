package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// FeedSource is what the fetcher reads about a source.
type FeedSource struct {
	ID                  int64
	URL                 string
	RSSHubPath          string
	Kind                string
	ETag                string
	LastModified        string
	FetchInterval       time.Duration
	ConsecutiveFailures int
	SiteURL             string
	IconURL             string
	// IconCheckedSite is the site address whose homepage was last searched for an icon.
	IconCheckedSite string
	// AITasks are the AI tasks queued for each new entry, from the source's automatic switches.
	// The fetcher clears them while AI is not configured.
	AITasks []string
}

// NewSource describes a subscription to add. Exactly one of URL and RSSHubPath is set.
type NewSource struct {
	URL           string
	RSSHubPath    string
	Kind          string
	FetchInterval time.Duration
}

// DueSources lists unpaused sources whose next fetch time has come, the longest waiting first.
func (s *Store) DueSources(ctx context.Context, now time.Time, limit int) ([]FeedSource, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, url, rsshub_path, kind, etag, last_modified, fetch_interval_sec, consecutive_failures,
			auto_summary, auto_translate, site_url, icon_url, icon_checked_site
		FROM sources WHERE paused = 0 AND next_fetch_at <= ? ORDER BY next_fetch_at, id LIMIT ?`,
		now.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sources []FeedSource
	for rows.Next() {
		var src FeedSource
		var interval int64
		var summary, translate bool
		if err := rows.Scan(&src.ID, &src.URL, &src.RSSHubPath, &src.Kind, &src.ETag, &src.LastModified,
			&interval, &src.ConsecutiveFailures, &summary, &translate, &src.SiteURL, &src.IconURL,
			&src.IconCheckedSite); err != nil {
			return nil, err
		}
		src.FetchInterval = time.Duration(interval) * time.Second
		if summary {
			src.AITasks = append(src.AITasks, TaskSummary)
		}
		if translate {
			src.AITasks = append(src.AITasks, TaskTranslation)
		}
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

// FetchSuccess records a fetch that reached the feed, including 304 Not Modified.
// Empty validators keep the stored ones. Bookkeeping does not change the source's updated_at.
func (s *Store) FetchSuccess(ctx context.Context, id int64, etag, lastModified string, now, next time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sources SET last_fetched_at = ?1, last_success_at = ?1, consecutive_failures = 0, last_error = '',
			etag = CASE WHEN ?2 = '' THEN etag ELSE ?2 END,
			last_modified = CASE WHEN ?3 = '' THEN last_modified ELSE ?3 END,
			next_fetch_at = ?4
		WHERE id = ?5`,
		now.UnixMilli(), etag, lastModified, next.UnixMilli(), id)
	return err
}

// SetSourceIcon records an icon lookup for the site address site. An icon already set is kept, and an empty
// icon only marks the site as searched.
func (s *Store) SetSourceIcon(ctx context.Context, id int64, icon, site string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sources SET icon_url = CASE WHEN icon_url = '' THEN ? ELSE icon_url END, icon_checked_site = ?
		WHERE id = ?`, icon, site, id)
	return err
}

// FetchFailure records a failed fetch and when to try again.
func (s *Store) FetchFailure(ctx context.Context, id int64, message string, now, next time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sources SET last_fetched_at = ?, consecutive_failures = consecutive_failures + 1, last_error = ?, next_fetch_at = ?
		WHERE id = ?`,
		now.UnixMilli(), message, next.UnixMilli(), id)
	return err
}

// Image is one picture of an entry, stored in the images JSON array.
type Image struct {
	URL    string `json:"url"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// FeedEntry is one item read from a feed. Zero values mean absent.
type FeedEntry struct {
	GUID         string
	Title        string
	URL          string
	Author       string
	PublishedAt  time.Time
	Summary      string
	ContentHTML  string
	ThumbnailURL string
	DurationSec  int
	Platform     string
	Language     string // BCP 47 code, "" when unknown
	Images       []Image
}

// SaveFeed writes the items of one fetch and fills the source's empty title and site address.
// Items are inserted from last to first, so with equal created_at the newest item gets the highest ID.
// An existing entry keeps its read and starred state, and its updated_at changes only when its content did.
// A new item published before expired would be pruned again at once, so it is not added; zero keeps all.
// Each new entry gets a pending result for every task in src.AITasks, except the translation of an entry
// already in targetLanguage, the language translations are made in.
// It returns how many entries are new.
func (s *Store) SaveFeed(ctx context.Context, src FeedSource, title, siteURL string, entries []FeedEntry, targetLanguage string, expired, now time.Time) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	extract := "skipped"
	if src.Kind == "article" {
		extract = "pending"
	}
	created := 0
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		var exists int
		var extracted string // the full text of an entry that already has it
		isNew := false
		switch err := tx.QueryRowContext(ctx, "SELECT 1, CASE extract_status WHEN 'ok' THEN search_text ELSE '' END FROM entries WHERE source_id = ? AND guid = ?",
			src.ID, e.GUID).Scan(&exists, &extracted); {
		case errors.Is(err, sql.ErrNoRows):
			if !expired.IsZero() && !e.PublishedAt.IsZero() && e.PublishedAt.Before(expired) {
				continue
			}
			created++
			isNew = true
		case err != nil:
			return 0, err
		}
		images := e.Images
		if images == nil {
			images = []Image{}
		}
		imagesJSON, err := json.Marshal(images)
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO entries (source_id, guid, title, url, author, published_at, rss_summary, rss_content_html,
				extract_status, thumbnail_url, duration_sec, platform, images, language, reading_sec, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (source_id, guid) DO UPDATE SET
				title = excluded.title, url = excluded.url, author = excluded.author, published_at = excluded.published_at,
				rss_summary = excluded.rss_summary, rss_content_html = excluded.rss_content_html,
				thumbnail_url = excluded.thumbnail_url, duration_sec = excluded.duration_sec,
				platform = excluded.platform, images = excluded.images,
				language = coalesce(excluded.language, language), reading_sec = excluded.reading_sec,
				updated_at = excluded.updated_at
			WHERE title IS NOT excluded.title OR url IS NOT excluded.url OR author IS NOT excluded.author
				OR published_at IS NOT excluded.published_at OR rss_summary IS NOT excluded.rss_summary
				OR rss_content_html IS NOT excluded.rss_content_html OR thumbnail_url IS NOT excluded.thumbnail_url
				OR duration_sec IS NOT excluded.duration_sec OR platform IS NOT excluded.platform
				OR images IS NOT excluded.images`,
			src.ID, e.GUID, e.Title, e.URL, e.Author, nullTime(e.PublishedAt), e.Summary, e.ContentHTML,
			extract, e.ThumbnailURL, nullInt(e.DurationSec), e.Platform, string(imagesJSON), nullString(e.Language),
			readingValue(src.Kind, e.DurationSec, extracted, e.ContentHTML, e.Summary), now.UnixMilli(), now.UnixMilli(),
		); err != nil {
			return 0, err
		}
		if !isNew {
			continue
		}
		for _, task := range src.AITasks {
			if task == TaskTranslation && e.Language != "" && e.Language == targetLanguage {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO entry_ai (entry_id, task, status, updated_at)
				SELECT id, ?, 'pending', ? FROM entries WHERE source_id = ? AND guid = ?`,
				task, now.UnixMilli(), src.ID, e.GUID); err != nil {
				return 0, err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE sources SET title = CASE WHEN title = '' THEN ? ELSE title END,
			site_url = CASE WHEN site_url = '' THEN ? ELSE site_url END
		WHERE id = ?`, title, siteURL, src.ID); err != nil {
		return 0, err
	}
	return created, tx.Commit()
}

// Setting returns a setting, or "" when it was never set.
func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

// SetSetting stores a setting.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value", key, value)
	return err
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UnixMilli()
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

// ExtractTarget is an article entry waiting for its full text.
type ExtractTarget struct {
	ID    int64
	URL   string
	Title string
}

// PendingExtractions lists article entries never tried first, newest first, then failed ones with
// attempts left. Entries of sources that are no longer articles are left alone.
func (s *Store) PendingExtractions(ctx context.Context, limit, maxAttempts int) ([]ExtractTarget, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.url, e.title FROM entries e JOIN sources s ON s.id = e.source_id
		WHERE s.kind = 'article'
			AND (e.extract_status = 'pending' OR (e.extract_status = 'failed' AND e.extract_attempts < ?))
		ORDER BY e.extract_status = 'failed', e.created_at DESC, e.id DESC
		LIMIT ?`, maxAttempts, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var targets []ExtractTarget
	for rows.Next() {
		var t ExtractTarget
		if err := rows.Scan(&t.ID, &t.URL, &t.Title); err != nil {
			return nil, err
		}
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

// SaveExtraction records one extraction attempt. Empty html marks it failed.
// The language of the full text, "" when unknown, replaces the one detected from the feed. Derived text does not
// change the entry's updated_at.
func (s *Store) SaveExtraction(ctx context.Context, id int64, html, text, language string) error {
	status := "ok"
	if html == "" {
		status = "failed"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// A failed or skipped extraction leaves the estimate as it was; the full text replaces the feed's content in it.
	reading := sql.NullInt64{}
	update := false
	if status == "ok" {
		var kind, content, summary string
		var duration sql.NullInt64
		err := tx.QueryRowContext(ctx, "SELECT kind, duration_sec, rss_content_html, rss_summary FROM entries WHERE id = ?", id).
			Scan(&kind, &duration, &content, &summary)
		if err == nil {
			reading, update = readingValue(kind, int(duration.Int64), text, content, summary), true
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE entries SET extract_status = ?, extracted_html = ?, search_text = ?, language = coalesce(?, language),
			reading_sec = CASE WHEN ? THEN ? ELSE reading_sec END, extract_attempts = extract_attempts + 1
		WHERE id = ?`, status, html, text, nullString(language), update, reading, id); err != nil {
		return err
	}
	return tx.Commit()
}

// PruneEntries deletes unstarred entries published, or else added, before cutoff, read or not.
func (s *Store) PruneEntries(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM entries WHERE starred = 0 AND coalesce(published_at, created_at) < ?", cutoff.UnixMilli())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
