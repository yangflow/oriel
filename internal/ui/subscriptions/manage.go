package subscriptions

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// manageParallel is how many requests a bulk action has in flight at once; there is no bulk endpoint, the client asks
// once per subscription.
const manageParallel = 4

// unchanged is the choice of the change sheet that leaves a field as it is.
const unchanged int64 = -2

// heardAfter is the quiet time after the selection changed before a screen reader hears the count.
const heardAfter = 500 * time.Millisecond

// bulkToastID is the ID of the toast that offers to undo a bulk delete.
const bulkToastID = deletedToast + "bulk"

// The actions a bulk run does.
const (
	jobPause = iota
	jobResume
	jobChange
	jobDelete
	jobRestore
)

// failure is why one subscription of a bulk run was not done; gone says the server does not have it any more.
type failure struct {
	reason, detail string
	gone           bool
}

// job is a bulk run: one request per subscription, at most manageParallel at once.
type job struct {
	kind     int
	patch    client.SourcePatch
	ids      []int64
	srcs     map[int64]client.Source
	next     int // the index in ids of the next request to start
	inflight int
	done     int
	okIDs    []int64
	deleted  []client.Source // what the run deleted, to undo
	failed   []int64
	fails    map[int64]failure
	merge    bool // a delete that retries the failed ones of an earlier run: its undo joins the toast on screen
}

// redo is what Try again repeats.
type redo struct {
	kind  int
	patch client.SourcePatch
}

// bulkUndo is the subscriptions the bulk delete toast restores; it lives in the client's memory only.
type bulkUndo struct {
	srcs    []client.Source
	shown   bool
	version int
}

// managing is the manage dialog.
type managing struct {
	open         bool
	list         ui.ScrollState
	filter       string
	focusFilter  bool
	selected     map[int64]bool
	anchor       int64 // where Shift extends the selection from
	cursor       int64 // the row that has the keyboard focus, 0 for none
	focusRow     int64 // the row to move the focus to at the next frame
	heardAt      time.Time
	heardPending bool
	run          *job
	again        redo
	band         string
	fails        map[int64]failure
	failed       []int64
	sheet        struct {
		open              bool
		form              ui.ScrollState
		every, kind, keep int64
	}
	confirm bool
}

// HasSources reports whether there is a subscription to manage.
func (s *Screen) HasSources() bool { return s.deps.Sources != nil && len(s.deps.Sources()) > 0 }

// Manage opens the dialog that changes several subscriptions at once.
func (s *Screen) Manage() {
	if s.api == nil || !s.HasSources() {
		return
	}
	run := s.manage.run
	s.manage = managing{open: true, focusFilter: true, selected: map[int64]bool{}, run: run}
}

// put fills the {name} placeholders of text, given as name, value pairs.
func put(text string, pairs ...string) string {
	for i := 0; i+1 < len(pairs); i += 2 {
		text = strings.ReplaceAll(text, "{"+pairs[i]+"}", pairs[i+1])
	}
	return text
}

// visible is the subscriptions the filter lets through.
func (s *Screen) visible() []client.Source {
	var all []client.Source
	if s.deps.Sources != nil {
		all = s.deps.Sources()
	}
	q := strings.ToLower(strings.TrimSpace(s.manage.filter))
	if q == "" {
		return all
	}
	var out []client.Source
	for _, src := range all {
		for _, text := range []string{Name(src), src.Title, src.URL, src.RSSHubPath} {
			if strings.Contains(strings.ToLower(text), q) {
				out = append(out, src)
				break
			}
		}
	}
	return out
}

func indexOf(rows []client.Source, id int64) int {
	return slices.IndexFunc(rows, func(src client.Source) bool { return src.ID == id })
}

// touch notes that the selection changed, so a screen reader hears the count once the changes stop.
func (s *Screen) touch(c *ui.Context) {
	m := &s.manage
	m.heardPending, m.heardAt = true, c.Now()
	c.After(heardAfter)
	s.clearBand()
}

func (s *Screen) selectOnly(c *ui.Context, id int64) {
	m := &s.manage
	m.selected = map[int64]bool{id: true}
	m.anchor = id
	s.touch(c)
}

