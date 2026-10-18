// Package subscriptions adds, edits, pauses, refreshes and deletes subscriptions, in dialogs over the timeline; see
// design/screens/subscriptions.html. Everything goes through the HTTP API.
package subscriptions

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strconv"
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
	Add, AddShort, Adding, Cancel, Close, Save, Saving, Address, AddressPlaceholder, AddressHelp, ShowAs       string
	Article, Video, Social, Image, Title, TitlePlaceholder, Every, Minutes, Hours, Days, FoundFeed, FoundRoute string
	FoundYouTubeChannel, FoundYouTubeUser, FoundYouTubePlaylist, FoundBilibili, FoundX, FoundGitHubRepo        string
	FoundGitHubUser, FoundTelegram, FoundWeibo, ViaRSSHub, NeedsBase, RSSHubAddress, RSSHubPlaceholder         string
	BaseRequired, InvalidBase, InvalidAddress, Exists, ExistsUnknown, ShowIt, Unreachable, Failed, Added       string
	EditTitle, Refresh, Edit, Pause, Resume, Delete, ConfirmDelete, Checked, CheckedNow, NotChecked, NextIn    string
	NextSoon, FailedOnce, FailedTimes, Paused, PauseLabel, PauseHelp, Mute, MuteHelp, DeleteTitle, DeleteBody  string
	Deleted, PausedNow, Resumed, Queued, ResumeFirst, Gone, ActionFailed, Separator                            string
}

var translations = func() map[string]copy {
	var values map[string]copy
	if err := json.Unmarshal(catalog, &values); err != nil {
		panic(err)
	}
	return values
}()

// kinds are the kinds a subscription shows as, in the order of the picker.
var kinds = []string{"article", "video", "social", "image"}

// intervals are the fetch intervals offered, in seconds; the first is the default.
var intervals = []int64{15 * 60, 30 * 60, 3600, 3 * 3600, 6 * 3600, 12 * 3600, 24 * 3600}

// API is what the dialogs read and write on the server; client.Session is one.
type API interface {
	AddSource(ctx context.Context, src client.NewSource) (client.Source, error)
	UpdateSource(ctx context.Context, id int64, patch client.SourcePatch) (client.Source, error)
	DeleteSource(ctx context.Context, id int64) error
	RefreshSource(ctx context.Context, id int64) error
	Settings(ctx context.Context) (client.Settings, error)
	SetSettings(ctx context.Context, patch client.SettingsPatch) (client.Settings, error)
}

// Deps is what the dialogs do outside themselves. Go runs work away from the UI thread and then the function it
// returns on the UI thread; Now is the clock, time.Now by default. Sources are the subscriptions the timeline has,
// to name the one an address is already followed as. Added, Changed and Removed tell the timeline what the server
// took, Show chooses a subscription in it, and Expired shows the connect screen because the server no longer takes
// the session.
type Deps struct {
	Go      func(work func() (apply func()))
	Now     func() time.Time
	Sources func() []client.Source
	Added   func(client.Source)
	Changed func(client.Source)
	Removed func(id int64)
	Show    func(id int64)
	Expired func()
}

// The settings of the add dialog, which say whether a route needs an RSSHub instance first.
const (
	settingsLoading = iota
	settingsLoaded
	settingsUnknown // they did not load; the server checks the subscription all the same
)

type field int

const (
	noField field = iota
	addressField
	baseField
	titleField
)

// adding is the add dialog.
type adding struct {
	open       bool
	address    string
	base       string // the RSSHub instance typed when the settings have none
	title      string
	kind       int // in kinds
	suggested  int // the kind the address suggests, which kind follows until another is picked
	interval   int64
	settings   int
	root       string // the RSSHub instance of the settings
	addressErr string
	exists     int64 // the subscription the address is already followed as, for "Show it"
	baseErr    string
	formErr    string
	busy       bool
	focus      field
}

// editing is the edit dialog of src.
type editing struct {
	open     bool
	src      client.Source
	title    string
	kind     int
	interval int64
	paused   bool
	mute     string
	formErr  string
	busy     bool
	focus    bool
}

// Screen keeps the dialogs and the work they started.
type Screen struct {
	deps     Deps
	text     copy
	api      API
	gen      int // counts servers used, so the results of the one before are dropped
	add      adding
	edit     editing
	deleting bool
	doomed   client.Source // the subscription being deleted
	announce string
	toast    string
}

// New selects the best available translation, falling back to English.
func New(systemLocale string, deps Deps) *Screen {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Screen{deps: deps, text: translations[locale.Language(systemLocale)]}
}

// Use makes the dialogs work on api, and closes the ones of the server before.
func (s *Screen) Use(api API) {
	s.api = api
	s.gen++
	s.add, s.edit, s.deleting = adding{}, editing{}, false
}

