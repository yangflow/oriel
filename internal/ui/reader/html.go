package reader

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/yangflow/oriel/internal/tracker"
	"github.com/yangflow/oriel/internal/ui/typeset"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// What the reader draws of an article's HTML: headings, paragraphs, lists, quotes, code, pictures with their
// captions, rules, tables (those with a head and rows of plain cells as a grid, the others as a line of text a row),
// superscript, subscript and footnotes. Links keep their address, those within the page the block they lead to;
// scripts, styles, forms, frames, video and audio are dropped. The blocks are made off the UI thread, once an
// article loads.
const (
	paragraph = iota
	heading
	code    // a pre block, its text as written
	picture // an img
	caption // a figcaption
	rule    // an hr
	table   // a table of plain cells
	embed   // a video, audio, iframe, object or embed, shown as one row
)

// The kinds of an embed row, kept in the level of its block.
const (
	embedVideo = iota
	embedAudio
	embedOther
)

// top is the jump of a link to the top of the page.
const top = "#"

// span is a run of text in one style; link is the address it opens, "" for none. jump is the id of the block a link
// within the page leads to, top for the top of the page, "" for none. ref is the number of the footnote the span
// refers to, back marks the arrow that leads from a footnote back to the text: with no jump it leads to the
// reference last followed.
type span struct {
	text                     string
	bold, italic, mono, gone bool
	link                     string
	jump                     string
	ref                      int
	back                     bool
}

// Alignments of a table cell.
const (
	alignStart = iota
	alignCenter
	alignEnd
)

// cell is a cell of a table: its text, how many columns it spans, whether it heads its column and how it aligns;
// number marks the cells of a column of numbers.
type cell struct {
	spans  []span
	span   int
	head   bool
	align  int
	number bool
}

// grid is a table: rows of cells where the first row heads the columns when head is set, the columns it has, and its
// caption.
type grid struct {
	rows    [][]cell
	cols    int
	head    bool
	caption []span
}

// block is a line of the article as the reader draws it.
type block struct {
	kind   int
	level  int    // of a heading: 2 for h1 and h2, 3 for the smaller ones; of an embed: embedVideo, embedAudio or embedOther
	quote  int    // how many blockquotes it is in
	depth  int    // how many lists it is in
	marker string // the bullet or number of the first block of a list item
	spans  []span // of a paragraph, heading or caption
	text   string // of a code block
	src    string // of a picture, an absolute http or https address
	alt    string // of a picture its description, of an embed the title the element gives
	width  int    // of a picture, 0 when the page does not say
	height int
	table  *grid    // of a table
	ids    []string // the ids and names within the page that lead here
	ix     int      // where it is in the text
	notes  bool     // it is in the notes at the end of the text
	note   int      // the number of the footnote it is part of, 0 for none

	// laid is set once set holds the pieces of spans that Chinese typesetting spaces (nil when none needs it); it is
	// worked out when the block is first drawn as Chinese text and never changes, as the spans do not.
	set  [][]typeset.Piece
	laid bool
}

// style is what the elements around a text make of it.
type style struct {
	bold, italic, mono, gone bool
	link                     string
	jump                     string
	script                   int // 1 for superscript, 2 for subscript
	ref                      bool
	back                     bool
}

// of is the span a text in the style makes.
func (st style) of(text string) span {
	sp := span{text: text, bold: st.bold, italic: st.italic, mono: st.mono, gone: st.gone, link: st.link, jump: st.jump,
		back: st.back}
	if st.ref {
		sp.ref = -1
	}
	return sp
}

type builder struct {
	base   *url.URL
	blocks []block
	spans  []span
	quote  int
	lists  []int    // the next number of each list, 0 for a list with bullets
	marker string   // waits for the first block of a list item
	space  bool     // the text so far ends in white space, which the next text does not repeat
	ids    []string // the ids and names seen since the last block, for the next one
	notes  bool     // inside the notes at the end of the text
	noteN  int      // the notes seen in them
	noteAt int      // the note being read, 0 outside one
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
	resolve(b.blocks)
	return b.blocks
}

