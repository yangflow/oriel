// Package reader is the reader: an article in its full text, drawn natively in the place of the timeline's list,
// or what the feed carries while the text is being got or when it could not be; see design/screens/reader.html.
// Everything it shows comes through the HTTP API.
package reader

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/theme"
)

//go:embed copy.json
var catalog []byte

type copy struct {
	Back, Previous, Next, Star, Unstar, MarkRead, MarkUnread, Open, FeedNote, ExtractingNote, OpenOriginal string
	Extracting, ExtractingHint, Failed, FailedHint, Error, ErrorHint, Retry, Loading, FullText, OpenFailed string
	DateLayout, YearLayout, Shortcut                                                                       string
	Summarize, HideSummary, Translate, ShowOriginal, Summary, By, Summarizing, Cancel, SummaryFailed       string
	Translating, Translated, Translation, Original, TranslationFailed, SummaryReady, TranslationReady      string
	AIUnreachable, AITimeout, AIUnauthorized, AIRateLimited, AIUpstream, AIEmpty, AIOff, AIOffTitle        string
	OpenSettings, Close                                                                                    string
}

var translations = func() map[string]copy {
	var values map[string]copy
	if err := json.Unmarshal(catalog, &values); err != nil {
		panic(err)
	}
	return values
}()

const (
	pollEvery    = 5 * time.Second // how often an article whose text is being got is asked for again
	aiPollEvery  = 2 * time.Second // how often a summary or translation being made is asked for again
	maxDownloads = 4
)

// API is what the reader reads on the server; client.Session is one.
type API interface {
	Entry(ctx context.Context, id int64) (client.Article, error)
	Image(ctx context.Context, address string) ([]byte, error)
	AI(ctx context.Context, id int64) (client.AIResults, error)
	RequestAI(ctx context.Context, id int64, task string) (client.AIResult, error)
}

// Deps is what the reader does outside itself. Go runs work away from the UI thread and then the function it returns
// on the UI thread. Now is the clock and Decode turns the bytes of a picture into a bitmap, off the UI thread; they
// default to time.Now and ui.DecodeBitmap. Expired reports whether err means the server no longer takes the session,
// and then shows the connect screen. Back returns to the list, Move goes to the article by entries away, and Star and
// ToggleRead change the entry read. OpenSettings opens the AI group of the settings.
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
}

