// Package timeline is the main screen: the entries of every subscription in one list, newest first, each kind drawn
// in its own way; see design/screens/timeline.html. Everything it shows comes through the HTTP API.
package timeline

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/prefs"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/reader"
	"github.com/yangflow/oriel/internal/ui/subscriptions"
	"github.com/yangflow/oriel/internal/ui/theme"
)

//go:embed copy.json
var catalog []byte

type copy struct {
	All, Article, Video, Social, Image, Kinds, Sources, Everything, Unread, Read, Starred, Show             string
	Search, SearchLabel, Clear, UnreadCount, Results, MoreResults, MarkAllIn, Star, Unstar, MarkRead        string
	MarkUnread, Open, Failing, NewEntries, Loading, LoadingMore, LoadMoreFailed, AllLoaded                  string
	TooShort, NoResults, NoResultsHint, UnreadDone, UnreadDoneHint, NoStarred, NoStarredHint                string
	Empty, EmptyHint, NoSubscriptions, NoSubscriptionsHint, Error, ErrorHint, Retry, Marked                 string
	Starring, Unstarring, ActionFailed, OpenFailed, ThisComputer, Server, Settings, Duration                string
	MorePictures, Pictures, Now, Minutes, Hours, Yesterday, Days, DateLayout, YearLayout, Separator         string
	AddSource, EditSource, AddFirst, PausedMark, Undo, Dismiss, NoMoreUnread, KeysTitle, KeysMenu, HelpMenu string
	Close, Or, InTimeline, InReader, Anywhere, KeyNext, KeyPrevious, KeyNextUnread, KeyOpen, KeyStar        string
	KeyRead, KeyOpenBrowser, KeySearch, KeyEscape, KeyUndo, KeyNextArticle, KeyPreviousArticle              string
	KeyBack, KeyAdd, KeySettings, KeyKeys                                                                   string
}

var translations = func() map[string]copy {
	var values map[string]copy
	if err := json.Unmarshal(catalog, &values); err != nil {
		panic(err)
	}
	return values
}()

const (
	pageSize     = 50
	nearEnd      = 10               // rows from the end of what is loaded at which the next page loads
	pollEvery    = 60 * time.Second // how often the sources and the first page are checked for new entries
	debounce     = 300 * time.Millisecond
	minQuery     = 3 // characters a search needs, as the server's trigram index does
	maxDownloads = 4
	keepPictures = 300 // decoded pictures kept in memory
	wallAhead    = 800 // how far beyond the view the image wall loads pictures and the next page
)

// kinds are the tabs of the sidebar, "" for all.
var kinds = []string{"", "article", "video", "social", "image"}

// API is what the screen reads and writes on the server; client.Session is one.
type API interface {
	subscriptions.API
	reader.API
	Entries(ctx context.Context, q client.Query) (client.Page, error)
	Sources(ctx context.Context) ([]client.Source, error)
	SetEntry(ctx context.Context, id int64, patch client.EntryPatch) (client.Entry, error)
	MarkRead(ctx context.Context, scope client.ReadScope) ([]int64, error)
	MarkUnread(ctx context.Context, ids []int64) (int, error)
}

// Deps is what the screen does outside itself. Go runs work away from the UI thread and then the function it
// returns on the UI thread. Now is the clock and Decode turns the bytes of a picture into a bitmap, off the UI thread;
// they default to time.Now and ui.DecodeBitmap. OpenConnect shows the connect screen, from the server bar,
// OpenSettings the settings, from the gear beside it, OpenAISettings the settings on the AI group, from the entry
// points of summaries and translations while AI is not set up, and Expired the connect screen because the server no
// longer takes the session. Reading gives the reading preferences of this device, for the reader.
type Deps struct {
	Go             func(work func() (apply func()))
	Now            func() time.Time
	Decode         func([]byte) (*ui.Bitmap, error)
	OpenConnect    func()
	OpenSettings   func()
	OpenAISettings func()
	Expired        func()
	Reading        func() prefs.Preferences
}

// scope is what the sidebar chose: a kind, "" for all, or one source, with kind its kind for the tabs.
type scope struct {
	kind   string
	source int64
}

const (
	everything = iota
	unread
	starred
)

// picture is a picture of an entry, once it loads: bitmap, or failed, which shows the placeholder until a reload.
type picture struct {
	bitmap *ui.Bitmap
	failed bool
	used   int // when it was last drawn, to drop the least recently drawn
}

// marking is a "mark all as read" that can be undone: the entries it marked, and the text of its toast, once shown.
type marking struct {
	ids   []int64
	text  string
	shown bool
}

// The IDs of the toasts the screen replaces rather than stacks.
const (
	markedToast = "timeline.marked"
	noMoreToast = "timeline.no-more-unread"
)

// Screen keeps the timeline: the scope, the entries loaded, and the work under way.
type Screen struct {
	deps   Deps
	text   copy
	api    API
	server string // the host of the server, "" for this computer
	gen    int    // counts servers used, so the results of the one before are dropped

	sources       []client.Source
	sourcesLoaded bool
	scope         scope
	tab           int // the tab of scope.kind
	filter        int // everything, unread or starred

	query     string    // as typed
	searched  string    // the search in effect, trimmed
	editedAt  time.Time // when the query last changed, for the debounce
	editing   bool
	searchBox bool // give the search field the focus in the next frame
	typing    bool // the search field has the focus
	clearing  bool // Escape cleared the search field

	entries  []client.Entry
	next     string // the cursor of the next page, "" at the end
	loading  bool   // the first page
	loadErr  bool
	more     bool // the next page
	moreErr  bool
	wantMore bool // a row near the end was drawn in this frame
	run      int  // counts first pages, so a result the user moved on from is dropped
	polledAt time.Time
	polling  bool
	fresh    int // entries newer than the top one, found by polling

	selected int // the entry chosen, -1 for none
	focusRow int // the entry to give the focus in the next frame, -1 for none
	hovered  int // the entry under the pointer in the last frame
	toTop    bool
	list     ui.ListState
	grid     ui.GridState
	wall     ui.ScrollState

	pictures  map[string]*picture
	wanted    []string // pictures to download, the latest asked first
	downloads int
	drawn     int

	undoable *marking              // the last "mark all as read", until its toast closes
	seeking  bool                  // N found no unread entry loaded and waits for the next page
	noMore   bool                  // show that no unread entry is below
	keys     bool                  // the key sheet shows
	subs     *subscriptions.Screen // the dialogs that add and edit subscriptions
	reader   *reader.Screen        // in the place of the list while an article is read
	reading  int64                 // the entry read, 0 for none
	announce string
	toast    string
}

// New selects the best available translation, falling back to English. The screen shows nothing until Use.
func New(systemLocale string, deps Deps) *Screen {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Decode == nil {
		deps.Decode = ui.DecodeBitmap
	}
	s := &Screen{deps: deps, text: translations[locale.Language(systemLocale)], selected: -1, focusRow: -1,
		hovered: -1, pictures: map[string]*picture{}}
	s.subs = subscriptions.New(systemLocale, subscriptions.Deps{
		Go:           deps.Go,
		Now:          deps.Now,
		Sources:      func() []client.Source { return s.sources },
		Icon:         s.bitmap,
		Added:        s.added,
		Changed:      s.changed,
		Removed:      s.removed,
		Show:         func(id int64) { s.choose(scope{kind: s.kindOf(id), source: id}) },
		OpenSettings: deps.OpenAISettings,
		Expired: func() {
			s.gen++
			s.api = nil
			s.deps.Expired()
		},
	})
	s.reader = reader.New(systemLocale, reader.Deps{
		Go:           deps.Go,
		Now:          deps.Now,
		Decode:       deps.Decode,
		Expired:      s.expired,
		OpenSettings: deps.OpenAISettings,
		Reading:      deps.Reading,
		Back:         s.back,
		Move:         s.step,
		NextUnread:   s.nextUnread,
		Star:         func() { s.toggleStar(s.index(s.reading)) },
		ToggleRead:   func() { s.toggleRead(s.index(s.reading)) },
	})
	return s
}

// Use shows the timeline of api, whose host is server ("" for this computer), from the start.
func (s *Screen) Use(api API, server string) {
	s.api, s.server = api, server
	s.gen++
	s.sources, s.sourcesLoaded = nil, false
	s.scope, s.tab, s.filter = scope{}, 0, everything
	s.query, s.searched, s.editing = "", "", false
	s.pictures, s.wanted, s.downloads = map[string]*picture{}, nil, 0
	s.undoable, s.seeking, s.keys = nil, false, false
	s.subs.Use(api)
	s.reader.Use(api)
	s.loadSources()
	s.reload()
}

// Refresh loads the subscriptions and the entries again, as after an import added subscriptions.
func (s *Screen) Refresh() {
	s.loadSources()
	s.reload()
}

func (s *Screen) expired(err error) bool {
	if !errors.Is(err, client.ErrUnauthorized) {
		return false
	}
	s.gen++
	s.api = nil
	s.subs.Use(nil)
	s.reader.Use(nil)
	s.reading = 0
	s.deps.Expired()
	return true
}

func (s *Screen) kindOf(id int64) string {
	for _, src := range s.sources {
		if src.ID == id {
			return src.Kind
		}
	}
	return ""
}

// added shows a subscription the server took, until the sources load again with it.
func (s *Screen) added(src client.Source) {
	s.sources = append(s.sources, src)
	s.loadSources()
}

// changed shows the change of a subscription, and the entries again when it changes which ones show.
func (s *Screen) changed(src client.Source) {
	i := slices.IndexFunc(s.sources, func(old client.Source) bool { return old.ID == src.ID })
	if i < 0 {
		s.loadSources()
		return
	}
	old := s.sources[i]
	s.sources[i] = src
	s.loadSources()
	if s.scope.source == src.ID && src.Kind != old.Kind {
		s.scope.kind = src.Kind
		s.tab = max(0, indexOf(kinds, src.Kind))
	}
	if src.Title != old.Title || src.Kind != old.Kind || src.MuteKeywords != old.MuteKeywords {
		s.reload()
	}
}

// removed drops a subscription the server no longer has, with its entries.
func (s *Screen) removed(id int64) {
	s.sources = slices.DeleteFunc(s.sources, func(src client.Source) bool { return src.ID == id })
	s.loadSources()
	if s.scope.source == id {
		s.choose(scope{})
		return
	}
	s.reload()
}

func (s *Screen) loadSources() {
	if s.api == nil {
		return
	}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		sources, err := api.Sources(context.Background())
		return func() {
			if gen != s.gen || s.expired(err) || err != nil {
				return
			}
			s.sources, s.sourcesLoaded = sources, true
		}
	})
}

