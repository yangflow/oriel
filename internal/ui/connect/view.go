// Package connect is the screen that connects the desktop app to an Oriel server and signs in; see
// design/screens/connect.html. The password lives only in the screen while it is typed and is sent once, to sign in.
package connect

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/theme"
)

//go:embed copy.json
var catalog []byte

type copy struct {
	Title, Address, Password, PasswordLabel, Show, Hide, Reachable, UseLocal, Connect, Connecting               string
	Unreachable, NotOriel, PasswordRequired, WrongPassword, TooManyAttempts, ServerError, Failed, Expired, Menu string
}

var translations = func() map[string]copy {
	var values map[string]copy
	if err := json.Unmarshal(catalog, &values); err != nil {
		panic(err)
	}
	return values
}()

// MenuLabel is the menu item that opens the screen, in the language of the system locale.
func MenuLabel(systemLocale string) string { return translations[locale.Language(systemLocale)].Menu }

// Deps is what the screen does outside itself. Go runs work away from the UI thread and then the function it
// returns on the UI thread; Connected and UseLocal run on the UI thread.
type Deps struct {
	Probe     func(ctx context.Context, address string) (client.Server, error)
	Login     func(ctx context.Context, server client.Server, password string) (client.Session, error)
	Go        func(work func() (apply func()))
	Connected func(client.Session)
	UseLocal  func()
}

type phase int

const (
	idle phase = iota
	probing
	signingIn
)

type field int

const (
	none field = iota
	addressField
	passwordField
	formField
)

// problem is what went wrong, shown under the field it is about.
type problem struct {
	field   field
	icon    *ui.SVG
	message string
}

// Screen keeps the form and the state of a connection under way.
type Screen struct {
	deps Deps
	text copy

	address, password string
	shown             bool
	phase             phase
	run               int // counts connections, so a result the user moved on from is dropped
	problem           problem
	expired           string // the server whose session expired, as shown
	focus             field  // the field to give the focus in the next frame
	announce          string
}

// New selects the best available translation, falling back to English.
func New(systemLocale string, deps Deps) *Screen {
	return &Screen{deps: deps, text: translations[locale.Language(systemLocale)], focus: addressField}
}

// SetLanguage shows the screen in the language of systemLocale, keeping what it shows.
func (s *Screen) SetLanguage(systemLocale string) {
	s.text = translations[locale.Language(systemLocale)]
}

// Open shows the form again for server, empty for none, with why the saved session of it can no longer be used:
// client.ErrUnauthorized when it expired, or the error of checking it. The password is always empty.
func (s *Screen) Open(server string, err error) {
	s.run++
	s.address, s.password, s.shown, s.phase, s.problem, s.expired = server, "", false, idle, problem{}, ""
	s.focus = addressField
	if errors.Is(err, client.ErrUnauthorized) {
		s.expired, s.focus = host(server), passwordField
	} else if err != nil {
		s.fail(err)
	}
}

func host(server string) string {
	if u, err := url.Parse(server); err == nil && u.Host != "" {
		return u.Host
	}
	return server
}

func (s *Screen) fail(err error) {
	t := s.text
	switch {
	case errors.Is(err, client.ErrUnreachable):
		s.problem = problem{addressField, icons.WifiOff, t.Unreachable}
	case errors.Is(err, client.ErrNotOriel):
		s.problem = problem{addressField, icons.Server, t.NotOriel}
	case errors.Is(err, client.ErrPasswordRequired):
		s.problem = problem{passwordField, icons.LockKeyhole, t.PasswordRequired}
	case errors.Is(err, client.ErrWrongPassword):
		s.problem = problem{passwordField, icons.CircleAlert, t.WrongPassword}
	case errors.Is(err, client.ErrTooManyAttempts):
		s.problem = problem{passwordField, icons.Clock, t.TooManyAttempts}
	case errors.Is(err, client.ErrUnwritable):
		s.problem = problem{formField, icons.Database, t.ServerError}
	default:
		s.problem = problem{formField, icons.CircleAlert, t.Failed}
	}
	s.focus = s.problem.field
	if s.focus == formField {
		s.focus = addressField
	}
	s.announce = s.problem.message
}

func (s *Screen) connect() {
	if s.phase != idle {
		return
	}
	if strings.TrimSpace(s.address) == "" {
		s.focus = addressField
		return
	}
	s.run++
	run, address, password := s.run, s.address, s.password
	s.phase, s.problem, s.focus = probing, problem{}, none
	s.deps.Go(func() func() {
		server, err := s.deps.Probe(context.Background(), address)
		if err != nil {
			return func() { s.finish(run, client.Session{}, err) }
		}
		s.deps.Go(func() func() {
			session, err := s.deps.Login(context.Background(), server, password)
			return func() { s.finish(run, session, err) }
		})
		return func() {
			if s.run == run && s.phase == probing {
				s.phase = signingIn
			}
		}
	})
}

