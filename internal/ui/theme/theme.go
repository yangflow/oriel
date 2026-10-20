// Package theme sets Oriel's palette on the MyGo theme: paper and ink, warm paper neutrals, a softened ink and a muted
// teal accent, light and dark. The values are the color table in design/tokens.md.
package theme

import (
	"time"
	"unicode"

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
	if c.Preferences().HighContrast {
		t.Border = t.Border.Mix(t.Text, 0.45)
		t.TextMuted = t.TextMuted.Mix(t.Text, 0.4)
		t.Focus.A = 255
	}
	c.SetTheme(&t)
}

// Soft tints a color toward the background, for the faint fills behind an icon or a status.
func Soft(t *ui.Theme, color ui.Color) ui.Color {
	if t.Dark {
		return color.Mix(t.Background, 0.84)
	}
	return color.Mix(t.Background, 0.88)
}

// Chosen is the fill of the place you are in a navigation list, a neutral ink: Text at 7% (9% in dark); its icon and
// count are in the accent.
func Chosen(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.Text.Alpha(0.09)
	}
	return t.Text.Alpha(0.07)
}

var fillFade = ui.ElementTransition{Duration: 140 * time.Millisecond, Colors: true}

// Fade lets a row's hover and chosen fills fade in and out, colors only, so nothing moves even with reduced motion.
// The row needs a stable ID: a Key, or the same place among its siblings each frame.
func Fade(e ui.Element) ui.Element {
	return e.Transition(fillFade)
}

// Raised draws the chosen segment of a control: a face raised off the track, Background with a hairline in light,
// SurfacePressed in dark.
func Raised(t *ui.Theme, b ui.Element) {
	if t.Dark {
		b.Background(t.SurfacePressed)
		return
	}
	b.Background(t.Background).Shadow(0, 0, 0, 1, t.Border)
}

// Pane fills the sidebar with a floating pane of glass tinted Surface; with high contrast it is opaque Surface with a
// Border rim.
func Pane(c *ui.Context, e ui.Element) ui.Element {
	t := c.Theme()
	if c.Preferences().HighContrast {
		return e.Background(t.Surface).Border(1, t.Border)
	}
	return e.Material(glass.Glass{Tint: t.Surface})
}

// Capsule fills a toolbar control with clear glass, a little darker under the pointer; with high contrast it is
// opaque Surface with a Border rim, SurfaceHover under the pointer.
func Capsule(c *ui.Context, e ui.Element, hover bool) ui.Element {
	t := c.Theme()
	if c.Preferences().HighContrast {
		if hover {
			return e.Background(t.SurfaceHover).Border(1, t.Border)
		}
		return e.Background(t.Surface).Border(1, t.Border)
	}
	if hover {
		return e.Material(glass.Glass{Tint: t.Text.Alpha(0.06)})
	}
	return e.Material(glass.Glass{})
}

// Edge fades the content scrolling under the toolbar row into the background (the soft scroll edge). Paint it over
// the content and before the toolbar.
func Edge(c *ui.Context) {
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(Bar + 10).PassThrough().Material(glass.ScrollEdge{})
}

// Lifted draws the chosen segment of a control on glass: Background lifted off the track by a soft shadow in light,
// SurfacePressed in dark.
func Lifted(t *ui.Theme, b ui.Element) {
	if t.Dark {
		b.Background(t.SurfacePressed)
		return
	}
	b.Background(t.Background).Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.14))
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

func light(t *ui.Theme) {
	t.Background = ui.Hex("#f8f6f1")
	t.Surface = ui.Hex("#efebe3")
	t.SurfaceHover = ui.Hex("#e7e2d8")
	t.SurfacePressed = ui.Hex("#ded8cc")
	t.Border = ui.Hex("#e3ddd2")
	t.Text = ui.Hex("#2b2824")
	t.TextMuted = ui.Hex("#68625a")
	t.Accent = ui.Hex("#2b6f66")
	t.AccentHover = ui.Hex("#245e57")
	t.AccentPressed = ui.Hex("#1f4f49")
	t.AccentText = ui.Hex("#ffffff")
	t.Danger = ui.Hex("#c2410c")
	t.Warning = ui.Hex("#a16207")
	t.Success = ui.Hex("#15803d")
	t.Selection = ui.RGBA(43, 111, 102, 0.22)
	t.Focus = ui.RGBA(43, 111, 102, 0.5)
}

func dark(t *ui.Theme) {
	t.Background = ui.Hex("#1d1c1a")
	t.Surface = ui.Hex("#262422")
	t.SurfaceHover = ui.Hex("#302d2a")
	t.SurfacePressed = ui.Hex("#3a3733")
	t.Border = ui.Hex("#36332f")
	t.Text = ui.Hex("#e4ded5")
	t.TextMuted = ui.Hex("#a09a91")
	t.Accent = ui.Hex("#6cbcae")
	t.AccentHover = ui.Hex("#86cbbf")
	t.AccentPressed = ui.Hex("#58a597")
	t.AccentText = ui.Hex("#10241f")
	t.Danger = ui.Hex("#fb923c")
	t.Warning = ui.Hex("#facc15")
	t.Success = ui.Hex("#4ade80")
	t.Selection = ui.RGBA(108, 188, 174, 0.3)
	t.Focus = ui.RGBA(108, 188, 174, 0.5)
}