func (s *Screen) query1(cursor string) client.Query {
	q := client.Query{Source: s.scope.source, Unread: s.filter == unread, Starred: s.filter == starred,
		Search: s.searched, Limit: pageSize, Cursor: cursor}
	if s.scope.source == 0 {
		q.Kind = s.scope.kind
	}
	return q
}

// reload loads the first page of the scope again, from the top.
func (s *Screen) reload() {
	if s.api == nil {
		return
	}
	s.run++
	run, gen, api, q := s.run, s.gen, s.api, s.query1("")
	s.entries, s.next, s.loading, s.loadErr, s.more, s.moreErr, s.fresh = nil, "", true, false, false, false, 0
	s.selected, s.focusRow, s.hovered, s.toTop, s.reading, s.seeking = -1, -1, -1, true, 0, false
	for address, p := range s.pictures {
		if p.failed {
			delete(s.pictures, address)
		}
	}
	s.deps.Go(func() func() {
		page, err := api.Entries(context.Background(), q)
		return func() {
			if gen != s.gen || run != s.run {
				return
			}
			s.loading = false
			if s.expired(err) {
				return
			}
			if err != nil {
				s.loadErr, s.announce = true, s.text.Error
				return
			}
			s.entries, s.next, s.polledAt = page.Entries, page.Next, s.deps.Now()
		}
	})
}

func (s *Screen) loadMore() {
	if s.api == nil || s.next == "" || s.loading || s.more || s.moreErr {
		return
	}
	s.more = true
	run, gen, api, q := s.run, s.gen, s.api, s.query1(s.next)
	s.deps.Go(func() func() {
		page, err := api.Entries(context.Background(), q)
		return func() {
			if gen != s.gen || run != s.run {
				return
			}
			s.more = false
			if s.expired(err) {
				return
			}
			if err != nil {
				s.moreErr, s.announce = true, s.text.LoadMoreFailed
				return
			}
			s.entries, s.next = append(s.entries, page.Entries...), page.Next
			if s.next == "" {
				s.announce = s.text.AllLoaded
			}
		}
	})
}

// poll checks the sources and the first page for new entries every pollEvery, without moving the list.
func (s *Screen) poll(c *ui.Context) {
	if s.api == nil || s.loading || s.loadErr || s.polling || s.searched != "" {
		return
	}
	now := s.deps.Now()
	if wait := pollEvery - now.Sub(s.polledAt); wait > 0 {
		c.After(wait)
		return
	}
	s.polling, s.polledAt = true, now
	run, gen, api, q := s.run, s.gen, s.api, s.query1("")
	s.deps.Go(func() func() {
		sources, sourcesErr := api.Sources(context.Background())
		page, err := api.Entries(context.Background(), q)
		return func() {
			if gen != s.gen {
				return
			}
			s.polling = false
			if s.expired(sourcesErr) || s.expired(err) {
				return
			}
			if sourcesErr == nil {
				s.sources, s.sourcesLoaded = sources, true
			}
			if err != nil || run != s.run {
				return
			}
			if len(s.entries) == 0 {
				s.entries, s.next = page.Entries, page.Next
				return
			}
			top, n := s.entries[0], 0
			for _, e := range page.Entries {
				if e.CreatedAt > top.CreatedAt || e.CreatedAt == top.CreatedAt && e.ID > top.ID {
					n++
				}
			}
			if n > 0 && n != s.fresh {
				s.announce = s.format(s.text.NewEntries, n)
			}
			s.fresh = n
		}
	})
}

func (s *Screen) choose(next scope) {
	s.back()
	if next == s.scope {
		return
	}
	s.scope = next
	s.tab = max(0, indexOf(kinds, next.kind))
	s.reload()
}

func indexOf(list []string, v string) int {
	for i, x := range list {
		if x == v {
			return i
		}
	}
	return -1
}

// trimmed is the query as searched, and whether it is too short to send.
func (s *Screen) trimmed() (string, bool) {
	q := strings.TrimSpace(s.query)
	return q, q != "" && utf8.RuneCountInString(q) < minQuery
}

// search applies the query typed, unless it is too short.
func (s *Screen) search() {
	s.editing = false
	q, short := s.trimmed()
	if short || q == s.searched {
		return
	}
	s.searched = q
	s.reload()
}

func (s *Screen) index(id int64) int {
	for i, e := range s.entries {
		if e.ID == id {
			return i
		}
	}
	return -1
}

// count moves the unread count of the source of e by delta.
func (s *Screen) count(e client.Entry, delta int) {
	for i := range s.sources {
		if s.sources[i].ID == e.SourceID {
			s.sources[i].UnreadCount = max(0, s.sources[i].UnreadCount+delta)
		}
	}
}

// change sets the fields of patch on the entry at once, and back if the server does not take them.
func (s *Screen) change(id int64, patch client.EntryPatch) {
	i := s.index(id)
	if i < 0 || s.api == nil {
		return
	}
	before := s.entries[i]
	s.set(i, patch)
	back := client.EntryPatch{}
	if patch.IsRead != nil {
		back.IsRead = &before.IsRead
	}
	if patch.Starred != nil {
		back.Starred = &before.Starred
	}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		_, err := api.SetEntry(context.Background(), id, patch)
		return func() {
			if gen != s.gen || err == nil || s.expired(err) {
				return
			}
			if i := s.index(id); i >= 0 {
				s.set(i, back)
			}
			s.toast = s.text.ActionFailed
		}
	})
}

func (s *Screen) set(i int, patch client.EntryPatch) {
	e := &s.entries[i]
	if patch.IsRead != nil && *patch.IsRead != e.IsRead {
		e.IsRead = *patch.IsRead
		if e.IsRead {
			s.count(*e, -1)
		} else {
			s.count(*e, 1)
		}
	}
	if patch.Starred != nil {
		e.Starred = *patch.Starred
	}
}

func (s *Screen) toggleStar(i int) {
	if i < 0 || i >= len(s.entries) {
		return
	}
	on := !s.entries[i].Starred
	s.change(s.entries[i].ID, client.EntryPatch{Starred: &on})
	s.announce = s.text.Unstarring
	if on {
		s.announce = s.text.Starring
	}
}

func (s *Screen) toggleRead(i int) {
	if i < 0 || i >= len(s.entries) {
		return
	}
	on := !s.entries[i].IsRead
	s.change(s.entries[i].ID, client.EntryPatch{IsRead: &on})
	s.announce = s.text.MarkUnread
	if on {
		s.announce = s.text.MarkRead
	}
}

// open opens the entry and marks it read: an article in the reader, the other kinds in the browser.
func (s *Screen) open(c *ui.Context, i int) {
	if i < 0 || i >= len(s.entries) {
		return
	}
	if s.entries[i].Kind == "article" {
		s.read(i)
		return
	}
	s.browse(c, i)
}

// browse opens the entry in the browser and marks it read.
func (s *Screen) browse(c *ui.Context, i int) {
	if i < 0 || i >= len(s.entries) {
		return
	}
	e := s.entries[i]
	s.selected = i
	if e.URL != "" {
		c.OpenURLThen(e.URL, func(err error) {
			if err != nil {
				s.toast = s.text.OpenFailed
				c.Invalidate()
			}
		})
	}
	s.markRead(e)
}

func (s *Screen) markRead(e client.Entry) {
	if !e.IsRead {
		on := true
		s.change(e.ID, client.EntryPatch{IsRead: &on})
	}
}

// read shows the article at i in the reader and marks it read; the reader's toggle marks it unread again.
func (s *Screen) read(i int) {
	e := s.entries[i]
	s.selected, s.reading = i, e.ID
	s.reader.Open(e)
	s.markRead(e)
	if i >= len(s.entries)-nearEnd {
		s.loadMore()
	}
}

// article returns the article by entries from the one read, -1 when there is none loaded.
func (s *Screen) article(by int) int {
	for i := s.index(s.reading) + by; i >= 0 && i < len(s.entries); i += by {
		if s.entries[i].Kind == "article" {
			return i
		}
	}
	return -1
}

// step reads the article by entries from the one read, asking for the next page when there is none loaded after it.
func (s *Screen) step(by int) {
	if i := s.article(by); i >= 0 {
		s.read(i)
	} else if by > 0 {
		s.loadMore()
	}
}

// back closes the reader, giving the entry read the focus in the list.
func (s *Screen) back() {
	if s.reading == 0 {
		return
	}
	i := s.index(s.reading)
	s.reading, s.seeking = 0, false
	if i < 0 {
		return
	}
	s.selected, s.focusRow = i, i
	s.list.ScrollIntoView(i)
	s.grid.ScrollIntoView(i)
}

// move chooses the entry by entries from the one chosen and gives it the focus.
func (s *Screen) move(by int) {
	if len(s.entries) == 0 {
		return
	}
	s.seeking = false
	i := s.selected + by
	if s.selected < 0 {
		i = 0
	}
	i = min(max(i, 0), len(s.entries)-1)
	s.selected, s.focusRow = i, i
	s.list.ScrollIntoView(i)
	s.grid.ScrollIntoView(i)
}

// unreadIn returns the unread entries of the scope, as the sources count them.
func (s *Screen) unreadIn(sc scope) int {
	n := 0
	for _, src := range s.sources {
		if sc.source == src.ID || sc.source == 0 && (sc.kind == "" || sc.kind == src.Kind) {
			n += src.UnreadCount
		}
	}
	return n
}

func (s *Screen) scopeName() string {
	if s.scope.source != 0 {
		for _, src := range s.sources {
			if src.ID == s.scope.source {
				return subscriptions.Name(src)
			}
		}
	}
	return s.kindName(s.scope.kind)
}

func (s *Screen) kindName(kind string) string {
	switch kind {
	case "article":
		return s.text.Article
	case "video":
		return s.text.Video
	case "social":
		return s.text.Social
	case "image":
		return s.text.Image
	}
	return s.text.All
}

func (s *Screen) canMarkAll() bool {
	return s.filter != starred && s.searched == "" && len(s.entries) > 0 && s.unreadIn(s.scope) > 0
}

// markAll marks the entries of the scope read, up to the newest loaded, and offers to undo it.
func (s *Screen) markAll() {
	if !s.canMarkAll() || s.api == nil {
		return
	}
	scope := client.ReadScope{SourceID: s.scope.source, UpTo: s.entries[0].ID}
	if s.scope.source == 0 {
		scope.Kind = s.scope.kind
	}
	name, gen, api := s.scopeName(), s.gen, s.api
	s.deps.Go(func() func() {
		ids, err := api.MarkRead(context.Background(), scope)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			if err != nil {
				s.toast = s.text.ActionFailed
				return
			}
			s.setRead(ids, true)
			s.loadSources()
			text := strings.ReplaceAll(s.format(s.text.Marked, len(ids)), "{scope}", name)
			s.undoable = &marking{ids: ids, text: text}
		}
	})
}

