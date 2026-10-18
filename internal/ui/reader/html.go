package reader

import (
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// What the reader draws of an article's HTML: headings, paragraphs, lists, quotes, code, pictures with their
// captions, rules, and tables as a line of text a row. Links keep their address; scripts, styles, forms, frames,
// video and audio are dropped. The blocks are made off the UI thread, once an article loads.
const (
	paragraph = iota
	heading
	code    // a pre block, its text as written
	picture // an img
	caption // a figcaption
	rule    // an hr
)

// span is a run of text in one style; link is the address it opens, "" for none.
type span struct {
	text                     string
	bold, italic, mono, gone bool
	link                     string
}

// block is a line of the article as the reader draws it.
type block struct {
	kind   int
	level  int    // of a heading: 2 for h1 and h2, 3 for the smaller ones
	quote  int    // how many blockquotes it is in
	depth  int    // how many lists it is in
	marker string // the bullet or number of the first block of a list item
	spans  []span // of a paragraph, heading or caption
	text   string // of a code block
	src    string // of a picture, an absolute http or https address
	alt    string
	width  int // of a picture, 0 when the page does not say
	height int
}

// style is what the elements around a text make of it.
type style struct {
	bold, italic, mono, gone bool
	link                     string
}

type builder struct {
	base   *url.URL
	blocks []block
	spans  []span
	quote  int
	lists  []int  // the next number of each list, 0 for a list with bullets
	marker string // waits for the first block of a list item
	space  bool   // the text so far ends in white space, which the next text does not repeat
}

// parse turns the HTML of an article into blocks; base resolves relative addresses, and title is dropped when it
// opens the text as a heading, since the reader shows it above.
func parse(source string, base *url.URL, title string) []block {
	root, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return nil
	}
	b := &builder{base: base, space: true}
	b.walk(root, style{})
	b.flush(paragraph, 0)
	if len(b.blocks) > 0 && b.blocks[0].kind == heading && plain(b.blocks[0].spans) == strings.TrimSpace(title) {
		b.blocks = b.blocks[1:]
	}
	return b.blocks
}

func plain(spans []span) string {
	var s strings.Builder
	for _, sp := range spans {
		s.WriteString(sp.text)
	}
	return strings.TrimSpace(s.String())
}

func (b *builder) walk(n *html.Node, st style) {
	switch n.Type {
	case html.TextNode:
		b.text(n.Data, st)
		return
	case html.ElementNode:
	default:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			b.walk(c, st)
		}
		return
	}
	switch n.DataAtom {
	case atom.Script, atom.Style, atom.Noscript, atom.Template, atom.Head, atom.Iframe, atom.Object, atom.Embed,
		atom.Form, atom.Button, atom.Input, atom.Select, atom.Textarea, atom.Svg, atom.Math, atom.Video, atom.Audio,
		atom.Canvas:
		return
	case atom.Br:
		b.add(span{text: "\n", bold: st.bold, italic: st.italic, mono: st.mono, gone: st.gone, link: st.link})
		b.space = true
		return
	case atom.Hr:
		b.flush(paragraph, 0)
		b.blocks = append(b.blocks, block{kind: rule, quote: b.quote})
		return
	case atom.Img:
		b.picture(n)
		return
	case atom.Pre:
		b.flush(paragraph, 0)
		text := strings.TrimRightFunc(strings.TrimPrefix(raw(n), "\n"), unicode.IsSpace)
		if text != "" {
			b.blocks = append(b.blocks, b.mark(block{kind: code, text: text}))
		}
		b.space = true
		return
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		b.flush(paragraph, 0)
		level := 3
		if n.DataAtom == atom.H1 || n.DataAtom == atom.H2 {
			level = 2
		}
		st.bold = true
		b.children(n, st)
		b.flush(heading, level)
		return
	case atom.Figcaption:
		b.flush(paragraph, 0)
		b.children(n, st)
		b.flush(caption, 0)
		return
	case atom.Blockquote:
		b.flush(paragraph, 0)
		b.quote++
		b.children(n, st)
		b.flush(paragraph, 0)
		b.quote--
		return
	case atom.Ul, atom.Ol:
		b.flush(paragraph, 0)
		next := 0
		if n.DataAtom == atom.Ol {
			next = 1
			if start, err := strconv.Atoi(attr(n, "start")); err == nil {
				next = start
			}
		}
		b.lists = append(b.lists, next)
		b.children(n, st)
		b.flush(paragraph, 0)
		b.lists = b.lists[:len(b.lists)-1]
		return
	case atom.Li:
		b.flush(paragraph, 0)
		if k := len(b.lists) - 1; k >= 0 {
			switch {
			case b.lists[k] > 0:
				b.marker = strconv.Itoa(b.lists[k]) + "."
				b.lists[k]++
			case k > 0:
				b.marker = "◦"
			default:
				b.marker = "•"
			}
		}
		b.children(n, st)
		b.flush(paragraph, 0)
		b.marker = ""
		return
	case atom.Tr:
		b.flush(paragraph, 0)
		first := true
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode || c.DataAtom != atom.Td && c.DataAtom != atom.Th {
				continue
			}
			if !first {
				b.text(" · ", style{})
			}
			first = false
			cell := st
			cell.bold = cell.bold || c.DataAtom == atom.Th
			b.children(c, cell)
		}
		b.flush(paragraph, 0)
		return
	case atom.A:
		if href := b.address(attr(n, "href"), "mailto"); href != "" {
			st.link = href
		}
	case atom.Strong, atom.B, atom.Dt, atom.Th:
		st.bold = true
	case atom.Em, atom.I, atom.Cite, atom.Dfn:
		st.italic = true
	case atom.Code, atom.Kbd, atom.Samp, atom.Tt:
		st.mono = true
	case atom.Del, atom.S, atom.Strike:
		st.gone = true
	}
	if blockLevel(n.DataAtom) {
		b.flush(paragraph, 0)
		b.children(n, st)
		b.flush(paragraph, 0)
		return
	}
	b.children(n, st)
}

