package reader

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

func parsed(source string) []block {
	base, _ := url.Parse("https://example.com/posts/1")
	return parse(source, base, "Hello")
}

func TestScripts(t *testing.T) {
	for _, c := range []struct{ html, want string }{
		{"<p>H<sub>2</sub>O</p>", "H₂O"},
		{"<p>m<sup>2</sup></p>", "m²"},
		{"<p>x<sup>n+1</sup></p>", "xⁿ⁺¹"},
		{"<p>x<sup>abc!</sup></p>", "x^(abc!)"},
		{"<p>a<sub>xyz</sub></p>", "a_(xyz)"},
		{"<p>a<sub>1 2</sub></p>", "a_(1 2)"},
	} {
		if got := plain(parsed(c.html)[0].spans); got != c.want {
			t.Errorf("%s: got %q, want %q", c.html, got, c.want)
		}
	}
}

const noted = `<p>One<sup><a href="#fn1" id="r1">1</a></sup> two<a href="#fn2" id="r2" role="doc-noteref">[2]</a>
 gone<sup><a href="#nowhere">3</a></sup> <a href="#sec">section</a> <a href="#">up</a> <a href="#top">top</a>.</p>
<h2 id="sec">Section</h2>
<section class="footnotes" role="doc-endnotes"><hr><ol>
<li id="fn1"><p>First <a href="#r1" role="doc-backlink">↩</a></p></li>
<li id="fn2"><p>Second</p><p>more</p></li></ol></section>`

func TestFootnotes(t *testing.T) {
	blocks := parsed(noted)
	var refs, jumps []string
	for _, sp := range blocks[0].spans {
		if sp.ref != 0 {
			refs = append(refs, sp.text)
			if sp.ref < 0 {
				t.Errorf("reference %q is still a candidate", sp.text)
			}
		}
		if sp.jump != "" {
			jumps = append(jumps, sp.jump)
		}
	}
	if strings.Join(refs, " ") != "¹ ²" {
		t.Errorf("references %q, want the first two", refs)
	}
	if strings.Join(jumps, " ") != "fn1 fn2 sec # #" {
		t.Errorf("jumps %q: the unresolved one stays text, # and #top go to the top", jumps)
	}
	if sp := blocks[0].spans; !strings.Contains(plain(sp), "³") {
		t.Errorf("the unresolved reference lost its text in %q", plain(sp))
	}
	var notes []block
	for _, b := range blocks {
		if b.note > 0 {
			notes = append(notes, b)
		}
	}
	if len(notes) != 3 || notes[0].note != 1 || notes[1].note != 2 || notes[2].note != 2 || notes[0].marker != "1." {
		t.Fatalf("notes %+v", notes)
	}
	backs := func(b block) (n int, jump string) {
		for _, sp := range b.spans {
			if sp.back {
				n++
				jump = sp.jump
			}
		}
		return
	}
	if n, jump := backs(notes[0]); n != 1 || jump != "r1" {
		t.Errorf("first note: %d arrows leading to %q, want its own", n, jump)
	}
	if n, jump := backs(notes[2]); n != 1 || jump != "" {
		t.Errorf("second note: %d arrows leading to %q, want one appended after the last block", n, jump)
	}
	if n, _ := backs(notes[1]); n != 0 {
		t.Errorf("an arrow in the middle of a note")
	}
	if blocks[len(blocks)-4].kind != rule || !blocks[len(blocks)-4].notes {
		t.Errorf("the notes do not start with their rule")
	}
}

func table370(t *testing.T, source string) *grid {
	t.Helper()
	for _, b := range parsed(source) {
		if b.kind == table {
			return b.table
		}
	}
	return nil
}