func (s *Screen) toggle(c *ui.Context, id int64) {
	m := &s.manage
	if m.selected[id] {
		delete(m.selected, id)
	} else {
		m.selected[id] = true
	}
	m.anchor = id
	s.touch(c)
}

// rangeTo selects the rows from the anchor to id, and nothing else.
func (s *Screen) rangeTo(c *ui.Context, rows []client.Source, id int64) {
	m := &s.manage
	from, to := indexOf(rows, m.anchor), indexOf(rows, id)
	if to < 0 {
		return
	}
	if from < 0 {
		m.anchor, from = id, to
	}
	if from > to {
		from, to = to, from
	}
	m.selected = map[int64]bool{}
	for _, src := range rows[from : to+1] {
		m.selected[src.ID] = true
	}
	s.touch(c)
}

func (s *Screen) selectAll(c *ui.Context, rows []client.Source) {
	m := &s.manage
	m.selected = map[int64]bool{}
	for _, src := range rows {
		m.selected[src.ID] = true
	}
	s.touch(c)
}

func (s *Screen) clearSelection(c *ui.Context) {
	s.manage.selected = map[int64]bool{}
	s.touch(c)
}

// hear announces the count of the selection once it stopped changing.
func (s *Screen) hear(c *ui.Context) {
	m := &s.manage
	if !m.heardPending {
		return
	}
	if wait := heardAfter - c.Now().Sub(m.heardAt); wait > 0 {
		c.After(wait)
		return
	}
	m.heardPending = false
	if n := len(m.selected); n > 0 {
		s.announce = s.count(s.text.SelectedN, n)
	} else {
		s.announce = s.text.NothingSelected
	}
}

// selection is the selected subscriptions in list order.
func (s *Screen) selection(rows []client.Source) []client.Source {
	var out []client.Source
	for _, src := range rows {
		if s.manage.selected[src.ID] {
			out = append(out, src)
		}
	}
	return out
}

// settingsOf is the line of settings under the name of a row.
func (s *Screen) settingsOf(src client.Source) string {
	var every string
	switch {
	case src.FetchIntervalAuto && src.FetchIntervalSec > 0:
		every = put(s.text.AutoEvery, "every", s.every(src.FetchIntervalSec))
	case src.FetchIntervalAuto:
		every = s.text.Auto
	default:
		every = put(s.text.EveryOf, "x", s.every(src.FetchIntervalSec))
	}
	keep := s.text.KeepSet
	switch {
	case src.KeepDays == -1:
		keep = s.text.KeepAll
	case src.KeepDays > 0:
		keep = put(s.text.KeepOf, "keep", s.keepName(int64(src.KeepDays)))
	}
	return strings.Join([]string{every, s.kindName(src.Kind), keep}, s.text.Separator)
}

func (s *Screen) rowLabel(src client.Source, selected bool) string {
	parts := []string{Name(src), s.settingsOf(src)}
	if src.Paused {
		parts = append(parts, s.text.PausedMark)
	}
	if f, ok := s.manage.fails[src.ID]; ok {
		parts = append(parts, f.reason)
	} else if src.LastError != "" {
		parts = append(parts, s.text.FailingMark)
	}
	if selected {
		parts = append(parts, s.text.SelectedMark)
	}
	return strings.Join(parts, s.text.LabelSep)
}

// checkBox draws the 16 DIP box of a row or of select all: state 0 off, 1 on, 2 partly on.
func checkBox(c *ui.Context, key string, state int) ui.Element {
	t := c.Theme()
	b := ui.Box(c).Key(key).Size(theme.Px(t, 16), theme.Px(t, 16)).Radius(5).Center().Role(ui.RoleNone)
	if state == 0 {
		return b.Background(theme.RaisedFill(t)).Border(1.5, theme.LineStrong(t))
	}
	icon := icons.Check
	if state == 2 {
		icon = icons.Minus
	}
	return b.Background(t.Accent).Children(func() {
		ui.Icon(c, icon).FontSize(theme.Px(t, 12)).TextColor(t.AccentText)
	})
}

