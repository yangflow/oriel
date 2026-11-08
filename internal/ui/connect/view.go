// Package connect is the screen that connects the desktop app to an Oriel server and signs in; see
// the connect page of the screens canvas. The password lives only in the screen while it is typed and is sent once, to sign in.
package connect

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/design"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/theme"
	"github.com/yangflow/oriel/internal/ui/theme/tokens"
)

//go:embed copy.json
var catalog []byte

type copy struct {
	Title, Address, Password, PasswordLabel, Show, Reachable, UseLocal, Connect, Connecting                     string
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
	run               int                // counts connections, so a result the user moved on from is dropped
	cancel            context.CancelFunc // ends the requests of the connection under way
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
	s.advance()
	s.address, s.password, s.shown, s.phase, s.problem, s.expired = server, "", false, idle, problem{}, ""
	s.focus = addressField
	if errors.Is(err, client.ErrUnauthorized) {
		s.expired, s.focus = host(server), passwordField
	} else if err != nil {
		s.fail(err)
	}
}

// advance drops the connection under way and cancels its requests.
func (s *Screen) advance() {
	s.run++
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
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
	s.advance()
	run, address, password := s.run, s.address, s.password
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.phase, s.problem, s.focus = probing, problem{}, none
	s.deps.Go(func() func() {
		server, err := s.deps.Probe(ctx, address)
		if err != nil {
			return func() { s.finish(run, client.Session{}, err) }
		}
		s.deps.Go(func() func() {
			session, err := s.deps.Login(ctx, server, password)
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
	s.advance()
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
		ui.Column(c).FillWidth().MinHeight(height).Center().Padding(theme.Px(t, tokens.Space6)).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(theme.Px(t, tokens.SheetWidth)).Gap(theme.Px(t, tokens.Space4)).
				Children(func() {
					s.header(c)
					s.group(c, s.text.Address, addressField, func() { s.addressField(c, busy) })
					s.group(c, s.text.PasswordLabel, passwordField, func() { s.passwordField(c, busy) })
					s.errorLine(c, formField)
					s.actions(c, busy)
				})
		})
	})
}

// group is a field with its name above it, design BConnect; a click on the name focuses the field, as a label does.
func (s *Screen) group(c *ui.Context, name string, f field, draw func()) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(theme.Px(t, tokens.Space1_5)).Children(func() {
		// The input carries the same name, so screen readers skip the visible one.
		label := ui.Text(c, name).FontSize(theme.Px(t, tokens.TextMetaStrong.Size.En)).
			FontWeight(tokens.TextMetaStrong.Weight).TextColor(theme.Text2(t)).Role(ui.RoleNone)
		if label.Clicked() && s.phase == idle {
			s.focus = f
		}
		draw()
	})
}

var logo = ui.MustParseSVG(design.Icon)

func (s *Screen) header(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space3)).
		Padding(0, 0, theme.Px(t, tokens.Space2), 0).Children(func() {
		mark := theme.Px(t, logoSize)
		ui.Image(c, logo).Size(mark, mark)
		ui.Text(c, s.text.Title).FontSize(theme.Px(t, tokens.TextTitle.Size.En)).FontWeight(tokens.TextTitle.Weight).
			Role(ui.RoleHeading).Level(1)
		if s.expired != "" {
			before, after, _ := strings.Cut(s.text.Expired, "{server}")
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space1_5)).Children(func() {
				ui.Icon(c, icons.Clock).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
				ui.RichText(c,
					ui.Span{Text: before, Color: theme.Text2(t)},
					ui.Span{Text: s.expired, Weight: 500},
					ui.Span{Text: after, Color: theme.Text2(t)},
				).FontSize(theme.Px(t, tokens.TextMeta.Size.En))
			})
		}
	})
}

// addressExample is the address field's placeholder, the same in every language.
const addressExample = "https://oriel.example.com"

// logoSize is the product icon above the title, design ALogo at 64: two display lines.
const logoSize = 2 * tokens.Space8

