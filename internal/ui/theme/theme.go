// Package theme sets Oriel's palette on the MyGo theme: warm paper in light, graphite in dark, an opaque ink pane for
// the sidebar and one muted teal accent. Every value comes from package tokens, generated from design/tokens.json;
// the roles ui.Theme lacks are helpers here, which return the pane's value when called with PaneTheme's theme and a
// reading preset's when called with ReadingTheme's.
package theme

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/prefs"
	"github.com/yangflow/oriel/internal/ui/theme/tokens"
)

// Bar is the height of the toolbar row each screen floats over its content, in DIPs: the window buttons sit
// centered in it, and content scrolls under it starting this far down.
const Bar = tokens.Toolbar

// Apply replaces the frame's theme with Oriel's palette, light or dark as the preferences say (the system appearance
// by default), with the preferences' font family (the system font when it is empty) and scaled by their interface
// size. Call it first in the root view of every window, each frame. The text size starts from the theme's, so the
// desktop's text size still applies, and the colors are strengthened where the desktop asks for high contrast.
func Apply(c *ui.Context, p prefs.Preferences) {
	t := *c.Theme()
	t.Dark = p.Dark(t.Dark)
	s := &tokens.SchemeLight
	if t.Dark {
		s = &tokens.SchemeDark
	}
	s.SetRoles(&t)
	tokens.SetMetrics(&t)
	t.Font = p.Font
	t.FontSize *= float32(p.InterfaceFactor())
	if c.Preferences().HighContrast {
		ink := opaque(s.Fill)
		t.SurfaceHover = ink.Alpha(tokens.HighContrast.Hover)
		t.SurfacePressed = ink.Alpha(tokens.HighContrast.Pressed)
		strengthen(&t)
	}
	c.SetTheme(&t)
}

// strengthen applies the high contrast rules that are not alphas: line is fg at 55%, muted moves 40% toward fg and
// focus is opaque.
func strengthen(t *ui.Theme) {
	t.Border = t.Text.Alpha(tokens.OpacityHighLine)
	t.TextMuted = t.TextMuted.Mix(t.Text, tokens.OpacityHighMuted)
	t.Focus.A = 255
}

// highContrast reports whether t came from Apply or PaneTheme with high contrast on: only then is Focus opaque.
func highContrast(t *ui.Theme) bool { return t.Focus.A == 255 }

// opaque is c without its alpha: the ink a token's alpha fill is made of.
func opaque(c ui.Color) ui.Color { return ui.RGB(c.R, c.G, c.B) }

// colors is what the helpers return for one kind of theme: the main scheme, the pane or a reading preset.
type colors struct {
	ink                                      ui.Color // what the alpha fills are made of
	fg2, prose, surface, float, raised       ui.Color
	fill, selected, line2, faint, star, soft ui.Color
}

func mainColors(s *tokens.Scheme) colors {
	return colors{opaque(s.Fill), s.Fg2, s.Prose, s.Surface, s.Float, s.Raised,
		s.Fill, s.Selected, s.Line2, s.Faint, s.Star, s.Soft}
}

func paneColors(s *tokens.Scheme) colors {
	// The pane has no surface, float or raised of its own: things on it take its fill.
	return colors{opaque(s.PaneFill), s.PaneFg2, s.PaneFg, s.PaneFill, s.PaneFill, s.PaneFill,
		s.PaneFill, s.PaneSelected, opaque(s.PaneFill).Alpha(tokens.OpacityPaneRule), s.PaneFaint, tokens.SchemeDark.Star, tokens.SchemeDark.Soft}
}

func presetColors(p *tokens.Preset, s *tokens.Scheme) colors {
	ink := p.Ink
	return colors{ink, p.Fg2, p.Prose, p.Surface, p.Raised, p.Raised,
		ink.Alpha(alphaOf(s.Fill)), ink.Alpha(alphaOf(s.Selected)), ink.Alpha(alphaOf(s.Line2)), p.Faint, p.Star,
		p.Accent.Alpha(alphaOf(s.Soft))}
}