func (s *Screen) manageRow(c *ui.Context, rows []client.Source, src client.Source, locked bool) {
	t := c.Theme()
	m := &s.manage
	id := src.ID
	sel := m.selected[id]
	f, failed := m.fails[id]
	row := theme.Fade(ui.ButtonBase(c).Key("mrow" + strconv.FormatInt(id, 10)))
	row.Role(ui.RoleListItem).Selected(sel).Label(s.rowLabel(src, sel)).Disabled(locked)
	if failed {
		row.Description(f.detail)
	}
	row.FillWidth().MinHeight(theme.Px(t, 48)).Padding(theme.Px(t, 6), theme.Px(t, 12)).Radius(8).
		Gap(theme.Px(t, 12)).AlignItems(ui.Center)
	switch {
	case sel:
		row.Background(theme.Soft(t, t.Accent))
	case row.Hovered() && !locked:
		row.Background(t.SurfaceHover)
	}
	if locked && m.run != nil {
		row.Opacity(0.55)
	}
	if row.Focused() {
		m.cursor = id
	}
	if m.focusRow == id {
		m.focusRow = 0
		row.Focus().ScrollIntoView()
	}
	var boxClick ui.Element
	row.Children(func() {
		state := 0
		if sel {
			state = 1
		}
		boxClick = checkBox(c, "mchk"+strconv.FormatInt(id, 10), state)
		boxClicked := boxClick.Clicked()
		mods := boxClick.ClickModifiers()
		if boxClicked {
			if mods&ui.Shift != 0 {
				s.rangeTo(c, rows, id)
			} else {
				s.toggle(c, id)
			}
		}
		s.glyph(c, src.Kind, src)
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(theme.Px(t, 1)).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Px(t, 6)).Children(func() {
				if src.Paused {
					ui.Icon(c, icons.Pause).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
				} else if src.LastError != "" {
					ui.Icon(c, icons.TriangleAlert).FontSize(theme.IconSmall(t)).TextColor(t.Warning)
				}
				name := ui.Text(c, Name(src)).FontSize(theme.Px(t, 13)).SingleLine().Shrink(1).MinWidth(0)
				if src.Paused {
					name.TextColor(t.TextMuted)
				} else {
					name.FontWeight(500)
				}
			})
			ui.Text(c, s.settingsOf(src)).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).SingleLine()
			if failed {
				ui.Row(c).AlignItems(ui.Start).Gap(theme.Px(t, 6)).Children(func() {
					ui.Icon(c, icons.CircleAlert).FontSize(theme.IconSmall(t)).TextColor(t.Danger).Margin(1, 0, 0, 0)
					ui.Text(c, f.reason).FontSize(theme.Px(t, 12)).TextColor(t.Danger).Shrink(1).Wrap()
				})
			}
		})
	})
	clicked := row.Clicked()
	if locked {
		return
	}
	switch {
	case row.Shortcut(0, ui.KeySpace):
		s.toggle(c, id)
	case row.Shortcut(ui.Shift, ui.KeyDown):
		s.extend(c, rows, id, 1)
	case row.Shortcut(ui.Shift, ui.KeyUp):
		s.extend(c, rows, id, -1)
	case row.Shortcut(ui.Super, ui.KeyA), row.Shortcut(ui.Ctrl, ui.KeyA):
		s.selectAll(c, rows)
	case row.Shortcut(ui.Shift, ui.KeyTab):
		m.focusFilter = true
	case clicked:
		switch mods := row.ClickModifiers(); {
		case mods&ui.Shift != 0:
			s.rangeTo(c, rows, id)
		case mods&(ui.Super|ui.Ctrl) != 0:
			s.toggle(c, id)
		default:
			s.selectOnly(c, id)
		}
		m.cursor = id
	}
}

// extend grows the selection by one row up or down and moves the focus there.
func (s *Screen) extend(c *ui.Context, rows []client.Source, id int64, step int) {
	m := &s.manage
	i := indexOf(rows, id) + step
	if i < 0 || i >= len(rows) {
		return
	}
	if len(m.selected) == 0 {
		m.anchor = id
	}
	s.rangeTo(c, rows, rows[i].ID)
	m.focusRow = rows[i].ID
}

