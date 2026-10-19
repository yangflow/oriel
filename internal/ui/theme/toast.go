package theme

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/ui/icons"
)

// ToastLook is the Data of a toast that shows more than its text: Icon before the text, Keys, the shortcut of the
// action, beside its label, and Close, the name of a close button, which a toast with an action has.
type ToastLook struct {
	Icon  *ui.SVG
	Keys  string
	Close string
}

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
			box := p.Root.Row().AlignItems(ui.Center).Gap(px(t, 10)).MinHeight(px(t, 40)).MaxWidth(width-t.Space(10)).
				Padding(px(t, 6), px(t, 14)).Radius(px(t, 12)).Background(fill).TextColor(text).
				Shadow(0, 8, 28, 0, ui.RGBA(0, 0, 0, 0.28))
			if ts.Action != "" {
				box.Padding(px(t, 6), px(t, 6), px(t, 6), px(t, 14))
			}
			box.Opacity(fade(c, p))
			box.Children(func() {
				if look.Icon != nil {
					ui.Icon(c, look.Icon).FontSize(px(t, 16))
				}
				ui.Text(c, ts.Title).Shrink(1).MinWidth(0).SingleLine().FontSize(px(t, 12.5))
				if ts.Action == "" {
					return
				}
				action := p.ActionButton().Row().AlignItems(ui.Center).Gap(px(t, 6)).Height(px(t, 28)).
					PaddingX(px(t, 10)).Radius(px(t, 8)).Background(text.Alpha(0.16))
				if action.Hovered() {
					action.Background(text.Alpha(0.24))
				}
				action.Children(func() {
					ui.Text(c, ts.Action).FontSize(px(t, 12.5)).FontWeight(600).SingleLine()
					if look.Keys != "" {
						ui.Text(c, look.Keys).FontSize(px(t, 12.5)).TextColor(text.Alpha(0.6)).SingleLine()
					}
				})
				if look.Close == "" {
					return
				}
				close := p.CloseButton().Label(look.Close).Tooltip(look.Close).Size(px(t, 28), px(t, 28)).Center().
					Radius(px(t, 8))
				if close.Hovered() {
					close.Background(text.Alpha(0.16))
				}
				close.Children(func() {
					ui.Icon(c, icons.X).FontSize(px(t, 14)).TextColor(text.Alpha(0.7))
				})
			})
		}
	})
}

// fade is the opacity of a toast: it fades in, and out at the end.
func fade(c *ui.Context, p ui.ToastParts) float32 {
	const length = 200 * time.Millisecond
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

func px(t *ui.Theme, v float32) float32 { return t.Rem(v / 13) }
