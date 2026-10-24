package timeline

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

// openPalette presses the shortcut and returns once the palette has its field.
func openPalette(t *testing.T, s *Screen, tt *ui.Tester) {
	t.Helper()
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	if !s.pal.open {
		t.Fatal("the shortcut does not open the palette")
	}
}

// shownNames are the names of the rows the palette shows now.
func shownNames(s *Screen) []string {
	var names []string
	for _, it := range paletteShown(s.paletteItems(), s.pal.query) {
		names = append(names, it.name)
	}
	return names
}

func TestPaletteRank(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		want        int
	}{
		{"Go Blog", "", 0},
		{"Go Blog", "go", 0},
		{"Go Blog", "GO B", 0},
		{"Go Blog", "gob", 0},
		{"Go Blog", "blog", 1},
		{"Import and export", "exp", 1},
		{"Gopher", "oph", 2},
		{"Go Blog", "gbg", 3},
		{"Go Blog", "xyz", -1},
		{"前往设置", "设置", 2},
		{"Appearance", "apx", -1},
	} {
		if got := paletteRank(tc.name, tc.query); got != tc.want {
			t.Errorf("paletteRank(%q, %q) = %d, want %d", tc.name, tc.query, got, tc.want)
		}
	}
}

func TestPaletteOpens(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		t.Run(language, func(t *testing.T) {
			s, f, tt := setup(t, language)
			openPalette(t, s, tt)
			if !tt.Focused(s.text.PaletteGoTo) {
				t.Fatalf("the field has no focus: %v", tt.Texts())
			}
			for _, text := range []string{s.text.PaletteGoTo} {
				if _, ok := tt.Find(text); !ok {
					t.Errorf("no %q", text)
				}
			}
			tt.Key(ui.Cmd, ui.KeyK)
			tt.Frame()
			if s.pal.open {
				t.Fatal("the shortcut does not close it")
			}
			openPalette(t, s, tt)
			tt.Key(0, ui.KeyEscape)
			tt.Frame()
			if s.pal.open {
				t.Fatal("Escape does not close it")
			}
			if len(f.queries) != 1 {
				t.Errorf("queries %v", f.queries)
			}
		})
	}
}

func TestPaletteFilter(t *testing.T) {
	s, f, tt := setup(t, "en")
	openPalette(t, s, tt)
	if got := shownNames(s); got[0] != "Mark all in “All” as read" || !slices.Contains(got, "Go Blog") || len(got) > paletteMax {
		t.Fatalf("empty query shows %v", got)
	}
	tt.Type("go")
	tt.Frame()
	got := shownNames(s)
	if len(got) < 2 || got[0] != "Go Blog" || got[1] != "Gopher" {
		t.Fatalf("go shows %v", got)
	}
	tt.Type(" b")
	tt.Frame()
	if got := shownNames(s); len(got) == 0 || got[0] != "Go Blog" {
		t.Fatalf("\"go b\" shows %v", got)
	}
	if len(f.queries) != 1 || len(f.queue) != 0 {
		t.Fatalf("typing asked the server: %v, %d queued", f.queries, len(f.queue))
	}
}

func TestPaletteNone(t *testing.T) {
	for language, none := range map[string]string{"en": "No matches", "zh-Hans": "没有匹配"} {
		t.Run(language, func(t *testing.T) {
			s, f, tt := setup(t, language)
			openPalette(t, s, tt)
			tt.Type("qqqq")
			tt.Frame()
			if !tt.HasText(none) {
				t.Fatalf("no %q in %v", none, tt.Texts())
			}
			tt.Key(0, ui.KeyEnter)
			tt.Frame()
			if !s.pal.open || len(f.queries) != 1 {
				t.Fatal("Enter with no match did something")
			}
			f.now = f.now.Add(time.Second)
			tt.Advance(time.Second)
			tt.Frame()
			if !slices.Contains(tt.Announcements(), none) {
				t.Errorf("announcements %v", tt.Announcements())
			}
		})
	}
}

func TestPaletteEnter(t *testing.T) {
	s, f, tt := setup(t, "en")
	openPalette(t, s, tt)
	tt.Type("talks")
	tt.Key(0, ui.KeyEnter)
	f.settle(t, tt)
	if s.pal.open || s.scope.source != 2 {
		t.Fatalf("open %v, scope %+v", s.pal.open, s.scope)
	}
	if q := f.queries[len(f.queries)-1]; q.Source != 2 {
		t.Fatalf("last query %+v", q)
	}

	openPalette(t, s, tt)
	if s.pal.query != "" {
		t.Fatalf("the query stayed: %q", s.pal.query)
	}
	tt.Type("videos")
	tt.Key(0, ui.KeyEnter)
	f.settle(t, tt)
	if s.scope.kind != "video" || s.scope.source != 0 {
		t.Fatalf("scope %+v", s.scope)
	}
}

func TestPaletteClick(t *testing.T) {
	s, f, tt := setup(t, "en")
	openPalette(t, s, tt)
	tt.Type("photos")
	tt.Frame()
	label := s.paletteLabel(paletteShown(s.paletteItems(), "photos")[0])
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if s.pal.open || s.scope.source != 4 {
		t.Fatalf("open %v, scope %+v", s.pal.open, s.scope)
	}
}

