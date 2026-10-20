package timeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/prefs"
	"github.com/yangflow/oriel/internal/ui/theme"
)

var now = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func ago(d time.Duration) int64 { return now.Add(-d).UnixMilli() }

// fake stands in for the server and the background: work waits in a queue until step runs it.
type fake struct {
	queue    []func() func()
	now      time.Time
	entries  []client.Entry // newest first
	sources  []client.Source
	queries  []client.Query
	patches  map[int64][]client.EntryPatch
	marked   []client.ReadScope
	unmarked [][]int64
	pictures []string
	articles []int64 // the entries asked for in full

	entriesErr, setErr, markErr             error
	connects, settings, aiSettings, expired int
}

func (f *fake) Entries(_ context.Context, q client.Query) (client.Page, error) {
	f.queries = append(f.queries, q)
	if f.entriesErr != nil {
		return client.Page{}, f.entriesErr
	}
	var found []client.Entry
	for _, e := range f.entries {
		if q.Kind != "" && e.Kind != q.Kind || q.Source != 0 && e.SourceID != q.Source || q.Unread && e.IsRead ||
			q.Starred && !e.Starred || q.Search != "" && !strings.Contains(strings.ToLower(e.Title), q.Search) {
			continue
		}
		found = append(found, e)
	}
	start, _ := strconv.Atoi(q.Cursor)
	end := min(start+q.Limit, len(found))
	page := client.Page{Entries: slices.Clone(found[start:end])}
	if end < len(found) {
		page.Next = strconv.Itoa(end)
	}
	return page, nil
}

func (f *fake) Sources(context.Context) ([]client.Source, error) {
	if f.entriesErr != nil {
		return nil, f.entriesErr
	}
	return slices.Clone(f.sources), nil
}

func (f *fake) SetEntry(_ context.Context, id int64, patch client.EntryPatch) (client.Entry, error) {
	f.patches[id] = append(f.patches[id], patch)
	return client.Entry{}, f.setErr
}

func (f *fake) MarkRead(_ context.Context, scope client.ReadScope) ([]int64, error) {
	f.marked = append(f.marked, scope)
	if f.markErr != nil {
		return nil, f.markErr
	}
	var ids []int64
	for i, e := range f.entries {
		if !e.IsRead && e.ID <= scope.UpTo && (scope.Kind == "" || e.Kind == scope.Kind) {
			f.entries[i].IsRead = true
			ids = append(ids, e.ID)
		}
	}
	f.count()
	return ids, nil
}

func (f *fake) MarkUnread(_ context.Context, ids []int64) (int, error) {
	f.unmarked = append(f.unmarked, ids)
	if f.markErr != nil {
		return 0, f.markErr
	}
	n := 0
	for i, e := range f.entries {
		if e.IsRead && slices.Contains(ids, e.ID) {
			f.entries[i].IsRead = false
			n++
		}
	}
	f.count()
	return n, nil
}

// count sets the unread counts of the sources from the entries.
func (f *fake) count() {
	for i := range f.sources {
		f.sources[i].UnreadCount = 0
		for _, e := range f.entries {
			if e.SourceID == f.sources[i].ID && !e.IsRead {
				f.sources[i].UnreadCount++
			}
		}
	}
}

func (f *fake) Entry(_ context.Context, id int64) (client.Article, error) {
	f.articles = append(f.articles, id)
	for _, e := range f.entries {
		if e.ID == id {
			e.ExtractStatus = "ok"
			return client.Article{Entry: e, ExtractedHTML: "<p>The text of " + e.Title + ".</p>"}, nil
		}
	}
	return client.Article{}, &client.APIError{Status: 404, Code: "not_found"}
}

func (f *fake) Image(_ context.Context, address string) ([]byte, error) {
	f.pictures = append(f.pictures, address)
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 3)))
	return b.Bytes(), nil
}

func (f *fake) AddSource(_ context.Context, src client.NewSource) (client.Source, error) {
	added := client.Source{ID: 10, URL: src.URL, RSSHubPath: src.RSSHubPath, Kind: src.Kind}
	f.sources = append(f.sources, added)
	return added, nil
}

func (f *fake) UpdateSource(_ context.Context, id int64, patch client.SourcePatch) (client.Source, error) {
	for i, src := range f.sources {
		if src.ID == id {
			if patch.Paused != nil {
				f.sources[i].Paused = *patch.Paused
			}
			if patch.Kind != nil {
				f.sources[i].Kind = *patch.Kind
			}
			return f.sources[i], nil
		}
	}
	return client.Source{}, &client.APIError{Status: 404, Code: "not_found"}
}

func (f *fake) DeleteSource(_ context.Context, id int64) error {
	f.sources = slices.DeleteFunc(f.sources, func(src client.Source) bool { return src.ID == id })
	f.entries = slices.DeleteFunc(f.entries, func(e client.Entry) bool { return e.SourceID == id })
	return nil
}

func (f *fake) RefreshSource(context.Context, int64) error { return nil }

func (f *fake) Settings(context.Context) (client.Settings, error) {
	return client.Settings{RSSHubBaseURL: "https://rsshub.example.com"}, nil
}

func (f *fake) SetSettings(context.Context, client.SettingsPatch) (client.Settings, error) {
	return client.Settings{}, nil
}

func (f *fake) AI(context.Context, int64) (client.AIResults, error) { return client.AIResults{}, nil }

// RequestAI answers as a server without AI set up.
func (f *fake) RequestAI(context.Context, int64, string) (client.AIResult, error) {
	return client.AIResult{}, &client.APIError{Status: 409, Code: "ai_not_configured"}
}

