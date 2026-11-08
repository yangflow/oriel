package subscriptions

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The edit dialog has no Hide from All, which lives on the page; its Save leaves a hidden subscription hidden.
func TestEditHideFromAll(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.sources[0].HideFromAll = true
	s.Edit(f.sources[0])
	tt.Frame()
	f.settle(tt)
	if tt.HasText(s.text.HideLabel) || tt.HasText(s.text.HideHelp) {
		t.Fatalf("the dialog shows Hide from All: %v", tt.Texts())
	}
	tt.Type(" weekly")
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if p := f.patches[1]; len(p) != 1 || p[0].HideFromAll != nil || p[0].Title == nil {
		t.Fatalf("patches %+v", p)
	}
}

func TestHideFromAllMenu(t *testing.T) {
	s, f, _ := setup(t, "en")
	tt := ui.NewTester(func(c *ui.Context) {
		for _, src := range f.sources {
			ui.Text(c, Name(src)).ContextMenu(func(m *ui.Menu) { s.Menu(m, src) })
		}
		s.View(c)
	}, 1000, 760)
	tt.Frame()
	tt.RightClick("Go Blog")
	if menu := tt.Menu(); !slices.Contains(menu, "Hide from All") || slices.Contains(menu, "Show in All") {
		t.Fatalf("menu %q", menu)
	}
	if err := tt.ChooseMenuItem("Hide from All"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if p := f.patches[1]; len(p) != 1 || p[0].HideFromAll == nil || !*p[0].HideFromAll || len(f.changed) != 1 || !f.changed[0].HideFromAll {
		t.Fatalf("patches %+v, changed %+v", p, f.changed)
	}

	// The menu follows the subscription: once hidden it offers to show it again.
	f.sources[0].HideFromAll = true
	tt.Frame()
	tt.RightClick("Go Blog")
	if menu := tt.Menu(); !slices.Contains(menu, "Show in All") || slices.Contains(menu, "Hide from All") {
		t.Fatalf("menu %q", menu)
	}
	if err := tt.ChooseMenuItem("Show in All"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if p := f.patches[1]; len(p) != 2 || p[1].HideFromAll == nil || *p[1].HideFromAll {
		t.Fatalf("patches %+v", p)
	}
}

// The page's Hide from All switch in Chinese, with its help.
func TestHideFromAllInChinese(t *testing.T) {
	_, _, tt := paged(t, "zh-Hans", 0, nil)
	if !tt.HasText("不在全部里显示") || !tt.HasText("只在点进这个订阅时看到；检索和收藏里还在。") {
		t.Fatalf("texts %v", tt.Texts())
	}
}