// Open reports whether a dialog shows, which takes the keys from the timeline.
func (s *Screen) Open() bool { return s.add.open || s.edit.open || s.deleting }

// Name is what names a subscription: its title, which is empty until the first fetch, else the host of its address,
// else its route.
func Name(src client.Source) string {
	if title := strings.TrimSpace(src.Title); title != "" {
		return title
	}
	if u, err := url.Parse(src.URL); err == nil && u.Hostname() != "" {
		return strings.TrimPrefix(u.Hostname(), "www.")
	}
	if src.RSSHubPath != "" {
		return src.RSSHubPath
	}
	return src.URL
}

func (s *Screen) fill(text, name string) string { return strings.ReplaceAll(text, "{name}", name) }

func (s *Screen) count(text string, n int) string {
	return strings.ReplaceAll(text, "{n}", strconv.Itoa(n))
}

func (s *Screen) expired(err error) bool {
	if !errors.Is(err, client.ErrUnauthorized) {
		return false
	}
	s.gen++
	s.api = nil
	s.add, s.edit, s.deleting = adding{}, editing{}, false
	s.deps.Expired()
	return true
}

func code(err error) string {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

// problem says why a request failed: the server is not reached, or it failed.
func (s *Screen) problem(err error) string {
	if errors.Is(err, client.ErrUnreachable) {
		return s.text.Unreachable
	}
	return s.text.Failed
}

// Add opens the add dialog and loads the settings, for the RSSHub instance.
func (s *Screen) Add() {
	if s.api == nil {
		return
	}
	s.add = adding{open: true, interval: intervals[0], focus: addressField}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		settings, err := api.Settings(context.Background())
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			if err != nil {
				s.add.settings = settingsUnknown
				return
			}
			s.add.settings, s.add.root = settingsLoaded, settings.RSSHubBaseURL
		}
	})
}

// needsBase reports whether t is a route while the settings have no RSSHub instance.
func (s *Screen) needsBase(t target) bool {
	return t.Route != "" && s.add.settings == settingsLoaded && strings.TrimSpace(s.add.root) == ""
}

func (s *Screen) canAdd() bool {
	a := s.add
	return !a.busy && s.api != nil && strings.TrimSpace(a.address) != "" && a.addressErr == "" &&
		a.settings != settingsLoading
}

func (s *Screen) submit() {
	a := &s.add
	if !s.canAdd() {
		return
	}
	t, ok := recognize(a.address, a.root)
	if !ok {
		a.addressErr, a.focus, s.announce = s.text.InvalidAddress, addressField, s.text.InvalidAddress
		return
	}
	base := ""
	if s.needsBase(t) {
		if base = strings.TrimSpace(a.base); base == "" {
			a.baseErr, a.focus, s.announce = s.text.BaseRequired, baseField, s.text.BaseRequired
			return
		}
	}
	a.busy, a.baseErr, a.formErr = true, "", ""
	src := client.NewSource{URL: t.URL, RSSHubPath: t.Route, Kind: kinds[a.kind], Title: strings.TrimSpace(a.title),
		FetchIntervalSec: a.interval}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		ctx := context.Background()
		if base != "" {
			settings, err := api.SetSettings(ctx, client.SettingsPatch{RSSHubBaseURL: &base})
			if err != nil {
				return func() {
					if gen != s.gen || s.expired(err) {
						return
					}
					s.add.busy = false
					if code(err) == "invalid_rsshub_base_url" {
						s.add.baseErr, s.add.focus = s.text.InvalidBase, baseField
					} else {
						s.add.formErr = s.problem(err)
					}
					s.announce = s.add.baseErr + s.add.formErr
				}
			}
			base = settings.RSSHubBaseURL
		}
		added, err := api.AddSource(ctx, src)
		return func() {
			if gen != s.gen {
				return
			}
			a := &s.add
			a.busy = false
			if base != "" {
				a.root = base
			}
			if s.expired(err) {
				return
			}
			switch {
			case err == nil:
				a.open = false
				s.deps.Added(added)
				s.toast = s.fill(s.text.Added, Name(added))
			case code(err) == "source_exists":
				a.addressErr, a.exists = s.text.ExistsUnknown, 0
				for _, known := range s.deps.Sources() {
					if known.URL == src.URL && known.RSSHubPath == src.RSSHubPath {
						a.addressErr, a.exists = s.fill(s.text.Exists, Name(known)), known.ID
					}
				}
				a.focus, s.announce = addressField, a.addressErr
			case code(err) == "invalid_url" || code(err) == "invalid_rsshub_path" || code(err) == "invalid_source":
				a.addressErr, a.focus, s.announce = s.text.InvalidAddress, addressField, s.text.InvalidAddress
			default:
				a.formErr = s.problem(err)
				s.announce = a.formErr
			}
		}
	})
}