func (f *fake) deps() Deps {
	return Deps{
		Go:             func(work func() func()) { f.queue = append(f.queue, work) },
		Now:            func() time.Time { return f.now },
		OpenConnect:    func() { f.connects++ },
		OpenSettings:   func() { f.settings++ },
		OpenAISettings: func() { f.aiSettings++ },
		Expired:        func() { f.expired++ },
	}
}

// step runs the oldest queued work and applies its result, as the window does on the UI thread.
func (f *fake) step(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if len(f.queue) == 0 {
		t.Fatal("no work queued")
	}
	work := f.queue[0]
	f.queue = f.queue[1:]
	work()()
	tt.Frame()
}

// settle runs the queued work until there is none.
func (f *fake) settle(t *testing.T, tt *ui.Tester) {
	t.Helper()
	for len(f.queue) > 0 {
		f.step(t, tt)
	}
}

func sample() ([]client.Source, []client.Entry) {
	sources := []client.Source{
		{ID: 1, Title: "Go Blog", Kind: "article", UnreadCount: 2},
		{ID: 2, Title: "Talks", Kind: "video", UnreadCount: 1},
		{ID: 3, Title: "Gopher", Kind: "social", UnreadCount: 1, LastError: "status 500"},
		{ID: 4, Title: "Photos", Kind: "image", UnreadCount: 1},
	}
	entries := []client.Entry{
		{ID: 8, SourceID: 1, SourceTitle: "Go Blog", Kind: "article", Title: "Go 1.27 is released",
			URL: "https://go.dev/blog/go1.27", Summary: "The latest Go release.", ThumbnailURL: "https://go.dev/t.png",
			CreatedAt: ago(5 * time.Minute)},
		{ID: 7, SourceID: 2, SourceTitle: "Talks", Kind: "video", Title: "Profiling in practice",
			URL: "https://video.example/7", ThumbnailURL: "https://video.example/7.jpg", DurationSec: 754,
			Platform: "YouTube", CreatedAt: ago(2 * time.Hour)},
		{ID: 6, SourceID: 3, SourceTitle: "Gopher", Kind: "social", Author: "Gopher", Platform: "@gopher",
			Summary: "Five pictures from the meetup", URL: "https://social.example/6", CreatedAt: ago(26 * time.Hour),
			Images: []client.Image{{URL: "https://social.example/a.jpg"}, {URL: "https://social.example/b.jpg"},
				{URL: "https://social.example/c.jpg"}, {URL: "https://social.example/d.jpg"},
				{URL: "https://social.example/e.jpg"}}},
		{ID: 5, SourceID: 4, SourceTitle: "Photos", Kind: "image", Title: "Harbor at dawn",
			URL: "https://photos.example/5", Images: []client.Image{{URL: "https://photos.example/5.jpg", Width: 1200,
				Height: 800}}, CreatedAt: ago(3 * 24 * time.Hour)},
		{ID: 4, SourceID: 1, SourceTitle: "Go Blog", Kind: "article", Title: "Range over func",
			URL: "https://go.dev/blog/range", IsRead: true, Starred: true, CreatedAt: ago(40 * 24 * time.Hour)},
		{ID: 3, SourceID: 1, SourceTitle: "Go Blog", Kind: "article", Title: "Go turns 16", URL: "https://go.dev/16",
			CreatedAt: ago(400 * 24 * time.Hour)},
	}
	return sources, entries
}

func setup(t *testing.T, language string) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	sources, entries := sample()
	f := &fake{now: now, sources: sources, entries: entries, patches: map[int64][]client.EntryPatch{}}
	s := New(language, f.deps())
	tt := ui.NewTester(func(c *ui.Context) { s.View(c); theme.Toasts(c) }, 1100, 800)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	return s, f, tt
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

func TestTimeline(t *testing.T) {
	s, f, tt := setup(t, "en")
	for _, label := range []string{
		"Go Blog, Go 1.27 is released, 5 min, Unread",
		"Talks, Profiling in practice, 2 h, Unread",
		"Gopher, Five pictures from the meetup, Yesterday, Unread",
		"Photos, Harbor at dawn, 3 days, Unread",
		"Go Blog, Range over func, Sep 8, Read, Starred",
		"Go Blog, Go turns 16, Sep 13, 2025, Unread",
	} {
		if _, ok := tt.Find(label); !ok {
			t.Errorf("no row %q", label)
		}
	}
	for _, text := range []string{"All", "5 unread", "Subscriptions", "This computer", "12:34", "+2"} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
	for _, label := range []string{"Last fetch failed", "Duration 12:34", "5 pictures", "2 more pictures",
		"Server: This computer", "Mark all in “All” as read"} {
		if _, ok := tt.Find(label); !ok {
			t.Errorf("no %q", label)
		}
	}
	if len(f.queries) != 1 || f.queries[0] != (client.Query{Limit: pageSize}) {
		t.Fatalf("queries %v", f.queries)
	}
	if len(f.pictures) == 0 {
		t.Fatal("no pictures were loaded")
	}
	if err := tt.Click("Server: This computer"); err != nil || f.connects != 1 {
		t.Fatalf("connect screen opened %d times, %v", f.connects, err)
	}
	if err := tt.Click("Settings"); err != nil || f.settings != 1 {
		t.Fatalf("settings opened %d times, %v", f.settings, err)
	}
	s.Refresh()
	if len(f.queue) != 2 {
		t.Fatalf("refreshing queued %d loads", len(f.queue))
	}
	f.queue = nil
	if s.selected != -1 {
		t.Fatalf("selected %d before any key", s.selected)
	}
}