// undo marks the entries of m unread again.
func (s *Screen) undo(m *marking) {
	if s.undoable == m {
		s.undoable = nil
	}
	if s.api == nil || len(m.ids) == 0 {
		return
	}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		_, err := api.MarkUnread(context.Background(), m.ids)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			if err != nil {
				s.toast = s.text.ActionFailed
				return
			}
			s.setRead(m.ids, false)
			s.loadSources()
		}
	})
}

// setRead shows the loaded entries among ids read or unread; the sources load again for the counts.
func (s *Screen) setRead(ids []int64, read bool) {
	for i := range s.entries {
		if slices.Contains(ids, s.entries[i].ID) {
			s.entries[i].IsRead = read
		}
	}
}

// unreadBelow returns the first unread entry after i, only articles when articles, -1 when none is loaded.
func (s *Screen) unreadBelow(i int, articles bool) int {
	for i++; i < len(s.entries); i++ {
		if e := s.entries[i]; !e.IsRead && (!articles || e.Kind == "article") {
			return i
		}
	}
	return -1
}

// nextUnread goes to the next unread entry: in the reader the next unread article, in the list the next unread entry.
// When none is loaded it loads the next page and looks again, and says so when there is none.
func (s *Screen) nextUnread() {
	if s.loading {
		return
	}
	from := s.selected
	if s.reading != 0 {
		from = s.index(s.reading)
	}
	if i := s.unreadBelow(from, s.reading != 0); i >= 0 {
		s.seeking = false
		if s.reading != 0 {
			s.read(i)
			return
		}
		s.selected, s.focusRow = i, i
		s.list.ScrollIntoView(i)
		s.grid.ScrollIntoView(i)
		return
	}
	if s.next != "" && !s.moreErr {
		s.seeking = true
		s.loadMore()
		return
	}
	s.seeking = false
	if !s.moreErr {
		s.noMore = true
	}
}

func (s *Screen) format(text string, n int) string {
	return strings.ReplaceAll(text, "{n}", strconv.Itoa(n))
}

// when is the time of an entry as the list shows it: how long ago within a week, else the date.
func (s *Screen) when(e client.Entry, now time.Time) string {
	ms := e.PublishedAt
	if ms == 0 {
		ms = e.CreatedAt
	}
	at := time.UnixMilli(ms).In(now.Location())
	ago := now.Sub(at)
	day := func(t time.Time) time.Time { y, m, d := t.Date(); return time.Date(y, m, d, 12, 0, 0, 0, time.UTC) }
	days := int(day(now).Sub(day(at)).Hours() / 24)
	switch {
	case ago < time.Minute:
		return s.text.Now
	case ago < time.Hour:
		return s.format(s.text.Minutes, int(ago/time.Minute))
	case days == 0:
		return s.format(s.text.Hours, int(ago/time.Hour))
	case days == 1:
		return s.text.Yesterday
	case days < 7:
		return s.format(s.text.Days, days)
	case at.Year() == now.Year():
		return at.Format(s.text.DateLayout)
	}
	return at.Format(s.text.YearLayout)
}

// title is what names an entry: its title, or the start of its text for a post without one.
func title(e client.Entry) string {
	if t := strings.TrimSpace(e.Title); t != "" {
		return t
	}
	text := strings.Join(strings.Fields(e.Summary), " ")
	if utf8.RuneCountInString(text) > 80 {
		text = string([]rune(text)[:80]) + "…"
	}
	return text
}

// label is how assistive technology reads an entry: source, title, time, read or not, starred.
func (s *Screen) label(e client.Entry, now time.Time) string {
	parts := []string{e.SourceTitle, title(e), s.when(e, now), s.text.Unread}
	if e.IsRead {
		parts[3] = s.text.Read
	}
	if e.Starred {
		parts = append(parts, s.text.Starred)
	}
	return strings.Join(parts, s.text.Separator)
}

func duration(sec int) string {
	if sec >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", sec/3600, sec/60%60, sec%60)
	}
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

// bitmap returns the decoded picture at address, asking for it if it has not been.
func (s *Screen) bitmap(address string) *ui.Bitmap {
	if address == "" || s.api == nil {
		return nil
	}
	p := s.pictures[address]
	if p == nil {
		p = &picture{}
		s.pictures[address] = p
		s.wanted = append(s.wanted, address)
	}
	s.drawn++
	p.used = s.drawn
	return p.bitmap
}

// download starts the pictures asked for, up to maxDownloads at a time, the latest asked first.
func (s *Screen) download() {
	for s.downloads < maxDownloads && len(s.wanted) > 0 {
		address := s.wanted[len(s.wanted)-1]
		s.wanted = s.wanted[:len(s.wanted)-1]
		if p := s.pictures[address]; p == nil || p.bitmap != nil || p.failed {
			continue
		}
		s.downloads++
		gen, api, decode := s.gen, s.api, s.deps.Decode
		s.deps.Go(func() func() {
			data, err := api.Image(context.Background(), address)
			var bitmap *ui.Bitmap
			if err == nil {
				bitmap, err = decode(data)
			}
			return func() {
				if gen != s.gen {
					return
				}
				s.downloads--
				if p := s.pictures[address]; p != nil {
					p.bitmap, p.failed = bitmap, err != nil
				}
				s.forget()
			}
		})
	}
}

// forget drops the least recently drawn pictures beyond keepPictures.
func (s *Screen) forget() {
	for len(s.pictures) > keepPictures {
		oldest, at := "", 0
		for address, p := range s.pictures {
			if (p.bitmap != nil || p.failed) && (oldest == "" || p.used < at) {
				oldest, at = address, p.used
			}
		}
		if oldest == "" {
			return
		}
		delete(s.pictures, oldest)
	}
}

// px converts a size of the mockup, drawn at a 13 px font, to the theme's text size.
func px(t *ui.Theme, v float32) float32 { return t.Rem(v / 13) }

func kindIcon(kind string) *ui.SVG {
	switch kind {
	case "article":
		return icons.Newspaper
	case "video":
		return icons.Play
	case "social":
		return icons.MessageCircle
	case "image":
		return icons.Image
	}
	return icons.Inbox
}

// View draws the timeline; it does no disk or network work, which runs through Deps.
func (s *Screen) View(c *ui.Context) {
	t := c.Theme()
	now := s.deps.Now()
	if s.editing {
		if wait := debounce - now.Sub(s.editedAt); wait > 0 {
			c.After(wait)
		} else {
			s.search()
		}
	}
	s.poll(c)
	if s.reading != 0 && s.index(s.reading) < 0 {
		s.reading = 0
	}
	if s.reading != 0 {
		s.typing = false
	}
	if s.seeking && !s.more {
		s.nextUnread()
	}
	open := s.keys || s.subs.Open()
	if !open {
		c.OnShortcut(ui.Cmd, ui.KeyF, func() {
			s.back()
			s.searchBox = true
		})
		c.OnShortcut(ui.Cmd, ui.KeyN, s.subs.Add)
		c.OnShortcut(ui.Cmd, ui.KeySlash, s.ShowKeys)
		if m := s.undoable; m != nil && !s.typing {
			c.OnShortcut(ui.Cmd, ui.KeyZ, func() {
				c.CloseToast(markedToast)
				s.undo(m)
			})
		}
	}
	if !open && !s.typing {
		c.OnShortcut(ui.Shift, ui.KeySlash, s.ShowKeys)
	}
	// The keys of the list work wherever the focus is, but not while the search takes the letters typed, a dialog
	// shows or the reader, which has its own, is open.
	if !s.typing && !open && s.reading == 0 {
		for _, k := range []struct {
			key ui.Key
			do  func()
		}{
			{ui.KeyJ, func() { s.move(1) }}, {ui.KeyDown, func() { s.move(1) }},
			{ui.KeyK, func() { s.move(-1) }}, {ui.KeyUp, func() { s.move(-1) }},
			{ui.KeyO, func() { s.open(c, s.selected) }}, {ui.KeyEnter, func() { s.open(c, s.selected) }},
			{ui.KeyS, func() { s.toggleStar(s.selected) }}, {ui.KeyM, func() { s.toggleRead(s.selected) }},
			{ui.KeyN, s.nextUnread}, {ui.KeySlash, func() { s.searchBox = true }},
		} {
			c.OnShortcut(0, k.key, k.do)
		}
	}
	width, _ := c.Size()
	compact := width < 760
	s.wantMore = false
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(t.Background).Children(func() {
		s.sidebar(c, compact)
		if s.reading != 0 {
			e := s.entries[s.index(s.reading)]
			s.reader.View(c, reader.Nav{Entry: e, Icon: s.bitmap(s.iconOf(e.SourceID)), Scope: s.scopeName(),
				Previous: s.article(-1) >= 0, Next: s.article(1) >= 0 || s.next != "", Keys: !open})
			return
		}
		// The toolbar row floats over the content, which scrolls under it and fades under the scroll edge.
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			s.hint(c)
			s.content(c, now, compact)
			theme.Edge(c)
			s.toolbar(c, compact)
			s.pill(c)
		})
	})
	if s.wantMore {
		s.loadMore()
	}
	s.keySheet(c)
	s.subs.View(c)
	s.download()
	if s.announce != "" {
		c.Announce(s.announce)
		s.announce = ""
	}
	if s.toast != "" {
		c.Toast(s.toast)
		s.toast = ""
	}
	s.toasts(c)
}

// toasts shows the toasts of "mark all as read", with its undo, and of N finding no unread entry.
func (s *Screen) toasts(c *ui.Context) {
	if m := s.undoable; m != nil && !m.shown {
		m.shown = true
		c.AddToast(ui.Toast{ID: markedToast, Title: m.text, Timeout: 8 * time.Second, Action: s.text.Undo,
			OnAction: func() { s.undo(m) },
			OnClose: func() {
				if s.undoable == m {
					s.undoable = nil
				}
			},
			Data: theme.ToastLook{Icon: icons.CheckCheck, Keys: modifier() + "Z", Close: s.text.Dismiss}})
	}
	if s.noMore {
		s.noMore = false
		c.AddToast(ui.Toast{ID: noMoreToast, Title: s.text.NoMoreUnread,
			Data: theme.ToastLook{Icon: icons.CircleCheckBig}})
	}
}

// modifier is how the key sheet and the toasts write the Cmd of ui.Cmd: ⌘ on macOS, Ctrl+ elsewhere.
func modifier() string {
	if runtime.GOOS == "darwin" {
		return "⌘"
	}
	return "Ctrl+"
}

// ShowKeys shows the key sheet, from the menu or ?.
func (s *Screen) ShowKeys() {
	if s.subs.Open() {
		return
	}
	s.keys = true
}

