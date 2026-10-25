package timeline

import (
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/subscriptions"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// The measures of the command palette, in the units of the mockup (design/screens/timeline.html): px scales them.
const (
	paletteMax      = 50  // the most rows it shows
	paletteWidth    = 560 // the most width
	paletteRow      = 32  // the height of a row and of the search field
	paletteHead     = 26  // the height of a group header, which has 8 above its text
	paletteList     = 456 // the most height of the list
	paletteBottom   = 8   // the list's padding below
	paletteTop      = 0.15
	paletteShare    = 0.6                    // the most share of the window's height that the list takes
	paletteAnnounce = 300 * time.Millisecond // how long the result count waits for more typing before it is announced
)

// The groups of the palette, in the order they list.
const (
	groupActions = iota
	groupKinds
	groupSources
	groupSettings
)

// paletteItem is a row of the palette: something to go to or to do.
type paletteItem struct {
	group int
	name  string
	icon  *ui.SVG // nil for a subscription, which shows its own glyph
	site  string  // the address of the subscription's icon
	count int     // unread entries of a kind or subscription, 0 for none
	keys  []string
	run   func()
	rank  int
}

// paletteState is the command palette: whether it shows, what was typed and which row is highlighted.
type paletteState struct {
	open   bool
	focus  bool // give the search field the focus in the next frame
	reveal bool // scroll the highlighted row into view in the next frame
	query  string
	from   string // the query the highlight was last placed for
	hl     int    // the highlighted item among the ones shown
	list   ui.ListState

	said, told string    // what the result count is and what was last announced
	saidAt     time.Time // since when it has been what it is
}

// openPalette shows the command palette, from Cmd/Ctrl+K, unless a dialog shows.
func (s *Screen) openPalette() {
	if s.api == nil || s.keys || s.subs.Open() || s.pal.open {
		return
	}
	s.pal = paletteState{open: true, focus: true, reveal: true}
}

// paletteItems are all the rows the palette can show, in the order of their groups.
func (s *Screen) paletteItems() []paletteItem {
	cmd := modifier()
	var items []paletteItem
	add := func(it paletteItem) { items = append(items, it) }
	if s.canMarkAll() {
		add(paletteItem{group: groupActions, name: strings.ReplaceAll(s.text.MarkAllIn, "{scope}", s.scopeName()),
			icon: icons.CheckCheck, keys: []string{shiftKey(), "A"}, run: s.markAll})
	}
	if s.reading != 0 {
		name, icon := s.text.KeyImmersive, icons.Maximize2
		if s.immersive {
			name, icon = s.text.PaletteExitImmersive, icons.Minimize2
		}
		add(paletteItem{group: groupActions, name: name, icon: icon, keys: []string{"Z"}, run: s.toggleImmersive})
	}
	add(paletteItem{group: groupActions, name: s.text.AddSource, icon: icons.Plus, keys: []string{cmd, "N"}, run: s.subs.Add})
	add(paletteItem{group: groupActions, name: s.text.RefreshAll, icon: icons.RefreshCw, keys: []string{cmd, "R"}, run: s.RefreshAll})
	add(paletteItem{group: groupActions, name: s.text.PaletteHealth, icon: icons.HeartPulse, run: s.subs.Health})
	if s.subs.HasSources() {
		add(paletteItem{group: groupActions, name: s.text.PaletteManage, icon: icons.ListChecks, run: s.subs.Manage})
	}
	if s.filter == unread {
		add(paletteItem{group: groupActions, name: s.text.PaletteShowAll, icon: icons.CircleDot, keys: []string{"A"}, run: func() { s.setFilter(everything) }})
	} else {
		add(paletteItem{group: groupActions, name: s.text.PaletteUnreadOnly, icon: icons.CircleDot, keys: []string{"U"}, run: func() { s.setFilter(unread) }})
	}
	add(paletteItem{group: groupActions, name: s.text.PaletteRange, icon: icons.CalendarDays, keys: []string{"T"}, run: s.cycleRange})
	add(paletteItem{group: groupActions, name: s.text.PaletteOpenSettings, icon: icons.Settings, keys: []string{cmd, ","},
		run: func() {
			if s.deps.OpenSettings != nil {
				s.deps.OpenSettings()
			}
		}})
	add(paletteItem{group: groupActions, name: s.text.KeyKeys, icon: icons.Keyboard, keys: []string{"?"}, run: s.ShowKeys})
	for i, kind := range kinds {
		add(paletteItem{group: groupKinds, name: s.kindName(kind), icon: kindIcon(kind), count: s.unreadIn(scope{kind: kind}),
			run: func() { s.pickKind(i) }})
	}
	for _, src := range s.sources {
		add(paletteItem{group: groupSources, name: subscriptions.Name(src), site: src.IconURL, count: src.UnreadCount,
			run: func() {
				s.failingOnly = false
				s.choose(scope{kind: src.Kind, source: src.ID})
			}})
	}
	for _, page := range []struct {
		id, name string
		icon     *ui.SVG
	}{{"appearance", s.text.PageAppearance, icons.Palette}, {"fetching", s.text.PageFetching, icons.Rss},
		{"opml", s.text.PageImport, icons.ArrowUpDown}, {"ai", s.text.PageAI, icons.Sparkles},
		{"devices", s.text.PageDevices, icons.Laptop}, {"activity", s.text.PageActivity, icons.Database}} {
		add(paletteItem{group: groupSettings, name: page.name, icon: page.icon, run: func() {
			if s.deps.OpenSettingsPage != nil {
				s.deps.OpenSettingsPage(page.id)
			}
		}})
	}
	return items
}

// setFilter shows everything or only the unread entries, as the buttons of the toolbar do.
func (s *Screen) setFilter(filter int) {
	s.back()
	if filter != s.filter {
		s.show(func() { s.filter = filter })
	}
}

// paletteShown are the rows that match query, best first, at most paletteMax of them. The rank decides first, then the
// order of the groups, then the order the rows were made in.
func paletteShown(items []paletteItem, query string) []paletteItem {
	shown := items[:0:0]
	for _, it := range items {
		if it.rank = paletteRank(it.name, query); it.rank >= 0 {
			shown = append(shown, it)
		}
	}
	slices.SortStableFunc(shown, func(a, b paletteItem) int {
		if a.rank != b.rank {
			return a.rank - b.rank
		}
		return a.group - b.group
	})
	return shown[:min(len(shown), paletteMax)]
}

// paletteRank says how well name matches query, ignoring case and spaces: 0 for a name that starts with it, 1 for a
// word that does, 2 for one that contains it, 3 for one that has its letters in order, and -1 for no match. An empty
// query matches everything at 0.
func paletteRank(name, query string) int {
	q := fold(query)
	if q == "" {
		return 0
	}
	n := fold(name)
	if strings.HasPrefix(n, q) {
		return 0
	}
	prev := ' '
	for i, r := range name {
		if isBreak(prev) && !isBreak(r) && strings.HasPrefix(fold(name[i:]), q) {
			return 1
		}
		prev = r
	}
	if strings.Contains(n, q) {
		return 2
	}
	rest := []rune(q)
	for _, r := range n {
		if r == rest[0] {
			if rest = rest[1:]; len(rest) == 0 {
				return 3
			}
		}
	}
	return -1
}

// fold is s in lower case without its spaces.
func fold(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, s)
}