// Nav is what the timeline tells the reader each frame: the entry as the list has it, with its read and starred
// state, its subscription's site icon once decoded, the scope Back returns to, whether there are articles before and
// after it, and whether the keys apply, which they do not while a dialog shows.
type Nav struct {
	Entry          client.Entry
	Icon           *ui.Bitmap
	Scope          string
	Previous, Next bool
	Keys           bool
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

// loaded is a picture of the article, once it loads: bitmap, or failed, which leaves it out.
type loaded struct {
	bitmap *ui.Bitmap
	failed bool
}

// Screen keeps the article read: its text as blocks, and the work under way.
type Screen struct {
	deps Deps
	text copy
	api  API
	gen  int        // counts articles opened, so the results for the one before are dropped
	icon *ui.Bitmap // the site icon of the entry, as the timeline last had it

	id       int64
	loading  bool
	failed   bool
	article  client.Article
	full     []block // the text taken from the page
	feed     []block // what the feed carries, or its summary
	polledAt time.Time
	polling  bool

	scroll ui.ScrollState
	focus  bool // give the page the focus in the next frame
	han    bool // the article's title is Chinese, so its text runs taller

	pictures  map[string]*loaded
	wanted    []string
	downloads int

	jobs       [2]job
	aiOff      bool // AI is not set up: the card says where to set it
	shown      int  // with the translation ready, 0 shows it and 1 the original
	title      string
	translated []block
	aiPolledAt time.Time
	aiPolling  bool

	announce string
	toast    string
}

// New selects the best available translation, falling back to English. The reader shows nothing until Open.
func New(systemLocale string, deps Deps) *Screen {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Decode == nil {
		deps.Decode = ui.DecodeBitmap
	}
	return &Screen{deps: deps, text: translations[locale.Language(systemLocale)], pictures: map[string]*loaded{}}
}

// Use reads from api from now on, dropping the work under way for the server before.
func (s *Screen) Use(api API) {
	s.api = api
	s.gen++
	s.id = 0
}

// Open shows e: it loads the article, from the top, and gives it the focus.
func (s *Screen) Open(e client.Entry) {
	s.gen++
	s.id, s.loading, s.failed, s.polling = e.ID, true, false, false
	s.article, s.full, s.feed = client.Article{Entry: e}, nil, nil
	s.scroll.X, s.scroll.Y = 0, 0
	s.focus = true
	s.pictures, s.wanted, s.downloads = map[string]*loaded{}, nil, 0
	s.jobs, s.aiOff, s.shown, s.title, s.translated, s.aiPolling = [2]job{}, false, 0, "", nil, false
	s.load(false)
	s.loadAI(true)
}

// load asks for the article; poll asks again while its text is being got, keeping what shows when that fails.
func (s *Screen) load(poll bool) {
	if s.api == nil || s.id == 0 {
		return
	}
	gen, api, id := s.gen, s.api, s.id
	s.polling = poll
	s.deps.Go(func() func() {
		a, err := api.Entry(context.Background(), id)
		var full, feed []block
		if err == nil {
			base, _ := url.Parse(a.URL)
			full = parse(a.ExtractedHTML, base, a.Title)
			feed = parse(a.ContentHTML, base, a.Title)
			if summary := strings.Join(strings.Fields(a.Summary), " "); len(feed) == 0 && summary != "" {
				feed = []block{{kind: paragraph, spans: []span{{text: summary}}}}
			}
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
			if poll && a.ExtractStatus == "ok" && len(full) > 0 {
				s.announce = s.text.FullText
			}
			s.article, s.full, s.feed = a, full, feed
		}
	})
}

// poll asks for the article every pollEvery while its text is being got.
func (s *Screen) poll(c *ui.Context) {
	if s.loading || s.failed || s.polling || s.article.ExtractStatus != "pending" {
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
	gen, api, id := s.gen, s.api, s.id
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
			if first {
				if r.Summary != nil && r.Summary.Status != "failed" && !s.jobs[summary].on {
					s.jobs[summary].on = true
					s.apply(summary, *r.Summary, false)
				}
				return
			}
			for i, result := range []*client.AIResult{r.Summary, r.Translation} {
				if j := s.jobs[i]; result != nil && j.working && !j.asking {
					s.apply(i, *result, true)
				}
			}
		}
	})
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
)

// body is what the article shows, and how.
func (s *Screen) body() ([]block, int) {
	switch {
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

// when is the time of the article in full: the date, with the year when it is not this one, and the time of day.
func (s *Screen) when(e client.Entry) string {
	ms := e.PublishedAt
	if ms == 0 {
		ms = e.CreatedAt
	}
	now := s.deps.Now()
	at := time.UnixMilli(ms).In(now.Location())
	if at.Year() == now.Year() {
		return at.Format(s.text.DateLayout)
	}
	return at.Format(s.text.YearLayout)
}

// bitmap returns the decoded picture at address, asking for it if it has not been, and whether it failed.
func (s *Screen) bitmap(address string) (*ui.Bitmap, bool) {
	p := s.pictures[address]
	if p == nil {
		p = &loaded{}
		s.pictures[address] = p
		s.wanted = append(s.wanted, address)
	}
	return p.bitmap, p.failed
}

// download starts the pictures asked for, up to maxDownloads at a time, in the order of the text.
func (s *Screen) download() {
	for s.api != nil && s.downloads < maxDownloads && len(s.wanted) > 0 {
		address := s.wanted[0]
		s.wanted = s.wanted[1:]
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
			}
		})
	}
}

// px converts a size of the mockup, drawn at a 13 px font, to the theme's text size.
func px(t *ui.Theme, v float32) float32 { return t.Rem(v / 13) }