// MenuLabels are the Help menu and its item that shows the key sheet, in the language of the system locale.
func MenuLabels(systemLocale string) (help, keys string) {
	text := translations[locale.Language(systemLocale)]
	return text.HelpMenu, text.KeysMenu
}

// key is a row of the key sheet: what it does, and the keys that do it, each a list pressed together.
type key struct {
	name string
	keys [][]string
}

// keySheet is the dialog listing the keys of the timeline, the reader and the whole window.
func (s *Screen) keySheet(c *ui.Context) {
	if !s.keys {
		return
	}
	t := c.Theme()
	width, height := c.Size()
	cmd := modifier()
	one := func(k ...string) [][]string {
		out := make([][]string, len(k))
		for i, v := range k {
			out[i] = []string{v}
		}
		return out
	}
	timeline := []key{{s.text.KeyNext, one("J", "↓")}, {s.text.KeyPrevious, one("K", "↑")},
		{s.text.KeyNextUnread, one("N")}, {s.text.KeyOpen, one("O", "↩")}, {s.text.KeyStar, one("S")},
		{s.text.KeyRead, one("M")}, {s.text.KeySearch, [][]string{{"/"}, {cmd, "F"}}},
		{s.text.KeyEscape, one("Esc")}, {s.text.KeyUndo, [][]string{{cmd, "Z"}}}}
	reading := []key{{s.text.KeyNextArticle, one("J")}, {s.text.KeyPreviousArticle, one("K")},
		{s.text.KeyNextUnread, one("N")}, {s.text.KeyStar, one("S")}, {s.text.KeyRead, one("M")},
		{s.text.KeyOpenBrowser, one("O")}, {s.text.KeyBack, one("Esc")}}
	anywhere := []key{{s.text.KeyAdd, [][]string{{cmd, "N"}}}, {s.text.KeySettings, [][]string{{cmd, ","}}},
		{s.text.KeyKeys, one("?")}}
	ui.DialogBase(c, &s.keys, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.28)).Padding(t.Space(4))
		panel.Column().Width(min(px(t, 560), width-t.Space(8))).MaxHeight(height-t.Space(8)).Radius(px(t, 14)).
			Background(theme.RaisedFill(t)).Border(0.5, theme.LineStrong(t)).Shadow(0, 16, 40, 0, ui.RGBA(0, 0, 0, 0.25)).
			Label(s.text.KeysTitle)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Padding(t.Space(4), t.Space(4), t.Space(2), t.Space(5)).
			Children(func() {
				ui.Text(c, s.text.KeysTitle).Grow(1).FontSize(px(t, 15)).FontWeight(600).Role(ui.RoleHeading).
					Level(2)
				x := ui.ButtonBase(c).Size(px(t, 28), px(t, 28)).Radius(t.Space(1.5)).Label(s.text.Close).
					Tooltip(s.text.Close)
				color := t.TextMuted
				if x.Hovered() {
					x.Background(t.SurfaceHover)
					color = t.Text
				}
				x.Children(func() { ui.Icon(c, icons.X).FontSize(px(t, 16)).TextColor(color) })
				if x.Clicked() {
					s.keys = false
				}
			})
		ui.Scroll(c).FillWidth().Shrink(1).MinHeight(0).Children(func() {
			cols := ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(px(t, 24)).Padding(px(t, 4), t.Space(5), t.Space(5))
			if width < 760 {
				cols.Column().Gap(0)
			}
			cols.Children(func() {
				ui.Column(c).Grow(1).Basis(0).FillWidth().Children(func() {
					s.keyGroup(c, s.text.InTimeline, timeline)
				})
				ui.Column(c).Grow(1).Basis(0).FillWidth().Children(func() {
					s.keyGroup(c, s.text.InReader, reading)
					s.keyGroup(c, s.text.Anywhere, anywhere)
				})
			})
		})
	})
}

func (s *Screen) keyGroup(c *ui.Context, name string, rows []key) {
	t := c.Theme()
	ui.Text(c, strings.ToUpper(name)).FontSize(px(t, 11)).FontWeight(600).TextColor(t.TextMuted).
		Margin(px(t, 12), 0, px(t, 4), 0).Role(ui.RoleHeading).Level(3)
	for _, row := range rows {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).PaddingY(px(t, 5)).Children(func() {
			ui.Text(c, row.name).Grow(1).MinWidth(0).FontSize(px(t, 12.5))
			ui.Row(c).AlignItems(ui.Center).Gap(px(t, 4)).Children(func() {
				for i, keys := range row.keys {
					if i > 0 {
						ui.Text(c, s.text.Or).FontSize(px(t, 11)).TextColor(t.TextMuted)
					}
					for _, k := range keys {
						ui.Box(c).MinWidth(px(t, 22)).Height(px(t, 22)).PaddingX(px(t, 6)).Center().Radius(6).
							Background(theme.Track(t)).Border(0.5, theme.LineStrong(t)).Children(func() {
							ui.Text(c, k).FontSize(px(t, 11.5)).NoWrap()
						})
					}
				}
			})
		})
	}
}

// sideWidth is the width of the sidebar, in units of the main theme.
func sideWidth(t *ui.Theme, compact bool) float32 {
	if compact {
		return px(t, 184)
	}
	return px(t, 216)
}

// sidebar is the ink pane. It is drawn with the pane's own theme, set before its elements are created and put back
// after, so every role in it (text, hover and chosen fills, the accent) is the pane's.
func (s *Screen) sidebar(c *ui.Context, compact bool) {
	main := c.Theme()
	pane := ui.Column(c).Width(sideWidth(main, compact)).Margin(8, 0, 8, 8)
	theme.Pane(c, pane)
	c.SetTheme(theme.PaneTheme(main))
	defer c.SetTheme(main)
	pane.Children(func() {
		t := c.Theme()
		// The first 40 are the window buttons' and drag the window.
		ui.Box(c).FillWidth().Height(40).DragWindow()
		s.tabs(c)
		list := s.sources
		if s.scope.kind != "" {
			list = nil
			for _, src := range s.sources {
				if src.Kind == s.scope.kind {
					list = append(list, src)
				}
			}
		}
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(px(t, 4), px(t, 8), px(t, 6)).Label(s.text.Sources).Children(func() {
				s.section(c)
				for _, src := range list {
					s.sourceItem(c, src)
				}
			})
		})
		s.serverBar(c)
	})
}

// tabs are the kinds at the top of the sidebar, a list of an icon, a name and the unread count each; the arrow keys
// move between them. Names are Text2, the chosen one Text in weight 500; counts are TextMuted and step to Text2 under
// the pointer and when chosen; the icon is TextMuted, Text2 under the pointer and the Accent when chosen.
func (s *Screen) tabs(c *ui.Context) {
	t := c.Theme()
	before := s.tab
	seg := ui.SegmentedBase(c, &s.tab, len(kinds))
	seg.Track.Column().FillWidth().Gap(px(t, 1)).Padding(0, px(t, 8), px(t, 8)).Label(s.text.Kinds).
		Children(func() {
			for i, kind := range kinds {
				name := s.kindName(kind)
				on := i == s.tab
				tab := seg.Segment(i).FillWidth().Height(px(t, 30)).Gap(px(t, 10)).PaddingX(px(t, 10)).
					Radius(px(t, 8)).Label(name)
				icon, count := t.TextMuted, t.TextMuted
				switch {
				case on:
					tab.Background(theme.Chosen(t))
					icon, count = t.Accent, theme.Text2(t)
				case tab.Hovered():
					tab.Background(t.SurfaceHover)
					icon, count = theme.Text2(t), theme.Text2(t)
				}
				theme.Fade(tab)
				n := s.unreadIn(scope{kind: kind})
				if n > 0 {
					tab.Description(s.format(s.text.UnreadCount, n))
				}
				tab.Children(func() {
					ui.Icon(c, kindIcon(kind)).FontSize(px(t, 16)).TextColor(icon)
					label := ui.Text(c, name).Grow(1).MinWidth(0).SingleLine().TextColor(theme.Text2(t))
					if on {
						label.TextColor(t.Text).FontWeight(500)
					}
					if n > 0 {
						ui.Text(c, strconv.Itoa(n)).FontSize(px(t, 11)).FontWeight(500).FontFeatures("tnum").
							TextColor(count)
					}
				})
				if tab.Clicked() {
					s.tab = i
					s.back()
				}
			}
		})
	if s.tab != before || seg.Track.Changed() {
		s.choose(scope{kind: kinds[s.tab]})
	}
}

// section is the header of the subscriptions in the sidebar, with the button adding one.
func (s *Screen) section(c *ui.Context) {
	t := c.Theme()
	name := s.text.Sources
	if s.scope.kind != "" {
		name = s.kindName(s.scope.kind)
	}
	size, tracking := px(t, 11), px(t, 11)*0.04
	if theme.Han(name) {
		size, tracking = px(t, 12), px(t, 12)*0.02
	}
	ui.Row(c).FillWidth().Height(px(t, 22)).AlignItems(ui.Center).Margin(px(t, 14), 0, px(t, 4), px(t, 10)).
		Children(func() {
			ui.Text(c, name).Grow(1).MinWidth(0).SingleLine().FontSize(size).FontWeight(500).
				LetterSpacing(tracking).TextColor(t.TextMuted)
			b := ui.ButtonBase(c).Size(px(t, 22), px(t, 22)).Radius(px(t, 8)).Label(s.text.AddSource).
				Tooltip(s.text.AddSource)
			color := t.TextMuted
			if b.Hovered() {
				b.Background(t.SurfaceHover)
				color = t.Text
			}
			b.Children(func() { ui.Icon(c, icons.Plus).FontSize(px(t, 14)).TextColor(color) })
			if b.Clicked() {
				s.subs.Add()
			}
		})
}

func (s *Screen) sourceItem(c *ui.Context, src client.Source) {
	t := c.Theme()
	chosen := s.scope.source == src.ID
	name := subscriptions.Name(src)
	item := ui.ButtonBase(c).Key(src.ID).FillWidth().Height(px(t, 30)).Gap(px(t, 10)).PaddingX(px(t, 10)).
		Radius(px(t, 8)).Label(name).ContextMenu(func(m *ui.Menu) { s.subs.Menu(m, src) })
	count := t.TextMuted
	switch {
	case chosen:
		item.Background(theme.Chosen(t))
		count = theme.Text2(t)
	case item.Pressed():
		item.Background(t.SurfacePressed)
		count = theme.Text2(t)
	case item.Hovered():
		item.Background(t.SurfaceHover)
		count = theme.Text2(t)
	}
	theme.Fade(item)
	item.Children(func() {
		s.glyph(c, name, src.IconURL, 18)
		color := theme.Text2(t)
		if src.Paused {
			color = t.TextMuted
		}
		label := ui.Text(c, name).Grow(1).MinWidth(0).SingleLine().TextColor(color)
		if chosen {
			label.TextColor(t.Text).FontWeight(500)
		}
		if src.Paused {
			ui.Box(c).Label(s.text.PausedMark).Tooltip(s.text.PausedMark).Role(ui.RoleImage).Children(func() {
				ui.Icon(c, icons.Pause).FontSize(px(t, 14)).TextColor(t.TextMuted)
			})
		} else if src.LastError != "" {
			ui.Box(c).Label(s.text.Failing).Tooltip(s.text.Failing).Role(ui.RoleImage).Children(func() {
				ui.Icon(c, icons.CircleAlert).FontSize(px(t, 14)).TextColor(t.Warning)
			})
		}
		if src.UnreadCount > 0 && !src.Paused {
			ui.Text(c, strconv.Itoa(src.UnreadCount)).FontSize(px(t, 11)).FontWeight(500).FontFeatures("tnum").
				TextColor(count)
		}
	})
	if item.Clicked() {
		s.choose(scope{kind: src.Kind, source: src.ID})
	}
}

