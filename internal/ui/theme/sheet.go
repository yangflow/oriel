package theme

import "github.com/egoist/mygo/ui"

// Level is how high a sheet floats: a popup (a menu, a select, the font picker) or a dialog. The values are in
// design/tokens.md, Sheet and Scrim.
type Level int

const (
	// Popup is a small surface anchored to its control.
	Popup Level = iota
	// Dialog is a surface centered over a scrim.
	Dialog
)

// SheetLook is what Sheet draws for a level: the corner radius in design pixels, the width and color of the rim, and
// the shadow's offset, blur and color.
type SheetLook struct {
	Radius, RimWidth float32
	Rim              ui.Color
	Offset, Blur     float32
	Shadow           ui.Color
}

// SheetFor returns the look of a sheet of level in t's scheme; high is the desktop's high contrast. In light the rim
// is a 0.5 DIP hairline in ink 12%; in dark a 1 DIP line in white 8%; with high contrast a 1 DIP Border, whichever the
// scheme. The shadow is warm ink in light and black in dark, deeper for a dialog.
func SheetFor(t *ui.Theme, high bool, level Level) SheetLook {
	l := SheetLook{Radius: 12, RimWidth: 0.5, Rim: Ink(t, 0.12), Offset: 12, Blur: 32, Shadow: ui.RGBA(28, 24, 18, 0.22)}
	if level == Dialog {
		l.Radius, l.Offset, l.Blur, l.Shadow = 16, 20, 50, ui.RGBA(28, 24, 18, 0.25)
	}
	switch {
	case high:
		l.RimWidth, l.Rim = 1, t.Border
	case t.Dark:
		l.RimWidth, l.Rim = 1, ui.RGBA(255, 255, 255, 0.08)
	}
	if t.Dark {
		l.Shadow = ui.RGBA(0, 0, 0, 0.5)
		if level == Dialog {
			l.Shadow = ui.RGBA(0, 0, 0, 0.55)
		}
	}
	return l
}

// Sheet gives e the face of a floating surface of level: RaisedFill, the radius, the rim and the shadow. Every
// dialog, popup and picker goes through it, so they all look alike.
func Sheet(c *ui.Context, e ui.Element, level Level) ui.Element {
	t := c.Theme()
	l := SheetFor(t, c.Preferences().HighContrast, level)
	return e.Background(RaisedFill(t)).Radius(Px(t, l.Radius)).Border(l.RimWidth, l.Rim).Shadow(0, l.Offset, l.Blur, 0, l.Shadow)
}

// ScrimFor is the color that dims the window behind a dialog: warm ink at 32% in light, black at 45% in dark, and at
// 50% and 60% with high contrast.
func ScrimFor(t *ui.Theme, high bool) ui.Color {
	switch {
	case t.Dark && high:
		return ui.RGBA(0, 0, 0, 0.6)
	case t.Dark:
		return ui.RGBA(0, 0, 0, 0.45)
	case high:
		return ui.RGBA(28, 24, 18, 0.5)
	}
	return ui.RGBA(28, 24, 18, 0.32)
}

// Scrim dims the window behind a dialog: the backdrop element DialogBase gives.
func Scrim(c *ui.Context, backdrop ui.Element) ui.Element {
	return backdrop.Background(ScrimFor(c.Theme(), c.Preferences().HighContrast))
}
