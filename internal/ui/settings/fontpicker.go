package settings

import (
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// The measures of the font picker, in the units of the mockup (design/screens/settings.html): px scales them.
const (
	pickerWidth   = 300 // the least width; the popup is also as wide as its control
	pickerHeight  = 360 // the most height; only the list scrolls
	pickerGap     = 4   // between the control and the popup
	pickerMargin  = 8   // between the popup and the window
	pickerRow     = 32  // the height of a row and of the search field
	pickerRule    = 13  // the hairline between the pinned rows and the list, with its margins
	pickerStatus  = 56  // the loading and the empty line
	pickerHead    = 46  // the search field with its margins, up to the list
	pickerPadding = 14  // the list's padding above and below
)

// announcePause is how long the result count of the picker waits for more typing before it is announced.
const announcePause = 300 * time.Millisecond

// fontPicker is the state of one font picker: the popup under a control that chooses a font family. The caller supplies
// the pinned rows (the Interface font: System font and the current family; the Reading font of #101: Follow interface
// and Serif), the picker adds the installed families below them and searches both.
type fontPicker struct {
	open   bool
	focus  bool // the search field takes the focus as it is built
	query  string
	hl     int // the highlighted row among the shown ones, -1 with none
	list   ui.ListState
	reveal revealing

	// What shown was made from, so that it is made again only when one of them changes.
	from struct {
		query  string
		count  int
		loaded bool
		pins   []choice
	}
	folded []string // the lower case names of the families
	shown  []choice // the rows that match: the pinned ones, then the families
	pinned int      // how many of shown are pinned

	told, said string    // what was last announced, and what the result count is now
	saidAt     time.Time // since when it has been what it is
}

// revealing says how the list scrolls to the highlighted row: not at all, by the least, or to the middle.
type revealing uint8

const (
	stay revealing = iota
	near
	middle
)

// fontPick draws the control of a font picker and, while it is open, its popup. It returns the value chosen and whether
// it was chosen in this frame. families is nil until the installed ones are known, which the popup shows as it waits.
func (s *Screen) fontPick(c *ui.Context, p *fontPicker, label, current string, pinned []choice, families []string) (string, bool) {
	t := c.Theme()
	loaded := families != nil
	shown := pinned[0]
	for _, o := range pinned {
		if o.value == current {
			shown = o
		}
	}

	trig := ui.ButtonBase(c)
	trig.MinWidth(theme.Px(t, 160)).MaxWidth(theme.Px(t, 240)).Height(theme.Px(t, 32)).AlignItems(ui.Center).Gap(t.Space(2)).
		Padding(0, t.Space(2), 0, theme.Px(t, 10)).Radius(t.Radius).Label(label).Value(shown.name).Expanded(p.open).
		Role(ui.RolePopUpButton)
	trigger(c, trig)
	if p.open {
		trig.Background(theme.Chosen(t))
	}
	trig.Children(func() {
		name := ui.Text(c, shown.name).Grow(1).MinWidth(0).SingleLine()
		if shown.family != "" {
			name.Font(shown.family)
		}
		ui.Icon(c, icons.ChevronDown).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
	})
	if trig.Clicked() {
		if p.open {
			p.open = false
		} else {
			p.start(pinned, families, loaded, current)
		}
	}

	var picked string
	var chosen bool
	if p.open {
		p.sync(pinned, families, loaded, current)
		now := s.deps.Now()
		ui.PopoverBase(c, trig, &p.open, func(panel ui.Element) {
			picked, chosen = s.pickerPanel(c, p, panel, trig, current, loaded)
		})
		s.count(c, p, now, loaded)
	}
	if chosen {
		p.open = false
		trig.Focus()
		return picked, picked != current
	}
	return "", false
}

// start opens the popup on a clean search with the highlight on the current choice.
func (p *fontPicker) start(pinned []choice, families []string, loaded bool, current string) {
	p.open, p.focus, p.query = true, true, ""
	p.told, p.said = "", ""
	p.from.pins = nil
	p.sync(pinned, families, loaded, current)
	p.hl = max(slices.IndexFunc(p.shown, func(o choice) bool { return o.value == current }), 0)
	if len(p.shown) == 0 {
		p.hl = -1
	}
	p.reveal = middle
}

// sync makes the matching rows again when the query, the pinned rows or the families changed.
func (p *fontPicker) sync(pinned []choice, families []string, loaded bool, current string) {
	f := &p.from
	if f.query == p.query && f.count == len(families) && f.loaded == loaded && slices.Equal(f.pins, pinned) {
		return
	}
	queryChanged := f.query != p.query
	f.query, f.count, f.loaded, f.pins = p.query, len(families), loaded, slices.Clone(pinned)
	if len(p.folded) != len(families) {
		p.folded = p.folded[:0]
		for _, name := range families {
			p.folded = append(p.folded, strings.ToLower(name))
		}
	}
	q := strings.ToLower(p.query)
	p.shown = p.shown[:0]
	for _, o := range pinned {
		if strings.Contains(strings.ToLower(o.name), q) {
			p.shown = append(p.shown, o)
		}
	}
	p.pinned = len(p.shown)
	for i, name := range families {
		// A pinned family is not repeated below the pinned rows.
		if strings.Contains(p.folded[i], q) && !slices.ContainsFunc(pinned, func(o choice) bool { return o.value == name }) {
			p.shown = append(p.shown, choice{value: name, name: name, family: name})
		}
	}
	switch {
	case len(p.shown) == 0:
		p.hl = -1
	case queryChanged || p.hl < 0:
		p.hl = 0
		p.reveal = near
	default:
		p.hl = min(p.hl, len(p.shown)-1)
	}
}

// layout is what the list shows besides the rows: the hairline after the pinned rows, and the loading or the empty line.
func (p *fontPicker) layout(loaded bool) (rule, status bool) {
	families := len(p.shown) - p.pinned
	rule = p.pinned > 0 && (families > 0 || !loaded)
	status = !loaded || len(p.shown) == 0
	return rule, status
}

// rows is how many rows the list has: the shown ones, the hairline and the line of status.
func (p *fontPicker) rows(loaded bool) int {
	rule, status := p.layout(loaded)
	n := len(p.shown)
	if rule {
		n++
	}
	if status {
		n++
	}
	return n
}

// at is the row of the list that shows the option i of shown.
func (p *fontPicker) at(i int, loaded bool) int {
	if i < p.pinned {
		return i
	}
	rule, status := p.layout(loaded)
	if rule {
		i++
	}
	if status {
		i++
	}
	return i
}

// contentHeight is the height of the list with its padding, in the units of the mockup.
func (p *fontPicker) contentHeight(loaded bool) float32 {
	rule, status := p.layout(loaded)
	h := float32(len(p.shown)*pickerRow + pickerPadding)
	if rule {
		h += pickerRule
	}
	if status {
		h += pickerStatus
	}
	return h
}

// pickerPanel builds the popup: the search field and the list. It returns the value chosen by Return or a click.
func (s *Screen) pickerPanel(c *ui.Context, p *fontPicker, panel, trig ui.Element, current string, loaded bool) (string, bool) {
	t := c.Theme()
	_, window := c.Size()
	b := trig.Bounds()

	// Below the control or above it, whichever has more room, as high as the content and at most 360.
	below := window - theme.Px(t, pickerMargin) - (b.Y + b.H + theme.Px(t, pickerGap))
	above := b.Y - theme.Px(t, pickerGap) - theme.Px(t, pickerMargin)
	want := min(theme.Px(t, pickerHead+p.contentHeight(loaded)), theme.Px(t, pickerHeight))
	up := below < want && above > below
	room := below
	at, self := ui.AnchorBottomEnd, ui.AnchorTopEnd
	if up {
		room, at, self = above, ui.AnchorTopEnd, ui.AnchorBottomEnd
	}
	height := min(want, max(room, theme.Px(t, pickerHead+pickerRow)))
	panel.AttachTo(trig, at, self).Margin(theme.Px(t, pickerGap), 0, theme.Px(t, pickerGap), 0).
		Width(max(theme.Px(t, pickerWidth), b.W)).Height(height)
	theme.Sheet(c, panel, theme.Popup)

	var picked string
	var chosen bool
	ui.Column(c).Fill().Children(func() {
		var in ui.Element
		search := ui.Row(c).Margin(theme.Px(t, 8), theme.Px(t, 8), 0, theme.Px(t, 8)).Height(theme.Px(t, pickerRow)).AlignItems(ui.Center).
			Gap(theme.Px(t, 8)).PaddingX(theme.Px(t, 10)).Radius(theme.Px(t, 8)).Background(t.Surface).Shrink(0)
		search.Children(func() {
			ui.Icon(c, icons.Search).FontSize(theme.IconMedium(t)).TextColor(t.TextMuted)
			in = ui.TextInputBase(c, &p.query).Grow(1).MinWidth(0).FocusRing(false).
				Label(s.text.FontSearch).Placeholder(s.text.FontSearch)
			if p.focus {
				in.Focus()
				p.focus = false
			}
		})
		search.Shadow(0, 0, 0, 2, t.Focus)

		// The arrows, Home and End move the highlight while the caret stays in the text. The field edits with Home and End,
		// which it gives to no shortcut, so they come through OnTextKey, between frames.
		n := len(p.shown)
		in.OnTextKey(func(k ui.TextKey) bool {
			if k.Mods != 0 || len(p.shown) == 0 {
				return false
			}
			to := p.hl
			switch k.Key {
			case ui.KeyDown:
				to++
			case ui.KeyUp:
				to--
			case ui.KeyHome:
				to = 0
			case ui.KeyEnd:
				to = len(p.shown) - 1
			default:
				return false
			}
			p.hl, p.reveal = max(0, min(to, len(p.shown)-1)), near
			return true
		})
		if in.Submitted() && p.hl >= 0 && p.hl < n {
			picked, chosen = p.shown[p.hl].value, true
		}
		if in.Shortcut(0, ui.KeyEscape) {
			p.open = false
			trig.Focus()
		}

		if row := p.at(p.hl, loaded); p.hl >= 0 && p.reveal != stay {
			if p.reveal == middle {
				p.list.ScrollTo(row, ui.Center)
			} else {
				p.list.ScrollIntoView(row)
			}
		}
		p.reveal = stay

		rule, status := p.layout(loaded)
		list := ui.List(c, &p.list, p.rows(loaded), func(i int) {
			switch {
			case i < p.pinned:
			case rule && i == p.pinned:
				ui.Row(c).FillWidth().Height(1).Margin(theme.Px(t, 6), theme.Px(t, 2), theme.Px(t, 6), theme.Px(t, 2)).Background(t.Border).
					Role(ui.RoleNone)
				return
			case status && i == p.pinned+btoi(rule):
				s.pickerStatus(c, p, loaded)
				return
			}
			o := i
			if i >= p.pinned {
				o = i - btoi(rule) - btoi(status)
			}
			opt := p.shown[o]
			row := s.pickerOption(c, opt, opt.value == current, o == p.hl)
			if o == p.hl {
				in.ActiveDescendant(row)
			}
			if row.Clicked() {
				picked, chosen = opt.value, true
			}
		})
		list.Grow(1).MinHeight(0).Margin(theme.Px(t, 6), 0, 0, 0).Padding(theme.Px(t, 6), theme.Px(t, 8), theme.Px(t, 8), theme.Px(t, 8)).
			Label(s.text.FontList)
	})
	return picked, chosen
}

// pickerOption is a row of the list: the family in its own face, a check at the right for the current choice.
func (s *Screen) pickerOption(c *ui.Context, o choice, current, highlighted bool) ui.Element {
	t := c.Theme()
	name := o.name
	if current {
		name += ", " + s.text.FontCurrent
	}
	row := ui.Row(c).FillWidth().Height(theme.Px(t, pickerRow)).AlignItems(ui.Center).Gap(theme.Px(t, 8)).
		Padding(0, theme.Px(t, 8), 0, theme.Px(t, 10)).Radius(theme.Px(t, 6)).Role(ui.RoleListItem).Label(name).KeepFocus()
	switch {
	case highlighted:
		row.Background(theme.Chosen(t))
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		face := ui.Text(c, o.name).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").FontSize(theme.Px(t, 13))
		if o.family != "" {
			face.Font(o.family)
		}
		if current {
			ui.Icon(c, icons.Check).FontSize(theme.IconSmall(t)).TextColor(t.Accent)
		}
	})
	return row
}