// box is a text field, design CField: its input built by input and what trails it on the fill well.
func (s *Screen) box(c *ui.Context, key string, busy bool, input func(), trail func()) {
	t := c.Theme()
	invalid := s.problem.field == fieldOf(key)
	box := ui.Row(c).Key(key).FillWidth().Height(theme.Px(t, tokens.Control)).AlignItems(ui.Center).
		Gap(theme.Px(t, tokens.Space1_5)).Padding(0, theme.Px(t, tokens.Space1), 0, theme.Px(t, tokens.Space3)).
		Radius(theme.Px(t, tokens.RadiusControl)).Background(theme.Track(t))
	if c.Preferences().HighContrast {
		box.Border(tokens.Hairline, t.Border)
	}
	focused := box.FocusWithin() && !busy
	switch {
	case focused:
		box.Background(theme.RaisedFill(t)).Shadow(0, 0, 0, theme.Px(t, tokens.FocusRing), t.Focus)
	case invalid:
		box.Border(theme.Px(t, tokens.Hairline), t.Danger)
	case box.Hovered() && !busy:
		box.Background(t.SurfaceHover.Over(theme.Track(t)))
	}
	box.Children(func() {
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
	s.box(c, "address", busy, func() {
		s.input(c, &s.address, addressField, s.text.Address, addressExample, busy)
	}, func() {
		if s.phase == signingIn {
			side := theme.Px(t, tokens.RowDense)
			ui.Box(c).Size(side, side).Center().Label(s.text.Reachable).Role(ui.RoleImage).Children(func() {
				ui.Icon(c, icons.Check).FontSize(theme.IconMedium(t)).TextColor(t.Success)
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
	s.box(c, "password", busy, func() {
		in := s.input(c, &s.password, passwordField, s.text.PasswordLabel, s.text.Password, busy)
		if !s.shown {
			in.Password()
		}
	}, func() {
		icon := icons.Eye
		if s.shown {
			icon = icons.EyeOff
		}
		// The name stays the same and the pressed state says whether the password shows.
		side := theme.Px(t, tokens.RowDense)
		eye := theme.Fade(ui.ButtonBase(c).Size(side, side).Shrink(0).Radius(theme.Px(t, tokens.RadiusSm)).
			Label(s.text.Show).Tooltip(s.text.Show).Role(ui.RoleToggleButton).Checked(s.shown).Disabled(busy))
		color := theme.ToolFill(t, eye, theme.ToolState{Disabled: busy})
		eye.Children(func() { ui.Icon(c, icon).FontSize(theme.IconSmall(t)).TextColor(color) })
		if eye.Clicked() {
			s.shown = !s.shown
		}
	})
	if s.password != before && s.problem.field == passwordField {
		s.problem = problem{}
	}
	s.errorLine(c, passwordField)
}

// errorLine is what went wrong with f, design CBanner quiet in the danger tone: only the glyph takes the tone.
func (s *Screen) errorLine(c *ui.Context, f field) {
	if s.problem.field != f {
		return
	}
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space2)).Children(func() {
		ui.Icon(c, s.problem.icon).FontSize(theme.IconSmall(t)).TextColor(t.Danger)
		ui.Text(c, s.problem.message).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).TextColor(theme.Text2(t)).Wrap()
	})
}

// actions is Connect across the sheet and, under it, the quiet way back to this computer's server.
func (s *Screen) actions(c *ui.Context, busy bool) {
	t := c.Theme()
	ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space3)).Children(func() {
		label := s.text.Connect
		if busy {
			label = s.text.Connecting
		}
		primary := theme.Button{Style: theme.Primary, Disabled: busy, Busy: busy, Text: label}.Draw(c)
		primary.FillWidth()
		if primary.Clicked() {
			s.connect()
		}
		local := theme.Link{Muted: true, Disabled: busy, Icon: icons.Laptop, Text: s.text.UseLocal}.Draw(c)
		if local.Clicked() && !busy {
			s.advance()
			s.deps.UseLocal()
		}
	})
}
