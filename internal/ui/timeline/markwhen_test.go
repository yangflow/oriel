package timeline

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/prefs"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// mode is the settings a "Mark as read" test runs with, and whether the window has the focus.
type mode struct {
	p      prefs.Preferences
	active bool
}

// whenMarked is crowd with the choice of when entries are marked read, and a window that has the focus. height is the
// window's height with the 13 point font of macOS; the window grows with the font of the system (14 on Linux and
// Windows) so that it shows as many rows, which the scrolling in the tests counts on.
func whenMarked(t *testing.T, when string, n, height int) (*Screen, *fake, *ui.Tester, *mode) {
	t.Helper()
	height = int(float32(height) * theme.Px(ui.LightTheme(), 13) / 13)
	m := &mode{p: prefs.Default(), active: true}
	m.p.MarkReadWhen, m.p.MarkReadDwellSec = when, 5
	f := &fake{now: now, patches: map[int64][]client.EntryPatch{},
		sources: []client.Source{{ID: 1, Title: "Go Blog", Kind: "article", UnreadCount: n}}}
	for i := n; i >= 1; i-- {
		kind, url := "article", fmt.Sprintf("https://go.dev/%d", i)
		if i == 1 {
			kind, url = "video", "https://video.example/1"
		}
		f.entries = append(f.entries, client.Entry{ID: int64(i), SourceID: 1, SourceTitle: "Go Blog", Kind: kind,
			Title: fmt.Sprintf("Entry %d", i), URL: url, CreatedAt: ago(time.Duration(n-i+1) * time.Minute)})
	}
	d := f.deps()
	d.Reading = func() prefs.Preferences { return m.p }
	d.Active = func() bool { return m.active }
	s := New("en", d)
	tt := ui.NewTester(func(c *ui.Context) { s.View(c); theme.Toasts(c) }, 1100, height)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	tt.Frame()
	tt.Frame()
	return s, f, tt, m
}

// row is the name of the unread row i.
func row(s *Screen, i int) string {
	return fmt.Sprintf("Go Blog, %s, %d min ago, Unread", s.entries[i].Title, i+1)
}

// openRow opens the entry at i by a click and lets the reader load.
func openRow(t *testing.T, s *Screen, f *fake, tt *ui.Tester, i int) {
	t.Helper()
	if err := tt.Click(row(s, i)); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	tt.Frame()
}

// stayFor lets the entry on show stay for secs seconds, a second and a frame at a time.
func stayFor(t *testing.T, f *fake, tt *ui.Tester, secs int) {
	t.Helper()
	for range secs {
		f.pass(t, tt, time.Second)
	}
}

// scrollTo moves the list as the reader's scrolling does, and draws the frames that see it.
func scrollTo(tt *ui.Tester, s *Screen, i int) {
	s.list.ScrollTo(i, ui.Start)
	tt.Frame()
	tt.Frame()
}

func readIDs(s *Screen) []int64 {
	var ids []int64
	for _, e := range s.entries {
		if e.IsRead {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

func TestMarkWhenOpen(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkOnOpen, 20, 600)
	openRow(t, s, f, tt, 0)
	if !s.entries[0].IsRead {
		t.Fatal("opening did not mark the article")
	}
	if got := f.patches[s.entries[0].ID]; len(got) != 1 || !*got[0].IsRead {
		t.Fatalf("patches %v", got)
	}
}

func TestMarkWhenNever(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkNever, 20, 600)
	openRow(t, s, f, tt, 0)
	stayFor(t, f, tt, 30)
	tt.Key(0, ui.KeyEscape)
	scrollTo(tt, s, 10)
	f.pass(t, tt, time.Second)
	if len(f.patches) != 0 || len(readIDs(s)) != 0 {
		t.Fatalf("marked %v with %v sent", readIDs(s), f.patches)
	}
	// What the reader marks stays marked.
	tt.Key(0, ui.KeyJ)
	tt.Key(0, ui.KeyM)
	f.settle(t, tt)
	if len(readIDs(s)) != 1 {
		t.Fatalf("read %v", readIDs(s))
	}
}

func TestMarkWhenStay(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkAfterStay, 20, 600)
	id := s.entries[0].ID
	openRow(t, s, f, tt, 0)
	if s.entries[0].IsRead {
		t.Fatal("opening marked the article before the stay")
	}
	stayFor(t, f, tt, 4)
	if s.entries[0].IsRead {
		t.Fatal("marked before the stay was over")
	}
	stayFor(t, f, tt, 1)
	if !s.entries[0].IsRead || len(f.patches[id]) != 1 {
		t.Fatalf("not marked after the stay: %v", f.patches[id])
	}
	if !slices.Contains(tt.Announcements(), s.text.MarkRead) {
		t.Fatalf("announcements %v", tt.Announcements())
	}
	if tt.HasText("Undo") {
		t.Fatal("a mark by the stay offers an Undo toast")
	}
	stayFor(t, f, tt, 10)
	if len(f.patches[id]) != 1 {
		t.Fatalf("patches %v", f.patches[id])
	}
}

