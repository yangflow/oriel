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

// managing opens the manage dialog over six subscriptions: Alpha to Foxtrot, with the IDs 1 to 6.
func managing6(t *testing.T, language string) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	s, f, _ := setup(t, language)
	f.sources = nil
	for i, name := range names {
		f.sources = append(f.sources, client.Source{ID: int64(i + 1), Title: name, URL: "https://" + strings.ToLower(name) + ".example/feed",
			Kind: "article", FetchIntervalSec: 900, FetchIntervalAuto: true})
	}
	tt := ui.NewTester(func(c *ui.Context) { s.View(c); theme.Toasts(c) }, 1000, 760)
	tt.Frame()
	s.Manage()
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
		t.Fatal(err)
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

func TestManageNeedsSubscriptions(t *testing.T) {
	s, f, _ := setup(t, "en")
	f.sources = nil
	if s.HasSources() {
		t.Fatal("HasSources with none")
	}
	s.Manage()
	if s.Open() {
		t.Fatal("opened without subscriptions")
	}
}

func TestManageOpens(t *testing.T) {
	s, _, tt := managing6(t, "en")
	if !s.Open() || !tt.HasText(s.text.ManageTitle) || !tt.HasText(s.text.ManageSub) || !tt.HasText("6 subscriptions") {
		t.Fatalf("texts %v", tt.Texts())
	}
	for _, name := range names {
		if !tt.HasText(name) {
			t.Fatalf("no row %q", name)
		}
	}
	if !tt.Focused(s.text.FilterLabel) {
		t.Fatal("the filter does not have the focus")
	}
}

func TestManageClickSelection(t *testing.T) {
	s, _, tt := managing6(t, "en")
	mustClick(t, tt, "Bravo")
	wantChosen(t, s, 2)
	mustClick(t, tt, "Delta")
	wantChosen(t, s, 4)
	mustClickWith(t, tt, ui.Super, "Alpha")
	wantChosen(t, s, 1, 4)
	mustClickWith(t, tt, ui.Ctrl, "Echo")
	wantChosen(t, s, 1, 4, 5)
	mustClickWith(t, tt, ui.Ctrl, "Delta")
	wantChosen(t, s, 1, 5)
	mustClickWith(t, tt, ui.Shift, "Foxtrot")
	wantChosen(t, s, 4, 5, 6)
	mustClick(t, tt, "Bravo")
	mustClickWith(t, tt, ui.Shift, "Delta")
	wantChosen(t, s, 2, 3, 4)
	mustClickWith(t, tt, ui.Shift, "Alpha")
	wantChosen(t, s, 1, 2)
}

func TestManageCheckbox(t *testing.T) {
	s, _, tt := managing6(t, "en")
	clickBox(t, tt, s, 3)
	wantChosen(t, s, 3)
	clickBox(t, tt, s, 5)
	wantChosen(t, s, 3, 5)
	clickBox(t, tt, s, 3)
	wantChosen(t, s, 5)
}

