package reader

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// findSetup is a reader with its text loaded, and the theme the tester draws with.
func findSetup(t *testing.T, language string) (*Screen, *fake, *ui.Tester, **ui.Theme) {
	t.Helper()
	article := full()
	f := &fake{now: now, article: article, nav: Nav{Entry: article.Entry, Scope: "All", Previous: true, Next: true, Keys: true}}
	s := New(language, f.deps())
	s.Use(f)
	var th *ui.Theme
	tt := ui.NewTester(func(c *ui.Context) { th = c.Theme(); s.View(c, f.nav) }, 1100, 1600)
	s.Open(article.Entry)
	tt.Frame()
	return s, f, tt, &th
}

func TestFindOpensOnlyOnceTheArticleIsLoaded(t *testing.T) {
	s, f, tt, _ := findSetup(t, "en")
	tt.Key(ui.Cmd, ui.KeyF)
	if s.find.open {
		t.Fatal("find opens while the article is loading")
	}
	f.settle(tt)
	tt.Key(ui.Cmd, ui.KeyF)
	if !s.find.open || !tt.HasText("Done") {
		t.Fatalf("find does not open: %v", tt.Texts())
	}
	if len(f.calls) != 0 {
		t.Fatalf("calls %v: Cmd+F left the reader", f.calls)
	}
}

func TestFindStepsAndWraps(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		t.Run(language, func(t *testing.T) {
			s, f, tt, _ := findSetup(t, language)
			f.settle(tt)
			tt.Key(ui.Cmd, ui.KeyF)
			tt.Type("go")
			tt.Frame()
			n := len(s.find.hits)
			if n < 3 {
				t.Fatalf("%d matches of go", n)
			}
			if s.find.cur != 0 {
				t.Fatalf("current %d, want the first", s.find.cur)
			}
			tt.Key(ui.Shift, ui.KeyEnter)
			if s.find.cur != n-1 {
				t.Fatalf("Shift+Enter from the first goes to %d, want %d", s.find.cur, n-1)
			}
			tt.Key(0, ui.KeyEnter)
			if s.find.cur != 0 {
				t.Fatalf("Enter from the last goes to %d, want the first", s.find.cur)
			}
			for i := 1; i < n; i++ {
				tt.Key(0, ui.KeyEnter)
				if s.find.cur != i {
					t.Fatalf("Enter %d goes to %d", i, s.find.cur)
				}
			}
			if len(f.calls) != 0 {
				t.Fatalf("calls %v: the field's keys reached the reader", f.calls)
			}
		})
	}
}

func TestFindHighlightsAndCounts(t *testing.T) {
	s, f, tt, _ := findSetup(t, "en")
	f.settle(tt)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("faster")
	tt.Frame()
	if len(s.find.hits) != 1 || !tt.HasText("1 of 1") {
		t.Fatalf("hits %v, texts %v", s.find.hits, tt.Texts())
	}
	tt.Type("zzz")
	tt.Frame()
	if len(s.find.hits) != 0 || !tt.HasText("No matches") {
		t.Fatalf("hits %v, texts %v", s.find.hits, tt.Texts())
	}
	// Typing keys of the reader into the field does not run them.
	tt.Type("jksmoz")
	tt.Frame()
	if len(f.calls) != 0 {
		t.Fatalf("calls %v: typing in the field ran the reader's keys", f.calls)
	}
}

func TestFindSearchesTheViewShown(t *testing.T) {
	s, f, tt, _ := findSetup(t, "en")
	f.settle(tt)
	f.reply = client.AIResult{Status: "pending"}
	click(t, tt, "Translate")
	f.settle(tt)
	f.ai.Translation = &client.AIResult{Status: "ok", Text: "Go 1.27 ist da\n\nHeute erscheint Go 1.27.\nSchneller.\n", Model: "m"}
	f.wait(tt, aiPollEvery)
	f.settle(tt)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("schneller")
	tt.Frame()
	if len(s.find.hits) != 1 {
		t.Fatalf("translation: hits %v", s.find.hits)
	}
	// The original has no such word: the search runs again on the text now shown.
	click(t, tt, "Original")
	tt.Frame()
	if len(s.find.hits) != 0 || !tt.HasText("No matches") {
		t.Fatalf("original: hits %v, texts %v", s.find.hits, tt.Texts())
	}
	click(t, tt, "Translation")
	tt.Frame()
	if len(s.find.hits) != 1 {
		t.Fatalf("translation again: hits %v", s.find.hits)
	}
}

