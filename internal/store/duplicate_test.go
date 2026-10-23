package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestFeedURLKey(t *testing.T) {
	same := [][]string{
		{"https://example.org/feed", "http://example.org/feed", "https://www.example.org/feed", "HTTP://WWW.Example.ORG:80/feed/", "https://example.org/feed#top"},
		{"https://example.org", "http://example.org/", "https://www.example.org:443"},
		{"https://example.org/feed?a=1&b=2", "https://example.org/feed?b=2&a=1", "http://example.org/feed/?utm_source=x&b=2&a=1&fbclid=z"},
		{"https://example.org/feed?a=2&a=1", "https://example.org/feed?a=1&a=2"},
	}
	for _, group := range same {
		for _, other := range group[1:] {
			if feedURLKey(group[0]) != feedURLKey(other) {
				t.Errorf("%q and %q should be one feed: %q %q", group[0], other, feedURLKey(group[0]), feedURLKey(other))
			}
		}
	}
	different := [][2]string{
		{"https://example.org/feed", "https://example.org/Feed"},
		{"https://example.org/feed", "https://example.org/feed?a=1"},
		{"https://example.org/feed", "https://example.org:8443/feed"},
		{"https://example.org/feed", "https://blog.example.org/feed"},
		{"https://example.org/feed?a=1", "https://example.org/feed?a=2"},
	}
	for _, pair := range different {
		if feedURLKey(pair[0]) == feedURLKey(pair[1]) {
			t.Errorf("%q and %q should differ", pair[0], pair[1])
		}
	}
}

func TestFindDuplicate(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.UnixMilli(1_000_000)
	if err := s.SetSetting(ctx, "rsshub_base_url", "https://hub.example\nhttps://alt.example/rsshub/"); err != nil {
		t.Fatal(err)
	}
	add := func(src NewSource) Source {
		t.Helper()
		src.Kind, src.FetchInterval = "article", time.Hour
		created, err := s.CreateSource(ctx, src, "", now)
		if err != nil {
			t.Fatalf("%+v: %v", src, err)
		}
		return created
	}
	web := add(NewSource{URL: "https://example.org/feed.xml"})
	route := add(NewSource{RSSHubPath: "/github/issue/a/b"})
	channel := add(NewSource{RSSHubPath: "/youtube/channel/UC123"})
	playlist := add(NewSource{URL: "https://www.youtube.com/feeds/videos.xml?playlist_id=PL9"})

	cases := []struct {
		name string
		src  NewSource
		want int64 // 0 for none
	}{
		{"scheme and www", NewSource{URL: "http://www.example.org/feed.xml/"}, web.ID},
		{"tracking and fragment", NewSource{URL: "https://example.org/feed.xml?utm_medium=rss#x"}, web.ID},
		{"another query", NewSource{URL: "https://example.org/feed.xml?x=1"}, 0},
		{"another path", NewSource{URL: "https://example.org/other.xml"}, 0},
		{"route again", NewSource{RSSHubPath: "/github/issue/a/b/"}, route.ID},
		{"route with another parameter", NewSource{RSSHubPath: "/github/issue/a/c"}, 0},
		{"route address on the first instance", NewSource{URL: "https://hub.example/github/issue/a/b"}, route.ID},
		{"route address on an instance with a path", NewSource{URL: "http://alt.example/rsshub/github/issue/a/b"}, route.ID},
		{"route address on an unknown host", NewSource{URL: "https://other.example/github/issue/a/b"}, 0},
		{"instance address outside its path", NewSource{URL: "https://alt.example/github/issue/a/b"}, 0},
		{"youtube feed for a channel route", NewSource{URL: "https://www.youtube.com/feeds/videos.xml?channel_id=UC123"}, channel.ID},
		{"youtube route for a playlist feed", NewSource{RSSHubPath: "/youtube/playlist/PL9"}, playlist.ID},
		{"another youtube channel", NewSource{URL: "https://www.youtube.com/feeds/videos.xml?channel_id=UC999"}, 0},
	}
	for _, c := range cases {
		id, found, err := s.FindDuplicate(ctx, c.src)
		if err != nil || found != (c.want != 0) || id != c.want {
			t.Errorf("%s: id %d found %v err %v, want %d", c.name, id, found, err, c.want)
		}
	}

	// The reverse: the route is added after the address on an instance.
	hubFeed := add(NewSource{URL: "https://hub.example/twitter/user/x"})
	if id, found, _ := s.FindDuplicate(ctx, NewSource{RSSHubPath: "/twitter/user/x"}); !found || id != hubFeed.ID {
		t.Errorf("route for an instance address: %d %v", id, found)
	}
}

func TestDuplicateRefusedAndForced(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.UnixMilli(1_000_000)
	src := NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}
	first, err := s.CreateSource(ctx, src, "First", now)
	if err != nil {
		t.Fatal(err)
	}
	src.URL = "http://www.example.org/feed"
	_, err = s.CreateSource(ctx, src, "", now)
	var exists *ExistsError
	if !errors.Is(err, ErrExists) || !errors.As(err, &exists) || exists.ID != first.ID {
		t.Fatalf("duplicate: %v", err)
	}
	src.AllowDuplicate = true
	if _, err := s.CreateSource(ctx, src, "Second", now); err != nil {
		t.Fatalf("forced: %v", err)
	}
	src.URL = "https://example.org/feed"
	if _, err := s.CreateSource(ctx, src, "", now); !errors.Is(err, ErrExists) {
		t.Fatalf("the very same address, forced: %v", err)
	}
}
