// Package reader is the reader: an article in its full text, drawn natively in the place of the timeline's list,
// or what the feed carries while the text is being got or when it could not be; see the reader page of the screens canvas.
// Everything it shows comes through the HTTP API.
package reader

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/prefs"
	"github.com/yangflow/oriel/internal/tracker"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/pictures"
	"github.com/yangflow/oriel/internal/ui/theme"
	"github.com/yangflow/oriel/internal/ui/theme/tokens"
	"github.com/yangflow/oriel/internal/ui/typeset"
)

//go:embed copy.json
var catalog []byte

type copy struct {
	Back, Previous, Next, Star, Unstar, MarkRead, MarkUnread, Open, FeedNote, FeedNoteHint                               string
	ExtractingNote, OpenOriginal, Extracting, Failed, FailedHint, Error, ErrorHint, Retry, Loading, FullText, OpenFailed string
	Shortcut                                                                                                             string
	Summarize, HideSummary, Translate, ShowOriginal, Summary, By, Summarizing, Cancel, SummaryFailed                     string
	Translating, Translated, Translation, Original, ShownText, SummaryReady, TranslationReady                            string
	AIUnreachable, AITimeout, AIUnauthorized, AIRateLimited, AICapReached, AIUpstream, AIEmpty, AIOff, AIOffTitle        string
	OpenSettings, Close, ReadingTime, Resumed, TextSwitch, ShowFull, ShowFeed, RetryFailed                               string
	TranslateSelection, ExplainSelection, CopyAnswer, Copied, SelectLess, SelectLessHint                                 string
	SelectionAIOff, ExplanationReady, TranslateDone, LanguageZhHans, LanguageEn                                          string
	AlsoHead, Revised, ShowChanges, HideChanges, Changes, Removed, Added, Unchanged, TitleLabel                          string
	ExitImmersive, FindLabel, Immersive, Find                                                                            string
	BackToText, Footnote, TableName                                                                                      string
	More, CopyLink, LinkCopied, CopyMarkdown, SaveMarkdown, Saved, SaveFailed, ExportFailed, MarkdownFiles               string
	PicFailed, PicFailedName, PicFailedNoAlt, PicRetry                                                                   string
	EmbedVideo, EmbedAudio, EmbedOther, EmbedTitled, EmbedName, OnlyVideo, OnlyAudio, OnlyOther, OnlyHint                string
}

var translations = func() map[string]copy {
	var values map[string]copy
	if err := json.Unmarshal(catalog, &values); err != nil {
		panic(err)
	}
	return values
}()

const (
	pollEvery   = 5 * time.Second // how often an article whose text is being got is asked for again
	aiPollEvery = 2 * time.Second // how often a summary or translation being made is asked for again
	keepCached  = 3               // articles kept for a step: the one read and the two beside it
)

// API is what the reader reads on the server; client.Session is one.
type API interface {
	Entry(ctx context.Context, id int64) (client.Article, error)
	Image(ctx context.Context, address string) ([]byte, error)
	AI(ctx context.Context, id int64) (client.AIResults, error)
	RequestAI(ctx context.Context, id int64, task string) (client.AIResult, error)
	SetEntry(ctx context.Context, id int64, patch client.EntryPatch) (client.Entry, error)
	RetryExtraction(ctx context.Context, id int64) (client.Article, error)
	Selection(ctx context.Context, task, text, title string) (answer, model string, err error)
	Revision(ctx context.Context, id int64) (client.Revision, error)
}

// Deps is what the reader does outside itself. Go runs work away from the UI thread and then the function it returns
// on the UI thread. Now is the clock and Decode turns the bytes of a picture into a bitmap, off the UI thread; they
// default to time.Now and ui.DecodeBitmap. Expired reports whether err means the server no longer takes the session,
// and then shows the connect screen. Back returns to the list, Move goes to the article by entries away, and Star and
// ToggleRead change the entry read. OpenSettings opens the AI group of the settings. Reading gives the preferences
// of this device for the article's text size and line spacing, each frame; nil means the defaults. ReadingFont gives
// the font list of the article's body, "" (and nil) for the interface font; the shell works it out when the
// preferences or the installed fonts change. Save asks where to save a file, offered under name and with the filter
// label, and returns the path chosen, "" when none was; it runs off the UI thread. OpenCopy shows the copy of the
// article that has the entry id, from the line of the other sources that carry it. Immerse turns immersive reading on
// or off; the timeline owns it, as it owns the sidebar the reading covers.
type Deps struct {
	Go           func(work func() (apply func()))
	Now          func() time.Time
	Decode       func([]byte) (*ui.Bitmap, error)
	Expired      func(err error) bool
	Back         func()
	Move         func(by int)
	NextUnread   func()
	Star         func()
	ToggleRead   func()
	OpenSettings func()
	Reading      func() prefs.Preferences
	ReadingFont  func() string
	Save         func(name, filter string) (string, error)
	OpenCopy     func(id int64)
	Immerse      func()
}

// Nav is what the timeline tells the reader each frame: the entry as the list has it, with its read and starred
// state, its subscription's site icon once decoded, the scope Back returns to, whether there are articles before and
// after it, whether the keys apply, which they do not while a dialog shows, and whether the window controls sit over
// the toolbar, which they do while the timeline hides its sidebar.
type Nav struct {
	Entry          client.Entry
	Icon           *ui.Bitmap
	Scope          string
	Previous, Next bool
	Keys           bool
	Controls       bool
	Column         float32 // width of the column the reader fills, which decides what the toolbar moves into More; 0 is the window
	Immersive      bool    // immersive reading is on: the toolbar shows only under the pointer or with the focus in it
	// Pane: the reader is the right pane of the Community beside its list. Its toolbar is laid out as in a narrow
	// window and has no Back and no previous and next arrows; Escape, J and K do those.
	Pane bool
}

const (
	summary = iota // the jobs, in the order of the server's tasks
	translation
)

var tasks = [...]string{"summary", "translation"}

// job is the summary or the translation of the article: on while its card or note shows; working from the request
// until the result is ready, polled for once the request is answered; failed with the error code.
type job struct {
	on, working, asking, failed bool
	ticket                      int // counts requests and cancels, so the answer to one cancelled is dropped
	text, model, code           string
}

// selection is the answer to the text selected in the body, which shows in a card over the foot of the reader: the
// task and the text asked about, then working, or the answer with its model, or why there is none. The answer is not
// kept anywhere; ticket counts the questions asked and the cards closed, so the answer to one before is dropped.
type selection struct {
	on, working, failed, aiOff, long bool
	task, text, answer, model, code  string
	ticket                           int
}

// cached is an article got before it is opened: its text as blocks, and the summary and translation as the server had
// them when ai is set.
type cached struct {
	id         int64
	article    client.Article
	full, feed []block
	ai         client.AIResults
	hasAI      bool
}

// Screen keeps the article read: its text as blocks, and the work under way.
type Screen struct {
	lang  string // the language of the screen
	hours int    // the hour cycle of the system, 12 or 24, 0 while unknown
	deps  Deps
	text  copy
	api   API
	gen   int        // counts articles opened, so the results for the one before are dropped
	icon  *ui.Bitmap // the site icon of the entry, as the timeline last had it

	// The requests under way end with what they were for: current with the article, ahead with the list the cache is for,
	// server with the server (what the reader asked the server to make or store stays asked when the article changes).
	current, ahead, server scope

	id       int64
	loading  bool
	failed   bool
	article  client.Article
	full     []block // the text taken from the page
	feed     []block // what the feed carries, or its summary
	polledAt time.Time
	polling  bool
	retrying bool // a request to get the full text again is under way
	feedText int  // 0 shows the full text, 1 what the feed carries, when the entry has both

	// Footnotes and tables: the block a link within the page last led to, the block of the
	// reference last followed by footnote, the scroll of each table wider than the text by its block, the blocks
	// drawn now, and the width each table was last seen to scroll, to draw it again when that changes.
	landing landing
	refs    map[int]int
	tables  map[int]*ui.ScrollState
	drawn   []block
	seen    map[int][2]float32

	scroll ui.ScrollState
	focus  bool   // give the page the focus in the next frame
	han    bool   // the article is Chinese, so its text runs taller and is typeset by the Chinese rules
	target string // the language translations are made in, "" until the server says

	pics *pictures.Queue // the pictures of the article open; its downloads are cancelled when another opens or the server changes
	// How tall each picture of the text was drawn, in the order of the text, and the top of the page area: a picture that
	// settles above the top moves the scroll offset by its change in height.
	settled map[int]settle
	nth     int
	viewTop float32

	rev        revision // the notice of an article that was updated, and what changed
	jobs       [2]job
	sel        selection
	find       find // the find row and its matches
	aiOff      bool // AI is not set up: the card says where to set it
	shown      int  // with the translation ready, 0 shows it and 1 the original
	title      string
	translated []block
	aiPolledAt time.Time
	aiPolling  bool

	cache    []cached       // the articles got ahead, the one used last at the end
	fetching map[int64]bool // the articles being got ahead
	epoch    int            // counts the servers and lists the cache is for, so an answer for one before is dropped

	// The pieces of the texts drawn as one string, for Chinese typesetting.
	titleSet, summarySet memo

	// The reading position, see docs/product/screens.md "阅读器": the fraction last stored, the one asked to restore (-1 for
	// none), the scroll offset last seen and when it moved, and the writes under way. known holds what this session
	// stored, which the list and the cache may not have yet.
	stored, resume float64
	lastY          float32
	movedAt        time.Time
	moved          bool // the offset moved since the last write
	laid           int  // frames the body was laid out in, so that the scroll range is not a stale one
	bodyHow        int
	writing        bool
	queued         *write
	known          map[int64]float64
	resumed        bool // show the toast of a restored position

	announce string
	toast    string
	clip     string // text for the clipboard, once converted
	clipped  bool
}

// landing is the block a link within the page led to: it is lit for a moment, and scrolled to once, in the next frame.
type landing struct {
	ix     int
	at     time.Time
	on     bool
	scroll bool
}

// lit is how long the block a link led to stays lit.
const lit = 1600 * time.Millisecond

// write is a reading position to store for an entry.
type write struct {
	id       int64
	progress float64
}

// memo is the pieces of a text that is drawn as one string, kept while the text stays the same, so that the
// typesetting is not worked out again each frame.
type memo struct {
	text   string
	pieces []typeset.Piece
	ok     bool
}

func (m *memo) of(text string) []typeset.Piece {
	if !m.ok || m.text != text {
		m.text, m.pieces, m.ok = text, typeset.Text(text), true
	}
	return m.pieces
}

// New selects the best available translation, falling back to English. The reader shows nothing until Open.
func New(systemLocale string, deps Deps) *Screen {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Decode == nil {
		deps.Decode = ui.DecodeBitmap
	}
	return &Screen{deps: deps, lang: locale.Language(systemLocale), text: translations[locale.Language(systemLocale)], pics: pictures.New(false, 0),
		fetching: map[int64]bool{}, known: map[int64]float64{}, resume: -1}
}

// SetLanguage shows the screen in the language of systemLocale, keeping what it shows.
func (s *Screen) SetLanguage(systemLocale string) {
	s.lang = locale.Language(systemLocale)
	s.text = translations[s.lang]
}

// scope is a context that ends when the work it is for is dropped, and a fresh one begins.
type scope struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (p *scope) get() context.Context {
	if p.ctx == nil {
		p.renew()
	}
	return p.ctx
}

func (p *scope) renew() {
	if p.cancel != nil {
		p.cancel()
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())
}

// Use reads from api from now on, dropping the work under way for the server before.
func (s *Screen) Use(api API) {
	s.api = api
	s.gen++
	s.current.renew()
	s.server.renew()
	s.pics.Stop()
	s.id, s.moved, s.queued, s.resume = 0, false, nil, -1
	s.closeSelection()
	s.known = map[int64]float64{}
	s.Forget()
}

