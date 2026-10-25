package timeline

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRemindChoices(t *testing.T) {
	s, _, _ := setup(t, "en")
	at := func(y int, m time.Month, d, h, min int) time.Time {
		return time.Date(y, m, d, h, min, 0, 0, time.Local)
	}
	for _, c := range []struct {
		name string
		now  time.Time
		want [4]time.Time
	}{
		{"morning", at(2026, time.October, 21, 10, 30), // a Wednesday
			[4]time.Time{at(2026, time.October, 21, 11, 30), at(2026, time.October, 21, 18, 0), at(2026, time.October, 22, 9, 0), at(2026, time.October, 26, 9, 0)}},
		{"night", at(2026, time.October, 21, 19, 0),
			[4]time.Time{at(2026, time.October, 21, 20, 0), at(2026, time.October, 22, 18, 0), at(2026, time.October, 22, 9, 0), at(2026, time.October, 26, 9, 0)}},
		{"monday", at(2026, time.October, 26, 8, 0),
			[4]time.Time{at(2026, time.October, 26, 9, 0), at(2026, time.October, 26, 18, 0), at(2026, time.October, 27, 9, 0), at(2026, time.November, 2, 9, 0)}},
		{"sunday night", at(2026, time.October, 25, 23, 30),
			[4]time.Time{at(2026, time.October, 26, 0, 30), at(2026, time.October, 26, 18, 0), at(2026, time.October, 26, 9, 0), at(2026, time.October, 26, 9, 0)}},
	} {
		got := s.remindChoices(c.now)
		for i, choice := range got {
			if !choice.at.Equal(c.want[i]) {
				t.Errorf("%s: choice %d (%s) = %v, want %v", c.name, i, choice.name, choice.at, c.want[i])
			}
		}
	}
}

func TestRemindFromTheMenu(t *testing.T) {
	for language, c := range map[string]struct{ later, hour, cancel string }{
		"en":      {"Remind me later", "In 1 hour", "Cancel reminder"},
		"zh-Hans": {"稍后提醒我", "1 小时后", "取消提醒"},
	} {
		t.Run(language, func(t *testing.T) {
			s, f, tt := setup(t, language)
			id := s.entries[0].ID
			if err := tt.RightClick(title(s.entries[0])); err != nil {
				t.Fatal(err)
			}
			if err := tt.ChooseMenuItem(c.later, c.cancel); err == nil {
				t.Fatal("cancelled a reminder that was not set")
			}
			tt.CloseMenu()
			if err := tt.RightClick(title(s.entries[0])); err != nil {
				t.Fatal(err)
			}
			if err := tt.ChooseMenuItem(c.later, c.hour); err != nil {
				t.Fatal(err)
			}
			f.settle(t, tt)
			patches := f.patches[id]
			want := f.now.Add(time.Hour).UnixMilli()
			if len(patches) != 1 || patches[0].RemindAt == nil || *patches[0].RemindAt != want || patches[0].IsRead == nil || !*patches[0].IsRead {
				t.Fatalf("patches %+v, want a reminder at %d that marks it read", patches, want)
			}
			if e := s.entries[0]; !e.IsRead || e.RemindAt != want {
				t.Fatalf("entry read %v, remind %d", e.IsRead, e.RemindAt)
			}
			if label := s.label(s.entries[0], f.now); !strings.Contains(label, s.remindWhen(want)) {
				t.Fatalf("label %q does not say when", label)
			}

			// The menu of an entry with a reminder can cancel it, and the entry stays read.
			if err := tt.RightClick(title(s.entries[0])); err != nil {
				t.Fatal(err)
			}
			if err := tt.ChooseMenuItem(c.later, c.cancel); err != nil {
				t.Fatal(err)
			}
			f.settle(t, tt)
			patches = f.patches[id]
			if len(patches) != 2 || patches[1].RemindAt == nil || *patches[1].RemindAt != 0 {
				t.Fatalf("patches %+v", patches)
			}
			if e := s.entries[0]; !e.IsRead || e.RemindAt != 0 {
				t.Fatalf("entry read %v, remind %d", e.IsRead, e.RemindAt)
			}
		})
	}
}

func TestRemindCancelledByOpeningAndByMarkingUnread(t *testing.T) {
	s, f, tt := setup(t, "en")
	at := f.now.Add(time.Hour).UnixMilli()
	s.entries[0].IsRead, s.entries[0].RemindAt = true, at
	s.entries[1].IsRead, s.entries[1].RemindAt = true, at
	opened, other := s.entries[0].ID, s.entries[1].ID

	s.markRead(s.entries[0])
	f.settle(t, tt)
	if p := f.patches[opened]; len(p) != 1 || p[0].RemindAt == nil || *p[0].RemindAt != 0 || s.entries[0].RemindAt != 0 {
		t.Fatalf("opening: patches %+v, remind %d", p, s.entries[0].RemindAt)
	}
	s.toggleRead(1)
	f.settle(t, tt)
	if s.entries[1].IsRead || s.entries[1].RemindAt != 0 || len(f.patches[other]) != 1 {
		t.Fatalf("marking unread: read %v, remind %d, patches %+v", s.entries[1].IsRead, s.entries[1].RemindAt, f.patches[other])
	}
}

func TestRemindFromThePalette(t *testing.T) {
	s, f, tt := setup(t, "en")
	s.selected = 0
	id := s.entries[0].ID
	has := func(name string) bool {
		return slices.ContainsFunc(s.paletteItems(), func(it paletteItem) bool { return it.name == name })
	}
	for _, want := range []string{"Remind me later: In 1 hour", "Remind me later: This evening", "Remind me later: Tomorrow morning", "Remind me later: Next week"} {
		if !has(want) {
			t.Errorf("palette has no %q", want)
		}
	}
	if has("Cancel reminder") {
		t.Error("the palette cancels a reminder that is not set")
	}
	for _, it := range s.paletteItems() {
		if it.name == "Remind me later: In 1 hour" {
			it.run()
		}
	}
	f.settle(t, tt)
	if p := f.patches[id]; len(p) != 1 || p[0].RemindAt == nil || s.entries[0].RemindAt == 0 {
		t.Fatalf("patches %+v", p)
	}
	if !has("Cancel reminder") {
		t.Error("the palette cannot cancel the reminder")
	}
	s.selected = -1
	if has("Remind me later: In 1 hour") {
		t.Error("the palette sets a reminder with no entry chosen")
	}
}
