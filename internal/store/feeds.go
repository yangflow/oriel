package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// FeedSource is what the fetcher reads about a source.
type FeedSource struct {
	ID            int64
	URL           string
	RSSHubPath    string
	Kind          string
	ETag          string
	LastModified  string
	FetchInterval time.Duration
	// FetchIntervalAuto is set when the fetcher works the interval out from the feed.
	FetchIntervalAuto   bool
	ConsecutiveFailures int
	// FirstSuccess is set while no fetch of the source has reached the feed yet.
	FirstSuccess bool
	SiteURL      string
	IconURL      string
	// IconCheckedSite is the site address whose homepage was last searched for an icon.
	IconCheckedSite string
	// AITasks are the AI tasks queued for each new entry, from the source's automatic switches.
	// The fetcher clears them while AI is not configured.
	AITasks []string
	// AIFetchLimit is the most new entries of one fetch that get AITasks, 0 for all of them.
	AIFetchLimit int
	// WebSubHub and WebSubTopic are the hub and topic the feed declared at its last fetch, empty for none.
	WebSubHub, WebSubTopic string
	// WebSubUntil is when the active push subscription runs out, zero when there is none.
	WebSubUntil time.Time
	// KeepDays is the source's own keep period: 0 follows the global retention, -1 keeps forever, else days.
	KeepDays int
}

// NewSource describes a subscription to add. Exactly one of URL and RSSHubPath is set.
type NewSource struct {
	URL           string
	RSSHubPath    string
	Kind          string
	FetchInterval time.Duration
	// FetchIntervalAuto makes the fetcher work the interval out from the feed; FetchInterval is used until it can.
	FetchIntervalAuto bool
	// FirstFetchDelay postpones the first fetch; zero makes the source due at once.
	FirstFetchDelay time.Duration
	// AllowDuplicate adds the source even when another subscription is the same feed in another form (see
	// FindDuplicate); the very same address is still refused.
	AllowDuplicate bool
}

// DueSources lists unpaused sources whose next fetch time has come, the longest waiting first.
func (s *Store) DueSources(ctx context.Context, now time.Time, limit int) ([]FeedSource, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, url, rsshub_path, kind, etag, last_modified, fetch_interval_sec, fetch_interval_auto, last_success_at IS NULL, consecutive_failures,
			auto_summary, auto_translate, site_url, icon_url, icon_checked_site, websub_hub, websub_topic,
			CASE WHEN websub_state = 'active' THEN websub_expires_at END, keep_days
		FROM sources WHERE paused = 0 AND deleted_at IS NULL AND next_fetch_at <= ? ORDER BY next_fetch_at, id LIMIT ?`,
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
		var until sql.NullInt64
		if err := rows.Scan(&src.ID, &src.URL, &src.RSSHubPath, &src.Kind, &src.ETag, &src.LastModified,
			&interval, &src.FetchIntervalAuto, &src.FirstSuccess, &src.ConsecutiveFailures, &summary, &translate, &src.SiteURL, &src.IconURL,
			&src.IconCheckedSite, &src.WebSubHub, &src.WebSubTopic, &until, &src.KeepDays); err != nil {
			return nil, err
		}
		if until.Valid {
			src.WebSubUntil = time.UnixMilli(until.Int64)
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
// Empty validators keep the stored ones. A non-zero interval is stored as the interval in use when the source is
// still automatic. instance is the RSSHub instance that served the feed, empty for a source that is not an RSSHub
// route. Bookkeeping does not change the source's updated_at.
func (s *Store) FetchSuccess(ctx context.Context, id int64, etag, lastModified, instance string, now, next time.Time, interval time.Duration) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sources SET last_fetched_at = ?1, last_success_at = ?1, consecutive_failures = 0, last_error = '', last_error_kind = '',
			etag = CASE WHEN ?2 = '' THEN etag ELSE ?2 END,
			last_modified = CASE WHEN ?3 = '' THEN last_modified ELSE ?3 END,
			next_fetch_at = ?4,
			fetch_interval_sec = CASE WHEN fetch_interval_auto = 1 AND ?5 > 0 THEN ?5 ELSE fetch_interval_sec END,
			rsshub_instance = ?6
		WHERE id = ?7`,
		now.UnixMilli(), etag, lastModified, next.UnixMilli(), int64(interval/time.Second), instance, id)
	return err
}

