package timeline

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// foldGroup is the entries of one subscription on one day that sit over its daily cap, drawn as one row.
type foldGroup struct {
	source  int64
	day     int
	name    string
	entries []client.Entry // in the order of the list
}

// foldable tells whether the list folds: the column layouts do, the grid and the wall show every entry.
func (s *Screen) foldable() bool { return s.layout() == columnLayout }

// take keeps the entries over their subscription's daily cap out of a page of the list, in s.folds, and returns the
// rest. The grid and the wall, and any list the server sends no folded entry for, are returned as they are.
func (s *Screen) take(page []client.Entry) []client.Entry {
	if !s.foldable() || !slices.ContainsFunc(page, func(e client.Entry) bool { return e.Folded }) {
		return page
	}
	shown := make([]client.Entry, 0, len(page))
	for _, e := range page {
		switch {
		case !e.Folded:
			shown = append(shown, e)
		case !slices.ContainsFunc(s.folds, func(f client.Entry) bool { return f.ID == e.ID }):
			s.folds = append(s.folds, e)
		}
	}
	return shown
}

// folded reports whether the list keeps e out of sight in a fold row.
func (s *Screen) folded(e client.Entry) bool { return e.Folded && s.foldable() }

// day is the calendar day of e on this device.
func (s *Screen) day(e client.Entry) int {
	at := time.UnixMilli(e.CreatedAt).In(s.deps.Now().Location())
	return at.Year()*400 + at.YearDay()
}

// foldGroups groups the entries kept out of sight by subscription and day, in the order of the list.
func (s *Screen) foldGroups() []foldGroup {
	if len(s.folds) == 0 {
		return nil
	}
	folds := slices.Clone(s.folds)
	slices.SortStableFunc(folds, func(a, b client.Entry) int {
		switch {
		case a.ID == b.ID:
			return 0
		case s.after(a, b):
			return 1
		}
		return -1
	})
	var groups []foldGroup
	for _, e := range folds {
		day := s.day(e)
		i := slices.IndexFunc(groups, func(g foldGroup) bool { return g.source == e.SourceID && g.day == day })
		if i < 0 {
			groups = append(groups, foldGroup{source: e.SourceID, day: day, name: e.SourceTitle})
			i = len(groups) - 1
		}
		groups[i].entries = append(groups[i].entries, e)
	}
	return groups
}

// foldRows places the fold rows: each goes above the first entry that comes after the first entry it holds, or after
// the last entry when none does. The key is the index of the entry, len(s.entries) for the end.
func (s *Screen) foldRows() map[int][]foldGroup {
	groups := s.foldGroups()
	if len(groups) == 0 {
		return nil
	}
	rows := map[int][]foldGroup{}
	for _, g := range groups {
		at := slices.IndexFunc(s.entries, func(e client.Entry) bool { return s.after(e, g.entries[0]) })
		if at < 0 {
			at = len(s.entries)
		}
		rows[at] = append(rows[at], g)
	}
	return rows
}

// foldRow is the row of g: its glyph, how many entries it holds, and a chevron. It is a button that shows them.
func (s *Screen) foldRow(c *ui.Context, g foldGroup) {
	t := c.Theme()
	n := len(g.entries)
	text := strings.ReplaceAll(s.format(s.text.FoldMore, n), "{name}", g.name)
	label := strings.ReplaceAll(s.format(s.text.FoldLabel, n), "{name}", g.name)
	b := ui.ButtonBase(c).Key("fold"+strconv.FormatInt(g.source, 10)+"-"+strconv.Itoa(g.day)).FillWidth().
		Height(theme.Px(t, 36)).AlignItems(ui.Center).Gap(theme.Px(t, 12)).Padding(0, theme.Px(t, 16), 0, theme.Px(t, 28)).
		Radius(theme.Px(t, 10)).Label(label)
	switch {
	case b.Pressed():
		b.Background(t.SurfacePressed)
	case b.Hovered() || b.Focused():
		b.Background(t.SurfaceHover)
	}
	theme.Fade(b)
	b.Children(func() {
		s.glyph(c, g.name, s.iconOf(g.source), 16)
		ui.Text(c, text).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
		ui.Icon(c, icons.ChevronDown).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
	})
	if b.Clicked() {
		s.unfolding = &g // the list is being built: the entries change when it is done
	}
}

// unfold puts the entries of g in their places in the list, keeps the choice on its entry, and gives the focus to
// the first of them, as the row it replaces is gone.
func (s *Screen) unfold(g foldGroup) {
	var selected, focus int64
	if s.selected >= 0 && s.selected < len(s.entries) {
		selected = s.entries[s.selected].ID
	}
	if s.focusRow >= 0 && s.focusRow < len(s.entries) {
		focus = s.entries[s.focusRow].ID
	}
	s.folds = slices.DeleteFunc(s.folds, func(f client.Entry) bool {
		return slices.ContainsFunc(g.entries, func(e client.Entry) bool { return e.ID == f.ID })
	})
	entries := slices.Clone(s.entries)
	for _, e := range g.entries {
		e.Folded = false
		at := slices.IndexFunc(entries, func(x client.Entry) bool { return s.after(x, e) })
		if at < 0 {
			at = len(entries)
		}
		entries = slices.Insert(entries, at, e)
	}
	s.entries = entries
	s.overlay()
	s.selected, s.hovered = s.index(selected), -1
	s.focusRow = s.index(focus)
	if focus == 0 {
		s.focusRow = s.index(g.entries[0].ID)
	}
	s.announce = s.format(s.text.FoldShown, len(g.entries))
}

// unfoldClicked shows the group whose row was clicked in this frame, once the list is built.
func (s *Screen) unfoldClicked() {
	if g := s.unfolding; g != nil {
		s.unfolding = nil
		s.unfold(*g)
	}
}

// dropFolds forgets the folded entries among ids, which are read now.
func (s *Screen) dropFolds(ids []int64) {
	s.folds = slices.DeleteFunc(s.folds, func(e client.Entry) bool { return slices.Contains(ids, e.ID) })
}
