package theme

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/ui/icons"
)

// ButtonStyle is the look of a Button.
type ButtonStyle int

const (
	// Secondary is a Track fill with Text: the default.
	Secondary ButtonStyle = iota
	// Primary is the Accent fill with AccentText: the one action a form is for.
	Primary
	// Danger is a destructive action that is not the default: Danger text, no fill, Soft(Danger) under the pointer.
	Danger
	// Plain is an action with no fill and Text, SurfaceHover-like fill under the pointer.
	Plain
)

// Button is the one text button of dialogs, forms and settings, by the values in design/tokens.md: 32 high (26 when
// Small), radius 8, a 14 icon, 500 text (12 when Small), a 140 ms fade of the fill and the Focus ring ButtonBase
// draws. A Primary or Secondary button has 14 side padding and the rim of Border with high contrast; a Danger or
// Plain one has 8 and no rim. Disabled is Surface with TextMuted (no fill for Danger and Plain), never a faded accent.
type Button struct {
	Style ButtonStyle
	Small bool // 26 high with 12 text and 10 side padding, for a health line, a row or a card
	On    bool // Secondary only: the chosen one of a few, in Soft(Accent) with Accent text
	// Disabled is inert and dimmed; Busy swaps the icon for a turning loader and does not disable.
	Disabled, Busy bool
	Grow           bool // takes an equal share of the row, for two buttons that share its width
	IconAfter      bool // the icon follows the text
	Icon           *ui.SVG
	Text           string
	// Key keys the button; Label names it when the text does not; Checked makes it a toggle button, pressed when
	// true.
	Key     any
	Label   string
	Checked *bool
}

// ButtonHeight is the height of a Button in mockup pixels, 26 when small.
func ButtonHeight(small bool) float32 {
	if small {
		return 26
	}
	return 32
}

// Draw builds the button and returns it for Clicked and its other state. A Button with an Icon and no Text is an
// icon-only button, a square of the button's height; it needs a Label.
func (bt Button) Draw(c *ui.Context) ui.Element {
	t := c.Theme()
	h := Px(t, ButtonHeight(bt.Small))
	b := Fade(ui.ButtonBase(c))
	if bt.Key != nil {
		b.Key(bt.Key)
	}
	b.Height(h).Gap(t.Space(1.5)).Radius(t.Radius).Disabled(bt.Disabled)
	switch {
	case bt.Text == "":
		b.Width(h)
	case bt.Small:
		b.PaddingX(Px(t, 10))
	case bt.Style == Danger || bt.Style == Plain:
		b.PaddingX(t.Space(2))
	default:
		b.PaddingX(Px(t, 14))
	}
	if bt.Grow {
		b.Grow(1).Basis(0)
	}
	if bt.Label != "" {
		b.Label(bt.Label)
		if bt.Text == "" {
			b.Tooltip(bt.Label)
		}
	}
	if bt.Checked != nil {
		b.Role(ui.RoleToggleButton).Checked(*bt.Checked)
	}
	fill, color := bt.look(t, b)
	if c.Preferences().HighContrast && (bt.Style == Primary || bt.Style == Secondary) {
		b.Border(1, t.Border)
	}
	b.Background(fill).TextColor(color)
	b.Children(func() {
		icon := func() {
			switch {
			case bt.Busy:
				spin := ui.Icon(c, icons.LoaderCircle).FontSize(IconSmall(t))
				spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
			case bt.Icon != nil:
				ui.Icon(c, bt.Icon).FontSize(IconSmall(t))
			}
		}
		if !bt.IconAfter {
			icon()
		}
		if bt.Text != "" {
			label := ui.Text(c, bt.Text).NoWrap().FontWeight(500)
			if bt.Small {
				label.FontSize(Px(t, 12))
			}
		}
		if bt.IconAfter {
			icon()
		}
	})
	return b
}

// look is the fill and the text color of the button b in its state.
func (bt Button) look(t *ui.Theme, b ui.Element) (fill, color ui.Color) {
	track, ink := Track(t), Ink(t, 1)
	quiet := bt.Style == Danger || bt.Style == Plain
	switch {
	case bt.Disabled && quiet:
		return ui.Color{}, t.TextMuted
	case bt.Disabled:
		return t.Surface, t.TextMuted
	case bt.On && bt.Style == Secondary:
		return Soft(t, t.Accent), t.Accent
	}
	switch bt.Style {
	case Primary:
		switch {
		case b.Pressed():
			return t.AccentPressed, t.AccentText
		case b.Hovered():
			return t.AccentHover, t.AccentText
		}
		return t.Accent, t.AccentText
	case Danger:
		switch {
		case b.Pressed():
			return Soft(t, t.Danger).Mix(ink, 0.05), t.Danger
		case b.Hovered():
			return Soft(t, t.Danger), t.Danger
		}
		return ui.Color{}, t.Danger
	case Plain:
		switch {
		case b.Pressed():
			return t.SurfacePressed, t.Text
		case b.Hovered():
			return t.SurfaceHover, t.Text
		}
		return ui.Color{}, t.Text
	}
	switch {
	case b.Pressed():
		return track.Mix(ink, 0.1), t.Text
	case b.Hovered():
		return track.Mix(ink, 0.06), t.Text
	}
	return track, t.Text
}
