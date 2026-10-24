// Package timeline is the main screen: the entries of every subscription in one list, newest first, each kind drawn
// in its own way; see design/screens/timeline.html. Everything it shows comes through the HTTP API.
package timeline

import (
	"cmp"
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
	"github.com/yangflow/oriel/internal/search"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/pictures"
	"github.com/yangflow/oriel/internal/ui/reader"
	"github.com/yangflow/oriel/internal/ui/subscriptions"
	"github.com/yangflow/oriel/internal/ui/theme"
)

//go:embed copy.json
var catalog []byte

type copy struct {
	DropTitle, DropHint, DropBusy string

	AlsoChip, AlsoName, AlsoHead                                                                                                                                        string
	All, Article, Video, Social, Image, Kinds, Sources, Everything, Unread, Read, Starred, Leaving, Show                                                                string
	Search, SearchLabel, Clear, UnreadCount, Results, MoreResults, MarkAllIn, Star, Unstar, MarkRead                                                                    string
	MarkUnread, Open, Blocked, BlockedPublic, FeedRemoved, FailedOnce, FailedTimes, NewEntries, Offline, Loading, LoadingMore, LoadMoreFailed, AllLoaded                string
	SyntaxLead, SyntaxFrom, SyntaxPhrase, SyntaxExclude                                                                                                                 string
	Limited, NoResults, NoResultsHint, UnreadDone, UnreadDoneHint, NoStarred, NoStarredHint                                                                             string
	Empty, EmptyHint, NoSubscriptions, NoSubscriptionsHint, Error, ErrorHint, Retry, Marked                                                                             string
	Starring, Unstarring, ActionFailed, OpenFailed, ThisComputer, Server, Settings, Duration                                                                            string
	MorePictures, Pictures, AISummary, Separator                                                                                                                        string
	AddSource, EditSource, AddFirst, ImportOPML, RestoreBackup, PausedMark, Undo, Dismiss, NoMoreUnread, KeysTitle, KeysMenu, HelpMenu                                  string
	Close, Or, InTimeline, InReader, Anywhere, KeyNext, KeyPrevious, KeyNextUnread, KeyOpen, KeyStar                                                                    string
	KeyRead, KeyOpenBrowser, KeySearch, KeyEscape, KeyUndo, KeyNextArticle, KeyPreviousArticle                                                                          string
	KeyFind, KeyImmersive, KeyImmersiveExit, PaletteExitImmersive, ImmersiveOn, ImmersiveOff                                                                            string
	KeyBack, KeyAdd, KeyAddContinue, KeySettings, KeyKeys, FailingName, FailingUnread, FailingCount, FailingShow                                                        string
	FailingTitle, ShowAll, MarkAbove, MarkedAbove, MarkBelow, MarkedBelow, MarkedVisible, MarkedScrolled, KeyMarkVisible, KeyScope, KeySidebar, KeyMenu                 string
	TimeAny, TimeUpTo, TimeChip, TimeNameAny, TimeNameUpTo, ReadingTime, NothingFits, NothingFitsHint, AnyLength                                                        string
	ShowGrid, ShowList, KeyPalette, PaletteTitle, PaletteGoTo, PaletteOpenSettings, PaletteHealth, PaletteManage, PaletteActions, PaletteUnreadOnly, PaletteShowAll     string
	PaletteNone, PaletteCount, PaletteCountOne, PageAppearance, PageFetching, PageImport, PageAI, PageDevices, PageActivity, HideSidebar, ShowSidebar, KeyToggleSidebar string
	MarkOlder1, MarkOlder7, MarkOlder30, MarkedOlder, NoneOlder, Age1, Age7, Age30                                                                                      string
	FilterSources, ClearFilter, UnreadFirst, NoMatchingSources                                                                                                          string
	CopyLink, CopyMarkdown, SaveMarkdown                                                                                                                                string
	LastLooked                                                                                                                                                          string
	LbTitle, LbPicture, LbPrev, LbNext, LbOpenPicture, LbFailed, KeyPictures                                                                                            string
	OrderNewest, OrderOldest, OrderOldestNote, NameJoin                                                                                                                 string
	TimelineMenu, KeyUnread, KeyAll, KeyStarred, KeyMarkAll, KeyLoadNew                                                                                                 string
	RefreshAll, RefreshingName, RefreshingAnnounce, Refreshed, RefreshedFailed, RefreshedPart, RefreshNone, RefreshFailed, SubscriptionsMenu                            string
	Retrying, Pending                                                                                                                                                   string
	Fetching, FetchedEntries, FetchedEntry, FetchedNone, FetchFailed, FreshEntries, FreshEntry, NothingNewIn                                                            string
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
	lookEvery    = 15 * time.Second // how often the last-looked marker is kept while the list rests at the top
	maxCount     = 999              // the most an unread count shows in the sidebar and the palette; more read "999+"
	maxNewCount  = 50               // the most new entries the divider counts; more show as "50+"
	pollEvery    = 60 * time.Second // how often the sources and the first page are checked for new entries
	checkGap     = 10 * time.Second // the least time between a check and one asked for by the window coming to the front
	watchEvery   = 3 * time.Second  // how often a subscription just added or refreshed is checked for its fetch
	watchFor     = 60 * time.Second // how long it is waited for
	debounce     = 300 * time.Millisecond
	refreshEvery = 2 * time.Second // how often the progress of "refresh all" is read
	refreshLimit = 3 * time.Minute // how long it counts before it stops waiting for the rest
	keepPictures = 300             // decoded pictures kept in memory
	wallAhead    = 800             // how far beyond the view the image wall loads pictures and the next page
)

// retryAfter is how long to wait to check again after the first, second, third and later failures in a row while the
// server is out of reach; the last one repeats.
var retryAfter = []time.Duration{3 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second}

// kinds are the tabs of the sidebar, "" for all.
var kinds = []string{"", "article", "video", "social", "image"}

// API is what the screen reads and writes on the server; client.Session is one.
type API interface {
	subscriptions.API
	reader.API
	Entries(ctx context.Context, q client.Query) (client.Page, error)
	Sources(ctx context.Context) (client.SourceList, error)
	SetEntry(ctx context.Context, id int64, patch client.EntryPatch) (client.Entry, error)
	MarkRead(ctx context.Context, scope client.ReadScope) (client.Marked, error)
	MarkUnread(ctx context.Context, m client.Marked) ([]int64, error)
	RefreshAll(ctx context.Context) (client.RefreshResult, error)
}

// Deps is what the screen does outside itself. Go runs work away from the UI thread and then the function it
// returns on the UI thread. Now is the clock and Decode turns the bytes of a picture into a bitmap, off the UI thread;
// they default to time.Now and ui.DecodeBitmap. OpenConnect shows the connect screen, from the server bar,
// OpenSettings the settings, from the gear beside it, OpenAISettings the settings on the AI group, from the entry
// points of summaries and translations while AI is not set up, OpenImport the settings on the import and export
// group, from the empty timeline of a first run, OpenSettingsPage the settings on the group called page
// ("appearance", "fetching", "opml", "ai", "devices" or "activity"), from the command palette, and Expired the connect screen
// because the server no longer takes the session. Reading gives the reading preferences of this device, for the reader, and ReadingFont the
// font list of the article's body that they come to, "" for the interface font. Change keeps preferences the
// screen changed itself, the list or grid of a scope. Save asks where to save a file, for the reader's export.
type Deps struct {
	Go               func(work func() (apply func()))
	Now              func() time.Time
	Decode           func([]byte) (*ui.Bitmap, error)
	OpenConnect      func()
	OpenSettings     func()
	OpenAISettings   func()
	OpenImport       func()
	DropFiles        func(paths []string)
	OpenSettingsPage func(page string)
	Expired          func()
	Reading          func() prefs.Preferences
	ReadingFont      func() string
	Change           func(prefs.Preferences)
	Save             func(name, filter string) (string, error)
	// Active tells whether the window has the focus and shows this screen; nil means it does. The marker of what the
	// reader last looked at moves only while it is true.
	Active func() bool
	// Look keeps the marker of what the reader last looked at, in the background. nil keeps nothing.
	Look func(prefs.Marker)
	// Remember keeps the scope and filter the timeline was left in on a server (its host, "" for this computer), in
	// the background. nil keeps nothing.
	Remember func(server string, place prefs.Place)
	// Unread is told the unread entries of All as the server counts them, each time the count changes, for the Dock
	// badge and the tray icon. nil is told nothing.
	Unread func(n int)
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

// marking is a "mark as read" (all, above or a subscription) that can be undone: the entries it marked, and the text of its toast, once shown.
type marking struct {
	client.Marked
	text  string
	shown bool
}

// maxUndo is the most entries one undo takes: the server refuses more, so a bigger mark as read offers no Undo.
const maxUndo = 10000

// keepLists is how many lists the screen keeps to draw again at once.
const keepLists = 8

// skeletonDelay is how long the first page may take before the skeleton is drawn; a switch that answers sooner shows
// no loading state at all.
const skeletonDelay = 300 * time.Millisecond

// listKey is what defines a list: the scope, the filter and the reading-time budget. A search is not kept.
type listKey struct {
	scope  scope
	filter int
	budget int
	oldest bool // oldest first
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
	refreshedToast = "timeline.refreshed"
	fetchedToast   = "timeline.fetched." // followed by the subscription's ID
)

// watch is a subscription added or refreshed whose fetch is waited for: its last fetch and unread count as they were,
// and when to give up.
type watch struct {
	fetchedAt int64
	unread    int
	added     bool
	until     time.Time
}

// fetchNote is the result of a fetch, to show as a toast once.
type fetchNote struct {
	id     int64
	text   string
	failed bool
}

// Screen keeps the timeline: the scope, the entries loaded, and the work under way.
type Screen struct {
	lang   string // the language of the screen
	hours  int    // the hour cycle of the system, 12 or 24, 0 while unknown
	deps   Deps
	text   copy
	api    API
	server string // the host of the server, "" for this computer
	gen    int    // counts servers used, so the results of the one before are dropped

	sources       []client.Source
	sourcesGen    uint64 // counts the changes to sources, to tell when side is out of date
	side          sideCache
	failingOnly   bool // the sidebar lists only the subscriptions whose last fetch failed
	sourcesLoaded bool
	// unreadTotal is the unread entries of All as the server counts them: an article several sources carry is one.
	unreadTotal int
	scope       scope
	tab         int  // the tab of scope.kind
	filter      int  // everything, unread or starred
	budget      int  // the most minutes an entry may take to read, 0 for any length; not kept between servers
	oldest      bool // the list runs oldest first, as the preferences of this device choose
	restoring   bool // the scope is a subscription kept from the last run, to check when the sources load

	query      string    // as typed
	searched   string    // the search in effect, trimmed
	termsOf    string    // the search termList was read from
	termList   []string  // the words and phrases of the search, to mark
	editedAt   time.Time // when the query last changed, for the debounce
	editing    bool
	searchBox  bool // give the search field the focus in the next frame
	focusSeen  bool // the search field had the focus in the last frame
	searchOpen bool // the search icon of a narrow toolbar was opened and stays a field until it loses the focus
	typing     bool // the search field has the focus
	clearing   bool // Escape cleared the search field

	sideQuery  string // the sidebar's filter of subscriptions, as typed; not kept between runs
	filtering  bool   // the sidebar's filter field has the focus
	firstFocus bool   // give the first matching subscription the focus in the next frame

	entries  []client.Entry
	next     string    // the cursor of the next page, "" at the end
	loading  bool      // the first page
	loadedAt time.Time // when the first page was asked for; the skeleton waits skeletonDelay after it
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
	failures int       // checks that failed in a row; picks the wait before the next one
	queue    []waiting // star and read changes the server did not get, kept in memory and sent when it is back
	sending  bool      // the queue is being sent
	updated  time.Time // the last load or check that succeeded
	fresh    int       // entries newer than the top one, found by polling

	lb        lightbox        // the pictures of a social or image entry, over the window
	stay      stay            // how long the entry on show has stayed, for "After a short stay"
	pass      passing         // the entries scrolled past, for "After scrolling past"
	selected  int             // the entry chosen, -1 for none
	focusRow  int             // the entry to give the focus in the next frame, -1 for none
	sideFocus bool            // give the sidebar the focus in the next frame
	inSide    bool            // the focus is in the sidebar, or goes there
	sideKeyed bool            // the focus in the sidebar came by the keyboard; a click on a kind or subscription clears it
	copyID    int64           // the copy of the article read that the reader shows, 0 for the row's own
	also      int64           // the entry whose other sources show under it, 0 for none
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
	scrolls   [2]ui.ScrollState // how far the rows and the grid cells are scrolled, as the last frame left them
	edge      bool              // the content shown is scrolled away from its top, which gives the toolbar its edge

	pics *pictures.Queue // the pictures asked for, the latest asked first; its downloads are cancelled when the server changes

	base      prefs.Marker     // where the divider stands: the marker as it was when the list was loaded
	divider   int              // the entry the divider was drawn above in the last frame, -1 for none
	away      bool             // the list was scrolled off the top since the divider showed
	dismissed map[listKey]bool // the lists whose divider was dismissed
	seen      prefs.Marker     // the newest entry seen at the top of the list, kept at the next write
	atTop     bool             // the last frame was in the state that moves the marker
	lookedAt  time.Time        // when the marker was last kept

	watching  map[int64]watch       // the subscriptions whose fetch is waited for, by ID
	watchedAt time.Time             // when they were last asked about
	watchBusy bool                  // they are being asked about
	notes     []fetchNote           // fetch results to show as toasts
	undoable  *marking              // the last "mark as read", until its toast closes
	seeking   bool                  // N found no unread entry loaded and waits for the next page
	noMore    bool                  // show that no unread entry is below
	noOlder   string                // show that no unread entry is older than this age, in words
	keys      bool                  // the key sheet shows
	pal       paletteState          // the command palette
	subs      *subscriptions.Screen // the dialogs that add and edit subscriptions
	reader    *reader.Screen        // in the place of the list while an article is read
	reading   int64                 // the entry read, 0 for none
	immersive bool                  // immersive reading: the reader covers the sidebar and hides its toolbar; never kept
	announce  string
	toast     string
	refresh   *refreshing // "refresh all" from the request to the result, nil for none
	result    *refreshed  // the result to show as a toast
}

// refreshing is "refresh all" under way: the server queued queued subscriptions at the time at, and each one that was
// fetched since counts as done.
type refreshing struct {
	at, queued, done, failed int
	deadline, next           time.Time
	checking                 bool
}

// refreshed is the toast that ends "refresh all": its text and whether it carries the alert mark.
type refreshed struct {
	text  string
	alert bool
}

// New selects the best available translation, falling back to English. The screen shows nothing until Use.
func New(systemLocale string, deps Deps) *Screen {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Decode == nil {
		deps.Decode = ui.DecodeBitmap
	}
	s := &Screen{deps: deps, lang: locale.Language(systemLocale), text: translations[locale.Language(systemLocale)], selected: -1, focusRow: -1,
		hovered: -1, divider: -1, pics: pictures.New(true, keepPictures), rects: map[int]ui.Rect{}, dismissed: map[listKey]bool{}}
	s.subs = subscriptions.New(systemLocale, subscriptions.Deps{
		Go:           deps.Go,
		Now:          deps.Now,
		Sources:      func() []client.Source { return s.sources },
		Icon:         s.bitmap,
		Added:        s.added,
		Refreshed:    func(src client.Source) { s.watch(src, false) },
		Changed:      s.changed,
		Removed:      s.removed,
		Restored:     s.restored,
		Bulk:         s.bulk,
		Show:         func(id int64) { s.choose(scope{kind: s.kindOf(id), source: id}) },
		MarkRead:     s.markSource,
		MarkOlder:    s.markSourceOlder,
		OpenSettings: deps.OpenAISettings,
		OpenRSSHub: func() {
			if deps.OpenSettingsPage != nil {
				deps.OpenSettingsPage("fetching")
			}
		},
		Expired: func() {
			s.gen++
			s.pics.Stop()
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
		Save:         deps.Save,
		Back:         s.back,
		Move:         s.step,
		NextUnread:   s.nextUnread,
		Star:         func() { s.toggleStar(s.index(s.reading)) },
		ToggleRead:   func() { s.toggleRead(s.index(s.reading)) },
		OpenCopy:     s.showCopy,
		Immerse:      s.toggleImmersive,
	})
	return s
}

// SetLanguage shows the screen, its dialogs and the reader in the language of systemLocale, keeping the scope, the
// selection, the place in the list and the article open.
func (s *Screen) SetLanguage(systemLocale string) {
	s.lang = locale.Language(systemLocale)
	s.text = translations[s.lang]
	s.subs.SetLanguage(systemLocale)
	s.reader.SetLanguage(systemLocale)
}

// Use shows the timeline of api, whose host is server ("" for this computer), from the start.
func (s *Screen) Use(api API, server string) {
	s.api, s.server = api, server
	s.gen++
	s.pass = passing{}
	s.pics.Stop()
	s.sources, s.sourcesLoaded = nil, false
	s.setUnread(0)
	s.sourcesGen++
	s.scope, s.tab, s.filter, s.budget = scope{}, 0, everything, 0
	s.restorePlace()
	s.oldest = s.views().OldestFirst
	s.query, s.searched, s.editing = "", "", false
	s.lists, s.anchor = nil, 0
	s.seen, s.atTop = prefs.Marker{}, false
	s.renew()
	s.pics.Reset()
	s.undoable, s.seeking, s.keys, s.pal = nil, false, false, paletteState{}
	s.refresh, s.result = nil, nil
	s.queue, s.sending, s.failures = nil, false, 0
	s.watching, s.watchBusy, s.notes = nil, false, nil
	s.subs.Use(api)
	s.reader.Use(api)
	s.loadSources()
	s.reload()
}

// restorePlace puts the scope and filter back to where the reader left them on this server. A subscription is taken on
// trust until the sources load, which drop it if it is gone.
func (s *Screen) restorePlace() {
	s.restoring = false
	if s.deps.Reading == nil {
		return
	}
	p := s.deps.Reading().Normalize().Places.Get(s.server)
	s.scope = scope{kind: p.Kind, source: p.Source}
	s.tab = max(0, indexOf(kinds, p.Kind))
	s.restoring = p.Source != 0
	switch p.Filter {
	case prefs.FilterUnread:
		s.filter = unread
	case prefs.FilterStarred:
		s.filter = starred
	}
}

// remember keeps the scope and filter when show changed them.
func (s *Screen) remember() {
	if s.deps.Remember == nil {
		return
	}
	p := prefs.Place{Kind: s.scope.kind, Source: s.scope.source}
	switch s.filter {
	case unread:
		p.Filter = prefs.FilterUnread
	case starred:
		p.Filter = prefs.FilterStarred
	}
	s.deps.Remember(s.server, p)
}

// Refresh loads the subscriptions and the entries again, as after an import added subscriptions.
func (s *Screen) Refresh() {
	s.lists = nil
	s.loadSources()
	s.reload()
}

// ViewSources shows all subscriptions, from the result of an import that added some: the scope is All, the failure
// filter and the words typed in the sidebar's filter are cleared, a hidden sidebar shows, and the focus goes to the
// sidebar as with H.
func (s *Screen) ViewSources() {
	s.failingOnly, s.sideQuery = false, ""
	s.choose(scope{})
	s.toSidebar()
}

func (s *Screen) expired(err error) bool {
	if !errors.Is(err, client.ErrUnauthorized) {
		return false
	}
	s.gen++
	s.pics.Stop()
	s.api = nil
	s.lists = nil
	s.refresh, s.result = nil, nil
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
	s.sourcesGen++
	s.loadSources()
	s.watch(src, true)
}

// watch starts waiting for the fetch of src, which was just added or queued to fetch now.
func (s *Screen) watch(src client.Source, added bool) {
	if s.watching == nil {
		s.watching = map[int64]watch{}
	}
	now := s.deps.Now()
	s.watching[src.ID] = watch{fetchedAt: src.LastFetchedAt, unread: src.UnreadCount, added: added, until: now.Add(watchFor)}
	if !s.watchBusy {
		s.watchedAt = now
	}
}

// watchFetches asks every watchEvery for the subscriptions added or refreshed until each has fetched or watchFor is
// over, and says how the fetch went.
func (s *Screen) watchFetches(c *ui.Context) {
	if len(s.watching) == 0 || s.api == nil || s.watchBusy {
		return
	}
	now := s.deps.Now()
	if wait := watchEvery - now.Sub(s.watchedAt); wait > 0 {
		c.After(wait)
		return
	}
	s.watchBusy, s.watchedAt = true, now
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		sources, err := api.Sources(context.Background())
		return func() {
			if gen != s.gen {
				return
			}
			s.watchBusy = false
			if s.expired(err) {
				return
			}
			if err == nil {
				s.setSources(sources)
			}
			s.settleWatches(err == nil)
		}
	})
}

