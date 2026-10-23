package theme

import (
	"sync"

	"github.com/egoist/mygo/ui"
)

// measureKey names one measurement: the text with the font and size it is drawn in, wrapped at width (0 for none).
type measureKey struct {
	text, font string
	size       float32
	weight     int
	width      float32
}

// measured keeps the size of text laid out, so a steady frame measures nothing: lists can have hundreds of rows.
var measured = struct {
	sync.Mutex
	size map[measureKey][2]float32
}{size: map[measureKey][2]float32{}}

// measure is c.MeasureText of span wrapped at width, from the cache when this text was measured before in the same
// font. The cache is dropped when it grows past a few thousand texts.
func measure(c *ui.Context, span ui.Span, width float32) (float32, float32) {
	t := c.Theme()
	size := span.Size
	if size == 0 {
		size = t.FontSize
	}
	key := measureKey{text: span.Text, font: t.Font, size: size, weight: span.Weight, width: width}
	measured.Lock()
	got, ok := measured.size[key]
	measured.Unlock()
	if ok {
		return got[0], got[1]
	}
	w, h := c.MeasureText(width, span)
	measured.Lock()
	if len(measured.size) > 4096 {
		clear(measured.size)
	}
	measured.size[key] = [2]float32{w, h}
	measured.Unlock()
	return w, h
}

// Cut reports whether text, drawn in el at size DIPs (0 for the theme's) and weight (0 for regular), is cut short by
// the box el has: wider than it on one line, or taller than lines lines when wrapped at its width. It reads the box of
// the previous frame, so it is false in the first frame of an element. Measurements are cached by text, font, size
// and weight, so it is cheap to call for every row of a list while building.
func Cut(c *ui.Context, el ui.Element, text string, size float32, weight, lines int) bool {
	width := el.Bounds().W
	if width <= 0 || text == "" {
		return false
	}
	span := ui.Span{Text: text, Size: size, Weight: weight}
	one, line := measure(c, span, 0)
	if lines <= 1 {
		return one > width+0.5
	}
	if one <= width+0.5 {
		return false
	}
	_, wrapped := measure(c, span, width)
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
		tip.MaxWidth(t.Space(80)).Padding(t.Space(1.25), t.Space(2)).Radius(t.Space(1.5)).
			Background(fill).TextColor(ink).FontSize(Px(t, 12))
		tip.Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.2))
		tip.Children(func() { ui.Text(c, text) })
		tip.AttachTo(el, ui.AnchorTop, ui.AnchorBottom).Margin(0, 0, t.Space(1.5), 0)
	})
}
