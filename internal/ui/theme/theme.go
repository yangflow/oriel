// Package theme sets Oriel's palette on the MyGo theme: warm neutrals and a teal accent, light and dark. The values
// are the color table in design/tokens.md.
package theme

import (
	"unicode"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// Bar is the height of the toolbar row each screen floats over its content, in DIPs: the window buttons sit
// centered in it, and content scrolls under it starting this far down.
const Bar = 52

// Apply replaces the frame's theme with Oriel's palette for the system appearance. Call it first in the root view of
// every window, each frame. Metrics stay the theme's, so the desktop's text size still applies, and the colors are
// strengthened again where the desktop asks for high contrast.
func Apply(c *ui.Context) {
	t := *c.Theme()
	if t.Dark {
		dark(&t)
	} else {
		light(&t)
	}
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

// Chosen is the fill of the place you are in a navigation list, the accent at 12% (20% in dark); its icon and count
// are in the accent.
func Chosen(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.Accent.Alpha(0.2)
	}
	return t.Accent.Alpha(0.12)
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
	t.Background = ui.Hex("#fbfaf7")
	t.Surface = ui.Hex("#f2f0eb")
	t.SurfaceHover = ui.Hex("#e9e6df")
	t.SurfacePressed = ui.Hex("#dfdbd2")
	t.Border = ui.Hex("#dcd7cd")
	t.Text = ui.Hex("#1f1d1a")
	t.TextMuted = ui.Hex("#6f6a64")
	t.Accent = ui.Hex("#0f766e")
	t.AccentHover = ui.Hex("#115e59")
	t.AccentPressed = ui.Hex("#134e4a")
	t.AccentText = ui.Hex("#ffffff")
	t.Danger = ui.Hex("#c2410c")
	t.Warning = ui.Hex("#a16207")
	t.Success = ui.Hex("#15803d")
	t.Selection = ui.RGBA(15, 118, 110, 0.22)
	t.Focus = ui.RGBA(15, 118, 110, 0.5)
}

func dark(t *ui.Theme) {
	t.Background = ui.Hex("#161513")
	t.Surface = ui.Hex("#25231f")
	t.SurfaceHover = ui.Hex("#2f2c28")
	t.SurfacePressed = ui.Hex("#3a3732")
	t.Border = ui.Hex("#3d3933")
	t.Text = ui.Hex("#ede9e3")
	t.TextMuted = ui.Hex("#a8a29e")
	t.Accent = ui.Hex("#4fbfad")
	t.AccentHover = ui.Hex("#6dd0c0")
	t.AccentPressed = ui.Hex("#3aa596")
	t.AccentText = ui.Hex("#0b2522")
	t.Danger = ui.Hex("#fb923c")
	t.Warning = ui.Hex("#facc15")
	t.Success = ui.Hex("#4ade80")
	t.Selection = ui.RGBA(79, 191, 173, 0.3)
	t.Focus = ui.RGBA(79, 191, 173, 0.5)
}
