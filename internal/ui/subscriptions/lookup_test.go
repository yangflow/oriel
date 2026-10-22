package subscriptions

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

// pageAddress types a web page address and lets the pause pass, so the lookup runs and its answer shows.
func pageAddress(t *testing.T, f *fake, tt *ui.Tester, address string) {
	t.Helper()
	tt.SetSize(1000, 1400)
	tt.Type(address)
	tt.Frame()
	f.lateBy += lookAfter
	tt.Frame()
	f.settle(tt)
	tt.Frame()
}

func TestLookupFindsAFeed(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	f.found = []client.Candidate{{URL: "https://blog.example/rss.xml", Title: "The Blog", Origin: "alternate"}}
	tt.SetSize(1000, 1400)
	tt.Type("https://blog.example/")
	tt.Frame()
	if !tt.HasText(s.text.SearchingFeeds) || len(f.looked) != 0 {
		t.Fatalf("looked %v, texts %v", f.looked, tt.Texts())
	}
	f.lateBy += lookAfter
	tt.Frame()
	if !tt.HasText(s.text.SearchingFeeds) || len(f.queue) != 1 {
		t.Fatalf("not searching: %v", tt.Texts())
	}
	f.settle(tt)
	tt.Frame()
	if !slices.Equal(f.looked, []string{"https://blog.example/"}) || tt.HasText(s.text.SearchingFeeds) ||
		!tt.HasText("The Blog") || !tt.HasText("https://blog.example/rss.xml") {
		t.Fatalf("looked %v, texts %v", f.looked, tt.Texts())
	}
	if err := tt.Click("Add"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	want := client.NewSource{URL: "https://blog.example/rss.xml", Kind: "article"}
	if len(f.added) != 1 || f.added[0] != want {
		t.Fatalf("added %+v", f.added)
	}
}

func TestLookupPicksOneOfMany(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	f.found = []client.Candidate{
		{URL: "https://blog.example/rss.xml", Title: "Posts", Origin: "alternate"},
		{URL: "https://blog.example/comments.xml", Title: "Comments", Origin: "alternate"},
	}
	pageAddress(t, f, tt, "https://blog.example/")
	if !tt.HasText("Found 2 feeds. Choose one.") || !tt.HasText("Posts") || !tt.HasText("Comments") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("https://blog.example/comments.xml"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.add.look.chosen != 1 {
		t.Fatalf("chosen %d", s.add.look.chosen)
	}
	if err := tt.Click("Add"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if len(f.added) != 1 || f.added[0].URL != "https://blog.example/comments.xml" {
		t.Fatalf("added %+v", f.added)
	}
}

func TestLookupRuleFillsTheRoute(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	f.found = []client.Candidate{{RSSHubPath: "/zhihu/zhuanlan/abc", Kind: "article", Origin: "rule", Site: "zhihu",
		Label: "column", Subject: "abc"}}
	pageAddress(t, f, tt, "https://zhuanlan.zhihu.com/abc")
	for _, text := range []string{"Zhihu · column abc", "RSSHub route /zhihu/zhuanlan/abc"} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
	if err := tt.Click("Add"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if len(f.added) != 1 || f.added[0].RSSHubPath != "/zhihu/zhuanlan/abc" || f.added[0].URL != "" ||
		f.added[0].Kind != "article" {
		t.Fatalf("added %+v", f.added)
	}
}

func TestLookupFindsNothing(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	pageAddress(t, f, tt, "https://plain.example/")
	if !tt.HasText(s.text.NoneFound) {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("Add"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if len(f.added) != 1 || f.added[0].URL != "https://plain.example/" {
		t.Fatalf("added %+v", f.added)
	}
}

func TestLookupFailsQuietly(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	f.discoverErr = client.ErrUnreachable
	pageAddress(t, f, tt, "https://down.example/")
	if tt.HasText(s.text.SearchingFeeds) || tt.HasText(s.text.NoneFound) || tt.HasText(s.text.Unreachable) {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("Add"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if len(f.added) != 1 || f.added[0].URL != "https://down.example/" {
		t.Fatalf("added %+v", f.added)
	}
}

func TestLookupSkipsWhatAddressesItself(t *testing.T) {
	for _, address := range []string{"https://x.com/golang", "https://blog.example/feed.xml", "/telegram/channel/golang"} {
		s, f, tt := setup(t, "en")
		open(t, s, f, tt)
		pageAddress(t, f, tt, address)
		if len(f.looked) != 0 || tt.HasText(s.text.SearchingFeeds) {
			t.Fatalf("%q: looked %v, texts %v", address, f.looked, tt.Texts())
		}
	}
}

func TestLookupDropsAnAnswerForAnotherAddress(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	f.found = []client.Candidate{{URL: "https://blog.example/rss.xml", Title: "The Blog", Origin: "alternate"}}
	tt.SetSize(1000, 1400)
	tt.Type("https://blog.example/")
	tt.Frame()
	f.lateBy += lookAfter
	tt.Frame()
	tt.Type("x")
	tt.Frame()
	f.settle(tt)
	if s.add.look.state == lookDone || tt.HasText("The Blog") {
		t.Fatalf("state %d, texts %v", s.add.look.state, tt.Texts())
	}
}

func TestLookupAddDoesNotWaitOutThePause(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	f.found = []client.Candidate{{URL: "https://blog.example/rss.xml", Origin: "alternate"}}
	tt.Type("https://blog.example/")
	tt.Key(0, ui.KeyEnter)
	if len(f.added) != 0 || s.add.look.state != lookSearching {
		t.Fatalf("added %v, state %d", f.added, s.add.look.state)
	}
	tt.Key(0, ui.KeyEnter) // held while it searches
	f.settle(tt)
	if len(f.added) != 0 || s.add.look.state != lookDone {
		t.Fatalf("added %v, state %d", f.added, s.add.look.state)
	}
	tt.Key(0, ui.KeyEnter)
	f.settle(tt)
	if len(f.added) != 1 || f.added[0].URL != "https://blog.example/rss.xml" {
		t.Fatalf("added %+v", f.added)
	}
}

func TestLookupExpired(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	f.discoverErr = client.ErrUnauthorized
	pageAddress(t, f, tt, "https://blog.example/")
	if s.Open() || f.expired != 1 {
		t.Fatalf("open %v, expired %d", s.Open(), f.expired)
	}
}

func TestLookupChinese(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	open(t, s, f, tt)
	f.found = []client.Candidate{{RSSHubPath: "/bilibili/user/video/1", Kind: "video", Origin: "rule", Site: "bilibili",
		Label: "videos", Subject: "1"}}
	pageAddress(t, f, tt, "https://example.com/space/1")
	if !tt.HasText("哔哩哔哩 · 用户 1 的视频") {
		t.Fatalf("texts %v", tt.Texts())
	}
}
