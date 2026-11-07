package theme

import (
	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/ui/theme/tokens"
)

// Link is a text button, design CLink: label text in Accent with no fill, underlined only under the pointer. Muted
// is a quiet action beside a status line, Text2 and Text under the pointer. An icon is icon-s with a space-1 gap,
// after the text when IconAfter. The button keeps a row-dense hit height around the text.
type Link struct {
	Text            string
	Icon            *ui.SVG
	IconAfter       bool
	Muted, Disabled bool
	// Key keys the button; Label names it when the text alone does not.
	Key   any
	Label string
}

// Draw builds the link and returns it for Clicked.
func (l Link) Draw(c *ui.Context) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Row().AlignItems(ui.Center).Gap(Px(t, tokens.Space1)).Height(Px(t, tokens.RowDense)).
		PaddingX(Px(t, tokens.Space1)).Radius(Px(t, tokens.RadiusXS)).Disabled(l.Disabled)
	if l.Key != nil {
		b.Key(l.Key)
	}
	label := l.Label
	if label == "" {
		label = l.Text
	}
	b.Label(label)
	color := t.Accent
	hot := b.Hovered() && !l.Disabled
	switch {
	case l.Disabled:
		color = t.TextMuted
	case l.Muted && hot:
		color = t.Text
	case l.Muted:
		color = Text2(t)
	case hot:
		color = t.AccentHover
	}
	b.Children(func() {
		icon := func() {
			if l.Icon != nil {
				ui.Icon(c, l.Icon).FontSize(IconSmall(t)).TextColor(color)
			}
		}
		if !l.IconAfter {
			icon()
		}
		text := ui.Text(c, l.Text).FontWeight(500).TextColor(color).NoWrap()
		if hot {
			text.Underline()
		}
		if l.IconAfter {
			icon()
		}
	})
	return b
}