func TestPaletteArrows(t *testing.T) {
	s, _, tt := setup(t, "en")
	openPalette(t, s, tt)
	n := len(shownNames(s))
	tt.Key(0, ui.KeyUp)
	if s.pal.hl != n-1 {
		t.Fatalf("Up from the first row goes to %d of %d", s.pal.hl, n)
	}
	tt.Key(0, ui.KeyDown)
	if s.pal.hl != 0 {
		t.Fatalf("Down from the last row goes to %d", s.pal.hl)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	if s.pal.hl != 2 {
		t.Fatalf("highlight %d", s.pal.hl)
	}
	tt.Type("g")
	tt.Frame()
	if s.pal.hl != 0 {
		t.Fatalf("typing keeps the highlight at %d", s.pal.hl)
	}
}

func TestPaletteActions(t *testing.T) {
	run := func(t *testing.T, s *Screen, tt *ui.Tester, query string) {
		t.Helper()
		openPalette(t, s, tt)
		tt.Type(query)
		tt.Key(0, ui.KeyEnter)
		tt.Frame()
	}
	t.Run("mark all", func(t *testing.T) {
		s, f, tt := setup(t, "en")
		run(t, s, tt, "mark all")
		f.settle(t, tt)
		if len(f.marked) != 1 {
			t.Fatalf("marked %v", f.marked)
		}
	})
	t.Run("add", func(t *testing.T) {
		s, _, tt := setup(t, "en")
		run(t, s, tt, s.text.AddSource)
		if s.pal.open || !s.subs.Open() {
			t.Fatalf("palette %v, subscriptions %v", s.pal.open, s.subs.Open())
		}
	})
	t.Run("health", func(t *testing.T) {
		s, f, tt := setup(t, "en")
		f.health = client.Health{WindowDays: 30, Sources: []client.SourceHealth{{ID: 1, Title: "Old", State: client.HealthDead,
			Cause: client.HealthCauseSilent, Days: 120}}}
		run(t, s, tt, "subscription health")
		f.settle(t, tt)
		if s.pal.open || !s.subs.Open() {
			t.Fatalf("palette %v, subscriptions %v", s.pal.open, s.subs.Open())
		}
	})
	t.Run("manage", func(t *testing.T) {
		s, f, tt := setup(t, "en")
		run(t, s, tt, "manage subscriptions")
		f.settle(t, tt)
		if s.pal.open || !s.subs.Open() || !s.subs.HasSources() {
			t.Fatalf("palette %v, subscriptions %v", s.pal.open, s.subs.Open())
		}
	})
	t.Run("no manage without subscriptions", func(t *testing.T) {
		s, _, tt := setup(t, "en")
		s.sources = nil
		openPalette(t, s, tt)
		if s.HasSources() || slices.Contains(tt.Texts(), s.text.PaletteManage) {
			t.Fatal("the palette offers to manage no subscriptions")
		}
	})
	t.Run("unread", func(t *testing.T) {
		s, f, tt := setup(t, "en")
		run(t, s, tt, "unread only")
		f.settle(t, tt)
		if s.filter != unread {
			t.Fatalf("filter %d", s.filter)
		}
		run(t, s, tt, "show all")
		f.settle(t, tt)
		if s.filter == unread {
			t.Fatal("still unread only")
		}
	})
	t.Run("settings", func(t *testing.T) {
		s, f, tt := setup(t, "en")
		run(t, s, tt, "open settings")
		if f.settings != 1 {
			t.Fatalf("settings opened %d times", f.settings)
		}
	})
	t.Run("keys", func(t *testing.T) {
		s, _, tt := setup(t, "en")
		run(t, s, tt, "keyboard")
		if !s.keys || s.pal.open {
			t.Fatalf("keys %v, palette %v", s.keys, s.pal.open)
		}
	})
	t.Run("pages", func(t *testing.T) {
		s, f, tt := setup(t, "en")
		for _, tc := range []struct{ query, page string }{{"appearance", "appearance"}, {"fetching", "fetching"},
			{"import", "opml"}, {"ai", "ai"}, {"devices", "devices"}, {"activity", "activity"}} {
			run(t, s, tt, tc.query)
			if got := f.pages; len(got) == 0 || got[len(got)-1] != tc.page {
				t.Fatalf("%q opened %v", tc.query, got)
			}
		}
	})
	t.Run("zh-Hans", func(t *testing.T) {
		s, f, tt := setup(t, "zh-Hans")
		run(t, s, tt, "设备")
		if len(f.pages) != 1 || f.pages[0] != "devices" {
			t.Fatalf("pages %v", f.pages)
		}
	})
}

func TestPaletteUnavailable(t *testing.T) {
	s, _, tt := setup(t, "en")
	s.ShowKeys()
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	if s.pal.open {
		t.Fatal("the palette opens over the key sheet")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	s.subs.Add()
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	if s.pal.open {
		t.Fatal("the palette opens over the subscriptions dialog")
	}
}

func TestPaletteKeySheet(t *testing.T) {
	for language, title := range map[string]string{"en": "Command palette", "zh-Hans": "命令面板"} {
		t.Run(language, func(t *testing.T) {
			s, _, tt := setup(t, language)
			s.ShowKeys()
			tt.Frame()
			if !tt.HasText(title) || !tt.HasText("K") {
				t.Fatalf("the key sheet lacks the palette: %v", tt.Texts())
			}
		})
	}
}

// TestPaletteFarHighlight wraps the highlight to the last row, far from the rows built at first: the row is built and
// shown when it is the one the field points assistive technology at (ActiveDescendant is called while it is built).
func TestPaletteFarHighlight(t *testing.T) {
	s, _, tt := setup(t, "en")
	openPalette(t, s, tt)
	shown := paletteShown(s.paletteItems(), "")
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if last := len(shown) - 1; s.pal.hl != last || !tt.HasText(shown[last].name) {
		t.Fatalf("highlight %d of %d, texts %v", s.pal.hl, len(shown), tt.Texts())
	}
}
