package reader

import (
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/ui/theme"
	"github.com/yangflow/oriel/internal/ui/typeset"
	"golang.org/x/text/unicode/norm"
)

// maxHits is where counting matches stops, so a one-letter query in a long article stays cheap.
const maxHits = 1000

// The units of text that are searched, in the order they are drawn: the title, the summary card, then the blocks.
const (
	unitTitle   = 0
	unitSummary = 1
	unitBlocks  = 2
)

// hit is a match: its unit and the runes [from, to) of that unit's text.
type hit struct{ unit, from, to int }

// mark is a match as a part of one text sees it, in runes within that text, current for the one the bar is on.
type mark struct {
	from, to int
	current  bool
}

// findSig is what the matches are for: when it changes they are worked out anew, and only then.
type findSig struct {
	query, title, summary string
	blocks                *block
	n, gen                int
}

// find is the find row of the reader: see docs/design.md "Find in the article".
type find struct {
	open  bool
	query string
	cur   int // the match the bar is on
	was   bool

	sig     findSig
	ok      bool
	hits    []hit
	byUnit  map[int][]int // the indices into hits of each unit
	version int           // counts the times the matches were worked out
	seen    [2]int        // the version and match the view was last scrolled to
}

// openFind shows the find row. It does nothing while there is no text to search.
func (s *Screen) openFind() {
	if !s.Ready() {
		return
	}
	s.find.open = true
}

// closeFind hides the row and its highlight, the focus going back to the article.
func (s *Screen) closeFind() {
	s.find.open = false
}

// findRefresh works the matches out when the query or what is shown has changed since the last frame.
func (s *Screen) findRefresh(title string, blocks []block) {
	f := &s.find
	if !f.open || f.query == "" {
		f.hits, f.byUnit, f.ok, f.sig = nil, nil, false, findSig{}
		return
	}
	sig := findSig{query: f.query, title: title, summary: s.summaryText(), n: len(blocks), gen: s.gen}
	if len(blocks) > 0 {
		sig.blocks = &blocks[0]
	}
	if f.ok && f.sig == sig {
		return
	}
	f.sig, f.ok = sig, true
	f.version++
	f.hits, f.byUnit = nil, map[int][]int{}
	query := fold([]rune(f.query))
	if len(query.runes) == 0 {
		return
	}
	units := []string{title, sig.summary}
	for _, b := range blocks {
		units = append(units, blockText(b))
	}
	for u, text := range units {
		for _, m := range search(text, query.runes, maxHits-len(f.hits)) {
			f.byUnit[u] = append(f.byUnit[u], len(f.hits))
			f.hits = append(f.hits, hit{u, m[0], m[1]})
		}
		if len(f.hits) >= maxHits {
			break
		}
	}
	f.cur = max(0, min(f.cur, len(f.hits)-1))
}

// summaryText is the text of the summary card while it shows a summary, "" otherwise.
func (s *Screen) summaryText() string {
	if j := s.jobs[summary]; j.on && !j.working && !j.failed && !s.aiOff {
		return strings.TrimSpace(j.text)
	}
	return ""
}

// blockText is the text of a block as it is drawn, its spans one after another.
func blockText(b block) string {
	if b.kind == code {
		return b.text
	}
	var sb strings.Builder
	for _, sp := range b.spans {
		sb.WriteString(sp.text)
	}
	return sb.String()
}

// folded is a text with its case and accents taken off, and for each of its runes the rune of the text it came from.
type folded struct {
	runes []rune
	from  []int
}

// fold takes the case and the accents off text, so that "resume" finds "Résumé".
func fold(text []rune) folded {
	f := folded{runes: make([]rune, 0, len(text)), from: make([]int, 0, len(text))}
	for i, r := range text {
		if r < 0x80 {
			f.runes, f.from = append(f.runes, unicode.ToLower(r)), append(f.from, i)
			continue
		}
		for _, d := range norm.NFD.String(string(r)) {
			if !unicode.Is(unicode.Mn, d) {
				f.runes, f.from = append(f.runes, unicode.ToLower(d)), append(f.from, i)
			}
		}
	}
	return f
}

// search is the matches of query, already folded, in text, as [from, to) runes of text and at most limit of them.
func search(text string, query []rune, limit int) [][2]int {
	if text == "" || limit <= 0 {
		return nil
	}
	orig := []rune(text)
	f := fold(orig)
	var out [][2]int
	for i := 0; i+len(query) <= len(f.runes) && len(out) < limit; {
		if !equal(f.runes[i:i+len(query)], query) {
			i++
			continue
		}
		out = append(out, [2]int{f.from[i], f.from[i+len(query)-1] + 1})
		i += len(query)
	}
	return out
}

