package timeline

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/prefs"
)

// glance stands in for the preferences and the window of the marker of what the reader last looked at.
type glance struct {
	p      prefs.Preferences
	active bool
	kept   []prefs.Marker // what Look was asked to keep, in order
}

// looking is a timeline of n entries from Entry n, the newest, to Entry 1, each a minute apart, with a marker kept
// before the list loads. at is the number of the entry the marker stands for, 0 for none.
func looking(t *testing.T, language string, n int, at int64, active bool) (*Screen, *fake, *ui.Tester, *glance) {
	t.Helper()
	g := &glance{active: active}
	f := &fake{now: now, patches: map[int64][]client.EntryPatch{},
		sources: []client.Source{{ID: 1, Title: "Go Blog", Kind: "article", UnreadCount: n}}}
	for i := n; i >= 1; i-- {
		f.entries = append(f.entries, client.Entry{ID: int64(i), SourceID: 1, SourceTitle: "Go Blog", Kind: "article",
			Title: fmt.Sprintf("Entry %d", i), URL: fmt.Sprintf("https://go.dev/%d", i),
			CreatedAt: ago(time.Duration(n-i+1) * time.Minute)})
	}
	if at != 0 {
		g.p.LastLooked = prefs.Marker{At: ago(time.Duration(n-int(at)+1) * time.Minute), ID: at}
	}
	d := f.deps()
	d.Reading = func() prefs.Preferences { return g.p }
	d.Change = func(n prefs.Preferences) { g.p = n }
	d.Active = func() bool { return g.active }
	d.Look = func(m prefs.Marker) { g.p.LastLooked = m; g.kept = append(g.kept, m) }
	s := New(language, d)
	tt := ui.NewTester(func(c *ui.Context) { c.Theme().FontSize = 14; s.View(c) }, 1100, 800)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	tt.Frame() // the rects of a frame are read by the next
	tt.Frame()
	return s, f, tt, g
}

// The divider stands above the first entry at or older than the marker and counts the entries above it; the marker
// itself moves on to the newest entry as the list rests at the top.
func TestLastLookedDivider(t *testing.T) {
	for language, label := range map[string]string{"en": "5 new since you last looked", "zh-Hans": "上次看到这里 · 新增 5 条"} {
		t.Run(language, func(t *testing.T) {
			s, f, tt, g := looking(t, language, 60, 55, true)
			if !tt.HasText(label) {
				t.Fatalf("no divider %q in %v", label, tt.Texts())
			}
			if s.divider != 5 || s.entries[5].ID != 55 {
				t.Fatalf("the divider is above entry %d (%d)", s.divider, s.entries[5].ID)
			}
			want := prefs.Marker{At: s.entries[0].CreatedAt, ID: 60}
			if !reflect.DeepEqual(g.kept, []prefs.Marker{want}) {
				t.Fatalf("kept %v, want %v", g.kept, want)
			}
			f.now = f.now.Add(time.Minute)
			tt.Frame()
			if len(g.kept) != 1 || s.divider != 5 {
				t.Fatalf("kept %v, divider %d", g.kept, s.divider)
			}
		})
	}
}

// Entries newer than the marker are counted from what is loaded, capped at 50+.
func TestLastLookedCount(t *testing.T) {
	s, f, tt, _ := looking(t, "en", 120, 60, false)
	if s.divider != -1 {
		t.Fatalf("a divider with the marker not loaded: %d", s.divider)
	}
	s.loadMore()
	f.settle(t, tt)
	tt.Frame()
	if s.divider != 60 {
		t.Fatalf("divider %d", s.divider)
	}
	s.list.ScrollTo(s.divider, ui.Start)
	tt.Frame()
	tt.Frame()
	if !tt.HasText("50+ new since you last looked") {
		t.Fatalf("no capped count in %v", tt.Texts())
	}
}

