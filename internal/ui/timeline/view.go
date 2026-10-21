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
	All, Article, Video, Social, Image, Kinds, Sources, Everything, Unread, Read, Starred, Show                                       string
	Search, SearchLabel, Clear, UnreadCount, Results, MoreResults, MarkAllIn, Star, Unstar, MarkRead                                  string
	MarkUnread, Open, FailedOnce, FailedTimes, NewEntries, Offline, Loading, LoadingMore, LoadMoreFailed, AllLoaded                   string
	TooShort, NoResults, NoResultsHint, UnreadDone, UnreadDoneHint, NoStarred, NoStarredHint                                          string
	Empty, EmptyHint, NoSubscriptions, NoSubscriptionsHint, Error, ErrorHint, Retry, Marked                                           string
	Starring, Unstarring, ActionFailed, OpenFailed, ThisComputer, Server, Settings, Duration                                          string
	MorePictures, Pictures, AISummary, Now, Minutes, Hours, Yesterday, Days, DateLayout, YearLayout, Separator                        string
	AddSource, EditSource, AddFirst, ImportOPML, PausedMark, Undo, Dismiss, NoMoreUnread, KeysTitle, KeysMenu, HelpMenu               string
	Close, Or, InTimeline, InReader, Anywhere, KeyNext, KeyPrevious, KeyNextUnread, KeyOpen, KeyStar                                  string
	KeyRead, KeyOpenBrowser, KeySearch, KeyEscape, KeyUndo, KeyNextArticle, KeyPreviousArticle                                        string
	KeyBack, KeyAdd, KeySettings, KeyKeys, FailingName, FailingUnread, FailingCount, FailingShow                                      string
	FailingTitle, ShowAll, MarkAbove, MarkedAbove, MarkedVisible, KeyMarkVisible, KeyScope, KeySidebar, KeyMenu                       string
	TimeAny, TimeUpTo, TimeChip, TimeNameAny, TimeNameUpTo, ReadingTime, NothingFits, NothingFitsHint, AnyLength                      string
	ShowGrid, ShowList, KeyPalette, PaletteTitle, PaletteGoTo, PaletteOpenSettings, PaletteActions, PaletteUnreadOnly, PaletteShowAll string
	PaletteNone, PaletteCount, PaletteCountOne, PageAppearance, PageFetching, PageImport, PageAI, PageDevices                         string
	MarkOlder1, MarkOlder7, MarkOlder30, MarkedOlder, NoneOlder, Age1, Age7, Age30                                                    string
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
// points of summaries and translations while AI is not set up, OpenImport the settings on the import and export
// group, from the empty timeline of a first run, OpenSettingsPage the settings on the group called page
// ("appearance", "fetching", "opml", "ai" or "devices"), from the command palette, and Expired the connect screen
// because the server no longer takes the session. Reading gives the reading preferences of this device, for the reader, and ReadingFont the
// font list of the article's body that they come to, "" for the interface font. Change keeps preferences the
// screen changed itself, the list or grid of a scope.
type Deps struct {
	Go               func(work func() (apply func()))
	Now              func() time.Time
	Decode           func([]byte) (*ui.Bitmap, error)
	OpenConnect      func()
	OpenSettings     func()
	OpenAISettings   func()
	OpenImport       func()
	OpenSettingsPage func(page string)
	Expired          func()
	Reading          func() prefs.Preferences
	ReadingFont      func() string
	Change           func(prefs.Preferences)
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

// marking is a "mark as read" (all, above or a subscription) that can be undone: the entries it marked, and the text of its toast, once shown.
type marking struct {
	ids   []int64
	text  string
	shown bool
}

// keepLists is how many lists the screen keeps to draw again at once.
const keepLists = 8

// listKey is what defines a list: the scope, the filter and the reading-time budget. A search is not kept.
type listKey struct {
	scope  scope
	filter int
	budget int
}

// budgets are the reading-time budgets the menu offers, in minutes; 0 is any length.
var budgets = []int{5, 10, 15, 30}

// snapshot is a list left, kept to draw again, where it was, when it is chosen again.
type snapshot struct {
	key      listKey
	entries  []client.Entry
	next     string
	anchor   int64   // the first entry in view, 0 for none
	wallY    float32 // how far the Images wall was scrolled
	selected int64   // the entry chosen, 0 for none
}

// The IDs of the toasts the screen replaces rather than stacks.
const (
	markedToast    = "timeline.marked"
	noMoreToast    = "timeline.no-more-unread"
	noneOlderToast = "timeline.none-older"
)

// Screen keeps the timeline: the scope, the entries loaded, and the work under way.
type Screen struct {
	deps   Deps
	text   copy
	api    API
	server string // the host of the server, "" for this computer
	gen    int    // counts servers used, so the results of the one before are dropped

	sources       []client.Source
	failingOnly   bool // the sidebar lists only the subscriptions whose last fetch failed
	sourcesLoaded bool
	scope         scope
	tab           int // the tab of scope.kind
	filter        int // everything, unread or starred
	budget        int // the most minutes an entry may take to read, 0 for any length; not kept between servers

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
	wantMore bool       // a row near the end was drawn in this frame
	run      int        // counts first pages, so a result the user moved on from is dropped
	checking bool       // a list drawn from memory is being checked against the server
	lists    []snapshot // the lists left, the least recent first
	polledAt time.Time
	polling  bool
	offline  bool      // a background check failed after the list loaded
	updated  time.Time // the last load or check that succeeded
	fresh    int       // entries newer than the top one, found by polling

	selected  int             // the entry chosen, -1 for none
	focusRow  int             // the entry to give the focus in the next frame, -1 for none
	sideFocus bool            // give the sidebar the focus in the next frame
	inSide    bool            // the focus is in the sidebar, or goes there
	sideKeyed bool            // the focus in the sidebar came by the keyboard; a click on a kind or subscription clears it
	hovered   int             // the entry under the pointer in the last frame
	height    float32         // of the window in the last frame
	rects     map[int]ui.Rect // where the entries were drawn in the last frame, in window DIPs
	toTop     bool
	anchor    int64     // the first entry in view in the last frame, 0 for none
	viewed    viewerSet // the viewers the last frame drew with
	pin       int64     // the entry to scroll to in the next draw, after a viewer changed; 0 for none
	list      ui.ListState
	grid      ui.GridState
	wall      ui.ScrollState

	pictures  map[string]*picture
	wanted    []string // pictures to download, the latest asked first
	downloads int
	drawn     int

	undoable *marking              // the last "mark as read", until its toast closes
	seeking  bool                  // N found no unread entry loaded and waits for the next page
	noMore   bool                  // show that no unread entry is below
	noOlder  string                // show that no unread entry is older than this age, in words
	keys     bool                  // the key sheet shows
	pal      paletteState          // the command palette
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
		hovered: -1, pictures: map[string]*picture{}, rects: map[int]ui.Rect{}}
	s.subs = subscriptions.New(systemLocale, subscriptions.Deps{
		Go:           deps.Go,
		Now:          deps.Now,
		Sources:      func() []client.Source { return s.sources },
		Icon:         s.bitmap,
		Added:        s.added,
		Changed:      s.changed,
		Removed:      s.removed,
		Show:         func(id int64) { s.choose(scope{kind: s.kindOf(id), source: id}) },
		MarkRead:     s.markSource,
		MarkOlder:    s.markSourceOlder,
		OpenSettings: deps.OpenAISettings,
		Expired: func() {
			s.gen++
			s.api = nil
			s.lists = nil
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
		ReadingFont:  deps.ReadingFont,
		Back:         s.back,
		Move:         s.step,
		NextUnread:   s.nextUnread,
		Star:         func() { s.toggleStar(s.index(s.reading)) },
		ToggleRead:   func() { s.toggleRead(s.index(s.reading)) },
	})
	return s
}

// SetLanguage shows the screen, its dialogs and the reader in the language of systemLocale, keeping the scope, the
// selection, the place in the list and the article open.
func (s *Screen) SetLanguage(systemLocale string) {
	s.text = translations[locale.Language(systemLocale)]
	s.subs.SetLanguage(systemLocale)
	s.reader.SetLanguage(systemLocale)
}

// Use shows the timeline of api, whose host is server ("" for this computer), from the start.
func (s *Screen) Use(api API, server string) {
	s.api, s.server = api, server
	s.gen++
	s.sources, s.sourcesLoaded = nil, false
	s.scope, s.tab, s.filter, s.budget = scope{}, 0, everything, 0
	s.query, s.searched, s.editing = "", "", false
	s.lists, s.anchor = nil, 0
	s.pictures, s.wanted, s.downloads = map[string]*picture{}, nil, 0
	s.undoable, s.seeking, s.keys, s.pal = nil, false, false, paletteState{}
	s.subs.Use(api)
	s.reader.Use(api)
	s.loadSources()
	s.reload()
}

// Refresh loads the subscriptions and the entries again, as after an import added subscriptions.
func (s *Screen) Refresh() {
	s.lists = nil
	s.loadSources()
	s.reload()
}

func (s *Screen) expired(err error) bool {
	if !errors.Is(err, client.ErrUnauthorized) {
		return false
	}
	s.gen++
	s.api = nil
	s.lists = nil
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
		s.lists = nil
		s.reload()
	}
}