func TestChinese(t *testing.T) {
	_, _, tt := setup(t, "zh-Hans")
	for _, label := range []string{"Go Blog，Go 1.27 is released，5 分钟前，未读", "Go Blog，Range over func，9月8日，已读，收藏",
		"Go Blog，Go turns 16，2025年9月13日，未读"} {
		if _, ok := tt.Find(label); !ok {
			t.Errorf("no row %q", label)
		}
	}
	if !tt.HasText("5 条未读") || !tt.HasText("本机") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestKindsAndSources(t *testing.T) {
	s, f, tt := setup(t, "en")
	if err := tt.Click("Articles"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if q := f.queries[len(f.queries)-1]; q.Kind != "article" || q.Source != 0 {
		t.Fatalf("query %v", q)
	}
	if !tt.HasText("Go Blog") || tt.HasText("Talks") || tt.HasText("Photos") {
		t.Fatalf("the sources are not those of articles: %v", tt.Texts())
	}
	if !tt.HasText("2 unread") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("All"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if err := tt.Click("Talks"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if q := f.queries[len(f.queries)-1]; q.Source != 2 || q.Kind != "" {
		t.Fatalf("query %v", q)
	}
	if s.tab != 2 || s.scope != (scope{kind: "video", source: 2}) {
		t.Fatalf("tab %d, scope %v", s.tab, s.scope)
	}
	// The same scope again loads nothing.
	n := len(f.queries)
	tt.Click("Talks")
	if len(f.queue) != 0 || len(f.queries) != n {
		t.Fatal("choosing the same source loaded again")
	}
}

func TestLayouts(t *testing.T) {
	for _, kind := range []string{"Articles", "Videos", "Social", "Images"} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s dark %v", kind, dark), func(t *testing.T) {
				_, f, tt := setup(t, "en")
				tt.SetDark(dark)
				if err := tt.Click(kind); err != nil {
					t.Fatal(err)
				}
				f.settle(t, tt)
				f.settle(t, tt)
				var row string
				switch kind {
				case "Videos":
					row = "Talks, Profiling in practice, 2 h, Unread"
				case "Images":
					row = "Photos, Harbor at dawn, 3 days, Unread"
				case "Social":
					row = "Gopher, Five pictures from the meetup, Yesterday, Unread"
				default:
					row = "Go Blog, Go 1.27 is released, 5 min, Unread"
				}
				r, ok := tt.Find(row)
				if !ok {
					t.Fatalf("no row %q in %v", row, tt.Texts())
				}
				tt.Move(r.X+r.W/2, r.Y+r.H/2)
				for _, label := range []string{"Star", "Mark as read", "Open in browser"} {
					if _, ok := tt.Find(label); !ok {
						t.Errorf("no %q on the hovered entry", label)
					}
				}
				if tt.Image() == nil {
					t.Fatal("nothing drawn")
				}
			})
		}
	}
}

func TestCompact(t *testing.T) {
	_, _, tt := setup(t, "en")
	count := func(text string) int {
		n := 0
		for _, s := range tt.Texts() {
			if s == text {
				n++
			}
		}
		return n
	}
	if count("Starred") != 2 {
		t.Fatalf("the filter shows %d times", count("Starred"))
	}
	tt.SetSize(700, 600)
	if count("Starred") != 1 {
		t.Fatal("the filters keep their names in a narrow window")
	}
}