// Forget drops the articles got ahead and the answers still to come, as when the list they were beside changes.
func (s *Screen) Forget() {
	s.epoch++
	s.ahead.renew()
	s.cache, s.fetching = nil, map[int64]bool{}
}

// Prefetch gets the articles ids, the neighbours of the one read, ahead of the step to them: their text and the
// summary and translation the server already has. It only reads, never asks for a summary or translation to be made,
// and fails silently, which leaves the step as slow as it would have been. It keeps nothing but the article read and
// ids. One being got for the step already is got again by it, which is simpler than waiting for the first and costs
// one request.
func (s *Screen) Prefetch(ids ...int64) {
	if s.api == nil || s.id == 0 {
		return
	}
	s.cache = slices.DeleteFunc(s.cache, func(c cached) bool { return c.id != s.id && !slices.Contains(ids, c.id) })
	epoch, api, ctx := s.epoch, s.api, s.ahead.get()
	for _, id := range ids {
		if id == s.id || s.fetching[id] || slices.ContainsFunc(s.cache, func(c cached) bool { return c.id == id && c.hasAI }) {
			continue
		}
		s.fetching[id] = true
		s.deps.Go(func() func() {
			a, err := api.Entry(ctx, id)
			var r client.AIResults
			var aiErr error
			if err == nil {
				r, aiErr = api.AI(ctx, id)
			}
			return func() {
				if epoch != s.epoch {
					return
				}
				delete(s.fetching, id)
				if err != nil {
					return
				}
				c := article(a)
				c.ai, c.hasAI = r, aiErr == nil
				s.remember(c)
			}
		})
	}
}

// article turns what the server answers for an article into what the reader keeps of it.
func article(a client.Article) cached {
	base, _ := url.Parse(a.URL)
	c := cached{id: a.ID, article: a, full: parse(a.ExtractedHTML, base, a.Title), feed: parse(a.ContentHTML, base, a.Title)}
	if summary := strings.Join(strings.Fields(a.Summary), " "); !textual(c.feed) && summary != "" {
		c.feed = append(c.feed, block{kind: paragraph, spans: []span{{text: summary}}})
	}
	return c
}

// remember keeps c as the one used last, in the place of the one of its article, and drops the one used longest ago
// when there are more than keepCached.
func (s *Screen) remember(c cached) {
	if i := slices.IndexFunc(s.cache, func(o cached) bool { return o.id == c.id }); i >= 0 && !c.hasAI {
		c.ai, c.hasAI = s.cache[i].ai, s.cache[i].hasAI
	}
	s.cache = slices.DeleteFunc(s.cache, func(o cached) bool { return o.id == c.id })
	s.cache = append(s.cache, c)
	if len(s.cache) > keepCached {
		s.cache = s.cache[len(s.cache)-keepCached:]
	}
}

// Ready tells whether an article is on show with its text: one is open and it is neither loading nor failed.
func (s *Screen) Ready() bool { return s.id != 0 && !s.loading && !s.failed }

// Open shows e: it loads the article, from the top, and gives it the focus.
func (s *Screen) Open(e client.Entry) {
	s.Leave()
	s.gen++
	s.current.renew()
	s.pics.Stop()
	s.id, s.loading, s.failed, s.polling, s.retrying, s.feedText = e.ID, true, false, false, false, 0
	s.article, s.full, s.feed = client.Article{Entry: e}, nil, nil
	s.scroll.X, s.scroll.Y = 0, 0
	s.landing, s.refs, s.tables, s.seen, s.drawn = landing{}, nil, nil, nil, nil
	s.settled = nil
	s.startPosition(e)
	s.focus = true
	s.pics.Reset()
	s.jobs, s.aiOff, s.shown, s.title, s.translated, s.aiPolling = [2]job{}, false, 0, "", nil, false
	s.closeSelection()
	s.closeFind()
	if i := slices.IndexFunc(s.cache, func(c cached) bool { return c.id == e.ID }); i >= 0 {
		c := s.cache[i]
		s.remember(c)
		s.loading, s.polledAt = false, s.deps.Now()
		s.article, s.full, s.feed = c.article, c.full, c.feed
		s.article.Entry, s.article.ExtractStatus = e, c.article.ExtractStatus
		if c.hasAI {
			s.showAI(c.ai)
		}
	} else {
		s.load(false)
	}
	s.loadAI(true)
	s.loadRevision(e)
}

// load asks for the article; poll asks again while its text is being got, keeping what shows when that fails.
func (s *Screen) load(poll bool) {
	if s.api == nil || s.id == 0 {
		return
	}
	gen, epoch, api, id, ctx := s.gen, s.epoch, s.api, s.id, s.current.get()
	s.polling = poll
	s.deps.Go(func() func() {
		a, err := api.Entry(ctx, id)
		var c cached
		if err == nil {
			c = article(a)
		}
		return func() {
			if gen != s.gen {
				return
			}
			s.loading, s.polling, s.polledAt = false, false, s.deps.Now()
			if s.deps.Expired(err) {
				return
			}
			if err != nil {
				if !poll {
					s.failed, s.announce = true, s.text.Error
				}
				return
			}
			if poll && a.ExtractStatus == "ok" && textual(c.full) {
				s.announce = s.text.FullText
			}
			s.article, s.full, s.feed = a, c.full, c.feed
			if epoch == s.epoch {
				s.remember(c)
			}
		}
	})
}

// retry asks the server to get the full text again, and shows the article as being got meanwhile; the reader's
// polling then waits for the result. When the request fails the state it had comes back.
func (s *Screen) retry() {
	if s.api == nil || s.id == 0 || s.retrying || s.article.ExtractStatus != "failed" {
		return
	}
	gen, epoch, api, id, ctx := s.gen, s.epoch, s.api, s.id, s.server.get()
	s.retrying, s.polledAt = true, s.deps.Now()
	s.article.ExtractStatus = "pending"
	s.announce = s.text.Extracting
	s.deps.Go(func() func() {
		a, err := api.RetryExtraction(ctx, id)
		var c cached
		if err == nil {
			c = article(a)
		}
		return func() {
			if gen != s.gen {
				return
			}
			s.retrying, s.polledAt = false, s.deps.Now()
			if s.deps.Expired(err) {
				return
			}
			if err != nil {
				s.article.ExtractStatus, s.toast = "failed", s.text.RetryFailed
				var failure *client.APIError
				if errors.As(err, &failure) && failure.Status == http.StatusConflict {
					s.load(true) // the text was got in the meantime: show where it stands
				}
				return
			}
			s.article, s.full, s.feed = a, c.full, c.feed
			if epoch == s.epoch {
				s.remember(c)
			}
		}
	})
}

// poll asks for the article every pollEvery while its text is being got.
func (s *Screen) poll(c *ui.Context) {
	if s.loading || s.failed || s.polling || s.retrying || s.article.ExtractStatus != "pending" {
		return
	}
	if wait := pollEvery - s.deps.Now().Sub(s.polledAt); wait > 0 {
		c.After(wait)
		return
	}
	s.load(true)
}

// loadAI asks for the summary and the translation: first, as the article opens, to show a summary already made or
// being made; then while one is being made, to see whether it is ready.
func (s *Screen) loadAI(first bool) {
	if s.api == nil || s.id == 0 {
		return
	}
	gen, epoch, api, id, ctx := s.gen, s.epoch, s.api, s.id, s.current.get()
	s.aiPolling = true
	s.deps.Go(func() func() {
		r, err := api.AI(ctx, id)
		return func() {
			if gen != s.gen {
				return
			}
			s.aiPolling, s.aiPolledAt = false, s.deps.Now()
			if err != nil {
				if !first {
					s.deps.Expired(err)
				}
				return
			}
			if i := slices.IndexFunc(s.cache, func(c cached) bool { return c.id == id }); i >= 0 && epoch == s.epoch {
				s.cache[i].ai, s.cache[i].hasAI = r, true
			}
			if first {
				s.showAI(r)
				return
			}
			s.target = r.TargetLanguage
			for i, result := range []*client.AIResult{r.Summary, r.Translation} {
				if j := s.jobs[i]; result != nil && j.working && !j.asking {
					s.apply(i, *result, true)
				}
			}
		}
	})
}

// showAI shows the summary already made or being made, as the article opens, unless the reader has asked for it or
// hidden it since.
func (s *Screen) showAI(r client.AIResults) {
	s.target = r.TargetLanguage
	if s.jobs[summary].ticket != 0 {
		return
	}
	if r.Summary == nil || r.Summary.Status == "failed" {
		s.jobs[summary] = job{}
		return
	}
	s.jobs[summary].on = true
	s.apply(summary, *r.Summary, false)
}

// request asks the server for job i, which answers with the result when it is ready and unchanged, and otherwise
// makes it.
func (s *Screen) request(i int) {
	if s.api == nil || s.id == 0 {
		return
	}
	j := &s.jobs[i]
	j.ticket++
	j.on, j.working, j.asking, j.failed = true, true, true, false
	s.aiOff = false
	if i == translation {
		s.shown = 0
	}
	gen, ticket, api, id, ctx := s.gen, j.ticket, s.api, s.id, s.server.get()
	s.deps.Go(func() func() {
		r, err := api.RequestAI(ctx, id, tasks[i])
		return func() {
			if gen != s.gen || ticket != j.ticket {
				return
			}
			j.asking, s.aiPolledAt = false, s.deps.Now()
			switch {
			case s.deps.Expired(err):
				j.on, j.working = false, false
			case errorCode(err) == "ai_not_configured":
				j.on, j.working, s.aiOff = false, false, true
				s.announce = s.text.AIOffTitle
			case err != nil:
				s.apply(i, client.AIResult{Status: "failed"}, true)
			default:
				s.apply(i, r, true)
			}
		}
	})
}

// apply shows result for job i, and says so when announce.
func (s *Screen) apply(i int, result client.AIResult, announce bool) {
	j := &s.jobs[i]
	j.working, j.failed, j.code = result.Status == "pending", result.Status == "failed", result.Error
	switch {
	case result.Status == "ok":
		j.text, j.model = result.Text, result.Model
		if i == translation {
			s.title, s.translated = translated(result.Text)
		}
		if announce {
			s.announce = []string{s.text.SummaryReady, s.text.TranslationReady}[i]
		}
	case j.failed && announce && i == summary:
		s.announce = s.text.SummaryFailed
	case j.failed && announce:
		s.announce = s.reason(j.code)
	}
}

// cancel stops waiting for job i and hides it; the server makes it all the same.
func (s *Screen) cancel(i int) {
	j := &s.jobs[i]
	j.ticket++
	j.on, j.working, j.asking = false, false, false
}

// pollAI asks for the results every aiPollEvery while one is being made.
func (s *Screen) pollAI(c *ui.Context) {
	waiting := false
	for _, j := range s.jobs {
		waiting = waiting || j.working && !j.asking
	}
	if !waiting || s.aiPolling {
		return
	}
	if wait := aiPollEvery - s.deps.Now().Sub(s.aiPolledAt); wait > 0 {
		c.After(wait)
		return
	}
	s.loadAI(false)
}

// canTranslate reports whether the entry offers a translation: not when it is already in the language translations
// are made in, unless a translation of it is under way or made.
func (s *Screen) canTranslate(e client.Entry) bool {
	return s.jobs[translation].on || e.Language == "" || e.Language != s.target
}

// translating reports whether the translation shows in place of the original.
func (s *Screen) translating() bool {
	j := s.jobs[translation]
	return j.on && !j.working && !j.failed && s.shown == 0
}

// reason says why a summary or translation failed, by its error code.
func (s *Screen) reason(code string) string {
	switch code {
	case "ai_unreachable":
		return s.text.AIUnreachable
	case "ai_timeout":
		return s.text.AITimeout
	case "ai_unauthorized":
		return s.text.AIUnauthorized
	case "ai_rate_limited":
		return s.text.AIRateLimited
	case "ai_empty":
		return s.text.AIEmpty
	case "ai_cap_reached":
		return s.text.AICapReached
	case "":
		return s.text.ErrorHint
	}
	return s.text.AIUpstream
}

