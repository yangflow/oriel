package subscriptions

import "testing"

func TestEditOpenTarget(t *testing.T) {
	s, f, tt := setup(t, "en")
	tt.SetSize(1000, 1600)
	s.Edit(f.sources[0])
	tt.Frame()
	f.settle(tt)
	if !tt.HasText("Opens in") || !tt.HasText("Follow the kind") || !tt.HasText(s.text.OpenTargetHelp) {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("Follow the kind"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("In the browser"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p := f.patches[f.sources[0].ID]
	if len(p) != 1 || p[0].OpenTarget == nil || *p[0].OpenTarget != "browser" || p[0].Title != nil {
		t.Fatalf("patches %+v", p)
	}

	// A source that opens in the reader shows it, and untouched sends nothing.
	src := f.sources[1]
	src.OpenTarget = "reader"
	s.Edit(src)
	tt.Frame()
	f.settle(tt)
	if !tt.HasText("In the reader") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	for _, patch := range f.patches[src.ID] {
		if patch.OpenTarget != nil {
			t.Fatalf("patches %+v", f.patches[src.ID])
		}
	}
}

func TestEditOpenTargetChinese(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	tt.SetSize(1000, 1600)
	s.Edit(f.sources[0])
	tt.Frame()
	f.settle(tt)
	if !tt.HasText("点开时") || !tt.HasText("跟随形态") || !tt.HasText(s.text.OpenTargetHelp) {
		t.Fatalf("texts %v", tt.Texts())
	}
}
