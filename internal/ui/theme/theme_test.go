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
		if got.Dark != tc.want || (got.Background == ui.Hex("#171918")) != tc.want {
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
	for name, c := range map[string][2]ui.Color{
		"Background": {light.Background, ui.Hex("#fdfcfa")}, "Surface": {light.Surface, ui.Hex("#f6f3ee")},
		"Text": {light.Text, ui.Hex("#25221e")}, "TextMuted": {light.TextMuted, ui.Hex("#736d62")},
		"Accent": {light.Accent, ui.Hex("#2a6d65")}, "Danger": {light.Danger, ui.Hex("#b4533c")},
		"Inverse": {light.Inverse, ui.Hex("#2a2620")}, "Text2": {Text2(&light), ui.Hex("#58534a")},
		"Prose": {Prose(&light), ui.Hex("#3a362f")}, "Track": {Track(&light), ui.Hex("#ece8e0")},
		"RaisedFill": {RaisedFill(&light), ui.Hex("#ffffff")}, "LineStrong": {LineStrong(&light), ui.Hex("#d4cec2")},
		"Disabled": {Disabled(&light), ui.Hex("#b3ada1")}, "Star": {Star(&light), ui.Hex("#b98a22")},
		"Ink": {Ink(&light, 1), ui.RGB(37, 34, 30)}, "Marker": {Marker(&light), ui.Hex("#2a6d65")},
	} {
		if c[0] != c[1] {
			t.Errorf("light %s %v, want %v", name, c[0], c[1])
		}
	}
	if light.Dark || light.Radius != 8 {
		t.Errorf("light: %+v", light)
	}
	dark := applied(t, true, ui.Preferences{})
	for name, c := range map[string][2]ui.Color{
		"Background": {dark.Background, ui.Hex("#171918")}, "Surface": {dark.Surface, ui.Hex("#1d1f1e")},
		"Text": {dark.Text, ui.Hex("#dde1df")}, "TextMuted": {dark.TextMuted, ui.Hex("#8a918f")},
		"Accent": {dark.Accent, ui.Hex("#78beb1")}, "Inverse": {dark.Inverse, ui.Hex("#333837")},
		"Text2": {Text2(&dark), ui.Hex("#aab0ae")}, "Prose": {Prose(&dark), ui.Hex("#c3c9c7")},
		"Track": {Track(&dark), ui.Hex("#2c2f2e")}, "RaisedFill": {RaisedFill(&dark), ui.Hex("#252827")},
		"LineStrong": {LineStrong(&dark), ui.Hex("#3d4241")}, "Disabled": {Disabled(&dark), ui.Hex("#565b59")},
		"Star": {Star(&dark), ui.Hex("#e2b24f")}, "Ink": {Ink(&dark, 1), ui.RGB(234, 240, 238)},
	} {
		if c[0] != c[1] {
			t.Errorf("dark %s %v, want %v", name, c[0], c[1])
		}
	}
	if !dark.Dark {
		t.Error("dark is not dark")
	}
}

func TestPaneTheme(t *testing.T) {
	for _, tc := range []struct {
		dark                   bool
		bg, text, text2, muted string
		ink                    ui.Color
		rim                    float32
	}{
		{false, "#37332e", "#ece7dd", "#cdc7bb", "#a49e91", ui.RGB(255, 248, 235), 0.1},
		{true, "#1a2422", "#e8eeec", "#cbd5d2", "#93a39f", ui.RGB(235, 248, 244), 0.08},
	} {
		main := applied(t, tc.dark, ui.Preferences{})
		p := PaneTheme(&main)
		if !p.Dark || p.Background != ui.Hex(tc.bg) || p.Text != ui.Hex(tc.text) || p.TextMuted != ui.Hex(tc.muted) {
			t.Errorf("dark %v: pane %+v", tc.dark, p)
		}
		if Text2(p) != ui.Hex(tc.text2) || Ink(p, 1) != tc.ink || Ink(p, 0.5) != tc.ink.Alpha(0.5) {
			t.Errorf("dark %v: pane Text2 %v, ink %v", tc.dark, Text2(p), Ink(p, 1))
		}
		if p.Accent != ui.Hex("#7cc9bb") || p.Warning != ui.Hex("#d6aa55") || p.Success != ui.Hex("#8ab997") ||
			p.Surface != tc.ink.Alpha(0.11) || p.Border != tc.ink.Alpha(0.08) || p.Scrollbar != tc.ink.Alpha(0.3) ||
			p.Inverse != ui.Hex("#333837") {
			t.Errorf("dark %v: pane roles %+v", tc.dark, p)
		}
		if Track(p) != tc.ink.Alpha(0.11) || Chosen(p) != tc.ink.Alpha(0.12) || Soft(p, p.Accent) != p.Accent.Alpha(0.12) ||
			Star(p) != ui.Hex("#e2b24f") || Marker(p) != p.Accent || p.SurfaceHover != tc.ink.Alpha(0.055) ||
			p.SurfacePressed != tc.ink.Alpha(0.085) {
			t.Errorf("dark %v: pane helpers", tc.dark)
		}
		if PaneTheme(p) != p {
			t.Errorf("dark %v: PaneTheme of the pane theme is not itself", tc.dark)
		}
		if main.Background == p.Background || main.Dark != tc.dark {
			t.Errorf("dark %v: PaneTheme changed main", tc.dark)
		}
	}
}