// glyph is a subscription's site icon, or its monogram while the icon is not decoded and when it has none: a square
// of size, 18 in the pane and 15 in the meta line, with a 0.5 DIP inset rim in ink at 20% so a dark icon still reads.
func (s *Screen) glyph(c *ui.Context, name, icon string, size float32) {
	t := c.Theme()
	bitmap := s.bitmap(icon)
	if bitmap == nil {
		monogram(c, name, size)
		return
	}
	radius := radiusOf(size)
	ui.Box(c).Size(px(t, size), px(t, size)).Radius(radius).Clip().Children(func() {
		ui.Image(c, bitmap).Fit(ui.Cover).Fill()
		ui.Box(c).Absolute().Fill().Radius(radius).Border(0.5, theme.Ink(t, 0.2))
	})
}

// radiusOf is the corner radius of a glyph of size: 5 for the pane's, 4 for the meta line's.
func radiusOf(size float32) float32 {
	if size > 16 {
		return 5
	}
	return 4
}

// iconOf returns the site icon of the subscription id, "" when it has none.
func (s *Screen) iconOf(id int64) string {
	for _, src := range s.sources {
		if src.ID == id {
			return src.IconURL
		}
	}
	return ""
}

// monogram is the first letter of a subscription on a Track tile, in Text2, whatever its kind.
func monogram(c *ui.Context, name string, size float32) {
	t := c.Theme()
	letter := "·"
	if r, _ := utf8.DecodeRuneInString(strings.TrimLeft(name, "@#· ")); r != utf8.RuneError {
		letter = strings.ToUpper(string(r))
	}
	font := px(t, 10.5)
	if size <= 16 {
		font = px(t, 9)
	}
	ui.Box(c).Size(px(t, size), px(t, size)).Radius(radiusOf(size)).Center().Background(theme.Track(t)).Children(func() {
		ui.Text(c, letter).FontSize(font).FontWeight(600).TextColor(theme.Text2(t))
	})
}

func (s *Screen) serverBar(c *ui.Context) {
	t := c.Theme()
	name, icon := s.text.ThisComputer, icons.Laptop
	if s.server != "" {
		name, icon = s.server, icons.Server
	}
	// Inset like the rows above it, so its hover fill is a rounded row inside the pane, clear of its corner.
	ui.Row(c).FillWidth().Height(px(t, 40)).AlignItems(ui.Center).Padding(0, px(t, 6), 0, px(t, 8)).Children(func() {
		bar := ui.ButtonBase(c).Grow(1).MinWidth(0).Height(px(t, 30)).Gap(px(t, 8)).PaddingX(px(t, 10)).
			Radius(px(t, 8)).Label(strings.ReplaceAll(s.text.Server, "{server}", name))
		if bar.Hovered() {
			bar.Background(t.SurfaceHover)
		}
		theme.Fade(bar)
		bar.Children(func() {
			ui.Icon(c, icon).FontSize(px(t, 16)).TextColor(t.TextMuted)
			ui.Text(c, name).Grow(1).MinWidth(0).SingleLine().FontSize(px(t, 12)).LetterSpacing(px(t, 12) * 0.01).
				TextColor(t.TextMuted)
			ui.Box(c).Size(6, 6).Radius(3).Background(t.Success)
		})
		if bar.Clicked() {
			s.deps.OpenConnect()
		}
		gear := ui.ButtonBase(c).Size(px(t, 28), px(t, 28)).Radius(px(t, 8)).Label(s.text.Settings).
			Tooltip(s.text.Settings)
		color := t.TextMuted
		if gear.Hovered() {
			gear.Background(t.SurfaceHover)
			color = t.Text
		}
		gear.Children(func() { ui.Icon(c, icons.Settings).FontSize(px(t, 16)).TextColor(color) })
		if gear.Clicked() {
			s.deps.OpenSettings()
		}
	})
}

func (s *Screen) toolbar(c *ui.Context, compact bool) {
	t := c.Theme()
	bar := c.TitleBar()
	// Keyed, so the search field keeps its focus when the hint above it in the column comes and goes.
	ui.Row(c).Key("toolbar").Absolute().Top(0).Left(0).Right(0).Height(theme.Bar).AlignItems(ui.Center).Gap(px(t, 10)).
		Padding(0, t.Space(3)+bar.Right, 0, t.Space(5)).DragWindow().Children(func() {
		ui.Text(c, s.scopeName()).FontSize(px(t, 15)).FontWeight(600).SingleLine().MinWidth(0).Shrink(1).
			Role(ui.RoleHeading).Level(1)
		sub := ""
		switch {
		case s.searched != "" && !s.loading && !s.loadErr:
			sub = s.format(s.text.Results, len(s.entries))
			if s.next != "" {
				sub = s.format(s.text.MoreResults, len(s.entries))
			}
		case s.searched == "" && s.sourcesLoaded:
			if n := s.unreadIn(s.scope); n > 0 {
				sub = s.format(s.text.UnreadCount, n)
			}
		}
		if sub != "" {
			ui.Text(c, sub).FontSize(px(t, 12)).FontFeatures("tnum").TextColor(t.TextMuted).NoWrap()
		}
		s.editButton(c)
		ui.Box(c).Grow(1)
		s.filters(c, compact)
		s.searchField(c, compact)
		s.markAllButton(c)
	})
}

func (s *Screen) filters(c *ui.Context, compact bool) {
	t := c.Theme()
	before := s.filter
	seg := ui.SegmentedBase(c, &s.filter, 3)
	theme.Capsule(c, seg.Track.Height(px(t, 32)).Padding(px(t, 3)).Gap(2).Radius(px(t, 16)), false).Label(s.text.Show).
		Children(func() {
			for i, item := range []struct {
				icon *ui.SVG
				name string
			}{{icons.Inbox, s.text.Everything}, {icons.CircleDot, s.text.Unread}, {icons.Star, s.text.Starred}} {
				on := i == s.filter
				b := seg.Segment(i).Height(px(t, 26)).Gap(px(t, 4)).PaddingX(px(t, 11)).Radius(px(t, 13)).
					Label(item.name).Tooltip(item.name)
				color := t.TextMuted
				switch {
				case on:
					theme.Lifted(t, b)
					color = t.Text
				case b.Hovered():
					b.Background(t.SurfaceHover)
					color = t.Text
				}
				theme.Fade(b)
				b.Children(func() {
					ui.Icon(c, item.icon).FontSize(px(t, 14)).TextColor(color)
					if !compact {
						ui.Text(c, item.name).FontSize(px(t, 12)).FontWeight(500).NoWrap().TextColor(color)
					}
				})
			}
		})
	if s.filter != before {
		s.reload()
	}
}

// searchField is a search field whose text input takes the focus from Cmd+F and /, which ui.SearchField does not
// let it: the magnifier, the input, and a button clearing it, as Escape does.
func (s *Screen) searchField(c *ui.Context, compact bool) {
	t := c.Theme()
	width := px(t, 200)
	if compact {
		width = px(t, 136)
	}
	_, short := s.trimmed()
	box := ui.Row(c).Key("search").Width(width).Height(px(t, 32)).Gap(px(t, 6)).Padding(0, px(t, 6), 0, px(t, 12)).
		Radius(px(t, 16))
	focused := s.typing
	switch {
	case c.Preferences().HighContrast:
		theme.Capsule(c, box, box.Hovered())
	case focused:
		box.Background(theme.RaisedFill(t)).Shadow(0, 0, 0, 2, t.Focus)
	case box.Hovered():
		box.Background(theme.Ink(t, 0.075))
	default:
		box.Background(theme.Ink(t, 0.055))
	}
	theme.Fade(box)
	var input ui.Element
	cleared := s.clearing
	s.clearing = false
	box.Children(func() {
		ui.Icon(c, icons.Search).FontSize(px(t, 14)).TextColor(t.TextMuted)
		input = ui.TextInputBase(c, &s.query).Grow(1).MinWidth(0).Label(s.text.SearchLabel).
			Placeholder(s.text.Search)
		if short {
			input.Error(s.text.TooShort)
		}
		if s.query != "" {
			input.OnShortcut(0, ui.KeyEscape, func() { s.clearing = true })
			clear := ui.ButtonBase(c).Size(px(t, 20), px(t, 20)).Radius(px(t, 6)).Label(s.text.Clear)
			if clear.Hovered() {
				clear.Background(t.SurfaceHover)
			}
			clear.Children(func() { ui.Icon(c, icons.X).FontSize(px(t, 12)).TextColor(t.TextMuted) })
			if clear.Clicked() {
				cleared = true
			}
		}
	})
	if box.Clicked() || s.searchBox {
		input.Focus()
		s.searchBox = false
	}
	s.typing = input.Focused()
	if short {
		box.InsetShadow(0, 0, 0, 1.5, t.Warning)
	}
	if cleared {
		s.query = ""
		input.Focus()
		s.search()
		return
	}
	if input.Changed() {
		s.editedAt, s.editing = s.deps.Now(), true
		if strings.TrimSpace(s.query) == "" {
			s.search()
		}
	}
	if input.Submitted() {
		s.search()
	}
}

// editButton edits the subscription chosen in the sidebar.
func (s *Screen) editButton(c *ui.Context) {
	i := slices.IndexFunc(s.sources, func(src client.Source) bool { return src.ID == s.scope.source })
	if s.scope.source == 0 || i < 0 {
		return
	}
	t := c.Theme()
	b := ui.ButtonBase(c).Size(px(t, 32), px(t, 32)).Radius(px(t, 8)).Label(s.text.EditSource).
		Tooltip(s.text.EditSource)
	color := toolFill(t, b)
	b.Children(func() { ui.Icon(c, icons.Pencil).FontSize(px(t, 16)).TextColor(color) })
	if b.Clicked() {
		s.subs.Edit(s.sources[i])
	}
}