// settleWatches ends the waits whose fetch is done, whose subscription is gone or whose time is up, and notes how the
// fetches went. listed says s.sources was just loaded.
func (s *Screen) settleWatches(listed bool) {
	now := s.deps.Now()
	for id, w := range s.watching {
		i := slices.IndexFunc(s.sources, func(src client.Source) bool { return src.ID == id })
		switch {
		case i < 0:
			delete(s.watching, id)
		case listed && s.sources[i].LastFetchedAt != w.fetchedAt:
			delete(s.watching, id)
			s.notes = append(s.notes, s.fetchNote(s.sources[i], w))
		case !now.Before(w.until):
			delete(s.watching, id)
		}
	}
}

// fetchNote words how the fetch of src went: its failure, the entries a new subscription got, or the entries a refresh
// found.
func (s *Screen) fetchNote(src client.Source, w watch) fetchNote {
	name := subscriptions.Name(src)
	note := fetchNote{id: src.ID}
	var text string
	switch n := src.UnreadCount; {
	case src.LastError != "":
		text, note.failed = strings.ReplaceAll(s.text.FetchFailed, "{error}", s.failure(src)), true
	case w.added && n == 0:
		text = s.text.FetchedNone
	case w.added && n == 1:
		text = s.text.FetchedEntry
	case w.added:
		text = s.format(s.text.FetchedEntries, n)
	case n-w.unread == 1:
		text = s.text.FreshEntry
	case n-w.unread > 1:
		text = s.format(s.text.FreshEntries, n-w.unread)
	default:
		text = s.text.NothingNewIn
	}
	note.text = strings.ReplaceAll(text, "{name}", name)
	return note
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
	s.sourcesGen++
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
	s.sourcesGen++
	s.loadSources()
	s.lists = nil
	if s.scope.source == id {
		s.entries = nil
		s.choose(scope{})
		return
	}
	s.reload()
}

// restored brings back a subscription that was deleted a moment ago, with its entries.
func (s *Screen) restored(src client.Source) {
	if !slices.ContainsFunc(s.sources, func(old client.Source) bool { return old.ID == src.ID }) {
		s.sources = append(s.sources, src)
		s.sourcesGen++
	}
	s.loadSources()
	s.lists = nil
	s.reload()
}

// bulk takes what a bulk run did to the subscriptions as one change: the subscriptions and the entries load once.
func (s *Screen) bulk(changed []client.Source, removed []int64, restored []client.Source) {
	again, scopeGone := len(restored) > 0 || len(removed) > 0, false
	for _, src := range changed {
		i := slices.IndexFunc(s.sources, func(old client.Source) bool { return old.ID == src.ID })
		if i < 0 {
			continue
		}
		old := s.sources[i]
		s.sources[i] = src
		if s.scope.source == src.ID && src.Kind != old.Kind {
			s.scope.kind = src.Kind
			s.tab = max(0, indexOf(kinds, src.Kind))
		}
		again = again || src.Title != old.Title || src.Kind != old.Kind || src.MuteKeywords != old.MuteKeywords
	}
	for _, id := range removed {
		scopeGone = scopeGone || s.scope.source == id
	}
	s.sources = slices.DeleteFunc(s.sources, func(src client.Source) bool { return slices.Contains(removed, src.ID) })
	for _, src := range restored {
		if !slices.ContainsFunc(s.sources, func(old client.Source) bool { return old.ID == src.ID }) {
			s.sources = append(s.sources, src)
		}
	}
	s.sourcesGen++
	s.loadSources()
	switch {
	case scopeGone:
		s.lists, s.entries = nil, nil
		s.choose(scope{})
	case again:
		s.lists = nil
		s.reload()
	}
}

// setSources takes the subscriptions as the server counts them.
func (s *Screen) setSources(list client.SourceList) {
	s.sources, s.sourcesLoaded = list.Sources, true
	s.setUnread(list.UnreadTotal)
	s.sourcesGen++
}

// setUnread takes the unread count of All and tells Deps.Unread when it changed.
func (s *Screen) setUnread(n int) {
	if n == s.unreadTotal {
		return
	}
	s.unreadTotal = n
	if s.deps.Unread != nil {
		s.deps.Unread(n)
	}
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
			s.setSources(sources)
			if s.restoring {
				s.restoring = false
				s.checkRestored()
			}
		}
	})
}

// checkRestored goes to everything when the subscription kept from the last run is gone, and takes the kind it has now
// otherwise.
func (s *Screen) checkRestored() {
	if kind := s.kindOf(s.scope.source); kind != "" {
		s.scope.kind = kind
		s.tab = max(0, indexOf(kinds, kind))
		return
	}
	s.entries = nil
	s.show(func() { s.scope, s.tab = scope{}, 0 })
}

func (s *Screen) query1(cursor string) client.Query {
	q := client.Query{Source: s.scope.source, Unread: s.filter == unread, Starred: s.filter == starred,
		MaxMinutes: s.budget, Search: s.searched, Limit: pageSize, Cursor: cursor, Oldest: s.oldest}
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
	s.flushPass()
	s.pass.rebase()
	s.run++
	s.also = 0
	run, gen, api, q := s.run, s.gen, s.api, s.query1("")
	s.loadedAt = s.deps.Now()
	s.entries, s.next, s.loading, s.loadErr, s.more, s.moreErr, s.fresh = nil, "", true, false, false, false, 0
	s.offline, s.failures = false, 0
	s.selected, s.focusRow, s.hovered, s.toTop, s.reading, s.copyID, s.seeking = -1, -1, -1, true, 0, 0, false
	s.checking = false
	s.divider, s.away = -1, false
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
			s.overlay()
			s.flush()
		}
	})
}

// retryPictures forgets the pictures that failed to load, so the list tries them again.
func (s *Screen) retryPictures() {
	s.pics.ForgetFailed()
}

func (s *Screen) key() listKey { return listKey{s.scope, s.filter, s.budget, s.oldest} }

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
	s.pass.rebase()
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
	s.flushPass()
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
	s.also = 0
	s.leave(s.key())
	before := s.scope
	beforeFilter := s.filter
	change()
	if s.scope != before || s.filter != beforeFilter {
		s.remember()
	}
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
	s.pass.rebase()
	s.run++
	run, gen, api, q := s.run, s.gen, s.api, s.query1("")
	s.entries, s.next, s.anchor = snap.entries, snap.next, snap.anchor
	s.loading, s.loadErr, s.more, s.moreErr, s.fresh, s.offline, s.checking = false, false, false, false, 0, false, true
	s.failures = 0
	s.selected, s.focusRow, s.hovered, s.toTop, s.reading, s.seeking = s.index(snap.selected), -1, -1, false, 0, false
	s.polledAt, s.failures = s.deps.Now(), 0
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
				s.lost()
				return
			}
			s.polledAt = s.deps.Now()
			s.found()
			s.merge(page)
		}
	})
}

// holdsTop tells whether entries newer than the top one must not be put above a grid or a wall scrolled away from
// its top, which would move what is in view; the column of rows keeps its place by the keys of its rows.
func (s *Screen) holdsTop() bool {
	switch {
	case s.oldest:
		return false
	case s.walls():
		return s.wall.Y > 0
	case s.grids():
		return len(s.entries) == 0 || s.anchor != s.entries[0].ID
	}
	return false
}

// older tells whether a is older than b.
func older(a, b client.Entry) bool {
	return a.CreatedAt < b.CreatedAt || a.CreatedAt == b.CreatedAt && a.ID < b.ID
}

// looked is the marker kept for what the reader last looked at.
func (s *Screen) looked() prefs.Marker {
	if s.deps.Reading == nil {
		return prefs.Marker{}
	}
	return s.deps.Reading().LastLooked
}

// renew puts the divider where the marker kept now stands, and shows it again where it was dismissed: for a new
// server and for the list loaded again from "new".
func (s *Screen) renew() {
	s.base = s.looked()
	clear(s.dismissed)
}

// markerOf is the marker that stands for the entry e.
func markerOf(e client.Entry) prefs.Marker { return prefs.Marker{At: e.CreatedAt, ID: e.ID} }

// movesMarker tells whether the list on screen may move the marker: only the list of everything, in the order of the
// whole timeline, newest first, has the newest entry there is as its first. In a kind, a subscription, Unread,
// Starred, a reading time, a search or oldest first the first entry is not that, so those only read the marker.
func (s *Screen) movesMarker() bool {
	return s.api != nil && s.reading == 0 && !s.loading && !s.loadErr && len(s.entries) > 0 && s.searched == "" &&
		s.scope == (scope{}) && s.filter == everything && s.budget == 0 && !s.oldest
}

// onTop tells whether the first entry was drawn whole in the last frame, or the grid or wall is at its top.
func (s *Screen) onTop() bool {
	switch s.layout() {
	case gridLayout, squareLayout, wallLayout:
		return !s.holdsTop()
	}
	return s.fullyShown(0)
}

// look runs after each frame: it moves the marker to the newest entry seen while the window is active, the list is
// the whole timeline and rests at the top, keeps it when that state ends and every lookEvery while it lasts, and
// dismisses the divider when the list comes back to the top.
func (s *Screen) look(c *ui.Context) {
	active := s.deps.Active == nil || s.deps.Active()
	top := active && s.movesMarker() && s.onTop()
	kept := s.looked()
	if top {
		if m := markerOf(s.entries[0]); s.seen.Newer(m.At, m.ID) && kept.Newer(m.At, m.ID) {
			s.seen = m
		}
	}
	if dirty := s.seen.Valid() && s.seen != kept; dirty {
		switch wait := lookEvery - s.deps.Now().Sub(s.lookedAt); {
		case !top && s.atTop:
			s.keep()
		case top && wait <= 0:
			s.keep()
		case top:
			c.After(wait)
		}
	}
	s.atTop = top
	if s.divider < 0 || !active || s.reading != 0 {
		return
	}
	switch {
	case s.scrolled():
		s.away = true
	case s.away && s.onTop():
		s.dismiss()
	}
}

// scrolled tells whether the list was drawn in the last frame without its first entry: the reader scrolled it away,
// which a first frame, drawn before the list has its place, is not.
func (s *Screen) scrolled() bool {
	switch s.layout() {
	case gridLayout, squareLayout, wallLayout:
		return s.holdsTop()
	}
	_, drawn := s.rects[0]
	return len(s.rects) > 0 && !drawn
}

// keep keeps the newest entry seen as the marker, in the background.
func (s *Screen) keep() {
	s.lookedAt = s.deps.Now()
	if s.deps.Look != nil {
		s.deps.Look(s.seen)
	}
}

// dismiss takes the divider away from this list. Where the marker may move it catches up with the first entry.
func (s *Screen) dismiss() {
	s.dismissed[s.key()] = true
	s.divider, s.away = -1, false
	if s.movesMarker() {
		if m := markerOf(s.entries[0]); s.seen.Newer(m.At, m.ID) && s.looked().Newer(m.At, m.ID) {
			s.seen = m
		}
		if s.seen.Valid() && s.seen != s.looked() {
			s.keep()
		}
	}
}

// dividerAt is the entry the divider goes above: the first at or older than the marker, so that every entry before it
// is newer and the count of those is its index; -1 when the divider does not show. It does not show on a first run, in a search, oldest first, in a list
// that was dismissed, with nothing newer than the marker, or while all that is loaded is newer (the marker is further down).
func (s *Screen) dividerAt() int {
	if !s.base.Valid() || s.searched != "" || s.oldest || s.dismissed[s.key()] {
		return -1
	}
	i := slices.IndexFunc(s.entries, func(e client.Entry) bool { return !s.base.Newer(e.CreatedAt, e.ID) })
	if i <= 0 {
		return -1
	}
	return i
}

// after tells whether a comes after b in the list: older newest first, newer oldest first.
func (s *Screen) after(a, b client.Entry) bool {
	if s.oldest {
		return older(b, a)
	}
	return older(a, b)
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
			if !have[e.ID] && (len(fresh) == 0 || s.after(e, fresh[len(fresh)-1])) {
				merged = append(merged, e)
			}
		}
		if len(merged) > len(fresh) {
			next = s.next
		}
	}
	s.entries, s.next = merged, next
	s.overlay()
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
			s.also = 0
			s.overlay()
			if s.next == "" {
				s.announce = s.text.AllLoaded
			}
		}
	})
}

// RefreshAll asks the server to check every active subscription now, from the menu, Cmd/Ctrl+R and the palette. The
// progress shows in the sidebar and the result in a toast; asking again while it runs does nothing.
func (s *Screen) RefreshAll() {
	if s.api == nil || s.refresh != nil || s.keys || s.subs.Open() || s.pal.open || s.lb.open {
		return
	}
	r := &refreshing{}
	s.refresh = r
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		res, err := api.RefreshAll(context.Background())
		return func() {
			if gen != s.gen || s.refresh != r {
				return
			}
			if s.expired(err) {
				return
			}
			switch {
			case err != nil:
				s.refresh, s.result = nil, &refreshed{text: s.text.RefreshFailed, alert: true}
			case res.Queued == 0:
				s.refresh, s.result = nil, &refreshed{text: s.text.RefreshNone}
			default:
				now := s.deps.Now()
				r.at, r.queued = int(res.At), res.Queued
				r.deadline, r.next = now.Add(refreshLimit), now.Add(refreshEvery)
				s.announce = s.format(s.text.RefreshingAnnounce, res.Queued)
			}
		}
	})
}

// progress reads which subscriptions "refresh all" has fetched, every refreshEvery, and ends it when all were or the
// time is out.
func (s *Screen) progress(c *ui.Context) {
	r := s.refresh
	if r == nil || r.queued == 0 || r.checking || s.api == nil {
		return
	}
	now := s.deps.Now()
	if wait := r.next.Sub(now); wait > 0 {
		c.After(wait)
		return
	}
	r.checking, r.next = true, now.Add(refreshEvery)
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		sources, err := api.Sources(context.Background())
		return func() {
			if gen != s.gen || s.refresh != r {
				return
			}
			r.checking = false
			if s.expired(err) {
				return
			}
			if err == nil {
				s.setSources(sources)
				r.done, r.failed = 0, 0
				for _, src := range sources.Sources {
					if !src.Paused && src.LastFetchedAt >= int64(r.at) {
						r.done++
						if src.LastError != "" {
							r.failed++
						}
					}
				}
				r.done = min(r.done, r.queued)
			}
			switch {
			case r.done >= r.queued:
				s.finishRefresh(s.refreshText(s.text.Refreshed, s.text.RefreshedFailed, r), r.failed > 0)
			case !s.deps.Now().Before(r.deadline):
				text := strings.ReplaceAll(s.text.RefreshedPart, "{done}", strconv.Itoa(r.done))
				s.finishRefresh(s.format(text, r.queued), false)
			}
		}
	})
}

// refreshText is the result when all were checked: plain, or with how many failed.
func (s *Screen) refreshText(ok, failed string, r *refreshing) string {
	text := ok
	if r.failed > 0 {
		text = strings.ReplaceAll(failed, "{failed}", strconv.Itoa(r.failed))
	}
	return s.format(text, r.queued)
}

// finishRefresh ends "refresh all" with its toast and has the timeline look for new entries at once.
func (s *Screen) finishRefresh(text string, alert bool) {
	s.refresh, s.result = nil, &refreshed{text: text, alert: alert}
	s.polledAt = time.Time{}
}

// interval is how long to wait between background checks: pollEvery, or while the server is out of reach the wait
// that the failures in a row call for.
func (s *Screen) interval() time.Duration {
	if !s.offline || s.failures == 0 {
		return pollEvery
	}
	return retryAfter[min(s.failures, len(retryAfter))-1]
}

// Check looks at the server now, as the window coming to the front and the computer waking ask. While it is out of
// reach it does at once; otherwise it does unless a check ran in the last checkGap.
func (s *Screen) Check() {
	if s.api == nil {
		return
	}
	if s.offline || s.deps.Now().Sub(s.polledAt) >= checkGap {
		s.polledAt = time.Time{}
	}
}

// lost notes a background check that failed: the line under the toolbar shows, and the next check comes sooner.
func (s *Screen) lost() {
	s.failures++
	if !s.offline {
		s.offline, s.announce = true, s.staleText()
	}
}

// drop notes a write that did not reach the server: it takes the first wait before the next check, unless checks
// already failed.
func (s *Screen) drop() {
	if !s.offline {
		s.offline, s.announce = true, s.staleText()
		s.failures = 1
		s.polledAt = s.deps.Now()
	}
}

// found notes that the server answered: the line goes, the waits start over, and the changes waiting are sent.
func (s *Screen) found() {
	s.offline, s.failures = false, 0
	s.updated = s.deps.Now()
	s.flush()
}

