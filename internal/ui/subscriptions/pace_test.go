package subscriptions

import "testing"

func TestEditPace(t *testing.T) {
	s, f, tt := setup(t, "en")
	tt.SetSize(1000, 1600)
	s.Edit(f.sources[0])
	tt.Frame()
	f.settle(tt)
	if s.edit.pace != 0 || !tt.HasText("Pace") || !tt.HasText("Off") || !tt.HasText(s.text.PaceHelp) {
		t.Fatalf("pace %d, texts %v", s.edit.pace, tt.Texts())
	}
	if err := tt.Click("Off"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Daily · 2 d"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p := f.patches[1]
	if len(p) != 1 || p[0].PaceSec == nil || *p[0].PaceSec != 2*24*3600 || p[0].Title != nil || p[0].KeepDays != nil {
		t.Fatalf("patches %+v", p)
	}

	// A pace the menu does not list shows as hours or days and can go back to off.
	src := f.sources[1]
	src.PaceSec, src.DailyCap = 5*3600, 20 // the cap shows a number, so only the pace menu shows "Off"
	s.Edit(src)
	tt.Frame()
	f.settle(tt)
	if !tt.HasText("5 h") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("5 h"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Off"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p = f.patches[2]
	if len(p) != 1 || p[0].PaceSec == nil || *p[0].PaceSec != 0 {
		t.Fatalf("patches %+v", p)
	}

	// Untouched, the pace is not sent.
	src.PaceSec = 3 * 3600
	s.Edit(src)
	tt.Frame()
	if !tt.HasText("Breaking · 3 h") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	if len(f.patches[2]) != 1 {
		t.Fatalf("saving without a change sent %v", f.patches[2])
	}
}

func TestEditPaceInChinese(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	tt.SetSize(1000, 1600)
	src := f.sources[0]
	src.PaceSec = 7 * 24 * 3600
	s.Edit(src)
	tt.Frame()
	f.settle(tt)
	if !tt.HasText("周更 · 7 天") || !tt.HasText("节奏") {
		t.Fatalf("texts %v", tt.Texts())
	}
}
