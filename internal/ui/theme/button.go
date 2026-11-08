package theme

import (
	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/theme/tokens"
)

// ButtonStyle is the look of a Button.
type ButtonStyle int

const (
	// Secondary is the fill with Text: the default.
	Secondary ButtonStyle = iota
	// Primary is the Accent fill with AccentText: the one action a form is for.
	Primary
	// Danger is a destructive action: the fill with Danger text, never a red fill.
	Danger
	// Plain is an action with no fill and Text2, Text on the hover fill under the pointer.
	Plain
)

// Button is the one text button of dialogs, forms and settings, design CButton: control high (control-small when
// Small), radius-control (radius-sm when Small), an icon-s icon, 500 text (12 when Small), a motion-fast fade of the
// fill and the Focus ring ButtonBase draws. Its side padding is space-3 (space-2 when Small); a Primary or Secondary
// button has the rim of Border with high contrast. Disabled keeps the fill (none for Plain) with TextMuted, never a
// faded accent.
type Button struct {
	Style ButtonStyle
	Small bool // control-small high with 12 text and space-2 side padding, for a health line, a row or a card
	On    bool // Secondary or Plain: the chosen one of a few, in Soft(Accent) with Accent text
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

// ButtonHeight is the height of a Button in mockup pixels: control, control-small when small.
func ButtonHeight(small bool) float32 {
	if small {
		return tokens.ControlSmall
	}
	return tokens.Control
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
	b.Height(h).Gap(Px(t, tokens.Space1_5)).Radius(Px(t, tokens.RadiusControl)).Disabled(bt.Disabled)
	if bt.Small {
		b.Radius(Px(t, tokens.RadiusSm))
	}
	switch {
	case bt.Text == "":
		b.Width(h)
	case bt.Small:
		b.PaddingX(Px(t, tokens.Space2))
	default:
		b.PaddingX(Px(t, tokens.Space3))
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
				spin.Rotate(spin.Loop("spin", tokens.MotionSpin, ui.Linear) * 360)
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
				label.FontSize(Px(t, tokens.TextMeta.Size.En))
			}
		}
		if bt.IconAfter {
			icon()
		}
	})
	return b
}

// look is the fill and the text color of the button b in its state: the variants of design CButton.
func (bt Button) look(t *ui.Theme, b ui.Element) (fill, color ui.Color) {
	track := Track(t)
	switch {
	case bt.Disabled && bt.Style == Plain:
		return ui.Color{}, t.TextMuted
	case bt.Disabled:
		return track, t.TextMuted
	case bt.On && (bt.Style == Secondary || bt.Style == Plain):
		return Soft(t, t.Accent), t.Accent
	}
	if bt.Style == Primary {
		switch {
		case b.Pressed():
			return t.AccentPressed, t.AccentText
		case b.Hovered():
			return t.AccentHover, t.AccentText
		}
		return t.Accent, t.AccentText
	}
	color = t.Text
	switch bt.Style {
	case Danger:
		color = t.Danger
	case Plain:
		track = ui.Color{}
		if !b.Hovered() && !b.Pressed() {
			color = Text2(t)
		}
	}
	switch {
	case b.Pressed():
		return t.SurfacePressed, color
	case b.Hovered():
		return t.SurfaceHover, color
	}
	return track, color
}