// View draws the reader in the place of the list; it does no disk or network work, which runs through Deps.
func (s *Screen) View(c *ui.Context, nav Nav) {
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
			{ui.KeyEscape, s.deps.Back},
		} {
			c.OnShortcut(0, k.key, k.do)
		}
	}
	width, _ := c.Size()
	narrow := width < 760
	// The toolbar row floats over the article, which scrolls under it and fades under the scroll edge.
	ui.Column(c).Grow(1).MinWidth(0).Children(func() {
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
		theme.Edge(c)
		s.toolbar(c, nav, ready)
	})
	s.download()
	if s.announce != "" {
		c.Announce(s.announce)
		s.announce = ""
	}
	if s.toast != "" {
		c.Toast(s.toast)
		s.toast = ""
	}
}

func (s *Screen) toolbar(c *ui.Context, nav Nav, ready bool) {
	t := c.Theme()
	e := nav.Entry
	bar := c.TitleBar()
	// group is a capsule of glass holding tools that belong together, in place of separators.
	group := func(tools func()) {
		g := ui.Row(c).Height(px(t, 32)).AlignItems(ui.Center).PaddingX(2).Radius(px(t, 16))
		theme.Capsule(c, g, false).Children(tools)
	}
	ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(theme.Bar).AlignItems(ui.Center).Gap(t.Space(2)).
		Padding(0, t.Space(3)+bar.Right, 0, t.Space(3)).DragWindow().Children(func() {
		back := strings.ReplaceAll(s.text.Back, "{scope}", nav.Scope)
		b := ui.ButtonBase(c).Height(px(t, 32)).MinWidth(0).Shrink(1).Gap(2).Padding(0, px(t, 12), 0, px(t, 6)).
			Radius(px(t, 16)).Label(back).Tooltip(s.shortcut(back, "Esc"))
		theme.Capsule(c, b, b.Hovered())
		b.Children(func() {
			ui.Icon(c, icons.ChevronLeft).FontSize(px(t, 16)).TextColor(t.Text)
			ui.Text(c, nav.Scope).SingleLine().MinWidth(0).Shrink(1).TextColor(t.Text)
		})
		if b.Clicked() {
			s.deps.Back()
		}
		ui.Box(c).Grow(1)
		group(func() {
			if s.tool(c, icons.ChevronUp, s.text.Previous, "K", t.TextMuted, !nav.Previous).Clicked() {
				s.deps.Move(-1)
			}
			if s.tool(c, icons.ChevronDown, s.text.Next, "J", t.TextMuted, !nav.Next).Clicked() {
				s.deps.Move(1)
			}
		})
		group(func() {
			on, label := s.jobs[summary].on, s.text.Summarize
			if on {
				label = s.text.HideSummary
			}
			if s.toggle(c, icons.Sparkles, label, on, !ready).Clicked() {
				if on {
					s.cancel(summary)
				} else {
					s.request(summary)
				}
			}
			on, label = s.translating(), s.text.Translate
			if on {
				label = s.text.ShowOriginal
			}
			if s.toggle(c, icons.Languages, label, on, !ready).Clicked() {
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
		group(func() {
			star, starColor := s.text.Star, t.TextMuted
			if e.Starred {
				star, starColor = s.text.Unstar, t.Warning
			}
			if s.tool(c, icons.Star, star, "S", starColor, !ready).Role(ui.RoleToggleButton).Checked(e.Starred).
				Clicked() {
				s.deps.Star()
			}
			read, readIcon := s.text.MarkRead, icons.CircleCheckBig
			if e.IsRead {
				read, readIcon = s.text.MarkUnread, icons.CircleDot
			}
			if s.tool(c, readIcon, read, "M", t.TextMuted, !ready).Clicked() {
				s.deps.ToggleRead()
			}
			if s.tool(c, icons.ExternalLink, s.text.Open, "O", t.TextMuted, !ready || e.URL == "").Clicked() {
				s.openOriginal(c)
			}
		})
	})
}

// tool is a button of the toolbar: an icon, named by label, its key, if any, in the tooltip.
func (s *Screen) tool(c *ui.Context, icon *ui.SVG, label, key string, color ui.Color, disabled bool) ui.Element {
	t := c.Theme()
	tip := label
	if key != "" {
		tip = s.shortcut(label, key)
	}
	b := ui.ButtonBase(c).Size(px(t, 28), px(t, 28)).Radius(px(t, 14)).Label(label).Tooltip(tip).Disabled(disabled)
	switch {
	case disabled:
		color = color.Alpha(0.45)
	case b.Hovered():
		b.Background(t.Text.Alpha(0.08))
		if color == t.TextMuted {
			color = t.Text
		}
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(px(t, 16)).TextColor(color) })
	return b
}

// toggle is a tool that stays pressed while on, in the accent color on its soft tint.
func (s *Screen) toggle(c *ui.Context, icon *ui.SVG, label string, on, disabled bool) ui.Element {
	t := c.Theme()
	color := t.TextMuted
	if on {
		color = t.Accent
	}
	b := s.tool(c, icon, label, "", color, disabled).Role(ui.RoleToggleButton).Checked(on)
	if on && !disabled {
		b.Background(theme.Soft(t, t.Accent))
	}
	return b
}

// page is the scrolling article: its source and time, its title, a note on where its text comes from, and the text.
func (s *Screen) page(c *ui.Context, narrow bool) {
	t := c.Theme()
	e := s.article.Entry
	pad := []float32{theme.Bar + px(t, 28), px(t, 32), px(t, 48), px(t, 32)}
	if narrow {
		pad = []float32{theme.Bar + px(t, 20), px(t, 20), px(t, 40), px(t, 20)}
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
		ui.Column(c).FillWidth().MaxWidth(px(t, 680)).Margin(0, ui.Auto).Padding(pad...).Selectable().
			Children(func() {
				ui.Row(c).MinWidth(0).AlignItems(ui.Center).Gap(t.Space(1.5)).Children(func() {
					glyph(c, e.SourceTitle, s.icon)
					ui.Text(c, e.SourceTitle).FontSize(px(t, 12)).FontWeight(500).SingleLine().MinWidth(0).Shrink(1)
					ui.Text(c, "·").FontSize(px(t, 12)).TextColor(t.TextMuted)
					ui.Text(c, s.when(e)).FontSize(px(t, 12)).TextColor(t.TextMuted).NoWrap()
				})
				size := px(t, 24)
				if narrow {
					size = px(t, 20)
				}
				s.han = theme.Han(title)
				ui.Text(c, title).FontSize(size).FontWeight(650).LineHeight(s.leading(1.25, 1.4)).Wrap().Margin(px(t, 10), 0, 0, 0).
					Role(ui.RoleHeading).Level(1)
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
				if len(blocks) > 0 {
					ui.Column(c).FillWidth().Margin(px(t, 22), 0, 0, 0).Children(func() { s.blocks(c, blocks) })
				}
			})
		switch how {
		case waiting:
			s.empty(c, icons.LoaderCircle, t.Accent, s.text.Extracting, s.text.ExtractingHint, func() {
				if s.button(c, icons.ExternalLink, s.text.OpenOriginal, false) {
					s.openOriginal(c)
				}
			}).FillWidth().Padding(t.Space(6), t.Space(6), t.Space(6), t.Space(6))
		case nothing:
			s.empty(c, icons.FileText, t.Accent, s.text.Failed, s.text.FailedHint, func() {
				if s.button(c, icons.ExternalLink, s.text.OpenOriginal, true) {
					s.openOriginal(c)
				}
			}).FillWidth().Padding(t.Space(6), t.Space(6), t.Space(6), t.Space(6))
		}
	})
}

// glyph is the site icon of the subscription, in a tile with a rim, or its monogram when there is no icon.
func glyph(c *ui.Context, name string, icon *ui.Bitmap) {
	t := c.Theme()
	if icon == nil {
		monogram(c, name)
		return
	}
	ui.Box(c).Size(px(t, 16), px(t, 16)).Radius(4).Clip().Children(func() {
		ui.Image(c, icon).Fit(ui.Cover).Fill()
		ui.Box(c).Absolute().Fill().Radius(4).Border(1, t.Border)
	})
}

func monogram(c *ui.Context, name string) {
	t := c.Theme()
	letter := "·"
	if r, _ := utf8.DecodeRuneInString(strings.TrimLeft(name, "@#· ")); r != utf8.RuneError {
		letter = strings.ToUpper(string(r))
	}
	ui.Box(c).Size(px(t, 16), px(t, 16)).Radius(4).Center().Background(theme.Soft(t, t.Accent)).Children(func() {
		ui.Text(c, letter).FontSize(px(t, 10)).FontWeight(600).TextColor(t.Accent)
	})
}

func (s *Screen) skeleton(c *ui.Context, pad []float32) {
	t := c.Theme()
	ui.Column(c).FillWidth().MaxWidth(px(t, 680)).Margin(0, ui.Auto).Padding(pad...).Label(s.text.Loading).
		Role(ui.RoleProgress).Children(func() {
		bar := func(width, height, top, bottom float32) {
			ui.Row(c).FillWidth().Margin(px(t, top), 0, px(t, bottom), 0).Children(func() {
				ui.Box(c).Grow(width).Basis(0).Height(px(t, height)).Radius(px(t, height) / 2).Background(t.Surface)
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
	color, background := t.TextMuted, t.Surface
	if warn {
		color, background = t.Warning, theme.Soft(t, t.Warning)
	}
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(t.Space(2)).Margin(px(t, 18), 0, 0, 0).Padding(px(t, 10), px(t, 12)).
		Radius(t.Radius).Background(background).Role(ui.RoleStatus).Children(func() {
		if warn {
			ui.Icon(c, icons.TriangleAlert).FontSize(px(t, 14)).TextColor(color).Margin(2, 0, 0, 0)
		} else {
			spin := ui.Icon(c, icons.LoaderCircle).FontSize(px(t, 14)).TextColor(color).Margin(2, 0, 0, 0)
			spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
		}
		ui.RichText(c, ui.Span{Text: text + " "}).Grow(1).MinWidth(0).FontSize(px(t, 12.5)).TextColor(color).Wrap().
			Children(func() {
				if s.article.URL != "" {
					ui.Link(c, s.text.OpenOriginal, s.article.URL).TextColor(color).Underline().FontWeight(500)
				}
			})
	})
}

// card is the box under the title holding the summary, or saying AI is not set up: a head line, then the rest.
func (s *Screen) card(c *ui.Context, background ui.Color, label string, children func()) {
	t := c.Theme()
	ui.Column(c).FillWidth().Margin(px(t, 18), 0, 0, 0).Padding(px(t, 12), px(t, 14), px(t, 14), px(t, 14)).
		Radius(px(t, 10)).Background(background).Role(ui.RoleGroup).Label(label).Children(children)
}

// head is the first line of a card: an icon, the title in color, what follows it, and a button at the end.
func (s *Screen) head(c *ui.Context, icon *ui.SVG, color ui.Color, title string, children func()) ui.Element {
	t := c.Theme()
	return ui.Row(c).FillWidth().Height(px(t, 22)).AlignItems(ui.Center).Gap(px(t, 6)).Children(func() {
		i := ui.Icon(c, icon).FontSize(px(t, 14)).TextColor(color)
		if icon == icons.LoaderCircle {
			i.Rotate(i.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
		}
		ui.Text(c, title).FontSize(px(t, 12)).FontWeight(600).TextColor(color).SingleLine().MinWidth(0).Shrink(1)
		children()
	})
}

// mini is the close button at the end of a card's head.
func (s *Screen) mini(c *ui.Context, label string) bool {
	t := c.Theme()
	b := ui.ButtonBase(c).Size(px(t, 22), px(t, 22)).Margin(0, 0, 0, ui.Auto).Radius(px(t, 5)).Label(label).
		Tooltip(label)
	color := t.TextMuted
	if b.Hovered() {
		b.Background(t.SurfaceHover)
		color = t.Text
	}
	b.Children(func() { ui.Icon(c, icons.X).FontSize(px(t, 13)).TextColor(color) })
	return b.Clicked()
}

// textButton is a button of a card's head or a note that is only its text, in color.
func (s *Screen) textButton(c *ui.Context, label string, color ui.Color) bool {
	t := c.Theme()
	b := ui.ButtonBase(c).Height(px(t, 22)).Margin(0, 0, 0, ui.Auto).PaddingX(px(t, 6)).Radius(px(t, 5)).Label(label)
	if b.Hovered() {
		b.Background(t.Background.Alpha(0.6))
	}
	b.Children(func() { ui.Text(c, label).FontSize(px(t, 12)).FontWeight(500).NoWrap().TextColor(color) })
	return b.Clicked()
}

// small is the small button of a card: an icon and a label, primary in the accent color.
func (s *Screen) small(c *ui.Context, icon *ui.SVG, label string, primary bool) bool {
	t := c.Theme()
	b := ui.ButtonBase(c).Height(px(t, 26)).Margin(px(t, 10), 0, 0, 0).Gap(t.Space(1.5)).PaddingX(px(t, 10)).
		Radius(t.Radius).Label(label)
	color := t.Text
	switch {
	case primary && b.Pressed():
		b.Background(t.AccentPressed)
	case primary && b.Hovered():
		b.Background(t.AccentHover)
	case primary:
		b.Background(t.Accent)
	case b.Hovered():
		b.Border(1, t.Border).Background(t.SurfaceHover)
	default:
		b.Border(1, t.Border).Background(t.Background)
	}
	if primary {
		color = t.AccentText
	}
	b.Children(func() {
		ui.Icon(c, icon).FontSize(px(t, 14)).TextColor(color)
		ui.Text(c, label).FontSize(px(t, 12)).FontWeight(500).NoWrap().TextColor(color)
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
				ui.Row(c).FillWidth().Margin(px(t, 9), 0, 0, 0).Children(func() {
					ui.Box(c).Grow(width).Basis(0).Height(px(t, 10)).Radius(px(t, 5)).
						Background(t.Accent.Mix(t.Background, 0.86))
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
			ui.Text(c, s.reason(j.code)).Margin(px(t, 6), 0, 0, 0).FontSize(px(t, 14)).LineHeight(1.6).Wrap()
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
					ui.Text(c, "· "+by).FontSize(px(t, 12)).TextColor(t.TextMuted).SingleLine().MinWidth(0).Shrink(1)
				}
				if s.mini(c, s.text.HideSummary) {
					s.cancel(summary)
				}
			})
			text := strings.TrimSpace(j.text)
			line := float32(1.6)
			if theme.Han(text) {
				line = 1.75
			}
			ui.Text(c, text).Margin(px(t, 6), 0, 0, 0).FontSize(px(t, 14)).LineHeight(line).Wrap()
		})
	}
}

// aiOffCard stands in for a summary or translation while AI is not set up, and leads to the settings.
func (s *Screen) aiOffCard(c *ui.Context) {
	t := c.Theme()
	s.card(c, t.Surface, s.text.AIOffTitle, func() {
		s.head(c, icons.Sparkles, t.Text, s.text.AIOffTitle, func() {
			if s.mini(c, s.text.Close) {
				s.aiOff = false
			}
		}).Role(ui.RoleStatus)
		ui.Text(c, s.text.AIOff).Margin(px(t, 6), 0, 0, 0).FontSize(px(t, 13)).LineHeight(1.6).TextColor(t.TextMuted).
			Wrap()
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
	row := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Margin(px(t, 18), 0, 0, 0).
		Padding(px(t, 10), px(t, 12)).Radius(t.Radius)
	switch {
	case j.working:
		row.Background(t.Surface).Role(ui.RoleStatus).Children(func() {
			spin := ui.Icon(c, icons.LoaderCircle).FontSize(px(t, 14)).TextColor(t.TextMuted)
			spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
			ui.Text(c, s.text.Translating).Grow(1).MinWidth(0).FontSize(px(t, 12.5)).TextColor(t.TextMuted).Wrap()
			if s.textButton(c, s.text.Cancel, t.TextMuted) {
				s.cancel(translation)
			}
		})
	case j.failed:
		reason := strings.ReplaceAll(s.text.TranslationFailed, "{reason}", s.reason(j.code))
		row.Background(theme.Soft(t, t.Danger)).Role(ui.RoleStatus).Children(func() {
			ui.Icon(c, icons.TriangleAlert).FontSize(px(t, 14)).TextColor(t.Danger)
			ui.Text(c, reason).Grow(1).MinWidth(0).FontSize(px(t, 12.5)).TextColor(t.Danger).Wrap()
			if s.textButton(c, s.text.Retry, t.Danger) {
				s.request(translation)
			}
		})
	default:
		row.Background(theme.Soft(t, t.Accent)).Children(func() {
			ui.Icon(c, icons.Languages).FontSize(px(t, 14)).TextColor(t.Accent)
			by := strings.ReplaceAll(s.text.Translated, "{model}", j.model)
			ui.Text(c, by).Grow(1).MinWidth(0).FontSize(px(t, 12.5)).TextColor(t.Accent).Wrap()
			seg := ui.SegmentedBase(c, &s.shown, 2)
			seg.Track.Margin(-4, -6, -4, 0).Padding(2).Gap(2).Radius(t.Radius).Background(t.Surface).
				Label(s.text.Translate).Children(func() {
				for i, name := range []string{s.text.Translation, s.text.Original} {
					b := seg.Segment(i).Height(px(t, 24)).PaddingX(t.Space(2.5)).Radius(t.Space(1.5)).Label(name)
					color := t.TextMuted
					switch {
					case i == s.shown:
						theme.Raised(t, b)
						color = t.Text
					case b.Hovered():
						color = t.Text
					}
					b.Children(func() { ui.Text(c, name).FontSize(px(t, 12)).NoWrap().TextColor(color) })
				}
			})
		})
	}
}

// empty is a state standing in for the text: a tile, a heading, a hint, and the buttons.
func (s *Screen) empty(c *ui.Context, icon *ui.SVG, color ui.Color, title, hint string, buttons func()) ui.Element {
	t := c.Theme()
	return ui.Column(c).Center().Gap(t.Space(3)).Role(ui.RoleStatus).Children(func() {
		ui.Box(c).Size(t.Space(12), t.Space(12)).Radius(t.Space(3)).Center().Background(theme.Soft(t, color)).
			Children(func() {
				i := ui.Icon(c, icon).FontSize(t.Space(6)).TextColor(color)
				if icon == icons.LoaderCircle {
					i.Rotate(i.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
				}
			})
		ui.Text(c, title).FontSize(px(t, 15)).FontWeight(600).Role(ui.RoleHeading).Level(2)
		ui.Text(c, hint).MaxWidth(px(t, 320)).TextColor(t.TextMuted).TextAlign(ui.Center).Wrap()
		ui.Row(c).Gap(t.Space(2)).Children(buttons)
	})
}

// button is a button of an empty state, primary in the accent color; it reports a click.
func (s *Screen) button(c *ui.Context, icon *ui.SVG, label string, primary bool) bool {
	t := c.Theme()
	b := ui.ButtonBase(c).Height(t.Space(8)).Gap(t.Space(1.5)).PaddingX(t.Space(3.5)).Radius(t.Radius)
	color := t.Text
	switch {
	case primary && b.Pressed():
		b.Background(t.AccentPressed)
	case primary && b.Hovered():
		b.Background(t.AccentHover)
	case primary:
		b.Background(t.Accent)
	case b.Hovered():
		b.Border(1, t.Border).Background(t.SurfaceHover)
	default:
		b.Border(1, t.Border).Background(t.Background)
	}
	if primary {
		color = t.AccentText
	}
	b.Children(func() {
		ui.Icon(c, icon).FontSize(px(t, 16)).TextColor(color)
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
			s.block(c, blocks, i, t.Text)
			i++
			continue
		}
		start := i
		for i < len(blocks) && blocks[i].quote > 0 {
			i++
		}
		run := blocks[start:i]
		ui.Column(c).Key(start).FillWidth().Margin(gap(t, blocks, start), 0, 0, 0).Padding(2, 0, 2, px(t, 14)).
			BorderWidth(0, 0, 0, 3).BorderColor(t.Border).Children(func() {
			for k := range run {
				s.block(c, run, k, t.TextMuted)
			}
		})
	}
}

// gap is the space above the block at i, as the margins of the mockup's prose collapse.
func gap(t *ui.Theme, blocks []block, i int) float32 {
	if i == 0 {
		return 0
	}
	b, before := blocks[i], blocks[i-1]
	switch {
	case b.kind == heading && b.level == 2:
		return px(t, 26)
	case b.kind == heading:
		return px(t, 22)
	case before.kind == heading && before.level == 2:
		return px(t, 10)
	case before.kind == heading:
		return px(t, 8)
	case b.kind == rule || before.kind == rule:
		return px(t, 24)
	case b.kind == caption && before.kind == picture:
		return px(t, 6)
	case b.kind == picture || before.kind == picture || before.kind == caption:
		return px(t, 20)
	case b.depth > 0 && before.depth > 0:
		return px(t, 4)
	}
	return px(t, 14)
}

func (s *Screen) block(c *ui.Context, blocks []block, i int, color ui.Color) {
	t := c.Theme()
	b := blocks[i]
	top := gap(t, blocks, i)
	indent := px(t, 14) * float32(max(0, b.quote-1))
	if b.depth > 0 {
		indent += px(t, 22) * float32(b.depth-1)
	}
	box := ui.Column(c).Key(i).FillWidth().Margin(top, 0, 0, indent)
	box.Children(func() {
		switch b.kind {
		case rule:
			ui.Box(c).FillWidth().Height(1).Background(t.Border)
		case picture:
			s.picture(c, b)
		case code:
			ui.ScrollHorizontal(c).FillWidth().Radius(t.Radius).Background(t.Surface).Children(func() {
				ui.Text(c, b.text).Font("monospace").FontSize(px(t, 13)).LineHeight(1.5).NoWrap().TextColor(color).
					Padding(px(t, 12), px(t, 14))
			})
		case heading:
			size := px(t, 18)
			if b.level == 3 {
				size = px(t, 15.5)
			}
			s.inline(c, b.spans, color).FontSize(size).FontWeight(600).LineHeight(s.leading(1.3, 1.4)).Role(ui.RoleHeading).
				Level(b.level)
		case caption:
			s.inline(c, b.spans, t.TextMuted).FontSize(px(t, 12))
		default:
			if b.depth == 0 {
				s.inline(c, b.spans, color).FontSize(px(t, 15)).LineHeight(s.leading(1.65, 1.85))
				return
			}
			ui.Row(c).FillWidth().AlignItems(ui.Start).Children(func() {
				ui.Text(c, b.marker).Width(px(t, 22)).Shrink(0).FontSize(px(t, 15)).LineHeight(s.leading(1.65, 1.85)).
					TextColor(t.TextMuted)
				s.inline(c, b.spans, color).Grow(1).MinWidth(0).FontSize(px(t, 15)).LineHeight(s.leading(1.65, 1.85))
			})
		}
	})
}

// leading is the line height of the article's text, the second when its title is Chinese.
func (s *Screen) leading(latin, han float32) float32 {
	if s.han {
		return han
	}
	return latin
}

// inline is a paragraph of spans, their links opening in the browser.
func (s *Screen) inline(c *ui.Context, spans []span, color ui.Color) ui.Element {
	t := c.Theme()
	return ui.RichText(c).TextColor(color).Wrap().Children(func() {
		for k, sp := range spans {
			var e ui.Element
			if sp.link != "" {
				e = ui.Link(c.Key(k), sp.text, sp.link).TextColor(t.Accent).Underline()
			} else {
				e = ui.Text(c, sp.text).Key(k)
			}
			if sp.bold {
				e.FontWeight(600)
			}
			if sp.italic {
				e.Italic()
			}
			if sp.mono {
				e.Font("monospace").FontSize(px(t, 13)).TextBackground(t.Surface)
			}
			if sp.gone {
				e.Strikethrough()
			}
		}
	})
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
	box := ui.Box(c).FillWidth().AspectRatio(ratio).Radius(t.Radius).Background(t.Surface).Clip().Role(ui.RoleImage).
		Label(b.alt)
	if width > 0 {
		box.MaxWidth(float32(width))
	}
	box.Children(func() {
		if bitmap != nil {
			ui.Image(c, bitmap).Fit(ui.Cover).Fill()
		}
	})
}
