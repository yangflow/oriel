package settings

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/prefs"
	"github.com/yangflow/oriel/internal/ui/theme"
)

var now = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

const (
	summaryPrompt   = "Summarize in {language}."
	translatePrompt = "Translate into {language}."
)

// fake stands in for the server, the file dialogs and the background: work waits in a queue until settle runs it.
type fake struct {
	prefs    prefs.Preferences
	fonts    []string
	changes  []prefs.Preferences
	queue    []func() func()
	settings client.Settings
	devices  []client.Device
	imported client.Imported
	opml     []byte

	set     []client.SettingsPatch
	revoked []int64
	sent    [][]byte

	settingsErr, setErr, devicesErr, revokeErr, importErr, exportErr, testErr error

	model string // the model that answers a test
	tests int

	open, save string // the paths the dialogs answer, "" for cancelled
	filters    []string
	back       int
	refreshed  int
	expired    int
}

func (f *fake) Settings(context.Context) (client.Settings, error) { return f.settings, f.settingsErr }

func (f *fake) SetSettings(_ context.Context, patch client.SettingsPatch) (client.Settings, error) {
	f.set = append(f.set, patch)
	if f.setErr != nil {
		return client.Settings{}, f.setErr
	}
	if patch.RSSHubBaseURL != nil {
		f.settings.RSSHubBaseURL = strings.TrimSuffix(*patch.RSSHubBaseURL, "/")
	}
	if patch.RetentionDays != nil {
		f.settings.RetentionDays = *patch.RetentionDays
	}
	if patch.AIBaseURL != nil {
		f.settings.AIBaseURL = strings.TrimSuffix(*patch.AIBaseURL, "/")
	}
	if patch.AIModel != nil {
		f.settings.AIModel = *patch.AIModel
	}
	if patch.AIAPIKey != nil {
		f.settings.AIAPIKeySet = *patch.AIAPIKey != ""
	}
	if patch.AILanguage != nil {
		f.settings.AILanguage = *patch.AILanguage
	}
	if patch.PromptSummary != nil {
		f.settings.PromptSummary = cmp.Or(*patch.PromptSummary, f.settings.DefaultPromptSummary)
	}
	if patch.PromptTranslate != nil {
		f.settings.PromptTranslate = cmp.Or(*patch.PromptTranslate, f.settings.DefaultPromptTranslate)
	}
	return f.settings, nil
}

func (f *fake) Devices(context.Context) ([]client.Device, error) { return f.devices, f.devicesErr }

func (f *fake) RevokeDevice(_ context.Context, id int64) error {
	f.revoked = append(f.revoked, id)
	return f.revokeErr
}

func (f *fake) ImportOPML(_ context.Context, opml []byte) (client.Imported, error) {
	f.sent = append(f.sent, opml)
	return f.imported, f.importErr
}

func (f *fake) ExportOPML(context.Context) ([]byte, error) { return f.opml, f.exportErr }

func (f *fake) TestAI(context.Context) (string, error) {
	f.tests++
	return f.model, f.testErr
}