// poll checks the sources and the first page for new entries every pollEvery, without moving the list.
func (s *Screen) poll(c *ui.Context) {
	if s.api == nil || s.loading || s.loadErr || s.checking || s.polling || s.searched != "" {
		return
	}
	now := s.deps.Now()
	if wait := s.interval() - now.Sub(s.polledAt); wait > 0 {
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
				s.setSources(sources)
			}
			if run != s.run {
				return
			}
			if sourcesErr != nil || err != nil {
				s.lost()
				return
			}
			s.found()
			if len(s.entries) == 0 {
				s.entries, s.next = page.Entries, page.Next
				s.overlay()
				return
			}
			if s.oldest {
				// Oldest first has no newest entry to compare with: new entries come when paging reaches the end.
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

// limited reports whether the search applied is one or two characters, which the server answers from titles and
// summaries only, at most 200 entries.
func (s *Screen) limited() bool {
	return search.Parse(s.searched).Short()
}

// search applies the query typed.
func (s *Screen) search() {
	s.editing = false
	q := strings.TrimSpace(s.query)
	if q == s.searched {
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
			s.sourcesGen++
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
	if s.offline || s.waitingFor(id) >= 0 {
		// The server is out of reach, or an earlier change of this entry is still waiting and must go first.
		s.enqueue(id, patch, back, before.Group)
		return
	}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		reply, err := api.SetEntry(context.Background(), id, patch)
		return func() {
			if gen != s.gen {
				return
			}
			if err == nil {
				// The server changes every copy of an article, also when this view shows the row alone (one
				// subscription, a search): the reply lists the copies, and the counts of their sources are read again.
				if before.Group != nil || reply.Group != nil && len(reply.Group.Entries) > 0 {
					s.loadSources()
				}
				return
			}
			if s.expired(err) {
				return
			}
			if unreached(err) {
				s.enqueue(id, patch, back, before.Group)
				s.drop()
				return
			}
			s.revert(id, back, before.Group)
		}
	})
}

// revert puts an entry back the way it was when the server refused its change.
// group is the other copies as they were, each with its own state, which the entry gets back as they were.
func (s *Screen) revert(id int64, back client.EntryPatch, group *client.Group) {
	if i := s.index(id); i >= 0 {
		s.setRow(i, back)
		if e := &s.entries[i]; e.Group != nil && group != nil {
			members := slices.Clone(e.Group.Entries)
			for k := range members {
				m := &members[k]
				j := slices.IndexFunc(group.Entries, func(o client.GroupMember) bool { return o.ID == m.ID })
				if j < 0 {
					continue
				}
				m.IsRead, m.Starred = s.moveMember(*m, group.Entries[j].IsRead), group.Entries[j].Starred
			}
			e.Group = &client.Group{By: e.Group.By, Entries: members}
		}
	}
	s.patchLists([]int64{id}, back)
	s.toast = s.text.ActionFailed
}

// unreached tells whether err says the server could not be reached, not that it refused: a refusal is an answer, and
// an expired session has its own way.
func unreached(err error) bool {
	var refused *client.APIError
	return err != nil && !errors.Is(err, client.ErrUnauthorized) && !errors.As(err, &refused)
}

// waiting is a star or read change the server did not get. back is how the entry was before the first change that
// is waiting, which a refusal puts it back to; version counts the changes folded into it.
type waiting struct {
	id          int64
	patch, back client.EntryPatch
	// group is the other copies of the article as they were before the first change that is waiting.
	group   *client.Group
	version int
}

// waitingFor is the place in the queue of the changes waiting for entry id, -1 for none.
func (s *Screen) waitingFor(id int64) int {
	return slices.IndexFunc(s.queue, func(w waiting) bool { return w.id == id })
}

// enqueue keeps a change the server did not get. A second change of the same entry folds into the first: the last
// value of each field is sent, and the way back is the state before the first.
func (s *Screen) enqueue(id int64, patch, back client.EntryPatch, group *client.Group) {
	i := s.waitingFor(id)
	if i < 0 {
		s.queue = append(s.queue, waiting{id: id, patch: patch, back: back, group: group, version: 1})
		return
	}
	w := &s.queue[i]
	if patch.IsRead != nil {
		w.patch.IsRead = patch.IsRead
	}
	if patch.Starred != nil {
		w.patch.Starred = patch.Starred
	}
	if w.back.IsRead == nil {
		w.back.IsRead = back.IsRead
	}
	if w.back.Starred == nil {
		w.back.Starred = back.Starred
	}
	w.version++
}

// overlay puts the changes waiting back on the entries just loaded, which the server answered without them.
func (s *Screen) overlay() {
	for _, w := range s.queue {
		if i := s.index(w.id); i >= 0 {
			s.set(i, w.patch)
		}
	}
}

// flush sends the changes waiting, in order, now that the server answered. One the server takes is done; one it
// refuses goes back with a note; one that finds the server out of reach again waits for the next time.
func (s *Screen) flush() {
	if len(s.queue) == 0 || s.sending || s.api == nil {
		return
	}
	s.sending = true
	batch := slices.Clone(s.queue)
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		errs := make([]error, 0, len(batch))
		for _, w := range batch {
			_, err := api.SetEntry(context.Background(), w.id, w.patch)
			errs = append(errs, err)
			if unreached(err) || errors.Is(err, client.ErrUnauthorized) {
				break
			}
		}
		return func() {
			if gen != s.gen {
				return
			}
			s.sending = false
			for i, err := range errs {
				w := batch[i]
				if s.expired(err) {
					return
				}
				if unreached(err) {
					s.drop()
					return
				}
				if j := s.waitingFor(w.id); j >= 0 && s.queue[j].version == w.version {
					s.queue = slices.Delete(s.queue, j, j+1)
				}
				if err != nil {
					s.revert(w.id, w.back, w.group)
				}
			}
			if !s.offline {
				s.flush()
			}
		}
	})
}

// setRow sets the fields of patch on the row alone and moves the counts of its source.
func (s *Screen) setRow(i int, patch client.EntryPatch) {
	e := &s.entries[i]
	if patch.IsRead != nil && *patch.IsRead != e.IsRead {
		e.IsRead = *patch.IsRead
		delta := 1
		if e.IsRead {
			delta = -1
		}
		s.count(*e, delta)
		s.setUnread(max(0, s.unreadTotal+delta))
	}
	if patch.Starred != nil {
		e.Starred = *patch.Starred
	}
}

// moveMember moves the count of the source of copy m for it being read or not, and returns read.
func (s *Screen) moveMember(m client.GroupMember, read bool) bool {
	if m.IsRead != read {
		delta := 1
		if read {
			delta = -1
		}
		s.count(client.Entry{SourceID: m.SourceID}, delta)
	}
	return read
}

func (s *Screen) set(i int, patch client.EntryPatch) {
	s.setRow(i, patch)
	e := &s.entries[i]
	if e.Group != nil {
		// A change reaches every copy of the article. The server's counts come with the reload after it is taken.
		members := slices.Clone(e.Group.Entries)
		for k := range members {
			m := &members[k]
			if patch.IsRead != nil {
				m.IsRead = s.moveMember(*m, *patch.IsRead)
			}
			if patch.Starred != nil {
				m.Starred = *patch.Starred
			}
		}
		e.Group = &client.Group{By: e.Group.By, Entries: members}
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
	if s.stay.id == s.entries[i].ID {
		s.stay.done = true // the reader's own choice stands: the stay does not mark it again
	}
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
	s.markOpened(e, false)
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
	s.selected, s.reading, s.copyID = i, e.ID, 0
	s.reader.Open(e)
	s.prefetch()
	s.markOpened(e, true)
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
	s.reading, s.copyID, s.seeking, s.immersive = 0, 0, false, false
	if i < 0 {
		return
	}
	s.selected, s.focusRow = i, i
	s.list.ScrollIntoView(i)
	s.grid.ScrollIntoView(i)
	s.pass.rebase()
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

// countText is an unread count as the sidebar and the palette draw it: above maxCount it reads "999+", so the column
// never grows past that text; the full number stays in the accessible description.
func countText(n int) string {
	if n > maxCount {
		return strconv.Itoa(maxCount) + "+"
	}
	return strconv.Itoa(n)
}

// unreadIn returns the unread entries of the scope, as the sources count them; All is the server's total, which
// counts an article several sources carry once.
func (s *Screen) unreadIn(sc scope) int {
	if sc.source == 0 && sc.kind == "" {
		return s.unreadTotal
	}
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
	upTo := s.entries[0].ID
	if s.oldest {
		upTo = s.entries[len(s.entries)-1].ID
	}
	scope := client.ReadScope{SourceID: s.scope.source, UpTo: upTo}
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
	if s.oldest {
		// The entries above are the older ones, from the first of the timeline up to the one before id.
		scope = client.ReadScope{SourceID: s.scope.source, UpTo: s.entries[s.index(id)-1].ID}
	}
	if s.scope.source == 0 {
		scope.Kind = s.scope.kind
	}
	s.markScope(scope, s.text.MarkedAbove, "")
}

// canMarkBelow says whether an unread entry is loaded below the entry id, where "Mark below as read" can apply: not
// under Starred or a search, which the server's scopes do not know.
func (s *Screen) canMarkBelow(id int64) bool {
	if s.filter == starred || s.budget != 0 || s.searched != "" {
		return false
	}
	i := s.index(id)
	if i < 0 {
		return false
	}
	return slices.ContainsFunc(s.entries[i+1:], func(e client.Entry) bool { return !e.IsRead })
}

// markBelow marks the loaded entries below the entry id read, in the scope, and offers to undo it.
func (s *Screen) markBelow(id int64) {
	if !s.canMarkBelow(id) || s.api == nil {
		return
	}
	last := s.entries[len(s.entries)-1].ID
	// The entries below are the older ones, from the one after id to the last loaded: not the ones the list has not
	// loaded yet, and not those that arrived after it was opened.
	scope := client.ReadScope{SourceID: s.scope.source, UpTo: s.entries[s.index(id)+1].ID, From: last}
	if s.oldest {
		// The entries below are the newer ones, from the one after id up to the last loaded.
		scope = client.ReadScope{SourceID: s.scope.source, Newer: id, UpTo: last}
	}
	if s.scope.source == 0 {
		scope.Kind = s.scope.kind
	}
	s.markScope(scope, s.text.MarkedBelow, "")
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
		if s.oldest {
			scope = client.ReadScope{SourceID: s.scope.source, UpTo: s.entries[end-1].ID}
			if first > 0 {
				scope.Newer = s.entries[first-1].ID
			}
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
		marked, err := api.MarkRead(context.Background(), scope)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			if err != nil {
				s.toast = s.text.ActionFailed
				return
			}
			ids := marked.IDs
			if len(ids) == 0 && none != "" {
				s.noOlder = none
				return
			}
			s.setRead(ids, true)
			s.loadSources()
			s.undoable = nil
			if len(ids) > maxUndo {
				return
			}
			s.undoable = &marking{Marked: marked, text: strings.ReplaceAll(s.format(text, len(ids)), "{scope}", name)}
		}
	})
}

// undo marks the entries of m unread again.
func (s *Screen) undo(m *marking) {
	if s.undoable == m {
		s.undoable = nil
	}
	if s.api == nil || len(m.IDs) == 0 {
		return
	}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		ids, err := api.MarkUnread(context.Background(), m.Marked)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			if err != nil {
				s.toast = s.text.ActionFailed
				return
			}
			s.setRead(ids, false)
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
	return strings.ReplaceAll(s.text.Offline, "{time}", locale.Clock(s.lang, s.hours, s.updated))
}

func (s *Screen) format(text string, n int) string { return locale.Count(text, "n", n) }

// reason is why src is failing, as the edit dialog of the subscription says it.
func (s *Screen) reason(src client.Source) string {
	text := s.text.FailedOnce
	if src.ConsecutiveFailures > 1 {
		text = s.format(s.text.FailedTimes, src.ConsecutiveFailures)
	}
	return strings.ReplaceAll(text, "{error}", s.failure(src))
}

// failure is the reason src failed, in the user's language when the site's bot protection turned the fetch away.
func (s *Screen) failure(src client.Source) string {
	return subscriptions.FailureReason(s.lang, src, s.text.Blocked, s.text.BlockedPublic, s.text.FeedRemoved)
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
	return locale.Capital(locale.Ago(s.lang, now, time.UnixMilli(ms)))
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
	if s.waitingFor(e.ID) >= 0 {
		parts = append(parts, s.text.Pending)
	}
	if fade(e, now) < 1 {
		parts = append(parts, s.text.Leaving)
	}
	return strings.Join(parts, s.text.Separator)
}

// fadedOpacity is how clear an unread entry is when its time is nearly up.
const fadedOpacity = 0.55

// fade is the opacity of an entry: 1 until the server says it starts to fade (the last quarter of its source's
// pace), then down to fadedOpacity at the moment it leaves All and Unread. See docs/product.md "节奏".
func fade(e client.Entry, now time.Time) float32 {
	if e.FadesAt <= 0 || e.ExpiresAt <= e.FadesAt || e.IsRead || e.Starred {
		return 1
	}
	at := now.UnixMilli()
	if at <= e.FadesAt {
		return 1
	}
	if at >= e.ExpiresAt {
		return fadedOpacity
	}
	progress := float32(at-e.FadesAt) / float32(e.ExpiresAt-e.FadesAt)
	return 1 - progress*(1-fadedOpacity)
}

// readLabel is "6 min read" for an entry that is read and takes a minute or more; podcasts and videos, which have a
// duration of their own, and entries with no estimate have none.
func (s *Screen) readLabel(e client.Entry) string {
	if e.ReadingSec < 60 || e.DurationSec > 0 || e.Kind == "video" {
		return ""
	}
	return s.format(s.text.ReadingTime, e.ReadingSec/60)
}

// budgetName is the name of the reading-time button, which states the budget.
func (s *Screen) budgetName() string {
	if s.budget == 0 {
		return s.text.TimeNameAny
	}
	return s.format(s.text.TimeNameUpTo, s.budget)
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
	return s.pics.Get(address, s.deps.Now()).Bitmap
}

