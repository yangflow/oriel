package timeline

import (
	"testing"

	"github.com/yangflow/oriel/internal/client"
)

func TestHiddenSourceStaysOutOfTheKindCountsAndIsMarked(t *testing.T) {
	s, f, tt := setup(t, "en")
	if s.unreadIn(scope{kind: "image"}) != 1 {
		t.Fatalf("image unread = %d", s.unreadIn(scope{kind: "image"}))
	}
	if _, ok := tt.Find("Hidden from All"); ok {
		t.Fatal("a source that shows in All is marked")
	}

	if err := tt.RightClick("Photos"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Hide from All"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if !s.sources[3].HideFromAll {
		t.Fatalf("not hidden: %+v", s.sources[3])
	}
	// The marker and the screen reader name of the row say it; the row's own number stays.
	if _, ok := tt.Find("Hidden from All"); !ok {
		t.Fatalf("no marker: %v", tt.Texts())
	}
	if _, ok := tt.Find("Photos, hidden from All"); !ok {
		t.Fatalf("the row does not say it: %v", tt.Texts())
	}
	if got := s.unreadIn(scope{kind: "image"}); got != 0 {
		t.Errorf("image unread = %d, want 0", got)
	}
	if got := s.unreadIn(scope{source: 4}); got != 1 {
		t.Errorf("the source's own unread = %d, want 1", got)
	}

	// Reading one of its entries (in Starred, say) leaves the total of All alone.
	total := s.unreadTotal
	read := true
	s.setRow(s.index(5), client.EntryPatch{IsRead: &read})
	if s.unreadTotal != total {
		t.Errorf("total %d -> %d", total, s.unreadTotal)
	}

	if err := tt.RightClick("Photos"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Show in All"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if _, ok := tt.Find("Hidden from All"); ok || s.sources[3].HideFromAll {
		t.Fatal("still hidden")
	}
}

func TestHiddenSourceInChinese(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	f.sources[3].HideFromAll = true
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	if _, ok := tt.Find("Photos，不在全部里显示"); !ok {
		t.Fatalf("texts %v", tt.Texts())
	}
	if _, ok := tt.Find("不在全部里显示"); !ok {
		t.Fatalf("no marker: %v", tt.Texts())
	}
}
