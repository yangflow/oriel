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
)

var now = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// fake stands in for the server, the timeline and the background: work waits in a queue until step runs it.
type fake struct {
	queue    []func() func()
	sources  []client.Source
	settings client.Settings

	added   []client.NewSource
	patches map[int64][]client.SourcePatch
	deleted []int64
	fetched []int64
	set     []client.SettingsPatch

	addErr, updateErr, deleteErr, refreshErr, settingsErr, setErr error

	changed []client.Source
	removed []int64
	shown   []int64
	marked  []int64
	opened  int // the AI settings
	expired int
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

func (f *fake) RefreshSource(_ context.Context, id int64) error {
	f.fetched = append(f.fetched, id)
	return f.refreshErr
}

func (f *fake) Settings(context.Context) (client.Settings, error) {
	return f.settings, f.settingsErr
}

func (f *fake) SetSettings(_ context.Context, patch client.SettingsPatch) (client.Settings, error) {
	f.set = append(f.set, patch)
	if f.setErr != nil {
		return client.Settings{}, f.setErr
	}
	f.settings.RSSHubBaseURL = *patch.RSSHubBaseURL
	return f.settings, nil
}

func (f *fake) deps() Deps {
	return Deps{
		Go:           func(work func() func()) { f.queue = append(f.queue, work) },
		Now:          func() time.Time { return now },
		Sources:      func() []client.Source { return f.sources },
		Added:        func(src client.Source) { f.sources = append(f.sources, src) },
		Changed:      func(src client.Source) { f.changed = append(f.changed, src) },
		Removed:      func(id int64) { f.removed = append(f.removed, id) },
		Show:         func(id int64) { f.shown = append(f.shown, id) },
		MarkRead:     func(src client.Source) { f.marked = append(f.marked, src.ID) },
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
				FetchIntervalSec: 900, LastFetchedAt: now.Add(-3 * time.Minute).UnixMilli(),
				NextFetchAt: now.Add(12 * time.Minute).UnixMilli()},
			{ID: 2, RSSHubPath: "/telegram/channel/golang", Kind: "social", FetchIntervalSec: 3600,
				LastError: "status 503", ConsecutiveFailures: 3},
		},
		settings: client.Settings{RSSHubBaseURL: "https://rsshub.example.com", RetentionDays: 30},
		patches:  map[int64][]client.SourcePatch{},
	}
	s := New(language, f.deps())
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
	if err := tt.Click("15 min"); err != nil {
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

func TestAddFullAddressOnTheInstance(t *testing.T) {
	s, f, tt := setup(t, "en")
	open(t, s, f, tt)
	tt.Type("https://rsshub.example.com/github/issue/golang/go")
	tt.Key(0, ui.KeyEnter)
	f.settle(tt)
	if len(f.added) != 1 || f.added[0].RSSHubPath != "/github/issue/golang/go" || f.added[0].URL != "" ||
		f.added[0].Kind != "article" || f.added[0].FetchIntervalSec != 900 {
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
	s.add.address = "https://go.dev/x"
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
	f.settings.RSSHubBaseURL = ""
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
	if len(f.set) != 2 || *f.set[1].RSSHubBaseURL != "https://rsshub.example.com/" {
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
	tt.Type("https://go.dev/feed")
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
		"Checked 3 min ago · next in 12 min", "15 min"} {
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
	if menu := tt.Menu(); !slices.Equal(menu, []string{"Refresh", "Mark subscription as read", "Edit…", "Pause", "-", "Delete…"}) {
		t.Fatalf("menu %q", menu)
	}
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
