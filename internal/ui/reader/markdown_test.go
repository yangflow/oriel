package reader

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

func markdownOf(a client.Article) string {
	return markdown(a.Entry, exported(article(a)), time.UTC)
}

func TestMarkdown(t *testing.T) {
	a := client.Article{Entry: entry(), ExtractedHTML: `<h1>Go 1.27 is released</h1>
<p>Today the Go team is happy to release <a href="/doc/go1.27">Go 1.27</a>, with <strong>bold</strong>,
<em>italic</em> and <code>code</code>.<br>Second line, a_b * c.</p>
<h2>Tools</h2>
<ul><li>Faster builds<ul><li>Cached</li></ul></li><li>Smaller binaries</li></ul>
<ol><li>First</li><li>Second</li></ol>
<blockquote><p>Quoted words.</p></blockquote>
<pre><code>go build ./...
echo "` + "```" + `"</code></pre>
<figure><img src="/images/gopher.png" alt="A gopher" width="600" height="400"><figcaption>The gopher</figcaption></figure>
<h3>Notes</h3><p>1. not a list</p><hr>`}
	a.Author = "The Go Team"
	a.ExtractStatus = "ok"
	want := "# Go 1.27 is released\n\n" +
		"- Source: Go Blog\n" +
		"- Author: The Go Team\n" +
		"- Date: 2026-10-18 11:55\n" +
		"- Link: <https://go.dev/blog/go1.27>\n\n" +
		"Today the Go team is happy to release [Go 1.27](https://go.dev/doc/go1.27), with **bold**, *italic* and `code`.\\\nSecond line, a\\_b \\* c.\n\n" +
		"## Tools\n\n" +
		"- Faster builds\n" +
		"    - Cached\n" +
		"- Smaller binaries\n\n" +
		"1. First\n" +
		"2. Second\n\n" +
		"> Quoted words.\n\n" +
		"````\ngo build ./...\necho \"```\"\n````\n\n" +
		"![A gopher](https://go.dev/images/gopher.png)\n\n" +
		"*The gopher*\n\n" +
		"### Notes\n\n" +
		"1\\. not a list\n\n" +
		"---\n"
	if got := markdownOf(a); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestMarkdownFeedFallback(t *testing.T) {
	// No extracted text: the feed's content, as the reader shows it, and without tracking pictures.
	a := client.Article{Entry: entry(), ContentHTML: `<p>What the feed <em>carries</em>.</p><img src="https://example.com/p.png" alt="Pic">`}
	a.ExtractStatus = "failed"
	got := markdownOf(a)
	if !strings.HasSuffix(got, "\n\nWhat the feed *carries*.\n\n![Pic](https://example.com/p.png)\n") {
		t.Fatalf("got\n%s", got)
	}
	// Neither text: the feed's summary stands in.
	a.ContentHTML = ""
	if got := markdownOf(a); !strings.HasSuffix(got, "\n\nThe latest Go release.\n") {
		t.Fatalf("got\n%s", got)
	}
	// Nothing at all: the header only, without the empty lines of the missing parts.
	a.Summary, a.Author, a.URL, a.PublishedAt, a.CreatedAt = "", "", "", 0, 0
	if got, want := markdownOf(a), "# Go 1.27 is released\n\n- Source: Go Blog\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestMarkdownTitleFallback(t *testing.T) {
	e := client.Entry{SourceTitle: "Go Blog"}
	if got, want := markdown(e, nil, time.UTC), "# Go Blog\n\n- Source: Go Blog\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := markdown(client.Entry{}, nil, time.UTC); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestFileName(t *testing.T) {
	for title, want := range map[string]string{
		"Go 1.27 is released":        "Go 1.27 is released.md",
		"  Go:  what/why? \"now\"  ": "Go- what-why- -now-.md",
		"":                           "entry.md",
		"???":                        "entry.md",
		"...":                        "entry.md",
		"Line\nbreak\ttab":           "Line break tab.md",
		"CON":                        "CON_.md",
		"发布 Go 1.27：更快":              "发布 Go 1.27：更快.md",
		strings.Repeat("长", 100):     strings.Repeat("长", 80) + ".md",
	} {
		if got := fileName(title); got != want {
			t.Errorf("fileName(%q) = %q, want %q", title, got, want)
		}
	}
}

// export clicks the item of the More menu and runs the work it queued.
func export(t *testing.T, f *fake, tt *ui.Tester, more, item string) {
	t.Helper()
	if err := tt.Click(more); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(item); err != nil {
		t.Fatal(err)
	}
	for len(f.queue) > 0 {
		work := f.queue[0]
		f.queue = f.queue[1:]
		work()()
		tt.Frame()
	}
}

func TestExportCopyAndSave(t *testing.T) {
	for language, c := range map[string]struct{ more, copied, saved string }{
		"en":      {"More", "Copied", "Saved as Go 1.27 is released.md"},
		"zh-Hans": {"更多", "已复制", "已保存为 Go 1.27 is released.md"},
	} {
		t.Run(language, func(t *testing.T) {
			_, f, tt := setup(t, language, full())
			f.settle(tt)
			if err := tt.Click(c.more); err != nil {
				t.Fatal(err)
			}
			wantMenu := []string{"Copy as Markdown", "Save as Markdown…"}
			if language == "zh-Hans" {
				wantMenu = []string{"复制为 Markdown", "存为 Markdown…"}
			}
			if menu := tt.Menu(); !slices.Equal(menu, wantMenu) {
				t.Fatalf("menu %q", menu)
			}
			tt.CloseMenu()

			want := markdown(f.article.Entry, exported(article(f.article)), time.Local)
			export(t, f, tt, c.more, wantMenu[0])
			if tt.Clipboard() != want || !slices.Contains(tt.Announcements(), c.copied) {
				t.Fatalf("clipboard %q, announcements %v", tt.Clipboard(), tt.Announcements())
			}

			f.saveTo = filepath.Join(t.TempDir(), "out.md")
			export(t, f, tt, c.more, wantMenu[1])
			data, err := os.ReadFile(f.saveTo)
			if err != nil || string(data) != want {
				t.Fatalf("file %q (%v), want %q", data, err, want)
			}
			if !slices.Equal(f.dialogs, []string{"Go 1.27 is released.md | Markdown"}) {
				t.Fatalf("dialogs %q", f.dialogs)
			}
			if !slices.Contains(tt.Announcements(), strings.ReplaceAll(c.saved, "Go 1.27 is released.md", "out.md")) {
				t.Fatalf("announcements %v", tt.Announcements())
			}
		})
	}
}

func TestExportCancelledAndFailed(t *testing.T) {
	_, f, tt := setup(t, "en", full())
	f.settle(tt)
	// A cancelled dialog writes nothing and says nothing.
	before := len(tt.Announcements())
	export(t, f, tt, "More", "Save as Markdown…")
	if len(f.dialogs) != 1 || len(tt.Announcements()) != before {
		t.Fatalf("dialogs %q, announcements %v", f.dialogs, tt.Announcements())
	}
	// A file that cannot be written is told.
	f.saveTo = filepath.Join(t.TempDir(), "missing", "out.md")
	export(t, f, tt, "More", "Save as Markdown…")
	if _, err := os.Stat(f.saveTo); err == nil {
		t.Fatal("the file was written")
	}
	if !tt.HasText("Couldn’t save out.md") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestExportOtherEntry(t *testing.T) {
	// An entry that is not open (the timeline's context menu) is got from the server, then converted as the same text.
	s, f, tt := setup(t, "en", full())
	f.settle(tt)
	other := client.Article{Entry: client.Entry{ID: 9, SourceTitle: "Go Blog", Title: "Another", URL: "https://go.dev/b"},
		ContentHTML: "<p>From the feed.</p>"}
	other.ExtractStatus = "failed"
	f.article = other
	asked := f.asked
	s.Export(other.Entry, false)
	f.settle(tt)
	want := markdown(other.Entry, exported(article(other)), time.Local)
	if f.asked != asked+1 || tt.Clipboard() != want || !strings.HasSuffix(want, "\n\nFrom the feed.\n") {
		t.Fatalf("asked %d, clipboard %q, want %q", f.asked-asked, tt.Clipboard(), want)
	}
}