func (b *builder) children(n *html.Node, st style) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.walk(c, st)
	}
}

func blockLevel(a atom.Atom) bool {
	switch a {
	case atom.P, atom.Div, atom.Section, atom.Article, atom.Main, atom.Header, atom.Footer, atom.Aside, atom.Nav,
		atom.Figure, atom.Table, atom.Thead, atom.Tbody, atom.Tfoot, atom.Caption, atom.Dl, atom.Dt, atom.Dd,
		atom.Details, atom.Summary, atom.Address, atom.Center:
		return true
	}
	return false
}

// text adds text in a style, its white space collapsed to one space as a browser does.
func (b *builder) text(s string, st style) {
	var out strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !b.space {
				out.WriteByte(' ')
				b.space = true
			}
			continue
		}
		out.WriteRune(r)
		b.space = false
	}
	if out.Len() == 0 {
		return
	}
	b.add(span{text: out.String(), bold: st.bold, italic: st.italic, mono: st.mono, gone: st.gone, link: st.link})
}

// add puts a span after the text so far, joining it to the last one when they are in the same style.
func (b *builder) add(sp span) {
	if k := len(b.spans) - 1; k >= 0 {
		last := &b.spans[k]
		if last.bold == sp.bold && last.italic == sp.italic && last.mono == sp.mono && last.gone == sp.gone &&
			last.link == sp.link {
			last.text += sp.text
			return
		}
	}
	b.spans = append(b.spans, sp)
}

// flush ends the text so far as a block of kind, unless it is only white space.
func (b *builder) flush(kind, level int) {
	spans := b.spans
	b.spans, b.space = nil, true
	for len(spans) > 0 && strings.TrimSpace(spans[0].text) == "" {
		spans = spans[1:]
	}
	for len(spans) > 0 && strings.TrimSpace(spans[len(spans)-1].text) == "" {
		spans = spans[:len(spans)-1]
	}
	if len(spans) == 0 {
		return
	}
	spans[0].text = strings.TrimLeftFunc(spans[0].text, unicode.IsSpace)
	last := &spans[len(spans)-1]
	last.text = strings.TrimRightFunc(last.text, unicode.IsSpace)
	b.blocks = append(b.blocks, b.mark(block{kind: kind, level: level, spans: spans}))
}

// mark places a block in the quotes and lists it is in, giving it the marker of a list item it opens.
func (b *builder) mark(bl block) block {
	bl.quote, bl.depth, bl.marker = b.quote, len(b.lists), b.marker
	b.marker = ""
	return bl
}

func (b *builder) picture(n *html.Node) {
	src := attr(n, "src")
	if src == "" || strings.HasPrefix(src, "data:") {
		src = attr(n, "data-src")
	}
	src = b.address(src)
	width, _ := strconv.Atoi(attr(n, "width"))
	height, _ := strconv.Atoi(attr(n, "height"))
	// Pictures of a pixel or two count visits; they are not part of the article.
	if src == "" || width > 0 && width <= 2 || height > 0 && height <= 2 {
		return
	}
	b.flush(paragraph, 0)
	b.blocks = append(b.blocks, b.mark(block{kind: picture, src: src, alt: strings.TrimSpace(attr(n, "alt")),
		width: width, height: height}))
}

// address resolves a link of the page against its address, keeping http and https and the other schemes given;
// a link within the page, or to a script, is "".
func (b *builder) address(raw string, schemes ...string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if b.base != nil {
		u = b.base.ResolveReference(u)
	}
	switch scheme := strings.ToLower(u.Scheme); {
	case (scheme == "http" || scheme == "https") && u.Host != "":
		return u.String()
	case scheme != "" && scheme != "http" && scheme != "https" && contains(schemes, scheme):
		return u.String()
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// raw is the text of a pre element as written, with its line breaks.
func raw(n *html.Node) string {
	var s strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch {
		case n.Type == html.TextNode:
			s.WriteString(n.Data)
		case n.Type == html.ElementNode && n.DataAtom == atom.Br:
			s.WriteByte('\n')
		default:
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
	}
	walk(n)
	return s.String()
}