func TestTables(t *testing.T) {
	g := table370(t, `<table><caption>Sizes</caption><thead><tr><th>Name</th><th align="center">Size</th><th>N</th></tr></thead>
<tr><td>a</td><td>1.5</td><td align="right">1,000</td></tr><tr><td colspan="2">b <b>bold</b></td><td>2</td></tr></table>`)
	if g == nil {
		t.Fatal("no grid")
	}
	if !g.head || g.cols != 3 || len(g.rows) != 3 || plain(g.caption) != "Sizes" {
		t.Fatalf("grid %+v", g)
	}
	if !g.rows[0][0].head || g.rows[1][0].head {
		t.Errorf("header cells")
	}
	if g.rows[0][1].align != alignCenter || g.rows[1][2].align != alignEnd {
		t.Errorf("alignments %d %d", g.rows[0][1].align, g.rows[1][2].align)
	}
	if g.rows[2][0].span != 2 || len(g.rows[2]) != 2 || plain(g.rows[2][0].spans) != "b bold" {
		t.Errorf("colspan: %+v", g.rows[2])
	}
	if !g.rows[1][2].number || g.rows[1][0].number {
		t.Errorf("numeric columns: %+v", g.rows[1])
	}
	g = table370(t, `<table><tr><th>Name</th><th>N</th></tr><tr><td>a</td><td>1.5</td></tr><tr><td>b</td><td>-2</td></tr></table>`)
	if g == nil || !g.rows[1][1].number || !g.rows[2][1].number || g.rows[1][0].number || g.rows[0][1].number {
		t.Errorf("numeric column: %+v", g)
	}
	g = table370(t, `<table><tr><th>a</th><th>b</th></tr><tr><td rowspan="2">x</td><td>y</td></tr><tr><td>z</td></tr></table>`)
	if g == nil || len(g.rows[2]) != 2 || plain(g.rows[2][0].spans) != "" || plain(g.rows[2][1].spans) != "z" {
		t.Errorf("rowspan leaves the covered position empty: %+v", g)
	}
	for name, source := range map[string]string{
		"one row":     `<table><tr><td>a</td><td>b</td></tr></table>`,
		"one column":  `<table><tr><td>a</td></tr><tr><td>b</td></tr></table>`,
		"block cells": `<table><tr><td><ul><li>a</li></ul></td><td>b</td></tr><tr><td>c</td><td>d</td></tr></table>`,
		"nested":      `<table><tr><td><table><tr><td>a</td></tr></table></td><td>b</td></tr><tr><td>c</td><td>d</td></tr></table>`,
		"picture":     `<table><tr><td><img src="a.png"></td><td>b</td></tr><tr><td>c</td><td>d</td></tr></table>`,
	} {
		if table370(t, source) != nil {
			t.Errorf("%s is a data table", name)
		}
	}
}

func footnoted(language string) (*Screen, *fake, *ui.Tester) {
	a := full()
	a.ExtractedHTML = noted + strings.Repeat("<p>Filler text that makes the page longer than the window.</p>", 30)
	return setupLong(language, a)
}

func setupLong(language string, a client.Article) (*Screen, *fake, *ui.Tester) {
	f := &fake{now: now, article: a, nav: Nav{Entry: a.Entry, Scope: "All", Previous: true, Next: true, Keys: true}}
	s := New(language, f.deps())
	s.Use(f)
	tt := ui.NewTester(func(c *ui.Context) { s.View(c, f.nav) }, 1100, 800)
	s.Open(a.Entry)
	tt.Frame()
	f.settle(tt)
	return s, f, tt
}

func TestFootnoteNamesAndJump(t *testing.T) {
	for language, names := range map[string][3]string{
		"en":      {"Footnote 1", "Footnote 2", "Back to the text"},
		"zh-Hans": {"脚注 1", "脚注 2", "回到正文"},
	} {
		s, f, tt := footnoted(language)
		expect(t, tt, names[0], names[1], names[2])
		click(t, tt, names[1])
		tt.Frame()
		if !s.landing.on || s.landing.ix == 0 {
			t.Fatalf("%s: no landing %+v", language, s.landing)
		}
		f.wait(tt, lit-100*time.Millisecond)
		if !s.landing.on {
			t.Fatalf("%s: the highlight ended early", language)
		}
		f.wait(tt, 200*time.Millisecond)
		if s.landing.on {
			t.Fatalf("%s: the highlight stays", language)
		}
	}
}

func TestFootnoteHighlightLastsWithoutFade(t *testing.T) {
	s, f, tt := footnoted("en")
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	click(t, tt, "Footnote 1")
	tt.Frame()
	if !s.landing.on {
		t.Fatal("no highlight under reduced motion")
	}
	f.wait(tt, lit/2)
	if !s.landing.on {
		t.Fatal("the highlight ended early")
	}
	f.wait(tt, lit)
	if s.landing.on {
		t.Fatal("the highlight stays")
	}
}

func TestFootnoteBackReturnsToLastReference(t *testing.T) {
	s, f, tt := footnoted("en")
	first := s.drawn[0].ix
	click(t, tt, "Footnote 2")
	tt.Frame()
	if !s.landing.on || s.landing.ix == first {
		t.Fatalf("the reference did not lead to the note: %+v", s.landing)
	}
	f.wait(tt, 2*lit)
	// The arrow of the second note is the one added after its last block, as the page gave none.
	backs := 0
	for _, b := range s.drawn {
		if b.note == 2 {
			for _, sp := range b.spans {
				if sp.back {
					backs++
				}
			}
		}
	}
	if backs != 1 {
		t.Fatalf("%d arrows in the second note", backs)
	}
	click(t, tt, "Back to the text")
	tt.Frame()
	if !s.landing.on || s.landing.ix != first {
		t.Fatalf("back did not lead to the reference: %+v, want block %d", s.landing, first)
	}
}

