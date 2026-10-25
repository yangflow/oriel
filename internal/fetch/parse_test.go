package fetch

import (
	"context"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
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
	feed, err := parse([]byte(body), "", u)
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
	if _, err := parse([]byte("<html><body>not a feed</body></html>"), "", nil); err == nil {
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

func TestTextJoinsInlineNodesWithoutSpaces(t *testing.T) {
	for fragment, want := range map[string]string{
		`because of the <a href="/moon">moon</a>.`: "because of the moon.",
		`<em>Hello</em>, <strong>world</strong>!`:  "Hello, world!",
		`one<br>two`:                                "one two",
		`<p>first</p><p>second</p>`:                 "first second",
		`<ul><li>a</li><li>b</li></ul>`:             "a b",
		`a<script>x()</script> b<style>p{}</style>`: "a b",
		`  spaced   <b>out</b>  `:                   "spaced out",
	} {
		if got := text(fragment); got != want {
			t.Errorf("%q: %q, want %q", fragment, got, want)
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

func TestParseDecodesTheHeaderCharset(t *testing.T) {
	const xmlBody = `<rss version="2.0"><channel><title>中文博客</title><item><title>第一篇</title><guid>1</guid>` +
		`<description>你好，世界</description></item></channel></rss>`
	gbk, err := simplifiedchinese.GBK.NewEncoder().String(xmlBody)
	if err != nil {
		t.Fatal(err)
	}
	declared, err := simplifiedchinese.GBK.NewEncoder().String(`<?xml version="1.0" encoding="gbk"?>` + xmlBody)
	if err != nil {
		t.Fatal(err)
	}
	conflicting, err := simplifiedchinese.GBK.NewEncoder().String(`<?xml version="1.0" encoding="utf-8"?>` + xmlBody)
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct{ body, contentType string }{
		"header only":            {`<?xml version="1.0"?>` + gbk, "application/rss+xml; charset=gbk"},
		"no declaration":         {gbk, "application/rss+xml; charset=GBK"},
		"declaration only":       {declared, "application/rss+xml"},
		"both":                   {declared, `application/xml; charset="gb2312"`},
		"the header wins":        {conflicting, "text/xml; charset=gbk"},
		"unknown header charset": {declared, "application/xml; charset=nonsense"},
	} {
		feed, err := parse([]byte(c.body), c.contentType, nil)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if feed.title != "中文博客" || len(feed.entries) != 1 || feed.entries[0].Title != "第一篇" || feed.entries[0].Summary != "你好，世界" {
			t.Errorf("%s: %q %+v", name, feed.title, feed.entries)
		}
	}

	// Text that is already UTF-8 stays as it is, with or without a charset in the header.
	for _, contentType := range []string{"", "application/rss+xml", "application/rss+xml; charset=utf-8"} {
		feed, err := parse([]byte(`<?xml version="1.0" encoding="UTF-8"?>`+xmlBody), contentType, nil)
		if err != nil || feed.title != "中文博客" {
			t.Errorf("%q: %q %v", contentType, feed.title, err)
		}
	}
}

func TestParseTrustsTheHeaderOnlyWhenItFitsTheBytes(t *testing.T) {
	const xmlBody = `<rss version="2.0"><channel><title>中文博客</title><item><title>第一篇</title><guid>1</guid>` +
		`<description>你好，世界</description></item></channel></rss>`
	encode := func(enc *encoding.Encoder, s string) string {
		out, err := enc.String(s)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	gbkDeclared := encode(simplifiedchinese.GBK.NewEncoder(), `<?xml version="1.0" encoding="gbk"?>`+xmlBody)
	utf8Declared := `<?xml version="1.0" encoding="utf-8"?>` + xmlBody
	utf8Bom := "\xef\xbb\xbf" + utf8Declared
	utf16 := func(e unicode.Endianness, bom unicode.BOMPolicy, s string) string {
		return encode(unicode.UTF16(e, bom).NewEncoder(), s)
	}
	utf16Declared := `<?xml version="1.0" encoding="utf-16"?>` + xmlBody
	for name, c := range map[string]struct{ body, contentType string }{
		"gbk declared, header says utf-8":       {gbkDeclared, "text/xml; charset=utf-8"},
		"gbk declared, header says us-ascii":    {gbkDeclared, "text/xml; charset=US-ASCII"},
		"utf-8 declared, header says gbk":       {utf8Declared, "text/xml; charset=gbk"},
		"utf-8 declared, header says latin-1":   {utf8Declared, "application/xml; charset=iso-8859-1"},
		"utf-8 with a byte order mark":          {utf8Bom, ""},
		"utf-8 with a byte order mark, header":  {utf8Bom, "application/xml; charset=utf-8"},
		"utf-8 mark, header says gbk":           {utf8Bom, "application/xml; charset=gbk"},
		"utf-16 le with a mark":                 {utf16(unicode.LittleEndian, unicode.UseBOM, utf16Declared), ""},
		"utf-16 be with a mark":                 {utf16(unicode.BigEndian, unicode.UseBOM, utf16Declared), ""},
		"utf-16 with a mark and a header":       {utf16(unicode.LittleEndian, unicode.UseBOM, utf16Declared), "application/xml; charset=utf-16"},
		"utf-16 mark beats the header":          {utf16(unicode.BigEndian, unicode.UseBOM, utf16Declared), "application/xml; charset=utf-16le"},
		"utf-16 header, no mark":                {utf16(unicode.LittleEndian, unicode.IgnoreBOM, xmlBody), "application/xml; charset=utf-16"},
		"utf-16le header, no mark":              {utf16(unicode.LittleEndian, unicode.IgnoreBOM, utf16Declared), "application/xml; charset=utf-16le"},
		"utf-16be header, no mark":              {utf16(unicode.BigEndian, unicode.IgnoreBOM, utf16Declared), "application/xml; charset=UTF-16BE"},
		"gbk declared, header says nothing":     {gbkDeclared, "application/xml"},
		"utf-8 declared, utf-8 header, no mark": {utf8Declared, "application/xml; charset=utf-8"},
	} {
		feed, err := parse([]byte(c.body), c.contentType, nil)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if feed.title != "中文博客" || len(feed.entries) != 1 || feed.entries[0].Title != "第一篇" || feed.entries[0].Summary != "你好，世界" {
			t.Errorf("%s: %q %+v", name, feed.title, feed.entries)
		}
	}

	// UTF-16 with neither a mark nor a header cannot be told apart from noise: a plain parse error, not garbage.
	if feed, err := parse([]byte(utf16(unicode.LittleEndian, unicode.IgnoreBOM, utf16Declared)), "application/xml", nil); err == nil {
		t.Errorf("utf-16 without a mark or a header parsed as %q", feed.title)
	}
}

func TestParseJSONFeedWithACharsetHeader(t *testing.T) {
	const body = `{"version":"https://jsonfeed.org/version/1.1","title":"中文博客","items":[{"id":"1","title":"第一篇",` +
		`"content_text":"你好，世界"}]}`
	gbk, err := simplifiedchinese.GBK.NewEncoder().String(body)
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct{ body, contentType string }{
		"utf-8": {body, "application/feed+json; charset=utf-8"},
		"none":  {body, "application/feed+json"},
		"gbk":   {gbk, "application/feed+json; charset=gbk"},
	} {
		feed, err := parse([]byte(c.body), c.contentType, nil)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if feed.title != "中文博客" || len(feed.entries) != 1 || feed.entries[0].Title != "第一篇" || feed.entries[0].Summary != "你好，世界" {
			t.Errorf("%s: %q %+v", name, feed.title, feed.entries)
		}
	}
}

func TestParseJSONFeedAttachmentDuration(t *testing.T) {
	for _, version := range []string{"1", "1.1"} {
		for _, c := range []struct {
			name, attachments string
			want              int
		}{
			{"audio", `[{"url":"https://example.org/episode.mp3","mime_type":"audio/mpeg","duration_in_seconds":95}]`, 95},
			{"video", `[{"url":"https://example.org/episode.mp4","mime_type":"video/mp4","duration_in_seconds":125}]`, 125},
			{"first positive", `[{"duration_in_seconds":0},{"duration_in_seconds":95},{"duration_in_seconds":125}]`, 95},
			{"negative then positive", `[{"duration_in_seconds":-5},{"duration_in_seconds":95}]`, 95},
			{"negative", `[{"duration_in_seconds":-5}]`, 0},
			{"zero", `[{"duration_in_seconds":0}]`, 0},
			{"size is not duration", `[{"url":"https://example.org/episode.mp3","mime_type":"audio/mpeg","size_in_bytes":95000}]`, 0},
			{"empty", `[]`, 0},
			{"null", `null`, 0},
		} {
			t.Run(version+"/"+c.name, func(t *testing.T) {
				body := `{"version":"https://jsonfeed.org/version/` + version + `","title":"Podcast","items":[` +
					`{"id":"episode","content_text":"A short description.","attachments":` + c.attachments + `},` +
					`{"id":"text","content_text":"A text-only entry."}]}`
				feed := mustParse(t, body, "https://example.org/feed.json")
				if len(feed.entries) != 2 {
					t.Fatalf("got %d entries, want 2", len(feed.entries))
				}
				if got := feed.entries[0].DurationSec; got != c.want {
					t.Errorf("duration = %d, want %d", got, c.want)
				}
				if got := feed.entries[1].DurationSec; got != 0 {
					t.Errorf("text-only duration = %d, want 0", got)
				}
			})
		}
	}
}

func TestJSONFeedPodcastKeepsItsReadingTime(t *testing.T) {
	body, err := os.ReadFile("testdata/feeds/jsonfeed-1.1.json")
	if err != nil {
		t.Fatal(err)
	}
	feed := mustParse(t, string(body), corpusFeedBase)
	st := openStore(t)
	ctx := context.Background()
	now := time.UnixMilli(1_790_000_000_000)
	src, err := st.CreateSource(ctx, store.NewSource{URL: "https://example.org/feed.json", Kind: "article", FetchInterval: time.Hour}, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveFeed(ctx, store.FeedSource{ID: src.ID, Kind: src.Kind}, feed.title, feed.siteURL, feed.entries, "", time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	entries, err := st.Entries(ctx, store.EntryFilter{SourceID: src.ID}, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.URL == "https://bench.example.com/2026/podcast" {
			if entry.DurationSec != 95 {
				t.Errorf("podcast duration = %d, want 95 seconds", entry.DurationSec)
			}
			if entry.ReadingSec == nil {
				t.Fatal("podcast reading time is missing")
			}
			if *entry.ReadingSec != 95 {
				t.Errorf("podcast reading time = %d, want 95 seconds", *entry.ReadingSec)
			}
			return
		}
	}
	t.Fatal("podcast was not stored")
}
