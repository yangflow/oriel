package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"reflect"
	"sync/atomic"
	"testing"
)

const rssBody = `<rss version="2.0"><channel><title>Notes</title><link>https://x.example/</link></channel></rss>`

// discoverer reaches httptest servers on the loopback address, which the real one refuses.
func discoverer() *Discoverer {
	d := NewDiscoverer("test")
	d.client = guardedClient(func(netip.AddrPort) bool { return false })
	return d
}

func pages(t *testing.T, routes map[string]string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if body == rssBody {
			w.Header().Set("Content-Type", "application/rss+xml")
		} else {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestDiscoverAlternates(t *testing.T) {
	srv, _ := pages(t, map[string]string{"/blog/": `<html><head>
		<link rel="alternate" type="application/rss+xml" title="Posts" href="/blog/rss.xml">
		<link rel="alternate" type="application/atom+xml" href="atom.xml">
		<link rel="alternate" type="application/rss+xml" href="/blog/rss.xml">
		<link rel="alternate" type="text/html" href="/fr/">
		<link rel="stylesheet" type="application/rss+xml" href="/not-a-feed">
		<link rel="alternate" type="application/feed+json" title="JSON" href="https://other.example/feed.json">
		</head></html>`, "/feed": rssBody})
	got, err := discoverer().Discover(context.Background(), srv.URL+"/blog/")
	if err != nil {
		t.Fatal(err)
	}
	want := []Candidate{
		{URL: srv.URL + "/blog/rss.xml", Title: "Posts", Origin: "alternate"},
		{URL: srv.URL + "/blog/atom.xml", Origin: "alternate"},
		{URL: "https://other.example/feed.json", Title: "JSON", Origin: "alternate"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestDiscoverTheAddressIsAFeed(t *testing.T) {
	srv, hits := pages(t, map[string]string{"/feed.xml": rssBody})
	got, err := discoverer().Discover(context.Background(), srv.URL+"/feed.xml")
	if err != nil {
		t.Fatal(err)
	}
	want := []Candidate{{URL: srv.URL + "/feed.xml", Title: "Notes", Origin: "feed"}}
	if !reflect.DeepEqual(got, want) || hits.Load() != 1 {
		t.Errorf("got %+v after %d requests, want %+v after 1", got, hits.Load(), want)
	}
}

func TestDiscoverCommonPaths(t *testing.T) {
	srv, _ := pages(t, map[string]string{
		"/about":     `<html><head><title>About</title></head></html>`,
		"/rss":       rssBody,
		"/index.xml": rssBody,
		"/atom.xml":  `<html>not a feed</html>`,
	})
	got, err := discoverer().Discover(context.Background(), srv.URL+"/about")
	if err != nil {
		t.Fatal(err)
	}
	want := []Candidate{
		{URL: srv.URL + "/rss", Title: "Notes", Origin: "common"},
		{URL: srv.URL + "/index.xml", Title: "Notes", Origin: "common"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestDiscoverCommonPathsWhenThePageIsGone(t *testing.T) {
	srv, _ := pages(t, map[string]string{"/feed": rssBody})
	got, err := discoverer().Discover(context.Background(), srv.URL+"/missing/page")
	if err != nil || len(got) != 1 || got[0].URL != srv.URL+"/feed" {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestDiscoverNothing(t *testing.T) {
	srv, _ := pages(t, map[string]string{"/": `<html><head><title>Hi</title></head></html>`})
	got, err := discoverer().Discover(context.Background(), srv.URL+"/")
	if err != nil || len(got) != 0 {
		t.Errorf("got %+v, %v, want none", got, err)
	}
}

func TestDiscoverPageUnavailable(t *testing.T) {
	srv, _ := pages(t, nil)
	_, err := discoverer().Discover(context.Background(), srv.URL+"/nothing")
	if !errors.Is(err, ErrPageUnavailable) {
		t.Errorf("error %v, want ErrPageUnavailable", err)
	}
}

func TestDiscoverRefusesNonPublicAddresses(t *testing.T) {
	srv, hits := pages(t, map[string]string{"/": `<link rel="alternate" type="application/rss+xml" href="/rss">`})
	// The real discoverer: the loopback address of the test server is refused before any request is served.
	_, err := NewDiscoverer("test").Discover(context.Background(), srv.URL+"/")
	if !errors.Is(err, ErrBlockedAddress) || hits.Load() != 0 {
		t.Errorf("error %v after %d requests, want ErrBlockedAddress after none", err, hits.Load())
	}
}

func TestDiscoverInvalidAddress(t *testing.T) {
	for _, address := range []string{"", "example.org/blog", "ftp://example.org/feed", "http://", "/bilibili/user/video/1"} {
		if _, err := discoverer().Discover(context.Background(), address); !errors.Is(err, ErrInvalidAddress) {
			t.Errorf("%q: error %v, want ErrInvalidAddress", address, err)
		}
	}
}

func TestDiscoverRules(t *testing.T) {
	rule := func(route, site, label, kind, subject string) Candidate {
		return Candidate{RSSHubPath: route, Kind: kind, Origin: "rule", Site: site, Label: label, Subject: subject}
	}
	for _, c := range []struct {
		page string
		want []Candidate
	}{
		{"https://www.youtube.com/channel/UCabc_123-x", []Candidate{{URL: "https://www.youtube.com/feeds/videos.xml?channel_id=UCabc_123-x",
			Kind: "video", Origin: "rule", Site: "youtube", Label: "channel", Subject: "UCabc_123-x"}}},
		{"https://m.youtube.com/@golang/videos", []Candidate{rule("/youtube/user/@golang", "youtube", "channel", "video", "@golang")}},
		{"https://www.youtube.com/playlist?list=PL1", []Candidate{{URL: "https://www.youtube.com/feeds/videos.xml?playlist_id=PL1",
			Kind: "video", Origin: "rule", Site: "youtube", Label: "playlist", Subject: "PL1"}}},
		{"https://space.bilibili.com/2267573/video", []Candidate{
			rule("/bilibili/user/video/2267573", "bilibili", "videos", "video", "2267573"),
			rule("/bilibili/user/dynamic/2267573", "bilibili", "dynamic", "social", "2267573")}},
		{"https://weibo.com/u/1669879400", []Candidate{rule("/weibo/user/1669879400", "weibo", "posts", "social", "1669879400")}},
		{"https://m.weibo.cn/profile/1669879400", []Candidate{rule("/weibo/user/1669879400", "weibo", "posts", "social", "1669879400")}},
		{"https://www.zhihu.com/people/someone/posts", []Candidate{
			rule("/zhihu/people/activities/someone", "zhihu", "activities", "article", "someone"),
			rule("/zhihu/people/answers/someone", "zhihu", "answers", "article", "someone")}},
		{"https://zhuanlan.zhihu.com/c_123", []Candidate{rule("/zhihu/zhuanlan/c_123", "zhihu", "column", "article", "c_123")}},
		{"https://www.zhihu.com/question/19550225", []Candidate{rule("/zhihu/question/19550225", "zhihu", "question", "article", "19550225")}},
		{"https://x.com/golang", []Candidate{rule("/twitter/user/golang", "x", "posts", "social", "golang")}},
		{"https://twitter.com/golang/status/1", []Candidate{rule("/twitter/user/golang", "x", "posts", "social", "golang")}},
	} {
		u, _ := url.Parse(c.page)
		if got := matchRules(u); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.page, got, c.want)
		}
	}
	for _, page := range []string{"https://x.com/home", "https://space.bilibili.com/me", "https://www.youtube.com/watch?v=1",
		"https://www.youtube.com/channel/abc", "https://weibo.com/", "https://www.zhihu.com/hot", "https://example.org/golang"} {
		u, _ := url.Parse(page)
		if got := matchRules(u); got != nil {
			t.Errorf("%s: got %+v, want none", page, got)
		}
	}
}

func TestDiscoverRuleNeedsNoRequest(t *testing.T) {
	// A rule match is answered without a request, so even the real discoverer reaches no network.
	got, err := NewDiscoverer("test").Discover(context.Background(), "https://space.bilibili.com/2267573")
	if err != nil || len(got) != 2 {
		t.Errorf("got %+v, %v", got, err)
	}
}
