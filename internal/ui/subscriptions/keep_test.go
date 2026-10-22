package subscriptions

import "testing"

func TestEditKeep(t *testing.T) {
	s, f, tt := setup(t, "en")
	tt.SetSize(1000, 1400)
	s.Edit(f.sources[0])
	tt.Frame()
	f.settle(tt)
	if s.edit.keep != 0 || !tt.HasText("Follow settings · 1 month") || !tt.HasText(s.text.KeepHelp) {
		t.Fatalf("keep %d, texts %v", s.edit.keep, tt.Texts())
	}
	if err := tt.Click("Follow settings · 1 month"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("3 months"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p := f.patches[1]
	if len(p) != 1 || p[0].KeepDays == nil || *p[0].KeepDays != 90 || p[0].Title != nil {
		t.Fatalf("patches %+v", p)
	}

	// A number of days the menu does not list shows as it is and can go back to following the settings.
	src := f.sources[1]
	src.KeepDays = 45
	s.Edit(src)
	tt.Frame()
	f.settle(tt)
	if !tt.HasText("45 d") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("45 d"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Follow settings"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p = f.patches[2]
	if len(p) != 1 || p[0].KeepDays == nil || *p[0].KeepDays != 0 {
		t.Fatalf("patches %+v", p)
	}

	// Untouched, the keep period is not sent.
	src.KeepDays = -1
	s.Edit(src)
	tt.Frame()
	if !tt.HasText("Forever") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	if len(f.patches[2]) != 1 {
		t.Fatalf("saving without a change sent %v", f.patches[2])
	}
}

func TestEditKeepFollowsForeverSettings(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	f.settings.RetentionDays = 0
	tt.SetSize(1000, 1400)
	s.Edit(f.sources[0])
	tt.Frame()
	f.settle(tt)
	if !tt.HasText("跟随设置 · 永久") {
		t.Fatalf("texts %v", tt.Texts())
	}
}
