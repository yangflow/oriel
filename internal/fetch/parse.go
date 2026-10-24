package fetch

import (
	"bytes"
	"mime"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mmcdole/gofeed"
	ext "github.com/mmcdole/gofeed/extensions"
	jsonfeed "github.com/mmcdole/gofeed/json"
	"github.com/yangflow/oriel/internal/store"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/unicode"
)

const maxImages = 20

// parsed is a feed reduced to what the store keeps.
type parsed struct {
	title, siteURL string
	// iconURL is the feed's own image, empty when it has none the image proxy can show.
	iconURL string
	entries []store.FeedEntry
}

// xmlDeclaration matches the encoding named in an XML declaration at the start of a document.
var xmlDeclaration = regexp.MustCompile(`^(\x{FEFF}?\s*<\?xml[^>]*?\bencoding\s*=\s*)("[^"]*"|'[^']*')`)

// headerCharset is the canonical name of the charset in a Content-Type, empty when there is none or it is unknown.
func headerCharset(contentType string) string {
	_, params, err := mime.ParseMediaType(contentType)
	label := strings.ToLower(strings.TrimSpace(params["charset"]))
	if err != nil || label == "" {
		return ""
	}
	if label == "us-ascii" || label == "ascii" {
		return "utf-8" // the web maps it to windows-1252, but a feed in ASCII is also valid UTF-8
	}
	_, name := charset.Lookup(label)
	return name
}

// declaresUTF8 reports whether body says it is UTF-8: by a byte order mark, or by an XML declaration that names it.
func declaresUTF8(body []byte) bool {
	if bytes.HasPrefix(body, []byte{0xEF, 0xBB, 0xBF}) {
		return true
	}
	m := xmlDeclaration.FindSubmatch(body)
	if m == nil {
		return false
	}
	_, name := charset.Lookup(strings.Trim(string(m[2]), `"'`))
	return name == "utf-8"
}

