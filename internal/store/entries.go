package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"modernc.org/sqlite"
)

// Entry is a timeline item as clients see it. Zero values mean absent; the HTML bodies are only read for one entry.
type Entry struct {
	ID            int64
	SourceID      int64
	SourceTitle   string
	Kind          string
	Title         string
	URL           string
	Author        string
	PublishedAt   time.Time
	Summary       string
	AISummary     string // the stored AI summary when it is ok, the start of it in a list; "" when there is none
	ContentHTML   string
	ExtractedHTML string
	ThumbnailURL  string
	DurationSec   int
	Platform      string
	Language      string   // BCP 47 code, "" when unknown
	ReadingSec    *int     // estimated seconds, nil when unknown; see docs/product.md "阅读时间"
	ReadProgress  *float64 // how far the article was read, 0 to 1, nil when never scrolled; see docs/product.md "阅读器"
	Images        []Image
	ExtractStatus string
	IsRead        bool
	ReadAt        time.Time
	Starred       bool
	CreatedAt     time.Time
	// FadesAt and ExpiresAt are when an unread entry of a source with a pace starts to fade and leaves All and Unread;
	// zero for a read or starred entry and for a source without a pace.
	FadesAt   time.Time
	ExpiresAt time.Time
}

// Position is a place on the timeline, which runs from the newest created_at and ID to the oldest.
type Position struct {
	CreatedAt int64
	ID        int64
}

// EntryFilter narrows the timeline. Zero values do not filter.
type EntryFilter struct {
	SourceID      int64
	Kind          string
	Unread        bool
	Starred       bool
	MaxReadingSec int       // only entries that take at most this many seconds; unknown ones never match; 0 does not filter
	Query         string    // matched as one phrase in the full-text index
	Oldest        bool      // list oldest first instead of newest first
	After         *Position // the next page after this position: older entries, or newer ones when Oldest
	UpTo          *Position // only this position and older ones
	Newer         *Position // only entries newer than this position
	OlderThan     int64     // only entries shown as before this time, in Unix milliseconds: published_at, else created_at; 0 does not filter
}

// EntryPatch holds the reading state a user may change; nil leaves it as it is.
type EntryPatch struct {
	IsRead       *bool
	Starred      *bool
	ReadProgress *float64 // 0 to 1; the caller checks the range
}

const entryColumns = `
	e.id, e.source_id, CASE WHEN s.title <> '' THEN s.title WHEN s.url <> '' THEN s.url ELSE s.rsshub_path END, s.kind,
	e.title, e.url, e.author, e.published_at, e.rss_summary, e.thumbnail_url, e.duration_sec, e.platform, e.images,
	e.language, e.extract_status, e.is_read, e.read_at, e.starred, e.created_at, e.reading_sec, e.read_progress,
	s.pace_sec, coalesce(nullif(e.published_at, 0), e.created_at)`

// shown keeps the entries of s that none of its mute keywords hides. Starred entries are never hidden.
// A source without keywords is decided in SQL, so the Go function runs only for sources that have some.
const shown = "(e.starred = 1 OR s.mute_keywords = '' OR NOT muted(s.mute_keywords, e.title, e.rss_summary))"

// live keeps out the unread entries whose source has a pace and whose time is up: they leave All and Unread and
// their counts, and stay in search, Starred and the source's own view. The time is the published time, else the
// time the entry was fetched, plus the pace; read and starred entries never expire. See docs/product.md "节奏".
const live = "(s.pace_sec = 0 OR e.is_read = 1 OR e.starred = 1 OR " +
	"coalesce(nullif(e.published_at, 0), e.created_at) + s.pace_sec * 1000 > CAST(unixepoch('subsec') * 1000 AS INTEGER))"

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("muted", 3, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		keywords, _ := args[0].(string)
		title, _ := args[1].(string)
		summary, _ := args[2].(string)
		return muted(keywords, title, summary), nil
	})
}

// muted reports whether the title or the summary contains one of the keywords, one per line, ignoring case.
func muted(keywords, title, summary string) bool {
	if keywords == "" {
		return false
	}
	title, summary = strings.ToLower(title), strings.ToLower(summary)
	for keyword := range strings.Lines(keywords) {
		keyword = strings.ToLower(strings.TrimSpace(keyword))
		if keyword != "" && (strings.Contains(title, keyword) || strings.Contains(summary, keyword)) {
			return true
		}
	}
	return false
}

