// Package theme sets Oriel's palette on the MyGo theme: warm paper in light, neutral graphite in dark, an opaque ink
// pane for the sidebar and one muted teal accent. The roles ui.Theme lacks are helpers here, which return the pane's
// value when called with PaneTheme's theme. The values are in design/tokens.md.
package theme

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/prefs"
)

// Bar is the height of the toolbar row each screen floats over its content, in DIPs: the window buttons sit
// centered in it, and content scrolls under it starting this far down.
const Bar = 52

// Apply replaces the frame's theme with Oriel's palette, light or dark as the preferences say (the system appearance
// by default), and with the preferences' font family (the system font when it is empty). Call it first in the root
// view of every window, each frame. Metrics stay the theme's, so the desktop's text size still applies, and the colors
// are strengthened again where the desktop asks for high contrast.
func Apply(c *ui.Context, p prefs.Preferences) {
	t := *c.Theme()
	t.Dark = p.Dark(t.Dark)
	if t.Dark {
		dark(&t)
	} else {
		light(&t)
	}
	t.Font = p.Font
	t.Radius = 8
	high := c.Preferences().HighContrast
	t.SurfaceHover = Ink(&t, state(&t, 0, high))
	t.SurfacePressed = Ink(&t, state(&t, 1, high))
	t.Border = Ink(&t, 0.075)
	if high {
		t.Border = t.Text.Alpha(0.55)
		t.TextMuted = t.TextMuted.Mix(t.Text, 0.4)
		t.Focus.A = 255
	}
	c.SetTheme(&t)
}

// highContrast reports whether t came from Apply or PaneTheme with high contrast on: only then is Focus opaque.
func highContrast(t *ui.Theme) bool { return t.Focus.A == 255 }

// state is the alpha of a state fill, step 0 hover, 1 pressed, 2 chosen: 3, 5 and 6.5% in light, 4, 6 and 8% in
// dark, 5.5, 8.5 and 12% in the pane, 8, 12 and 16% with high contrast.
func state(t *ui.Theme, step int, high bool) float32 {
	switch {
	case high:
		return [3]float32{0.08, 0.12, 0.16}[step]
	case isPane(t):
		return [3]float32{0.055, 0.085, 0.12}[step]
	case t.Dark:
		return [3]float32{0.04, 0.06, 0.08}[step]
	}
	return [3]float32{0.03, 0.05, 0.065}[step]
}

// The inks the alpha fills are made of, and the two fills a theme is known to be the pane's by.
var (
	paperInk     = ui.RGB(37, 34, 30)
	graphiteInk  = ui.RGB(234, 240, 238)
	paneLightInk = ui.RGB(255, 248, 235)
	paneDarkInk  = ui.RGB(235, 248, 244)
	paneLightBG  = ui.Hex("#37332e")
	paneDarkBG   = ui.Hex("#1a2422")
)

// isPane reports whether t is a theme from PaneTheme: its Background is one of the two pane fills, which no content
// theme has.
func isPane(t *ui.Theme) bool { return t.Background == paneLightBG || t.Background == paneDarkBG }

// pick returns the value for a content theme in light, in dark, and for the pane in its light and dark scheme.
func pick(t *ui.Theme, light, dark, paneLight, paneDark ui.Color) ui.Color {
	switch {
	case t.Background == paneDarkBG:
		return paneDark
	case t.Background == paneLightBG:
		return paneLight
	case t.Dark:
		return dark
	}
	return light
}