// download starts the pictures asked for, the latest asked first.
func (s *Screen) download() {
	gen := s.gen
	s.pics.Download(s.api, s.deps.Decode, s.deps.Go, s.deps.Now, func() bool { return gen == s.gen })
}

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
	s.hours = c.Region().HourCycle
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
	s.progress(c)
	s.watchFetches(c)
	if s.reading != 0 && s.index(s.reading) < 0 {
		s.reading = 0
	}
	if s.reading != 0 {
		s.typing, s.filtering = false, false
	}
	if s.seeking && !s.more {
		s.nextUnread()
	}
	if s.lb.open && s.index(s.lb.entry) < 0 {
		s.lb.open = false
	}
	open := s.keys || s.subs.Open() || s.pal.open || s.lb.open
	if !open {
		c.OnShortcut(ui.Cmd, ui.KeyK, s.openPalette)
		if s.reading == 0 {
			// In the reader the same keys find in the article, which the reader handles.
			c.OnShortcut(ui.Cmd, ui.KeyF, func() { s.searchBox = true })
		}
		c.OnShortcut(ui.Cmd, ui.KeyN, s.subs.Add)
		c.OnShortcut(ui.Cmd, ui.KeyR, s.RefreshAll)
		c.OnShortcut(ui.Cmd, ui.KeySlash, s.ShowKeys)
		c.OnShortcut(ui.Cmd, ui.KeyBackslash, func() {
			if s.immersive {
				s.toggleImmersive()
				return
			}
			s.toggleSidebar()
		})
		if m := s.undoable; m != nil && !s.typing && !s.filtering {
			c.OnShortcut(ui.Cmd, ui.KeyZ, func() {
				c.CloseToast(markedToast)
				s.undo(m)
			})
		}
	}
	if !open && !s.typing && !s.filtering {
		c.OnShortcut(ui.Shift, ui.KeySlash, s.ShowKeys)
	}
	width, height := c.Size()
	compact := width < 760
	s.height = height
	if s.reading == 0 {
		s.immersive = false
	}
	// Immersive reading covers the sidebar without touching the preference, which comes back when it ends.
	hidden := s.views().SidebarHidden || s.immersive
	if v := s.viewerSet(); v != s.viewed {
		// A viewer changed in Settings: the list is drawn again in place, at the entry that was first in view.
		if s.viewed != (viewerSet{}) {
			s.pin = s.anchor
			s.pass.rebase()
		}
		s.viewed = v
	}
	if want := s.views().OldestFirst; want != s.oldest {
		// The order changed: the list for the new order is shown, from its top.
		s.show(func() { s.oldest = want })
	}
	s.wantMore = false
	s.markTick(c, s.deps.Now())
	s.look(c) // reads the rects of the last frame
	clear(s.rects)
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(t.Background).Children(func() {
		if !hidden {
			s.sidebar(c, compact)
		}
		if s.reading != 0 {
			e := s.shownCopy(s.entries[s.index(s.reading)])
			s.reader.View(c, reader.Nav{Entry: e, Icon: s.bitmap(s.iconOf(e.SourceID)), Scope: s.scopeName(),
				Previous: s.article(-1) >= 0, Next: s.article(1) >= 0 || s.next != "", Keys: !open, Controls: hidden, Immersive: s.immersive,
				Column: mainColumn(t, width, hidden, compact)})
			return
		}
		// The toolbar row floats over the content, which scrolls under it and fades under the scroll edge.
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			s.hint(c)
			s.stale(c)
			s.content(c, now, compact)
			theme.Edge(c, s.edge)
			s.toolbar(c, compact, hidden)
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
	s.viewPictures(c)
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
	s.reader.Flush(c)
}

// listKeys registers the keys of the list and of the sidebar. They work wherever the focus is, but not while the search
// takes the letters typed, a dialog shows or the reader, which has its own, is open. They are registered after the
// frame is laid out, which is when it is known whether the focus is in the sidebar; when it came there by the keyboard
// (H, Left, Tab, the arrows) the keys of the entries are inert, as the arrows and Enter are the sidebar's own, and
// Escape, L and Right go back to the list. Focus a click put on a kind or subscription does not count: the keys of
// the entries act on the list at once.
func (s *Screen) listKeys(c *ui.Context, open bool) {
	if s.typing || s.filtering || open || s.reading != 0 {
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
		{ui.KeySpace, func() { s.openPictures(s.selected, 0) }},
		{ui.KeyS, func() { s.toggleStar(s.selected) }}, {ui.KeyM, func() { s.toggleRead(s.selected) }},
		{ui.KeyN, s.nextUnread}, {ui.KeyV, s.markVisible},
		{ui.KeyU, func() { s.Run(ShowUnread) }}, {ui.KeyA, func() { s.Run(ShowAll) }},
		{ui.KeyR, func() { s.Run(LoadNew) }},
	} {
		c.OnShortcut(0, k.key, k.do)
	}
	c.OnShortcut(ui.Shift, ui.Key8, func() { s.Run(ShowStarred) })
	c.OnShortcut(ui.Shift, ui.KeyA, func() { s.Run(MarkAllRead) })
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

// toSidebar gives the focus to the subscription chosen, else the kind chosen; a sidebar that is hidden shows first.
func (s *Screen) toSidebar() {
	if s.views().SidebarHidden {
		s.toggleSidebar()
		if s.views().SidebarHidden {
			return
		}
	}
	s.sideFocus, s.inSide, s.sideKeyed = true, true, true
}

// toggleSidebar hides the sidebar if it shows and shows it if it is hidden, and keeps the choice on this device. The
// focus that was in the sidebar goes with it.
func (s *Screen) toggleSidebar() {
	if s.deps.Change == nil {
		return
	}
	p := s.views()
	p.SidebarHidden = !p.SidebarHidden
	if p.SidebarHidden {
		s.sideFocus, s.inSide, s.sideKeyed = false, false, false
	}
	s.deps.Change(p)
}

// toggleImmersive turns immersive reading on while an article is read, and off, and says so to screen readers. The
// sidebar's focus goes with the sidebar. Outside the reader it does nothing.
func (s *Screen) toggleImmersive() {
	if s.reading == 0 {
		return
	}
	s.immersive = !s.immersive
	s.announce = s.text.ImmersiveOff
	if s.immersive {
		s.announce = s.text.ImmersiveOn
		s.sideFocus, s.inSide, s.sideKeyed = false, false, false
	}
}

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
	if r := s.result; r != nil {
		s.result = nil
		icon := icons.CircleCheckBig
		if r.alert {
			icon = icons.CircleAlert
		}
		c.AddToast(ui.Toast{ID: refreshedToast, Title: r.text, Data: theme.ToastLook{Icon: icon}})
	}
	for _, n := range s.notes {
		icon := icons.CircleCheckBig
		if n.failed {
			icon = icons.CircleAlert
		}
		c.AddToast(ui.Toast{ID: fetchedToast + strconv.FormatInt(n.id, 10), Title: n.text, Data: theme.ToastLook{Icon: icon}})
	}
	s.notes = nil
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

// shiftKey is how the key sheet writes Shift: ⇧ on macOS, Shift+ elsewhere.
func shiftKey() string {
	if runtime.GOOS == "darwin" {
		return "⇧"
	}
	return "Shift+"
}

// ShowKeys shows the key sheet, from the menu or ?.
func (s *Screen) ShowKeys() {
	if s.subs.Open() || s.pal.open {
		return
	}
	s.keys = true
}

// Command is an action of the timeline that has a key and a menu item.
type Command int

const (
	ShowUnread Command = iota
	ShowAll
	ShowStarred
	MarkAllRead
	LoadNew
	Immersive
)

// Run does the command, as its key or menu item does. Nothing happens while a dialog shows, and the commands that
// act on the list are inert where the button would be, as for mark all read in a search or under Starred.
func (s *Screen) Run(cmd Command) {
	if s.api == nil || s.keys || s.subs.Open() || s.pal.open || s.lb.open {
		return
	}
	switch cmd {
	case ShowUnread:
		s.setFilter(unread)
	case ShowAll:
		s.setFilter(everything)
	case ShowStarred:
		s.setFilter(starred)
	case MarkAllRead:
		if s.reading == 0 {
			s.markAll()
		}
	case Immersive:
		s.toggleImmersive()
	case LoadNew:
		if s.fresh > 0 && s.reading == 0 {
			s.renew()
			s.reload()
		}
	}
}

// MenuItem is an item of the Timeline menu.
type MenuItem struct {
	Label   string
	Command Command
}

// TimelineMenu is the Timeline menu and its items, in the language of the system locale. The items have no
// accelerator: the single keys they show in the key sheet must not fire while a text field has the focus, which a
// menu accelerator cannot tell.
func TimelineMenu(systemLocale string) (name string, items []MenuItem) {
	text := translations[locale.Language(systemLocale)]
	return text.TimelineMenu, []MenuItem{{text.KeyUnread, ShowUnread}, {text.KeyAll, ShowAll},
		{text.KeyStarred, ShowStarred}, {text.KeyMarkAll, MarkAllRead}, {text.KeyLoadNew, LoadNew},
		{text.KeyImmersive, Immersive}}
}

// RefreshMenuLabels are the Subscriptions menu, its item that refreshes all of them and its item that manages
// several at once, in the language of the system locale.
func RefreshMenuLabels(systemLocale string) (menu, refresh, manage string) {
	text := translations[locale.Language(systemLocale)]
	return text.SubscriptionsMenu, text.RefreshAll, text.PaletteManage
}

// HasSources reports whether there is a subscription to manage, which the menu item shows.
func (s *Screen) HasSources() bool { return s.api != nil && s.subs.HasSources() }

// Manage opens the dialog that changes several subscriptions at once, from the menu.
func (s *Screen) Manage() {
	if s.api == nil || s.keys || s.subs.Open() || s.pal.open || s.lb.open {
		return
	}
	s.subs.Manage()
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
		{s.text.KeyNextUnread, one("N")}, {s.text.KeyOpen, one("O", "↩")}, {s.text.KeyPictures, one("Space")},
		{s.text.KeyStar, one("S")},
		{s.text.KeyRead, one("M")}, {s.text.KeyMarkVisible, one("V")},
		{s.text.KeyMarkAll, [][]string{{shiftKey(), "A"}}}, {s.text.KeyUnread, one("U")}, {s.text.KeyAll, one("A")},
		{s.text.KeyStarred, one("*")}, {s.text.KeyLoadNew, one("R")},
		{s.text.KeySearch, [][]string{{"/"}, {cmd, "F"}}},
		{s.text.KeyEscape, one("Esc")}, {s.text.KeyScope, one("1–5")}, {s.text.KeySidebar, one("H", "←")},
		{s.text.KeyBack, one("L", "→", "Esc")}, {s.text.KeyMenu, menu}, {s.text.KeyUndo, [][]string{{cmd, "Z"}}}}
	reading := []key{{s.text.KeyNextArticle, one("J")}, {s.text.KeyPreviousArticle, one("K")},
		{s.text.KeyNextUnread, one("N")}, {s.text.KeyStar, one("S")}, {s.text.KeyRead, one("M")},
		{s.text.KeyOpenBrowser, one("O")}, {s.text.KeyFind, [][]string{{cmd, "F"}}}, {s.text.KeyImmersive, one("Z")}, {s.text.KeyImmersiveExit, one("Esc")},
		{s.text.KeyBack, one("Esc")}}
	anywhere := []key{{s.text.KeyPalette, [][]string{{cmd, "K"}}}, {s.text.KeyAdd, [][]string{{cmd, "N"}}}, {s.text.KeyAddContinue, [][]string{{cmd, "↩"}}}, {s.text.RefreshAll, [][]string{{cmd, "R"}}}, {s.text.KeySettings, [][]string{{cmd, ","}}},
		{s.text.KeyToggleSidebar, [][]string{{cmd, "\\"}}}, {s.text.KeyKeys, one("?")}}
	ui.DialogBase(c, &s.keys, func(backdrop, panel ui.Element) {
		theme.Scrim(c, backdrop).Padding(t.Space(4))
		panel.Column().Width(min(theme.Px(t, 560), width-t.Space(8))).MaxHeight(height - t.Space(8)).
			Label(s.text.KeysTitle)
		theme.Sheet(c, panel, theme.Dialog)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Padding(t.Space(4), t.Space(4), t.Space(2), t.Space(5)).
			Children(func() {
				ui.Text(c, s.text.KeysTitle).Grow(1).FontSize(theme.Px(t, 15)).FontWeight(600).Role(ui.RoleHeading).
					Level(2)
				x := ui.ButtonBase(c).Size(theme.Px(t, 28), theme.Px(t, 28)).Radius(t.Space(1.5)).Label(s.text.Close).
					Tooltip(s.text.Close)
				color := t.TextMuted
				if x.Hovered() {
					x.Background(t.SurfaceHover)
					color = t.Text
				}
				x.Children(func() { ui.Icon(c, icons.X).FontSize(theme.IconMedium(t)).TextColor(color) })
				if x.Clicked() {
					s.keys = false
				}
			})
		ui.Scroll(c).FillWidth().Shrink(1).MinHeight(0).Children(func() {
			cols := ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(theme.Px(t, 24)).Padding(theme.Px(t, 4), t.Space(5), t.Space(5))
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
	ui.Text(c, strings.ToUpper(name)).FontSize(theme.Px(t, 11)).FontWeight(600).TextColor(t.TextMuted).
		Margin(theme.Px(t, 12), 0, theme.Px(t, 4), 0).Role(ui.RoleHeading).Level(3)
	for _, row := range rows {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).PaddingY(theme.Px(t, 5)).Children(func() {
			ui.Text(c, row.name).Grow(1).MinWidth(0).FontSize(theme.Px(t, 12))
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Px(t, 4)).Children(func() {
				for i, keys := range row.keys {
					if i > 0 {
						ui.Text(c, s.text.Or).FontSize(theme.Px(t, 11)).TextColor(t.TextMuted)
					}
					for _, k := range keys {
						ui.Box(c).MinWidth(theme.Px(t, 22)).Height(theme.Px(t, 22)).PaddingX(theme.Px(t, 6)).Center().Radius(6).
							Background(theme.Track(t)).Border(0.5, theme.LineStrong(t)).Children(func() {
							ui.Text(c, k).FontSize(theme.Px(t, 11)).NoWrap()
						})
					}
				}
			})
		})
	}
}

// mainColumn is the width of the column right of the sidebar, the whole window while the sidebar is hidden.
func mainColumn(t *ui.Theme, width float32, hidden, compact bool) float32 {
	if hidden {
		return width
	}
	return width - (sideWidth(t, compact) + 8)
}

// sideWidth is the width of the sidebar, in units of the main theme.
func sideWidth(t *ui.Theme, compact bool) float32 {
	if compact {
		return theme.Px(t, 184)
	}
	return theme.Px(t, 216)
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
		// The top 40 are the window buttons' at the left and the button that hides the pane at the right, centered
		// on the toolbar row's middle line (the pane floats 8 from the top); the rest of it drags the window.
		ui.Row(c).FillWidth().Height(40).AlignItems(ui.Start).Justify(ui.End).
			Padding(theme.Bar/2-8-theme.Px(t, 32)/2, theme.Px(t, paneLine-32/2), 0, 0).DragWindow().Children(func() { s.sidebarButton(c, false) })
		side := s.sidebarList()
		list, failing := side.list, side.failing
		s.tabs(c, s.sideFocus && !side.onSource)
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(theme.Px(t, 4), theme.Px(t, 8), theme.Px(t, 6)).Label(s.text.Sources).Children(func() {
				s.section(c, failing)
				s.sourceTools(c)
				if len(list) == 0 && strings.TrimSpace(s.sideQuery) != "" {
					ui.Box(c).FillWidth().Padding(theme.Px(t, 6), theme.Px(t, 10)).Role(ui.RoleStatus).Label(s.text.NoMatchingSources).
						Children(func() {
							ui.Text(c, s.text.NoMatchingSources).FontSize(theme.Px(t, 13)).TextColor(t.TextMuted)
						})
				}
				ui.Column(c).FillWidth().FocusGroup(ui.Vertical).Children(func() {
					for i, src := range list {
						s.sourceItem(c, src, i == 0 && s.firstFocus)
					}
					s.firstFocus = false
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
	seg.Track.Column().FocusGroup(ui.Vertical).FillWidth().Gap(theme.Px(t, 1)).Padding(0, theme.Px(t, 8), theme.Px(t, 8)).Label(s.text.Kinds).
		Children(func() {
			for i, kind := range kinds {
				name := s.kindName(kind)
				on := i == s.tab
				tab := seg.Segment(i).FillWidth().Height(theme.Px(t, 30)).Gap(theme.Px(t, 10)).PaddingX(theme.Px(t, 10)).
					Radius(theme.Px(t, 8)).Label(name)
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
				cut := false
				tab.Children(func() {
					ui.Icon(c, kindIcon(kind)).FontSize(theme.IconMedium(t)).TextColor(icon)
					label := ui.Text(c, name).Grow(1).MinWidth(0).SingleLine().TextColor(theme.Text2(t))
					weight := 0
					if on {
						label.TextColor(t.Text).FontWeight(500)
						weight = 500
					}
					cut = theme.Cut(c, label, name, 0, weight, 1)
					if n > 0 {
						ui.Text(c, countText(n)).FontSize(theme.Px(t, 11)).FontWeight(500).FontFeatures("tnum").
							TextColor(count)
					}
				})
				if cut {
					theme.Tip(c, tab, name)
				}
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
	size, tracking := theme.Px(t, 11), theme.Px(t, 11)*0.04
	if theme.Han(name) {
		size, tracking = theme.Px(t, 12), theme.Px(t, 12)*0.02
	}
	// The label follows the icons of the rows (10 in). The button is centered on the pane's trailing line, so its
	// 22 square sits paneLineInset(22) in from the column's edge.
	ui.Row(c).FillWidth().Height(theme.Px(t, 22)).AlignItems(ui.Center).Margin(theme.Px(t, 14), 0, theme.Px(t, 4), 0).
		Padding(0, paneLineInset(t, 22), 0, theme.Px(t, 10)).
		Children(func() {
			ui.Text(c, name).Grow(1).MinWidth(0).SingleLine().FontSize(size).FontWeight(500).
				LetterSpacing(tracking).TextColor(t.TextMuted)
			switch {
			case s.failingOnly:
				if s.link(c, nil, s.text.ShowAll, s.text.ShowAll, theme.Text2(t)) {
					s.failingOnly = false
				}
			case s.refresh != nil && s.refresh.queued > 0:
				s.progressLink(c)
			case failing > 0:
				show := s.format(s.text.FailingShow, failing)
				if s.link(c, icons.CircleAlert, s.format(s.text.FailingCount, failing), show, t.Warning) {
					s.failingOnly = true
				}
			}
			b := ui.ButtonBase(c).Shrink(0).Label(s.text.AddSource).Tooltip(s.text.AddSource)
			color := t.TextMuted
			var fill ui.Color
			if b.Hovered() {
				fill = t.SurfaceHover
				color = t.Text
			}
			mx, my := theme.Hit{W: theme.Px(t, 22), H: theme.Px(t, 22), Radius: theme.Px(t, 8), Fill: fill}.Apply(c, b, func() {
				ui.Icon(c, icons.Plus).FontSize(theme.IconSmall(t)).TextColor(color)
			})
			b.Margin(my, mx, my, mx)
			if b.Clicked() {
				s.subs.Add()
			}
		})
}

// sideCache is the subscriptions the sidebar lists, kept while what they come from stays the same: drawing a frame
// must not copy and sort them again when nothing changed.
type sideCache struct {
	valid       bool
	gen         uint64
	query       string
	unreadFirst bool
	kind        string
	source      int64
	failingOnly bool

	list     []client.Source
	failing  int  // subscriptions of the kind that failed, before the list narrows to them
	onSource bool // the open subscription is in the list, whatever its kind
	runs     int  // how many times it was built
}

// sidebarList is the subscriptions to list in the sidebar: those of the open kind, narrowed to the failing ones when
// asked, to the ones the filter matches, and ordered by unread count with Unread first on. It is built again only
// when the subscriptions, the filter, the order, the kind, the open subscription or the failing narrowing change.
func (s *Screen) sidebarList() *sideCache {
	c := &s.side
	unreadFirst := s.views().SourceSort == prefs.SortUnread
	if c.valid && c.gen == s.sourcesGen && c.query == s.sideQuery && c.unreadFirst == unreadFirst &&
		c.kind == s.scope.kind && c.source == s.scope.source && c.failingOnly == s.failingOnly {
		return c
	}
	*c = sideCache{valid: true, gen: s.sourcesGen, query: s.sideQuery, unreadFirst: unreadFirst, kind: s.scope.kind,
		source: s.scope.source, failingOnly: s.failingOnly, runs: c.runs + 1}
	q := strings.ToLower(strings.TrimSpace(s.sideQuery))
	type row struct {
		src  client.Source
		name string
	}
	var rows []row
	for _, src := range s.sources {
		name := strings.ToLower(subscriptions.Name(src))
		named := strings.Contains(name, q)
		if src.ID == s.scope.source && named && (!s.failingOnly || failed(src)) {
			c.onSource = true
		}
		if s.scope.kind != "" && src.Kind != s.scope.kind {
			continue
		}
		if failed(src) {
			c.failing++
		}
		if named && (!s.failingOnly || failed(src)) {
			rows = append(rows, row{src, name})
		}
	}
	if unreadFirst {
		unread := func(src client.Source) int {
			if src.Paused {
				return 0
			}
			return src.UnreadCount
		}
		slices.SortStableFunc(rows, func(a, b row) int {
			if d := cmp.Compare(unread(b.src), unread(a.src)); d != 0 {
				return d
			}
			return strings.Compare(a.name, b.name)
		})
	}
	if c.failing == 0 && s.failingOnly {
		// Nothing fails any more, so the narrowing to failing subscriptions ends and the list is built without it.
		s.failingOnly = false
		return s.sidebarList()
	}
	c.list = make([]client.Source, len(rows))
	for i, r := range rows {
		c.list[i] = r.src
	}
	return c
}

// paneLine is how far from the pane's right edge the centers of its icon buttons are: the button that hides the
// pane, the add button, the Unread first toggle and the settings gear all sit on it. paneEdge is the pane's side
// padding of the lists, which the rows of the section and the filter sit inside of.
const (
	paneLine = 25
	paneEdge = 8
)

// paneLineInset is how far in from the column's edge (paneEdge in) a square button of the given size is, so that
// its center is on paneLine.
func paneLineInset(t *ui.Theme, size float32) float32 {
	return theme.Px(t, paneLine-size/2-paneEdge)
}

// sourceTools is the row under the section header: the filter field of the subscriptions, which grows, and the toggle
// that puts those with unread entries first. It is not there while there are no subscriptions.
func (s *Screen) sourceTools(c *ui.Context) {
	if len(s.sources) == 0 {
		return
	}
	t := c.Theme()
	unreadFirst := s.views().SourceSort == prefs.SortUnread
	ui.Row(c).FillWidth().Height(theme.Px(t, 28)).Gap(theme.Px(t, 4)).Margin(0, 0, theme.Px(t, 6), 0).
		Padding(0, paneLineInset(t, 28), 0, 0).Children(func() {
		box := ui.Row(c).Key("filter").Grow(1).MinWidth(0).Height(theme.Px(t, 28)).Gap(theme.Px(t, 6)).
			Padding(0, theme.Px(t, 4), 0, theme.Px(t, 10)).Radius(theme.Px(t, 8)).AlignItems(ui.Center)
		switch {
		case s.filtering:
			box.Background(theme.Ink(t, 0.085)).Shadow(0, 0, 0, 2, t.Focus)
		case box.Hovered():
			box.Background(theme.Ink(t, 0.085))
		default:
			box.Background(theme.Ink(t, 0.055))
		}
		theme.Fade(box)
		var input ui.Element
		cleared := false
		box.Children(func() {
			ui.Icon(c, icons.Search).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
			input = ui.TextInputBase(c, &s.sideQuery).Grow(1).MinWidth(0).TextColor(t.Text).Label(s.text.FilterSources).
				Placeholder(s.text.FilterSources)
			input.OnShortcut(0, ui.KeyEscape, func() {
				if s.sideQuery != "" {
					s.sideQuery = ""
					return
				}
				s.toSidebar()
			})
			for _, k := range []ui.Key{ui.KeyDown, ui.KeyEnter} {
				input.OnShortcut(0, k, func() { s.firstFocus = true })
			}
			if s.sideQuery != "" {
				clear := ui.ButtonBase(c).Label(s.text.ClearFilter).Tooltip(s.text.ClearFilter)
				var fill ui.Color
				if clear.Hovered() {
					fill = t.SurfaceHover
				}
				mx, my := theme.Hit{W: theme.Px(t, 20), H: theme.Px(t, 20), Radius: theme.Px(t, 6), Fill: fill}.Apply(c, clear, func() {
					ui.Icon(c, icons.X).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
				})
				clear.Margin(my, mx, my, mx)
				cleared = clear.Clicked()
			}
		})
		if box.Clicked() {
			input.Focus()
		}
		s.filtering = input.Focused()
		if cleared {
			s.sideQuery = ""
			input.Focus()
		}

		b := ui.ButtonBase(c).Key("unread-first").Size(theme.Px(t, 28), theme.Px(t, 28)).Shrink(0).Radius(theme.Px(t, 8)).
			Role(ui.RoleToggleButton).Label(s.text.UnreadFirst).Tooltip(s.text.UnreadFirst).Checked(unreadFirst)
		color := t.TextMuted
		switch {
		case unreadFirst:
			b.Background(theme.Chosen(t))
			color = t.Accent
		case b.Hovered():
			b.Background(t.SurfaceHover)
			color = theme.Text2(t)
		}
		theme.Fade(b)
		b.Children(func() { ui.Icon(c, icons.ArrowUpDown).FontSize(theme.IconSmall(t)).TextColor(color) })
		if b.Clicked() && s.deps.Change != nil {
			p := s.views()
			p.SourceSort = prefs.SortUnread
			if unreadFirst {
				p.SourceSort = prefs.SortTitle
			}
			s.deps.Change(p)
		}
	})
}

// link is a small text button in the sidebar's section header, with an optional icon; it reports a click.
func (s *Screen) link(c *ui.Context, icon *ui.SVG, text, label string, color ui.Color) bool {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(label).Tooltip(label)
	var fill ui.Color
	if b.Hovered() {
		fill = t.SurfaceHover
	}
	_, y := theme.Hit{H: theme.Px(t, 20), Radius: theme.Px(t, 6), Fill: fill, PadX: theme.Px(t, 6), Gap: theme.Px(t, 4)}.Apply(c, b, func() {
		if icon != nil {
			ui.Icon(c, icon).FontSize(theme.IconSmall(t)).TextColor(color)
		}
		ui.Text(c, text).SingleLine().FontSize(theme.Px(t, 11)).FontWeight(500).TextColor(color)
	})
	b.Margin(y, 0, y, 0)
	return b.Clicked()
}

// progressLink is the count of "refresh all" in the heading of the sidebar, in the place of the failing count: a
// turning loader and how many of the subscriptions were checked.
func (s *Screen) progressLink(c *ui.Context) {
	t := c.Theme()
	r := s.refresh
	label := s.format(strings.ReplaceAll(s.text.RefreshingName, "{done}", strconv.Itoa(r.done)), r.queued)
	ui.Row(c).Height(theme.Px(t, 20)).AlignItems(ui.Center).Gap(theme.Px(t, 4)).PaddingX(theme.Px(t, 6)).Label(label).
		Children(func() {
			spin := ui.Icon(c, icons.LoaderCircle).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
			spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
			ui.Text(c, strconv.Itoa(r.done)+"/"+strconv.Itoa(r.queued)).SingleLine().FontSize(theme.Px(t, 11)).FontWeight(500).
				TextColor(t.TextMuted)
		})
}

func (s *Screen) sourceItem(c *ui.Context, src client.Source, first bool) {
	t := c.Theme()
	chosen := s.scope.source == src.ID
	name := subscriptions.Name(src)
	label := name
	_, fetching := s.watching[src.ID]
	if failed(src) {
		label = s.failingName(name, src.UnreadCount)
	}
	if fetching {
		label = strings.ReplaceAll(s.text.Fetching, "{name}", name)
	}
	item := ui.ButtonBase(c).Key(src.ID).FillWidth().Height(theme.Px(t, 30)).Gap(theme.Px(t, 10)).PaddingX(theme.Px(t, 10)).
		Radius(theme.Px(t, 8)).Label(label).ContextMenu(func(m *ui.Menu) { s.subs.Menu(m, src) })
	if src.UnreadCount > 0 && !src.Paused {
		item.Description(s.format(s.text.UnreadCount, src.UnreadCount))
	}
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
	if (chosen && s.sideFocus) || first {
		item.Focus()
		s.sideKeyed = s.sideKeyed || first
	}
	s.sideKeyed = s.sideKeyed || item.FocusVisible()
	cut := false
	item.Children(func() {
		s.glyph(c, name, src.IconURL, 18)
		color := theme.Text2(t)
		if src.Paused {
			color = t.TextMuted
		}
		title := ui.Text(c, name).Grow(1).MinWidth(0).SingleLine().TextColor(color)
		weight := 0
		if chosen {
			title.TextColor(t.Text).FontWeight(500)
			weight = 500
		}
		cut = theme.Cut(c, title, name, 0, weight, 1)
		if fetching && !src.Paused {
			ui.Spinner(c).FontSize(theme.Px(t, 13)).Label(label)
		} else if src.Paused {
			ui.Box(c).Label(s.text.PausedMark).Tooltip(s.text.PausedMark).Role(ui.RoleImage).Children(func() {
				ui.Icon(c, icons.Pause).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
			})
		} else if src.LastError != "" {
			reason := subscriptions.FailureDetail(s.lang, src, s.reason(src))
			ui.Box(c).Label(reason).Tooltip(reason).Role(ui.RoleImage).Children(func() {
				ui.Icon(c, icons.CircleAlert).FontSize(theme.IconSmall(t)).TextColor(t.Warning)
			})
		}
		if src.UnreadCount > 0 && !src.Paused {
			ui.Text(c, countText(src.UnreadCount)).FontSize(theme.Px(t, 11)).FontWeight(500).FontFeatures("tnum").
				TextColor(count)
		}
	})
	if cut {
		theme.Tip(c, item, name)
	}
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
		theme.Monogram(c, name, size)
		return
	}
	radius := theme.GlyphRadius(size)
	ui.Box(c).Size(theme.Px(t, size), theme.Px(t, size)).Radius(radius).Clip().Children(func() {
		ui.Image(c, bitmap).Fit(ui.Cover).Fill()
		ui.Box(c).Absolute().Fill().Radius(radius).Border(0.5, theme.Ink(t, 0.2))
	})
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

func (s *Screen) serverBar(c *ui.Context) {
	t := c.Theme()
	name, icon := s.text.ThisComputer, icons.Laptop
	if s.server != "" {
		name, icon = s.server, icons.Server
	}
	// Inset like the rows above it, so its hover fill is a rounded row inside the pane, clear of its corner.
	ui.Row(c).FillWidth().Height(theme.Px(t, 40)).AlignItems(ui.Center).Padding(0, theme.Px(t, paneEdge)+paneLineInset(t, 28), 0, theme.Px(t, 8)).
		Children(func() {
			dot := t.Success
			if s.offline {
				dot = t.Warning
			}
			bar := ui.ButtonBase(c).Grow(1).MinWidth(0).Height(theme.Px(t, 30)).Gap(theme.Px(t, 8)).PaddingX(theme.Px(t, 10)).
				Radius(theme.Px(t, 8)).Label(strings.ReplaceAll(s.text.Server, "{server}", name))
			if bar.Hovered() {
				bar.Background(t.SurfaceHover)
			}
			theme.Fade(bar)
			bar.Children(func() {
				ui.Icon(c, icon).FontSize(theme.IconMedium(t)).TextColor(t.TextMuted)
				ui.Text(c, name).Grow(1).MinWidth(0).SingleLine().FontSize(theme.Px(t, 12)).LetterSpacing(theme.Px(t, 12) * 0.01).
					TextColor(t.TextMuted)
				ui.Box(c).Size(6, 6).Radius(theme.Pill(6)).Background(dot)
			})
			if bar.Clicked() {
				s.deps.OpenConnect()
			}
			gear := ui.ButtonBase(c).Size(theme.Px(t, 28), theme.Px(t, 28)).Radius(theme.Px(t, 8)).Label(s.text.Settings).
				Tooltip(s.text.Settings)
			color := t.TextMuted
			if gear.Hovered() {
				gear.Background(t.SurfaceHover)
				color = t.Text
			}
			gear.Children(func() { ui.Icon(c, icons.Settings).FontSize(theme.IconMedium(t)).TextColor(color) })
			if gear.Clicked() {
				s.deps.OpenSettings()
			}
		})
}

// narrowColumn is the main column width under which the toolbar spends less: the segments are padded 7, the
// gap between the items is 4 and the unread count is left out. fieldColumn is the width from which search is a field;
// under it search is an icon that expands when it is clicked. Both are about the column, not the window, so hiding the
// sidebar gives the toolbar its room back.
const (
	narrowColumn = 580
	fieldColumn  = 736
)

// toolbar is three parts on one 52 high row: the title at the leading end, the segments in the middle, and at the
// trailing end a group of icon buttons with search last. The title's and the trailing part's boxes grow equally from
// nothing, so the segments sit in the middle of the column however long the title is.
func (s *Screen) toolbar(c *ui.Context, compact, hidden bool) {
	t := c.Theme()
	bar := c.TitleBar()
	// The window controls sit over the pane; with the pane hidden they sit over the toolbar, whose left gives them room.
	left := t.Space(3)
	if hidden {
		left += bar.Left
	}
	width, _ := c.Size()
	column := mainColumn(t, width, hidden, compact)
	narrow := column < narrowColumn
	field := column >= fieldColumn
	gap := theme.Px(t, 10)
	if narrow {
		gap = theme.Px(t, 4)
	}
	// Keyed, so the search field keeps its focus when the hint above it in the column comes and goes.
	ui.Row(c).Key("toolbar").Absolute().Top(0).Left(0).Right(0).Height(theme.Bar).AlignItems(ui.Center).Gap(gap).
		Padding(0, t.Space(3)+bar.Right, 0, left).DragWindow().Children(func() {
		ui.Row(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Gap(gap).Children(func() {
			if hidden {
				s.sidebarButton(c, true)
			}
			ui.Text(c, s.scopeName()).FontSize(theme.Px(t, 15)).FontWeight(600).SingleLine().MinWidth(theme.Px(t, 40)).Shrink(1).
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
			// In a narrow column the count gives way before the title does.
			if sub != "" && !narrow {
				ui.Text(c, sub).FontSize(theme.Px(t, 12)).FontFeatures("tnum").TextColor(t.TextMuted).NoWrap()
			}
			s.editButton(c)
		})
		s.filters(c, narrow)
		ui.Row(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Justify(ui.End).Gap(gap).Children(func() {
			// Where search is only an icon, it takes the room of the other buttons while it is open.
			open := field || s.searchOpen || s.searchBox || s.typing || s.query != ""
			if open && !field {
				s.searchField(c, true)
				return
			}
			// The icon buttons are one group with the same gap between them, whatever the column.
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Px(t, 4)).Shrink(0).Children(func() {
				s.budgetButton(c, !field)
				s.gridButton(c)
				s.markAllButton(c)
			})
			if field {
				s.searchField(c, false)
				return
			}
			s.searchIcon(c)
		})
	})
}

func (s *Screen) filters(c *ui.Context, compact bool) {
	t := c.Theme()
	before := s.filter
	seg := ui.SegmentedBase(c, &s.filter, 3)
	theme.Capsule(c, seg.Track.Height(theme.Px(t, 32)).Shrink(0).Padding(theme.Px(t, 3)).Gap(2).Radius(theme.Px(t, 16)), false).Label(s.text.Show).
		Children(func() {
			for i, item := range []struct {
				icon *ui.SVG
				name string
			}{{icons.Inbox, s.text.Everything}, {icons.CircleDot, s.text.Unread}, {icons.Star, s.text.Starred}} {
				on := i == s.filter
				pad := theme.Px(t, 11)
				if compact {
					pad = theme.Px(t, 7)
				}
				b := seg.Segment(i).Height(theme.Px(t, 26)).Gap(theme.Px(t, 4)).PaddingX(pad).Radius(theme.Pill(theme.Px(t, 26))).
					Label(item.name)
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
					ui.Icon(c, item.icon).FontSize(theme.IconSmall(t)).TextColor(color)
					if !compact {
						ui.Text(c, item.name).FontSize(theme.Px(t, 12)).FontWeight(500).NoWrap().TextColor(color)
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
// icon and a dot when narrow). Its menu lists the budgets, then under a hairline the two orders; choosing a budget or
// an order reloads the list from the top. Oldest first tints the clock like a budget does, with no text beside it.
func (s *Screen) budgetButton(c *ui.Context, compact bool) {
	t := c.Theme()
	name := s.budgetName()
	if s.oldest {
		name += s.text.NameJoin + s.text.OrderOldestNote
	}
	timed := s.budget != 0
	on := timed || s.oldest
	b := ui.ButtonBase(c).Key("budget").Height(theme.Px(t, 32)).Shrink(0).Radius(theme.Px(t, 8)).Label(name).Tooltip(name)
	color := toolFill(t, b)
	if on {
		color = t.Accent
		fill := theme.Soft(t, t.Accent)
		if b.Hovered() || b.Pressed() {
			fill = fill.Mix(t.Accent, 0.12)
		}
		b.Background(fill)
	}
	if timed && !compact {
		b.Gap(theme.Px(t, 5)).Padding(0, theme.Px(t, 12), 0, theme.Px(t, 10))
	} else {
		b.Width(theme.Px(t, 32))
	}
	b.Children(func() {
		ui.Icon(c, icons.Clock).FontSize(theme.IconMedium(t)).TextColor(color)
		if timed && !compact {
			ui.Text(c, s.format(s.text.TimeChip, s.budget)).FontSize(theme.Px(t, 12)).
				FontWeight(500).FontFeatures("tnum").NoWrap().TextColor(t.Accent)
		}
		if on && compact {
			ui.Box(c).Absolute().Top(theme.Px(t, 4)).Right(theme.Px(t, 4)).Size(theme.Px(t, 7), theme.Px(t, 7)).Radius(theme.Pill(theme.Px(t, 7))).
				Background(t.Accent)
		}
	})
	b.Menu(func(m *ui.Menu) {
		if m.Item(s.text.TimeAny).Checked(s.budget == 0).Chosen() {
			s.setBudget(0)
		}
		for _, n := range budgets {
			label := s.format(s.text.TimeUpTo, n)
			if m.Item(label).Checked(s.budget == n).Chosen() {
				s.setBudget(n)
			}
		}
		m.Separator()
		if m.Item(s.text.OrderNewest).Checked(!s.oldest).Chosen() && s.oldest {
			s.switchOrder()
		}
		if m.Item(s.text.OrderOldest).Checked(s.oldest).Chosen() && !s.oldest {
			s.switchOrder()
		}
		// The menu is native and its rows have no second line, so the note is a disabled row of its own.
		m.Item(s.text.OrderOldestNote).Disabled(true)
	})
}

// sidebarButton hides the sidebar or shows it: a bare 32 square icon button with the panel icon, named by what it does.
func (s *Screen) sidebarButton(c *ui.Context, hidden bool) {
	t := c.Theme()
	name := s.text.HideSidebar
	if hidden {
		name = s.text.ShowSidebar
	}
	b := ui.ButtonBase(c).Key("sidebar").Size(theme.Px(t, 32), theme.Px(t, 32)).Shrink(0).Radius(theme.Px(t, 8)).Label(name).Tooltip(name)
	color := toolFill(t, b)
	b.Children(func() { ui.Icon(c, icons.PanelLeft).FontSize(theme.IconMedium(t)).TextColor(color) })
	if b.Clicked() {
		s.toggleSidebar()
	}
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
	b := ui.ButtonBase(c).Key("grid").Size(theme.Px(t, 32), theme.Px(t, 32)).Shrink(0).Radius(theme.Px(t, 8)).Label(name).Tooltip(name)
	color := toolFill(t, b)
	b.Children(func() { ui.Icon(c, icon).FontSize(theme.IconMedium(t)).TextColor(color) })
	if b.Clicked() {
		s.switchGrid()
	}
}

// tight tells whether the list density of this device is Compact.
func (s *Screen) tight() bool { return s.views().Density == prefs.DensityCompact }

// dense is a size of the mockup under the list density: comfortable, or compact where the density is Compact.
func (s *Screen) dense(t *ui.Theme, comfortable, compact float32) float32 {
	if s.tight() {
		return theme.Px(t, compact)
	}
	return theme.Px(t, comfortable)
}

// switchOrder asks for the other order. The list follows when the preferences change, in the next frame.
func (s *Screen) switchOrder() {
	if s.deps.Change == nil {
		return
	}
	p := s.views()
	p.OldestFirst = !s.oldest
	s.deps.Change(p)
	if p.OldestFirst {
		s.announce = s.text.OrderOldest
	} else {
		s.announce = s.text.OrderNewest
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
// let it: the magnifier, the input, and a button clearing it, as Escape does. It is 200 wide and gives room before
// anything else does; opened from the icon of a narrow toolbar (opened) it stays until it loses the focus with no
// text in it.
func (s *Screen) searchField(c *ui.Context, opened bool) {
	t := c.Theme()
	box := ui.Row(c).Key("search").Width(theme.Px(t, 200)).MinWidth(theme.Px(t, 48)).Shrink(100).Height(theme.Px(t, 32)).Gap(theme.Px(t, 6)).Padding(0, theme.Px(t, 6), 0, theme.Px(t, 12)).
		Radius(theme.Px(t, 16))
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
		ui.Icon(c, icons.Search).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
		input = ui.TextInputBase(c, &s.query).Grow(1).MinWidth(0).Label(s.text.SearchLabel).
			Placeholder(s.text.Search)
		if s.syntaxShown() {
			input.Description(s.syntaxDescription())
		}
		if s.query != "" {
			input.OnShortcut(0, ui.KeyEscape, func() { s.clearing = true })
			clear := ui.ButtonBase(c).Label(s.text.Clear)
			var fill ui.Color
			if clear.Hovered() {
				fill = t.SurfaceHover
			}
			mx, my := theme.Hit{W: theme.Px(t, 20), H: theme.Px(t, 20), Radius: theme.Px(t, 6), Fill: fill}.Apply(c, clear, func() {
				ui.Icon(c, icons.X).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
			})
			clear.Margin(my, mx, my, mx)
			if clear.Clicked() {
				cleared = true
			}
		}
	})
	if box.Clicked() || s.searchBox {
		input.Focus()
		s.searchBox = false
		s.searchOpen = opened
	}
	s.typing = input.Focused()
	if opened && s.searchOpen && !s.typing && s.focusSeen {
		s.searchOpen = false
	}
	s.focusSeen = s.typing
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

// searchIcon is search in a toolbar too narrow for the field: a bare 32 square icon button that opens the field.
func (s *Screen) searchIcon(c *ui.Context) {
	t := c.Theme()
	b := ui.ButtonBase(c).Key("search").Size(theme.Px(t, 32), theme.Px(t, 32)).Shrink(0).Radius(theme.Px(t, 8)).Label(s.text.Search).
		Tooltip(s.text.Search)
	color := toolFill(t, b)
	b.Children(func() { ui.Icon(c, icons.Search).FontSize(theme.IconMedium(t)).TextColor(color) })
	if b.Clicked() {
		s.searchBox = true
	}
}

// editButton edits the subscription chosen in the sidebar.
func (s *Screen) editButton(c *ui.Context) {
	i := slices.IndexFunc(s.sources, func(src client.Source) bool { return src.ID == s.scope.source })
	if s.scope.source == 0 || i < 0 {
		return
	}
	t := c.Theme()
	b := ui.ButtonBase(c).Size(theme.Px(t, 32), theme.Px(t, 32)).Shrink(0).Radius(theme.Px(t, 8)).Label(s.text.EditSource).
		Tooltip(s.text.EditSource)
	color := toolFill(t, b)
	b.Children(func() { ui.Icon(c, icons.Pencil).FontSize(theme.IconMedium(t)).TextColor(color) })
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
	b := ui.ButtonBase(c).Size(theme.Px(t, 32), theme.Px(t, 32)).Shrink(0).Radius(theme.Px(t, 8)).Label(label).Tooltip(label).Disabled(!can)
	color := theme.Disabled(t)
	if can {
		color = toolFill(t, b)
	}
	b.Children(func() { ui.Icon(c, icons.CheckCheck).FontSize(theme.IconMedium(t)).TextColor(color) })
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
	t := c.Theme()
	switch {
	case s.limited():
		// Under the toolbar row, the content starting below it.
		ui.Row(c).AlignSelf(ui.Stretch).AlignItems(ui.Center).Gap(theme.Px(t, 8)).Margin(theme.Bar+4, theme.Px(t, 12), 4).
			Padding(theme.Px(t, 7), theme.Px(t, 14)).Radius(theme.Px(t, 12)).Background(theme.Soft(t, t.Warning)).Role(ui.RoleStatus).
			Children(func() {
				ui.Icon(c, icons.CircleAlert).FontSize(theme.IconSmall(t)).TextColor(t.Warning)
				ui.Text(c, s.text.Limited).FontSize(theme.Px(t, 12)).TextColor(theme.Text2(t))
			})
	case s.syntaxShown():
		s.syntax(c)
	}
}

// syntaxShown tells whether the line of search syntax shows: while the search field has the focus and is empty.
func (s *Screen) syntaxShown() bool {
	return s.typing && s.query == ""
}

// syntaxWords are the operator words of the syntax line with what follows each, in the order shown; the operator is
// drawn medium and the example regular. A phrase and an exclusion put the example inside and after the mark.
func (s *Screen) syntaxWords() [][2]string {
	return [][2]string{{"from:", s.text.SyntaxFrom}, {"kind:", "video"}, {"after:", "2026-01-01"},
		{`"`, s.text.SyntaxPhrase + `"`}, {"-", s.text.SyntaxExclude}}
}

// syntaxDescription is the syntax line as one text, which the search field takes as its description.
func (s *Screen) syntaxDescription() string {
	parts := []string{s.text.SyntaxLead}
	for _, w := range s.syntaxWords() {
		parts = append(parts, w[0]+w[1])
	}
	return strings.Join(parts, " ")
}

// syntax is the one line of search syntax in the strip under the toolbar: information, not a caution, so the Track fill
// and not the warning tint. It wraps to a second line in a narrow column.
func (s *Screen) syntax(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Key("syntax").AlignSelf(ui.Stretch).AlignItems(ui.Start).Gap(theme.Px(t, 8)).Margin(theme.Bar+4, theme.Px(t, 12), 4).
		Padding(theme.Px(t, 7), theme.Px(t, 14)).Radius(theme.Px(t, 12)).Background(theme.Track(t)).Children(func() {
		ui.Icon(c, icons.Info).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted).Margin(theme.Px(t, 1), 0, 0, 0)
		ui.Row(c).Wrap().Grow(1).MinWidth(0).GapX(theme.Px(t, 12)).GapY(theme.Px(t, 4)).Children(func() {
			ui.Text(c, s.text.SyntaxLead).FontSize(theme.Px(t, 12)).TextColor(theme.Text2(t)).NoWrap()
			for _, w := range s.syntaxWords() {
				ui.RichText(c, ui.Span{Text: w[0], Weight: 500, Color: t.Text}, ui.Span{Text: w[1], Color: theme.Text2(t)}).
					FontSize(theme.Px(t, 12)).NoWrap()
			}
		})
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
	if s.limited() {
		top = 4
	}
	row := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, 6)).Height(staleHeight).Role(ui.RoleStatus)
	if s.inColumn() {
		maxWidth := theme.Px(t, 680)
		if s.scope.kind == "social" {
			maxWidth = theme.Px(t, 720)
		}
		row.MaxWidth(maxWidth).Margin(top, ui.Auto, 0).Padding(0, theme.Px(t, 20), 0, theme.Px(t, 36))
	} else {
		row.Margin(top, 0, 0).Padding(0, theme.Px(t, 20))
	}
	row.Children(func() {
		ui.Icon(c, icons.WifiOff).FontSize(theme.IconSmall(t)).TextColor(t.Warning)
		ui.Text(c, s.staleText()).FontSize(theme.Px(t, 12)).FontFeatures("tnum").TextColor(t.TextMuted).SingleLine().
			MinWidth(0).Shrink(1)
		busy := s.polling || s.checking
		name, color := s.text.Retry, t.Accent
		if busy {
			name, color = s.text.Retrying, t.TextMuted
		}
		b := ui.ButtonBase(c).Shrink(0).Label(name).Disabled(busy)
		var fill ui.Color
		if b.Hovered() && !busy {
			fill = t.SurfaceHover
		}
		_, y := theme.Hit{H: theme.Px(t, 22), Radius: theme.Px(t, 6), Fill: fill, PadX: theme.Px(t, 8)}.Apply(c, b, func() {
			ui.Text(c, name).FontSize(theme.Px(t, 12)).FontWeight(500).TextColor(color).NoWrap()
		})
		b.Margin(y, 0, y, 0)
		if b.Clicked() {
			s.polledAt = time.Time{}
			c.Invalidate()
		}
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
	b := ui.ButtonBase(c).Attach(ui.AnchorTop, ui.AnchorTop).Top(theme.Px(t, 60)).Height(theme.Px(t, 28)).Gap(t.Space(1.5)).
		Padding(0, t.Space(3), 0, t.Space(2.5)).Radius(theme.Px(t, 14)).Background(t.Accent).
		Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.18)).Label(text)
	if b.Hovered() {
		b.Background(t.AccentHover)
	}
	b.Children(func() {
		ui.Icon(c, icons.ArrowUp).FontSize(theme.IconSmall(t)).TextColor(t.AccentText)
		ui.Text(c, text).FontSize(theme.Px(t, 12)).FontWeight(500).TextColor(t.AccentText).NoWrap()
	})
	if b.Clicked() {
		s.renew()
		s.reload()
	}
}

// content is the list, or the state that stands in for it.
func (s *Screen) content(c *ui.Context, now time.Time, compact bool) {
	s.divider = -1
	s.edge = false
	area := ui.Column(c).Grow(1).FillWidth().MinHeight(0)
	area.Children(func() {
		switch {
		case s.api == nil:
		case s.loading:
			// A page that comes back at once is not worth a flash of bars: draw nothing until the wait is over.
			if wait := skeletonDelay - now.Sub(s.loadedAt); wait > 0 {
				c.After(wait)
				return
			}
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
				s.empty(c, icons.Clock, s.format(s.text.NothingFits, s.budget),
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
	if s.toTop || s.pin != 0 {
		s.pass.rebase() // the list was put in place by the program
	}
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
	if s.limited() {
		top = 0
	}
	if s.staleShown() {
		top += staleHeight
	}
	return top
}

func (s *Screen) skeleton(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().MaxWidth(theme.Px(t, 680)).Margin(0, ui.Auto).
		Padding(s.top()+t.Space(1), t.Space(5), t.Space(1)).
		Label(s.text.Loading).Role(ui.RoleProgress).Children(func() {
		for i := range 4 {
			ui.Row(c).Key(i).FillWidth().AlignItems(ui.Start).Gap(theme.Px(t, 14)).
				Padding(theme.Px(t, 16), theme.Px(t, 16), theme.Px(t, 16), theme.Px(t, 28)).Children(func() {
				ui.Column(c).Grow(1).Gap(t.Space(2)).Children(func() {
					for _, bar := range []struct{ width, height float32 }{{0.36, 10}, {0.7, 14}, {0.92, 10}} {
						ui.Box(c).Width(theme.Px(t, 420) * bar.width).Height(theme.Px(t, bar.height)).Radius(theme.Pill(theme.Px(t, bar.height))).
							Background(theme.Track(t))
					}
				})
				if i%2 == 0 {
					ui.Box(c).Size(theme.Px(t, 96), theme.Px(t, 64)).Radius(theme.Px(t, 8)).Background(theme.Track(t)).
						Margin(theme.Px(t, 24), 0, 0, 0)
				}
			})
		}
	})
}

// DropBusy reports whether files dropped now are turned away, because a dialog is open, and says so with a toast.
func (s *Screen) DropBusy() bool {
	if s.keys || s.subs.Open() || s.pal.open || s.lb.open {
		s.toast = s.text.DropBusy
		return true
	}
	return false
}

func (s *Screen) empty(c *ui.Context, icon *ui.SVG, title, hint string, failed bool) {
	t := c.Theme()
	col := ui.Column(c).Fill().Center().Gap(t.Space(3)).Padding(s.top()+t.Space(6), t.Space(6), t.Space(6)).
		Role(ui.RoleStatus)
	// The first-run empty state takes OPML files dropped on it, handing them to the settings' import like the rest
	// of the window.
	first := title == s.text.NoSubscriptions
	if first {
		if over := col.FileDragOver(); over {
			icon, title, hint = icons.Upload, s.text.DropTitle, s.text.DropHint
			col.Background(theme.Soft(t, t.Accent)).Border(1.5, t.Accent).BorderStyle(ui.BorderDashed).Radius(t.Radius)
			first = false
		}
		if paths := col.DroppedFiles(); len(paths) > 0 && s.deps.DropFiles != nil {
			s.deps.DropFiles(paths)
		}
	}
	col.Children(func() {
		fill, color := t.Surface, t.TextMuted
		if failed {
			fill, color = theme.Soft(t, t.Danger), t.Danger
		}
		ui.Box(c).Size(theme.Px(t, 52), theme.Px(t, 52)).Radius(theme.Px(t, 16)).Center().Background(fill).
			Children(func() { ui.Icon(c, icon).FontSize(theme.IconLarge(t)).TextColor(color) })
		ui.Text(c, title).FontSize(theme.Px(t, 15)).FontWeight(600).Role(ui.RoleHeading).Level(2)
		ui.Text(c, hint).MaxWidth(theme.Px(t, 300)).TextColor(t.TextMuted).Wrap()
		if failed {
			b := ui.ButtonBase(c).Height(theme.Px(t, 32)).Gap(theme.Px(t, 6)).PaddingX(theme.Px(t, 14)).Radius(theme.Px(t, 10)).
				Background(theme.Track(t))
			if b.Pressed() {
				b.Background(theme.Track(t).Mix(theme.Ink(t, 1), 0.16))
			} else if b.Hovered() {
				b.Background(theme.Track(t).Mix(theme.Ink(t, 1), 0.1))
			}
			theme.Fade(b)
			b.Children(func() {
				ui.Icon(c, icons.RefreshCw).FontSize(theme.IconMedium(t)).TextColor(t.Text)
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
			b := ui.ButtonBase(c).Height(theme.Px(t, 32)).PaddingX(theme.Px(t, 14)).Radius(theme.Px(t, 10)).Background(theme.Track(t))
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
		if first {
			ui.Row(c).Wrap().Justify(ui.Center).Gap(t.Space(2)).Children(func() {
				b := ui.ButtonBase(c).Height(t.Space(8)).Gap(t.Space(1.5)).PaddingX(t.Space(3.5)).Radius(t.Radius).
					Background(t.Accent)
				if b.Pressed() {
					b.Background(t.AccentPressed)
				} else if b.Hovered() {
					b.Background(t.AccentHover)
				}
				b.Children(func() {
					ui.Icon(c, icons.Plus).FontSize(theme.IconMedium(t)).TextColor(t.AccentText)
					ui.Text(c, s.text.AddFirst).NoWrap().FontWeight(500).TextColor(t.AccentText)
				})
				if b.Clicked() {
					s.subs.Add()
				}
				// Importing a file and restoring a backup both go to Import and export in Settings.
				for _, quiet := range []struct {
					icon  *ui.SVG
					label string
				}{{icons.Upload, s.text.ImportOPML}, {icons.Database, s.text.RestoreBackup}} {
					i := ui.ButtonBase(c).Height(t.Space(8)).Gap(t.Space(1.5)).PaddingX(t.Space(3.5)).Radius(t.Radius).
						Background(theme.Track(t))
					if i.Pressed() {
						i.Background(theme.Track(t).Mix(theme.Ink(t, 1), 0.16))
					} else if i.Hovered() {
						i.Background(theme.Track(t).Mix(theme.Ink(t, 1), 0.1))
					}
					theme.Fade(i)
					i.Children(func() {
						ui.Icon(c, quiet.icon).FontSize(theme.IconMedium(t)).TextColor(t.Text)
						ui.Text(c, quiet.label).NoWrap().FontWeight(500)
					})
					if i.Clicked() {
						s.deps.OpenImport()
					}
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
				ui.Icon(c, icons.CircleAlert).FontSize(theme.IconSmall(t)).TextColor(t.Danger)
				ui.Text(c, s.text.LoadMoreFailed).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted)
				b := ui.ButtonBase(c).Height(theme.Px(t, 24)).Gap(t.Space(1)).PaddingX(t.Space(2)).Radius(t.Space(1.5)).
					Label(s.text.Retry)
				if b.Hovered() {
					b.Background(t.SurfaceHover)
				}
				b.Children(func() {
					ui.Icon(c, icons.RefreshCw).FontSize(theme.IconSmall(t)).TextColor(t.Accent)
					ui.Text(c, s.text.Retry).FontSize(theme.Px(t, 12)).TextColor(t.Accent)
				})
				if b.Clicked() {
					s.moreErr = false
					s.loadMore()
				}
				return
			}
			spin := ui.Icon(c, icons.LoaderCircle).FontSize(theme.IconMedium(t)).TextColor(t.TextMuted)
			spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
			ui.Text(c, s.text.LoadingMore).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted)
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
	if opacity := fade(entry, now); opacity < 1 {
		e.Opacity(opacity)
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

// entryMenu fills the context menu of the entry id: the actions of its row, and "Mark above as read" and "Mark below as read" under them.
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
	if m.Item(s.text.CopyLink).Disabled(e.URL == "").Chosen() {
		s.reader.CopyLink(e)
	}
	m.Separator()
	if m.Item(s.text.CopyMarkdown).Chosen() {
		s.reader.Export(e, false)
	}
	if m.Item(s.text.SaveMarkdown).Disabled(s.deps.Save == nil).Chosen() {
		s.reader.Export(e, true)
	}
	m.Separator()
	if m.Item(s.text.MarkAbove).Disabled(!s.canMarkAbove(id)).Chosen() {
		s.markAbove(id)
	}
	if m.Item(s.text.MarkBelow).Disabled(!s.canMarkBelow(id)).Chosen() {
		s.markBelow(id)
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
	bar := ui.Row(c).Key("actions").Gap(theme.Px(t, 2)).Transition(actionFade)
	switch at {
	case onCard:
		bar.Attach(ui.AnchorBottomRight, ui.AnchorBottomRight)
	case centered:
		bar.Attach(ui.AnchorRight, ui.AnchorRight).Right(theme.Px(t, 10))
	case belowMeta:
		bar.Attach(ui.AnchorBottomRight, ui.AnchorBottomRight).Bottom(theme.Px(t, 14)).Right(theme.Px(t, 10))
	default:
		bar.Attach(ui.AnchorTopRight, ui.AnchorTopRight).Top(theme.Px(t, 14)).Right(theme.Px(t, 10))
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
	b := ui.ButtonBase(c).Size(theme.Px(t, 28), theme.Px(t, 24)).Radius(theme.Px(t, 6)).Label(label).Tooltip(label)
	switch {
	case b.Pressed():
		b.Background(theme.Chosen(t))
	case b.Hovered():
		b.Background(t.SurfacePressed)
		if !keep {
			color = t.Text
		}
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(theme.IconMedium(t)).TextColor(color) })
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
	ui.Row(c).FillWidth().Height(theme.Px(t, 20)).MinWidth(0).AlignItems(ui.Center).Gap(theme.Px(t, 6)).Padding(0, right, 0, 0).
		Children(func() {
			if lead && !e.IsRead {
				ui.Box(c).Size(theme.Px(t, 7), theme.Px(t, 7)).Radius(theme.Pill(theme.Px(t, 7))).Background(t.Accent)
			}
			s.glyph(c, e.SourceTitle, s.iconOf(e.SourceID), 15)
			ui.Text(c, e.SourceTitle).FontSize(theme.Px(t, 12)).FontWeight(500).SingleLine().MinWidth(0).Shrink(1).
				TextColor(source)
			ui.Text(c, "·").FontSize(theme.Px(t, 12)).TextColor(muted).Opacity(0.7)
			ui.Text(c, s.when(e, now)).FontSize(theme.Px(t, 12)).FontFeatures("tnum").TextColor(muted).NoWrap()
			if !lead {
				s.alsoChip(c, e, muted)
			}
			if reading := s.readLabel(e); reading != "" {
				ui.Text(c, "·").FontSize(theme.Px(t, 12)).TextColor(muted).Opacity(0.7)
				ui.Text(c, reading).FontSize(theme.Px(t, 12)).FontFeatures("tnum").TextColor(muted).NoWrap()
			}
			if e.Starred {
				ui.Icon(c, icons.Star).FontSize(theme.IconSmall(t)).TextColor(theme.Star(t))
			}
			if s.waitingFor(e.ID) >= 0 {
				ui.Icon(c, icons.Upload).FontSize(theme.IconSmall(t)).TextColor(muted)
			}
			if platform && e.Platform != "" {
				ui.Text(c, "·").FontSize(theme.Px(t, 12)).TextColor(muted).Opacity(0.7)
				ui.Icon(c, icons.Play).FontSize(theme.IconSmall(t)).TextColor(muted)
				ui.Text(c, e.Platform).FontSize(theme.Px(t, 12)).SingleLine().MinWidth(0).Shrink(1).TextColor(muted)
			}
		})
}

// grouped tells whether e shows the other sources that carry its article: the server sends them only where articles
// are shown once, and a single subscription and a search list every copy.
func (s *Screen) grouped(e client.Entry) bool {
	return e.Group != nil && len(e.Group.Entries) > 0 && s.scope.source == 0 && s.searched == ""
}

// alsoChip is the small "N more sources" button after the time of a row; it shows or hides the list of the other
// sources under the row. The click does not reach the row under it.
func (s *Screen) alsoChip(c *ui.Context, e client.Entry, muted ui.Color) {
	if !s.grouped(e) {
		return
	}
	t := c.Theme()
	n := len(e.Group.Entries)
	open := s.also == e.ID
	b := ui.ButtonBase(c).Key("also").Label(s.format(s.text.AlsoName, n)).Expanded(open)
	color := muted
	var fill ui.Color
	switch {
	case b.Pressed():
		fill = theme.Chosen(t)
		color = theme.Text2(t)
	case b.Hovered():
		fill = t.SurfacePressed
		color = theme.Text2(t)
	case open:
		color = theme.Text2(t)
	}
	_, y := theme.Hit{H: theme.Px(t, 20), Radius: theme.Px(t, 6), Fill: fill}.Apply(c, b, func() {
		ui.Row(c).AlignItems(ui.Center).Gap(theme.Px(t, 3)).Padding(0, theme.Px(t, 6), 0, theme.Px(t, 4)).Children(func() {
			ui.Icon(c, icons.Layers).FontSize(theme.IconSmall(t)).TextColor(color)
			ui.Text(c, s.format(s.text.AlsoChip, n)).FontSize(theme.Px(t, 12)).NoWrap().TextColor(color)
		})
	})
	b.Margin(y, 0, y, 0)
	if b.Clicked() {
		if open {
			s.also = 0
		} else {
			s.also = e.ID
		}
	}
}

// alsoList is the other sources of the row that is open, one line each under its content: the icon and name of the
// source, the title and the time. A click opens that copy in the reader.
func (s *Screen) alsoList(c *ui.Context, e client.Entry, now time.Time, muted ui.Color) {
	if s.also != e.ID || !s.grouped(e) {
		return
	}
	t := c.Theme()
	ui.Row(c).Key("also-list").FillWidth().AlignItems(ui.Stretch).Margin(theme.Px(t, 6), 0, 0, theme.Px(t, 1)).Role(ui.RoleList).
		Label(s.text.AlsoHead).Children(func() {
		ui.Box(c).Width(2).Shrink(0).Background(t.Border)
		ui.Column(c).Grow(1).MinWidth(0).Padding(0, 0, 0, theme.Px(t, 12)).Children(func() {
			for _, m := range e.Group.Entries {
				s.alsoItem(c, e, m, now, muted)
			}
		})
	})
}

// alsoItem is one other source in the list under a row.
func (s *Screen) alsoItem(c *ui.Context, row client.Entry, m client.GroupMember, now time.Time, muted ui.Color) {
	t := c.Theme()
	title := m.Title
	if title == "" {
		title = m.URL
	}
	source, color := theme.Text2(t), t.Text
	if row.IsRead {
		color = muted
	}
	b := ui.ButtonBase(c).Key(m.ID).FillWidth().Height(theme.Px(t, 30)).Margin(0, theme.Px(t, -8), 0, theme.Px(t, -4)).
		Padding(0, theme.Px(t, 8), 0, theme.Px(t, 4)).Radius(theme.Px(t, 8)).Role(ui.RoleListItem).
		Label(m.SourceTitle + s.text.Separator + title)
	switch {
	case b.Pressed():
		b.Background(t.SurfacePressed)
	case b.Hovered():
		b.Background(t.SurfaceHover)
	}
	b.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, 8)).Children(func() {
			s.glyph(c, m.SourceTitle, s.iconOf(m.SourceID), 16)
			ui.Text(c, m.SourceTitle).FontSize(theme.Px(t, 12)).FontWeight(500).SingleLine().MaxWidth(theme.Px(t, 140)).Shrink(0).
				TextColor(source)
			ui.Text(c, title).FontSize(theme.Px(t, 13)).SingleLine().Grow(1).MinWidth(0).TextColor(color)
			ui.Text(c, s.when(client.Entry{CreatedAt: m.CreatedAt}, now)).FontSize(theme.Px(t, 12)).FontFeatures("tnum").
				TextColor(muted).NoWrap()
		})
	})
	if b.Clicked() {
		s.openCopy(c, row.ID, m)
	}
}

// openCopy opens the copy m of the article of the row id, from the list under the row: an article in the reader, the
// other kinds in the browser. The row stays the place in the list, and the article is marked read as a whole.
func (s *Screen) openCopy(c *ui.Context, id int64, m client.GroupMember) {
	i := s.index(id)
	if i < 0 {
		return
	}
	e := s.entries[i]
	if e.Kind != "article" {
		if m.URL != "" {
			c.OpenURLThen(m.URL, func(err error) {
				if err != nil {
					s.toast = s.text.OpenFailed
					c.Invalidate()
				}
			})
		}
		s.selected = i
		s.markOpened(e, false)
		return
	}
	s.read(i)
	s.showCopy(m.ID)
}

// showCopy has the reader show the copy of the article read that has the entry id; the row it belongs to, 0, shows the
// row itself.
func (s *Screen) showCopy(id int64) {
	i := s.index(s.reading)
	if i < 0 {
		return
	}
	s.copyID = id
	s.reader.Open(s.shownCopy(s.entries[i]))
}

// shownCopy is the entry the reader shows for the row e: the row, or the copy of its article chosen from the list. Its
// read and starred state are the whole article's, and its group the other copies, the row's own among them.
func (s *Screen) shownCopy(e client.Entry) client.Entry {
	if s.copyID == 0 || e.Group == nil {
		return e
	}
	k := slices.IndexFunc(e.Group.Entries, func(m client.GroupMember) bool { return m.ID == s.copyID })
	if k < 0 {
		return e
	}
	m := e.Group.Entries[k]
	others := []client.GroupMember{{ID: e.ID, SourceID: e.SourceID, SourceTitle: e.SourceTitle, Title: e.Title,
		URL: e.URL, IsRead: e.IsRead, Starred: e.Starred, CreatedAt: e.CreatedAt}}
	others = append(others, slices.Delete(slices.Clone(e.Group.Entries), k, k+1)...)
	e.ID, e.SourceID, e.SourceTitle, e.Title, e.URL, e.CreatedAt = m.ID, m.SourceID, m.SourceTitle, m.Title, m.URL, m.CreatedAt
	e.Group = &client.Group{By: e.Group.By, Entries: others}
	return e
}

// maxMarked is the most pieces marked in one text.
const maxMarked = 8

// terms is what the search asks to find, its words and phrases; the excluded ones and the operators are not in it.
func (s *Screen) terms() []string {
	if s.termsOf != s.searched {
		s.termsOf, s.termList = s.searched, search.Parse(s.searched).Include
	}
	return s.termList
}

// asciiLower lowers A to Z only, so the result is as long as the text and its offsets are the text's.
func asciiLower(text string) string {
	b := []byte(text)
	for i, ch := range b {
		if ch >= 'A' && ch <= 'Z' {
			b[i] = ch + 'a' - 'A'
		}
	}
	return string(b)
}

// matches returns the byte ranges of text that hold one of the terms, ignoring the case of A to Z, in order and with
// overlapping or touching ranges joined, the first maxMarked of them.
func matches(text string, terms []string) [][2]int {
	lower := asciiLower(text)
	var found [][2]int
	for _, term := range terms {
		term = asciiLower(term)
		for from := 0; term != ""; {
			i := strings.Index(lower[from:], term)
			if i < 0 {
				break
			}
			found = append(found, [2]int{from + i, from + i + len(term)})
			from += i + len(term)
		}
	}
	slices.SortFunc(found, func(a, b [2]int) int { return cmp.Compare(a[0], b[0]) })
	var joined [][2]int
	for _, r := range found {
		if n := len(joined); n > 0 && r[0] <= joined[n-1][1] {
			joined[n-1][1] = max(joined[n-1][1], r[1])
		} else {
			joined = append(joined, r)
		}
	}
	return joined[:min(len(joined), maxMarked)]
}

// marked splits text into spans, the pieces holding the words and phrases of the search drawn on the warning tint
// and underlined, so that they do not rest on color alone. A marked piece is Text2 where the text is TextMuted, which
// is under 4.5:1 on the tint. The marks are paint only: they are not read out and do not split the row's name.
func (s *Screen) marked(c *ui.Context, text string, color ui.Color) []ui.Span {
	t := c.Theme()
	var spans []ui.Span
	at := 0
	fill, thickness := theme.Soft(t, t.Warning), theme.Px(t, 1.5)
	if c.Preferences().HighContrast {
		fill, thickness = t.Warning.Alpha(0.16), theme.Px(t, 2)
	}
	ink := color
	if color == t.TextMuted {
		ink = theme.Text2(t)
	}
	for _, r := range matches(text, s.terms()) {
		if r[0] > at {
			spans = append(spans, ui.Span{Text: text[at:r[0]], Color: color})
		}
		spans = append(spans, ui.Span{Text: text[r[0]:r[1]], Color: ink, Background: fill, Radius: theme.Px(t, 4),
			Underline: true, DecorationColor: ink, DecorationThickness: thickness})
		at = r[1]
	}
	return append(spans, ui.Span{Text: text[at:], Color: color})
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
	maxWidth := theme.Px(t, 680)
	if s.scope.kind == "social" {
		maxWidth = theme.Px(t, 720)
	}
	n := len(s.entries)
	if s.hasFooter() {
		n++
	}
	s.divider = s.dividerAt()
	ui.List(c, &s.list, n, func(i int) {
		// The row is 16 wider than the column on each side, so its text lines up with the toolbar's.
		ui.Column(c).FillWidth().MaxWidth(maxWidth).Margin(0, ui.Auto).PaddingX(theme.Px(t, 8)).Children(func() {
			if i >= len(s.entries) {
				s.footer(c)
				return
			}
			if i == s.divider {
				s.divide(c, i)
			}
			s.row(c, i, now, compact)
		})
	}).TrackScroll(&s.scrolls[0]).Grow(1).FillWidth().Padding(s.top()+t.Space(1), 0, t.Space(6), 0).
		ScrollbarInsets(s.top(), 0, 0, 0).Label(s.scopeName())
	s.edge = s.scrolls[0].Y > 0
}

// divide is the divider above the first entry at or older than the marker of what the reader last looked at: a hairline
// with the count of the entries above it, newer than the marker and loaded, in the middle. It is a button that
// dismisses it, named by its text.
func (s *Screen) divide(c *ui.Context, newer int) {
	t := c.Theme()
	count := strconv.Itoa(newer)
	if newer > maxNewCount {
		count = strconv.Itoa(maxNewCount) + "+"
	}
	label := strings.ReplaceAll(s.text.LastLooked, "{n}", count)
	b := ui.ButtonBase(c).Key("looked").FillWidth().Height(theme.Px(t, 28)).Padding(0, theme.Px(t, 16), 0, theme.Px(t, 28)).Label(label)
	color := t.Accent
	if b.Hovered() {
		color = t.AccentHover
	}
	b.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, 10)).Children(func() {
			ui.Box(c).Grow(1).Height(1).Background(t.Border)
			ui.Text(c, label).FontSize(theme.Px(t, 12)).FontWeight(500).TextColor(color).NoWrap()
			ui.Box(c).Grow(1).Height(1).Background(t.Border)
		})
	})
	if b.Clicked() {
		s.dismiss()
	}
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
		r = ui.Row(c).Key(e.ID).FillWidth().Height(s.dense(t, 36, 30)).AlignItems(ui.Center).Gap(theme.Px(t, 12)).
			Padding(0, theme.Px(t, 16), 0, theme.Px(t, 28)).Radius(theme.Px(t, 10))
	case e.Kind == "social" && v.SocialViewer == prefs.ViewCompact:
		r = ui.Row(c).Key(e.ID).FillWidth().AlignItems(ui.Start).Gap(s.dense(t, 14, 10)).
			Padding(s.dense(t, 12, 8), theme.Px(t, 16), s.dense(t, 12, 8), theme.Px(t, 28)).Radius(theme.Px(t, 12))
	default:
		r = ui.Row(c).Key(e.ID).FillWidth().AlignItems(ui.Start).Gap(s.dense(t, 14, 10)).
			Padding(s.dense(t, 16, 10), theme.Px(t, 16), s.dense(t, 16, 10), theme.Px(t, 28)).Radius(theme.Px(t, 12))
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
		// The divider, the marker and the dot are always there, clear when they do not show: one that came and went
		// would move the ids of the content after it, and a click pressed on a picture as the row is chosen would
		// land on an element that is no longer there.
		none := ui.RGBA(0, 0, 0, 0)
		divider := ui.Box(c).Absolute().Top(0).Left(theme.Px(t, 28)).Right(theme.Px(t, 16)).Height(1).Background(none)
		if i > 0 && !quiet && i != s.divider {
			divider.Background(t.Border)
		}
		marker := float32(18)
		if oneLine {
			marker = 14
		}
		mark := ui.Box(c).Attach(ui.AnchorLeft, ui.AnchorLeft).Left(8).Size(2.5, marker).Radius(2).Background(none)
		if chosen {
			mark.Background(theme.Marker(t))
		}
		dot := ui.Box(c).Absolute().Left(theme.Px(t, 14)).Size(theme.Px(t, 7), theme.Px(t, 7)).Radius(theme.Pill(theme.Px(t, 7))).Background(none)
		if oneLine {
			dot.Top(theme.Px(t, 18) - theme.Px(t, 3.5))
		} else {
			dot.Top(theme.Px(t, 23))
		}
		if !e.IsRead {
			dot.Background(t.Accent)
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
	text := ui.RichText(c, s.marked(c, title(e), color)...).FontSize(theme.Px(t, size)).FontWeight(weight).Wrap().MaxLines(2)
	line, tracking := float32(1.35), -theme.Px(t, size)*0.005
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
	ui.Column(c).Grow(1).MinWidth(0).Gap(s.dense(t, 3, 2)).Children(func() {
		s.meta(c, e, now, false, theme.Px(t, 88), muted, false)
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
				spans := []ui.Span{{Text: summary, Color: color}} // a generated summary is not the text searched
				if !generated {
					spans = s.marked(c, summary, color)
				}
				text := ui.RichText(c, spans...).FontSize(theme.Px(t, 13)).MaxLines(2).LineHeight(1.5)
				if theme.Han(summary) {
					text.LineHeight(1.7)
				}
				if generated {
					text.Grow(1).MinWidth(0)
				}
			}
			if generated {
				// The sparkle says the text was generated; its label is how assistive technology says so.
				ui.Row(c).Gap(theme.Px(t, 6)).AlignItems(ui.Start).Children(func() {
					ui.Icon(c, icons.Sparkles).FontSize(theme.IconSmall(t)).TextColor(muted).Margin(theme.Px(t, 4), 0, 0, 0).
						Label(s.text.AISummary).Role(ui.RoleImage)
					line()
				})
			} else {
				line()
			}
		}
		s.alsoList(c, e, now, muted)
	})
	if e.ThumbnailURL != "" {
		// Under the meta line, so the row's actions never cover it, and never taller than the text: it starts 24 down.
		s.picture(c, e.ThumbnailURL, true, theme.Px(t, 8), nil).Size(theme.Px(t, 96), theme.Px(t, 64)).AlignSelf(ui.Start).
			Margin(theme.Px(t, 24), 0, 0, 0)
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
	field := theme.Px(t, 128)
	if narrow {
		field = theme.Px(t, 96)
	}
	s.glyph(c, e.SourceTitle, s.iconOf(e.SourceID), 16)
	ui.Text(c, e.SourceTitle).FontSize(theme.Px(t, 12)).FontWeight(500).SingleLine().Width(field).Shrink(0).TextColor(source)
	ui.RichText(c, s.marked(c, title(e), color)...).FontSize(theme.Px(t, 13)).FontWeight(weight).SingleLine().Grow(1).
		MinWidth(0)
	if e.Starred {
		ui.Icon(c, icons.Star).FontSize(theme.IconSmall(t)).TextColor(theme.Star(t))
	}
	if s.waitingFor(e.ID) >= 0 {
		ui.Icon(c, icons.Upload).FontSize(theme.IconSmall(t)).TextColor(muted)
	}
	when, width := ui.Color(muted), theme.Px(t, 56)
	if acting {
		when, width = ui.RGBA(0, 0, 0, 0), theme.Px(t, 84)
	}
	ui.Text(c, s.when(e, now)).FontSize(theme.Px(t, 12)).FontFeatures("tnum").TextColor(when).NoWrap().MinWidth(width).
		TextAlign(ui.End)
}

// largeRow is a video with its cover across the row, then the title and the meta line under it.
func (s *Screen) largeRow(c *ui.Context, e client.Entry, now time.Time, muted ui.Color) {
	t := c.Theme()
	ui.Column(c).Grow(1).MinWidth(0).Gap(s.dense(t, 8, 6)).Children(func() {
		s.coverOf(c, e, true, true, theme.Px(t, 12), 44).FillWidth().MaxWidth(theme.Px(t, 640))
		s.titleText(c, e, 15, muted)
		s.meta(c, e, now, false, theme.Px(t, 88), muted, true)
		s.alsoList(c, e, now, muted)
	})
}

func (s *Screen) cover(c *ui.Context, e client.Entry, load, play bool) ui.Element {
	return s.coverOf(c, e, load, play, theme.Px(c.Theme(), 10), 36)
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
			ui.Box(c).Attach(ui.AnchorCenter, ui.AnchorCenter).Size(disc, disc).Radius(theme.Pill(disc)).Center().
				Background(ui.RGBA(28, 24, 18, 0.4)).Children(func() {
				ui.Icon(c, icons.Play).FontSize(theme.IconMedium(t)).TextColor(ui.Hex("#ffffff")).Margin(0, 0, 0, 2)
			})
		}
		if e.DurationSec > 0 {
			ui.Text(c, duration(e.DurationSec)).Attach(ui.AnchorBottomRight, ui.AnchorBottomRight).Right(6).Bottom(6).
				Padding(1, 6).Radius(4).Background(ui.RGBA(28, 24, 18, 0.62)).FontSize(theme.Px(t, 11)).FontWeight(500).
				FontFeatures("tnum").TextColor(ui.Hex("#ffffff"))
		}
	}).AspectRatio(16.0 / 9).Label(label).Role(ui.RoleImage)
}

func (s *Screen) videoRow(c *ui.Context, e client.Entry, now time.Time, compact bool, muted ui.Color) {
	t := c.Theme()
	width := theme.Px(t, 184)
	if compact {
		width = theme.Px(t, 136)
	}
	s.cover(c, e, true, true).Width(width).Margin(theme.Px(t, 24), 0, 0, 0).AlignSelf(ui.Start)
	ui.Column(c).Grow(1).MinWidth(0).Gap(s.dense(t, 3, 2)).Children(func() {
		s.meta(c, e, now, false, theme.Px(t, 88), muted, false)
		s.titleText(c, e, 15, muted)
		if e.Platform != "" {
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Px(t, 6)).Children(func() {
				ui.Icon(c, icons.Play).FontSize(theme.IconSmall(t)).TextColor(muted)
				ui.Text(c, e.Platform).FontSize(theme.Px(t, 12)).TextColor(muted)
			})
		}
		s.alsoList(c, e, now, muted)
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
	ui.Column(c).Grow(1).MinWidth(0).Gap(s.dense(t, 3, 2)).Children(func() {
		s.meta(c, e, now, false, theme.Px(t, 88), muted, false)
		if e.Author != "" || e.Platform != "" {
			ui.RichText(c, ui.Span{Text: e.Author, Weight: 500, Color: author},
				ui.Span{Text: " " + e.Platform, Color: muted}).FontSize(theme.Px(t, 13)).SingleLine()
		}
		text := strings.TrimSpace(e.Summary)
		if text == "" {
			text = e.Title
		}
		body := ui.RichText(c, s.marked(c, text, color)...).FontSize(theme.Px(t, 13)).MaxLines(6).Wrap().LineHeight(1.55)
		if theme.Han(text) {
			body.LineHeight(1.7)
		}
		s.gallery(c, e)
		s.alsoList(c, e, now, muted)
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
	ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Px(t, 4)).Children(func() {
		s.meta(c, e, now, false, theme.Px(t, 88), muted, false)
		spans := []ui.Span{{Text: e.Author, Weight: 500, Color: author}, {Text: " " + e.Platform + " ", Color: muted}}
		if e.Author == "" && e.Platform == "" {
			spans = nil
		}
		body := ui.RichText(c, append(spans, s.marked(c, text, color)...)...).FontSize(theme.Px(t, 13)).MaxLines(2).Wrap().
			LineHeight(1.55)
		if theme.Han(text) {
			body.LineHeight(1.7)
		}
		if n := len(e.Images); n > 1 {
			s.strip(c, e)
		}
		s.alsoList(c, e, now, muted)
	})
	if len(e.Images) == 1 {
		// Under the meta line, so the row's actions never cover it, as an article's thumbnail is.
		s.zoom(s.pictureOf(c, e.Images[0].URL, true, theme.Px(t, 8), theme.IconMedium(t), nil), s.index(e.ID), 0).Size(theme.Px(t, 56), theme.Px(t, 56)).
			AlignSelf(ui.Start).Margin(theme.Px(t, 24), 0, 0, 0).Label(s.format(s.text.Pictures, 1)).Role(ui.RoleImage)
	}
}

// strip is the pictures of a post as a row of at most four small squares, the last saying how many more follow.
func (s *Screen) strip(c *ui.Context, e client.Entry) {
	t := c.Theme()
	n := len(e.Images)
	shown := min(n, 4)
	ui.Row(c).Gap(theme.Px(t, 4)).Margin(theme.Px(t, 4), 0, 0, 0).Label(s.format(s.text.Pictures, n)).Role(ui.RoleImage).
		Children(func() {
			for k, img := range e.Images[:shown] {
				var over func()
				if k == shown-1 && n > shown {
					more := s.format(s.text.MorePictures, n-shown)
					over = func() {
						ui.Box(c).Fill().Absolute().Center().Background(ui.RGBA(28, 24, 18, 0.5)).Label(more).
							Children(func() {
								ui.Text(c, "+"+strconv.Itoa(n-shown)).FontSize(theme.Px(t, 11)).FontWeight(600).
									FontFeatures("tnum").TextColor(ui.Hex("#ffffff"))
							})
					}
				}
				s.zoom(s.pictureOf(c, img.URL, true, theme.Px(t, 6), theme.IconMedium(t), over).Key(k), s.index(e.ID), k).
					Size(theme.Px(t, 40), theme.Px(t, 40))
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
	grid := ui.Grid(c).Columns(shown).Gap(4).Radius(theme.Px(t, 12)).Clip().Margin(theme.Px(t, 8), 0, 0, 0).
		Label(s.format(s.text.Pictures, n)).Role(ui.RoleImage)
	switch {
	case shown == 1:
		grid.MaxWidth(theme.Px(t, 384))
	case s.scope.kind == "social":
		grid.MaxWidth(theme.Px(t, 520))
	default:
		grid.MaxWidth(theme.Px(t, 440))
	}
	grid.Children(func() {
		for k, img := range e.Images[:shown] {
			var over func()
			if k == shown-1 && n > shown {
				more := s.format(s.text.MorePictures, n-shown)
				over = func() {
					ui.Box(c).Fill().Absolute().Center().Background(ui.RGBA(28, 24, 18, 0.5)).Label(more).Children(func() {
						ui.Text(c, "+"+strconv.Itoa(n-shown)).FontSize(theme.Px(t, 15)).FontWeight(600).
							FontFeatures("tnum").TextColor(ui.Hex("#ffffff"))
					})
				}
			}
			p := s.zoom(s.picture(c, img.URL, true, 0, over), s.index(e.ID), k)
			switch shown {
			case 1:
				p.AspectRatio(16.0 / 10).MaxHeight(theme.Px(t, 240))
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
	ui.Column(c).Grow(1).MinWidth(0).Gap(s.dense(t, 3, 2)).Children(func() {
		s.meta(c, e, now, false, theme.Px(t, 88), muted, false)
		s.titleText(c, e, 15, muted)
		if address := firstPicture(e); address != "" {
			s.zoom(s.picture(c, address, true, theme.Px(t, 12), nil), s.index(e.ID), 0).FillWidth().AspectRatio(1.5).
				MaxHeight(theme.Px(t, 320)).Margin(theme.Px(t, 8), 0, 0, 0)
		}
		s.alsoList(c, e, now, muted)
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
	return s.pictureOf(c, address, load, radius, theme.IconLarge(c.Theme()), over)
}

// pictureOf is picture with a failed mark of the given icon size.
func (s *Screen) pictureOf(c *ui.Context, address string, load bool, radius, mark float32, over func()) ui.Element {
	t := c.Theme()
	var bitmap *ui.Bitmap
	if load {
		bitmap = s.bitmap(address)
	} else if p := s.pics.Peek(address); p != nil {
		bitmap = p.Bitmap
	}
	failed := s.pics.Peek(address) != nil && s.pics.Peek(address).Failed
	return ui.Box(c).Background(theme.Track(t)).Radius(radius).Clip().Children(func() {
		if bitmap != nil {
			ui.Image(c, bitmap).Fit(ui.Cover).Fill()
		}
		if failed {
			ui.Box(c).Fill().Absolute().Center().PassThrough().Children(func() {
				ui.Icon(c, icons.Image).FontSize(mark).TextColor(t.TextMuted)
			})
		}
		if over != nil {
			over()
		}
		ui.Box(c).Fill().Absolute().Radius(radius).PassThrough().Border(0.5, theme.Ink(t, 0.10))
	})
}

// lightbox is the pictures of one entry shown over the window, and which of them.
type lightbox struct {
	open     bool
	entry    int64
	at       int
	pictures []string // the addresses of the entry's pictures, as the feed gives them
}

// pictureList is the addresses of the pictures the lightbox pages through for a social or image entry: its pictures,
// or for an image entry without any its thumbnail.
func pictureList(e client.Entry) []string {
	if e.Kind != "social" && e.Kind != "image" {
		return nil
	}
	list := make([]string, len(e.Images))
	for k, img := range e.Images {
		list[k] = img.URL
	}
	if len(list) == 0 && e.Kind == "image" && e.ThumbnailURL != "" {
		list = append(list, e.ThumbnailURL)
	}
	return list
}

// openPictures shows the pictures of the entry at i in the lightbox, from picture at, and marks the entry read.
func (s *Screen) openPictures(i, at int) {
	if i < 0 || i >= len(s.entries) {
		return
	}
	e := s.entries[i]
	list := pictureList(e)
	if len(list) == 0 {
		return
	}
	s.selected = i
	s.lb = lightbox{open: true, entry: e.ID, at: min(max(at, 0), len(list)-1), pictures: list}
	s.announce = s.pictureName(s.lb.at)
	s.markOpened(e, true)
}

// page shows picture to of the lightbox, which stays on the first or the last one at the ends.
func (s *Screen) page(to int) {
	to = min(max(to, 0), len(s.lb.pictures)-1)
	if to == s.lb.at {
		return
	}
	s.lb.at = to
	s.announce = s.pictureName(to)
}

// pictureName is "Picture {n} of {total}" for picture at of the lightbox.
func (s *Screen) pictureName(at int) string { return s.numbered(at, len(s.lb.pictures)) }

// numbered is "Picture {n} of {total}" for picture at of total.
func (s *Screen) numbered(at, total int) string {
	return strings.NewReplacer("{n}", strconv.Itoa(at+1), "{total}", strconv.Itoa(total)).Replace(s.text.LbPicture)
}

// zoom makes the picture p, picture k of the entry at i, open the lightbox when clicked. The click does not reach the
// row under it, which would open the entry in the browser.
func (s *Screen) zoom(p ui.Element, i, k int) ui.Element {
	if i >= 0 && i < len(s.entries) {
		p.Label(s.numbered(k, len(pictureList(s.entries[i]))))
	}
	if p.Clicked() {
		s.openPictures(i, k)
	}
	return p
}

// viewPictures draws the lightbox while it is open: a solid dark layer over the whole window, the bar, the picture
// fitted between the bars and the side buttons, Previous and Next, and the caption. The picture comes through the image
// proxy and the decode cache like the rows' do; the ones either side of it are asked for so that paging finds them.
func (s *Screen) viewPictures(c *ui.Context) {
	if !s.lb.open {
		return
	}
	i := s.index(s.lb.entry)
	if i < 0 {
		s.lb.open = false
		return
	}
	e := s.entries[i]
	t := c.Theme()
	p := theme.PaneTheme(t)
	width, height := c.Size()
	list, at := s.lb.pictures, s.lb.at
	last := len(list) - 1
	var bitmap *ui.Bitmap
	failed := false
	for k := max(at-1, 0); k <= min(at+1, last); k++ {
		b := s.bitmap(list[k])
		if k == at {
			bitmap = b
			failed = s.pics.Peek(list[k]) != nil && s.pics.Peek(list[k]).Failed
		}
	}
	name := title(e)
	if e.Author != "" {
		name = e.Author
	}
	ui.DialogBase(c, &s.lb.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGB(20, 17, 13))
		panel.Fill().Label(strings.ReplaceAll(s.text.LbTitle, "{title}", name))
		if panel.Clicked() {
			s.lb.open = false
		}
		for _, k := range []struct {
			key ui.Key
			do  func()
		}{
			{ui.KeyLeft, func() { s.page(at - 1) }}, {ui.KeyRight, func() { s.page(at + 1) }},
			{ui.KeyHome, func() { s.page(0) }}, {ui.KeyEnd, func() { s.page(last) }},
			{ui.KeyO, func() { s.browse(c, i) }},
		} {
			if panel.OverlayShortcut(0, k.key) {
				k.do()
			}
		}
		// button is a bare control of the bar or beside the picture: the pane's hover and pressed fills, and the focus
		// ring when the keyboard reached it.
		button := func(b ui.Element, label string, radius float32, tip bool) bool {
			b.Radius(radius).Label(label)
			if tip {
				b.Tooltip(label)
			}
			switch {
			case b.Pressed():
				b.Background(theme.Ink(p, 0.085))
			case b.Hovered():
				b.Background(theme.Ink(p, 0.055))
			}
			if b.FocusVisible() {
				b.Shadow(0, 0, 0, 2, p.Focus)
			}
			return b.Clicked()
		}
		if last > 0 {
			ui.Box(c).Absolute().Left(0).Right(0).Top(0).Height(theme.Px(t, 56)).Center().PassThrough().Children(func() {
				ui.Text(c, strconv.Itoa(at+1)+" / "+strconv.Itoa(last+1)).FontSize(theme.Px(t, 13)).FontWeight(500).
					FontFeatures("tnum").TextColor(p.Text)
			})
		}
		ui.Row(c).Absolute().Left(0).Right(0).Top(0).Height(theme.Px(t, 56)).AlignItems(ui.Center).Justify(ui.End).
			Gap(theme.Px(t, 8)).PaddingX(theme.Px(t, 12)).Children(func() {
			browser := ui.ButtonBase(c).Height(theme.Px(t, 32)).Background(theme.Ink(p, 0.10))
			if width < 760 {
				browser.Width(theme.Px(t, 32))
			}
			if button(browser, s.text.Open, theme.Px(t, 8), false) {
				s.browse(c, i)
			}
			browser.Children(func() {
				ui.Row(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(theme.Px(t, 6)).PaddingX(theme.Px(t, 12)).Children(func() {
					ui.Icon(c, icons.ExternalLink).FontSize(theme.IconMedium(t)).TextColor(p.Text)
					if width >= 760 {
						ui.Text(c, s.text.Open).FontSize(theme.Px(t, 13)).FontWeight(500).TextColor(p.Text)
					}
				})
			})
			pic := ui.ButtonBase(c).Size(theme.Px(t, 32), theme.Px(t, 32)).Center()
			if button(pic, s.text.LbOpenPicture, theme.Px(t, 8), true) {
				c.OpenURLThen(list[at], func(err error) {
					if err != nil {
						s.toast = s.text.OpenFailed
						c.Invalidate()
					}
				})
			}
			pic.Children(func() { ui.Icon(c, icons.Image).FontSize(theme.IconMedium(t)).TextColor(theme.Text2(p)) })
			closer := ui.ButtonBase(c).Size(theme.Px(t, 32), theme.Px(t, 32)).Center()
			if button(closer, s.text.Close, theme.Px(t, 8), true) {
				s.lb.open = false
			}
			closer.Children(func() { ui.Icon(c, icons.X).FontSize(theme.IconMedium(t)).TextColor(theme.Text2(p)) })
		})
		if last > 0 {
			side := func(prev bool, icon *ui.SVG, label string, to int, off bool) {
				holder := ui.Box(c).Absolute().Top(0).Bottom(0).Center().PassThrough()
				if prev {
					holder.Left(theme.Px(t, 20))
				} else {
					holder.Right(theme.Px(t, 20))
				}
				holder.Children(func() {
					b := ui.ButtonBase(c).Size(theme.Px(t, 44), theme.Px(t, 44)).Center().Background(theme.Ink(p, 0.10)).
						Disabled(off)
					color := p.Text
					if off {
						color = theme.Disabled(p)
					} else if b.Hovered() {
						b.Background(theme.Ink(p, 0.18))
					}
					if button(b, label, theme.Px(t, 22), true) {
						s.page(to)
					}
					b.Children(func() { ui.Icon(c, icon).FontSize(theme.IconLarge(t)).TextColor(color) })
				})
			}
			side(true, icons.ChevronLeft, s.text.LbPrev, at-1, at == 0)
			side(false, icons.ChevronRight, s.text.LbNext, at+1, at == last)
		}
		fitW, fitH := width-theme.Px(t, 144), height-theme.Px(t, 100)
		ui.Box(c).Absolute().Left(theme.Px(t, 72)).Right(theme.Px(t, 72)).Top(theme.Px(t, 56)).Bottom(theme.Px(t, 44)).Center().
			Focusable().FocusRing(false).AutoFocus().Label(s.pictureName(at) + s.text.Separator + name).
			Role(ui.RoleImage).Children(func() {
			switch {
			case failed:
				w := min(fitW*0.6, theme.Px(t, 480))
				card := ui.Column(c).Size(w, w*3/4).AlignItems(ui.Center).Justify(ui.Center).Gap(theme.Px(t, 10)).Radius(8).
					Background(ui.RGB(42, 38, 33))
				card.Clicked()
				card.Children(func() {
					ui.Icon(c, icons.ImageOff).FontSize(theme.IconLarge(t)).TextColor(p.TextMuted)
					ui.Text(c, s.text.LbFailed).FontSize(theme.Px(t, 13)).TextColor(p.TextMuted)
				})
			case bitmap != nil:
				w, h := bitmap.Size()
				scale := min(1, fitW/float32(w), fitH/float32(h))
				shown := ui.Box(c).Size(float32(w)*scale, float32(h)*scale).Radius(8).Clip()
				shown.Clicked()
				shown.Children(func() { ui.Image(c, bitmap).Fill() })
			default:
				w := min(fitW, fitH*4/3, theme.Px(t, 640))
				ui.Box(c).Size(w, w*3/4).Center().Radius(8).Background(theme.Ink(p, 0.06)).Children(func() {
					ui.Spinner(c)
				})
			}
		})
		ui.Row(c).Absolute().Left(theme.Px(t, 72)).Right(theme.Px(t, 72)).Bottom(0).Height(theme.Px(t, 44)).AlignItems(ui.Center).
			Justify(ui.Center).Gap(theme.Px(t, 6)).PassThrough().Children(func() {
			if e.Kind == "social" && (e.Author != "" || e.Platform != "") {
				ui.RichText(c, ui.Span{Text: e.Author, Weight: 500, Color: theme.Text2(p)},
					ui.Span{Text: " " + e.Platform, Color: p.TextMuted}).FontSize(theme.Px(t, 12)).SingleLine()
			} else {
				ui.Text(c, title(e)).FontSize(theme.Px(t, 12)).TextColor(p.TextMuted).SingleLine()
			}
		})
	})
}

// columns returns how many columns of at least minWidth fit in the width left of the sidebar, gap apart, and how
// wide each is.
func columns(c *ui.Context, compact bool, minWidth, gap, most float32) (int, float32) {
	t := c.Theme()
	width, _ := c.Size()
	width -= sideWidth(t, compact) + 8 + 2*theme.Px(t, 24)
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
	gap, minWidth := s.dense(t, 14, 10), theme.Px(t, 200)
	if square {
		minWidth = theme.Px(t, 170)
	}
	_, cell := columns(c, compact, minWidth, gap, 0)
	height := cell*9/16 + theme.Px(t, 13.5)*2.9 + theme.Px(t, 18) + t.Space(3) + theme.Px(t, 20)
	if square {
		height = cell + theme.Px(t, 13)*1.5 + theme.Px(t, 20) + t.Space(3)
	}
	ui.GridView(c, &s.grid, len(s.entries), minWidth, height, func(i int) {
		e := s.entries[i]
		card := ui.Column(c).Fill().Gap(theme.Px(t, 8)).Radius(theme.Px(t, 10))
		if square {
			card.Gap(theme.Px(t, 4))
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
	}).TrackScroll(&s.scrolls[1]).Grow(1).FillWidth().Gap(gap).Padding(s.top()+t.Space(4), theme.Px(t, 24), t.Space(6), theme.Px(t, 24)).
		ScrollbarInsets(s.top(), 0, 0, 0).Label(s.scopeName())
	s.edge = s.scrolls[1].Y > 0
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
			ui.Row(c).Attach(ui.AnchorTopRight, ui.AnchorTopRight).Top(theme.Px(t, 6)).Right(theme.Px(t, 6)).
				AlignItems(ui.Center).Gap(theme.Px(t, 3)).Padding(theme.Px(t, 1), theme.Px(t, 6), theme.Px(t, 1), theme.Px(t, 5)).Radius(4).
				Background(ui.RGBA(28, 24, 18, 0.62)).Children(func() {
				ui.Icon(c, icons.Images).FontSize(theme.IconSmall(t)).TextColor(ui.Hex("#ffffff"))
				ui.Text(c, strconv.Itoa(n)).FontSize(theme.Px(t, 11)).FontWeight(500).FontFeatures("tnum").
					TextColor(ui.Hex("#ffffff"))
			})
		}
	}
	s.zoom(s.picture(c, firstPicture(e), true, theme.Px(t, 10), over), s.index(e.ID), 0).FillWidth().AspectRatio(1).
		Label(label).Role(ui.RoleImage)
	color, weight := t.Text, 600
	if e.IsRead {
		color, weight = t.TextMuted, 500
	}
	ui.Text(c, title(e)).FontSize(theme.Px(t, 13)).FontWeight(weight).SingleLine().MinWidth(0).TextColor(color)
}

// wallOf is the Images tab: a masonry wall, each picture in the shortest column.
func (s *Screen) wallOf(c *ui.Context, now time.Time, compact bool) {
	t := c.Theme()
	if s.toTop {
		s.wall.Y = 0
	}
	gap := s.dense(t, 14, 10)
	tile := t.Space(4)
	if s.tight() {
		tile = tile * 10 / 16
	}
	n, width := columns(c, compact, theme.Px(t, 170), gap, 3)
	_, viewHeight := c.Size()
	caption := theme.Px(t, 12)*1.45 + theme.Px(t, 18) + 2*t.Space(1) + tile
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
	s.edge = s.wall.Y > 0
	ui.Scroll(c).TrackScroll(&s.wall).Grow(1).FillWidth().ScrollbarInsets(s.top(), 0, 0, 0).Label(s.scopeName()).
		Children(func() {
			ui.Row(c).FillWidth().Gap(gap).Padding(top, theme.Px(t, 24), t.Space(6), theme.Px(t, 24)).
				AlignItems(ui.Start).Children(func() {
				for k := range cols {
					ui.Column(c).Key(k).Grow(1).Basis(0).MinWidth(0).Gap(tile).Children(func() {
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
	tile := ui.Column(c).Key(e.ID).FillWidth().Gap(theme.Px(t, 6)).Radius(theme.Px(t, 10))
	hovered, chosen := s.item(c, tile, i, now, true)
	tile.Children(func() {
		s.zoom(s.picture(c, firstPicture(e), near, theme.Px(t, 10), nil), i, 0).FillWidth().AspectRatio(aspect(e))
		color := t.Text
		if e.IsRead {
			color = t.TextMuted
		}
		ui.Text(c, title(e)).FontSize(theme.Px(t, 12)).FontWeight(500).SingleLine().TextColor(color)
		s.meta(c, e, now, true, 0, t.TextMuted, false)
		if hovered || chosen || tile.Focused() {
			s.actions(c, i, onCard)
		}
	})
}
