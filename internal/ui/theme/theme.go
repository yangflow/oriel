// Package theme sets Oriel's palette on the MyGo theme: warm neutrals and a teal accent, light and dark. The values
// are the color table in design/tokens.md.
package theme

import "github.com/egoist/mygo/ui"

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
	t.Background = ui.Hex("#1b1a18")
	t.Surface = ui.Hex("#25231f")
	t.SurfaceHover = ui.Hex("#2f2c28")
	t.SurfacePressed = ui.Hex("#3a3732")
	t.Border = ui.Hex("#3d3933")
	t.Text = ui.Hex("#ede9e3")
	t.TextMuted = ui.Hex("#a8a29e")
	t.Accent = ui.Hex("#2dd4bf")
	t.AccentHover = ui.Hex("#5eead4")
	t.AccentPressed = ui.Hex("#14b8a6")
	t.AccentText = ui.Hex("#0b2522")
	t.Danger = ui.Hex("#fb923c")
	t.Warning = ui.Hex("#facc15")
	t.Success = ui.Hex("#4ade80")
	t.Selection = ui.RGBA(45, 212, 191, 0.32)
	t.Focus = ui.RGBA(45, 212, 191, 0.55)
}
