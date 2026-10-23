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
	"unicode/utf8"

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
	// Group lists the other visible entries with the same link, earliest arrival first, when the caller asked for them
	// (AttachGroups); IsRead and Starred then say so for the whole group. See docs/product.md "同一篇文章只显示一次".
	Group []GroupMember
}

// GroupMember is another copy of an article, carried by another source or by the same one.
type GroupMember struct {
	ID          int64
	SourceID    int64
	SourceTitle string
	Title       string
	URL         string
	IsRead      bool
	Starred     bool
	CreatedAt   time.Time
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
	// Group shows entries with the same link once: only the earliest visible one of them matches, and read and starred are
	// those of the whole group. It has no effect with SourceID or Query set.
	Group bool
}

// EntryPatch holds the reading state a user may change; nil leaves it as it is.
type EntryPatch struct {
	IsRead       *bool
	Starred      *bool
	ReadProgress *float64 // 0 to 1; the caller checks the range
	Group        bool     // IsRead and Starred apply to every visible entry with the same link, not only this one
}

const entryColumns = `
	e.id, e.source_id, CASE WHEN s.title <> '' THEN s.title WHEN s.url <> '' THEN s.url ELSE s.rsshub_path END, s.kind,
	e.title, e.url, e.author, e.published_at, e.rss_summary, e.thumbnail_url, e.duration_sec, e.platform, e.images,
	e.language, e.extract_status, e.is_read, e.read_at, e.starred, e.created_at, e.reading_sec, e.read_progress,
	s.pace_sec, coalesce(nullif(e.published_at, 0), e.created_at)`

// shown keeps the entries of s that none of its mute keywords and none of the global ones hides, and drops those of a deleted source. Starred
// entries are never hidden by keywords. A source without keywords is decided in SQL, so the Go function runs only for
// sources that have some.
const shown = "(s.deleted_at IS NULL AND (e.starred = 1 OR " +
	"((s.mute_keywords = '' OR NOT muted(s.mute_keywords, e.title, e.rss_summary)) AND " +
	"(" + globalMute + " = '' OR NOT muted(" + globalMute + ", e.title, e.rss_summary)))))"

// globalMute is the global muted keywords, empty while the setting is not set.
const globalMute = "coalesce((SELECT value FROM settings WHERE key = '" + MuteSetting + "'), '')"

// live keeps out the unread entries whose source has a pace and whose time is up: they leave All and Unread and
// their counts, and stay in search, Starred and the source's own view. The time is the published time, else the
// time the entry was fetched, plus the pace; read and starred entries never expire. See docs/product.md "节奏".
const live = "(s.pace_sec = 0 OR e.is_read = 1 OR e.starred = 1 OR " +
	"coalesce(nullif(e.published_at, 0), e.created_at) + s.pace_sec * 1000 > CAST(unixepoch('subsec') * 1000 AS INTEGER))"

// copyOf rewrites a condition on entries e and sources s to one on entries m and sources ms.
var copyOf = strings.NewReplacer("e.", "m.", "s.", "ms.").Replace

// visibleCopy keeps the entries m that the timeline can show: not muted, source not deleted, not expired.
var visibleCopy = copyOf(shown + " AND " + live)

// otherCopies selects the visible entries m (of source ms) that share the link of entry e and are not e.
const otherCopies = "FROM entries m JOIN sources ms ON ms.id = m.source_id WHERE m.link_key = e.link_key AND m.link_key <> '' AND m.id <> e.id"

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

// MinIndexedQuery is the fewest characters the trigram index can match; a shorter query is a ShortQuery.
const MinIndexedQuery = 3

// ShortQuery reports whether q is a search of one or two characters, which the index cannot answer. It is matched
// against the title and the summary only, by LIKE.
func ShortQuery(q string) bool {
	return q != "" && utf8.RuneCountInString(q) < MinIndexedQuery
}

// likeEscaper escapes the characters LIKE treats as patterns, with backslash as the escape character.
var likeEscaper = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)

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
	grouped := f.Group && f.SourceID == 0 && f.Query == ""
	visible := " AND " + visibleCopy
	if grouped {
		// Only the earliest visible copy of an article is a row.
		add("(e.link_key = '' OR NOT EXISTS (SELECT 1 " + otherCopies + " AND (m.created_at, m.id) < (e.created_at, e.id)" + visible + "))")
	}
	if f.Unread {
		add(column("e.is_read") + " = 0")
		if grouped {
			add("(e.link_key = '' OR NOT EXISTS (SELECT 1 " + otherCopies + " AND m.is_read = 1" + visible + "))")
		}
	}
	if f.Starred {
		if grouped {
			add("(e.starred = 1 OR (e.link_key <> '' AND EXISTS (SELECT 1 " + otherCopies + " AND m.starred = 1" + visible + ")))")
		} else {
			add("e.starred = 1")
		}
	}
	if f.MaxReadingSec > 0 {
		add("e.reading_sec <= ?", f.MaxReadingSec) // NULL never compares true
	}
	if ShortQuery(f.Query) {
		// The trigram index cannot match fewer than three characters: scan the title and the summary instead.
		pattern := "%" + likeEscaper.Replace(f.Query) + "%"
		add(`(e.title LIKE ? ESCAPE '\' OR e.rss_summary LIKE ? ESCAPE '\')`, pattern, pattern)
	} else if f.Query != "" {
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if filter.Group && filter.SourceID == 0 && filter.Query == "" {
		err = s.AttachGroups(ctx, entries)
	}
	return entries, err
}