func errorCode(err error) string {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

// translated splits a translation, plain text, into its title, the first line, and its paragraphs, one a line.
func translated(text string) (string, []block) {
	var title string
	var blocks []block
	for line := range strings.Lines(text) {
		switch line = strings.TrimSpace(line); {
		case line == "":
		case title == "":
			title = line
		default:
			blocks = append(blocks, block{kind: paragraph, spans: []span{{text: line}}})
		}
	}
	return title, blocks
}

const (
	whole    = iota // the full text
	carried         // what the feed carries, the text not got
	getting         // what the feed carries, the text being got
	waiting         // nothing yet, the text being got
	nothing         // nothing to show
	chosen          // what the feed carries, as the reader chose it over the full text
	embedded        // nothing to read, but the text holds a video, audio or embedded content
)

// switchable reports whether the article has both the full text and what the feed carries, so that the reader can
// choose between them.
func (s *Screen) switchable() bool {
	return s.article.ExtractStatus == "ok" && textual(s.full) && textual(s.feed)
}

// chosenFeed reports whether the reader chose what the feed carries; a translation, made of the full text, takes
// the place of both.
func (s *Screen) chosenFeed() bool {
	return s.feedText == 1 && s.switchable() && !s.translating()
}

// switched goes to the top of the text now shown, after the reader chose the other one. The position of the full
// text is stored first; it belongs to that text alone and is not restored for the next choice.
func (s *Screen) switched(c *ui.Context, from int) {
	if from == 0 && s.moved {
		s.save()
	}
	s.scroll.X, s.scroll.Y = 0, 0
	s.landing, s.refs, s.tables, s.seen = landing{}, nil, nil, nil
	s.settled = nil
	s.lastY, s.moved, s.resume = 0, false, -1
	c.Invalidate()
}

// body is what the article shows, and how.
func (s *Screen) body() ([]block, int) {
	switch {
	case s.chosenFeed():
		return s.feed, chosen
	case s.article.ExtractStatus == "ok" && textual(s.full):
		return s.full, whole
	case s.article.ExtractStatus == "pending" && textual(s.feed):
		return s.feed, getting
	case s.article.ExtractStatus == "pending":
		return nil, waiting
	case textual(s.feed):
		return s.feed, carried
	}
	// Nothing to read; a text that holds only video, audio or embedded content is told apart from one with nothing.
	rows := s.feed
	if s.article.ExtractStatus == "ok" && len(s.full) > 0 {
		rows = s.full
	}
	if len(rows) > 0 {
		return rows, embedded
	}
	return nil, nothing
}

func (s *Screen) openOriginal(c *ui.Context) {
	if s.article.URL == "" {
		return
	}
	svc := c.Services()
	c.OpenURLThen(tracker.StripLink(s.article.URL), func(err error) {
		if err != nil {
			s.toast = s.text.OpenFailed
			svc.Invalidate()
		}
	})
}

func (s *Screen) shortcut(action, key string) string {
	return strings.NewReplacer("{action}", action, "{key}", key).Replace(s.text.Shortcut)
}

// readLabel is "6 min read" for an entry that takes a minute or more to read, and "" for a video, an entry with a
// duration of its own, or one with no estimate.
func (s *Screen) readLabel(e client.Entry) string {
	if e.ReadingSec < 60 || e.DurationSec > 0 || e.Kind == "video" {
		return ""
	}
	return strings.ReplaceAll(s.text.ReadingTime, "{n}", strconv.Itoa(e.ReadingSec/60))
}

// when is the time of the article in full: the date, with the year when it is not this one, and the time of day.
func (s *Screen) when(e client.Entry) string {
	ms := e.PublishedAt
	if ms == 0 {
		ms = e.CreatedAt
	}
	return locale.DateTime(s.lang, s.hours, s.deps.Now(), time.UnixMilli(ms))
}

// bitmap returns the decoded picture at address, asking for it if it has not been, and whether it failed.
func (s *Screen) bitmap(address string) (*ui.Bitmap, bool) {
	p := s.pics.Get(address, s.deps.Now())
	return p.Bitmap, p.Failed
}

// download starts the pictures asked for, in the order of the text.
func (s *Screen) download() {
	if s.api == nil {
		return
	}
	gen := s.gen
	s.pics.Download(s.api, s.deps.Decode, s.deps.Go, s.deps.Now, func() bool { return gen == s.gen })
}

// View draws the reader in the place of the list; it does no disk or network work, which runs through Deps.
func (s *Screen) View(c *ui.Context, nav Nav) {
	s.hours = c.Region().HourCycle
	t := c.Theme()
	// The list holds the read and star state as they change; the extraction is as the article last loaded.
	status := s.article.ExtractStatus
	s.article.Entry = nav.Entry
	s.article.ExtractStatus = status
	s.icon = nav.Icon
	s.poll(c)
	s.pollAI(c)
	ready := !s.loading && !s.failed
	if nav.Keys {
		for _, k := range []struct {
			key ui.Key
			do  func()
		}{
			{ui.KeyJ, func() {
				if nav.Next {
					s.deps.Move(1)
				}
			}},
			{ui.KeyK, func() {
				if nav.Previous {
					s.deps.Move(-1)
				}
			}},
			{ui.KeyN, func() {
				if s.deps.NextUnread != nil {
					s.deps.NextUnread()
				}
			}},
			{ui.KeyS, func() {
				if ready {
					s.deps.Star()
				}
			}},
			{ui.KeyM, func() {
				if ready {
					s.deps.ToggleRead()
				}
			}},
			{ui.KeyO, func() {
				if ready {
					s.openOriginal(c)
				}
			}},
			{ui.KeyZ, s.immerse},
			{ui.KeyEscape, func() {
				switch {
				case s.find.open:
					s.closeFind()
				case s.sel.on:
					s.closeSelection()
				case nav.Immersive:
					s.immerse()
				default:
					s.deps.Back()
				}
			}},
		} {
			c.OnShortcut(0, k.key, k.do)
		}
		c.OnShortcut(ui.Cmd, ui.KeyF, s.openFind)
	}
	if s.find.was && !s.find.open {
		// The row closed, by Escape or Done: the focus goes back to the article.
		s.focus = true
	}
	s.find.was = s.find.open
	width, _ := c.Size()
	narrow := width < 760 || nav.Pane
	// The toolbar row floats over the article, which scrolls under it and fades under the scroll edge.
	// The column keeps its identity when the sidebar beside it comes and goes, so the focus in the article stays.
	// A reading appearance recolors the article, its toolbar and its scroll edge; the selection card, the toasts and
	// everything else keep the theme.
	paper := theme.ReadingTheme(t, s.reading().ReadingPreset)
	c.SetTheme(paper)
	col := ui.Column(c).Key("reader").Grow(1).MinWidth(0)
	if paper != t {
		col.Background(paper.Background)
	}
	col.Children(func() {
		switch {
		case s.failed:
			s.empty(c, icons.WifiOff, s.text.Error, s.text.ErrorHint, &next{icons.RefreshCw, s.text.Retry, func() {
				s.loading, s.failed = true, false
				s.load(false)
			}}).Grow(1).FillWidth().Margin(theme.Bar, 0, 0, 0)
		default:
			s.page(c, narrow)
		}
		theme.Edge(c, s.scroll.Y > 0)
		s.toolbar(c, nav, ready, narrow)
		s.findRow(c, narrow)
		c.SetTheme(t)
		s.selectionCard(c)
		c.SetTheme(paper)
	})
	c.SetTheme(t)
	s.track(c)
	s.toasts(c)
	s.download()
	s.Flush(c)
}

// immerse turns immersive reading on or off, as Z does.
func (s *Screen) immerse() {
	if s.deps.Immerse != nil {
		s.deps.Immerse()
	}
}

func (s *Screen) toolbar(c *ui.Context, nav Nav, ready, narrow bool) {
	t := c.Theme()
	e := nav.Entry
	bar := c.TitleBar()
	// The row lines up with the article column, design BReaderBar: the glyph of its first button stands on the text's
	// left edge and the glyph of its last on the right one, a button's inset inside the column's padding. In a narrow
	// window it spans the window.
	inset := theme.Px(t, tokens.Space6)
	if narrow {
		inset = theme.Px(t, tokens.Space2)
	}
	left, right := inset, inset
	// The row is centered in the whole column, as the article is. With the timeline's sidebar hidden the window
	// controls sit over the toolbar: only where the column's own edge leaves them too little room does the row give
	// way, by padding that side more, so it keeps its width and every button stays on the article's lines otherwise.
	wide := s.columnWidth(c, nav)
	edge := (wide - min(wide, s.column(t))) / 2
	if nav.Controls {
		left = max(left, bar.Left-edge)
	}
	right = max(right, bar.Right-edge)
	row := ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(theme.Bar).DragWindow().Key("toolbar")
	// In immersive reading the toolbar is only transparent while it is not wanted, so its buttons keep their place in the
	// tab order and one that takes the focus brings it back.
	if nav.Immersive && !row.Hovered() && !row.FocusWithin() {
		row.Opacity(0)
	}
	row.Children(func() {
		ui.Row(c).FillWidth().MaxWidth(s.column(t)).Margin(0, ui.Auto).Height(theme.Bar).AlignItems(ui.Center).
			Gap(theme.Px(t, tokens.Space1)).Padding(0, right, 0, left).Children(func() {
			s.toolbarItems(c, nav, e, ready, narrow)
		})
	})
}

// toolbarItems are the buttons of the bar: back, previous and next; then Full text | Feed when the article has both,
// the summary and translation toggles, star, the read toggle, open in the browser and More. In immersive reading only
// the way out stays; the keys still do the rest.
func (s *Screen) toolbarItems(c *ui.Context, nav Nav, e client.Entry, ready, narrow bool) {
	t := c.Theme()
	if nav.Immersive {
		ui.Box(c).Grow(1)
		if s.tool(c, icons.Minimize2, s.text.ExitImmersive, "Z", theme.ToolState{}).Clicked() {
			s.immerse()
		}
		return
	}
	if !nav.Pane {
		back := strings.ReplaceAll(s.text.Back, "{scope}", nav.Scope)
		if s.tool(c, icons.ChevronLeft, back, "Esc", theme.ToolState{}).Clicked() {
			s.deps.Back()
		}
		if s.tool(c, icons.ChevronUp, s.text.Previous, "K", theme.ToolState{Disabled: !nav.Previous}).Clicked() {
			s.deps.Move(-1)
		}
		if s.tool(c, icons.ChevronDown, s.text.Next, "J", theme.ToolState{Disabled: !nav.Next}).Clicked() {
			s.deps.Move(1)
		}
	}
	// Beside the list there is nothing to go back to and J and K step, so the row starts with the actions.
	ui.Box(c).Grow(1)
	// Full text | Feed stands first among the actions, and moves into More in a narrow window.
	both := s.switchable() && !s.translating()
	if both && !narrow {
		s.textSwitch(c)
		s.divider(c)
	}
	on, label := s.jobs[summary].on, s.text.Summarize
	if on {
		label = s.text.HideSummary
	}
	if s.tool(c, icons.Sparkles, label, "", theme.ToolState{On: on, Disabled: !ready}).Role(ui.RoleToggleButton).
		Checked(on).Clicked() {
		if on {
			s.cancel(summary)
		} else {
			s.request(summary)
		}
	}
	// A translation the article cannot use stays in place, disabled; its name and tooltip say why. A disabled button
	// shows no tooltip, so the box around it does.
	if !s.canTranslate(e) {
		reason := s.translateDone()
		ui.Box(c).Key("translate-done").Tooltip(reason).Children(func() {
			s.tool(c, icons.Languages, reason, "", theme.ToolState{Disabled: true}).Tooltip("")
		})
	} else {
		on, label = s.translating(), s.text.Translate
		if on {
			label = s.text.ShowOriginal
		}
		if s.tool(c, icons.Languages, label, "", theme.ToolState{On: on, Disabled: !ready}).Role(ui.RoleToggleButton).
			Checked(on).Clicked() {
			j := s.jobs[translation]
			switch {
			case j.on && j.working:
				s.cancel(translation)
			case j.on && !j.failed:
				s.shown = 1 - s.shown
			default:
				s.request(translation)
			}
		}
	}
	s.divider(c)
	star, starState := s.text.Star, theme.ToolState{Disabled: !ready}
	if e.Starred {
		star, starState.On, starState.Tint = s.text.Unstar, true, theme.Star(t)
	}
	if s.tool(c, icons.Star, star, "S", starState).Role(ui.RoleToggleButton).Checked(e.Starred).Clicked() {
		s.deps.Star()
	}
	// The read toggle is the unread dot of the timeline's rows: in Accent while the entry is unread.
	read, dot := s.text.MarkRead, t.Accent
	if e.IsRead {
		read, dot = s.text.MarkUnread, ui.Color{}
	}
	// Read and open are the buttons a narrow column moves into More, the last of them first.
	buttons := [...]struct {
		icon     *ui.SVG
		label    string
		key      string
		disabled bool
		tint     ui.Color
		do       func()
	}{
		{icons.CircleDot, read, "M", !ready, dot, s.deps.ToggleRead},
		{icons.ExternalLink, s.text.Open, "O", !ready || e.URL == "", ui.Color{}, func() { s.openOriginal(c) }},
	}
	inline := len(buttons) - overflow(s.columnWidth(c, nav))
	for _, bt := range buttons[:inline] {
		if s.tinted(c, bt.icon, bt.label, bt.key, theme.ToolState{Disabled: bt.disabled}, bt.tint).Clicked() {
			bt.do()
		}
	}
	s.tool(c, icons.Ellipsis, s.text.More, "", theme.ToolState{Disabled: !ready}).Menu(func(m *ui.Menu) {
		for _, bt := range buttons[inline:] {
			if m.Item(bt.label).Disabled(bt.disabled).Chosen() {
				bt.do()
			}
		}
		if m.Item(s.text.CopyLink).Disabled(e.URL == "").Chosen() {
			s.CopyLink(e)
		}
		if both && narrow {
			m.Separator()
			for i, name := range []string{s.text.ShowFull, s.text.ShowFeed} {
				if m.Item(name).Checked(s.feedText == i).Chosen() && s.feedText != i {
					from := s.feedText
					s.feedText = i
					s.switched(c, from)
				}
			}
		}
		m.Separator()
		if s.deps.Immerse != nil && m.Item(s.text.Immersive).Chosen() {
			s.immerse()
		}
		if m.Item(s.text.Find).Chosen() {
			s.openFind()
		}
		m.Separator()
		if m.Item(s.text.CopyMarkdown).Chosen() {
			s.Export(e, false)
		}
		if m.Item(s.text.SaveMarkdown).Disabled(s.deps.Save == nil).Chosen() {
			s.Export(e, true)
		}
	})
}

// divider is the line2 hairline between groups of the bar, as tall as an icon.
func (s *Screen) divider(c *ui.Context) {
	t := c.Theme()
	ui.Box(c).Width(tokens.Hairline).Height(theme.IconMedium(t)).Shrink(0).Margin(0, theme.Px(t, tokens.Space1)).
		Background(theme.LineStrong(t))
}

// overflow is how many of the toolbar's read and open buttons move into More in a main column this wide: open below
// 440, and mark read too below 380.
func overflow(column float32) int {
	switch {
	case column < 380:
		return 2
	case column < 440:
		return 1
	}
	return 0
}

// columnWidth is the width of the column the reader fills: what the timeline says, else the window.
func (s *Screen) columnWidth(c *ui.Context, nav Nav) float32 {
	if nav.Column > 0 {
		return nav.Column
	}
	w, _ := c.Size()
	return w
}

// translateDone is why Translate is disabled: the article is already in the language translations are made in.
func (s *Screen) translateDone() string {
	return strings.ReplaceAll(s.text.TranslateDone, "{language}", s.language())
}

// language is the name of the language translations are made in.
func (s *Screen) language() string {
	switch s.target {
	case "zh-Hans":
		return s.text.LanguageZhHans
	case "en":
		return s.text.LanguageEn
	}
	return s.target
}

// CopyLink puts the original link of the entry e on the clipboard and says so in a toast and to screen readers. It
// does no work of its own; the clipboard and the messages follow in the next frame (see Flush). An entry with no link
// does nothing. See docs/product/screens.md, Copy link.
func (s *Screen) CopyLink(e client.Entry) {
	if e.URL == "" {
		return
	}
	s.clip, s.clipped, s.announce, s.toast = tracker.StripLink(e.URL), true, s.text.LinkCopied, s.text.LinkCopied
}

// Export copies the entry e as Markdown to the clipboard, or with save asks where to save it and writes the file. The
// open article is converted as it shows; any other is got from the server first. The work runs in the background,
// and the clipboard and the messages follow in the next frame (see Flush). See docs/product/screens.md, Export as Markdown.
func (s *Screen) Export(e client.Entry, save bool) {
	if s.api == nil || s.deps.Save == nil && save {
		return
	}
	api, id, at := s.api, e.ID, time.Local
	a, blocks, have := s.article, []block(nil), e.ID == s.id && !s.loading && !s.failed
	if have {
		blocks, _ = s.body() // the original, also while a translation shows
	}
	s.deps.Go(func() func() {
		var err error
		if !have {
			a, err = api.Entry(context.Background(), id)
			blocks = exported(article(a))
		}
		if err != nil {
			return func() {
				if !s.deps.Expired(err) {
					s.toast = s.text.ExportFailed
				}
			}
		}
		text := markdown(a.Entry, blocks, at)
		if !save {
			return func() { s.clip, s.clipped, s.announce, s.toast = text, true, s.text.Copied, s.text.Copied }
		}
		path, err := s.deps.Save(fileName(a.Title), s.text.MarkdownFiles)
		if err != nil || path == "" {
			return func() {}
		}
		name := filepath.Base(path)
		written := os.WriteFile(path, []byte(text), 0o644) == nil
		return func() {
			if written {
				s.announce = strings.ReplaceAll(s.text.Saved, "{file}", name)
				s.toast = s.announce
			} else {
				s.toast = strings.ReplaceAll(s.text.SaveFailed, "{file}", name)
			}
		}
	})
}

// exported is the text of an article that goes into its Markdown: the full text when it was extracted, else what the
// feed carries, as the reader shows it.
func exported(c cached) []block {
	if c.article.ExtractStatus == "ok" && textual(c.full) {
		return c.full
	}
	return c.feed
}

// Flush puts the clipboard text, announcement and toast left by the last frame's work in place. View does it, and the
// timeline does it while the list shows and the reader does not, for an export started from the list.
func (s *Screen) Flush(c *ui.Context) {
	if s.clipped {
		c.WriteClipboard(s.clip)
		s.clip, s.clipped = "", false
	}
	if s.announce != "" {
		c.Announce(s.announce)
		s.announce = ""
	}
	if s.toast != "" {
		c.Toast(s.toast)
		s.toast = ""
	}
}

// tool is a bare icon button of the toolbar, design CIconButton: an icon, named by label, its key, if any, in the
// tooltip. Its look is the one toolbar button of theme.ToolButton.
func (s *Screen) tool(c *ui.Context, icon *ui.SVG, label, key string, st theme.ToolState) ui.Element {
	return s.tinted(c, icon, label, key, st, ui.Color{})
}

// tinted is tool with its glyph kept in tint, when one is given, while the button is enabled and not on.
func (s *Screen) tinted(c *ui.Context, icon *ui.SVG, label, key string, st theme.ToolState, tint ui.Color) ui.Element {
	t := c.Theme()
	tip := label
	if key != "" {
		tip = s.shortcut(label, key)
	}
	b, color := theme.ToolButton(c, st)
	if tint != (ui.Color{}) && !st.Disabled && !st.On {
		color = tint
	}
	b.Label(label).Tooltip(tip)
	theme.Fade(b).Children(func() { ui.Icon(c, icon).FontSize(theme.IconMedium(t)).TextColor(color) })
	return b
}

// page is the scrolling article, design BReaderHead and BProse: its head, the AI cards, and the text, a
// stack-section apart in the column.
func (s *Screen) page(c *ui.Context, narrow bool) {
	t := c.Theme()
	e := s.article.Entry
	pad := []float32{theme.Bar + theme.Px(t, tokens.Space6), theme.Px(t, tokens.Space8), theme.Px(t, tokens.Space12),
		theme.Px(t, tokens.Space8)}
	if narrow {
		pad = []float32{theme.Bar + theme.Px(t, tokens.Space4), theme.Px(t, tokens.Space4), theme.Px(t, tokens.Space8),
			theme.Px(t, tokens.Space4)}
	}
	title, original := strings.TrimSpace(e.Title), ""
	if translated := strings.TrimSpace(e.TranslatedTitle); translated != "" {
		title, original = translated, strings.TrimSpace(e.Title) // the title translation made for the list
	}
	if s.translating() && s.title != "" {
		title, original = s.title, ""
	}
	area := ui.Scroll(c).TrackScroll(&s.scroll).Grow(1).FillWidth().ScrollbarInsets(theme.Bar, 0, 0, 0).Focusable().
		FocusRing(false).Label(title)
	if s.focus {
		area.Focus()
		s.focus = false
	}
	s.viewTop, s.nth = area.Bounds().Y, 0
	area.Children(func() {
		if s.loading {
			s.skeleton(c, pad)
			return
		}
		blocks, how := s.body()
		if s.translating() {
			blocks, how = s.translated, whole
		}
		rows := blocks
		if how == embedded {
			blocks = nil // the rows are told in the empty state, not drawn
		}
		s.findRefresh(title, blocks)
		s.drawn = blocks
		if s.landing.on {
			if left := lit - s.deps.Now().Sub(s.landing.at); left > 0 {
				c.After(left)
			} else {
				s.landing.on = false
			}
		}
		s.han = s.hanText(e, title, blocks)
		body := ui.Column(c).FillWidth().MaxWidth(s.column(t)).Margin(0, ui.Auto).Padding(pad...).
			Gap(theme.Px(t, tokens.Space6)).Selectable()
		// The menu is built by this call, so the body is named before it.
		body.ContextMenu(func(m *ui.Menu) { s.selectionMenu(m, body) })
		body.Children(func() {
			s.articleHead(c, e, title, original, how, narrow)
			if s.jobs[translation].on {
				s.translationCard(c)
			}
			if s.aiOff {
				s.aiOffCard(c)
			} else if s.jobs[summary].on {
				s.summaryCard(c)
			}
			if len(blocks) > 0 {
				ui.Column(c).FillWidth().Children(func() { s.blocks(c, blocks) })
			}
		})
		switch how {
		case waiting:
			s.empty(c, icons.LoaderCircle, s.text.Extracting, "", s.openNext(c)).FillWidth()
		case embedded:
			s.embeddedOnly(c, rows)
		case nothing:
			then := s.openNext(c)
			if s.article.ExtractStatus == "failed" {
				then = &next{icons.RefreshCw, s.text.Retry, s.retry}
			}
			s.empty(c, icons.FileText, s.text.Failed, s.text.FailedHint, then).FillWidth()
		}
	})
}

// articleHead is the top of the article, design BReaderHead, a stack-block: the source line, the title in display (display-compact
// in a narrow window), the original of a translated title, the other sources that carry it, and the banners that say
// the article changed or where its text comes from.
func (s *Screen) articleHead(c *ui.Context, e client.Entry, title, original string, how int, narrow bool) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(theme.Px(t, tokens.Space3)).Children(func() {
		meta := theme.Px(t, tokens.TextMeta.Size.En)
		dot := func() {
			ui.Text(c, "·").FontSize(theme.Px(t, tokens.TextMeta.Size.En)).TextColor(t.TextMuted.Alpha(tokens.OpacitySeparator))
		}
		ui.Row(c).MinWidth(0).AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space1_5)).Children(func() {
			glyph(c, e.SourceTitle, s.icon)
			strong := theme.Px(t, tokens.TextMetaStrong.Size.En)
			ui.Text(c, e.SourceTitle).FontSize(theme.Px(t, tokens.TextMetaStrong.Size.En)).FontWeight(tokens.TextMetaStrong.Weight).
				LetterSpacing(strong * tokens.TextMetaStrong.Tracking.En).TextColor(theme.Text2(t)).SingleLine().MinWidth(0).
				Shrink(1)
			dot()
			ui.Text(c, s.when(e)).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).LetterSpacing(meta * tokens.TextMeta.Tracking.En).FontFeatures("tnum").
				TextColor(t.TextMuted).NoWrap()
			if reading := s.readLabel(e); reading != "" {
				dot()
				ui.Text(c, reading).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).LetterSpacing(meta * tokens.TextMeta.Tracking.En).FontFeatures("tnum").
					TextColor(t.TextMuted).NoWrap()
			}
		})
		style := tokens.TextDisplay
		if narrow {
			style = tokens.TextDisplayCompact
		}
		size := theme.Px(t, style.Size.En)
		tracking, line := style.Tracking.En*size, s.leading(style.Line.En/style.Size.En, style.Line.Zh/style.Size.Zh)
		if theme.Han(title) {
			tracking = style.Tracking.Zh * size
		}
		ui.Column(c).FillWidth().Gap(theme.Px(t, tokens.Space1_5)).Children(func() {
			s.plain(c, &s.titleSet, title, unitTitle, s.han, size, tracking).FontSize(size).FontWeight(style.Weight).
				LineHeight(line).LetterSpacing(tracking).TextColor(t.Text).Wrap().Role(ui.RoleHeading).Level(1)
			if original != "" {
				// The title as the feed wrote it, under the translated one.
				ui.Text(c, original).TextColor(t.TextMuted).Wrap()
			}
		})
		s.alsoLine(c, e)
		s.revisedLine(c, e)
		switch how {
		case getting:
			theme.Banner{Busy: true, Title: s.text.Extracting, Text: s.text.ExtractingNote}.Draw(c)
		case carried:
			bn := theme.Banner{Icon: icons.FileText, Tone: t.Warning, Title: s.text.FeedNote, Text: s.text.FeedNoteHint}
			if s.article.ExtractStatus == "failed" {
				bn.Actions = func() {
					if (theme.Button{Small: true, Icon: icons.RefreshCw, Text: s.text.Retry}).Draw(c).Clicked() {
						s.retry()
					}
				}
			}
			bn.Draw(c)
		}
	})
}