// Edit opens the edit dialog of src.
func (s *Screen) Edit(src client.Source) {
	if s.api == nil {
		return
	}
	s.edit = editing{open: true, src: src, title: src.Title, kind: max(0, slices.Index(kinds, src.Kind)),
		interval: src.FetchIntervalSec, paused: src.Paused, mute: src.MuteKeywords, focus: true}
	if s.edit.interval == 0 {
		s.edit.interval = intervals[0]
	}
}

func (s *Screen) save() {
	e := &s.edit
	if e.busy || s.api == nil {
		return
	}
	var patch client.SourcePatch
	changed := false
	if title := strings.TrimSpace(e.title); title != e.src.Title {
		patch.Title, changed = &title, true
	}
	if kind := kinds[e.kind]; kind != e.src.Kind {
		patch.Kind, changed = &kind, true
	}
	if interval := e.interval; interval != e.src.FetchIntervalSec {
		patch.FetchIntervalSec, changed = &interval, true
	}
	if paused := e.paused; paused != e.src.Paused {
		patch.Paused, changed = &paused, true
	}
	if mute := e.mute; mute != e.src.MuteKeywords {
		patch.MuteKeywords, changed = &mute, true
	}
	if !changed {
		e.open = false
		return
	}
	e.busy, e.formErr = true, ""
	gen, api, id := s.gen, s.api, e.src.ID
	s.deps.Go(func() func() {
		src, err := api.UpdateSource(context.Background(), id, patch)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			s.edit.busy = false
			switch {
			case err == nil:
				s.edit.open = false
				s.deps.Changed(src)
			case code(err) == "not_found":
				s.gone(id)
			default:
				s.edit.formErr = s.problem(err)
				s.announce = s.edit.formErr
			}
		}
	})
}

// gone drops a subscription the server no longer has.
func (s *Screen) gone(id int64) {
	if s.edit.src.ID == id {
		s.edit.open = false
	}
	s.toast = s.text.Gone
	s.deps.Removed(id)
}

// Refresh asks the server to fetch src now.
func (s *Screen) Refresh(src client.Source) {
	if s.api == nil {
		return
	}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		err := api.RefreshSource(context.Background(), src.ID)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			switch {
			case err == nil:
				s.toast = s.fill(s.text.Queued, Name(src))
			case code(err) == "source_paused":
				s.toast = s.fill(s.text.ResumeFirst, Name(src))
			case code(err) == "not_found":
				s.gone(src.ID)
			default:
				s.toast = s.problem(err)
			}
		}
	})
}

// SetPaused pauses src, or resumes it.
func (s *Screen) SetPaused(src client.Source, paused bool) {
	if s.api == nil {
		return
	}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		changed, err := api.UpdateSource(context.Background(), src.ID, client.SourcePatch{Paused: &paused})
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			switch {
			case err == nil:
				s.deps.Changed(changed)
				s.toast = s.fill(s.text.Resumed, Name(changed))
				if paused {
					s.toast = s.fill(s.text.PausedNow, Name(changed))
				}
			case code(err) == "not_found":
				s.gone(src.ID)
			default:
				s.toast = s.text.ActionFailed
			}
		}
	})
}

// AskDelete asks whether to delete src, with its entries.
func (s *Screen) AskDelete(src client.Source) {
	if s.api == nil {
		return
	}
	s.edit.open = false
	s.doomed, s.deleting = src, true
}

func (s *Screen) remove() {
	if s.api == nil {
		return
	}
	gen, api, src := s.gen, s.api, s.doomed
	s.deps.Go(func() func() {
		err := api.DeleteSource(context.Background(), src.ID)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			switch {
			case err == nil:
				s.toast = s.fill(s.text.Deleted, Name(src))
				s.deps.Removed(src.ID)
			case code(err) == "not_found":
				s.gone(src.ID)
			default:
				s.toast = s.problem(err)
			}
		}
	})
}

// Menu fills the context menu of src in the sidebar.
func (s *Screen) Menu(m *ui.Menu, src client.Source) {
	if m.Item(s.text.Refresh).Disabled(src.Paused).Chosen() {
		s.Refresh(src)
	}
	if m.Item(s.text.Edit).Chosen() {
		s.Edit(src)
	}
	pause := s.text.Pause
	if src.Paused {
		pause = s.text.Resume
	}
	if m.Item(pause).Chosen() {
		s.SetPaused(src, !src.Paused)
	}
	m.Separator()
	if m.Item(s.text.Delete).Chosen() {
		s.AskDelete(src)
	}
}