// pickerStatus is the line the list shows while the families are read, or when nothing matches.
func (s *Screen) pickerStatus(c *ui.Context, p *fontPicker, loaded bool) {
	t := c.Theme()
	row := ui.Row(c).FillWidth().MinHeight(theme.Px(t, pickerStatus)).AlignItems(ui.Center).Justify(ui.Center).
		Gap(theme.Px(t, 8)).Padding(theme.Px(t, 12)).Role(ui.RoleStatus)
	row.Children(func() {
		if !loaded {
			spin := ui.Icon(c, icons.LoaderCircle).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
			spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
			ui.Text(c, s.text.FontLoading).TextColor(t.TextMuted).Wrap().TextAlign(ui.Center)
			return
		}
		ui.Text(c, s.fill(s.text.FontNone, "{q}", p.query)).TextColor(t.TextMuted).Wrap().TextAlign(ui.Center)
	})
}

// count announces how many fonts match once the result has stayed the same for a short pause.
func (s *Screen) count(c *ui.Context, p *fontPicker, now time.Time, loaded bool) {
	text := ""
	switch {
	case !loaded:
	case len(p.shown) == 0:
		text = s.fill(s.text.FontNone, "{q}", p.query)
	case len(p.shown) == 1:
		text = s.text.FontCountOne
	default:
		text = locale.Count(s.text.FontCount, "n", len(p.shown))
	}
	if text != p.said {
		p.said, p.saidAt = text, now
	}
	if p.said == "" || p.said == p.told {
		return
	}
	if wait := announcePause - now.Sub(p.saidAt); wait > 0 {
		c.After(wait)
		return
	}
	c.Announce(p.said)
	p.told = p.said
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