// escape is what Esc and a click on the backdrop do: step back out of the dialog one level at a time.
func (s *Screen) escape(c *ui.Context) {
	m := &s.manage
	switch {
	case m.run != nil, m.sheet.open, m.confirm:
	case len(m.selected) > 0:
		s.clearSelection(c)
	default:
		s.clearBand()
		m.open = false
	}
}

// common says what every one of srcs has: the name of the value, or mixed.
func (s *Screen) common(srcs []client.Source, of func(client.Source) string) string {
	if len(srcs) == 0 {
		return s.text.NowMixed
	}
	first := of(srcs[0])
	for _, src := range srcs[1:] {
		if of(src) != first {
			return s.text.NowMixed
		}
	}
	return put(s.text.NowOne, "x", first)
}

func (s *Screen) manageDialog(c *ui.Context) {
	t := c.Theme()
	m := &s.manage
	_, height := c.Size()
	rows := s.visible()
	for id := range m.selected {
		if indexOf(rows, id) < 0 {
			delete(m.selected, id)
		}
	}
	locked := m.run != nil
	keep := true
	ui.DialogBase(c, &keep, func(backdrop, panel ui.Element) {
		theme.Scrim(c, backdrop).Padding(t.Space(4))
		panel.Column().Width(theme.Px(t, 640)).MaxHeight(height - t.Space(8)).Label(s.text.ManageTitle)
		theme.Sheet(c, panel, theme.Dialog)
		s.headerBusy(c, &keep, func() { tile(c, icons.ListChecks) }, s.text.ManageTitle, s.text.ManageSub, locked)
		ui.Column(c).FillWidth().Gap(theme.Px(t, 8)).Padding(0, t.Space(5), theme.Px(t, 8), t.Space(5)).Children(func() {
			s.filterField(c, rows)
			if m.band != "" {
				s.band(c)
			}
			s.actionBar(c, rows)
		})
		ui.Box(c).FillWidth().Height(1).Background(t.Border)
		ui.Scroll(c).TrackScroll(&m.list).FillWidth().Shrink(1).MinHeight(0).Children(func() {
			list := ui.Column(c).FillWidth().Padding(theme.Px(t, 8), t.Space(4)).Role(ui.RoleList).Label(s.text.ManageTitle).
				FocusGroup(ui.Vertical)
			list.Children(func() {
				if len(rows) == 0 {
					ui.Text(c, s.text.NoMatch).TextColor(t.TextMuted).Padding(t.Space(6), 0).Role(ui.RoleStatus)
				}
				for _, src := range rows {
					s.manageRow(c, rows, src, locked)
				}
			})
		})
		ui.Box(c).FillWidth().Height(1).Background(t.Border)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Padding(t.Space(4), t.Space(5), t.Space(4.5),
			t.Space(5)).Children(func() {
			ui.Text(c, s.text.ManageHint).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Grow(1).MinWidth(0)
			if (theme.Button{Style: theme.Secondary, Text: s.text.Close, Disabled: locked}).Draw(c).Clicked() {
				keep = false
			}
		})
	})
	if !keep {
		s.escape(c)
	}
	if m.sheet.open {
		s.changeSheet(c, s.selection(rows))
	}
	if m.confirm {
		n := len(m.selected)
		if ui.AlertDialog(c, &m.confirm, s.count(s.text.DeleteManyTitle, n), s.text.DeleteManyBody, s.text.Cancel,
			s.text.ConfirmDelete) == 1 {
			s.startJob(jobDelete, client.SourcePatch{}, ids(s.selection(rows)), false)
		}
	}
	s.hear(c)
}

func ids(srcs []client.Source) []int64 {
	out := make([]int64, len(srcs))
	for i, src := range srcs {
		out[i] = src.ID
	}
	return out
}

