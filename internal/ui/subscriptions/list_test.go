package subscriptions

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/theme"
)

var names = []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot"}

// listing6 shows the list of Settings → Subscriptions over six subscriptions: Alpha to Foxtrot, with the IDs 1 to 6.
func listing6(t *testing.T, language string) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	s, f, _ := setup(t, language)
	f.sources = nil
	for i, name := range names {
		f.sources = append(f.sources, client.Source{ID: int64(i + 1), Title: name, URL: "https://" + strings.ToLower(name) + ".example/feed",
			Kind: "article", FetchIntervalSec: 900, FetchIntervalAuto: true})
	}
	s.OpenList()
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { s.Page(c) })
		s.View(c)
		theme.Toasts(c)
	}, 1000, 900)
	tt.Frame()
	return s, f, tt
}

func chosen(s *Screen) []int64 {
	var out []int64
	for id := range s.manage.selected {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

func mustClick(t *testing.T, tt *ui.Tester, text string) {
	t.Helper()
	if err := tt.Click(text); err != nil {
		t.Fatalf("%v in %v", err, tt.Texts())
	}
}

func mustClickWith(t *testing.T, tt *ui.Tester, mods ui.Modifiers, text string) {
	t.Helper()
	if err := tt.ClickWith(mods, text); err != nil {
		t.Fatal(err)
	}
}

func wantChosen(t *testing.T, s *Screen, want ...int64) {
	t.Helper()
	if got := chosen(s); !slices.Equal(got, want) {
		t.Fatalf("selected %v, want %v", got, want)
	}
}

// choose selects the rows named, as Cmd+click does.
func choose(t *testing.T, tt *ui.Tester, names ...string) {
	t.Helper()
	for _, name := range names {
		mustClickWith(t, tt, ui.Super, name)
	}
}

func TestListHasSources(t *testing.T) {
	s, f, tt := setup(t, "en")
	if !s.HasSources() {
		t.Fatal("no HasSources with two")
	}
	f.sources = nil
	if s.HasSources() {
		t.Fatal("HasSources with none")
	}
	s.OpenList()
	tt = ui.NewTester(func(c *ui.Context) { s.Page(c) }, 1000, 760)
	tt.Frame()
	if !tt.HasText(s.text.NoSubscriptions) || tt.HasText(s.count(s.text.OfN, 0)) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestListShows(t *testing.T) {
	s, f, tt := listing6(t, "en")
	if !tt.HasText("6 subscriptions") || !tt.HasText(s.text.FilterLabel) || !tt.HasText(s.text.AllKinds) {
		t.Fatalf("texts %v", tt.Texts())
	}
	for _, name := range names {
		if !tt.HasText(name) {
			t.Fatalf("no row %q", name)
		}
	}
	if want := "Articles · No folder · Auto · 15 min"; s.settingsOf(f.sources[0]) != want || !tt.HasText(want) {
		t.Fatalf("meta line %q, texts %v", s.settingsOf(f.sources[0]), tt.Texts())
	}
	if s.Crumb() != "" || s.Open() {
		t.Fatal("the list is not where it starts")
	}
}

// TestListClickOpensPage is a click on a row: it opens the subscription's page and leaves the selection alone; Back
// returns to the list with the row focused.
func TestListClickOpensPage(t *testing.T) {
	s, f, tt := listing6(t, "en")
	choose(t, tt, "Alpha")
	mustClick(t, tt, "Charlie")
	f.settle(tt)
	if s.Crumb() != "Charlie" || !s.pg.open {
		t.Fatalf("crumb %q", s.Crumb())
	}
	wantChosen(t, s, 1)
	if s.Open() {
		t.Fatal("the page counts as a dialog")
	}
	s.Back()
	tt.Frame()
	if s.Crumb() != "" || !tt.Focused(s.rowLabel(f.sources[2], false)) {
		t.Fatalf("crumb %q, the row is not focused", s.Crumb())
	}
}

func TestListModifierSelection(t *testing.T) {
	s, _, tt := listing6(t, "en")
	mustClickWith(t, tt, ui.Super, "Alpha")
	wantChosen(t, s, 1)
	mustClickWith(t, tt, ui.Ctrl, "Delta")
	wantChosen(t, s, 1, 4)
	mustClickWith(t, tt, ui.Ctrl, "Echo")
	wantChosen(t, s, 1, 4, 5)
	mustClickWith(t, tt, ui.Ctrl, "Delta")
	wantChosen(t, s, 1, 5)
	mustClickWith(t, tt, ui.Shift, "Foxtrot")
	wantChosen(t, s, 4, 5, 6)
	mustClickWith(t, tt, ui.Shift, "Alpha")
	wantChosen(t, s, 1, 2, 3, 4)
	if s.pg.open {
		t.Fatal("a click with a modifier opened the page")
	}
}

func TestListCheckbox(t *testing.T) {
	s, _, tt := listing6(t, "en")
	clickBox(t, tt, s, 3, 0)
	wantChosen(t, s, 3)
	clickBox(t, tt, s, 5, 0)
	wantChosen(t, s, 3, 5)
	clickBox(t, tt, s, 3, 0)
	wantChosen(t, s, 5)
	clickBox(t, tt, s, 2, ui.Shift)
	wantChosen(t, s, 2, 3)
	if s.pg.open {
		t.Fatal("a click on a check box opened the page")
	}
}

// press clicks a button of the action bar, by the label it has for the selection there is.
func press(t *testing.T, tt *ui.Tester, s *Screen, what string) {
	t.Helper()
	sel := s.selection(s.visible())
	label := map[string]string{
		"pause":   s.count(s.text.PauseNLabel, len(pending(sel, true))),
		"resume":  s.count(s.text.ResumeNLabel, len(pending(sel, false))),
		"refresh": s.count(s.text.RefreshNLabel, len(pending(sel, true))),
		"change":  s.count(s.text.ChangeNLabel, len(sel)),
		"delete":  s.count(s.text.DeleteNLabel, len(sel)),
		"clear":   s.text.ClearSelection,
		"all":     s.text.SelectAll,
	}[what]
	mustClick(t, tt, label)
}

// hasButton reports whether the action bar shows a button of what, as press names them.
func hasButton(tt *ui.Tester, s *Screen, what string) bool {
	sel := s.selection(s.visible())
	label := map[string]string{
		"pause":  s.count(s.text.PauseNLabel, len(pending(sel, true))),
		"resume": s.count(s.text.ResumeNLabel, len(pending(sel, false))),
	}[what]
	_, ok := tt.Find(label)
	return ok
}

// clickBox clicks the check box of the row of the subscription id, with mods held.
func clickBox(t *testing.T, tt *ui.Tester, s *Screen, id int64, mods ui.Modifiers) {
	t.Helper()
	src := s.visible()[indexOf(s.visible(), id)]
	r, ok := tt.Find(s.rowLabel(src, s.manage.selected[id]))
	if !ok {
		t.Fatalf("no row for %d", id)
	}
	if mods == 0 {
		tt.ClickAt(r.X+16+8, r.Y+r.H/2)
	} else {
		tt.ClickAtWith(mods, r.X+16+8, r.Y+r.H/2)
	}
	tt.Frame()
}

func TestListSelectAll(t *testing.T) {
	s, _, tt := listing6(t, "en")
	press(t, tt, s, "all")
	wantChosen(t, s, 1, 2, 3, 4, 5, 6)
	if !tt.HasText("6 selected") {
		t.Fatalf("texts %v", tt.Texts())
	}
	press(t, tt, s, "clear")
	choose(t, tt, "Bravo", "Charlie")
	press(t, tt, s, "all")
	wantChosen(t, s, 1, 2, 3, 4, 5, 6)
	press(t, tt, s, "all")
	wantChosen(t, s)
}

func TestListKeyboard(t *testing.T) {
	s, _, tt := listing6(t, "en")
	mustClick(t, tt, s.text.FilterLabel)
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if !tt.Focused(s.rowLabel(s.visible()[0], false)) {
		t.Fatalf("down from the filter did not reach the first row")
	}
	tt.Key(0, ui.KeySpace)
	wantChosen(t, s, 1)
	tt.Key(ui.Shift, ui.KeyDown)
	tt.Key(ui.Shift, ui.KeyDown)
	wantChosen(t, s, 1, 2, 3)
	tt.Key(ui.Shift, ui.KeyUp)
	wantChosen(t, s, 1, 2)
	tt.Key(ui.Super, ui.KeyA)
	wantChosen(t, s, 1, 2, 3, 4, 5, 6)
	if !s.Escape() {
		t.Fatal("Escape did not take the selection")
	}
	tt.Frame()
	wantChosen(t, s)
	tt.Key(0, ui.KeyEnd)
	tt.Key(0, ui.KeySpace)
	wantChosen(t, s, 6)
	tt.Key(0, ui.KeyHome)
	tt.Key(0, ui.KeySpace)
	wantChosen(t, s, 1, 6)
	tt.Key(ui.Ctrl, ui.KeyA)
	wantChosen(t, s, 1, 2, 3, 4, 5, 6)
	s.Escape()
	tt.Frame()
	// With nothing selected the tools row is back, and Shift+Tab reaches its filter.
	tt.Key(ui.Shift, ui.KeyTab)
	tt.Frame()
	if !tt.Focused(s.text.FilterLabel) {
		t.Fatal("Shift+Tab did not return to the filter")
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if s.Crumb() != "Alpha" {
		t.Fatalf("Enter opened %q", s.Crumb())
	}
}

// TestListEscape is Escape after a field gave up the focus: a run holds it, a page with nothing typed goes back to
// the list, a selection is cleared, and with none of these Settings goes on with its own rule.
func TestListEscape(t *testing.T) {
	s, f, tt := listing6(t, "en")
	if s.Escape() {
		t.Fatal("Escape took nothing")
	}
	choose(t, tt, "Alpha", "Bravo")
	mustClick(t, tt, s.text.DeleteMany)
	tt.Frame()
	if !s.Open() {
		t.Fatal("the confirmation is not a dialog")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.manage.confirm || s.Open() {
		t.Fatal("escape did not close the confirmation")
	}
	wantChosen(t, s, 1, 2)
	press(t, tt, s, "change")
	if !s.manage.sheet.open || !s.Open() {
		t.Fatal("no change sheet")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.manage.sheet.open {
		t.Fatal("escape did not close the sheet")
	}
	wantChosen(t, s, 1, 2)
	press(t, tt, s, "pause")
	if !s.Escape() || s.manage.run == nil {
		t.Fatal("Escape stopped a run, or went on to Settings")
	}
	f.settle(tt)
	choose(t, tt, "Alpha")
	if !s.Escape() || len(chosen(s)) != 0 {
		t.Fatal("Escape did not clear the selection")
	}
	mustClick(t, tt, "Alpha")
	f.settle(tt)
	s.pg.e.title = "Changed"
	if s.Escape() || !s.Unsaved() || s.Crumb() != "Alpha" {
		t.Fatal("Escape left a page with text not saved")
	}
	s.pg.e.title = "Alpha"
	if !s.Escape() || s.Crumb() != "" {
		t.Fatal("Escape did not go back to the list")
	}
}

func TestListFilter(t *testing.T) {
	s, _, tt := listing6(t, "en")
	mustClick(t, tt, s.text.FilterLabel)
	tt.Type("char")
	tt.Frame()
	if !s.Typing() {
		t.Fatal("the filter does not count as typing")
	}
	if tt.HasText("Bravo") || !tt.HasText("Charlie") || tt.HasText("Alpha") {
		t.Fatalf("texts %v", tt.Texts())
	}
	press(t, tt, s, "all")
	wantChosen(t, s, 3)
	s.manage.filter = "nothing like it"
	tt.Frame()
	if !tt.HasText(s.text.NoMatch) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

// TestListFilterDropsHiddenRows unselects the rows the filter hides, so an action only touches what shows.
func TestListFilterDropsHiddenRows(t *testing.T) {
	s, _, tt := listing6(t, "en")
	choose(t, tt, "Bravo", "Charlie")
	s.manage.filter = "char"
	tt.Frame()
	wantChosen(t, s, 3)
}

func TestListKindFilter(t *testing.T) {
	s, f, tt := listing6(t, "en")
	f.sources[1].Kind = "video"
	choose(t, tt, "Alpha")
	s.manage.kind = int64(slices.Index(kinds, "video") + 1)
	tt.Frame()
	if !tt.HasText("Bravo") || tt.HasText("Alpha") || !tt.HasText(s.kindName("video")) {
		t.Fatalf("texts %v", tt.Texts())
	}
	wantChosen(t, s)
}

func TestListPauseAndResume(t *testing.T) {
	s, f, tt := listing6(t, "en")
	f.sources[1].Paused = true
	tt.Frame()
	choose(t, tt, "Alpha")
	mustClickWith(t, tt, ui.Shift, "Charlie")
	press(t, tt, s, "pause")
	if len(f.queue) != 2 {
		t.Fatalf("%d requests started, want the 2 that are not paused", len(f.queue))
	}
	if !tt.HasText("Applying to 2 subscriptions…") {
		t.Fatalf("texts %v", tt.Texts())
	}
	f.settle(tt)
	if len(f.patches[2]) != 0 || len(f.patches[1]) != 1 || len(f.patches[3]) != 1 {
		t.Fatalf("patches %v", f.patches)
	}
	if p := f.patches[1][0]; p.Paused == nil || !*p.Paused || p.Title != nil || p.Kind != nil {
		t.Fatalf("patch %+v", p)
	}
	for _, id := range []int64{1, 2, 3} {
		if !f.sources[id-1].Paused {
			t.Fatalf("source %d not paused", id)
		}
	}
	wantChosen(t, s, 2)
	if !tt.HasText("Paused 2 subscriptions") {
		t.Fatalf("texts %v", tt.Texts())
	}
	press(t, tt, s, "resume")
	f.settle(tt)
	if p := f.patches[2]; len(p) != 1 || p[0].Paused == nil || *p[0].Paused || f.sources[1].Paused {
		t.Fatalf("resume patches %v", p)
	}
	if !tt.HasText("Resumed 1 subscription") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestListPauseOnlyForLive(t *testing.T) {
	s, f, tt := listing6(t, "en")
	f.sources[0].Paused = true
	tt.Frame()
	choose(t, tt, "Alpha")
	if hasButton(tt, s, "pause") || !hasButton(tt, s, "resume") {
		t.Fatal("an all paused selection offers Pause or no Resume")
	}
	choose(t, tt, "Bravo")
	if !hasButton(tt, s, "pause") || !hasButton(tt, s, "resume") {
		t.Fatal("a mixed selection should offer both")
	}
}

// TestListRefresh asks the server to fetch the selected subscriptions that are not paused, and says so in a toast;
// the timeline's list does not change.
func TestListRefresh(t *testing.T) {
	s, f, tt := listing6(t, "en")
	f.sources[1].Paused = true
	tt.Frame()
	choose(t, tt, "Alpha", "Bravo", "Charlie")
	if _, ok := tt.Find("Refresh 2 subscriptions"); !ok {
		t.Fatalf("no refresh button for the 2 not paused: %v", tt.Texts())
	}
	press(t, tt, s, "refresh")
	f.settle(tt)
	if !slices.Equal(f.fetched, []int64{1, 3}) || f.bulks != 0 {
		t.Fatalf("fetched %v, bulks %d", f.fetched, f.bulks)
	}
	if !tt.HasText("2 subscriptions will fetch now") || !slices.Contains(tt.Announcements(), "Refreshing 2 subscriptions") {
		t.Fatalf("texts %v, announcements %v", tt.Texts(), tt.Announcements())
	}
	wantChosen(t, s, 2)
}

func TestListParallelAndProgress(t *testing.T) {
	s, f, tt := listing6(t, "en")
	press(t, tt, s, "all")
	press(t, tt, s, "pause")
	if len(f.queue) != manageParallel {
		t.Fatalf("%d requests in flight, want %d", len(f.queue), manageParallel)
	}
	f.queue[0]()()
	f.queue = f.queue[1:]
	tt.Frame()
	if len(f.queue) != manageParallel || !tt.HasText("Applying to 6 subscriptions…") {
		t.Fatalf("queue %d, texts %v", len(f.queue), tt.Texts())
	}
	if !slices.Contains(tt.Announcements(), "Pausing 6 subscriptions") {
		t.Fatalf("announcements %v", tt.Announcements())
	}
	f.settle(tt)
	if s.manage.run != nil || len(f.patches) != 6 {
		t.Fatalf("run %v patches %d", s.manage.run, len(f.patches))
	}
}

func TestListChangeSendsOnlyChanged(t *testing.T) {
	s, f, tt := listing6(t, "en")
	choose(t, tt, "Bravo", "Delta")
	press(t, tt, s, "change")
	if !tt.HasText("Change 2 subscriptions") || !tt.HasText(s.text.ChangeSub) || !tt.HasText(s.text.KeepAsIs) {
		t.Fatalf("texts %v", tt.Texts())
	}
	// Apply waits for a change.
	mustClick(t, tt, s.text.Apply)
	if len(f.queue) != 0 || !s.manage.sheet.open {
		t.Fatal("Apply without a change sent something")
	}
	s.manage.sheet.kind = 2
	mustClick(t, tt, s.text.Apply)
	f.settle(tt)
	if len(f.patches[2]) != 1 || len(f.patches[4]) != 1 {
		t.Fatalf("patches %v", f.patches)
	}
	p := f.patches[2][0]
	if p.Kind == nil || *p.Kind != kinds[2] || p.FetchIntervalSec != nil || p.FetchIntervalAuto != nil || p.KeepDays != nil || p.Paused != nil {
		t.Fatalf("patch %+v", p)
	}
	if f.sources[1].Kind != kinds[2] || f.sources[3].Kind != kinds[2] {
		t.Fatal("kinds not changed")
	}
	if !tt.HasText("Changed 2 subscriptions") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

// TestListChangeEveryField sends each of the eight fields of the change sheet as chosen.
func TestListChangeEveryField(t *testing.T) {
	sh := changing{kind: 1, folder: 7, every: 0, target: 2, keep: -1, proxy: proxyDirect, pause: 1, hide: 0}
	p, ok := sh.patch()
	if !ok || *p.Kind != kinds[1] || *p.FolderID != 7 || !*p.FetchIntervalAuto || p.FetchIntervalSec != nil ||
		*p.OpenTarget != openTargets[2] || *p.KeepDays != -1 || *p.Proxy != "direct" || !*p.Paused || *p.HideFromAll {
		t.Fatalf("patch %+v", p)
	}
	sh = changing{kind: unchanged, folder: unchanged, every: 3600, target: unchanged, keep: unchanged, proxy: proxyFollow,
		pause: unchanged, hide: unchanged}
	p, ok = sh.patch()
	if !ok || p.Kind != nil || p.FolderID != nil || p.FetchIntervalAuto != nil || *p.FetchIntervalSec != 3600 ||
		*p.Proxy != "" || p.Paused != nil || p.HideFromAll != nil {
		t.Fatalf("patch %+v", p)
	}
}

func TestListChangeIntervalAndKeep(t *testing.T) {
	s, f, tt := listing6(t, "en")
	press(t, tt, s, "all")
	press(t, tt, s, "change")
	s.manage.sheet.every, s.manage.sheet.keep = 0, -1
	mustClick(t, tt, s.text.Apply)
	f.settle(tt)
	p := f.patches[1][0]
	if p.FetchIntervalAuto == nil || !*p.FetchIntervalAuto || p.FetchIntervalSec != nil || p.KeepDays == nil || *p.KeepDays != -1 || p.Kind != nil {
		t.Fatalf("auto patch %+v", p)
	}
	press(t, tt, s, "all")
	press(t, tt, s, "change")
	s.manage.sheet.every = intervals[len(intervals)-1]
	mustClick(t, tt, s.text.Apply)
	f.settle(tt)
	p = f.patches[1][1]
	if p.FetchIntervalAuto != nil || p.FetchIntervalSec == nil || *p.FetchIntervalSec != intervals[len(intervals)-1] || p.KeepDays != nil {
		t.Fatalf("interval patch %+v", p)
	}
}

func TestListPartialFailureAndRetry(t *testing.T) {
	s, f, tt := listing6(t, "en")
	f.failOn = map[int64]error{2: &client.APIError{Status: 500, Code: "server"}, 3: client.ErrUnreachable}
	choose(t, tt, "Alpha")
	mustClickWith(t, tt, ui.Shift, "Delta")
	press(t, tt, s, "pause")
	f.settle(tt)
	if got := s.manage.band; got != "Paused 2. 2 couldn’t be finished." {
		t.Fatalf("band %q", got)
	}
	wantChosen(t, s, 2, 3)
	if !tt.HasText(s.text.RServer) || !tt.HasText(s.text.RNet) || !tt.HasText(s.text.RetryFailed) {
		t.Fatalf("texts %v", tt.Texts())
	}
	if !slices.Contains(tt.Announcements(), s.manage.band) {
		t.Fatalf("announcements %v", tt.Announcements())
	}
	if !f.sources[0].Paused || f.sources[1].Paused {
		t.Fatal("sources wrong")
	}
	f.failOn = nil
	mustClick(t, tt, s.text.RetryFailed)
	if len(f.queue) != 2 {
		t.Fatalf("%d requests for the retry", len(f.queue))
	}
	f.settle(tt)
	if s.manage.band != "" || !f.sources[1].Paused || !f.sources[2].Paused {
		t.Fatalf("band %q", s.manage.band)
	}
	if len(f.patches[1]) != 1 || len(f.patches[4]) != 1 {
		t.Fatalf("done ones were asked again: %v", f.patches)
	}
	wantChosen(t, s)
}

func TestListAllFailed(t *testing.T) {
	s, f, tt := listing6(t, "en")
	f.failOn = map[int64]error{1: client.ErrUnreachable, 2: client.ErrUnreachable}
	choose(t, tt, "Alpha", "Bravo")
	press(t, tt, s, "pause")
	f.settle(tt)
	if s.manage.band != "Nothing was done and nothing changed. Couldn’t reach the server." {
		t.Fatalf("band %q", s.manage.band)
	}
	wantChosen(t, s, 1, 2)
	mustClick(t, tt, s.text.DismissResult)
	if s.manage.band != "" || tt.HasText(s.text.RetryFailed) {
		t.Fatal("band not dismissed")
	}
	wantChosen(t, s, 1, 2)
}

func TestListGoneRows(t *testing.T) {
	s, f, tt := listing6(t, "en")
	f.failOn = map[int64]error{2: &client.APIError{Status: 404, Code: "not_found"}}
	choose(t, tt, "Alpha")
	mustClickWith(t, tt, ui.Shift, "Bravo")
	press(t, tt, s, "pause")
	f.settle(tt)
	if !tt.HasText(s.text.RGone) || tt.HasText(s.text.RetryFailed) {
		t.Fatalf("texts %v", tt.Texts())
	}
	mustClick(t, tt, s.text.DismissResult)
	tt.Frame()
	if !slices.Equal(f.removed, []int64{2}) || tt.HasText("Bravo") {
		t.Fatalf("removed %v", f.removed)
	}
}

func TestListExpired(t *testing.T) {
	s, f, tt := listing6(t, "en")
	f.failOn = map[int64]error{1: client.ErrUnauthorized}
	choose(t, tt, "Alpha")
	press(t, tt, s, "pause")
	f.settle(tt)
	if f.expired != 1 || s.manage.run != nil || len(chosen(s)) != 0 {
		t.Fatalf("expired %d, run %v, selected %v", f.expired, s.manage.run, chosen(s))
	}
}

func TestListDeleteAndUndo(t *testing.T) {
	s, f, tt := listing6(t, "en")
	choose(t, tt, "Bravo", "Delta")
	press(t, tt, s, "delete")
	tt.Frame()
	if !tt.HasText("Delete 2 subscriptions?") || !tt.HasText(s.text.DeleteManyBody) {
		t.Fatalf("texts %v", tt.Texts())
	}
	mustClick(t, tt, s.text.Cancel)
	if len(f.deleted) != 0 || len(chosen(s)) != 2 {
		t.Fatal("cancel deleted")
	}
	press(t, tt, s, "delete")
	tt.Frame()
	mustClick(t, tt, s.text.ConfirmDelete)
	f.settle(tt)
	tt.Frame()
	if !slices.Equal(f.deleted, []int64{2, 4}) || !slices.Equal(f.removed, []int64{2, 4}) {
		t.Fatalf("deleted %v removed %v", f.deleted, f.removed)
	}
	if tt.HasText("Bravo") || !tt.HasText("Deleted 2 subscriptions") || !tt.HasText(s.text.Undo) {
		t.Fatalf("texts %v", tt.Texts())
	}
	wantChosen(t, s)
	mustClick(t, tt, s.text.Undo)
	f.settle(tt)
	tt.Frame()
	if !slices.Equal(f.restoredIDs, []int64{2, 4}) || len(f.sources) != 6 {
		t.Fatalf("restored %v sources %d", f.restoredIDs, len(f.sources))
	}
	if !tt.HasText("Bravo") || !tt.HasText("Restored 2 subscriptions") {
		t.Fatalf("texts %v", tt.Texts())
	}
	wantChosen(t, s, 2, 4)
}

func TestListDeletePartialRetryAndUndo(t *testing.T) {
	s, f, tt := listing6(t, "en")
	f.failOn = map[int64]error{3: &client.APIError{Status: 500, Code: "server"}}
	choose(t, tt, "Alpha")
	mustClickWith(t, tt, ui.Shift, "Charlie")
	press(t, tt, s, "delete")
	tt.Frame()
	mustClick(t, tt, s.text.ConfirmDelete)
	f.settle(tt)
	tt.Frame()
	if s.manage.band != "Deleted 2. 1 couldn’t be deleted." {
		t.Fatalf("band %q", s.manage.band)
	}
	wantChosen(t, s, 3)
	if !tt.HasText("Deleted 2 subscriptions") {
		t.Fatalf("no undo toast: %v", tt.Texts())
	}
	f.failOn = nil
	mustClick(t, tt, s.text.RetryFailed)
	f.settle(tt)
	tt.Frame()
	if !tt.HasText("Deleted 3 subscriptions") || tt.HasText("Deleted 2 subscriptions") {
		t.Fatalf("the toast did not take in the retry: %v", tt.Texts())
	}
	mustClick(t, tt, s.text.Undo)
	f.settle(tt)
	tt.Frame()
	if !slices.Equal(f.restoredIDs, []int64{1, 2, 3}) || len(f.sources) != 6 {
		t.Fatalf("restored %v", f.restoredIDs)
	}
}

func TestListDeleteGoneCountsAsDeleted(t *testing.T) {
	s, f, tt := listing6(t, "en")
	f.failOn = map[int64]error{1: &client.APIError{Status: 404, Code: "not_found"}}
	choose(t, tt, "Alpha", "Bravo")
	press(t, tt, s, "delete")
	tt.Frame()
	mustClick(t, tt, s.text.ConfirmDelete)
	f.settle(tt)
	tt.Frame()
	if s.manage.band != "" || !slices.Equal(f.removed, []int64{1, 2}) {
		t.Fatalf("band %q removed %v", s.manage.band, f.removed)
	}
	if !tt.HasText("Deleted 1 subscription") {
		t.Fatalf("texts %v", tt.Texts())
	}
	f.failOn = nil
	mustClick(t, tt, s.text.Undo)
	f.settle(tt)
	if !slices.Equal(f.restoredIDs, []int64{2}) {
		t.Fatalf("restore asked for %v", f.restoredIDs)
	}
}

func TestListRestoreFails(t *testing.T) {
	s, f, tt := listing6(t, "en")
	choose(t, tt, "Alpha")
	press(t, tt, s, "delete")
	tt.Frame()
	mustClick(t, tt, s.text.ConfirmDelete)
	f.settle(tt)
	tt.Frame()
	f.failOn = map[int64]error{1: &client.APIError{Status: 404, Code: "not_found"}}
	mustClick(t, tt, s.text.Undo)
	f.settle(tt)
	tt.Frame()
	if !tt.HasText(s.text.RGone) && !tt.HasText(s.text.UndoFailed) {
		t.Fatalf("texts %v", tt.Texts())
	}
	if len(f.sources) != 5 {
		t.Fatal("a failed restore added the subscription")
	}
}

func TestListLabelsAndAnnouncements(t *testing.T) {
	s, f, tt := listing6(t, "en")
	f.sources[1].Paused = true
	f.sources[2].LastError, f.sources[2].ConsecutiveFailures = "status 503", 3
	f.sources[3].HideFromAll = true
	tt.Frame()
	if !tt.HasText(s.failingOf(f.sources[2])) || !strings.HasPrefix(s.failingOf(f.sources[2]), "Failed 3 times · ") {
		t.Fatalf("failing line %q in %v", s.failingOf(f.sources[2]), tt.Texts())
	}
	choose(t, tt, "Alpha")
	tt.Advance(heardAfter + time.Millisecond)
	tt.Frame()
	if !slices.Contains(tt.Announcements(), "1 selected") {
		t.Fatalf("announcements %v", tt.Announcements())
	}
	selected := s.rowLabel(f.sources[0], true)
	if !strings.HasPrefix(selected, "Alpha") || !strings.HasSuffix(selected, s.text.SelectedMark) {
		t.Fatalf("selected row label %q", selected)
	}
	for id, mark := range map[int]string{0: s.text.SelectedMark, 1: s.text.PausedMark, 2: s.failingOf(f.sources[2]), 3: s.text.HiddenMark} {
		label := s.rowLabel(f.sources[id], id == 0)
		if _, ok := tt.Find(label); !ok || !strings.Contains(label, mark) {
			t.Fatalf("no row labeled %q in %v", label, tt.Texts())
		}
	}
	for _, label := range []string{s.text.SelectAll, s.text.ListName, "Pause 1 subscription", "Change 1 subscription",
		"Refresh 1 subscription", "Delete 1 subscription", "Actions for 1 selected subscription"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("no %q in %v", label, tt.Texts())
		}
	}
	press(t, tt, s, "clear")
	tt.Advance(heardAfter + time.Millisecond)
	tt.Frame()
	if !slices.Contains(tt.Announcements(), s.text.NothingSelected) {
		t.Fatalf("announcements %v", tt.Announcements())
	}
}

func TestListAnnouncementWaitsForTheEnd(t *testing.T) {
	_, _, tt := listing6(t, "en")
	choose(t, tt, "Alpha", "Bravo", "Charlie")
	tt.Advance(heardAfter + time.Millisecond)
	tt.Frame()
	var heard []string
	for _, a := range tt.Announcements() {
		if strings.HasSuffix(a, "selected") {
			heard = append(heard, a)
		}
	}
	if !slices.Equal(heard, []string{"3 selected"}) {
		t.Fatalf("heard %v", heard)
	}
}

func TestListChinese(t *testing.T) {
	s, _, tt := listing6(t, "zh-Hans")
	if !tt.HasText("共 6 个订阅") || !tt.HasText("全部形态") {
		t.Fatalf("texts %v", tt.Texts())
	}
	choose(t, tt, "Alpha")
	if !tt.HasText("已选 1 个") {
		t.Fatalf("texts %v", tt.Texts())
	}
	for _, label := range []string{"暂停 1 个订阅", "修改 1 个订阅", "更新 1 个订阅", "删除 1 个订阅"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("no %q in %v", label, tt.Texts())
		}
	}
	press(t, tt, s, "change")
	if !tt.HasText("修改 1 个订阅") || !tt.HasText("保持原样") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestListLockedWhileRunning(t *testing.T) {
	s, f, tt := listing6(t, "en")
	choose(t, tt, "Alpha")
	press(t, tt, s, "pause")
	tt.Frame()
	_ = tt.ClickWith(ui.Super, "Bravo")
	_ = tt.Click("Charlie")
	if len(chosen(s)) != 1 || s.pg.open {
		t.Fatalf("a row took a click while a run was going: %v, page %v", chosen(s), s.pg.open)
	}
	f.settle(tt)
}

// TestListOpenListResets is the list as the sidebar or the menu opens it: nothing selected, no band, no page.
func TestListOpenListResets(t *testing.T) {
	s, f, tt := listing6(t, "en")
	f.failOn = map[int64]error{1: client.ErrUnreachable}
	choose(t, tt, "Alpha", "Bravo")
	press(t, tt, s, "pause")
	f.settle(tt)
	mustClick(t, tt, "Charlie")
	f.settle(tt)
	s.OpenList()
	tt.Frame()
	if len(chosen(s)) != 0 || s.manage.band != "" || s.pg.open {
		t.Fatalf("selected %v, band %q, page %v", chosen(s), s.manage.band, s.pg.open)
	}
}

func TestListResetByUse(t *testing.T) {
	s, f, tt := listing6(t, "en")
	choose(t, tt, "Alpha")
	s.Use(f)
	tt.Frame()
	if len(chosen(s)) != 0 || s.Open() {
		t.Fatal("Use left the selection")
	}
}

func TestListRowsDoNotAliasTheTimelineList(t *testing.T) {
	s, f, _ := listing6(t, "en")
	rows := s.visible()
	f.sources = slices.DeleteFunc(f.sources, func(src client.Source) bool { return src.ID == 1 })
	if len(rows) != 6 || rows[0].ID != 1 || rows[5].ID != 6 {
		t.Fatalf("the rows of the frame followed a delete: %v", rows)
	}
}

func TestListShiftRangeWhenTheAnchorIsFilteredAway(t *testing.T) {
	s, _, tt := listing6(t, "en")
	mustClick(t, tt, s.text.FilterLabel)
	tt.Type("o") // Bravo, Echo and Foxtrot
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	tt.Key(0, ui.KeySpace)
	wantChosen(t, s, 5)
	s.manage.anchor = 1 // Alpha is not in the list now
	tt.Key(ui.Shift, ui.KeyUp)
	wantChosen(t, s, 2, 5)
}

func TestListBulkTellsTheTimelineOnce(t *testing.T) {
	s, f, tt := listing6(t, "en")
	press(t, tt, s, "all")
	press(t, tt, s, "pause")
	f.settle(tt)
	if f.bulks != 1 || len(f.changed) != 6 {
		t.Fatalf("bulks %d changed %d", f.bulks, len(f.changed))
	}
	press(t, tt, s, "all")
	press(t, tt, s, "delete")
	tt.Frame()
	mustClick(t, tt, s.text.ConfirmDelete)
	f.settle(tt)
	if f.bulks != 2 || len(f.removed) != 6 {
		t.Fatalf("bulks %d removed %d", f.bulks, len(f.removed))
	}
}

func TestListUndoPressedDuringARunWaits(t *testing.T) {
	s, f, tt := listing6(t, "en")
	choose(t, tt, "Alpha", "Bravo")
	press(t, tt, s, "delete")
	tt.Frame()
	mustClick(t, tt, s.text.ConfirmDelete)
	f.settle(tt)
	tt.Frame()
	choose(t, tt, "Charlie")
	press(t, tt, s, "pause")
	if s.manage.run == nil {
		t.Fatal("no run")
	}
	mustClick(t, tt, s.text.Undo)
	if len(f.restoredIDs) != 0 || s.pending == nil {
		t.Fatalf("undo during a run: restored %v pending %v", f.restoredIDs, s.pending)
	}
	f.settle(tt)
	tt.Frame()
	if !slices.Equal(f.restoredIDs, []int64{1, 2}) || s.pending != nil || len(f.sources) != 6 {
		t.Fatalf("restored %v pending %v sources %d", f.restoredIDs, s.pending, len(f.sources))
	}
}

func TestListBulkDeleteReplacesSingleDeleteToasts(t *testing.T) {
	s, f, tt := listing6(t, "en")
	single := f.sources[5]
	s.deleted = append(s.deleted, &deletion{src: single})
	tt.Frame()
	want := s.fill(s.text.Deleted, Name(single))
	if !tt.HasText(want) {
		t.Fatalf("no toast of the single delete: %v", tt.Texts())
	}
	choose(t, tt, "Alpha")
	press(t, tt, s, "delete")
	tt.Frame()
	mustClick(t, tt, s.text.ConfirmDelete)
	f.settle(tt)
	tt.Frame()
	if tt.HasText(want) || len(s.deleted) != 0 || !tt.HasText("Deleted 1 subscription") {
		t.Fatalf("the single toast stayed: %v", tt.Texts())
	}
}

// TestListEmptiedByDelete shows No subscriptions yet once every one is deleted, and Undo brings them back.
func TestListEmptiedByDelete(t *testing.T) {
	s, f, tt := listing6(t, "en")
	press(t, tt, s, "all")
	press(t, tt, s, "delete")
	tt.Frame()
	mustClick(t, tt, s.text.ConfirmDelete)
	f.settle(tt)
	tt.Frame()
	if !tt.HasText(s.text.NoSubscriptions) || s.Open() {
		t.Fatalf("texts %v", tt.Texts())
	}
	mustClick(t, tt, s.text.Undo)
	f.settle(tt)
	tt.Frame()
	if len(f.sources) != 6 || !tt.HasText("Alpha") {
		t.Fatalf("sources %d", len(f.sources))
	}
}

func TestListRunCancelledByUse(t *testing.T) {
	s, f, tt := listing6(t, "en")
	press(t, tt, s, "all")
	press(t, tt, s, "pause")
	for _, work := range f.queue {
		work()
	}
	f.queue = nil
	if len(f.updateCtx) == 0 || f.updateCtx[0].Err() != nil {
		t.Fatalf("the requests of the run are not under way: %v", f.updateCtx)
	}
	s.Use(f)
	for i, ctx := range f.updateCtx {
		if ctx.Err() == nil {
			t.Fatalf("request %d of the run was not cancelled", i)
		}
	}
}