func TestMarkWhenStayChosenTime(t *testing.T) {
	s, f, tt, m := whenMarked(t, prefs.MarkAfterStay, 20, 600)
	m.p.MarkReadDwellSec = 3
	openRow(t, s, f, tt, 0)
	stayFor(t, f, tt, 3)
	if !s.entries[0].IsRead {
		t.Fatal("not marked after 3 seconds")
	}
}

func TestMarkWhenStayPausesWithoutFocus(t *testing.T) {
	s, f, tt, m := whenMarked(t, prefs.MarkAfterStay, 20, 600)
	openRow(t, s, f, tt, 0)
	stayFor(t, f, tt, 3)
	m.active = false
	stayFor(t, f, tt, 30)
	if s.entries[0].IsRead {
		t.Fatal("the time without the focus counted")
	}
	m.active = true
	stayFor(t, f, tt, 2) // the first frame with the focus only starts the count
	if s.entries[0].IsRead {
		t.Fatal("marked early")
	}
	stayFor(t, f, tt, 1)
	if !s.entries[0].IsRead {
		t.Fatal("the stay before the pause was lost")
	}
}

func TestMarkWhenStayIgnoresAGap(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkAfterStay, 20, 600)
	openRow(t, s, f, tt, 0)
	f.pass(t, tt, time.Second)
	f.pass(t, tt, time.Hour) // no frame was drawn for an hour
	if s.entries[0].IsRead {
		t.Fatal("an hour with no frame counted whole")
	}
}

func TestMarkWhenStayStartsOverOnNavigation(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkAfterStay, 20, 600)
	openRow(t, s, f, tt, 0)
	stayFor(t, f, tt, 4)
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	openRow(t, s, f, tt, 0)
	stayFor(t, f, tt, 4)
	if s.entries[0].IsRead {
		t.Fatal("the stay before leaving counted again")
	}
	stayFor(t, f, tt, 1)
	if !s.entries[0].IsRead {
		t.Fatal("not marked after a whole stay")
	}
	// Stepping to another article in the reader starts over for it.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	openRow(t, s, f, tt, 1)
	stayFor(t, f, tt, 4)
	tt.Key(0, ui.KeyJ)
	f.settle(t, tt)
	stayFor(t, f, tt, 4)
	if s.entries[1].IsRead || s.entries[2].IsRead {
		t.Fatalf("read %v", readIDs(s))
	}
}

func TestMarkWhenStayUnreadStays(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkAfterStay, 20, 600)
	id := s.entries[0].ID
	openRow(t, s, f, tt, 0)
	stayFor(t, f, tt, 5)
	if !s.entries[0].IsRead {
		t.Fatal("not marked")
	}
	// The reader marks it unread: the stay does not mark it again.
	tt.Key(0, ui.KeyM)
	f.settle(t, tt)
	if s.entries[0].IsRead {
		t.Fatal("M did not mark it unread")
	}
	stayFor(t, f, tt, 20)
	if s.entries[0].IsRead || len(f.patches[id]) != 2 {
		t.Fatalf("read %v, patches %v", s.entries[0].IsRead, f.patches[id])
	}
}

func TestMarkWhenStayMarkedByHandFirst(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkAfterStay, 20, 600)
	id := s.entries[0].ID
	openRow(t, s, f, tt, 0)
	tt.Key(0, ui.KeyM)
	f.settle(t, tt)
	if !s.entries[0].IsRead {
		t.Fatal("M did not mark it")
	}
	stayFor(t, f, tt, 20)
	if len(f.patches[id]) != 1 {
		t.Fatalf("patches %v", f.patches[id])
	}
}