// View draws the dialog that is open, over the timeline; it does no disk or network work, which runs through Deps.
func (s *Screen) View(c *ui.Context) {
	if s.add.open {
		s.addDialog(c)
	}
	if s.edit.open {
		s.editDialog(c)
	}
	if s.deleting {
		name := Name(s.doomed)
		if ui.AlertDialog(c, &s.deleting, s.fill(s.text.DeleteTitle, name), s.text.DeleteBody, s.text.Cancel,
			s.text.ConfirmDelete) == 1 {
			s.remove()
		}
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

// px converts a size of the mockup, drawn at a 13 px font, to the theme's text size.
func px(t *ui.Theme, v float32) float32 { return t.Rem(v / 13) }

func kindColor(t *ui.Theme, kind string) ui.Color {
	switch kind {
	case "video":
		return t.Danger
	case "social":
		return t.Warning
	case "image":
		return t.Success
	}
	return t.Accent
}

func kindIcon(kind string) *ui.SVG {
	switch kind {
	case "video":
		return icons.Play
	case "social":
		return icons.MessageCircle
	case "image":
		return icons.Image
	}
	return icons.Newspaper
}

func (s *Screen) kindName(kind string) string {
	switch kind {
	case "video":
		return s.text.Video
	case "social":
		return s.text.Social
	case "image":
		return s.text.Image
	}
	return s.text.Article
}

// dialog draws a dialog of the mockup: a header, a form that scrolls when the window is too low, and a footer.
func (s *Screen) dialog(c *ui.Context, open *bool, width float32, title string, header, form, footer func()) {
	t := c.Theme()
	_, height := c.Size()
	ui.DialogBase(c, open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.28)).Padding(t.Space(4))
		panel.Column().Width(px(t, width)).MaxHeight(height-t.Space(8)).Radius(12).Background(t.Background).
			Border(1, t.Border).Shadow(0, 16, 40, 0, ui.RGBA(0, 0, 0, 0.25)).Label(title)
		header()
		ui.Scroll(c).FillWidth().Shrink(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Space(3.5)).Padding(t.Space(3.5), t.Space(5), t.Space(1), t.Space(5)).
				Children(form)
		})
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Padding(t.Space(4), t.Space(5), t.Space(4.5),
			t.Space(5)).Children(footer)
	})
}

func (s *Screen) header(c *ui.Context, open *bool, lead func(), title, sub string) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Padding(t.Space(4.5), t.Space(5), t.Space(1),
		t.Space(5)).Children(func() {
		lead()
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, title).FontSize(px(t, 15)).FontWeight(600).Role(ui.RoleHeading).Level(2)
			if sub != "" {
				ui.Text(c, sub).FontSize(px(t, 12)).TextColor(t.TextMuted).SingleLine()
			}
		})
		x := ui.ButtonBase(c).Size(px(t, 28), px(t, 28)).Radius(t.Space(1.5)).AlignSelf(ui.Start).
			Label(s.text.Close).Tooltip(s.text.Close)
		color := t.TextMuted
		if x.Hovered() {
			x.Background(t.SurfaceHover)
			color = t.Text
		}
		x.Children(func() { ui.Icon(c, icons.X).FontSize(px(t, 16)).TextColor(color) })
		if x.Clicked() {
			*open = false
		}
	})
}

func label(c *ui.Context, text string) {
	t := c.Theme()
	ui.Text(c, text).FontSize(px(t, 12)).FontWeight(500)
}

func help(c *ui.Context, text string) {
	t := c.Theme()
	ui.Text(c, text).FontSize(px(t, 12)).TextColor(t.TextMuted).Wrap()
}

// box is the frame of a text field: its leading icon, if any, and the input built by input.
func box(c *ui.Context, key string, lead *ui.SVG, invalid bool, fill ui.Color, input func()) {
	t := c.Theme()
	b := ui.Row(c).Key(key).FillWidth().Height(px(t, 32)).AlignItems(ui.Center).Gap(t.Space(2)).
		PaddingX(t.Space(2.5)).Radius(t.Radius).Border(1, t.Border).Background(fill)
	color := t.TextMuted
	switch focused := b.FocusWithin(); {
	case invalid:
		b.BorderColor(t.Danger)
		color = t.Danger
		if focused {
			b.Shadow(0, 0, 0, 3, theme.Soft(t, t.Danger))
		}
	case focused:
		b.BorderColor(t.Accent).Background(t.Background).Shadow(0, 0, 0, 3, t.Focus)
	}
	b.Children(func() {
		if lead != nil {
			ui.Icon(c, lead).FontSize(px(t, 16)).TextColor(color)
		}
		input()
	})
}