// AttachGroups fills Group of each entry with the other visible entries that share its link, earliest arrival first, and
// makes IsRead and Starred those of the whole group: read if any copy is read, starred if any is starred. Entries
// without copies are left as they are.
func (s *Store) AttachGroups(ctx context.Context, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	at := make(map[int64]int, len(entries))
	args := make([]any, len(entries))
	for i, entry := range entries {
		at[entry.ID] = i
		args[i] = entry.ID
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, m.id, m.source_id, CASE WHEN ms.title <> '' THEN ms.title WHEN ms.url <> '' THEN ms.url ELSE ms.rsshub_path END, m.title, m.url, m.is_read, m.starred, m.created_at
		FROM entries e JOIN entries m ON m.link_key = e.link_key AND m.link_key <> '' AND m.id <> e.id
		JOIN sources ms ON ms.id = m.source_id
		WHERE e.id IN (?`+strings.Repeat(", ?", len(entries)-1)+") AND "+visibleCopy+`
		ORDER BY e.id, m.created_at, m.id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var of, created int64
		var member GroupMember
		if err := rows.Scan(&of, &member.ID, &member.SourceID, &member.SourceTitle, &member.Title, &member.URL, &member.IsRead, &member.Starred, &created); err != nil {
			return err
		}
		member.CreatedAt = time.UnixMilli(created)
		entry := &entries[at[of]]
		entry.Group = append(entry.Group, member)
		entry.IsRead = entry.IsRead || member.IsRead
		entry.Starred = entry.Starred || member.Starred
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range entries {
		if entries[i].IsRead || entries[i].Starred {
			entries[i].FadesAt, entries[i].ExpiresAt = time.Time{}, time.Time{}
		}
	}
	return nil
}

// UnreadTotal counts the rows of the timeline's Unread view, entries with the same link counted once when group is set.
// Without grouping it is the sum of the sources' unread counts.
func (s *Store) UnreadTotal(ctx context.Context, group bool) (int, error) {
	where, args := EntryFilter{Unread: true, Group: group}.where()
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM entries e JOIN sources s ON s.id = e.source_id WHERE "+where, args...).Scan(&n)
	return n, err
}

// GroupSameLinkSetting is the setting that says whether entries with the same link show once; "0" is off, anything else
// on.
const GroupSameLinkSetting = "group_same_link"

// GroupSameLink reports whether the timeline shows entries with the same link once.
func (s *Store) GroupSameLink(ctx context.Context) (bool, error) {
	value, err := s.Setting(ctx, GroupSameLinkSetting)
	return value != "0", err
}

// Entry returns one entry with its feed and extracted HTML.
func (s *Store) Entry(ctx context.Context, id int64) (Entry, error) {
	var content, extracted string
	entry, err := scanEntry(s.db.QueryRowContext(ctx, "SELECT"+entryColumns+`, e.rss_content_html, e.extracted_html
		FROM entries e JOIN sources s ON s.id = e.source_id WHERE e.id = ? AND s.deleted_at IS NULL`, id), &content, &extracted)
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
			WHERE id IN (SELECT e.id FROM entries e JOIN sources s ON s.id = e.source_id WHERE s.deleted_at IS NULL AND
				(e.id = ?4 OR (?5 AND e.link_key <> '' AND e.link_key = (SELECT link_key FROM entries WHERE id = ?4) AND `+shown+" AND "+live+`)))`,
			nullBool(patch.IsRead), now.UnixMilli(), nullBool(patch.Starred), id, patch.Group)
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
		result, err := s.db.ExecContext(ctx, "UPDATE entries SET read_progress = ? WHERE id = ? AND source_id IN (SELECT id FROM sources WHERE deleted_at IS NULL)", *patch.ReadProgress, id)
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

// WithGroup returns the entry with its group attached (AttachGroups).
func (s *Store) WithGroup(ctx context.Context, entry Entry) (Entry, error) {
	list := []Entry{entry}
	err := s.AttachGroups(ctx, list)
	return list[0], err
}

// MarkRead marks every unread entry matching the filter as read and returns the IDs it changed, so the change can be
// undone with MarkUnread. With a grouping filter the rows it matches are marked together with the other visible entries
// of their groups, and those IDs are returned too.
func (s *Store) MarkRead(ctx context.Context, filter EntryFilter, now time.Time) ([]int64, error) {
	filter.Unread = true
	where, args := filter.where()
	query := "SELECT e.id FROM entries e JOIN sources s ON s.id = e.source_id WHERE " + where
	args = append([]any{now.UnixMilli()}, args...)
	if filter.Group && filter.SourceID == 0 && filter.Query == "" {
		query += " UNION SELECT m.id FROM entries e JOIN sources s ON s.id = e.source_id JOIN entries m ON m.link_key = e.link_key AND m.link_key <> '' " +
			"JOIN sources ms ON ms.id = m.source_id WHERE " + where + " AND " + visibleCopy
		args = append(args, args[1:]...)
	}
	rows, err := s.db.QueryContext(ctx, "UPDATE entries SET is_read = 1, read_at = ? WHERE is_read = 0 AND id IN ("+query+") RETURNING id", args...)
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
	err := s.db.QueryRowContext(ctx, "SELECT created_at FROM entries WHERE id = ? AND source_id IN (SELECT id FROM sources WHERE deleted_at IS NULL)", id).Scan(&position.CreatedAt)
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
