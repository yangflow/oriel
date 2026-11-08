package theme

import (
	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/theme/tokens"
)

// ToastLook is the Data of a toast that shows more than its text: Icon before the text, Keys, the shortcut of the
// action, beside its label, and Close, the name of a close button, which a toast with an action has.
type ToastLook struct {
	Icon  *ui.SVG
	Keys  string
	Close string
}

// toastMin is the least height of a toast.
const toastMin = tokens.Toolbar - 12

// Toasts draws the window's toasts with Oriel's look, centered at the bottom in Inverse on InverseText. Call it once
// in the root view, after Apply.
func Toasts(c *ui.Context) {
	t := c.Theme()
	fill, text := t.Inverse, t.InverseText
	if fill == ui.Transparent {
		fill = t.Text
	}
	if text == ui.Transparent {
		text = t.Background
	}
	width, _ := c.Size()
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		viewport.Padding(0, 0, t.Space(5), 0).AlignItems(ui.Center).Gap(t.Space(2))
		for _, ts := range toasts {
			look, _ := ts.Data.(ToastLook)
			p := ui.ToastBase(c, ts)
			box := p.Root.Row().AlignItems(ui.Center).Gap(Px(t, tokens.Space2)).MinHeight(Px(t, toastMin)).
				MaxWidth(min(Px(t, tokens.Dialog), width-t.Space(10))).Padding(Px(t, tokens.Space1_5), Px(t, tokens.Space4)).
				Radius(Px(t, tokens.RadiusCard)).Background(fill).TextColor(text)
			popup := Elevation(t).Popup
			popup.Rim = 0
			Shadow(t, box, popup)
			if ts.Action != "" {
				box.Padding(Px(t, tokens.Space1_5), Px(t, tokens.Space1_5), Px(t, tokens.Space1_5), Px(t, tokens.Space4))
			}
			box.Opacity(fade(c, p))
			cut := false
			box.Children(func() {
				if look.Icon != nil {
					ui.Icon(c, look.Icon).FontSize(IconMedium(t))
				}
				title := ui.Text(c, ts.Title).Shrink(1).MinWidth(0).MaxLines(2)
				cut = Cut(c, title, ts.Title, t.FontSize, 0, 2)
				if ts.Action == "" {
					return
				}
				action := p.ActionButton().Row().AlignItems(ui.Center).Gap(Px(t, tokens.Space1_5)).
					Height(Px(t, tokens.Row)).PaddingX(Px(t, tokens.Space2)).Radius(Px(t, tokens.RadiusControl))
				if action.Hovered() {
					action.Background(text.Alpha(tokens.OpacityInverseFill))
				}
				action.Children(func() {
					label := ui.Text(c, ts.Action).FontWeight(600).SingleLine()
					if action.Hovered() {
						label.Underline()
					}
					if look.Keys != "" {
						ui.Text(c, look.Keys).FontSize(Px(t, tokens.TextMeta.Size.En)).TextColor(text.Alpha(tokens.OpacityInverseMuted)).SingleLine()
					}
				})
				if look.Close == "" {
					return
				}
				close := p.CloseButton().Label(look.Close).Tooltip(look.Close).Size(Px(t, tokens.Row), Px(t, tokens.Row)).
					Center().Radius(Px(t, tokens.RadiusControl))
				if close.Hovered() {
					close.Background(text.Alpha(tokens.OpacityInverseFill))
				}
				close.Children(func() {
					ui.Icon(c, icons.X).FontSize(IconSmall(t)).TextColor(text.Alpha(tokens.OpacityInverseMuted))
				})
			})
			if cut {
				Tip(c, p.Root, ts.Title)
			}
		}
	})
}

// fade is the opacity of a toast: it fades in, and out at the end.
func fade(c *ui.Context, p ui.ToastParts) float32 {
	const length = tokens.MotionBase
	left := p.Left()
	switch {
	case left <= 0:
		return 1
	case left < length:
		c.AnimationFrame()
		return float32(left) / float32(length)
	}
	c.After(left - length)
	return 1
}