func errorLine(c *ui.Context, text string, link string) bool {
	t := c.Theme()
	clicked := false
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(t.Space(1.5)).Children(func() {
		ui.Icon(c, icons.CircleAlert).FontSize(px(t, 14)).TextColor(t.Danger).Margin(1, 0, 0, 0)
		ui.Text(c, text).FontSize(px(t, 12)).TextColor(t.Danger).Shrink(1).Wrap()
		if link != "" {
			clicked = ui.Link(c, link, "").FontSize(px(t, 12)).FontWeight(500).Clicked()
		}
	})
	return clicked
}

const (
	secondary = iota
	primary
	quiet // a destructive action that is not the default, in the danger color without a fill
	small // in the health line
)

func button(c *ui.Context, style int, disabled bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Height(px(t, 32)).Gap(t.Space(1.5)).PaddingX(px(t, 14)).Radius(t.Radius).Disabled(disabled)
	fill, border := t.Background, t.Border
	switch style {
	case primary:
		fill, border = t.Accent, t.Accent
	case quiet:
		fill, border = ui.Color{}, ui.Color{}
		b.PaddingX(t.Space(2))
	case small:
		b.Height(px(t, 26)).PaddingX(px(t, 10))
	}
	switch {
	case disabled:
		b.Opacity(0.5)
	case b.Pressed() && style == primary:
		fill, border = t.AccentPressed, t.AccentPressed
	case b.Hovered() && style == primary:
		fill, border = t.AccentHover, t.AccentHover
	case b.Pressed():
		fill = t.SurfacePressed
	case b.Hovered():
		fill = t.SurfaceHover
	}
	return b.Background(fill).Border(1, border)
}

// textButton fills a button with an optional icon and its text, a spinner for the icon while busy.
func textButton(c *ui.Context, b ui.Element, style int, icon *ui.SVG, text string, busy bool) {
	t := c.Theme()
	color := t.Text
	switch style {
	case primary:
		color = t.AccentText
	case quiet:
		color = t.Danger
	}
	size := px(t, 14)
	b.Children(func() {
		if busy {
			spin := ui.Icon(c, icons.LoaderCircle).FontSize(size).TextColor(color)
			spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
		} else if icon != nil {
			ui.Icon(c, icon).FontSize(size).TextColor(color)
		}
		text := ui.Text(c, text).NoWrap().TextColor(color)
		if style == small {
			text.FontSize(px(t, 12))
		}
		if style == primary || style == quiet {
			text.FontWeight(500)
		}
	})
}

func tile(c *ui.Context, icon *ui.SVG) {
	t := c.Theme()
	ui.Box(c).Size(px(t, 32), px(t, 32)).Radius(8).Center().Background(theme.Soft(t, t.Accent)).Children(func() {
		ui.Icon(c, icon).FontSize(px(t, 18)).TextColor(t.Accent)
	})
}

func monogram(c *ui.Context, kind, name string) {
	t := c.Theme()
	letter := "·"
	if r, _ := utf8.DecodeRuneInString(strings.TrimLeft(name, "@#·/ ")); r != utf8.RuneError {
		letter = strings.ToUpper(string(r))
	}
	color := kindColor(t, kind)
	ui.Box(c).Size(px(t, 32), px(t, 32)).Radius(8).Center().Background(theme.Soft(t, color)).Children(func() {
		ui.Text(c, letter).FontSize(px(t, 15)).FontWeight(600).TextColor(color)
	})
}

func (s *Screen) addDialog(c *ui.Context) {
	a := &s.add
	t, valid := recognize(a.address, a.root)
	if suggested := max(0, slices.Index(kinds, t.Kind)); a.kind == a.suggested {
		a.kind, a.suggested = suggested, suggested
	}
	s.dialog(c, &a.open, 440, s.text.Add, func() {
		s.header(c, &a.open, func() { tile(c, icons.Rss) }, s.text.Add, "")
	}, func() {
		s.addressField(c, t, valid)
		s.kindPicker(c, &a.kind)
		s.pair(c, func() {
			s.titleField(c, &a.title, a.busy, a.focus == titleField, s.submit)
		}, func() {
			s.intervalSelect(c, &a.interval, a.busy)
		})
		if a.formErr != "" {
			errorLine(c, a.formErr, "")
		}
	}, func() {
		ui.Box(c).Grow(1)
		cancel := button(c, secondary, false)
		textButton(c, cancel, secondary, nil, s.text.Cancel, false)
		if cancel.Clicked() {
			a.open = false
		}
		add := button(c, primary, !s.canAdd() && !a.busy)
		text := s.text.AddShort
		if a.busy {
			text = s.text.Adding
		}
		textButton(c, add, primary, nil, text, a.busy)
		if add.Clicked() {
			s.submit()
		}
	})
}

