package timeline

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/prefs"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// foldered is the sample with the folder "Reading" holding Go Blog and Talks, and "Spare" holding nothing.
func foldered(t *testing.T, language string, p *prefs.Preferences) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	sources, entries := sample()
	sources[0].FolderID = 7
	sources[1].FolderID = 7
	f := &fake{now: now, sources: sources, entries: entries, patches: map[int64][]client.EntryPatch{},
		folders: []client.Folder{{ID: 7, Name: "Reading"}, {ID: 8, Name: "Spare"}}}
	d := f.deps()
	d.Reading = func() prefs.Preferences { return *p }
	d.Change = func(n prefs.Preferences) { *p = n }
	s := New(language, d)
	tt := newTester(func(c *ui.Context) { s.View(c); theme.Toasts(c) }, 1100, 800)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	tt.Frame()
	return s, f, tt
}

var folderNames = []string{"Reading", "Spare", "Go Blog", "Talks", "Gopher", "Photos"}

// Under All the sidebar lists every folder, even an empty one, with its subscriptions under it, and the unfiled
// subscriptions after the folders.
func TestSidebarFoldersUnderAll(t *testing.T) {
	p := prefs.Default()
	_, _, tt := foldered(t, "en", &p)
	got := sourceOrder(t, tt, folderNames...)
	if want := []string{"Reading", "Go Blog", "Talks", "Spare", "Gopher", "Photos"}; !slices.Equal(got, want) {
		t.Fatalf("sidebar order %v, want %v", got, want)
	}
}

// Under a kind the sidebar lists the subscriptions of that kind by folder and hides a folder that has none of them.
func TestSidebarFoldersUnderKind(t *testing.T) {
	p := prefs.Default()
	_, f, tt := foldered(t, "en", &p)
	pick(t, f, tt, "Articles")
	got := sourceOrder(t, tt, folderNames...)
	if want := []string{"Reading", "Go Blog"}; !slices.Equal(got, want) {
		t.Fatalf("sidebar order %v, want %v", got, want)
	}
	pick(t, f, tt, "Social")
	got = sourceOrder(t, tt, folderNames...)
	if want := []string{"Gopher"}; !slices.Equal(got, want) {
		t.Fatalf("sidebar order %v, want %v", got, want)
	}
}