// where turns a filter into conditions on entries e joined with sources s. Muted entries never match.
func (f EntryFilter) where() (string, []any) {
	conditions := []string{shown}
	var args []any
	add := func(condition string, values ...any) {
		conditions = append(conditions, condition)
		args = append(args, values...)
	}
	if f.SourceID != 0 {
		add("e.source_id = ?", f.SourceID)
	} else if f.Query == "" {
		add(live)
	}
	// Inside one source the source index already gives the order. The unary plus keeps the kind and unread
	// conditions from drawing the planner to an index over every source, which reads far more rows.
	column := func(name string) string {
		if f.SourceID != 0 {
			return "+" + name
		}
		return name
	}
	if f.Kind != "" {
		add(column("e.kind")+" = ?", f.Kind)
	}
	if f.Unread {
		add(column("e.is_read") + " = 0")
	}
	if f.Starred {
		add("e.starred = 1")
	}
	if f.MaxReadingSec > 0 {
		add("e.reading_sec <= ?", f.MaxReadingSec) // NULL never compares true
	}
	if f.Query != "" {
		add("e.id IN (SELECT rowid FROM entries_fts WHERE entries_fts MATCH ?)", `"`+strings.ReplaceAll(f.Query, `"`, `""`)+`"`)
	}
	if f.After != nil {
		if f.Oldest {
			add("(e.created_at, e.id) > (?, ?)", f.After.CreatedAt, f.After.ID)
		} else {
			add("(e.created_at, e.id) < (?, ?)", f.After.CreatedAt, f.After.ID)
		}
	}
	if f.UpTo != nil {
		add("(e.created_at, e.id) <= (?, ?)", f.UpTo.CreatedAt, f.UpTo.ID)
	}
	if f.Newer != nil {
		add("(e.created_at, e.id) > (?, ?)", f.Newer.CreatedAt, f.Newer.ID)
	}
	if f.OlderThan > 0 {
		add("coalesce(nullif(e.published_at, 0), e.created_at) < ?", f.OlderThan)
	}
	return strings.Join(conditions, " AND "), args
}

// listAISummaryRunes is how much of an AI summary a list reads from the database; the caller trims it further.
const listAISummaryRunes = 1000

// timelineQuery selects the entries of the filter, newest first or oldest first, each with the start of its ok AI summary as the
// column after entryColumns. Its last parameter is the limit.
func (f EntryFilter) timelineQuery() (string, []any) {
	where, args := f.where()
	order := "DESC"
	if f.Oldest {
		order = "ASC"
	}
	return "SELECT" + entryColumns + `, COALESCE(substr(a.text, 1, ` + strconv.Itoa(listAISummaryRunes) + `), '')
		FROM entries e JOIN sources s ON s.id = e.source_id
		LEFT JOIN entry_ai a ON a.entry_id = e.id AND a.task = '` + TaskSummary + `' AND a.status = 'ok'
		WHERE ` + where + " ORDER BY e.created_at " + order + ", e.id " + order + " LIMIT ?", args
}