func TestMarkWhenStayBrowserMarksAtOnce(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkAfterStay, 20, 600)
	last := len(s.entries) - 1
	scrollTo(tt, s, last)
	if err := tt.Click(fmt.Sprintf("Go Blog, Entry 1, %d min ago, Unread", len(s.entries))); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if !s.entries[last].IsRead {
		t.Fatal("an entry opened in the browser waits for the stay")
	}
}

func TestMarkWhenStayDoesNotCountTheList(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkAfterStay, 20, 600)
	stayFor(t, f, tt, 20)
	if len(readIDs(s)) != 0 {
		t.Fatalf("read %v", readIDs(s))
	}
}

// scrolled lets the list rest after a scroll, long enough for the entries scrolled past to be marked.
func scrolled(t *testing.T, f *fake, tt *ui.Tester) {
	t.Helper()
	f.pass(t, tt, passQuiet)
	f.pass(t, tt, time.Second)
}

func TestMarkWhenScrollPast(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkOnScroll, 60, 420)
	first, _ := shown(t, s)
	if first != 0 {
		t.Fatalf("the list starts at %d", first)
	}
	// Nothing is marked by looking at the list, or by waiting.
	f.pass(t, tt, 10*time.Second)
	if len(readIDs(s)) != 0 {
		t.Fatalf("read %v", readIDs(s))
	}
	scrollTo(tt, s, 4)
	top := s.top()
	past := 0
	for i := range s.entries {
		if r, ok := s.rects[i]; ok && r.Y+r.H <= top {
			past = i + 1
		}
	}
	if past < 3 {
		t.Fatalf("only %d entries went past the top", past)
	}
	// They wait for the list to rest.
	f.pass(t, tt, passQuiet/2)
	if len(readIDs(s)) != 0 {
		t.Fatalf("read %v before the list rested", readIDs(s))
	}
	scrolled(t, f, tt)
	for i, e := range s.entries {
		if e.IsRead != (i < past) {
			t.Fatalf("entry %d read %v, %d went past", e.ID, e.IsRead, past)
		}
	}
	if len(f.patches) != past {
		t.Fatalf("%d changes sent for %d entries", len(f.patches), past)
	}
	want := fmt.Sprintf("Marked %d scrolled-past entries as read", past)
	if !tt.HasText(want) || !tt.HasText("Undo") {
		t.Fatalf("no %q in %v", want, tt.Texts())
	}
}

func TestMarkWhenScrollPastUndo(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkOnScroll, 60, 420)
	scrollTo(tt, s, 5)
	scrolled(t, f, tt)
	read := readIDs(s)
	if len(read) < 3 {
		t.Fatalf("read %v", read)
	}
	if err := tt.Click("Undo"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if len(f.unmarked) != 1 {
		t.Fatalf("unmarked %v", f.unmarked)
	}
	got := slices.Clone(f.unmarked[0])
	slices.Sort(got)
	slices.Sort(read)
	if !slices.Equal(got, read) {
		t.Fatalf("unmarked %v, want %v", got, read)
	}
}

func TestMarkWhenScrollPastUndoKey(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkOnScroll, 60, 420)
	scrollTo(tt, s, 5)
	scrolled(t, f, tt)
	tt.Key(ui.Cmd, ui.KeyZ)
	f.settle(t, tt)
	if len(f.unmarked) != 1 {
		t.Fatalf("unmarked %v", f.unmarked)
	}
}

func TestMarkWhenScrollPastOnlyWhatWasSeen(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkOnScroll, 60, 420)
	_, end := shown(t, s)
	// A jump far down passes the entries that were in view and none of those that never were.
	scrollTo(tt, s, 40)
	scrolled(t, f, tt)
	read := readIDs(s)
	if len(read) == 0 || len(read) > end+1 {
		t.Fatalf("read %v after a jump from a window of %d entries", read, end)
	}
	for _, e := range s.entries[end+1 : 38] {
		if e.IsRead {
			t.Fatalf("entry %d was never in view and was marked", e.ID)
		}
	}
}

func TestMarkWhenScrollPastBackUpMarksNothingMore(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkOnScroll, 60, 420)
	scrollTo(tt, s, 5)
	scrolled(t, f, tt)
	before := len(f.patches)
	scrollTo(tt, s, 0)
	scrolled(t, f, tt)
	if len(f.patches) != before {
		t.Fatalf("scrolling back up sent %d more changes", len(f.patches)-before)
	}
}

