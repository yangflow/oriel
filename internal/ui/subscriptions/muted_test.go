package subscriptions

import (
	"slices"
	"strings"
	"testing"

	"github.com/yangflow/oriel/internal/client"
)

// The Mute keywords of the page say how many entries they hide, asked again as they are typed.
func TestPageMutedCount(t *testing.T) {
	s, f, _ := setup(t, "en")
	f.hidden = 7
	f.sources[0].MuteKeywords = "sponsored"
	s.OpenSource(f.sources[0])
	tt := pageTester(s)
	if tt.HasText("7 hidden now.") {
		t.Fatal("the count shows before the server answered")
	}
	f.settle(tt)
	if !tt.HasText("7 hidden now.") || !slices.Equal(f.asked, []string{"sponsored"}) {
		t.Fatalf("asked %q, texts %v", f.asked, tt.Texts())
	}

	// Typing asks again, and only the last of quick changes is asked for; the old number stays meanwhile.
	f.hidden = 0
	focusField(t, tt, s.text.Mute)
	tt.Type("x")
	tt.Type("y")
	if !tt.HasText("7 hidden now.") {
		t.Fatalf("the old count went: %v", tt.Texts())
	}
	f.settle(tt)
	if !tt.HasText("Nothing hidden now.") || len(f.asked) != 2 || f.asked[1] == "sponsored" {
		t.Fatalf("asked %q, texts %v", f.asked, tt.Texts())
	}
	if len(f.patches[1]) != 0 {
		t.Fatalf("the keywords were sent before Save keywords: %+v", f.patches)
	}
	mustClick(t, tt, s.text.SaveMute)
	f.settle(tt)
	if p := f.patches[1]; len(p) != 1 || p[0].MuteKeywords == nil || !strings.Contains(*p[0].MuteKeywords, "xy") || p[0].Title != nil {
		t.Fatalf("patches %+v", p)
	}
}

// An empty box hides nothing and asks nothing; a failed question leaves no number.
func TestPageMutedCountFails(t *testing.T) {
	s, f, tt := paged(t, "en", 1, nil)
	if tt.HasText("Nothing hidden now.") || len(f.asked) != 0 {
		t.Fatalf("no keywords: asked %q, texts %v", f.asked, tt.Texts())
	}
	f.hidden, f.hiddenErr = 7, &client.APIError{Status: 500, Code: "internal"}
	focusField(t, tt, s.text.Mute)
	tt.Type("ad")
	f.settle(tt)
	if len(f.asked) != 1 || tt.HasText("Nothing hidden now.") || tt.HasText("7 hidden now.") {
		t.Fatalf("failed: asked %q, texts %v", f.asked, tt.Texts())
	}
}

func TestPageMutedCountChinese(t *testing.T) {
	s, f, _ := setup(t, "zh-Hans")
	f.hidden = 3
	f.sources[0].MuteKeywords = "广告"
	s.OpenSource(f.sources[0])
	tt := pageTester(s)
	f.settle(tt)
	if !tt.HasText("现在隐藏了 3 条。") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestPageMutedCountSessionEnds(t *testing.T) {
	s, f, _ := setup(t, "en")
	f.hiddenErr = client.ErrUnauthorized
	f.sources[0].MuteKeywords = "ad"
	s.OpenSource(f.sources[0])
	tt := pageTester(s)
	f.settle(tt)
	if f.expired != 1 {
		t.Fatalf("expired %d", f.expired)
	}
}
