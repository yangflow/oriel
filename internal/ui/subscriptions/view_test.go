package subscriptions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/theme"
)

var now = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// fake stands in for the server, the timeline and the background: work waits in a queue until step runs it.
type fake struct {
	queue    []func() func()
	sources  []client.Source
	settings client.Settings
	health   client.Health

	added       []client.NewSource
	patches     map[int64][]client.SourcePatch
	deleted     []int64
	restoredIDs []int64
	restoreErr  error
	fetched     []int64
	set         []client.SettingsPatch
	looked      []string // the addresses Discover was asked for
	found       []client.Candidate
	lateBy      time.Duration // how far the clock is past now

	searched   []string // the queries FindRoutes was asked for
	routes     client.Routes
	routesErr  error
	previewed  []string // the paths PreviewRoute was asked for
	preview    client.RoutePreview
	previewErr error

	addErr, updateErr, deleteErr, refreshErr, settingsErr, setErr, healthErr, discoverErr error

	hidden    int      // what the server says the mute keywords hide
	hiddenErr error    // why it cannot say
	asked     []string // the keywords it was asked about

	changed  []client.Source
	removed  []int64
	restored []client.Source
	shown    []int64
	marked   []int64
	older    [][2]int64 // subscription and days of each "Mark older than"
	opened   int        // the AI settings
	expired  int
}

func (f *fake) AddSource(_ context.Context, src client.NewSource) (client.Source, error) {
	f.added = append(f.added, src)
	if f.addErr != nil {
		return client.Source{}, f.addErr
	}
	return client.Source{ID: 9, URL: src.URL, RSSHubPath: src.RSSHubPath, Kind: src.Kind, Title: src.Title,
		FetchIntervalSec: src.FetchIntervalSec}, nil
}

func (f *fake) UpdateSource(_ context.Context, id int64, patch client.SourcePatch) (client.Source, error) {
	f.patches[id] = append(f.patches[id], patch)
	if f.updateErr != nil {
		return client.Source{}, f.updateErr
	}
	for _, src := range f.sources {
		if src.ID == id {
			if patch.Title != nil {
				src.Title = *patch.Title
			}
			if patch.Paused != nil {
				src.Paused = *patch.Paused
			}
			return src, nil
		}
	}
	return client.Source{}, &client.APIError{Status: 404, Code: "not_found"}
}

func (f *fake) DeleteSource(_ context.Context, id int64) error {
	f.deleted = append(f.deleted, id)
	return f.deleteErr
}

func (f *fake) RestoreSource(_ context.Context, id int64) (client.Source, error) {
	f.restoredIDs = append(f.restoredIDs, id)
	if f.restoreErr != nil {
		return client.Source{}, f.restoreErr
	}
	return client.Source{ID: id, Title: "Back"}, nil
}

func (f *fake) RefreshSource(_ context.Context, id int64) error {
	f.fetched = append(f.fetched, id)
	return f.refreshErr
}

func (f *fake) SourcesHealth(context.Context) (client.Health, error) {
	return f.health, f.healthErr
}

func (f *fake) MutedCount(_ context.Context, _ int64, keywords string) (int, error) {
	f.asked = append(f.asked, keywords)
	return f.hidden, f.hiddenErr
}

func (f *fake) Settings(context.Context) (client.Settings, error) {
	return f.settings, f.settingsErr
}

func (f *fake) SetSettings(_ context.Context, patch client.SettingsPatch) (client.Settings, error) {
	f.set = append(f.set, patch)
	if f.setErr != nil {
		return client.Settings{}, f.setErr
	}
	f.settings.RSSHubBaseURLs = *patch.RSSHubBaseURLs
	return f.settings, nil
}

func (f *fake) Discover(_ context.Context, address string) ([]client.Candidate, error) {
	f.looked = append(f.looked, address)
	return f.found, f.discoverErr
}

func (f *fake) FindRoutes(_ context.Context, query string, _ bool) (client.Routes, error) {
	f.searched = append(f.searched, query)
	return f.routes, f.routesErr
}

func (f *fake) PreviewRoute(_ context.Context, path string) (client.RoutePreview, error) {
	f.previewed = append(f.previewed, path)
	return f.preview, f.previewErr
}

