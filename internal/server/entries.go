package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yangflow/oriel/internal/store"
)

const (
	defaultPage = 50
	maxPage     = 100
	// minQuery is the shortest text the trigram index can match.
	minQuery = 3
	// maxListAISummary is the most characters of an AI summary a list carries; the full one is at /api/entries/{id}/ai.
	maxListAISummary = 300
	// maxBudgetMinutes is the longest reading-time budget a client may ask for: one day.
	maxBudgetMinutes = 1440
)

type entryJSON struct {
	ID            int64         `json:"id"`
	SourceID      int64         `json:"sourceId"`
	SourceTitle   string        `json:"sourceTitle"`
	Kind          string        `json:"kind"`
	Title         string        `json:"title"`
	URL           string        `json:"url"`
	Author        string        `json:"author"`
	PublishedAt   *int64        `json:"publishedAt"`
	Summary       string        `json:"summary"`
	AISummary     string        `json:"aiSummary,omitempty"`
	ThumbnailURL  string        `json:"thumbnailUrl"`
	DurationSec   *int          `json:"durationSec"`
	Platform      string        `json:"platform"`
	Language      string        `json:"language"`
	ReadingSec    *int          `json:"readingSec"`
	ReadProgress  *float64      `json:"readProgress,omitempty"`
	Images        []store.Image `json:"images"`
	ExtractStatus string        `json:"extractStatus"`
	IsRead        bool          `json:"isRead"`
	ReadAt        *int64        `json:"readAt"`
	Starred       bool          `json:"starred"`
	CreatedAt     int64         `json:"createdAt"`
	// FadesAt and ExpiresAt are set for an unread entry of a source with a pace, in Unix milliseconds.
	FadesAt   *int64 `json:"fadesAt,omitempty"`
	ExpiresAt *int64 `json:"expiresAt,omitempty"`
	// Group carries the other visible entries with the same link; it is left out for an entry that has none.
	Group *groupJSON `json:"group,omitempty"`
}

type groupJSON struct {
	By      string            `json:"by"`
	Entries []groupMemberJSON `json:"entries"`
}