// Entries returns up to limit entries of the timeline, newest first or oldest first. Their AISummary is the start of the stored AI
// summary, up to 1000 characters; it never makes one.
func (s *Store) Entries(ctx context.Context, filter EntryFilter, limit int) ([]Entry, error) {
	query, args := filter.timelineQuery()
	rows, err := s.db.QueryContext(ctx, query, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []Entry{}
	for rows.Next() {
		var ai string
		entry, err := scanEntry(rows, &ai)
		if err != nil {
			return nil, err
		}
		entry.AISummary = ai
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// Entry returns one entry with its feed and extracted HTML.
func (s *Store) Entry(ctx context.Context, id int64) (Entry, error) {
	var content, extracted string
	entry, err := scanEntry(s.db.QueryRowContext(ctx, "SELECT"+entryColumns+`, e.rss_content_html, e.extracted_html
		FROM entries e JOIN sources s ON s.id = e.source_id WHERE e.id = ?`, id), &content, &extracted)
	entry.ContentHTML, entry.ExtractedHTML = content, extracted
	return entry, err
}

// UpdateEntry changes the reading state and the reading position and returns the entry. Marking a read entry read keeps its read_at.
// Reading state does not change the entry's updated_at, which follows the feed content only.
func (s *Store) UpdateEntry(ctx context.Context, id int64, patch EntryPatch, now time.Time) (Entry, error) {
	if patch.IsRead != nil || patch.Starred != nil {
		result, err := s.db.ExecContext(ctx, `
			UPDATE entries SET
				is_read = coalesce(?1, is_read),
				read_at = CASE WHEN ?1 IS NULL THEN read_at WHEN ?1 = 0 THEN NULL ELSE coalesce(read_at, ?2) END,
				starred = coalesce(?3, starred)
			WHERE id = ?4`, nullBool(patch.IsRead), now.UnixMilli(), nullBool(patch.Starred), id)
		if err != nil {
			return Entry{}, err
		}
		if n, err := result.RowsAffected(); err != nil {
			return Entry{}, err
		} else if n == 0 {
			return Entry{}, ErrNotFound
		}
	}
	if patch.ReadProgress != nil {
		result, err := s.db.ExecContext(ctx, "UPDATE entries SET read_progress = ? WHERE id = ?", *patch.ReadProgress, id)
		if err != nil {
			return Entry{}, err
		}
		if n, err := result.RowsAffected(); err != nil {
			return Entry{}, err
		} else if n == 0 {
			return Entry{}, ErrNotFound
		}
	}
	return s.Entry(ctx, id)
}

// MarkRead marks every unread entry matching the filter as read and returns the IDs it changed, so the change can be
// undone with MarkUnread.
func (s *Store) MarkRead(ctx context.Context, filter EntryFilter, now time.Time) ([]int64, error) {
	filter.Unread = true
	where, args := filter.where()
	rows, err := s.db.QueryContext(ctx, `
		UPDATE entries SET is_read = 1, read_at = ? WHERE id IN (
			SELECT e.id FROM entries e JOIN sources s ON s.id = e.source_id WHERE `+where+") RETURNING id",
		append([]any{now.UnixMilli()}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MarkUnread marks the read entries among ids as unread and returns the IDs of those it changed. Unknown IDs are skipped. When
// readAt is not zero, only entries read at exactly that time (Unix milliseconds) change: the ones a MarkRead at that
// time marked and nothing has marked since.
func (s *Store) MarkUnread(ctx context.Context, ids []int64, readAt int64) ([]int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	statement, err := tx.PrepareContext(ctx, "UPDATE entries SET is_read = 0, read_at = NULL WHERE id = ? AND is_read = 1 AND (? = 0 OR read_at = ?)")
	if err != nil {
		return nil, err
	}
	defer statement.Close()
	marked := []int64{}
	for _, id := range ids {
		result, err := statement.ExecContext(ctx, id, readAt, readAt)
		if err != nil {
			return nil, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if n > 0 {
			marked = append(marked, id)
		}
	}
	return marked, tx.Commit()
}

// EntryPosition returns where an entry sits on the timeline.
func (s *Store) EntryPosition(ctx context.Context, id int64) (Position, error) {
	position := Position{ID: id}
	err := s.db.QueryRowContext(ctx, "SELECT created_at FROM entries WHERE id = ?", id).Scan(&position.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Position{}, ErrNotFound
	}
	return position, err
}

func nullBool(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}

func scanEntry(row interface{ Scan(...any) error }, extra ...any) (Entry, error) {
	var entry Entry
	var published, readAt, duration sql.NullInt64
	var images string
	var language sql.NullString
	var reading sql.NullInt64
	var progress sql.NullFloat64
	var created, paceSec, base int64
	dest := append([]any{&entry.ID, &entry.SourceID, &entry.SourceTitle, &entry.Kind, &entry.Title, &entry.URL,
		&entry.Author, &published, &entry.Summary, &entry.ThumbnailURL, &duration, &entry.Platform, &images,
		&language, &entry.ExtractStatus, &entry.IsRead, &readAt, &entry.Starred, &created, &reading, &progress, &paceSec, &base}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Entry{}, ErrNotFound
		}
		return Entry{}, err
	}
	if err := json.Unmarshal([]byte(images), &entry.Images); err != nil {
		return Entry{}, err
	}
	if published.Valid {
		entry.PublishedAt = time.UnixMilli(published.Int64)
	}
	if readAt.Valid {
		entry.ReadAt = time.UnixMilli(readAt.Int64)
	}
	entry.Language = language.String
	if reading.Valid {
		sec := int(reading.Int64)
		entry.ReadingSec = &sec
	}
	if progress.Valid {
		entry.ReadProgress = &progress.Float64
	}
	entry.DurationSec = int(duration.Int64)
	entry.CreatedAt = time.UnixMilli(created)
	if paceSec > 0 && !entry.IsRead && !entry.Starred {
		entry.FadesAt = time.UnixMilli(base + paceSec*750)
		entry.ExpiresAt = time.UnixMilli(base + paceSec*1000)
	}
	return entry, nil
}
