package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
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
	ContentHTML   string
	ExtractedHTML string
	ThumbnailURL  string
	DurationSec   int
	Platform      string
	Language      string // BCP 47 code, "" when unknown
	Images        []Image
	ExtractStatus string
	IsRead        bool
	ReadAt        time.Time
	Starred       bool
	CreatedAt     time.Time
}

// Position is a place on the timeline, which runs from the newest created_at and ID to the oldest.
type Position struct {
	CreatedAt int64
	ID        int64
}

// EntryFilter narrows the timeline. Zero values do not filter.
type EntryFilter struct {
	SourceID int64
	Kind     string
	Unread   bool
	Starred  bool
	Query    string    // matched as one phrase in the full-text index
	After    *Position // only entries older than this position
	UpTo     *Position // only this position and older ones
	Newer    *Position // only entries newer than this position
}

// EntryPatch holds the reading state a user may change; nil leaves it as it is.
type EntryPatch struct {
	IsRead  *bool
	Starred *bool
}

const entryColumns = `
	e.id, e.source_id, CASE WHEN s.title <> '' THEN s.title WHEN s.url <> '' THEN s.url ELSE s.rsshub_path END, s.kind,
	e.title, e.url, e.author, e.published_at, e.rss_summary, e.thumbnail_url, e.duration_sec, e.platform, e.images,
	e.language, e.extract_status, e.is_read, e.read_at, e.starred, e.created_at`

// shown keeps the entries of s that none of its mute keywords hides. Starred entries are never hidden.
const shown = "(e.starred = 1 OR NOT muted(s.mute_keywords, e.title, e.rss_summary))"

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
	}
	if f.Kind != "" {
		add("s.kind = ?", f.Kind)
	}
	if f.Unread {
		add("e.is_read = 0")
	}
	if f.Starred {
		add("e.starred = 1")
	}
	if f.Query != "" {
		add("e.id IN (SELECT rowid FROM entries_fts WHERE entries_fts MATCH ?)", `"`+strings.ReplaceAll(f.Query, `"`, `""`)+`"`)
	}
	if f.After != nil {
		add("(e.created_at, e.id) < (?, ?)", f.After.CreatedAt, f.After.ID)
	}
	if f.UpTo != nil {
		add("(e.created_at, e.id) <= (?, ?)", f.UpTo.CreatedAt, f.UpTo.ID)
	}
	if f.Newer != nil {
		add("(e.created_at, e.id) > (?, ?)", f.Newer.CreatedAt, f.Newer.ID)
	}
	return strings.Join(conditions, " AND "), args
}

// Entries returns up to limit entries of the timeline, newest first.
func (s *Store) Entries(ctx context.Context, filter EntryFilter, limit int) ([]Entry, error) {
	where, args := filter.where()
	rows, err := s.db.QueryContext(ctx, "SELECT"+entryColumns+`
		FROM entries e JOIN sources s ON s.id = e.source_id
		WHERE `+where+" ORDER BY e.created_at DESC, e.id DESC LIMIT ?", append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []Entry{}
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
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

// UpdateEntry changes the reading state and returns the entry. Marking a read entry read keeps its read_at.
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

// MarkUnread marks the read entries among ids as unread and returns how many changed. Unknown IDs are skipped.
func (s *Store) MarkUnread(ctx context.Context, ids []int64) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	statement, err := tx.PrepareContext(ctx, "UPDATE entries SET is_read = 0, read_at = NULL WHERE id = ? AND is_read = 1")
	if err != nil {
		return 0, err
	}
	defer statement.Close()
	var marked int64
	for _, id := range ids {
		result, err := statement.ExecContext(ctx, id)
		if err != nil {
			return 0, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		marked += n
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
	var created int64
	dest := append([]any{&entry.ID, &entry.SourceID, &entry.SourceTitle, &entry.Kind, &entry.Title, &entry.URL,
		&entry.Author, &published, &entry.Summary, &entry.ThumbnailURL, &duration, &entry.Platform, &images,
		&language, &entry.ExtractStatus, &entry.IsRead, &readAt, &entry.Starred, &created}, extra...)
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
	entry.DurationSec = int(duration.Int64)
	entry.CreatedAt = time.UnixMilli(created)
	return entry, nil
}