// press clicks a button of the action bar, by the label it has for the selection there is.
func press(t *testing.T, tt *ui.Tester, s *Screen, what string) {
	t.Helper()
	sel := s.selection(s.visible())
	label := map[string]string{
		"pause":  s.count(s.text.PauseNLabel, len(pending(sel, true))),
		"resume": s.count(s.text.ResumeNLabel, len(pending(sel, false))),
		"change": s.count(s.text.ChangeNLabel, len(sel)),
		"delete": s.count(s.text.DeleteNLabel, len(sel)),
		"clear":  s.text.ClearSelection,
		"all":    s.text.SelectAll,
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

// clickBox clicks the check box of the row of the subscription id.
func clickBox(t *testing.T, tt *ui.Tester, s *Screen, id int64) {
	t.Helper()
	i := indexOf(s.visible(), id)
	src := s.visible()[i]
	r, ok := tt.Find(s.rowLabel(src, s.manage.selected[id]))
	if !ok {
		t.Fatalf("no row for %d", id)
	}
	tt.ClickAt(r.X+12+8, r.Y+r.H/2)
}

func TestManageSelectAll(t *testing.T) {
	s, _, tt := managing6(t, "en")
	press(t, tt, s, "all")
	wantChosen(t, s, 1, 2, 3, 4, 5, 6)
	if !tt.HasText("6 selected") {
		t.Fatalf("texts %v", tt.Texts())
	}
	mustClick(t, tt, "Bravo")
	mustClickWith(t, tt, ui.Ctrl, "Charlie")
	press(t, tt, s, "all")
	wantChosen(t, s, 1, 2, 3, 4, 5, 6)
	press(t, tt, s, "all")
	wantChosen(t, s)
}

func TestManageKeyboard(t *testing.T) {
	s, _, tt := managing6(t, "en")
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
	tt.Key(0, ui.KeyEscape)
	wantChosen(t, s)
	tt.Key(0, ui.KeyEnd)
	tt.Key(0, ui.KeySpace)
	wantChosen(t, s, 6)
	tt.Key(0, ui.KeyHome)
	tt.Key(0, ui.KeySpace)
	wantChosen(t, s, 1, 6)
	tt.Key(ui.Ctrl, ui.KeyA)
	wantChosen(t, s, 1, 2, 3, 4, 5, 6)
	tt.Key(ui.Shift, ui.KeyTab)
	tt.Frame()
	if !tt.Focused(s.text.FilterLabel) {
		t.Fatal("Shift+Tab did not return to the filter")
	}
}

func TestManageEscapeOrder(t *testing.T) {
	s, f, tt := managing6(t, "en")
	f.failOn = map[int64]error{2: &client.APIError{Status: 500, Code: "server"}}
	mustClick(t, tt, "Alpha")
	mustClickWith(t, tt, ui.Ctrl, "Bravo")
	mustClick(t, tt, s.text.Delete)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if s.manage.confirm || !s.manage.open {
		t.Fatalf("escape on the confirmation: confirm %v open %v", s.manage.confirm, s.manage.open)
	}
	wantChosen(t, s, 1, 2)
	mustClick(t, tt, s.text.ChangeN)
	tt.Frame()
	if !s.manage.sheet.open {
		t.Fatal("no change sheet")
	}
	tt.Key(0, ui.KeyEscape)
	if s.manage.sheet.open || !s.manage.open {
		t.Fatal("escape did not close the sheet only")
	}
	wantChosen(t, s, 1, 2)
	tt.Key(0, ui.KeyEscape)
	wantChosen(t, s)
	if !s.manage.open {
		t.Fatal("escape with a selection closed the dialog")
	}
	tt.Key(0, ui.KeyEscape)
	if s.manage.open || s.Open() {
		t.Fatal("escape did not close the dialog")
	}
}

func TestManageFilter(t *testing.T) {
	s, _, tt := managing6(t, "en")
	mustClick(t, tt, "Bravo")
	mustClick(t, tt, s.text.FilterLabel)
	tt.Type("char")
	tt.Frame()
	if tt.HasText("Bravo") || !tt.HasText("Charlie") || tt.HasText("Alpha") {
		t.Fatalf("texts %v", tt.Texts())
	}
	wantChosen(t, s)
	press(t, tt, s, "all")
	wantChosen(t, s, 3)
}

func TestManagePauseAndResume(t *testing.T) {
	s, f, tt := managing6(t, "en")
	f.sources[1].Paused = true
	tt.Frame()
	mustClick(t, tt, "Alpha")
	mustClickWith(t, tt, ui.Shift, "Charlie")
	press(t, tt, s, "pause")
	if len(f.queue) != 2 {
		t.Fatalf("%d requests started, want the 2 that are not paused", len(f.queue))
	}
	if !tt.HasText("Pausing… 0 of 2") {
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

func TestManagePauseOnlyForLive(t *testing.T) {
	s, f, tt := managing6(t, "en")
	f.sources[0].Paused = true
	tt.Frame()
	mustClick(t, tt, "Alpha")
	if hasButton(tt, s, "pause") || !hasButton(tt, s, "resume") {
		t.Fatal("an all paused selection offers Pause or no Resume")
	}
	mustClickWith(t, tt, ui.Ctrl, "Bravo")
	if !hasButton(tt, s, "pause") || !hasButton(tt, s, "resume") {
		t.Fatal("a mixed selection should offer both")
	}
}

func TestManageParallelAndProgress(t *testing.T) {
	s, f, tt := managing6(t, "en")
	press(t, tt, s, "all")
	press(t, tt, s, "pause")
	if len(f.queue) != manageParallel {
		t.Fatalf("%d requests in flight, want %d", len(f.queue), manageParallel)
	}
	f.queue[0]()()
	f.queue = f.queue[1:]
	tt.Frame()
	if len(f.queue) != manageParallel || !tt.HasText("Pausing… 1 of 6") {
		t.Fatalf("queue %d, texts %v", len(f.queue), tt.Texts())
	}
	if !slices.Contains(tt.Announcements(), "Pausing 6 subscriptions") {
		t.Fatalf("announcements %v", tt.Announcements())
	}
	tt.Key(0, ui.KeyEscape)
	if s.manage.run == nil || !s.manage.open {
		t.Fatal("escape stopped a run")
	}
	f.settle(tt)
	if s.manage.run != nil || len(f.patches) != 6 {
		t.Fatalf("run %v patches %d", s.manage.run, len(f.patches))
	}
}

func TestManageChangeSendsOnlyChanged(t *testing.T) {
	s, f, tt := managing6(t, "en")
	mustClick(t, tt, "Bravo")
	mustClickWith(t, tt, ui.Ctrl, "Delta")
	press(t, tt, s, "change")
	if !tt.HasText("Change settings for 2 subscriptions") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if !tt.HasText(s.text.KeepAsIs) {
		t.Fatalf("texts %v", tt.Texts())
	}
	apply := func() {
		t.Helper()
		mustClick(t, tt, s.text.Apply)
		f.settle(tt)
	}
	// Apply waits for a change.
	mustClick(t, tt, s.text.Apply)
	if len(f.queue) != 0 || !s.manage.sheet.open {
		t.Fatal("Apply without a change sent something")
	}
	s.manage.sheet.kind = 2
	apply()
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

func TestManageChangeIntervalAndKeep(t *testing.T) {
	s, f, tt := managing6(t, "en")
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

func TestManageChangeChoices(t *testing.T) {
	s, f, tt := managing6(t, "en")
	f.sources[0].Kind = "video"
	mustClick(t, tt, "Alpha")
	mustClickWith(t, tt, ui.Ctrl, "Bravo")
	press(t, tt, s, "change")
	if got := strings.Count(strings.Join(tt.Texts(), "|"), s.text.NowMixed); got < 1 {
		t.Fatalf("no mixed note in %v", tt.Texts())
	}
	if !tt.HasText(put(s.text.NowOne, "x", s.every(0))) {
		t.Fatalf("no common interval in %v", tt.Texts())
	}
}

func TestManagePartialFailureAndRetry(t *testing.T) {
	s, f, tt := managing6(t, "en")
	f.failOn = map[int64]error{2: &client.APIError{Status: 500, Code: "server"}, 3: client.ErrUnreachable}
	mustClick(t, tt, "Alpha")
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
	if f.sources[0].Paused != true || f.sources[1].Paused {
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

func TestManageAllFailed(t *testing.T) {
	s, f, tt := managing6(t, "en")
	f.failOn = map[int64]error{1: client.ErrUnreachable, 2: client.ErrUnreachable}
	mustClick(t, tt, "Alpha")
	mustClickWith(t, tt, ui.Ctrl, "Bravo")
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

func TestManageGoneRows(t *testing.T) {
	s, f, tt := managing6(t, "en")
	f.failOn = map[int64]error{2: &client.APIError{Status: 404, Code: "not_found"}}
	mustClick(t, tt, "Alpha")
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

func TestManageExpired(t *testing.T) {
	s, f, tt := managing6(t, "en")
	f.failOn = map[int64]error{1: client.ErrUnauthorized}
	mustClick(t, tt, "Alpha")
	press(t, tt, s, "pause")
	f.settle(tt)
	if f.expired != 1 || s.manage.open || s.Open() {
		t.Fatalf("expired %d open %v", f.expired, s.manage.open)
	}
}

func TestManageDeleteAndUndo(t *testing.T) {
	s, f, tt := managing6(t, "en")
	mustClick(t, tt, "Bravo")
	mustClickWith(t, tt, ui.Ctrl, "Delta")
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

func TestManageDeletePartialRetryAndUndo(t *testing.T) {
	s, f, tt := managing6(t, "en")
	f.failOn = map[int64]error{3: &client.APIError{Status: 500, Code: "server"}}
	mustClick(t, tt, "Alpha")
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

func TestManageDeleteGoneCountsAsDeleted(t *testing.T) {
	s, f, tt := managing6(t, "en")
	f.failOn = map[int64]error{1: &client.APIError{Status: 404, Code: "not_found"}}
	mustClick(t, tt, "Alpha")
	mustClickWith(t, tt, ui.Ctrl, "Bravo")
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

func TestManageRestoreFails(t *testing.T) {
	s, f, tt := managing6(t, "en")
	mustClick(t, tt, "Alpha")
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

func TestManageLabelsAndAnnouncements(t *testing.T) {
	s, f, tt := managing6(t, "en")
	f.sources[1].Paused = true
	f.sources[2].LastError = "boom"
	tt.Frame()
	mustClick(t, tt, "Alpha")
	tt.Advance(heardAfter + time.Millisecond)
	tt.Frame()
	if !slices.Contains(tt.Announcements(), "1 selected") {
		t.Fatalf("announcements %v", tt.Announcements())
	}
	selected := s.rowLabel(f.sources[0], true)
	if !strings.HasPrefix(selected, "Alpha") || !strings.HasSuffix(selected, s.text.SelectedMark) {
		t.Fatalf("selected row label %q", selected)
	}
	for id, mark := range map[int]string{0: selected, 1: s.text.PausedMark, 2: s.text.FailingMark} {
		label := s.rowLabel(f.sources[id], id == 0)
		if _, ok := tt.Find(label); !ok || !strings.Contains(label, mark) {
			t.Fatalf("no row labeled %q in %v", label, tt.Texts())
		}
	}
	for _, label := range []string{s.text.SelectAll, "Pause 1 subscription", "Change settings of 1 subscription", "Delete 1 subscription",
		"Actions for 1 selected subscription"} {
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

func TestManageAnnouncementWaitsForTheEnd(t *testing.T) {
	_, _, tt := managing6(t, "en")
	mustClick(t, tt, "Alpha")
	mustClickWith(t, tt, ui.Ctrl, "Bravo")
	mustClickWith(t, tt, ui.Ctrl, "Charlie")
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

func TestManageChinese(t *testing.T) {
	s, _, tt := managing6(t, "zh-Hans")
	if !tt.HasText(s.text.ManageTitle) || s.text.ManageTitle != "管理订阅" {
		t.Fatalf("texts %v", tt.Texts())
	}
	mustClick(t, tt, "Alpha")
	if !tt.HasText("已选 1 个") {
		t.Fatalf("texts %v", tt.Texts())
	}
	for _, label := range []string{"暂停 1 个订阅", "修改 1 个订阅的设置", "删除 1 个订阅"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("no %q in %v", label, tt.Texts())
		}
	}
}

func TestManageLockedWhileRunning(t *testing.T) {
	s, f, tt := managing6(t, "en")
	mustClick(t, tt, "Alpha")
	press(t, tt, s, "pause")
	tt.Frame()
	if err := tt.Click("Bravo"); err == nil && len(chosen(s)) != 1 {
		t.Fatal("a row took a click while a run was going")
	}
	if len(chosen(s)) != 1 {
		t.Fatalf("selection changed during a run: %v", chosen(s))
	}
	f.settle(tt)
}

func TestManageClosedByDeps(t *testing.T) {
	s, f, tt := managing6(t, "en")
	mustClick(t, tt, "Alpha")
	s.Use(f)
	tt.Frame()
	if s.manage.open || s.Open() {
		t.Fatal("Use left the dialog open")
	}
}

func TestManageRowsDoNotAliasTheTimelineList(t *testing.T) {
	s, f, _ := managing6(t, "en")
	rows := s.visible()
	f.sources = slices.DeleteFunc(f.sources, func(src client.Source) bool { return src.ID == 1 })
	if len(rows) != 6 || rows[0].ID != 1 || rows[5].ID != 6 {
		t.Fatalf("the rows of the frame followed a delete: %v", rows)
	}
}

func TestManageShiftRangeWhenTheAnchorIsFilteredAway(t *testing.T) {
	s, _, tt := managing6(t, "en")
	mustClick(t, tt, s.text.FilterLabel)
	tt.Type("o")
	tt.Frame()
	s.manage.selected, s.manage.anchor = map[int64]bool{5: true}, 1 // Alpha is not in the list now
	tt.Key(0, ui.KeyDown)
	tt.Key(ui.Shift, ui.KeyDown)
	wantChosen(t, s, 2, 5)
}

func TestManageBulkTellsTheTimelineOnce(t *testing.T) {
	s, f, tt := managing6(t, "en")
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

func TestManageUndoPressedDuringARunWaits(t *testing.T) {
	s, f, tt := managing6(t, "en")
	mustClick(t, tt, "Alpha")
	mustClickWith(t, tt, ui.Ctrl, "Bravo")
	press(t, tt, s, "delete")
	tt.Frame()
	mustClick(t, tt, s.text.ConfirmDelete)
	f.settle(tt)
	tt.Frame()
	mustClick(t, tt, "Charlie")
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

func TestManageBulkDeleteReplacesSingleDeleteToasts(t *testing.T) {
	s, f, tt := managing6(t, "en")
	single := f.sources[5]
	s.deleted = append(s.deleted, &deletion{src: single})
	tt.Frame()
	want := s.fill(s.text.Deleted, Name(single))
	if !tt.HasText(want) {
		t.Fatalf("no toast of the single delete: %v", tt.Texts())
	}
	mustClick(t, tt, "Alpha")
	press(t, tt, s, "delete")
	tt.Frame()
	mustClick(t, tt, s.text.ConfirmDelete)
	f.settle(tt)
	tt.Frame()
	if tt.HasText(want) || len(s.deleted) != 0 || !tt.HasText("Deleted 1 subscription") {
		t.Fatalf("the single toast stayed: %v", tt.Texts())
	}
}

func TestManageClosesWhenTheLastSubscriptionIsGone(t *testing.T) {
	s, f, tt := managing6(t, "en")
	press(t, tt, s, "all")
	press(t, tt, s, "delete")
	tt.Frame()
	mustClick(t, tt, s.text.ConfirmDelete)
	f.settle(tt)
	tt.Frame()
	if s.manage.open || s.Open() {
		t.Fatal("the dialog stayed over an empty list")
	}
	mustClick(t, tt, s.text.Undo)
	f.settle(tt)
	tt.Frame()
	if len(f.sources) != 6 {
		t.Fatalf("sources %d", len(f.sources))
	}
}

func TestManageRunCancelledByUse(t *testing.T) {
	s, f, tt := managing6(t, "en")
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