// openNext is Open the original as the one thing an empty state offers, nil when the entry has no link.
func (s *Screen) openNext(c *ui.Context) *next {
	if s.article.URL == "" {
		return nil
	}
	return &next{icons.ExternalLink, s.text.OpenOriginal, func() { s.openOriginal(c) }}
}

// embedWords is what an embed row says, and the icon in front of it, by the kind of the element.
func (s *Screen) embedWords(b block) (string, *ui.SVG) {
	words, icon := s.text.EmbedOther, icons.AppWindow
	switch b.level {
	case embedVideo:
		words, icon = s.text.EmbedVideo, icons.Play
	case embedAudio:
		words, icon = s.text.EmbedAudio, icons.Volume2
	}
	return words, icon
}

// embedLabel is what a screen reader hears of an embed row: the words, and the title the element gives, which is
// never drawn.
func (s *Screen) embedLabel(b block, words string) string {
	if b.alt == "" {
		return words
	}
	return strings.NewReplacer("{what}", words, "{title}", b.alt).Replace(s.text.EmbedTitled)
}

// embedRow is the row for a video, audio or embedded content, an inset-card on surface: its icon, what it is and, at
// the end, Open in browser. The whole row is one button that opens the entry's link; with no link it is plain text.
func (s *Screen) embedRow(c *ui.Context, b block) {
	t := c.Theme()
	words, icon := s.embedWords(b)
	parts := func(open bool) {
		ui.Icon(c, icon).FontSize(theme.IconMedium(t)).TextColor(t.TextMuted)
		ui.Text(c, words).TextColor(theme.Text2(t)).MaxLines(1).Grow(1).Shrink(1).MinWidth(0)
		if open {
			ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space1)).Children(func() {
				ui.Text(c, s.text.Open).FontSize(theme.Px(t, tokens.TextMetaStrong.Size.En)).
					FontWeight(tokens.TextMetaStrong.Weight).NoWrap().TextColor(t.Accent)
				ui.Icon(c, icons.ExternalLink).FontSize(theme.IconSmall(t)).TextColor(t.Accent)
			})
		}
	}
	label := s.embedLabel(b, words)
	frame := func(e ui.Element) ui.Element {
		return e.FillWidth().MinHeight(theme.Px(t, tokens.RowCompact)).Gap(theme.Px(t, tokens.Space2)).
			Padding(theme.Px(t, tokens.Space2), theme.Px(t, tokens.Space3)).Radius(theme.Px(t, tokens.RadiusControl))
	}
	if s.article.URL == "" {
		frame(ui.Row(c).AlignItems(ui.Center)).Background(theme.Surface(t)).Label(label).Children(func() { parts(false) })
		return
	}
	row := ui.ButtonBase(c)
	frame(row).Label(strings.ReplaceAll(s.text.EmbedName, "{what}", label))
	switch {
	case row.Pressed():
		row.Background(theme.Surface(t).Mix(t.Text, 0.12))
	case row.Hovered():
		row.Background(theme.Surface(t).Mix(t.Text, 0.08))
	default:
		row.Background(theme.Surface(t))
	}
	theme.Fade(row).Children(func() { parts(true) })
	if row.Clicked() {
		s.openOriginal(c)
	}
}