func TestStateFillsStep(t *testing.T) {
	for _, high := range []bool{false, true} {
		for name, th := range map[string]ui.Theme{
			"light": applied(t, false, ui.Preferences{HighContrast: high}),
			"dark":  applied(t, true, ui.Preferences{HighContrast: high}),
		} {
			base := th
			pane := PaneTheme(&base)
			for _, tt := range []*ui.Theme{&base, pane} {
				h, p, c := tt.SurfaceHover.A, tt.SurfacePressed.A, Chosen(tt).A
				if !(h < p && p < c) {
					t.Errorf("%s high %v: fills %d, %d, %d do not step", name, high, h, p, c)
				}
			}
		}
	}
	light := applied(t, false, ui.Preferences{})
	if light.SurfaceHover != Ink(&light, 0.03) || light.SurfacePressed != Ink(&light, 0.05) ||
		Chosen(&light) != Ink(&light, 0.065) || light.Border != Ink(&light, 0.075) {
		t.Error("light fills are not 3, 5 and 6.5% with a 7.5% hairline")
	}
	dark := applied(t, true, ui.Preferences{})
	if dark.SurfaceHover != Ink(&dark, 0.04) || dark.SurfacePressed != Ink(&dark, 0.06) || Chosen(&dark) != Ink(&dark, 0.08) {
		t.Error("dark fills are not 4, 6 and 8%")
	}
	strong := applied(t, false, ui.Preferences{HighContrast: true})
	if strong.SurfaceHover != Ink(&strong, 0.08) || strong.SurfacePressed != Ink(&strong, 0.12) || Chosen(&strong) != Ink(&strong, 0.16) {
		t.Error("high contrast fills are not 8, 12 and 16%")
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
		if strong.Border != strong.Text.Alpha(0.55) || strong.TextMuted != normal.TextMuted.Mix(normal.Text, 0.4) ||
			strong.Focus.A != 255 {
			t.Errorf("dark %v: high contrast border %v, muted %v, focus %v", dark, strong.Border, strong.TextMuted, strong.Focus)
		}
		if r := contrast(strong.Text, strong.Background); r < 4.5 {
			t.Errorf("dark %v: text %.2f:1", dark, r)
		}
		if r := contrast(strong.TextMuted, strong.Background); r < contrast(normal.TextMuted, normal.Background) {
			t.Errorf("dark %v: muted text lost contrast", dark)
		}
		pn, ps := PaneTheme(&normal), PaneTheme(&strong)
		if ps.Border == pn.Border || ps.TextMuted == pn.TextMuted || ps.Focus.A != 255 || pn.Focus.A == 255 {
			t.Errorf("dark %v: pane high contrast changed nothing", dark)
		}
		if r := contrast(ps.TextMuted, ps.Background); r < 4.5 {
			t.Errorf("dark %v: pane muted text %.2f:1", dark, r)
		}
	}
}