func isASCII(body []byte) bool {
	for _, b := range body {
		if b >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// decodeFeed converts a body to UTF-8 by the charset in the Content-Type, which wins over the XML declaration, and
// rewrites the declaration to say UTF-8 so that the parser does not convert the text a second time. A UTF-16 byte
// order mark wins over the header. The header is trusted only while it fits the bytes: UTF-8 in the header over a body
// that is not valid UTF-8 is ignored, and so is another charset in the header over a body that is valid UTF-8 with
// non-ASCII text and says UTF-8 (a byte order mark or the declaration). A body with no usable charset is returned as is: the parser reads the
// declaration.
func decodeFeed(body []byte, contentType string) []byte {
	name := headerCharset(contentType)
	switch {
	case bytes.HasPrefix(body, []byte{0xFF, 0xFE}):
		name = "utf-16le"
	case bytes.HasPrefix(body, []byte{0xFE, 0xFF}):
		name = "utf-16be"
	}
	var decoder *encoding.Decoder
	switch name {
	case "":
		return body
	case "utf-8":
		if !utf8.Valid(body) {
			return body
		}
		return xmlDeclaration.ReplaceAll(body, []byte(`${1}"utf-8"`))
	case "utf-16le":
		decoder = unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder()
	case "utf-16be":
		decoder = unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewDecoder()
	default:
		if declaresUTF8(body) && utf8.Valid(body) && !isASCII(body) {
			return body
		}
		enc, _ := charset.Lookup(name)
		if enc == nil {
			return body
		}
		decoder = enc.NewDecoder()
	}
	decoded, err := decoder.Bytes(body)
	if err != nil {
		return body
	}
	return xmlDeclaration.ReplaceAll(decoded, []byte(`${1}"utf-8"`))
}

// parse reads RSS, Atom or JSON Feed. contentType is the response's Content-Type, empty when there is none. base
// resolves relative links.
func parse(body []byte, contentType string, base *url.URL) (parsed, error) {
	parser := gofeed.NewParser()
	parser.KeepOriginalFeed = true
	feed, err := parser.Parse(bytes.NewReader(decodeFeed(body, contentType)))
	if err != nil {
		return parsed{}, err
	}
	out := parsed{title: strings.TrimSpace(feed.Title), siteURL: absolute(base, feed.Link)}
	if feed.Image != nil {
		if icon := absolute(base, feed.Image.URL); usableIcon(icon, "") {
			out.iconURL = icon
		}
	}
	jsonFeed, _ := feed.OriginalFeed().(*jsonfeed.Feed)
	for i, item := range feed.Items {
		link := absolute(base, item.Link)
		guid := strings.TrimSpace(item.GUID)
		if guid == "" {
			guid = normalizeLink(link)
		}
		if guid == "" {
			continue
		}
		description := withoutQuotes(item.Description)
		content := withoutQuotes(item.Content)
		if content == "" {
			content = description
		}
		summary := text(content)
		if summary == "" {
			summary = text(description)
		}
		published := item.PublishedParsed
		if published == nil {
			published = item.UpdatedParsed
		}
		durationSec := duration(item)
		if jsonFeed != nil {
			durationSec = jsonDuration(jsonFeed.Items[i])
		}
		entry := store.FeedEntry{
			GUID:         guid,
			Title:        text(item.Title),
			URL:          link,
			Author:       author(item),
			Summary:      summary,
			ContentHTML:  content,
			ThumbnailURL: thumbnail(item, base),
			DurationSec:  durationSec,
			Platform:     platform(link),
			Images:       images(item, content, base),
		}
		entry.Language = entryLanguage(entry.Title, summary)
		if published != nil {
			entry.PublishedAt = *published
		}
		out.entries = append(out.entries, entry)
	}
	return out, nil
}

// absolute resolves ref against base and keeps it only if it is an http or https URL.
func absolute(base *url.URL, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}

// normalizeLink is the entry's identity when the feed gives no GUID: lowercase host, no trailing slash.
func normalizeLink(link string) string {
	u, err := url.Parse(link)
	if err != nil || link == "" {
		return ""
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String()
}

func author(item *gofeed.Item) string {
	if item.DublinCoreExt != nil && len(item.DublinCoreExt.Creator) > 0 {
		return strings.TrimSpace(item.DublinCoreExt.Creator[0])
	}
	for _, person := range item.Authors {
		if person != nil && strings.TrimSpace(person.Name) != "" {
			return strings.TrimSpace(person.Name)
		}
	}
	return ""
}

func platform(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	switch strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."), "m.") {
	case "youtube.com", "youtu.be":
		return "youtube"
	case "bilibili.com", "b23.tv":
		return "bilibili"
	case "x.com", "twitter.com":
		return "x"
	}
	return ""
}

// media returns the media: extension elements of an item, including those inside media:group.
func media(item *gofeed.Item, name string) []ext.Extension {
	found := item.Extensions["media"][name]
	for _, group := range item.Extensions["media"]["group"] {
		found = append(found, group.Children[name]...)
	}
	return found
}

func thumbnail(item *gofeed.Item, base *url.URL) string {
	for _, t := range media(item, "thumbnail") {
		if u := absolute(base, t.Attrs["url"]); u != "" {
			return u
		}
	}
	if item.ITunesExt != nil {
		if u := absolute(base, item.ITunesExt.Image); u != "" {
			return u
		}
	}
	if item.Image != nil {
		if u := absolute(base, item.Image.URL); u != "" {
			return u
		}
	}
	for _, enclosure := range item.Enclosures {
		if strings.HasPrefix(enclosure.Type, "image/") {
			if u := absolute(base, enclosure.URL); u != "" {
				return u
			}
		}
	}
	return ""
}

// duration reads itunes:duration or the duration attribute of media:content, in seconds.
func duration(item *gofeed.Item) int {
	if item.ITunesExt != nil {
		if seconds := parseDuration(item.ITunesExt.Duration); seconds > 0 {
			return seconds
		}
	}
	for _, content := range media(item, "content") {
		if seconds := parseDuration(content.Attrs["duration"]); seconds > 0 {
			return seconds
		}
	}
	return 0
}

// jsonDuration reads the first positive attachment duration, which gofeed's universal model omits.
func jsonDuration(item *jsonfeed.Item) int {
	if item.Attachments == nil {
		return 0
	}
	for _, attachment := range *item.Attachments {
		if attachment.DurationInSeconds > 0 {
			return int(attachment.DurationInSeconds)
		}
	}
	return 0
}

// parseDuration accepts seconds, mm:ss and hh:mm:ss.
func parseDuration(raw string) int {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) > 3 || parts[0] == "" {
		return 0
	}
	total := 0.0
	for _, part := range parts {
		n, err := strconv.ParseFloat(part, 64)
		if err != nil || n < 0 {
			return 0
		}
		total = total*60 + n
	}
	return int(total + 0.5)
}

// images prefers media:content and image enclosures, and falls back to the img tags of the content.
func images(item *gofeed.Item, content string, base *url.URL) []store.Image {
	var found []store.Image
	for _, m := range media(item, "content") {
		if m.Attrs["medium"] == "image" || strings.HasPrefix(m.Attrs["type"], "image/") {
			found = appendImage(found, base, m.Attrs["url"], m.Attrs["width"], m.Attrs["height"])
		}
	}
	for _, enclosure := range item.Enclosures {
		if strings.HasPrefix(enclosure.Type, "image/") {
			found = appendImage(found, base, enclosure.URL, "", "")
		}
	}
	if len(found) == 0 && content != "" {
		walk(content, func(n *html.Node) {
			if n.DataAtom == atom.Img {
				found = appendImage(found, base, attr(n, "src"), attr(n, "width"), attr(n, "height"))
			}
		})
	}
	return found
}

// appendImage adds a new http(s) image, keeping its size only when both sides are known.
func appendImage(images []store.Image, base *url.URL, src, width, height string) []store.Image {
	u := absolute(base, src)
	if u == "" || len(images) >= maxImages {
		return images
	}
	for _, image := range images {
		if image.URL == u {
			return images
		}
	}
	image := store.Image{URL: u}
	w, errW := strconv.Atoi(width)
	h, errH := strconv.Atoi(height)
	if errW == nil && errH == nil && w > 0 && h > 0 {
		image.Width, image.Height = w, h
	}
	return append(images, image)
}

// withoutQuotes removes Discourse quote blocks, so a reply does not carry another post's words as its own.
func withoutQuotes(fragment string) string {
	if !strings.Contains(fragment, "quote") {
		return strings.TrimSpace(fragment)
	}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"})
	if err != nil {
		return strings.TrimSpace(fragment)
	}
	var out strings.Builder
	for _, n := range nodes {
		if isQuote(n) {
			continue
		}
		removeQuotes(n)
		html.Render(&out, n)
	}
	return strings.TrimSpace(out.String())
}

func isQuote(n *html.Node) bool {
	return n.DataAtom == atom.Aside && strings.Contains(" "+attr(n, "class")+" ", " quote ")
}

func removeQuotes(n *html.Node) {
	for child := n.FirstChild; child != nil; {
		next := child.NextSibling
		if isQuote(child) {
			n.RemoveChild(child)
		} else {
			removeQuotes(child)
		}
		child = next
	}
}

// text is the visible text of an HTML fragment on one line.
func text(fragment string) string {
	if !strings.ContainsAny(fragment, "<&") {
		return strings.Join(strings.Fields(fragment), " ")
	}
	var b strings.Builder
	walk(fragment, func(n *html.Node) {
		if n.Type == html.TextNode && (n.Parent == nil || (n.Parent.DataAtom != atom.Script && n.Parent.DataAtom != atom.Style)) {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
	})
	return strings.Join(strings.Fields(b.String()), " ")
}

func walk(fragment string, visit func(*html.Node)) {
	nodes, err := html.ParseFragment(strings.NewReader(fragment), &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"})
	if err != nil {
		return
	}
	var visitAll func(*html.Node)
	visitAll = func(n *html.Node) {
		visit(n)
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visitAll(child)
		}
	}
	for _, n := range nodes {
		visitAll(n)
	}
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}