// embeddedOnly is the empty state of a text that is nothing but video, audio and embedded content: it says what is
// here, by the kind of the rows, and offers the entry's link; nothing failed, so there is no Try again.
func (s *Screen) embeddedOnly(c *ui.Context, rows []block) {
	kinds := map[int]bool{}
	for _, b := range rows {
		if b.kind == embed {
			kinds[b.level] = true
		}
	}
	title, icon := s.text.OnlyOther, icons.AppWindow
	switch {
	case len(kinds) == 1 && kinds[embedVideo]:
		title, icon = s.text.OnlyVideo, icons.Play
	case len(kinds) == 1 && kinds[embedAudio]:
		title, icon = s.text.OnlyAudio, icons.Volume2
	}
	var then *next
	if s.article.URL != "" {
		then = &next{icons.ExternalLink, s.text.Open, func() { s.openOriginal(c) }}
	}
	s.empty(c, icon, title, s.text.OnlyHint, then).FillWidth()
}

// alsoLine is the other sources that carry the article, under its title: a text button each, with the source's
// monogram, name and time, that shows that copy. There is none for an article that only one source carries, or when the screen cannot show a copy.
func (s *Screen) alsoLine(c *ui.Context, e client.Entry) {
	if s.deps.OpenCopy == nil || e.Group == nil || len(e.Group.Entries) == 0 {
		return
	}
	t := c.Theme()
	ui.Row(c).Key("also").FillWidth().AlignItems(ui.Center).Wrap().Gap(theme.Px(t, tokens.Space0_5)).
		Role(ui.RoleList).Label(s.text.AlsoHead).Children(func() {
		ui.Text(c, s.text.AlsoHead).FontSize(theme.Px(t, tokens.TextMetaStrong.Size.En)).FontWeight(tokens.TextMetaStrong.Weight).NoWrap().TextColor(t.TextMuted).
			Margin(0, theme.Px(t, tokens.Space1), 0, 0)
		for _, m := range e.Group.Entries {
			label := m.SourceTitle + " " + s.when(client.Entry{CreatedAt: m.CreatedAt})
			b := ui.ButtonBase(c).Key(m.ID).Height(theme.Px(t, tokens.Row)).PaddingX(theme.Px(t, tokens.Space2)).
				Gap(theme.Px(t, tokens.Space1_5)).Radius(theme.Px(t, tokens.RadiusSm)).Role(ui.RoleListItem).Label(label)
			color := theme.Text2(t)
			switch {
			case b.Pressed():
				b.Background(t.SurfacePressed)
				color = t.Text
			case b.Hovered():
				b.Background(t.SurfaceHover)
				color = t.Text
			}
			theme.Fade(b).Children(func() {
				theme.Monogram(c, m.SourceTitle, tokens.TileS)
				ui.Text(c, m.SourceTitle).FontWeight(tokens.TextLabel.Weight).NoWrap().TextColor(color)
				ui.Text(c, s.when(client.Entry{CreatedAt: m.CreatedAt})).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).
					FontFeatures("tnum").NoWrap().TextColor(t.TextMuted)
			})
			if b.Clicked() {
				s.deps.OpenCopy(m.ID)
			}
		}
	})
}

// glyph is the site icon of the subscription, a tile-s tile with a rim, or its monogram when there is no icon.
func glyph(c *ui.Context, name string, icon *ui.Bitmap) {
	t := c.Theme()
	if icon == nil {
		theme.Monogram(c, name, tokens.TileS)
		return
	}
	tile := theme.Px(t, tokens.TileS)
	ui.Box(c).Size(tile, tile).Radius(theme.Px(t, tokens.RadiusXS)).Clip().Children(func() {
		ui.Image(c, icon).Fit(ui.Cover).Fill()
		ui.Box(c).Absolute().Fill().Radius(theme.Px(t, tokens.RadiusXS)).Border(0.5, theme.Ink(t, 0.2))
	})
}

// skeleton holds the place of the article while it loads: the source line, the title and two paragraphs, in track bars
// as high as their text.
func (s *Screen) skeleton(c *ui.Context, pad []float32) {
	t := c.Theme()
	ui.Column(c).FillWidth().MaxWidth(s.column(t)).Margin(0, ui.Auto).Padding(pad...).Gap(theme.Px(t, tokens.Space3)).
		Label(s.text.Loading).Role(ui.RoleProgress).Children(func() {
		bar := func(width, height float32) {
			ui.Row(c).FillWidth().Children(func() {
				h := theme.Px(t, height)
				ui.Box(c).Grow(width).Basis(0).Height(h).Radius(theme.Pill(h)).Background(theme.Track(t))
				ui.Box(c).Grow(100 - width).Basis(0)
			})
		}
		bar(30, tokens.TextMeta.Size.En)
		bar(80, tokens.TextDisplay.Size.En)
		for range 2 {
			ui.Column(c).FillWidth().Gap(theme.Px(t, tokens.Space2)).Margin(theme.Px(t, tokens.Space3), 0, 0).Children(func() {
				for range 3 {
					bar(96, tokens.TextMeta.Size.En)
				}
				bar(64, tokens.TextMeta.Size.En)
			})
		}
	})
}

// segmented is the choice between a few texts, design CSegmented: a track with the chosen segment raised. A small one is
// control-small high, for a card. It says whether the choice changed.
func (s *Screen) segmented(c *ui.Context, at *int, label string, names []string, small bool) bool {
	t := c.Theme()
	from := *at
	high, pad := float32(tokens.Control), float32(tokens.Space3)
	if small {
		high, pad = tokens.ControlSmall, tokens.Space2
	}
	inner := theme.Px(t, tokens.Space0_5)
	seg := ui.SegmentedBase(c, at, len(names))
	seg.Track.Height(theme.Px(t, high)).Padding(inner).Gap(inner).Radius(theme.Px(t, tokens.RadiusControl)).
		Background(theme.Track(t)).Label(label).Children(func() {
		for i, name := range names {
			b := seg.Segment(i).Height(theme.Px(t, high) - 2*inner).PaddingX(theme.Px(t, pad)).
				Radius(theme.Px(t, tokens.RadiusSm)).Label(name)
			color := theme.Text2(t)
			switch {
			case i == *at:
				theme.Raised(t, b)
				color = t.Text
			case b.Hovered():
				color = t.Text
			}
			theme.Fade(b).Children(func() {
				ui.Text(c, name).FontSize(theme.Px(t, tokens.TextMetaStrong.Size.En)).FontWeight(tokens.TextMetaStrong.Weight).NoWrap().TextColor(color)
			})
		}
	})
	return *at != from
}

