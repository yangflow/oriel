package theme

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/prefs"
	"github.com/yangflow/oriel/internal/ui/theme/tokens"
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
	for _, dark := range []bool{false, true} {
		th := applied(t, dark, ui.Preferences{})
		s := &tokens.SchemeLight
		if dark {
			s = &tokens.SchemeDark
		}
		for name, c := range map[string][2]ui.Color{
			"Background": {th.Background, s.Paper}, "Surface": {th.Surface, s.Fill},
			"Text": {th.Text, s.Fg}, "TextMuted": {th.TextMuted, s.Muted}, "Accent": {th.Accent, s.Accent},
			"Danger": {th.Danger, s.Danger}, "Inverse": {th.Inverse, s.Inverse}, "Border": {th.Border, s.Line},
			"Text2": {Text2(&th), s.Fg2}, "Prose": {Prose(&th), s.Prose}, "Surface()": {Surface(&th), s.Surface},
			"Track": {Track(&th), s.Fill}, "RaisedFill": {RaisedFill(&th), s.Float}, "LineStrong": {LineStrong(&th), s.Line2},
			"Disabled": {Disabled(&th), s.Faint}, "Star": {Star(&th), s.Star}, "Chosen": {Chosen(&th), s.Selected},
			"Ink": {Ink(&th, 1), ui.RGB(s.Fill.R, s.Fill.G, s.Fill.B)}, "Marker": {Marker(&th), s.Accent},
			"Soft": {Soft(&th, th.Accent), s.Soft},
		} {
			if c[0] != c[1] {
				t.Errorf("dark %v: %s %v, want %v", dark, name, c[0], c[1])
			}
		}
		if th.Dark != dark || th.Radius != 8 {
			t.Errorf("dark %v: %+v", dark, th)
		}
	}
	// The light values the design system's audit was signed off with.
	light := applied(t, false, ui.Preferences{})
	if Text2(&light) != ui.Hex("#4c4840") || light.TextMuted != ui.Hex("#6a645a") || light.Danger != ui.Hex("#a34b36") {
		t.Error("light palette is not the final design's")
	}
}

func TestPaneTheme(t *testing.T) {
	for _, dark := range []bool{false, true} {
		s := &tokens.SchemeLight
		if dark {
			s = &tokens.SchemeDark
		}
		main := applied(t, dark, ui.Preferences{})
		p := PaneTheme(&main)
		ink := ui.RGB(s.PaneFill.R, s.PaneFill.G, s.PaneFill.B)
		if !p.Dark || p.Background != s.Pane || p.Text != s.PaneFg || p.TextMuted != s.PaneMuted || p.Accent != s.PaneAccent {
			t.Errorf("dark %v: pane %+v", dark, p)
		}
		if Text2(p) != s.PaneFg2 || Ink(p, 1) != ink || Disabled(p) != s.PaneFaint {
			t.Errorf("dark %v: pane Text2 %v, ink %v", dark, Text2(p), Ink(p, 1))
		}
		if p.Warning != tokens.SchemeDark.Warning || p.Success != tokens.SchemeDark.Success ||
			p.Inverse != tokens.SchemeDark.Inverse || p.Surface != s.PaneFill || p.Border != s.PaneLine ||
			p.Scrollbar != ink.Alpha(0.3) {
			t.Errorf("dark %v: pane roles %+v", dark, p)
		}
		if Track(p) != s.PaneFill || Chosen(p) != s.PaneSelected || p.SurfaceHover != s.PaneHover ||
			p.SurfacePressed != s.PanePressed || Star(p) != tokens.SchemeDark.Star || Marker(p) != p.Accent {
			t.Errorf("dark %v: pane helpers", dark)
		}
		if PaneTheme(p) != p {
			t.Errorf("dark %v: PaneTheme of the pane theme is not itself", dark)
		}
		if main.Background == p.Background || main.Dark != dark {
			t.Errorf("dark %v: PaneTheme changed main", dark)
		}
	}
}

