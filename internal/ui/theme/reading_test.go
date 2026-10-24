package theme

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/prefs"
)

// readingThemes returns the preset's theme in each scheme, with and without high contrast, made the way the reader
// makes it.
func readingThemes(t *testing.T, preset string, high bool) map[string]*ui.Theme {
	t.Helper()
	out := map[string]*ui.Theme{}
	for _, dark := range []bool{false, true} {
		var got *ui.Theme
		tt := ui.NewTester(func(c *ui.Context) {
			Apply(c, prefs.Default())
			got = ReadingTheme(c.Theme(), preset)
			ui.Text(c, "x")
		}, 200, 100)
		tt.SetDark(dark)
		tt.SetPreferences(ui.Preferences{HighContrast: high})
		tt.Frame()
		name := "light"
		if dark {
			name = "dark"
		}
		out[name] = got
	}
	return out
}

// TestReadingPresetContrast holds every documented pair of each preset and scheme to AA: 4.5:1 for text, 3:1 for
// graphics, borders and the focus ring (design/tokens.md, Reading appearance presets).
func TestReadingPresetContrast(t *testing.T) {
	for _, preset := range []string{prefs.PresetSepia, prefs.PresetMist} {
		for scheme, th := range readingThemes(t, preset, false) {
			name := preset + " " + scheme
			if presetOf(th) == nil {
				t.Fatalf("%s: not recognised as a reading theme", name)
			}
			sel := th.Selection.Over(th.Background)
			text := []struct {
				what         string
				fg, bg       ui.Color
				least        float64
				documentedAt float64
			}{
				{"Text on Background", th.Text, th.Background, 4.5, 11.6},
				{"Prose on Background", Prose(th), th.Background, 4.5, 9.6},
				{"Prose on Surface", Prose(th), th.Surface, 4.5, 8.6},
				{"Text2 on Background", Text2(th), th.Background, 4.5, 7.0},
				{"Text2 on Surface", Text2(th), th.Surface, 4.5, 6.3},
				{"TextMuted on Background", th.TextMuted, th.Background, 4.5, 5.2},
				{"TextMuted on Surface", th.TextMuted, th.Surface, 4.5, 4.7},
				{"Accent on Background", th.Accent, th.Background, 4.5, 5.7},
				{"Accent on Surface", th.Accent, th.Surface, 4.5, 5.2},
				{"AccentText on Accent", th.AccentText, th.Accent, 4.5, 6.8},
				{"AccentText on AccentHover", th.AccentText, th.AccentHover, 4.5, 0},
				{"AccentText on AccentPressed", th.AccentText, th.AccentPressed, 4.5, 0},
				{"Danger on Background", th.Danger, th.Background, 4.5, 5.2},
				{"Prose on Selection", Prose(th), sel, 4.5, 4.8},
				{"Text on RaisedFill", th.Text, RaisedFill(th), 4.5, 0},
				{"Text on Track", th.Text, Track(th), 4.5, 0},
			}
			for _, c := range text {
				r := contrast(c.fg, c.bg)
				if r < c.least {
					t.Errorf("%s: %s is %.2f:1, text needs %.1f:1", name, c.what, r, c.least)
				}
				if c.documentedAt > 0 && r < c.documentedAt-0.05 {
					t.Errorf("%s: %s is %.2f:1, less than the documented %.1f:1", name, c.what, r, c.documentedAt)
				}
			}
			for _, g := range []struct {
				what   string
				fg, bg ui.Color
			}{
				{"Warning on Background", th.Warning, th.Background},
				{"Success on Background", th.Success, th.Background},
				{"Star on Background", Star(th), th.Background},
				{"Accent on Selection", th.Accent, sel},
				{"focus ring on Background", th.Focus.Over(th.Background), th.Background},
				{"focus ring on Surface", th.Focus.Over(th.Surface), th.Surface},
			} {
				if r := contrast(g.fg, g.bg); r < 3 {
					t.Errorf("%s: %s is %.2f:1, graphics need 3:1", name, g.what, r)
				}
			}
		}
		for scheme, th := range readingThemes(t, preset, true) {
			name := preset + " " + scheme + " high contrast"
			if r := contrast(th.TextMuted, th.Background); r < 4.5 {
				t.Errorf("%s: TextMuted on Background is %.2f:1", name, r)
			}
			if r := contrast(th.TextMuted, th.Surface); r < 4.5 {
				t.Errorf("%s: TextMuted on Surface is %.2f:1", name, r)
			}
			if r := contrast(th.Border.Over(th.Background), th.Background); r < 3 {
				t.Errorf("%s: Border on Background is %.2f:1, borders need 3:1", name, r)
			}
			if r := contrast(th.Focus, th.Background); r < 3 {
				t.Errorf("%s: focus ring is %.2f:1", name, r)
			}
		}
	}
}

// TestReadingThemeOnlyRecolors checks that a preset swaps paper and ink and keeps metrics and scheme, that the
// theme that follows the theme, and any value it does not know, is the main theme itself, and that a reading theme
// is its own.
func TestReadingThemeOnlyRecolors(t *testing.T) {
	for _, dark := range []bool{false, true} {
		main := applied(t, dark, ui.Preferences{})
		for _, none := range []string{"", "solarized", "SEPIA"} {
			if got := ReadingTheme(&main, none); got != &main {
				t.Errorf("dark %v: preset %q changed the theme", dark, none)
			}
		}
		got := ReadingTheme(&main, prefs.PresetSepia)
		if got.Dark != main.Dark || got.Font != main.Font || got.Radius != main.Radius || got.Warning != main.Warning || got.Success != main.Success {
			t.Errorf("dark %v: the preset changed more than paper and ink", dark)
		}
		if got.Background == main.Background || got.Text == main.Text || got.Accent == main.Accent {
			t.Errorf("dark %v: the preset left the theme's colours", dark)
		}
		if again := ReadingTheme(got, prefs.PresetMist); again != got {
			t.Errorf("dark %v: a reading theme was recoloured", dark)
		}
		if Ink(got, 0.5) != got.Text.Alpha(0.5) {
			t.Errorf("dark %v: ink is not the preset's text", dark)
		}
	}
	main := applied(t, false, ui.Preferences{})
	pane := PaneTheme(&main)
	if ReadingTheme(pane, prefs.PresetSepia) != pane {
		t.Error("the ink pane was recoloured")
	}
}
