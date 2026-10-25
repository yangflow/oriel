package subscriptions

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestEditHideFromAll(t *testing.T) {
	s, f, tt := setup(t, "en")
	tt.SetSize(1000, 1600)
	s.Edit(f.sources[0])
	tt.Frame()
	f.settle(tt)
	if s.edit.hide || !tt.HasText("Hide from All") || !tt.HasText(s.text.HideHelp) {
		t.Fatalf("hide %v, texts %v", s.edit.hide, tt.Texts())
	}
	if err := tt.Click("Hide from All"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p := f.patches[1]
	if len(p) != 1 || p[0].HideFromAll == nil || !*p[0].HideFromAll || p[0].Title != nil || p[0].Paused != nil {
		t.Fatalf("patches %+v", p)
	}
	if len(f.changed) != 1 || !f.changed[0].HideFromAll {
		t.Fatalf("changed %+v", f.changed)
	}

	// Untouched, the switch is not sent; a hidden subscription opens with it on and can be shown again.
	src := f.changed[0]
	s.Edit(src)
	tt.Frame()
	if !s.edit.hide {
		t.Fatal("the switch is off for a hidden subscription")
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	if len(f.patches[1]) != 1 {
		t.Fatalf("saving without a change sent %v", f.patches[1])
	}
	s.Edit(src)
	tt.Frame()
	if err := tt.Click("Hide from All"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if p := f.patches[1]; len(p) != 2 || p[1].HideFromAll == nil || *p[1].HideFromAll {
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

func TestHideFromAllInChinese(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	tt.SetSize(1000, 1600)
	s.Edit(f.sources[0])
	tt.Frame()
	f.settle(tt)
	if !tt.HasText("不在全部里显示") || !tt.HasText("只在点进这个订阅时看到；检索和收藏里还在。") {
		t.Fatalf("texts %v", tt.Texts())
	}
}