func alphaOf(c ui.Color) float32 { return float32(c.A) / 255 }

var (
	lightColors     = mainColors(&tokens.SchemeLight)
	darkColors      = mainColors(&tokens.SchemeDark)
	paneLightColors = paneColors(&tokens.SchemeLight)
	paneDarkColors  = paneColors(&tokens.SchemeDark)
)

// isPane reports whether t is a theme from PaneTheme: its Background is one of the two pane grounds, which no content
// theme has.
func isPane(t *ui.Theme) bool {
	return t.Background == tokens.SchemeLight.Pane || t.Background == tokens.SchemeDark.Pane
}

// colorsOf returns the helpers' values for t.
func colorsOf(t *ui.Theme) *colors {
	switch {
	case t.Background == tokens.SchemeDark.Pane:
		return &paneDarkColors
	case t.Background == tokens.SchemeLight.Pane:
		return &paneLightColors
	}
	if p := presetOf(t); p != nil {
		return &p.colors
	}
	if t.Dark {
		return &darkColors
	}
	return &lightColors
}

// PaneTheme returns the theme of the ink pane for the main theme: a dark theme in both schemes, opaque, with the
// pane tokens of main's scheme, charcoal when main is light and deep teal when main is dark. Metrics and the font
// stay main's. High contrast is read from main (Apply leaves Focus opaque then): the state fills step up and the
// rules of strengthen apply. Call c.SetTheme with it before the sidebar's elements are created and set main back
// after. Given a pane theme, it returns it unchanged. The helpers of this package called with it return the pane
// values.
func PaneTheme(main *ui.Theme) *ui.Theme {
	if isPane(main) {
		return main
	}
	s := &tokens.SchemeLight
	if main.Dark {
		s = &tokens.SchemeDark
	}
	t := *main
	t.Dark = true
	// The meaning colors and the inverse are the dark scheme's: the pane is dark in both.
	tokens.SchemeDark.SetRoles(&t)
	s.SetPaneRoles(&t)
	t.AccentHover, t.AccentPressed = s.PaneAccent.Mix(t.Text, tokens.OpacityAccentMix), s.PaneAccent.Mix(t.Background, tokens.OpacityAccentMix)
	t.Selection, t.Focus = t.Accent.Alpha(tokens.OpacityPaneSelection), t.Accent.Alpha(alphaOf(s.Focus))
	t.Scrollbar = opaque(s.PaneFill).Alpha(tokens.OpacityPaneScrollbar)
	if highContrast(main) {
		ink := opaque(s.PaneFill)
		t.SurfaceHover = ink.Alpha(tokens.HighContrast.Hover)
		t.SurfacePressed = ink.Alpha(tokens.HighContrast.Pressed)
		strengthen(&t)
	}
	return &t
}

// Ink is the color of every alpha fill, at alpha a: the scheme's warm or cool ink, the pane's or the preset's.
func Ink(t *ui.Theme, a float32) ui.Color { return colorsOf(t).ink.Alpha(a) }

// Text2 is fg2, the step between Text and TextMuted: source names, summaries, unselected names in the pane.
func Text2(t *ui.Theme) ui.Color { return colorsOf(t).fg2 }

// Prose is the color of the reading body; the pane has none and returns its Text.
func Prose(t *ui.Theme) ui.Color { return colorsOf(t).prose }

// Surface is the opaque ground of things that sit on a region: banners, the empty state's disc, cards, notes,
// code blocks. MyGo's Surface role is the fill, which is a control's face.
func Surface(t *ui.Theme) ui.Color { return colorsOf(t).surface }

// Track is the fill, the resting face of a control: a segmented groove, a field, a monogram tile, a skeleton bar.
func Track(t *ui.Theme) ui.Color {
	if highContrast(t) {
		return colorsOf(t).ink.Alpha(tokens.HighContrast.Fill)
	}
	return colorsOf(t).fill
}