func (f *fake) deps() Deps {
	return Deps{
		Go:  func(work func() func()) { f.queue = append(f.queue, work) },
		Now: func() time.Time { return now },
		Open: func(filter string) (string, error) {
			f.filters = append(f.filters, filter)
			return f.open, nil
		},
		Save: func(name, filter string) (string, error) {
			f.filters = append(f.filters, filter)
			return f.save, nil
		},
		Back:        func() { f.back++ },
		Imported:    func() { f.refreshed++ },
		Expired:     func() { f.expired++ },
		Preferences: func() prefs.Preferences { return f.prefs },
		Fonts:       func() []string { return f.fonts },
		Change: func(p prefs.Preferences) {
			f.prefs = p
			f.changes = append(f.changes, p)
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

func setup(t *testing.T, language string, signedIn bool) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	f := &fake{
		settings: client.Settings{RSSHubBaseURL: "https://rsshub.example.com", RetentionDays: 30, AILanguage: "zh-Hans",
			PromptSummary: summaryPrompt, PromptTranslate: translatePrompt,
			DefaultPromptSummary: summaryPrompt, DefaultPromptTranslate: translatePrompt},
		devices: []client.Device{
			{ID: 4, UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 " +
				"(KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1",
				CreatedAt: now.AddDate(0, -1, 0).UnixMilli(), LastSeenAt: now.Add(-3 * time.Hour).UnixMilli()},
			{ID: 7, UserAgent: "Oriel/0.9.0 (macOS)", CreatedAt: now.AddDate(0, 0, -2).UnixMilli(),
				LastSeenAt: now.UnixMilli(), Current: true},
			{ID: 2, UserAgent: "", CreatedAt: now.AddDate(-1, 0, 0).UnixMilli(),
				LastSeenAt: now.AddDate(0, 0, -40).UnixMilli()},
		},
	}
	s := New(language, f.deps())
	s.Use(f, "reader.example.com", signedIn)
	s.Open()
	tt := ui.NewTester(s.View, 1000, 760)
	tt.Frame()
	f.settle(tt)
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
	if MenuLabel("zh-Hans-CN") != "设置…" || MenuLabel("fr") != "Settings…" {
		t.Fatalf("menu %q %q", MenuLabel("zh-Hans-CN"), MenuLabel("fr"))
	}
}

func TestShows(t *testing.T) {
	_, _, tt := setup(t, "en", true)
	for _, text := range []string{"Fetching", "Import and export", "Devices", "reader.example.com", "RSSHub instance",
		"Keep entries", "1 month"} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
}

// clickBase clicks the RSSHub instance field, left of its Save button; its label is also the setting's heading.
func clickBase(t *testing.T, s *Screen, tt *ui.Tester) {
	t.Helper()
	r, ok := tt.Find(s.text.Save)
	if !ok {
		t.Fatalf("no Save in %v", tt.Texts())
	}
	tt.ClickAt(r.X-40, r.Y+r.H/2)
}

func TestLoadFails(t *testing.T) {
	f := &fake{settingsErr: errors.New("offline")}
	s := New("en", f.deps())
	s.Use(f, "", false)
	s.Open()
	tt := ui.NewTester(s.View, 1000, 760)
	tt.Frame()
	if !tt.HasText(s.text.Loading) {
		t.Fatalf("no skeleton in %v", tt.Texts())
	}
	f.settle(tt)
	if !tt.HasText(s.text.Error) || !slices.Contains(tt.Announcements(), s.text.Error) {
		t.Fatalf("texts %v", tt.Texts())
	}
	f.settingsErr = nil
	f.settings = client.Settings{RetentionDays: 0}
	if err := tt.Click(s.text.Retry); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if tt.HasText(s.text.Error) || !tt.HasText("Forever") || !tt.HasText("This computer") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestSaveBase(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	if err := tt.Click(s.text.Save); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if len(f.set) != 0 {
		t.Fatalf("saved an unchanged instance: %v", f.set)
	}
	clickBase(t, s, tt)
	tt.Type("/")
	tt.Key(0, ui.KeyEnter)
	f.settle(tt)
	if len(f.set) != 1 || *f.set[0].RSSHubBaseURL != "https://rsshub.example.com/" {
		t.Fatalf("set %v", f.set)
	}
	if !tt.HasText(s.text.Saved) || s.base != "https://rsshub.example.com" {
		t.Fatalf("texts %v, base %q", tt.Texts(), s.base)
	}
}

func TestInvalidBase(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans", true)
	f.setErr = &client.APIError{Status: 400, Code: "invalid_rsshub_base_url"}
	clickBase(t, s, tt)
	tt.Type("x")
	if err := tt.Click(s.text.Save); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	tt.Frame()
	if !tt.HasText(s.text.InvalidBase) || !tt.Focused(s.text.RSSHub) {
		t.Fatalf("texts %v", tt.Texts())
	}
	tt.Type("y")
	if tt.HasText(s.text.InvalidBase) {
		t.Fatal("the error stays after typing")
	}
	f.setErr = errors.New("offline")
	tt.Key(0, ui.KeyEnter)
	f.settle(tt)
	if !tt.HasText(s.text.SaveFailed) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestRetention(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	if err := tt.Click("1 month"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("3 months"); err != nil {
		t.Fatalf("%v in %v", err, tt.Texts())
	}
	f.settle(tt)
	if len(f.set) != 1 || *f.set[0].RetentionDays != 90 || !tt.HasText(s.text.RetentionSaved) {
		t.Fatalf("set %v, texts %v", f.set, tt.Texts())
	}

	f.setErr = errors.New("offline")
	if err := tt.Click("3 months"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Forever"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !tt.HasText(s.text.SaveFailed) || !tt.HasText("3 months") || s.retention != 90 {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestRetentionOutsidePresets(t *testing.T) {
	s, _, tt := setup(t, "en", true)
	s.settings.RetentionDays, s.retention = 45, 45
	tt.Frame()
	if err := tt.Click("45 days"); err != nil {
		t.Fatal(err)
	}
	want := []string{"Forever", "1 week", "1 month", "45 days", "3 months", "6 months", "1 year"}
	if texts := tt.Texts(); !slices.Equal(texts[len(texts)-len(want):], want) {
		t.Fatalf("texts %v", texts)
	}
}

func writeOPML(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImport(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	f.open = writeOPML(t, "feeds.opml", "<opml/>")
	f.imported = client.Imported{Added: 3, Existing: 1, Failed: []client.ImportFail{
		{Title: "Broken", Address: "ftp://example.com/feed", Error: "invalid_url"},
		{Address: "x", Error: "something_new"},
	}}
	if err := tt.Click(s.text.OPML); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(s.text.Import); err != nil {
		t.Fatal(err)
	}
	// Choosing the file runs before the import, which runs while the screen shows it.
	work := f.queue[0]
	f.queue = f.queue[1:]
	work()()
	tt.Frame()
	if !tt.HasText("Importing feeds.opml") {
		t.Fatalf("texts %v", tt.Texts())
	}
	f.settle(tt)
	if len(f.sent) != 1 || string(f.sent[0]) != "<opml/>" || f.filters[0] != s.text.OPMLFiles {
		t.Fatalf("sent %q, filters %v", f.sent, f.filters)
	}
	want := "Imported feeds.opml: 3 added, 1 already subscribed, 2 could not be added."
	for _, text := range []string{want, "ftp://example.com/feed", "Not an http or https address", "something_new"} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
	if f.refreshed != 1 {
		t.Fatalf("refreshed %d", f.refreshed)
	}
	if err := tt.Click(s.text.Dismiss); err != nil {
		t.Fatal(err)
	}
	if tt.HasText(want) {
		t.Fatal("the result stays after dismissing")
	}
}

func TestImportRefused(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	s.group = opml
	big := writeOPML(t, "big.opml", strings.Repeat("x", client.MaxOPML+1))
	for _, c := range []struct {
		path string
		err  error
		want string
	}{
		{big, nil, "big.opml is larger than 1 MB."},
		{filepath.Join(t.TempDir(), "gone.opml"), nil, "gone.opml could not be read."},
		{writeOPML(t, "page.html", "<html/>"), &client.APIError{Status: 400, Code: "invalid_opml"},
			"page.html is not an OPML file. Nothing was added."},
		{writeOPML(t, "a.opml", "<opml/>"), errors.New("offline"), "Couldn’t import a.opml."},
	} {
		f.open, f.importErr, f.sent = c.path, c.err, nil
		tt.Frame()
		if err := tt.Click(s.text.Import); err != nil {
			t.Fatal(err)
		}
		f.settle(tt)
		if !tt.HasText(c.want) {
			t.Errorf("no %q in %v", c.want, tt.Texts())
		}
		if c.path == big && len(f.sent) != 0 {
			t.Error("sent a file over the limit")
		}
	}
	if f.refreshed != 0 {
		t.Fatalf("refreshed %d", f.refreshed)
	}
}

func TestImportCancelled(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	s.group = opml
	tt.Frame()
	if err := tt.Click(s.text.Import); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if s.busy || len(f.sent) != 0 || tt.HasText("Imported") {
		t.Fatalf("busy %v, sent %v", s.busy, f.sent)
	}
}

func TestExport(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	s.group = opml
	f.save = filepath.Join(t.TempDir(), "mine.opml")
	f.opml = []byte(`<?xml version="1.0"?><opml version="2.0"><body>` +
		`<outline text="Tech"><outline text="Go" xmlUrl="https://go.dev/blog/feed.atom"/></outline>` +
		`<outline text="Go issues" orielRsshubPath="/github/issue/golang/go"/></body></opml>`)
	tt.Frame()
	if err := tt.Click(s.text.Export); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	written, err := os.ReadFile(f.save)
	if err != nil || string(written) != string(f.opml) {
		t.Fatalf("written %q, %v", written, err)
	}
	if !tt.HasText("Exported 2 subscriptions to mine.opml.") {
		t.Fatalf("texts %v", tt.Texts())
	}

	f.save = filepath.Join(t.TempDir(), "missing", "mine.opml")
	if err := tt.Click(s.text.Export); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !tt.HasText("mine.opml could not be saved.") {
		t.Fatalf("texts %v", tt.Texts())
	}
	f.exportErr = errors.New("offline")
	if err := tt.Click(s.text.Export); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !tt.HasText(s.text.ExportFailed) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestDevices(t *testing.T) {
	s, _, tt := setup(t, "en", true)
	if err := tt.Click(s.text.Devices); err != nil {
		t.Fatal(err)
	}
	// 1000 wide leaves the main area room for the table.
	texts := tt.Texts()
	for _, text := range []string{
		s.text.ColDevice, s.text.ColPlatform, s.text.ColSigned, s.text.ColLastUsed,
		"Oriel", s.text.ThisDevice, "macOS", "Oct 16", "just now",
		"Safari", "iPhone", "Sep 18", "3 hours ago",
		"Unknown device", "Oct 18, 2025", "Sep 8",
	} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, texts)
		}
	}
	current := slices.Index(texts, "Oriel")
	if current < 0 || texts[current+1] != s.text.ThisDevice {
		t.Fatalf("the current device is not first and marked: %v", texts)
	}
	if n := strings.Count(strings.Join(texts, "\n"), s.text.Revoke); n != 3 {
		t.Fatalf("%d revoke buttons in %v", n, texts)
	}
}

// rect finds a text's place, or fails.
func rect(t *testing.T, tt *ui.Tester, text string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(text)
	if !ok {
		t.Fatalf("no %q in %v", text, tt.Texts())
	}
	return r
}

func TestDevicesTable(t *testing.T) {
	s, _, tt := setup(t, "en", true)
	s.group = devices
	tt.Frame()
	platform, signed, last := rect(t, tt, s.text.ColPlatform), rect(t, tt, s.text.ColSigned), rect(t, tt, s.text.ColLastUsed)
	for text, head := range map[string]ui.Rect{"macOS": platform, "iPhone": platform, "Oct 16": signed, "Sep 18": signed} {
		if r := rect(t, tt, text); r.X != head.X {
			t.Errorf("%q at %v, its column at %v", text, r.X, head.X)
		}
	}
	// Last used ends at the column's right edge, and Revoke is the last column.
	if r := rect(t, tt, "3 hours ago"); r.X+r.W != last.X+last.W {
		t.Errorf("last used ends at %v, want %v", r.X+r.W, last.X+last.W)
	}
	if b := rect(t, tt, "Revoke “Safari · iPhone”?"); b.X < last.X+last.W || b.W == 0 {
		t.Errorf("revoke at %v, last used ends at %v", b, last.X+last.W)
	}
	if a, b := rect(t, tt, "Safari"), rect(t, tt, "Unknown device"); a.X != b.X || a.Y >= b.Y {
		t.Errorf("devices at %v and %v", a, b)
	}
}

func TestDevicesStacked(t *testing.T) {
	s, _, tt := setup(t, "en", true)
	s.group = devices
	// 840 wide leaves the main area 616, short of a table.
	tt.SetSize(840, 700)
	tt.Frame()
	texts := tt.Texts()
	current := slices.Index(texts, "Oriel · macOS")
	if current < 0 || texts[current+1] != s.text.ThisDevice {
		t.Fatalf("the current device is not first and marked: %v", texts)
	}
	for _, text := range []string{
		"Signed in Oct 16 · last used just now",
		"Safari · iPhone", "Signed in Sep 18 · last used 3 hours ago",
		"Unknown device", "Signed in Oct 18, 2025 · last used Sep 8",
	} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, texts)
		}
	}
	if tt.HasText(s.text.ColPlatform) {
		t.Errorf("a table at 840 wide: %v", texts)
	}
	tt.SetSize(640, 600)
	tt.Frame()
	if !tt.HasText("Safari · iPhone") || tt.HasText(s.text.ColPlatform) {
		t.Errorf("minimum window: %v", tt.Texts())
	}
}

func TestRevoke(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	s.group = devices
	tt.Frame()
	if err := tt.Click("Revoke “Safari · iPhone”?"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(s.text.RevokeBody) {
		t.Fatalf("no dialog in %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	if f.back != 0 || len(f.revoked) != 0 {
		t.Fatalf("back %d, revoked %v", f.back, f.revoked)
	}
	if err := tt.Click("Revoke “Unknown device”?"); err != nil {
		t.Fatal(err)
	}
	f.revokeErr = errors.New("offline")
	if err := tt.Click(s.text.RevokeConfirm); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !tt.HasText("Couldn’t revoke Unknown device.") || !tt.HasText("Unknown device") {
		t.Fatalf("texts %v", tt.Texts())
	}
	tt.Frame()
	f.revokeErr = &client.APIError{Status: 404, Code: "not_found"}
	if err := tt.Click("Revoke “Unknown device”?"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(s.text.RevokeConfirm); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !slices.Equal(f.revoked, []int64{2, 2}) || !tt.HasText("Unknown device is signed out.") {
		t.Fatalf("revoked %v, texts %v", f.revoked, tt.Texts())
	}
	if len(s.devices) != 2 {
		t.Fatalf("devices %v", s.devices)
	}
}

func TestLocalDevices(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans", false)
	if err := tt.Click(s.text.Devices); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(s.text.LocalDevices) || tt.HasText(s.text.ThisDevice) {
		t.Fatalf("texts %v", tt.Texts())
	}
	if len(f.queue) != 0 {
		t.Fatal("loaded devices without a session")
	}
}

func TestDeviceNames(t *testing.T) {
	s := New("en", Deps{})
	for _, c := range []struct{ ua, want string }{
		{"Oriel/0.9.0 (Windows)", "Oriel · Windows"},
		{"Oriel/dev", "Oriel"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 " +
			"Safari/537.36 Edg/130.0", "Edge · Windows"},
		{"Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0", "Firefox · Linux"},
		{"Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 Chrome/130.0 Mobile Safari/537.36", "Chrome · Android"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/18.0 Safari/605.1.15",
			"Safari · macOS"},
		{"curl/8.7.1", "curl/8.7.1"},
	} {
		ua, want := c.ua, c.want
		if got, _ := s.device(client.Device{UserAgent: ua}); got != want {
			t.Errorf("%q is %q, want %q", ua, got, want)
		}
	}
}

func TestExpired(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	f.setErr = client.ErrUnauthorized
	if err := tt.Click("1 month"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("1 year"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if f.expired != 1 || s.api != nil {
		t.Fatalf("expired %d", f.expired)
	}
}

func TestStaleServer(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	s.Open()
	other := &fake{settings: client.Settings{RSSHubBaseURL: "https://other.example.com", RetentionDays: 7}}
	s.Use(other, "", false)
	s.Open()
	f.settle(tt)
	if s.settings.RetentionDays != 7 || !tt.HasText("This computer") {
		t.Fatalf("settings %+v, texts %v", s.settings, tt.Texts())
	}
}

func TestBack(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	tt.Key(0, ui.KeyEscape)
	if err := tt.Click(s.text.BackTimeline); err != nil {
		t.Fatal(err)
	}
	if f.back != 2 {
		t.Fatalf("back %d", f.back)
	}
}

func TestAIColumns(t *testing.T) {
	s, _, tt := openAI(t, "en", true)
	// 1100 wide leaves the main area 876, short of two columns: Prompts is under Connection.
	tt.SetSize(1100, 1400)
	tt.Frame()
	conn, prompts := rect(t, tt, s.text.Connection), rect(t, tt, s.text.Prompts)
	if prompts.Y <= conn.Y || prompts.X != conn.X {
		t.Errorf("one column: connection %v, prompts %v", conn, prompts)
	}
	// 1300 wide leaves 1076: Prompts is beside Connection, and the page is centered.
	tt.SetSize(1300, 900)
	tt.Frame()
	conn, prompts = rect(t, tt, s.text.Connection), rect(t, tt, s.text.Prompts)
	if prompts.Y != conn.Y || prompts.X <= conn.X {
		t.Errorf("two columns: connection %v, prompts %v", conn, prompts)
	}
	// The lead stops at 600, scaled with the text size, short of the two columns under it.
	if lead := rect(t, tt, s.text.AILead); lead.X != conn.X || lead.W >= 2*conn.W {
		t.Errorf("lead %v, connection %v", lead, conn)
	}
	// At the minimum window the single column keeps its Connection heading.
	tt.SetSize(640, 600)
	tt.Frame()
	if !tt.HasText(s.text.Connection) || !tt.HasText(s.text.Prompts) {
		t.Errorf("minimum window: %v", tt.Texts())
	}
}

func TestNarrow(t *testing.T) {
	s, _, tt := setup(t, "en", true)
	tt.SetSize(640, 600)
	tt.Frame()
	if !tt.HasText(s.text.RSSHub) || !tt.HasText(s.text.BackTimeline) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

// openAI shows the AI group, with the endpoint saved when configured.
func openAI(t *testing.T, language string, configured bool) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	s, f, tt := setup(t, language, true)
	if configured {
		f.settings.AIBaseURL, f.settings.AIModel, f.settings.AIAPIKeySet = "https://api.example.com/v1", "small", true
	}
	f.model = "small"
	s.OpenAI()
	f.settle(tt)
	return s, f, tt
}

func TestOpenImport(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	s.OpenImport()
	f.settle(tt)
	if s.group != opml || !tt.HasText("Import and export") || !tt.HasText(s.text.Import) {
		t.Fatalf("group %d, texts %v", s.group, tt.Texts())
	}
}

// press clicks the element showing or labeled label.
func press(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatalf("%v in %v", err, tt.Texts())
	}
}

// edit changes the AI settings as typed, as the fields do between frames.
func edit(tt *ui.Tester, change func()) {
	change()
	tt.Frame()
}

func TestAIShows(t *testing.T) {
	s, f, tt := openAI(t, "en", false)
	if s.group != ai {
		t.Fatalf("group %d", s.group)
	}
	for _, text := range []string{s.text.AILead, s.text.Endpoint, s.text.Model, s.text.KeyUnset, "简体中文",
		s.text.PromptSummary, s.text.Reset} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
	if tt.HasText(s.text.RemoveKey) || s.prompts != [2]string{summaryPrompt, translatePrompt} {
		t.Fatalf("texts %v, prompts %q", tt.Texts(), s.prompts)
	}
	press(t, tt, s.text.Test)
	f.settle(tt)
	if f.tests != 0 {
		t.Fatal("tested an endpoint not set up")
	}
}

func TestSaveAI(t *testing.T) {
	s, f, tt := openAI(t, "en", false)
	edit(tt, func() { s.aiBase, s.aiModel, s.aiKey = "https://api.example.com/v1/", " small ", "sk-test" })
	press(t, tt, "简体中文")
	press(t, tt, "English")
	press(t, tt, s.text.Save)
	f.settle(tt)
	if len(f.set) != 1 {
		t.Fatalf("set %v", f.set)
	}
	p := f.set[0]
	if *p.AIBaseURL != "https://api.example.com/v1/" || *p.AIModel != "small" || *p.AIAPIKey != "sk-test" ||
		*p.AILanguage != "en" || p.RSSHubBaseURL != nil || p.PromptSummary != nil {
		t.Fatalf("patch %+v", p)
	}
	if !tt.HasText(s.text.AISaved) || !tt.HasText(s.text.KeySet) || !tt.HasText(s.text.RemoveKey) || s.aiKey != "" ||
		s.aiBase != "https://api.example.com/v1" || !slices.Contains(tt.Announcements(), s.text.AISaved) {
		t.Fatalf("texts %v, base %q", tt.Texts(), s.aiBase)
	}
	press(t, tt, s.text.Save)
	f.settle(tt)
	if len(f.set) != 1 {
		t.Fatalf("saved without a change: %v", f.set)
	}
}

func TestInvalidAIBase(t *testing.T) {
	s, f, tt := openAI(t, "zh-Hans", true)
	f.setErr = &client.APIError{Status: 400, Code: "invalid_ai_base_url"}
	edit(tt, func() { s.aiBase = "x" })
	if !tt.HasText(s.text.TestFirst) {
		t.Fatalf("no save-first note in %v", tt.Texts())
	}
	press(t, tt, s.text.Test)
	f.settle(tt)
	if f.tests != 0 {
		t.Fatal("tested with unsaved changes")
	}
	press(t, tt, s.text.Save)
	f.settle(tt)
	tt.Frame()
	if !tt.HasText(s.text.InvalidAIBase) || !tt.Focused(s.text.Endpoint) || tt.HasText(s.text.TestFirst) ||
		!slices.Contains(tt.Announcements(), s.text.InvalidAIBase) {
		t.Fatalf("texts %v", tt.Texts())
	}
	tt.Type("y")
	if tt.HasText(s.text.InvalidAIBase) || s.aiBase != "xy" {
		t.Fatalf("the error stays after typing %q", s.aiBase)
	}
	f.setErr = errors.New("offline")
	tt.Key(0, ui.KeyEnter)
	f.settle(tt)
	if !tt.HasText(s.text.SaveFailed) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestRemoveKey(t *testing.T) {
	s, f, tt := openAI(t, "en", true)
	press(t, tt, s.text.RemoveKey)
	f.settle(tt)
	if len(f.set) != 1 || f.set[0].AIAPIKey == nil || *f.set[0].AIAPIKey != "" || f.set[0].AIModel != nil {
		t.Fatalf("set %+v", f.set)
	}
	if !tt.HasText(s.text.KeyRemoved) || !tt.HasText(s.text.KeyUnset) || tt.HasText(s.text.RemoveKey) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestTestAI(t *testing.T) {
	s, f, tt := openAI(t, "en", true)
	press(t, tt, s.text.Test)
	if !tt.HasText(s.text.Testing) {
		t.Fatalf("texts %v", tt.Texts())
	}
	press(t, tt, s.text.Test)
	f.settle(tt)
	if f.tests != 1 || !tt.HasText("Connected. small answered.") {
		t.Fatalf("tests %d, texts %v", f.tests, tt.Texts())
	}
	for code, want := range map[string]string{
		"ai_unauthorized": s.text.AIUnauthorized, "ai_unreachable": s.text.AIUnreachable,
		"ai_timeout": s.text.AITimeout, "ai_rate_limited": s.text.AIRateLimited, "ai_upstream": s.text.AIUpstream,
		"ai_empty": s.text.AIUpstream, "": s.text.ErrorHint,
	} {
		f.testErr = &client.APIError{Status: 502, Code: code}
		if code == "" {
			f.testErr = errors.New("offline")
		}
		press(t, tt, s.text.Test)
		f.settle(tt)
		if !tt.HasText(want) || !slices.Contains(tt.Announcements(), want) {
			t.Errorf("%q: no %q in %v", code, want, tt.Texts())
		}
	}
	// The model field sits under its heading, across the card.
	r, _ := tt.Find(s.text.Model)
	tt.ClickAt(r.X+20, r.Y+r.H+28)
	tt.Type("-2")
	if !strings.Contains(s.aiModel, "-2") || tt.HasText(s.text.ErrorHint) || !tt.HasText(s.text.TestFirst) {
		t.Fatalf("model %q, texts %v", s.aiModel, tt.Texts())
	}
}

func TestPrompts(t *testing.T) {
	s, f, tt := openAI(t, "en", true)
	tt.SetSize(1000, 1200)
	tt.Frame()
	save, reset := s.text.Prompts+" · "+s.text.Save, s.text.PromptSummary+" · "+s.text.Reset
	edit(tt, func() { s.prompts[0] += " Be brief." })
	press(t, tt, save)
	f.settle(tt)
	if len(f.set) != 1 || *f.set[0].PromptSummary != summaryPrompt+" Be brief." || f.set[0].PromptTranslate != nil ||
		f.set[0].AIBaseURL != nil || !tt.HasText(s.text.AISaved) {
		t.Fatalf("set %+v, texts %v", f.set, tt.Texts())
	}
	press(t, tt, reset)
	if s.prompts[0] != summaryPrompt || tt.HasText(s.text.AISaved) {
		t.Fatalf("prompt %q, texts %v", s.prompts[0], tt.Texts())
	}
	f.setErr = &client.APIError{Status: 400, Code: "invalid_prompt"}
	press(t, tt, save)
	f.settle(tt)
	if len(f.set) != 2 || *f.set[1].PromptSummary != "" || !tt.HasText(s.text.InvalidPrompt) {
		t.Fatalf("set %+v, texts %v", f.set, tt.Texts())
	}
	f.setErr = nil
	press(t, tt, save)
	f.settle(tt)
	if s.settings.PromptSummary != summaryPrompt || s.promptsDirty() {
		t.Fatalf("settings %+v", s.settings)
	}
	press(t, tt, save)
	f.settle(tt)
	if len(f.set) != 3 {
		t.Fatalf("saved unchanged prompts: %v", f.set)
	}
}

func TestAIExpired(t *testing.T) {
	s, f, tt := openAI(t, "en", true)
	f.testErr = client.ErrUnauthorized
	press(t, tt, s.text.Test)
	f.settle(tt)
	if f.expired != 1 || s.testing || s.api != nil {
		t.Fatalf("expired %d, testing %v", f.expired, s.testing)
	}
}

// openAppearance shows the appearance group with the default preferences.
func openAppearance(t *testing.T, language string) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	s, f, tt := setup(t, language, true)
	f.prefs = prefs.Default()
	s.OpenAppearance()
	f.settle(tt)
	tt.Frame()
	return s, f, tt
}

func TestAppearanceShows(t *testing.T) {
	s, _, tt := openAppearance(t, "en")
	for _, want := range []string{s.text.AppearanceLead, s.text.Theme, s.text.ThemeSystem, s.text.UILanguage,
		s.text.SystemChoice, s.text.UIFont, s.text.SystemFont, s.text.Reading, s.text.TextSize, s.text.SizeMedium,
		s.text.LineSpacing, s.text.SpacingStandard, s.text.PreviewText} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %v", want, tt.Texts())
		}
	}
	if tt.HasText(s.text.RetentionSaved) || tt.HasText("Keep entries") {
		t.Fatalf("the fetching group shows: %v", tt.Texts())
	}
}

func TestAppearanceChoices(t *testing.T) {
	s, f, tt := openAppearance(t, "en")
	press(t, tt, s.text.ThemeDark)
	press(t, tt, s.text.SizeXLarge)
	press(t, tt, s.text.SpacingRelaxed)
	want := prefs.Preferences{Theme: prefs.ThemeDark, ReadingSize: prefs.SizeXLarge, LineSpacing: prefs.SpacingRelaxed}
	if len(f.changes) != 3 || f.prefs != want {
		t.Fatalf("changes %v", f.changes)
	}
	press(t, tt, s.text.ThemeSystem)
	if f.prefs.Theme != "" {
		t.Fatalf("theme %q", f.prefs.Theme)
	}
}

func TestAppearanceLanguage(t *testing.T) {
	_, f, tt := openAppearance(t, "en")
	f.prefs.Language = prefs.LanguageEnglish
	tt.Frame()
	press(t, tt, "English")
	press(t, tt, "简体中文")
	if f.prefs.Language != prefs.LanguageChinese {
		t.Fatalf("language %q", f.prefs.Language)
	}
}

func TestAppearanceFonts(t *testing.T) {
	s, f, tt := openAppearance(t, "en")
	press(t, tt, s.text.SystemFont)
	if tt.HasText("Georgia") {
		t.Fatalf("a font shows before the list loads: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	f.fonts = []string{"Georgia", "Menlo"}
	tt.Frame()
	press(t, tt, s.text.SystemFont)
	press(t, tt, "Georgia")
	if f.prefs.Font != "Georgia" {
		t.Fatalf("font %q", f.prefs.Font)
	}
}

func TestAppearanceFontGone(t *testing.T) {
	s, f, tt := openAppearance(t, "en")
	f.prefs.Font = "Gone Sans"
	f.fonts = []string{"Georgia"}
	tt.Frame()
	if !tt.HasText(s.text.SystemFont) || tt.HasText("Gone Sans") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestAppearancePreferenceFailed(t *testing.T) {
	s, _, tt := openAppearance(t, "zh-Hans")
	s.PreferenceFailed()
	tt.Frame()
	if !tt.HasText(s.text.PreferenceFailed) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

// TestPalettes draws every group under Oriel's theme in light, dark and with high contrast: the pane's own theme
// must not leak into the page, so every group's texts are still there.
func TestPalettes(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, high := range []bool{false, true} {
			s, f, _ := setup(t, "en", true)
			p := prefs.Default()
			p.Theme = prefs.ThemeLight
			if dark {
				p.Theme = prefs.ThemeDark
			}
			f.prefs = p
			tt := ui.NewTester(func(c *ui.Context) {
				theme.Apply(c, f.prefs)
				s.View(c)
			}, 1000, 760)
			tt.SetPreferences(ui.Preferences{HighContrast: high})
			for g := range 5 {
				s.group = g
				tt.Frame()
				f.settle(tt)
				if !tt.HasText(s.groupName(g)) || !tt.HasText(s.text.BackTimeline) {
					t.Errorf("dark %v high %v group %d: %v", dark, high, g, tt.Texts())
				}
			}
		}
	}
}
