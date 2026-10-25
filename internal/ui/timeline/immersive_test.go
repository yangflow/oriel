package timeline

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/prefs"
)

// reading opens the newest article of the sample in a screen that keeps its preferences in p.
func reading(t *testing.T, p *prefs.Preferences) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	s, f, tt := viewing(t, p)
	if err := tt.Click("Go Blog, Go 1.27 is released, 5 min ago, Unread"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if s.reading == 0 {
		t.Fatal("the article did not open")
	}
	return s, f, tt
}

func TestImmersiveEnterAndLeave(t *testing.T) {
	p := prefs.Default()
	s, _, tt := reading(t, &p)
	if _, ok := tt.Find("Kinds"); !ok {
		t.Fatal("no sidebar before immersive reading")
	}
	tt.Key(0, ui.KeyZ)
	tt.Frame()
	if !s.immersive || !slices.Contains(tt.Announcements(), "Immersive reading on") {
		t.Fatalf("Z: immersive %v, announcements %v", s.immersive, tt.Announcements())
	}
	if _, ok := tt.Find("Kinds"); ok {
		t.Fatal("the sidebar shows in immersive reading")
	}
	if p.SidebarHidden {
		t.Fatal("immersive reading changed the sidebar preference")
	}
	if _, ok := tt.Find("Exit immersive reading"); !ok {
		t.Fatal("no exit button in the toolbar")
	}
	tt.Key(0, ui.KeyZ)
	tt.Frame()
	if s.immersive || !slices.Contains(tt.Announcements(), "Immersive reading off") {
		t.Fatalf("Z again: immersive %v, announcements %v", s.immersive, tt.Announcements())
	}
	if _, ok := tt.Find("Kinds"); !ok {
		t.Fatal("the sidebar did not come back")
	}
	if _, ok := tt.Find("Exit immersive reading"); ok {
		t.Fatal("the exit button stays outside immersive reading")
	}
}

func TestImmersiveKeepsStepping(t *testing.T) {
	p := prefs.Default()
	s, f, tt := reading(t, &p)
	tt.Key(0, ui.KeyZ)
	tt.Frame()
	first := s.reading
	tt.Key(0, ui.KeyJ)
	f.settle(t, tt)
	if s.reading == first || !s.immersive {
		t.Fatalf("J: reading %d (was %d), immersive %v", s.reading, first, s.immersive)
	}
	tt.Key(0, ui.KeyK)
	f.settle(t, tt)
	if s.reading != first || !s.immersive {
		t.Fatalf("K: reading %d (want %d), immersive %v", s.reading, first, s.immersive)
	}
}

func TestImmersiveEscape(t *testing.T) {
	p := prefs.Default()
	s, _, tt := reading(t, &p)
	tt.Key(0, ui.KeyZ)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.immersive || s.reading == 0 {
		t.Fatalf("the first Escape: immersive %v, reading %d", s.immersive, s.reading)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.reading != 0 {
		t.Fatal("the second Escape does not go back to the list")
	}
}

func TestImmersiveBackslashLeaves(t *testing.T) {
	p := prefs.Default()
	s, _, tt := reading(t, &p)
	tt.Key(0, ui.KeyZ)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyBackslash)
	tt.Frame()
	if s.immersive || p.SidebarHidden {
		t.Fatalf("Cmd+\\: immersive %v, sidebar hidden %v", s.immersive, p.SidebarHidden)
	}
	if _, ok := tt.Find("Kinds"); !ok {
		t.Fatal("the sidebar did not come back")
	}
	tt.Key(ui.Cmd, ui.KeyBackslash)
	tt.Frame()
	if !p.SidebarHidden {
		t.Fatal("Cmd+\\ outside immersive reading no longer hides the sidebar")
	}
}

func TestImmersiveKeepsHiddenSidebarPreference(t *testing.T) {
	p := prefs.Default()
	p.SidebarHidden = true
	s, _, tt := reading(t, &p)
	tt.Key(0, ui.KeyZ)
	tt.Frame()
	tt.Key(0, ui.KeyZ)
	tt.Frame()
	if s.immersive || !p.SidebarHidden {
		t.Fatalf("immersive %v, sidebar hidden %v", s.immersive, p.SidebarHidden)
	}
	if _, ok := tt.Find("Kinds"); ok {
		t.Fatal("the sidebar shows although the preference hides it")
	}
}

func TestImmersiveNotInTheList(t *testing.T) {
	p := prefs.Default()
	s, _, tt := viewing(t, &p)
	tt.Key(0, ui.KeyZ)
	tt.Frame()
	if s.immersive {
		t.Fatal("Z started immersive reading in the list")
	}
	s.Run(Immersive)
	if s.immersive {
		t.Fatal("the menu item started immersive reading in the list")
	}
	openPalette(t, s, tt)
	if slices.Contains(shownNames(s), "Immersive reading") {
		t.Fatal("the palette offers immersive reading in the list")
	}
}

func TestImmersiveEndsWithTheReader(t *testing.T) {
	p := prefs.Default()
	s, _, tt := reading(t, &p)
	tt.Key(0, ui.KeyZ)
	tt.Frame()
	if err := tt.Click("Exit immersive reading"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.immersive || s.reading == 0 {
		t.Fatalf("the exit button: immersive %v, reading %d", s.immersive, s.reading)
	}
	tt.Key(0, ui.KeyZ)
	tt.Frame()
	s.back()
	tt.Frame()
	if s.immersive {
		t.Fatal("immersive reading outlives the reader")
	}
}

func TestImmersiveMenuAndPalette(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		t.Run(language, func(t *testing.T) {
			p := prefs.Default()
			s, _, tt := reading(t, &p)
			s.SetLanguage(language)
			tt.Frame()
			_, items := TimelineMenu(language)
			if i := slices.IndexFunc(items, func(it MenuItem) bool { return it.Command == Immersive }); i < 0 || items[i].Label != s.text.KeyImmersive {
				t.Fatalf("the Timeline menu is %v", items)
			}
			openPalette(t, s, tt)
			if !slices.Contains(shownNames(s), s.text.KeyImmersive) {
				t.Fatalf("palette %v", shownNames(s))
			}
			tt.Key(0, ui.KeyEscape)
			tt.Frame()
			s.Run(Immersive)
			tt.Frame()
			if !s.immersive {
				t.Fatal("the menu item does not start it")
			}
			openPalette(t, s, tt)
			if !slices.Contains(shownNames(s), s.text.PaletteExitImmersive) || slices.Contains(shownNames(s), s.text.KeyImmersive) {
				t.Fatalf("palette %v", shownNames(s))
			}
		})
	}
}

func TestImmersiveKeySheet(t *testing.T) {
	p := prefs.Default()
	s, _, tt := reading(t, &p)
	s.ShowKeys()
	tt.Frame()
	for _, name := range []string{"Immersive reading", "Leave immersive reading"} {
		if !tt.HasText(name) {
			t.Fatalf("no %q in %v", name, tt.Texts())
		}
	}
}