func (f *fake) deps() Deps {
	return Deps{
		Go:           func(work func() func()) { f.queue = append(f.queue, work) },
		Now:          func() time.Time { return now.Add(f.lateBy) },
		Sources:      func() []client.Source { return f.sources },
		Added:        func(src client.Source) { f.sources = append(f.sources, src) },
		Changed:      func(src client.Source) { f.changed = append(f.changed, src) },
		Removed:      func(id int64) { f.removed = append(f.removed, id) },
		Restored:     func(src client.Source) { f.restored = append(f.restored, src) },
		Show:         func(id int64) { f.shown = append(f.shown, id) },
		MarkRead:     func(src client.Source) { f.marked = append(f.marked, src.ID) },
		MarkOlder:    func(src client.Source, days int) { f.older = append(f.older, [2]int64{src.ID, int64(days)}) },
		OpenSettings: func() { f.opened++ },
		Expired:      func() { f.expired++ },
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

func setup(t *testing.T, language string) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	f := &fake{
		sources: []client.Source{
			{ID: 1, Title: "Go Blog", URL: "https://go.dev/blog/feed.atom", Kind: "article",
				FetchIntervalSec: 900, FetchIntervalAuto: true, LastFetchedAt: now.Add(-3 * time.Minute).UnixMilli(),
				NextFetchAt: now.Add(12 * time.Minute).UnixMilli()},
			{ID: 2, RSSHubPath: "/telegram/channel/golang", Kind: "social", FetchIntervalSec: 3600,
				LastError: "status 503", ConsecutiveFailures: 3},
		},
		settings: client.Settings{RSSHubBaseURLs: []string{"https://rsshub.example.com"}, RetentionDays: 30},
		patches:  map[int64][]client.SourcePatch{},
	}
	s := New(language, f.deps())
	s.muteWait = 0
	s.Use(f)
	tt := ui.NewTester(s.View, 1000, 760)
	tt.Frame()
	return s, f, tt
}

// open opens the add dialog and loads the settings.
func open(t *testing.T, s *Screen, f *fake, tt *ui.Tester) {
	t.Helper()
	s.Add()
	tt.Frame()
	f.settle(tt)
	if !tt.Focused(s.text.Address) {
		t.Fatal("the address field is not focused")
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

func TestRecognize(t *testing.T) {
	const base = "https://rsshub.example.com/"
	for _, c := range []struct {
		input string
		want  target
	}{
		{"  https://go.dev/blog/feed.atom ", target{URL: "https://go.dev/blog/feed.atom", Platform: feed, Name: "go.dev"}},
		{"/github/issue/golang/go", target{Route: "/github/issue/golang/go", Platform: route}},
		{"https://rsshub.example.com/bilibili/user/video/2", target{Route: "/bilibili/user/video/2", Platform: route}},
		{"https://www.youtube.com/channel/UCabc", target{URL: "https://www.youtube.com/feeds/videos.xml?channel_id=UCabc",
			Kind: "video", Platform: youtubeChannel, Name: "UCabc"}},
		{"https://youtube.com/@golang", target{Route: "/youtube/user/@golang", Kind: "video", Platform: youtubeUser,
			Name: "@golang"}},
		{"https://m.youtube.com/playlist?list=PL1", target{URL: "https://www.youtube.com/feeds/videos.xml?playlist_id=PL1",
			Kind: "video", Platform: youtubePlaylist, Name: "PL1"}},
		{"https://space.bilibili.com/2267573", target{Route: "/bilibili/user/video/2267573", Kind: "video",
			Platform: bilibili, Name: "2267573"}},
		{"https://x.com/golang", target{Route: "/twitter/user/golang", Kind: "social", Platform: xUser, Name: "golang"}},
		{"https://twitter.com/home", target{URL: "https://twitter.com/home", Platform: feed, Name: "twitter.com"}},
		{"https://github.com/golang/go.git", target{URL: "https://github.com/golang/go/releases.atom", Kind: "article",
			Platform: githubRepo, Name: "golang/go"}},
		{"https://github.com/rsc", target{URL: "https://github.com/rsc.atom", Kind: "article", Platform: githubUser,
			Name: "rsc"}},
		{"https://github.com/golang/go/commits.atom", target{URL: "https://github.com/golang/go/commits.atom",
			Platform: feed, Name: "github.com"}},
		{"https://t.me/s/golang", target{Route: "/telegram/channel/golang", Kind: "social", Platform: telegram,
			Name: "golang"}},
		{"https://weibo.com/u/1195230310", target{Route: "/weibo/user/1195230310", Kind: "social", Platform: weibo,
			Name: "1195230310"}},
	} {
		got, ok := recognize(c.input, base)
		if !ok || got != c.want {
			t.Errorf("recognize(%q) = %+v, %v; want %+v", c.input, got, ok, c.want)
		}
	}
	for _, input := range []string{"", "go.dev/blog", "ftp://go.dev/feed", "//go.dev/feed", "https://", "mailto:a@b"} {
		if got, ok := recognize(input, base); ok {
			t.Errorf("recognize(%q) = %+v, want invalid", input, got)
		}
	}
	if got, _ := recognize("https://rsshub.example.com/x", ""); got.URL != "https://rsshub.example.com/x" {
		t.Errorf("without an instance the address is a feed: %+v", got)
	}
}

func TestName(t *testing.T) {
	for _, c := range []struct {
		src  client.Source
		want string
	}{
		{client.Source{Title: " Go Blog ", URL: "https://go.dev/feed"}, "Go Blog"},
		{client.Source{URL: "https://www.go.dev/feed"}, "go.dev"},
		{client.Source{RSSHubPath: "/telegram/channel/golang"}, "/telegram/channel/golang"},
	} {
		if got := Name(c.src); got != c.want {
			t.Errorf("Name(%+v) = %q, want %q", c.src, got, c.want)
		}
	}
}

func TestAdd(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	if !tt.HasText(s.text.AddressHelp) {
		t.Fatalf("no help in %v", tt.Texts())
	}
	tt.Type("https://x.com/golang")
	tt.Frame()
	for _, text := range []string{"X · posts by @golang", "RSSHub route /twitter/user/golang"} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
	if s.add.kind != slices.Index(kinds, "social") {
		t.Fatalf("kind %d is not the platform's", s.add.kind)
	}
	if err := tt.Click("Videos"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Auto"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("1 h"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Add"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Adding") {
		t.Fatalf("not busy: %v", tt.Texts())
	}
	f.settle(tt)
	want := client.NewSource{RSSHubPath: "/twitter/user/golang", Kind: "video", FetchIntervalSec: 3600}
	if len(f.added) != 1 || f.added[0] != want {
		t.Fatalf("added %+v", f.added)
	}
	if s.Open() || len(f.sources) != 3 {
		t.Fatalf("open %v, sources %v", s.Open(), f.sources)
	}
	if len(f.set) != 0 {
		t.Fatalf("settings changed: %v", f.set)
	}
}

func TestAddAndContinue(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	tt.Type("https://x.com/golang")
	tt.Frame()
	if err := tt.Click("Add and continue"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !s.Open() || s.add.address != "" || s.add.title != "" || !tt.Focused(s.text.Address) {
		t.Fatalf("open %v, address %q, texts %v", s.Open(), s.add.address, tt.Texts())
	}
	if !tt.HasText("Added “/twitter/user/golang”") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if !tt.HasText("Done") || tt.HasText("Cancel") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if len(f.sources) != 3 {
		t.Fatalf("the timeline was not told: %v", f.sources)
	}

	// The shortcut adds the next one, and a failed one blocks nothing.
	f.addErr = &client.APIError{Status: 409, Code: "source_exists"}
	tt.Type("https://go.dev/blog/feed.atom")
	tt.Key(ui.Cmd, ui.KeyEnter)
	f.settle(tt)
	if !tt.HasText("You already follow this as “Go Blog”.") || !s.Open() || s.add.count != 1 {
		t.Fatalf("count %d, texts %v", s.add.count, tt.Texts())
	}
	f.addErr = nil
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("https://example.com/feed.xml")
	tt.Key(ui.Cmd, ui.KeyEnter)
	f.settle(tt)
	if s.add.count != 2 || !tt.HasText("Added “example.com” · 2 added") {
		t.Fatalf("count %d, texts %v", s.add.count, tt.Texts())
	}
	if len(f.added) != 3 || len(f.sources) != 4 {
		t.Fatalf("added %v, sources %d", f.added, len(f.sources))
	}

	// Enter adds the last one and closes.
	tt.Type("https://example.org/feed.xml")
	tt.Key(0, ui.KeyEnter)
	f.settle(tt)
	if s.Open() || len(f.added) != 4 {
		t.Fatalf("open %v, added %v", s.Open(), f.added)
	}
}

func TestAddAndContinueNeedsAnAddress(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	tt.Key(ui.Cmd, ui.KeyEnter)
	f.settle(tt)
	if len(f.added) != 0 || !s.Open() {
		t.Fatalf("added %v", f.added)
	}
}

func TestAddAndContinueChinese(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	open(t, s, f, tt)
	tt.Type("https://example.com/feed.xml")
	tt.Frame()
	if err := tt.Click("添加并继续"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !tt.HasText("已添加「example.com」") || !tt.HasText("完成") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestAddFullAddressOnTheInstance(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	tt.Type("https://rsshub.example.com/github/issue/golang/go")
	tt.Key(0, ui.KeyEnter)
	f.settle(tt)
	if len(f.added) != 1 || f.added[0].RSSHubPath != "/github/issue/golang/go" || f.added[0].URL != "" ||
		f.added[0].Kind != "article" || f.added[0].FetchIntervalSec != 0 {
		t.Fatalf("added %+v", f.added)
	}
}

func TestAddFullAddressOnABackupInstance(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.settings.RSSHubBaseURLs = []string{"https://rsshub.example.com", "https://backup.example.com"}
	open(t, s, f, tt)
	tt.Type("https://backup.example.com/github/issue/golang/go")
	tt.Key(0, ui.KeyEnter)
	f.settle(tt)
	if len(f.added) != 1 || f.added[0].RSSHubPath != "/github/issue/golang/go" || f.added[0].URL != "" {
		t.Fatalf("added %+v", f.added)
	}
}

func TestInvalidAddress(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	tt.Type("go.dev/blog")
	tt.Key(0, ui.KeyEnter)
	if !tt.HasText(s.text.InvalidAddress) || len(f.added) != 0 || len(f.queue) != 0 {
		t.Fatalf("texts %v, added %v", tt.Texts(), f.added)
	}
	if !slices.Contains(tt.Announcements(), s.text.InvalidAddress) {
		t.Fatalf("announcements %v", tt.Announcements())
	}
	tt.Type("x")
	if tt.HasText(s.text.InvalidAddress) {
		t.Fatal("the error stays after typing")
	}

	f.addErr = &client.APIError{Status: 400, Code: "invalid_url"}
	s.add.address = "https://go.dev/x.xml"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	f.settle(tt)
	if !tt.HasText(s.text.InvalidAddress) || !s.Open() {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestExists(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	f.addErr = &client.APIError{Status: 409, Code: "source_exists"}
	tt.Type("https://go.dev/blog/feed.atom")
	tt.Key(0, ui.KeyEnter)
	f.settle(tt)
	if !tt.HasText("You already follow this as “Go Blog”.") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("Show it"); err != nil {
		t.Fatal(err)
	}
	if s.Open() || !slices.Equal(f.shown, []int64{1}) {
		t.Fatalf("open %v, shown %v", s.Open(), f.shown)
	}

	open(t, s, f, tt)
	tt.Type("https://go.dev/other.xml")
	tt.Key(0, ui.KeyEnter)
	f.settle(tt)
	if !tt.HasText(s.text.ExistsUnknown) || tt.HasText("Show it") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestNeedsBase(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.settings.RSSHubBaseURLs = nil
	open(t, s, f, tt)
	tt.Type("https://t.me/golang")
	tt.Frame()
	if !tt.HasText(s.text.NeedsBase) {
		t.Fatalf("no base prompt in %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	if !tt.HasText(s.text.BaseRequired) || !tt.Focused("RSSHub address") || len(f.queue) != 0 {
		t.Fatalf("texts %v", tt.Texts())
	}

	f.setErr = &client.APIError{Status: 400, Code: "invalid_rsshub_base_url"}
	tt.Type("rsshub")
	tt.Key(0, ui.KeyEnter)
	f.settle(tt)
	if !tt.HasText(s.text.InvalidBase) || len(f.added) != 0 {
		t.Fatalf("texts %v, added %v", tt.Texts(), f.added)
	}

	f.setErr = nil
	s.add.base = " https://rsshub.example.com/ "
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	f.settle(tt)
	if len(f.set) != 2 || (*f.set[1].RSSHubBaseURLs)[0] != "https://rsshub.example.com/" {
		t.Fatalf("settings %v", f.set)
	}
	if len(f.added) != 1 || f.added[0].RSSHubPath != "/telegram/channel/golang" || s.Open() {
		t.Fatalf("added %+v", f.added)
	}
}

func TestSettingsFail(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.settingsErr = client.ErrUnreachable
	open(t, s, f, tt)
	tt.Type("/telegram/channel/golang")
	tt.Frame()
	if tt.HasText(s.text.NeedsBase) {
		t.Fatal("asks for an instance it does not know is missing")
	}
	f.addErr = fmt.Errorf("post: %w", client.ErrUnreachable)
	tt.Key(0, ui.KeyEnter)
	f.settle(tt)
	if !tt.HasText(s.text.Unreachable) || !s.Open() {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestExpired(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	f.addErr = client.ErrUnauthorized
	tt.Type("https://go.dev/feed.xml")
	tt.Key(0, ui.KeyEnter)
	f.settle(tt)
	if s.Open() || f.expired != 1 {
		t.Fatalf("open %v, expired %d", s.Open(), f.expired)
	}
	s.Add()
	if s.Open() {
		t.Fatal("opens without a server")
	}
}

func TestEdit(t *testing.T) {
	s, f, tt := setup(t, "en")
	s.Edit(f.sources[0])
	tt.Frame()
	for _, text := range []string{"Edit subscription", "https://go.dev/blog/feed.atom",
		"Checked 3 min ago · next in 12 min", "Auto · 15 min"} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
	if !tt.Focused("Title") {
		t.Fatal("the title is not focused")
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	if s.Open() || len(f.patches[1]) != 0 {
		t.Fatalf("saving nothing sent %v", f.patches)
	}

	s.Edit(f.sources[0])
	tt.Frame()
	tt.Type(" weekly")
	if err := tt.Click("Pause fetching"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p := f.patches[1]
	if len(p) != 1 || p[0].Title == nil || *p[0].Title != "Go Blog weekly" || p[0].Paused == nil || !*p[0].Paused ||
		p[0].Kind != nil || p[0].FetchIntervalSec != nil || p[0].MuteKeywords != nil {
		t.Fatalf("patches %+v", p)
	}
	if s.Open() || len(f.changed) != 1 || f.changed[0].Title != "Go Blog weekly" {
		t.Fatalf("changed %v", f.changed)
	}
}

func TestEditServedBy(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.settings.RSSHubBaseURLs = []string{"https://rsshub.example.com", "https://backup.example.com"}
	route := client.Source{ID: 3, RSSHubPath: "/github/issue/golang/go", Kind: "article", FetchIntervalSec: 900}
	for instance, want := range map[string]string{
		"":                           "",
		"https://rsshub.example.com": "Served by rsshub.example.com",
		"https://backup.example.com": "Served by backup.example.com (backup)",
	} {
		route.RSSHubInstance = instance
		s.Edit(route)
		tt.Frame()
		f.settle(tt)
		tt.Frame()
		if want != "" && !tt.HasText(want) {
			t.Errorf("no %q in %v", want, tt.Texts())
		}
		if want == "" && tt.HasText("Served by") {
			t.Errorf("served by shown for a route not fetched: %v", tt.Texts())
		}
	}
	feed := f.sources[0]
	feed.RSSHubInstance = "https://backup.example.com"
	s.Edit(feed)
	tt.Frame()
	f.settle(tt)
	tt.Frame()
	if tt.HasText("Served by") {
		t.Errorf("served by shown for a feed: %v", tt.Texts())
	}
}

func TestEditAI(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.settings.AIBaseURL, f.settings.AIModel = "https://api.example.com/v1", "small"
	f.sources[0].PromptTranslate, f.sources[0].AutoTranslate = "Translate plainly.", true
	tt.SetSize(1000, 1100)
	s.Edit(f.sources[0])
	tt.Frame()
	f.settle(tt)
	for _, text := range []string{s.text.AIGroup, s.text.AutoSummary, s.text.AutoHelp, s.text.AutoTranslate,
		s.text.PromptSummary, s.text.PromptTranslate} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
	if tt.HasText(s.text.AIOff) || s.edit.prompts != [2]string{"", "Translate plainly."} || !s.edit.auto[1] {
		t.Fatalf("texts %v, edit %+v", tt.Texts(), s.edit)
	}
	if err := tt.Click(s.text.AutoHelp); err != nil {
		t.Fatal(err)
	}
	s.edit.prompts = [2]string{"  Summarize in one line.  ", ""}
	if err := tt.Click(s.text.Save); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p := f.patches[1]
	if len(p) != 1 || p[0].AutoSummary == nil || !*p[0].AutoSummary || p[0].AutoTranslate != nil ||
		p[0].PromptSummary == nil || *p[0].PromptSummary != "Summarize in one line." ||
		p[0].PromptTranslate == nil || *p[0].PromptTranslate != "" || p[0].Title != nil {
		t.Fatalf("patches %+v", p)
	}
}

func TestEditAIOff(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	tt.SetSize(1000, 1400)
	s.Edit(f.sources[0])
	tt.Frame()
	if tt.HasText(s.text.AIOff) {
		t.Fatal("the warning shows before the settings load")
	}
	f.settle(tt)
	if !tt.HasText(s.text.AIOff) {
		t.Fatalf("no warning in %v", tt.Texts())
	}
	if err := tt.Click(s.text.OpenSettings); err != nil {
		t.Fatal(err)
	}
	if f.opened != 1 || s.Open() || len(f.patches[1]) != 0 {
		t.Fatalf("opened %d, open %v", f.opened, s.Open())
	}

	// Settings that do not load say nothing of AI; the server queues nothing without it all the same.
	f.settingsErr = errors.New("offline")
	s.Edit(f.sources[0])
	f.settle(tt)
	if tt.HasText(s.text.AIOff) {
		t.Fatal("the warning shows without the settings")
	}
}

func TestHealth(t *testing.T) {
	s, f, _ := setup(t, "en")
	for _, c := range []struct {
		src  client.Source
		want string
		bad  bool
	}{
		{f.sources[0], "Checked 3 min ago · next in 12 min", false},
		{f.sources[1], "Failed 3 times in a row: status 503", true},
		{client.Source{LastError: "timeout", ConsecutiveFailures: 1}, "Last fetch failed: timeout", true},
		{client.Source{Paused: true, LastError: "timeout"}, s.text.Paused, false},
		{client.Source{}, "Not checked yet", false},
		{client.Source{LastFetchedAt: now.UnixMilli(), NextFetchAt: now.UnixMilli()},
			"Checked just now · next any moment", false},
		{client.Source{LastFetchedAt: now.Add(-50 * time.Hour).UnixMilli(), NextFetchAt: now.Add(3 * time.Hour).UnixMilli()},
			"Checked 2 d ago · next in 3 h", false},
	} {
		if got, bad := s.healthText(c.src, now); got != c.want || bad != c.bad {
			t.Errorf("healthText(%+v) = %q, %v; want %q, %v", c.src, got, bad, c.want, c.bad)
		}
	}
}

func TestEditGone(t *testing.T) {
	s, f, tt := setup(t, "en")
	s.Edit(client.Source{ID: 7, Title: "Old", Kind: "article", FetchIntervalSec: 900})
	tt.Frame()
	tt.Type("er")
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if s.Open() || !slices.Equal(f.removed, []int64{7}) {
		t.Fatalf("open %v, removed %v", s.Open(), f.removed)
	}
}

func TestEditFails(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.updateErr = &client.APIError{Status: 500, Code: "internal"}
	s.Edit(f.sources[0])
	tt.Frame()
	tt.Type("!")
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !s.Open() || !tt.HasText(s.text.Failed) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestMenu(t *testing.T) {
	s, f, tt := setup(t, "en")
	view := func(c *ui.Context) {
		for _, src := range f.sources {
			ui.Text(c, Name(src)).ContextMenu(func(m *ui.Menu) { s.Menu(m, src) })
		}
		s.View(c)
	}
	tt = ui.NewTester(view, 1000, 760)
	tt.Frame()
	if err := tt.RightClick("Go Blog"); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); !slices.Equal(menu, []string{"Refresh", "Mark subscription as read", "Mark older than 1 day as read",
		"Mark older than 1 week as read", "Mark older than 1 month as read", "Edit…", "Pause", "-", "Delete…"}) {
		t.Fatalf("menu %q", menu)
	}
	// Nothing unread: the older-than items are dimmed with "Mark subscription as read".
	if err := tt.ChooseMenuItem("Mark older than 1 week as read"); err == nil {
		t.Fatal("marked a subscription with nothing unread")
	}
	tt.CloseMenu()
	f.sources[0].UnreadCount = 3
	tt.Frame()
	tt.RightClick("Go Blog")
	if err := tt.ChooseMenuItem("Mark older than 1 week as read"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.older, [][2]int64{{1, 7}}) {
		t.Fatalf("older %v", f.older)
	}
	f.sources[0].UnreadCount = 0
	tt.Frame()
	tt.RightClick("Go Blog")
	if err := tt.ChooseMenuItem("Refresh"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !slices.Equal(f.fetched, []int64{1}) {
		t.Fatalf("fetched %v", f.fetched)
	}

	f.refreshErr = &client.APIError{Status: 409, Code: "source_paused"}
	s.Refresh(f.sources[0])
	f.settle(tt)
	if len(f.fetched) != 2 {
		t.Fatalf("fetched %v", f.fetched)
	}

	tt.RightClick("Go Blog")
	if err := tt.ChooseMenuItem("Pause"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if p := f.patches[1]; len(p) != 1 || p[0].Paused == nil || !*p[0].Paused || len(f.changed) != 1 {
		t.Fatalf("patches %+v", p)
	}

	tt.RightClick("/telegram/channel/golang")
	if err := tt.ChooseMenuItem("Delete…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Delete “/telegram/channel/golang”?") || !tt.HasText(s.text.DeleteBody) {
		t.Fatalf("texts %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	if s.Open() || len(f.deleted) != 0 {
		t.Fatalf("escape deleted %v", f.deleted)
	}
	s.AskDelete(f.sources[1])
	tt.Frame()
	if err := tt.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !slices.Equal(f.deleted, []int64{2}) || !slices.Equal(f.removed, []int64{2}) {
		t.Fatalf("deleted %v, removed %v", f.deleted, f.removed)
	}
}

func TestUndoDelete(t *testing.T) {
	for language, texts := range map[string][2]string{"en": {"Deleted “/telegram/channel/golang”", "Undo"}, "zh-Hans": {"已删除「/telegram/channel/golang」", "撤销"}} {
		t.Run(language, func(t *testing.T) {
			s, f, _ := setup(t, language)
			tt := ui.NewTester(func(c *ui.Context) { s.View(c); theme.Toasts(c) }, 1000, 760)
			tt.Frame()
			s.AskDelete(f.sources[1])
			tt.Frame()
			if err := tt.Click(s.text.ConfirmDelete); err != nil {
				t.Fatal(err)
			}
			f.settle(tt)
			tt.Frame()
			if !tt.HasText(texts[0]) || !tt.HasText(texts[1]) {
				t.Fatalf("no Deleted toast with Undo in %v", tt.Texts())
			}
			if err := tt.Click(texts[1]); err != nil {
				t.Fatal(err)
			}
			f.settle(tt)
			if !slices.Equal(f.restoredIDs, []int64{2}) || len(f.restored) != 1 || f.restored[0].ID != 2 {
				t.Fatalf("restore asked for %v, the timeline got %v", f.restoredIDs, f.restored)
			}
		})
	}
}

func TestUndoDeleteTooLate(t *testing.T) {
	s, f, _ := setup(t, "en")
	tt := ui.NewTester(func(c *ui.Context) { s.View(c); theme.Toasts(c) }, 1000, 760)
	tt.Frame()
	s.AskDelete(f.sources[1])
	tt.Frame()
	if err := tt.Click(s.text.ConfirmDelete); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	tt.Frame()
	f.restoreErr = &client.APIError{Status: 404, Code: "not_found"}
	if err := tt.Click("Undo"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	tt.Frame()
	if len(f.restored) != 0 || !tt.HasText(s.text.UndoFailed) {
		t.Fatalf("restored %v, texts %v", f.restored, tt.Texts())
	}
}

func TestTryAgain(t *testing.T) {
	s, f, _ := setup(t, "en")
	src := f.sources[1]
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Text(c, Name(src)).ContextMenu(func(m *ui.Menu) { s.Menu(m, src) })
		s.View(c)
	}, 1000, 760)
	tt.Frame()
	tt.RightClick(Name(src))
	if menu := tt.Menu(); len(menu) == 0 || menu[0] != "Try again" {
		t.Fatalf("menu %q", menu)
	}
	if err := tt.ChooseMenuItem("Try again"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !slices.Equal(f.fetched, []int64{2}) {
		t.Fatalf("fetched %v", f.fetched)
	}

	s.Edit(src)
	tt.Frame()
	if _, ok := tt.Find("Try again"); !ok {
		t.Fatalf("no Try again in %v", tt.Texts())
	}
	if err := tt.Click("Try again"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !slices.Equal(f.fetched, []int64{2, 2}) {
		t.Fatalf("fetched %v", f.fetched)
	}

	tt.Key(0, ui.KeyEscape)
	s.Edit(f.sources[0])
	tt.Frame()
	if _, ok := tt.Find("Refresh"); !ok || tt.HasText("Try again") {
		t.Fatalf("a healthy subscription reads %v", tt.Texts())
	}
}

func TestPausedMenu(t *testing.T) {
	s, f, _ := setup(t, "en")
	src := client.Source{ID: 1, Title: "Go Blog", Paused: true}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Text(c, Name(src)).ContextMenu(func(m *ui.Menu) { s.Menu(m, src) })
		s.View(c)
	}, 600, 400)
	tt.Frame()
	tt.RightClick("Go Blog")
	if menu := tt.Menu(); !slices.Contains(menu, "Resume") {
		t.Fatalf("menu %q", menu)
	}
	tt.ChooseMenuItem("Refresh")
	f.settle(tt)
	if len(f.fetched) != 0 {
		t.Fatal("refreshed a paused subscription")
	}
}

func TestChinese(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	open(t, s, f, tt)
	if !tt.HasText("添加订阅") || !tt.HasText(s.text.AddressHelp) {
		t.Fatalf("texts %v", tt.Texts())
	}
	s.add.open = false
	s.Edit(f.sources[0])
	tt.Frame()
	if !tt.HasText(s.text.EditTitle) || !tt.HasText("3 分钟前检查过 · 12 分钟后再查") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestEditAutoInterval(t *testing.T) {
	s, f, tt := setup(t, "en")
	s.Edit(f.sources[0])
	tt.Frame()
	if s.edit.interval != 0 {
		t.Fatalf("interval choice %d for an automatic subscription", s.edit.interval)
	}
	if err := tt.Click("Auto · 15 min"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("6 h"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p := f.patches[1]
	if len(p) != 1 || p[0].FetchIntervalSec == nil || *p[0].FetchIntervalSec != 6*3600 || p[0].FetchIntervalAuto != nil {
		t.Fatalf("patches %+v", p)
	}

	s.Edit(f.sources[1])
	tt.Frame()
	if err := tt.Click("1 h"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Auto"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p = f.patches[2]
	if len(p) != 1 || p[0].FetchIntervalAuto == nil || !*p[0].FetchIntervalAuto || p[0].FetchIntervalSec != nil {
		t.Fatalf("patches %+v", p)
	}
}

func TestEditKindSuggestion(t *testing.T) {
	s, f, tt := setup(t, "en")
	src := f.sources[0]
	src.SuggestedKind = "video"
	s.Edit(src)
	tt.Frame()
	for _, text := range []string{"Most entries in this feed are videos.", "Show as Videos"} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
	if s.edit.kind != slices.Index(kinds, "article") {
		t.Fatalf("the suggestion changed the kind to %d", s.edit.kind)
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	if s.Open() || len(f.patches[1]) != 0 {
		t.Fatalf("saving without choosing sent %v", f.patches)
	}

	s.Edit(src)
	tt.Frame()
	if err := tt.Click("Show as Videos"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("Most entries in this feed are videos.") {
		t.Error("the suggestion stays once taken")
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p := f.patches[1]
	if len(p) != 1 || p[0].Kind == nil || *p[0].Kind != "video" {
		t.Fatalf("patches %+v", p)
	}

	src.Kind = "video"
	s.Edit(src)
	tt.Frame()
	if tt.HasText("Most entries in this feed are videos.") {
		t.Error("suggests the kind it already is")
	}
}

func TestEditSuggestionChinese(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	src := f.sources[0]
	src.SuggestedKind = "image"
	s.Edit(src)
	tt.Frame()
	for _, text := range []string{"这个订阅的条目大多是图片。", "显示为图片", "自动 · 15 分钟"} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
}

// flagged is what the server lists for the health dialog: the first two subscriptions of setup and one more.
func flagged() client.Health {
	return client.Health{WindowDays: 30, Sources: []client.SourceHealth{
		{ID: 1, Title: "Go Blog", Kind: "article", State: client.HealthDead, Cause: client.HealthCauseSilent, Days: 143},
		{ID: 2, RSSHubPath: "/telegram/channel/golang", Kind: "social", State: client.HealthFailing, ConsecutiveFailures: 5,
			LastError: "status 503", Recent: 48, RecentRead: 6},
		{ID: 3, Title: "Firehose", Kind: "article", State: client.HealthRarelyRead, Recent: 120, RecentRead: 3, RecentStarred: 1},
		{ID: 4, Title: "Haiku", Kind: "article", State: client.HealthDead, Cause: "failing", Days: 41,
			ConsecutiveFailures: 9, LastError: "HTTP 404"},
	}}
}

func TestHealthDialog(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.health = flagged()
	s.Health()
	tt.Frame()
	if !s.Open() || !slices.Contains(tt.Texts(), "Checking…") {
		t.Fatalf("loading: open %v, texts %v", s.Open(), tt.Texts())
	}
	f.settle(tt)
	for _, want := range []string{
		"No new entries for 143 days", "Last 30 days: 0 new, 0 read, 0 starred",
		"Failed 5 times in a row: status 503", "Last 30 days: 48 new, 6 read, 0 starred",
		"Read 3 of 120 new entries in the last 30 days", "Last 30 days: 120 new, 3 read, 1 starred",
		"Failing for 41 days: HTTP 404",
	} {
		if !slices.Contains(tt.Texts(), want) {
			t.Errorf("no %q in %v", want, tt.Texts())
		}
	}
	if err := tt.Click("Pause Go Blog"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if len(f.patches[1]) != 1 || f.patches[1][0].Paused == nil || !*f.patches[1][0].Paused || len(f.changed) != 1 {
		t.Fatalf("patches %v, changed %v", f.patches, f.changed)
	}
	if slices.Contains(tt.Texts(), "No new entries for 143 days") || len(s.report.items) != 3 {
		t.Fatalf("the paused subscription is still listed: %v", tt.Texts())
	}
	if err := tt.Click("Unsubscribe from Haiku"); err != nil {
		t.Fatal(err)
	}
	if !s.deleting || len(f.deleted) != 0 {
		t.Fatalf("deleting %v, deleted %v", s.deleting, f.deleted)
	}
	tt.Key(0, ui.KeyEscape)
	if !s.report.open || len(f.deleted) != 0 || len(s.report.items) != 3 {
		t.Fatalf("cancel: open %v, deleted %v", s.report.open, f.deleted)
	}
	if err := tt.Click("Unsubscribe from Haiku"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !slices.Equal(f.deleted, []int64{4}) || len(s.report.items) != 2 || !s.report.open {
		t.Fatalf("deleted %v, items %v, open %v", f.deleted, s.report.items, s.report.open)
	}
	if err := tt.Click(s.text.Close); err != nil {
		t.Fatal(err)
	}
	if s.Open() {
		t.Fatal("Close leaves the dialog open")
	}
}

func TestHealthDialogEmpty(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.health = client.Health{WindowDays: 30}
	s.Health()
	f.settle(tt)
	if !slices.Contains(tt.Texts(), "All your subscriptions look healthy") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestHealthDialogFails(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.healthErr = client.ErrUnreachable
	s.Health()
	f.settle(tt)
	if !slices.Contains(tt.Texts(), s.text.HealthError) {
		t.Fatalf("texts %v", tt.Texts())
	}
	f.healthErr, f.health = nil, flagged()
	if err := tt.Click(s.text.Retry); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !slices.Contains(tt.Texts(), "Checking…") {
		t.Fatalf("texts %v", tt.Texts())
	}
	f.settle(tt)
	if !slices.Contains(tt.Texts(), "No new entries for 143 days") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestHealthDialogExpired(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.healthErr = client.ErrUnauthorized
	s.Health()
	f.settle(tt)
	if s.Open() || f.expired != 1 {
		t.Fatalf("open %v, expired %d", s.Open(), f.expired)
	}
	s.Health()
	if s.Open() {
		t.Fatal("opens without a server")
	}
}

func TestHealthDialogDropsAnAnswerFromAnotherServer(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.health = flagged()
	s.Health()
	s.Use(f)
	f.settle(tt)
	if s.Open() || len(s.report.items) != 0 {
		t.Fatalf("open %v, items %v", s.Open(), s.report.items)
	}
}

func TestHealthDialogChinese(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	f.health = flagged()
	s.Health()
	f.settle(tt)
	for _, want := range []string{"已有 143 天没有新条目", "已连续失败 5 次：status 503", "近 30 天新增 120 条，只读了 3 条",
		"已连续失败 41 天：HTTP 404", "近 30 天：新增 48 条，已读 6 条，收藏 0 条"} {
		if !slices.Contains(tt.Texts(), want) {
			t.Errorf("no %q in %v", want, tt.Texts())
		}
	}
}