// removed drops a subscription the server no longer has, with its entries.
func (s *Screen) removed(id int64) {
	s.sources = slices.DeleteFunc(s.sources, func(src client.Source) bool { return src.ID == id })
	s.loadSources()
	s.lists = nil
	if s.scope.source == id {
		s.entries = nil
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
		MaxMinutes: s.budget, Search: s.searched, Limit: pageSize, Cursor: cursor}
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
	s.offline = false
	s.selected, s.focusRow, s.hovered, s.toTop, s.reading, s.seeking = -1, -1, -1, true, 0, false
	s.checking = false
	s.retryPictures()
	s.reader.Forget()
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
			s.updated = s.polledAt
		}
	})
}

// retryPictures forgets the pictures that failed to load, so the list tries them again.
func (s *Screen) retryPictures() {
	for address, p := range s.pictures {
		if p.failed {
			delete(s.pictures, address)
		}
	}
}

func (s *Screen) key() listKey { return listKey{s.scope, s.filter, s.budget} }

// views are the viewers of the four kinds, as the preferences of this device choose them: the defaults where the
// screen has no preferences to read.
func (s *Screen) views() prefs.Preferences {
	if s.deps.Reading == nil {
		return prefs.Default()
	}
	return s.deps.Reading().Normalize()
}

// viewerSet is the viewers in force, to tell when one changes.
type viewerSet [4]string

func (s *Screen) viewerSet() viewerSet {
	v := s.views()
	return viewerSet{v.ArticleViewer, v.VideoViewer, v.SocialViewer, v.ImageViewer}
}

// layout is how the list is laid out, which the viewers of Videos and Images and the scope decide.
type layout int

const (
	columnLayout layout = iota // one column of rows
	gridLayout                 // the Videos grid of covers
	squareLayout               // the Images grid of squares
	wallLayout                 // the Images wall
)

// layout tells how the list is laid out. The grid and the wall are used only where a grid is offered (see gridable)
// and chosen, by the switch of the toolbar or else by the viewers; everywhere else, and in a list chosen, entries are
// rows or cards in a column.
func (s *Screen) layout() layout {
	if !s.gridable() || !s.gridOn() {
		return columnLayout
	}
	switch {
	case s.scope.kind == "video":
		return gridLayout
	case s.views().ImageViewer == prefs.ViewGrid:
		return squareLayout
	}
	return wallLayout
}

// gridable tells whether the list could be a grid: in the Videos and Images scopes and in one subscription of those
// kinds, and not in a search.
func (s *Screen) gridable() bool {
	return s.searched == "" && (s.scope.kind == "video" || s.scope.kind == "image")
}

// gridDefault is whether the scope is a grid before the reader switches it: the Videos and Images scopes follow the
// viewers of Settings (a grid for Videos Grid, for Images Wall and Grid), a subscription is a list.
func (s *Screen) gridDefault() bool {
	if s.scope.source != 0 {
		return false
	}
	v := s.views()
	return s.scope.kind == "video" && v.VideoViewer == prefs.ViewGrid ||
		s.scope.kind == "image" && (v.ImageViewer == prefs.ViewWall || v.ImageViewer == prefs.ViewGrid)
}

// gridOn is whether the scope shows the grid: the switch the reader made on this device, else the default.
func (s *Screen) gridOn() bool {
	switch s.views().Grids.Get(prefs.GridScope(s.scope.kind, s.scope.source)) {
	case prefs.ViewGrid:
		return true
	case prefs.ViewList:
		return false
	}
	return s.gridDefault()
}

// switchGrid shows the scope as a grid if it is a list and the other way round, and keeps the choice on this device.
// The list is drawn again in place, at the entry that was first in view.
func (s *Screen) switchGrid() {
	if s.deps.Change == nil || !s.gridable() {
		return
	}
	want, def := prefs.ViewGrid, prefs.ViewList
	if s.gridOn() {
		want = prefs.ViewList
	}
	if s.gridDefault() {
		def = prefs.ViewGrid
	}
	p := s.views()
	p.Grids = p.Grids.Set(prefs.GridScope(s.scope.kind, s.scope.source), want, def)
	s.pin = s.anchor
	s.deps.Change(p)
}

// grids and walls tell whether the list is drawn as a grid of cells (the Videos grid or the Images grid) or as the
// Images wall, which keep their place differently from the column of rows.
func (s *Screen) grids() bool {
	l := s.layout()
	return l == gridLayout || l == squareLayout
}
func (s *Screen) walls() bool { return s.layout() == wallLayout }

// leave keeps the list on screen, which has the key old, to draw it again, where it was, when it is chosen again.
// Lists not loaded, empty or failed and searches are not kept.
func (s *Screen) leave(old listKey) {
	if s.searched != "" || s.loading || s.loadErr || len(s.entries) == 0 {
		return
	}
	snap := snapshot{key: old, entries: s.entries, next: s.next, anchor: s.anchor, wallY: s.wall.Y}
	if s.selected >= 0 && s.selected < len(s.entries) {
		snap.selected = s.entries[s.selected].ID
	}
	s.lists = slices.DeleteFunc(s.lists, func(l snapshot) bool { return l.key == old })
	s.lists = append(s.lists, snap)
	if len(s.lists) > keepLists {
		s.lists = slices.Delete(s.lists, 0, 1)
	}
}

// show changes what the list shows, the scope, the filter or the search, by change. The list left is kept; one
// chosen again that is kept is drawn at once where it was and checked in the background, any other is loaded.
func (s *Screen) show(change func()) {
	s.leave(s.key())
	change()
	if s.searched == "" {
		if i := slices.IndexFunc(s.lists, func(l snapshot) bool { return l.key == s.key() }); i >= 0 {
			snap := s.lists[i]
			s.lists = slices.Delete(s.lists, i, i+1)
			s.restore(snap)
			return
		}
	}
	s.reload()
}

// restore draws snap at once, scrolled to the entry that was first in view and with the entry that was chosen, and
// checks the first page against the server.
func (s *Screen) restore(snap snapshot) {
	if s.api == nil {
		return
	}
	s.run++
	run, gen, api, q := s.run, s.gen, s.api, s.query1("")
	s.entries, s.next, s.anchor = snap.entries, snap.next, snap.anchor
	s.loading, s.loadErr, s.more, s.moreErr, s.fresh, s.offline, s.checking = false, false, false, false, 0, false, true
	s.selected, s.focusRow, s.hovered, s.toTop, s.reading, s.seeking = s.index(snap.selected), -1, -1, false, 0, false
	s.polledAt = s.deps.Now()
	s.retryPictures()
	s.reader.Forget()
	switch i := s.index(snap.anchor); {
	case s.walls():
		s.wall.Y = snap.wallY
	case i < 0:
	case s.grids():
		s.grid.ScrollTo(i, ui.Start)
	default:
		s.list.ScrollTo(i, ui.Start)
	}
	s.deps.Go(func() func() {
		page, err := api.Entries(context.Background(), q)
		return func() {
			if gen != s.gen || run != s.run {
				return
			}
			s.checking = false
			if s.expired(err) {
				return
			}
			if err != nil {
				if !s.offline {
					s.offline, s.announce = true, s.staleText()
				}
				return
			}
			s.offline, s.polledAt = false, s.deps.Now()
			s.updated = s.polledAt
			s.merge(page)
		}
	})
}

// holdsTop tells whether entries newer than the top one must not be put above a grid or a wall scrolled away from
// its top, which would move what is in view; the column of rows keeps its place by the keys of its rows.
func (s *Screen) holdsTop() bool {
	switch {
	case s.walls():
		return s.wall.Y > 0
	case s.grids():
		return len(s.entries) == 0 || s.anchor != s.entries[0].ID
	}
	return false
}

// older tells whether a comes after b in the timeline.
func older(a, b client.Entry) bool {
	return a.CreatedAt < b.CreatedAt || a.CreatedAt == b.CreatedAt && a.ID < b.ID
}

// merge puts the first page, fetched again for a list drawn from memory, in its place: the page, then the entries
// loaded before that come after its last one. The choice stays on its entry, if the page and the list still have
// it. A grid or wall scrolled away from its top does not take the entries newer than its top one; they show as
// "new" as polling shows them. The article being read is left as it is.
func (s *Screen) merge(page client.Page) {
	if s.reading != 0 {
		return
	}
	cached, fresh := s.entries, page.Entries
	if len(cached) > 0 && s.holdsTop() {
		n := 0
		fresh = slices.DeleteFunc(slices.Clone(fresh), func(e client.Entry) bool {
			newer := older(cached[0], e)
			if newer {
				n++
			}
			return newer
		})
		if n > 0 {
			s.announce = s.format(s.text.NewEntries, n)
		}
		s.fresh = n
	}
	var selected, focus int64
	if s.selected >= 0 && s.selected < len(cached) {
		selected = cached[s.selected].ID
	}
	if s.focusRow >= 0 && s.focusRow < len(cached) {
		focus = cached[s.focusRow].ID
	}
	merged, next := fresh, page.Next
	if page.Next != "" {
		have := make(map[int64]bool, len(fresh))
		for _, e := range fresh {
			have[e.ID] = true
		}
		merged = slices.Clone(fresh)
		for _, e := range cached {
			if !have[e.ID] && (len(fresh) == 0 || older(e, fresh[len(fresh)-1])) {
				merged = append(merged, e)
			}
		}
		if len(merged) > len(fresh) {
			next = s.next
		}
	}
	s.entries, s.next = merged, next
	s.selected, s.focusRow, s.hovered = s.index(selected), s.index(focus), -1
}