func TestFindEscapeComesFirst(t *testing.T) {
	s, f, tt, _ := findSetup(t, "en")
	f.settle(tt)
	f.selReply = "An answer."
	askAbout(t, tt, "Translate selection")
	f.settle(tt)
	f.nav.Immersive = true
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyF)
	if !s.find.open {
		t.Fatal("find is not open")
	}
	tt.Key(0, ui.KeyEscape)
	if s.find.open || !tt.HasText("An answer.") || len(f.calls) != 0 {
		t.Fatalf("Escape: open %v, calls %v", s.find.open, f.calls)
	}
	// Done closes it too.
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Click("Done")
	if s.find.open {
		t.Fatal("Done does not close it")
	}
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	absent(t, tt, "An answer.")
	if len(f.calls) != 0 {
		t.Fatalf("calls %v: the selection card closes before immersive reading", f.calls)
	}
	tt.Key(0, ui.KeyEscape)
	if !slices.Equal(f.calls, []string{"immerse"}) {
		t.Fatalf("calls %v", f.calls)
	}
}

func TestFindClosesWithAnotherArticle(t *testing.T) {
	s, f, tt, _ := findSetup(t, "en")
	f.settle(tt)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("go")
	tt.Frame()
	s.Open(entry())
	tt.Frame()
	if s.find.open || len(s.find.hits) != 0 {
		t.Fatalf("open %v, hits %v", s.find.open, s.find.hits)
	}
}

func TestFindMovesTheArticleDown(t *testing.T) {
	_, f, tt, th := findSetup(t, "en")
	f.settle(tt)
	before, _ := tt.Find("Faster builds")
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Frame()
	after, _ := tt.Find("Faster builds")
	if d, want := after.Y-before.Y, findHeight(*th); d < want-1 || d > want+1 {
		t.Fatalf("the article moved by %v, want %v", d, want)
	}
	if r, ok := tt.Find("Done"); !ok || r.Y < theme.Bar {
		t.Fatalf("Done at %v, %v: not below the toolbar", r, ok)
	}
}

func TestFoldIgnoresCaseAndAccents(t *testing.T) {
	q := fold([]rune("resume"))
	got := search("Résumé of the RESUME, résumé", q.runes, 10)
	want := [][2]int{{0, 6}, {14, 20}, {22, 28}}
	if !slices.Equal(got, want) {
		t.Fatalf("matches %v, want %v", got, want)
	}
	if got := search("aaaa", []rune("a"), 3); len(got) != 3 {
		t.Fatalf("the limit gives %d matches", len(got))
	}
}

func TestFindEscapeFromTheArticle(t *testing.T) {
	s, f, tt, _ := findSetup(t, "en")
	f.settle(tt)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("go")
	tt.Frame()
	// Clicking in the article takes the focus out of the field; Escape then closes the row, not the reader.
	tt.Click("Faster builds")
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if s.find.open || len(f.calls) != 0 {
		t.Fatalf("open %v, calls %v", s.find.open, f.calls)
	}
	tt.Frame()
	if !tt.Focused("Go 1.27 is released") {
		t.Fatal("the focus did not return to the article")
	}
	tt.Key(0, ui.KeyEscape)
	if !slices.Equal(f.calls, []string{"back"}) {
		t.Fatalf("calls %v", f.calls)
	}
}

func TestFindScrollsToTheMatchBelowTheBars(t *testing.T) {
	a := full()
	html := ""
	for i := 0; i < 60; i++ {
		html += "<p>Paragraph " + string(rune('A'+i%26)) + " says nothing at all about it, and goes on for a line or two so that the page is long.</p>"
	}
	a.ExtractedHTML = html + "<p>The needle is here.</p>"
	f := &fake{now: now, article: a, nav: Nav{Entry: a.Entry, Scope: "All", Keys: true}}
	s := New("en", f.deps())
	s.Use(f)
	var th *ui.Theme
	tt := ui.NewTester(func(c *ui.Context) { th = c.Theme(); s.View(c, f.nav) }, 1100, 700)
	s.Open(a.Entry)
	tt.Frame()
	f.settle(tt)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("needle")
	tt.Frame()
	tt.Frame()
	r, ok := tt.Find("needle")
	if !ok {
		r, ok = tt.Find("The needle is here.")
	}
	if !ok {
		t.Fatalf("the match is not on screen: %v", tt.Texts())
	}
	if top := theme.Bar + findHeight(th); r.Y < top {
		t.Fatalf("the match is at %v, behind the bars that end at %v", r.Y, top)
	}
	if r.Y+r.H > 700 {
		t.Fatalf("the match is at %v, below the window", r.Y)
	}
}

