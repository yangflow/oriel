package timeline

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/prefs"
)

// communing is viewing with a Community subscription of three posts on a 1100 by 800 window, whose main column is 876
// wide, enough for three panes; it shows the Community.
func communing(t *testing.T, p *prefs.Preferences) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	s, f, tt := viewing(t, p)
	f.sources = append(f.sources, client.Source{ID: 5, Title: "Board", Kind: "forum", UnreadCount: 3})
	for i, title := range []string{"Third thread", "Second thread", "First thread"} {
		f.entries = append(f.entries, client.Entry{ID: int64(20 - i), SourceID: 5, SourceTitle: "Board", Kind: "forum",
			Title: title, URL: "https://board.example/" + title, CreatedAt: ago(time.Duration(i+1) * time.Minute)})
	}
	slices.SortFunc(f.entries, func(a, b client.Entry) int { return int(b.CreatedAt - a.CreatedAt) })
	s.loadSources()
	f.settle(t, tt)
	pick(t, f, tt, "Community")
	return s, f, tt
}

// In three panes the list stays beside the reader: J and K swap the post without leaving the list, and Escape closes
// the post to the note.
func TestCommunityPanes(t *testing.T) {
	p := prefs.Default()
	s, f, tt := communing(t, &p)
	if s.scope.kind != "forum" || !s.paned {
		t.Fatalf("scope %v, paned %v", s.scope, s.paned)
	}
	if !tt.HasText("Choose a post to read") || !tt.HasText("Use J and K, or click a row.") {
		t.Fatalf("no note in %v", tt.Texts())
	}
	tt.Key(0, ui.KeyJ)
	f.settle(t, tt)
	if s.reading != 20 || !tt.HasText("The text of Third thread.") || tt.HasText("Choose a post to read") {
		t.Fatalf("reading %d, texts %v", s.reading, tt.Texts())
	}
	if !tt.HasText("Second thread") || !tt.HasText("First thread") {
		t.Fatalf("the list left the window: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyJ)
	f.settle(t, tt)
	if s.reading != 19 || !tt.HasText("The text of Second thread.") || !tt.HasText("Third thread") {
		t.Fatalf("reading %d, texts %v", s.reading, tt.Texts())
	}
	tt.Key(0, ui.KeyK)
	f.settle(t, tt)
	if s.reading != 20 {
		t.Fatalf("K read %d", s.reading)
	}
	// A click on a row of the list swaps the post as well.
	if err := tt.Click("Board, First thread, 3 min ago, Unread"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if s.reading != 18 || !tt.HasText("The text of First thread.") {
		t.Fatalf("reading %d, texts %v", s.reading, tt.Texts())
	}
	// The keys of the list that change it still act with a post open.
	tt.Key(0, ui.KeyU)
	f.settle(t, tt)
	if s.filter != unread {
		t.Fatalf("U left the filter at %d", s.filter)
	}
	tt.Key(0, ui.KeyEscape)
	f.settle(t, tt)
	if s.reading != 0 || !tt.HasText("Choose a post to read") {
		t.Fatalf("reading %d, texts %v", s.reading, tt.Texts())
	}
}

// A window too narrow for three panes, and the viewer List, open a post in place of the list, as an article does.
func TestCommunityFallsBack(t *testing.T) {
	p := prefs.Default()
	s, f, tt := communing(t, &p)
	tt.SetSize(900, 800)
	tt.Frame()
	if s.paned || tt.HasText("Choose a post to read") {
		t.Fatalf("paned in a narrow window: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyJ)
	if s.reading != 0 || s.selected < 0 {
		t.Fatalf("J opened a post in the list: reading %d", s.reading)
	}
	tt.Key(0, ui.KeyEnter)
	f.settle(t, tt)
	if s.reading != 20 || tt.HasText("Second thread") {
		t.Fatalf("reading %d, the list shows: %v", s.reading, tt.Texts())
	}
	// Widening the window while the post is open draws the panes around it.
	tt.SetSize(1100, 800)
	tt.Frame()
	if !s.paned || s.reading != 20 || !tt.HasText("Second thread") || !tt.HasText("The text of Third thread.") {
		t.Fatalf("paned %v, reading %d, texts %v", s.paned, s.reading, tt.Texts())
	}
	tt.SetSize(900, 800)
	tt.Frame()
	if s.paned || s.reading != 20 {
		t.Fatalf("paned %v, reading %d", s.paned, s.reading)
	}
	tt.Key(0, ui.KeyEscape)
	f.settle(t, tt)
	if s.reading != 0 || !tt.HasText("Second thread") {
		t.Fatalf("reading %d, texts %v", s.reading, tt.Texts())
	}

	tt.SetSize(1100, 800)
	p.ForumViewer = prefs.ViewList
	tt.Frame()
	tt.Frame()
	if s.paned || tt.HasText("Choose a post to read") {
		t.Fatalf("paned with the viewer List: %v", tt.Texts())
	}
}

// The toolbars of the two panes: the list's keeps the title with the unread count, the filters, the time range and
// search; the reader's has its actions, which work from the pane, but no Back and no previous and next arrows.
func TestCommunityPaneToolbars(t *testing.T) {
	p := prefs.Default()
	s, f, tt := communing(t, &p)
	for _, text := range []string{"Community", "3 unread"} {
		if !tt.HasText(text) {
			t.Fatalf("no %q in the list pane: %v", text, tt.Texts())
		}
	}
	for _, label := range []string{"Time range: any time", "Search", "Show"} {
		if _, ok := tt.Find(label); !ok {
			t.Errorf("no %q in the list pane", label)
		}
	}
	tt.Key(0, ui.KeyJ)
	f.settle(t, tt)
	if s.reading != 20 {
		t.Fatalf("reading %d", s.reading)
	}
	for _, label := range []string{"Summarize", "Translate", "Star", "Open in browser", "Copy link", "More"} {
		if _, ok := tt.Find(label); !ok {
			t.Errorf("no %q in the reader pane: %v", label, tt.Texts())
		}
	}
	for _, label := range []string{"Previous", "Next"} {
		if _, ok := tt.Find(label); ok {
			t.Errorf("%q in the reader pane", label)
		}
	}
	if err := tt.Click("Star"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if !s.entries[s.index(20)].Starred {
		t.Fatalf("the pane's Star did not star the post: %v", f.patches[20])
	}
	if err := tt.Click("Open in browser"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if got := tt.OpenedURLs(); !slices.Equal(got, []string{"https://board.example/Third thread"}) {
		t.Fatalf("opened %v", got)
	}
	if s.reading != 20 {
		t.Fatalf("the pane closed the post: reading %d", s.reading)
	}
}

// The panes are for the Community only, and need a main column of 800: hiding the sidebar gives the window to them.
func TestCommunityPanesScope(t *testing.T) {
	p := prefs.Default()
	s, f, tt := communing(t, &p)
	pick(t, f, tt, "Articles")
	if s.paned {
		t.Fatal("panes in the Articles")
	}
	pick(t, f, tt, "Community")
	tt.SetSize(1000, 800) // a main column of 776
	tt.Frame()
	if s.paned {
		t.Fatal("panes in a main column of 776")
	}
	p.SidebarHidden = true
	tt.Frame()
	tt.Frame()
	if !s.paned {
		t.Fatal("no panes with the sidebar hidden in a window of 1000")
	}
}