// RaisedFill is float, the face of things that float over content: a focused search, a dialog, a popup, the row
// actions.
func RaisedFill(t *ui.Theme) ui.Color { return colorsOf(t).float }

// LineStrong is line2: rims of floats and raised segments, form edges.
func LineStrong(t *ui.Theme) ui.Color { return colorsOf(t).line2 }

// Disabled is faint: inactive icons and glyphs, never the only copy of a label.
func Disabled(t *ui.Theme) ui.Color { return colorsOf(t).faint }

// Star colors a starred entry; a graphic, never text.
func Star(t *ui.Theme) ui.Color { return colorsOf(t).star }

// Marker colors the selection marker of a timeline row: the Accent (the pane's accent in the pane).
func Marker(t *ui.Theme) ui.Color { return t.Accent }

// Chosen is the selected fill of a row or a navigation item, between hover and pressed; with high contrast, ink at
// the high contrast ladder's selected alpha. Its icon is in the accent.
func Chosen(t *ui.Theme) ui.Color {
	if highContrast(t) {
		return colorsOf(t).ink.Alpha(tokens.HighContrast.Selected)
	}
	return colorsOf(t).selected
}

// Soft is color at the soft alpha of t's scheme: the faint fill behind an icon or a status, an on toolbar button.
func Soft(t *ui.Theme, color ui.Color) ui.Color {
	return color.Alpha(alphaOf(colorsOf(t).soft))
}

var fillFade = ui.ElementTransition{Duration: tokens.MotionFast, Colors: true}

// Fade lets a row's hover and chosen fills fade in and out, colors only, so nothing moves even with reduced motion.
// The row needs a stable ID: a Key, or the same place among its siblings each frame.
func Fade(e ui.Element) ui.Element {
	return e.Transition(fillFade)
}

// MinTarget is the least side of a pointer target in DIPs (WCAG 2.5.8): a control drawn smaller is
// still this big to the pointer.
const MinTarget = 24

// Hit is a small control: what it draws, W by H with Radius and Fill, and the pointer target around it, at least
// MinTarget on each side. A W of 0 draws as wide as its children and PadX around them.
type Hit struct {
	W, H, Radius float32
	Fill         ui.Color
	PadX, Gap    float32
	Fade         bool // the fill fades in and out
}

// Apply makes b the target and draws the control inside it, centered, from children. The target takes more room
// than the control draws, so the caller gives the room back with the margins it returns, across and down, both 0 or
// negative: b.Margin(y, x, y, x) leaves the control in its place in the row, and the target reaches into the gap
// beside it, not into its neighbor.
func (h Hit) Apply(c *ui.Context, b ui.Element, children func()) (x, y float32) {
	side := max(MinTarget, h.H)
	b.Height(side).Center()
	x, y = 0, (h.H-side)/2
	if h.W > 0 {
		wide := max(MinTarget, h.W)
		b.Width(wide)
		x = (h.W - wide) / 2
	} else {
		b.MinWidth(MinTarget)
	}
	b.Children(func() {
		in := ui.Row(c).Height(h.H).Center().Gap(h.Gap).PaddingX(h.PadX).Radius(h.Radius).Background(h.Fill)
		if h.W > 0 {
			in.Width(h.W)
		}
		if h.Fade {
			Fade(in)
		}
		in.Children(children)
	})
	return x, y
}

// Elevation returns the shadows of t's scheme: the pane and the dark presets are dark.
func Elevation(t *ui.Theme) *tokens.Elevation {
	if t.Dark {
		return &tokens.ElevationDark
	}
	return &tokens.ElevationLight
}

// Shadow gives b the shadow s at t's text size, and its rim when s has one; with high contrast the rim is 1 DIP in
// t's Border.
func Shadow(t *ui.Theme, b ui.Element, s tokens.Shadow) ui.Element {
	b.Shadow(Px(t, s.X), Px(t, s.Y), Px(t, s.Blur), 0, s.Color)
	switch {
	case highContrast(t):
		b.Border(1, t.Border)
	case s.Rim > 0:
		b.Border(s.Rim, s.RimColor)
	}
	return b
}