func TestWideTableBar(t *testing.T) {
	wide := strings.Repeat("one two three four five six seven eight ", 3)
	a := full()
	a.ExtractedHTML = `<table><tr>` + strings.Repeat("<th>H</th>", 5) + `</tr><tr>` + strings.Repeat("<td>"+wide+"</td>", 5) +
		`</tr></table>`
	_, f, tt := setup(t, "en", a)
	f.settle(tt)
	tt.Frame()
	r, ok := tt.Find("Table, 5 columns, 2 rows")
	if !ok {
		t.Fatal("no table")
	}
	y := int(r.Y + r.H + 7)
	left, right := int(r.X+12), int(r.X+r.W-12)
	dark := func(x int) bool {
		c := tt.Image().RGBAAt(x, y)
		return int(c.R)+int(c.G)+int(c.B) < 3*200
	}
	if !dark(left) || dark(right) {
		t.Errorf("at the start the thumb is at the left: %v %v", dark(left), dark(right))
	}
	tt.Scroll(r.X+r.W/2, r.Y+r.H/2, 5000, 0)
	tt.Frame()
	tt.Frame()
	if dark(left) || !dark(right) {
		t.Errorf("at the end the thumb is at the right: %v %v", dark(left), dark(right))
	}
}

func TestTopLinks(t *testing.T) {
	s, _, tt := footnoted("en")
	click(t, tt, "up")
	tt.Frame()
	if s.landing.on {
		t.Error("a link to the top highlights a block")
	}
}

// stops counts the presses of the Tab key after which the element labeled name, or what holds it, has the focus.
func stops(tt *ui.Tester, name string) int {
	n := 0
	for range 80 {
		tt.Key(0, ui.KeyTab)
		if tt.Focused(name) {
			n++
		}
	}
	return n
}

func TestTableAccessibility(t *testing.T) {
	wide := strings.Repeat("one two three four five six seven eight ", 3)
	cells := func(text string, n int) string { return strings.Repeat("<td>"+text+"</td>", n) }
	heads := func(n int) string { return strings.Repeat("<th>H</th>", n) }
	counts := map[string]int{}
	for name, html := range map[string]string{
		"narrow": `<table><tr>` + heads(2) + `</tr><tr>` + cells("x", 2) + `</tr></table>`,
		"wide":   `<table><tr>` + heads(5) + `</tr><tr>` + cells(wide, 5) + `</tr></table>`,
	} {
		a := full()
		a.ExtractedHTML = html
		_, f, tt := setup(t, "en", a)
		f.settle(tt)
		tt.Frame()
		label := map[string]string{"narrow": "Table, 2 columns, 2 rows", "wide": "Table, 5 columns, 2 rows"}[name]
		expect(t, tt, label)
		if r, _ := tt.Find(label); name == "narrow" && r.W > 300 {
			t.Errorf("a narrow table does not hug its content: %+v", r)
		}
		counts[name] = stops(tt, label)
	}
	if counts["wide"] <= counts["narrow"] {
		t.Errorf("stops of the Tab key on a table: %d wide, %d narrow; only the wide one takes the focus", counts["wide"],
			counts["narrow"])
	}
}

func TestTableChinese(t *testing.T) {
	a := full()
	a.ExtractedHTML = `<table><tr><th>A</th><th>B</th></tr><tr><td>x</td><td>y</td></tr></table>`
	_, f, tt := setup(t, "zh-Hans", a)
	f.settle(tt)
	expect(t, tt, "表格，2 列 2 行")
}

func TestMarkdownTablesAndFootnotes(t *testing.T) {
	a := full()
	a.ExtractedHTML = `<p>One<sup><a href="#fn1" id="r1">1</a></sup> and H<sub>2</sub>O.</p>
<table><caption>Sizes</caption><tr><th>Name</th><th>Size</th></tr><tr><td>a|b</td><td>line<br>two</td></tr><tr><td colspan="2">wide</td></tr></table>
<table><tr><th>Name</th><th>Size</th></tr></table>
<section class="footnotes" role="doc-endnotes"><hr><ol>
<li id="fn1"><p>First <a href="#r1" role="doc-backlink">↩</a></p><p>more</p></li></ol></section>`
	a.Author = ""
	got := markdownOf(a)
	want := "One[^1] and H₂O.\n\n" +
		"| Name | Size |\n| --- | --- |\n| a\\|b | line two |\n| wide |  |\n\n*Sizes*\n\n" +
		"**Name** · **Size**\n\n" +
		"[^1]: First\n\n    more\n"
	if !strings.HasSuffix(got, want) {
		t.Fatalf("got\n%s\nwant it to end with\n%s", got, want)
	}
}
