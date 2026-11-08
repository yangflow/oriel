package subscriptions

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/theme"
	"github.com/yangflow/oriel/internal/ui/theme/tokens"
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
	jobRefresh
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
	changed  []client.Source // what the run changed
	restored []client.Source // what the run brought back
	failed   []int64
	fails    map[int64]failure
	merge    bool // a delete that retries the failed ones of an earlier run: its undo joins the toast on screen
	ctx      context.Context
	stop     context.CancelFunc // cancels the requests still under way when the server is left
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

// changing is the change sheet: one choice per field, unchanged for the ones it leaves as each subscription has them.
type changing struct {
	open                                                  bool
	form                                                  ui.ScrollState
	kind, folder, every, target, keep, proxy, pause, hide int64
}

// managing is the list of Settings → Subscriptions and its bulk actions.
type managing struct {
	filter       string
	kind         int64 // 0 for all kinds, else 1 + the index in kinds
	focusFilter  bool
	selected     map[int64]bool
	anchor       int64 // where Shift extends the selection from
	cursor       int64 // the row that has the keyboard focus, 0 for none
	focusRow     int64 // the row to move the focus to at the next frame
	heardAt      time.Time
	heardPending bool
	typing       bool // the filter had the focus at the last frame
	run          *job
	again        redo
	band         string
	fails        map[int64]failure
	failed       []int64
	sheet        changing
	confirm      bool
}

// HasSources reports whether there is a subscription to manage.
func (s *Screen) HasSources() bool { return s.deps.Sources != nil && len(s.deps.Sources()) > 0 }

// OpenList shows the list of Settings → Subscriptions, with nothing selected and no result band; a bulk run under way
// goes on.
func (s *Screen) OpenList() {
	s.clearBand()
	m := &s.manage
	*m = managing{selected: map[int64]bool{}, run: m.run, again: m.again}
	s.closePage()
}

// Crumb is the name of the subscription whose page shows, "" while the list shows.
func (s *Screen) Crumb() string {
	if !s.pg.open {
		return ""
	}
	return Name(s.pg.e.src)
}

// Typing reports whether a text field of the list or the page had the focus at the last frame.
func (s *Screen) Typing() bool { return s.manage.typing || (s.pg.open && s.pg.typing) }

// Unsaved reports whether the page has text typed and not saved.
func (s *Screen) Unsaved() bool { return s.pg.open && s.pageDirty() }

// Escape is Escape on Settings → Subscriptions, after a text field has given up the focus: a run under way holds it, a
// page goes back to the list (unless it has text not saved), and a selection is cleared. It reports whether it did
// something; when not, Settings goes on with its own rule.
func (s *Screen) Escape() bool {
	switch {
	case s.manage.run != nil:
		return true
	case s.pg.open:
		if s.pageDirty() {
			return false
		}
		s.Back()
		return true
	case len(s.manage.selected) > 0:
		s.manage.selected = map[int64]bool{}
		s.touch()
		return true
	}
	return false
}

// Page draws Settings → Subscriptions in the page of Settings: the list, or the page of one subscription.
func (s *Screen) Page(c *ui.Context) {
	if s.manage.selected == nil {
		s.manage.selected = map[int64]bool{}
	}
	if s.pg.open {
		s.page(c)
		return
	}
	s.listPage(c)
}

// put fills the {name} placeholders of text, given as name, value pairs.
func put(text string, pairs ...string) string {
	for i := 0; i+1 < len(pairs); i += 2 {
		text = strings.ReplaceAll(text, "{"+pairs[i]+"}", pairs[i+1])
	}
	return text
}

// visible is the subscriptions the filter and the kinds select let through.
func (s *Screen) visible() []client.Source {
	var all []client.Source
	if s.deps.Sources != nil {
		all = s.deps.Sources()
	}
	q := strings.ToLower(strings.TrimSpace(s.manage.filter))
	kind := ""
	if k := s.manage.kind; k > 0 && int(k) <= len(kinds) {
		kind = kinds[k-1]
	}
	var out []client.Source // a new slice: Deps.Removed may shorten the list while a frame is still walking these rows
	for _, src := range all {
		if kind != "" && cmp.Or(src.Kind, kinds[0]) != kind {
			continue
		}
		if q == "" || slices.ContainsFunc([]string{Name(src), src.Title, src.URL, src.RSSHubPath}, func(text string) bool {
			return strings.Contains(strings.ToLower(text), q)
		}) {
			out = append(out, src)
		}
	}
	return out
}

func indexOf(rows []client.Source, id int64) int {
	return slices.IndexFunc(rows, func(src client.Source) bool { return src.ID == id })
}