// TestStateFillsStep holds the state ladder in order: hover under selected under pressed, so a hovered chosen row
// still reads as chosen and pressing it still shows.
func TestStateFillsStep(t *testing.T) {
	for _, high := range []bool{false, true} {
		for name, th := range map[string]ui.Theme{
			"light": applied(t, false, ui.Preferences{HighContrast: high}),
			"dark":  applied(t, true, ui.Preferences{HighContrast: high}),
		} {
			base := th
			pane := PaneTheme(&base)
			for _, tt := range []*ui.Theme{&base, pane} {
				f, h, c, p := Track(tt).A, tt.SurfaceHover.A, Chosen(tt).A, tt.SurfacePressed.A
				if !(f < h && h < c && c < p) {
					t.Errorf("%s high %v: fills %d, %d, %d, %d do not step", name, high, f, h, c, p)
				}
			}
		}
	}
	strong := applied(t, false, ui.Preferences{HighContrast: true})
	hc := tokens.HighContrast
	if Track(&strong) != Ink(&strong, hc.Fill) || strong.SurfaceHover != Ink(&strong, hc.Hover) ||
		Chosen(&strong) != Ink(&strong, hc.Selected) || strong.SurfacePressed != Ink(&strong, hc.Pressed) {
		t.Error("high contrast fills are not the high contrast ladder")
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
		for _, ground := range []ui.Color{th.Background, Surface(&th)} {
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
		if r := contrast(pane.Accent, pane.Background); r < 3 {
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
			"main": {&th, []ui.Color{th.Background, Surface(&th)}},
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
			for _, ground := range []ui.Color{th.Background, Surface(&th)} {
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
				if r := contrast(text, Track(&th).Over(th.Background)); r < 4.5 {
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
	if glass == rgba(Surface(&th)) {
		t.Error("capsule is opaque Surface without high contrast")
	}
	th, solid := fill(t, ui.Preferences{HighContrast: true}, paint)
	if solid != rgba(Surface(&th)) {
		t.Errorf("high contrast fill %v, want Surface %v", solid, rgba(Surface(&th)))
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
}

func TestSheet(t *testing.T) {
	for _, tc := range []struct {
		name       string
		dark, high bool
	}{{"light", false, false}, {"dark", true, false}, {"light high", false, true}, {"dark high", true, true}} {
		th := applied(t, tc.dark, ui.Preferences{HighContrast: tc.high})
		pop, dlg := SheetFor(&th, tc.high, Popup), SheetFor(&th, tc.high, Dialog)
		if pop.Radius != tokens.RadiusCard || dlg.Radius != tokens.RadiusPane || pop.Shadow.Blur >= dlg.Shadow.Blur ||
			pop.Shadow.Y >= dlg.Shadow.Y {
			t.Errorf("%s: popup %+v, dialog %+v", tc.name, pop, dlg)
		}
		e := tokens.ElevationLight
		if tc.dark {
			e = tokens.ElevationDark
		}
		switch {
		case tc.high:
			if dlg.Shadow.Rim != 1 || dlg.Shadow.RimColor != th.Border {
				t.Errorf("%s: rim %v %v", tc.name, dlg.Shadow.Rim, dlg.Shadow.RimColor)
			}
		case dlg.Shadow != e.Dialog || pop.Shadow != e.Popup:
			t.Errorf("%s: shadows %+v, %+v", tc.name, pop.Shadow, dlg.Shadow)
		}
	}
	light, dark := applied(t, false, ui.Preferences{}), applied(t, true, ui.Preferences{})
	for _, tc := range []struct {
		th   *ui.Theme
		high bool
		want ui.Color
	}{{&light, false, tokens.SchemeLight.Scrim}, {&dark, false, tokens.SchemeDark.Scrim},
		{&light, true, ui.RGBA(28, 24, 18, 0.5)}, {&dark, true, ui.RGBA(0, 0, 0, 0.6)}} {
		if got := ScrimFor(tc.th, tc.high); got != tc.want {
			t.Errorf("dark %v high %v: scrim %v, want %v", tc.th.Dark, tc.high, got, tc.want)
		}
	}
}