// resolve numbers the blocks, finds the blocks the links within the page lead to, and the footnotes the references
// refer to. A link to nowhere is left as text, a footnote without a way back to the text gets one.
func resolve(blocks []block) {
	where := map[string]int{}
	last := map[int]int{} // the last block of each footnote
	for i := range blocks {
		blocks[i].ix = i
		for _, id := range blocks[i].ids {
			if _, taken := where[id]; !taken {
				where[id] = i
			}
		}
		if blocks[i].note > 0 {
			last[blocks[i].note] = i
		}
	}
	fix := func(spans []span) {
		for k := range spans {
			sp := &spans[k]
			to, found := where[sp.jump]
			if sp.ref == -1 {
				sp.ref = 0
				if found && blocks[to].note > 0 {
					sp.ref = blocks[to].note
				}
			}
			switch {
			case sp.jump == "top" && !found:
				sp.jump = top
			case sp.jump != "" && sp.jump != top && !found:
				sp.jump = ""
			}
		}
	}
	for i := range blocks {
		fix(blocks[i].spans)
		if g := blocks[i].table; g != nil {
			fix(g.caption)
			for _, row := range g.rows {
				for _, c := range row {
					fix(c.spans)
				}
			}
		}
	}
	for n, at := range last {
		spans := blocks[at].spans
		back := false
		for i := range blocks {
			if blocks[i].note == n {
				for _, sp := range blocks[i].spans {
					back = back || sp.back
				}
			}
		}
		if !back && len(spans) > 0 {
			blocks[at].spans = append(slices.Clip(spans), span{text: " "}, span{text: "↩", back: true})
		}
	}
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
	b.ids = b.collect(n)
	if !b.notes && isNotes(n) {
		b.flush(paragraph, 0)
		b.notes, b.noteN = true, 0
		b.blocks = append(b.blocks, b.attach(block{kind: rule, quote: b.quote}))
		b.children(n, st)
		b.flush(paragraph, 0)
		b.notes, b.noteAt = false, 0
		return
	}
	switch n.DataAtom {
	case atom.Script, atom.Style, atom.Noscript, atom.Template, atom.Head, atom.Form, atom.Button, atom.Input,
		atom.Select, atom.Textarea, atom.Svg, atom.Math, atom.Canvas:
		return
	case atom.Iframe, atom.Object, atom.Embed, atom.Video, atom.Audio:
		b.embed(n)
		return
	case atom.Br:
		b.add(st.of("\n"))
		b.space = true
		return
	case atom.Hr:
		b.flush(paragraph, 0)
		if !b.notes {
			b.blocks = append(b.blocks, b.attach(block{kind: rule, quote: b.quote}))
		}
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
		if b.notes && b.noteAt == 0 {
			b.noteN++
			b.noteAt = b.noteN
			defer func() { b.noteAt = 0 }()
		}
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
	case atom.Table:
		b.flush(paragraph, 0)
		if g := b.grid(n); g != nil {
			b.blocks = append(b.blocks, b.mark(block{kind: table, table: g}))
			b.space = true
			return
		}
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
		} else if frag, ok := fragment(attr(n, "href")); ok {
			st.jump = frag
			noteref := attr(n, "role") == "doc-noteref"
			switch {
			case isBack(n):
				st.back = true
				b.add(st.of("↩"))
				b.space = false
				return
			case noteref || st.script == 1 || onlySup(n):
				st.ref = true
				if noteref {
					st.script = 1
				}
			}
		}
	case atom.Sup:
		st.script = 1
	case atom.Sub:
		st.script = 2
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
	text := out.String()
	if st.ref {
		text = strings.NewReplacer("[", "", "]", "").Replace(text)
	}
	if st.script != 0 {
		text = scripted(text, st.script)
	}
	b.add(st.of(text))
}

var supers = map[rune]rune{'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴', '5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸',
	'9': '⁹', '+': '⁺', '-': '⁻', '−': '⁻', '=': '⁼', '(': '⁽', ')': '⁾', 'n': 'ⁿ', 'i': 'ⁱ'}

