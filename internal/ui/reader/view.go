// Package reader is the reader: an article in its full text, drawn natively in the place of the timeline's list,
// or what the feed carries while the text is being got or when it could not be; see design/screens/reader.html.
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
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/pictures"
	"github.com/yangflow/oriel/internal/ui/theme"
	"github.com/yangflow/oriel/internal/ui/typeset"
)

//go:embed copy.json
var catalog []byte

type copy struct {
	Back, Previous, Next, Star, Unstar, MarkRead, MarkUnread, Open, FeedNote, ExtractingNote, OpenOriginal        string
	Extracting, ExtractingHint, Failed, FailedHint, Error, ErrorHint, Retry, Loading, FullText, OpenFailed        string
	Shortcut                                                                                                      string
	Summarize, HideSummary, Translate, ShowOriginal, Summary, By, Summarizing, Cancel, SummaryFailed              string
	Translating, Translated, Translation, Original, TranslationFailed, SummaryReady, TranslationReady             string
	AIUnreachable, AITimeout, AIUnauthorized, AIRateLimited, AICapReached, AIUpstream, AIEmpty, AIOff, AIOffTitle string
	OpenSettings, Close, ReadingTime, Resumed, TextSwitch, ShowFull, ShowFeed, RetryFailed                        string
	TranslateSelection, ExplainSelection, CopyAnswer, Copied, SelectionFailed, SelectLess, SelectLessHint         string
	SelectionAIOff, ExplanationReady, TranslateDone, LanguageZhHans, LanguageEn                                   string
	AlsoHead                                                                                                      string
	ExitImmersive, FindLabel                                                                                      string
	BackToText, Footnote, TableName                                                                               string
	More, CopyLink, LinkCopied, CopyMarkdown, SaveMarkdown, Saved, SaveFailed, ExportFailed, MarkdownFiles        string
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

	// Footnotes and tables, see docs/design.md "阅读器": the block a link within the page last led to, the block of the
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
	titleSet, summarySet, noteSet, translatedSet memo

	// The reading position, see docs/product.md "阅读器": the fraction last stored, the one asked to restore (-1 for
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

// Use reads from api from now on, dropping the work under way for the server before.
func (s *Screen) Use(api API) {
	s.api = api
	s.gen++
	s.pics.Stop()
	s.id, s.moved, s.queued, s.resume = 0, false, nil, -1
	s.closeSelection()
	s.known = map[int64]float64{}
	s.Forget()
}

// Forget drops the articles got ahead and the answers still to come, as when the list they were beside changes.
func (s *Screen) Forget() {
	s.epoch++
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
	epoch, api := s.epoch, s.api
	for _, id := range ids {
		if id == s.id || s.fetching[id] || slices.ContainsFunc(s.cache, func(c cached) bool { return c.id == id && c.hasAI }) {
			continue
		}
		s.fetching[id] = true
		s.deps.Go(func() func() {
			a, err := api.Entry(context.Background(), id)
			var r client.AIResults
			var aiErr error
			if err == nil {
				r, aiErr = api.AI(context.Background(), id)
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
	if summary := strings.Join(strings.Fields(a.Summary), " "); len(c.feed) == 0 && summary != "" {
		c.feed = []block{{kind: paragraph, spans: []span{{text: summary}}}}
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
	s.pics.Stop()
	s.id, s.loading, s.failed, s.polling, s.retrying, s.feedText = e.ID, true, false, false, false, 0
	s.article, s.full, s.feed = client.Article{Entry: e}, nil, nil
	s.scroll.X, s.scroll.Y = 0, 0
	s.landing, s.refs, s.tables, s.seen, s.drawn = landing{}, nil, nil, nil, nil
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
}

// load asks for the article; poll asks again while its text is being got, keeping what shows when that fails.
func (s *Screen) load(poll bool) {
	if s.api == nil || s.id == 0 {
		return
	}
	gen, epoch, api, id := s.gen, s.epoch, s.api, s.id
	s.polling = poll
	s.deps.Go(func() func() {
		a, err := api.Entry(context.Background(), id)
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
			if poll && a.ExtractStatus == "ok" && len(c.full) > 0 {
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
	gen, epoch, api, id := s.gen, s.epoch, s.api, s.id
	s.retrying, s.polledAt = true, s.deps.Now()
	s.article.ExtractStatus = "pending"
	s.announce = s.text.Extracting
	s.deps.Go(func() func() {
		a, err := api.RetryExtraction(context.Background(), id)
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
	gen, epoch, api, id := s.gen, s.epoch, s.api, s.id
	s.aiPolling = true
	s.deps.Go(func() func() {
		r, err := api.AI(context.Background(), id)
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
	gen, ticket, api, id := s.gen, j.ticket, s.api, s.id
	s.deps.Go(func() func() {
		r, err := api.RequestAI(context.Background(), id, tasks[i])
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
	whole   = iota // the full text
	carried        // what the feed carries, the text not got
	getting        // what the feed carries, the text being got
	waiting        // nothing yet, the text being got
	nothing        // nothing to show
	chosen         // what the feed carries, as the reader chose it over the full text
)

// switchable reports whether the article has both the full text and what the feed carries, so that the reader can
// choose between them.
func (s *Screen) switchable() bool {
	return s.article.ExtractStatus == "ok" && len(s.full) > 0 && len(s.feed) > 0
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
	s.lastY, s.moved, s.resume = 0, false, -1
	c.Invalidate()
}

// body is what the article shows, and how.
func (s *Screen) body() ([]block, int) {
	switch {
	case s.chosenFeed():
		return s.feed, chosen
	case s.article.ExtractStatus == "ok" && len(s.full) > 0:
		return s.full, whole
	case s.article.ExtractStatus == "pending" && len(s.feed) > 0:
		return s.feed, getting
	case s.article.ExtractStatus == "pending":
		return nil, waiting
	case len(s.feed) > 0:
		return s.feed, carried
	}
	return nil, nothing
}

func (s *Screen) openOriginal(c *ui.Context) {
	if s.article.URL == "" {
		return
	}
	c.OpenURLThen(s.article.URL, func(err error) {
		if err != nil {
			s.toast = s.text.OpenFailed
			c.Invalidate()
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
	narrow := width < 760
	// The toolbar row floats over the article, which scrolls under it and fades under the scroll edge.
	// The column keeps its identity when the sidebar beside it comes and goes, so the focus in the article stays.
	ui.Column(c).Key("reader").Grow(1).MinWidth(0).Children(func() {
		switch {
		case s.failed:
			s.empty(c, icons.WifiOff, t.Danger, s.text.Error, s.text.ErrorHint, func() {
				if s.button(c, icons.RefreshCw, s.text.Retry, false) {
					s.loading, s.failed = true, false
					s.load(false)
				}
			}).Grow(1).FillWidth().Margin(theme.Bar, 0, 0, 0)
		default:
			s.page(c, narrow)
		}
		theme.Edge(c, s.scroll.Y > 0)
		s.toolbar(c, nav, ready, narrow)
		s.findRow(c, narrow)
		s.selectionCard(c)
	})
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
	// The row lines up with the article column: its back button's text starts at the column's left edge and its
	// last button ends at the right one. In a narrow window it spans the window.
	left, right := theme.Px(t, 28), theme.Px(t, 32)
	if narrow {
		left, right = theme.Px(t, 12), theme.Px(t, 16)
	}
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
			Gap(theme.Px(t, 4)).Padding(0, right, 0, left).Children(func() {
			s.toolbarItems(c, nav, e, ready, narrow)
		})
	})
}

func (s *Screen) toolbarItems(c *ui.Context, nav Nav, e client.Entry, ready, narrow bool) {
	t := c.Theme()
	back := strings.ReplaceAll(s.text.Back, "{scope}", nav.Scope)
	b := ui.ButtonBase(c).Height(theme.Px(t, 32)).MinWidth(0).Shrink(1).Gap(theme.Px(t, 2)).Padding(0, theme.Px(t, 10), 0, theme.Px(t, 4)).
		Radius(theme.Px(t, 8)).Label(back)
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
		ui.Icon(c, icons.ChevronLeft).FontSize(theme.IconMedium(t)).TextColor(color)
		ui.Text(c, nav.Scope).SingleLine().MinWidth(0).Shrink(1).FontWeight(500).TextColor(color)
	})
	if b.Clicked() {
		s.deps.Back()
	}
	ui.Box(c).Grow(1)
	if s.tool(c, icons.ChevronUp, s.text.Previous, "K", theme.ToolState{Disabled: !nav.Previous}).Clicked() {
		s.deps.Move(-1)
	}
	if s.tool(c, icons.ChevronDown, s.text.Next, "J", theme.ToolState{Disabled: !nav.Next}).Clicked() {
		s.deps.Move(1)
	}
	// The one capsule of glass holds the two AI toggles, in place of separators.
	g := ui.Row(c).Height(theme.Px(t, 34)).AlignItems(ui.Center).Gap(1).PaddingX(theme.Px(t, 4)).Radius(theme.Pill(theme.Px(t, 34)))
	theme.Capsule(c, g, false).Children(func() {
		on, label := s.jobs[summary].on, s.text.Summarize
		if on {
			label = s.text.HideSummary
		}
		if s.toggle(c, icons.Sparkles, label, label, on, !ready, narrow).Clicked() {
			if on {
				s.cancel(summary)
			} else {
				s.request(summary)
			}
		}
		// A translation the article cannot use stays in place, disabled, still labelled Translate; its name says why.
		if !s.canTranslate(e) {
			reason := s.translateDone()
			ui.Box(c).Key("translate-done").Tooltip(reason).Children(func() {
				s.toggle(c, icons.Languages, s.text.Translate, reason, false, true, narrow)
			})
			return
		}
		on, label = s.translating(), s.text.Translate
		if on {
			label = s.text.ShowOriginal
		}
		if s.toggle(c, icons.Languages, label, label, on, !ready, narrow).Clicked() {
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
	})
	star, starState := s.text.Star, theme.ToolState{Disabled: !ready}
	if e.Starred {
		star, starState.On, starState.Tint = s.text.Unstar, true, theme.Star(t)
	}
	if s.tool(c, icons.Star, star, "S", starState).Role(ui.RoleToggleButton).Checked(e.Starred).Clicked() {
		s.deps.Star()
	}
	read, readIcon := s.text.MarkRead, icons.MailOpen
	if e.IsRead {
		read, readIcon = s.text.MarkUnread, icons.Mail
	}
	// Read, open and copy link are the buttons a narrow column moves into More, the last of them first.
	buttons := [...]struct {
		icon     *ui.SVG
		label    string
		key      string
		disabled bool
		do       func()
	}{
		{readIcon, read, "M", !ready, s.deps.ToggleRead},
		{icons.ExternalLink, s.text.Open, "O", !ready || e.URL == "", func() { s.openOriginal(c) }},
		{icons.Link, s.text.CopyLink, "", !ready || e.URL == "", func() { s.CopyLink(e) }},
	}
	inline := len(buttons) - overflow(s.columnWidth(c, nav))
	for _, bt := range buttons[:inline] {
		if s.tool(c, bt.icon, bt.label, bt.key, theme.ToolState{Disabled: bt.disabled}).Clicked() {
			bt.do()
		}
	}
	if nav.Immersive && s.tool(c, icons.Minimize2, s.text.ExitImmersive, "Z", theme.ToolState{}).Clicked() {
		s.immerse()
	}
	s.tool(c, icons.Ellipsis, s.text.More, "", theme.ToolState{Disabled: !ready}).Menu(func(m *ui.Menu) {
		for _, bt := range buttons[inline:] {
			if m.Item(bt.label).Disabled(bt.disabled).Chosen() {
				bt.do()
			}
		}
		if inline < len(buttons) {
			m.Separator()
		}
		if m.Item(s.text.CopyMarkdown).Chosen() {
			s.Export(e, false)
		}
		if m.Item(s.text.SaveMarkdown).Disabled(s.deps.Save == nil).Chosen() {
			s.Export(e, true)
		}
	})
}

// overflow is how many of the toolbar's read, open and copy link buttons move into More in a main column this wide:
// copy link below 500, open too below 440, and mark read too below 380. See docs/design.md, Reader.
func overflow(column float32) int {
	switch {
	case column < 380:
		return 3
	case column < 440:
		return 2
	case column < 500:
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
	name := s.target
	switch s.target {
	case "zh-Hans":
		name = s.text.LanguageZhHans
	case "en":
		name = s.text.LanguageEn
	}
	return strings.ReplaceAll(s.text.TranslateDone, "{language}", name)
}

// CopyLink puts the original link of the entry e on the clipboard and says so in a toast and to screen readers. It
// does no work of its own; the clipboard and the messages follow in the next frame (see Flush). An entry with no link
// does nothing. See docs/product.md, Copy link.
func (s *Screen) CopyLink(e client.Entry) {
	if e.URL == "" {
		return
	}
	s.clip, s.clipped, s.announce, s.toast = e.URL, true, s.text.LinkCopied, s.text.LinkCopied
}

// Export copies the entry e as Markdown to the clipboard, or with save asks where to save it and writes the file. The
// open article is converted as it shows; any other is got from the server first. The work runs in the background,
// and the clipboard and the messages follow in the next frame (see Flush). See docs/product.md, Export as Markdown.
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
	if c.article.ExtractStatus == "ok" && len(c.full) > 0 {
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

// tool is a bare icon button of the toolbar: an icon, named by label, its key, if any, in the tooltip. Its look is
// the one toolbar button of theme.ToolButton.
func (s *Screen) tool(c *ui.Context, icon *ui.SVG, label, key string, st theme.ToolState) ui.Element {
	t := c.Theme()
	tip := label
	if key != "" {
		tip = s.shortcut(label, key)
	}
	b, color := theme.ToolButton(c, st)
	b.Label(label).Tooltip(tip)
	theme.Fade(b).Children(func() { ui.Icon(c, icon).FontSize(theme.IconMedium(t)).TextColor(color) })
	return b
}

// toggle is a button of the capsule that stays pressed while on, in the accent color on its soft tint. It carries its
// label beside the icon, and only the icon in a narrow window. Its name is the label, or the reason it is disabled; only
// a reason gets a tooltip, as a label alone is never repeated in one.
func (s *Screen) toggle(c *ui.Context, icon *ui.SVG, label, name string, on, disabled, narrow bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Height(theme.Px(t, 28)).Gap(theme.Px(t, 6)).Radius(theme.Pill(theme.Px(t, 28))).Label(name).
		Disabled(disabled).Role(ui.RoleToggleButton).Checked(on)
	if name != label {
		b.Tooltip(name)
	}
	if narrow {
		b.Width(theme.Px(t, 30))
	} else {
		b.Padding(0, theme.Px(t, 11), 0, theme.Px(t, 9))
	}
	color := t.TextMuted
	switch {
	case disabled:
		color = theme.Disabled(t)
	case on:
		b.Background(theme.Soft(t, t.Accent))
		color = t.Accent
	case b.Pressed():
		b.Background(t.SurfacePressed)
		color = t.Text
	case b.Hovered():
		b.Background(t.SurfaceHover)
		color = t.Text
	}
	theme.Fade(b).Children(func() {
		ui.Icon(c, icon).FontSize(theme.IconMedium(t)).TextColor(color)
		if !narrow {
			ui.Text(c, label).FontSize(theme.Px(t, 12)).FontWeight(500).LetterSpacing(theme.Px(t, 12) * 0.01).NoWrap().TextColor(color)
		}
	})
	return b
}

// page is the scrolling article: its source and time, its title, a note on where its text comes from, and the text.
func (s *Screen) page(c *ui.Context, narrow bool) {
	t := c.Theme()
	e := s.article.Entry
	pad := []float32{theme.Bar + theme.Px(t, 28), theme.Px(t, 32), theme.Px(t, 48), theme.Px(t, 32)}
	if narrow {
		pad = []float32{theme.Bar + theme.Px(t, 20), theme.Px(t, 20), theme.Px(t, 40), theme.Px(t, 20)}
	}
	if s.find.open {
		pad[0] += findHeight(t)
	}
	title := strings.TrimSpace(e.Title)
	if s.translating() && s.title != "" {
		title = s.title
	}
	area := ui.Scroll(c).TrackScroll(&s.scroll).Grow(1).FillWidth().ScrollbarInsets(theme.Bar, 0, 0, 0).Focusable().
		FocusRing(false).Label(title)
	if s.focus {
		area.Focus()
		s.focus = false
	}
	area.Children(func() {
		if s.loading {
			s.skeleton(c, pad)
			return
		}
		blocks, how := s.body()
		if s.translating() {
			blocks, how = s.translated, whole
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
		body := ui.Column(c).FillWidth().MaxWidth(s.column(t)).Margin(0, ui.Auto).Padding(pad...).Selectable()
		// The menu is built by this call, so the body is named before it.
		body.ContextMenu(func(m *ui.Menu) { s.selectionMenu(m, body) })
		body.Children(func() {
			ui.Row(c).MinWidth(0).AlignItems(ui.Center).Gap(t.Space(1.5)).Children(func() {
				glyph(c, e.SourceTitle, s.icon)
				ui.Text(c, e.SourceTitle).FontSize(theme.Px(t, 12)).FontWeight(500).LetterSpacing(theme.Px(t, 12) * 0.005).
					TextColor(theme.Text2(t)).SingleLine().MinWidth(0).Shrink(1)
				ui.Text(c, "·").FontSize(theme.Px(t, 12)).TextColor(t.TextMuted.Alpha(0.7))
				ui.Text(c, s.when(e)).FontSize(theme.Px(t, 12)).LetterSpacing(theme.Px(t, 12) * 0.005).FontFeatures("tnum").
					TextColor(t.TextMuted).NoWrap()
				if reading := s.readLabel(e); reading != "" {
					ui.Text(c, "·").FontSize(theme.Px(t, 12)).TextColor(t.TextMuted.Alpha(0.7))
					ui.Text(c, reading).FontSize(theme.Px(t, 12)).LetterSpacing(theme.Px(t, 12) * 0.005).FontFeatures("tnum").
						TextColor(t.TextMuted).NoWrap()
				}
			})
			size := theme.Px(t, 24)
			if narrow {
				size = theme.Px(t, 21)
			}
			s.han = s.hanText(e, title, blocks)
			tracking, line := -0.015*size, s.leading(1.28, 1.42)
			if theme.Han(title) {
				tracking = 0
			}
			s.plain(c, &s.titleSet, title, unitTitle, s.han, size, tracking).FontSize(size).FontWeight(600).LineHeight(line).
				LetterSpacing(tracking).TextColor(t.Text).Wrap().Margin(theme.Px(t, 12), 0, 0, 0).Role(ui.RoleHeading).Level(1)
			s.alsoLine(c, e)
			if s.jobs[translation].on {
				s.translationNote(c)
			}
			if s.aiOff {
				s.aiOffCard(c)
			} else if s.jobs[summary].on {
				s.summaryCard(c)
			}
			switch how {
			case getting:
				s.note(c, false, s.text.ExtractingNote)
			case carried:
				s.note(c, true, s.text.FeedNote)
			}
			if s.switchable() && !s.translating() {
				s.textSwitch(c)
			}
			if len(blocks) > 0 {
				ui.Column(c).FillWidth().Margin(theme.Px(t, 22), 0, 0, 0).Children(func() { s.blocks(c, blocks) })
			}
		})
		switch how {
		case waiting:
			s.empty(c, icons.LoaderCircle, t.TextMuted, s.text.Extracting, s.text.ExtractingHint, func() {
				if s.button(c, icons.ExternalLink, s.text.OpenOriginal, false) {
					s.openOriginal(c)
				}
			}).FillWidth().Padding(t.Space(6), t.Space(6), t.Space(6), t.Space(6))
		case nothing:
			s.empty(c, icons.FileText, t.TextMuted, s.text.Failed, s.text.FailedHint, func() {
				if s.button(c, icons.ExternalLink, s.text.OpenOriginal, true) {
					s.openOriginal(c)
				}
				if s.article.ExtractStatus == "failed" && s.button(c, icons.RefreshCw, s.text.Retry, false) {
					s.retry()
				}
			}).FillWidth().Padding(t.Space(6), t.Space(6), t.Space(6), t.Space(6))
		}
	})
}

// alsoLine is the other sources that carry the article, under its title: a text button each, with the source's
// monogram, name and time, that shows that copy. There is none for an article that only one source carries, or when the screen cannot show a copy.
func (s *Screen) alsoLine(c *ui.Context, e client.Entry) {
	if s.deps.OpenCopy == nil || e.Group == nil || len(e.Group.Entries) == 0 {
		return
	}
	t := c.Theme()
	ui.Row(c).Key("also").FillWidth().AlignItems(ui.Center).Wrap().Gap(theme.Px(t, 2)).Margin(theme.Px(t, 10), 0, 0, 0).
		Role(ui.RoleList).Label(s.text.AlsoHead).Children(func() {
		ui.Text(c, s.text.AlsoHead).FontSize(theme.Px(t, 12)).FontWeight(500).NoWrap().TextColor(t.TextMuted).
			Margin(0, theme.Px(t, 4), 0, 0)
		for _, m := range e.Group.Entries {
			label := m.SourceTitle + " " + s.when(client.Entry{CreatedAt: m.CreatedAt})
			b := ui.ButtonBase(c).Key(m.ID).Height(theme.Px(t, 28)).PaddingX(theme.Px(t, 8)).Gap(theme.Px(t, 6)).Radius(theme.Px(t, 6)).
				Role(ui.RoleListItem).Label(label)
			color := theme.Text2(t)
			switch {
			case b.Pressed():
				b.Background(t.SurfacePressed)
				color = t.Text
			case b.Hovered():
				b.Background(t.SurfaceHover)
				color = t.Text
			}
			b.Children(func() {
				theme.Monogram(c, m.SourceTitle, 15)
				ui.Text(c, m.SourceTitle).FontSize(theme.Px(t, 13)).FontWeight(500).NoWrap().TextColor(color)
				ui.Text(c, s.when(client.Entry{CreatedAt: m.CreatedAt})).FontSize(theme.Px(t, 12)).FontFeatures("tnum").
					NoWrap().TextColor(t.TextMuted)
			})
			if b.Clicked() {
				s.deps.OpenCopy(m.ID)
			}
		}
	})
}

// glyph is the site icon of the subscription, in a tile with a rim, or its monogram when there is no icon.
func glyph(c *ui.Context, name string, icon *ui.Bitmap) {
	t := c.Theme()
	if icon == nil {
		theme.Monogram(c, name, 15)
		return
	}
	ui.Box(c).Size(theme.Px(t, 15), theme.Px(t, 15)).Radius(4).Clip().Children(func() {
		ui.Image(c, icon).Fit(ui.Cover).Fill()
		ui.Box(c).Absolute().Fill().Radius(4).Border(0.5, theme.Ink(t, 0.2))
	})
}

func (s *Screen) skeleton(c *ui.Context, pad []float32) {
	t := c.Theme()
	ui.Column(c).FillWidth().MaxWidth(s.column(t)).Margin(0, ui.Auto).Padding(pad...).Label(s.text.Loading).
		Role(ui.RoleProgress).Children(func() {
		bar := func(width, height, top, bottom float32) {
			ui.Row(c).FillWidth().Margin(theme.Px(t, top), 0, theme.Px(t, bottom), 0).Children(func() {
				ui.Box(c).Grow(width).Basis(0).Height(theme.Px(t, height)).Radius(theme.Pill(theme.Px(t, height))).Background(theme.Track(t))
				ui.Box(c).Grow(100 - width).Basis(0)
			})
		}
		bar(30, 12, 10, 10)
		bar(80, 22, 14, 22)
		for range 2 {
			for range 3 {
				bar(96, 12, 0, 10)
			}
			bar(64, 12, 0, 24)
		}
	})
}

// note says where the text shown comes from, with a link to the original: the warning tint when the text could
// not be got, the Surface color with a spinner while it is being got.
func (s *Screen) note(c *ui.Context, warn bool, text string) {
	t := c.Theme()
	color, background, icon := theme.Text2(t), t.Surface, t.TextMuted
	if warn {
		background, icon = theme.Soft(t, t.Warning), t.Warning
	}
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(t.Space(2)).Margin(theme.Px(t, 18), 0, 0, 0).Padding(theme.Px(t, 11), theme.Px(t, 14)).
		Radius(theme.Px(t, 12)).Background(background).Role(ui.RoleStatus).Children(func() {
		if warn {
			ui.Icon(c, icons.TriangleAlert).FontSize(theme.IconSmall(t)).TextColor(icon).Margin(2, 0, 0, 0)
		} else {
			spin := ui.Icon(c, icons.LoaderCircle).FontSize(theme.IconSmall(t)).TextColor(icon).Margin(2, 0, 0, 0)
			spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
		}
		ui.RichText(c).Grow(1).MinWidth(0).FontSize(theme.Px(t, 12)).TextColor(color).Wrap().Children(func() {
			s.pieces(c, &s.noteSet, text+" ", s.han, theme.Px(t, 12), 0)
			if s.article.URL != "" {
				ui.Link(c, s.text.OpenOriginal, s.article.URL).TextColor(color).Underline().FontWeight(500)
			}
		})
		if warn && s.article.ExtractStatus == "failed" && s.textButton(c, s.text.Retry, color) {
			s.retry()
		}
	})
}

// textSwitch is the choice between the full text and what the feed carries, under the title and its notes.
func (s *Screen) textSwitch(c *ui.Context) {
	t := c.Theme()
	from := s.feedText
	ui.Row(c).FillWidth().Margin(theme.Px(t, 18), 0, 0, 0).Children(func() {
		seg := ui.SegmentedBase(c, &s.feedText, 2)
		seg.Track.Padding(2).Gap(2).Radius(theme.Px(t, 8)).Background(theme.Ink(t, 0.07)).Label(s.text.TextSwitch).
			Children(func() {
				for i, name := range []string{s.text.ShowFull, s.text.ShowFeed} {
					b := seg.Segment(i).Height(theme.Px(t, 24)).PaddingX(theme.Px(t, 9)).Radius(theme.Px(t, 6)).Label(name)
					color := theme.Text2(t)
					switch {
					case i == s.feedText:
						theme.Raised(t, b)
						color = t.Text
					case b.Hovered():
						color = t.Text
					}
					b.Children(func() { ui.Text(c, name).FontSize(theme.Px(t, 12)).NoWrap().TextColor(color) })
				}
			})
	})
	if s.feedText != from {
		s.switched(c, from)
	}
}

// card is the box under the title holding the summary, or saying AI is not set up: a head line, then the rest.
func (s *Screen) card(c *ui.Context, background ui.Color, label string, children func()) {
	t := c.Theme()
	ui.Column(c).FillWidth().Margin(theme.Px(t, 18), 0, 0, 0).Padding(theme.Px(t, 14), theme.Px(t, 16), theme.Px(t, 16), theme.Px(t, 16)).
		Radius(theme.Px(t, 14)).Background(background).Role(ui.RoleGroup).Label(label).Children(children)
}

// head is the first line of a card: an icon, the title in color, what follows it, and a button at the end.
func (s *Screen) head(c *ui.Context, icon *ui.SVG, color ui.Color, title string, children func()) ui.Element {
	t := c.Theme()
	return ui.Row(c).FillWidth().Height(theme.Px(t, 22)).AlignItems(ui.Center).Gap(theme.Px(t, 6)).Children(func() {
		i := ui.Icon(c, icon).FontSize(theme.IconSmall(t)).TextColor(color)
		if icon == icons.LoaderCircle {
			i.Rotate(i.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
		}
		ui.Text(c, title).FontSize(theme.Px(t, 12)).FontWeight(500).LetterSpacing(theme.Px(t, 12) * 0.01).TextColor(color).SingleLine().
			MinWidth(0).Shrink(1)
		children()
	})
}

// mini is the close button at the end of a card's head.
func (s *Screen) mini(c *ui.Context, label string) bool {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(label).Tooltip(label)
	color := t.TextMuted
	var fill ui.Color
	switch {
	case b.Pressed():
		fill = t.SurfacePressed
		color = t.Text
	case b.Hovered():
		fill = t.SurfaceHover
		color = t.Text
	}
	x, y := theme.Hit{W: theme.Px(t, 22), H: theme.Px(t, 22), Radius: theme.Px(t, 6), Fill: fill, Fade: true}.Apply(c, b, func() {
		ui.Icon(c, icons.X).FontSize(theme.IconSmall(t)).TextColor(color)
	})
	b.Margin(y, x, y, ui.Auto)
	return b.Clicked()
}

// textButton is a button of a card's head or a note that is only its text, in color.
func (s *Screen) textButton(c *ui.Context, label string, color ui.Color) bool {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(label)
	var fill ui.Color
	switch {
	case b.Pressed():
		fill = t.SurfacePressed
	case b.Hovered():
		fill = t.SurfaceHover
	}
	_, y := theme.Hit{H: theme.Px(t, 22), Radius: theme.Px(t, 6), Fill: fill, PadX: theme.Px(t, 6), Fade: true}.Apply(c, b, func() {
		ui.Text(c, label).FontSize(theme.Px(t, 12)).FontWeight(500).NoWrap().TextColor(color)
	})
	b.Margin(y, 0, y, ui.Auto)
	return b.Clicked()
}

// small is the small button of a card: an icon (none when nil) and a label, primary in the accent color.
func (s *Screen) small(c *ui.Context, icon *ui.SVG, label string, primary bool) bool {
	t := c.Theme()
	b := ui.ButtonBase(c).Height(theme.Px(t, 26)).Margin(theme.Px(t, 10), 0, 0, 0).Gap(t.Space(1.5)).PaddingX(theme.Px(t, 10)).
		Radius(theme.Px(t, 8)).Label(label)
	color := t.Text
	switch {
	case primary && b.Pressed():
		b.Background(t.AccentPressed)
	case primary && b.Hovered():
		b.Background(t.AccentHover)
	case primary:
		b.Background(t.Accent)
	case b.Pressed():
		b.Background(theme.Track(t).Mix(t.Text, 0.16))
	case b.Hovered():
		b.Background(theme.Track(t).Mix(t.Text, 0.1))
	default:
		b.Background(theme.Track(t))
	}
	if primary {
		color = t.AccentText
	}
	b.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).FontSize(theme.IconSmall(t)).TextColor(color)
		}
		ui.Text(c, label).FontSize(theme.Px(t, 12)).FontWeight(500).NoWrap().TextColor(color)
	})
	return b.Clicked()
}

// summaryCard is the summary under the title: being made, with its Cancel and skeleton lines; made, with the model
// that made it; or failed, with why and Try again.
func (s *Screen) summaryCard(c *ui.Context) {
	t := c.Theme()
	j := s.jobs[summary]
	switch {
	case j.working:
		s.card(c, theme.Soft(t, t.Accent), s.text.Summary, func() {
			s.head(c, icons.LoaderCircle, t.Accent, s.text.Summarizing, func() {
				if s.textButton(c, s.text.Cancel, t.TextMuted) {
					s.cancel(summary)
				}
			}).Role(ui.RoleStatus)
			for _, width := range []float32{96, 90, 58} {
				ui.Row(c).FillWidth().Margin(theme.Px(t, 9), 0, 0, 0).Children(func() {
					ui.Box(c).Grow(width).Basis(0).Height(theme.Px(t, 10)).Radius(theme.Pill(theme.Px(t, 10))).
						Background(theme.Ink(t, 0.08))
					ui.Box(c).Grow(100 - width).Basis(0)
				})
			}
		})
	case j.failed:
		s.card(c, theme.Soft(t, t.Danger), s.text.Summary, func() {
			s.head(c, icons.TriangleAlert, t.Danger, s.text.SummaryFailed, func() {
				if s.mini(c, s.text.HideSummary) {
					s.cancel(summary)
				}
			}).Role(ui.RoleStatus)
			ui.Text(c, s.reason(j.code)).Margin(theme.Px(t, 6), 0, 0, 0).FontSize(theme.Px(t, 13)).
				LineHeight(cardLine(s.reason(j.code))).TextColor(t.Text).Wrap()
			ui.Row(c).Children(func() {
				if s.small(c, icons.RefreshCw, s.text.Retry, false) {
					s.request(summary)
				}
			})
		})
	default:
		s.card(c, theme.Soft(t, t.Accent), s.text.Summary, func() {
			s.head(c, icons.Sparkles, t.Accent, s.text.Summary, func() {
				if j.model != "" {
					by := strings.ReplaceAll(s.text.By, "{model}", j.model)
					ui.Text(c, "· "+by).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).SingleLine().MinWidth(0).Shrink(1)
				}
				if s.mini(c, s.text.HideSummary) {
					s.cancel(summary)
				}
			})
			text := strings.TrimSpace(j.text)
			s.plain(c, &s.summarySet, text, unitSummary, theme.Han(text), theme.Px(t, 13), 0).Margin(theme.Px(t, 6), 0, 0, 0).FontSize(theme.Px(t, 13)).
				LineHeight(cardLine(text)).TextColor(t.Text).Wrap()
		})
	}
}

// aiOffCard stands in for a summary or translation while AI is not set up, and leads to the settings.
func (s *Screen) aiOffCard(c *ui.Context) {
	t := c.Theme()
	s.card(c, t.Surface, s.text.AIOffTitle, func() {
		s.head(c, icons.Sparkles, theme.Text2(t), s.text.AIOffTitle, func() {
			if s.mini(c, s.text.Close) {
				s.aiOff = false
			}
		}).Role(ui.RoleStatus)
		ui.Text(c, s.text.AIOff).Margin(theme.Px(t, 6), 0, 0, 0).FontSize(theme.Px(t, 13)).LineHeight(cardLine(s.text.AIOff)).
			TextColor(theme.Text2(t)).Wrap()
		ui.Row(c).Children(func() {
			if s.small(c, icons.Settings, s.text.OpenSettings, true) && s.deps.OpenSettings != nil {
				s.deps.OpenSettings()
			}
		})
	})
}

// translationNote says what the translation is doing under the title: being made, with Cancel, the original
// meanwhile; failed, with why and Try again; made, with the model and the Translation | Original switch.
func (s *Screen) translationNote(c *ui.Context) {
	t := c.Theme()
	j := s.jobs[translation]
	row := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Margin(theme.Px(t, 18), 0, 0, 0).
		Padding(theme.Px(t, 11), theme.Px(t, 14)).Radius(theme.Px(t, 12))
	text2 := theme.Text2(t)
	switch {
	case j.working:
		row.Background(t.Surface).Role(ui.RoleStatus).Children(func() {
			spin := ui.Icon(c, icons.LoaderCircle).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
			spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
			ui.Text(c, s.text.Translating).Grow(1).MinWidth(0).FontSize(theme.Px(t, 12)).TextColor(text2).Wrap()
			if s.textButton(c, s.text.Cancel, text2) {
				s.cancel(translation)
			}
		})
	case j.failed:
		reason := strings.ReplaceAll(s.text.TranslationFailed, "{reason}", s.reason(j.code))
		row.Background(theme.Soft(t, t.Danger)).Role(ui.RoleStatus).Children(func() {
			ui.Icon(c, icons.TriangleAlert).FontSize(theme.IconSmall(t)).TextColor(t.Danger)
			ui.Text(c, reason).Grow(1).MinWidth(0).FontSize(theme.Px(t, 12)).TextColor(text2).Wrap()
			if s.textButton(c, s.text.Retry, text2) {
				s.request(translation)
			}
		})
	default:
		row.Background(theme.Soft(t, t.Accent)).Children(func() {
			ui.Icon(c, icons.Languages).FontSize(theme.IconSmall(t)).TextColor(t.Accent)
			by := strings.ReplaceAll(s.text.Translated, "{model}", j.model)
			s.plain(c, &s.translatedSet, by, -1, s.han, theme.Px(t, 12), 0).Grow(1).MinWidth(0).FontSize(theme.Px(t, 12)).
				TextColor(text2).Wrap()
			seg := ui.SegmentedBase(c, &s.shown, 2)
			seg.Track.Margin(-4, -6, -4, 0).Padding(2).Gap(2).Radius(theme.Px(t, 8)).Background(theme.Ink(t, 0.07)).
				Label(s.text.Translate).Children(func() {
				for i, name := range []string{s.text.Translation, s.text.Original} {
					b := seg.Segment(i).Height(theme.Px(t, 24)).PaddingX(theme.Px(t, 9)).Radius(theme.Px(t, 6)).Label(name)
					color := theme.Text2(t)
					switch {
					case i == s.shown:
						theme.Raised(t, b)
						color = t.Text
					case b.Hovered():
						color = t.Text
					}
					b.Children(func() { ui.Text(c, name).FontSize(theme.Px(t, 12)).NoWrap().TextColor(color) })
				}
			})
		})
	}
}

// empty is a state standing in for the text: a tile, a heading, a hint, and the buttons.
func (s *Screen) empty(c *ui.Context, icon *ui.SVG, color ui.Color, title, hint string, buttons func()) ui.Element {
	t := c.Theme()
	return ui.Column(c).Center().Gap(t.Space(3)).Role(ui.RoleStatus).Children(func() {
		tile := t.Surface
		if color == t.Danger {
			tile = theme.Soft(t, color)
		}
		ui.Box(c).Size(t.Space(12), t.Space(12)).Radius(t.Space(4)).Center().Background(tile).
			Children(func() {
				i := ui.Icon(c, icon).FontSize(theme.IconLarge(t)).TextColor(color)
				if icon == icons.LoaderCircle {
					i.Rotate(i.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
				}
			})
		ui.Text(c, title).FontSize(theme.Px(t, 15)).FontWeight(600).Role(ui.RoleHeading).Level(2)
		ui.Text(c, hint).MaxWidth(theme.Px(t, 320)).TextColor(t.TextMuted).TextAlign(ui.Center).Wrap()
		ui.Row(c).Gap(t.Space(2)).Children(buttons)
	})
}

// button is a button of an empty state, primary in the accent color; it reports a click.
func (s *Screen) button(c *ui.Context, icon *ui.SVG, label string, primary bool) bool {
	t := c.Theme()
	b := ui.ButtonBase(c).Height(t.Space(8)).Gap(t.Space(1.5)).PaddingX(t.Space(3.5)).Radius(theme.Px(t, 10))
	color := t.Text
	switch {
	case primary && b.Pressed():
		b.Background(t.AccentPressed)
	case primary && b.Hovered():
		b.Background(t.AccentHover)
	case primary:
		b.Background(t.Accent)
	case b.Pressed():
		b.Background(theme.Track(t).Mix(t.Text, 0.16))
	case b.Hovered():
		b.Background(theme.Track(t).Mix(t.Text, 0.1))
	default:
		b.Background(theme.Track(t))
	}
	if primary {
		color = t.AccentText
	}
	b.Children(func() {
		ui.Icon(c, icon).FontSize(theme.IconMedium(t)).TextColor(color)
		text := ui.Text(c, label).NoWrap().TextColor(color)
		if primary {
			text.FontWeight(500)
		}
	})
	return b.Clicked()
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
	case b.kind == picture || before.kind == picture || before.kind == caption:
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
		s.revealBlock(c, unit)
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
			s.table(c, b)
		case picture:
			s.picture(c, b)
		case code:
			ui.ScrollHorizontal(c).FillWidth().Radius(theme.Px(t, 12)).Background(t.Surface).Children(func() {
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
			s.inline(c, b, unit, color, size).FontSize(size).FontWeight(600).LineHeight(line).LetterSpacing(tracking * size).
				Role(ui.RoleHeading).Level(b.level)
		case caption:
			s.inline(c, b, unit, t.TextMuted, theme.Px(t, 12)).FontSize(theme.Px(t, 12)).LineHeight(1.5)
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
	e := s.inline(c, b, unit, color, size)
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
func (s *Screen) inline(c *ui.Context, b block, unit int, color ui.Color, size float32) ui.Element {
	t := c.Theme()
	return ui.RichText(c).TextColor(color).Wrap().Children(func() {
		n, base := 0, 0
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
// where cells are cut off and a bar for the place; see docs/design.md "阅读器".
func (s *Screen) table(c *ui.Context, b block) {
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
	label := strings.NewReplacer("{c}", strconv.Itoa(g.cols), "{r}", strconv.Itoa(len(g.rows))).Replace(s.text.TableName)
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
				n := 0
				for r, row := range g.rows {
					for _, cl := range row {
						n++
						s.cell(c, cl, r, size, n)
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
		s.inline(c, block{spans: g.caption}, -1, t.TextMuted, theme.Px(t, 12)).FontSize(theme.Px(t, 12)).LineHeight(1.5).
			Margin(theme.Px(t, 8), 0, 0, 0)
	}
}

// cell is a cell of a table, in row r: padded, with a divider above it unless it is in the first row, the header
// row on the Surface color in Text at weight 600.
func (s *Screen) cell(c *ui.Context, cl cell, r int, size float32, key int) {
	t := c.Theme()
	box := ui.Column(c).Key(key).ColumnSpan(cl.span).MinWidth(theme.Px(t, 48)).MaxWidth(theme.Px(t, 240)).
		Padding(theme.Px(t, 8), theme.Px(t, 12))
	if cl.head {
		box.Background(t.Surface)
	}
	if r > 0 {
		box.BorderWidth(0.5, 0, 0, 0).BorderColor(t.Border)
	}
	box.Children(func() {
		color := theme.Prose(t)
		if cl.head {
			color = t.Text
		}
		e := s.inline(c, block{spans: cl.spans}, -1, color, size).FontSize(size).LineHeight(1.5).FillWidth()
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

// picture is a picture of the text on the Surface color until it loads, as wide as the page says or the text,
// whichever is less; one that fails to load is left out.
func (s *Screen) picture(c *ui.Context, b block) {
	t := c.Theme()
	bitmap, failed := s.bitmap(b.src)
	if failed {
		return
	}
	width, height := b.width, b.height
	if (width == 0 || height == 0) && bitmap != nil {
		width, height = bitmap.Size()
	}
	ratio := float32(16.0 / 9)
	if width > 0 && height > 0 {
		ratio = float32(width) / float32(height)
	}
	box := ui.Box(c).FillWidth().AspectRatio(ratio).Radius(theme.Px(t, 12)).Background(t.Surface).Clip().Role(ui.RoleImage).
		Label(b.alt)
	if width > 0 {
		box.MaxWidth(float32(width))
	}
	box.Children(func() {
		if bitmap != nil {
			ui.Image(c, bitmap).Fit(ui.Cover).Fill()
		}
		ui.Box(c).Absolute().Fill().Radius(theme.Px(t, 12)).Border(0.5, theme.Ink(t, 0.1)).PassThrough()
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