// toolFill fills a bare toolbar button for its state, SurfaceHover under the pointer and SurfacePressed pressed, and
// returns the color of its icon.
func toolFill(t *ui.Theme, b ui.Element) ui.Color {
	switch {
	case b.Pressed():
		b.Background(t.SurfacePressed)
		return t.Text
	case b.Hovered():
		b.Background(t.SurfaceHover)
		return t.Text
	}
	return t.TextMuted
}

// markAllLabel names the button by what it would mark: "Mark all in “Videos” as read".
func (s *Screen) markAllLabel() string {
	return strings.ReplaceAll(s.text.MarkAllIn, "{scope}", s.scopeName())
}

func (s *Screen) markAllButton(c *ui.Context) {
	t := c.Theme()
	can := s.canMarkAll()
	label := s.markAllLabel()
	b := ui.ButtonBase(c).Size(px(t, 32), px(t, 32)).Radius(px(t, 8)).Label(label).Tooltip(label).Disabled(!can)
	color := theme.Disabled(t)
	if can {
		color = toolFill(t, b)
	}
	b.Children(func() { ui.Icon(c, icons.CheckCheck).FontSize(px(t, 16)).TextColor(color) })
	if b.Clicked() {
		s.markAll()
	}
}

func (s *Screen) hint(c *ui.Context) {
	if _, short := s.trimmed(); !short {
		return
	}
	t := c.Theme()
	// Under the toolbar row, the content starting below it.
	ui.Row(c).AlignSelf(ui.Stretch).AlignItems(ui.Center).Gap(px(t, 8)).Margin(theme.Bar+4, px(t, 12), 4).
		Padding(px(t, 7), px(t, 14)).Radius(px(t, 12)).Background(theme.Soft(t, t.Warning)).Role(ui.RoleStatus).
		Children(func() {
			ui.Icon(c, icons.CircleAlert).FontSize(px(t, 14)).TextColor(t.Warning)
			ui.Text(c, s.text.TooShort).FontSize(px(t, 12)).TextColor(theme.Text2(t))
		})
}

func (s *Screen) pill(c *ui.Context) {
	if s.fresh == 0 {
		return
	}
	t := c.Theme()
	text := s.format(s.text.NewEntries, s.fresh)
	b := ui.ButtonBase(c).Attach(ui.AnchorTop, ui.AnchorTop).Top(px(t, 60)).Height(px(t, 28)).Gap(t.Space(1.5)).
		Padding(0, t.Space(3), 0, t.Space(2.5)).Radius(px(t, 14)).Background(t.Accent).
		Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.18)).Label(text)
	if b.Hovered() {
		b.Background(t.AccentHover)
	}
	b.Children(func() {
		ui.Icon(c, icons.ArrowUp).FontSize(px(t, 14)).TextColor(t.AccentText)
		ui.Text(c, text).FontSize(px(t, 12)).FontWeight(500).TextColor(t.AccentText).NoWrap()
	})
	if b.Clicked() {
		s.reload()
	}
}

// content is the list, or the state that stands in for it.
func (s *Screen) content(c *ui.Context, now time.Time, compact bool) {
	area := ui.Column(c).Grow(1).FillWidth().MinHeight(0)
	area.Children(func() {
		switch {
		case s.api == nil:
		case s.loading:
			s.skeleton(c)
		case s.loadErr:
			s.empty(c, icons.WifiOff, s.text.Error, s.text.ErrorHint, true)
		case len(s.entries) == 0:
			switch {
			case s.sourcesLoaded && len(s.sources) == 0:
				s.empty(c, icons.Rss, s.text.NoSubscriptions, s.text.NoSubscriptionsHint, false)
			case s.searched != "":
				s.empty(c, icons.Search, strings.ReplaceAll(s.text.NoResults, "{q}", s.searched), s.text.NoResultsHint,
					false)
			case s.filter == unread:
				s.empty(c, icons.CircleCheckBig, s.text.UnreadDone, s.text.UnreadDoneHint, false)
			case s.filter == starred:
				s.empty(c, icons.Star, s.text.NoStarred, s.text.NoStarredHint, false)
			default:
				s.empty(c, icons.Inbox, s.text.Empty, s.text.EmptyHint, false)
			}
		case s.scope.kind == "video" && s.scope.source == 0 && s.searched == "":
			s.videoGrid(c, now, compact)
		case s.scope.kind == "image" && s.scope.source == 0 && s.searched == "":
			s.wallOf(c, now, compact)
		default:
			s.rows(c, now, compact)
		}
	})
	s.toTop = false
}

// top is how far down the content starts: below the toolbar row, which it scrolls under, or below the hint, which
// already sits below the row.
func (s *Screen) top() float32 {
	if _, short := s.trimmed(); short {
		return 0
	}
	return theme.Bar
}

func (s *Screen) skeleton(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().MaxWidth(px(t, 680)).Margin(0, ui.Auto).
		Padding(s.top()+t.Space(1), t.Space(5), t.Space(1)).
		Label(s.text.Loading).Role(ui.RoleProgress).Children(func() {
		for i := range 4 {
			ui.Row(c).Key(i).FillWidth().AlignItems(ui.Start).Gap(px(t, 14)).
				Padding(px(t, 16), px(t, 16), px(t, 16), px(t, 28)).Children(func() {
				ui.Column(c).Grow(1).Gap(t.Space(2)).Children(func() {
					for _, bar := range []struct{ width, height float32 }{{0.36, 10}, {0.7, 14}, {0.92, 10}} {
						ui.Box(c).Width(px(t, 420) * bar.width).Height(px(t, bar.height)).Radius(5).
							Background(theme.Track(t))
					}
				})
				if i%2 == 0 {
					ui.Box(c).Size(px(t, 96), px(t, 64)).Radius(px(t, 8)).Background(theme.Track(t)).
						Margin(px(t, 24), 0, 0, 0)
				}
			})
		}
	})
}

func (s *Screen) empty(c *ui.Context, icon *ui.SVG, title, hint string, failed bool) {
	t := c.Theme()
	ui.Column(c).Fill().Center().Gap(t.Space(3)).Padding(s.top()+t.Space(6), t.Space(6), t.Space(6)).
		Role(ui.RoleStatus).Children(func() {
		fill, color := t.Surface, t.TextMuted
		if failed {
			fill, color = theme.Soft(t, t.Danger), t.Danger
		}
		ui.Box(c).Size(px(t, 52), px(t, 52)).Radius(px(t, 16)).Center().Background(fill).
			Children(func() { ui.Icon(c, icon).FontSize(px(t, 24)).TextColor(color) })
		ui.Text(c, title).FontSize(px(t, 15)).FontWeight(600).Role(ui.RoleHeading).Level(2)
		ui.Text(c, hint).MaxWidth(px(t, 300)).TextColor(t.TextMuted).Wrap()
		if failed {
			b := ui.ButtonBase(c).Height(px(t, 32)).Gap(px(t, 6)).PaddingX(px(t, 14)).Radius(px(t, 10)).
				Background(theme.Track(t))
			if b.Pressed() {
				b.Background(theme.Track(t).Mix(theme.Ink(t, 1), 0.16))
			} else if b.Hovered() {
				b.Background(theme.Track(t).Mix(theme.Ink(t, 1), 0.1))
			}
			theme.Fade(b)
			b.Children(func() {
				ui.Icon(c, icons.RefreshCw).FontSize(px(t, 16)).TextColor(t.Text)
				ui.Text(c, s.text.Retry).NoWrap().FontWeight(500)
			})
			if b.Clicked() {
				if !s.sourcesLoaded {
					s.loadSources()
				}
				s.reload()
			}
		}
		if title == s.text.NoSubscriptions {
			b := ui.ButtonBase(c).Height(t.Space(8)).Gap(t.Space(1.5)).PaddingX(t.Space(3.5)).Radius(t.Radius).
				Background(t.Accent)
			if b.Pressed() {
				b.Background(t.AccentPressed)
			} else if b.Hovered() {
				b.Background(t.AccentHover)
			}
			b.Children(func() {
				ui.Icon(c, icons.Plus).FontSize(px(t, 16)).TextColor(t.AccentText)
				ui.Text(c, s.text.AddFirst).NoWrap().FontWeight(500).TextColor(t.AccentText)
			})
			if b.Clicked() {
				s.subs.Add()
			}
		}
	})
}

// footer is the end of the loaded entries: loading the next page, or a retry when that failed.
func (s *Screen) footer(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).FillWidth().Justify(ui.Center).AlignItems(ui.Center).Gap(t.Space(2)).Padding(t.Space(4)).
		Children(func() {
			if s.moreErr {
				ui.Icon(c, icons.CircleAlert).FontSize(px(t, 14)).TextColor(t.Danger)
				ui.Text(c, s.text.LoadMoreFailed).FontSize(px(t, 12)).TextColor(t.TextMuted)
				b := ui.ButtonBase(c).Height(px(t, 24)).Gap(t.Space(1)).PaddingX(t.Space(2)).Radius(t.Space(1.5)).
					Label(s.text.Retry)
				if b.Hovered() {
					b.Background(t.SurfaceHover)
				}
				b.Children(func() {
					ui.Icon(c, icons.RefreshCw).FontSize(px(t, 14)).TextColor(t.Accent)
					ui.Text(c, s.text.Retry).FontSize(px(t, 12)).TextColor(t.Accent)
				})
				if b.Clicked() {
					s.moreErr = false
					s.loadMore()
				}
				return
			}
			spin := ui.Icon(c, icons.LoaderCircle).FontSize(px(t, 16)).TextColor(t.TextMuted)
			spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
			ui.Text(c, s.text.LoadingMore).FontSize(px(t, 12)).TextColor(t.TextMuted)
		})
}

func (s *Screen) hasFooter() bool { return s.more || s.moreErr }

// item is the frame of an entry in any layout: it opens on a click, takes the focus as the keys choose it, and shows
// its actions while the pointer is on it or it is chosen. It rings the focus when the keyboard is on it, and a card,
// which has no fill to show the choice, when it is chosen.
func (s *Screen) item(c *ui.Context, e ui.Element, i int, now time.Time, ringChosen bool) (hovered, chosen bool) {
	t := c.Theme()
	entry := s.entries[i]
	e.Focusable().FocusRing(false).Label(s.label(entry, now))
	if s.focusRow == i {
		e.Focus().ScrollIntoView()
		s.focusRow = -1
	}
	if e.Focused() && s.selected != i {
		s.selected = i
	}
	hovered, chosen = e.Hovered(), s.selected == i
	if hovered {
		s.hovered = i
	} else if s.hovered == i {
		s.hovered = -1
	}
	if e.Focused() || chosen && ringChosen {
		e.Shadow(0, 0, 0, 2, t.Focus)
	}
	if e.Clicked() {
		s.open(c, i)
	}
	if i >= len(s.entries)-nearEnd {
		s.wantMore = true
	}
	return hovered, chosen
}

