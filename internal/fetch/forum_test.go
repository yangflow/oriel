package fetch

import (
	"testing"

	"github.com/yangflow/oriel/internal/store"
)

func TestForumFeed(t *testing.T) {
	for _, c := range []struct {
		address, route string
		want           bool
	}{
		{"https://www.v2ex.com/index.xml", "", true},
		{"https://v2ex.com/feed/tab/tech.xml", "", true},
		{"https://linux.do/latest.rss", "", true},
		{"https://meta.discourse.org/latest.rss", "", true},
		{"https://forum.example/c/general/4.rss", "", true},
		{"https://forum.example/tag/go.rss", "", true},
		{"https://forum.example/t/topic/12.rss", "", true},
		{"https://blog.example/feed.xml", "", false},
		{"https://blog.example/latest.xml", "", false},
		{"https://blog.example/news/a.rss", "", false},
		{"https://notv2ex.com/feed", "", false},
		{"", "/v2ex/topics/hot", true},
		{"", "/linuxdo/latest", true},
		{"", "/discourse/meta.discourse.org", true},
		{"", "/github/issue/golang/go", false},
		{"not a url", "", false},
	} {
		if got := ForumFeed(c.address, c.route); got != c.want {
			t.Errorf("%q %q: %v, want %v", c.address, c.route, got, c.want)
		}
	}
}

func TestSuggestKindFor(t *testing.T) {
	video := store.FeedEntry{DurationSec: 600}
	for _, c := range []struct {
		name    string
		src     store.FeedSource
		entries []store.FeedEntry
		want    string
	}{
		{"v2ex", store.FeedSource{URL: "https://www.v2ex.com/index.xml", Kind: "article"}, nil, "forum"},
		{"linux.do", store.FeedSource{URL: "https://linux.do/latest.rss", Kind: "social"}, nil, "forum"},
		{"rsshub route", store.FeedSource{RSSHubPath: "/v2ex/topics/hot", Kind: "article"}, nil, "forum"},
		{"already forum", store.FeedSource{URL: "https://linux.do/latest.rss", Kind: "forum"}, nil, ""},
		{"falls back to the entries", store.FeedSource{URL: "https://blog.example/feed", Kind: "article"},
			[]store.FeedEntry{video, video, video}, "video"},
		{"nothing to suggest", store.FeedSource{URL: "https://blog.example/feed", Kind: "article"}, nil, ""},
	} {
		if got := SuggestKindFor(c.src, c.entries); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
