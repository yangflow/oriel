package reader

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

// embedArticle is an entry whose full text is full and whose feed carries feed.
func embedArticle(status, full, feed string) client.Article {
	a := client.Article{Entry: entry(), ExtractedHTML: full}
	a.ContentHTML, a.ExtractStatus, a.Summary = feed, status, ""
	return a
}

func opened(t *testing.T, a client.Article) (*fake, *ui.Tester) {
	t.Helper()
	_, f, tt := setup(t, "en", a)
	f.settle(tt)
	return f, tt
}

// shown is how many elements of the last frame show exactly text.
func shown(tt *ui.Tester, text string) int {
	n := 0
	for _, s := range tt.Texts() {
		if s == text {
			n++
		}
	}
	return n
}

func TestEmbedRowsInText(t *testing.T) {
	a := embedArticle("ok", `<p>Before the video.</p><video src="v.mp4" title="Launch"></video><p>Between.</p>`+
		`<audio src="a.mp3"></audio><iframe src="https://player.example/x"></iframe><script>x()</script>`+
		`<form><input></form><p>After.</p>`, "")
	_, tt := opened(t, a)
	expect(t, tt, "Before the video.", "Between.", "After.", "There’s a video here", "There’s audio here",
		"There’s embedded content here")
	absent(t, tt, "No text to show", "Only a video here", "Only embedded content here")
	// Each row is one button named by its words and the action.
	for _, name := range []string{"There’s a video here: Launch. Open in browser", "There’s audio here. Open in browser",
		"There’s embedded content here. Open in browser"} {
		if _, ok := tt.Find(name); !ok {
			t.Fatalf("no button %q in %v", name, tt.Texts())
		}
	}
}

func TestEmbedRowOpensTheEntry(t *testing.T) {
	_, tt := opened(t, embedArticle("ok", `<p>Text.</p><iframe src="https://player.example/x"></iframe>`, ""))
	click(t, tt, "There’s embedded content here. Open in browser")
	if got := tt.OpenedURLs(); !slices.Equal(got, []string{"https://go.dev/blog/go1.27"}) {
		t.Fatalf("opened %v, want the entry link and not the embed address", got)
	}
}

func TestEmbedRowWithoutALink(t *testing.T) {
	a := embedArticle("ok", `<p>Text.</p><video src="v.mp4"></video>`, "")
	a.URL = ""
	_, tt := opened(t, a)
	expect(t, tt, "There’s a video here")
	if n := shown(tt, "Open in browser"); n != 1 { // the toolbar's
		t.Fatalf("%d Open in browser, want the toolbar's alone", n)
	}
	if _, ok := tt.Find("There’s a video here. Open in browser"); ok {
		t.Fatal("the row is a button with no link to open")
	}
}

func TestEmbedOnly(t *testing.T) {
	for _, c := range []struct{ name, html, title string }{
		{"video", `<video src="v.mp4"></video>`, "Only a video here"},
		{"audio", `<audio src="a.mp3"></audio>`, "Only audio here"},
		{"iframe", `<iframe src="https://player.example/x"></iframe>`, "Only embedded content here"},
		{"mix", `<video src="v.mp4"></video><audio src="a.mp3"></audio>`, "Only embedded content here"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, tt := opened(t, embedArticle("ok", c.html, ""))
			expect(t, tt, c.title, "The reader doesn’t play video or audio. Open it in the browser.", "Go 1.27 is released")
			absent(t, tt, "No text to show", "Try again", "There’s a video here", "There’s audio here")
			click(t, tt, "Open in browser")
			f.settle(tt)
			if got := tt.OpenedURLs(); !slices.Equal(got, []string{"https://go.dev/blog/go1.27"}) {
				t.Fatalf("opened %v", got)
			}
		})
	}
}

func TestEmbedOnlyWithoutALink(t *testing.T) {
	a := embedArticle("ok", `<video src="v.mp4"></video>`, "")
	a.URL = ""
	_, tt := opened(t, a)
	expect(t, tt, "Only a video here")
	if n := shown(tt, "Open in browser"); n != 1 { // the toolbar's
		t.Fatalf("%d Open in browser, want the toolbar's alone", n)
	}
}

func TestEmbedOnlyInTheFeed(t *testing.T) {
	// The page could not be extracted and the feed carries only a player: nothing failed that Try again would mend.
	_, tt := opened(t, embedArticle("failed", "", `<iframe src="https://player.example/x"></iframe>`))
	expect(t, tt, "Only embedded content here")
	absent(t, tt, "No text to show", "Try again")
}

func TestEmbedOnlyFullTextFallsBackToTheFeed(t *testing.T) {
	_, tt := opened(t, embedArticle("ok", `<video src="v.mp4"></video>`, `<p>The feed has the words.</p>`))
	expect(t, tt, "The feed has the words.")
	absent(t, tt, "Only a video here", "No text to show")
}

func TestNoTextAtAll(t *testing.T) {
	_, tt := opened(t, embedArticle("failed", "", `<script>x()</script><form></form>`))
	expect(t, tt, "No text to show")
	absent(t, tt, "Only embedded content here")
}

func TestEmbedTitleOnlyInTheName(t *testing.T) {
	_, tt := opened(t, embedArticle("ok", `<p>Text.</p><video title="Launch day"></video>`, ""))
	if _, ok := tt.Find("There’s a video here: Launch day. Open in browser"); !ok {
		t.Fatalf("title missing from the name: %v", tt.Texts())
	}
	if slices.ContainsFunc(tt.Texts(), func(s string) bool { return s == "There’s a video here: Launch day" }) {
		t.Fatalf("the title is drawn: %v", tt.Texts())
	}
	expect(t, tt, "There’s a video here")
}

func TestEmbedsAreNotText(t *testing.T) {
	a := embedArticle("ok", `<p>Before.</p><video src="v.mp4"></video><p>After.</p>`, "")
	md := markdownOf(a)
	if !strings.Contains(md, "Before.\n\nAfter.") || strings.Contains(md, "video") {
		t.Fatalf("rows reach the Markdown: %q", md)
	}
	for _, b := range parse(a.ExtractedHTML, nil, "") {
		if b.kind == embed && blockText(b) != "" {
			t.Fatalf("an embed row has text %q to search", blockText(b))
		}
	}
	a.Entry = entry()
	f := &fake{now: now, article: a, nav: Nav{Entry: a.Entry, Scope: "All", Keys: true}}
	s := New("en", f.deps())
	s.Use(f)
	tt := ui.NewTester(func(c *ui.Context) { s.View(c, f.nav) }, 1100, 800)
	s.Open(a.Entry)
	tt.Frame()
	f.settle(tt)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("video")
	tt.Frame()
	if len(s.find.hits) != 0 {
		t.Fatalf("find matches %d times in the rows", len(s.find.hits))
	}
}