// actionFade fades the actions of a row in and out, opacity only.
var actionFade = ui.ElementTransition{Duration: 120 * time.Millisecond, Enter: &ui.Motion{}, Exit: &ui.Motion{}}

// actions are the bare icons of a row, in its meta line at the right: the row's own meta line has room for them. A
// card has its meta line at the bottom of the card.
func (s *Screen) actions(c *ui.Context, i int, card bool) {
	t := c.Theme()
	e := s.entries[i]
	bar := ui.Row(c).Key("actions").Gap(px(t, 1)).Transition(actionFade)
	if card {
		bar.Attach(ui.AnchorBottomRight, ui.AnchorBottomRight)
	} else {
		bar.Attach(ui.AnchorTopRight, ui.AnchorTopRight).Top(px(t, 14)).Right(px(t, 10))
	}
	bar.Children(func() {
		star, starColor := s.text.Star, t.TextMuted
		if e.Starred {
			star, starColor = s.text.Unstar, theme.Star(t)
		}
		if s.action(c, star, icons.Star, starColor, e.Starred) {
			s.toggleStar(i)
		}
		read, readIcon := s.text.MarkRead, icons.MailOpen
		if e.IsRead {
			read, readIcon = s.text.MarkUnread, icons.Mail
		}
		if s.action(c, read, readIcon, t.TextMuted, false) {
			s.toggleRead(i)
		}
		if s.action(c, s.text.Open, icons.ExternalLink, t.TextMuted, false) {
			s.browse(c, i)
		}
	})
}

// action is one bare icon of a row: 27 by 24, SurfacePressed under the pointer and Chosen pressed. A starred one
// keeps its own color.
func (s *Screen) action(c *ui.Context, label string, icon *ui.SVG, color ui.Color, keep bool) bool {
	t := c.Theme()
	b := ui.ButtonBase(c).Size(px(t, 27), px(t, 24)).Radius(px(t, 7)).Label(label).Tooltip(label)
	switch {
	case b.Pressed():
		b.Background(theme.Chosen(t))
	case b.Hovered():
		b.Background(t.SurfacePressed)
		if !keep {
			color = t.Text
		}
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(px(t, 16)).TextColor(color) })
	return b.Clicked()
}

// meta is the line of the source, the time and the star, 20 high; right is the room kept at its right for the
// actions, and lead comes first, as the unread dot of a card. The source is Text2, TextMuted once read.
func (s *Screen) meta(c *ui.Context, e client.Entry, now time.Time, lead bool, right float32, muted ui.Color) {
	t := c.Theme()
	source := theme.Text2(t)
	if e.IsRead {
		source = muted
	}
	ui.Row(c).FillWidth().Height(px(t, 20)).MinWidth(0).AlignItems(ui.Center).Gap(px(t, 6)).Padding(0, right, 0, 0).
		Children(func() {
			if lead && !e.IsRead {
				ui.Box(c).Size(px(t, 7), px(t, 7)).Radius(px(t, 3.5)).Background(t.Accent)
			}
			s.glyph(c, e.SourceTitle, s.iconOf(e.SourceID), 15)
			ui.Text(c, e.SourceTitle).FontSize(px(t, 12)).FontWeight(500).SingleLine().MinWidth(0).Shrink(1).
				TextColor(source)
			ui.Text(c, "·").FontSize(px(t, 12)).TextColor(muted).Opacity(0.7)
			ui.Text(c, s.when(e, now)).FontSize(px(t, 12)).FontFeatures("tnum").TextColor(muted).NoWrap()
			if e.Starred {
				ui.Icon(c, icons.Star).FontSize(px(t, 13)).TextColor(theme.Star(t))
			}
		})
}

// marked splits text into spans, the parts matching the search on the warning tint.
func (s *Screen) marked(c *ui.Context, text string, color ui.Color) []ui.Span {
	q := strings.ToLower(s.searched)
	lower := strings.ToLower(text)
	if q == "" || len(lower) != len(text) {
		return []ui.Span{{Text: text, Color: color}}
	}
	t := c.Theme()
	var spans []ui.Span
	for {
		i := strings.Index(lower, q)
		if i < 0 {
			break
		}
		spans = append(spans, ui.Span{Text: text[:i], Color: color},
			ui.Span{Text: text[i : i+len(q)], Color: color, Background: theme.Soft(t, t.Warning)})
		text, lower = text[i+len(q):], lower[i+len(q):]
	}
	return append(spans, ui.Span{Text: text, Color: color})
}

// rows is one column of entries, each drawn by its kind: All, Articles, Social, a source, and any search.
func (s *Screen) rows(c *ui.Context, now time.Time, compact bool) {
	t := c.Theme()
	s.list.Key = func(i int) any {
		if i >= len(s.entries) {
			return "footer"
		}
		return s.entries[i].ID
	}
	s.list.Label = func(i int) string {
		if i >= len(s.entries) {
			return s.text.LoadingMore
		}
		return s.label(s.entries[i], now)
	}
	if s.toTop {
		s.list.ScrollTo(0, ui.Start)
	}
	maxWidth := px(t, 680)
	if s.scope.kind == "social" {
		maxWidth = px(t, 720)
	}
	n := len(s.entries)
	if s.hasFooter() {
		n++
	}
	ui.List(c, &s.list, n, func(i int) {
		// The row is 16 wider than the column on each side, so its text lines up with the toolbar's.
		ui.Column(c).FillWidth().MaxWidth(maxWidth).Margin(0, ui.Auto).PaddingX(px(t, 8)).Children(func() {
			if i >= len(s.entries) {
				s.footer(c)
				return
			}
			s.row(c, i, now, compact)
		})
	}).Grow(1).FillWidth().Padding(s.top()+t.Space(1), 0, t.Space(6), 0).ScrollbarInsets(s.top(), 0, 0, 0).
		Label(s.scopeName())
}

// row is one entry: hover, pressed and chosen are the three steps of one fill, and the chosen row carries the marker.
// The separator above it and the one above the next go while the row is hovered or chosen, and TextMuted text steps to
// Text2 on the chosen fill.
func (s *Screen) row(c *ui.Context, i int, now time.Time, compact bool) {
	t := c.Theme()
	e := s.entries[i]
	r := ui.Row(c).Key(e.ID).FillWidth().AlignItems(ui.Start).Gap(px(t, 14)).
		Padding(px(t, 16), px(t, 16), px(t, 16), px(t, 28)).Radius(px(t, 12))
	hovered, chosen := s.item(c, r, i, now, false)
	muted := t.TextMuted
	switch {
	case chosen:
		r.Background(theme.Chosen(t))
		muted = theme.Text2(t)
	case r.Pressed():
		r.Background(t.SurfacePressed)
	case hovered:
		r.Background(t.SurfaceHover)
	}
	theme.Fade(r)
	quiet := hovered || chosen || i > 0 && (s.hovered == i-1 || s.selected == i-1)
	r.Children(func() {
		if i > 0 && !quiet {
			ui.Box(c).Absolute().Top(0).Left(px(t, 28)).Right(px(t, 16)).Height(1).Background(t.Border)
		}
		if chosen {
			ui.Box(c).Attach(ui.AnchorLeft, ui.AnchorLeft).Left(8).Size(2.5, 18).Radius(2).
				Background(theme.Marker(t))
		}
		if !e.IsRead {
			ui.Box(c).Absolute().Left(px(t, 14)).Top(px(t, 23)).Size(px(t, 7), px(t, 7)).Radius(px(t, 3.5)).
				Background(t.Accent)
		}
		switch e.Kind {
		case "video":
			s.videoRow(c, e, now, compact, muted)
		case "social":
			s.socialRow(c, e, now, muted)
		case "image":
			s.imageRow(c, e, now, muted)
		default:
			s.articleRow(c, e, now, muted)
		}
		if hovered || chosen || r.Focused() {
			s.actions(c, i, false)
		}
	})
}

// titleText is a title in 15 (13 in a card) semibold; read, it is TextMuted in weight 500.
func (s *Screen) titleText(c *ui.Context, e client.Entry, size float32, muted ui.Color) ui.Element {
	t := c.Theme()
	color, weight := t.Text, 600
	if e.IsRead {
		color, weight = muted, 500
	}
	text := ui.RichText(c, s.marked(c, title(e), color)...).FontSize(px(t, size)).FontWeight(weight).Wrap()
	line, tracking := float32(1.35), -px(t, size)*0.005
	if size < 14 {
		line = 1.4
	}
	if theme.Han(title(e)) {
		line, tracking = 1.5, 0
	}
	return text.LineHeight(line).LetterSpacing(tracking)
}

func (s *Screen) articleRow(c *ui.Context, e client.Entry, now time.Time, muted ui.Color) {
	t := c.Theme()
	ui.Column(c).Grow(1).MinWidth(0).Gap(px(t, 3)).Children(func() {
		s.meta(c, e, now, false, px(t, 88), muted)
		s.titleText(c, e, 15, muted)
		if summary := strings.Join(strings.Fields(e.Summary), " "); summary != "" {
			color := theme.Text2(t)
			if e.IsRead {
				color = muted
			}
			text := ui.RichText(c, s.marked(c, summary, color)...).FontSize(px(t, 13)).MaxLines(2).LineHeight(1.5)
			if theme.Han(summary) {
				text.LineHeight(1.7)
			}
		}
	})
	if e.ThumbnailURL != "" {
		// Under the meta line, so the row's actions never cover it, and never taller than the text: it starts 24 down.
		s.picture(c, e.ThumbnailURL, true, px(t, 8), nil).Size(px(t, 96), px(t, 64)).AlignSelf(ui.Start).
			Margin(px(t, 24), 0, 0, 0)
	}
}

func (s *Screen) cover(c *ui.Context, e client.Entry, load, play bool) ui.Element {
	t := c.Theme()
	label := ""
	if e.DurationSec > 0 {
		label = strings.ReplaceAll(s.text.Duration, "{d}", duration(e.DurationSec))
	}
	return s.picture(c, e.ThumbnailURL, load, px(t, 10), func() {
		if play {
			ui.Box(c).Attach(ui.AnchorCenter, ui.AnchorCenter).Size(36, 36).Radius(18).Center().
				Background(ui.RGBA(28, 24, 18, 0.4)).Children(func() {
				ui.Icon(c, icons.Play).FontSize(16).TextColor(ui.Hex("#ffffff")).Margin(0, 0, 0, 2)
			})
		}
		if e.DurationSec > 0 {
			ui.Text(c, duration(e.DurationSec)).Attach(ui.AnchorBottomRight, ui.AnchorBottomRight).Right(6).Bottom(6).
				Padding(1, 6).Radius(5).Background(ui.RGBA(28, 24, 18, 0.62)).FontSize(px(t, 11)).FontWeight(500).
				FontFeatures("tnum").TextColor(ui.Hex("#ffffff"))
		}
	}).AspectRatio(16.0 / 9).Label(label).Role(ui.RoleImage)
}