func (s *Screen) finish(run int, session client.Session, err error) {
	if run != s.run {
		return
	}
	s.phase = idle
	if err != nil {
		s.fail(err)
		return
	}
	s.password, s.shown, s.expired = "", false, ""
	s.deps.Connected(session)
}

// View draws the form; it does no disk or network work, which runs through Deps.
func (s *Screen) View(c *ui.Context) {
	busy := s.phase != idle
	if s.announce != "" {
		c.Announce(s.announce)
		s.announce = ""
	}
	_, height := c.Size()
	ui.Box(c).Fill().Children(func() {
		s.form(c, height, busy)
		// No title bar: the top row drags the window and leaves the window buttons room.
		ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(theme.Bar).DragWindow()
	})
}

func (s *Screen) form(c *ui.Context, height float32, busy bool) {
	t := c.Theme()
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().MinHeight(height).Center().Padding(t.Space(6)).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(t.Space(80)).Gap(t.Space(6)).Children(func() {
				s.header(c)
				ui.Column(c).FillWidth().Gap(t.Space(2)).Children(func() {
					ui.Column(c).FillWidth().Gap(t.Space(1.5)).Children(func() { s.addressField(c, busy) })
					ui.Column(c).FillWidth().Gap(t.Space(1.5)).Children(func() { s.passwordField(c, busy) })
					s.errorLine(c, formField)
				})
				s.actions(c, busy)
			})
		})
	})
}

func (s *Screen) header(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
		ui.Box(c).Size(t.Space(12), t.Space(12)).Radius(t.Space(3)).Center().Background(theme.Soft(t, t.Accent)).
			Children(func() { ui.Icon(c, icons.Server).FontSize(t.Space(6)).TextColor(t.Accent) })
		ui.Text(c, s.text.Title).FontSize(t.Rem(1.3)).FontWeight(600).Role(ui.RoleHeading).Level(1)
		if s.expired != "" {
			before, after, _ := strings.Cut(s.text.Expired, "{server}")
			ui.Row(c).AlignItems(ui.Center).Gap(t.Space(1.5)).Children(func() {
				ui.Icon(c, icons.Clock).FontSize(t.Rem(1.05)).TextColor(t.TextMuted)
				ui.RichText(c,
					ui.Span{Text: before, Color: t.TextMuted},
					ui.Span{Text: s.expired, Weight: 500},
					ui.Span{Text: after, Color: t.TextMuted},
				).FontSize(t.Rem(0.92))
			})
		}
	})
}

// box is the frame of a text field: its leading icon, the input built by input, and what trails it.
func (s *Screen) box(c *ui.Context, key string, lead *ui.SVG, busy, ok bool, input func(), trail func()) {
	t := c.Theme()
	invalid := s.problem.field == fieldOf(key)
	box := ui.Row(c).Key(key).FillWidth().Height(t.Space(8)).AlignItems(ui.Center).Gap(t.Space(2)).
		Padding(0, t.Space(1), 0, t.Space(2.5)).Radius(t.Radius).Background(t.Surface)
	if c.Preferences().HighContrast {
		box.Border(1, t.Border)
	} else {
		box.Border(0.5, theme.LineStrong(t))
	}
	focused := box.FocusWithin() && !busy
	color := t.TextMuted
	switch {
	case invalid:
		box.Border(1.5, t.Danger)
		color = t.Danger
		if focused {
			box.Shadow(0, 0, 0, 2, theme.Soft(t, t.Danger))
		}
	case focused:
		box.Border(0, ui.Color{}).Background(theme.RaisedFill(t)).Shadow(0, 0, 0, 2, t.Focus)
	case ok:
		color = t.Success
	}
	box.Children(func() {
		ui.Icon(c, lead).FontSize(t.Rem(1.25)).TextColor(color)
		input()
		trail()
	})
}

func fieldOf(key string) field {
	if key == "address" {
		return addressField
	}
	return passwordField
}

func (s *Screen) input(c *ui.Context, value *string, f field, label, placeholder string, busy bool) ui.Element {
	t := c.Theme()
	in := ui.TextInputBase(c, value).Grow(1).MinWidth(0).FocusRing(false).Label(label).Placeholder(placeholder).
		Disabled(busy)
	if busy {
		in.TextColor(t.TextMuted)
	}
	if s.problem.field == f {
		in.Error(s.problem.message)
	}
	if s.focus == f {
		in.Focus()
		s.focus = none
	}
	if in.Submitted() {
		s.connect()
	}
	return in
}