// A folder collapses to its row and opens again, and the choice is handed to the preferences to keep.
func TestSidebarFolderCollapse(t *testing.T) {
	p := prefs.Default()
	s, _, tt := foldered(t, "en", &p)
	if err := tt.Click("Collapse Reading"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !p.FoldedFolders.Has(s.server, 7) {
		t.Fatal("the collapse was not handed to the preferences")
	}
	got := sourceOrder(t, tt, folderNames...)
	if want := []string{"Reading", "Spare", "Gopher", "Photos"}; !slices.Equal(got, want) {
		t.Fatalf("sidebar order %v, want %v", got, want)
	}
	if err := tt.Click("Expand Reading"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if p.FoldedFolders.Has(s.server, 7) {
		t.Fatal("the folder is still kept collapsed")
	}
	got = sourceOrder(t, tt, folderNames...)
	if want := []string{"Reading", "Go Blog", "Talks", "Spare", "Gopher", "Photos"}; !slices.Equal(got, want) {
		t.Fatalf("sidebar order %v, want %v", got, want)
	}
}

// A collapsed folder still shows the subscription that is open, and a filter looks inside it.
func TestSidebarFolderCollapsedKeepsOpenSource(t *testing.T) {
	p := prefs.Default()
	p.FoldedFolders = p.FoldedFolders.Set("", 7, true)
	s, f, tt := foldered(t, "en", &p)
	if got := sourceOrder(t, tt, folderNames...); slices.Contains(got, "Go Blog") {
		t.Fatalf("a collapsed folder lists %v", got)
	}
	chooseSource(t, s, f, tt, "", 1)
	if got := sourceOrder(t, tt, folderNames...); !slices.Contains(got, "Go Blog") || slices.Contains(got, "Talks") {
		t.Fatalf("sidebar order %v", got)
	}
}

// A click on a folder reads it as one timeline: the entries of its subscriptions, asked for by the folder, with the
// kind that is chosen.
func TestFolderTimeline(t *testing.T) {
	p := prefs.Default()
	s, f, tt := foldered(t, "en", &p)
	if err := tt.Click("Reading"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	f.settle(t, tt)
	tt.Frame()
	if s.scope.folder != 7 || s.scope.source != 0 {
		t.Fatalf("scope %+v", s.scope)
	}
	last := f.queries[len(f.queries)-1]
	if last.Folder != 7 || last.Kind != "" {
		t.Fatalf("query %+v", last)
	}
	if !tt.HasText("Go 1.27 is released") || !tt.HasText("Profiling in practice") || tt.HasText("Harbor at dawn") {
		t.Fatalf("entries %v", tt.Texts())
	}

	// The unread count of a folder is for the scope: the kind narrows it.
	if got := s.unreadIn(scope{folder: 7}); got != 3 {
		t.Fatalf("unread in the folder %d", got)
	}
	if got := s.unreadIn(scope{kind: "article", folder: 7}); got != 2 {
		t.Fatalf("unread of articles in the folder %d", got)
	}
}

// The kind chosen above a folder is kept when a folder is chosen.
func TestFolderTimelineKeepsKind(t *testing.T) {
	p := prefs.Default()
	s, f, tt := foldered(t, "en", &p)
	pick(t, f, tt, "Articles")
	if err := tt.Click("Reading"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	f.settle(t, tt)
	tt.Frame()
	last := f.queries[len(f.queries)-1]
	if last.Folder != 7 || last.Kind != "article" || s.scope.kind != "article" {
		t.Fatalf("query %+v, scope %+v", last, s.scope)
	}
}

// A folder that is gone from the server takes the timeline back to All.
func TestFolderGoneLeavesTimeline(t *testing.T) {
	p := prefs.Default()
	s, f, tt := foldered(t, "en", &p)
	if err := tt.Click("Reading"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	f.settle(t, tt)
	f.folders = f.folders[1:]
	for i := range f.sources {
		f.sources[i].FolderID = 0
	}
	s.loadSources()
	f.settle(t, tt)
	f.settle(t, tt)
	tt.Frame()
	if s.scope.folder != 0 {
		t.Fatalf("scope %+v", s.scope)
	}
	if got := sourceOrder(t, tt, folderNames...); slices.Contains(got, "Reading") {
		t.Fatalf("sidebar order %v", got)
	}
}

// The place of a folder is remembered and found again, and a folder that is gone by then does not hold the timeline.
func TestFolderPlaceRestored(t *testing.T) {
	f := newPlaceFake()
	f.sources[0].FolderID = 7
	f.folders = []client.Folder{{ID: 7, Name: "Reading"}}
	k := newKept()
	_, tt := k.open(t, f, "")
	if err := tt.Click("Reading"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if got := k.p.Places.Get(""); got != (prefs.Place{Folder: 7}) {
		t.Fatalf("kept %+v", got)
	}
	again, _ := k.open(t, f, "")
	f.settle(t, tt)
	if again.scope != (scope{folder: 7}) || again.restoring {
		t.Fatalf("restored scope %+v, restoring %v", again.scope, again.restoring)
	}
	if last := f.queries[len(f.queries)-1]; last.Folder != 7 {
		t.Fatalf("first page asked with %+v", last)
	}

	f.folders = nil
	f.sources[0].FolderID = 0
	gone, _ := k.open(t, f, "")
	f.settle(t, tt)
	if gone.scope != (scope{}) || gone.restoring {
		t.Fatalf("a folder that is gone holds the scope %+v", gone.scope)
	}
}