// Raised draws the chosen segment of a control on a Track: the raised face with the raised shadow and its rim.
func Raised(t *ui.Theme, b ui.Element) {
	Shadow(t, b.Background(colorsOf(t).raised), Elevation(t).Raised)
}

// Lifted draws the chosen segment of a control on glass: ink at the selected alpha.
func Lifted(t *ui.Theme, b ui.Element) {
	b.Background(Chosen(t))
}

// Pane fills the sidebar with the opaque ink pane: the Background of PaneTheme (the element may be built under the
// main theme or the pane's) and radius-pane, with no rim; with high contrast a 1 DIP rim in the pane theme's Border.
func Pane(c *ui.Context, e ui.Element) ui.Element {
	t := PaneTheme(c.Theme())
	e.Background(t.Background).Radius(Px(t, tokens.RadiusPane))
	if highContrast(t) {
		return e.Border(1, t.Border)
	}
	return e
}

// Capsule fills a toolbar control with glass in the `glass` tint, a little darker under the pointer; with high contrast it is the opaque Surface with a Border rim, SurfaceHover
// over it under the pointer.
func Capsule(c *ui.Context, e ui.Element, hover bool) ui.Element {
	t := c.Theme()
	if c.Preferences().HighContrast {
		if hover {
			return e.Background(t.SurfaceHover.Over(Surface(t))).Border(1, t.Border)
		}
		return e.Background(Surface(t)).Border(1, t.Border)
	}
	tint := tokens.SchemeLight.Glass
	if t.Dark {
		tint = tokens.SchemeDark.Glass
	}
	if presetOf(t) != nil {
		raised := RaisedFill(t)
		raised.A = tint.A
		tint = raised
	}
	if hover {
		tint = tint.Mix(Ink(t, 1), HoverMix(t))
	}
	return e.Material(glass.Glass{Tint: tint})
}

var edgeFade = ui.ElementTransition{Duration: tokens.MotionFast, Colors: true}

// Edge draws the solid bar behind a toolbar row: Background, Bar high, with a 0.5 DIP Border hairline under it once
// the content has scrolled (1 DIP with high contrast). The hairline fades in and out over motion-fast, colors only. Paint
// it over the content and before the toolbar.
func Edge(c *ui.Context, scrolled bool) {
	t := c.Theme()
	ui.Box(c).Key("edge-bar").Absolute().Top(0).Left(0).Right(0).Height(Bar).Background(t.Background).PassThrough()
	line := t.Border.Alpha(0)
	if scrolled {
		line = t.Border
	}
	width := float32(0.5)
	if c.Preferences().HighContrast {
		width = 1
	}
	ui.Box(c).Key("edge-line").Absolute().Top(Bar).Left(0).Right(0).Height(width).Background(line).
		Transition(edgeFade).PassThrough()
}

// ToolState is how a toolbar icon button looks: Disabled has no fill and the Disabled icon, On has the Soft tint of
// Tint (the Accent when zero) and an icon in Tint.
type ToolState struct {
	Disabled, On bool
	Tint         ui.Color
}

// ToolButton is the one toolbar icon button: control square, radius-control, icon-m, Text2 becoming Text under the
// pointer, SurfaceHover under the pointer and SurfacePressed pressed. It returns the button and the color for its
// icon.
func ToolButton(c *ui.Context, st ToolState) (ui.Element, ui.Color) {
	t := c.Theme()
	b := ui.ButtonBase(c).Size(Px(t, tokens.Control), Px(t, tokens.Control)).Shrink(0).Radius(Px(t, tokens.RadiusControl)).
		Disabled(st.Disabled)
	return b, ToolFill(t, b, st)
}

