package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// ErrExists reports that the same subscription is already stored.
var ErrExists = errors.New("already exists")

// ProxyDirect is the proxy of a source that connects without one, whatever the global proxy is.
const ProxyDirect = "direct"

// SourceProxy is the fetch proxy a source sets for itself. URL is empty to follow the global proxy, ProxyDirect to
// connect without one, else the address of the proxy; User and Password are its credentials.
type SourceProxy struct {
	URL, User, Password string
}

// ErrPaused reports that a paused subscription cannot be made due.
var ErrPaused = errors.New("paused")

// Source is a subscription as clients see it. Zero times mean never.
type Source struct {
	ID                  int64
	URL                 string
	RSSHubPath          string
	Title               string
	SiteURL             string
	IconURL             string
	Kind                string
	FetchInterval       time.Duration
	FetchIntervalAuto   bool
	SuggestedKind       string
	NextFetchAt         time.Time
	LastFetchedAt       time.Time
	LastSuccessAt       time.Time
	LastError           string
	LastErrorKind       string // stable code of the reason, "" when it has none
	ConsecutiveFailures int
	PromptSummary       string
	PromptTranslate     string
	AutoSummary         bool
	AutoTranslate       bool
	MuteKeywords        string
	Paused              bool
	KeepDays            int       // 0 follows the global retention, -1 keeps forever, else days
	PaceSec             int       // how long an unread entry stays in All and Unread, in seconds; 0 is off
	ExtractSelector     string    // CSS selector of the element that holds the article, empty for automatic extraction
	ExtractReferer      string    // Referer sent with the request for an entry page, empty for none
	ExtractRewrites     string    // rewrite rules for an entry link before it is fetched, one "from => to" per line
	RSSHubInstance      string    // the RSSHub instance that served the last successful fetch, empty when none did
	WebSubHub           string    // the hub the feed declares, empty for none
	WebSubUntil         time.Time // when the active push subscription runs out, zero when there is none
	Proxy               string    // "" follows the global fetch proxy, ProxyDirect connects without one, else the proxy's address
	ProxyAuthSet        bool      // whether a user name or password is stored for Proxy; they are never read back
	UnreadCount         int
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// SourcePatch holds the fields a user may change; nil leaves a field as it is.
type SourcePatch struct {
	Title         *string
	Kind          *string
	FetchInterval *time.Duration
	// FetchIntervalAuto switches the automatic interval on or off; the interval in use stays until the next fetch
	// or until FetchInterval sets another.
	FetchIntervalAuto *bool
	PromptSummary     *string
	PromptTranslate   *string
	AutoSummary       *bool
	AutoTranslate     *bool
	MuteKeywords      *string
	Paused            *bool
	KeepDays          *int
	PaceSec           *int
	ExtractSelector   *string
	ExtractReferer    *string
	ExtractRewrites   *string
	// Proxy is the source's own fetch proxy. ProxyUser and ProxyPassword are write-only, and "" deletes them; a changed
	// Proxy deletes the stored ones unless new ones are given with it.
	Proxy         *string
	ProxyUser     *string
	ProxyPassword *string
}

// sourceColumnsHead and sourceColumnsTail surround the unread count, which Source and Sources compute differently.
const sourceColumnsHead = `
	s.id, s.url, s.rsshub_path, s.title, s.site_url, s.icon_url, s.kind, s.fetch_interval_sec, s.fetch_interval_auto, s.suggested_kind, s.next_fetch_at,
	s.last_fetched_at, s.last_success_at, s.last_error, s.last_error_kind, s.consecutive_failures, s.prompt_summary, s.prompt_translate,
	s.auto_summary, s.auto_translate, s.mute_keywords, s.paused, s.keep_days, s.pace_sec, s.extract_selector, s.extract_referer, s.extract_rewrites, s.rsshub_instance,
	s.proxy, s.proxy_user <> '' OR s.proxy_password <> '',
	s.websub_hub, CASE WHEN s.websub_state = 'active' THEN s.websub_expires_at END,
	`

const sourceColumnsTail = `, s.created_at, s.updated_at`

// unreadOf counts the unread entries of one source, as it appears in the list; it runs for a single source.
const unreadOf = `(SELECT count(*) FROM entries e WHERE e.source_id = s.id AND e.is_read = 0 AND ` + shown + ` AND ` + live + `)`

// unreadShown is shown for the grouped count, with two changes that keep exactly the entries shown keeps: the global
// keywords come from the one-row table g, read once rather than for every entry, and an entry of a source without
// keywords, while no global keywords are set, is shown before its row is read, so the count mostly runs on the index.
const unreadShown = "(s.deleted_at IS NULL AND ((s.mute_keywords = '' AND g.v = '') OR e.starred = 1 OR " +
	"((s.mute_keywords = '' OR NOT muted(s.mute_keywords, e.title, e.rss_summary)) AND " +
	"(g.v = '' OR NOT muted(g.v, e.title, e.rss_summary)))))"

// unreadByID counts the unread entries of every source in one pass, by the same rules as unreadOf. The partial index
// entries_source_unread is named so that the rows come grouped by source without a sort; sources without a row count as zero.
const unreadByID = `(SELECT e.source_id AS source_id, count(*) AS n FROM entries e INDEXED BY entries_source_unread JOIN sources s ON s.id = e.source_id,
	(SELECT ` + globalMute + ` AS v) g
	WHERE e.is_read = 0 AND ` + unreadShown + ` AND ` + live + ` GROUP BY e.source_id)`

// CreateSource adds a subscription with a title, which may be empty, and returns it. The source is due at once
// unless src.FirstFetchDelay says otherwise.
// It returns an *ExistsError, which matches ErrExists, when the same feed is already subscribed.
// Feed addresses are compared by sourceKeys, so a trailing slash, a capitalised host, a default port, http against
// https, a "www." prefix or a RSSHub route against the same address on an instance does not make a second
// subscription; the address is stored as given. src.AllowDuplicate skips the comparison, but two sources still
// cannot share the very same address.
func (s *Store) CreateSource(ctx context.Context, src NewSource, title string, now time.Time) (Source, error) {
	idx, err := s.DuplicateIndex(ctx)
	if err != nil {
		return Source{}, err
	}
	return s.CreateSourceIn(ctx, idx, src, title, now)
}

// CreateSourceIn is CreateSource with the identities of the existing subscriptions already read, for adding many:
// idx is kept up to date, so it must not be shared between goroutines.
func (s *Store) CreateSourceIn(ctx context.Context, idx *DuplicateIndex, src NewSource, title string, now time.Time) (Source, error) {
	if !src.AllowDuplicate {
		if id, found := idx.Find(src); found {
			return Source{}, &ExistsError{ID: id}
		}
	}
	// A deleted subscription that src would repeat goes now, not at the end of its undo window: adding the feed again
	// is a new subscription, not a restore.
	for _, id := range idx.deletedLike(src) {
		if _, err := s.db.ExecContext(ctx, "DELETE FROM sources WHERE id = ?", id); err != nil {
			return Source{}, err
		}
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO sources (url, rsshub_path, title, kind, fetch_interval_sec, fetch_interval_auto, next_fetch_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		src.URL, src.RSSHubPath, title, src.Kind, int64(src.FetchInterval/time.Second), src.FetchIntervalAuto,
		now.Add(max(src.FirstFetchDelay, 0)).UnixMilli(), now.UnixMilli(), now.UnixMilli())
	if err != nil {
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			if id, found := idx.live[exactKey(src.URL, src.RSSHubPath)]; found {
				return Source{}, &ExistsError{ID: id}
			}
			return Source{}, ErrExists
		}
		return Source{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Source{}, err
	}
	idx.Add(id, src)
	return s.Source(ctx, id)
}

// Sources lists every subscription by title, or by address when the title is empty.
func (s *Store) Sources(ctx context.Context) ([]Source, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT"+sourceColumnsHead+"coalesce(u.n, 0)"+sourceColumnsTail+`
		FROM sources s LEFT JOIN `+unreadByID+` u ON u.source_id = s.id WHERE s.deleted_at IS NULL ORDER BY lower(CASE WHEN s.title <> '' THEN s.title WHEN s.url <> '' THEN s.url ELSE s.rsshub_path END), s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := []Source{}
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

// Source returns one subscription.
func (s *Store) Source(ctx context.Context, id int64) (Source, error) {
	return scanSource(s.db.QueryRowContext(ctx, "SELECT"+sourceColumnsHead+unreadOf+sourceColumnsTail+" FROM sources s WHERE s.id = ? AND s.deleted_at IS NULL", id))
}

// UpdateSource applies a patch and returns the changed subscription. An empty patch changes nothing.
func (s *Store) UpdateSource(ctx context.Context, id int64, patch SourcePatch, now time.Time) (Source, error) {
	var sets []string
	var args []any
	set := func(column string, value any) {
		sets = append(sets, column+" = ?")
		args = append(args, value)
	}
	if patch.Title != nil {
		set("title", *patch.Title)
	}
	if patch.Kind != nil {
		set("kind", *patch.Kind)
	}
	if patch.FetchInterval != nil {
		set("fetch_interval_sec", int64(*patch.FetchInterval/time.Second))
	}
	if patch.FetchIntervalAuto != nil {
		set("fetch_interval_auto", *patch.FetchIntervalAuto)
	}
	if patch.PromptSummary != nil {
		set("prompt_summary", *patch.PromptSummary)
	}
	if patch.PromptTranslate != nil {
		set("prompt_translate", *patch.PromptTranslate)
	}
	if patch.AutoSummary != nil {
		set("auto_summary", *patch.AutoSummary)
	}
	if patch.AutoTranslate != nil {
		set("auto_translate", *patch.AutoTranslate)
	}
	if patch.MuteKeywords != nil {
		set("mute_keywords", *patch.MuteKeywords)
	}
	if patch.Paused != nil {
		set("paused", *patch.Paused)
	}
	if patch.KeepDays != nil {
		set("keep_days", *patch.KeepDays)
	}
	if patch.PaceSec != nil {
		set("pace_sec", *patch.PaceSec)
	}
	if patch.ExtractSelector != nil {
		set("extract_selector", *patch.ExtractSelector)
	}
	if patch.ExtractReferer != nil {
		set("extract_referer", *patch.ExtractReferer)
	}
	if patch.ExtractRewrites != nil {
		set("extract_rewrites", *patch.ExtractRewrites)
	}
	if patch.Proxy != nil {
		set("proxy", *patch.Proxy)
	}
	// Credentials do not follow a source to another proxy: a changed address deletes them unless new ones come with it.
	for _, field := range []struct {
		column string
		value  *string
	}{{"proxy_user", patch.ProxyUser}, {"proxy_password", patch.ProxyPassword}} {
		switch {
		case field.value != nil:
			set(field.column, *field.value)
		case patch.Proxy != nil:
			sets = append(sets, field.column+" = CASE WHEN proxy = ? THEN "+field.column+" ELSE '' END")
			args = append(args, *patch.Proxy)
		}
	}
	if len(sets) > 0 {
		set("updated_at", now.UnixMilli())
		result, err := s.db.ExecContext(ctx, "UPDATE sources SET "+strings.Join(sets, ", ")+" WHERE id = ? AND deleted_at IS NULL", append(args, id)...)
		if err != nil {
			return Source{}, err
		}
		if n, err := result.RowsAffected(); err != nil {
			return Source{}, err
		} else if n == 0 {
			return Source{}, ErrNotFound
		}
	}
	return s.Source(ctx, id)
}

// MutedCount is how many entries of a source the keywords, one per line, would hide now: the same rule as the
// timeline's, so starred entries do not count. They need not be the saved keywords, so a dialog can show the effect
// of an edit before it is saved.
func (s *Store) MutedCount(ctx context.Context, id int64, keywords string) (int, error) {
	if _, err := s.Source(ctx, id); err != nil {
		return 0, err
	}
	var n int
	err := s.db.QueryRowContext(ctx,
		"SELECT count(*) FROM entries e WHERE e.source_id = ? AND e.starred = 0 AND muted(?, e.title, e.rss_summary)",
		id, keywords).Scan(&n)
	return n, err
}

// MutedCountAll says how many entries of all subscriptions the given global keywords hide, without saving them:
// every entry they match, starred ones aside, whether or not a source's own keywords hide it too.
func (s *Store) MutedCountAll(ctx context.Context, keywords string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM entries e JOIN sources s ON s.id = e.source_id
		WHERE s.deleted_at IS NULL AND e.starred = 0 AND muted(?, e.title, e.rss_summary)`, keywords).Scan(&n)
	return n, err
}

// DeleteUndoWindow is how long a deleted subscription can be restored. The client's toast shows for less, so that a
// click on Undo does not meet the end of the window.
const DeleteUndoWindow = 30 * time.Second

// DeleteSource hides a subscription with its entries everywhere. RestoreSource brings it back within
// DeleteUndoWindow; PurgeDeletedSources removes it for good after that. A subscription already deleted is ErrNotFound.
func (s *Store) DeleteSource(ctx context.Context, id int64, now time.Time) error {
	result, err := s.db.ExecContext(ctx, "UPDATE sources SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL", now.UnixMilli(), id)
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

// RestoreSource brings back a subscription deleted at most DeleteUndoWindow before now, with its entries and
// settings, and returns it. Otherwise, or when it was never deleted, it returns ErrNotFound.
func (s *Store) RestoreSource(ctx context.Context, id int64, now time.Time) (Source, error) {
	result, err := s.db.ExecContext(ctx, "UPDATE sources SET deleted_at = NULL WHERE id = ? AND deleted_at > ?",
		id, now.Add(-DeleteUndoWindow).UnixMilli())
	if err != nil {
		return Source{}, err
	}
	if n, err := result.RowsAffected(); err != nil {
		return Source{}, err
	} else if n == 0 {
		return Source{}, ErrNotFound
	}
	return s.Source(ctx, id)
}

// PurgeDeletedSources removes the subscriptions whose undo window is over, and through the foreign key all their
// entries, starred or not. It returns how many subscriptions and how many entries it removed.
func (s *Store) PurgeDeletedSources(ctx context.Context, now time.Time) (sources, entries int64, err error) {
	cutoff := now.Add(-DeleteUndoWindow).UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM entries WHERE source_id IN (SELECT id FROM sources WHERE deleted_at <= ?)", cutoff).Scan(&entries); err != nil {
		return 0, 0, err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM sources WHERE deleted_at <= ?", cutoff)
	if err != nil {
		return 0, 0, err
	}
	if sources, err = result.RowsAffected(); err != nil {
		return 0, 0, err
	}
	return sources, entries, tx.Commit()
}

// MarkSourceDue makes an unpaused subscription due now, without changing its updated_at.
// It returns ErrPaused for a paused subscription.
func (s *Store) MarkSourceDue(ctx context.Context, id int64, now time.Time) error {
	var paused bool
	if err := s.db.QueryRowContext(ctx, "SELECT paused FROM sources WHERE id = ? AND deleted_at IS NULL", id).Scan(&paused); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if paused {
		return ErrPaused
	}
	_, err := s.db.ExecContext(ctx, "UPDATE sources SET next_fetch_at = ? WHERE id = ? AND paused = 0 AND deleted_at IS NULL", now.UnixMilli(), id)
	return err
}

// MarkAllSourcesDue makes every unpaused subscription due now, without changing updated_at, and returns how many it
// made due. The fetch loop picks them up like any other due subscription.
func (s *Store) MarkAllSourcesDue(ctx context.Context, now time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx, "UPDATE sources SET next_fetch_at = ? WHERE paused = 0 AND deleted_at IS NULL", now.UnixMilli())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func scanSource(row interface{ Scan(...any) error }) (Source, error) {
	var src Source
	var interval, next, created, updated int64
	var fetched, success, websubUntil sql.NullInt64
	if err := row.Scan(&src.ID, &src.URL, &src.RSSHubPath, &src.Title, &src.SiteURL, &src.IconURL, &src.Kind, &interval,
		&src.FetchIntervalAuto, &src.SuggestedKind, &next, &fetched, &success, &src.LastError, &src.LastErrorKind, &src.ConsecutiveFailures, &src.PromptSummary, &src.PromptTranslate,
		&src.AutoSummary, &src.AutoTranslate, &src.MuteKeywords, &src.Paused, &src.KeepDays, &src.PaceSec, &src.ExtractSelector, &src.ExtractReferer, &src.ExtractRewrites, &src.RSSHubInstance, &src.Proxy, &src.ProxyAuthSet, &src.WebSubHub, &websubUntil, &src.UnreadCount, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Source{}, ErrNotFound
		}
		return Source{}, err
	}
	src.FetchInterval = time.Duration(interval) * time.Second
	src.NextFetchAt, src.CreatedAt, src.UpdatedAt = time.UnixMilli(next), time.UnixMilli(created), time.UnixMilli(updated)
	if fetched.Valid {
		src.LastFetchedAt = time.UnixMilli(fetched.Int64)
	}
	if success.Valid {
		src.LastSuccessAt = time.UnixMilli(success.Int64)
	}
	if websubUntil.Valid {
		src.WebSubUntil = time.UnixMilli(websubUntil.Int64)
	}
	return src, nil
}