// patchLists puts the change of the entries ids in the lists kept: the flags, and out of Unread what is read and out
// of Starred what is not starred. Entries a list does not have are not added; the check on return gets the first page.
func (s *Screen) patchLists(ids []int64, patch client.EntryPatch) {
	set := make(map[int64]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	for i := range s.lists {
		l := &s.lists[i]
		l.entries = slices.DeleteFunc(l.entries, func(e client.Entry) bool {
			if !set[e.ID] {
				return false
			}
			return patch.IsRead != nil && *patch.IsRead && l.key.filter == unread ||
				patch.Starred != nil && !*patch.Starred && l.key.filter == starred
		})
		for j := range l.entries {
			if !set[l.entries[j].ID] {
				continue
			}
			if patch.IsRead != nil {
				l.entries[j].IsRead = *patch.IsRead
			}
			if patch.Starred != nil {
				l.entries[j].Starred = *patch.Starred
			}
		}
	}
}

func (s *Screen) loadMore() {
	if s.api == nil || s.next == "" || s.loading || s.more || s.moreErr || s.checking {
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
	if s.api == nil || s.loading || s.loadErr || s.checking || s.polling || s.searched != "" {
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
			if run != s.run {
				return
			}
			if sourcesErr != nil || err != nil {
				if !s.offline {
					s.offline, s.announce = true, s.staleText()
				}
				return
			}
			s.offline, s.updated = false, s.deps.Now()
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
	s.show(func() {
		s.scope = next
		s.tab = max(0, indexOf(kinds, next.kind))
	})
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
	s.show(func() { s.searched = q })
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
	s.patchLists([]int64{id}, patch)
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
			s.patchLists([]int64{id}, back)
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
	s.prefetch()
	s.markRead(e)
	if i >= len(s.entries)-nearEnd {
		s.loadMore()
	}
}

// prefetch has the reader get the articles beside the one read, so the step to them shows at once.
func (s *Screen) prefetch() {
	var ids []int64
	for _, by := range []int{-1, 1} {
		if i := s.article(by); i >= 0 {
			ids = append(ids, s.entries[i].ID)
		}
	}
	s.reader.Prefetch(ids...)
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
	s.reader.Leave()
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
	return s.filter != starred && s.budget == 0 && s.searched == "" && len(s.entries) > 0 && s.unreadIn(s.scope) > 0
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
	s.markScope(scope, s.text.Marked, s.scopeName())
}

// canMarkAbove says whether an unread entry is loaded above the entry id, where "Mark above as read" can apply: not
// under Starred or a search, which the server's scopes do not know.
func (s *Screen) canMarkAbove(id int64) bool {
	if s.filter == starred || s.budget != 0 || s.searched != "" {
		return false
	}
	for _, e := range s.entries {
		if e.ID == id {
			return false
		}
		if !e.IsRead {
			return true
		}
	}
	return false
}

// markAbove marks the loaded entries above the entry id read, in the scope, and offers to undo it.
func (s *Screen) markAbove(id int64) {
	if !s.canMarkAbove(id) || s.api == nil {
		return
	}
	scope := client.ReadScope{SourceID: s.scope.source, Newer: id, UpTo: s.entries[0].ID}
	if s.scope.source == 0 {
		scope.Kind = s.scope.kind
	}
	s.markScope(scope, s.text.MarkedAbove, "")
}

// fullyShown says whether the entry i was drawn whole in the last frame: between the toolbar, which the list scrolls
// under, and the bottom of the window.
func (s *Screen) fullyShown(i int) bool {
	r, ok := s.rects[i]
	return ok && r.H > 0 && r.Y >= s.top() && r.Y+r.H <= s.height
}

// markVisible marks the unread entries that are whole in the window read, and scrolls the entry after them to the
// top. The last entry loaded is left out while another page follows, since the entries below it are not known; it
// stays unread and is the one scrolled to. At the end of the list it moves the choice to the last entry marked and does
// not scroll. Under Starred or a search, which the server's scopes do not know, it does nothing.
func (s *Screen) markVisible() {
	if s.filter == starred || s.budget != 0 || s.searched != "" || s.loading || s.api == nil {
		return
	}
	first := 0
	for first < len(s.entries) && !s.fullyShown(first) {
		first++
	}
	end := first
	for end < len(s.entries) && s.fullyShown(end) {
		end++
	}
	if first == len(s.entries) {
		return
	}
	if end == len(s.entries) && s.next != "" {
		end--
	}
	if slices.ContainsFunc(s.entries[first:end], func(e client.Entry) bool { return !e.IsRead }) {
		scope := client.ReadScope{SourceID: s.scope.source, UpTo: s.entries[first].ID}
		if end < len(s.entries) {
			scope.Newer = s.entries[end].ID
		}
		if s.scope.source == 0 {
			scope.Kind = s.scope.kind
		}
		s.markScope(scope, s.text.MarkedVisible, "")
	}
	if end == len(s.entries) {
		s.selected, s.focusRow = end-1, end-1
		return
	}
	s.selected, s.focusRow = end, end
	switch {
	case s.grids():
		s.grid.ScrollTo(end, ui.Start)
	case s.walls():
		s.wall.Y = min(max(s.rects[end].Y+s.wall.Y-s.top(), 0), s.wall.MaxY)
	default:
		s.list.ScrollTo(end, ui.Start)
	}
}

// markSource marks everything unread in the subscription read, whatever the timeline shows, and offers to undo it.
func (s *Screen) markSource(src client.Source) {
	if src.UnreadCount == 0 || s.api == nil {
		return
	}
	s.markScope(client.ReadScope{SourceID: src.ID}, s.text.Marked, subscriptions.Name(src))
}

// olderDays are the ages of the "Mark older than" items, in days.
var olderDays = []int{1, 7, 30}

// olderText is the label of the item that marks what is older than days read, and age that span in words.
func (s *Screen) olderText(days int) (label, age string) {
	switch days {
	case 1:
		return s.text.MarkOlder1, s.text.Age1
	case 7:
		return s.text.MarkOlder7, s.text.Age7
	}
	return s.text.MarkOlder30, s.text.Age30
}

// markOlder marks the entries of scope that are older than days read, whatever the timeline shows, and offers to undo
// it. When none is, it says so and leaves the last undo as it was.
func (s *Screen) markOlder(scope client.ReadScope, days int, name string) {
	_, age := s.olderText(days)
	scope.OlderThan = s.deps.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	s.markScopeText(scope, strings.ReplaceAll(s.text.MarkedOlder, "{age}", age), name, age)
}

// markAllOlder is markOlder for the scope of the timeline, under the same conditions as markAll.
func (s *Screen) markAllOlder(days int) {
	if !s.canMarkAll() || s.api == nil {
		return
	}
	scope := client.ReadScope{SourceID: s.scope.source}
	if s.scope.source == 0 {
		scope.Kind = s.scope.kind
	}
	s.markOlder(scope, days, s.scopeName())
}

// markSourceOlder is markOlder for a subscription, from its menu in the sidebar.
func (s *Screen) markSourceOlder(src client.Source, days int) {
	if src.UnreadCount == 0 || s.api == nil {
		return
	}
	s.markOlder(client.ReadScope{SourceID: src.ID}, days, subscriptions.Name(src))
}

// markScope marks the entries of scope read; text is the toast, with {n} for how many were marked and {scope} for name.
func (s *Screen) markScope(scope client.ReadScope, text, name string) {
	s.markScopeText(scope, text, name, "")
}

// markScopeText is markScope; when none is not empty and no entry was marked, the toast says that none is older than
// that age instead.
func (s *Screen) markScopeText(scope client.ReadScope, text, name, none string) {
	gen, api := s.gen, s.api
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
			if len(ids) == 0 && none != "" {
				s.noOlder = none
				return
			}
			s.setRead(ids, true)
			s.loadSources()
			s.undoable = &marking{ids: ids, text: strings.ReplaceAll(s.format(text, len(ids)), "{scope}", name)}
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
	s.patchLists(ids, client.EntryPatch{IsRead: &read})
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

// staleText says that the server is out of reach and when the list was last brought up to date.
func (s *Screen) staleText() string {
	return strings.ReplaceAll(s.text.Offline, "{time}", s.updated.Format("15:04"))
}

func (s *Screen) format(text string, n int) string {
	return strings.ReplaceAll(text, "{n}", strconv.Itoa(n))
}

// reason is why src is failing, as the edit dialog of the subscription says it.
func (s *Screen) reason(src client.Source) string {
	text := s.text.FailedOnce
	if src.ConsecutiveFailures > 1 {
		text = s.format(s.text.FailedTimes, src.ConsecutiveFailures)
	}
	return strings.ReplaceAll(text, "{error}", src.LastError)
}

// failingName is the name of a failing subscription for a screen reader: its name, its unread count when it has
// any, and that its last fetch failed.
func (s *Screen) failingName(name string, unread int) string {
	text := s.text.FailingName
	if unread > 0 {
		text = s.text.FailingUnread
	}
	return strings.ReplaceAll(s.format(text, unread), "{name}", name)
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
	parts := []string{e.SourceTitle, title(e), s.when(e, now)}
	if reading := s.readLabel(e); reading != "" {
		parts = append(parts, reading)
	}
	if e.IsRead {
		parts = append(parts, s.text.Read)
	} else {
		parts = append(parts, s.text.Unread)
	}
	if e.Starred {
		parts = append(parts, s.text.Starred)
	}
	return strings.Join(parts, s.text.Separator)
}

// readLabel is "6 min read" for an entry that is read and takes a minute or more; podcasts and videos, which have a
// duration of their own, and entries with no estimate have none.
func (s *Screen) readLabel(e client.Entry) string {
	if e.ReadingSec < 60 || e.DurationSec > 0 || e.Kind == "video" {
		return ""
	}
	return strings.ReplaceAll(s.text.ReadingTime, "{n}", strconv.Itoa(e.ReadingSec/60))
}

// budgetName is the name of the reading-time button, which states the budget.
func (s *Screen) budgetName() string {
	if s.budget == 0 {
		return s.text.TimeNameAny
	}
	return strings.ReplaceAll(s.text.TimeNameUpTo, "{n}", strconv.Itoa(s.budget))
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
	open := s.keys || s.subs.Open() || s.pal.open
	if !open {
		c.OnShortcut(ui.Cmd, ui.KeyK, s.openPalette)
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
	width, height := c.Size()
	compact := width < 760
	s.height = height
	if v := s.viewerSet(); v != s.viewed {
		// A viewer changed in Settings: the list is drawn again in place, at the entry that was first in view.
		if s.viewed != (viewerSet{}) {
			s.pin = s.anchor
		}
		s.viewed = v
	}
	s.wantMore = false
	clear(s.rects)
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
			s.stale(c)
			s.content(c, now, compact)
			theme.Edge(c)
			s.toolbar(c, compact)
			s.pill(c)
		})
	})
	s.listKeys(c, open)
	if s.wantMore {
		s.loadMore()
	}
	s.keySheet(c)
	s.palette(c)
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

// listKeys registers the keys of the list and of the sidebar. They work wherever the focus is, but not while the search
// takes the letters typed, a dialog shows or the reader, which has its own, is open. They are registered after the
// frame is laid out, which is when it is known whether the focus is in the sidebar; when it came there by the keyboard
// (H, Left, Tab, the arrows) the keys of the entries are inert, as the arrows and Enter are the sidebar's own, and
// Escape, L and Right go back to the list. Focus a click put on a kind or subscription does not count: the keys of
// the entries act on the list at once.
func (s *Screen) listKeys(c *ui.Context, open bool) {
	if s.typing || open || s.reading != 0 {
		return
	}
	c.OnShortcut(0, ui.KeySlash, func() { s.searchBox = true })
	for i, k := range []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5} {
		c.OnShortcut(0, k, func() { s.pickKind(i) })
	}
	if s.inSide {
		for _, k := range []ui.Key{ui.KeyL, ui.KeyRight, ui.KeyEscape} {
			c.OnShortcut(0, k, s.toList)
		}
		return
	}
	for _, k := range []struct {
		key ui.Key
		do  func()
	}{
		{ui.KeyH, s.toSidebar}, {ui.KeyLeft, s.toSidebar},
		{ui.KeyJ, func() { s.move(1) }}, {ui.KeyDown, func() { s.move(1) }},
		{ui.KeyK, func() { s.move(-1) }}, {ui.KeyUp, func() { s.move(-1) }},
		{ui.KeyO, func() { s.open(c, s.selected) }}, {ui.KeyEnter, func() { s.open(c, s.selected) }},
		{ui.KeyS, func() { s.toggleStar(s.selected) }}, {ui.KeyM, func() { s.toggleRead(s.selected) }},
		{ui.KeyN, s.nextUnread}, {ui.KeyV, s.markVisible},
	} {
		c.OnShortcut(0, k.key, k.do)
	}
}

// pickKind shows the kind at index i of the sidebar and says which and how many unread entries it has.
func (s *Screen) pickKind(i int) {
	s.failingOnly = false
	s.choose(scope{kind: kinds[i]})
	say := s.kindName(kinds[i])
	if n := s.unreadIn(scope{kind: kinds[i]}); n > 0 {
		say += s.text.Separator + s.format(s.text.UnreadCount, n)
	}
	s.announce = say
}

// toSidebar gives the focus to the subscription chosen, else the kind chosen.
func (s *Screen) toSidebar() { s.sideFocus, s.inSide, s.sideKeyed = true, true, true }

// toList gives the focus back to the entry chosen, the first one when none is.
func (s *Screen) toList() {
	s.inSide = false
	if len(s.entries) == 0 {
		return
	}
	i := s.selected
	if i < 0 || i >= len(s.entries) {
		i = 0
	}
	s.selected, s.focusRow = i, i
	s.list.ScrollIntoView(i)
	s.grid.ScrollIntoView(i)
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
	if s.noOlder != "" {
		age := s.noOlder
		s.noOlder = ""
		c.AddToast(ui.Toast{ID: noneOlderToast, Title: strings.ReplaceAll(s.text.NoneOlder, "{age}", age),
			Data: theme.ToastLook{Icon: icons.CircleCheckBig}})
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
	if s.subs.Open() || s.pal.open {
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
	menu := [][]string{{"Shift+", "F10"}, {"Menu"}}
	if runtime.GOOS == "darwin" {
		menu = [][]string{{"⇧", "F10"}}
	}
	timeline := []key{{s.text.KeyNext, one("J", "↓")}, {s.text.KeyPrevious, one("K", "↑")},
		{s.text.KeyNextUnread, one("N")}, {s.text.KeyOpen, one("O", "↩")}, {s.text.KeyStar, one("S")},
		{s.text.KeyRead, one("M")}, {s.text.KeyMarkVisible, one("V")},
		{s.text.KeySearch, [][]string{{"/"}, {cmd, "F"}}},
		{s.text.KeyEscape, one("Esc")}, {s.text.KeyScope, one("1–5")}, {s.text.KeySidebar, one("H", "←")},
		{s.text.KeyBack, one("L", "→", "Esc")}, {s.text.KeyMenu, menu}, {s.text.KeyUndo, [][]string{{cmd, "Z"}}}}
	reading := []key{{s.text.KeyNextArticle, one("J")}, {s.text.KeyPreviousArticle, one("K")},
		{s.text.KeyNextUnread, one("N")}, {s.text.KeyStar, one("S")}, {s.text.KeyRead, one("M")},
		{s.text.KeyOpenBrowser, one("O")}, {s.text.KeyBack, one("Esc")}}
	anywhere := []key{{s.text.KeyPalette, [][]string{{cmd, "K"}}}, {s.text.KeyAdd, [][]string{{cmd, "N"}}}, {s.text.KeySettings, [][]string{{cmd, ","}}},
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
	// The focus is moving to the sidebar or to an entry in this frame, which FocusWithin does not show yet. Focus that
	// a click put on an item does not count: the pointer chose, and the keys of the entries still act on the list.
	goingIn := s.sideFocus
	c.SetTheme(theme.PaneTheme(main))
	defer c.SetTheme(main)
	pane.Children(func() {
		t := c.Theme()
		// The first 40 are the window buttons' and drag the window.
		ui.Box(c).FillWidth().Height(40).DragWindow()
		onSource := slices.ContainsFunc(s.sources, func(src client.Source) bool {
			return src.ID == s.scope.source && (!s.failingOnly || failed(src))
		})
		s.tabs(c, s.sideFocus && !onSource)
		list := s.sources
		if s.scope.kind != "" {
			list = nil
			for _, src := range s.sources {
				if src.Kind == s.scope.kind {
					list = append(list, src)
				}
			}
		}
		failing := 0
		for _, src := range list {
			if failed(src) {
				failing++
			}
		}
		if failing == 0 {
			s.failingOnly = false
		}
		if s.failingOnly {
			list = slices.DeleteFunc(slices.Clone(list), func(src client.Source) bool { return !failed(src) })
		}
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(px(t, 4), px(t, 8), px(t, 6)).Label(s.text.Sources).Children(func() {
				s.section(c, failing)
				ui.Column(c).FillWidth().FocusGroup(ui.Vertical).Children(func() {
					for _, src := range list {
						s.sourceItem(c, src)
					}
				})
			})
		})
		s.serverBar(c)
		s.sideFocus = false
	})
	s.inSide = goingIn || (pane.FocusWithin() && s.focusRow < 0 && s.sideKeyed)
}

// tabs are the kinds at the top of the sidebar, a list of an icon, a name and the unread count each; Up and Down move
// between them, and Left and Right are left to go back to the list. focus gives the focus to the one chosen. Names are
// Text2, the chosen one Text in weight 500; counts are TextMuted and step to Text2 under the pointer and when chosen;
// the icon is TextMuted, Text2 under the pointer and the Accent when chosen.
func (s *Screen) tabs(c *ui.Context, focus bool) {
	t := c.Theme()
	before := s.tab
	seg := ui.SegmentedBase(c, &s.tab, len(kinds))
	seg.Track.Column().FocusGroup(ui.Vertical).FillWidth().Gap(px(t, 1)).Padding(0, px(t, 8), px(t, 8)).Label(s.text.Kinds).
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
				if focus && on {
					tab.Focus()
				}
				s.sideKeyed = s.sideKeyed || tab.FocusVisible()
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
					s.sideKeyed = tab.FocusVisible()
					s.tab = i
					s.failingOnly = false
					s.back()
				}
			}
		})
	if s.tab != before || seg.Track.Changed() {
		s.failingOnly = false
		s.choose(scope{kind: kinds[s.tab]})
	}
}