var subs = map[rune]rune{'0': '₀', '1': '₁', '2': '₂', '3': '₃', '4': '₄', '5': '₅', '6': '₆', '7': '₇', '8': '₈',
	'9': '₉', '+': '₊', '-': '₋', '−': '₋', '=': '₌', '(': '₍', ')': '₎', 'a': 'ₐ', 'e': 'ₑ', 'o': 'ₒ', 'x': 'ₓ',
	'h': 'ₕ', 'k': 'ₖ', 'l': 'ₗ', 'm': 'ₘ', 'n': 'ₙ', 'p': 'ₚ', 's': 'ₛ', 't': 'ₜ', 'i': 'ᵢ', 'j': 'ⱼ', 'r': 'ᵣ',
	'u': 'ᵤ', 'v': 'ᵥ'}

// scripted writes text as superscript (1) or subscript (2) with the characters Unicode has for it; when it lacks one,
// as ^(text) or _(text) at body size, which reads right with any font.
func scripted(s string, script int) string {
	core := strings.TrimSpace(s)
	if core == "" {
		return s
	}
	lead := s[:strings.Index(s, core)]
	trail := s[len(lead)+len(core):]
	chars, open := supers, "^("
	if script == 2 {
		chars, open = subs, "_("
	}
	var out strings.Builder
	for _, r := range core {
		m, ok := chars[r]
		if !ok {
			return lead + open + core + ")" + trail
		}
		out.WriteRune(m)
	}
	return lead + out.String() + trail
}

// fragment is where in the page a link that is only a fragment leads: the id, top for the top.
func fragment(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "#") {
		return "", false
	}
	id, err := url.PathUnescape(raw[1:])
	if err != nil {
		id = raw[1:]
	}
	if id == "" {
		return top, true
	}
	return id, true
}

// isBack tells the arrow that leads from a footnote back to the text.
func isBack(n *html.Node) bool {
	if attr(n, "role") == "doc-backlink" || contains(strings.Fields(attr(n, "class")), "footnote-backref") {
		return true
	}
	switch strings.TrimSpace(strings.ReplaceAll(raw(n), "\ufe0e", "")) {
	case "↩", "↵":
		return true
	}
	return false
}

// onlySup tells a link whose text is a superscript.
func onlySup(n *html.Node) bool {
	c := n.FirstChild
	return c != nil && c.NextSibling == nil && c.Type == html.ElementNode && c.DataAtom == atom.Sup
}

// isNotes tells the element that holds the notes at the end of the text.
func isNotes(n *html.Node) bool {
	return attr(n, "role") == "doc-endnotes" || contains(strings.Fields(attr(n, "class")), "footnotes")
}

// collect adds the ids and names of an element to those waiting for a block; an element that is a block of its own
// ends the text before, so the id leads to where it starts.
func (b *builder) collect(n *html.Node) []string {
	id := attr(n, "id")
	if id == "" && n.DataAtom == atom.A {
		id = attr(n, "name")
	}
	if id == "" {
		return b.ids
	}
	switch a := n.DataAtom; {
	case blockLevel(a) || a == atom.H1 || a == atom.H2 || a == atom.H3 || a == atom.H4 || a == atom.H5 ||
		a == atom.H6 || a == atom.Li || a == atom.Ul || a == atom.Ol || a == atom.Pre || a == atom.Blockquote ||
		a == atom.Tr || a == atom.Hr || a == atom.Img:
		b.flush(paragraph, 0)
	}
	return append(b.ids, id)
}

// attach gives a block the ids waiting for it, and the footnote it is part of.
func (b *builder) attach(bl block) block {
	bl.ids, b.ids = b.ids, nil
	bl.notes, bl.note = b.notes, b.noteAt
	return bl
}