// A kind counts only its own entries, and only the whole timeline moves the marker.
func TestLastLookedKind(t *testing.T) {
	sources, entries := sample()
	f := &fake{now: now, sources: sources, entries: entries, patches: map[int64][]client.EntryPatch{}}
	g := &glance{active: true}
	g.p.LastLooked = prefs.Marker{At: ago(50 * 24 * time.Hour), ID: 3}
	d := f.deps()
	d.Reading = func() prefs.Preferences { return g.p }
	d.Look = func(m prefs.Marker) { g.p.LastLooked = m; g.kept = append(g.kept, m) }
	d.Active = func() bool { return g.active }
	s := New("en", d)
	tt := ui.NewTester(func(c *ui.Context) { c.Theme().FontSize = 14; s.View(c) }, 1100, 800)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	tt.Frame()
	if !tt.HasText("5 new since you last looked") {
		t.Fatalf("All: %v", tt.Texts())
	}
	n := len(g.kept)
	pick(t, f, tt, "Articles")
	if !tt.HasText("2 new since you last looked") {
		t.Fatalf("Articles: %v", tt.Texts())
	}
	if len(g.kept) != n {
		t.Fatalf("a kind moved the marker: %v", g.kept)
	}
}

// There is no divider on a first run, which still starts the marker at the newest entry, and none in a search.
func TestLastLookedNone(t *testing.T) {
	s, _, tt, g := looking(t, "en", 60, 0, true)
	if s.divider != -1 || tt.HasText("new since you last looked") {
		t.Fatalf("a divider on a first run: %d", s.divider)
	}
	if len(g.kept) != 1 || g.kept[0].ID != 60 {
		t.Fatalf("kept %v", g.kept)
	}

	s, f, tt, g := looking(t, "en", 60, 55, false)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("entry")
	f.now = f.now.Add(debounce)
	tt.Frame()
	f.settle(t, tt)
	tt.Frame()
	if s.divider != -1 || tt.HasText("new since you last looked") {
		t.Fatalf("a divider in a search: %d", s.divider)
	}
	g.active = true
	tt.Frame()
	if len(g.kept) != 0 {
		t.Fatalf("a search moved the marker: %v", g.kept)
	}
}

// The marker moves only while the window is active and the list rests at the top; at most every lookEvery.
func TestLastLookedMoves(t *testing.T) {
	s, f, tt, g := looking(t, "en", 60, 55, false)
	tt.Frame()
	if len(g.kept) != 0 {
		t.Fatalf("an inactive window moved the marker: %v", g.kept)
	}
	g.active = true
	tt.Frame()
	if len(g.kept) != 1 || g.kept[0].ID != 60 {
		t.Fatalf("at the top: kept %v", g.kept)
	}
	// A newer entry is picked up and kept only after lookEvery.
	s.entries = append([]client.Entry{{ID: 61, SourceID: 1, Kind: "article", Title: "Entry 61",
		URL: "https://go.dev/61", CreatedAt: ago(0)}}, s.entries...)
	tt.Frame()
	if len(g.kept) != 1 || s.seen.ID != 61 {
		t.Fatalf("kept %v, seen %v: too soon", g.kept, s.seen)
	}
	f.now = f.now.Add(lookEvery)
	tt.Frame()
	if len(g.kept) != 2 || g.kept[1].ID != 61 {
		t.Fatalf("kept %v after the interval", g.kept)
	}
}

// The divider stays while the list opens at the top, and goes when the list is scrolled away and back; the marker does
// not move while the list is away.
func TestLastLookedScroll(t *testing.T) {
	s, _, tt, g := looking(t, "en", 60, 55, false)
	s.list.ScrollTo(30, ui.Start)
	tt.Frame()
	tt.Frame()
	g.active = true
	tt.Frame()
	if len(g.kept) != 0 || s.divider != 5 {
		t.Fatalf("away: kept %v, divider %d", g.kept, s.divider)
	}
	s.list.ScrollTo(0, ui.Start)
	tt.Frame()
	tt.Frame()
	tt.Frame()
	if s.divider != -1 || len(g.kept) != 1 || g.kept[0].ID != 60 {
		t.Fatalf("back: kept %v, divider %d", g.kept, s.divider)
	}
}

// The divider goes when it is clicked.
func TestLastLookedClick(t *testing.T) {
	s, _, tt, _ := looking(t, "en", 60, 55, true)
	if err := tt.Click("5 new since you last looked"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.divider != -1 || tt.HasText("5 new since you last looked") {
		t.Fatalf("a click left the divider: %d", s.divider)
	}
}