// PaneTheme returns the theme of the ink pane for the main theme: a dark theme in both schemes, opaque, with roles of
// its own, charcoal when main is light and deep teal when main is dark. Metrics and the font stay main's. High
// contrast is read from main (Apply leaves Focus opaque then): the state fills step up and Border becomes the rim of
// Pane. Call c.SetTheme with it before the sidebar's elements are created and set main back after. Given a pane
// theme, it returns it unchanged. The helpers of this package called with it return the pane values.
func PaneTheme(main *ui.Theme) *ui.Theme {
	if isPane(main) {
		return main
	}
	t := *main
	t.Dark = true
	ink := paneLightInk
	if main.Dark {
		ink = paneDarkInk
		t.Background, t.Text, t.TextMuted = paneDarkBG, ui.Hex("#e8eeec"), ui.Hex("#93a39f")
	} else {
		t.Background, t.Text, t.TextMuted = paneLightBG, ui.Hex("#ece7dd"), ui.Hex("#a49e91")
	}
	high := highContrast(main)
	t.Surface = ink.Alpha(0.11)
	t.SurfaceHover = ink.Alpha(state(&t, 0, high))
	t.SurfacePressed = ink.Alpha(state(&t, 1, high))
	t.Border = ink.Alpha(0.08)
	t.Accent, t.AccentHover, t.AccentPressed = ui.Hex("#7cc9bb"), ui.Hex("#92d5c9"), ui.Hex("#68b5a7")
	t.AccentText = ui.Hex("#10241f")
	t.Danger, t.Warning, t.Success = ui.Hex("#e0897a"), ui.Hex("#d6aa55"), ui.Hex("#8ab997")
	t.Selection, t.Focus = t.Accent.Alpha(0.3), t.Accent.Alpha(0.7)
	t.Inverse, t.InverseText = ui.Hex("#333837"), ui.Hex("#f4efe6")
	t.Scrollbar = ink.Alpha(0.3)
	if high {
		t.Border = ink.Mix(t.Text, 0.45)
		t.TextMuted = t.TextMuted.Mix(t.Text, 0.4)
		t.Focus.A = 255
	}
	return &t
}

// Ink is the color of every alpha fill, at alpha a: rgb(37, 34, 30) in light, rgb(234, 240, 238) in dark, and the
// pane's warm white or mint white in the pane.
func Ink(t *ui.Theme, a float32) ui.Color {
	return pick(t, paperInk, graphiteInk, paneLightInk, paneDarkInk).Alpha(a)
}

// Text2 is the step between Text and TextMuted: source names, summaries, unselected names in the pane.
func Text2(t *ui.Theme) ui.Color {
	return pick(t, ui.Hex("#58534a"), ui.Hex("#aab0ae"), ui.Hex("#cdc7bb"), ui.Hex("#cbd5d2"))
}

// Prose is the color of the reading body; the pane has none and returns its Text.
func Prose(t *ui.Theme) ui.Color {
	if isPane(t) {
		return t.Text
	}
	return pick(t, ui.Hex("#3a362f"), ui.Hex("#c3c9c7"), ui.Color{}, ui.Color{})
}

// Track is the trough of a segmented control, a monogram tile and a skeleton bar; in the pane, ink at 11%.
func Track(t *ui.Theme) ui.Color {
	if isPane(t) {
		return Ink(t, 0.11)
	}
	return pick(t, ui.Hex("#ece8e0"), ui.Hex("#2c2f2e"), ui.Color{}, ui.Color{})
}

// RaisedFill is the face of a focused search, a dialog, a sheet, a popover and the chosen segment on a Track; the
// pane has none and returns its Surface.
func RaisedFill(t *ui.Theme) ui.Color {
	if isPane(t) {
		return t.Surface
	}
	return pick(t, ui.Hex("#ffffff"), ui.Hex("#252827"), ui.Color{}, ui.Color{})
}

// LineStrong is the edge of a form field, rarely; in the pane, ink at 16%.
func LineStrong(t *ui.Theme) ui.Color {
	if isPane(t) {
		return Ink(t, 0.16)
	}
	return pick(t, ui.Hex("#d4cec2"), ui.Hex("#3d4241"), ui.Color{}, ui.Color{})
}