// RecentPublished returns the publication times of the source's newest dated entries, newest first, at most limit.
// Times after now are left out: a feed that dates entries in the future says nothing about how often it publishes.
func (s *Store) RecentPublished(ctx context.Context, id int64, now time.Time, limit int) ([]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT published_at FROM entries WHERE source_id = ? AND published_at IS NOT NULL AND published_at <= ?
		ORDER BY published_at DESC LIMIT ?`, id, now.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var times []time.Time
	for rows.Next() {
		var ms int64
		if err := rows.Scan(&ms); err != nil {
			return nil, err
		}
		times = append(times, time.UnixMilli(ms))
	}
	return times, rows.Err()
}

// SuggestKind records the kind a source's feed looks like, "" for none. It never changes the source's kind and
// does not change its updated_at.
func (s *Store) SuggestKind(ctx context.Context, id int64, kind string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE sources SET suggested_kind = ? WHERE id = ?", kind, id)
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

// FetchFailure records a failed fetch and when to try again. kind is the stable code of the reason, empty when
// the reason has none.
func (s *Store) FetchFailure(ctx context.Context, id int64, message, kind string, now, next time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sources SET last_fetched_at = ?, consecutive_failures = consecutive_failures + 1, last_error = ?, last_error_kind = ?,
			next_fetch_at = ?
		WHERE id = ?`,
		now.UnixMilli(), message, kind, next.UnixMilli(), id)
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
// An item with a GUID not yet stored is still an existing entry when it is the only item of the fetch with its link
// and the source has exactly one entry with that link whose GUID is not in the fetch: the feed changed the GUID, and
// the entry takes the new one. Entries of other sources are never matched.
// An existing entry keeps its read and starred state, and its updated_at changes only when its content did.
// A new item published before expired would be pruned again at once, so it is not added; zero keeps all.
// Each new entry gets a pending result for every task in src.AITasks, except the translation of an entry
// already in targetLanguage, the language translations are made in. Only the newest src.AIFetchLimit entries of the
// fetch get them (no cap when it is 0); the older ones get a failed result with the code AICapReached.
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
	// The new entries that get automatic AI tasks, oldest first.
	type queuedAI struct {
		id    int64
		tasks []string
	}
	var queued []queuedAI
	// Counts of the links and GUIDs in this fetch, and the source's stored links, read when first needed.
	var itemLinks, itemGUIDs map[string]int
	var stored map[string][]linkedEntry
	rules, err := sourceRules(ctx, tx, src.ID, src.Kind)
	if err != nil {
		return 0, err
	}
	// The GUIDs the source already has, and whether each has extracted text, read once instead of one query per item.
	known, err := sourceGUIDs(ctx, tx, src.ID)
	if err != nil {
		return 0, err
	}
	upsert, err := tx.PrepareContext(ctx, upsertEntrySQL)
	if err != nil {
		return 0, err
	}
	defer upsert.Close()
	extractedText, err := tx.PrepareContext(ctx, "SELECT search_text FROM entries WHERE source_id = ? AND guid = ?")
	if err != nil {
		return 0, err
	}
	defer extractedText.Close()
	addAI, err := tx.PrepareContext(ctx, "INSERT INTO entry_ai (entry_id, task, status, error, updated_at) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return 0, err
	}
	defer addAI.Close()
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		var extracted string // the full text of an entry that already has it
		isNew := false
		switch hasText, exists := known[e.GUID]; {
		case exists:
			// Only the reading time uses the text, and only when nothing else decides it.
			if hasText && e.DurationSec <= 0 && src.Kind != "video" && src.Kind != "image" {
				if err := extractedText.QueryRowContext(ctx, src.ID, e.GUID).Scan(&extracted); err != nil {
					return 0, err
				}
			}
		default:
			if stored == nil {
				itemLinks, itemGUIDs = map[string]int{}, map[string]int{}
				for _, it := range entries {
					itemLinks[linkKey(it.URL)]++
					itemGUIDs[it.GUID]++
				}
				var err error
				if stored, err = sourceLinks(ctx, tx, src.ID); err != nil {
					return 0, err
				}
			}
			if old, ok := renamedEntry(e, stored, itemLinks, itemGUIDs); ok {
				// The feed changed this entry's GUID: it is the same entry, so keep it (and its read and starred
				// state) under the new GUID and let the update below refresh its content.
				if _, err := tx.ExecContext(ctx, "UPDATE entries SET guid = ? WHERE id = ?", e.GUID, old.id); err != nil {
					return 0, err
				}
				delete(stored, linkKey(e.URL))
				known[e.GUID] = false
				break
			}
			if !expired.IsZero() && !e.PublishedAt.IsZero() && e.PublishedAt.Before(expired) {
				continue
			}
			created++
			isNew = true
		}
		images := e.Images
		if images == nil {
			images = []Image{}
		}
		imagesJSON, err := json.Marshal(images)
		if err != nil {
			return 0, err
		}
		// RETURNING gives the id of a new entry; an existing entry that did not change returns no row.
		var id int64
		if err := upsert.QueryRowContext(ctx,
			src.ID, e.GUID, e.Title, e.URL, e.Author, nullTime(e.PublishedAt), e.Summary, e.ContentHTML,
			extract, e.ThumbnailURL, nullInt(e.DurationSec), e.Platform, string(imagesJSON), nullString(e.Language),
			readingValue(src.Kind, e.DurationSec, extracted, e.ContentHTML, e.Summary), now.UnixMilli(), now.UnixMilli(),
		).Scan(&id); err != nil && !(errors.Is(err, sql.ErrNoRows) && !isNew) {
			return 0, err
		}
		if _, ok := known[e.GUID]; !ok {
			known[e.GUID] = false
		}
		if !isNew {
			continue
		}
		star, read := ruleActions(rules, e)
		if star {
			if _, err := tx.ExecContext(ctx, "UPDATE entries SET starred = 1 WHERE id = ?", id); err != nil {
				return 0, err
			}
		}
		if read {
			if _, err := tx.ExecContext(ctx, "UPDATE entries SET is_read = 1, read_at = ? WHERE id = ?", now.UnixMilli(), id); err != nil {
				return 0, err
			}
		}
		var tasks []string
		for _, task := range src.AITasks {
			if task == TaskTranslation && e.Language != "" && e.Language == targetLanguage {
				continue
			}
			tasks = append(tasks, task)
		}
		if len(tasks) > 0 {
			queued = append(queued, queuedAI{id: id, tasks: tasks})
		}
	}
	// The newest entries get the automatic tasks; the ones beyond the cap of this fetch are recorded as stopped by it.
	for i, q := range queued {
		status, code := "pending", ""
		if src.AIFetchLimit > 0 && i < len(queued)-src.AIFetchLimit {
			status, code = "failed", AICapReached
		}
		for _, task := range q.tasks {
			if _, err := addAI.ExecContext(ctx, q.id, task, status, code, now.UnixMilli()); err != nil {
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

// upsertEntrySQL writes one item of a fetch and returns the id of an entry that was added or changed.
const upsertEntrySQL = `
	INSERT INTO entries (source_id, guid, title, url, author, published_at, rss_summary, rss_content_html,
		extract_status, thumbnail_url, duration_sec, platform, images, language, reading_sec, created_at, updated_at, link_key)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, group_key(?4, ?2))
	ON CONFLICT (source_id, guid) DO UPDATE SET
		title = excluded.title, url = excluded.url, link_key = excluded.link_key, author = excluded.author, published_at = excluded.published_at,
		rss_summary = excluded.rss_summary, rss_content_html = excluded.rss_content_html,
		thumbnail_url = excluded.thumbnail_url, duration_sec = excluded.duration_sec,
		platform = excluded.platform, images = excluded.images,
		language = coalesce(excluded.language, language), reading_sec = excluded.reading_sec,
		updated_at = excluded.updated_at
	WHERE title IS NOT excluded.title OR url IS NOT excluded.url OR author IS NOT excluded.author
		OR published_at IS NOT excluded.published_at OR rss_summary IS NOT excluded.rss_summary
		OR rss_content_html IS NOT excluded.rss_content_html OR thumbnail_url IS NOT excluded.thumbnail_url
		OR duration_sec IS NOT excluded.duration_sec OR platform IS NOT excluded.platform
		OR images IS NOT excluded.images
	RETURNING id`

// sourceGUIDs maps the GUID of every entry of the source to whether it has extracted full text.
func sourceGUIDs(ctx context.Context, tx *sql.Tx, sourceID int64) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, "SELECT guid, extract_status = 'ok' FROM entries WHERE source_id = ?", sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := map[string]bool{}
	for rows.Next() {
		var guid string
		var extracted bool
		if err := rows.Scan(&guid, &extracted); err != nil {
			return nil, err
		}
		known[guid] = extracted
	}
	return known, rows.Err()
}

// renamedEntry finds the stored entry an item with an unknown GUID really is: the one entry of the source with the
// same link. It returns false when no entry or several entries have the link, when several items of this fetch share
// it (a feed whose items all link to the homepage), or when the stored entry's GUID is still in this fetch.
func renamedEntry(e FeedEntry, stored map[string][]linkedEntry, itemLinks, itemGUIDs map[string]int) (linkedEntry, bool) {
	key := linkKey(e.URL)
	if key == "" || itemLinks[key] != 1 || len(stored[key]) != 1 {
		return linkedEntry{}, false
	}
	old := stored[key][0]
	if itemGUIDs[old.guid] > 0 {
		return linkedEntry{}, false
	}
	return old, true
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
	ID       int64
	URL      string
	Title    string
	Attempts int // extractions already tried and failed
	// The extraction rules of the entry's source; see docs/product.md "正文抽取".
	Selector string
	Referer  string
	Rewrites string
}

// pendingExtractionsQuery is the query of PendingExtractions. The condition on extract_status repeats the one of the
// partial index entries_extract_open, which the planner needs to see to use it. The sort that remains runs over the
// open entries only: a plan that read the index in the wanted order would walk the whole timeline instead.
const pendingExtractionsQuery = `
		SELECT e.id, e.url, e.title, e.extract_attempts, s.extract_selector, s.extract_referer, s.extract_rewrites FROM entries e JOIN sources s ON s.id = e.source_id
		WHERE s.kind = 'article' AND s.deleted_at IS NULL AND e.extract_status IN ('pending', 'failed')
			AND (e.extract_status = 'pending' OR (e.extract_status = 'failed' AND e.next_extract_at <= ?))
		ORDER BY e.extract_status = 'failed', e.created_at DESC, e.id DESC
		LIMIT ?`

// PendingExtractions lists article entries never tried first, newest first, then failed ones whose next attempt is
// due at now. A failed entry without a next attempt gave up for good. Entries of sources that are no longer articles
// are left alone.
func (s *Store) PendingExtractions(ctx context.Context, now time.Time, limit int) ([]ExtractTarget, error) {
	rows, err := s.db.QueryContext(ctx, pendingExtractionsQuery, now.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var targets []ExtractTarget
	for rows.Next() {
		var t ExtractTarget
		if err := rows.Scan(&t.ID, &t.URL, &t.Title, &t.Attempts, &t.Selector, &t.Referer, &t.Rewrites); err != nil {
			return nil, err
		}
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

// FailExtraction records a failed extraction attempt. A non-zero retryAt is when it may be tried again; the zero time
// gives up on the entry until the user retries it.
func (s *Store) FailExtraction(ctx context.Context, id int64, retryAt time.Time) error {
	var next any
	if !retryAt.IsZero() {
		next = retryAt.UnixMilli()
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE entries SET extract_status = 'failed', extracted_html = '', search_text = '',
			extract_attempts = extract_attempts + 1, next_extract_at = ?
		WHERE id = ?`, next, id)
	return err
}

// SaveExtraction records one extraction attempt. Empty html marks it failed for good; FailExtraction schedules a retry.
// The language of the full text, "" when unknown, replaces the one detected from the feed. Derived text does not
// change the entry's updated_at.
func (s *Store) SaveExtraction(ctx context.Context, id int64, html, text, language string) error {
	status := "ok"
	if html == "" {
		status = "failed"
	}
	// A failed or skipped extraction leaves the estimate as it was; the full text replaces the feed's content in it.
	// The read stays outside the write: a read transaction that later writes fails at once when another writer
	// committed in between, and none of the fields read here change with an extraction.
	reading := sql.NullInt64{}
	update := false
	if status == "ok" {
		var kind, content, summary string
		var duration sql.NullInt64
		err := s.db.QueryRowContext(ctx, "SELECT kind, duration_sec, rss_content_html, rss_summary FROM entries WHERE id = ?", id).
			Scan(&kind, &duration, &content, &summary)
		if err == nil {
			reading, update = readingValue(kind, int(duration.Int64), text, content, summary), true
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE entries SET extract_status = ?, extracted_html = ?, search_text = ?, language = coalesce(?, language),
			reading_sec = CASE WHEN ? THEN ? ELSE reading_sec END, extract_attempts = extract_attempts + 1,
			next_extract_at = NULL
		WHERE id = ?`, status, html, text, nullString(language), update, reading, id)
	return err
}

// RetryExtraction starts the full-text extraction of a failed entry over: its attempts are cleared and its status goes
// back to pending, so the next round picks it up with all the attempts again. An entry already pending stays as it is.
// It returns the entry, ErrNotFound for a missing one, and ErrNotRetryable for one that is neither failed nor pending.
func (s *Store) RetryExtraction(ctx context.Context, id int64) (Entry, error) {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE entries SET extract_status = 'pending', extract_attempts = 0, next_extract_at = NULL
		WHERE id = ? AND extract_status = 'failed'`, id); err != nil {
		return Entry{}, err
	}
	entry, err := s.Entry(ctx, id)
	if err != nil {
		return Entry{}, err
	}
	if entry.ExtractStatus != "pending" {
		return Entry{}, ErrNotRetryable
	}
	return entry, nil
}

// pruneBatch is how many entries one PruneEntries transaction deletes.
const pruneBatch = 500

// pruneExpired is the retention rule of PruneEntries: an unstarred entry whose source keeps entries for a limited
// time (?1 is the global days) and that is older than that time (?2 is now, ?3 the milliseconds in a day).
const pruneExpired = `starred = 0 AND EXISTS (
	SELECT 1 FROM sources s
	WHERE s.id = entries.source_id
		AND CASE WHEN s.keep_days > 0 THEN s.keep_days WHEN s.keep_days = 0 THEN ?1 ELSE 0 END > 0
		AND coalesce(entries.published_at, entries.created_at) <
			?2 - ?3 * CASE WHEN s.keep_days > 0 THEN s.keep_days ELSE ?1 END)`

// PruneEntries deletes unstarred entries published, or else added, before the keep period of their source ended,
// read or not. A source's own keep_days wins over globalDays; 0 follows globalDays and -1 keeps forever, and a
// globalDays of 0 keeps forever. It finds the expired ids once, then deletes them pruneBatch at a time, each batch
// its own transaction, so the write lock is free between batches, and returns the total. Every batch checks the
// rule again, so an entry starred meanwhile stays. When it fails or ctx ends, the batches already done stay.
func (s *Store) PruneEntries(ctx context.Context, now time.Time, globalDays int) (int64, error) {
	const dayMillis = 24 * 60 * 60 * 1000
	args := []any{globalDays, now.UnixMilli(), int64(dayMillis)}
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM entries WHERE "+pruneExpired, args...)
	if err != nil {
		return 0, err
	}
	var ids []int64
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
		marks := strings.TrimSuffix(strings.Repeat("?,", n), ",")
		batchArgs := append([]any(nil), args...)
		for _, id := range ids[:n] {
			batchArgs = append(batchArgs, id)
		}
		result, err := s.db.ExecContext(ctx, "DELETE FROM entries WHERE "+pruneExpired+" AND id IN ("+marks+")", batchArgs...)
		if err != nil {
			return total, err
		}
		deleted, err := result.RowsAffected()
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