// isBreak says whether a word ends at r: a space, a punctuation mark or a symbol.
func isBreak(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) }

// groupName is the header of a group, and what a screen reader says before the name of a row.
func (s *Screen) groupName(group int) string {
	switch group {
	case groupActions:
		return s.text.PaletteActions
	case groupKinds:
		return s.text.Kinds
	case groupSources:
		return s.text.Sources
	}
	return s.text.Settings
}

// paletteLabel is what a screen reader says for a row: its group, its name and its unread count.
func (s *Screen) paletteLabel(it paletteItem) string {
	label := s.groupName(it.group) + s.text.Separator + it.name
	if it.count > 0 {
		label += s.text.Separator + s.format(s.text.UnreadCount, it.count)
	}
	return label
}

// paletteLine is a line of the list: a group header, or the row of an item shown.
type paletteLine struct {
	header bool
	group  int // of the header
	item   int // among the items shown
}

// palette draws the command palette while it is open. Nothing here reads the disk or the network: the rows are made from
// the subscriptions already loaded.
func (s *Screen) palette(c *ui.Context) {
	p := &s.pal
	if !p.open {
		return
	}
	t := c.Theme()
	width, height := c.Size()
	now := s.deps.Now()
	query := strings.TrimSpace(p.query)
	shown := paletteShown(s.paletteItems(), query)
	if p.from != p.query {
		p.from, p.hl, p.reveal = p.query, 0, true
	}
	p.hl = max(0, min(p.hl, len(shown)-1))

	// With an empty query the rows keep their groups under headers; with a query they are one ranked list.
	var lines []paletteLine
	at := make([]int, len(shown)) // the line of each item
	listHeight := float32(paletteBottom)
	for i, it := range shown {
		if query == "" && (i == 0 || shown[i-1].group != it.group) {
			lines = append(lines, paletteLine{header: true, group: it.group})
			listHeight += paletteHead
		}
		at[i] = len(lines)
		lines = append(lines, paletteLine{item: i})
		listHeight += paletteRow
	}
	top := height * paletteTop

	var run func()
	ui.DialogBase(c, &p.open, func(backdrop, panel ui.Element) {
		theme.Scrim(c, backdrop).Justify(ui.Start).Padding(top, t.Space(6), t.Space(3), t.Space(6))
		panel.Column().Width(min(theme.Px(t, paletteWidth), width-t.Space(12))).MaxHeight(height - top - t.Space(3)).
			Label(s.text.PaletteTitle)
		theme.Sheet(c, panel, theme.Dialog)
		var in ui.Element
		field := ui.Row(c).Margin(theme.Px(t, 8), theme.Px(t, 8), theme.Px(t, 4), theme.Px(t, 8)).Height(theme.Px(t, paletteRow)).AlignItems(ui.Center).
			Gap(theme.Px(t, 8)).PaddingX(theme.Px(t, 10)).Radius(theme.Px(t, 8)).Background(theme.RaisedFill(t)).Shrink(0)
		field.Children(func() {
			ui.Icon(c, icons.Search).FontSize(theme.IconMedium(t)).TextColor(t.TextMuted)
			in = ui.TextInputBase(c, &p.query).Grow(1).MinWidth(0).FocusRing(false).Label(s.text.PaletteGoTo).
				Placeholder(s.text.PaletteGoTo)
			if p.focus {
				in.Focus()
				p.focus = false
			}
		})
		field.Shadow(0, 0, 0, 2, t.Focus)

		// The arrows move the highlight, and wrap, while the caret stays in the text. They come through OnTextKey, between
		// frames; the row is announced at once.
		n := len(shown)
		in.OnTextKey(func(k ui.TextKey) bool {
			if k.Mods != 0 || n == 0 || k.Key != ui.KeyDown && k.Key != ui.KeyUp {
				return false
			}
			step := 1
			if k.Key == ui.KeyUp {
				step = -1
			}
			p.hl, p.reveal = (p.hl+step+n)%n, true
			s.announce = s.paletteLabel(shown[p.hl])
			return true
		})
		in.OnShortcut(ui.Cmd, ui.KeyK, func() { p.open = false })
		if in.Submitted() && n > 0 {
			run = shown[p.hl].run
		}

		if n == 0 {
			ui.Row(c).FillWidth().Justify(ui.Center).Padding(theme.Px(t, 20), theme.Px(t, 8), theme.Px(t, 24), theme.Px(t, 8)).Role(ui.RoleStatus).
				Children(func() {
					ui.Text(c, s.text.PaletteNone).TextColor(t.TextMuted).TextAlign(ui.Center)
				})
			return
		}
		if p.reveal {
			p.list.ScrollIntoView(at[p.hl])
			p.reveal = false
		}
		list := ui.List(c, &p.list, len(lines), func(i int) {
			line := lines[i]
			if line.header {
				s.paletteHeader(c, line.group)
				return
			}
			row := s.paletteRow(c, shown[line.item], line.item == p.hl, query != "")
			if line.item == p.hl {
				in.ActiveDescendant(row)
			}
			if row.Clicked() {
				run = shown[line.item].run
			}
		})
		list.Height(min(theme.Px(t, listHeight), theme.Px(t, paletteList), height*paletteShare)).Padding(0, theme.Px(t, 8), theme.Px(t, paletteBottom), theme.Px(t, 8)).
			Label(s.text.PaletteGoTo)
	})

	s.paletteCount(c, query, len(shown), now)
	if run != nil {
		p.open = false
		run()
	}
}