// Disabled colors inactive icons and glyphs, never the only copy of a label; in the pane, TextMuted at 55%.
func Disabled(t *ui.Theme) ui.Color {
	if isPane(t) {
		return t.TextMuted.Alpha(0.55)
	}
	return pick(t, ui.Hex("#b3ada1"), ui.Hex("#565b59"), ui.Color{}, ui.Color{})
}

// Star colors a starred entry; a graphic, never text.
func Star(t *ui.Theme) ui.Color {
	return pick(t, ui.Hex("#b98a22"), ui.Hex("#e2b24f"), ui.Hex("#e2b24f"), ui.Hex("#e2b24f"))
}

// Marker colors the selection marker of a timeline row: the Accent (the pane's ChromeAccent).
func Marker(t *ui.Theme) ui.Color { return t.Accent }

// Chosen is the selected fill of a row or a navigation item: ink at 6.5% in light, 8% in dark, 12% in the pane and
// 16% with high contrast. Its icon is in the accent.
func Chosen(t *ui.Theme) ui.Color {
	return Ink(t, state(t, 2, highContrast(t)))
}

// Soft is color at 9% in light and 12% in dark and in the pane: the faint fill behind an icon or a status.
func Soft(t *ui.Theme, color ui.Color) ui.Color {
	if t.Dark {
		return color.Alpha(0.12)
	}
	return color.Alpha(0.09)
}

var fillFade = ui.ElementTransition{Duration: 140 * time.Millisecond, Colors: true}

// Fade lets a row's hover and chosen fills fade in and out, colors only, so nothing moves even with reduced motion.
// The row needs a stable ID: a Key, or the same place among its siblings each frame.
func Fade(e ui.Element) ui.Element {
	return e.Transition(fillFade)
}

// Raised draws the chosen segment of a control on a Track: RaisedFill with a hairline.
func Raised(t *ui.Theme, b ui.Element) {
	b.Background(RaisedFill(t)).Shadow(0, 0, 0, 1, t.Border)
}

// Lifted draws the chosen segment of a control on glass: ink at 10%.
func Lifted(t *ui.Theme, b ui.Element) {
	b.Background(Ink(t, 0.1))
}

// Pane fills the sidebar with the opaque ink pane: the Background of PaneTheme (the element may be built under the
// main theme or the pane's), radius 14 and a 0.5 DIP inner rim in the pane ink, 10% in the light scheme and 8% in the
// dark one. With high contrast the rim is 1 DIP in the pane theme's Border.
func Pane(c *ui.Context, e ui.Element) ui.Element {
	t := PaneTheme(c.Theme())
	e.Background(t.Background).Radius(14)
	if highContrast(t) {
		return e.Border(1, t.Border)
	}
	rim := float32(0.1)
	if t.Background == paneDarkBG {
		rim = 0.08
	}
	return e.Border(0.5, Ink(t, rim))
}

// Capsule fills a toolbar control with glass tinted rgba(255, 254, 251, 0.66) in light and rgba(58, 63, 62, 0.52) in
// dark, a little darker under the pointer; with high contrast it is opaque Surface with a Border rim, SurfaceHover
// under the pointer.
func Capsule(c *ui.Context, e ui.Element, hover bool) ui.Element {
	t := c.Theme()
	if c.Preferences().HighContrast {
		if hover {
			return e.Background(t.SurfaceHover).Border(1, t.Border)
		}
		return e.Background(t.Surface).Border(1, t.Border)
	}
	tint := ui.RGBA(255, 254, 251, 0.66)
	if t.Dark {
		tint = ui.RGBA(58, 63, 62, 0.52)
	}
	if hover {
		tint = tint.Mix(Ink(t, 1), 0.08)
	}
	return e.Material(glass.Glass{Tint: tint})
}

var edgeFade = ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true}

// Edge draws the solid bar behind a toolbar row: Background, Bar high, with a 0.5 DIP Border hairline under it once
// the content has scrolled (1 DIP with high contrast). The hairline fades in and out over 120 ms, colors only. Paint
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

