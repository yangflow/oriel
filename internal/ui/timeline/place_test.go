package timeline

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/prefs"
)

// kept is the preferences of the device as the tests keep them, saved the way the shell does.
type kept struct{ p prefs.Preferences }

func (k *kept) deps(f *fake) Deps {
	d := f.deps()
	d.Reading = func() prefs.Preferences { return k.p }
	d.Remember = func(server string, place prefs.Place) { k.p.Places = k.p.Places.Set(server, place) }
	return d
}

// open shows a new screen on the server, as after a restart.
func (k *kept) open(t *testing.T, f *fake, server string) (*Screen, *ui.Tester) {
	t.Helper()
	s := New("en", k.deps(f))
	tt := newTester(s.View, 1100, 800)
	s.Use(f, server)
	tt.Frame()
	f.settle(t, tt)
	return s, tt
}

func newKept() *kept { return &kept{p: prefs.Default()} }

func newPlaceFake() *fake {
	sources, entries := sample()
	return &fake{now: now, sources: sources, entries: entries, patches: map[int64][]client.EntryPatch{}}
}

func TestRestoreKindAndFilter(t *testing.T) {
	f := newPlaceFake()
	k := newKept()
	s, tt := k.open(t, f, "home.lan:7470")
	if err := tt.Click("Videos"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if err := tt.Click("Unread"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	want := prefs.Place{Kind: "video", Filter: prefs.FilterUnread}
	if got := k.p.Places.Get("home.lan:7470"); got != want {
		t.Fatalf("kept %+v, want %+v", got, want)
	}
	if s.scope.kind != "video" {
		t.Fatalf("scope %+v", s.scope)
	}

	again, _ := k.open(t, f, "home.lan:7470")
	if again.scope != (scope{kind: "video"}) || again.filter != unread || again.tab != indexOf(kinds, "video") {
		t.Fatalf("restored scope %+v, tab %d, filter %d", again.scope, again.tab, again.filter)
	}
	last := f.queries[len(f.queries)-1]
	if last.Kind != "video" || !last.Unread {
		t.Fatalf("first page asked with %+v", last)
	}
}

func TestRestoreSubscription(t *testing.T) {
	f := newPlaceFake()
	k := newKept()
	src := f.sources[0]
	k.p.Places = k.p.Places.Set("", prefs.Place{Kind: src.Kind, Source: src.ID, Filter: prefs.FilterStarred})
	s, _ := k.open(t, f, "")
	if s.scope != (scope{kind: src.Kind, source: src.ID}) || s.filter != starred || s.restoring {
		t.Fatalf("scope %+v, filter %d, restoring %v", s.scope, s.filter, s.restoring)
	}
}

func TestRestoreDeletedSubscription(t *testing.T) {
	f := newPlaceFake()
	k := newKept()
	k.p.Places = k.p.Places.Set("", prefs.Place{Kind: "article", Source: 9999, Filter: prefs.FilterUnread})
	s, _ := k.open(t, f, "")
	if s.scope != (scope{}) || s.tab != 0 || s.loadErr || s.loading {
		t.Fatalf("scope %+v, tab %d, error %v", s.scope, s.tab, s.loadErr)
	}
	if s.filter != unread || len(s.entries) == 0 {
		t.Fatalf("filter %d, %d entries", s.filter, len(s.entries))
	}
	if got := k.p.Places.Get(""); got != (prefs.Place{Filter: prefs.FilterUnread}) {
		t.Fatalf("the missing subscription was kept: %+v", got)
	}
}

func TestRestoreOtherServer(t *testing.T) {
	f := newPlaceFake()
	k := newKept()
	k.p.Places = k.p.Places.Set("home.lan:7470", prefs.Place{Kind: "video", Filter: prefs.FilterUnread})
	for _, server := range []string{"", "other.lan:7470"} {
		s, _ := k.open(t, f, server)
		if s.scope != (scope{}) || s.filter != everything {
			t.Fatalf("%q inherited scope %+v, filter %d", server, s.scope, s.filter)
		}
	}
	s, _ := k.open(t, f, "home.lan:7470")
	if s.scope.kind != "video" {
		t.Fatalf("scope %+v", s.scope)
	}
}

func TestRestoreRange(t *testing.T) {
	f := newPlaceFake()
	k := newKept()
	_, tt := k.open(t, f, "home.lan:7470")
	tt.Key(0, ui.KeyT)
	f.settle(t, tt)
	if got, want := k.p.Places.Get("home.lan:7470"), (prefs.Place{Range: prefs.RangeToday}); got != want {
		t.Fatalf("kept %+v, want %+v", got, want)
	}
	again, _ := k.open(t, f, "home.lan:7470")
	if again.span != today {
		t.Fatalf("restored span %d", again.span)
	}
	if last := f.queries[len(f.queries)-1]; last.Since == 0 {
		t.Fatalf("first page asked with %+v", last)
	}
	tt.Key(0, ui.KeyT)
	f.settle(t, tt)
	tt.Key(0, ui.KeyT)
	f.settle(t, tt)
	if got := k.p.Places.Get("home.lan:7470"); got != (prefs.Place{}) {
		t.Fatalf("kept %+v after any time", got)
	}
}