func equal(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// marks is the matches of a unit that fall in the runes [base, base+n) of its text, as offsets in that part.
func (f *find) marks(unit, base, n int) []mark {
	if !f.open || len(f.byUnit[unit]) == 0 {
		return nil
	}
	var out []mark
	for _, k := range f.byUnit[unit] {
		h := f.hits[k]
		if h.to <= base || h.from >= base+n {
			continue
		}
		out = append(out, mark{max(h.from-base, 0), min(h.to-base, n), k == f.cur})
	}
	return out
}

// current is the unit of the match the bar is on, -1 for none.
func (f *find) current() int {
	if !f.open || f.cur >= len(f.hits) {
		return -1
	}
	return f.hits[f.cur].unit
}

// reveal says that the match the bar is on has not been scrolled to yet, and that it now has been.
func (f *find) reveal() bool {
	if f.current() < 0 || f.seen == [2]int{f.version, f.cur} {
		return false
	}
	f.seen = [2]int{f.version, f.cur}
	return true
}

// part is a piece of a text, drawn as one Text: em its letter spacing as typeset gave it, state 0 for plain, 1 for a
// match and 2 for the current match.
type part struct {
	text  string
	em    float32
	state int
}

// cut splits pieces where marks begin and end.
func cut(pieces []typeset.Piece, marks []mark) []part {
	var out []part
	pos := 0
	for _, p := range pieces {
		runes := []rune(p.Text)
		for i := 0; i < len(runes); {
			state, end := 0, len(runes)
			for _, m := range marks {
				switch {
				case pos+i >= m.from && pos+i < m.to:
					state, end = 1, min(end, m.to-pos)
					if m.current {
						state = 2
					}
				case m.from > pos+i:
					end = min(end, m.from-pos)
				}
			}
			out = append(out, part{string(runes[i:end]), p.Em, state})
			i = end
		}
		pos += len(runes)
	}
	return out
}

// mark gives a match its look: the Warning tint over the page, the current one deeper and underlined, and the text
// in the Text color whatever it was, for its contrast on the tint.
func (s *Screen) mark(c *ui.Context, e ui.Element, state int) {
	t := c.Theme()
	switch state {
	case 1:
		e.TextColor(t.Text).TextBackground(t.Warning.Alpha(0.25))
	case 2:
		e.TextColor(t.Text).TextBackground(t.Warning.Alpha(0.4)).Underline()
	}
}

// pieceList is the pieces a text is drawn in: the typeset ones when the text is Chinese and typesetting is on, else it.
func (s *Screen) pieceList(m *memo, text string, chinese bool) []typeset.Piece {
	if chinese && s.reading().ChineseTypesetting {
		if pieces := m.of(text); pieces != nil {
			return pieces
		}
	}
	return []typeset.Piece{{Text: text}}
}

// findRow is the find row, under the toolbar: Background, the height of findHeight, its bar lined up with the article
// column, with a hairline under it. The article is padded down by its height, so nothing is hidden.
func (s *Screen) findRow(c *ui.Context, narrow bool) {
	t := c.Theme()
	f := &s.find
	if !f.open {
		return
	}
	inset := theme.Px(t, 28)
	if narrow {
		inset = theme.Px(t, 12)
	}
	ui.Row(c).Key("find-row").Absolute().Top(theme.Bar).Left(0).Right(0).Height(findHeight(t)).Background(t.Background).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.FindBar(c, &f.open, &f.query, len(f.hits), &f.cur).FillWidth().MaxWidth(s.column(t)).Margin(0, ui.Auto).
			Height(findHeight(t)).Padding(0, inset).Background(t.Background).BorderWidth(0, 0, 0, 0).
			Label(s.text.FindLabel)
	})
}

// findHeight is the height of the find row.
func findHeight(t *ui.Theme) float32 { return theme.Px(t, 44) }

// revealBlock puts a marker in the block of unit that reaches up under the toolbar and the find row, and has it scrolled
// into view when the block holds the match the bar has just gone to: the match comes to rest below the bars, not behind.
func (s *Screen) revealBlock(c *ui.Context, unit int) {
	if s.find.current() != unit || !s.find.reveal() {
		return
	}
	t := c.Theme()
	up := theme.Bar + findHeight(t) + theme.Px(t, 16)
	ui.Box(c).Key("find-reveal").Absolute().Top(-up).Left(0).Right(0).Bottom(0).PassThrough().ScrollIntoView()
}