func TestSoft(t *testing.T) {
	th := applied(t, false, ui.Preferences{})
	if s := Soft(&th, th.Accent); s != th.Accent.Alpha(0.09) {
		t.Errorf("soft accent %v", s)
	}
	th = applied(t, true, ui.Preferences{})
	if s := Soft(&th, th.Accent); s != th.Accent.Alpha(0.12) {
		t.Errorf("dark soft accent %v", s)
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
		for _, ground := range []ui.Color{th.Background, th.Surface} {
			for _, text := range []ui.Color{th.Text, Text2(&th), Prose(&th), th.TextMuted, th.Accent} {
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
		for _, status := range []ui.Color{th.Danger, th.Success} {
			if r := contrast(status, th.Background); r < 4.5 {
				t.Errorf("dark %v: status %v is %.2f:1", dark, status, r)
			}
		}
		for _, graphic := range []ui.Color{th.Warning, Star(&th)} {
			if r := contrast(graphic, th.Background); r < 3 {
				t.Errorf("dark %v: graphic %v is %.2f:1", dark, graphic, r)
			}
		}
		// Ink on paper, not black on white: titles stay below the harshness of 16:1.
		if r := contrast(th.Text, th.Background); r > 16 {
			t.Errorf("dark %v: body text is %.2f:1, harsher than paper and ink", dark, r)
		}
		pane := PaneTheme(&th)
		for _, text := range []ui.Color{pane.Text, Text2(pane), pane.TextMuted} {
			if r := contrast(text, pane.Background); r < 4.5 {
				t.Errorf("dark %v: pane text %v is %.2f:1", dark, text, r)
			}
		}
		if r := contrast(pane.Accent, pane.Background); r < 4.5 {
			t.Errorf("dark %v: pane accent is %.2f:1", dark, r)
		}
		sel := Chosen(pane).Over(pane.Background)
		for _, text := range []ui.Color{pane.Text, Text2(pane)} {
			if r := contrast(text, sel); r < 4.5 {
				t.Errorf("dark %v: pane text %v on the selected fill is %.2f:1", dark, text, r)
			}
		}
		for _, graphic := range []ui.Color{pane.Accent, pane.Warning, pane.Success} {
			if r := contrast(graphic, sel); r < 3 {
				t.Errorf("dark %v: pane graphic %v on the selected fill is %.2f:1", dark, graphic, r)
			}
		}
	}
}

// TestFocusRing holds the keyboard ring to 3:1, the bar for control boundaries, over every ground it is drawn on.
func TestFocusRing(t *testing.T) {
	for _, dark := range []bool{false, true} {
		th := applied(t, dark, ui.Preferences{})
		pane := PaneTheme(&th)
		for name, tc := range map[string]struct {
			theme   *ui.Theme
			grounds []ui.Color
		}{
			"main": {&th, []ui.Color{th.Background, th.Surface}},
			"pane": {pane, []ui.Color{pane.Background}},
		} {
			for _, ground := range tc.grounds {
				if r := contrast(tc.theme.Focus.Over(ground), ground); r < 3 {
					t.Errorf("dark %v: %s focus ring on %v is %.2f:1", dark, name, ground, r)
				}
			}
		}
	}
}

// TestFillContrast holds the text drawn on a Track and on the soft fills of a result box to 4.5:1, in light, dark and
// with high contrast. TextMuted is left out on purpose: it does not hold 4.5:1 on a Track.
func TestFillContrast(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, high := range []bool{false, true} {
			th := applied(t, dark, ui.Preferences{HighContrast: high})
			for _, ground := range []ui.Color{th.Background, th.Surface} {
				softs := map[string]ui.Color{
					"Soft(Danger)":  Soft(&th, th.Danger).Over(ground),
					"Soft(Warning)": Soft(&th, th.Warning).Over(ground),
					"plain box":     Ink(&th, 0.045).Over(ground),
				}
				for name, fill := range softs {
					for _, text := range []ui.Color{th.Text, Text2(&th)} {
						if r := contrast(text, fill); r < 4.5 {
							t.Errorf("dark %v high %v: %v on %s over %v is %.2f:1", dark, high, text, name, ground, r)
						}
					}
				}
				if r := contrast(th.Danger, softs["Soft(Danger)"]); r < 3 {
					t.Errorf("dark %v high %v: Danger icon on Soft(Danger) over %v is %.2f:1", dark, high, ground, r)
				}
			}
			for _, text := range []ui.Color{th.Text, Text2(&th)} {
				if r := contrast(text, Track(&th)); r < 4.5 {
					t.Errorf("dark %v high %v: %v on Track is %.2f:1", dark, high, text, r)
				}
			}
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

func TestPaneOpaque(t *testing.T) {
	paint := func(c *ui.Context, e ui.Element) { Pane(c, e) }
	for _, high := range []bool{false, true} {
		th, got := fill(t, ui.Preferences{HighContrast: high}, paint)
		if want := PaneTheme(&th).Background; got != rgba(want) {
			t.Errorf("high contrast %v: pane fill %v, want %v", high, got, rgba(want))
		}
	}
}

func TestCapsuleOpaqueInHighContrast(t *testing.T) {
	paint := func(c *ui.Context, e ui.Element) { Capsule(c, e, false) }
	th, glass := fill(t, ui.Preferences{}, paint)
	if glass == rgba(th.Surface) {
		t.Error("capsule is opaque Surface without high contrast")
	}
	th, solid := fill(t, ui.Preferences{HighContrast: true}, paint)
	if solid != rgba(th.Surface) {
		t.Errorf("high contrast fill %v, want Surface %v", solid, rgba(th.Surface))
	}
}

func TestHanLanguage(t *testing.T) {
	for code, want := range map[string][2]bool{
		"zh-Hans": {true, true}, "zh-Hant": {true, true}, "zh": {true, true}, "ja": {true, true},
		"en": {false, true}, "de": {false, true}, "kor": {false, true}, "": {false, false},
	} {
		if han, known := HanLanguage(code); han != want[0] || known != want[1] {
			t.Errorf("%q: han %v known %v", code, han, known)
		}
	}
}

func TestMonogramSizes(t *testing.T) {
	for _, tc := range []struct {
		name string
		size float32
		want string
	}{
		{"go.dev", 15, "G"},
		{"@alice", 18, "A"},
		{"/r/golang", 32, "R"},
		{"", 15, "·"},
	} {
		tt := ui.NewTester(func(c *ui.Context) { Monogram(c, tc.name, tc.size) }, 100, 100)
		tt.Frame()
		if !tt.HasText(tc.want) {
			t.Fatalf("%q at %v has no letter %q", tc.name, tc.size, tc.want)
		}
	}
	if GlyphRadius(15) != 4 || GlyphRadius(18) != 5 || GlyphRadius(32) != 8 {
		t.Fatal("glyph radii")
	}
}

func TestSheet(t *testing.T) {
	for _, tc := range []struct {
		name       string
		dark, high bool
	}{{"light", false, false}, {"dark", true, false}, {"light high", false, true}, {"dark high", true, true}} {
		th := applied(t, tc.dark, ui.Preferences{HighContrast: tc.high})
		pop, dlg := SheetFor(&th, tc.high, Popup), SheetFor(&th, tc.high, Dialog)
		if pop.Radius != 12 || dlg.Radius != 16 || pop.Blur >= dlg.Blur || pop.Offset >= dlg.Offset {
			t.Errorf("%s: popup %+v, dialog %+v", tc.name, pop, dlg)
		}
		switch {
		case tc.high:
			if dlg.RimWidth != 1 || dlg.Rim != th.Border {
				t.Errorf("%s: rim %v %v", tc.name, dlg.RimWidth, dlg.Rim)
			}
		case tc.dark:
			if dlg.RimWidth != 1 || dlg.Rim != ui.RGBA(255, 255, 255, 0.08) || dlg.Shadow != ui.RGBA(0, 0, 0, 0.55) {
				t.Errorf("%s: rim %v %v, shadow %v", tc.name, dlg.RimWidth, dlg.Rim, dlg.Shadow)
			}
		default:
			if dlg.RimWidth != 0.5 || dlg.Rim != Ink(&th, 0.12) || dlg.Shadow != ui.RGBA(28, 24, 18, 0.25) {
				t.Errorf("%s: rim %v %v, shadow %v", tc.name, dlg.RimWidth, dlg.Rim, dlg.Shadow)
			}
		}
	}
	light, dark := applied(t, false, ui.Preferences{}), applied(t, true, ui.Preferences{})
	for _, tc := range []struct {
		th   *ui.Theme
		high bool
		want ui.Color
	}{{&light, false, ui.RGBA(28, 24, 18, 0.32)}, {&dark, false, ui.RGBA(0, 0, 0, 0.45)},
		{&light, true, ui.RGBA(28, 24, 18, 0.5)}, {&dark, true, ui.RGBA(0, 0, 0, 0.6)}} {
		if got := ScrimFor(tc.th, tc.high); got != tc.want {
			t.Errorf("dark %v high %v: scrim %v, want %v", tc.th.Dark, tc.high, got, tc.want)
		}
	}
}