// failed reports whether the last fetch of src failed; a paused subscription shows its pause instead.
func failed(src client.Source) bool { return src.LastError != "" && !src.Paused }

// section is the header of the subscriptions in the sidebar, with the button adding one. While failing
// subscriptions are listed it ends in how many, a button that narrows the list to them until Show all.
func (s *Screen) section(c *ui.Context, failing int) {
	t := c.Theme()
	name := s.text.Sources
	if s.scope.kind != "" {
		name = s.kindName(s.scope.kind)
	}
	if s.failingOnly {
		name = s.text.FailingTitle
	}
	size, tracking := px(t, 11), px(t, 11)*0.04
	if theme.Han(name) {
		size, tracking = px(t, 12), px(t, 12)*0.02
	}
	ui.Row(c).FillWidth().Height(px(t, 22)).AlignItems(ui.Center).Margin(px(t, 14), 0, px(t, 4), px(t, 10)).
		Children(func() {
			ui.Text(c, name).Grow(1).MinWidth(0).SingleLine().FontSize(size).FontWeight(500).
				LetterSpacing(tracking).TextColor(t.TextMuted)
			switch {
			case s.failingOnly:
				if s.link(c, nil, s.text.ShowAll, s.text.ShowAll, theme.Text2(t)) {
					s.failingOnly = false
				}
			case failing > 0:
				show := s.format(s.text.FailingShow, failing)
				if s.link(c, icons.CircleAlert, s.format(s.text.FailingCount, failing), show, t.Warning) {
					s.failingOnly = true
				}
			}
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

// link is a small text button in the sidebar's section header, with an optional icon; it reports a click.
func (s *Screen) link(c *ui.Context, icon *ui.SVG, text, label string, color ui.Color) bool {
	t := c.Theme()
	b := ui.ButtonBase(c).Height(px(t, 20)).Gap(px(t, 4)).PaddingX(px(t, 6)).Radius(px(t, 6)).Label(label).
		Tooltip(label)
	if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	b.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).FontSize(px(t, 12)).TextColor(color)
		}
		ui.Text(c, text).SingleLine().FontSize(px(t, 11)).FontWeight(500).TextColor(color)
	})
	return b.Clicked()
}