func (s *Screen) filterField(c *ui.Context, rows []client.Source) {
	t := c.Theme()
	m := &s.manage
	b := box(c, "mfilter", icons.Search, false, t.Surface, func() {
		in := ui.TextInputBase(c, &m.filter).Grow(1).MinWidth(0).FocusRing(false).Label(s.text.FilterLabel).
			Placeholder(s.text.FilterPlaceholder).Disabled(m.run != nil)
		if m.focusFilter {
			m.focusFilter = false
			in.Focus()
		}
	})
	if b.Shortcut(0, ui.KeyDown) && len(rows) > 0 {
		m.focusRow = rows[0].ID
		if indexOf(rows, m.cursor) >= 0 {
			m.focusRow = m.cursor
		}
	}
}

// selectState is 0, 1 or 2 for none, all or some of the visible rows selected.
func (s *Screen) selectState(rows []client.Source) int {
	n := len(s.manage.selected)
	switch {
	case n == 0:
		return 0
	case n >= len(rows):
		return 1
	}
	return 2
}

func (s *Screen) selectAllButton(c *ui.Context, rows []client.Source) {
	m := &s.manage
	state := s.selectState(rows)
	b := ui.ButtonBase(c).Key("mall").Role(ui.RoleCheckBox).Checked(state == 1).Label(s.text.SelectAll).
		Disabled(len(rows) == 0 || m.run != nil).AlignItems(ui.Center).Radius(5)
	if state == 2 {
		b.Mixed()
	}
	b.Children(func() { checkBox(c, "mallbox", state) })
	if b.Clicked() {
		if state == 1 {
			s.clearSelection(c)
		} else {
			s.selectAll(c, rows)
		}
	}
}

func (s *Screen) actionBar(c *ui.Context, rows []client.Source) {
	t := c.Theme()
	m := &s.manage
	sel := s.selection(rows)
	n := len(sel)
	switch {
	case m.run != nil:
		j := m.run
		text := put([]string{s.text.WorkingPause, s.text.WorkingResume, s.text.WorkingChange, s.text.WorkingDelete,
			s.text.WorkingRestore}[j.kind], "done", strconv.Itoa(j.done), "total", strconv.Itoa(len(j.ids)))
		ui.Row(c).FillWidth().Height(theme.Px(t, 40)).AlignItems(ui.Center).Gap(theme.Px(t, 8)).
			Padding(0, theme.Px(t, 12)).Radius(10).Background(theme.Soft(t, t.Accent)).Role(ui.RoleStatus).Label(text).
			Children(func() {
				spin := ui.Icon(c, icons.LoaderCircle).FontSize(theme.IconMedium(t)).TextColor(t.Accent)
				spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
				ui.Text(c, text).FontSize(theme.Px(t, 13))
			})
	case n == 0:
		ui.Row(c).FillWidth().Height(theme.Px(t, 40)).AlignItems(ui.Center).Gap(theme.Px(t, 8)).
			Padding(0, theme.Px(t, 12)).Radius(10).Children(func() {
			s.selectAllButton(c, rows)
			ui.Text(c, s.text.SelectAll).FontSize(theme.Px(t, 13)).Grow(1)
			ui.Text(c, s.count(s.text.OfN, len(rows))).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted)
		})
	default:
		anyPaused := slices.ContainsFunc(sel, func(src client.Source) bool { return src.Paused })
		anyLive := slices.ContainsFunc(sel, func(src client.Source) bool { return !src.Paused })
		ui.Row(c).FillWidth().MinHeight(theme.Px(t, 40)).AlignItems(ui.Center).Gap(theme.Px(t, 8)).
			Padding(0, theme.Px(t, 12)).Radius(10).Background(theme.Soft(t, t.Accent)).Role(ui.RoleToolbar).
			Label(s.count(s.text.BarLabel, n)).Children(func() {
			s.selectAllButton(c, rows)
			ui.Text(c, s.count(s.text.SelectedN, n)).FontSize(theme.Px(t, 13)).FontWeight(500).Grow(1).MinWidth(0)
			if anyLive && (theme.Button{Small: true, Icon: icons.Pause, Text: s.text.Pause, Key: "mpause",
				Label: s.count(s.text.PauseNLabel, len(pending(sel, true)))}).Draw(c).Clicked() {
				s.startJob(jobPause, client.SourcePatch{Paused: new(true)}, ids(pending(sel, true)), false)
			}
			if anyPaused && (theme.Button{Small: true, Icon: icons.Play, Text: s.text.Resume, Key: "mresume",
				Label: s.count(s.text.ResumeNLabel, len(pending(sel, false)))}).Draw(c).Clicked() {
				s.startJob(jobResume, client.SourcePatch{Paused: new(false)}, ids(pending(sel, false)), false)
			}
			if (theme.Button{Small: true, Icon: icons.SlidersHorizontal, Text: s.text.ChangeN, Key: "mchange",
				Label: s.count(s.text.ChangeNLabel, n)}).Draw(c).Clicked() {
				m.sheet.open = true
				m.sheet.every, m.sheet.kind, m.sheet.keep = unchanged, unchanged, unchanged
			}
			if (theme.Button{Style: theme.Danger, Small: true, Icon: icons.Trash2, Text: s.text.Delete, Key: "mdelete",
				Label: s.count(s.text.DeleteNLabel, n)}).Draw(c).Clicked() {
				m.confirm = true
			}
			if (theme.Button{Style: theme.Plain, Small: true, Icon: icons.X, Key: "mclear",
				Label: s.text.ClearSelection}).Draw(c).Clicked() {
				s.clearSelection(c)
			}
		})
	}
}