func (s *Screen) addressField(c *ui.Context, t target, valid bool) {
	th := c.Theme()
	a := &s.add
	ui.Column(c).FillWidth().Gap(th.Space(1)).Children(func() {
		label(c, s.text.Address)
		before := a.address
		empty := strings.TrimSpace(a.address) == ""
		needsBase := valid && !empty && s.needsBase(t)
		what, where := s.describe(t)
		box(c, "address", icons.Link, a.addressErr != "", th.Surface, func() {
			in := ui.TextInputBase(c, &a.address).Grow(1).MinWidth(0).FocusRing(false).Label(s.text.Address).
				Placeholder(s.text.AddressPlaceholder).Disabled(a.busy)
			switch {
			case a.addressErr != "":
				in.Error(a.addressErr)
			case empty || !valid:
				in.Description(s.text.AddressHelp)
			case needsBase:
				in.Description(s.text.NeedsBase)
			default:
				in.Description(what + s.text.Separator + where)
			}
			if a.focus == addressField {
				in.Focus()
				a.focus = noField
			}
			if in.Submitted() {
				s.submit()
			}
		})
		if a.address != before {
			a.addressErr, a.exists, a.formErr = "", 0, ""
		}
		switch {
		case a.addressErr != "":
			show := ""
			if a.exists != 0 {
				show = s.text.ShowIt
			}
			if errorLine(c, a.addressErr, show) {
				a.open = false
				s.deps.Show(a.exists)
			}
		case empty || !valid:
			help(c, s.text.AddressHelp)
		case needsBase:
			s.baseCard(c)
		default:
			s.found(c, t, what, where)
		}
	})
}

// describe says what t subscribes to, and the address or route it is.
func (s *Screen) describe(t target) (what, where string) {
	text := map[platform]string{
		feed: s.text.FoundFeed, route: s.text.FoundRoute, youtubeChannel: s.text.FoundYouTubeChannel,
		youtubeUser: s.text.FoundYouTubeUser, youtubePlaylist: s.text.FoundYouTubePlaylist,
		bilibili: s.text.FoundBilibili, xUser: s.text.FoundX, githubRepo: s.text.FoundGitHubRepo,
		githubUser: s.text.FoundGitHubUser, telegram: s.text.FoundTelegram, weibo: s.text.FoundWeibo,
	}[t.Platform]
	what = s.fill(text, t.Name)
	switch {
	case t.URL != "":
		where = t.URL
	case t.Platform == route:
		where = t.Route
	default:
		where = s.text.ViaRSSHub + " " + t.Route
	}
	return what, where
}

func (s *Screen) found(c *ui.Context, t target, what, where string) {
	th := c.Theme()
	icon := icons.Rss
	if t.Platform != feed {
		icon = kindIcon(kinds[s.add.kind])
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(px(th, 10)).Padding(th.Space(2), px(th, 10)).Radius(th.Radius).
		Background(theme.Soft(th, th.Accent)).Role(ui.RoleStatus).Label(what + s.text.Separator + where).
		Children(func() {
			ui.Icon(c, icon).FontSize(px(th, 16)).TextColor(th.Accent)
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, what).FontWeight(600).SingleLine()
				ui.Text(c, where).Font("monospace").FontSize(px(th, 12)).TextColor(th.TextMuted).SingleLine()
			})
			ui.Icon(c, icons.Check).FontSize(px(th, 16)).TextColor(th.Accent)
		})
}

// baseCard asks for the RSSHub instance a route needs, which the settings do not have yet.
func (s *Screen) baseCard(c *ui.Context) {
	t := c.Theme()
	a := &s.add
	ui.Column(c).FillWidth().Gap(t.Space(2)).Padding(px(t, 10)).Radius(t.Radius).
		Background(theme.Soft(t, t.Warning)).Role(ui.RoleStatus).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(t.Space(1.5)).Children(func() {
			ui.Icon(c, icons.TriangleAlert).FontSize(px(t, 14)).TextColor(t.Warning).Margin(2, 0, 0, 0)
			ui.Text(c, s.text.NeedsBase).FontWeight(500).TextColor(t.Warning).Shrink(1).Wrap()
		})
		before := a.base
		box(c, "base", icons.Link, a.baseErr != "", t.Background, func() {
			in := ui.TextInputBase(c, &a.base).Grow(1).MinWidth(0).FocusRing(false).Label(s.text.RSSHubAddress).
				Placeholder(s.text.RSSHubPlaceholder).Disabled(a.busy)
			if a.baseErr != "" {
				in.Error(a.baseErr)
			}
			if a.focus == baseField {
				in.Focus()
				a.focus = noField
			}
			if in.Submitted() {
				s.submit()
			}
		})
		if a.base != before {
			a.baseErr = ""
		}
		if a.baseErr != "" {
			errorLine(c, a.baseErr, "")
		}
	})
}

