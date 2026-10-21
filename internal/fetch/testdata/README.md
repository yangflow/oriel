# Parser corpus

Samples for `corpus_test.go`. Every sample here was written by hand for this repository, modeled on the structure of
real feeds and pages. Nothing was downloaded or copied, all names, addresses and text are invented (the hosts are
`example.*` domains), and there is no personal data. They are covered by the repository's MIT license.

`TestCorpusFeeds` reads each file in `feeds/` with `parse` and compares the result with the `*.golden.json` next to it.
`TestCorpusPages` reads each file in `pages/` through the charset decoder and `articleHTML` (Discourse post pick, then
Readability) and compares the extracted HTML with its golden file.

Feeds are parsed as if fetched from `https://feeds.example.org/blog/feed.xml`; pages are read from the address in
`corpusPageAddress` or `https://pages.example.org/articles/post`. Times in goldens are UTC RFC 3339; an empty
`published` means the feed gave no usable date. The store has no "kind" of entry, so the golden shows the nearest
fields: `platform`, `durationSec` and `images`.

After a deliberate change to parsing, rewrite the goldens and read the diff before committing it:

```sh
go test ./internal/fetch -run TestCorpus -update
```

## Feeds

| File | Origin | Covers |
| --- | --- | --- |
| `rss-wordpress.xml` | self-made, modeled on WordPress 6 RSS output | CDATA, `content:encoded`, `dc:creator`, `isPermaLink="false"` GUIDs, channel image |
| `rss-iso-8859-1.xml` | self-made, modeled on a French/German small-site feed; file bytes are ISO-8859-1 | declared ISO-8859-1 encoding, accents and umlauts |
| `rss-gbk.xml` | self-made, modeled on a Chinese blog feed; file bytes are GBK | declared GBK encoding, `author` as "mail (name)" |
| `rss-missing-dates.xml` | self-made, modeled on hand-written changelog feeds | no `pubDate`, empty and unparseable dates, no GUID (link as identity), item with neither link nor GUID |
| `rss-relative-links.xml` | self-made, modeled on static-site generators with relative output | relative feed, item, image and thumbnail addresses, protocol-relative image, `data:` image, `javascript:` and `mailto:` links |
| `rss-discourse.xml` | self-made, modeled on Discourse `latest.rss` | quote blocks removed from replies, a post that is only a quote |
| `rss-media-podcast.xml` | self-made, modeled on podcast and Media RSS feeds | `itunes:duration`, `itunes:image`, `media:group`, `media:content`, `media:thumbnail`, image enclosure |
| `rss-rsshub.xml` | self-made, modeled on RSSHub output for a social account and a video channel | `generator` RSSHub, `author` element, platform detection (x, bilibili), CJK text, empty description |
| `atom-blog.xml` | self-made, modeled on Jekyll and Hugo Atom output | Atom 1.0, escaped HTML content, XHTML content, `updated` only, no `id` |
| `atom-youtube.xml` | self-made, modeled on YouTube channel feeds | `yt:` and `media:group` extensions, platform detection (youtube) |
| `jsonfeed-1.1.json` | self-made, modeled on JSON Feed 1.1 | `authors`, `content_html`, `content_text`, `image`, `date_modified` only, audio attachment |
| `jsonfeed-1.0.json` | self-made, modeled on JSON Feed 1 | single `author`, `banner_image`, image attachment, item without `url`, invalid date |

## Pages

| File | Origin | Covers |
| --- | --- | --- |
| `article.html` | self-made, modeled on a blog post page | Readability: navigation, footer and byline dropped, links made absolute |
| `article-gbk.html` | self-made, modeled on a Chinese blog page; file bytes are GBK | charset taken from a `meta` tag |
| `discourse-topic.html` | self-made, modeled on the crawler view of a Discourse topic | pick of one post by address (`/77/3`), quote removed |