func TestMarkWhenScrollPastNotByTheProgram(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkOnScroll, 60, 420)
	s.list.ScrollTo(6, ui.Start)
	s.pass.rebase() // as a list put in place by the program does
	tt.Frame()
	tt.Frame()
	tt.Frame()
	scrolled(t, f, tt)
	if len(readIDs(s)) != 0 || len(f.patches) != 0 {
		t.Fatalf("a scroll by the program marked %v", readIDs(s))
	}
	// The reader's own scrolling from there counts again.
	scrollTo(tt, s, 14)
	scrolled(t, f, tt)
	if len(readIDs(s)) == 0 {
		t.Fatal("the scroll after it marked nothing")
	}
}

func TestMarkWhenScrollPastOpeningDoesNotMark(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkOnScroll, 60, 420)
	openRow(t, s, f, tt, 0)
	stayFor(t, f, tt, 20)
	if s.entries[0].IsRead {
		t.Fatal("opening marked the entry")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Frame()
	scrolled(t, f, tt)
	if len(readIDs(s)) != 0 {
		t.Fatalf("read %v after leaving the reader", readIDs(s))
	}
}

func TestMarkWhenScrollPastWhenLeavingTheScope(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkOnScroll, 60, 420)
	scrollTo(tt, s, 5)
	if len(readIDs(s)) != 0 {
		t.Fatal("marked before the list rested")
	}
	// Choosing another list does not lose what went past.
	if err := tt.Click("Go Blog"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if len(f.patches) < 3 {
		t.Fatalf("%d changes sent when the list was left", len(f.patches))
	}
}

func TestMarkWhenScrollPastSendsFourAtATime(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkOnScroll, 60, 420)
	f.slow = true
	for i := 4; i <= 28; i += 4 {
		scrollTo(tt, s, i)
	}
	scrolled(t, f, tt)
	if len(f.patches) < 8 {
		t.Fatalf("%d changes", len(f.patches))
	}
	if f.widest < 2 || f.widest > passWorkers {
		t.Fatalf("%d changes at once, want 2 to %d", f.widest, passWorkers)
	}
}

func TestMarkWhenScrollPastRefused(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkOnScroll, 60, 420)
	f.setErr = &client.APIError{Status: 500, Code: "internal"}
	scrollTo(tt, s, 5)
	scrolled(t, f, tt)
	if len(readIDs(s)) != 0 || s.sources[0].UnreadCount != 60 {
		t.Fatalf("read %v after the server refused", readIDs(s))
	}
	if !tt.HasText("Couldn’t save the change") || tt.HasText("Undo") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestMarkWhenScrollPastOffline(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkOnScroll, 60, 420)
	f.setErr = fmt.Errorf("offline")
	scrollTo(tt, s, 5)
	scrolled(t, f, tt)
	if len(readIDs(s)) == 0 || len(s.queue) != len(readIDs(s)) {
		t.Fatalf("read %v, kept %d to send later", readIDs(s), len(s.queue))
	}
}

func TestMarkWhenOtherModesLeaveTheScroll(t *testing.T) {
	s, f, tt, _ := whenMarked(t, prefs.MarkOnOpen, 60, 420)
	scrollTo(tt, s, 10)
	scrolled(t, f, tt)
	if len(f.patches) != 0 {
		t.Fatalf("scrolling marked %v", readIDs(s))
	}
}

func TestPassSigIgnoresLayoutJitter(t *testing.T) {
	base := passSig{first: 4, n: 6, sum: 1065.3462}
	for _, tc := range []struct {
		name string
		sig  passSig
		want bool
	}{
		{"the same", base, true},
		{"rounded differently", passSig{first: 4, n: 6, sum: 1065.3463}, true},
		{"rounded the other way", passSig{first: 4, n: 6, sum: 1065.0}, true},
		{"scrolled a little", passSig{first: 4, n: 6, sum: 1066.5}, false},
		{"another first row", passSig{first: 5, n: 6, sum: 1065.3462}, false},
		{"another count", passSig{first: 4, n: 7, sum: 1065.3462}, false},
	} {
		if got := tc.sig.same(base) && base.same(tc.sig); got != tc.want {
			t.Errorf("%s: same is %v, want %v", tc.name, got, tc.want)
		}
	}
}