// pending is the subscriptions of srcs that a pause (paused false) or a resume (true) still has to change.
func pending(srcs []client.Source, pause bool) []client.Source {
	return slices.DeleteFunc(slices.Clone(srcs), func(src client.Source) bool { return src.Paused == pause })
}

// band is the result of the last run that did not finish whole, with Try again and Dismiss.
func (s *Screen) band(c *ui.Context) {
	t := c.Theme()
	m := &s.manage
	retry := slices.ContainsFunc(m.failed, func(id int64) bool { return !m.fails[id].gone })
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, 8)).Padding(theme.Px(t, 8), theme.Px(t, 8),
		theme.Px(t, 8), theme.Px(t, 12)).Radius(10).Background(theme.Soft(t, t.Danger)).Role(ui.RoleStatus).
		Label(m.band).Children(func() {
		ui.Icon(c, icons.CircleAlert).FontSize(theme.IconMedium(t)).TextColor(t.Danger)
		ui.Text(c, m.band).FontSize(theme.Px(t, 12)).Grow(1).MinWidth(0).Wrap()
		if retry && (theme.Button{Small: true, Text: s.text.RetryFailed, Key: "mretry", Disabled: m.run != nil}).Draw(c).Clicked() {
			s.retry()
		}
		if (theme.Button{Style: theme.Plain, Small: true, Text: s.text.DismissResult, Key: "mdismiss"}).Draw(c).Clicked() {
			s.clearBand()
		}
	})
}

// clearBand dismisses the result of the last run. Subscriptions the server did not have any more leave the list.
func (s *Screen) clearBand() {
	m := &s.manage
	for _, id := range m.failed {
		if m.fails[id].gone {
			delete(m.selected, id)
			s.dropHealth(id)
			s.deps.Removed(id)
		}
	}
	m.band, m.fails, m.failed = "", nil, nil
}

func (s *Screen) retry() {
	m := &s.manage
	var left []int64
	for _, id := range m.failed {
		if !m.fails[id].gone {
			left = append(left, id)
		}
	}
	again := m.again
	s.clearBand()
	s.startJob(again.kind, again.patch, left, again.kind == jobDelete)
}

