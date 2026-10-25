package timeline

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// setupFolded is setup with two entries of the Go Blog over its daily cap, from the same day as its newest entry.
func setupFolded(t *testing.T, language string) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	sources, entries := sample()
	extra := []client.Entry{
		{ID: 18, SourceID: 1, SourceTitle: "Go Blog", Kind: "article", Title: "Over the cap one", URL: "https://go.dev/18",
			CreatedAt: ago(10 * time.Minute), Folded: true},
		{ID: 19, SourceID: 1, SourceTitle: "Go Blog", Kind: "article", Title: "Over the cap two", URL: "https://go.dev/19",
			CreatedAt: ago(20 * time.Minute), Folded: true},
	}
	entries = slices.Insert(entries, 1, extra...)
	f := &fake{now: now, sources: sources, entries: entries, patches: map[int64][]client.EntryPatch{}}
	s := New(language, f.deps())
	tt := newTester(func(c *ui.Context) { s.View(c); theme.Toasts(c) }, 1100, 800)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	return s, f, tt
}

func TestFoldRowShowsAndExpands(t *testing.T) {
	for language, c := range map[string]struct{ row, label, announced string }{
		"en":      {"2 more from Go Blog", "2 more from Go Blog, show", "Showed 2 entries"},
		"zh-Hans": {"还有 2 条来自 Go Blog", "还有 2 条来自 Go Blog，展开", "已展开 2 条"},
	} {
		t.Run(language, func(t *testing.T) {
			s, _, tt := setupFolded(t, language)
			if !tt.HasText(c.row) || tt.HasText("Over the cap one") || tt.HasText("Over the cap two") {
				t.Fatalf("texts %v", tt.Texts())
			}
			if len(s.entries) != 6 || len(s.folds) != 2 {
				t.Fatalf("%d entries, %d folded", len(s.entries), len(s.folds))
			}
			// The row stands after the newest entry of the day, and only the entries are in the order of j and k.
			s.selected = 0
			if err := tt.Click(c.label); err != nil {
				t.Fatalf("%v in %v", err, tt.Texts())
			}
			if tt.HasText(c.row) || !tt.HasText("Over the cap one") || !tt.HasText("Over the cap two") {
				t.Fatalf("not expanded: %v", tt.Texts())
			}
			ids := make([]int64, len(s.entries))
			for i, e := range s.entries {
				ids[i] = e.ID
			}
			if !slices.Equal(ids, []int64{8, 18, 19, 7, 6, 5, 4, 3}) || len(s.folds) != 0 {
				t.Fatalf("entries %v, folded %d", ids, len(s.folds))
			}
			// The focus goes to the first entry shown, as the row it was on is gone, and the choice follows it.
			if s.selected != 1 || !tt.Focused(s.label(s.entries[1], now)) {
				t.Fatalf("selected %d", s.selected)
			}
			if !slices.Contains(tt.Announcements(), c.announced) {
				t.Fatalf("announcements %v", tt.Announcements())
			}
		})
	}
}

func TestFoldRowKeyboard(t *testing.T) {
	s, _, tt := setupFolded(t, "en")
	// Next unread and the step keys go over entries; the fold row is a button the Tab key reaches.
	s.selected, s.focusRow = 0, 0
	tt.Key(0, ui.KeyDown)
	if s.selected != 1 || s.entries[1].ID != 7 {
		t.Fatalf("selected %d", s.selected)
	}
	for range 40 {
		if tt.Focused("2 more from Go Blog, show") {
			break
		}
		tt.Key(0, ui.KeyTab)
	}
	if !tt.Focused("2 more from Go Blog, show") {
		t.Fatal("the fold row never got the focus")
	}
	tt.Key(0, ui.KeyEnter)
	if len(s.folds) != 0 || len(s.entries) != 8 {
		t.Fatalf("Enter did not expand: %d entries, %d folded", len(s.entries), len(s.folds))
	}
}

func TestFoldedEntriesAreNotMarkedVisible(t *testing.T) {
	s, _, tt := setupFolded(t, "en")
	s.setRead([]int64{18}, true)
	if len(s.folds) != 1 || s.folds[0].ID != 19 {
		t.Fatalf("folded %v", s.folds)
	}
	tt.Frame()
	if !tt.HasText("1 more from Go Blog") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestGridsDoNotFold(t *testing.T) {
	sources, entries := sample()
	entries = slices.Insert(entries, 2, client.Entry{ID: 17, SourceID: 2, SourceTitle: "Talks", Kind: "video",
		Title: "Over the cap video", URL: "https://video.example/17", CreatedAt: ago(3 * time.Hour), Folded: true})
	f := &fake{now: now, sources: sources, entries: entries, patches: map[int64][]client.EntryPatch{}}
	s := New("en", f.deps())
	tt := newTester(func(c *ui.Context) { s.View(c); theme.Toasts(c) }, 1100, 800)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	s.choose(scope{kind: "video"})
	f.settle(t, tt)
	if s.layout() == columnLayout {
		t.Skip("Videos is a list by default here")
	}
	if len(s.folds) != 0 || s.index(17) < 0 {
		t.Fatalf("the grid folded: %d folded, entries %v", len(s.folds), s.entries)
	}
}

func TestFoldsLeaveWithTheList(t *testing.T) {
	s, f, tt := setupFolded(t, "en")
	s.choose(scope{kind: "video"})
	f.settle(t, tt)
	if len(s.folds) != 0 {
		t.Fatalf("folded %v", s.folds)
	}
	s.choose(scope{})
	f.settle(t, tt)
	if len(s.folds) != 2 || !tt.HasText("2 more from Go Blog") {
		t.Fatalf("folded %d, texts %v", len(s.folds), tt.Texts())
	}
}