// grid reads a table of a head and rows of plain cells as a grid; it is nil for any other table, which is read as a
// line of text a row.
func (b *builder) grid(n *html.Node) *grid {
	var trs []*html.Node
	var capt, headRow *html.Node
	var collect func(*html.Node, bool)
	collect = func(p *html.Node, inHead bool) {
		for c := p.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.DataAtom {
			case atom.Caption:
				if capt == nil {
					capt = c
				}
			case atom.Tr:
				if inHead && headRow == nil {
					headRow = c
				}
				trs = append(trs, c)
			case atom.Thead, atom.Tbody, atom.Tfoot:
				collect(c, c.DataAtom == atom.Thead)
			}
		}
	}
	collect(n, false)
	if len(trs) < 2 {
		return nil
	}
	var ids []string
	read := func(c *html.Node) ([]span, bool) {
		if complex(c) {
			return nil, false
		}
		sub := &builder{base: b.base, space: true}
		sub.children(c, style{})
		sub.flush(paragraph, 0)
		var spans []span
		for _, bl := range sub.blocks {
			if bl.kind != paragraph || bl.quote != 0 || bl.depth != 0 {
				return nil, false
			}
			if len(spans) > 0 {
				spans = append(spans, span{text: " "})
			}
			spans = append(spans, bl.spans...)
		}
		ids = append(ids, sub.ids...)
		for _, bl := range sub.blocks {
			ids = append(ids, bl.ids...)
		}
		return spans, true
	}
	g := &grid{}
	covered := map[[2]int]bool{}
	for r, tr := range trs {
		var row []cell
		col := 0
		pad := func() {
			for covered[[2]int{r, col}] {
				row = append(row, cell{span: 1, align: -1})
				col++
			}
		}
		for c := tr.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode || c.DataAtom != atom.Td && c.DataAtom != atom.Th {
				continue
			}
			pad()
			spans, ok := read(c)
			if !ok {
				return nil
			}
			cl := cell{spans: spans, span: 1, head: c.DataAtom == atom.Th, align: cellAlign(c)}
			if k, err := strconv.Atoi(attr(c, "colspan")); err == nil && k > 1 {
				cl.span = min(k, 64)
			}
			if k, err := strconv.Atoi(attr(c, "rowspan")); err == nil && k > 1 {
				for dr := 1; dr < min(k, 256); dr++ {
					for dc := 0; dc < cl.span; dc++ {
						covered[[2]int{r + dr, col + dc}] = true
					}
				}
			}
			row = append(row, cl)
			col += cl.span
		}
		pad()
		g.rows = append(g.rows, row)
		g.cols = max(g.cols, col)
	}
	if g.cols < 2 {
		return nil
	}
	allTh := len(g.rows[0]) > 0
	for _, cl := range g.rows[0] {
		allTh = allTh && cl.head
	}
	g.head = allTh || headRow == trs[0]
	for r, row := range g.rows {
		w := 0
		for _, cl := range row {
			w += cl.span
		}
		for ; w < g.cols; w++ {
			row = append(row, cell{span: 1, align: -1})
		}
		g.rows[r] = row
		for k := range row {
			row[k].head = g.head && r == 0
		}
	}
	g.numbers()
	if capt != nil {
		if spans, ok := read(capt); ok {
			g.caption = trimSpans(spans)
		}
	}
	b.ids = append(b.ids, ids...)
	return g
}

var figure = regexp.MustCompile(`^[+\-−]?[$€£¥]?\d[\d,.]*\s?%?$`)

// numbers finds the columns of numbers, which align to the end with figures of one width, and settles the
// alignment of every cell.
func (g *grid) numbers() {
	for c := 0; c < g.cols; c++ {
		count, only := 0, true
		for r, row := range g.rows {
			if g.head && r == 0 {
				continue
			}
			at := 0
			for _, cl := range row {
				if at <= c && c < at+cl.span {
					text := plain(cl.spans)
					switch {
					case text == "" || text == "-" || text == "—":
					case cl.span == 1 && figure.MatchString(text):
						count++
					default:
						only = false
					}
				}
				at += cl.span
			}
		}
		if count == 0 || !only {
			continue
		}
		for r, row := range g.rows {
			at := 0
			for k := range row {
				if at == c && row[k].span == 1 {
					row[k].number = !(g.head && r == 0)
					if row[k].align < 0 {
						row[k].align = alignEnd
					}
				}
				at += row[k].span
			}
		}
	}
	for _, row := range g.rows {
		for k := range row {
			row[k].align = max(row[k].align, alignStart)
		}
	}
}