// ToolFill fills b for its state and returns the color of its icon.
func ToolFill(t *ui.Theme, b ui.Element, st ToolState) ui.Color {
	if st.Disabled {
		return Disabled(t)
	}
	if st.On {
		tint := st.Tint
		if tint == (ui.Color{}) {
			tint = t.Accent
		}
		fill := Soft(t, tint)
		if b.Hovered() || b.Pressed() {
			fill = fill.Mix(tint, HoverMix(t))
		}
		b.Background(fill)
		return tint
	}
	switch {
	case b.Pressed():
		b.Background(t.SurfacePressed)
		return t.Text
	case b.Hovered():
		b.Background(t.SurfaceHover)
		return t.Text
	}
	return Text2(t)
}

// Han reports whether a text has Chinese characters; Chinese runs taller, so such text gets a larger line height.
func Han(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// HanLanguage reports whether text in the language, a BCP 47 code, runs taller: Chinese and Japanese do. Known is
// false for "" and the codes it cannot tell by.
func HanLanguage(code string) (han, known bool) {
	if code == "" {
		return false, false
	}
	base, _, _ := strings.Cut(code, "-")
	return base == "zh" || base == "ja", true
}

// Px converts a size of the mockup, drawn at a 13 px font, to the theme's text size.
func Px(t *ui.Theme, v float32) float32 { return t.Rem(v / 13) }

// Monogram draws the first letter of a subscription's name on a Track tile, in Text2 at weight 600 and 0.58 of the
// tile, for a square of size DIPs (in mockup pixels): tile-s in the meta line and the reader, tile-m in the pane,
// tile-l in the subscriptions list.
func Monogram(c *ui.Context, name string, size float32) {
	t := c.Theme()
	letter := "·"
	if r, _ := utf8.DecodeRuneInString(strings.TrimLeft(name, "@#·/ ")); r != utf8.RuneError {
		letter = strings.ToUpper(string(r))
	}
	radius, font := float32(tokens.RadiusXS), size*tileLetter
	ui.Box(c).Size(Px(t, size), Px(t, size)).Radius(radius).Center().Background(Track(t)).Children(func() {
		ui.Text(c, letter).FontSize(Px(t, font)).FontWeight(600).TextColor(Text2(t))
	})
}

// tileLetter is the size of a monogram's letter against its tile.
const tileLetter = 0.58

func schemeOf(t *ui.Theme) *tokens.Scheme {
	if t.Dark {
		return &tokens.SchemeDark
	}
	return &tokens.SchemeLight
}

// Rule is the line of a quote and the rim of a small tile: ink at the rule alpha of t's scheme.
func Rule(t *ui.Theme) ui.Color { return Ink(t, alphaOf(schemeOf(t).Rule)) }

// Underline is the accent at the alpha of a link's underline, for a thin accent line.
func Underline(t *ui.Theme) ui.Color { return t.Accent.Alpha(alphaOf(schemeOf(t).LinkUnderline)) }

// HoverMix is how far a fill moves toward ink under the pointer, the hover alpha of t.
func HoverMix(t *ui.Theme) float32 { return alphaOf(t.SurfaceHover) }

// PressMix is how far a fill moves toward ink while pressed, the pressed alpha of t.
func PressMix(t *ui.Theme) float32 { return alphaOf(t.SurfacePressed) }

// Pill is the corner radius of a pill or a circle of the given height: half of it. It is the one radius that is
// not a tier of design/tokens.md, because it follows the shape it rounds.
func Pill(height float32) float32 { return height / 2 }

// IconSmall is the inline icon size for t.
func IconSmall(t *ui.Theme) float32 { return Px(t, tokens.IconS) }

// IconMedium is the icon size of icon buttons and leading icons for t.
func IconMedium(t *ui.Theme) float32 { return Px(t, tokens.IconM) }

// IconLarge is the icon size of states, such as the tile of an empty state, for t.
func IconLarge(t *ui.Theme) float32 { return Px(t, tokens.IconL) }

// Badge is the fill of a badge or overlay on a picture, the same in both schemes because it sits on the picture.
func Badge(t *ui.Theme) ui.Color { return tokens.SchemeLight.Badge }

// OnInverse is the text on a Badge.
func OnInverse(t *ui.Theme) ui.Color { return tokens.SchemeLight.OnInverse }