// touch notes that the selection changed, so a screen reader hears the count once the changes stop, and drops the
// result band.
func (s *Screen) touch() {
	m := &s.manage
	m.heardPending, m.heardAt = true, time.Time{}
	s.clearBand()
}

func (s *Screen) toggle(id int64) {
	m := &s.manage
	if m.selected[id] {
		delete(m.selected, id)
	} else {
		m.selected[id] = true
	}
	m.anchor = id
	s.touch()
}

// rangeTo selects the rows from the anchor to id, and nothing else. An anchor filtered out counts from the row with
// the keyboard focus.
func (s *Screen) rangeTo(rows []client.Source, id int64) {
	m := &s.manage
	to := indexOf(rows, id)
	if to < 0 {
		return
	}
	from := indexOf(rows, m.anchor)
	if from < 0 {
		from = indexOf(rows, m.cursor)
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
	s.touch()
}

func (s *Screen) selectAll(rows []client.Source) {
	m := &s.manage
	m.selected = map[int64]bool{}
	for _, src := range rows {
		m.selected[src.ID] = true
	}
	s.touch()
}

func (s *Screen) clearSelection() {
	s.manage.selected = map[int64]bool{}
	s.touch()
}

// hear announces the count of the selection once it stopped changing.
func (s *Screen) hear(c *ui.Context) {
	m := &s.manage
	if !m.heardPending {
		return
	}
	if m.heardAt.IsZero() {
		m.heardAt = c.Now()
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

// everyOf names the fetch interval of src: "Auto · 6 h" with the interval Auto uses now, else the fixed one.
func (s *Screen) everyOf(src client.Source) string {
	if src.FetchIntervalAuto {
		if src.FetchIntervalSec > 0 {
			return put(s.text.AutoEvery, "every", s.every(src.FetchIntervalSec))
		}
		return s.text.Auto
	}
	return s.every(src.FetchIntervalSec)
}

// folderOf names the folder src is in, or says it is in none.
func (s *Screen) folderOf(src client.Source) string {
	if src.FolderID != 0 {
		if name := s.folderName(src.FolderID); name != "" {
			return name
		}
	}
	return s.text.NoFolder
}

// settingsOf is the meta line under the name of a row: kind, folder, interval.
func (s *Screen) settingsOf(src client.Source) string {
	return strings.Join([]string{s.kindName(src.Kind), s.folderOf(src), s.everyOf(src)}, s.text.Separator)
}

// failingOf is the meta line of a row whose fetches fail: how many times, and why.
func (s *Screen) failingOf(src client.Source) string {
	why := FailureReason(s.lang, src, s.text.Blocked, s.text.BlockedPublic, s.text.FeedRemoved)
	return put(s.count(s.text.RowFailed, max(1, src.ConsecutiveFailures)), "error", why)
}

func (s *Screen) rowLabel(src client.Source, selected bool) string {
	parts := []string{Name(src), s.kindName(src.Kind), s.folderOf(src), s.everyOf(src)}
	if src.Paused {
		parts = append(parts, s.text.PausedMark)
	}
	if src.HideFromAll {
		parts = append(parts, s.text.HiddenMark)
	}
	if f, ok := s.manage.fails[src.ID]; ok {
		parts = append(parts, f.reason)
	} else if src.LastError != "" {
		parts = append(parts, s.failingOf(src))
	}
	if selected {
		parts = append(parts, s.text.SelectedMark)
	}
	return strings.Join(parts, s.text.LabelSep)
}

// checkBox draws the 16 box of a row or of select all: state 0 off, 1 on, 2 partly on.
func checkBox(c *ui.Context, key string, state int) ui.Element {
	t := c.Theme()
	b := ui.Box(c).Key(key).Size(theme.Px(t, tokens.Space4), theme.Px(t, tokens.Space4)).Radius(theme.Px(t, tokens.RadiusXS)).Center().Role(ui.RoleNone)
	if state == 0 {
		return b.Background(theme.RaisedFill(t)).Border(theme.Px(t, tokens.Hairline), theme.LineStrong(t))
	}
	return b.Background(t.Accent).Children(func() {
		if state == 2 {
			ui.Box(c).Size(theme.Px(t, tokens.Space2), theme.Px(t, tokens.Space0_5)).Radius(theme.Px(t, tokens.RadiusXS)).Background(t.AccentText)
			return
		}
		ui.Icon(c, icons.Check).FontSize(theme.IconSmall(t)).TextColor(t.AccentText)
	})
}

// rowGlyph is the glyph of a subscription, side mockup pixels square: the site icon, or the monogram on Track, with
// the radius-xs of every glyph.
func (s *Screen) rowGlyph(c *ui.Context, src client.Source, side float32) {
	t := c.Theme()
	var icon *ui.Bitmap
	if src.IconURL != "" && s.deps.Icon != nil {
		icon = s.deps.Icon(src.IconURL)
	}
	ui.Box(c).Size(theme.Px(t, side), theme.Px(t, side)).Shrink(0).Radius(theme.Px(t, tokens.RadiusXS)).Clip().Children(func() {
		if icon != nil {
			ui.Image(c, icon).Fit(ui.Cover).Fill()
			return
		}
		theme.Monogram(c, Name(src), side)
	})
}

// health is how a subscription stands, as its row's status column and its page's head show it.
type health int

const (
	healthOk health = iota
	healthFailing
	healthGone
	healthPaused
)

func healthOf(src client.Source) health {
	switch {
	case src.Paused:
		return healthPaused
	case src.LastError != "" && src.LastErrorKind == "gone":
		return healthGone
	case src.LastError != "":
		return healthFailing
	}
	return healthOk
}

// statusLine is the status of src in words: the pill's or the dot's text, the line under it and the color of that line.
func (s *Screen) statusLine(t *ui.Theme, src client.Source) (tag, sub string, ink, subInk ui.Color) {
	ago := func(ms int64) string { return locale.Ago(s.lang, s.deps.Now(), time.UnixMilli(ms)) }
	switch healthOf(src) {
	case healthPaused:
		return s.text.PausedMark, s.text.StatusPausedSub, theme.Text2(t), t.TextMuted
	case healthGone:
		return s.text.StatusGone, s.text.StatusGoneSub + s.text.Separator + ago(src.LastFetchedAt), t.Danger, t.Danger
	case healthFailing:
		why := FailureReason(s.lang, src, s.text.Blocked, s.text.BlockedPublic, s.text.FeedRemoved)
		return s.count(s.text.StatusFailed, max(1, src.ConsecutiveFailures)), why + s.text.Separator + ago(src.LastFetchedAt), t.Warning, t.Warning
	}
	sub = s.text.NotChecked
	if src.LastFetchedAt != 0 {
		sub = strings.ReplaceAll(s.text.StatusUpdated, "{t}", ago(src.LastFetchedAt))
	}
	return s.text.StatusOk, sub, theme.Text2(t), t.TextMuted
}

// statusPill is the capsule of a status that calls for a look: its icon and words in ink, on ink at the tint alpha.
func statusPill(c *ui.Context, icon *ui.SVG, text string, ink ui.Color) {
	t := c.Theme()
	h := theme.Px(t, tokens.ControlSmall)
	ui.Row(c).Height(h).Shrink(0).AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space1)).PaddingX(theme.Px(t, tokens.Space2)).
		Radius(theme.Pill(h)).Background(ink.Alpha(tokens.OpacityTint)).Children(func() {
		ui.Icon(c, icon).FontSize(theme.IconSmall(t)).TextColor(ink)
		ui.Text(c, text).FontSize(theme.Px(t, tokens.TextMetaStrong.Size.En)).FontWeight(tokens.TextMetaStrong.Weight).TextColor(ink).NoWrap()
	})
}

// statusColumn is the 176 wide status of a row: a success dot and Healthy, or the pill of a failing, removed or
// paused subscription, with a line under it.
func (s *Screen) statusColumn(c *ui.Context, src client.Source) {
	t := c.Theme()
	tag, sub, ink, subInk := s.statusLine(t, src)
	ui.Column(c).Width(theme.Px(t, tokens.Space12*3+tokens.Space8)).Shrink(0).AlignItems(ui.Start).Gap(theme.Px(t, tokens.Space1)).Children(func() {
		switch healthOf(src) {
		case healthOk:
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space1_5)).Children(func() {
				dot := theme.Px(t, tokens.Dot)
				ui.Box(c).Size(dot, dot).Radius(theme.Pill(dot)).Background(t.Success)
				ui.Text(c, tag).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).TextColor(ink).NoWrap()
			})
		case healthPaused:
			statusPill(c, icons.Pause, tag, ink)
		case healthGone:
			statusPill(c, icons.CircleAlert, tag, ink)
		default:
			statusPill(c, icons.TriangleAlert, tag, ink)
		}
		size := theme.Px(t, tokens.TextMeta.Size.En)
		line := ui.Text(c, sub).FontSize(size).TextColor(subInk).SingleLine().MaxWidth(theme.Px(t, tokens.Space12*3+tokens.Space8))
		if theme.Cut(c, line, sub, size, 0, 1) {
			theme.Tip(c, line, sub)
		}
	})
}