func (s *Screen) videoRow(c *ui.Context, e client.Entry, now time.Time, compact bool, muted ui.Color) {
	t := c.Theme()
	width := px(t, 184)
	if compact {
		width = px(t, 136)
	}
	s.cover(c, e, true, true).Width(width).Margin(px(t, 24), 0, 0, 0).AlignSelf(ui.Start)
	ui.Column(c).Grow(1).MinWidth(0).Gap(px(t, 3)).Children(func() {
		s.meta(c, e, now, false, px(t, 88), muted)
		s.titleText(c, e, 15, muted)
		if e.Platform != "" {
			ui.Row(c).AlignItems(ui.Center).Gap(px(t, 6)).Children(func() {
				ui.Icon(c, icons.Play).FontSize(px(t, 13)).TextColor(muted)
				ui.Text(c, e.Platform).FontSize(px(t, 12)).TextColor(muted)
			})
		}
	})
}

func (s *Screen) socialRow(c *ui.Context, e client.Entry, now time.Time, muted ui.Color) {
	t := c.Theme()
	color, author := t.Text, t.Text
	if e.IsRead {
		color, author = muted, theme.Text2(t)
	}
	ui.Column(c).Grow(1).MinWidth(0).Gap(px(t, 3)).Children(func() {
		s.meta(c, e, now, false, px(t, 88), muted)
		if e.Author != "" || e.Platform != "" {
			ui.RichText(c, ui.Span{Text: e.Author, Weight: 500, Color: author},
				ui.Span{Text: " " + e.Platform, Color: muted}).FontSize(px(t, 13)).SingleLine()
		}
		text := strings.TrimSpace(e.Summary)
		if text == "" {
			text = e.Title
		}
		body := ui.RichText(c, s.marked(c, text, color)...).FontSize(px(t, 13)).MaxLines(6).Wrap().LineHeight(1.55)
		if theme.Han(text) {
			body.LineHeight(1.7)
		}
		s.gallery(c, e)
	})
}

// gallery is the grid of the pictures of a post, never taller than one row of them: one picture keeps its shape, two
// sit side by side, and three or more are the first three as squares, the third saying how many more there are.
func (s *Screen) gallery(c *ui.Context, e client.Entry) {
	n := len(e.Images)
	if n == 0 {
		return
	}
	t := c.Theme()
	shown := min(n, 3)
	grid := ui.Grid(c).Columns(shown).Gap(4).Radius(px(t, 12)).Clip().Margin(px(t, 8), 0, 0, 0).
		Label(s.format(s.text.Pictures, n)).Role(ui.RoleImage)
	switch {
	case shown == 1:
		grid.MaxWidth(px(t, 384))
	case s.scope.kind == "social":
		grid.MaxWidth(px(t, 520))
	default:
		grid.MaxWidth(px(t, 440))
	}
	grid.Children(func() {
		for k, img := range e.Images[:shown] {
			var over func()
			if k == shown-1 && n > shown {
				more := s.format(s.text.MorePictures, n-shown)
				over = func() {
					ui.Box(c).Fill().Absolute().Center().Background(ui.RGBA(28, 24, 18, 0.5)).Label(more).Children(func() {
						ui.Text(c, "+"+strconv.Itoa(n-shown)).FontSize(px(t, 17)).FontWeight(600).
							FontFeatures("tnum").TextColor(ui.Hex("#ffffff"))
					})
				}
			}
			p := s.picture(c, img.URL, true, 0, over).Key(k)
			switch shown {
			case 1:
				p.AspectRatio(16.0 / 10).MaxHeight(px(t, 240))
			case 2:
				p.AspectRatio(4.0 / 3)
			default:
				p.AspectRatio(1)
			}
		}
	})
}

func (s *Screen) imageRow(c *ui.Context, e client.Entry, now time.Time, muted ui.Color) {
	t := c.Theme()
	ui.Column(c).Grow(1).MinWidth(0).Gap(px(t, 3)).Children(func() {
		s.meta(c, e, now, false, px(t, 88), muted)
		s.titleText(c, e, 15, muted)
		if address := firstPicture(e); address != "" {
			s.picture(c, address, true, px(t, 12), nil).FillWidth().AspectRatio(1.5).MaxHeight(px(t, 320)).
				Margin(px(t, 8), 0, 0, 0)
		}
	})
}

func firstPicture(e client.Entry) string {
	if len(e.Images) > 0 {
		return e.Images[0].URL
	}
	return e.ThumbnailURL
}

// aspect is the width over the height of the first picture of an image entry, 4:3 when it does not say.
func aspect(e client.Entry) float32 {
	if len(e.Images) > 0 && e.Images[0].Width > 0 && e.Images[0].Height > 0 {
		return min(max(float32(e.Images[0].Width)/float32(e.Images[0].Height), 0.4), 3)
	}
	return 4.0 / 3
}

// picture is a picture on the Track color, which shows until it loads or when it fails, with a hairline outline of
// ink over it; load false leaves it unasked, for those far from the view. over draws on it.
func (s *Screen) picture(c *ui.Context, address string, load bool, radius float32, over func()) ui.Element {
	t := c.Theme()
	var bitmap *ui.Bitmap
	if load {
		bitmap = s.bitmap(address)
	} else if p := s.pictures[address]; p != nil {
		bitmap = p.bitmap
	}
	return ui.Box(c).Background(theme.Track(t)).Radius(radius).Clip().Children(func() {
		if bitmap != nil {
			ui.Image(c, bitmap).Fit(ui.Cover).Fill()
		}
		if over != nil {
			over()
		}
		ui.Box(c).Fill().Absolute().Radius(radius).PassThrough().Border(0.5, theme.Ink(t, 0.10))
	})
}

// columns returns how many columns of at least minWidth fit in the width left of the sidebar, gap apart, and how
// wide each is.
func columns(c *ui.Context, compact bool, minWidth, gap, most float32) (int, float32) {
	t := c.Theme()
	width, _ := c.Size()
	width -= sideWidth(t, compact) + 8 + 2*px(t, 24)
	n := max(1, int((width+gap)/(minWidth+gap)))
	if most > 0 {
		n = min(n, int(most))
	}
	return n, (width - gap*float32(n-1)) / float32(n)
}

// videoGrid is the Videos tab: a grid of covers.
func (s *Screen) videoGrid(c *ui.Context, now time.Time, compact bool) {
	t := c.Theme()
	s.grid.Key = func(i int) any { return s.entries[i].ID }
	s.grid.Label = func(i int) string { return s.label(s.entries[i], now) }
	if s.toTop {
		s.grid.ScrollTo(0, ui.Start)
	}
	gap := px(t, 14)
	_, cell := columns(c, compact, px(t, 200), gap, 0)
	height := cell*9/16 + px(t, 13.5)*2.9 + px(t, 18) + t.Space(3) + px(t, 20)
	ui.GridView(c, &s.grid, len(s.entries), px(t, 200), height, func(i int) {
		e := s.entries[i]
		card := ui.Column(c).Fill().Gap(px(t, 8)).Radius(px(t, 10))
		hovered, chosen := s.item(c, card, i, now, true)
		card.Children(func() {
			s.cover(c, e, true, hovered).FillWidth()
			s.titleText(c, e, 13, t.TextMuted).MaxLines(2)
			s.meta(c, e, now, true, 0, t.TextMuted)
			if hovered || chosen || card.Focused() {
				s.actions(c, i, true)
			}
		})
	}).Grow(1).FillWidth().Gap(gap).Padding(s.top()+t.Space(4), px(t, 24), t.Space(6), px(t, 24)).
		ScrollbarInsets(s.top(), 0, 0, 0).Label(s.scopeName())
	if s.hasFooter() {
		s.footer(c)
	}
}

// wallOf is the Images tab: a masonry wall, each picture in the shortest column.
func (s *Screen) wallOf(c *ui.Context, now time.Time, compact bool) {
	t := c.Theme()
	if s.toTop {
		s.wall.Y = 0
	}
	gap := px(t, 14)
	n, width := columns(c, compact, px(t, 170), gap, 3)
	_, viewHeight := c.Size()
	caption := px(t, 12.5)*1.45 + px(t, 18) + 2*t.Space(1) + t.Space(4)
	cols := make([][]int, n)
	tops := make([][]float32, n)
	heights := make([]float32, n)
	for i, e := range s.entries {
		k := 0
		for j := range heights {
			if heights[j] < heights[k] {
				k = j
			}
		}
		cols[k] = append(cols[k], i)
		tops[k] = append(tops[k], heights[k])
		heights[k] += width/aspect(e) + caption
	}
	top := s.top() + t.Space(4)
	ui.Scroll(c).TrackScroll(&s.wall).Grow(1).FillWidth().ScrollbarInsets(s.top(), 0, 0, 0).Label(s.scopeName()).
		Children(func() {
			ui.Row(c).FillWidth().Gap(gap).Padding(top, px(t, 24), t.Space(6), px(t, 24)).
				AlignItems(ui.Start).Children(func() {
				for k := range cols {
					ui.Column(c).Key(k).Grow(1).Basis(0).MinWidth(0).Gap(t.Space(4)).Children(func() {
						for j, i := range cols[k] {
							at := top + tops[k][j]
							near := at+width > s.wall.Y-wallAhead && at < s.wall.Y+viewHeight+wallAhead
							s.tile(c, i, now, near)
						}
					})
				}
			})
			if s.hasFooter() {
				s.footer(c)
			}
		})
	// The wall draws every tile, so the next page loads by how far it is scrolled, not by the tiles drawn.
	s.wantMore = s.wall.Y >= s.wall.MaxY-wallAhead
}

func (s *Screen) tile(c *ui.Context, i int, now time.Time, near bool) {
	t := c.Theme()
	e := s.entries[i]
	tile := ui.Column(c).Key(e.ID).FillWidth().Gap(px(t, 6)).Radius(px(t, 10))
	hovered, chosen := s.item(c, tile, i, now, true)
	tile.Children(func() {
		s.picture(c, firstPicture(e), near, px(t, 10), nil).FillWidth().AspectRatio(aspect(e))
		color := t.Text
		if e.IsRead {
			color = t.TextMuted
		}
		ui.Text(c, title(e)).FontSize(px(t, 12.5)).FontWeight(500).SingleLine().TextColor(color)
		s.meta(c, e, now, true, 0, t.TextMuted)
		if hovered || chosen || tile.Focused() {
			s.actions(c, i, true)
		}
	})
}