var textAlign = regexp.MustCompile(`text-align\s*:\s*(left|start|center|right|end)`)

// cellAlign is the alignment a cell asks for, -1 for none.
func cellAlign(n *html.Node) int {
	v := strings.ToLower(strings.TrimSpace(attr(n, "align")))
	if m := textAlign.FindStringSubmatch(strings.ToLower(attr(n, "style"))); m != nil {
		v = m[1]
	}
	switch v {
	case "left", "start":
		return alignStart
	case "center":
		return alignCenter
	case "right", "end":
		return alignEnd
	}
	return -1
}

// complex tells a cell with more than text in it: a picture, a list, a quote, a heading, another table.
func complex(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		switch c.DataAtom {
		case atom.Img, atom.Pre, atom.Ul, atom.Ol, atom.Li, atom.Dl, atom.Blockquote, atom.H1, atom.H2, atom.H3,
			atom.H4, atom.H5, atom.H6, atom.Hr, atom.Figure, atom.Table:
			return true
		}
		if complex(c) {
			return true
		}
	}
	return false
}

// add puts a span after the text so far, joining it to the last one when they are in the same style.
func (b *builder) add(sp span) {
	if k := len(b.spans) - 1; k >= 0 {
		last := &b.spans[k]
		if last.bold == sp.bold && last.italic == sp.italic && last.mono == sp.mono && last.gone == sp.gone &&
			last.link == sp.link && last.jump == sp.jump && last.back == sp.back && last.ref == 0 && sp.ref == 0 {
			last.text += sp.text
			return
		}
	}
	b.spans = append(b.spans, sp)
}

// flush ends the text so far as a block of kind, unless it is only white space.
func (b *builder) flush(kind, level int) {
	spans := trimSpans(b.spans)
	b.spans, b.space = nil, true
	if len(spans) == 0 {
		return
	}
	b.blocks = append(b.blocks, b.mark(block{kind: kind, level: level, spans: spans}))
}

// trimSpans drops the white space at both ends of the text; it is nil when nothing else is left.
func trimSpans(spans []span) []span {
	for len(spans) > 0 && strings.TrimSpace(spans[0].text) == "" {
		spans = spans[1:]
	}
	for len(spans) > 0 && strings.TrimSpace(spans[len(spans)-1].text) == "" {
		spans = spans[:len(spans)-1]
	}
	if len(spans) == 0 {
		return nil
	}
	spans[0].text = strings.TrimLeftFunc(spans[0].text, unicode.IsSpace)
	last := &spans[len(spans)-1]
	last.text = strings.TrimRightFunc(last.text, unicode.IsSpace)
	return spans
}

// mark places a block in the quotes and lists it is in, giving it the marker of a list item it opens.
func (b *builder) mark(bl block) block {
	bl = b.attach(bl)
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
	// Pictures of a pixel or two, hidden ones and those of known trackers count visits; they are not part of the
	// article, and nothing asks for them.
	if src == "" || tracker.Counts(n, src) {
		return
	}
	b.flush(paragraph, 0)
	b.blocks = append(b.blocks, b.mark(block{kind: picture, src: src, alt: strings.TrimSpace(attr(n, "alt")),
		width: width, height: height}))
}

// embed puts one row in the place of a video, audio, iframe, object or embed element: the reader plays nothing and
// loads no page, but says what is here. Hidden elements get none, and what an element holds as a fallback is not
// shown.
func (b *builder) embed(n *html.Node) {
	if tracker.Hidden(n) {
		return
	}
	kind := embedOther
	switch n.DataAtom {
	case atom.Video:
		kind = embedVideo
	case atom.Audio:
		kind = embedAudio
	}
	b.flush(paragraph, 0)
	bl := b.attach(block{kind: embed, level: kind, alt: firstLine(attr(n, "title"))})
	bl.quote, bl.depth = b.quote, len(b.lists)
	b.blocks = append(b.blocks, bl)
}

// textual reports whether blocks hold anything but embed rows, that is, something to read.
func textual(blocks []block) bool {
	return slices.ContainsFunc(blocks, func(b block) bool { return b.kind != embed })
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