func (s *Screen) listRow(c *ui.Context, rows []client.Source, src client.Source, last, locked bool) {
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
	row.FillWidth().MinHeight(theme.Px(t, tokens.Space12+tokens.Space2)).Padding(theme.Px(t, tokens.Space2), theme.Px(t, tokens.Space3)).
		Gap(theme.Px(t, tokens.Space3)).AlignItems(ui.Center).Radius(theme.Px(t, tokens.RadiusControl))
	switch {
	case sel:
		row.Background(theme.Chosen(t))
	case row.Hovered() && !locked:
		row.Background(t.SurfaceHover)
	}
	if locked {
		row.Opacity(tokens.OpacityFaded)
	}
	if row.Focused() {
		m.cursor = id
	}
	if m.focusRow == id {
		m.focusRow = 0
		row.Focus().ScrollIntoView()
	}
	row.Children(func() {
		state := 0
		if sel {
			state = 1
		}
		box := checkBox(c, "mchk"+strconv.FormatInt(id, 10), state)
		if box.Clicked() && !locked {
			if box.ClickModifiers()&ui.Shift != 0 {
				s.rangeTo(rows, id)
			} else {
				s.toggle(id)
			}
		}
		glyph := ui.Box(c).Shrink(0)
		if src.Paused {
			glyph.Opacity(tokens.OpacityFaded)
		}
		glyph.Children(func() { s.rowGlyph(c, src, tokens.TileL) })
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(theme.Px(t, tokens.Space0_5)).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space1_5)).Children(func() {
				ink := t.Text
				if src.Paused {
					ink = t.TextMuted
				}
				ui.Text(c, Name(src)).FontSize(theme.Px(t, tokens.TextLabel.Size.En)).FontWeight(tokens.TextLabel.Weight).TextColor(ink).
					SingleLine().Shrink(1).MinWidth(0)
				if src.Paused {
					ui.Icon(c, icons.Pause).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted).Tooltip(s.text.PausedMark)
				}
				if src.HideFromAll {
					ui.Icon(c, icons.EyeOff).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted).Tooltip(s.text.HiddenMark)
				}
			})
			switch {
			case failed:
				ui.Row(c).AlignItems(ui.Start).Gap(theme.Px(t, tokens.Space1)).Children(func() {
					ui.Icon(c, icons.CircleAlert).FontSize(theme.IconSmall(t)).TextColor(t.Danger)
					ui.Text(c, f.reason).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).TextColor(t.Danger).Shrink(1).Wrap()
				})
			default:
				ui.Text(c, s.settingsOf(src)).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).TextColor(t.TextMuted).SingleLine()
			}
		})
		s.statusColumn(c, src)
		ui.Icon(c, icons.ChevronRight).FontSize(theme.IconSmall(t)).TextColor(theme.Disabled(t))
	})
	if !last && !sel && !m.selected[nextID(rows, id)] {
		ui.Box(c).FillWidth().Height(tokens.Hairline).Background(t.Border)
	}
	clicked := row.Clicked()
	if locked {
		return
	}
	switch {
	case row.Shortcut(0, ui.KeySpace):
		s.toggle(id)
	case row.Shortcut(0, ui.KeyEnter):
		s.OpenSource(src)
	case row.Shortcut(ui.Shift, ui.KeyDown):
		s.extend(rows, id, 1)
	case row.Shortcut(ui.Shift, ui.KeyUp):
		s.extend(rows, id, -1)
	case row.Shortcut(0, ui.KeyHome):
		m.focusRow = rows[0].ID
	case row.Shortcut(0, ui.KeyEnd):
		m.focusRow = rows[len(rows)-1].ID
	case row.Shortcut(ui.Super, ui.KeyA), row.Shortcut(ui.Ctrl, ui.KeyA):
		s.selectAll(rows)
	case row.Shortcut(ui.Shift, ui.KeyTab):
		m.focusFilter = true
	case clicked:
		m.cursor = id
		switch mods := row.ClickModifiers(); {
		case mods&ui.Shift != 0:
			s.rangeTo(rows, id)
		case mods&(ui.Super|ui.Ctrl) != 0:
			s.toggle(id)
		default:
			s.OpenSource(src)
		}
	}
}