func (s *Screen) changeSheet(c *ui.Context, sel []client.Source) {
	t := c.Theme()
	m := &s.manage
	sh := &m.sheet
	if len(sel) == 0 {
		sh.open = false
		return
	}
	s.dialog(c, &sh.open, &sh.form, 440, s.count(s.text.ChangeTitle, len(sel)), func() {
		s.header(c, &sh.open, func() { tile(c, icons.SlidersHorizontal) }, s.count(s.text.ChangeTitle, len(sel)), s.text.ChangeSub)
	}, func() {
		field := func(value *int64, options []int64, text string, name func(int64) string, of func(client.Source) string) {
			ui.Column(c).FillWidth().Gap(theme.Px(t, 6)).Children(func() {
				shown := s.text.KeepAsIs
				if *value != unchanged {
					shown = name(*value)
				}
				opts := append([]int64{unchanged}, options...)
				s.choiceSelect(c, value, opts, text, shown, func(v int64) string {
					if v == unchanged {
						return s.text.KeepAsIs
					}
					return name(v)
				}, false, *value == unchanged)
				help(c, s.common(sel, of))
			})
		}
		kindIdx := make([]int64, len(kinds))
		for i := range kinds {
			kindIdx[i] = int64(i)
		}
		field(&sh.every, intervals, s.text.Every, s.every, func(src client.Source) string { return s.every(intervalChoice(src)) })
		field(&sh.kind, kindIdx, s.text.ShowAs, func(i int64) string { return s.kindName(kinds[i]) },
			func(src client.Source) string { return s.kindName(src.Kind) })
		field(&sh.keep, keeps, s.text.Keep, s.keepName, func(src client.Source) string { return s.keepName(int64(src.KeepDays)) })
	}, func() {
		ui.Box(c).Grow(1)
		if (theme.Button{Text: s.text.Cancel}).Draw(c).Clicked() {
			sh.open = false
		}
		if (theme.Button{Style: theme.Primary, Text: s.text.Apply,
			Disabled: sh.every == unchanged && sh.kind == unchanged && sh.keep == unchanged}).Draw(c).Clicked() {
			var patch client.SourcePatch
			if sh.every != unchanged {
				if sh.every == 0 {
					patch.FetchIntervalAuto = new(true)
				} else {
					patch.FetchIntervalAuto = new(false)
					patch.FetchIntervalSec = new(sh.every)
				}
			}
			if sh.kind != unchanged {
				patch.Kind = new(kinds[sh.kind])
			}
			if sh.keep != unchanged {
				patch.KeepDays = new(int(sh.keep))
			}
			sh.open = false
			s.startJob(jobChange, patch, ids(sel), false)
		}
	})
}

// startJob runs kind for ids, one request each, and tells how it went when the last one is back.
func (s *Screen) startJob(kind int, patch client.SourcePatch, list []int64, merge bool) {
	m := &s.manage
	if s.api == nil || m.run != nil || len(list) == 0 {
		return
	}
	s.clearBand()
	srcs := map[int64]client.Source{}
	if s.deps.Sources != nil {
		for _, src := range s.deps.Sources() {
			srcs[src.ID] = src
		}
	}
	m.again = redo{kind: kind, patch: patch}
	m.run = &job{kind: kind, patch: patch, ids: list, srcs: srcs, fails: map[int64]failure{}, merge: merge}
	s.announce = s.count([]string{s.text.AnnouncePause, s.text.AnnounceResume, s.text.AnnounceChange, s.text.AnnounceDelete,
		s.text.AnnounceRestore}[kind], len(list))
	s.pump()
}

func (s *Screen) pump() {
	j := s.manage.run
	for j.inflight < manageParallel && j.next < len(j.ids) {
		id := j.ids[j.next]
		j.next++
		j.inflight++
		s.step(j, id)
	}
}

func (s *Screen) step(j *job, id int64) {
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		var src client.Source
		var err error
		ctx := context.Background()
		switch j.kind {
		case jobDelete:
			err = api.DeleteSource(ctx, id)
		case jobRestore:
			src, err = api.RestoreSource(ctx, id)
		default:
			src, err = api.UpdateSource(ctx, id, j.patch)
		}
		return func() {
			if gen != s.gen || s.manage.run != j || s.expired(err) {
				return
			}
			s.settle(j, id, src, err)
		}
	})
}

