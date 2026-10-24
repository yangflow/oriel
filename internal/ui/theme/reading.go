package theme

import (
	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/prefs"
)

// palette is the paper and ink of one variant of a reading appearance preset; the values are in design/tokens.md,
// Reading appearance presets. Everything else of the reader's theme is derived from them, as Apply does.
type palette struct {
	dark                                                                            bool
	background, surface, text, prose, text2, muted                                  ui.Color
	accent, accentHover, accentPressed, accentText, danger, star, track, raisedFill ui.Color
}

// readingPalettes holds the four variants: the preset, then the app's scheme.
var readingPalettes = map[string][2]palette{
	prefs.PresetSepia: {
		{false, ui.Hex("#f4ecd8"), ui.Hex("#ebe1c8"), ui.Hex("#33291d"), ui.Hex("#44382a"), ui.Hex("#5a4c39"), ui.Hex("#6f5f48"),
			ui.Hex("#1f6258"), ui.Hex("#22584e"), ui.Hex("#255146"), ui.Hex("#ffffff"), ui.Hex("#a2452f"), ui.Hex("#a07414"), ui.Hex("#e6dcc2"), ui.Hex("#fbf6e9")},
		{true, ui.Hex("#2a241b"), ui.Hex("#332c21"), ui.Hex("#eadfc8"), ui.Hex("#d8cbb0"), ui.Hex("#bcae92"), ui.Hex("#a79a7c"),
			ui.Hex("#8cc9b8"), ui.Hex("#9acfc1"), ui.Hex("#7bb1a2"), ui.Hex("#10241f"), ui.Hex("#e0897a"), ui.Hex("#e2b24f"), ui.Hex("#3a3226"), ui.Hex("#433a2d")},
	},
	prefs.PresetMist: {
		{false, ui.Hex("#e8edf0"), ui.Hex("#dce3e8"), ui.Hex("#1d2830"), ui.Hex("#2c3841"), ui.Hex("#464f57"), ui.Hex("#56616b"),
			ui.Hex("#1d6470"), ui.Hex("#1d5a65"), ui.Hex("#1d525d"), ui.Hex("#ffffff"), ui.Hex("#a2452f"), ui.Hex("#a07414"), ui.Hex("#d6dee4"), ui.Hex("#f3f6f8")},
		{true, ui.Hex("#1b2227"), ui.Hex("#232c33"), ui.Hex("#e2e9ee"), ui.Hex("#c6d0d7"), ui.Hex("#a9b4bc"), ui.Hex("#8d9aa4"),
			ui.Hex("#86c5d0"), ui.Hex("#95ccd6"), ui.Hex("#76adb7"), ui.Hex("#102329"), ui.Hex("#e0897a"), ui.Hex("#e2b24f"), ui.Hex("#2b353d"), ui.Hex("#34404a")},
	},
}

// presetOf returns the palette whose paper t has, or nil for a theme that is not a reading theme.
func presetOf(t *ui.Theme) *palette {
	for _, variants := range readingPalettes {
		for i := range variants {
			if t.Background == variants[i].background {
				return &variants[i]
			}
		}
	}
	return nil
}

// ReadingTheme returns the theme the reader draws with for a reading appearance preset (prefs.PresetSepia or
// prefs.PresetMist): main with the paper, ink and links of the preset's light or dark variant, following main's
// scheme. Any other preset, the empty one that follows the theme included, gives main itself. Metrics and the font
// stay main's, and high contrast is read from main as PaneTheme does. The helpers of this package called with it
// return the preset's values. Call c.SetTheme with it before the reader's elements are created and set main back
// after; given a reading theme, it returns it unchanged.
func ReadingTheme(main *ui.Theme, preset string) *ui.Theme {
	if presetOf(main) != nil {
		return main
	}
	variants, ok := readingPalettes[preset]
	if !ok || isPane(main) {
		return main
	}
	p := variants[0]
	if main.Dark {
		p = variants[1]
	}
	high := highContrast(main)
	t := *main
	t.Background, t.Surface, t.Text, t.TextMuted = p.background, p.surface, p.text, p.muted
	t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = p.accent, p.accentHover, p.accentPressed, p.accentText
	t.Danger = p.danger
	t.SurfaceHover = Ink(&t, state(&t, 0, high))
	t.SurfacePressed = Ink(&t, state(&t, 1, high))
	t.Border = Ink(&t, 0.075)
	t.Selection = t.Accent.Alpha(0.22)
	t.Scrollbar = Ink(&t, 0.28)
	if p.dark {
		t.Selection = t.Accent.Alpha(0.3)
		t.Scrollbar = Ink(&t, 0.3)
	}
	t.Focus = t.Accent.Alpha(0.8)
	if high {
		t.Border = t.Text.Alpha(0.55)
		t.TextMuted = t.TextMuted.Mix(t.Text, 0.4)
		t.Focus.A = 255
	}
	return &t
}