// textSwitch is the choice between the full text and what the feed carries, first among the toolbar's actions.
func (s *Screen) textSwitch(c *ui.Context) {
	from := s.feedText
	if s.segmented(c, &s.feedText, s.text.TextSwitch, []string{s.text.ShowFull, s.text.ShowFeed}, false) {
		s.switched(c, from)
	}
}

// card is an AI card, design BAICard: an inset-card on surface, radius-card, its head an icon in accent and the
// title in label with the state at its end, then the rest a space-2 below.
func (s *Screen) card(c *ui.Context, icon *ui.SVG, title string, state, children func()) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(theme.Px(t, tokens.Space2)).Padding(theme.Px(t, tokens.Space3), theme.Px(t, tokens.Space4)).
		Radius(theme.Px(t, tokens.RadiusCard)).Background(theme.Surface(t)).Role(ui.RoleGroup).Label(title).Children(func() {
		ui.Row(c).FillWidth().MinHeight(theme.Px(t, tokens.RowDense)).AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space1_5)).
			Children(func() {
				ui.Icon(c, icon).FontSize(theme.IconSmall(t)).TextColor(t.Accent)
				ui.Text(c, title).FontWeight(tokens.TextLabel.Weight).NoWrap()
				ui.Row(c).Grow(1).MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space2)).
					Children(state)
			})
		if children != nil {
			children()
		}
	})
}

// status is a card's state in meta muted, with a turning loader in front while busy.
func (s *Screen) status(c *ui.Context, text string, busy bool) {
	t := c.Theme()
	if busy {
		spin := ui.Icon(c, icons.LoaderCircle).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
		spin.Rotate(spin.Loop("spin", tokens.MotionSpin, ui.Linear) * 360)
	}
	meta := theme.Px(t, tokens.TextMeta.Size.En)
	ui.Text(c, text).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).LetterSpacing(meta * tokens.TextMeta.Tracking.En).TextColor(t.TextMuted).SingleLine().
		MinWidth(0).Shrink(1).Role(ui.RoleStatus)
}

// failure is why a card's work failed, in one line, with Try again.
func (s *Screen) failure(c *ui.Context, code string, retry func()) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space2)).Role(ui.RoleStatus).Children(func() {
		ui.Text(c, s.reason(code)).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).TextColor(theme.Text2(t)).Wrap().
			Grow(1).MinWidth(0)
		if (theme.Link{Text: s.text.Retry}).Draw(c).Clicked() {
			retry()
		}
	})
}

// summaryCard is the summary above the text: being made, with Cancel; made, with the model that made it, in body
// prose; or failed, with why and Try again.
func (s *Screen) summaryCard(c *ui.Context) {
	t := c.Theme()
	j := s.jobs[summary]
	switch {
	case j.working:
		s.card(c, icons.Sparkles, s.text.Summary, func() {
			s.status(c, s.text.Summarizing, true)
			if (theme.Link{Text: s.text.Cancel, Muted: true}).Draw(c).Clicked() {
				s.cancel(summary)
			}
		}, nil)
	case j.failed:
		s.card(c, icons.Sparkles, s.text.Summary, func() {}, func() { s.failure(c, j.code, func() { s.request(summary) }) })
	default:
		s.card(c, icons.Sparkles, s.text.Summary, func() {
			if j.model != "" {
				s.status(c, strings.ReplaceAll(s.text.By, "{model}", j.model), false)
			}
		}, func() {
			text := strings.TrimSpace(j.text)
			size := theme.Px(t, tokens.TextBody.Size.En)
			s.plain(c, &s.summarySet, text, unitSummary, theme.Han(text), size, 0).FontSize(size).LineHeight(cardLine(text)).
				TextColor(theme.Prose(t)).Wrap()
		})
	}
}

// aiOffCard stands in for a summary or translation while AI is not set up, and leads to the settings.
func (s *Screen) aiOffCard(c *ui.Context) {
	t := c.Theme()
	s.card(c, icons.Sparkles, s.text.AIOffTitle, func() {
		if (theme.Link{Text: s.text.Close, Muted: true}).Draw(c).Clicked() {
			s.aiOff = false
		}
	}, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space2)).Role(ui.RoleStatus).Children(func() {
			ui.Text(c, s.text.AIOff).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).TextColor(theme.Text2(t)).Wrap().
				Grow(1).MinWidth(0)
			if (theme.Link{Text: s.text.OpenSettings}).Draw(c).Clicked() && s.deps.OpenSettings != nil {
				s.deps.OpenSettings()
			}
		})
	})
}

// translationCard says what the translation is doing, above the text, headed by the language it is made in: being made, with Cancel, the original
// meanwhile; failed, with why and Try again; made, with the model and Translation | Original.
func (s *Screen) translationCard(c *ui.Context) {
	j, head := s.jobs[translation], s.language()
	if head == "" {
		head = s.text.Translation
	}
	switch {
	case j.working:
		s.card(c, icons.Languages, head, func() {
			s.status(c, s.text.Translating, true)
			if (theme.Link{Text: s.text.Cancel, Muted: true}).Draw(c).Clicked() {
				s.cancel(translation)
			}
		}, nil)
	case j.failed:
		s.card(c, icons.Languages, head, func() {}, func() {
			s.failure(c, j.code, func() { s.request(translation) })
		})
	default:
		s.card(c, icons.Languages, head, func() {
			s.status(c, strings.ReplaceAll(s.text.Translated, "{model}", j.model), false)
			s.segmented(c, &s.shown, s.text.ShownText, []string{s.text.Translation, s.text.Original}, true)
		}, nil)
	}
}

// next is the one thing an empty state offers to do next.
type next struct {
	icon *ui.SVG
	text string
	do   func()
}

// empty is a state standing in for the text, design BEmpty: the icon on a surface disc twice tile-l, a display title
// saying what is missing, a body fact only when the title cannot carry it, and the one thing to do next as a button.
func (s *Screen) empty(c *ui.Context, icon *ui.SVG, title, fact string, then *next) ui.Element {
	t := c.Theme()
	return ui.Column(c).Center().Padding(theme.Px(t, tokens.Space6)).Role(ui.RoleStatus).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(theme.Px(t, tokens.Dialog)).AlignItems(ui.Center).Children(func() {
			disc := theme.Px(t, 2*tokens.TileL)
			ui.Box(c).Size(disc, disc).Radius(theme.Pill(disc)).Center().Background(theme.Surface(t)).Children(func() {
				i := ui.Icon(c, icon).FontSize(theme.IconLarge(t)).TextColor(theme.Text2(t))
				if icon == icons.LoaderCircle {
					i.Rotate(i.Loop("spin", tokens.MotionSpin, ui.Linear) * 360)
				}
			})
			ui.Text(c, title).FontSize(theme.Px(t, tokens.TextDisplay.Size.En)).FontWeight(tokens.TextDisplay.Weight).
				TextAlign(ui.Center).Wrap().Margin(theme.Px(t, tokens.Space6), 0, 0).Role(ui.RoleHeading).Level(2)
			if fact != "" {
				ui.Text(c, fact).TextColor(theme.Text2(t)).TextAlign(ui.Center).Wrap().Margin(theme.Px(t, tokens.Space2), 0, 0)
			}
			if then != nil {
				b := theme.Button{Icon: then.icon, Text: then.text}.Draw(c)
				b.Margin(theme.Px(t, tokens.Space6), 0, 0)
				if b.Clicked() {
					then.do()
				}
			}
		})
	})
}

// blocks draws the text, a run of blocks in quotes drawn together behind a line on the left.
func (s *Screen) blocks(c *ui.Context, blocks []block) {
	t := c.Theme()
	for i := 0; i < len(blocks); {
		if blocks[i].quote == 0 {
			s.block(c, blocks, i, 0, theme.Prose(t))
			i++
			continue
		}
		start := i
		for i < len(blocks) && blocks[i].quote > 0 {
			i++
		}
		run := blocks[start:i]
		ui.Column(c).Key(start).FillWidth().Margin(gap(t, blocks, start, s.scale()), 0, 0, 0).Padding(0, 0, 0, theme.Px(t, 18)).
			BorderWidth(0, 0, 0, 2).BorderColor(theme.Ink(t, 0.16)).Children(func() {
			for k := range run {
				s.block(c, run, k, start, theme.Text2(t))
			}
		})
	}
}

// gap is the space above the block at i, as the margins of the mockup's prose collapse: em margins of the body (16)
// and of h2 and h3 (19 and 16) follow the text size, the rule's 24 does not.
func gap(t *ui.Theme, blocks []block, i int, scale float32) float32 {
	if i == 0 {
		return 0
	}
	b, before := blocks[i], blocks[i-1]
	switch {
	case b.kind == heading && b.level == 2:
		return theme.Px(t, 1.9*19) * scale
	case b.kind == heading:
		return theme.Px(t, 1.6*16) * scale
	case before.kind == heading && before.level == 2:
		return theme.Px(t, 0.6*19) * scale
	case before.kind == heading:
		return theme.Px(t, 0.5*16) * scale
	case b.kind == rule && b.notes:
		return theme.Px(t, 2*13) * scale
	case before.kind == rule && before.notes:
		return theme.Px(t, 13) * scale
	case b.notes && before.notes:
		return theme.Px(t, 0.5*13) * scale
	case b.kind == rule || before.kind == rule:
		return theme.Px(t, 24)
	case b.kind == table || before.kind == table:
		return theme.Px(t, 1.4*16) * scale
	case b.kind == caption && before.kind == picture:
		return theme.Px(t, 8)
	case b.kind == caption && before.kind == embed:
		return theme.Px(t, 8)
	case b.kind == picture || before.kind == picture || before.kind == caption || b.kind == embed || before.kind == embed:
		return theme.Px(t, 1.4*16) * scale
	case b.depth > 0 && before.depth > 0:
		return theme.Px(t, 0.3*16) * scale
	}
	return theme.Px(t, 1.05*16) * scale
}