func TestFindInChineseTypesetText(t *testing.T) {
	a := full()
	a.ExtractedHTML = "<p>今天 Go 团队发布了 Go 1.27 版本，带来更快的构建。</p>"
	a.Entry.Title = "Go 1.27 发布"
	a.Entry.Language = "zh"
	f := &fake{now: now, article: a, nav: Nav{Entry: a.Entry, Scope: "All", Keys: true}}
	s := New("zh-Hans", f.deps())
	s.Use(f)
	tt := ui.NewTester(func(c *ui.Context) { s.View(c, f.nav) }, 1100, 800)
	s.Open(a.Entry)
	tt.Frame()
	f.settle(tt)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("go")
	tt.Frame()
	if len(s.find.hits) != 3 {
		t.Fatalf("hits %v", s.find.hits)
	}
	tt.Type("zzz")
	tt.Frame()
	if len(s.find.hits) != 0 {
		t.Fatalf("hits %v", s.find.hits)
	}
}

func TestFindCountsTheCellsOfATable(t *testing.T) {
	a := full()
	a.ExtractedHTML = `<p>Intro.</p><table><caption>Sizes of Zebra</caption><tr><th>Name</th><th>Size</th></tr>` +
		`<tr><td>Yak</td><td>Yak Yak</td></tr><tr><td>Ox</td><td>Zebra</td></tr></table>`
	s, f, tt, _ := findSetup(t, "en")
	f.article = a
	s.Open(a.Entry)
	f.settle(tt)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("yak")
	tt.Frame()
	if n := len(s.find.hits); n != 3 {
		t.Fatalf("%d matches of yak in the cells, want 3", n)
	}
	if !tt.HasText("1 of 3") {
		t.Fatalf("the bar does not count them: %v", tt.Texts())
	}
	for _, c := range []struct{ query, count string }{{"zebra", "1 of 2"}, {"ox", "1 of 1"}, {"yak size", "No matches"}, {"size ya", "No matches"}} {
		tt.Key(ui.Cmd, ui.KeyA)
		tt.Type(c.query)
		tt.Frame()
		if !tt.HasText(c.count) {
			t.Errorf("%q: want %q, got %v", c.query, c.count, tt.Texts())
		}
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("ox")
	tt.Frame()
	if r, ok := tt.Find("Ox"); !ok || r.W == 0 {
		t.Fatalf("the match is not drawn: %v", tt.Texts())
	}
}

func TestFindScrollsATableToTheMatch(t *testing.T) {
	long := strings.Repeat("one two three four five six seven eight ", 3)
	a := full()
	a.ExtractedHTML = `<table><tr>` + strings.Repeat("<th>H</th>", 5) + `</tr><tr>` + strings.Repeat("<td>"+long+"</td>", 4) +
		`<td>` + long + `needle</td></tr></table>`
	s, f, tt, _ := findSetup(t, "en")
	f.article = a
	s.Open(a.Entry)
	f.settle(tt)
	tt.Frame()
	frame, ok := tt.Find("Table, 5 columns, 2 rows")
	if !ok {
		t.Fatal("no table")
	}
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("needle")
	for range 4 {
		tt.Frame()
	}
	if len(s.find.hits) != 1 {
		t.Fatalf("%d matches", len(s.find.hits))
	}
	r, ok := tt.Find(long + "needle")
	if !ok {
		t.Fatalf("the cell is not drawn: %v", tt.Texts())
	}
	if r.X < frame.X || r.X+r.W > frame.X+frame.W {
		t.Fatalf("the match at %v..%v is outside the table %v..%v", r.X, r.X+r.W, frame.X, frame.X+frame.W)
	}
	for _, st := range s.tables {
		if st.X < 1 {
			t.Fatalf("the table did not scroll: %v of %v", st.X, st.MaxX)
		}
	}
}
