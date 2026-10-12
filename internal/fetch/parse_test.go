package fetch

import (
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

const rssFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/"
  xmlns:media="http://search.yahoo.com/mrss/" xmlns:dc="http://purl.org/dc/elements/1.1/"
  xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd">
<channel>
  <title> Example Blog </title>
  <link>https://blog.example.org/</link>
  <item>
    <title>First &amp; best</title>
    <link>/posts/1</link>
    <guid isPermaLink="false">post-1</guid>
    <dc:creator>Ann</dc:creator>
    <pubDate>Mon, 12 Oct 2026 10:00:00 +0800</pubDate>
    <description>Short summary</description>
    <content:encoded><![CDATA[<p>Hello <b>world</b></p><img src="/a.png" width="640" height="480"><img src="https://cdn.example.org/b.png">]]></content:encoded>
  </item>
  <item>
    <title>No guid</title>
    <link>HTTPS://Blog.Example.org/posts/2/</link>
    <description><![CDATA[<p>Only a <i>description</i></p>]]></description>
    <media:thumbnail url="https://cdn.example.org/t.jpg"/>
    <itunes:duration>1:02:03</itunes:duration>
  </item>
  <item><title>Neither guid nor link</title></item>
</channel>
</rss>`

const atomFeed = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:media="http://search.yahoo.com/mrss/">
  <title>Videos</title>
  <link rel="alternate" href="https://www.youtube.com/channel/abc"/>
  <entry>
    <id>yt:video:xyz</id>
    <title>A video</title>
    <link rel="alternate" href="https://www.youtube.com/watch?v=xyz"/>
    <author><name>Channel</name></author>
    <updated>2026-10-11T09:00:00Z</updated>
    <media:group>
      <media:thumbnail url="https://i.ytimg.com/vi/xyz/hq.jpg" width="480" height="360"/>
      <media:content url="https://i.ytimg.com/vi/xyz/full.jpg" medium="image" width="1280" height="720"/>
    </media:group>
  </entry>
</feed>`

const discourseFeed = `<rss version="2.0"><channel><title>Forum</title><item>
  <title>Reply</title><guid>r1</guid>
  <description><![CDATA[<aside class="quote no-group"><blockquote>someone else's words</blockquote></aside><p>My reply</p><div><aside class="quote"><p>nested</p></aside></div>]]></description>
</item></channel></rss>`

func mustParse(t *testing.T, body, base string) parsed {
	t.Helper()
	u, _ := url.Parse(base)
	feed, err := parse([]byte(body), u)
	if err != nil {
		t.Fatal(err)
	}
	return feed
}

func TestParseRSS(t *testing.T) {
	feed := mustParse(t, rssFeed, "https://blog.example.org/feed.xml")
	if feed.title != "Example Blog" || feed.siteURL != "https://blog.example.org/" || len(feed.entries) != 2 {
		t.Fatalf("feed: %q %q %d entries", feed.title, feed.siteURL, len(feed.entries))
	}
	first := feed.entries[0]
	want := store.FeedEntry{
		GUID: "post-1", Title: "First & best", URL: "https://blog.example.org/posts/1", Author: "Ann",
		PublishedAt: time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC),
		Summary:     "Hello world",
		ContentHTML: `<p>Hello <b>world</b></p><img src="/a.png" width="640" height="480"><img src="https://cdn.example.org/b.png">`,
		// gofeed takes the first image of the content as the item image.
		ThumbnailURL: "https://blog.example.org/a.png",
		Images: []store.Image{
			{URL: "https://blog.example.org/a.png", Width: 640, Height: 480},
			{URL: "https://cdn.example.org/b.png"},
		},
	}
	first.PublishedAt = first.PublishedAt.UTC()
	if !reflect.DeepEqual(first, want) {
		t.Errorf("first:\n got %+v\nwant %+v", first, want)
	}
	second := feed.entries[1]
	if second.GUID != "https://blog.example.org/posts/2" || second.ThumbnailURL != "https://cdn.example.org/t.jpg" ||
		second.DurationSec != 3723 || second.Summary != "Only a description" || !second.PublishedAt.IsZero() {
		t.Errorf("second: %+v", second)
	}
}

func TestParseAtom(t *testing.T) {
	feed := mustParse(t, atomFeed, "https://www.youtube.com/feeds/videos.xml?channel_id=abc")
	entry := feed.entries[0]
	if entry.GUID != "yt:video:xyz" || entry.Platform != "youtube" || entry.Author != "Channel" ||
		entry.ThumbnailURL != "https://i.ytimg.com/vi/xyz/hq.jpg" ||
		!entry.PublishedAt.Equal(time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)) ||
		!reflect.DeepEqual(entry.Images, []store.Image{{URL: "https://i.ytimg.com/vi/xyz/full.jpg", Width: 1280, Height: 720}}) {
		t.Errorf("entry: %+v", entry)
	}
}

func TestParseDropsQuotedPosts(t *testing.T) {
	entry := mustParse(t, discourseFeed, "https://forum.example.org/latest.rss").entries[0]
	if entry.Summary != "My reply" || entry.ContentHTML != "<p>My reply</p><div></div>" {
		t.Errorf("entry: %q %q", entry.Summary, entry.ContentHTML)
	}
}

func TestParseRejectsNonFeeds(t *testing.T) {
	if _, err := parse([]byte("<html><body>not a feed</body></html>"), nil); err == nil {
		t.Error("parsed an HTML page")
	}
}

func TestParseDuration(t *testing.T) {
	for raw, want := range map[string]int{"90": 90, "1:30": 90, "1:00:00": 3600, "12.6": 13, "": 0, "x": 0, "1:2:3:4": 0, "-5": 0} {
		if got := parseDuration(raw); got != want {
			t.Errorf("%q: %d, want %d", raw, got, want)
		}
	}
}

func TestPlatform(t *testing.T) {
	for link, want := range map[string]string{
		"https://www.youtube.com/watch?v=1": "youtube", "https://youtu.be/1": "youtube",
		"https://m.bilibili.com/video/BV1": "bilibili", "https://x.com/a/status/1": "x",
		"https://twitter.com/a": "x", "https://example.org": "", "": "",
	} {
		if got := platform(link); got != want {
			t.Errorf("%q: %q, want %q", link, got, want)
		}
	}
}