// nextID is the ID of the row after id, 0 for none.
func nextID(rows []client.Source, id int64) int64 {
	if i := indexOf(rows, id); i >= 0 && i+1 < len(rows) {
		return rows[i+1].ID
	}
	return 0
}

// extend grows the selection by one row up or down and moves the focus there.
func (s *Screen) extend(rows []client.Source, id int64, step int) {
	m := &s.manage
	i := indexOf(rows, id) + step
	if i < 0 || i >= len(rows) {
		return
	}
	if len(m.selected) == 0 || indexOf(rows, m.anchor) < 0 {
		m.anchor = id
	}
	s.rangeTo(rows, rows[i].ID)
	m.focusRow = rows[i].ID
}

// listPage is the list: the tools row or the action bar, the result band, the card of rows and their count.
func (s *Screen) listPage(c *ui.Context) {
	t := c.Theme()
	m := &s.manage
	rows := s.visible()
	for id := range m.selected {
		if indexOf(rows, id) < 0 && m.run == nil {
			delete(m.selected, id)
		}
	}
	locked := m.run != nil
	m.typing = false
	ui.Column(c).FillWidth().Gap(theme.Px(t, tokens.Space3)).Children(func() {
		s.actionBar(c, rows)
		if m.band != "" {
			s.band(c)
		}
		all := 0
		if s.deps.Sources != nil {
			all = len(s.deps.Sources())
		}
		list := ui.Column(c).FillWidth().Radius(theme.Px(t, tokens.RadiusCard)).Background(theme.Surface(t)).Clip().
			Padding(theme.Px(t, tokens.Space1)).Role(ui.RoleList).Label(s.text.ListName).FocusGroup(ui.Vertical)
		list.Children(func() {
			switch {
			case all == 0:
				ui.Text(c, s.text.NoSubscriptions).TextColor(t.TextMuted).Padding(theme.Px(t, tokens.Space6), theme.Px(t, tokens.Space4)).Role(ui.RoleStatus)
			case len(rows) == 0:
				ui.Text(c, s.text.NoMatch).TextColor(t.TextMuted).Padding(theme.Px(t, tokens.Space6), theme.Px(t, tokens.Space4)).Role(ui.RoleStatus)
			}
			for i, src := range rows {
				s.listRow(c, rows, src, i == len(rows)-1, locked)
			}
		})
		if all > 0 {
			ui.Text(c, s.count(s.text.OfN, all)).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).FontFeatures("tnum").TextColor(t.TextMuted).
				Margin(0, 0, 0, theme.Px(t, tokens.Space4))
		}
	})
	if m.sheet.open {
		s.changeSheet(c, s.selection(rows))
	}
	if m.confirm {
		n := len(s.selection(rows))
		if s.confirm(c, &m.confirm, s.count(s.text.DeleteManyTitle, n), s.text.DeleteManyBody) {
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

// filterField is the 32 high filter of the tools row, Raised with the edge hairline.
func (s *Screen) filterField(c *ui.Context, rows []client.Source) {
	t := c.Theme()
	m := &s.manage
	b := ui.Row(c).Key("mfilter").Grow(1).MinWidth(0).Height(theme.Px(t, tokens.Control)).AlignItems(ui.Center).
		Gap(theme.Px(t, tokens.Space2)).PaddingX(theme.Px(t, tokens.Space2+tokens.Space0_5)).Radius(theme.Px(t, tokens.RadiusControl)).
		Background(theme.RaisedFill(t)).Border(theme.Px(t, tokens.Hairline), theme.LineStrong(t))
	if b.FocusWithin() {
		m.typing = true
		b.Shadow(0, 0, 0, theme.Px(t, tokens.FocusRing), t.Focus)
	}
	b.Children(func() {
		ui.Icon(c, icons.Search).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
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

// kindFilter is the kinds select of the tools row: All kinds, then each kind.
func (s *Screen) kindFilter(c *ui.Context) {
	t := c.Theme()
	m := &s.manage
	options := make([]int64, len(kinds)+1)
	for i := range options {
		options[i] = int64(i)
	}
	name := func(v int64) string {
		if v == 0 {
			return s.text.AllKinds
		}
		return s.kindName(kinds[v-1])
	}
	ui.Box(c).Width(theme.Px(t, tokens.MenuMin)).Shrink(0).Children(func() {
		s.selectTrigger(c, &m.kind, options, s.text.KindFilter, name(m.kind), name, nil, m.run != nil, false)
	})
}

// selectState is 0, 1 or 2 for none, all or some of the visible rows selected.
func (s *Screen) selectState(rows []client.Source) int {
	n := len(s.selection(rows))
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
	t := c.Theme()
	state := s.selectState(rows)
	b := ui.ButtonBase(c).Key("mall").Role(ui.RoleCheckBox).Checked(state == 1).Label(s.text.SelectAll).
		Disabled(len(rows) == 0 || m.run != nil).AlignItems(ui.Center).Radius(theme.Px(t, tokens.RadiusXS))
	if state == 2 {
		b.Mixed()
	}
	b.Children(func() { checkBox(c, "mallbox", state) })
	if b.Clicked() {
		if state == 1 {
			s.clearSelection()
		} else {
			s.selectAll(rows)
		}
	}
}

// actionBar is the 48 high tools row, which turns into the action bar on soft with a selection and into the progress
// of a run.
func (s *Screen) actionBar(c *ui.Context, rows []client.Source) {
	t := c.Theme()
	m := &s.manage
	sel := s.selection(rows)
	n := len(sel)
	bar := ui.Row(c).Key("mbar").FillWidth().Height(theme.Px(t, tokens.Control+tokens.Space4)).AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space2)).
		Padding(0, theme.Px(t, tokens.Space2), 0, theme.Px(t, tokens.Space4)).Radius(theme.Px(t, tokens.RadiusCard))
	if m.run == nil && n == 0 {
		bar.Gap(theme.Px(t, tokens.Space3)).PaddingX(theme.Px(t, tokens.Space4))
	}
	switch {
	case m.run != nil:
		j := m.run
		text := s.count(s.text.Applying, len(j.ids))
		bar.Background(theme.Soft(t, t.Accent)).Role(ui.RoleStatus).Label(text).Children(func() {
			s.selectAllButton(c, rows)
			spin := ui.Icon(c, icons.LoaderCircle).FontSize(theme.IconMedium(t)).TextColor(t.Accent)
			spin.Rotate(spin.Loop("spin", tokens.MotionSpin, ui.Linear) * 360)
			ui.Text(c, text).FontSize(theme.Px(t, tokens.TextBody.Size.En)).FontFeatures("tnum").Grow(1).MinWidth(0).SingleLine()
		})
	case n == 0:
		bar.Children(func() {
			s.selectAllButton(c, rows)
			s.filterField(c, rows)
			s.kindFilter(c)
		})
	default:
		anyPaused := slices.ContainsFunc(sel, func(src client.Source) bool { return src.Paused })
		anyLive := slices.ContainsFunc(sel, func(src client.Source) bool { return !src.Paused })
		bar.Background(theme.Soft(t, t.Accent)).Role(ui.RoleToolbar).Label(s.count(s.text.BarLabel, n)).Children(func() {
			s.selectAllButton(c, rows)
			ui.Text(c, s.count(s.text.SelectedN, n)).FontSize(theme.Px(t, tokens.TextLabel.Size.En)).FontWeight(tokens.TextLabel.Weight).
				FontFeatures("tnum").Margin(0, 0, 0, theme.Px(t, tokens.Space1)).Grow(1).MinWidth(0).SingleLine()
			if (theme.Button{Text: s.text.ChangeN, Key: "mchange",
				Label: s.count(s.text.ChangeNLabel, n)}).Draw(c).Clicked() {
				m.sheet = changing{open: true, kind: unchanged, folder: unchanged, every: unchanged, target: unchanged, keep: unchanged,
					proxy: unchanged, pause: unchanged, hide: unchanged}
			}
			if anyLive && (theme.Button{Icon: icons.Pause, Text: s.text.Pause, Key: "mpause",
				Label: s.count(s.text.PauseNLabel, len(pending(sel, true)))}).Draw(c).Clicked() {
				s.startJob(jobPause, client.SourcePatch{Paused: new(true)}, ids(pending(sel, true)), false)
			}
			if anyPaused && (theme.Button{Icon: icons.Play, Text: s.text.Resume, Key: "mresume",
				Label: s.count(s.text.ResumeNLabel, len(pending(sel, false)))}).Draw(c).Clicked() {
				s.startJob(jobResume, client.SourcePatch{Paused: new(false)}, ids(pending(sel, false)), false)
			}
			live := pending(sel, true)
			if (theme.Button{Icon: icons.RefreshCw, Text: s.text.Refresh, Key: "mrefresh", Disabled: len(live) == 0,
				Label: s.count(s.text.RefreshNLabel, len(live))}).Draw(c).Clicked() {
				s.startJob(jobRefresh, client.SourcePatch{}, ids(live), false)
			}
			if (theme.Button{Style: theme.Danger, Text: s.text.DeleteMany, Key: "mdelete",
				Label: s.count(s.text.DeleteNLabel, n)}).Draw(c).Clicked() {
				m.confirm = true
			}
			ui.Box(c).Size(tokens.Hairline, theme.Px(t, tokens.Space4)).Margin(0, theme.Px(t, tokens.Space1)).Background(theme.LineStrong(t)).Role(ui.RoleNone)
			x, color := theme.ToolButton(c, theme.ToolState{})
			x.Key("mclear").Size(theme.Px(t, tokens.Control), theme.Px(t, tokens.Control)).Label(s.text.ClearSelection).Tooltip(s.text.ClearSelection)
			x.Children(func() { ui.Icon(c, icons.X).FontSize(theme.IconMedium(t)).TextColor(color) })
			if x.Clicked() {
				s.clearSelection()
			}
		})
	}
}

// pending is the subscriptions of srcs that a pause (pause true) or a resume (false) still has to change.
func pending(srcs []client.Source, pause bool) []client.Source {
	return slices.DeleteFunc(slices.Clone(srcs), func(src client.Source) bool { return src.Paused == pause })
}

// band is the result of the last run that did not finish whole, with Try again and Dismiss.
func (s *Screen) band(c *ui.Context) {
	t := c.Theme()
	m := &s.manage
	retry := slices.ContainsFunc(m.failed, func(id int64) bool { return !m.fails[id].gone })
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space2)).Padding(theme.Px(t, tokens.Space2), theme.Px(t, tokens.Space2),
		theme.Px(t, tokens.Space2), theme.Px(t, tokens.Space3)).Radius(theme.Px(t, tokens.RadiusControl)).Background(theme.Surface(t)).
		Role(ui.RoleStatus).Label(m.band).Children(func() {
		ui.Icon(c, icons.TriangleAlert).FontSize(theme.IconSmall(t)).TextColor(t.Warning)
		ui.Text(c, m.band).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).Grow(1).MinWidth(0).Wrap()
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

// changeSheet changes several subscriptions at once: eight selects in two columns, each on Leave as is until chosen.
func (s *Screen) changeSheet(c *ui.Context, sel []client.Source) {
	t := c.Theme()
	sh := &s.manage.sheet
	if len(sel) == 0 {
		sh.open = false
		return
	}
	title := s.count(s.text.ChangeTitle, len(sel))
	s.dialog(c, &sh.open, &sh.form, tokens.DialogWide-2*tokens.Space4-tokens.Space1*2, title, func() {
		s.header(c, head{open: &sh.open, lead: func() { chip(c, icons.SlidersHorizontal, false) }, title: title, sub: s.text.ChangeSub})
	}, func() {
		field := func(value *int64, options []int64, text string, name func(int64) string) func() {
			return func() {
				shown := s.text.KeepAsIs
				if *value != unchanged {
					shown = name(*value)
				}
				s.choiceSelect(c, value, append([]int64{unchanged}, options...), text, shown, func(v int64) string {
					if v == unchanged {
						return s.text.KeepAsIs
					}
					return name(v)
				}, false, *value == unchanged)
			}
		}
		kindIdx := make([]int64, len(kinds))
		for i := range kinds {
			kindIdx[i] = int64(i)
		}
		onOff := func(v int64) string {
			if v == 1 {
				return s.text.On
			}
			return s.text.Off
		}
		proxyName := func(v int64) string {
			if v == proxyDirect {
				return s.text.ProxyDirect
			}
			return s.text.ProxyFollowShort
		}
		fields := []func(){
			field(&sh.kind, kindIdx, s.text.ShowAs, func(i int64) string { return s.kindName(kinds[i]) }),
		}
		if folders := s.folders(); len(folders) > 0 {
			options := []int64{0}
			for _, f := range folders {
				options = append(options, f.ID)
			}
			fields = append(fields, field(&sh.folder, options, s.text.Folder, s.folderName))
		}
		fields = append(fields,
			field(&sh.every, intervals, s.text.Every, s.every),
			field(&sh.target, []int64{0, 1, 2}, s.text.OpenTarget, s.openName),
			field(&sh.keep, keeps, s.text.Keep, s.keepName),
			field(&sh.proxy, []int64{proxyFollow, proxyDirect}, s.text.Proxy, proxyName),
			field(&sh.pause, []int64{1, 0}, s.text.PauseLabel, onOff),
			field(&sh.hide, []int64{1, 0}, s.text.HideLabel, onOff))
		ui.Column(c).FillWidth().Gap(theme.Px(t, tokens.Space3)).Children(func() {
			for i := 0; i < len(fields); i += 2 {
				right := func() {}
				if i+1 < len(fields) {
					right = fields[i+1]
				}
				s.pair(c, fields[i], right)
			}
		})
	}, func() {
		ui.Box(c).Grow(1)
		if (theme.Button{Style: theme.Secondary, Text: s.text.Cancel}).Draw(c).Clicked() {
			sh.open = false
		}
		patch, any := sh.patch()
		if (theme.Button{Style: theme.Primary, Text: s.text.Apply, Disabled: !any}).Draw(c).Clicked() {
			sh.open = false
			s.startJob(jobChange, patch, ids(sel), false)
		}
	})
}

// patch is what the change sheet sends to each subscription: only the fields it changes, and whether there is one.
func (sh *changing) patch() (client.SourcePatch, bool) {
	var p client.SourcePatch
	any := false
	if sh.kind != unchanged {
		p.Kind, any = new(kinds[sh.kind]), true
	}
	if sh.folder != unchanged {
		p.FolderID, any = new(sh.folder), true
	}
	if sh.every != unchanged {
		if sh.every == 0 {
			p.FetchIntervalAuto = new(true)
		} else {
			p.FetchIntervalSec = new(sh.every)
		}
		any = true
	}
	if sh.target != unchanged {
		p.OpenTarget, any = new(openTargets[sh.target]), true
	}
	if sh.keep != unchanged {
		p.KeepDays, any = new(int(sh.keep)), true
	}
	if sh.proxy != unchanged {
		proxy := ""
		if sh.proxy == proxyDirect {
			proxy = "direct"
		}
		p.Proxy, any = &proxy, true
	}
	if sh.pause != unchanged {
		p.Paused, any = new(sh.pause == 1), true
	}
	if sh.hide != unchanged {
		p.HideFromAll, any = new(sh.hide == 1), true
	}
	return p, any
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
	ctx, stop := context.WithCancel(context.Background())
	m.run = &job{kind: kind, patch: patch, ids: list, srcs: srcs, fails: map[int64]failure{}, merge: merge, ctx: ctx, stop: stop}
	s.announce = s.count([]string{s.text.AnnouncePause, s.text.AnnounceResume, s.text.AnnounceChange, s.text.AnnounceDelete,
		s.text.AnnounceRestore, s.text.AnnounceRefresh}[kind], len(list))
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
		ctx := j.ctx
		switch j.kind {
		case jobDelete:
			err = api.DeleteSource(ctx, id)
		case jobRestore:
			src, err = api.RestoreSource(ctx, id)
		case jobRefresh:
			err = api.RefreshSource(ctx, id)
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
		case jobRestore:
			j.restored = append(j.restored, src)
		case jobRefresh:
		default:
			if src.Paused {
				s.dropHealth(id)
			}
			j.changed = append(j.changed, src)
		}
	case j.kind == jobDelete && code(err) == "not_found":
		j.okIDs = append(j.okIDs, id)
		s.dropHealth(id)
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
	j.stop()
	defer func() {
		if u := s.pending; u != nil {
			s.pending = nil
			s.restoreMany(u)
		}
	}()
	for _, id := range j.okIDs {
		delete(m.selected, id)
	}
	ok, bad := len(j.okIDs), len(j.failed)
	var removed []int64
	if j.kind == jobDelete {
		removed = j.okIDs
	}
	if j.kind != jobRefresh {
		s.deps.Bulk(j.changed, removed, j.restored)
	}
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
		switch {
		case j.kind == jobDelete && len(j.deleted) == 0:
			s.toast = s.count(s.text.DeletedMany, ok)
		case j.kind == jobRefresh:
			s.toast = s.count(s.text.RefreshedMany, ok)
		case j.kind != jobDelete:
			s.toast = s.count([]string{s.text.PausedMany, s.text.ResumedMany, s.text.ChangedMany}[j.kind], ok)
		}
		return
	}
	m.fails, m.failed = j.fails, j.failed
	if ok == 0 {
		m.band = put(s.text.AllFailed, "reason", j.fails[j.failed[0]].reason)
	} else {
		partial := []string{s.text.PartialPause, s.text.PartialResume, s.text.PartialChange, s.text.PartialDelete, "",
			s.text.PartialRefresh}[j.kind]
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
	for _, d := range s.deleted { // one toast at a time: a new bulk delete voids the Undo of the single deletes before it
		c.CloseToast(deletedToast + strconv.FormatInt(d.src.ID, 10))
	}
	s.deleted = nil
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
	if s.api == nil {
		return
	}
	if m.run != nil { // Undo was pressed while a run is going: it starts when that run is done
		s.pending = u
		return
	}
	if s.undo == u {
		s.undo = nil
	}
	s.startJob(jobRestore, client.SourcePatch{}, ids(u.srcs), false)
}
