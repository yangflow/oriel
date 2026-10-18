// Package reader is the reader: an article in its full text, drawn natively in the place of the timeline's list,
// or what the feed carries while the text is being got or when it could not be; see design/screens/reader.html.
// Everything it shows comes through the HTTP API.
package reader

import (
	"context"
	_ "embed"
	"encoding/json"
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
	maxDownloads = 4
)

// API is what the reader reads on the server; client.Session is one.
type API interface {
	Entry(ctx context.Context, id int64) (client.Article, error)
	Image(ctx context.Context, address string) ([]byte, error)
}

// Deps is what the reader does outside itself. Go runs work away from the UI thread and then the function it returns
// on the UI thread. Now is the clock and Decode turns the bytes of a picture into a bitmap, off the UI thread; they
// default to time.Now and ui.DecodeBitmap. Expired reports whether err means the server no longer takes the session,
// and then shows the connect screen. Back returns to the list, Move goes to the article by entries away, and Star and
// ToggleRead change the entry read.
type Deps struct {
	Go         func(work func() (apply func()))
	Now        func() time.Time
	Decode     func([]byte) (*ui.Bitmap, error)
	Expired    func(err error) bool
	Back       func()
	Move       func(by int)
	Star       func()
	ToggleRead func()
}

// Nav is what the timeline tells the reader each frame: the entry as the list has it, with its read and starred
// state, the scope Back returns to, whether there are articles before and after it, and whether the keys apply,
// which they do not while a dialog shows.
type Nav struct {
	Entry          client.Entry
	Scope          string
	Previous, Next bool
	Keys           bool
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
	gen  int // counts articles opened, so the results for the one before are dropped

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

	pictures  map[string]*loaded
	wanted    []string
	downloads int

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
	s.load(false)
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
	s.poll(c)
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
	ui.Column(c).Grow(1).MinWidth(0).Children(func() {
		s.toolbar(c, nav, ready)
		ui.Box(c).FillWidth().Height(1).Background(t.Border)
		switch {
		case s.failed:
			s.empty(c, icons.WifiOff, t.Danger, s.text.Error, s.text.ErrorHint, func() {
				if s.button(c, icons.RefreshCw, s.text.Retry, false) {
					s.loading, s.failed = true, false
					s.load(false)
				}
			}).Grow(1).FillWidth()
		default:
			s.page(c, narrow)
		}
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
	ui.Row(c).FillWidth().Height(px(t, 48)).AlignItems(ui.Center).Gap(2).Padding(0, t.Space(3), 0, t.Space(2)).
		Children(func() {
			back := strings.ReplaceAll(s.text.Back, "{scope}", nav.Scope)
			b := ui.ButtonBase(c).Height(px(t, 28)).MinWidth(0).Shrink(1).Gap(2).Padding(0, t.Space(2), 0, t.Space(1)).
				Radius(t.Space(1.5)).Label(back).Tooltip(s.shortcut(back, "Esc"))
			color := t.TextMuted
			if b.Hovered() {
				b.Background(t.SurfaceHover)
				color = t.Text
			}
			b.Children(func() {
				ui.Icon(c, icons.ChevronLeft).FontSize(px(t, 16)).TextColor(color)
				ui.Text(c, nav.Scope).SingleLine().MinWidth(0).Shrink(1).TextColor(color)
			})
			if b.Clicked() {
				s.deps.Back()
			}
			ui.Box(c).Grow(1)
			if s.tool(c, icons.ChevronUp, s.text.Previous, "K", t.TextMuted, !nav.Previous).Clicked() {
				s.deps.Move(-1)
			}
			if s.tool(c, icons.ChevronDown, s.text.Next, "J", t.TextMuted, !nav.Next).Clicked() {
				s.deps.Move(1)
			}
			ui.Box(c).Size(1, px(t, 16)).Margin(0, t.Space(1.5)).Background(t.Border)
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
}

// tool is a button of the toolbar: an icon, named by label, its key in the tooltip.
func (s *Screen) tool(c *ui.Context, icon *ui.SVG, label, key string, color ui.Color, disabled bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Size(px(t, 28), px(t, 28)).Radius(t.Space(1.5)).Label(label).
		Tooltip(s.shortcut(label, key)).Disabled(disabled)
	switch {
	case disabled:
		color = color.Alpha(0.45)
	case b.Hovered():
		b.Background(t.SurfaceHover)
		if color == t.TextMuted {
			color = t.Text
		}
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(px(t, 16)).TextColor(color) })
	return b
}

// page is the scrolling article: its source and time, its title, a note on where its text comes from, and the text.
func (s *Screen) page(c *ui.Context, narrow bool) {
	t := c.Theme()
	e := s.article.Entry
	pad := []float32{px(t, 28), px(t, 32), px(t, 48), px(t, 32)}
	if narrow {
		pad = []float32{px(t, 20), px(t, 20), px(t, 40), px(t, 20)}
	}
	title := strings.TrimSpace(e.Title)
	area := ui.Scroll(c).TrackScroll(&s.scroll).Grow(1).FillWidth().Focusable().FocusRing(false).Label(title)
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
		ui.Column(c).FillWidth().MaxWidth(px(t, 680)).Margin(0, ui.Auto).Padding(pad...).Selectable().
			Children(func() {
				ui.Row(c).MinWidth(0).AlignItems(ui.Center).Gap(t.Space(1.5)).Children(func() {
					monogram(c, e.SourceTitle)
					ui.Text(c, e.SourceTitle).FontSize(px(t, 12)).FontWeight(500).SingleLine().MinWidth(0).Shrink(1)
					ui.Text(c, "·").FontSize(px(t, 12)).TextColor(t.TextMuted)
					ui.Text(c, s.when(e)).FontSize(px(t, 12)).TextColor(t.TextMuted).NoWrap()
				})
				size := px(t, 24)
				if narrow {
					size = px(t, 20)
				}
				ui.Text(c, title).FontSize(size).FontWeight(650).LineHeight(1.25).Wrap().Margin(px(t, 10), 0, 0, 0).
					Role(ui.RoleHeading).Level(1)
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
			s.inline(c, b.spans, color).FontSize(size).FontWeight(600).LineHeight(1.3).Role(ui.RoleHeading).
				Level(b.level)
		case caption:
			s.inline(c, b.spans, t.TextMuted).FontSize(px(t, 12))
		default:
			if b.depth == 0 {
				s.inline(c, b.spans, color).FontSize(px(t, 15)).LineHeight(1.65)
				return
			}
			ui.Row(c).FillWidth().AlignItems(ui.Start).Children(func() {
				ui.Text(c, b.marker).Width(px(t, 22)).Shrink(0).FontSize(px(t, 15)).LineHeight(1.65).
					TextColor(t.TextMuted)
				s.inline(c, b.spans, color).Grow(1).MinWidth(0).FontSize(px(t, 15)).LineHeight(1.65)
			})
		}
	})
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