// paletteHeader is the muted header over the rows of a group.
func (s *Screen) paletteHeader(c *ui.Context, group int) {
	t := c.Theme()
	ui.Row(c).FillWidth().Height(theme.Px(t, paletteHead)).AlignItems(ui.End).Padding(0, theme.Px(t, 8), theme.Px(t, 4), theme.Px(t, 8)).Children(func() {
		ui.Text(c, strings.ToUpper(s.groupName(group))).FontSize(theme.Px(t, 11)).FontWeight(500).TextColor(t.TextMuted).
			Role(ui.RoleHeading).Level(3)
	})
}

// paletteRow is a row: its icon, its name, then at the right its group in muted text when the rows are ranked, and its
// unread count or the keys that do the same.
func (s *Screen) paletteRow(c *ui.Context, it paletteItem, highlighted, ranked bool) ui.Element {
	t := c.Theme()
	row := ui.Row(c).FillWidth().Height(theme.Px(t, paletteRow)).AlignItems(ui.Center).Gap(theme.Px(t, 8)).PaddingX(theme.Px(t, 8)).
		Radius(theme.Px(t, 6)).Role(ui.RoleListItem).Label(s.paletteLabel(it)).KeepFocus()
	switch {
	case highlighted:
		row.Background(theme.Chosen(t))
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		if it.icon != nil {
			ui.Icon(c, it.icon).FontSize(theme.IconMedium(t)).TextColor(t.TextMuted)
		} else {
			s.glyph(c, it.name, it.site, 16)
		}
		ui.Text(c, it.name).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").FontSize(theme.Px(t, 13))
		if ranked {
			ui.Text(c, s.groupName(it.group)).FontSize(theme.Px(t, 11)).TextColor(t.TextMuted).NoWrap().Shrink(0)
		}
		if it.count > 0 {
			ui.Text(c, countText(it.count)).FontSize(theme.Px(t, 11)).FontWeight(500).FontFeatures("tnum").
				TextColor(t.TextMuted).NoWrap().Shrink(0)
		}
		if len(it.keys) > 0 {
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Px(t, 4)).Shrink(0).Children(func() {
				for _, k := range it.keys {
					ui.Box(c).MinWidth(theme.Px(t, 22)).Height(theme.Px(t, 22)).PaddingX(theme.Px(t, 6)).Center().Radius(6).
						Background(theme.Track(t)).Children(func() { ui.Text(c, k).FontSize(theme.Px(t, 11)).FontWeight(500).NoWrap() })
				}
			})
		}
	})
	return row
}

// paletteCount announces how many rows match once the query has stayed the same for a short pause.
func (s *Screen) paletteCount(c *ui.Context, query string, n int, now time.Time) {
	p := &s.pal
	text := ""
	switch {
	case query == "":
	case n == 0:
		text = s.text.PaletteNone
	case n == 1:
		text = s.text.PaletteCountOne
	default:
		text = s.format(s.text.PaletteCount, n)
	}
	if text != p.said {
		p.said, p.saidAt = text, now
	}
	if p.said == "" || p.said == p.told {
		return
	}
	if wait := paletteAnnounce - now.Sub(p.saidAt); wait > 0 {
		c.After(wait)
		return
	}
	c.Announce(p.said)
	p.told = p.said
}
