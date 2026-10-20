package theme

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/prefs"
)

func applied(t *testing.T, dark bool, up ui.Preferences) ui.Theme {
	t.Helper()
	return appliedWith(t, dark, up, prefs.Default())
}

func appliedWith(t *testing.T, dark bool, up ui.Preferences, own prefs.Preferences) ui.Theme {
	t.Helper()
	var got ui.Theme
	tt := ui.NewTester(func(c *ui.Context) {
		Apply(c, own)
		got = *c.Theme()
		ui.Text(c, "x")
	}, 200, 100)
	tt.SetDark(dark)
	tt.SetPreferences(up)
	tt.Frame()
	return got
}

func TestThemeChoice(t *testing.T) {
	for _, tc := range []struct {
		theme  string
		system bool
		want   bool
	}{{"", false, false}, {"", true, true}, {prefs.ThemeLight, true, false}, {prefs.ThemeDark, false, true}} {
		got := appliedWith(t, tc.system, ui.Preferences{}, prefs.Preferences{Theme: tc.theme})
		if got.Dark != tc.want || (got.Background == ui.Hex("#1d1c1a")) != tc.want {
			t.Errorf("theme %q on a dark system %v: dark %v, background %v", tc.theme, tc.system, got.Dark, got.Background)
		}
	}
}

func TestFontChoice(t *testing.T) {
	if got := applied(t, false, ui.Preferences{}); got.Font != "" {
		t.Errorf("default font %q", got.Font)
	}
	if got := appliedWith(t, false, ui.Preferences{}, prefs.Preferences{Font: "Georgia"}); got.Font != "Georgia" {
		t.Errorf("font %q", got.Font)
	}
}

func TestPalette(t *testing.T) {
	light := applied(t, false, ui.Preferences{})
	if light.Dark || light.Accent != ui.Hex("#2b6f66") || light.Background != ui.Hex("#f8f6f1") || light.Radius != 8 {
		t.Errorf("light: %+v", light)
	}
	dark := applied(t, true, ui.Preferences{})
	if !dark.Dark || dark.Accent != ui.Hex("#6cbcae") || dark.Background != ui.Hex("#1d1c1a") {
		t.Errorf("dark: %+v", dark)
	}
}

func TestKeepsTextScale(t *testing.T) {
	normal := applied(t, false, ui.Preferences{})
	large := applied(t, false, ui.Preferences{TextScale: 2})
	if large.FontSize <= normal.FontSize {
		t.Errorf("text scale lost: %v, %v", normal.FontSize, large.FontSize)
	}
}

func TestHighContrast(t *testing.T) {
	for _, dark := range []bool{false, true} {
		normal := applied(t, dark, ui.Preferences{})
		strong := applied(t, dark, ui.Preferences{HighContrast: true})
		if strong.Border == normal.Border || strong.TextMuted == normal.TextMuted || strong.Focus.A != 255 {
			t.Errorf("dark %v: high contrast changed nothing", dark)
		}
	}
}

func TestSoft(t *testing.T) {
	th := applied(t, false, ui.Preferences{})
	if s := Soft(&th, th.Accent); s == th.Accent || s == th.Background {
		t.Errorf("soft accent %v", s)
	}
}

func TestChosen(t *testing.T) {
	for _, dark := range []bool{false, true} {
		th := applied(t, dark, ui.Preferences{})
		if c := Chosen(&th); c.A == 0 || c.A == 255 || [3]uint8{c.R, c.G, c.B} != [3]uint8{th.Text.R, th.Text.G, th.Text.B} {
			t.Errorf("dark %v: chosen fill %v is not a tint of the ink", dark, c)
		}
	}
}

// contrast is the WCAG 2.1 contrast ratio of two opaque colors.
func contrast(a, b ui.Color) float64 {
	lum := func(c ui.Color) float64 {
		var sum float64
		for i, v := range []uint8{c.R, c.G, c.B} {
			x := float64(v) / 255
			if x <= 0.04045 {
				x /= 12.92
			} else {
				x = math.Pow((x+0.055)/1.055, 2.4)
			}
			sum += []float64{0.2126, 0.7152, 0.0722}[i] * x
		}
		return sum
	}
	x, y := lum(a)+0.05, lum(b)+0.05
	return math.Max(x, y) / math.Min(x, y)
}

func TestContrast(t *testing.T) {
	for _, dark := range []bool{false, true} {
		th := applied(t, dark, ui.Preferences{})
		for _, ground := range []ui.Color{th.Background, th.Surface, th.SurfaceHover} {
			for _, text := range []ui.Color{th.Text, th.TextMuted, th.Accent} {
				if r := contrast(text, ground); r < 4.5 {
					t.Errorf("dark %v: %v on %v is %.2f:1", dark, text, ground, r)
				}
			}
		}
		for _, face := range []ui.Color{th.Accent, th.AccentHover, th.AccentPressed} {
			if r := contrast(th.AccentText, face); r < 4.5 {
				t.Errorf("dark %v: accent text on %v is %.2f:1", dark, face, r)
			}
		}
		for _, status := range []ui.Color{th.Danger, th.Warning, th.Success} {
			if r := contrast(status, th.Background); r < 4.5 {
				t.Errorf("dark %v: status %v is %.2f:1", dark, status, r)
			}
		}
		// Ink on paper, not black on white: long reading wants body text near 13:1.
		if r := contrast(th.Text, th.Background); r > 14 {
			t.Errorf("dark %v: body text is %.2f:1, harsher than paper and ink", dark, r)
		}
	}
}

func TestHan(t *testing.T) {
	for text, want := range map[string]bool{"Oriel 发布": true, "Hello, world": false, "": false, "かな": false} {
		if Han(text) != want {
			t.Errorf("Han(%q) = %v", text, !want)
		}
	}
}

// fill renders a 100 square with the given fill over a red backdrop and returns the color at its center.
func fill(t *testing.T, up ui.Preferences, paint func(c *ui.Context, e ui.Element)) (ui.Theme, [4]uint8) {
	t.Helper()
	var got ui.Theme
	tt := ui.NewTester(func(c *ui.Context) {
		Apply(c, prefs.Default())
		got = *c.Theme()
		ui.Box(c).Fill().Background(ui.Hex("#ff0000")).Children(func() {
			paint(c, ui.Box(c).Width(100).Height(100))
		})
	}, 200, 200)
	tt.SetPreferences(up)
	tt.Frame()
	p := tt.Image().RGBAAt(50, 50)
	return got, [4]uint8{p.R, p.G, p.B, p.A}
}

func rgba(c ui.Color) [4]uint8 { return [4]uint8{c.R, c.G, c.B, c.A} }

func TestGlassOpaqueInHighContrast(t *testing.T) {
	for name, paint := range map[string]func(c *ui.Context, e ui.Element){
		"pane":    func(c *ui.Context, e ui.Element) { Pane(c, e) },
		"capsule": func(c *ui.Context, e ui.Element) { Capsule(c, e, false) },
	} {
		th, glass := fill(t, ui.Preferences{}, paint)
		if glass == rgba(th.Surface) {
			t.Errorf("%s: glass is opaque Surface without high contrast", name)
		}
		th, solid := fill(t, ui.Preferences{HighContrast: true}, paint)
		if solid != rgba(th.Surface) {
			t.Errorf("%s: high contrast fill %v, want Surface %v", name, solid, rgba(th.Surface))
		}
	}
}
