package theme

import (
	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/ui/theme/tokens"
)

// preset is one variant of a reading appearance preset: its tokens and what the helpers return for it.
type preset struct {
	dark   bool
	p      *tokens.Preset
	colors colors
}

// readingPresets holds each preset's light and dark variant by name, from tokens.Presets.
var readingPresets = func() map[string][2]*preset {
	m := map[string][2]*preset{}
	for name, variants := range tokens.Presets {
		light, dark := variants[0], variants[1]
		m[name] = [2]*preset{
			{false, &light, presetColors(&light, &tokens.SchemeLight)},
			{true, &dark, presetColors(&dark, &tokens.SchemeDark)},
		}
	}
	return m
}()

// presetOf returns the preset whose page t has, or nil for a theme that is not a reading theme.
func presetOf(t *ui.Theme) *preset {
	for _, variants := range readingPresets {
		for _, v := range variants {
			if t.Background == v.p.Page {
				return v
			}
		}
	}
	return nil
}

// ReadingTheme returns the theme the reader draws with for a reading appearance preset (prefs.PresetSepia or
// prefs.PresetMist): main with the page, ink and links of the preset's light or dark variant, following main's
// scheme. Any other preset, the empty one that follows the theme included, gives main itself. Metrics and the font
// stay main's, and high contrast is read from main as PaneTheme does. The helpers of this package called with it
// return the preset's values. Call c.SetTheme with it before the reader's elements are created and set main back
// after; given a reading theme, it returns it unchanged.
func ReadingTheme(main *ui.Theme, name string) *ui.Theme {
	if presetOf(main) != nil {
		return main
	}
	variants, ok := readingPresets[name]
	if !ok || isPane(main) {
		return main
	}
	v := variants[0]
	s := &tokens.SchemeLight
	if main.Dark {
		v, s = variants[1], &tokens.SchemeDark
	}
	p := v.p
	t := *main
	t.Background, t.Surface, t.Text, t.TextMuted = p.Page, v.colors.fill, p.Fg, p.Muted
	t.Accent, t.AccentText = p.Accent, p.OnAccent
	t.AccentHover, t.AccentPressed = p.Accent.Mix(p.Fg, 0.15), p.Accent.Mix(p.Fg, 0.3)
	t.Danger, t.Success, t.Warning = p.Danger, p.Success, p.Warning
	t.SurfaceHover = p.Ink.Alpha(alphaOf(s.Hover))
	t.SurfacePressed = p.Ink.Alpha(alphaOf(s.Pressed))
	t.Border = p.Ink.Alpha(alphaOf(s.Line))
	t.Selection = p.Accent.Alpha(alphaOf(s.Selection))
	// The presets' grounds are darker than paper, so the ring is a step stronger to keep 3:1 on their surface.
	t.Focus = p.Accent.Alpha(0.8)
	t.Scrollbar = p.Ink.Alpha(alphaOf(s.Scrollbar))
	if highContrast(main) {
		t.SurfaceHover = p.Ink.Alpha(tokens.HighContrast.Hover)
		t.SurfacePressed = p.Ink.Alpha(tokens.HighContrast.Pressed)
		strengthen(&t)
	}
	return &t
}
