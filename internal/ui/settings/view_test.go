package settings

import (
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
)

var now = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// fake stands in for the server, the file dialogs and the background: work waits in a queue until settle runs it.
type fake struct {
	queue    []func() func()
	settings client.Settings
	devices  []client.Device
	imported client.Imported
	opml     []byte

	set     []client.SettingsPatch
	revoked []int64
	sent    [][]byte

	settingsErr, setErr, devicesErr, revokeErr, importErr, exportErr error

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
		Back:     func() { f.back++ },
		Imported: func() { f.refreshed++ },
		Expired:  func() { f.expired++ },
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
		settings: client.Settings{RSSHubBaseURL: "https://rsshub.example.com", RetentionDays: 30},
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

// field clicks the RSSHub instance field, left of its Save button; its label is also the setting's heading.
func field(t *testing.T, s *Screen, tt *ui.Tester) {
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
	field(t, s, tt)
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
	field(t, s, tt)
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
	if n := strings.Count(strings.Join(texts, "\n"), s.text.Revoke); n != 2 {
		t.Fatalf("%d revoke buttons in %v", n, texts)
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

func TestNarrow(t *testing.T) {
	s, _, tt := setup(t, "en", true)
	tt.SetSize(640, 600)
	tt.Frame()
	if !tt.HasText(s.text.RSSHub) || !tt.HasText(s.text.BackTimeline) {
		t.Fatalf("texts %v", tt.Texts())
	}
}
