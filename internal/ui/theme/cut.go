package theme

import "github.com/egoist/mygo/ui"

// Cut reports whether text, drawn in el at size DIPs (0 for the theme's) and weight (0 for regular), is cut short by
// the box el has: wider than it on one line, or taller than lines lines when wrapped at its width. It reads the box of
// the previous frame, so it is false in the first frame of an element, and it measures only; call it while building.
func Cut(c *ui.Context, el ui.Element, text string, size float32, weight, lines int) bool {
	width := el.Bounds().W
	if width <= 0 || text == "" {
		return false
	}
	span := ui.Span{Text: text, Size: size, Weight: weight}
	one, line := c.MeasureText(0, span)
	if lines <= 1 {
		return one > width+0.5
	}
	_, wrapped := c.MeasureText(width, span)
	return wrapped > float32(lines)*line+0.5
}

// Tip shows text in a tooltip by el, with the look of `Tooltip`, when the pointer rests on el or the keyboard focus
// comes to it. Unlike `Tooltip` it does not describe el to assistive technology: use it where el's name already is
// the text, so a screen reader does not read it twice.
func Tip(c *ui.Context, el ui.Element, text string) {
	if text == "" {
		return
	}
	t := c.Theme()
	fill, ink := t.Inverse, t.InverseText
	if fill == ui.Transparent {
		fill = t.Text
	}
	if ink == ui.Transparent {
		ink = t.Background
	}
	ui.TooltipBase(c, el, func(tip ui.Element) {
		tip.MaxWidth(t.Space(80)).Padding(t.Space(1.25), t.Space(2)).Radius(t.Space(1.25)).
			Background(fill).TextColor(ink).FontSize(Px(t, 12))
		tip.Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.2))
		tip.Children(func() { ui.Text(c, text) })
		tip.AttachTo(el, ui.AnchorTop, ui.AnchorBottom).Margin(0, 0, t.Space(1.5), 0)
	})
}