type groupMemberJSON struct {
	ID          int64  `json:"id"`
	SourceID    int64  `json:"sourceId"`
	SourceTitle string `json:"sourceTitle"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	IsRead      bool   `json:"isRead"`
	Starred     bool   `json:"starred"`
	CreatedAt   int64  `json:"createdAt"`
}

type fullEntryJSON struct {
	entryJSON
	ContentHTML   string `json:"contentHtml"`
	ExtractedHTML string `json:"extractedHtml"`
}

func millis(t time.Time) *int64 {
	if t.IsZero() {
		return nil
	}
	ms := t.UnixMilli()
	return &ms
}

func entryToJSON(e store.Entry) entryJSON {
	var duration *int
	if e.DurationSec != 0 {
		duration = &e.DurationSec
	}
	images := e.Images
	if images == nil {
		images = []store.Image{}
	}
	var group *groupJSON
	if len(e.Group) > 0 {
		group = &groupJSON{By: "link", Entries: make([]groupMemberJSON, len(e.Group))}
		for i, m := range e.Group {
			group.Entries[i] = groupMemberJSON{ID: m.ID, SourceID: m.SourceID, SourceTitle: m.SourceTitle, Title: m.Title,
				URL: m.URL, IsRead: m.IsRead, Starred: m.Starred, CreatedAt: m.CreatedAt.UnixMilli()}
		}
	}
	return entryJSON{
		Group: group,
		ID:    e.ID, SourceID: e.SourceID, SourceTitle: e.SourceTitle, Kind: e.Kind, Title: e.Title, URL: e.URL,
		Author: e.Author, PublishedAt: millis(e.PublishedAt), Summary: e.Summary, ThumbnailURL: e.ThumbnailURL,
		DurationSec: duration, Platform: e.Platform, Language: e.Language, ReadingSec: e.ReadingSec, ReadProgress: e.ReadProgress, Images: images, ExtractStatus: e.ExtractStatus,
		IsRead: e.IsRead, ReadAt: millis(e.ReadAt), Starred: e.Starred, CreatedAt: e.CreatedAt.UnixMilli(),
		FadesAt: millis(e.FadesAt), ExpiresAt: millis(e.ExpiresAt),
	}
}

// grouping reports whether the timeline shows entries with the same link once.
func (s *Server) grouping(w http.ResponseWriter, r *http.Request) (bool, bool) {
	on, err := s.store.GroupSameLink(r.Context())
	if err != nil {
		internal(w, err)
		return false, false
	}
	return on, true
}

// clipSummary flattens the white space of an AI summary and keeps at most maxListAISummary characters of it. A cut
// falls at a space when one is among the last fifth of what is kept, otherwise at the character; "…" follows it.
func clipSummary(text string) string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) <= maxListAISummary {
		return string(runes)
	}
	runes = runes[:maxListAISummary]
	for i := len(runes) - 1; i >= maxListAISummary*4/5; i-- {
		if runes[i] == ' ' {
			runes = runes[:i]
			break
		}
	}
	return string(runes) + "…"
}

// cursor writes a timeline position as an opaque page token; parseCursor reads it back.
func cursor(p store.Position) string {
	return strconv.FormatInt(p.CreatedAt, 10) + "." + strconv.FormatInt(p.ID, 10)
}

func parseCursor(raw string) (store.Position, bool) {
	created, id, ok := strings.Cut(raw, ".")
	if !ok {
		return store.Position{}, false
	}
	p := store.Position{}
	var err1, err2 error
	p.CreatedAt, err1 = strconv.ParseInt(created, 10, 64)
	p.ID, err2 = strconv.ParseInt(id, 10, 64)
	return p, err1 == nil && err2 == nil && p.ID > 0
}

func (s *Server) listEntries(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := store.EntryFilter{
		Kind: query.Get("kind"), Unread: query.Get("unread") == "1", Starred: query.Get("starred") == "1",
		Query: strings.TrimSpace(query.Get("q")),
	}
	if raw := query.Get("source"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			fail(w, http.StatusBadRequest, "invalid_source")
			return
		}
		filter.SourceID = id
	}
	if filter.Kind != "" && !kinds[filter.Kind] {
		fail(w, http.StatusBadRequest, "invalid_kind")
		return
	}
	if filter.Query != "" && utf8.RuneCountInString(filter.Query) < minQuery {
		fail(w, http.StatusBadRequest, "query_too_short")
		return
	}
	if raw := query.Get("maxMinutes"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxBudgetMinutes {
			fail(w, http.StatusBadRequest, "invalid_max_minutes")
			return
		}
		filter.MaxReadingSec = n * 60
	}
	switch query.Get("order") {
	case "", "desc":
	case "asc":
		filter.Oldest = true
	default:
		fail(w, http.StatusBadRequest, "invalid_order")
		return
	}
	limit := defaultPage
	if raw := query.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxPage {
			fail(w, http.StatusBadRequest, "invalid_limit")
			return
		}
		limit = n
	}
	if raw := query.Get("cursor"); raw != "" {
		after, ok := parseCursor(raw)
		if !ok {
			fail(w, http.StatusBadRequest, "invalid_cursor")
			return
		}
		filter.After = &after
	}
	group, ok := s.grouping(w, r)
	if !ok {
		return
	}
	filter.Group = group
	// One extra row tells whether another page follows.
	list, err := s.store.Entries(r.Context(), filter, limit+1)
	if err != nil {
		internal(w, err)
		return
	}
	var next *string
	if len(list) > limit {
		list = list[:limit]
		last := list[limit-1]
		token := cursor(store.Position{CreatedAt: last.CreatedAt.UnixMilli(), ID: last.ID})
		next = &token
	}
	items := make([]entryJSON, len(list))
	for i, entry := range list {
		items[i] = entryToJSON(entry)
		items[i].AISummary = clipSummary(entry.AISummary)
	}
	reply(w, http.StatusOK, map[string]any{"entries": items, "nextCursor": next})
}

func (s *Server) getEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	group, ok := s.grouping(w, r)
	if !ok {
		return
	}
	entry, err := s.store.Entry(r.Context(), id)
	if err == nil && group {
		entry, err = s.store.WithGroup(r.Context(), entry)
	}
	replyEntry(w, entry, err)
}

func (s *Server) updateEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		IsRead       *bool    `json:"isRead"`
		Starred      *bool    `json:"starred"`
		ReadProgress *float64 `json:"readProgress"`
	}
	if !readBody(w, r, &body) {
		return
	}
	if p := body.ReadProgress; p != nil && !(*p >= 0 && *p <= 1) {
		fail(w, http.StatusBadRequest, "invalid_progress")
		return
	}
	group, ok := s.grouping(w, r)
	if !ok {
		return
	}
	entry, err := s.store.UpdateEntry(r.Context(), id, store.EntryPatch{IsRead: body.IsRead, Starred: body.Starred,
		ReadProgress: body.ReadProgress, Group: group}, s.now())
	if err == nil && group {
		entry, err = s.store.WithGroup(r.Context(), entry)
	}
	replyEntry(w, entry, err)
}

func (s *Server) retryExtraction(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	entry, err := s.store.RetryExtraction(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotRetryable):
		fail(w, http.StatusConflict, "not_retryable")
	case err == nil:
		reply(w, http.StatusAccepted, fullEntryJSON{entryToJSON(entry), entry.ContentHTML, entry.ExtractedHTML})
	default:
		replyEntry(w, entry, err)
	}
}

func replyEntry(w http.ResponseWriter, entry store.Entry, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "not_found")
	case err != nil:
		internal(w, err)
	default:
		reply(w, http.StatusOK, fullEntryJSON{entryToJSON(entry), entry.ContentHTML, entry.ExtractedHTML})
	}
}

func (s *Server) markRead(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SourceID int64  `json:"sourceId"`
		Kind     string `json:"kind"`
		UpTo     int64  `json:"upTo"`
		Newer    int64  `json:"newerThan"`
		Older    *int64 `json:"olderThan"`
	}
	if r.ContentLength != 0 && !readBody(w, r, &body) {
		return
	}
	if body.Kind != "" && !kinds[body.Kind] {
		fail(w, http.StatusBadRequest, "invalid_kind")
		return
	}
	group, ok := s.grouping(w, r)
	if !ok {
		return
	}
	filter := store.EntryFilter{SourceID: body.SourceID, Kind: body.Kind, Group: group}
	if body.Older != nil {
		if *body.Older <= 0 {
			fail(w, http.StatusBadRequest, "invalid_older_than")
			return
		}
		filter.OlderThan = *body.Older
	}
	for _, bound := range []struct {
		id  int64
		set **store.Position
	}{{body.UpTo, &filter.UpTo}, {body.Newer, &filter.Newer}} {
		if bound.id == 0 {
			continue
		}
		position, err := s.store.EntryPosition(r.Context(), bound.id)
		if errors.Is(err, store.ErrNotFound) {
			fail(w, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			internal(w, err)
			return
		}
		*bound.set = &position
	}
	readAt := s.now()
	ids, err := s.store.MarkRead(r.Context(), filter, readAt)
	if err != nil {
		internal(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"marked": len(ids), "ids": ids, "readAt": readAt.UnixMilli()})
}

// maxUnread bounds one undo of "Mark all as read".
const maxUnread = 10000

func (s *Server) markUnread(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs    []int64 `json:"ids"`
		ReadAt int64   `json:"readAt"`
	}
	if !readBody(w, r, &body) {
		return
	}
	if body.ReadAt < 0 || len(body.IDs) == 0 || len(body.IDs) > maxUnread {
		fail(w, http.StatusBadRequest, "invalid_ids")
		return
	}
	marked, err := s.store.MarkUnread(r.Context(), body.IDs, body.ReadAt)
	if err != nil {
		internal(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"marked": len(marked), "ids": marked})
}
