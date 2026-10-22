package timeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
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
	saveTo     string
	queue      []func() func()
	now        time.Time
	entries    []client.Entry // newest first
	sources    []client.Source
	health     client.Health
	queries    []client.Query
	patches    map[int64][]client.EntryPatch
	marked     []client.ReadScope
	unmarked   [][]int64
	unmarkedAt []int64
	extraRead  int // entries MarkRead reports besides the ones in the list, to make a very large mark as read
	pictures   []string
	articles   []int64  // the entries asked for in full
	aiReads    []int64  // the entries whose summary and translation were read
	aiAsked    int      // the summaries and translations asked to be made
	pages      []string // the settings pages asked for

	refreshed  client.RefreshResult // what RefreshAll answers
	refreshes  int                  // the times it was asked
	refreshErr error

	adds    []string         // the addresses AddSource was asked for
	addErrs map[string]error // what AddSource answers for an address, if not success

	entriesErr, setErr, markErr, imageErr            error
	connects, settings, aiSettings, imports, expired int
}

func (f *fake) Entries(_ context.Context, q client.Query) (client.Page, error) {
	f.queries = append(f.queries, q)
	if f.entriesErr != nil {
		return client.Page{}, f.entriesErr
	}
	var found []client.Entry
	for _, e := range f.entries {
		if q.Kind != "" && e.Kind != q.Kind || q.Source != 0 && e.SourceID != q.Source || q.Unread && e.IsRead ||
			q.Starred && !e.Starred || q.MaxMinutes > 0 && (e.ReadingSec == 0 || e.ReadingSec > q.MaxMinutes*60) || q.Search != "" && !strings.Contains(strings.ToLower(e.Title), q.Search) {
			continue
		}
		found = append(found, e)
	}
	if q.Oldest {
		slices.Reverse(found)
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

// shownAt is the time a row shows: when it was published, else when it arrived.
func shownAt(e client.Entry) int64 {
	if e.PublishedAt != 0 {
		return e.PublishedAt
	}
	return e.CreatedAt
}

func (f *fake) MarkRead(_ context.Context, scope client.ReadScope) (client.Marked, error) {
	f.marked = append(f.marked, scope)
	if f.markErr != nil {
		return client.Marked{}, f.markErr
	}
	var ids []int64
	for i, e := range f.entries {
		if !e.IsRead && (scope.UpTo == 0 || e.ID <= scope.UpTo) && e.ID > scope.Newer &&
			(scope.SourceID == 0 || e.SourceID == scope.SourceID) && (scope.Kind == "" || e.Kind == scope.Kind) &&
			(scope.OlderThan == 0 || shownAt(e) < scope.OlderThan) {
			f.entries[i].IsRead = true
			ids = append(ids, e.ID)
		}
	}
	f.count()
	for i := 0; i < f.extraRead; i++ {
		ids = append(ids, int64(1000+i))
	}
	return client.Marked{IDs: ids, ReadAt: fakeReadAt}, nil
}

// fakeReadAt is the time the fake reads entries at.
const fakeReadAt = 1700000000000

func (f *fake) MarkUnread(_ context.Context, m client.Marked) ([]int64, error) {
	f.unmarked = append(f.unmarked, m.IDs)
	f.unmarkedAt = append(f.unmarkedAt, m.ReadAt)
	if f.markErr != nil {
		return nil, f.markErr
	}
	var ids []int64
	for i, e := range f.entries {
		if e.IsRead && slices.Contains(m.IDs, e.ID) {
			f.entries[i].IsRead = false
			ids = append(ids, e.ID)
		}
	}
	f.count()
	return ids, nil
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
	if f.imageErr != nil {
		return nil, f.imageErr
	}
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 3)))
	return b.Bytes(), nil
}

