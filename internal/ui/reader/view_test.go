package reader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/prefs"
	"github.com/yangflow/oriel/internal/ui/pictures"
	"github.com/yangflow/oriel/internal/ui/theme"
)

var now = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

var errExpired = errors.New("session expired")

// fake stands in for the server, the background and the timeline: work waits in a queue until settle runs it.
type fake struct {
	queue    []func() func()
	now      time.Time
	article  client.Article
	err      error
	asked    int
	pictures []string
	imageCtx []context.Context      // the context of each request for a picture
	imageErr int                    // pictures that fail before they work again
	imageAt  map[string]image.Point // the size of the pictures that are not 4 by 3
	nav      Nav
	calls    []string // what the reader asked the timeline to do

	ai       client.AIResults
	aiErr    error
	reply    client.AIResult // the answer to a request
	replyErr error
	requests []string
	polls    int

	retried  int // requests to get the full text again
	retryTo  client.Article
	retryErr error

	saved []saved // the reading positions stored, in order

	selected  []string // the tasks and texts asked about a selection, as "task: text"
	selTitles []string // the titles sent with them
	selReply  string   // the answer to a selection
	selErr    error
	saveTo    string   // the path the save dialog gives, "" when it is cancelled
	dialogs   []string // the file name and filter the save dialog was asked for
}

// saved is a reading position the reader stored.
type saved struct {
	id       int64
	progress float64
}

func (f *fake) Entry(_ context.Context, id int64) (client.Article, error) {
	f.asked++
	if f.err != nil {
		return client.Article{}, f.err
	}
	return f.article, nil
}

func (f *fake) Image(ctx context.Context, address string) ([]byte, error) {
	f.pictures = append(f.pictures, address)
	f.imageCtx = append(f.imageCtx, ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if f.imageErr > 0 {
		f.imageErr--
		return nil, errors.New("broken")
	}
	var b bytes.Buffer
	size := image.Pt(4, 3)
	if at, ok := f.imageAt[address]; ok {
		size = at
	}
	png.Encode(&b, image.NewRGBA(image.Rectangle{Max: size}))
	return b.Bytes(), nil
}

func (f *fake) AI(_ context.Context, id int64) (client.AIResults, error) {
	f.polls++
	return f.ai, f.aiErr
}

func (f *fake) RequestAI(_ context.Context, id int64, task string) (client.AIResult, error) {
	f.requests = append(f.requests, task)
	return f.reply, f.replyErr
}

func (f *fake) Selection(_ context.Context, task, text, title string) (string, string, error) {
	f.selected = append(f.selected, task+": "+text)
	f.selTitles = append(f.selTitles, title)
	return f.selReply, "gpt-4o-mini", f.selErr
}

func (f *fake) SetEntry(_ context.Context, id int64, patch client.EntryPatch) (client.Entry, error) {
	if patch.ReadProgress != nil {
		f.saved = append(f.saved, saved{id, *patch.ReadProgress})
	}
	return client.Entry{ID: id}, nil
}

func (f *fake) RetryExtraction(_ context.Context, id int64) (client.Article, error) {
	f.retried++
	if f.retryErr != nil {
		return client.Article{}, f.retryErr
	}
	f.article = f.retryTo
	return f.retryTo, nil
}

func (f *fake) deps() Deps {
	return Deps{
		Go:           func(work func() func()) { f.queue = append(f.queue, work) },
		Now:          func() time.Time { return f.now },
		Expired:      func(err error) bool { return errors.Is(err, errExpired) },
		Back:         func() { f.calls = append(f.calls, "back") },
		Move:         func(by int) { f.calls = append(f.calls, map[int]string{-1: "previous", 1: "next"}[by]) },
		Star:         func() { f.calls = append(f.calls, "star") },
		ToggleRead:   func() { f.calls = append(f.calls, "read") },
		OpenSettings: func() { f.calls = append(f.calls, "settings") },
		OpenCopy:     func(id int64) { f.calls = append(f.calls, fmt.Sprintf("copy %d", id)) },
		Immerse:      func() { f.calls = append(f.calls, "immerse") },
		Save: func(name, filter string) (string, error) {
			f.dialogs = append(f.dialogs, name+" | "+filter)
			return f.saveTo, nil
		},
	}
}

// settle runs the queued work until there is none, applying each result as the window does on the UI thread.
func (f *fake) settle(tt *ui.Tester) {
	for len(f.queue) > 0 {
		work := f.queue[0]
		f.queue = f.queue[1:]
		work()()
		tt.Frame()
	}
}

func entry() client.Entry {
	return client.Entry{ID: 8, SourceID: 1, SourceTitle: "Go Blog", Kind: "article", Title: "Go 1.27 is released",
		URL: "https://go.dev/blog/go1.27", Summary: "The latest Go release.", IsRead: true,
		PublishedAt: now.Add(-5 * time.Minute).UnixMilli()}
}

// setup opens article in a reader of language, without settling, so the loading state shows.
func setup(t *testing.T, language string, article client.Article) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	f := &fake{now: now, article: article,
		nav: Nav{Entry: article.Entry, Scope: "All", Previous: true, Next: true, Keys: true}}
	s := New(language, f.deps())
	s.Use(f)
	tt := ui.NewTester(func(c *ui.Context) { s.View(c, f.nav) }, 1100, 800)
	s.Open(article.Entry)
	tt.Frame()
	return s, f, tt
}

func full() client.Article {
	a := client.Article{Entry: entry(), ExtractedHTML: `<h1>Go 1.27 is released</h1>
<p>Today the Go team is happy to release <a href="/doc/go1.27">Go 1.27</a>.</p>
<figure><img src="/images/gopher.png" alt="A gopher" width="600" height="400"><figcaption>The gopher</figcaption></figure>
<h2>Tools</h2><ul><li>Faster builds</li><li>Smaller binaries</li></ul>`}
	a.ExtractStatus = "ok"
	return a
}

func expect(t *testing.T, tt *ui.Tester, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if !tt.HasText(text) {
			t.Fatalf("no %q in %v", text, tt.Texts())
		}
	}
}

func absent(t *testing.T, tt *ui.Tester, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if tt.HasText(text) {
			t.Fatalf("%q shows in %v", text, tt.Texts())
		}
	}
}