func (s *Screen) block(c *ui.Context, blocks []block, i, base int, color ui.Color) {
	t := c.Theme()
	if s.typeset() && !blocks[i].laid {
		blocks[i].set, blocks[i].laid = s.layout(blocks[i].spans), true
	}
	b := blocks[i]
	scale := s.scale()
	top := gap(t, blocks, i, scale)
	indent := theme.Px(t, 18) * float32(max(0, b.quote-1))
	if b.depth > 0 {
		indent += theme.Px(t, 22) * float32(b.depth-1)
	}
	unit := unitBlocks + base + i
	box := ui.Column(c).Key(i).FillWidth().Margin(top, 0, 0, indent)
	notes := b.notes && b.kind != rule
	if notes {
		box.Padding(theme.Px(t, 2), theme.Px(t, 6))
	}
	// The block a link led to is lit for a moment; it fades in and out, unless the desktop asks for less motion.
	arrived := !s.landing.at.IsZero() && b.ix == s.landing.ix && !s.translating()
	if arrived {
		if s.landing.on {
			box.Background(theme.Soft(t, t.Accent))
		}
		box.Radius(theme.Px(t, 6))
		if !c.Preferences().ReduceMotion {
			theme.Fade(box)
		}
	}
	box.Children(func() {
		lit := s.revealBlock(c, unit)
		if arrived && s.landing.scroll {
			// The block is scrolled to about 96 below the top: the sentinel is 96 above it and taller than the window,
			// and a region taller than the window is shown from its start.
			s.landing.scroll = false
			ui.Box(c).Key(-1).Absolute().Top(-theme.Px(t, 96)).Left(0).Width(1).Height(theme.Px(t, 4000)).PassThrough().
				ScrollIntoView()
		}
		switch b.kind {
		case rule:
			width := float32(0)
			if b.notes {
				width = theme.Px(t, 96)
			}
			hair := ui.Box(c).Height(1).Background(t.Border)
			if width > 0 {
				hair.Width(width)
			} else {
				hair.FillWidth()
			}
		case table:
			s.table(c, b, unit, lit)
		case picture:
			s.picture(c, b, box.Bounds().W)
		case embed:
			s.embedRow(c, b)
		case code:
			ui.ScrollHorizontal(c).FillWidth().Radius(theme.Px(t, 12)).Background(theme.Surface(t)).Children(func() {
				text := ui.Text(c, b.text)
				if marks := s.find.marks(unit, 0, len([]rune(b.text))); len(marks) > 0 {
					text = ui.RichText(c).Children(func() {
						for n, p := range cut([]typeset.Piece{{Text: b.text}}, marks) {
							s.mark(c, ui.Text(c, p.text).Key(n), p.state)
						}
					})
				}
				text.Font("monospace").FontSize(theme.Px(t, 13)*scale).LineHeight(1.6).NoWrap().TextColor(t.Text).
					Padding(theme.Px(t, 14), theme.Px(t, 16))
			})
		case heading:
			size, line, tracking := theme.Px(t, 19)*scale, s.leading(1.35, 1.5), float32(-0.01)
			if b.level == 3 {
				size, line, tracking = theme.Px(t, 16)*scale, s.leading(1.4, 1.5), 0
			}
			if s.han {
				tracking = 0
			}
			if b.quote == 0 {
				color = t.Text
			}
			s.inline(c, b, unit, 0, color, size).FontSize(size).FontWeight(600).LineHeight(line).LetterSpacing(tracking * size).
				Role(ui.RoleHeading).Level(b.level)
		case caption:
			s.inline(c, b, unit, 0, t.TextMuted, theme.Px(t, 12)).FontSize(theme.Px(t, 12)).LineHeight(1.5)
		default:
			line := float32(s.reading().Leading(s.han))
			body := theme.Px(t, 16) * scale
			size := body
			if b.notes {
				size = theme.Px(t, 13) * scale
				if b.quote == 0 {
					color = theme.Text2(t)
				}
			}
			if b.depth == 0 {
				s.prose(c, b, unit, color, size).FontSize(size).LineHeight(line)
				return
			}
			ui.Row(c).FillWidth().AlignItems(ui.Start).Children(func() {
				marker := ui.Text(c, b.marker).Width(theme.Px(t, 22) * scale).Shrink(0).FontSize(size).LineHeight(line).
					TextColor(t.TextMuted)
				if face := s.face(); face != "" {
					marker.Font(face)
				}
				s.prose(c, b, unit, color, size).Grow(1).MinWidth(0).FontSize(size).LineHeight(line)
			})
		}
	})
}

// reading is the preferences for the article's text.
func (s *Screen) reading() prefs.Preferences {
	if s.deps.Reading == nil {
		return prefs.Default()
	}
	return s.deps.Reading()
}

// scale is the factor of the article's sizes that follow the text size.
func (s *Screen) scale() float32 { return float32(s.reading().Scale()) }

// column is the greatest width of the text column, the toolbar's ends following it: 680 at the medium text size,
// growing and shrinking with it. The window can leave less.
func (s *Screen) column(t *ui.Theme) float32 { return theme.Px(t, float32(s.reading().Column())) }

// face is the font list of the article's body, "" for the interface font.
func (s *Screen) face() string {
	if s.deps.ReadingFont == nil {
		return ""
	}
	return s.deps.ReadingFont()
}

// prose draws spans as the article's body, in the reading font; the interface font where it is "".
func (s *Screen) prose(c *ui.Context, b block, unit int, color ui.Color, size float32) ui.Element {
	e := s.inline(c, b, unit, 0, color, size)
	if face := s.face(); face != "" {
		e.Font(face)
	}
	return e
}

// hanText reports whether the text shown, the title and the blocks, runs taller as Chinese. The language the server
// stored for the entry decides, except while a translation shows, which is not in that language; without one the
// characters do.
func (s *Screen) hanText(e client.Entry, title string, blocks []block) bool {
	if han, known := theme.HanLanguage(e.Language); known && !s.translating() {
		return han
	}
	return theme.Han(title) || hanBlocks(blocks)
}

// leading is the line height of the article's text, the second when its title is Chinese.
func (s *Screen) leading(latin, han float32) float32 {
	if s.han {
		return han
	}
	return latin
}

// typeset reports whether the text of the article is set by the rules of Chinese typesetting: the preference is on
// and the article is Chinese.
func (s *Screen) typeset() bool { return s.han && s.reading().ChineseTypesetting }

// layout is the pieces of spans that typesetting spaces, nil when none needs it. Code spans are left alone.
func (s *Screen) layout(spans []span) [][]typeset.Piece {
	runs := make([]typeset.Run, len(spans))
	for k, sp := range spans {
		runs[k] = typeset.Run{Text: sp.text, Code: sp.mono}
	}
	return typeset.Layout(runs)
}

// plain is a text drawn as one element: a Text, or when the text is Chinese, typesetting is on and it needs spacing, a
// RichText of its pieces. size is the font size and base the letter spacing the caller gives the element.
func (s *Screen) plain(c *ui.Context, m *memo, text string, unit int, chinese bool, size, base float32) ui.Element {
	if marks := s.find.marks(unit, 0, len([]rune(text))); len(marks) > 0 {
		e := ui.RichText(c).Children(func() {
			for n, p := range cut(s.pieceList(m, text, chinese), marks) {
				e := ui.Text(c, p.text).Key(n)
				if p.em != 0 {
					e.LetterSpacing(base + p.em*size)
				}
				s.mark(c, e, p.state)
			}
		})
		if s.find.current() == unit && s.find.reveal() {
			e.ScrollIntoView()
		}
		return e
	}
	if !chinese || !s.reading().ChineseTypesetting || m.of(text) == nil {
		return ui.Text(c, text)
	}
	return ui.RichText(c).Children(func() { s.pieces(c, m, text, true, size, base) })
}

// pieces draws a text as Text children of a RichText, the spacing of Chinese typesetting after the characters that
// need it when chinese is set and the preference is on.
func (s *Screen) pieces(c *ui.Context, m *memo, text string, chinese bool, size, base float32) {
	var pieces []typeset.Piece
	if chinese && s.reading().ChineseTypesetting {
		pieces = m.of(text)
	}
	if pieces == nil {
		ui.Text(c, text)
		return
	}
	for n, p := range pieces {
		e := ui.Text(c, p.Text).Key(n)
		if p.Em != 0 {
			e.LetterSpacing(base + p.Em*size)
		}
	}
}

// inline is a paragraph of spans, their links opening in the browser. size is the font size it will have, the em of
// the spacing of Chinese typesetting, whose pieces b has when it is set.
func (s *Screen) inline(c *ui.Context, b block, unit, from int, color ui.Color, size float32) ui.Element {
	t := c.Theme()
	return ui.RichText(c).TextColor(color).Wrap().Children(func() {
		n, base := 0, from
		for k, sp := range b.spans {
			pieces := []typeset.Piece{{Text: sp.text}}
			if b.set != nil && len(b.set[k]) > 0 {
				pieces = b.set[k]
			}
			n++
			plain := len(pieces) == 1 && pieces[0].Em == 0
			length := len([]rune(sp.text))
			marks := s.find.marks(unit, base, length)
			base += length
			if len(marks) > 0 {
				// A span with a match is cut into the parts the match divides it into; the rest of its look stays.
				parts := cut(pieces, marks)
				draw := func(c *ui.Context) {
					for _, p := range parts {
						n++
						e := ui.Text(c, p.text).Key(n)
						if p.em != 0 {
							e.LetterSpacing(p.em * size)
						}
						s.style(c, e, sp)
						s.mark(c, e, p.state)
					}
				}
				if sp.link != "" {
					e := ui.Link(c.Key(n), "", sp.link).TextColor(t.Accent).Underline()
					e.Children(func() { draw(c) })
				} else {
					draw(c)
				}
				continue
			}
			switch {
			case sp.jump != "" || sp.back:
				e := ui.Link(c.Key(n), sp.text, "").TextColor(t.Accent)
				if !plain {
					e = ui.Link(c.Key(n), "", "").TextColor(t.Accent)
					e.Children(func() { s.runs(c, pieces, size) })
				}
				switch {
				case sp.back:
					e.Label(s.text.BackToText)
				case sp.ref > 0:
					e.Label(strings.ReplaceAll(s.text.Footnote, "{n}", strconv.Itoa(sp.ref)))
				default:
					e.Underline()
				}
				s.style(c, e, sp)
				if e.Clicked() {
					s.follow(c, b, sp)
				}
			case sp.link != "" && plain:
				s.style(c, ui.Link(c.Key(n), sp.text, sp.link).TextColor(t.Accent).Underline(), sp)
			case sp.link != "":
				// One link holds all the pieces of its text, so it stays one tab stop named by the whole text.
				e := ui.Link(c.Key(n), "", sp.link).TextColor(t.Accent).Underline()
				e.Children(func() { s.runs(c, pieces, size) })
				s.style(c, e, sp)
			case plain:
				s.style(c, ui.Text(c, sp.text).Key(n), sp)
			default:
				for _, piece := range pieces {
					n++
					e := ui.Text(c, piece.Text).Key(n)
					if piece.Em != 0 {
						e.LetterSpacing(piece.Em * size)
					}
					s.style(c, e, sp)
				}
			}
		}
	})
}

// follow goes where a link within the page leads, from the block b: to the top, to the block with the id, or from a
// footnote back to the reference last followed, else the first one. The block it reaches is scrolled to and lit.
func (s *Screen) follow(c *ui.Context, b block, sp span) {
	to := -1
	switch {
	case sp.jump == top:
		s.scroll.ScrollTo(0, 0, ui.Smooth)
		return
	case sp.back && sp.jump == "":
		if at, ok := s.refs[b.note]; ok {
			to = at
		} else {
			to = slices.IndexFunc(s.drawn, func(o block) bool {
				return slices.ContainsFunc(o.spans, func(sp span) bool { return sp.ref == b.note })
			})
		}
	default:
		to = slices.IndexFunc(s.drawn, func(o block) bool { return slices.Contains(o.ids, sp.jump) })
		if sp.ref > 0 {
			if s.refs == nil {
				s.refs = map[int]int{}
			}
			s.refs[sp.ref] = b.ix
		}
	}
	if to < 0 || to >= len(s.drawn) {
		return
	}
	s.landing = landing{ix: s.drawn[to].ix, at: s.deps.Now(), on: true, scroll: true}
	c.Invalidate()
}

// table is a table of the text: a grid in a frame that scrolls sideways when it is wider than the text, with a fade
// where cells are cut off and a bar for the place.
// tableName is the accessible name of a table with its counts of columns and rows.
func tableName(text string, cols, rows int) string {
	return locale.Count(locale.Count(text, "c", cols), "r", rows)
}