// ToolButton is the one toolbar icon button: 32 by 32, radius 8, icon 16, TextMuted becoming Text under the pointer,
// SurfaceHover under the pointer and SurfacePressed pressed. It returns the button and the color for its icon.
func ToolButton(c *ui.Context, st ToolState) (ui.Element, ui.Color) {
	t := c.Theme()
	b := ui.ButtonBase(c).Size(t.Rem(32.0/13), t.Rem(32.0/13)).Shrink(0).Radius(t.Rem(8.0 / 13)).Disabled(st.Disabled)
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
			fill = fill.Mix(tint, 0.12)
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
	return t.TextMuted
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

func light(t *ui.Theme) {
	t.Background = ui.Hex("#fdfcfa")
	t.Surface = ui.Hex("#f6f3ee")
	t.Text = ui.Hex("#25221e")
	t.TextMuted = ui.Hex("#736d62")
	t.Accent = ui.Hex("#2a6d65")
	t.AccentHover = ui.Hex("#225a54")
	t.AccentPressed = ui.Hex("#1c4a45")
	t.AccentText = ui.Hex("#ffffff")
	t.Danger = ui.Hex("#b4533c")
	t.Warning = ui.Hex("#9a7222")
	t.Success = ui.Hex("#4c7c59")
	t.Selection = t.Accent.Alpha(0.22)
	t.Focus = t.Accent.Alpha(0.7)
	t.Inverse = ui.Hex("#2a2620")
	t.InverseText = ui.Hex("#f4efe6")
	t.Scrollbar = Ink(t, 0.28)
}

func dark(t *ui.Theme) {
	t.Background = ui.Hex("#171918")
	t.Surface = ui.Hex("#1d1f1e")
	t.Text = ui.Hex("#dde1df")
	t.TextMuted = ui.Hex("#8a918f")
	t.Accent = ui.Hex("#78beb1")
	t.AccentHover = ui.Hex("#8ccbbf")
	t.AccentPressed = ui.Hex("#64aa9d")
	t.AccentText = ui.Hex("#10241f")
	t.Danger = ui.Hex("#e0897a")
	t.Warning = ui.Hex("#d6aa55")
	t.Success = ui.Hex("#8ab997")
	t.Selection = t.Accent.Alpha(0.3)
	t.Focus = t.Accent.Alpha(0.7)
	t.Inverse = ui.Hex("#333837")
	t.InverseText = ui.Hex("#f4efe6")
	t.Scrollbar = Ink(t, 0.3)
}

// Px converts a size of the mockup, drawn at a 13 px font, to the theme's text size.
func Px(t *ui.Theme, v float32) float32 { return t.Rem(v / 13) }

// Monogram draws the first letter of a subscription's name on a Track tile, in Text2, for a square of size DIPs
// (in mockup pixels): 15 in the meta line and the reader, 18 in the timeline pane, 32 in the subscriptions list.
func Monogram(c *ui.Context, name string, size float32) {
	t := c.Theme()
	letter := "·"
	if r, _ := utf8.DecodeRuneInString(strings.TrimLeft(name, "@#·/ ")); r != utf8.RuneError {
		letter = strings.ToUpper(string(r))
	}
	radius, font := GlyphRadius(size), float32(9)
	switch {
	case size > 20:
		font = 15
	case size > 16:
		font = 10.5
	}
	ui.Box(c).Size(Px(t, size), Px(t, size)).Radius(radius).Center().Background(Track(t)).Children(func() {
		ui.Text(c, letter).FontSize(Px(t, font)).FontWeight(600).TextColor(Text2(t))
	})
}

// GlyphRadius is the corner radius of a glyph of size: 4 up to 16, 5 up to 20 and 8 above.
func GlyphRadius(size float32) float32 {
	switch {
	case size > 20:
		return 8
	case size > 16:
		return 5
	}
	return 4
}