func (s *Screen) kindPicker(c *ui.Context, value *int) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
		label(c, s.text.ShowAs)
		seg := ui.SegmentedBase(c, value, len(kinds))
		seg.Track.FillWidth().Padding(2).Gap(2).Radius(t.Radius).Background(t.Surface).Label(s.text.ShowAs).
			Children(func() {
				for i, kind := range kinds {
					name := s.kindName(kind)
					b := seg.Segment(i).Grow(1).Basis(0).Height(px(t, 30)).Gap(t.Space(1.5)).Radius(t.Space(1.5)).
						Label(name).Tooltip(name)
					color, iconColor := t.TextMuted, t.TextMuted
					switch {
					case i == *value:
						b.Background(t.Background).Shadow(0, 0, 0, 1, t.Border)
						color, iconColor = t.Text, t.Accent
					case b.Hovered():
						color, iconColor = t.Text, t.Text
					}
					b.Children(func() {
						ui.Icon(c, kindIcon(kind)).FontSize(px(t, 14)).TextColor(iconColor)
						ui.Text(c, name).NoWrap().TextColor(color)
					})
				}
			})
	})
}

func (s *Screen) pair(c *ui.Context, left, right func()) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(t.Space(3)).Children(func() {
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(t.Space(1)).Children(left)
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(t.Space(1)).Children(right)
	})
}

func (s *Screen) titleField(c *ui.Context, value *string, busy, focus bool, submit func()) {
	t := c.Theme()
	label(c, s.text.Title)
	box(c, "title", nil, false, t.Surface, func() {
		in := ui.TextInputBase(c, value).Grow(1).MinWidth(0).FocusRing(false).Label(s.text.Title).
			Placeholder(s.text.TitlePlaceholder).Disabled(busy)
		if focus {
			in.Focus()
		}
		if in.Submitted() {
			submit()
		}
	})
}

// every names a fetch interval in seconds.
func (s *Screen) every(sec int64) string {
	switch {
	case sec%(24*3600) == 0:
		return s.count(s.text.Days, int(sec/(24*3600)))
	case sec%3600 == 0:
		return s.count(s.text.Hours, int(sec/3600))
	}
	return s.count(s.text.Minutes, int((sec+30)/60))
}

// span names a length of time, at least a minute, in its largest whole unit.
func (s *Screen) span(d time.Duration) string {
	switch {
	case d < time.Hour:
		return s.count(s.text.Minutes, max(1, int(d/time.Minute)))
	case d < 24*time.Hour:
		return s.count(s.text.Hours, int(d/time.Hour))
	}
	return s.count(s.text.Days, int(d/(24*time.Hour)))
}

func (s *Screen) intervalSelect(c *ui.Context, value *int64, busy bool) {
	t := c.Theme()
	label(c, s.text.Every)
	options := intervals
	if !slices.Contains(options, *value) {
		options = append(slices.Clone(options), *value)
		slices.Sort(options)
	}
	sel := ui.SelectBase(c, value)
	sel.Trigger.FillWidth().Height(px(t, 32)).Gap(t.Space(2)).PaddingX(t.Space(2.5)).Radius(t.Radius).
		Border(1, t.Border).Background(t.Surface).Label(s.text.Every).Disabled(busy)
	if sel.Trigger.Focused() {
		sel.Trigger.BorderColor(t.Accent).Background(t.Background).Shadow(0, 0, 0, 3, t.Focus)
	} else if sel.Trigger.Hovered() {
		sel.Trigger.Background(t.SurfaceHover)
	}
	sel.Trigger.Children(func() {
		ui.Text(c, s.every(*value)).Grow(1).MinWidth(0).SingleLine()
		ui.Icon(c, icons.ChevronDown).FontSize(px(t, 14)).TextColor(t.TextMuted)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Margin(4, 0, 0, 0).Padding(4).MinWidth(px(t, 160)).Radius(t.Radius).Background(t.Background).
			Border(1, t.Border).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.18))
		for _, v := range options {
			item := sel.Item(v).FillWidth().Height(px(t, 28)).Gap(t.Space(2)).PaddingX(t.Space(2)).Radius(5)
			color := t.Text
			if item.Highlighted() {
				item.Background(t.Accent)
				color = t.AccentText
			}
			item.Children(func() {
				ui.Text(c, s.every(v)).Grow(1).NoWrap().TextColor(color)
				if v == *value {
					ui.Icon(c, icons.Check).FontSize(px(t, 14)).TextColor(color)
				}
			})
		}
	})
}