func (s *Screen) table(c *ui.Context, b block, unit int, lit bool) {
	t := c.Theme()
	g := b.table
	size := theme.Px(t, 13) * s.scale()
	if s.tables == nil {
		s.tables, s.seen = map[int]*ui.ScrollState{}, map[int][2]float32{}
	}
	st := s.tables[b.ix]
	if st == nil {
		st = &ui.ScrollState{}
		s.tables[b.ix] = st
	}
	// The scroll range is that of the frame before; draw again when it or the place changes.
	if now := [2]float32{st.X, st.MaxX}; s.seen[b.ix] != now {
		s.seen[b.ix] = now
		c.Invalidate()
	}
	wide := st.MaxX > 0.5
	radius := theme.Px(t, 12)
	label := tableName(s.text.TableName, g.cols, len(g.rows))
	frame := ui.Box(c).AlignSelf(ui.Start).MaxWidth(math.MaxFloat32).Radius(radius).Clip()
	var view ui.Element
	frame.Children(func() {
		view = ui.ScrollHorizontal(c).TrackScroll(st).Radius(radius).Role(ui.RoleTable).Label(label)
		if wide {
			view.Focusable()
		}
		view.Children(func() {
			tracks := make([]ui.Track, g.cols)
			for k := range tracks {
				tracks[k] = ui.FitContent()
			}
			ui.Grid(c).ColumnTracks(tracks...).Children(func() {
				n, base := 0, 0
				for r, row := range g.rows {
					for _, cl := range row {
						n++
						s.cell(c, cl, r, size, n, unit, base, lit)
						base += cellLen(cl) + 1
					}
				}
			})
		})
		ui.Box(c).Absolute().Fill().Radius(radius).Border(0.5, theme.Ink(t, 0.1)).PassThrough()
		if wide && st.X > 0.5 {
			ui.Box(c).Absolute().Top(0).Bottom(0).Left(0).Width(theme.Px(t, 32)).PassThrough().
				LinearGradient(ui.LinearGradient{From: t.Background, To: t.Background.Alpha(0), Angle: 90})
		}
		if wide && st.X < st.MaxX-0.5 {
			ui.Box(c).Absolute().Top(0).Bottom(0).Right(0).Width(theme.Px(t, 32)).PassThrough().
				LinearGradient(ui.LinearGradient{From: t.Background.Alpha(0), To: t.Background, Angle: 90})
		}
	})
	if wide {
		seen := view.Bounds().W
		ui.Row(c).FillWidth().Height(theme.Px(t, 3)).Margin(theme.Px(t, 6), 0, 0, 0).Radius(theme.Px(t, 4)).Background(theme.Track(t)).
			Children(func() {
				ui.Box(c).Grow(max(st.X, 0)).Basis(0)
				ui.Box(c).Grow(seen).Basis(0).Height(theme.Px(t, 3)).Radius(theme.Px(t, 4)).Background(theme.Ink(t, 0.28))
				ui.Box(c).Grow(max(st.MaxX-st.X, 0)).Basis(0)
			})
	}
	if len(g.caption) > 0 {
		s.inline(c, block{spans: g.caption}, unit, gridLen(g), t.TextMuted, theme.Px(t, 12)).FontSize(theme.Px(t, 12)).LineHeight(1.5).
			Margin(theme.Px(t, 8), 0, 0, 0)
	}
}

// cell is a cell of a table, in row r: padded, with a divider above it unless it is in the first row, the header
// row on the Surface color in Text at weight 600. unit and base say where its text begins in the text the table is
// searched as, and lit that the match the find bar has just gone to is to be shown: when it is in this cell, the
// cell is scrolled into view, the table sideways with it.
func (s *Screen) cell(c *ui.Context, cl cell, r int, size float32, key, unit, base int, lit bool) {
	t := c.Theme()
	box := ui.Column(c).Key(key).ColumnSpan(cl.span).MinWidth(theme.Px(t, 48)).MaxWidth(theme.Px(t, 240)).
		Padding(theme.Px(t, 8), theme.Px(t, 12))
	if cl.head {
		box.Background(theme.Surface(t))
	}
	if r > 0 {
		box.BorderWidth(0.5, 0, 0, 0).BorderColor(t.Border)
	}
	box.Children(func() {
		color := theme.Prose(t)
		if cl.head {
			color = t.Text
		}
		e := s.inline(c, block{spans: cl.spans}, unit, base, color, size).FontSize(size).LineHeight(1.5).FillWidth()
		if lit && s.find.within(unit, base, cellLen(cl)) {
			e.ScrollIntoView()
		}
		switch cl.align {
		case alignCenter:
			e.TextAlign(ui.Center)
		case alignEnd:
			e.TextAlign(ui.End)
		}
		if cl.head {
			e.FontWeight(600)
		}
		if cl.number {
			e.FontFeatures("tnum")
		}
	})
}

// runs draws the pieces of a span as texts, the spaced ones with their letter spacing.
func (s *Screen) runs(c *ui.Context, pieces []typeset.Piece, size float32) {
	for n, p := range pieces {
		e := ui.Text(c, p.Text).Key(n)
		if p.Em != 0 {
			e.LetterSpacing(p.Em * size)
		}
	}
}

// style gives a span's emphasis to the element that draws it.
func (s *Screen) style(c *ui.Context, e ui.Element, sp span) {
	t := c.Theme()
	if sp.bold {
		e.FontWeight(600)
	}
	if sp.italic {
		e.Italic()
	}
	if sp.mono {
		e.Font("monospace").FontSize(theme.Px(t, 16*0.86) * s.scale()).TextColor(t.Text).TextBackground(theme.Ink(t, 0.055))
	}
	if sp.gone {
		e.Strikethrough()
	}
}

// settle is how a picture of the text was last drawn: what it showed (loading, loaded or failed) and how tall.
type settle struct {
	state  int
	height float32
}

const (
	pictureLoading = iota
	pictureLoaded
	pictureFailed
)

// picture is a picture of the text. Until it arrives it is a box on the Surface color, as the page declares it or a
// flat 180 high; once it has, the real size decides, the whole picture at its own ratio and no wider than the column,
// fading in. One that fails keeps the box and says so, with the alt text and Try again; one declared under 48 is
// decoration and is left out. avail is the width of the column, as the last frame had it, 0 until known.
func (s *Screen) picture(c *ui.Context, b block, avail float32) {
	t := c.Theme()
	bitmap, failed := s.bitmap(b.src)
	n := s.nth
	s.nth++
	if failed && (b.width > 0 && b.width < 48 || b.height > 0 && b.height < 48) {
		return
	}
	state, ratio, limit, fixed := pictureLoading, float32(0), float32(b.width), float32(0)
	switch {
	case bitmap != nil:
		w, h := bitmap.Size()
		state, ratio, limit = pictureLoaded, float32(w)/float32(h), float32(w)
	case b.width > 0 && b.height > 0:
		ratio = float32(b.width) / float32(b.height)
	case avail > 0 && avail < theme.Px(t, 320):
		ratio = 16.0 / 9
	default:
		fixed = theme.Px(t, 180)
	}
	if failed {
		state = pictureFailed
	}
	width := avail
	if limit > 0 && (width == 0 || limit < width) {
		width = limit
	}
	height := fixed
	if ratio > 0 {
		height = width / ratio
	}
	least := float32(0)
	if failed {
		least = theme.Px(t, 48)
		height = max(height, least)
	}
	strip := failed && height < theme.Px(t, 140)
	name := b.alt
	if failed {
		name = strings.ReplaceAll(s.text.PicFailedName, "{alt}", b.alt)
		if b.alt == "" {
			name = s.text.PicFailedNoAlt
		}
	}
	box := ui.Box(c).FillWidth().Radius(theme.Px(t, 12)).Background(theme.Surface(t)).Clip().Label(name)
	if ratio > 0 {
		box.AspectRatio(ratio)
	} else {
		box.Height(fixed)
	}
	if failed {
		box.MinHeight(least).Role(ui.RoleGroup)
	} else {
		box.Role(ui.RoleImage)
	}
	if limit > 0 {
		box.MaxWidth(limit)
	}
	s.compensate(box, n, settle{state, height}, avail)
	box.Children(func() {
		switch {
		case bitmap != nil:
			ui.Image(c, bitmap).Key("bitmap").Fit(ui.Contain).Fill().Transition(pictureFade)
		case failed:
			s.pictureFailed(c, b, strip)
		}
		ui.Box(c).Absolute().Fill().Radius(theme.Px(t, 12)).Border(0.5, theme.Ink(t, 0.1)).PassThrough()
	})
}

// pictureFade is how a picture appears once decoded: its opacity only, so the box has already taken its height.
var pictureFade = ui.ElementTransition{Duration: 140 * time.Millisecond, Enter: &ui.Motion{}}

// compensate keeps the text being read where it is when a picture settles: if the box lay wholly above the top of the
// page area as the last frame drew it, the scroll offset moves by the change in height, in this same frame. A box on
// screen or below moves what is under it once. While the saved position waits to be restored, it is left to that.
func (s *Screen) compensate(box ui.Element, n int, now settle, avail float32) {
	if avail == 0 {
		return
	}
	if s.settled == nil {
		s.settled = map[int]settle{}
	}
	before, known := s.settled[n]
	s.settled[n] = now
	if !known || before.state == now.state || before.height == now.height || s.resume >= 0 {
		return
	}
	if r := box.Bounds(); r.H > 0 && r.Y+r.H <= s.viewTop {
		s.scroll.Y += now.height - before.height
		s.lastY = s.scroll.Y
	}
}

// pictureFailed is what a failed picture holds: the image-off icon, the alt text, what happened and Try again; in one
// row when the box is low.
func (s *Screen) pictureFailed(c *ui.Context, b block, strip bool) {
	t := c.Theme()
	retry := func() {
		button := ui.ButtonBase(c).Height(theme.Px(t, 32)).Gap(t.Space(1.5)).PaddingX(theme.Px(t, 10)).Radius(theme.Px(t, 8)).
			Label(s.text.PicRetry)
		switch {
		case button.Pressed():
			button.Background(theme.Track(t).Mix(t.Text, 0.16))
		case button.Hovered():
			button.Background(theme.Track(t).Mix(t.Text, 0.1))
		default:
			button.Background(theme.Track(t))
		}
		button.Children(func() {
			ui.Icon(c, icons.RotateCcw).FontSize(theme.IconSmall(t)).TextColor(t.Text)
			ui.Text(c, s.text.Retry).FontSize(theme.Px(t, 12)).FontWeight(500).NoWrap().TextColor(t.Text)
		})
		if button.Clicked() {
			s.pics.Again(b.src)
			s.download()
			c.Invalidate()
		}
	}
	if strip {
		ui.Row(c).Fill().AlignItems(ui.Center).Gap(theme.Px(t, 10)).Padding(theme.Px(t, 8), theme.Px(t, 12)).Children(func() {
			ui.Icon(c, icons.ImageOff).FontSize(theme.IconMedium(t)).TextColor(t.TextMuted)
			line := s.text.PicFailed
			if b.alt != "" {
				line = b.alt
			}
			ui.Text(c, line).FontSize(theme.Px(t, 13)).TextColor(theme.Text2(t)).MaxLines(1).Grow(1).Shrink(1).MinWidth(0)
			retry()
		})
		return
	}
	ui.Column(c).Fill().Center().Gap(theme.Px(t, 8)).Padding(theme.Px(t, 16)).Children(func() {
		ui.Icon(c, icons.ImageOff).FontSize(theme.IconLarge(t)).TextColor(t.TextMuted)
		if b.alt != "" {
			ui.Text(c, b.alt).FontSize(theme.Px(t, 13)).MaxWidth(theme.Px(t, 360)).MaxLines(3).TextAlign(ui.Center).
				TextColor(theme.Text2(t))
			ui.Text(c, s.text.PicFailed).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted)
		} else {
			ui.Text(c, s.text.PicFailed).FontSize(theme.Px(t, 13)).TextColor(theme.Text2(t))
		}
		retry()
	})
}

// cardLine is the line height of the text of a card: 1.65, 1.75 when it is Chinese.
func cardLine(text string) float32 {
	if theme.Han(text) {
		return 1.75
	}
	return 1.65
}

// hanBlocks reports whether the first paragraphs of the text are Chinese, which is what the body's line height
// follows when the title is not.
func hanBlocks(blocks []block) bool {
	for _, b := range blocks[:min(len(blocks), 4)] {
		for _, sp := range b.spans {
			if theme.Han(sp.text) {
				return true
			}
		}
	}
	return false
}
