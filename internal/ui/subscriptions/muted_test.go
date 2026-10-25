package subscriptions

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

// focusMute tabs from the title to the mute keywords.
func focusMute(t *testing.T, tt *ui.Tester) {
	t.Helper()
	for range 14 {
		if tt.Focused("Mute keywords") {
			return
		}
		tt.Key(0, ui.KeyTab)
	}
	t.Fatal("the mute keywords never got the focus")
}

func TestEditMutedCount(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.hidden = 7
	src := f.sources[0]
	src.MuteKeywords = "sponsored"
	s.Edit(src)
	tt.Frame()
	if tt.HasText("7 hidden now.") {
		t.Fatal("the count shows before the server answered")
	}
	f.settle(tt)
	if !tt.HasText("7 hidden now.") || !slices.Equal(f.asked, []string{"sponsored"}) {
		t.Fatalf("asked %q, texts %v", f.asked, tt.Texts())
	}

	// Typing asks again, and only the last of quick changes is asked for; the old number stays meanwhile.
	f.hidden = 0
	focusMute(t, tt)
	tt.Type("x")
	tt.Type("y")
	if !tt.HasText("7 hidden now.") {
		t.Fatalf("the old count went: %v", tt.Texts())
	}
	f.settle(tt)
	if !tt.HasText("Nothing hidden now.") || len(f.asked) != 2 || f.asked[1] == "sponsored" {
		t.Fatalf("asked %q, texts %v", f.asked, tt.Texts())
	}

	// An empty box hides nothing and asks nothing; a failed question leaves no number.
	s.Edit(f.sources[1])
	tt.Frame()
	f.settle(tt)
	if tt.HasText("Nothing hidden now.") || len(f.asked) != 2 {
		t.Fatalf("no keywords: asked %q, texts %v", f.asked, tt.Texts())
	}
	f.hidden, f.hiddenErr = 7, &client.APIError{Status: 500, Code: "internal"}
	focusMute(t, tt)
	tt.Type("ad")
	f.settle(tt)
	if len(f.asked) != 3 || tt.HasText("Nothing hidden now.") || tt.HasText("7 hidden now.") {
		t.Fatalf("failed: asked %q, texts %v", f.asked, tt.Texts())
	}
}

func TestEditMutedCountChinese(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	f.hidden = 3
	src := f.sources[0]
	src.MuteKeywords = "广告"
	s.Edit(src)
	tt.Frame()
	f.settle(tt)
	if !tt.HasText("现在隐藏了 3 条。") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestEditMutedCountSessionEnds(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.hiddenErr = client.ErrUnauthorized
	src := f.sources[0]
	src.MuteKeywords = "ad"
	s.Edit(src)
	tt.Frame()
	f.settle(tt)
	if s.Open() || f.expired != 1 {
		t.Fatalf("open %v, expired %d", s.Open(), f.expired)
	}
}