func TestCatalogsMatch(t *testing.T) {
	var raw map[string]map[string]string
	if err := json.Unmarshal(catalog, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2 {
		t.Fatalf("languages %v", reflect.ValueOf(raw).MapKeys())
	}
	fields := reflect.TypeFor[copy]().NumField()
	for language, values := range raw {
		if len(values) != fields {
			t.Errorf("%s has %d strings, want %d", language, len(values), fields)
		}
		for key, value := range values {
			if _, ok := raw["en"][key]; !ok {
				t.Errorf("%s has %s, en does not", language, key)
			}
			if value == "" {
				t.Errorf("%s.%s is empty", language, key)
			}
		}
	}
}

func TestFullText(t *testing.T) {
	_, f, tt := setup(t, "en", full())
	if _, ok := tt.Find("Loading"); !ok {
		t.Fatal("no loading state")
	}
	if _, ok := tt.Find("Remove star"); ok {
		t.Fatal("the star shows before the article loads")
	}
	f.settle(tt)
	expect(t, tt, "Go Blog", "Oct 18, 11:55 AM", "Go 1.27 is released", "Today the Go team is happy to release ",
		"Go 1.27", "The gopher", "Tools", "Faster builds", "Smaller binaries", "•")
	absent(t, tt, "This isn’t the full text: the page could not be extracted, so this is what the feed carries. ",
		"Loading")
	if !tt.Focused("Go 1.27 is released") {
		t.Fatal("the article does not take the focus")
	}
	if !slices.Equal(f.pictures, []string{"https://go.dev/images/gopher.png"}) {
		t.Fatalf("pictures %v", f.pictures)
	}
	if _, ok := tt.Find("A gopher"); !ok {
		t.Fatal("the picture is not named")
	}
	if err := tt.Click("Go 1.27"); err != nil {
		t.Fatal(err)
	}
	if opened := tt.OpenedURLs(); !slices.Equal(opened, []string{"https://go.dev/doc/go1.27"}) {
		t.Fatalf("opened %v", opened)
	}
	if err := tt.Click("Open in browser"); err != nil {
		t.Fatal(err)
	}
	if opened := tt.OpenedURLs(); len(opened) != 2 || opened[1] != "https://go.dev/blog/go1.27" {
		t.Fatalf("opened %v", opened)
	}
}

func TestFeedOnly(t *testing.T) {
	a := client.Article{Entry: entry(), ContentHTML: "<p>What the feed <em>carries</em>.</p>"}
	a.ExtractStatus = "failed"
	_, f, tt := setup(t, "en", a)
	f.settle(tt)
	expect(t, tt, "This isn’t the full text: the page could not be extracted, so this is what the feed carries. ",
		"What the feed ", "carries")
	if err := tt.Click("Open the original"); err != nil {
		t.Fatal(err)
	}
	if opened := tt.OpenedURLs(); !slices.Equal(opened, []string{"https://go.dev/blog/go1.27"}) {
		t.Fatalf("opened %v", opened)
	}
	// Without content, the summary stands in.
	a.ContentHTML = ""
	_, f, tt = setup(t, "en", a)
	f.settle(tt)
	expect(t, tt, "The latest Go release.")
}

func TestExtracting(t *testing.T) {
	a := client.Article{Entry: entry(), ContentHTML: "<p>The summary of the feed.</p>"}
	a.ExtractStatus = "pending"
	_, f, tt := setup(t, "en", a)
	f.settle(tt)
	expect(t, tt, "Getting the full text. Until then, this is what the feed carries. ", "The summary of the feed.")
	f.now = f.now.Add(4 * time.Second)
	tt.Frame()
	if len(f.queue) != 0 {
		t.Fatal("asked again before five seconds")
	}
	// A poll that fails keeps what shows.
	f.err = errors.New("status 502")
	f.now = f.now.Add(time.Second)
	tt.Frame()
	f.settle(tt)
	if f.asked != 2 {
		t.Fatalf("asked %d times", f.asked)
	}
	expect(t, tt, "The summary of the feed.")
	absent(t, tt, "Can’t load this entry")
	f.err = nil
	f.article.ExtractStatus, f.article.ExtractedHTML = "ok", "<p>The whole article.</p>"
	f.now = f.now.Add(5 * time.Second)
	tt.Frame()
	f.settle(tt)
	expect(t, tt, "The whole article.")
	absent(t, tt, "The summary of the feed.", "Getting the full text. Until then, this is what the feed carries. ")
	if !slices.Contains(tt.Announcements(), "Full text loaded") {
		t.Fatal("the full text is not announced")
	}
	f.now = f.now.Add(time.Minute)
	tt.Frame()
	if len(f.queue) != 0 {
		t.Fatal("asks again once the text is in")
	}
}

func TestWaiting(t *testing.T) {
	a := client.Article{Entry: entry()}
	a.Summary, a.ExtractStatus = "", "pending"
	_, f, tt := setup(t, "zh-Hans", a)
	f.settle(tt)
	expect(t, tt, "正在取全文", "读完网页后，文章会显示在这里。")
	if err := tt.Click("打开原文"); err != nil {
		t.Fatal(err)
	}
	if len(tt.OpenedURLs()) != 1 {
		t.Fatalf("opened %v", tt.OpenedURLs())
	}
	f.article.ExtractStatus = "failed"
	f.now = f.now.Add(pollEvery)
	tt.Frame()
	f.settle(tt)
	expect(t, tt, "没有可显示的正文", "网页抽不出正文，订阅里也没有这篇的内容。")
	absent(t, tt, "正在取全文")
}

func TestNothing(t *testing.T) {
	a := client.Article{Entry: entry()}
	a.Summary, a.ExtractStatus = "", "failed"
	_, f, tt := setup(t, "en", a)
	f.settle(tt)
	expect(t, tt, "No text to show", "Go 1.27 is released")
	tt.FailOpenURL(errors.New("no browser"))
	if err := tt.Click("Open the original"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	expect(t, tt, "Couldn’t open the link")
}

func TestRetryExtraction(t *testing.T) {
	for _, c := range []struct {
		language, retry, note, gettingNote, whole, notice string
	}{
		{"en", "Try again", "This isn’t the full text: the page could not be extracted, so this is what the feed carries. ",
			"Getting the full text. Until then, this is what the feed carries. ", "Full text loaded",
			"Couldn’t try again. Check the connection."},
		{"zh-Hans", "重试", "这不是全文：网页抽不出正文，下面是订阅里带的内容。", "正在取全文，先显示订阅里带的内容。",
			"全文已载入", "没能重试，检查一下连接。"},
	} {
		a := client.Article{Entry: entry(), ContentHTML: "<p>What the feed carries.</p>"}
		a.ExtractStatus = "failed"
		s, f, tt := setup(t, c.language, a)
		f.settle(tt)
		expect(t, tt, c.note, c.retry)
		// A request that fails leaves the failed state, and the button can be pressed again.
		f.retryErr = errors.New("status 502")
		if err := tt.Click(c.retry); err != nil {
			t.Fatal(err)
		}
		expect(t, tt, c.gettingNote)
		absent(t, tt, c.retry)
		f.settle(tt)
		expect(t, tt, c.note, c.retry, c.notice)
		if f.retried != 1 || s.article.ExtractStatus != "failed" {
			t.Fatalf("retried %d times, status %q", f.retried, s.article.ExtractStatus)
		}
		// The server takes it: the extracting state shows, then the full text once the reader's polling finds it.
		f.retryErr = nil
		f.retryTo = a
		f.retryTo.ExtractStatus = "pending"
		if err := tt.Click(c.retry); err != nil {
			t.Fatal(err)
		}
		f.settle(tt)
		expect(t, tt, c.gettingNote, "What the feed ", "carries")
		absent(t, tt, c.note)
		asked := f.asked
		f.now = f.now.Add(pollEvery - time.Second)
		tt.Frame()
		if len(f.queue) != 0 {
			t.Fatal("asked for the result before five seconds")
		}
		f.article.ExtractStatus, f.article.ExtractedHTML = "ok", "<p>The whole article.</p>"
		f.now = f.now.Add(time.Second)
		tt.Frame()
		f.settle(tt)
		if f.asked != asked+1 || f.retried != 2 {
			t.Fatalf("asked %d times after the retry, retried %d", f.asked-asked, f.retried)
		}
		expect(t, tt, "The whole article.")
		absent(t, tt, c.gettingNote)
		if !slices.Contains(tt.Announcements(), c.whole) {
			t.Fatalf("announcements %v", tt.Announcements())
		}
	}
}

func TestRetryFromNothing(t *testing.T) {
	a := client.Article{Entry: entry()}
	a.Summary, a.ExtractStatus = "", "failed"
	_, f, tt := setup(t, "en", a)
	f.settle(tt)
	expect(t, tt, "No text to show", "Open the original", "Try again")
	f.retryTo = a
	f.retryTo.ExtractStatus = "pending"
	if err := tt.Click("Try again"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	expect(t, tt, "Getting the full text", "The article will show here once the page has been read.")
	absent(t, tt, "No text to show", "Try again")
	// Only a failed extraction offers it: a skipped one has nothing to try.
	a.ExtractStatus = "skipped"
	_, f, tt = setup(t, "en", a)
	f.settle(tt)
	absent(t, tt, "Try again")
}

func TestError(t *testing.T) {
	a := full()
	s, f, tt := setup(t, "en", a)
	f.err = errors.New("status 502")
	f.settle(tt)
	expect(t, tt, "Can’t load this entry", "The server didn’t answer. Check the connection and try again.")
	if !slices.Contains(tt.Announcements(), "Can’t load this entry") {
		t.Fatal("the error is not announced")
	}
	if _, ok := tt.Find("Next entry"); !ok {
		t.Fatal("the toolbar does not show")
	}
	tt.Key(0, ui.KeyS)
	tt.Key(0, ui.KeyM)
	if len(f.calls) != 0 {
		t.Fatalf("calls %v on an entry not loaded", f.calls)
	}
	f.err = nil
	if err := tt.Click("Try again"); err != nil {
		t.Fatal(err)
	}
	if !s.loading {
		t.Fatal("Try again does not load")
	}
	f.settle(tt)
	expect(t, tt, "Faster builds")
	// A session the server no longer takes is the timeline's to handle; the reader shows no error for it.
	f.err = errExpired
	s.Open(a.Entry)
	tt.Frame()
	f.settle(tt)
	absent(t, tt, "Can’t load this entry")
}

func TestKeys(t *testing.T) {
	s, f, tt := setup(t, "en", full())
	f.settle(tt)
	for _, k := range []ui.Key{ui.KeyJ, ui.KeyK, ui.KeyS, ui.KeyM, ui.KeyEscape} {
		tt.Key(0, k)
	}
	if want := []string{"next", "previous", "star", "read", "back"}; !slices.Equal(f.calls, want) {
		t.Fatalf("calls %v", f.calls)
	}
	tt.Key(0, ui.KeyO)
	if len(tt.OpenedURLs()) != 1 {
		t.Fatalf("opened %v", tt.OpenedURLs())
	}
	f.calls = nil
	f.nav.Previous, f.nav.Next = false, false
	tt.Frame()
	tt.Key(0, ui.KeyJ)
	tt.Key(0, ui.KeyK)
	if len(f.calls) != 0 {
		t.Fatalf("calls %v with nothing before or after", f.calls)
	}
	f.nav.Keys = false
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if len(f.calls) != 0 {
		t.Fatalf("calls %v while a dialog shows", f.calls)
	}
	f.nav.Keys, f.nav.Next = true, true
	f.nav.Entry.Starred, f.nav.Entry.IsRead = true, false
	tt.Frame()
	for _, label := range []string{"Back to All", "Remove star", "Mark as read"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if err := tt.Click("Next entry"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"back", "star", "read", "next"}; !slices.Equal(f.calls, want) {
		t.Fatalf("calls %v", f.calls)
	}
	// The read and star state come from the timeline, the extraction from the article.
	if s.article.ExtractStatus != "ok" || !s.article.Starred {
		t.Fatalf("article %+v", s.article.Entry)
	}
}

// A result for the article before is dropped when another opens.
func TestStale(t *testing.T) {
	s, f, tt := setup(t, "en", full())
	other := entry()
	other.ID, other.Title = 9, "Another post"
	f.nav.Entry = other
	s.Open(other)
	f.article = client.Article{Entry: other, ContentHTML: "<p>Another text.</p>"}
	f.settle(tt)
	expect(t, tt, "Another text.")
	absent(t, tt, "Faster builds")
}

func TestDates(t *testing.T) {
	s, _, _ := setup(t, "zh-Hans", full())
	e := entry()
	if got := s.when(e); got != "10月18日 11:55" {
		t.Fatalf("this year %q", got)
	}
	e.PublishedAt, e.CreatedAt = 0, time.Date(2025, 3, 4, 9, 30, 0, 0, time.UTC).UnixMilli()
	if got := s.when(e); got != "2025年3月4日 09:30" {
		t.Fatalf("another year %q", got)
	}
}

// TestHourCycle checks that the time of day follows the hour cycle of the system, and the language's habit while the
// system says nothing.
func TestHourCycle(t *testing.T) {
	for _, tc := range []struct {
		lang  string
		hours int
		want  string
	}{
		{"en", 0, "Oct 18, 11:55 AM"}, {"en", 12, "Oct 18, 11:55 AM"}, {"en", 24, "Oct 18, 11:55"},
		{"zh-Hans", 0, "10月18日 11:55"}, {"zh-Hans", 12, "10月18日 上午11:55"}, {"zh-Hans", 24, "10月18日 11:55"},
	} {
		s, _, _ := setup(t, tc.lang, full())
		s.hours = tc.hours
		if got := s.when(entry()); got != tc.want {
			t.Errorf("%s with %d hours: %q, want %q", tc.lang, tc.hours, got, tc.want)
		}
	}
}

func TestParse(t *testing.T) {
	base, _ := url.Parse("https://example.com/posts/1")
	type want struct {
		kind          int
		text          string
		marker        string
		quote, depth  int
		level         int
		link, src     string
		bold, italic  bool
		mono, gone    bool
		width, height int
	}
	shape := func(blocks []block) []want {
		var got []want
		for _, b := range blocks {
			w := want{kind: b.kind, marker: b.marker, quote: b.quote, depth: b.depth, level: b.level, src: b.src,
				width: b.width, height: b.height}
			switch b.kind {
			case code:
				w.text = b.text
			case picture:
				w.text = b.alt
			default:
				w.text = plain(b.spans)
				if len(b.spans) == 1 {
					sp := b.spans[0]
					w.link, w.bold, w.italic, w.mono, w.gone = sp.link, sp.bold, sp.italic, sp.mono, sp.gone
				}
			}
			got = append(got, w)
		}
		return got
	}
	for _, c := range []struct {
		name, html string
		want       []want
	}{
		{"title dropped", "<h1>Hello</h1><h2>Part</h2><h4>Small</h4>",
			[]want{{kind: heading, text: "Part", level: 2, bold: true}, {kind: heading, text: "Small", level: 3,
				bold: true}}},
		{"white space collapses, br breaks", "<p>  one\n  two<br>three </p>\n<p>four</p>",
			[]want{{text: "one two\nthree"}, {text: "four"}}},
		{"ordered list with start", `<ol start="3"><li>c</li><li>d</li></ol>`,
			[]want{{text: "c", marker: "3.", depth: 1}, {text: "d", marker: "4.", depth: 1}}},
		{"nested list", "<ul><li>a<ul><li>b</li></ul></li></ul>",
			[]want{{text: "a", marker: "•", depth: 1}, {text: "b", marker: "◦", depth: 2}}},
		{"quote", "<blockquote><p>said</p><p>more</p></blockquote><p>after</p>",
			[]want{{text: "said", quote: 1}, {text: "more", quote: 1}, {text: "after"}}},
		{"code keeps its lines", "<pre><code>a := 1\n\tb := 2\n</code></pre>",
			[]want{{kind: code, text: "a := 1\n\tb := 2"}}},
		{"figure", `<figure><img src="p.png" alt="A view"><figcaption>Caption</figcaption></figure>`,
			[]want{{kind: picture, text: "A view", src: "https://example.com/posts/p.png"},
				{kind: caption, text: "Caption"}}},
		{"lazy picture", `<img src="data:image/gif;base64,R0lGOD" data-src="//cdn.example.com/x.jpg" width="800" height="600">`,
			[]want{{kind: picture, src: "https://cdn.example.com/x.jpg", width: 800, height: 600}}},
		{"tracking pixel", `<p>text<img src="/t.gif" width="1" height="1"></p>`, []want{{text: "text"}}},
		{"hidden attribute", `<p>text<img src="/t.gif" hidden></p>`, []want{{text: "text"}}},
		{"hidden by style", `<p>a<img src="/1.gif" style="display:none"><img src="/2.gif" style="Visibility: hidden !important">` +
			`<img src="/3.gif" style="width:1px"><img src="/4.gif" style="border:0;height: 0"></p>`, []want{{text: "a"}}},
		{"styled but visible", `<img src="/a.png" style="width:100%;border-radius:4px"><img src="/b.png" style="display:block">`,
			[]want{{kind: picture, src: "https://example.com/a.png"}, {kind: picture, src: "https://example.com/b.png"}}},
		{"known trackers", `<p>text<img src="https://pixel.wp.com/g.gif?v=ext"><img src="https://x.list-manage.com/track/open.php?u=1">` +
			`<img src="https://www.google-analytics.com/collect?v=1"><img src="/__utm.gif"></p>`, []want{{text: "text"}}},
		{"ordinary pictures on shared hosts stay", `<img src="https://i0.wp.com/example.com/a.jpg">` +
			`<img src="https://substackcdn.com/image/fetch/w_800/a.png"><img src="https://x.list-manage.com/images/logo.png">`,
			[]want{{kind: picture, src: "https://i0.wp.com/example.com/a.jpg"},
				{kind: picture, src: "https://substackcdn.com/image/fetch/w_800/a.png"},
				{kind: picture, src: "https://x.list-manage.com/images/logo.png"}}},
		{"dropped elements", `<script>x()</script><style>p{}</style><iframe src="/v"></iframe><form><input></form>` +
			`<video src="v.mp4">no video</video><p>kept</p>`,
			[]want{{kind: embed, level: embedOther}, {kind: embed, level: embedVideo}, {text: "kept"}}},
		{"embed kinds", `<p>a<audio controls><source src="a.mp3"></audio>b</p><object data="x.swf"><embed src="x.swf"></object>` +
			`<embed src="y">`, []want{{text: "a"}, {kind: embed, level: embedAudio}, {text: "b"}, {kind: embed, level: embedOther},
			{kind: embed, level: embedOther}}},
		{"hidden embeds", `<iframe src="/v" hidden></iframe><video style="display:none"></video>` +
			`<audio style="width:0"></audio><p>kept</p>`, []want{{text: "kept"}}},
		{"embed fallback is not drawn", `<video src="v.mp4"><p>Your browser has no video</p><track src="t.vtt"></video>`,
			[]want{{kind: embed, level: embedVideo}}},
		{"embed in a figure keeps its caption", `<figure><iframe src="/v"></iframe><figcaption>Talk</figcaption></figure>`,
			[]want{{kind: embed, level: embedOther}, {kind: caption, text: "Talk"}}},
		{"links", `<p><a href="javascript:alert(1)">script</a></p><p><a href="#top">top</a></p>` +
			`<p><a href="../about">about</a></p><p><a href="mailto:a@example.com">mail</a></p>`,
			[]want{{text: "script"}, {text: "top"}, {text: "about", link: "https://example.com/about"},
				{text: "mail", link: "mailto:a@example.com"}}},
		{"inline styles", "<p><strong>b</strong></p><p><em>i</em></p><p><code>m</code></p><p><del>g</del></p>",
			[]want{{text: "b", bold: true}, {text: "i", italic: true}, {text: "m", mono: true}, {text: "g", gone: true}}},
		{"data table", "<table><tr><th>Name</th><th>Size</th></tr><tr><td>a</td><td>1</td></tr></table>",
			[]want{{kind: table}}},
		{"table rows without a grid", "<table><tr><th>Name</th><th>Size</th></tr></table>" +
			"<table><tr><td>x</td></tr><tr><td>y</td></tr></table>",
			[]want{{text: "Name · Size"}, {text: "x"}, {text: "y"}}},
		{"rule", "<p>a</p><hr><p>b</p>", []want{{text: "a"}, {kind: rule}, {text: "b"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := shape(parse(c.html, base, "Hello")); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func click(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
}

// wait moves the clock by d and draws a frame, so a poll due by then is asked for.
func (f *fake) wait(tt *ui.Tester, d time.Duration) {
	f.now = f.now.Add(d)
	tt.Frame()
}

func TestSummary(t *testing.T) {
	_, f, tt := setup(t, "en", full())
	f.settle(tt)
	absent(t, tt, "Summary", "Summarizing…")
	f.reply = client.AIResult{Status: "pending"}
	click(t, tt, "Summarize")
	expect(t, tt, "Summarizing…", "Cancel")
	f.settle(tt)
	if !slices.Equal(f.requests, []string{"summary"}) {
		t.Fatalf("requests %v", f.requests)
	}
	polls := f.polls
	f.wait(tt, time.Second)
	if len(f.queue) != 0 {
		t.Fatal("polls before two seconds")
	}
	f.ai.Summary = &client.AIResult{Status: "ok", Text: "Go 1.27 is faster.", Model: "gpt-4o-mini"}
	f.wait(tt, time.Second)
	f.settle(tt)
	if f.polls != polls+1 {
		t.Fatalf("polled %d times", f.polls-polls)
	}
	expect(t, tt, "Summary", "· by gpt-4o-mini", "Go 1.27 is faster.", "Faster builds")
	absent(t, tt, "Summarizing…")
	if !slices.Contains(tt.Announcements(), "Summary ready") {
		t.Fatal("the summary is not announced")
	}
	f.wait(tt, time.Minute)
	if len(f.queue) != 0 {
		t.Fatal("polls once the summary is ready")
	}
	// The toolbar button and the card's close button both hide it.
	click(t, tt, "Hide the summary")
	absent(t, tt, "Go 1.27 is faster.")
	if _, ok := tt.Find("Summarize"); !ok {
		t.Fatal("the button is not named Summarize again")
	}
	// A summary the server has is answered at once.
	f.reply = *f.ai.Summary
	click(t, tt, "Summarize")
	f.settle(tt)
	expect(t, tt, "Go 1.27 is faster.")
}

func TestSummaryCancel(t *testing.T) {
	_, f, tt := setup(t, "en", full())
	f.settle(tt)
	f.reply = client.AIResult{Status: "pending"}
	click(t, tt, "Summarize")
	f.settle(tt)
	click(t, tt, "Cancel")
	absent(t, tt, "Summarizing…")
	f.wait(tt, time.Minute)
	if len(f.queue) != 0 {
		t.Fatal("polls after Cancel")
	}
	// The answer to a request cancelled before it comes is dropped.
	click(t, tt, "Summarize")
	click(t, tt, "Hide the summary")
	f.reply = client.AIResult{Status: "ok", Text: "Late.", Model: "m"}
	f.settle(tt)
	absent(t, tt, "Late.", "Summarizing…")
}

func TestSummaryFailed(t *testing.T) {
	_, f, tt := setup(t, "en", full())
	f.settle(tt)
	f.reply = client.AIResult{Status: "failed", Error: "ai_timeout"}
	click(t, tt, "Summarize")
	f.settle(tt)
	expect(t, tt, "Couldn’t summarize", "The AI endpoint took too long to answer.", "Try again")
	if !slices.Contains(tt.Announcements(), "Couldn’t summarize") {
		t.Fatal("the failure is not announced")
	}
	// A failure made while polling shows the same.
	f.reply = client.AIResult{Status: "pending"}
	click(t, tt, "Try again")
	f.settle(tt)
	expect(t, tt, "Summarizing…")
	f.ai.Summary = &client.AIResult{Status: "failed", Error: "ai_rate_limited"}
	f.wait(tt, aiPollEvery)
	f.settle(tt)
	expect(t, tt, "The AI endpoint is taking too many requests. Try again in a moment.")
	// The server not answering is said the same way.
	f.replyErr = errors.New("status 502")
	click(t, tt, "Try again")
	f.settle(tt)
	expect(t, tt, "Couldn’t summarize", "The server didn’t answer. Check the connection and try again.")
	if len(f.requests) != 3 {
		t.Fatalf("requests %v", f.requests)
	}
}

func TestReasons(t *testing.T) {
	s := New("en", (&fake{}).deps())
	for code, want := range map[string]string{
		"ai_unreachable":  "Couldn’t reach the AI endpoint.",
		"ai_timeout":      "The AI endpoint took too long to answer.",
		"ai_unauthorized": "The AI endpoint turned down the API key. Check it in Settings.",
		"ai_rate_limited": "The AI endpoint is taking too many requests. Try again in a moment.",
		"ai_upstream":     "The AI endpoint answered with an error.",
		"ai_unknown":      "The AI endpoint answered with an error.",
		"ai_empty":        "This entry has no text to send.",
		"ai_cap_reached":  "Automatic AI work reached its daily cap, so this was not done. Try it yourself, or raise the cap in Settings.",
		"":                "The server didn’t answer. Check the connection and try again.",
	} {
		if got := s.reason(code); got != want {
			t.Errorf("%q: %q", code, got)
		}
	}
}

// What the server already has shows as the article opens: the summary, made or being made, and not the
// translation.
func TestOpenWithResults(t *testing.T) {
	a := full()
	f := &fake{now: now, article: a, nav: Nav{Entry: a.Entry, Scope: "All", Keys: true}}
	f.ai = client.AIResults{
		Summary:     &client.AIResult{Status: "ok", Text: "Made before.", Model: "m"},
		Translation: &client.AIResult{Status: "ok", Text: "Titel\n\nAbsatz.", Model: "m"},
	}
	s := New("en", f.deps())
	s.Use(f)
	tt := ui.NewTester(func(c *ui.Context) { s.View(c, f.nav) }, 1100, 800)
	s.Open(a.Entry)
	f.settle(tt)
	expect(t, tt, "Made before.", "Go 1.27 is released", "Faster builds")
	absent(t, tt, "Titel", "Absatz.")
	if slices.Contains(tt.Announcements(), "Summary ready") {
		t.Fatal("a summary made before is announced")
	}
	if len(f.requests) != 0 {
		t.Fatalf("requests %v", f.requests)
	}
	// One being made shows as such, and is polled for.
	f.ai.Summary = &client.AIResult{Status: "pending", Text: "Made before."}
	s.Open(a.Entry)
	f.settle(tt)
	expect(t, tt, "Summarizing…")
	f.ai.Summary = &client.AIResult{Status: "ok", Text: "Made again.", Model: "m"}
	f.wait(tt, aiPollEvery)
	f.settle(tt)
	expect(t, tt, "Made again.")
	// One that failed before does not show.
	f.ai.Summary = &client.AIResult{Status: "failed", Error: "ai_timeout"}
	s.Open(a.Entry)
	f.settle(tt)
	absent(t, tt, "Couldn’t summarize", "Summary")
}

func TestAIOff(t *testing.T) {
	_, f, tt := setup(t, "en", full())
	f.settle(tt)
	f.replyErr = &client.APIError{Status: 409, Code: "ai_not_configured"}
	click(t, tt, "Translate")
	f.settle(tt)
	expect(t, tt, "AI isn’t set up", "Open settings",
		"Summaries and translations use an AI endpoint of your choosing, such as OpenAI or a model on this computer. Set one up in Settings first.")
	absent(t, tt, "Translating. The original stays here until it’s done.")
	click(t, tt, "Open settings")
	if !slices.Equal(f.calls, []string{"settings"}) {
		t.Fatalf("calls %v", f.calls)
	}
	if r, ok := tt.Find("Close"); !ok || r.W < theme.MinTarget || r.H < theme.MinTarget {
		t.Errorf("the close button is %v, under %d by %d", r, theme.MinTarget, theme.MinTarget)
	}
	click(t, tt, "Close")
	absent(t, tt, "AI isn’t set up")
	click(t, tt, "Summarize")
	f.settle(tt)
	expect(t, tt, "AI isn’t set up")
	absent(t, tt, "Summarizing…")
	if _, ok := tt.Find("Summarize"); !ok {
		t.Fatal("the summary button stays pressed")
	}
	// Once AI is set up, asking again takes the card's place.
	f.replyErr, f.reply = nil, client.AIResult{Status: "pending"}
	click(t, tt, "Summarize")
	absent(t, tt, "AI isn’t set up")
	expect(t, tt, "Summarizing…")
}

func TestTranslation(t *testing.T) {
	_, f, tt := setup(t, "en", full())
	f.settle(tt)
	f.reply = client.AIResult{Status: "pending"}
	click(t, tt, "Translate")
	f.settle(tt)
	// The original stays while the translation is made.
	expect(t, tt, "Translating. The original stays here until it’s done.", "Go 1.27 is released", "Faster builds")
	f.ai.Translation = &client.AIResult{Status: "ok", Text: "Go 1.27 ist da\n\nHeute erscheint Go 1.27.\nSchneller.\n",
		Model: "gpt-4o-mini"}
	f.wait(tt, aiPollEvery)
	f.settle(tt)
	expect(t, tt, "Translated by gpt-4o-mini", "Translation", "Original", "Go 1.27 ist da", "Heute erscheint Go 1.27.",
		"Schneller.")
	absent(t, tt, "Go 1.27 is released", "Faster builds", "Translating. The original stays here until it’s done.")
	if !slices.Contains(tt.Announcements(), "Translation ready") {
		t.Fatal("the translation is not announced")
	}
	// The switch and the toolbar button go back to the original and forth.
	click(t, tt, "Original")
	expect(t, tt, "Go 1.27 is released", "Faster builds", "Translated by gpt-4o-mini")
	absent(t, tt, "Schneller.")
	click(t, tt, "Translation")
	expect(t, tt, "Schneller.")
	click(t, tt, "Show the original")
	expect(t, tt, "Faster builds")
	click(t, tt, "Translate")
	expect(t, tt, "Schneller.")
	if len(f.requests) != 1 {
		t.Fatalf("requests %v", f.requests)
	}
}

func TestTranslationCancel(t *testing.T) {
	_, f, tt := setup(t, "en", full())
	f.settle(tt)
	f.reply = client.AIResult{Status: "pending"}
	click(t, tt, "Translate")
	f.settle(tt)
	click(t, tt, "Cancel")
	absent(t, tt, "Translating. The original stays here until it’s done.")
	f.wait(tt, time.Minute)
	if len(f.queue) != 0 {
		t.Fatal("polls after Cancel")
	}
	// The toolbar button cancels too.
	click(t, tt, "Translate")
	f.settle(tt)
	click(t, tt, "Translate")
	absent(t, tt, "Translating. The original stays here until it’s done.")
}

func TestTranslationFailed(t *testing.T) {
	_, f, tt := setup(t, "zh-Hans", full())
	f.settle(tt)
	f.reply = client.AIResult{Status: "failed", Error: "ai_unauthorized"}
	click(t, tt, "翻译")
	f.settle(tt)
	expect(t, tt, "没能翻译。AI 接口不接受这个 API key，去设置里检查一下。", "重试", "Faster builds")
	f.reply = client.AIResult{Status: "ok", Text: "标题\n正文。", Model: "m"}
	click(t, tt, "重试")
	f.settle(tt)
	expect(t, tt, "由 m 翻译", "译文", "原文", "标题", "正文。")
	if len(f.requests) != 2 {
		t.Fatalf("requests %v", f.requests)
	}
}

// Leaving the article stops waiting for its results, and the answers for it are dropped.
func TestAILeave(t *testing.T) {
	s, f, tt := setup(t, "en", full())
	f.settle(tt)
	f.reply = client.AIResult{Status: "ok", Text: "For the article before.", Model: "m"}
	click(t, tt, "Summarize")
	other := entry()
	other.ID, other.Title = 9, "Another post"
	f.nav.Entry = other
	s.Open(other)
	f.article = client.Article{Entry: other, ContentHTML: "<p>Another text.</p>"}
	f.settle(tt)
	expect(t, tt, "Another text.")
	absent(t, tt, "For the article before.", "Summarizing…")
	// A session the server no longer takes shows no card.
	f.replyErr = errExpired
	click(t, tt, "Summarize")
	f.settle(tt)
	absent(t, tt, "Summarizing…", "Couldn’t summarize")
}

func TestTranslated(t *testing.T) {
	title, blocks := translated("\n  Title  \n\nOne.\r\n  \nTwo.")
	if title != "Title" || len(blocks) != 2 || blocks[0].spans[0].text != "One." || blocks[1].spans[0].text != "Two." {
		t.Fatalf("%q %+v", title, blocks)
	}
}

// TestReadingPreferences shows the size and line spacing of the preferences move the text down the page.
func TestReadingPreferences(t *testing.T) {
	below := func(p prefs.Preferences) float32 {
		f := &fake{now: now, article: full(), nav: Nav{Entry: entry(), Scope: "All"}}
		deps := f.deps()
		deps.Reading = func() prefs.Preferences { return p }
		s := New("en", deps)
		s.Use(f)
		tt := ui.NewTester(func(c *ui.Context) { s.View(c, f.nav) }, 1100, 800)
		s.Open(f.article.Entry)
		tt.Frame()
		f.settle(tt)
		r, ok := tt.Find("Smaller binaries")
		if !ok {
			t.Fatalf("texts %v", tt.Texts())
		}
		return r.Y
	}
	standard := below(prefs.Default())
	large, relaxed, small, compact := prefs.Default(), prefs.Default(), prefs.Default(), prefs.Default()
	large.ReadingSize, relaxed.LineSpacing = prefs.SizeXLarge, prefs.SpacingRelaxed
	small.ReadingSize, compact.LineSpacing = prefs.SizeSmall, prefs.SpacingCompact
	if below(large) <= standard || below(relaxed) <= standard || below(small) >= standard || below(compact) >= standard {
		t.Fatalf("standard %v, large %v, relaxed %v, small %v, compact %v", standard, below(large), below(relaxed),
			below(small), below(compact))
	}
}

// TestReadingScale shows the body is 16 with line height 1.7 by default, 1.85 in Chinese, and that the four text
// sizes and three line spacings give the line heights of the design, in both languages.
func TestReadingScale(t *testing.T) {
	for _, scale := range textScales {
		t.Run(fmt.Sprint("text=", scale), func(t *testing.T) { readingScale(t, scale) })
	}
}

// textScales are the desktop text sizes the geometry tests run at: the design's 13 and the 14 of Linux and Windows.
var textScales = []float32{1, 14.0 / 13}

func readingScale(t *testing.T, scale float32) {
	var unit float32 // one design pixel in the theme in use
	line := func(p prefs.Preferences, han bool) float32 {
		art := full()
		if han {
			art.Title = "Go 1.27 发布"
			art.ExtractedHTML = `<p>今天 Go 团队很高兴发布新版本。</p><ul><li>更快的构建</li></ul>`
		}
		f := &fake{now: now, article: art, nav: Nav{Entry: art.Entry, Scope: "All"}}
		deps := f.deps()
		deps.Reading = func() prefs.Preferences { return p }
		s := New("en", deps)
		s.Use(f)
		// Tall enough to hold the whole article: a line cut by the window reports the height that shows.
		tt := ui.NewTester(func(c *ui.Context) { unit = theme.Px(c.Theme(), 1); s.View(c, f.nav) }, 1100, 2000)
		tt.SetPreferences(ui.Preferences{TextScale: scale})
		s.Open(art.Entry)
		tt.Frame()
		f.settle(tt)
		text := "Faster builds"
		if han {
			text = "今天 Go 团队很高兴发布新版本。"
		}
		r, ok := tt.Find(text)
		if !ok {
			t.Fatalf("texts %v", tt.Texts())
		}
		return r.H
	}
	near := func(got, want float32) bool { return got > want-0.6*unit && got < want+0.6*unit }
	for _, tc := range []struct {
		size, spacing string
		body          float32
		latin, han    float32
	}{
		{prefs.SizeMedium, prefs.SpacingStandard, 16, 1.7, 1.85},
		{prefs.SizeSmall, prefs.SpacingCompact, 14.9, 1.55, 1.7},
		{prefs.SizeLarge, prefs.SpacingRelaxed, 18.1, 1.85, 2.0},
		{prefs.SizeXLarge, prefs.SpacingStandard, 20.3, 1.7, 1.85},
	} {
		p := prefs.Preferences{ReadingSize: tc.size, LineSpacing: tc.spacing}
		if got := line(p, false); !near(got, tc.body*tc.latin*unit) {
			t.Errorf("%s %s: line %v, want %v", tc.size, tc.spacing, got, tc.body*tc.latin*unit)
		}
		if got := line(p, true); !near(got, tc.body*tc.han*unit) {
			t.Errorf("%s %s, Chinese: line %v, want %v", tc.size, tc.spacing, got, tc.body*tc.han*unit)
		}
	}
}

// TestToolbarNames shows Summarize and Translate carry their names beside the icon, and only the icon in a window
// narrower than 760.
func TestToolbarNames(t *testing.T) {
	for _, scale := range textScales {
		t.Run(fmt.Sprint("text=", scale), func(t *testing.T) { toolbarNames(t, scale) })
	}
}

func toolbarNames(t *testing.T, scale float32) {
	f := &fake{now: now, article: full(), nav: Nav{Entry: full().Entry, Scope: "All"}}
	s := New("en", f.deps())
	s.Use(f)
	var unit float32
	tt := ui.NewTester(func(c *ui.Context) { unit = theme.Px(c.Theme(), 1); s.View(c, f.nav) }, 1100, 800)
	tt.SetPreferences(ui.Preferences{TextScale: scale})
	s.Open(f.article.Entry)
	tt.Frame()
	f.settle(tt)
	wide, ok := tt.Find("Summarize")
	if !ok || wide.W < 60*unit {
		t.Fatalf("wide: %v %v", wide, ok)
	}
	tt.SetSize(700, 800)
	tt.Frame()
	narrow, ok := tt.Find("Summarize")
	if !ok || narrow.W > 32*unit {
		t.Fatalf("narrow: %v %v", narrow, ok)
	}
}

// TestLineHeightFollowsLanguage shows the stored language of an entry picks the Chinese or the Latin line height,
// and that the characters decide only when no language is stored.
func TestLineHeightFollowsLanguage(t *testing.T) {
	for _, scale := range textScales {
		t.Run(fmt.Sprint("text=", scale), func(t *testing.T) {
			var unit float32
			line := func(language string, han bool) float32 {
				art := full()
				art.Language = language
				text := "Faster builds"
				if han {
					art.Title = "Go 1.27 发布"
					text = "今天 Go 团队很高兴发布新版本。"
					art.ExtractedHTML = `<p>` + text + `</p>`
				}
				f := &fake{now: now, article: art, nav: Nav{Entry: art.Entry, Scope: "All"}}
				s := New("en", f.deps())
				s.Use(f)
				tt := ui.NewTester(func(c *ui.Context) { unit = theme.Px(c.Theme(), 1); s.View(c, f.nav) }, 1100, 2000)
				tt.SetPreferences(ui.Preferences{TextScale: scale})
				s.Open(art.Entry)
				tt.Frame()
				f.settle(tt)
				r, ok := tt.Find(text)
				if !ok {
					t.Fatalf("texts %v", tt.Texts())
				}
				return r.H
			}
			near := func(got, want float32) bool { return got > want-0.6*unit && got < want+0.6*unit }
			tall, short := float32(16*1.85), float32(16*1.7)
			for _, tc := range []struct {
				name, language string
				han            bool
				want           float32
			}{
				{"Latin text stored as Chinese", "zh-Hans", false, tall},
				{"Latin text stored as Traditional Chinese", "zh-Hant", false, tall},
				{"Latin text stored as Japanese", "ja", false, tall},
				{"Chinese text stored as English", "en", true, short},
				{"Chinese text stored as German", "de", true, short},
				{"Latin text without a language", "", false, short},
				{"Chinese text without a language", "", true, tall},
			} {
				if got := line(tc.language, tc.han); !near(got, tc.want*unit) {
					t.Errorf("%s: line %v, want %v", tc.name, got, tc.want*unit)
				}
			}
		})
	}
}

// several serves one article for each id, and counts what it is asked.
type several struct {
	*fake
	bodies  []int64
	reads   []int64
	failing map[int64]bool
}

func (a *several) Entry(_ context.Context, id int64) (client.Article, error) {
	a.bodies = append(a.bodies, id)
	if a.failing[id] {
		return client.Article{}, errors.New("unreachable")
	}
	article := full()
	article.ID = id
	article.Title = fmt.Sprintf("Article %d", id)
	article.ExtractedHTML = fmt.Sprintf("<p>Text %d.</p>", id)
	return article, nil
}

func (a *several) AI(_ context.Context, id int64) (client.AIResults, error) {
	a.reads = append(a.reads, id)
	return client.AIResults{TargetLanguage: "en", Summary: &client.AIResult{Status: "ok", Text: fmt.Sprintf("Short %d.", id)}}, nil
}

func prefetching(t *testing.T) (*Screen, *several, *ui.Tester) {
	t.Helper()
	a := &several{fake: &fake{now: now}, failing: map[int64]bool{}}
	s := New("en", a.deps())
	s.Use(a)
	tt := ui.NewTester(func(c *ui.Context) {
		s.View(c, Nav{Entry: s.article.Entry, Scope: "All", Previous: true, Next: true, Keys: true})
	}, 1100, 800)
	s.Open(client.Entry{ID: 10, Kind: "article", Title: "Article 10"})
	tt.Frame()
	a.settle(tt)
	return s, a, tt
}

func TestPrefetchHit(t *testing.T) {
	s, a, tt := prefetching(t)
	s.Prefetch(9, 11)
	a.settle(tt)
	if !slices.Equal(a.bodies, []int64{10, 9, 11}) || len(a.requests) != 0 {
		t.Fatalf("bodies %v, reads %v, requests %v", a.bodies, a.reads, a.requests)
	}
	s.Open(client.Entry{ID: 11, Kind: "article", Title: "Article 11"})
	tt.Frame()
	if s.loading {
		t.Fatal("a prefetched article shows the wait")
	}
	expect(t, tt, "Text 11.", "Short 11.")
	if len(a.bodies) != 3 {
		t.Fatalf("bodies %v", a.bodies)
	}
	// The summary is read again, as it may have changed.
	reads := len(a.reads)
	if len(a.queue) != 1 {
		t.Fatalf("%d jobs queued", len(a.queue))
	}
	a.settle(tt)
	if len(a.reads) != reads+1 || len(a.requests) != 0 {
		t.Fatalf("reads %v, requests %v", a.reads, a.requests)
	}
	// A miss waits as before.
	s.Open(client.Entry{ID: 40, Kind: "article", Title: "Article 40"})
	tt.Frame()
	if !s.loading {
		t.Fatal("an article not got ahead shows at once")
	}
	absent(t, tt, "Text 40.")
}

func TestPrefetchBounded(t *testing.T) {
	s, a, tt := prefetching(t)
	s.Prefetch(9, 11, 12, 13)
	a.settle(tt)
	if len(s.cache) > keepCached {
		t.Fatalf("%d articles kept", len(s.cache))
	}
	// Only the article read and its neighbours are kept.
	s.Prefetch(9, 11)
	a.settle(tt)
	for _, c := range s.cache {
		if c.id != 10 && c.id != 9 && c.id != 11 {
			t.Fatalf("kept %d", c.id)
		}
	}
	s.Prefetch(9, 11)
	if len(a.queue) != 0 {
		t.Fatal("articles kept are got again")
	}
}

func TestPrefetchDropped(t *testing.T) {
	s, a, tt := prefetching(t)
	s.Prefetch(9, 11)
	a.settle(tt)
	s.Use(a)
	if len(s.cache) != 0 {
		t.Fatalf("%d articles kept for the server before", len(s.cache))
	}
	// An answer for the server before comes after Use and is dropped.
	s.Open(client.Entry{ID: 10, Kind: "article"})
	s.Prefetch(11)
	s.Use(a)
	a.settle(tt)
	if len(s.cache) != 0 || len(s.fetching) != 0 {
		t.Fatalf("kept %d, getting %d", len(s.cache), len(s.fetching))
	}
	s.Open(client.Entry{ID: 10, Kind: "article"})
	a.settle(tt)
	s.Prefetch(11)
	s.Forget()
	a.settle(tt)
	if len(s.cache) != 0 {
		t.Fatal("an answer for the list before is kept")
	}
	s.Open(client.Entry{ID: 11, Kind: "article"})
	if !s.loading {
		t.Fatal("an article from the list before shows at once")
	}
}

func TestPrefetchFailsSilently(t *testing.T) {
	s, a, tt := prefetching(t)
	a.failing[11] = true
	s.Prefetch(11)
	a.settle(tt)
	if s.failed || len(s.cache) != 1 || len(a.requests) != 0 {
		t.Fatalf("failed %v, kept %d", s.failed, len(s.cache))
	}
	s.Open(client.Entry{ID: 11, Kind: "article"})
	if !s.loading {
		t.Fatal("a failed prefetch shows as a hit")
	}
}

// otherFace is an installed font family that draws sample wider or narrower than the default, so a test can tell it
// from the interface font; the test skips where there is none.
func otherFace(t *testing.T, sample string) string {
	t.Helper()
	base, _ := ui.ShapeText(sample, ui.Font{Size: 16}, 0).Size()
	for _, family := range ui.FontFamilies() {
		if w, _ := ui.ShapeText(sample, ui.Font{Family: family.Name, Size: 16}, 0).Size(); w != base {
			return family.Name
		}
	}
	t.Skip("no installed font differs from the default")
	return ""
}

// lead stands for the head of the page in shots: the text of the first paragraph marks where it ends.
const lead = "Today the Go team is happy to release Go 1.27."

// shots opens article with the reading font face and gives the pixels of each of texts as the window draws them.
func shots(t *testing.T, face string, article client.Article, texts ...string) []*image.RGBA {
	t.Helper()
	f := &fake{now: now, article: article, nav: Nav{Entry: article.Entry, Scope: "All"}}
	deps := f.deps()
	deps.ReadingFont = func() string { return face }
	s := New("en", deps)
	s.Use(f)
	tt := ui.NewTester(func(c *ui.Context) { s.View(c, f.nav) }, 1100, 2000)
	s.Open(article.Entry)
	tt.Frame()
	f.settle(tt)
	var out []*image.RGBA
	for _, text := range texts {
		r, ok := tt.Find(text)
		if !ok {
			t.Fatalf("no %q in %v", text, tt.Texts())
		}
		box := image.Rect(int(r.X), int(r.Y), int(r.X+r.W)+1, int(r.Y+r.H)+1)
		if text == lead {
			box = image.Rect(0, 0, 1100, int(r.Y)) // everything above the first paragraph: toolbar, title, byline
		}
		shot := image.NewRGBA(image.Rect(0, 0, box.Dx(), box.Dy()))
		draw.Draw(shot, shot.Bounds(), tt.Image(), box.Min, draw.Src)
		out = append(out, shot)
	}
	return out
}

// TestReadingFont shows the body, the paragraphs and the list items, takes the reading font, while the title, the
// headings and the code keep the interface or the monospace font.
func TestReadingFont(t *testing.T) {
	art := full()
	art.ExtractedHTML += `<p>Plain paragraph</p><pre><code>go build ./...</code></pre>`
	texts := []string{lead, "Tools", "Faster builds", "Plain paragraph", "go build ./..."}
	plain := shots(t, "", art, texts...)
	got := shots(t, otherFace(t, "Faster builds Plain paragraph"), art, texts...)
	for i, text := range texts {
		same := bytes.Equal(got[i].Pix, plain[i].Pix)
		if body := text == "Faster builds" || text == "Plain paragraph"; body == same {
			t.Errorf("%q: drawn as without the reading font is %v, want %v", text, same, !body)
		}
	}
}

// TestReadingColumn shows the text column is 680 at the medium text size and 635, 771 and 861 at the others, scaled
// with the theme's text size, and that the toolbar's ends follow it.
func TestReadingColumn(t *testing.T) {
	for _, scale := range textScales {
		for size, column := range map[string]float32{prefs.SizeSmall: 635, prefs.SizeMedium: 680, prefs.SizeLarge: 771,
			prefs.SizeXLarge: 861} {
			t.Run(fmt.Sprint("text=", scale, " ", size), func(t *testing.T) {
				art := full()
				art.ExtractedHTML = `<p>` + strings.Repeat("word ", 400) + `</p>`
				f := &fake{now: now, article: art, nav: Nav{Entry: art.Entry, Scope: "All"}}
				deps := f.deps()
				deps.Reading = func() prefs.Preferences { return prefs.Preferences{ReadingSize: size} }
				s := New("en", deps)
				s.Use(f)
				var unit float32
				tt := ui.NewTester(func(c *ui.Context) { unit = theme.Px(c.Theme(), 1); s.View(c, f.nav) }, 1800, 1200)
				tt.SetPreferences(ui.Preferences{TextScale: scale})
				s.Open(art.Entry)
				tt.Frame()
				f.settle(tt)
				body := ""
				for _, text := range tt.Texts() {
					if strings.HasPrefix(text, "word word") {
						body = text
					}
				}
				b, ok := tt.Find(body)
				if !ok {
					t.Fatalf("no body in %v", tt.Texts())
				}
				want := (column - 64) * unit // the column less its padding of 32 on each side
				if b.W < want-unit || b.W > want+unit {
					t.Errorf("body %v wide, want %v", b.W, want)
				}
			})
		}
	}
}

// TestChineseTypesetting shows the gap between Chinese and Latin text only when the switch is on and the text is
// Chinese, and never in code. The toolkit offers no letter spacing to read back, so the test measures how far the ink of
// the paragraph reaches.
func TestChineseTypesetting(t *testing.T) {
	for _, scale := range textScales {
		t.Run(fmt.Sprint("text=", scale), func(t *testing.T) { chineseTypesetting(t, scale) })
	}
}

func chineseTypesetting(t *testing.T, scale float32) {
	var unit float32
	// reach is the right edge of the ink of the paragraph with the text.
	reach := func(language, html, text string, on bool) float32 {
		art := full()
		art.Language = language
		art.ExtractedHTML = html
		f := &fake{now: now, article: art, nav: Nav{Entry: art.Entry, Scope: "All"}}
		deps := f.deps()
		p := prefs.Default()
		p.ChineseTypesetting = on
		deps.Reading = func() prefs.Preferences { return p }
		s := New("en", deps)
		s.Use(f)
		tt := ui.NewTester(func(c *ui.Context) { unit = theme.Px(c.Theme(), 1); s.View(c, f.nav) }, 1100, 2000)
		tt.SetPreferences(ui.Preferences{TextScale: scale})
		s.Open(art.Entry)
		tt.Frame()
		f.settle(tt)
		r, ok := tt.Find(text)
		if !ok {
			t.Fatalf("%q not found", text)
		}
		img := tt.Image()
		bg := img.At(int(r.X+r.W)-1, int(r.Y)+2)
		right := 0
		for y := int(r.Y); y < int(r.Y+r.H); y++ {
			for x := int(r.X); x < int(r.X+r.W); x++ {
				if img.At(x, y) != bg {
					right = max(right, x)
				}
			}
		}
		return float32(right)
	}
	const mixed = "使用Go语言写的RSS阅读器"
	on, off := reach("zh-Hans", "<p>"+mixed+"</p>", mixed, true), reach("zh-Hans", "<p>"+mixed+"</p>", mixed, false)
	// Four boundaries, a quarter of the 16 body size each; an engine that puts glyphs on whole pixels (Pango) may lose
	// up to a pixel at each.
	const boundaries = 4
	want := boundaries * 0.25 * 16 * unit
	if d := on - off; d < want-boundaries || d > want+boundaries {
		t.Errorf("Chinese text widened by %v, want %v", d, want)
	}
	const coded = "使用Go语言"
	html := "<p>使用<code>Go</code>语言</p>"
	if on, off := reach("zh-Hans", html, coded, true), reach("zh-Hans", html, coded, false); on != off {
		t.Errorf("code reached %v with the switch on and %v off", on, off)
	}
	if on, off := reach("en", "<p>"+mixed+"</p>", mixed, true), reach("en", "<p>"+mixed+"</p>", mixed, false); on != off {
		t.Errorf("English reached %v with the switch on and %v off", on, off)
	}
}

// TestChineseTypesettingLink shows a link whose text is spaced stays one link: as many stops of the Tab key as the same
// link unspaced, the whole text as its name, and one click opens it.
func TestChineseTypesettingLink(t *testing.T) {
	const text = "trigram分词器"
	stops := map[bool]int{}
	for _, on := range []bool{false, true} {
		art := full()
		art.Language = "zh-Hans"
		art.ExtractedHTML = `<p>支持<a href="https://example.com/trigram">` + text + `</a>的搜索</p>`
		f := &fake{now: now, article: art, nav: Nav{Entry: art.Entry, Scope: "All"}}
		deps := f.deps()
		p := prefs.Default()
		p.ChineseTypesetting = on
		deps.Reading = func() prefs.Preferences { return p }
		s := New("en", deps)
		s.Use(f)
		tt := ui.NewTester(func(c *ui.Context) { s.View(c, f.nav) }, 1100, 2000)
		s.Open(art.Entry)
		tt.Frame()
		f.settle(tt)
		if _, ok := tt.Find(text); !ok {
			t.Fatalf("the link is not named %q in %v", text, tt.Texts())
		}
		for range 40 {
			tt.Key(0, ui.KeyTab)
			if tt.Focused(text) {
				stops[on]++
			}
		}
		if err := tt.Click(text); err != nil {
			t.Fatal(err)
		}
		if opened := tt.OpenedURLs(); !slices.Equal(opened, []string{"https://example.com/trigram"}) {
			t.Fatalf("switch %v: opened %v", on, opened)
		}
	}
	if stops[true] == 0 || stops[true] != stops[false] {
		t.Errorf("stops of the Tab key on the link: %d spaced, %d unspaced", stops[true], stops[false])
	}
}

func TestReadingTime(t *testing.T) {
	for _, tc := range []struct {
		name     string
		language string
		edit     func(e *client.Entry)
		want     string
	}{
		{"minutes", "en", func(e *client.Entry) { e.ReadingSec = 360 }, "6 min read"},
		{"chinese", "zh-Hans", func(e *client.Entry) { e.ReadingSec = 360 }, "6 分钟阅读"},
		{"under a minute", "en", func(e *client.Entry) { e.ReadingSec = 40 }, ""},
		{"unknown", "en", func(e *client.Entry) {}, ""},
		{"duration", "en", func(e *client.Entry) { e.ReadingSec, e.DurationSec = 360, 360 }, ""},
		{"video", "en", func(e *client.Entry) { e.ReadingSec, e.Kind = 360, "video" }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			art := full()
			tc.edit(&art.Entry)
			_, f, tt := setup(t, tc.language, art)
			f.settle(tt)
			for _, text := range tt.Texts() {
				if strings.Contains(text, "min read") || strings.Contains(text, "分钟阅读") {
					if text != tc.want {
						t.Fatalf("shows %q, want %q", text, tc.want)
					}
					return
				}
			}
			if tc.want != "" {
				t.Fatalf("no %q: %v", tc.want, tt.Texts())
			}
		})
	}
}

// With the sidebar hidden the window buttons sit over the reader's toolbar, which leaves them room.
func TestToolbarInset(t *testing.T) {
	a := full()
	f := &fake{now: now, article: a, nav: Nav{Entry: a.Entry, Scope: "All", Keys: true}}
	s := New("en", f.deps())
	s.Use(f)
	tt := ui.NewTester(func(c *ui.Context) { s.View(c, f.nav) }, 640, 540)
	s.Open(a.Entry)
	tt.SetTitleBar(ui.TitleBar{Height: 52, Left: 78})
	shown, ok := tt.Find("Back to All")
	if !ok {
		t.Fatal("no Back button")
	}
	f.nav.Controls = true
	tt.Frame()
	hidden, ok := tt.Find("Back to All")
	if !ok {
		t.Fatal("no Back button")
	}
	if hidden.X < 78 {
		t.Fatalf("Back moved from %v to %v, want it clear of the 78 wide window controls", shown.X, hidden.X)
	}
}

// tipShows hovers the element with the given text and says whether a tooltip with the same text came up: the text then
// shows once more, as the tooltip's own.
func tipShows(t *testing.T, tt *ui.Tester, name string) bool {
	t.Helper()
	count := func() int {
		n := 0
		for _, text := range tt.Texts() {
			if text == name {
				n++
			}
		}
		return n
	}
	r, ok := tt.Find(name)
	if !ok {
		t.Fatalf("no %q in %v", name, tt.Texts())
	}
	tt.Move(0, 0)
	tt.Frame()
	tt.Advance(2 * time.Second)
	tt.Frame()
	before := count()
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	tt.Advance(2 * time.Second)
	tt.Frame()
	return count() > before
}

// The back button names itself in text and has no tooltip, nor has the Summarize toggle, in a wide window or a narrow
// one where only its icon shows; the icon-only buttons of the toolbar keep theirs.
func TestTooltips(t *testing.T) {
	for _, width := range []int{1400, 700} {
		s, f, tt := setup(t, "en", full())
		tt.SetSize(width, 900)
		f.settle(tt)
		tt.Frame()
		back := strings.ReplaceAll(s.text.Back, "{scope}", "All")
		if tipShows(t, tt, back) {
			t.Errorf("width %d: %q has a tooltip", width, back)
		}
		if tipShows(t, tt, "Summarize") {
			t.Errorf("width %d: Summarize has a tooltip", width)
		}
		if !tipShows(t, tt, s.text.CopyLink) {
			t.Errorf("width %d: %q has no tooltip", width, s.text.CopyLink)
		}
	}
}

func TestAlsoLine(t *testing.T) {
	article := full()
	article.Group = &client.Group{By: "link", Entries: []client.GroupMember{
		{ID: 18, SourceID: 3, SourceTitle: "Gopher", Title: "Go 1.27 is out", CreatedAt: now.Add(-4 * time.Minute).UnixMilli()},
		{ID: 28, SourceID: 4, SourceTitle: "Photos", Title: "Mirror", CreatedAt: now.Add(-3 * time.Hour).UnixMilli()},
	}}
	_, f, tt := setup(t, "en", article)
	f.nav.Entry = article.Entry
	f.settle(tt)
	expect(t, tt, "Also in", "Gopher", "Photos")
	if err := tt.Click("Photos Oct 18, 9:00 AM"); err != nil {
		t.Fatalf("%v in %v", err, tt.Texts())
	}
	if !slices.Equal(f.calls, []string{"copy 28"}) {
		t.Fatalf("calls %v", f.calls)
	}
}

func TestNoAlsoLineForOneSource(t *testing.T) {
	_, f, tt := setup(t, "en", full())
	f.settle(tt)
	absent(t, tt, "Also in")
}

func TestNoAlsoLineWithoutOpenCopy(t *testing.T) {
	article := full()
	article.Group = &client.Group{By: "link", Entries: []client.GroupMember{
		{ID: 18, SourceID: 3, SourceTitle: "Gopher", CreatedAt: now.Add(-4 * time.Minute).UnixMilli()}}}
	f := &fake{now: now, article: article, nav: Nav{Entry: article.Entry, Scope: "All", Keys: true}}
	d := f.deps()
	d.OpenCopy = nil
	s := New("en", d)
	s.Use(f)
	tt := ui.NewTester(func(c *ui.Context) { s.View(c, f.nav) }, 1100, 800)
	s.Open(article.Entry)
	tt.Frame()
	f.settle(tt)
	absent(t, tt, "Also in", "Gopher")
}

// download runs the queued work until a picture is being fetched, and leaves that request in the queue.
func (f *fake) download(t *testing.T, s *Screen, tt *ui.Tester) {
	t.Helper()
	for s.pics.Active() == 0 {
		if len(f.queue) == 0 {
			t.Fatal("no picture was asked for")
		}
		work := f.queue[0]
		f.queue = f.queue[1:]
		work()()
		tt.Frame()
	}
}

// TestPictureCancelledByOtherArticle opens another article while a picture is downloading: the request is cancelled and
// its result is not applied to the new article.
func TestPictureCancelledByOtherArticle(t *testing.T) {
	s, f, tt := setup(t, "en", full())
	f.download(t, s, tt)
	other := entry()
	other.ID = 9
	s.Open(other)
	for _, work := range f.queue {
		work()()
	}
	if len(f.imageCtx) != 1 || f.imageCtx[0].Err() == nil {
		t.Fatalf("the download of the article before was not cancelled: %v", f.imageCtx)
	}
	if s.pics.Len() != 0 || s.pics.Active() != 0 {
		t.Fatalf("a result for the article before was applied: %d pictures, %d downloading", s.pics.Len(), s.pics.Active())
	}
}

// TestPictureCancelledByServer switches to another server while a picture is downloading.
func TestPictureCancelledByServer(t *testing.T) {
	s, f, tt := setup(t, "en", full())
	f.download(t, s, tt)
	s.Use(&fake{now: now})
	for _, work := range f.queue {
		work()()
	}
	if len(f.imageCtx) != 1 || f.imageCtx[0].Err() == nil {
		t.Fatalf("the download for the server before was not cancelled: %v", f.imageCtx)
	}
	for _, address := range f.pictures {
		p := s.pics.Peek(address)
		if p != nil && (p.Bitmap != nil || p.Failed) {
			t.Fatalf("a result for the server before was applied to %s", address)
		}
	}
}

// TestPictureRetried tries a picture that failed again when it is drawn after pictures.Retry, and gives up after
// pictures.Tries downloads.
func TestPictureRetried(t *testing.T) {
	const address = "https://go.dev/images/gopher.png"
	t.Run("works the second time", func(t *testing.T) {
		s, f, tt := setup(t, "en", full())
		f.imageErr = 1
		f.settle(tt)
		if p := s.pics.Peek(address); p == nil || !p.Failed {
			t.Fatalf("the picture did not fail: %+v", p)
		}
		tt.Frame()
		f.settle(tt)
		if len(f.pictures) != 1 {
			t.Fatalf("asked again at once: %v", f.pictures)
		}
		f.now = f.now.Add(pictures.Retry)
		tt.Frame()
		f.settle(tt)
		if p := s.pics.Peek(address); len(f.pictures) != 2 || p == nil || p.Failed || p.Bitmap == nil {
			t.Fatalf("the picture was not got the second time: %v %+v", f.pictures, p)
		}
	})
	t.Run("gives up", func(t *testing.T) {
		s, f, tt := setup(t, "en", full())
		f.imageErr = 100
		f.settle(tt)
		for range 2 * pictures.Tries {
			f.now = f.now.Add(pictures.Retry)
			tt.Frame()
			f.settle(tt)
		}
		if len(f.pictures) != pictures.Tries || !s.pics.Peek(address).Failed {
			t.Fatalf("asked %d times, want %d: %v", len(f.pictures), pictures.Tries, f.pictures)
		}
	})
}

// TestReadingWidthAndPresetLive shows the line width and the reading appearance apply to an open reader at the next
// frame, with the article kept where it is scrolled.
func TestReadingWidthAndPresetLive(t *testing.T) {
	art := full()
	art.ExtractedHTML = strings.Repeat(`<p>`+strings.Repeat("word ", 120)+`</p>`, 12)
	f := &fake{now: now, article: art, nav: Nav{Entry: art.Entry, Scope: "All"}}
	deps := f.deps()
	p := prefs.Default()
	deps.Reading = func() prefs.Preferences { return p }
	s := New("en", deps)
	s.Use(f)
	tt := ui.NewTester(func(c *ui.Context) { s.View(c, f.nav) }, 1800, 700)
	s.Open(art.Entry)
	tt.Frame()
	f.settle(tt)
	body := func() ui.Rect {
		for _, text := range tt.Texts() {
			if strings.HasPrefix(text, "word word") {
				r, _ := tt.Find(text)
				return r
			}
		}
		t.Fatalf("no body in %v", tt.Texts())
		return ui.Rect{}
	}
	standard := body().W
	s.scroll.Y = 300
	tt.Frame()
	y := s.scroll.Y
	if y == 0 {
		t.Fatal("the article does not scroll")
	}
	p.ReadingWidth = prefs.WidthWide
	tt.Frame()
	if wide := body().W; wide <= standard*1.1 {
		t.Errorf("wide %v, standard %v", wide, standard)
	}
	p.ReadingWidth = prefs.WidthNarrow
	tt.Frame()
	if narrow := body().W; narrow >= standard*0.95 {
		t.Errorf("narrow %v, standard %v", narrow, standard)
	}
	p.ReadingPreset = prefs.PresetSepia
	tt.Frame()
	if s.scroll.Y == 0 {
		t.Errorf("the scroll was %v and is back at the top", y)
	}
}

// paperOf opens an article in the reader with a reading appearance and returns the tester, the theme of the window and
// the paper theme the article is drawn with. Beside the reader sits a box without a color of its own, which shows the
// window's root background.
func paperOf(t *testing.T, preset string, dark bool, setup func(f *fake)) (*ui.Tester, *ui.Theme, *ui.Theme, *Screen) {
	t.Helper()
	art := full()
	f := &fake{now: now, article: art, nav: Nav{Entry: art.Entry, Scope: "All"}}
	if setup != nil {
		setup(f)
	}
	deps := f.deps()
	deps.Reading = func() prefs.Preferences { return prefs.Preferences{ReadingPreset: preset} }
	s := New("en", deps)
	s.Use(f)
	var main, paper *ui.Theme
	tt := ui.NewTester(func(c *ui.Context) {
		main = c.Theme()
		paper = theme.ReadingTheme(main, preset)
		ui.Row(c).FillWidth().FillHeight().Children(func() {
			s.View(c, f.nav)
			ui.Box(c).Width(60).FillHeight()
		})
		if c.Theme() != main {
			t.Errorf("the reader left the window on a theme other than its own")
		}
	}, 1100, 800)
	tt.SetDark(dark)
	s.Open(art.Entry)
	tt.Frame()
	f.settle(tt)
	tt.Frame()
	return tt, main, paper, s
}

// colorCount is how many pixels of the image are exactly the color.
func colorCount(img *image.RGBA, want ui.Color) int {
	n := 0
	for i := 0; i+3 < len(img.Pix); i += 4 {
		got := ui.Color{R: img.Pix[i], G: img.Pix[i+1], B: img.Pix[i+2]}
		if sameColor(got, want) {
			n++
		}
	}
	return n
}

// TestReadingAppearanceKeepsTheWindowTheme shows the window's root background stays the theme's after the reader
// draws a preset paper, the article is on that paper, and the failed state draws its icon in the paper's own danger color.
func TestReadingAppearanceKeepsTheWindowTheme(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, preset := range []string{prefs.PresetSepia, prefs.PresetMist} {
			t.Run(fmt.Sprint(preset, " dark=", dark), func(t *testing.T) {
				tt, main, paper, _ := paperOf(t, preset, dark, nil)
				img := tt.Image()
				if main.Background == paper.Background {
					t.Fatal("the preset does not change the paper")
				}
				// The box beside the article shows the root; the article shows the paper.
				root := img.RGBAAt(img.Bounds().Dx()-4, img.Bounds().Dy()/2)
				if got := (ui.Color{R: root.R, G: root.G, B: root.B}); !sameColor(got, main.Background) {
					t.Errorf("root background %v, want the theme's %v", got, main.Background)
				}
				if colorCount(img, paper.Background) == 0 {
					t.Error("no paper drawn")
				}
				tt, main, paper, _ = paperOf(t, preset, dark, func(f *fake) { f.err = errors.New("down") })
				img = tt.Image()
				if colorCount(img, paper.Danger) == 0 || colorCount(img, main.Danger) != 0 {
					t.Errorf("failed icon: paper danger %d, theme danger %d", colorCount(img, paper.Danger), colorCount(img, main.Danger))
				}
			})
		}
	}
}

// sameColor tells two colors apart by more than the rounding of an 8 bit channel.
func sameColor(a, b ui.Color) bool {
	near := func(x, y uint8) bool { return int(x)-int(y) < 2 && int(y)-int(x) < 2 }
	return near(a.R, b.R) && near(a.G, b.G) && near(a.B, b.B)
}

// TestReadingAppearanceMenusStayNative shows the More menu is the system's menu, which no appearance recolors: it
// offers the same items on paper as without, and it opens from a window that is back on the theme's own colors.
func TestReadingAppearanceMenusStayNative(t *testing.T) {
	menu := func(preset string) []string {
		tt, _, _, s := paperOf(t, preset, false, nil)
		tt.SetSize(500, 800)
		tt.Frame()
		if err := tt.Click(s.text.More); err != nil {
			t.Fatal(err)
		}
		return tt.Menu()
	}
	plain, sepia := menu(""), menu(prefs.PresetSepia)
	if len(plain) == 0 || !slices.Equal(plain, sepia) {
		t.Fatalf("menu %v on paper, %v without", sepia, plain)
	}
}