func (s *Screen) editDialog(c *ui.Context) {
	e := &s.edit
	where := e.src.URL
	if where == "" {
		where = e.src.RSSHubPath
	}
	s.dialog(c, &e.open, 480, s.text.EditTitle, func() {
		s.header(c, &e.open, func() { monogram(c, kinds[e.kind], Name(e.src)) }, s.text.EditTitle, where)
	}, func() {
		s.health(c)
		focus := e.focus
		e.focus = false
		s.pair(c, func() {
			s.titleField(c, &e.title, e.busy, focus, s.save)
		}, func() {
			s.intervalSelect(c, &e.interval, e.busy)
		})
		s.kindPicker(c, &e.kind)
		s.pauseSwitch(c)
		s.muteField(c)
		if e.formErr != "" {
			errorLine(c, e.formErr, "")
		}
	}, func() {
		remove := button(c, quiet, e.busy)
		textButton(c, remove, quiet, icons.Trash2, s.text.Delete, false)
		if remove.Clicked() {
			s.AskDelete(e.src)
		}
		ui.Box(c).Grow(1)
		cancel := button(c, secondary, false)
		textButton(c, cancel, secondary, nil, s.text.Cancel, false)
		if cancel.Clicked() {
			e.open = false
		}
		save := button(c, primary, e.busy)
		text := s.text.Save
		if e.busy {
			text = s.text.Saving
		}
		textButton(c, save, primary, nil, text, e.busy)
		if save.Clicked() {
			s.save()
		}
	})
}

// healthText says how the fetching of src goes, and whether it is failing.
func (s *Screen) healthText(src client.Source, now time.Time) (string, bool) {
	switch {
	case src.Paused:
		return s.text.Paused, false
	case src.LastError != "":
		if src.ConsecutiveFailures > 1 {
			return strings.ReplaceAll(s.count(s.text.FailedTimes, src.ConsecutiveFailures), "{error}", src.LastError),
				true
		}
		return strings.ReplaceAll(s.text.FailedOnce, "{error}", src.LastError), true
	case src.LastFetchedAt == 0:
		return s.text.NotChecked, false
	}
	checked := s.text.CheckedNow
	if ago := now.Sub(time.UnixMilli(src.LastFetchedAt)); ago >= time.Minute {
		checked = strings.ReplaceAll(s.text.Checked, "{t}", s.span(ago))
	}
	next := s.text.NextSoon
	if in := time.UnixMilli(src.NextFetchAt).Sub(now); in >= time.Minute {
		next = strings.ReplaceAll(s.text.NextIn, "{t}", s.span(in))
	}
	return checked + s.text.Separator + next, false
}

func (s *Screen) health(c *ui.Context) {
	t := c.Theme()
	src := s.edit.src
	text, bad := s.healthText(src, s.deps.Now())
	icon, color, fill := icons.Check, t.TextMuted, t.Surface
	switch {
	case src.Paused:
		icon = icons.Pause
	case bad:
		icon, color, fill = icons.CircleAlert, t.Warning, theme.Soft(t, t.Warning)
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Padding(t.Space(2), px(t, 10)).Radius(t.Radius).
		Background(fill).Children(func() {
		ui.Icon(c, icon).FontSize(px(t, 14)).TextColor(color)
		ui.Text(c, text).Grow(1).Shrink(1).MinWidth(0).FontSize(px(t, 12)).TextColor(color).Wrap().
			Role(ui.RoleStatus)
		if !src.Paused {
			refresh := button(c, small, false)
			textButton(c, refresh, small, icons.RefreshCw, s.text.Refresh, false)
			if refresh.Clicked() {
				s.Refresh(src)
			}
		}
	})
}

func (s *Screen) pauseSwitch(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(px(t, 10)).Children(func() {
		text := ui.Column(c).Key("pause").Grow(1).MinWidth(0).Children(func() {
			label(c, s.text.PauseLabel)
			help(c, s.text.PauseHelp)
		})
		if text.Clicked() && !s.edit.busy {
			s.edit.paused = !s.edit.paused
		}
		ui.Switch(c, &s.edit.paused).Label(s.text.PauseLabel).Description(s.text.PauseHelp).Disabled(s.edit.busy)
	})
}

func (s *Screen) muteField(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
		label(c, s.text.Mute)
		ui.TextArea(c, &s.edit.mute).FillWidth().Lines(3, 6).Label(s.text.Mute).Description(s.text.MuteHelp).
			Disabled(s.edit.busy)
		help(c, s.text.MuteHelp)
	})
}