func (s *Screen) settle(j *job, id int64, src client.Source, err error) {
	j.inflight--
	j.done++
	switch {
	case err == nil:
		j.okIDs = append(j.okIDs, id)
		switch j.kind {
		case jobDelete:
			j.deleted = append(j.deleted, j.srcs[id])
			s.dropHealth(id)
			s.deps.Removed(id)
		case jobRestore:
			s.deps.Restored(src)
		default:
			if src.Paused {
				s.dropHealth(id)
			}
			s.deps.Changed(src)
		}
	case j.kind == jobDelete && code(err) == "not_found":
		j.okIDs = append(j.okIDs, id)
		s.dropHealth(id)
		s.deps.Removed(id)
	default:
		f := failure{reason: s.text.RServer, detail: err.Error()}
		switch {
		case errors.Is(err, client.ErrUnreachable):
			f.reason = s.text.RNet
		case code(err) == "not_found":
			f.reason, f.gone = s.text.RGone, true
		}
		j.failed = append(j.failed, id)
		j.fails[id] = f
	}
	s.pump()
	if j.inflight == 0 && j.next >= len(j.ids) {
		s.finish(j)
	}
}

func (s *Screen) finish(j *job) {
	m := &s.manage
	m.run = nil
	for _, id := range j.okIDs {
		delete(m.selected, id)
	}
	ok, bad := len(j.okIDs), len(j.failed)
	if j.kind == jobDelete && len(j.deleted) > 0 {
		s.pushUndo(j.deleted, j.merge)
	}
	if j.kind == jobRestore {
		for _, id := range j.okIDs {
			m.selected[id] = true
		}
		switch {
		case bad == 0:
			s.toast = s.count(s.text.RestoredMany, ok)
		case ok > 0:
			s.toast = put(s.text.RestoredSome, "ok", strconv.Itoa(ok), "bad", strconv.Itoa(bad))
		case !slices.ContainsFunc(j.failed, func(id int64) bool { return !j.fails[id].gone }):
			s.toast = s.text.UndoFailed
		default:
			s.toast = j.fails[j.failed[0]].reason
		}
		return
	}
	if bad == 0 {
		if j.kind == jobDelete && len(j.deleted) == 0 {
			s.toast = s.count(s.text.DeletedMany, ok)
		} else if j.kind != jobDelete {
			s.toast = s.count([]string{s.text.PausedMany, s.text.ResumedMany, s.text.ChangedMany}[j.kind], ok)
		}
		return
	}
	m.fails, m.failed = j.fails, j.failed
	if ok == 0 {
		m.band = put(s.text.AllFailed, "reason", j.fails[j.failed[0]].reason)
	} else {
		partial := []string{s.text.PartialPause, s.text.PartialResume, s.text.PartialChange, s.text.PartialDelete}[j.kind]
		m.band = put(partial, "ok", strconv.Itoa(ok), "bad", strconv.Itoa(bad))
	}
	s.announce = m.band
}

// pushUndo makes srcs what the bulk delete toast restores; merge adds them to the toast on screen, if there is one.
func (s *Screen) pushUndo(srcs []client.Source, merge bool) {
	s.closeBulk = false
	if merge && s.undo != nil {
		s.undo.srcs = append(s.undo.srcs, srcs...)
		s.undo.version++
		s.undo.shown = false
		return
	}
	s.undo = &bulkUndo{srcs: slices.Clone(srcs)}
}

// bulkToast offers Undo for the last bulk delete, which the client holds in memory until the toast is gone.
func (s *Screen) bulkToast(c *ui.Context) {
	if s.closeBulk {
		s.closeBulk = false
		c.CloseToast(bulkToastID)
	}
	u := s.undo
	if u == nil || u.shown {
		return
	}
	u.shown = true
	version := u.version
	c.AddToast(ui.Toast{ID: bulkToastID, Title: s.count(s.text.DeletedMany, len(u.srcs)), Timeout: undoToast,
		Action:   s.text.Undo,
		OnAction: func() { s.restoreMany(u) },
		OnClose: func() {
			if s.undo == u && u.version == version {
				s.undo = nil
			}
		},
		Data: theme.ToastLook{Icon: icons.Trash2, Close: s.text.Dismiss}})
}

// restoreMany takes the bulk delete of u back, one request per subscription.
func (s *Screen) restoreMany(u *bulkUndo) {
	m := &s.manage
	if s.api == nil || m.run != nil {
		return
	}
	if s.undo == u {
		s.undo = nil
	}
	s.startJob(jobRestore, client.SourcePatch{}, ids(u.srcs), false)
}