func TestKeyboard(t *testing.T) {
	s, f, tt := setup(t, "en")
	first := "Go Blog, Go 1.27 is released, 5 min, Unread"
	if err := tt.Click(first); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if s.reading != 8 || len(tt.OpenedURLs()) != 0 {
		t.Fatalf("reading %d, opened %v", s.reading, tt.OpenedURLs())
	}
	if !s.entries[0].IsRead || len(f.patches[8]) != 1 || !*f.patches[8][0].IsRead {
		t.Fatalf("not marked read: %v", f.patches)
	}
	tt.Key(0, ui.KeyEscape)
	if s.reading != 0 || !tt.Focused("Go Blog, Go 1.27 is released, 5 min, Read") {
		t.Fatalf("Escape does not return to the entry: reading %d", s.reading)
	}
	tt.Move(0, 0)
	tt.Key(0, ui.KeyJ)
	if s.selected != 1 || !tt.Focused("Talks, Profiling in practice, 2 h, Unread") {
		t.Fatalf("selected %d", s.selected)
	}
	tt.Key(0, ui.KeyS)
	f.settle(t, tt)
	if !s.entries[1].Starred || len(f.patches[7]) != 1 || !*f.patches[7][0].Starred {
		t.Fatalf("not starred: %v", f.patches)
	}
	if !slices.Contains(tt.Announcements(), "Starred") {
		t.Fatalf("announcements %v", tt.Announcements())
	}
	tt.Key(0, ui.KeyM)
	f.settle(t, tt)
	if !s.entries[1].IsRead || s.sources[1].UnreadCount != 0 {
		t.Fatalf("not read: %v, %v", s.entries[1], s.sources[1])
	}
	tt.Key(0, ui.KeyK)
	tt.Key(0, ui.KeyUp)
	if s.selected != 0 {
		t.Fatalf("selected %d", s.selected)
	}
	tt.Key(0, ui.KeyO)
	if s.reading != 8 {
		t.Fatalf("O reads %d", s.reading)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Key(0, ui.KeyJ)
	tt.Key(0, ui.KeyEnter)
	if s.reading != 0 || len(tt.OpenedURLs()) != 1 {
		t.Fatalf("a video opens %v, reading %d", tt.OpenedURLs(), s.reading)
	}
	tt.Key(0, ui.KeySlash)
	if !tt.Focused("Search entries") {
		t.Fatal("/ does not focus the search")
	}
	tt.Type("jk")
	if s.selected != 1 || s.query != "jk" {
		t.Fatalf("the keys moved the list while typing: selected %d, query %q", s.selected, s.query)
	}
}

// TestReaderAIOff follows the reader's way to the settings while AI is not set up.
func TestReaderAIOff(t *testing.T) {
	_, f, tt := setup(t, "en")
	if err := tt.Click("Go Blog, Go 1.27 is released, 5 min, Unread"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if err := tt.Click("Summarize"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if err := tt.Click("Open Settings…"); err != nil {
		t.Fatalf("%v in %v", err, tt.Texts())
	}
	if f.aiSettings != 1 || f.settings != 0 {
		t.Fatalf("AI settings %d, settings %d", f.aiSettings, f.settings)
	}
}

func TestReader(t *testing.T) {
	s, f, tt := setup(t, "en")
	if err := tt.Click("Go Blog, Go 1.27 is released, 5 min, Unread"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	for _, text := range []string{"Go Blog", "Go 1.27 is released", "The text of Go 1.27 is released.", "Oct 18, 11:55"} {
		if !tt.HasText(text) {
			t.Fatalf("no %q in %v", text, tt.Texts())
		}
	}
	if !tt.Focused("Go 1.27 is released") {
		t.Fatal("the article does not take the focus")
	}
	if _, ok := tt.Find("Back to All"); !ok {
		t.Fatal("no Back to All")
	}
	if _, ok := tt.Find("Search entries"); ok {
		t.Fatal("the list's toolbar shows over the reader")
	}
	// Opening marked it read; the reader's toggle undoes that.
	if err := tt.Click("Mark as unread"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if s.entries[0].IsRead || len(f.patches[8]) != 2 || *f.patches[8][1].IsRead {
		t.Fatalf("not marked unread: %v", f.patches[8])
	}
	tt.Key(0, ui.KeyS)
	f.settle(t, tt)
	if !s.entries[0].Starred {
		t.Fatal("S does not star the article")
	}
	if _, ok := tt.Find("Remove star"); !ok {
		t.Fatal("the star does not show")
	}
	if _, ok := tt.Find("Previous entry"); !ok || s.article(-1) >= 0 {
		t.Fatal("the first article has one before it")
	}
	// J skips the video, the post and the picture to the next article, which is read already.
	tt.Key(0, ui.KeyJ)
	f.settle(t, tt)
	if s.reading != 4 || !tt.HasText("The text of Range over func.") || len(f.patches[4]) != 0 {
		t.Fatalf("reading %d, patches %v", s.reading, f.patches[4])
	}
	tt.Key(0, ui.KeyJ)
	f.settle(t, tt)
	tt.Key(0, ui.KeyJ)
	if s.reading != 3 {
		t.Fatalf("J past the last article reads %d", s.reading)
	}
	tt.Key(0, ui.KeyK)
	f.settle(t, tt)
	if s.reading != 4 {
		t.Fatalf("K reads %d", s.reading)
	}
	tt.Key(0, ui.KeyO)
	if opened := tt.OpenedURLs(); !slices.Equal(opened, []string{"https://go.dev/blog/range"}) {
		t.Fatalf("opened %v", opened)
	}
	if err := tt.Click("Back to All"); err != nil {
		t.Fatal(err)
	}
	if s.reading != 0 || s.selected != 4 || !tt.Focused("Go Blog, Range over func, Sep 8, Read, Starred") {
		t.Fatalf("back to %d", s.selected)
	}
	// A click in the sidebar, on the scope shown too, closes the reader.
	tt.Key(0, ui.KeyEnter)
	f.settle(t, tt)
	if err := tt.Click("Go Blog"); err != nil {
		t.Fatal(err)
	}
	if s.reading != 0 || s.scope.source != 1 {
		t.Fatalf("reading %d in %v", s.reading, s.scope)
	}
	f.settle(t, tt)
	if err := tt.Click("Go Blog, Go 1.27 is released, 5 min, Unread"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if s.reading != 8 {
		t.Fatalf("reading %d in the source", s.reading)
	}
	if err := tt.Click("Go Blog"); err != nil {
		t.Fatal(err)
	}
	if s.reading != 0 {
		t.Fatal("the scope shown does not close the reader")
	}
}

func TestOpenFails(t *testing.T) {
	_, f, tt := setup(t, "en")
	tt.FailOpenURL(errors.New("no browser"))
	if err := tt.Click("Talks, Profiling in practice, 2 h, Unread"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if !tt.HasText("Couldn’t open the link") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestChangeFails(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.setErr = errors.New("status 500")
	tt.Key(0, ui.KeyJ)
	tt.Key(0, ui.KeyM)
	if !s.entries[0].IsRead || s.sources[0].UnreadCount != 1 {
		t.Fatal("the change does not show at once")
	}
	f.settle(t, tt)
	if s.entries[0].IsRead || s.sources[0].UnreadCount != 2 {
		t.Fatal("the change stays after the server refused it")
	}
	if !tt.HasText("Couldn’t save the change") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestExpired(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.entriesErr = client.ErrUnauthorized
	s.reload()
	f.settle(t, tt)
	if f.expired != 1 || s.api != nil {
		t.Fatalf("expired %d", f.expired)
	}
}

func TestSearch(t *testing.T) {
	s, f, tt := setup(t, "en")
	tt.Key(ui.Cmd, ui.KeyF)
	if !tt.Focused("Search entries") {
		t.Fatal("Cmd+F does not focus the search")
	}
	tt.Type("go")
	if !tt.HasText("Type at least 3 characters to search") {
		t.Fatalf("no hint: %v", tt.Texts())
	}
	f.now = f.now.Add(time.Second)
	tt.Frame()
	if len(f.queue) != 0 {
		t.Fatal("a query too short was sent")
	}
	tt.Type(" t")
	if len(f.queue) != 0 {
		t.Fatal("searched before the debounce")
	}
	f.now = f.now.Add(debounce)
	tt.Frame()
	f.settle(t, tt)
	if q := f.queries[len(f.queries)-1]; q.Search != "go t" {
		t.Fatalf("query %v", q)
	}
	if !tt.HasText("1 found") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if s.canMarkAll() {
		t.Fatal("mark all is offered for a search")
	}
	tt.Type("x")
	tt.Key(0, ui.KeyEnter)
	f.settle(t, tt)
	if !tt.HasText("Nothing matches “go tx”") {
		t.Fatalf("texts %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	f.settle(t, tt)
	if s.searched != "" || len(s.entries) != 6 {
		t.Fatalf("searched %q, %d entries", s.searched, len(s.entries))
	}
}

func TestNewEntries(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.entries = append([]client.Entry{{ID: 9, SourceID: 1, SourceTitle: "Go Blog", Kind: "article", Title: "Fresh",
		CreatedAt: now.UnixMilli()}}, f.entries...)
	f.sources[0].UnreadCount++
	f.now = f.now.Add(pollEvery)
	tt.Frame()
	f.settle(t, tt)
	if len(s.entries) != 6 || !tt.HasText("1 new") || !tt.HasText("6 unread") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if !slices.Contains(tt.Announcements(), "1 new") {
		t.Fatalf("announcements %v", tt.Announcements())
	}
	if err := tt.Click("1 new"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if len(s.entries) != 7 || tt.HasText("1 new") {
		t.Fatalf("%d entries", len(s.entries))
	}
}

func TestLanguageKeepsPlace(t *testing.T) {
	var entries []client.Entry
	for i := range 80 {
		entries = append(entries, client.Entry{ID: int64(200 - i), SourceID: 1, SourceTitle: "Go Blog",
			Kind: "article", Title: fmt.Sprintf("Post %d", i), CreatedAt: ago(time.Duration(i) * time.Hour)})
	}
	f := &fake{now: now, sources: []client.Source{{ID: 1, Title: "Go Blog", Kind: "article"}}, entries: entries,
		patches: map[int64][]client.EntryPatch{}}
	s := New("en", f.deps())
	tt := ui.NewTester(s.View, 1100, 800)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	s.list.ScrollTo(40, ui.Start)
	tt.Frame()
	s.SetLanguage("zh-Hans")
	tt.Frame()
	if _, ok := tt.Find("搜索条目"); !ok || !tt.HasText("Post 40") || tt.HasText("Post 0") {
		t.Fatalf("the list did not stay in place in Chinese: %v", tt.Texts())
	}
	s.read(40)
	f.settle(t, tt)
	s.SetLanguage("en")
	tt.Frame()
	if _, ok := tt.Find("Back to All"); !ok || s.reading != 160 || !tt.HasText("Post 40") {
		t.Fatalf("the reader did not stay open in English: reading %d, texts %v", s.reading, tt.Texts())
	}
	if err := tt.Click("Back to All"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if s.selected != 40 || !tt.HasText("Post 40") || tt.HasText("Post 0") {
		t.Fatalf("selected %d, texts %v", s.selected, tt.Texts())
	}
}

func TestLoadMore(t *testing.T) {
	var entries []client.Entry
	for i := range 120 {
		entries = append(entries, client.Entry{ID: int64(200 - i), SourceID: 1, SourceTitle: "Go Blog",
			Kind: "article", Title: fmt.Sprintf("Post %d", i), CreatedAt: ago(time.Duration(i) * time.Hour)})
	}
	f := &fake{now: now, sources: []client.Source{{ID: 1, Title: "Go Blog", Kind: "article"}}, entries: entries,
		patches: map[int64][]client.EntryPatch{}}
	s := New("en", f.deps())
	tt := ui.NewTester(s.View, 1100, 800)
	s.Use(f, "home.lan:7470")
	tt.Frame()
	f.settle(t, tt)
	if !tt.HasText("home.lan:7470") || len(s.entries) != pageSize {
		t.Fatalf("%d entries", len(s.entries))
	}
	f.entriesErr = errors.New("offline")
	s.list.ScrollTo(pageSize-1, ui.End)
	tt.Frame()
	f.settle(t, tt)
	if !tt.HasText("Couldn’t load more") {
		t.Fatalf("texts %v", tt.Texts())
	}
	f.entriesErr = nil
	if err := tt.Click("Try again"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if len(s.entries) != 2*pageSize {
		t.Fatalf("%d entries", len(s.entries))
	}
	s.list.ScrollTo(len(s.entries)-1, ui.End)
	tt.Frame()
	f.settle(t, tt)
	if len(s.entries) != 120 || !slices.Contains(tt.Announcements(), "All entries loaded") {
		t.Fatalf("%d entries, announcements %v", len(s.entries), tt.Announcements())
	}
}

func TestMarkAllRead(t *testing.T) {
	s, f, tt := setup(t, "en")
	if err := tt.Click("Mark all in “All” as read"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if !reflect.DeepEqual(f.marked, []client.ReadScope{{UpTo: 8}}) {
		t.Fatalf("marked %v", f.marked)
	}
	for _, e := range s.entries {
		if !e.IsRead {
			t.Fatalf("still unread: %v", e)
		}
	}
	if !slices.Contains(tt.Announcements(), "Marked 5 entries in “All” as read") || s.canMarkAll() {
		t.Fatalf("announcements %v", tt.Announcements())
	}
	if err := tt.Click("Undo"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if !reflect.DeepEqual(f.unmarked, [][]int64{{8, 7, 6, 5, 3}}) {
		t.Fatalf("unmarked %v", f.unmarked)
	}
	if s.unreadIn(s.scope) != 5 || tt.HasText("Undo") || s.undoable != nil {
		t.Fatalf("unread %d, texts %v", s.unreadIn(s.scope), tt.Texts())
	}
}

func TestUndoWithKey(t *testing.T) {
	s, f, tt := setup(t, "en")
	tt.Key(ui.Cmd, ui.KeyZ)
	f.settle(t, tt)
	if len(f.unmarked) != 0 {
		t.Fatalf("undid nothing: %v", f.unmarked)
	}
	if err := tt.Click("Mark all in “All” as read"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	tt.Key(ui.Cmd, ui.KeyZ)
	f.settle(t, tt)
	if len(f.unmarked) != 1 || s.unreadIn(s.scope) != 5 || tt.HasText("Undo") {
		t.Fatalf("unmarked %v, texts %v", f.unmarked, tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	f.settle(t, tt)
	if len(f.unmarked) != 1 {
		t.Fatalf("undid twice: %v", f.unmarked)
	}
}

func TestUndoDismissed(t *testing.T) {
	_, f, tt := setup(t, "en")
	if err := tt.Click("Mark all in “All” as read"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if err := tt.Click("Dismiss"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyZ)
	f.settle(t, tt)
	if len(f.unmarked) != 0 || tt.HasText("Undo") {
		t.Fatalf("unmarked %v, texts %v", f.unmarked, tt.Texts())
	}
}

func TestUndoFails(t *testing.T) {
	s, f, tt := setup(t, "en")
	if err := tt.Click("Mark all in “All” as read"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	f.markErr = errors.New("offline")
	if err := tt.Click("Undo"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if s.unreadIn(s.scope) != 0 || !slices.Contains(tt.Announcements(), "Couldn’t save the change") {
		t.Fatalf("unread %d, announcements %v", s.unreadIn(s.scope), tt.Announcements())
	}
}

func TestNextUnread(t *testing.T) {
	s, _, tt := setup(t, "en")
	tt.Key(0, ui.KeyN)
	if s.selected != 0 {
		t.Fatalf("selected %d", s.selected)
	}
	tt.Key(0, ui.KeyN)
	tt.Key(0, ui.KeyN)
	tt.Key(0, ui.KeyN)
	if s.selected != 3 {
		t.Fatalf("selected %d", s.selected)
	}
	tt.Key(0, ui.KeyN)
	if s.selected != 5 {
		t.Fatalf("skipped the read entry badly: selected %d", s.selected)
	}
	tt.Key(0, ui.KeyN)
	tt.Frame()
	if s.selected != 5 || !slices.Contains(tt.Announcements(), "No unread entries below") {
		t.Fatalf("selected %d, announcements %v", s.selected, tt.Announcements())
	}
}

func TestNextUnreadLoadsMore(t *testing.T) {
	sources, _ := sample()
	var entries []client.Entry
	for i := 120; i > 0; i-- {
		entries = append(entries, client.Entry{ID: int64(i), SourceID: 1, SourceTitle: "Go Blog", Kind: "article",
			Title: fmt.Sprintf("Entry %d", i), URL: fmt.Sprintf("https://go.dev/%d", i), IsRead: i > 10,
			CreatedAt: ago(time.Duration(121-i) * time.Minute)})
	}
	f := &fake{now: now, sources: sources, entries: entries, patches: map[int64][]client.EntryPatch{}}
	f.count()
	s := New("en", f.deps())
	tt := ui.NewTester(s.View, 1100, 800)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	if len(s.entries) != pageSize {
		t.Fatalf("%d entries", len(s.entries))
	}
	tt.Key(0, ui.KeyN)
	f.settle(t, tt)
	if len(s.entries) <= pageSize || s.entries[s.selected].ID != 10 {
		t.Fatalf("%d entries, selected %d", len(s.entries), s.selected)
	}
}

func TestNextUnreadInReader(t *testing.T) {
	s, f, tt := setup(t, "en")
	s.read(0)
	f.settle(t, tt)
	tt.Key(0, ui.KeyN)
	f.settle(t, tt)
	if s.reading != 3 {
		t.Fatalf("reading %d", s.reading)
	}
}

func TestKeySheet(t *testing.T) {
	s, _, tt := setup(t, "en")
	tt.TypeKey(ui.Shift, ui.KeySlash, "?")
	tt.Frame()
	if !s.keys || !tt.HasText("Keyboard shortcuts") || !tt.HasText("Next unread") {
		t.Fatalf("texts %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.keys || tt.HasText("Keyboard shortcuts") {
		t.Fatal("still open")
	}
	tt.Key(ui.Cmd, ui.KeySlash)
	tt.Frame()
	if !s.keys {
		t.Fatal("Cmd+/ did not open it")
	}
	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.keys {
		t.Fatal("still open")
	}
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("?")
	tt.Frame()
	if s.keys {
		t.Fatal("? opened the sheet while typing")
	}
}

func TestStates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(f *fake, s *Screen)
		text  string
	}{
		{"no subscriptions", func(f *fake, s *Screen) { f.sources, f.entries = nil, nil }, "No subscriptions yet"},
		{"empty", func(f *fake, s *Screen) { f.entries = nil }, "Nothing here yet"},
		{"unread done", func(f *fake, s *Screen) {
			for i := range f.entries {
				f.entries[i].IsRead = true
			}
		}, "All caught up"},
		{"no starred", func(f *fake, s *Screen) { f.entries[4].Starred = false }, "No starred entries"},
		{"error", func(f *fake, s *Screen) { f.entriesErr = errors.New("offline") }, "Can’t load the timeline"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f, tt := setup(t, "en")
			tc.setup(f, s)
			s.Use(f, "")
			s.filter = map[string]int{"unread done": unread, "no starred": starred}[tc.name]
			s.reload()
			tt.Frame()
			if _, ok := tt.Find("Loading"); !ok {
				t.Fatal("no loading state")
			}
			f.settle(t, tt)
			if !tt.HasText(tc.text) {
				t.Fatalf("texts %v", tt.Texts())
			}
			if tc.name == "error" {
				f.entriesErr = nil
				if err := tt.Click("Try again"); err != nil {
					t.Fatal(err)
				}
				f.settle(t, tt)
				if len(s.entries) != 6 || len(s.sources) != 4 {
					t.Fatalf("%d entries, %d sources", len(s.entries), len(s.sources))
				}
			}
		})
	}
}

func TestUseDropsTheServerBefore(t *testing.T) {
	s, f, tt := setup(t, "en")
	s.reload()
	other := &fake{now: now, patches: map[int64][]client.EntryPatch{}}
	s.Use(other, "home.lan")
	f.settle(t, tt)
	if len(s.entries) != 0 || !tt.HasText("No subscriptions yet") {
		t.Fatalf("%d entries from the server before", len(s.entries))
	}
}

func TestSubscriptions(t *testing.T) {
	s, f, tt := setup(t, "en")
	if err := tt.Click("Add subscription"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if !s.subs.Open() || !tt.Focused("Address") {
		t.Fatal("the + button does not open the add dialog")
	}
	tt.Key(0, ui.KeyJ)
	if s.selected != -1 {
		t.Fatal("the keys of the list work behind the dialog")
	}
	tt.Type("https://go.dev/feed.atom")
	tt.Key(0, ui.KeyEnter)
	f.settle(t, tt)
	if s.subs.Open() || len(s.sources) != 5 || !tt.HasText("go.dev") {
		t.Fatalf("sources %v, texts %v", s.sources, tt.Texts())
	}

	if err := tt.RightClick("Talks"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Pause"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if _, ok := tt.Find("Paused"); !ok || !s.sources[1].Paused {
		t.Fatalf("not paused: %v", s.sources[1])
	}

	if err := tt.Click("Talks"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if err := tt.Click("Edit subscription"); err != nil {
		t.Fatal(err)
	}
	if !s.subs.Open() || !tt.HasText("Paused. Nothing is fetched until you resume.") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("Delete…"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if _, ok := tt.Find("Talks"); ok || s.scope != (scope{}) || len(s.sources) != 4 {
		t.Fatalf("scope %v, sources %v", s.scope, s.sources)
	}
}

func TestAddFirst(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.sources, f.entries = nil, nil
	s.Use(f, "")
	f.settle(t, tt)
	if err := tt.Click("Add your first subscription"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if !s.subs.Open() {
		t.Fatal("the empty state does not open the add dialog")
	}
	tt.Key(0, ui.KeyEscape)
	if s.subs.Open() {
		t.Fatal("Escape does not close the dialog")
	}
	tt.Key(ui.Cmd, ui.KeyN)
	f.settle(t, tt)
	if !s.subs.Open() {
		t.Fatal("Cmd+N does not open the add dialog")
	}
}

func TestSiteIcons(t *testing.T) {
	sources, entries := sample()
	sources[0].IconURL = "https://go.dev/favicon.ico"
	f := &fake{now: now, sources: sources, entries: entries, patches: map[int64][]client.EntryPatch{}}
	s := New("en", f.deps())
	tt := ui.NewTester(s.View, 1100, 800)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	if !slices.Contains(f.pictures, "https://go.dev/favicon.ico") {
		t.Fatalf("pictures %v", f.pictures)
	}
	if p := s.pictures["https://go.dev/favicon.ico"]; p == nil || p.bitmap == nil {
		t.Fatal("the icon is not decoded")
	}
	if s.iconOf(1) != "https://go.dev/favicon.ico" || s.iconOf(2) != "" {
		t.Fatalf("icons %q %q", s.iconOf(1), s.iconOf(2))
	}
}

// pixel is the color drawn at a point of the tester's last frame, as 0 to 255 channels.
func pixel(img image.Image, x, y float32) [3]int {
	r, g, b, _ := img.At(int(x), int(y)).RGBA()
	return [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
}

func light(p [3]int) int { return p[0] + p[1] + p[2] }

func near(p [3]int, c ui.Color, tolerance int) bool {
	for i, v := range []uint8{c.R, c.G, c.B} {
		if d := p[i] - int(v); d > tolerance || d < -tolerance {
			return false
		}
	}
	return true
}

// inkSetup is setup with the main theme of the window kept, so a test can ask for the pane's values.
func inkSetup(t *testing.T, dark bool, scale float32) (*Screen, *fake, *ui.Tester, **ui.Theme) {
	t.Helper()
	sources, entries := sample()
	f := &fake{now: now, sources: sources, entries: entries, patches: map[int64][]client.EntryPatch{}}
	s := New("en", f.deps())
	var main *ui.Theme
	tt := ui.NewTester(func(c *ui.Context) { theme.Apply(c, prefs.Default()); main = c.Theme(); s.View(c) }, 1100, 800)
	tt.SetDark(dark)
	tt.SetPreferences(ui.Preferences{TextScale: scale})
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	return s, f, tt, &main
}

// inkCases runs a test in both schemes at the design's text size and at the 14 of Linux and Windows.
func inkCases(t *testing.T, run func(t *testing.T, dark bool, scale float32)) {
	for _, dark := range []bool{false, true} {
		for _, scale := range []float32{1, 14.0 / 13} {
			t.Run(fmt.Sprint("dark=", dark, "/text=", scale), func(t *testing.T) { run(t, dark, scale) })
		}
	}
}

func TestInkPane(t *testing.T) {
	inkCases(t, func(t *testing.T, dark bool, scale float32) {
		_, _, tt, main := inkSetup(t, dark, scale)
		pane := theme.PaneTheme(*main)
		talks, ok := tt.Find("Talks")
		if !ok {
			t.Fatalf("no pane item in %v", tt.Texts())
		}
		// The pane is ink in both schemes: the empty strip beside an item is its Background, the timeline's is not.
		x, y := talks.X-4, talks.Y+talks.H/2
		if !near(pixel(tt.Image(), x, y), pane.Background, 2) {
			t.Errorf("pane drawn %v, want %v", pixel(tt.Image(), x, y), pane.Background)
		}
		if near(pixel(tt.Image(), 700, 790), pane.Background, 2) {
			t.Error("the timeline is drawn on the pane's ink")
		}

		// An item's name is drawn in the pane's text roles, not the window's: some pixel of its label is close to
		// Text2 (unselected) and some of the selected one's to Text.
		stem := func(label string, want ui.Color) bool {
			r, _ := tt.Find(label)
			img := tt.Image()
			for py := r.Y; py < r.Y+r.H; py++ {
				for px := r.X; px < r.X+r.W; px++ {
					if near(pixel(img, px, py), want, 8) {
						return true
					}
				}
			}
			return false
		}
		if !stem("Talks", theme.Text2(pane)) {
			t.Errorf("an unselected pane item is not in the pane's Text2 %v", theme.Text2(pane))
		}
		if err := tt.Click("Go Blog"); err != nil {
			t.Fatal(err)
		}
		tt.Move(1000, 700) // off the pane
		tt.Frame()
		if !stem("Go Blog", pane.Text) {
			t.Errorf("the selected pane item is not in the pane's Text %v", pane.Text)
		}
	})
}

// The fill of a pane item and of a row steps up from hover to pressed to chosen, and the chosen row carries the marker.
func TestStateFills(t *testing.T) {
	inkCases(t, func(t *testing.T, dark bool, scale float32) {
		s, f, tt, _ := inkSetup(t, dark, scale)
		fade := func() { tt.Frame(); tt.Advance(time.Second); tt.Frame() }
		// The pane is dark, so more ink is lighter.
		talks, _ := tt.Find("Talks")
		x, y := talks.X+2, talks.Y+talks.H/2
		rest := light(pixel(tt.Image(), x, y))
		tt.Move(talks.X+talks.W/2, y)
		fade()
		hover := light(pixel(tt.Image(), x, y))
		tt.Press(talks.X+talks.W/2, y)
		fade()
		pressed := light(pixel(tt.Image(), x, y))
		tt.Release(talks.X+talks.W/2, y)
		f.settle(t, tt)
		tt.Move(1000, 700)
		fade()
		talks, _ = tt.Find("Talks")
		x, y = talks.X+2, talks.Y+talks.H/2
		chosen := light(pixel(tt.Image(), x, y))
		if !(rest < hover && hover < pressed && pressed < chosen) {
			t.Errorf("pane item fills rest %d, hover %d, pressed %d, chosen %d are not in steps", rest, hover, pressed, chosen)
		}
		if s.scope.source != 2 {
			t.Fatalf("scope %v after choosing Talks", s.scope)
		}

	})
}

// A row steps through the same three fills over the window's paper, and the chosen row carries the marker.
func TestRowFills(t *testing.T) {
	inkCases(t, func(t *testing.T, dark bool, scale float32) {
		s, _, tt, main := inkSetup(t, dark, scale)
		fade := func() { tt.Frame(); tt.Advance(time.Second); tt.Frame() }
		label := "Go Blog, Go 1.27 is released, 5 min, Unread"
		row, ok := tt.Find(label)
		if !ok {
			t.Fatalf("no row in %v", tt.Texts())
		}
		y := row.Y + row.H/2
		// The row's box starts at the pane's edge, whose pixel is blended with the pane's ink when the column does not
		// land on a whole pixel, so the paper is sampled a little inside it.
		bare := pixel(tt.Image(), row.X+2, y)
		rest := light(bare)
		tt.Move(row.X+row.W/2, y)
		fade()
		// The fill is as wide as the column, which starts where the hovered row's pixels leave the paper's.
		left := row.X + 2
		img := tt.Image()
		for pixel(img, left, y) == bare && left < row.X+row.W {
			left++
		}
		x := left + 3
		hover := light(pixel(tt.Image(), x, y))
		tt.Press(row.X+row.W/2, row.Y+row.H/2)
		tt.Move(row.X+row.W/2, 5) // released off the row: no click
		tt.Release(row.X+row.W/2, 5)
		if s.selected != 0 {
			t.Fatalf("selected %d after pressing the first row", s.selected)
		}
		fade()
		selected := light(pixel(tt.Image(), x, y))
		// Light ink is dark on paper and dark ink is light on charcoal: the steps move away from the paper.
		away := func(a, b int) bool {
			if (*main).Dark {
				return a < b
			}
			return a > b
		}
		if !(away(rest, hover) && away(hover, selected)) {
			t.Errorf("row fills rest %d, hover %d, chosen %d are not in steps", rest, hover, selected)
		}
		mx, my := left+9.25, y
		if got := pixel(tt.Image(), mx, my); !near(got, theme.Marker(*main), 6) {
			t.Errorf("selection marker drawn %v, want %v", got, theme.Marker(*main))
		}
	})
}