func (f *fake) AddSource(_ context.Context, src client.NewSource) (client.Source, error) {
	f.adds = append(f.adds, src.URL)
	if err := f.addErrs[src.URL]; err != nil {
		return client.Source{}, err
	}
	added := client.Source{ID: int64(10 + len(f.sources)), Title: src.URL, URL: src.URL, RSSHubPath: src.RSSHubPath, Kind: src.Kind}
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

func (f *fake) RefreshAll(context.Context) (client.RefreshResult, error) {
	f.refreshes++
	return f.refreshed, f.refreshErr
}

func (f *fake) SourcesHealth(context.Context) (client.Health, error) { return f.health, nil }

func (f *fake) Settings(context.Context) (client.Settings, error) {
	return client.Settings{RSSHubBaseURL: "https://rsshub.example.com"}, nil
}

func (f *fake) SetSettings(context.Context, client.SettingsPatch) (client.Settings, error) {
	return client.Settings{}, nil
}

func (f *fake) Discover(context.Context, string) ([]client.Candidate, error) { return nil, nil }

func (f *fake) FindRoutes(context.Context, string, bool) (client.Routes, error) {
	return client.Routes{}, nil
}

func (f *fake) PreviewRoute(context.Context, string) (client.RoutePreview, error) {
	return client.RoutePreview{}, nil
}

func (f *fake) AI(_ context.Context, id int64) (client.AIResults, error) {
	f.aiReads = append(f.aiReads, id)
	return client.AIResults{}, nil
}

// RequestAI answers as a server without AI set up.
func (f *fake) RequestAI(context.Context, int64, string) (client.AIResult, error) {
	f.aiAsked++
	return client.AIResult{}, &client.APIError{Status: 409, Code: "ai_not_configured"}
}

func (f *fake) RetryExtraction(context.Context, int64) (client.Article, error) {
	return client.Article{}, nil
}

func (f *fake) Selection(context.Context, string, string, string) (string, string, error) {
	return "", "", nil
}

func (f *fake) deps() Deps {
	return Deps{
		Go:               func(work func() func()) { f.queue = append(f.queue, work) },
		Now:              func() time.Time { return f.now },
		OpenConnect:      func() { f.connects++ },
		OpenSettings:     func() { f.settings++ },
		OpenAISettings:   func() { f.aiSettings++ },
		OpenImport:       func() { f.imports++ },
		OpenSettingsPage: func(page string) { f.pages = append(f.pages, page) },
		Expired:          func() { f.expired++ },
		Save:             func(string, string) (string, error) { return f.saveTo, nil },
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

// setupAI is setup with an AI summary on the newest article and a feed summary on the older one.
func setupAI(t *testing.T, language string) (*fake, *ui.Tester) {
	t.Helper()
	sources, entries := sample()
	entries[0].AISummary = "Generated overview of the release."
	entries[4].Summary = "Feed text of the old post."
	f := &fake{now: now, sources: sources, entries: entries, patches: map[int64][]client.EntryPatch{}}
	s := New(language, f.deps())
	tt := ui.NewTester(func(c *ui.Context) { s.View(c); theme.Toasts(c) }, 1100, 800)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	return f, tt
}

func TestAISummaryRow(t *testing.T) {
	for language, label := range map[string]string{"en": "AI summary", "zh-Hans": "AI 摘要"} {
		t.Run(language, func(t *testing.T) {
			f, tt := setupAI(t, language)
			if !tt.HasText("Generated overview of the release.") {
				t.Fatalf("no AI summary in %v", tt.Texts())
			}
			if tt.HasText("The latest Go release.") {
				t.Error("the feed summary shows next to the AI summary")
			}
			if !tt.HasText("Feed text of the old post.") {
				t.Error("an entry without an AI summary lost its feed summary")
			}
			n := 0
			for _, text := range tt.Texts() {
				if text == label {
					n++
				}
			}
			if n != 1 {
				t.Errorf("%d %q marks, want one", n, label)
			}
			if len(f.queries) != 1 {
				t.Errorf("%d list queries; drawing the list must ask nothing else", len(f.queries))
			}
		})
	}
}

func TestCatalogsMatch(t *testing.T) {
	var raw map[string]map[string]any
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
	for _, label := range []string{"Last fetch failed: status 500", "Duration 12:34", "5 pictures", "2 more pictures",
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

func TestFailingSubscriptions(t *testing.T) {
	s, f, tt := setup(t, "en")
	for _, label := range []string{"Gopher, 1 unread, last fetch failed", "Last fetch failed: status 500",
		"1 failing", "Show only the 1 failing subscriptions"} {
		if _, ok := tt.Find(label); !ok {
			t.Errorf("no %q in %v", label, tt.Texts())
		}
	}

	f.sources[3].LastError, f.sources[3].ConsecutiveFailures, f.sources[3].UnreadCount = "timeout", 4, 0
	s.Refresh()
	f.settle(t, tt)
	for _, label := range []string{"Photos, last fetch failed", "Failed 4 times in a row: timeout",
		"2 failing", "Show only the 2 failing subscriptions"} {
		if _, ok := tt.Find(label); !ok {
			t.Errorf("no %q in %v", label, tt.Texts())
		}
	}

	if err := tt.Click("Show only the 2 failing subscriptions"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	has := func(text string) bool { r, ok := tt.Find(text); return ok && r.X < 216 }
	if !has("Failing") || !has("Show all") || !has("Gopher") || !has("Photos") || has("Go Blog") || has("Talks") ||
		has("2 failing") {
		t.Fatalf("not the failing ones only: %v", tt.Texts())
	}
	if err := tt.Click("Show all"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !has("Go Blog") || !has("Subscriptions") || !has("2 failing") || s.failingOnly {
		t.Fatalf("not all again: %v", tt.Texts())
	}

	// Another kind returns to the whole list.
	tt.Click("Show only the 2 failing subscriptions")
	tt.Frame()
	if err := tt.Click("Videos"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if s.failingOnly || !has("Talks") {
		t.Fatalf("still failing only: %v", tt.Texts())
	}

	// When none fail any more, so does the narrowing.
	tt.Click("All")
	f.settle(t, tt)
	tt.Click("Show only the 2 failing subscriptions")
	tt.Frame()
	for i := range f.sources {
		f.sources[i].LastError = ""
	}
	s.Refresh()
	f.settle(t, tt)
	if s.failingOnly || !has("Go Blog") || has("Show all") || has("Failing") {
		t.Fatalf("still failing only: %v", tt.Texts())
	}
}

func TestFailingInChinese(t *testing.T) {
	_, _, tt := setup(t, "zh-Hans")
	for _, label := range []string{"Gopher，1 条未读，上次抓取失败", "上次抓取失败：status 500", "1 个失败",
		"只显示 1 个抓取失败的订阅"} {
		if _, ok := tt.Find(label); !ok {
			t.Errorf("no %q in %v", label, tt.Texts())
		}
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

	// palette: the list's keys type into the palette and leave the list alone.
	tt.Key(0, ui.KeyEscape)
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	before, patched := s.selected, len(f.patches[7])
	tt.Type("jsm")
	tt.Frame()
	if !s.pal.open || s.pal.query != "jsm" || s.selected != before || s.reading != 0 || len(f.patches[7]) != patched {
		t.Fatalf("the palette let list keys through: query %q, selected %d, patches %v", s.pal.query, s.selected, f.patches)
	}
}

// TestSidebarKeys follows the keys that switch the kind, go to the sidebar and come back.
func TestSidebarKeys(t *testing.T) {
	s, f, tt := setup(t, "en")
	tt.Key(0, ui.Key3)
	f.settle(t, tt)
	if s.scope != (scope{kind: "video"}) || s.tab != 2 || f.queries[len(f.queries)-1].Kind != "video" {
		t.Fatalf("scope %v, tab %d", s.scope, s.tab)
	}
	if !slices.Contains(tt.Announcements(), "Videos, 1 unread") {
		t.Fatalf("announcements %v", tt.Announcements())
	}
	tt.Key(0, ui.Key1)
	f.settle(t, tt)
	if s.scope != (scope{}) || s.tab != 0 || !slices.Contains(tt.Announcements(), "All, 5 unread") {
		t.Fatalf("scope %v, announcements %v", s.scope, tt.Announcements())
	}
	for i, kind := range kinds {
		tt.Key(0, []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5}[i])
		f.settle(t, tt)
		if s.scope.kind != kind {
			t.Fatalf("key %d chose %q", i+1, s.scope.kind)
		}
	}

	tt.Key(0, ui.Key1)
	f.settle(t, tt)
	tt.Key(0, ui.KeyJ)
	if s.selected != 0 {
		t.Fatalf("selected %d", s.selected)
	}
	tt.Key(0, ui.KeyH)
	tt.Frame()
	if !s.inSide || !tt.Focused("All") {
		t.Fatalf("H does not go to the sidebar: %v", s.inSide)
	}
	// The keys of the entries are inert while the focus is in the sidebar.
	tt.Key(0, ui.KeyS)
	tt.Key(0, ui.KeyM)
	tt.Key(0, ui.KeyO)
	tt.Key(0, ui.KeyN)
	tt.Key(0, ui.KeyK)
	if s.selected != 0 || s.reading != 0 || len(f.patches) != 0 || !s.inSide {
		t.Fatalf("selected %d, reading %d, patches %v", s.selected, s.reading, f.patches)
	}
	// Up and Down move among the kinds, whose arrows choose one.
	tt.Key(0, ui.KeyDown)
	f.settle(t, tt)
	if s.scope.kind != "article" {
		t.Fatalf("scope %v", s.scope)
	}
	// A kind chosen with a number while the focus is in the sidebar works too.
	tt.Key(0, ui.Key3)
	f.settle(t, tt)
	if s.scope.kind != "video" || !s.inSide {
		t.Fatalf("scope %v", s.scope)
	}
	for i, key := range []ui.Key{ui.KeyL, ui.KeyRight, ui.KeyEscape} {
		if i > 0 {
			tt.Key(0, ui.KeyLeft)
			tt.Frame()
			if !s.inSide {
				t.Fatal("Left does not go to the sidebar")
			}
		}
		tt.Key(0, key)
		tt.Frame()
		if s.inSide || !tt.Focused("Talks, Profiling in practice, 2 h, Unread") {
			t.Fatalf("key %v does not go back to the list: in %v, selected %d, row %d, entries %d, texts %v", key, s.inSide, s.selected, s.focusRow, len(s.entries), tt.Texts())
		}
	}
	tt.Key(0, ui.KeyJ)
	tt.Key(0, ui.KeyJ)
	if s.selected != 0 {
		t.Fatalf("selected %d", s.selected)
	}
}

// TestSidebarSources moves among the subscriptions of a kind and opens the menu of one with the keyboard.
func TestSidebarSources(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.sources = append(f.sources, client.Source{ID: 5, Title: "Second Blog", Kind: "article"})
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	tt.Key(0, ui.Key2)
	f.settle(t, tt)
	if err := tt.Click("Go Blog"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	tt.Key(0, ui.KeyH)
	tt.Frame()
	if !s.inSide || !tt.Focused("Go Blog") {
		t.Fatalf("H does not focus the chosen subscription: %v", s.inSide)
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if !tt.Focused("Second Blog") || s.scope.source != 1 {
		t.Fatalf("Down does not move among the subscriptions: scope %v", s.scope)
	}
	tt.Key(ui.Shift, ui.KeyF10)
	if len(tt.Menu()) == 0 {
		t.Fatal("Shift+F10 shows no menu of the subscription")
	}
}

// TestSidebarClick checks that a click on a kind or a subscription leaves the keys of the entries working on the new
// list, while focus that came by the keyboard keeps them inert.
func TestSidebarClick(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.sources = append(f.sources, client.Source{ID: 5, Title: "Second Blog", Kind: "article"})
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	for _, name := range []string{"Videos", "Articles", "Go Blog"} {
		if err := tt.Click(name); err != nil {
			t.Fatal(err)
		}
		f.settle(t, tt)
		tt.Frame()
		if s.inSide {
			t.Fatalf("a click on %s puts the keys in the sidebar", name)
		}
		tt.Key(0, ui.KeyJ)
		if s.selected != 0 {
			t.Fatalf("J after a click on %s selects %d", name, s.selected)
		}
	}

	// H goes there by the keyboard: the keys of the entries are inert.
	tt.Key(0, ui.KeyH)
	tt.Frame()
	tt.Key(0, ui.KeyJ)
	if !s.inSide || s.selected != 0 {
		t.Fatalf("J in the sidebar: in %v, selected %d", s.inSide, s.selected)
	}
	// Enter on a subscription chooses it and keeps the focus in the sidebar.
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	f.settle(t, tt)
	tt.Frame()
	tt.Key(0, ui.KeyJ)
	if s.scope.source != 5 || !s.inSide || s.selected != -1 {
		t.Fatalf("Enter: scope %v, in %v, selected %d", s.scope, s.inSide, s.selected)
	}
	// A click after the keyboard hands the keys back to the list.
	if err := tt.Click("Go Blog"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	tt.Frame()
	tt.Key(0, ui.KeyJ)
	// The list of Go Blog comes back with its choice, the first entry, so J moves to the second.
	if s.inSide || s.selected != 1 {
		t.Fatalf("after a click: in %v, selected %d", s.inSide, s.selected)
	}
}

// TestKeysIgnored checks the places where the keys of the sidebar do not act.
func TestKeysIgnored(t *testing.T) {
	keys := func(t *testing.T, s *Screen, tt *ui.Tester) {
		t.Helper()
		for _, key := range []ui.Key{ui.Key3, ui.Key5, ui.KeyH, ui.KeyLeft} {
			tt.Key(0, key)
			tt.Frame()
		}
		if s.scope != (scope{}) || s.inSide || s.sideFocus {
			t.Fatalf("a key acted: scope %v, in the sidebar %v", s.scope, s.inSide)
		}
	}
	t.Run("search", func(t *testing.T) {
		s, _, tt := setup(t, "en")
		tt.Key(0, ui.KeySlash)
		tt.Type("3h")
		if s.query != "3h" {
			t.Fatalf("query %q", s.query)
		}
		keys(t, s, tt)
	})
	t.Run("key sheet", func(t *testing.T) {
		s, _, tt := setup(t, "en")
		s.ShowKeys()
		tt.Frame()
		keys(t, s, tt)
	})
	t.Run("dialog", func(t *testing.T) {
		s, _, tt := setup(t, "en")
		s.subs.Add()
		tt.Frame()
		keys(t, s, tt)
	})
	t.Run("reader", func(t *testing.T) {
		s, f, tt := setup(t, "en")
		if err := tt.Click("Go Blog, Go 1.27 is released, 5 min, Unread"); err != nil {
			t.Fatal(err)
		}
		f.settle(t, tt)
		if s.reading == 0 {
			t.Fatal("the reader is not open")
		}
		keys(t, s, tt)
		if s.reading == 0 {
			t.Fatal("a key closed the reader")
		}
	})
	t.Run("in the sidebar", func(t *testing.T) {
		s, _, tt := setup(t, "en")
		tt.Key(0, ui.KeyH)
		tt.Frame()
		tt.Key(0, ui.KeyH)
		tt.Key(0, ui.KeyLeft)
		tt.Frame()
		if !s.inSide || s.selected != -1 {
			t.Fatalf("in the sidebar %v, selected %d", s.inSide, s.selected)
		}
	})
}

// TestEntryMenuKey opens the menu of the focused entry with Shift+F10.
func TestEntryMenuKey(t *testing.T) {
	_, _, tt := setup(t, "en")
	tt.Key(0, ui.KeyJ)
	tt.Key(ui.Shift, ui.KeyF10)
	if menu := tt.Menu(); len(menu) == 0 || menu[0] != "Star" {
		t.Fatalf("menu %q", menu)
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

// TestReaderPrefetch follows the articles beside the one read: got once in the background, shown at once by J and K,
// read only, and dropped with the list.
func TestReaderPrefetch(t *testing.T) {
	count := func(f *fake, id int64) int {
		n := 0
		for _, got := range f.articles {
			if got == id {
				n++
			}
		}
		return n
	}
	s, f, tt := setup(t, "en")
	if err := tt.Click("Go Blog, Go 1.27 is released, 5 min, Unread"); err != nil {
		t.Fatal(err)
	}
	// Nothing is kept yet: the first article loads, and the next one, 4, is got after it.
	if tt.HasText("The text of Go 1.27 is released.") {
		t.Fatal("the article shows before it loads")
	}
	f.settle(t, tt)
	if count(f, 8) != 1 || count(f, 4) != 1 || count(f, 3) != 0 || f.aiAsked != 0 {
		t.Fatalf("articles %v, summaries asked %d", f.articles, f.aiAsked)
	}
	if !slices.Contains(f.aiReads, 4) {
		t.Fatalf("the summary of the next article is not read: %v", f.aiReads)
	}
	// A prefetched article shows with the key, with no wait and no second request for it.
	tt.Key(0, ui.KeyJ)
	if s.reading != 4 || !tt.HasText("The text of Range over func.") {
		t.Fatalf("reading %d, texts %v", s.reading, tt.Texts())
	}
	f.settle(t, tt)
	if count(f, 4) != 1 || count(f, 8) != 1 || count(f, 3) != 1 || f.aiAsked != 0 {
		t.Fatalf("articles %v, summaries asked %d", f.articles, f.aiAsked)
	}
	// The summary is read again as the article opens, as it may have changed.
	if n := slices.Index(f.aiReads, 4); n < 0 || slices.Index(f.aiReads[n+1:], 4) < 0 {
		t.Fatalf("the summary is not read again: %v", f.aiReads)
	}
	tt.Key(0, ui.KeyJ)
	if s.reading != 3 || !tt.HasText("The text of Go turns 16.") {
		t.Fatalf("reading %d", s.reading)
	}
	f.settle(t, tt)
	// K back to 4 is kept too, and a step to an article not got shows the wait as before.
	tt.Key(0, ui.KeyK)
	if !tt.HasText("The text of Range over func.") {
		t.Fatal("the article before does not show at once")
	}
	f.settle(t, tt)
	s.reader.Forget()
	f.queue = nil
	got := len(f.articles)
	tt.Key(0, ui.KeyJ)
	if tt.HasText("The text of Go turns 16.") || len(f.queue) == 0 {
		t.Fatal("an article got before the list changed shows at once")
	}
	f.settle(t, tt)
	if !tt.HasText("The text of Go turns 16.") || len(f.articles) <= got {
		t.Fatalf("the article does not load: %v", f.articles)
	}
}

// TestReaderPrefetchDropped checks that changing the list stops what was got for the old one.
func TestReaderPrefetchDropped(t *testing.T) {
	s, f, tt := setup(t, "en")
	if err := tt.Click("Go Blog, Go 1.27 is released, 5 min, Unread"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if err := tt.Click("Back to All"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Go Blog"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	before := len(f.articles)
	if err := tt.Click("Go Blog, Go 1.27 is released, 5 min, Unread"); err != nil {
		t.Fatalf("%v in %v", err, tt.Texts())
	}
	f.settle(t, tt)
	if len(f.articles) == before || s.reading != 8 {
		t.Fatalf("reading %d, articles %v", s.reading, f.articles)
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
	f.setErr = &client.APIError{Status: 500, Code: "internal"}
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

// TestOrderToggle shows the reading-time menu offers the other order, keeps it in the preferences of the device, lists
// the oldest entry first and tints the clock while it is oldest first.
func TestOrderToggle(t *testing.T) {
	p := prefs.Default()
	s, f, tt := viewing(t, &p)
	if s.entries[0].ID != 8 || f.queries[len(f.queries)-1].Oldest {
		t.Fatalf("starts with %d, %+v", s.entries[0].ID, f.queries)
	}
	if err := tt.Click("Reading time: any length"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Oldest first"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	f.settle(t, tt)
	tt.Frame()
	last := f.queries[len(f.queries)-1]
	if !p.OldestFirst || !s.oldest || !last.Oldest || last.Cursor != "" {
		t.Fatalf("prefs %+v, oldest %v, queries %+v", p, s.oldest, f.queries)
	}
	if s.entries[0].ID != 3 || s.entries[len(s.entries)-1].ID != 8 {
		t.Fatalf("entries %v", s.entries)
	}
	if _, ok := tt.Find("Reading time: any length. Oldest first"); !ok {
		t.Fatalf("the clock is not named by the order: %v", tt.Texts())
	}
	if !slices.Contains(tt.Announcements(), "Oldest first") {
		t.Fatalf("announcements %v", tt.Announcements())
	}
	// Back to newest first: the list kept for that order is drawn again.
	if err := tt.Click("Reading time: any length. Oldest first"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Newest first"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	f.settle(t, tt)
	tt.Frame()
	if p.OldestFirst || s.oldest || s.entries[0].ID != 8 {
		t.Fatalf("prefs %+v, first %d", p, s.entries[0].ID)
	}
}

// TestOrderKept shows a device that chose oldest first lists that way from the start, and that polling makes no
// "new" pill in that order, since new entries come when paging reaches the end.
func TestOrderKept(t *testing.T) {
	p := prefs.Default()
	p.OldestFirst = true
	s, f, tt := viewing(t, &p)
	if !f.queries[0].Oldest || s.entries[0].ID != 3 {
		t.Fatalf("queries %+v, first %d", f.queries, s.entries[0].ID)
	}
	f.entries = append([]client.Entry{{ID: 9, SourceID: 1, SourceTitle: "Go Blog", Kind: "article", Title: "Fresh",
		CreatedAt: now.UnixMilli()}}, f.entries...)
	f.now = f.now.Add(pollEvery)
	tt.Frame()
	f.settle(t, tt)
	if s.fresh != 0 || tt.HasText("1 new") || len(s.entries) != 6 {
		t.Fatalf("fresh %d, %d entries, texts %v", s.fresh, len(s.entries), tt.Texts())
	}
}

// TestOrderMarks shows the marking actions cover the entries they say in the oldest-first order: those above an entry
// are the older ones.
func TestOrderMarks(t *testing.T) {
	p := prefs.Default()
	p.OldestFirst = true
	s, f, tt := viewing(t, &p)
	if err := tt.RightClick("Harbor at dawn"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Mark above as read"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if !reflect.DeepEqual(f.marked, []client.ReadScope{{UpTo: 4}}) {
		t.Fatalf("marked %v", f.marked)
	}
	if s.entries[0].ID != 3 || !s.entries[0].IsRead {
		t.Fatalf("entry %d read %v", s.entries[0].ID, s.entries[0].IsRead)
	}
	s.markAll()
	f.settle(t, tt)
	if got := f.marked[len(f.marked)-1]; got.UpTo != 8 || got.Newer != 0 {
		t.Fatalf("marked %v", f.marked)
	}
	for _, e := range s.entries {
		if !e.IsRead {
			t.Fatalf("entry %d is unread", e.ID)
		}
	}
}

// TestCompactDensity shows the Compact list density makes rows lower and keeps every part of them.
func TestCompactDensity(t *testing.T) {
	height := func(density string) (float32, bool) {
		p := prefs.Default()
		p.Density = density
		s, _, tt := viewing(t, &p)
		tt.Frame()
		return s.rects[0].H, tt.HasText("Go 1.27 is released") && tt.HasText("The latest Go release.")
	}
	comfortable, ok := height(prefs.DensityComfortable)
	if !ok {
		t.Fatal("the comfortable row lacks its parts")
	}
	compact, ok := height(prefs.DensityCompact)
	if !ok {
		t.Fatal("the compact row lost a part")
	}
	if compact >= comfortable {
		t.Fatalf("compact row %v is not lower than comfortable %v", compact, comfortable)
	}
}

func TestOfflineLine(t *testing.T) {
	s, f, tt := setup(t, "en")
	const line = "Can’t reach the server · last updated 12:00"
	f.entriesErr = errors.New("offline")
	f.now = f.now.Add(pollEvery)
	tt.Frame()
	f.settle(t, tt)
	if !s.offline || !tt.HasText(line) || len(s.entries) != 6 || !tt.HasText("Go 1.27 is released") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if !slices.Contains(tt.Announcements(), line) {
		t.Fatalf("announcements %v", tt.Announcements())
	}
	before := len(tt.Announcements())
	f.now = f.now.Add(pollEvery)
	tt.Frame()
	f.settle(t, tt)
	if !tt.HasText(line) || len(tt.Announcements()) != before {
		t.Fatalf("announcements %v", tt.Announcements())
	}
	f.entriesErr = nil
	f.now = f.now.Add(pollEvery)
	tt.Frame()
	f.settle(t, tt)
	if s.offline || tt.HasText(line) || len(s.entries) != 6 {
		t.Fatalf("texts %v", tt.Texts())
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
	if !reflect.DeepEqual(f.unmarked, [][]int64{{8, 7, 6, 5, 3}}) || !reflect.DeepEqual(f.unmarkedAt, []int64{fakeReadAt}) {
		t.Fatalf("unmarked %v at %v", f.unmarked, f.unmarkedAt)
	}
	if s.unreadIn(s.scope) != 5 || tt.HasText("Undo") || s.undoable != nil {
		t.Fatalf("unread %d, texts %v", s.unreadIn(s.scope), tt.Texts())
	}
}

func TestMarkOlder(t *testing.T) {
	for language, c := range map[string]struct {
		items                        []string
		all, one, week, marked, none string
	}{
		"en": {[]string{"Mark all in “All” as read", "-", "Mark older than 1 day as read", "Mark older than 1 week as read",
			"Mark older than 1 month as read"}, "Mark all in “All” as read", "Mark older than 1 day as read",
			"Mark older than 1 week as read", "Marked 3 entries in “All” older than 1 day as read",
			"No unread entries older than 1 week"},
		"zh-Hans": {[]string{"把「全部」全部标为已读", "-", "把 1 天前的标为已读", "把 1 周前的标为已读", "把 1 个月前的标为已读"},
			"把「全部」全部标为已读", "把 1 天前的标为已读", "把 1 周前的标为已读", "已把「全部」里 1 天前的 3 条标为已读",
			"没有 1 周前的未读条目"},
	} {
		t.Run(language, func(t *testing.T) {
			s, f, tt := setup(t, language)
			if err := tt.RightClick(c.all); err != nil {
				t.Fatal(err)
			}
			if menu := tt.Menu(); !slices.Equal(menu, c.items) {
				t.Fatalf("menu %q", menu)
			}
			if err := tt.ChooseMenuItem(c.one); err != nil {
				t.Fatal(err)
			}
			f.settle(t, tt)
			day := now.Add(-24 * time.Hour).UnixMilli()
			if !reflect.DeepEqual(f.marked, []client.ReadScope{{OlderThan: day}}) {
				t.Fatalf("marked %v, want a cut at %d", f.marked, day)
			}
			if !slices.Contains(tt.Announcements(), c.marked) || s.unreadIn(s.scope) != 2 {
				t.Fatalf("announcements %v, unread %d", tt.Announcements(), s.unreadIn(s.scope))
			}
			// The entries of the newest days stay unread.
			for _, e := range s.entries {
				if want := e.ID == 8 || e.ID == 7; !e.IsRead != want && e.ID != 4 {
					t.Fatalf("entry %d read %v", e.ID, e.IsRead)
				}
			}
			if err := tt.Click(s.text.Undo); err != nil {
				t.Fatal(err)
			}
			f.settle(t, tt)
			if !reflect.DeepEqual(f.unmarked, [][]int64{{6, 5, 3}}) || s.unreadIn(s.scope) != 5 {
				t.Fatalf("unmarked %v, unread %d", f.unmarked, s.unreadIn(s.scope))
			}

			// A week back only the oldest unread entry is left: one is marked, then none is, with no undo.
			if err := tt.RightClick(c.all); err != nil {
				t.Fatal(err)
			}
			if err := tt.ChooseMenuItem(c.week); err != nil {
				t.Fatal(err)
			}
			f.settle(t, tt)
			if err := tt.RightClick(c.all); err != nil {
				t.Fatal(err)
			}
			if err := tt.ChooseMenuItem(c.week); err != nil {
				t.Fatal(err)
			}
			f.settle(t, tt)
			week := now.Add(-7 * 24 * time.Hour).UnixMilli()
			if len(f.marked) != 3 || f.marked[2].OlderThan != week {
				t.Fatalf("marked %v", f.marked)
			}
			if !tt.HasText(c.none) {
				t.Fatalf("texts %v", tt.Texts())
			}
		})
	}
}

func TestMarkOlderScopeAndLimits(t *testing.T) {
	s, f, tt := setup(t, "en")
	// Under a kind the scope follows, and the month item reaches back 30 days.
	if err := tt.Click("Articles"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if err := tt.RightClick("Mark all in “Articles” as read"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Mark older than 1 month as read"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	month := now.Add(-30 * 24 * time.Hour).UnixMilli()
	if !reflect.DeepEqual(f.marked, []client.ReadScope{{Kind: "article", OlderThan: month}}) {
		t.Fatalf("marked %v", f.marked)
	}
	// A search or Starred has no scope the server knows: the menu is dimmed.
	if err := tt.Click("Starred"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if err := tt.RightClick("Mark all in “Articles” as read"); err == nil {
		if err := tt.ChooseMenuItem("Mark older than 1 day as read"); err == nil {
			t.Fatal("marked under Starred")
		}
		tt.CloseMenu()
	}
	if len(f.marked) != 1 || s.canMarkAll() {
		t.Fatalf("marked %v", f.marked)
	}
}

func TestMarkSourceOlder(t *testing.T) {
	s, f, tt := setup(t, "en")
	if err := tt.RightClick("Go Blog"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Mark older than 1 week as read"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	week := now.Add(-7 * 24 * time.Hour).UnixMilli()
	if !reflect.DeepEqual(f.marked, []client.ReadScope{{SourceID: 1, OlderThan: week}}) {
		t.Fatalf("marked %v", f.marked)
	}
	if !slices.Contains(tt.Announcements(), "Marked 1 entries in “Go Blog” older than 1 week as read") || s.sources[0].UnreadCount != 1 {
		t.Fatalf("announcements %v, sources %v", tt.Announcements(), s.sources)
	}
	if err := tt.Click("Undo"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if !reflect.DeepEqual(f.unmarked, [][]int64{{3}}) || s.sources[0].UnreadCount != 2 {
		t.Fatalf("unmarked %v, sources %v", f.unmarked, s.sources)
	}
}

func TestMarkAbove(t *testing.T) {
	s, f, tt := setup(t, "en")
	if err := tt.RightClick("Harbor at dawn"); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); !slices.Equal(menu, []string{"Star", "Mark as read", "Open in browser", "Copy link", "-", "Copy as Markdown", "Save as Markdown…", "-", "Mark above as read"}) {
		t.Fatalf("menu %q", menu)
	}
	if err := tt.ChooseMenuItem("Mark above as read"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if !reflect.DeepEqual(f.marked, []client.ReadScope{{Newer: 5, UpTo: 8}}) {
		t.Fatalf("marked %v", f.marked)
	}
	for i, read := range []bool{true, true, true, false, true, false} {
		if s.entries[i].IsRead != read {
			t.Fatalf("entry %d read %v", s.entries[i].ID, s.entries[i].IsRead)
		}
	}
	if !slices.Contains(tt.Announcements(), "Marked 3 entries above as read") || s.sources[0].UnreadCount != 1 {
		t.Fatalf("announcements %v, sources %v", tt.Announcements(), s.sources)
	}

	// Everything above is read now: the item is dimmed.
	if err := tt.RightClick("Harbor at dawn"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Mark above as read"); err == nil {
		t.Fatal("marked above with nothing unread above")
	}
	tt.CloseMenu()

	if err := tt.Click("Undo"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if !reflect.DeepEqual(f.unmarked, [][]int64{{8, 7, 6}}) || s.unreadIn(s.scope) != 5 || s.undoable != nil {
		t.Fatalf("unmarked %v, unread %d", f.unmarked, s.unreadIn(s.scope))
	}
}

func TestExportMarkdownFromMenu(t *testing.T) {
	_, f, tt := setup(t, "en")
	if err := tt.RightClick("Harbor at dawn"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Copy as Markdown"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	copied := tt.Clipboard()
	if !strings.HasPrefix(copied, "# Harbor at dawn\n") || !strings.HasSuffix(copied, "\n\nThe text of Harbor at dawn.\n") || !slices.Contains(tt.Announcements(), "Copied") {
		t.Fatalf("clipboard %q, announcements %v", copied, tt.Announcements())
	}
	f.saveTo = filepath.Join(t.TempDir(), "out.md")
	if err := tt.RightClick("Harbor at dawn"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Save as Markdown…"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if data, err := os.ReadFile(f.saveTo); err != nil || string(data) != copied {
		t.Fatalf("file %q (%v), want %q", data, err, copied)
	}
}

func TestMarkAboveScope(t *testing.T) {
	s, f, tt := setup(t, "en")
	// The first entry has nothing above it.
	if err := tt.RightClick("Go 1.27 is released"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Mark above as read"); err == nil {
		t.Fatal("marked above the first entry")
	}
	tt.CloseMenu()
	// Starred and a search list entries the server's scopes do not know.
	s.filter = starred
	if s.canMarkAbove(3) {
		t.Fatal("mark above is offered under Starred")
	}
	s.filter, s.searched = everything, "go"
	if s.canMarkAbove(3) {
		t.Fatal("mark above is offered for a search")
	}
	s.searched = ""

	// In a kind, the request carries the kind.
	if err := tt.Click("Articles"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if err := tt.RightClick("Go turns 16"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Mark above as read"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if !reflect.DeepEqual(f.marked, []client.ReadScope{{Kind: "article", Newer: 3, UpTo: 8}}) {
		t.Fatalf("marked %v", f.marked)
	}
}

// crowd is a timeline of n articles, newest first, in a window that shows only some of them.
func crowd(t *testing.T, n int, height int) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	f := &fake{now: now, patches: map[int64][]client.EntryPatch{},
		sources: []client.Source{{ID: 1, Title: "Go Blog", Kind: "article", UnreadCount: n}}}
	for i := n; i >= 1; i-- {
		f.entries = append(f.entries, client.Entry{ID: int64(i), SourceID: 1, SourceTitle: "Go Blog", Kind: "article",
			Title: fmt.Sprintf("Entry %d", i), URL: fmt.Sprintf("https://go.dev/%d", i),
			CreatedAt: ago(time.Duration(n-i+1) * time.Minute)})
	}
	s := New("en", f.deps())
	tt := ui.NewTester(func(c *ui.Context) { s.View(c); theme.Toasts(c) }, 1100, height)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	tt.Frame()
	return s, f, tt
}

// TestMarkVisibleWall marks the pictures that are whole on the Images wall, and scrolls the wall to the first one after.
func TestMarkVisibleWall(t *testing.T) {
	s, f, tt := crowd(t, 40, 600)
	for i := range f.entries {
		f.entries[i].Kind = "image"
		f.entries[i].Images = []client.Image{{URL: fmt.Sprintf("https://photos.example/%d.jpg", i), Width: 1200, Height: 800}}
	}
	f.sources[0].Kind = "image"
	if err := tt.Click("Images"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	tt.Key(0, ui.KeyL)
	tt.Frame()
	first, end := shown(t, s)
	if first != 0 {
		t.Fatalf("the wall starts at %d", first)
	}
	tt.Key(0, ui.KeyV)
	f.settle(t, tt)
	tt.Frame()
	want := client.ReadScope{Kind: "image", UpTo: s.entries[0].ID, Newer: s.entries[end].ID}
	if !reflect.DeepEqual(f.marked, []client.ReadScope{want}) || s.selected != end {
		t.Fatalf("marked %v, want %v, selected %d", f.marked, want, s.selected)
	}
	if s.wall.Y <= 0 || s.rects[end].Y < s.top() || s.rects[end].Y > s.top()+1 {
		t.Fatalf("the wall is at %v, entry %d at %v", s.wall.Y, end, s.rects[end].Y)
	}
}

// shown returns the entries that are whole in the window, as the list drew them: the first and one past the last.
func shown(t *testing.T, s *Screen) (first, end int) {
	t.Helper()
	for first < len(s.entries) && !s.fullyShown(first) {
		first++
	}
	for end = first; end < len(s.entries) && s.fullyShown(end); end++ {
	}
	if first == len(s.entries) || end-first < 3 || end == len(s.entries) {
		t.Fatalf("the window shows entries %d to %d of %d", first, end, len(s.entries))
	}
	return first, end
}

func TestMarkVisible(t *testing.T) {
	s, f, tt := crowd(t, 60, 420)
	// An entry read already is left alone and not counted.
	f.entries[1].IsRead = true
	s.entries[1].IsRead = true
	first, end := shown(t, s)
	if first != 0 {
		t.Fatalf("the list starts at %d", first)
	}
	tt.Key(0, ui.KeyV)
	f.settle(t, tt)
	tt.Frame()
	want := client.ReadScope{UpTo: s.entries[0].ID, Newer: s.entries[end].ID}
	if !reflect.DeepEqual(f.marked, []client.ReadScope{want}) {
		t.Fatalf("marked %v, want %v", f.marked, want)
	}
	for i, e := range s.entries[:end+1] {
		if e.IsRead != (i < end) {
			t.Fatalf("entry %d read %v", e.ID, e.IsRead)
		}
	}
	if s.selected != end || !tt.Focused(fmt.Sprintf("Go Blog, Entry %d, %d min, Unread", s.entries[end].ID, end+1)) {
		t.Fatalf("selected %d, want %d", s.selected, end)
	}
	// The entry after the visible ones is at the top, below the toolbar.
	r := s.rects[end]
	if r.Y < s.top() || r.Y > s.top()+float32(40) {
		t.Fatalf("entry %d is at %v, the toolbar ends at %v", end, r.Y, s.top())
	}
	wantText := fmt.Sprintf("Marked %d visible entries as read", end-1)
	if !slices.Contains(tt.Announcements(), wantText) || !tt.HasText("Undo") {
		t.Fatalf("announcements %v", tt.Announcements())
	}
	if s.sources[0].UnreadCount != 60-end {
		t.Fatalf("unread %d", s.sources[0].UnreadCount)
	}

	// Undo brings back the entries marked, not the one that was read before.
	if err := tt.Click("Undo"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if len(f.unmarked) != 1 || len(f.unmarked[0]) != end-1 || slices.Contains(f.unmarked[0], s.entries[1].ID) {
		t.Fatalf("unmarked %v", f.unmarked)
	}
}

func TestMarkVisibleUndoKey(t *testing.T) {
	s, f, tt := crowd(t, 60, 420)
	tt.Key(0, ui.KeyV)
	f.settle(t, tt)
	tt.Frame()
	if s.undoable == nil || len(f.marked) != 1 {
		t.Fatalf("marked %v", f.marked)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	f.settle(t, tt)
	if len(f.unmarked) != 1 || s.sources[0].UnreadCount != 60 || s.undoable != nil {
		t.Fatalf("unmarked %v, unread %d", f.unmarked, s.sources[0].UnreadCount)
	}
}

// TestMarkVisibleSharesUndo has the last marking replace the one before it.
func TestMarkVisibleSharesUndo(t *testing.T) {
	s, f, tt := crowd(t, 60, 420)
	tt.Key(0, ui.KeyV)
	f.settle(t, tt)
	tt.Frame()
	first := s.undoable
	tt.Key(0, ui.KeyV)
	f.settle(t, tt)
	tt.Frame()
	if len(f.marked) != 2 || s.undoable == nil || s.undoable == first {
		t.Fatalf("marked %v", f.marked)
	}
	if f.marked[1].UpTo != f.marked[0].Newer {
		t.Fatalf("the second marking starts at %d, the first ended before %d", f.marked[1].UpTo, f.marked[0].Newer)
	}
}

// TestMarkVisibleNothingUnread scrolls on without a request or a toast.
func TestMarkVisibleNothingUnread(t *testing.T) {
	s, f, tt := crowd(t, 60, 420)
	_, end := shown(t, s)
	for i := 0; i < end; i++ {
		f.entries[i].IsRead = true
		s.entries[i].IsRead = true
	}
	tt.Key(0, ui.KeyV)
	tt.Frame()
	if len(f.queue) != 0 || len(f.marked) != 0 || s.undoable != nil {
		t.Fatalf("queued %d, marked %v", len(f.queue), f.marked)
	}
	if s.selected != end || s.rects[end].Y > s.top()+40 {
		t.Fatalf("selected %d, entry %d at %v", s.selected, end, s.rects[end].Y)
	}
}

// TestMarkVisiblePageEdge leaves the last entry loaded unread while another page follows, and goes to it.
func TestMarkVisiblePageEdge(t *testing.T) {
	s, f, tt := crowd(t, 60, 20000)
	if s.next != "" {
		t.Fatal("more pages")
	}
	s.next, s.moreErr = "more", true // another page follows, and the footer waits for a retry
	last := len(s.entries) - 1
	tt.Key(0, ui.KeyV)
	f.settle(t, tt)
	if s.entries[last].IsRead || !s.entries[last-1].IsRead {
		t.Fatalf("the last entries read: %v %v", s.entries[last-1].IsRead, s.entries[last].IsRead)
	}
	if s.selected != last || len(f.marked) != 1 || f.marked[0].Newer != s.entries[last].ID {
		t.Fatalf("selected %d, marked %v", s.selected, f.marked)
	}
}

// TestMarkVisibleEnd marks to the end of the list, with no lower bound, and keeps the list where it is.
func TestMarkVisibleEnd(t *testing.T) {
	s, f, tt := crowd(t, 8, 20000)
	if s.next != "" {
		t.Fatal("more pages")
	}
	tt.Key(0, ui.KeyV)
	f.settle(t, tt)
	if !reflect.DeepEqual(f.marked, []client.ReadScope{{UpTo: 8}}) {
		t.Fatalf("marked %v", f.marked)
	}
	if s.selected != 7 || s.unreadIn(s.scope) != 0 {
		t.Fatalf("selected %d", s.selected)
	}
}

// TestMarkVisibleScope carries the kind of the tab in the request.
func TestMarkVisibleScope(t *testing.T) {
	s, f, tt := setup(t, "en")
	if err := tt.Click("Articles"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	tt.Key(0, ui.KeyL)
	tt.Frame()
	tt.Key(0, ui.KeyV)
	f.settle(t, tt)
	if !reflect.DeepEqual(f.marked, []client.ReadScope{{Kind: "article", UpTo: 8}}) || s.selected != 2 {
		t.Fatalf("marked %v, selected %d", f.marked, s.selected)
	}
}

// TestMarkVisibleIgnored leaves V alone where the visible entries are not the scope's or the key is not for the list.
func TestMarkVisibleIgnored(t *testing.T) {
	check := func(t *testing.T, s *Screen, f *fake, tt *ui.Tester) {
		t.Helper()
		tt.Key(0, ui.KeyV)
		f.settle(t, tt)
		tt.Frame()
		if len(f.marked) != 0 || s.undoable != nil {
			t.Fatalf("marked %v", f.marked)
		}
	}
	t.Run("starred", func(t *testing.T) {
		s, f, tt := crowd(t, 60, 420)
		s.filter = starred
		check(t, s, f, tt)
	})
	t.Run("search", func(t *testing.T) {
		s, f, tt := crowd(t, 60, 420)
		s.searched = "entry"
		check(t, s, f, tt)
	})
	t.Run("loading", func(t *testing.T) {
		s, f, tt := crowd(t, 60, 420)
		s.loading = true
		check(t, s, f, tt)
	})
	t.Run("search box", func(t *testing.T) {
		s, f, tt := crowd(t, 60, 420)
		tt.Key(0, ui.KeySlash)
		tt.Frame()
		tt.Type("v")
		if s.query != "v" {
			t.Fatalf("query %q", s.query)
		}
		check(t, s, f, tt)
	})
	t.Run("key sheet", func(t *testing.T) {
		s, f, tt := crowd(t, 60, 420)
		s.ShowKeys()
		tt.Frame()
		check(t, s, f, tt)
	})
	t.Run("dialog", func(t *testing.T) {
		s, f, tt := crowd(t, 60, 420)
		s.subs.Add()
		tt.Frame()
		check(t, s, f, tt)
	})
	t.Run("reader", func(t *testing.T) {
		s, f, tt := crowd(t, 60, 420)
		if err := tt.Click("Go Blog, Entry 60, 1 min, Unread"); err != nil {
			t.Fatal(err)
		}
		f.settle(t, tt)
		if s.reading == 0 {
			t.Fatal("the reader is not open")
		}
		check(t, s, f, tt)
	})
	t.Run("sidebar", func(t *testing.T) {
		s, f, tt := crowd(t, 60, 420)
		tt.Key(0, ui.KeyH)
		tt.Frame()
		if !s.inSide {
			t.Fatal("not in the sidebar")
		}
		check(t, s, f, tt)
	})
}

func TestEntryMenuActions(t *testing.T) {
	_, f, tt := setup(t, "en")
	if err := tt.RightClick("Profiling in practice"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Star"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if p := f.patches[7]; len(p) != 1 || p[0].Starred == nil || !*p[0].Starred {
		t.Fatalf("patches %+v", f.patches)
	}
	if err := tt.RightClick("Profiling in practice"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Mark as read"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if p := f.patches[7]; len(p) != 2 || p[1].IsRead == nil || !*p[1].IsRead {
		t.Fatalf("patches %+v", f.patches)
	}
	if err := tt.RightClick("Profiling in practice"); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); menu[0] != "Remove star" || menu[1] != "Mark as unread" {
		t.Fatalf("menu %q", menu)
	}
}

func TestMarkSource(t *testing.T) {
	s, f, tt := setup(t, "en")
	if err := tt.RightClick("Go Blog"); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); len(menu) < 2 || menu[1] != "Mark subscription as read" {
		t.Fatalf("menu %q", menu)
	}
	if err := tt.ChooseMenuItem("Mark subscription as read"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if !reflect.DeepEqual(f.marked, []client.ReadScope{{SourceID: 1}}) {
		t.Fatalf("marked %v", f.marked)
	}
	if !slices.Contains(tt.Announcements(), "Marked 2 entries in “Go Blog” as read") || s.sources[0].UnreadCount != 0 {
		t.Fatalf("announcements %v, sources %v", tt.Announcements(), s.sources)
	}
	// Nothing unread left: the item is dimmed.
	if err := tt.RightClick("Go Blog"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Mark subscription as read"); err == nil {
		t.Fatal("marked a subscription with nothing unread")
	}
	tt.CloseMenu()

	// One undo is shared: marking all as read replaces it, and Cmd+Z undoes the last one.
	if err := tt.Click("Mark all in “All” as read"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	tt.Key(ui.Cmd, ui.KeyZ)
	f.settle(t, tt)
	if !reflect.DeepEqual(f.unmarked, [][]int64{{7, 6, 5}}) || s.undoable != nil {
		t.Fatalf("unmarked %v", f.unmarked)
	}
}

func TestMarkSourceUndo(t *testing.T) {
	s, f, tt := setup(t, "en")
	if err := tt.RightClick("Talks"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Mark subscription as read"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if err := tt.Click("Undo"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if !reflect.DeepEqual(f.unmarked, [][]int64{{7}}) || s.sources[1].UnreadCount != 1 {
		t.Fatalf("unmarked %v, sources %v", f.unmarked, s.sources)
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

// TestUndoTooLarge shows a mark as read of more entries than one undo takes offers no Undo.
func TestUndoTooLarge(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.extraRead = maxUndo
	if err := tt.Click("Mark all in “All” as read"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	tt.Key(ui.Cmd, ui.KeyZ)
	f.settle(t, tt)
	if len(f.unmarked) != 0 || s.undoable != nil || tt.HasText("Undo") {
		t.Fatalf("unmarked %v, texts %v", f.unmarked, tt.Texts())
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
	for _, name := range []string{"Switch kind", "Go to the sidebar", "Context menu"} {
		if !tt.HasText(name) {
			t.Fatalf("no %q in %v", name, tt.Texts())
		}
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
			if _, ok := tt.Find("Loading"); ok {
				t.Fatal("the loading state is drawn before the wait is over")
			}
			f.now = f.now.Add(skeletonDelay)
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

func TestNoSubscriptionsImport(t *testing.T) {
	_, f, tt := setup(t, "en")
	f.sources, f.entries = nil, nil
	s := New("en", f.deps())
	tt = ui.NewTester(s.View, 1100, 800)
	s.Use(f, "")
	f.settle(t, tt)
	for _, text := range []string{"No subscriptions yet", "Add your first subscription", "Import OPML…",
		"Entries from your subscriptions show up here, newest first. Feed addresses work, and so do links to YouTube, X, GitHub, Bilibili, Weibo and Telegram."} {
		if !tt.HasText(text) {
			t.Fatalf("no %q in %v", text, tt.Texts())
		}
	}
	if err := tt.Click("Import OPML…"); err != nil {
		t.Fatal(err)
	}
	if f.imports != 1 || s.subs.Open() {
		t.Fatalf("imports %d, add dialog %v", f.imports, s.subs.Open())
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

// rowRect is the rectangle of the row or card with the given accessible name.
func rowRect(t *testing.T, tt *ui.Tester, label string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("no row %.60q", label)
	}
	return r
}

// A title stops at two lines whatever its length, and a word with no break point breaks where it must: the row
// keeps its width and a height of two lines. The full title stays the accessible name.
func TestLongTitles(t *testing.T) {
	words := strings.TrimSpace(strings.Repeat("A very long title that goes on and on ", 12))
	url := "https://example.com/" + strings.Repeat("a-path-with-no-break/", 20)
	han := strings.Repeat("高效工作，快乐生活。", 20)
	for _, tc := range []struct{ name, long, longer string }{
		{"words", words, words + words},
		{"no break point", url, url + url},
		{"Chinese", han, han + han},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sources, entries := sample()
			entries[0].Title, entries[0].Summary, entries[0].ThumbnailURL = tc.long, "", ""
			entries[5].Title = tc.longer
			f := &fake{now: now, sources: sources, entries: entries, patches: map[int64][]client.EntryPatch{}}
			s := New("en", f.deps())
			tt := ui.NewTester(s.View, 1100, 2400)
			s.Use(f, "")
			tt.Frame()
			f.settle(t, tt)
			a := rowRect(t, tt, "Go Blog, "+tc.long+", 5 min, Unread")
			b := rowRect(t, tt, "Go Blog, "+tc.longer+", Sep 13, 2025, Unread")
			one := rowRect(t, tt, "Go Blog, Range over func, Sep 8, Read, Starred")
			// The heights are compared to within a pixel, as the layout of other text sizes rounds differently.
			if a.H-b.H > 1 || b.H-a.H > 1 || a.H <= one.H {
				t.Errorf("rows of %v and %v tall, a short one %v: not stopped at two lines", a.H, b.H, one.H)
			}
			if a.W != b.W || a.W != one.W {
				t.Errorf("row widths %v, %v and %v differ", a.W, b.W, one.W)
			}
			if a.H > one.H+30 {
				t.Errorf("a row of %v is more than a line taller than a short one of %v", a.H, one.H)
			}
		})
	}
}

// A subscription's name shows in full in a tooltip once the pointer rests on its item.
func TestSourceTooltip(t *testing.T) {
	sources, entries := sample()
	name := "Ars Technica » Technology Lab: Tech Policy, Gadgets, Science and Culture, Plus Long Reads and Reviews"
	sources[1].Title = name
	f := &fake{now: now, sources: sources, entries: entries, patches: map[int64][]client.EntryPatch{}}
	s := New("en", f.deps())
	tt := ui.NewTester(s.View, 1100, 800)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	count := func() int {
		n := 0
		for _, text := range tt.Texts() {
			if text == name {
				n++
			}
		}
		return n
	}
	item, ok := tt.Find(name)
	if !ok {
		t.Fatalf("no pane item in %v", tt.Texts())
	}
	before := count()
	tt.Move(item.X+item.W/2, item.Y+item.H/2)
	tt.Frame()
	tt.Advance(2 * time.Second)
	tt.Frame()
	if count() <= before {
		t.Fatalf("no tooltip with the full name in %v", tt.Texts())
	}
}

// A picture that fails to load keeps its box on the Track color with a muted image icon; a video cover keeps its play
// mark and its duration.
func TestBrokenImages(t *testing.T) {
	sources, entries := sample()
	f := &fake{now: now, sources: sources, entries: entries, patches: map[int64][]client.EntryPatch{},
		imageErr: errors.New("broken")}
	s := New("en", f.deps())
	var main *ui.Theme
	tt := ui.NewTester(func(c *ui.Context) { theme.Apply(c, prefs.Default()); main = c.Theme(); s.View(c) }, 1100, 800)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	if p := s.pictures["https://go.dev/t.png"]; p == nil || !p.failed || p.bitmap != nil {
		t.Fatalf("the thumbnail did not fail: %+v", p)
	}
	track, muted := theme.Track(main), main.TextMuted
	// Within the row of the first entry the thumbnail box is on Track, and its middle holds the icon in the muted
	// color.
	row := rowRect(t, tt, "Go Blog, Go 1.27 is released, 5 min, Unread")
	img := tt.Image()
	minX, maxX, minY, maxY := float32(9999), float32(0), float32(9999), float32(0)
	for y := row.Y + 20; y < row.Y+row.H; y++ {
		for x := row.X + row.W - 240; x < row.X+row.W; x++ {
			if near(pixel(img, x, y), track, 2) {
				minX, maxX, minY, maxY = min(minX, x), max(maxX, x), min(minY, y), max(maxY, y)
			}
		}
	}
	// The box is 96 by 64 at the default text size and scales with it.
	if w, h, bw, bh := maxX-minX, maxY-minY, px(main, 96), px(main, 64); w < bw-6 || w > bw+2 || h < bh-6 || h > bh+2 {
		t.Fatalf("the failed thumbnail box is %v by %v, not %v by %v", w, h, bw, bh)
	}
	icon := 0
	for y := minY + 16; y < maxY-16; y++ {
		for x := minX + 28; x < maxX-28; x++ {
			if near(pixel(img, x, y), muted, 16) {
				icon++
			}
		}
	}
	if icon == 0 {
		t.Error("no muted image icon in the failed thumbnail")
	}
	if _, ok := tt.Find("Duration 12:34"); !ok || !tt.HasText("12:34") {
		t.Error("a failed video cover lost its duration")
	}
}

// twin is a timeline of two subscriptions, Alpha and Beta, with 80 entries each, alternating, newest first.
func twin(t *testing.T) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	f := &fake{now: now, patches: map[int64][]client.EntryPatch{}, sources: []client.Source{
		{ID: 1, Title: "Alpha", Kind: "article"}, {ID: 2, Title: "Beta", Kind: "article"}}}
	for i := range 160 {
		name := []string{"Alpha", "Beta"}[i%2]
		f.entries = append(f.entries, client.Entry{ID: int64(1000 - i), SourceID: int64(1 + i%2), SourceTitle: name,
			Kind: "article", Title: fmt.Sprintf("%s post %d", name, i/2), CreatedAt: ago(time.Duration(i) * time.Minute)})
	}
	s := New("en", f.deps())
	tt := ui.NewTester(func(c *ui.Context) { s.View(c); theme.Toasts(c) }, 1100, 800)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	return s, f, tt
}

// firstShown is the ID of the entry at the top of the list.
func firstShown(s *Screen) int64 {
	first, _ := s.list.Visible()
	return s.entries[first].ID
}

// keptNear says the list shows from the entry that was first in view, or from the one above it: the entry under
// the toolbar may show only in part, and the list is put back by whole entries.
func keptNear(s *Screen, shown int64) bool {
	at := s.index(shown)
	return at >= 0 && (firstShown(s) == shown || at > 0 && firstShown(s) == s.entries[at-1].ID)
}

// visit chooses the subscription by a click and loads what it needs.
func visit(t *testing.T, f *fake, tt *ui.Tester, name string) {
	t.Helper()
	if err := tt.Click(name); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	tt.Frame()
}

// leaveAlpha reads Alpha down to its second page, chooses an entry, and goes to Beta.
func leaveAlpha(t *testing.T, s *Screen, f *fake, tt *ui.Tester) (shown, chosen int64) {
	t.Helper()
	visit(t, f, tt, "Alpha")
	s.loadMore()
	f.settle(t, tt)
	s.list.ScrollTo(30, ui.Start)
	tt.Frame()
	s.selected = 33
	tt.Frame()
	shown, chosen = firstShown(s), s.entries[33].ID
	if len(s.entries) != 80 || shown == s.entries[0].ID {
		t.Fatalf("%d entries, the list shows from %d", len(s.entries), shown)
	}
	visit(t, f, tt, "Beta")
	if len(s.entries) == 0 || s.entries[0].SourceID != 2 {
		t.Fatalf("Beta shows %d entries, source %d, scope %v, queries %v", len(s.entries), s.entries[0].SourceID, s.scope, f.queries)
	}
	return shown, chosen
}

func TestSwitchBackKeepsPlace(t *testing.T) {
	s, f, tt := twin(t)
	shown, chosen := leaveAlpha(t, s, f, tt)
	before := len(f.queries)
	if err := tt.Click("Alpha"); err != nil {
		t.Fatal(err)
	}
	// Alpha is drawn at once, as it was left, while the first page is fetched again.
	if len(s.entries) != 80 || s.loading || s.entries[0].SourceID != 1 || len(f.queue) != 1 {
		t.Fatalf("%d entries, loading %v, %d queued", len(s.entries), s.loading, len(f.queue))
	}
	if s.selected < 0 || s.entries[s.selected].ID != chosen || !keptNear(s, shown) {
		t.Fatalf("selected %d, the list shows from %d, want %d and %d", s.selected, firstShown(s), chosen, shown)
	}
	drawn := firstShown(s)
	f.settle(t, tt)
	if q := f.queries[before]; q.Source != 1 || q.Cursor != "" || len(f.queries) != before+1 {
		t.Fatalf("queries %v", f.queries[before:])
	}
	if len(s.entries) != 80 || s.entries[s.selected].ID != chosen || firstShown(s) != drawn {
		t.Fatalf("after the check: %d entries, selected %d, the list shows from %d", len(s.entries), s.selected, firstShown(s))
	}
}

func TestCheckAddsNewEntryWithoutAJump(t *testing.T) {
	s, f, tt := twin(t)
	_, chosen := leaveAlpha(t, s, f, tt)
	if err := tt.Click("Alpha"); err != nil {
		t.Fatal(err)
	}
	drawn := firstShown(s)
	f.entries = append([]client.Entry{{ID: 2000, SourceID: 1, SourceTitle: "Alpha", Kind: "article",
		Title: "Alpha fresh", CreatedAt: now.UnixMilli()}}, f.entries...)
	read := true
	f.entries[1].IsRead = read
	f.settle(t, tt)
	if len(s.entries) != 81 || s.entries[0].ID != 2000 || !s.entries[1].IsRead {
		t.Fatalf("%d entries, the top is %d", len(s.entries), s.entries[0].ID)
	}
	if s.entries[s.selected].ID != chosen || firstShown(s) != drawn || s.next != "" {
		t.Fatalf("selected %d, the list shows from %d, next %q", s.entries[s.selected].ID, firstShown(s), s.next)
	}
}

func TestCheckDropsWhatTheServerLost(t *testing.T) {
	s, f, tt := twin(t)
	_, chosen := leaveAlpha(t, s, f, tt)
	f.entries = slices.DeleteFunc(f.entries, func(e client.Entry) bool { return e.ID == chosen || e.ID == 1000 })
	visit(t, f, tt, "Alpha")
	if s.selected != -1 || len(s.entries) != 78 || s.index(1000) >= 0 {
		t.Fatalf("selected %d, %d entries", s.selected, len(s.entries))
	}
}

func TestStaleCheckIgnored(t *testing.T) {
	s, f, tt := twin(t)
	leaveAlpha(t, s, f, tt)
	if err := tt.Click("Alpha"); err != nil {
		t.Fatal(err)
	}
	// Beta is chosen again before the check of Alpha comes back.
	if err := tt.Click("Beta"); err != nil {
		t.Fatal(err)
	}
	f.entries = append([]client.Entry{{ID: 2000, SourceID: 1, SourceTitle: "Alpha", Kind: "article",
		Title: "Alpha fresh", CreatedAt: now.UnixMilli()}}, f.entries...)
	f.settle(t, tt)
	if s.scope.source != 2 || len(s.entries) == 0 || s.entries[0].SourceID != 2 || s.checking {
		t.Fatalf("scope %v, %d entries, the top is of source %d", s.scope, len(s.entries), s.entries[0].SourceID)
	}
	// Alpha was left as the stale check found it; its own check puts the new entry in.
	visit(t, f, tt, "Alpha")
	if s.entries[0].ID != 2000 {
		t.Fatalf("the top is %d", s.entries[0].ID)
	}
}

func TestFilterKeepsList(t *testing.T) {
	s, f, tt := setup(t, "en")
	for _, name := range []string{"Unread", "Everything"} {
		if err := tt.Click(name); err != nil {
			t.Fatal(err)
		}
		f.settle(t, tt)
	}
	s.selected = 2
	if err := tt.Click("Unread"); err != nil {
		t.Fatal(err)
	}
	if len(s.entries) != 5 || s.loading {
		t.Fatalf("%d entries", len(s.entries))
	}
	f.settle(t, tt)
	if err := tt.Click("Everything"); err != nil {
		t.Fatal(err)
	}
	if len(s.entries) != 6 || s.selected != 2 {
		t.Fatalf("%d entries, selected %d", len(s.entries), s.selected)
	}
}

func TestMarkAllReadDropsKeptUnread(t *testing.T) {
	s, f, tt := setup(t, "en")
	for _, name := range []string{"Unread", "Everything"} {
		if err := tt.Click(name); err != nil {
			t.Fatal(err)
		}
		f.settle(t, tt)
	}
	if err := tt.Click("Mark all in “All” as read"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if err := tt.Click("Unread"); err != nil {
		t.Fatal(err)
	}
	if len(s.entries) != 0 || s.loading {
		t.Fatalf("the list kept by Unread still has %d entries", len(s.entries))
	}
	f.settle(t, tt)
	if len(s.entries) != 0 || !tt.HasText("All caught up") {
		t.Fatalf("%d entries, texts %v", len(s.entries), tt.Texts())
	}
}

func TestKeptListsAreBounded(t *testing.T) {
	f := &fake{now: now, patches: map[int64][]client.EntryPatch{}}
	for id := int64(1); id <= 10; id++ {
		f.sources = append(f.sources, client.Source{ID: id, Title: fmt.Sprintf("Source %d", id), Kind: "article"})
		f.entries = append(f.entries, client.Entry{ID: id, SourceID: id, Kind: "article", Title: fmt.Sprintf("Entry %d", id),
			CreatedAt: ago(time.Duration(id) * time.Minute)})
	}
	s := New("en", f.deps())
	tt := ui.NewTester(s.View, 1100, 800)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	for id := int64(1); id <= 10; id++ {
		s.choose(scope{kind: "article", source: id})
		f.settle(t, tt)
	}
	// Source 10 is on screen; of the nine left, the least recently seen is gone.
	var kept []int64
	for _, l := range s.lists {
		kept = append(kept, l.key.scope.source)
	}
	if !slices.Equal(kept, []int64{2, 3, 4, 5, 6, 7, 8, 9}) {
		t.Fatalf("kept %v", kept)
	}
	s.choose(scope{kind: "article", source: 1})
	if len(s.entries) != 0 || !s.loading {
		t.Fatalf("the list evicted shows %d entries", len(s.entries))
	}
	f.settle(t, tt)
	s.choose(scope{kind: "article", source: 9})
	if len(s.entries) != 1 || s.loading || len(s.lists) != keepLists-1 {
		t.Fatalf("%d entries, %d kept", len(s.entries), len(s.lists))
	}
}

// TestSkeletonWaits shows a switch that is answered within the wait never draws the skeleton, and a slow one does.
func TestSkeletonWaits(t *testing.T) {
	s, f, tt := setup(t, "en")
	s.Use(f, "")
	tt.Frame()
	if !s.loading {
		t.Fatal("not loading")
	}
	if _, ok := tt.Find("Loading"); ok {
		t.Fatal("the skeleton flashes at once")
	}
	f.now = f.now.Add(skeletonDelay / 2)
	tt.Frame()
	if _, ok := tt.Find("Loading"); ok {
		t.Fatal("the skeleton is drawn before the wait is over")
	}
	f.now = f.now.Add(skeletonDelay / 2)
	tt.Frame()
	if _, ok := tt.Find("Loading"); !ok {
		t.Fatal("a slow load shows no skeleton")
	}
	f.settle(t, tt)
	for _, name := range []string{"Unread", "Starred", "All"} {
		if err := tt.Click(name); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if _, ok := tt.Find("Loading"); ok {
			t.Fatalf("%s flashes the skeleton", name)
		}
		f.settle(t, tt)
	}
}

func TestUseClearsKeptLists(t *testing.T) {
	s, f, tt := twin(t)
	visit(t, f, tt, "Alpha")
	visit(t, f, tt, "Beta")
	if len(s.lists) == 0 {
		t.Fatal("nothing kept")
	}
	s.Use(f, "")
	if len(s.lists) != 0 {
		t.Fatalf("%d lists kept across Use", len(s.lists))
	}
	f.settle(t, tt)
	tt.Frame()
	if err := tt.Click("Alpha"); err != nil {
		t.Fatal(err)
	}
	if len(s.entries) != 0 || !s.loading {
		t.Fatalf("Alpha shows %d entries from before Use", len(s.entries))
	}
}

// readingSample gives some entries of the sample a reading time: 8 takes 6 minutes, 4 takes 20 and 3 half a minute.
func readingSample(f *fake) {
	f.entries[0].ReadingSec = 360
	f.entries[4].ReadingSec = 1200
	f.entries[5].ReadingSec = 30
}

func TestReadingTimeBudget(t *testing.T) {
	s, f, tt := setup(t, "en")
	readingSample(f)
	s.reload()
	f.settle(t, tt)
	if !tt.HasText("6 min read") || !tt.HasText("20 min read") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if !tt.HasText("5 unread") {
		t.Fatalf("no unread count: %v", tt.Texts())
	}
	if err := tt.Click("Reading time: any length"); err != nil {
		t.Fatal(err)
	}
	menu := tt.Menu()
	want := []string{"Any length", "Up to 5 minutes", "Up to 10 minutes", "Up to 15 minutes", "Up to 30 minutes", "-", "Newest first", "Oldest first"}
	if !slices.Equal(menu, want) {
		t.Fatalf("menu %q", menu)
	}
	if err := tt.ChooseMenuItem("Up to 10 minutes"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if q := f.queries[len(f.queries)-1]; q.MaxMinutes != 10 {
		t.Fatalf("query %+v", q)
	}
	if len(s.entries) != 2 || s.entries[0].ID != 8 || s.entries[1].ID != 3 {
		t.Fatalf("entries %v", s.entries)
	}
	if _, ok := tt.Find("Reading time: up to 10 minutes"); !ok || !tt.HasText("≤ 10 min") {
		t.Fatalf("no pill: %v", tt.Texts())
	}
	if tt.HasText("5 unread") {
		t.Fatalf("the unread count shows with a budget: %v", tt.Texts())
	}
	if s.canMarkAll() || s.canMarkAbove(3) {
		t.Fatal("marking is offered with a budget")
	}
	s.markVisible()
	if len(f.marked) != 0 {
		t.Fatalf("marked %v", f.marked)
	}
	// The budget stays across the filters and the scopes.
	if err := tt.Click("Unread"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if q := f.queries[len(f.queries)-1]; q.MaxMinutes != 10 || !q.Unread {
		t.Fatalf("query %+v", q)
	}
	// Chosen again, "Any length" clears it.
	if err := tt.Click("Reading time: up to 10 minutes"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Any length"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if s.budget != 0 || f.queries[len(f.queries)-1].MaxMinutes != 0 {
		t.Fatalf("budget %d", s.budget)
	}
}

func TestReadingTimeEmpty(t *testing.T) {
	s, f, tt := setup(t, "en")
	s.show(func() { s.budget = 5 })
	f.settle(t, tt)
	for _, text := range []string{"Nothing fits in 5 minutes", "Entries with no estimate, like pictures, are left out."} {
		if !tt.HasText(text) {
			t.Fatalf("no %q: %v", text, tt.Texts())
		}
	}
	if err := tt.Click("Show any length"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if s.budget != 0 || len(s.entries) != 6 {
		t.Fatalf("budget %d, %d entries", s.budget, len(s.entries))
	}
}

func TestReadingTimeLabel(t *testing.T) {
	s, f, _ := setup(t, "en")
	readingSample(f)
	article, short := f.entries[0], f.entries[5]
	if got := s.readLabel(article); got != "6 min read" {
		t.Fatalf("label %q", got)
	}
	if got := s.label(article, now); !strings.Contains(got, "6 min read, Unread") {
		t.Fatalf("accessibility label %q", got)
	}
	if got := s.readLabel(short); got != "" {
		t.Fatalf("a half minute reads %q", got)
	}
	if got := s.label(short, now); strings.Contains(got, "min read") {
		t.Fatalf("accessibility label %q", got)
	}
	podcast := article
	podcast.DurationSec = 3000
	video := article
	video.Kind = "video"
	if s.readLabel(podcast) != "" || s.readLabel(video) != "" {
		t.Fatal("an entry with a duration has a reading time")
	}
	s.text = translations["zh-Hans"]
	if got := s.readLabel(article); got != "6 分钟阅读" {
		t.Fatalf("label %q", got)
	}
}

func TestReadingTimeReset(t *testing.T) {
	s, f, tt := setup(t, "en")
	s.show(func() { s.budget = 15 })
	f.settle(t, tt)
	s.Use(f, "other")
	if s.budget != 0 {
		t.Fatalf("budget %d", s.budget)
	}
}

// viewing is the screen with the viewers of p, which the test may change while the screen is up.
func viewing(t *testing.T, p *prefs.Preferences) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	sources, entries := sample()
	entries[3].Images = append(entries[3].Images, client.Image{URL: "https://photos.example/5b.jpg"},
		client.Image{URL: "https://photos.example/5c.jpg"})
	f := &fake{now: now, sources: sources, entries: entries, patches: map[int64][]client.EntryPatch{}}
	d := f.deps()
	d.Reading = func() prefs.Preferences { return *p }
	d.Change = func(n prefs.Preferences) { *p = n }
	s := New("en", d)
	tt := ui.NewTester(func(c *ui.Context) { s.View(c); theme.Toasts(c) }, 1100, 800)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	tt.Frame()
	return s, f, tt
}

func pick(t *testing.T, f *fake, tt *ui.Tester, kind string) {
	t.Helper()
	if err := tt.Click(kind); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	f.settle(t, tt)
	tt.Frame()
}

// The defaults draw what the timeline drew before the viewers: rows, the Videos grid and the Images wall.
func TestViewersDefault(t *testing.T) {
	p := prefs.Default()
	s, f, tt := viewing(t, &p)
	if s.layout() != columnLayout {
		t.Fatalf("the mixed timeline is laid out as %v", s.layout())
	}
	pick(t, f, tt, "Videos")
	if s.layout() != gridLayout {
		t.Fatalf("Videos is laid out as %v", s.layout())
	}
	pick(t, f, tt, "Images")
	if s.layout() != wallLayout {
		t.Fatalf("Images is laid out as %v", s.layout())
	}
}

// A compact article is one line: the source, the title and the time, in a row lower than the default one.
func TestViewerCompactArticle(t *testing.T) {
	p := prefs.Default()
	_, _, tt := viewing(t, &p)
	label := "Go Blog, Go 1.27 is released, 5 min, Unread"
	before := rowRect(t, tt, label)
	p.ArticleViewer = prefs.ViewCompact
	tt.Frame()
	tt.Frame()
	after := rowRect(t, tt, label)
	if after.H >= before.H {
		t.Fatalf("the compact row is %v high, the default one %v", after.H, before.H)
	}
	if tt.HasText("The latest Go release.") {
		t.Fatal("the summary shows in a compact row")
	}
}

// A compact social post has no pictures of its own in the row beyond a small strip, and is lower than a card.
func TestViewerCompactSocial(t *testing.T) {
	p := prefs.Default()
	_, _, tt := viewing(t, &p)
	label := "Gopher, Five pictures from the meetup, Yesterday, Unread"
	before := rowRect(t, tt, label)
	p.SocialViewer = prefs.ViewCompact
	tt.Frame()
	tt.Frame()
	after := rowRect(t, tt, label)
	if after.H >= before.H {
		t.Fatalf("the compact post is %v high, the card %v", after.H, before.H)
	}
}

// A large cover video shows its cover across the row, in the mixed timeline as well, and is higher than the row.
func TestViewerLargeCover(t *testing.T) {
	p := prefs.Default()
	_, _, tt := viewing(t, &p)
	label := "Talks, Profiling in practice, 2 h, Unread"
	before := rowRect(t, tt, label)
	p.VideoViewer = prefs.ViewCover
	tt.Frame()
	tt.Frame()
	after := rowRect(t, tt, label)
	if after.H <= before.H {
		t.Fatalf("the cover row is %v high, the default one %v", after.H, before.H)
	}
}

// With the list or the cover as the video viewer the Videos scope is a column, not a grid.
func TestViewerVideoListNoGrid(t *testing.T) {
	for _, v := range []string{prefs.ViewList, prefs.ViewCover} {
		p := prefs.Default()
		p.VideoViewer = v
		s, f, tt := viewing(t, &p)
		pick(t, f, tt, "Videos")
		if s.layout() != columnLayout || s.grids() {
			t.Fatalf("%s: Videos is laid out as %v", v, s.layout())
		}
		rowRect(t, tt, "Talks, Profiling in practice, 2 h, Unread")
	}
}

// The square grid shows the Images scope as squares, with the count of pictures in the name of a cell.
func TestViewerImageGrid(t *testing.T) {
	p := prefs.Default()
	p.ImageViewer = prefs.ViewGrid
	s, f, tt := viewing(t, &p)
	pick(t, f, tt, "Images")
	if s.layout() != squareLayout || !s.grids() {
		t.Fatalf("Images is laid out as %v", s.layout())
	}
	r := rowRect(t, tt, "Harbor at dawn, 3 pictures")
	if r.W < 100 || r.H < r.W-1 || r.H > r.W+1 {
		t.Fatalf("the cell is %vx%v", r.W, r.H)
	}
	// The mixed timeline keeps cards.
	pick(t, f, tt, "All")
	if s.layout() != columnLayout {
		t.Fatalf("the mixed timeline is laid out as %v", s.layout())
	}
}

// The card viewer shows the Images scope as cards.
func TestViewerImageCard(t *testing.T) {
	p := prefs.Default()
	p.ImageViewer = prefs.ViewCard
	s, f, tt := viewing(t, &p)
	pick(t, f, tt, "Images")
	if s.layout() != columnLayout {
		t.Fatalf("Images is laid out as %v", s.layout())
	}
	rowRect(t, tt, "Photos, Harbor at dawn, 3 days, Unread")
}

// The keys move through the square grid one picture at a time, and the picture chosen is the one with the focus.
func TestViewerImageGridKeys(t *testing.T) {
	p := prefs.Default()
	p.ImageViewer = prefs.ViewGrid
	s, f, tt := viewing(t, &p)
	for i := 0; i < 11; i++ {
		f.entries = append(f.entries, client.Entry{ID: int64(20 + i), SourceID: 4, SourceTitle: "Photos", Kind: "image",
			Title: fmt.Sprintf("Shot %d", i), URL: fmt.Sprintf("https://photos.example/%d", i),
			Images:    []client.Image{{URL: fmt.Sprintf("https://photos.example/%d.jpg", i)}},
			CreatedAt: ago(time.Duration(i+1) * time.Hour)})
	}
	pick(t, f, tt, "Images")
	if len(s.entries) < 8 {
		t.Fatalf("%d pictures", len(s.entries))
	}
	tt.Key(0, ui.KeyJ)
	tt.Frame()
	first := s.selected
	tt.Key(0, ui.KeyJ)
	tt.Frame()
	if s.selected != first+1 {
		t.Fatalf("selected %d after %d", s.selected, first)
	}
	tt.Key(0, ui.KeyK)
	tt.Frame()
	if s.selected != first {
		t.Fatalf("selected %d, want %d", s.selected, first)
	}
}

// A change of viewer redraws the list in place: the scope and the entry chosen stay.
func TestViewerLive(t *testing.T) {
	p := prefs.Default()
	s, f, tt := viewing(t, &p)
	pick(t, f, tt, "Videos")
	tt.Key(0, ui.KeyJ)
	tt.Frame()
	id := s.entries[s.selected].ID
	p.VideoViewer = prefs.ViewList
	tt.Frame()
	tt.Frame()
	if s.scope.kind != "video" || s.layout() != columnLayout {
		t.Fatalf("scope %v, layout %v", s.scope, s.layout())
	}
	if s.selected < 0 || s.entries[s.selected].ID != id {
		t.Fatalf("selected %d, want entry %d", s.selected, id)
	}
}

// chooseSource opens one subscription as the sidebar does.
func chooseSource(t *testing.T, s *Screen, f *fake, tt *ui.Tester, kind string, id int64) {
	t.Helper()
	s.choose(scope{kind: kind, source: id})
	tt.Frame()
	f.settle(t, tt)
	f.settle(t, tt)
	tt.Frame()
}

func clickSwitch(t *testing.T, f *fake, tt *ui.Tester, name string) {
	t.Helper()
	if err := tt.Click(name); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	f.settle(t, tt)
	tt.Frame()
}

// The switch is in the toolbar where a grid is possible: the Videos and Images scopes and one subscription of those
// kinds, and not in the mixed timeline, the other kinds, their subscriptions or a search.
func TestGridSwitchOffered(t *testing.T) {
	p := prefs.Default()
	s, f, tt := viewing(t, &p)
	offered := func() bool {
		_, grid := tt.Find("Show as grid")
		_, list := tt.Find("Show as list")
		return grid || list
	}
	if offered() {
		t.Fatal("offered in the mixed timeline")
	}
	pick(t, f, tt, "Articles")
	if offered() {
		t.Fatal("offered in Articles")
	}
	pick(t, f, tt, "Videos")
	if _, ok := tt.Find("Show as list"); !ok {
		t.Fatal("Videos shows the grid by default and offers the list")
	}
	chooseSource(t, s, f, tt, "video", 2)
	if _, ok := tt.Find("Show as grid"); !ok {
		t.Fatal("a video subscription shows the list by default and offers the grid")
	}
	chooseSource(t, s, f, tt, "social", 3)
	if offered() {
		t.Fatal("offered in a social subscription")
	}
	chooseSource(t, s, f, tt, "article", 1)
	if offered() {
		t.Fatal("offered in an article subscription")
	}
	pick(t, f, tt, "Images")
	if !offered() {
		t.Fatal("not offered in Images")
	}
	s.query = "harbor"
	s.search()
	f.settle(t, tt)
	tt.Frame()
	if s.searched == "" || offered() {
		t.Fatalf("searched %q, offered %v", s.searched, offered())
	}
}

// The switch turns the Images scope from the wall to the list of cards and back, and keeps the choice in the
// preferences; a subscription's list becomes the wall, or the square grid when that is the Images viewer.
func TestGridSwitchImages(t *testing.T) {
	p := prefs.Default()
	s, f, tt := viewing(t, &p)
	pick(t, f, tt, "Images")
	if s.layout() != wallLayout {
		t.Fatalf("layout %v", s.layout())
	}
	clickSwitch(t, f, tt, "Show as list")
	if s.layout() != columnLayout || p.Grids.Get("kind:image") != prefs.ViewList {
		t.Fatalf("layout %v, grids %q", s.layout(), p.Grids)
	}
	rowRect(t, tt, "Photos, Harbor at dawn, 3 days, Unread")
	clickSwitch(t, f, tt, "Show as grid")
	if s.layout() != wallLayout || p.Grids != "" {
		t.Fatalf("layout %v, grids %q: a switch back to the default is not kept", s.layout(), p.Grids)
	}

	chooseSource(t, s, f, tt, "image", 4)
	if s.layout() != columnLayout {
		t.Fatalf("a subscription starts as a list: %v", s.layout())
	}
	clickSwitch(t, f, tt, "Show as grid")
	if s.layout() != wallLayout || p.Grids.Get("source:4") != prefs.ViewGrid {
		t.Fatalf("layout %v, grids %q", s.layout(), p.Grids)
	}
	p.ImageViewer = prefs.ViewGrid
	tt.Frame()
	if s.layout() != squareLayout {
		t.Fatalf("with the Grid viewer: %v", s.layout())
	}
	rowRect(t, tt, "Harbor at dawn, 3 pictures")
	clickSwitch(t, f, tt, "Show as list")
	if s.layout() != columnLayout || p.Grids != "" {
		t.Fatalf("layout %v, grids %q", s.layout(), p.Grids)
	}
}

// A video subscription as a grid is the cover grid, whatever the Videos viewer is; and the choice for it does not
// touch the Videos scope, and is there again when the scope is chosen again.
func TestGridSwitchRememberedPerScope(t *testing.T) {
	p := prefs.Default()
	p.VideoViewer = prefs.ViewCover
	s, f, tt := viewing(t, &p)
	pick(t, f, tt, "Videos")
	if s.layout() != columnLayout {
		t.Fatalf("Videos with the cover viewer: %v", s.layout())
	}
	clickSwitch(t, f, tt, "Show as grid")
	if s.layout() != gridLayout || p.Grids.Get("kind:video") != prefs.ViewGrid {
		t.Fatalf("layout %v, grids %q", s.layout(), p.Grids)
	}
	chooseSource(t, s, f, tt, "video", 2)
	if s.layout() != columnLayout {
		t.Fatalf("the subscription follows its own choice: %v", s.layout())
	}
	clickSwitch(t, f, tt, "Show as grid")
	if s.layout() != gridLayout {
		t.Fatalf("subscription %v", s.layout())
	}
	pick(t, f, tt, "Images")
	if s.layout() != wallLayout {
		t.Fatalf("Images %v", s.layout())
	}
	pick(t, f, tt, "Videos")
	if s.layout() != gridLayout {
		t.Fatalf("Videos %v", s.layout())
	}
	chooseSource(t, s, f, tt, "video", 2)
	if s.layout() != gridLayout {
		t.Fatalf("the subscription again %v", s.layout())
	}
	if p.Grids != "kind:video=grid,source:2=grid" {
		t.Fatalf("grids %q", p.Grids)
	}
	// The mixed timeline and a search are columns whatever was chosen.
	pick(t, f, tt, "All")
	if s.layout() != columnLayout {
		t.Fatalf("All %v", s.layout())
	}
}

// Switching keeps the entry chosen and the scope.
func TestGridSwitchKeepsSelection(t *testing.T) {
	p := prefs.Default()
	s, f, tt := viewing(t, &p)
	pick(t, f, tt, "Videos")
	tt.Key(0, ui.KeyJ)
	tt.Frame()
	id := s.entries[s.selected].ID
	clickSwitch(t, f, tt, "Show as list")
	if s.scope.kind != "video" || s.selected < 0 || s.entries[s.selected].ID != id {
		t.Fatalf("scope %v, selected %d, want entry %d", s.scope, s.selected, id)
	}
}

// In a subscription shown as a grid the keys move one picture at a time, V marks the one chosen, and the rest of
// the grid works as in the Images scope.
func TestGridSwitchSourceKeys(t *testing.T) {
	p := prefs.Default()
	p.ImageViewer = prefs.ViewGrid
	s, f, tt := viewing(t, &p)
	for i := 0; i < 11; i++ {
		f.entries = append(f.entries, client.Entry{ID: int64(20 + i), SourceID: 4, SourceTitle: "Photos", Kind: "image",
			Title: fmt.Sprintf("Shot %d", i), URL: fmt.Sprintf("https://photos.example/%d", i),
			Images:    []client.Image{{URL: fmt.Sprintf("https://photos.example/%d.jpg", i)}},
			CreatedAt: ago(time.Duration(i+1) * time.Hour)})
	}
	chooseSource(t, s, f, tt, "image", 4)
	clickSwitch(t, f, tt, "Show as grid")
	if s.layout() != squareLayout || len(s.entries) < 8 {
		t.Fatalf("layout %v, %d pictures", s.layout(), len(s.entries))
	}
	tt.Key(0, ui.KeyJ)
	tt.Frame()
	first := s.selected
	tt.Key(0, ui.KeyJ)
	tt.Frame()
	if s.selected != first+1 {
		t.Fatalf("selected %d after %d", s.selected, first)
	}
	tt.Key(0, ui.KeyK)
	tt.Frame()
	if s.selected != first {
		t.Fatalf("selected %d, want %d", s.selected, first)
	}
	tt.Key(0, ui.KeyV)
	f.settle(t, tt)
	tt.Frame()
	if len(f.marked) != 1 || f.marked[0].SourceID != 4 || f.marked[0].UpTo != s.entries[0].ID {
		t.Fatalf("marked %v", f.marked)
	}
}

// At the minimum width the toolbar keeps every button inside the window, the mark-all button above all, even in a
// subscription that adds the edit and grid buttons. It runs at 14, the text size of Linux and Windows, the widest.
func TestToolbarMinimumWidth(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		p := prefs.Default()
		p.SidebarHidden = hidden
		s, f, _ := viewing(t, &p)
		tt := ui.NewTester(func(c *ui.Context) { c.Theme().FontSize = 14; s.View(c); theme.Toasts(c) }, 640, 540)
		tt.Frame()
		f.settle(t, tt)
		tt.Frame()
		if !hidden {
			chooseSource(t, s, f, tt, "image", 4)
		} else {
			tt.Key(0, ui.Key5)
			f.settle(t, tt)
			tt.Frame()
		}
		for _, name := range []string{"Mark all in “Photos” as read", "Mark all in “Images” as read", "Show as grid", "Show as list", "Edit subscription",
			"Hide sidebar", "Show sidebar", "Search"} {
			if r, ok := tt.Find(name); ok && (r.X+r.W > 640 || r.W < 32) {
				t.Fatalf("hidden %v: %q at %v is cut off by the 640 wide window", hidden, name, r)
			}
		}
		if _, ok := tt.Find("Mark all in “Photos” as read"); !ok && !hidden {
			t.Fatal("no mark-all button")
		}
	}
}

// The sidebar button and Cmd+\ hide the sidebar and show it again; the choice goes through Deps.Change.
func TestSidebarToggle(t *testing.T) {
	p := prefs.Default()
	s, f, tt := viewing(t, &p)
	if _, ok := tt.Find("Hide sidebar"); !ok {
		t.Fatal("no Hide sidebar button")
	}
	if _, ok := tt.Find("Kinds"); !ok {
		t.Fatal("the sidebar is not shown by default")
	}
	if err := tt.Click("Hide sidebar"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !p.SidebarHidden {
		t.Fatal("the choice was not passed to Change")
	}
	if _, ok := tt.Find("Kinds"); ok {
		t.Fatal("the sidebar is still shown")
	}
	bar, ok := tt.Find("Show sidebar")
	if !ok || bar.W != bar.H {
		t.Fatalf("Show sidebar button %v, %v", bar, ok)
	}
	if err := tt.Click("Show sidebar"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if p.SidebarHidden {
		t.Fatal("showing the sidebar was not passed to Change")
	}
	if _, ok := tt.Find("Kinds"); !ok {
		t.Fatal("the sidebar is not back")
	}

	tt.Key(ui.Cmd, ui.KeyBackslash)
	tt.Frame()
	if !p.SidebarHidden {
		t.Fatal("the shortcut does not hide the sidebar")
	}
	// Kinds stay reachable by number with the pane hidden.
	tt.Key(0, ui.Key3)
	f.settle(t, tt)
	if s.scope.kind != "video" {
		t.Fatalf("scope %v", s.scope)
	}
	// H expands the sidebar and focuses it.
	tt.Key(0, ui.KeyH)
	tt.Frame()
	if p.SidebarHidden || !s.inSide {
		t.Fatalf("H: hidden %v, in sidebar %v", p.SidebarHidden, s.inSide)
	}
	tt.Key(ui.Cmd, ui.KeyBackslash)
	tt.Frame()
	if !p.SidebarHidden || s.inSide {
		t.Fatalf("hiding keeps the sidebar focus: hidden %v, in sidebar %v", p.SidebarHidden, s.inSide)
	}
}

// With the sidebar hidden the window buttons sit over the toolbar, so the toolbar's left starts after them.
func TestSidebarHiddenInset(t *testing.T) {
	p := prefs.Default()
	p.SidebarHidden = true
	_, _, tt := viewing(t, &p)
	tt.SetTitleBar(ui.TitleBar{Height: 52, Left: 78})
	hidden, ok := tt.Find("Show sidebar")
	if !ok {
		t.Fatal("no Show sidebar button")
	}
	if hidden.X < 78 {
		t.Fatalf("the button starts at %v, under the window buttons", hidden.X)
	}
	p.SidebarHidden = false
	tt.Frame()
	// With the pane shown the button is in the pane's top row, at its right end, level with the window buttons.
	shown, ok := tt.Find("Hide sidebar")
	if !ok || shown.X+shown.W > 8+216*shown.W/32 || shown.X < 8+216*shown.W/32-60 || shown.Y+shown.H > 52 {
		t.Fatalf("with the pane shown the button is at %v, %v", shown, ok)
	}
	if mid := shown.Y + shown.H/2; mid < 25 || mid > 27 {
		t.Fatalf("the button is centered at %v, not in the 52 high row", mid)
	}
}

// The button stays on the same line when the pane is hidden and shown again, and hidden it starts after the window buttons.
func TestSidebarButtonPlace(t *testing.T) {
	p := prefs.Default()
	_, _, tt := viewing(t, &p)
	tt.SetSize(1100, 800)
	tt.SetTitleBar(ui.TitleBar{Height: 52, Left: 78})
	tt.Frame()
	shown, ok := tt.Find("Hide sidebar")
	if !ok {
		t.Fatal("no Hide sidebar button")
	}
	p.SidebarHidden = true
	tt.Frame()
	hidden, ok := tt.Find("Show sidebar")
	if !ok || hidden.Y-shown.Y > 1 || shown.Y-hidden.Y > 1 || hidden.W-shown.W > 0.01 || shown.W-hidden.W > 0.01 {
		t.Fatalf("shown at %v, hidden at %v, %v", shown, hidden, ok)
	}
	if hidden.X < 78 {
		t.Fatalf("hidden, the button at %v is under the window buttons", hidden)
	}
}

// The segments are in the middle of the main column, and the buttons after them are one group in the order reading
// time, grid, mark all as read, with search last at the far right.
func TestToolbarLayout(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		p := prefs.Default()
		p.SidebarHidden = hidden
		s, f, _ := viewing(t, &p)
		tt := ui.NewTester(func(c *ui.Context) { c.Theme().FontSize = 14; s.View(c); theme.Toasts(c) }, 1280, 800)
		tt.Frame()
		f.settle(t, tt)
		tt.Frame()
		if !hidden {
			chooseSource(t, s, f, tt, "image", 4)
		} else {
			tt.Key(0, ui.Key5)
			f.settle(t, tt)
			tt.Frame()
		}
		left := float32(0)
		if !hidden {
			grid, _ := tt.Find(s.markAllLabel())
			left = 8 + 216*grid.W/32
		}
		first, _ := tt.Find(s.text.Everything)
		last, _ := tt.Find(s.text.Starred)
		mid := (first.X + last.X + last.W) / 2
		if want := (left + 1280) / 2; mid < want-1.5 || mid > want+1.5 {
			t.Fatalf("hidden %v: the segments are centered on %v, the column on %v", hidden, mid, want)
		}
		var xs []float32
		for _, name := range []string{"Reading time: any length", "Show as grid", s.markAllLabel(), "Search entries"} {
			r, ok := tt.Find(name)
			if name == "Show as grid" && !ok {
				continue
			}
			if !ok {
				t.Fatalf("hidden %v: no %q", hidden, name)
			}
			if r.X < last.X+last.W {
				t.Fatalf("hidden %v: %q at %v is not after the segments", hidden, name, r)
			}
			xs = append(xs, r.X)
		}
		if !slices.IsSorted(xs) {
			t.Fatalf("hidden %v: buttons in the wrong order: %v", hidden, xs)
		}
		search, _ := tt.Find("Search entries")
		if search.X+search.W < 1280-20 || search.X+search.W > 1280 {
			t.Fatalf("hidden %v: search ends at %v, not at the far right", hidden, search.X+search.W)
		}
	}
}

// In a narrow main column search is an icon after the group of buttons. Clicking it opens the field, which takes the
// room of the other buttons and the focus; with no text it goes back to an icon when it loses the focus, with text
// it stays.
func TestSearchIconOpens(t *testing.T) {
	p := prefs.Default()
	s, f, _ := viewing(t, &p)
	tt := ui.NewTester(func(c *ui.Context) { c.Theme().FontSize = 14; s.View(c); theme.Toasts(c) }, 700, 700)
	tt.Frame()
	f.settle(t, tt)
	tt.Frame()
	if _, ok := tt.Find("Search entries"); ok {
		t.Fatal("the field shows in a narrow window")
	}
	icon, ok := tt.Find("Search")
	if !ok || icon.W < 32 || icon.X+icon.W > 700 {
		t.Fatalf("search icon %v, %v", icon, ok)
	}
	if clock, _ := tt.Find("Reading time: any length"); clock.X+clock.W > icon.X {
		t.Fatalf("the icon at %v is not after the buttons, the first at %v", icon, clock)
	}
	if err := tt.Click("Search"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if !tt.Focused("Search entries") {
		t.Fatal("the opened field does not have the focus")
	}
	if _, ok := tt.Find("Reading time: any length"); ok {
		t.Fatal("the other buttons still show beside the open field")
	}
	if r, _ := tt.Find("Search entries"); r.X+r.W > 700 {
		t.Fatalf("the field at %v is cut off", r)
	}
	tt.Type("go t")
	tt.Frame()
	if err := tt.Click(s.text.Everything); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("Search entries"); !ok {
		t.Fatal("the field closed with text in it")
	}
	if err := tt.Click(s.text.Clear); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click(s.text.Everything); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("Search entries"); ok {
		t.Fatal("the field stays open with no text and no focus")
	}
	tt.Key(0, ui.KeySlash)
	tt.Frame()
	tt.Frame()
	if !tt.Focused("Search entries") {
		t.Fatal("/ does not open the field")
	}
}

// crowded is the sample with more subscriptions, set up with the preferences p kept on the device.
func crowded(t *testing.T, language string, p *prefs.Preferences) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	sources, entries := sample()
	sources = append(sources,
		client.Source{ID: 5, Title: "Second Blog", Kind: "article", UnreadCount: 9},
		client.Source{ID: 6, Title: "Zeta Blog", Kind: "article", UnreadCount: 5, Paused: true},
		client.Source{ID: 7, Title: "少数派", Kind: "article", UnreadCount: 4},
		client.Source{ID: 8, Title: "数据周刊", Kind: "article"})
	f := &fake{now: now, sources: sources, entries: entries, patches: map[int64][]client.EntryPatch{}}
	d := f.deps()
	d.Reading = func() prefs.Preferences { return *p }
	d.Change = func(n prefs.Preferences) { *p = n }
	s := New(language, d)
	tt := ui.NewTester(func(c *ui.Context) { c.Theme().FontSize = 14; s.View(c); theme.Toasts(c) }, 1100, 800)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	tt.Frame()
	return s, f, tt
}

// sourceOrder lists the names among names that the sidebar (the first 260 DIPs) shows, from the top down.
func sourceOrder(t *testing.T, tt *ui.Tester, names ...string) []string {
	t.Helper()
	var shown []string
	for _, n := range names {
		if r, ok := tt.Find(n); ok && r.X < 260 {
			shown = append(shown, n)
		}
	}
	slices.SortFunc(shown, func(a, b string) int {
		ra, _ := tt.Find(a)
		rb, _ := tt.Find(b)
		return int(ra.Y - rb.Y)
	})
	return shown
}

var crowdNames = []string{"Go Blog", "Talks", "Photos", "Second Blog", "Zeta Blog", "少数派", "数据周刊"}

// TestSourceFilter narrows the subscriptions by name as you type, ignoring case, in Chinese too, and says so when
// nothing matches.
func TestSourceFilter(t *testing.T) {
	for language, none := range map[string]string{"en": "No matching subscriptions", "zh-Hans": "没有匹配的订阅"} {
		t.Run(language, func(t *testing.T) {
			p := prefs.Default()
			s, _, tt := crowded(t, language, &p)
			if len(sourceOrder(t, tt, crowdNames...)) != len(crowdNames) {
				t.Fatalf("not all subscriptions show: %v", sourceOrder(t, tt, crowdNames...))
			}
			if tt.HasText(none) {
				t.Fatal("the empty line shows before any filter")
			}
			if err := tt.Click(s.text.FilterSources); err != nil {
				t.Fatal(err)
			}
			tt.Type("BLOG")
			tt.Frame()
			if got := sourceOrder(t, tt, crowdNames...); !slices.Equal(got, []string{"Go Blog", "Second Blog", "Zeta Blog"}) {
				t.Fatalf("BLOG shows %v", got)
			}
			for range 4 {
				tt.Key(0, ui.KeyBackspace)
			}
			tt.Type("数")
			tt.Frame()
			if got := sourceOrder(t, tt, crowdNames...); !slices.Equal(got, []string{"少数派", "数据周刊"}) {
				t.Fatalf("数 shows %v", got)
			}
			tt.Type("x")
			tt.Frame()
			if got := sourceOrder(t, tt, crowdNames...); len(got) != 0 || !tt.HasText(none) {
				t.Fatalf("no match shows %v, empty line %v", got, tt.HasText(none))
			}
			if err := tt.Click(s.text.ClearFilter); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if s.sideQuery != "" || len(sourceOrder(t, tt, crowdNames...)) != len(crowdNames) || tt.HasText(none) {
				t.Fatalf("the clear button leaves %q", s.sideQuery)
			}
		})
	}
}

// TestSourceFilterKeys checks that the single keys type into the field instead of acting, that Escape clears and then
// leaves it, and that Down goes to the first match.
func TestSourceFilterKeys(t *testing.T) {
	p := prefs.Default()
	s, _, tt := crowded(t, "en", &p)
	if err := tt.Click(s.text.FilterSources); err != nil {
		t.Fatal(err)
	}
	tt.Type("jk2")
	tt.Frame()
	if s.sideQuery != "jk2" || s.tab != 0 {
		t.Fatalf("the keys acted while typing: query %q, tab %d", s.sideQuery, s.tab)
	}
	if !s.filtering {
		t.Fatal("the field does not hold the focus")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.sideQuery != "" || !tt.Focused(s.text.FilterSources) {
		t.Fatalf("the first Escape leaves %q, focused %v", s.sideQuery, tt.Focused(s.text.FilterSources))
	}
	tt.Type("blog")
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if !tt.Focused("Go Blog") || s.filtering {
		t.Fatal("Down does not reach the first match")
	}
	if err := tt.Click(s.text.FilterSources); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.sideQuery != "" || s.filtering || !s.inSide {
		t.Fatalf("the second Escape does not leave the field: query %q, in sidebar %v", s.sideQuery, s.inSide)
	}
}

// TestSourceSort puts the subscriptions with unread entries first, keeps the choice in the preferences and counts a
// paused subscription as having none.
func TestSourceSort(t *testing.T) {
	p := prefs.Default()
	s, _, tt := crowded(t, "en", &p)
	if got := sourceOrder(t, tt, crowdNames...); !slices.Equal(got, []string{"Go Blog", "Talks", "Photos", "Second Blog", "Zeta Blog", "少数派", "数据周刊"}) {
		t.Fatalf("the default order is %v", got)
	}
	if err := tt.Click(s.text.UnreadFirst); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if p.SourceSort != prefs.SortUnread {
		t.Fatalf("the choice is not kept: %q", p.SourceSort)
	}
	want := []string{"Second Blog", "少数派", "Go Blog", "Photos", "Talks", "Zeta Blog", "数据周刊"}
	if got := sourceOrder(t, tt, crowdNames...); !slices.Equal(got, want) {
		t.Fatalf("unread first shows %v, want %v", got, want)
	}
	if err := tt.Click(s.text.FilterSources); err != nil {
		t.Fatal(err)
	}
	tt.Type("blog")
	tt.Frame()
	if got := sourceOrder(t, tt, crowdNames...); !slices.Equal(got, []string{"Second Blog", "Go Blog", "Zeta Blog"}) {
		t.Fatalf("the filter and the sort together show %v", got)
	}
	if err := tt.Click(s.text.UnreadFirst); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if p.SourceSort != prefs.SortTitle {
		t.Fatalf("the toggle does not go back: %q", p.SourceSort)
	}
}

// TestSourceToolsHidden has no filter while there is nothing to filter.
func TestSourceToolsHidden(t *testing.T) {
	s, f, tt := setup(t, "en")
	if _, ok := tt.Find(s.text.FilterSources); !ok {
		t.Fatal("no filter with subscriptions")
	}
	f.sources = nil
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	if _, ok := tt.Find(s.text.FilterSources); ok {
		t.Fatal("a filter with no subscriptions")
	}
}

const gopherRow = "Gopher, Five pictures from the meetup, Yesterday, Unread"

// A click on a picture of a post opens the lightbox on that picture, not the browser, and marks the post read.
func TestLightboxOpensOnThePicture(t *testing.T) {
	s, f, tt := setup(t, "en")
	pick(t, f, tt, "Social")
	if err := tt.Click("Picture 3 of 5"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	tt.Frame()
	if !s.lb.open || s.lb.entry != 6 || s.lb.at != 2 || len(tt.OpenedURLs()) != 0 {
		t.Fatalf("lightbox %+v, opened %v", s.lb, tt.OpenedURLs())
	}
	if !tt.HasText("3 / 5") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if !slices.Contains(tt.Announcements(), "Picture 3 of 5") {
		t.Fatalf("announcements %v", tt.Announcements())
	}
}

// The title and the text of a post still open the browser.
func TestLightboxTitleStillOpensTheBrowser(t *testing.T) {
	s, f, tt := setup(t, "en")
	pick(t, f, tt, "Social")
	if err := tt.Click("Five pictures from the meetup"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	if s.lb.open || len(tt.OpenedURLs()) != 1 {
		t.Fatalf("lightbox %+v, opened %v", s.lb, tt.OpenedURLs())
	}
}

// The keys and buttons page, and stop at the ends.
func TestLightboxPaging(t *testing.T) {
	s, f, tt := setup(t, "en")
	pick(t, f, tt, "Social")
	if err := tt.Click("Picture 1 of 5"); err != nil {
		t.Fatal(err)
	}
	f.settle(t, tt)
	tt.Frame()
	tt.Key(0, ui.KeyLeft)
	if s.lb.at != 0 {
		t.Fatalf("Left wrapped to %d", s.lb.at)
	}
	tt.Key(0, ui.KeyRight)
	tt.Key(0, ui.KeyRight)
	if s.lb.at != 2 {
		t.Fatalf("at %d", s.lb.at)
	}
	tt.Key(0, ui.KeyEnd)
	tt.Key(0, ui.KeyRight)
	if s.lb.at != 4 {
		t.Fatalf("End/Right gave %d", s.lb.at)
	}
	tt.Key(0, ui.KeyHome)
	if s.lb.at != 0 {
		t.Fatalf("Home gave %d", s.lb.at)
	}
	if err := tt.Click("Next picture"); err != nil || s.lb.at != 1 {
		t.Fatalf("Next: %v, %d", err, s.lb.at)
	}
	if err := tt.Click("Previous picture"); err != nil || s.lb.at != 0 {
		t.Fatalf("Previous: %v, %d", err, s.lb.at)
	}
}

// Space opens from a selected row; Escape and Close close it.
func TestLightboxSpaceAndClose(t *testing.T) {
	s, f, tt := setup(t, "en")
	s.openPictures(2, 0)
	tt.Frame()
	f.settle(t, tt)
	tt.Key(0, ui.KeyEscape)
	if s.lb.open {
		t.Fatal("Escape did not close it")
	}
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	if !s.lb.open || s.lb.at != 0 {
		t.Fatalf("Space: %+v", s.lb)
	}
	if err := tt.Click("Close"); err != nil || s.lb.open {
		t.Fatalf("Close: %v, %v", err, s.lb.open)
	}
}

// A picture that fails shows the placeholder, and paging still works.
func TestLightboxFailedPicture(t *testing.T) {
	sources, entries := sample()
	f := &fake{now: now, sources: sources, entries: entries, patches: map[int64][]client.EntryPatch{},
		imageErr: errors.New("broken")}
	s := New("en", f.deps())
	tt := ui.NewTester(func(c *ui.Context) { s.View(c); theme.Toasts(c) }, 1100, 800)
	s.Use(f, "")
	tt.Frame()
	f.settle(t, tt)
	s.openPictures(2, 3)
	tt.Frame()
	f.settle(t, tt)
	tt.Frame()
	if !tt.HasText("This picture can’t be shown") {
		t.Fatalf("texts %v", tt.Texts())
	}
	tt.Key(0, ui.KeyRight)
	if s.lb.at != 4 {
		t.Fatalf("at %d", s.lb.at)
	}
}

// A click on the dim layer closes the lightbox, a click on the picture does not, and the page buttons and the browser
// buttons do their work.
func TestLightboxDimLayer(t *testing.T) {
	s, f, tt := setup(t, "en")
	s.openPictures(2, 1)
	tt.Frame()
	f.settle(t, tt)
	tt.Frame()
	tt.ClickAt(550, 400)
	if !s.lb.open {
		t.Fatal("a click on the picture closed it")
	}
	tt.Key(0, ui.KeyO)
	if got := tt.OpenedURLs(); len(got) != 1 || got[0] != "https://social.example/6" {
		t.Fatalf("O opened %v", got)
	}
	if err := tt.Click("Open picture in browser"); err != nil {
		t.Fatal(err)
	}
	if got := tt.OpenedURLs(); len(got) != 2 || got[1] != "https://social.example/b.jpg" {
		t.Fatalf("opened %v", got)
	}
	tt.ClickAt(1095, 795)
	if s.lb.open {
		t.Fatal("a click on the dim layer did not close it")
	}
}

func TestLightboxChinese(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	s.openPictures(2, 1)
	tt.Frame()
	f.settle(t, tt)
	tt.Frame()
	for _, name := range []string{"「Gopher」的图片", "上一张", "下一张"} {
		if _, ok := tt.Find(name); !ok {
			t.Errorf("no %q in %v", name, tt.Texts())
		}
	}
	if !slices.Contains(tt.Announcements(), "第 2 张，共 5 张") {
		t.Fatalf("announcements %v", tt.Announcements())
	}
}

// At the minimum width the controls stay inside the window and Open in browser is an icon.
func TestLightboxMinimumWidth(t *testing.T) {
	p := prefs.Default()
	s, f, _ := viewing(t, &p)
	tt := ui.NewTester(func(c *ui.Context) { c.Theme().FontSize = 14; s.View(c); theme.Toasts(c) }, 640, 540)
	tt.Frame()
	f.settle(t, tt)
	s.openPictures(2, 2)
	tt.Frame()
	f.settle(t, tt)
	tt.Frame()
	for _, name := range []string{"Open in browser", "Open picture in browser", "Close", "Previous picture", "Next picture"} {
		r, ok := tt.Find(name)
		if !ok || r.X < 0 || r.X+r.W > 640 {
			t.Fatalf("%q at %v", name, r)
		}
	}
	if r, _ := tt.Find("Open in browser"); r.W > 40 {
		t.Fatalf("Open in browser is %v wide, not an icon", r.W)
	}
}

func TestCopyLinkFromMenu(t *testing.T) {
	for language, c := range map[string]struct{ item, copied string }{
		"en":      {"Copy link", "Link copied"},
		"zh-Hans": {"复制链接", "已复制链接"},
	} {
		t.Run(language, func(t *testing.T) {
			_, f, tt := setup(t, language)
			if err := tt.RightClick("Harbor at dawn"); err != nil {
				t.Fatal(err)
			}
			if err := tt.ChooseMenuItem(c.item); err != nil {
				t.Fatal(err)
			}
			f.settle(t, tt)
			if tt.Clipboard() != "https://photos.example/5" || !slices.Contains(tt.Announcements(), c.copied) {
				t.Fatalf("clipboard %q, announcements %v", tt.Clipboard(), tt.Announcements())
			}
		})
	}
}

// TestSidebarAddAlignsWithRows checks that the icon of the add button in the Subscriptions header ends where the counts
// of the rows below it end, 10 in from the edge of the row highlight.
func TestSidebarAddAlignsWithRows(t *testing.T) {
	f := newPlaceFake()
	_, tt := newKept().open(t, f, "")
	add, ok := tt.Find("Add subscription")
	if !ok {
		t.Fatalf("no add button: %v", tt.Texts())
	}
	count, ok := tt.Find("2")
	if !ok {
		t.Fatalf("no count: %v", tt.Texts())
	}
	// The width of the count's text depends on the fonts of the platform, so allow less than half a pixel.
	if got, want := add.X+add.W-4, count.X+count.W; math.Abs(float64(got-want)) >= 0.5 {
		t.Errorf("icon ends at %v, the counts at %v", got, want)
	}
}

// TestFilterKeys shows U, A and * pick the filter the buttons pick.
func TestFilterKeys(t *testing.T) {
	s, f, tt := setup(t, "en")
	for _, c := range []struct {
		mod  ui.Modifiers
		key  ui.Key
		want int
	}{{0, ui.KeyU, unread}, {ui.Shift, ui.Key8, starred}, {0, ui.KeyA, everything}} {
		tt.Key(c.mod, c.key)
		f.settle(t, tt)
		if s.filter != c.want {
			t.Fatalf("key %v: filter %d, want %d", c.key, s.filter, c.want)
		}
	}
	q := len(f.queries)
	tt.Key(0, ui.KeyA)
	f.settle(t, tt)
	if len(f.queries) != q {
		t.Fatal("the filter in use was loaded again")
	}
}

// TestMarkAllReadKey shows Shift+A marks the scope read and offers the undo, which Cmd+Z takes back.
func TestMarkAllReadKey(t *testing.T) {
	s, f, tt := setup(t, "en")
	tt.Key(0, ui.KeyA)
	if len(f.marked) != 0 {
		t.Fatalf("plain A marked %v", f.marked)
	}
	tt.Key(ui.Shift, ui.KeyA)
	f.settle(t, tt)
	if !reflect.DeepEqual(f.marked, []client.ReadScope{{UpTo: 8}}) || s.canMarkAll() {
		t.Fatalf("marked %v", f.marked)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	f.settle(t, tt)
	if len(f.unmarked) != 1 || s.unreadIn(s.scope) != 5 {
		t.Fatalf("unmarked %v", f.unmarked)
	}
}

// TestLoadNewKey shows R loads the entries the "N new" pill offers and does nothing without the pill.
func TestLoadNewKey(t *testing.T) {
	s, f, tt := setup(t, "en")
	q := len(f.queries)
	tt.Key(0, ui.KeyR)
	f.settle(t, tt)
	if len(f.queries) != q {
		t.Fatal("R loaded without new entries")
	}
	f.entries = append([]client.Entry{{ID: 9, SourceID: 1, SourceTitle: "Go Blog", Kind: "article", Title: "Fresh",
		CreatedAt: now.UnixMilli()}}, f.entries...)
	f.sources[0].UnreadCount++
	f.now = f.now.Add(pollEvery)
	tt.Frame()
	f.settle(t, tt)
	if !tt.HasText("1 new") {
		t.Fatalf("texts %v", tt.Texts())
	}
	tt.Key(0, ui.KeyR)
	f.settle(t, tt)
	if len(s.entries) != 7 || tt.HasText("1 new") {
		t.Fatalf("%d entries", len(s.entries))
	}
}

// TestCommandKeysWhileTyping shows the new keys type into the search and do nothing else, and that a dialog takes
// them too.
func TestCommandKeysWhileTyping(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.entries = append([]client.Entry{{ID: 9, SourceID: 1, SourceTitle: "Go Blog", Kind: "article", Title: "Fresh",
		CreatedAt: now.UnixMilli()}}, f.entries...)
	f.now = f.now.Add(pollEvery)
	tt.Frame()
	f.settle(t, tt)
	tt.Key(0, ui.KeySlash)
	if !tt.Focused("Search entries") {
		t.Fatal("/ does not focus the search")
	}
	q := len(f.queries)
	tt.Type("uar")
	tt.Key(ui.Shift, ui.KeyA)
	tt.Key(ui.Shift, ui.Key8)
	f.settle(t, tt)
	if s.filter != everything || len(f.marked) != 0 || s.fresh != 1 || s.query != "uar" || len(f.queries) != q {
		t.Fatalf("filter %d, marked %v, fresh %d, query %q", s.filter, f.marked, s.fresh, s.query)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Key(0, ui.KeyEscape)
	s.ShowKeys()
	tt.Frame()
	tt.Key(0, ui.KeyU)
	tt.Key(ui.Shift, ui.KeyA)
	tt.Key(0, ui.KeyR)
	f.settle(t, tt)
	if s.filter != everything || len(f.marked) != 0 || s.fresh != 1 {
		t.Fatalf("the key sheet let the keys through: filter %d, marked %v", s.filter, f.marked)
	}
}

// TestRunCommands shows the Timeline menu's commands do what the keys do, and the key sheet lists the keys.
func TestRunCommands(t *testing.T) {
	s, f, tt := setup(t, "en")
	s.Run(ShowUnread)
	f.settle(t, tt)
	if s.filter != unread {
		t.Fatalf("filter %d", s.filter)
	}
	s.Run(ShowStarred)
	f.settle(t, tt)
	s.Run(ShowAll)
	f.settle(t, tt)
	if s.filter != everything {
		t.Fatalf("filter %d", s.filter)
	}
	s.Run(MarkAllRead)
	f.settle(t, tt)
	if len(f.marked) != 1 {
		t.Fatalf("marked %v", f.marked)
	}
	name, items := TimelineMenu("en")
	if name != "Timeline" || len(items) != 5 || items[3].Label != "Mark all as read" {
		t.Fatalf("menu %q %v", name, items)
	}
	if name, _ := TimelineMenu("zh-Hans"); name != "时间线" {
		t.Fatalf("menu %q", name)
	}
	s.ShowKeys()
	tt.Frame()
	for _, text := range []string{"Mark all as read", "Show unread only", "Show all entries", "Show starred only",
		"Load new entries"} {
		if !tt.HasText(text) {
			t.Fatalf("the key sheet lacks %q", text)
		}
	}
}

// fetched marks the first n subscriptions as fetched at the fake time.
func (f *fake) fetched(n int) {
	for i := range f.sources[:n] {
		f.sources[i].LastFetchedAt = f.now.UnixMilli()
	}
}

// refreshAll presses Cmd+R and runs the request, which queued n subscriptions at the fake time.
func refreshAll(t *testing.T, f *fake, tt *ui.Tester, n int) {
	t.Helper()
	f.refreshed = client.RefreshResult{Queued: n, At: f.now.UnixMilli()}
	tt.Key(ui.Cmd, ui.KeyR)
	f.settle(t, tt)
}

// TestRefreshAll asks every subscription to be checked, shows the count in the sidebar while the server works through
// them, ignores a second request, and ends in a toast that says how many failed and has the timeline look at once.
func TestRefreshAll(t *testing.T) {
	s, f, tt := setup(t, "en")
	refreshAll(t, f, tt, 4)
	if f.refreshes != 1 || !tt.HasText("0/4") || !slices.Contains(tt.Announcements(), "Refreshing 4 subscriptions") {
		t.Fatalf("refreshes %d, texts %v, announcements %v", f.refreshes, tt.Texts(), tt.Announcements())
	}
	if tt.HasText("1 failing") {
		t.Error("the failing count shows beside the progress")
	}
	tt.Key(ui.Cmd, ui.KeyR)
	if f.refreshes != 1 || len(f.queue) != 0 {
		t.Fatalf("a second request went out: %d, %d queued", f.refreshes, len(f.queue))
	}

	f.now = f.now.Add(refreshEvery)
	f.fetched(2)
	tt.Frame()
	f.settle(t, tt)
	if !tt.HasText("2/4") || s.refresh == nil {
		t.Fatalf("texts %v", tt.Texts())
	}

	f.now = f.now.Add(refreshEvery)
	f.fetched(4)
	asked := len(f.queries)
	tt.Frame()
	f.settle(t, tt)
	if s.refresh != nil || tt.HasText("4/4") || !tt.HasText("Checked 4 subscriptions, 1 failed") || !tt.HasText("1 failing") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if len(f.queries) == asked {
		t.Error("the timeline did not look for new entries when the refresh ended")
	}
	refreshAll(t, f, tt, 4)
	if f.refreshes != 2 || s.refresh == nil {
		t.Fatalf("it cannot be asked again after it ended: %d", f.refreshes)
	}
}

func TestRefreshAllPlainResult(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.sources[2].LastError = ""
	refreshAll(t, f, tt, 4)
	f.now = f.now.Add(refreshEvery)
	f.fetched(4)
	tt.Frame()
	f.settle(t, tt)
	if s.refresh != nil || !tt.HasText("Checked 4 subscriptions") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

// TestRefreshAllTimeout stops counting after refreshLimit and says how many were checked.
func TestRefreshAllTimeout(t *testing.T) {
	s, f, tt := setup(t, "en")
	refreshAll(t, f, tt, 4)
	f.now = f.now.Add(refreshLimit)
	f.fetched(1)
	tt.Frame()
	f.settle(t, tt)
	if s.refresh != nil || !tt.HasText("Checked 1 of 4 subscriptions, the rest will follow") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestRefreshAllNothingAndFailure(t *testing.T) {
	s, f, tt := setup(t, "en")
	refreshAll(t, f, tt, 0)
	if s.refresh != nil || !tt.HasText("No subscriptions to refresh") || tt.HasText("0/0") {
		t.Fatalf("texts %v", tt.Texts())
	}
	f.refreshErr = errors.New("down")
	refreshAll(t, f, tt, 0)
	if s.refresh != nil || !tt.HasText("Can't refresh now. Try again later.") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestRefreshAllExpired(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.refreshErr = client.ErrUnauthorized
	refreshAll(t, f, tt, 0)
	if f.expired != 1 || s.refresh != nil {
		t.Fatalf("expired %d", f.expired)
	}
}

// TestRefreshAllEntryPoints reaches it from the palette and the key sheet, in both languages.
func TestRefreshAllEntryPoints(t *testing.T) {
	for language, name := range map[string]string{"en": "Refresh all subscriptions", "zh-Hans": "全部更新"} {
		t.Run(language, func(t *testing.T) {
			s, f, tt := setup(t, language)
			s.ShowKeys()
			tt.Frame()
			if !tt.HasText(name) {
				t.Fatalf("no key sheet row in %v", tt.Texts())
			}
			tt.Key(0, ui.KeyEscape)
			tt.Key(ui.Cmd, ui.KeyK)
			tt.Frame()
			if !tt.HasText(name) {
				t.Fatalf("no palette action in %v", tt.Texts())
			}
			if err := tt.Click(name); err != nil {
				t.Fatal(err)
			}
			f.settle(t, tt)
			if f.refreshes != 1 {
				t.Fatalf("refreshes %d", f.refreshes)
			}
		})
	}
	menu, item := RefreshMenuLabels("zh-Hans")
	if menu != "订阅" || item != "全部更新" {
		t.Errorf("labels %q %q", menu, item)
	}
}

// TestRefreshKeyBesideLoadNew shows Cmd+R refreshes all subscriptions without loading the new entries, and plain R
// loads them without refreshing.
func TestRefreshKeyBesideLoadNew(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.entries = append([]client.Entry{{ID: 9, SourceID: 1, SourceTitle: "Go Blog", Kind: "article", Title: "Fresh",
		CreatedAt: now.UnixMilli()}}, f.entries...)
	f.sources[0].UnreadCount++
	f.now = f.now.Add(pollEvery)
	tt.Frame()
	f.settle(t, tt)
	if !tt.HasText("1 new") {
		t.Fatalf("texts %v", tt.Texts())
	}
	before := len(s.entries)
	refreshAll(t, f, tt, 4)
	if f.refreshes != 1 || !tt.HasText("1 new") || len(s.entries) != before {
		t.Fatalf("refreshes %d, %d entries, texts %v", f.refreshes, len(s.entries), tt.Texts())
	}
	tt.Key(0, ui.KeyR)
	f.settle(t, tt)
	if f.refreshes != 1 || tt.HasText("1 new") || len(s.entries) != before+1 {
		t.Fatalf("refreshes %d, %d entries", f.refreshes, len(s.entries))
	}
}

// fetchedWith makes the server's copy of the subscription id look fetched: its fetch time, error and unread count.
func (f *fake) fetchedWith(id int64, unread int, lastError string) {
	for i := range f.sources {
		if f.sources[i].ID == id {
			f.sources[i].LastFetchedAt++
			f.sources[i].UnreadCount, f.sources[i].LastError = unread, lastError
		}
	}
}

// later moves the clock on and draws, which asks the server about the subscriptions waited for.
func (f *fake) later(t *testing.T, tt *ui.Tester, d time.Duration) {
	t.Helper()
	f.now = f.now.Add(d)
	tt.Frame()
	f.settle(t, tt)
}

// addFresh adds a subscription the server has not fetched yet.
func addFresh(t *testing.T, s *Screen, f *fake, tt *ui.Tester, title string) {
	t.Helper()
	added := client.Source{ID: 10, Title: title, Kind: "article"}
	f.sources = append(f.sources, added)
	s.added(added)
	tt.Frame()
	f.settle(t, tt)
}

func TestFetchResultOfNewSubscription(t *testing.T) {
	s, f, tt := setup(t, "en")
	addFresh(t, s, f, tt, "Fresh Feed")
	if err := tt.Click("Fresh Feed, fetching"); err != nil {
		t.Fatalf("no progress on the row: %v", err)
	}
	f.later(t, tt, time.Second)
	if len(f.queue) != 0 || len(s.watching) != 1 {
		t.Fatalf("asked before %v: queue %d, watching %v", watchEvery, len(f.queue), s.watching)
	}
	f.later(t, tt, 3*time.Second)
	if len(s.watching) != 1 || tt.HasText("Fetched") {
		t.Fatalf("done before the fetch: %v", tt.Texts())
	}
	f.fetchedWith(10, 12, "")
	f.later(t, tt, 3*time.Second)
	if len(s.watching) != 0 || !tt.HasText("Fetched 12 entries from “Fresh Feed”") {
		t.Fatalf("watching %v, texts %v", s.watching, tt.Texts())
	}
	if err := tt.Click("Fresh Feed, fetching"); err == nil {
		t.Fatal("the row still shows progress")
	}
	f.later(t, tt, 10*time.Second)
	if len(f.queue) != 0 {
		t.Fatal("kept asking after the fetch")
	}
}

func TestFetchResultNoEntries(t *testing.T) {
	s, f, tt := setup(t, "en")
	addFresh(t, s, f, tt, "Fresh Feed")
	f.fetchedWith(10, 0, "")
	f.later(t, tt, 3*time.Second)
	if !tt.HasText("“Fresh Feed” fetched, but has no entries yet") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestFetchResultOfRefresh(t *testing.T) {
	s, f, tt := setup(t, "en")
	s.subs.Refresh(f.sources[0])
	f.settle(t, tt)
	f.fetchedWith(1, 2, "")
	f.later(t, tt, 3*time.Second)
	if !tt.HasText("“Go Blog” has nothing new") {
		t.Fatalf("texts %v", tt.Texts())
	}
	s.subs.Refresh(f.sources[0])
	f.settle(t, tt)
	f.fetchedWith(1, 5, "")
	f.later(t, tt, 3*time.Second)
	if !tt.HasText("“Go Blog” has 3 new entries") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestFetchResultFailed(t *testing.T) {
	s, f, tt := setup(t, "en")
	addFresh(t, s, f, tt, "Fresh Feed")
	f.fetchedWith(10, 0, "HTTP 404")
	f.later(t, tt, 3*time.Second)
	if !tt.HasText("Couldn’t fetch “Fresh Feed”: HTTP 404") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestFetchWaitEnds(t *testing.T) {
	s, f, tt := setup(t, "en")
	s.subs.Refresh(f.sources[0])
	f.settle(t, tt)
	for range 21 {
		f.later(t, tt, 3*time.Second)
	}
	if len(s.watching) != 0 || len(f.queue) != 0 || tt.HasText("Go Blog has") {
		t.Fatalf("watching %v, texts %v", s.watching, tt.Texts())
	}
	f.later(t, tt, 3*time.Second)
	if len(f.queue) != 0 {
		t.Fatal("kept asking after the bound")
	}
}

func TestFetchWaitDropsRemoved(t *testing.T) {
	s, f, tt := setup(t, "en")
	s.subs.Refresh(f.sources[0])
	f.settle(t, tt)
	f.sources = f.sources[1:]
	f.later(t, tt, 3*time.Second)
	if len(s.watching) != 0 || tt.HasText("Go Blog has") {
		t.Fatalf("watching %v, texts %v", s.watching, tt.Texts())
	}
}

func TestFetchResultInChinese(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	addFresh(t, s, f, tt, "新订阅")
	f.fetchedWith(10, 1, "")
	f.later(t, tt, 3*time.Second)
	if !tt.HasText("「新订阅」抓到 1 条") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func (f *fake) MutedCount(context.Context, int64, string) (int, error) { return 0, nil }

// polls counts the checks of the first page so far.
func (f *fake) polls() int { return len(f.queries) }

// pass moves the clock on by wait and draws a frame, then lets the work it started finish.
func (f *fake) pass(t *testing.T, tt *ui.Tester, wait time.Duration) {
	t.Helper()
	f.now = f.now.Add(wait)
	tt.Frame()
	f.settle(t, tt)
}

func TestOfflineBackoff(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.entriesErr = errors.New("offline")
	f.pass(t, tt, pollEvery)
	if !s.offline {
		t.Fatal("the failed check did not take the screen offline")
	}
	for i, wait := range []time.Duration{3, 5, 10, 30, 30} {
		wait *= time.Second
		before := f.polls()
		f.pass(t, tt, wait-time.Second)
		if f.polls() != before {
			t.Fatalf("retry %d checked %v early", i+1, time.Second)
		}
		f.pass(t, tt, time.Second)
		if f.polls() != before+1 {
			t.Fatalf("retry %d did not check after %v", i+1, wait)
		}
	}
	f.entriesErr = nil
	f.pass(t, tt, 30*time.Second)
	if s.offline || s.failures != 0 {
		t.Fatal("the check that got through did not end the outage")
	}
	before := f.polls()
	f.pass(t, tt, pollEvery-time.Second)
	if f.polls() != before {
		t.Fatal("the usual wait did not come back after the outage")
	}
	f.pass(t, tt, time.Second)
	if f.polls() != before+1 {
		t.Fatal("the usual check did not run")
	}
}

func TestOfflineRetryButton(t *testing.T) {
	for language, names := range map[string][3]string{
		"en":      {"Can’t reach the server · last updated 12:00", "Try again", "Retrying…"},
		"zh-Hans": {"连不上服务端 · 上次更新于 12:00", "重试", "正在重试…"},
	} {
		t.Run(language, func(t *testing.T) {
			s, f, tt := setup(t, language)
			line, retry, retrying := names[0], names[1], names[2]
			has := func(text string) bool { return slices.Contains(tt.Texts(), text) }
			f.entriesErr = errors.New("offline")
			f.pass(t, tt, pollEvery)
			if !has(line) || !has(retry) {
				t.Fatalf("texts %v", tt.Texts())
			}
			before := f.polls()
			if err := tt.Click(retry); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if !has(retrying) || has(retry) {
				t.Fatalf("the click did not start a check at once: texts %v", tt.Texts())
			}
			f.settle(t, tt)
			tt.Frame()
			if f.polls() != before+1 {
				t.Fatalf("the click made %d checks", f.polls()-before)
			}
			if !has(retry) || has(retrying) || !s.offline {
				t.Fatalf("texts %v", tt.Texts())
			}
			f.entriesErr = nil
			if err := tt.Click(retry); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			f.settle(t, tt)
			tt.Frame()
			if s.offline || has(line) || has(retry) {
				t.Fatalf("the line stayed after the server answered: %v", tt.Texts())
			}
		})
	}
}

func TestCheckOnActivation(t *testing.T) {
	s, f, tt := setup(t, "en")
	before := f.polls()
	s.Check()
	f.pass(t, tt, time.Second)
	if f.polls() != before {
		t.Fatal("bringing the window forward right after a check checked again")
	}
	f.now = f.now.Add(checkGap)
	s.Check()
	f.pass(t, tt, 0)
	if f.polls() != before+1 {
		t.Fatalf("bringing the window forward after %v did not check", checkGap)
	}
	f.entriesErr = errors.New("offline")
	f.pass(t, tt, pollEvery)
	before = f.polls()
	s.Check()
	f.pass(t, tt, 0)
	if f.polls() != before+1 {
		t.Fatal("bringing the window forward while offline did not check at once")
	}
	f.entriesErr = nil
	s.Check()
	f.pass(t, tt, 0)
	if s.offline {
		t.Fatal("the check on waking did not notice the server back")
	}
	s.Use(nil, "")
	s.Check()
}

func TestQueuedChangesKept(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.setErr = errors.New("offline")
	tt.Key(0, ui.KeyJ)
	tt.Key(0, ui.KeyM)
	f.settle(t, tt)
	if !s.entries[0].IsRead || len(s.queue) != 1 || !s.offline {
		t.Fatalf("read %v, %d waiting, offline %v", s.entries[0].IsRead, len(s.queue), s.offline)
	}
	if tt.HasText("Couldn’t save the change") || !strings.Contains(s.label(s.entries[0], f.now), "Waiting to save") {
		t.Fatalf("texts %v", tt.Texts())
	}
	// Another change of the same entry while offline is not sent; it folds into the one waiting.
	sent := len(f.patches[8])
	tt.Key(0, ui.KeyS)
	f.settle(t, tt)
	if len(f.patches[8]) != sent || len(s.queue) != 1 || !s.entries[0].Starred {
		t.Fatalf("sent %d, waiting %d, starred %v", len(f.patches[8])-sent, len(s.queue), s.entries[0].Starred)
	}
	// The server answers the check but still does not take the writes: they wait on.
	f.pass(t, tt, 3*time.Second)
	if !s.offline || len(s.queue) != 1 || !s.entries[0].IsRead || !s.entries[0].Starred {
		t.Fatalf("offline %v, waiting %d", s.offline, len(s.queue))
	}
	// Loading the list again shows the changes on top of what the server still has.
	s.reload()
	f.settle(t, tt)
	if !s.entries[0].IsRead || !s.entries[0].Starred {
		t.Fatal("a list loaded while changes wait lost them")
	}
	f.setErr = nil
	f.pass(t, tt, 30*time.Second)
	last := f.patches[8][len(f.patches[8])-1]
	if len(s.queue) != 0 || last.IsRead == nil || !*last.IsRead || last.Starred == nil || !*last.Starred {
		t.Fatalf("waiting %d, last sent %+v", len(s.queue), last)
	}
	if !s.entries[0].IsRead || !s.entries[0].Starred || s.offline || tt.HasText("Couldn’t save the change") {
		t.Fatal("the changes did not stay after they were sent")
	}
	if strings.Contains(s.label(s.entries[0], f.now), "Waiting to save") {
		t.Fatal("the mark stayed after the change was sent")
	}
}

func TestQueuedChangeRefused(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.setErr = errors.New("offline")
	tt.Key(0, ui.KeyJ)
	tt.Key(0, ui.KeyM)
	f.settle(t, tt)
	f.setErr = &client.APIError{Status: 404, Code: "not_found"}
	f.pass(t, tt, 3*time.Second)
	if s.entries[0].IsRead || len(s.queue) != 0 || !tt.HasText("Couldn’t save the change") {
		t.Fatalf("read %v, waiting %d, texts %v", s.entries[0].IsRead, len(s.queue), tt.Texts())
	}
}

func TestQueuedChangesLostWithServer(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.setErr = errors.New("offline")
	tt.Key(0, ui.KeyJ)
	tt.Key(0, ui.KeyM)
	f.settle(t, tt)
	s.Use(f, "other:7470")
	f.settle(t, tt)
	if len(s.queue) != 0 {
		t.Fatal("the queue outlived the server it was made for")
	}
}
