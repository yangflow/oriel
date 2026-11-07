package theme

import (
	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/ui/theme/tokens"
)

// Level is how high a sheet floats: a popup (a menu, a select, the font picker) or a dialog. The values are in
// design/tokens.md, Elevation.
type Level int

const (
	// Popup is a small surface anchored to its control.
	Popup Level = iota
	// Dialog is a surface centered over a scrim.
	Dialog
)

// SheetLook is what Sheet draws for a level: the corner radius in design pixels and the elevation's shadow and rim.
type SheetLook struct {
	Radius float32
	Shadow tokens.Shadow
}

// SheetFor returns the look of a sheet of level in t's scheme; high is the desktop's high contrast, which makes the
// rim 1 DIP in t's Border. A popup has radius-card and elevation-popup, a dialog radius-pane and elevation-dialog.
func SheetFor(t *ui.Theme, high bool, level Level) SheetLook {
	e := Elevation(t)
	l := SheetLook{Radius: tokens.RadiusCard, Shadow: e.Popup}
	if level == Dialog {
		l = SheetLook{Radius: tokens.RadiusPane, Shadow: e.Dialog}
	}
	if high {
		l.Shadow.Rim, l.Shadow.RimColor = 1, t.Border
	}
	return l
}

// Sheet gives e the face of a floating surface of level: float, the radius, the rim and the shadow. Every dialog,
// popup and picker goes through it, so they all look alike.
func Sheet(c *ui.Context, e ui.Element, level Level) ui.Element {
	t := c.Theme()
	l := SheetFor(t, c.Preferences().HighContrast, level)
	s := l.Shadow
	return e.Background(RaisedFill(t)).Radius(Px(t, l.Radius)).Border(s.Rim, s.RimColor).
		Shadow(Px(t, s.X), Px(t, s.Y), Px(t, s.Blur), 0, s.Color)
}

// ScrimFor is the color that dims the window behind a dialog: the scheme's scrim, deeper with high contrast (50% in
// light, 60% in dark).
func ScrimFor(t *ui.Theme, high bool) ui.Color {
	s := tokens.SchemeLight.Scrim
	if t.Dark {
		s = tokens.SchemeDark.Scrim
	}
	switch {
	case t.Dark && high:
		return opaque(s).Alpha(0.6)
	case high:
		return opaque(s).Alpha(0.5)
	}
	return s
}

// Scrim dims the window behind a dialog: the backdrop element DialogBase gives.
func Scrim(c *ui.Context, backdrop ui.Element) ui.Element {
	return backdrop.Background(ScrimFor(c.Theme(), c.Preferences().HighContrast))
}