func (s *Screen) addressField(c *ui.Context, busy bool) {
	t := c.Theme()
	before := s.address
	s.box(c, "address", icons.Globe, busy, s.phase == signingIn, func() {
		s.input(c, &s.address, addressField, s.text.Address, s.text.Address, busy)
	}, func() {
		if s.phase == signingIn {
			ui.Box(c).Size(t.Space(6), t.Space(6)).Center().Label(s.text.Reachable).Role(ui.RoleImage).Children(func() {
				ui.Icon(c, icons.Check).FontSize(t.Rem(1.25)).TextColor(t.Success)
			})
		}
	})
	if s.address != before && s.problem.field == addressField {
		s.problem = problem{}
	}
	s.errorLine(c, addressField)
}

func (s *Screen) passwordField(c *ui.Context, busy bool) {
	t := c.Theme()
	before := s.password
	s.box(c, "password", icons.KeyRound, busy, false, func() {
		in := s.input(c, &s.password, passwordField, s.text.PasswordLabel, s.text.Password, busy)
		if !s.shown {
			in.Password()
		}
	}, func() {
		label, icon := s.text.Show, icons.Eye
		if s.shown {
			label, icon = s.text.Hide, icons.EyeOff
		}
		eye := theme.Fade(ui.ButtonBase(c).Size(t.Space(6), t.Space(6)).Radius(t.Space(1.5)).Label(label).
			Tooltip(label).Disabled(busy))
		color := t.TextMuted
		switch {
		case eye.Pressed():
			eye.Background(t.SurfacePressed)
			color = t.Text
		case eye.Hovered():
			eye.Background(t.SurfaceHover)
			color = t.Text
		}
		eye.Children(func() { ui.Icon(c, icon).FontSize(t.Rem(1.25)).TextColor(color) })
		if eye.Clicked() {
			s.shown = !s.shown
		}
	})
	if s.password != before && s.problem.field == passwordField {
		s.problem = problem{}
	}
	s.errorLine(c, passwordField)
}

func (s *Screen) errorLine(c *ui.Context, f field) {
	if s.problem.field != f {
		return
	}
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(t.Space(1.5)).PaddingX(t.Space(0.5)).Children(func() {
		ui.Icon(c, s.problem.icon).FontSize(t.Rem(1.05)).TextColor(t.Danger)
		ui.Text(c, s.problem.message).FontSize(t.Rem(0.92)).TextColor(t.Danger)
	})
}

func (s *Screen) actions(c *ui.Context, busy bool) {
	t := c.Theme()
	ui.Row(c).FillWidth().Gap(t.Space(2)).Children(func() {
		color := t.Text
		if busy {
			color = t.TextMuted
		}
		local := s.button(c, false, busy)
		local.Children(func() {
			ui.Icon(c, icons.Laptop).FontSize(t.Rem(1.25)).TextColor(color)
			ui.Text(c, s.text.UseLocal).NoWrap().FontWeight(500).TextColor(color)
		})
		if local.Clicked() && !busy {
			s.run++
			s.deps.UseLocal()
		}
		primary := s.button(c, true, busy)
		if busy {
			color = t.TextMuted
		} else {
			color = t.AccentText
		}
		primary.Children(func() {
			if busy {
				spin := ui.Icon(c, icons.LoaderCircle).FontSize(t.Rem(1.25)).TextColor(color)
				spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
				ui.Text(c, s.text.Connecting).NoWrap().FontWeight(500).TextColor(color)
			} else {
				ui.Text(c, s.text.Connect).NoWrap().FontWeight(500).TextColor(color)
				ui.Icon(c, icons.ArrowRight).FontSize(t.Rem(1.25)).TextColor(color)
			}
		})
		if primary.Clicked() {
			s.connect()
		}
	})
}

// button is a button of the form: on a Track, or Accent when primary; while busy both are Surface with a TextMuted
// label, never a faded accent.
func (s *Screen) button(c *ui.Context, primary, busy bool) ui.Element {
	t := c.Theme()
	b := theme.Fade(ui.ButtonBase(c).Grow(1).Basis(0).Height(t.Space(8)).Gap(t.Space(1.5)).PaddingX(t.Space(3)).
		Radius(t.Radius).Disabled(busy))
	track := theme.Track(t)
	fill := track
	if primary {
		fill = t.Accent
	}
	switch {
	case busy:
		fill = t.Surface
	case b.Pressed() && primary:
		fill = t.AccentPressed
	case b.Hovered() && primary:
		fill = t.AccentHover
	case b.Pressed():
		fill = track.Mix(theme.Ink(t, 1), 0.1)
	case b.Hovered():
		fill = track.Mix(theme.Ink(t, 1), 0.06)
	}
	if c.Preferences().HighContrast {
		b.Border(1, t.Border)
	}
	return b.Background(fill)
}