func (s *Screen) sourceItem(c *ui.Context, src client.Source) {
	t := c.Theme()
	chosen := s.scope.source == src.ID
	name := subscriptions.Name(src)
	label := name
	if failed(src) {
		label = s.failingName(name, src.UnreadCount)
	}
	item := ui.ButtonBase(c).Key(src.ID).FillWidth().Height(px(t, 30)).Gap(px(t, 10)).PaddingX(px(t, 10)).
		Radius(px(t, 8)).Label(label).ContextMenu(func(m *ui.Menu) { s.subs.Menu(m, src) })
	// MyGo's tooltip cannot be asked to show only when the text is cut, so the full name is always the tip.
	item.Tooltip(name)
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
	if chosen && s.sideFocus {
		item.Focus()
	}
	s.sideKeyed = s.sideKeyed || item.FocusVisible()
	item.Children(func() {
		s.glyph(c, name, src.IconURL, 18)
		color := theme.Text2(t)
		if src.Paused {
			color = t.TextMuted
		}
		title := ui.Text(c, name).Grow(1).MinWidth(0).SingleLine().TextColor(color)
		if chosen {
			title.TextColor(t.Text).FontWeight(500)
		}
		if src.Paused {
			ui.Box(c).Label(s.text.PausedMark).Tooltip(s.text.PausedMark).Role(ui.RoleImage).Children(func() {
				ui.Icon(c, icons.Pause).FontSize(px(t, 14)).TextColor(t.TextMuted)
			})
		} else if src.LastError != "" {
			reason := s.reason(src)
			ui.Box(c).Label(reason).Tooltip(reason).Role(ui.RoleImage).Children(func() {
				ui.Icon(c, icons.CircleAlert).FontSize(px(t, 14)).TextColor(t.Warning)
			})
		}
		if src.UnreadCount > 0 && !src.Paused {
			ui.Text(c, strconv.Itoa(src.UnreadCount)).FontSize(px(t, 11)).FontWeight(500).FontFeatures("tnum").
				TextColor(count)
		}
	})
	if item.Clicked() {
		s.sideKeyed = item.FocusVisible()
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
		dot := t.Success
		if s.offline {
			dot = t.Warning
		}
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
			ui.Box(c).Size(6, 6).Radius(3).Background(dot)
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
	gap := px(t, 10)
	if compact {
		gap = px(t, 4)
	}
	// Keyed, so the search field keeps its focus when the hint above it in the column comes and goes.
	ui.Row(c).Key("toolbar").Absolute().Top(0).Left(0).Right(0).Height(theme.Bar).AlignItems(ui.Center).Gap(gap).
		Padding(0, t.Space(3)+bar.Right, 0, t.Space(5)).DragWindow().Children(func() {
		ui.Text(c, s.scopeName()).FontSize(px(t, 15)).FontWeight(600).SingleLine().MinWidth(px(t, 56)).Shrink(1).
			Role(ui.RoleHeading).Level(1)
		sub := ""
		switch {
		case s.searched != "" && !s.loading && !s.loadErr:
			sub = s.format(s.text.Results, len(s.entries))
			if s.next != "" {
				sub = s.format(s.text.MoreResults, len(s.entries))
			}
		case s.searched == "" && s.budget == 0 && s.sourcesLoaded:
			if n := s.unreadIn(s.scope); n > 0 {
				sub = s.format(s.text.UnreadCount, n)
			}
		}
		// At the minimum width the count gives way before the title does.
		if sub != "" && !compact {
			ui.Text(c, sub).FontSize(px(t, 12)).FontFeatures("tnum").TextColor(t.TextMuted).NoWrap()
		}
		s.editButton(c)
		ui.Box(c).Grow(1)
		s.filters(c, compact)
		s.budgetButton(c, compact)
		s.gridButton(c)
		s.searchField(c, compact)
		s.markAllButton(c)
	})
}

func (s *Screen) filters(c *ui.Context, compact bool) {
	t := c.Theme()
	before := s.filter
	seg := ui.SegmentedBase(c, &s.filter, 3)
	theme.Capsule(c, seg.Track.Height(px(t, 32)).Shrink(0).Padding(px(t, 3)).Gap(2).Radius(px(t, 16)), false).Label(s.text.Show).
		Children(func() {
			for i, item := range []struct {
				icon *ui.SVG
				name string
			}{{icons.Inbox, s.text.Everything}, {icons.CircleDot, s.text.Unread}, {icons.Star, s.text.Starred}} {
				on := i == s.filter
				pad := px(t, 11)
				if compact {
					pad = px(t, 7)
				}
				b := seg.Segment(i).Height(px(t, 26)).Gap(px(t, 4)).PaddingX(pad).Radius(px(t, 13)).
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
	if after := s.filter; after != before {
		s.filter = before
		s.show(func() { s.filter = after })
	}
}

// budgetButton is the reading-time button: a bare clock icon, or with a budget a pill saying "≤ 10 min" (only the
// icon and a dot when narrow). Its menu lists the budgets; choosing one reloads the list from the top.
func (s *Screen) budgetButton(c *ui.Context, compact bool) {
	t := c.Theme()
	name := s.budgetName()
	on := s.budget != 0
	b := ui.ButtonBase(c).Key("budget").Height(px(t, 32)).Shrink(0).Radius(px(t, 16)).Label(name).Tooltip(name)
	color := toolFill(t, b)
	if on {
		color = t.Accent
		fill := theme.Soft(t, t.Accent)
		if b.Hovered() || b.Pressed() {
			fill = fill.Mix(t.Accent, 0.12)
		}
		b.Background(fill)
	}
	if on && !compact {
		b.Gap(px(t, 5)).Padding(0, px(t, 12), 0, px(t, 10))
	} else {
		b.Width(px(t, 32))
	}
	b.Children(func() {
		ui.Icon(c, icons.Clock).FontSize(px(t, 16)).TextColor(color)
		if on && !compact {
			ui.Text(c, strings.ReplaceAll(s.text.TimeChip, "{n}", strconv.Itoa(s.budget))).FontSize(px(t, 12)).
				FontWeight(500).FontFeatures("tnum").NoWrap().TextColor(t.Accent)
		}
		if on && compact {
			ui.Box(c).Absolute().Top(px(t, 4)).Right(px(t, 4)).Size(px(t, 7), px(t, 7)).Radius(px(t, 3.5)).
				Background(t.Accent)
		}
	})
	b.Menu(func(m *ui.Menu) {
		if m.Item(s.text.TimeAny).Checked(s.budget == 0).Chosen() {
			s.setBudget(0)
		}
		for _, n := range budgets {
			label := strings.ReplaceAll(s.text.TimeUpTo, "{n}", strconv.Itoa(n))
			if m.Item(label).Checked(s.budget == n).Chosen() {
				s.setBudget(n)
			}
		}
	})
}

// gridButton switches the list between rows and a grid: a bare 32 square icon button, the grid icon while the list
// shows rows and the list icon while it shows the grid, named by what it does. It is offered only where a grid is.
func (s *Screen) gridButton(c *ui.Context) {
	if !s.gridable() {
		return
	}
	t := c.Theme()
	icon, name := icons.LayoutGrid, s.text.ShowGrid
	if s.gridOn() {
		icon, name = icons.List, s.text.ShowList
	}
	b := ui.ButtonBase(c).Key("grid").Size(px(t, 32), px(t, 32)).Shrink(0).Radius(px(t, 16)).Label(name).Tooltip(name)
	color := toolFill(t, b)
	b.Children(func() { ui.Icon(c, icon).FontSize(px(t, 16)).TextColor(color) })
	if b.Clicked() {
		s.switchGrid()
	}
}

// setBudget limits the list to entries that take at most n minutes, 0 for any length, and shows it from the top.
func (s *Screen) setBudget(n int) {
	if n == s.budget {
		return
	}
	s.show(func() { s.budget = n })
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
	box := ui.Row(c).Key("search").Width(width).MinWidth(px(t, 72)).Shrink(100).Height(px(t, 32)).Gap(px(t, 6)).Padding(0, px(t, 6), 0, px(t, 12)).
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
	b := ui.ButtonBase(c).Size(px(t, 32), px(t, 32)).Shrink(0).Radius(px(t, 8)).Label(s.text.EditSource).
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
	b := ui.ButtonBase(c).Size(px(t, 32), px(t, 32)).Shrink(0).Radius(px(t, 8)).Label(label).Tooltip(label).Disabled(!can)
	color := theme.Disabled(t)
	if can {
		color = toolFill(t, b)
	}
	b.Children(func() { ui.Icon(c, icons.CheckCheck).FontSize(px(t, 16)).TextColor(color) })
	if b.Clicked() {
		s.markAll()
	}
	b.ContextMenu(func(m *ui.Menu) {
		if m.Item(label).Disabled(!can).Chosen() {
			s.markAll()
		}
		m.Separator()
		for _, days := range olderDays {
			text, _ := s.olderText(days)
			if m.Item(text).Disabled(!can).Chosen() {
				s.markAllOlder(days)
			}
		}
	})
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

// staleHeight is the height of the line that says the server is out of reach.
const staleHeight = 28

// staleShown tells whether the line that says the server is out of reach shows under the toolbar.
func (s *Screen) staleShown() bool {
	return s.offline && !s.loadErr && !s.loading
}

// stale is the quiet line under the toolbar while background checks fail, lined up with the list.
func (s *Screen) stale(c *ui.Context) {
	if !s.staleShown() {
		return
	}
	t := c.Theme()
	top := float32(theme.Bar)
	if _, short := s.trimmed(); short {
		top = 4
	}
	row := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(px(t, 6)).Height(staleHeight).Role(ui.RoleStatus)
	if s.inColumn() {
		maxWidth := px(t, 680)
		if s.scope.kind == "social" {
			maxWidth = px(t, 720)
		}
		row.MaxWidth(maxWidth).Margin(top, ui.Auto, 0).Padding(0, px(t, 20), 0, px(t, 36))
	} else {
		row.Margin(top, 0, 0).Padding(0, px(t, 20))
	}
	row.Children(func() {
		ui.Icon(c, icons.WifiOff).FontSize(px(t, 14)).TextColor(t.Warning)
		ui.Text(c, s.staleText()).FontSize(px(t, 12)).FontFeatures("tnum").TextColor(t.TextMuted).SingleLine().
			MinWidth(0).Shrink(1)
	})
}

// inColumn tells whether the entries sit in the centered column, not in the video grid or the picture wall.
func (s *Screen) inColumn() bool {
	return s.layout() == columnLayout
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
			case s.budget != 0:
				s.empty(c, icons.Clock, strings.ReplaceAll(s.text.NothingFits, "{n}", strconv.Itoa(s.budget)),
					s.text.NothingFitsHint, false)
			case s.filter == unread:
				s.empty(c, icons.CircleCheckBig, s.text.UnreadDone, s.text.UnreadDoneHint, false)
			case s.filter == starred:
				s.empty(c, icons.Star, s.text.NoStarred, s.text.NoStarredHint, false)
			default:
				s.empty(c, icons.Inbox, s.text.Empty, s.text.EmptyHint, false)
			}
		case s.layout() == gridLayout:
			s.cells(c, now, compact, false)
		case s.layout() == squareLayout:
			s.cells(c, now, compact, true)
		case s.layout() == wallLayout:
			s.wallOf(c, now, compact)
		default:
			s.rows(c, now, compact)
		}
	})
	s.toTop, s.pin = false, 0
	s.anchor = s.firstInView()
}

// firstInView is the first entry that shows in the list area, as the last draw placed the entries, 0 for none.
func (s *Screen) firstInView() int64 {
	first := -1
	for i, r := range s.rects {
		if i < len(s.entries) && r.H > 0 && r.Y+r.H > s.top() && r.Y < s.height && (first < 0 || i < first) {
			first = i
		}
	}
	if first < 0 {
		return 0
	}
	return s.entries[first].ID
}

// top is how far down the content starts: below the toolbar row, which it scrolls under, or below the hint, which
// already sits below the row. The line that says the server is out of reach adds its own height.
func (s *Screen) top() float32 {
	top := float32(theme.Bar)
	if _, short := s.trimmed(); short {
		top = 0
	}
	if s.staleShown() {
		top += staleHeight
	}
	return top
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
		if s.budget != 0 && !failed && icon == icons.Clock {
			b := ui.ButtonBase(c).Height(px(t, 32)).PaddingX(px(t, 14)).Radius(px(t, 10)).Background(theme.Track(t))
			if b.Pressed() {
				b.Background(theme.Track(t).Mix(theme.Ink(t, 1), 0.16))
			} else if b.Hovered() {
				b.Background(theme.Track(t).Mix(theme.Ink(t, 1), 0.1))
			}
			theme.Fade(b)
			b.Children(func() { ui.Text(c, s.text.AnyLength).NoWrap().FontWeight(500) })
			if b.Clicked() {
				s.setBudget(0)
			}
		}
		if title == s.text.NoSubscriptions {
			ui.Row(c).Wrap().Justify(ui.Center).Gap(t.Space(2)).Children(func() {
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
				i := ui.ButtonBase(c).Height(t.Space(8)).Gap(t.Space(1.5)).PaddingX(t.Space(3.5)).Radius(t.Radius).
					Background(theme.Track(t))
				if i.Pressed() {
					i.Background(theme.Track(t).Mix(theme.Ink(t, 1), 0.16))
				} else if i.Hovered() {
					i.Background(theme.Track(t).Mix(theme.Ink(t, 1), 0.1))
				}
				theme.Fade(i)
				i.Children(func() {
					ui.Icon(c, icons.Upload).FontSize(px(t, 16)).TextColor(t.Text)
					ui.Text(c, s.text.ImportOPML).NoWrap().FontWeight(500)
				})
				if i.Clicked() {
					s.deps.OpenImport()
				}
			})
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
	e.Focusable().FocusRing(false).Label(s.label(entry, now)).ContextMenu(func(m *ui.Menu) { s.entryMenu(c, m, entry.ID) })
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
	s.rects[i] = e.Bounds()
	if i >= len(s.entries)-nearEnd {
		s.wantMore = true
	}
	return hovered, chosen
}

// entryMenu fills the context menu of the entry id: the actions of its row, and "Mark above as read" under them.
func (s *Screen) entryMenu(c *ui.Context, m *ui.Menu, id int64) {
	i := s.index(id)
	if i < 0 {
		return
	}
	e := s.entries[i]
	star := s.text.Star
	if e.Starred {
		star = s.text.Unstar
	}
	if m.Item(star).Chosen() {
		s.toggleStar(s.index(id))
	}
	read := s.text.MarkRead
	if e.IsRead {
		read = s.text.MarkUnread
	}
	if m.Item(read).Chosen() {
		s.toggleRead(s.index(id))
	}
	if m.Item(s.text.Open).Chosen() {
		s.browse(c, s.index(id))
	}
	m.Separator()
	if m.Item(s.text.MarkAbove).Disabled(!s.canMarkAbove(id)).Chosen() {
		s.markAbove(id)
	}
}

// actionFade fades the actions of a row in and out, opacity only.
var actionFade = ui.ElementTransition{Duration: 120 * time.Millisecond, Enter: &ui.Motion{}, Exit: &ui.Motion{}}

// place is where the actions sit on an entry.
type place int

const (
	inMeta    place = iota // in the meta line of a row, at the right
	onCard                 // at the bottom right of a card, where its meta line is
	centered               // centered vertically at the right, in place of the time of a compact row
	belowMeta              // at the right of the meta line under a large cover, at the bottom of the row
)

// actions are the bare icons of an entry, at the place given: a row's own meta line has room for them at the top,
// a card has its meta line at the bottom of the card.
func (s *Screen) actions(c *ui.Context, i int, at place) {
	t := c.Theme()
	e := s.entries[i]
	bar := ui.Row(c).Key("actions").Gap(px(t, 1)).Transition(actionFade)
	switch at {
	case onCard:
		bar.Attach(ui.AnchorBottomRight, ui.AnchorBottomRight)
	case centered:
		bar.Attach(ui.AnchorRight, ui.AnchorRight).Right(px(t, 10))
	case belowMeta:
		bar.Attach(ui.AnchorBottomRight, ui.AnchorBottomRight).Bottom(px(t, 14)).Right(px(t, 10))
	default:
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
// actions, and lead comes first, as the unread dot of a card. The source is Text2, TextMuted once read. The platform
// of a video ends it when it is asked for, as under a large cover.
func (s *Screen) meta(c *ui.Context, e client.Entry, now time.Time, lead bool, right float32, muted ui.Color,
	platform bool) {
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
			if reading := s.readLabel(e); reading != "" {
				ui.Text(c, "·").FontSize(px(t, 12)).TextColor(muted).Opacity(0.7)
				ui.Text(c, reading).FontSize(px(t, 12)).FontFeatures("tnum").TextColor(muted).NoWrap()
			}
			if e.Starred {
				ui.Icon(c, icons.Star).FontSize(px(t, 13)).TextColor(theme.Star(t))
			}
			if platform && e.Platform != "" {
				ui.Text(c, "·").FontSize(px(t, 12)).TextColor(muted).Opacity(0.7)
				ui.Icon(c, icons.Play).FontSize(px(t, 13)).TextColor(muted)
				ui.Text(c, e.Platform).FontSize(px(t, 12)).SingleLine().MinWidth(0).Shrink(1).TextColor(muted)
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
	} else if i := s.index(s.pin); s.pin != 0 && i >= 0 {
		s.list.ScrollTo(i, ui.Start)
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
// Text2 on the chosen fill. A compact article is one line, 36 high; the rest of the rows are as high as their parts.
func (s *Screen) row(c *ui.Context, i int, now time.Time, compact bool) {
	t := c.Theme()
	e := s.entries[i]
	v := s.views()
	oneLine := e.Kind == "article" && v.ArticleViewer == prefs.ViewCompact
	var r ui.Element
	switch {
	case oneLine:
		r = ui.Row(c).Key(e.ID).FillWidth().Height(px(t, 36)).AlignItems(ui.Center).Gap(px(t, 12)).
			Padding(0, px(t, 16), 0, px(t, 28)).Radius(px(t, 10))
	case e.Kind == "social" && v.SocialViewer == prefs.ViewCompact:
		r = ui.Row(c).Key(e.ID).FillWidth().AlignItems(ui.Start).Gap(px(t, 14)).
			Padding(px(t, 12), px(t, 16), px(t, 12), px(t, 28)).Radius(px(t, 12))
	default:
		r = ui.Row(c).Key(e.ID).FillWidth().AlignItems(ui.Start).Gap(px(t, 14)).
			Padding(px(t, 16), px(t, 16), px(t, 16), px(t, 28)).Radius(px(t, 12))
	}
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
	showing := hovered || chosen || r.Focused()
	r.Children(func() {
		if i > 0 && !quiet {
			ui.Box(c).Absolute().Top(0).Left(px(t, 28)).Right(px(t, 16)).Height(1).Background(t.Border)
		}
		if chosen {
			marker := float32(18)
			if oneLine {
				marker = 14
			}
			ui.Box(c).Attach(ui.AnchorLeft, ui.AnchorLeft).Left(8).Size(2.5, marker).Radius(2).
				Background(theme.Marker(t))
		}
		if !e.IsRead {
			dot := ui.Box(c).Absolute().Left(px(t, 14)).Size(px(t, 7), px(t, 7)).Radius(px(t, 3.5)).
				Background(t.Accent)
			if oneLine {
				dot.Top(px(t, 18) - px(t, 3.5))
			} else {
				dot.Top(px(t, 23))
			}
		}
		at := inMeta
		switch e.Kind {
		case "video":
			if v.VideoViewer == prefs.ViewCover {
				s.largeRow(c, e, now, muted)
				at = belowMeta
			} else {
				s.videoRow(c, e, now, compact, muted)
			}
		case "social":
			s.socialRow(c, e, now, muted, v.SocialViewer == prefs.ViewCompact)
		case "image":
			s.imageRow(c, e, now, muted)
		default:
			if oneLine {
				s.compactRow(c, e, now, compact, showing, muted)
				at = centered
			} else {
				s.articleRow(c, e, now, muted)
			}
		}
		if showing {
			s.actions(c, i, at)
		}
	})
}

// titleText is a title in 15 (13 in a card) semibold; read, it is TextMuted in weight 500. It stops at two lines.
func (s *Screen) titleText(c *ui.Context, e client.Entry, size float32, muted ui.Color) ui.Element {
	t := c.Theme()
	color, weight := t.Text, 600
	if e.IsRead {
		color, weight = muted, 500
	}
	text := ui.RichText(c, s.marked(c, title(e), color)...).FontSize(px(t, size)).FontWeight(weight).Wrap().MaxLines(2)
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
		s.meta(c, e, now, false, px(t, 88), muted, false)
		s.titleText(c, e, 15, muted)
		summary, generated := strings.Join(strings.Fields(e.Summary), " "), false
		if ai := strings.Join(strings.Fields(e.AISummary), " "); ai != "" {
			summary, generated = ai, true
		}
		if summary != "" {
			color := theme.Text2(t)
			if e.IsRead {
				color = muted
			}
			line := func() {
				text := ui.RichText(c, s.marked(c, summary, color)...).FontSize(px(t, 13)).MaxLines(2).LineHeight(1.5)
				if theme.Han(summary) {
					text.LineHeight(1.7)
				}
				if generated {
					text.Grow(1).MinWidth(0)
				}
			}
			if generated {
				// The sparkle says the text was generated; its label is how assistive technology says so.
				ui.Row(c).Gap(px(t, 6)).AlignItems(ui.Start).Children(func() {
					ui.Icon(c, icons.Sparkles).FontSize(px(t, 13)).TextColor(muted).Margin(px(t, 4), 0, 0, 0).
						Label(s.text.AISummary).Role(ui.RoleImage)
					line()
				})
			} else {
				line()
			}
		}
	})
	if e.ThumbnailURL != "" {
		// Under the meta line, so the row's actions never cover it, and never taller than the text: it starts 24 down.
		s.picture(c, e.ThumbnailURL, true, px(t, 8), nil).Size(px(t, 96), px(t, 64)).AlignSelf(ui.Start).
			Margin(px(t, 24), 0, 0, 0)
	}
}

// compactRow is an article in one line: the site icon, the source in a field of its own, the title, the star when
// starred and the time, which the actions take the place of while they show.
func (s *Screen) compactRow(c *ui.Context, e client.Entry, now time.Time, narrow, acting bool, muted ui.Color) {
	t := c.Theme()
	source, color, weight := theme.Text2(t), t.Text, 600
	if e.IsRead {
		source, color, weight = muted, muted, 500
	}
	field := px(t, 128)
	if narrow {
		field = px(t, 96)
	}
	s.glyph(c, e.SourceTitle, s.iconOf(e.SourceID), 16)
	ui.Text(c, e.SourceTitle).FontSize(px(t, 12)).FontWeight(500).SingleLine().Width(field).Shrink(0).TextColor(source)
	ui.RichText(c, s.marked(c, title(e), color)...).FontSize(px(t, 13)).FontWeight(weight).SingleLine().Grow(1).
		MinWidth(0)
	if e.Starred {
		ui.Icon(c, icons.Star).FontSize(px(t, 13)).TextColor(theme.Star(t))
	}
	when, width := ui.Color(muted), px(t, 56)
	if acting {
		when, width = ui.RGBA(0, 0, 0, 0), px(t, 84)
	}
	ui.Text(c, s.when(e, now)).FontSize(px(t, 12)).FontFeatures("tnum").TextColor(when).NoWrap().MinWidth(width).
		TextAlign(ui.End)
}

// largeRow is a video with its cover across the row, then the title and the meta line under it.
func (s *Screen) largeRow(c *ui.Context, e client.Entry, now time.Time, muted ui.Color) {
	t := c.Theme()
	ui.Column(c).Grow(1).MinWidth(0).Gap(px(t, 8)).Children(func() {
		s.coverOf(c, e, true, true, px(t, 12), 44).FillWidth().MaxWidth(px(t, 640))
		s.titleText(c, e, 15, muted)
		s.meta(c, e, now, false, px(t, 88), muted, true)
	})
}

func (s *Screen) cover(c *ui.Context, e client.Entry, load, play bool) ui.Element {
	return s.coverOf(c, e, load, play, px(c.Theme(), 10), 36)
}

// coverOf is the 16:9 cover of a video with the given corner radius; play is the diameter of the disc of its play
// mark when that shows.
func (s *Screen) coverOf(c *ui.Context, e client.Entry, load, play bool, radius, disc float32) ui.Element {
	t := c.Theme()
	label := ""
	if e.DurationSec > 0 {
		label = strings.ReplaceAll(s.text.Duration, "{d}", duration(e.DurationSec))
	}
	return s.picture(c, e.ThumbnailURL, load, radius, func() {
		if play {
			ui.Box(c).Attach(ui.AnchorCenter, ui.AnchorCenter).Size(disc, disc).Radius(disc / 2).Center().
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
		s.meta(c, e, now, false, px(t, 88), muted, false)
		s.titleText(c, e, 15, muted)
		if e.Platform != "" {
			ui.Row(c).AlignItems(ui.Center).Gap(px(t, 6)).Children(func() {
				ui.Icon(c, icons.Play).FontSize(px(t, 13)).TextColor(muted)
				ui.Text(c, e.Platform).FontSize(px(t, 12)).TextColor(muted)
			})
		}
	})
}

func (s *Screen) socialRow(c *ui.Context, e client.Entry, now time.Time, muted ui.Color, brief bool) {
	t := c.Theme()
	color, author := t.Text, t.Text
	if e.IsRead {
		color, author = muted, theme.Text2(t)
	}
	if brief {
		s.briefPost(c, e, now, muted, color, author)
		return
	}
	ui.Column(c).Grow(1).MinWidth(0).Gap(px(t, 3)).Children(func() {
		s.meta(c, e, now, false, px(t, 88), muted, false)
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

// briefPost is a post in the Compact viewer: the meta line, the author and the text in two lines at most, and the
// pictures as thumbnails: one is a square at the right of the text, two or more are a strip under it.
func (s *Screen) briefPost(c *ui.Context, e client.Entry, now time.Time, muted, color, author ui.Color) {
	t := c.Theme()
	text := strings.TrimSpace(e.Summary)
	if text == "" {
		text = e.Title
	}
	ui.Column(c).Grow(1).MinWidth(0).Gap(px(t, 4)).Children(func() {
		s.meta(c, e, now, false, px(t, 88), muted, false)
		spans := []ui.Span{{Text: e.Author, Weight: 500, Color: author}, {Text: " " + e.Platform + " ", Color: muted}}
		if e.Author == "" && e.Platform == "" {
			spans = nil
		}
		body := ui.RichText(c, append(spans, s.marked(c, text, color)...)...).FontSize(px(t, 13)).MaxLines(2).Wrap().
			LineHeight(1.55)
		if theme.Han(text) {
			body.LineHeight(1.7)
		}
		if n := len(e.Images); n > 1 {
			s.strip(c, e)
		}
	})
	if len(e.Images) == 1 {
		// Under the meta line, so the row's actions never cover it, as an article's thumbnail is.
		s.pictureOf(c, e.Images[0].URL, true, px(t, 8), 16, nil).Size(px(t, 56), px(t, 56)).AlignSelf(ui.Start).
			Margin(px(t, 24), 0, 0, 0).Label(s.format(s.text.Pictures, 1)).Role(ui.RoleImage)
	}
}

// strip is the pictures of a post as a row of at most four small squares, the last saying how many more follow.
func (s *Screen) strip(c *ui.Context, e client.Entry) {
	t := c.Theme()
	n := len(e.Images)
	shown := min(n, 4)
	ui.Row(c).Gap(px(t, 4)).Margin(px(t, 4), 0, 0, 0).Label(s.format(s.text.Pictures, n)).Role(ui.RoleImage).
		Children(func() {
			for k, img := range e.Images[:shown] {
				var over func()
				if k == shown-1 && n > shown {
					more := s.format(s.text.MorePictures, n-shown)
					over = func() {
						ui.Box(c).Fill().Absolute().Center().Background(ui.RGBA(28, 24, 18, 0.5)).Label(more).
							Children(func() {
								ui.Text(c, "+"+strconv.Itoa(n-shown)).FontSize(px(t, 11)).FontWeight(600).
									FontFeatures("tnum").TextColor(ui.Hex("#ffffff"))
							})
					}
				}
				s.pictureOf(c, img.URL, true, px(t, 6), 16, over).Key(k).Size(px(t, 40), px(t, 40))
			}
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
		s.meta(c, e, now, false, px(t, 88), muted, false)
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

// picture is a picture on the Track color, which shows until it loads or when it fails, with a muted image icon on it
// then, and a hairline outline of ink over it; load false leaves it unasked, for those far from the view. over draws
// on it.
func (s *Screen) picture(c *ui.Context, address string, load bool, radius float32, over func()) ui.Element {
	return s.pictureOf(c, address, load, radius, 20, over)
}

// pictureOf is picture with a failed mark of the given size.
func (s *Screen) pictureOf(c *ui.Context, address string, load bool, radius, mark float32, over func()) ui.Element {
	t := c.Theme()
	var bitmap *ui.Bitmap
	if load {
		bitmap = s.bitmap(address)
	} else if p := s.pictures[address]; p != nil {
		bitmap = p.bitmap
	}
	failed := s.pictures[address] != nil && s.pictures[address].failed
	return ui.Box(c).Background(theme.Track(t)).Radius(radius).Clip().Children(func() {
		if bitmap != nil {
			ui.Image(c, bitmap).Fit(ui.Cover).Fill()
		}
		if failed {
			ui.Box(c).Fill().Absolute().Center().PassThrough().Children(func() {
				ui.Icon(c, icons.Image).FontSize(px(t, mark)).TextColor(t.TextMuted)
			})
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

// cells is the Videos tab, a grid of covers, or with square the Images tab as a grid of square pictures.
func (s *Screen) cells(c *ui.Context, now time.Time, compact, square bool) {
	t := c.Theme()
	s.grid.Key = func(i int) any { return s.entries[i].ID }
	s.grid.Label = func(i int) string { return s.label(s.entries[i], now) }
	if s.toTop {
		s.grid.ScrollTo(0, ui.Start)
	} else if i := s.index(s.pin); s.pin != 0 && i >= 0 {
		s.grid.ScrollTo(i, ui.Start)
	}
	gap, minWidth := px(t, 14), px(t, 200)
	if square {
		minWidth = px(t, 170)
	}
	_, cell := columns(c, compact, minWidth, gap, 0)
	height := cell*9/16 + px(t, 13.5)*2.9 + px(t, 18) + t.Space(3) + px(t, 20)
	if square {
		height = cell + px(t, 13)*1.5 + px(t, 20) + t.Space(3)
	}
	ui.GridView(c, &s.grid, len(s.entries), minWidth, height, func(i int) {
		e := s.entries[i]
		card := ui.Column(c).Fill().Gap(px(t, 8)).Radius(px(t, 10))
		if square {
			card.Gap(px(t, 4))
		}
		hovered, chosen := s.item(c, card, i, now, true)
		card.Children(func() {
			if square {
				s.square(c, e)
			} else {
				s.cover(c, e, true, hovered).FillWidth()
				s.titleText(c, e, 13, t.TextMuted)
			}
			s.meta(c, e, now, true, 0, t.TextMuted, false)
			if hovered || chosen || card.Focused() {
				s.actions(c, i, onCard)
			}
		})
	}).Grow(1).FillWidth().Gap(gap).Padding(s.top()+t.Space(4), px(t, 24), t.Space(6), px(t, 24)).
		ScrollbarInsets(s.top(), 0, 0, 0).Label(s.scopeName())
	if s.hasFooter() {
		s.footer(c)
	}
}

// square is the picture of an entry in the Images grid, square before it arrives, with the count of its pictures
// when there are several, and the title in one line under it.
func (s *Screen) square(c *ui.Context, e client.Entry) {
	t := c.Theme()
	label := title(e)
	var over func()
	if n := len(e.Images); n > 1 {
		label += s.text.Separator + s.format(s.text.Pictures, n)
		over = func() {
			ui.Row(c).Attach(ui.AnchorTopRight, ui.AnchorTopRight).Top(px(t, 6)).Right(px(t, 6)).
				AlignItems(ui.Center).Gap(px(t, 3)).Padding(px(t, 1), px(t, 6), px(t, 1), px(t, 5)).Radius(5).
				Background(ui.RGBA(28, 24, 18, 0.62)).Children(func() {
				ui.Icon(c, icons.Images).FontSize(px(t, 11)).TextColor(ui.Hex("#ffffff"))
				ui.Text(c, strconv.Itoa(n)).FontSize(px(t, 11)).FontWeight(500).FontFeatures("tnum").
					TextColor(ui.Hex("#ffffff"))
			})
		}
	}
	s.picture(c, firstPicture(e), true, px(t, 10), over).FillWidth().AspectRatio(1).Label(label).Role(ui.RoleImage)
	color, weight := t.Text, 600
	if e.IsRead {
		color, weight = t.TextMuted, 500
	}
	ui.Text(c, title(e)).FontSize(px(t, 13)).FontWeight(weight).SingleLine().MinWidth(0).TextColor(color)
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
	if i := s.index(s.pin); s.pin != 0 && !s.toTop && i >= 0 {
		for k := range cols {
			if j := slices.Index(cols[k], i); j >= 0 {
				s.wall.Y = max(tops[k][j], 0)
			}
		}
	}
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
							s.rects[i] = ui.Rect{Y: at - s.wall.Y, H: s.rects[i].H}
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
		s.meta(c, e, now, true, 0, t.TextMuted, false)
		if hovered || chosen || tile.Focused() {
			s.actions(c, i, onCard)
		}
	})
}
