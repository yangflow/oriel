package settings

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
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
	later    time.Duration // how far the clock has moved on from now
	changes  []prefs.Preferences
	queue    []func() func()
	settings client.Settings
	devices  []client.Device
	imported client.Imported
	jobID    string // the id of the last job started
	jobN     int
	jobPolls int                                    // how many times a job was asked about
	jobPoll  func(n int) (client.OPMLImport, error) // what the nth poll answers; an empty phase and no error means done
	opml     []byte
	diag     []byte

	set     []client.SettingsPatch
	revoked []int64
	sent    [][]byte

	settingsErr, setErr, devicesErr, revokeErr, importErr, exportErr, testErr, diagErr error

	activity    client.Activity
	storage     client.Storage
	started     client.Vacuum // what starting a compaction answers
	activityErr error
	storageErr  error
	vacuumErr   error
	reads       int // how many times the activity was read
	health      client.RSSHubHealth
	healthErr   error
	healthReads int // how many times the RSSHub health was read
	vacuums     int // how many times a compaction was started

	model string        // the model that answers a test
	check client.AITest // what else the test finds; its Model is model
	tests int

	importReqs                      []client.ImportRequest // what starting an import sent
	startAns, statusAns, confirmAns client.ImportStatus
	startErr, statusErr, confirmErr error
	polls, cancels                  int

	backup      []byte // what a backup streams
	backupErr   error
	restored    client.Restored
	restoreErr  error
	restoreSent [][]byte // the files uploaded to restore

	open, save string // the paths the dialogs answer, "" for cancelled
	folder     string // the folder the folder dialog answers
	folders    int    // how often the folder dialog was shown
	filters    []string
	names      []string // the file names the save dialog was offered
	back       int
	refreshed  int
	viewed     int
	expired    int

	rules      []client.Rule
	sources    []client.Source
	rulesErr   error // what listing the rules and the subscriptions answers
	ruleErr    error // what changing a rule answers
	ruleSent   []client.NewRule
	ruleBack   []client.Rule
	deletedIDs []int64
	nextRule   int64
	counted    []string // the keywords a count was asked for
	mutedCount int
	mutedErr   error
}

func (f *fake) Settings(context.Context) (client.Settings, error) { return f.settings, f.settingsErr }

func (f *fake) SetSettings(_ context.Context, patch client.SettingsPatch) (client.Settings, error) {
	f.set = append(f.set, patch)
	if f.setErr != nil {
		return client.Settings{}, f.setErr
	}
	if patch.MuteKeywords != nil {
		f.settings.MuteKeywords = *patch.MuteKeywords
	}
	if patch.RSSHubBaseURLs != nil {
		f.settings.RSSHubBaseURLs = nil
		for _, base := range *patch.RSSHubBaseURLs {
			f.settings.RSSHubBaseURLs = append(f.settings.RSSHubBaseURLs, strings.TrimSuffix(base, "/"))
		}
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
	if patch.AISummaryLimit != nil {
		f.settings.AISummaryLimit = cmp.Or(*patch.AISummaryLimit, f.settings.DefaultAISummaryLimit)
	}
	if patch.AIChunkLimit != nil {
		f.settings.AIChunkLimit = cmp.Or(*patch.AIChunkLimit, f.settings.DefaultAIChunkLimit)
	}
	if patch.AIAutoDaily != nil {
		f.settings.AIAutoDailyLimit = cmp.Or(*patch.AIAutoDaily, f.settings.DefaultAIAutoDaily)
	}
	if patch.AIAutoFetch != nil {
		f.settings.AIAutoFetchLimit = cmp.Or(*patch.AIAutoFetch, f.settings.DefaultAIAutoFetch)
	}
	if patch.PromptSummary != nil {
		f.settings.PromptSummary = cmp.Or(*patch.PromptSummary, f.settings.DefaultPromptSummary)
	}
	if patch.PromptTranslate != nil {
		f.settings.PromptTranslate = cmp.Or(*patch.PromptTranslate, f.settings.DefaultPromptTranslate)
	}
	if patch.GroupSameLink != nil {
		f.settings.GroupSameLink = *patch.GroupSameLink
	}
	if patch.AutoBackupEnabled != nil {
		f.settings.AutoBackupEnabled = *patch.AutoBackupEnabled
	}
	if patch.AutoBackupFolder != nil {
		f.settings.AutoBackupFolder = *patch.AutoBackupFolder
	}
	if patch.AutoBackupFrequency != nil {
		f.settings.AutoBackupFrequency = *patch.AutoBackupFrequency
	}
	if patch.AutoBackupKeep != nil {
		f.settings.AutoBackupKeep = *patch.AutoBackupKeep
	}
	return f.settings, nil
}

func (f *fake) Devices(context.Context) ([]client.Device, error) { return f.devices, f.devicesErr }

func (f *fake) RevokeDevice(_ context.Context, id int64) error {
	f.revoked = append(f.revoked, id)
	return f.revokeErr
}

// StartOPMLImport starts a job that reports running until the test says otherwise, and then done with f.imported.
func (f *fake) StartOPMLImport(_ context.Context, opml []byte) (client.OPMLImport, error) {
	f.sent = append(f.sent, opml)
	if f.importErr != nil {
		return client.OPMLImport{}, f.importErr
	}
	f.jobN++
	f.jobID = fmt.Sprintf("job-%d", f.jobN)
	return client.OPMLImport{ID: f.jobID, Phase: client.OPMLRunning}, nil
}

func (f *fake) OPMLImportStatus(context.Context) (client.OPMLImport, error) {
	f.jobPolls++
	if f.jobPoll != nil {
		if job, err := f.jobPoll(f.jobPolls); job.Phase != "" || err != nil {
			if job.ID == "" {
				job.ID = f.jobID
			}
			return job, err
		}
	}
	result := f.imported
	return client.OPMLImport{ID: f.jobID, Phase: client.OPMLDone, Result: &result}, nil
}

// finish runs the queued work and lets the clock move on one poll at a time until the import is over.
func (f *fake) finish(tt *ui.Tester, s *Screen) {
	f.settle(tt)
	for range 100 {
		if s.run == nil {
			return
		}
		f.after(tt, importEvery)
	}
}

func (f *fake) ExportOPML(context.Context) ([]byte, error) { return f.opml, f.exportErr }

func (f *fake) Backup(_ context.Context, dst io.Writer) (int64, error) {
	n, err := dst.Write(f.backup)
	if err == nil {
		err = f.backupErr
	}
	return int64(n), err
}

func (f *fake) Restore(_ context.Context, backup io.Reader) (client.Restored, error) {
	data, err := io.ReadAll(backup)
	if err != nil {
		return client.Restored{}, err
	}
	f.restoreSent = append(f.restoreSent, data)
	return f.restored, f.restoreErr
}

func (f *fake) TestAI(context.Context) (client.AITest, error) {
	f.tests++
	found := f.check
	found.Model = f.model
	return found, f.testErr
}

func (f *fake) StartImport(_ context.Context, req client.ImportRequest) (client.ImportStatus, error) {
	f.importReqs = append(f.importReqs, req)
	return f.startAns, f.startErr
}

func (f *fake) ImportStatus(context.Context) (client.ImportStatus, error) {
	f.polls++
	return f.statusAns, f.statusErr
}

func (f *fake) ConfirmImport(context.Context) (client.ImportStatus, error) {
	return f.confirmAns, f.confirmErr
}

func (f *fake) CancelImport(context.Context) (client.ImportStatus, error) {
	f.cancels++
	return client.ImportStatus{Phase: "idle"}, nil
}

func (f *fake) Sources(context.Context) (client.SourceList, error) {
	return client.SourceList{Sources: slices.Clone(f.sources)}, f.rulesErr
}

func (f *fake) Rules(context.Context) ([]client.Rule, error) {
	return slices.Clone(f.rules), f.rulesErr
}

func (f *fake) CreateRule(_ context.Context, rule client.NewRule) (client.Rule, error) {
	f.ruleSent = append(f.ruleSent, rule)
	if f.ruleErr != nil {
		return client.Rule{}, f.ruleErr
	}
	f.nextRule++
	r := client.Rule{ID: 100 + f.nextRule, SourceID: rule.SourceID, Kind: rule.Kind, Keywords: rule.Keywords,
		Author: rule.Author, Star: rule.Star, MarkRead: rule.MarkRead, Enabled: rule.Enabled}
	f.rules = append(f.rules, r)
	return r, nil
}

func (f *fake) RestoreRule(_ context.Context, rule client.Rule) (client.Rule, error) {
	f.ruleBack = append(f.ruleBack, rule)
	if f.ruleErr != nil {
		return client.Rule{}, f.ruleErr
	}
	f.rules = append(f.rules, rule)
	slices.SortStableFunc(f.rules, func(a, b client.Rule) int {
		return cmp.Or(cmp.Compare(a.CreatedAt, b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	return rule, nil
}

func (f *fake) UpdateRule(_ context.Context, id int64, rule client.NewRule) (client.Rule, error) {
	f.ruleSent = append(f.ruleSent, rule)
	if f.ruleErr != nil {
		return client.Rule{}, f.ruleErr
	}
	for i, r := range f.rules {
		if r.ID == id {
			f.rules[i] = client.Rule{ID: id, SourceID: rule.SourceID, Kind: rule.Kind, Keywords: rule.Keywords,
				Author: rule.Author, Star: rule.Star, MarkRead: rule.MarkRead, Enabled: rule.Enabled}
			return f.rules[i], nil
		}
	}
	return client.Rule{}, &client.APIError{Status: 404, Code: "not_found"}
}

func (f *fake) SetRuleEnabled(_ context.Context, id int64, enabled bool) (client.Rule, error) {
	if f.ruleErr != nil {
		return client.Rule{}, f.ruleErr
	}
	for i, r := range f.rules {
		if r.ID == id {
			f.rules[i].Enabled = enabled
			return f.rules[i], nil
		}
	}
	return client.Rule{}, &client.APIError{Status: 404, Code: "not_found"}
}

func (f *fake) DeleteRule(_ context.Context, id int64) error {
	if f.ruleErr != nil {
		return f.ruleErr
	}
	f.deletedIDs = append(f.deletedIDs, id)
	f.rules = slices.DeleteFunc(f.rules, func(r client.Rule) bool { return r.ID == id })
	return nil
}

func (f *fake) MutedCountAll(_ context.Context, keywords string) (int, error) {
	f.counted = append(f.counted, keywords)
	return f.mutedCount, f.mutedErr
}

func (f *fake) deps() Deps {
	return Deps{
		Go:  func(work func() func()) { f.queue = append(f.queue, work) },
		Now: func() time.Time { return now.Add(f.later) },
		Open: func(filter string, _ ...string) (string, error) {
			f.filters = append(f.filters, filter)
			return f.open, nil
		},
		Save: func(name, filter string) (string, error) {
			f.names = append(f.names, name)
			f.filters = append(f.filters, filter)
			return f.save, nil
		},
		Folder: func() (string, error) {
			f.folders++
			return f.folder, nil
		},
		Back:        func() { f.back++ },
		Imported:    func() { f.refreshed++ },
		ViewSources: func() { f.viewed++ },
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
		settings: client.Settings{RSSHubBaseURLs: []string{"https://rsshub.example.com"}, RetentionDays: 30, AILanguage: "zh-Hans",
			PromptSummary: summaryPrompt, PromptTranslate: translatePrompt,
			DefaultPromptSummary: summaryPrompt, DefaultPromptTranslate: translatePrompt,
			AISummaryLimit: 24000, AIChunkLimit: 6000, DefaultAISummaryLimit: 24000, DefaultAIChunkLimit: 6000,
			AIAutoDailyLimit: 200, AIAutoFetchLimit: 10, DefaultAIAutoDaily: 200, DefaultAIAutoFetch: 10,
			AutoBackupFrequency: "daily", AutoBackupKeep: 7},
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
	tt := ui.NewTester(func(c *ui.Context) { s.View(c); theme.Toasts(c) }, 1000, 760)
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
		t.Fatalf("saved an unchanged list: %v", f.set)
	}
	// Enter in the field is a new line, not a save; a repeat, a blank line and a trailing slash are tidied.
	edit(tt, func() {
		s.base = "https://rsshub.example.com/\n\nhttps://backup.example.com/\nHTTPS://RSSHUB.EXAMPLE.COM"
	})
	if len(f.set) != 0 {
		t.Fatalf("saved while typing: %v", f.set)
	}
	press(t, tt, s.text.Save)
	f.settle(tt)
	if len(f.set) != 1 || f.set[0].RSSHubBaseURL != nil || !slices.Equal(*f.set[0].RSSHubBaseURLs,
		[]string{"https://rsshub.example.com", "https://backup.example.com"}) {
		t.Fatalf("set %+v", f.set)
	}
	if !tt.HasText(s.text.Saved) || s.base != "https://rsshub.example.com\nhttps://backup.example.com" {
		t.Fatalf("texts %v, base %q", tt.Texts(), s.base)
	}

	// Emptying the field saves an empty list.
	edit(tt, func() { s.base = "" })
	press(t, tt, s.text.Save)
	f.settle(tt)
	if len(f.set) != 2 || f.set[1].RSSHubBaseURLs == nil || len(*f.set[1].RSSHubBaseURLs) != 0 {
		t.Fatalf("set %+v", f.set)
	}
}

func TestInvalidBase(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	// A line that is not an address is named by its number and nothing is sent.
	edit(tt, func() { s.base = "https://rsshub.example.com\n\nrsshub.example" })
	press(t, tt, s.text.Save)
	f.settle(tt)
	tt.Frame()
	line3 := strings.ReplaceAll(s.text.InvalidBase, "{n}", "3")
	if len(f.set) != 0 || !tt.HasText(line3) || !tt.Focused(s.text.RSSHub) {
		t.Fatalf("set %v, texts %v", f.set, tt.Texts())
	}
	tt.Type("x")
	if tt.HasText(line3) {
		t.Fatal("the error stays after typing")
	}

	// A refusal from the server shows too, and a failure to reach it is a different message.
	edit(tt, func() { s.base = "https://a.example\nhttps://b.example" })
	f.setErr = &client.APIError{Status: 400, Code: "invalid_rsshub_base_url"}
	press(t, tt, s.text.Save)
	f.settle(tt)
	tt.Frame()
	if !tt.HasText(s.text.InvalidBaseAny) {
		t.Fatalf("texts %v", tt.Texts())
	}
	f.setErr = &client.APIError{Status: 400, Code: "too_many_rsshub_base_urls"}
	press(t, tt, s.text.Save)
	f.settle(tt)
	if !tt.HasText(s.text.TooManyBase) {
		t.Fatalf("texts %v", tt.Texts())
	}
	f.setErr = errors.New("offline")
	press(t, tt, s.text.Save)
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
	if !tt.HasText("Importing feeds.opml…") {
		t.Fatalf("texts %v", tt.Texts())
	}
	f.finish(tt, s)
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
	if r, ok := tt.Find(s.text.Dismiss); !ok || r.W < theme.MinTarget || r.H < theme.MinTarget {
		t.Errorf("the dismiss button is %v, under %d by %d", r, theme.MinTarget, theme.MinTarget)
	}
	if err := tt.Click(s.text.Dismiss); err != nil {
		t.Fatal(err)
	}
	if tt.HasText(want) {
		t.Fatal("the result stays after dismissing")
	}
}

func TestImportDropped(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	f.imported = client.Imported{Added: 3, Existing: 1}
	s.ImportFiles([]string{writeOPML(t, "feeds.OPML", "<opml/>")})
	tt.Frame()
	if s.group != opml || !tt.HasText("Importing feeds.OPML…") {
		t.Fatalf("group %v, texts %v", s.group, tt.Texts())
	}
	f.finish(tt, s)
	if len(f.sent) != 1 || string(f.sent[0]) != "<opml/>" || f.refreshed != 1 {
		t.Fatalf("sent %q, refreshed %d", f.sent, f.refreshed)
	}
	if want := "Imported feeds.OPML: 3 added, 1 already subscribed."; !tt.HasText(want) {
		t.Fatalf("no %q in %v", want, tt.Texts())
	}
}

func TestImportDroppedNotOPML(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	s.ImportFiles([]string{writeOPML(t, "notes.txt", "hello")})
	f.finish(tt, s)
	if len(f.sent) != 0 || !tt.HasText("notes.txt is not an OPML file. Nothing was added.") {
		t.Fatalf("sent %q, texts %v", f.sent, tt.Texts())
	}
}

func TestImportDroppedSeveral(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	f.imported = client.Imported{Added: 2, Existing: 1, Failed: []client.ImportFail{
		{Title: "Broken", Address: "ftp://example.com/feed", Error: "invalid_url"}}}
	dir := t.TempDir()
	paths := []string{
		writeOPML(t, "a.opml", "<opml/>"),
		filepath.Join(dir, "gone.opml"),
		writeOPML(t, "big.xml", strings.Repeat("x", client.MaxOPML+1)),
		writeOPML(t, "photo.png", "png"),
		dir,
		writeOPML(t, "b.xml", "<opml/>"),
	}
	s.ImportFiles(paths)
	f.finish(tt, s)
	if len(f.sent) != 2 || f.refreshed != 1 {
		t.Fatalf("sent %d files, refreshed %d", len(f.sent), f.refreshed)
	}
	want := "Imported 6 files: 4 added, 2 already subscribed, 6 not added."
	for _, text := range []string{want, "ftp://example.com/feed", "gone.opml", "Could not be read", "big.xml", "Larger than 1 MB",
		"photo.png", "Not an OPML file"} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
}

func TestImportDroppedAllFail(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	s.ImportFiles([]string{writeOPML(t, "a.txt", "x"), writeOPML(t, "b.txt", "x")})
	f.finish(tt, s)
	if s.result.tone != refused || f.refreshed != 0 || !tt.HasText("Imported 2 files: 0 added, 0 already subscribed, 2 not added.") {
		t.Fatalf("result %+v, texts %v", s.result, tt.Texts())
	}
}

func TestImportDroppedBusy(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	path := writeOPML(t, "a.opml", "<opml/>")
	s.ImportFiles([]string{path})
	tt.Frame()
	s.ImportFiles([]string{path}) // while the first runs
	tt.Frame()
	if !tt.HasText(s.text.DropBusy) {
		t.Fatalf("no busy message in %v", tt.Texts())
	}
	f.finish(tt, s)
	if len(f.sent) != 1 {
		t.Fatalf("sent %d files", len(f.sent))
	}
	s.confirming = true
	s.ImportFiles([]string{path})
	f.finish(tt, s)
	if len(f.sent) != 1 {
		t.Fatalf("sent %d files with a dialog open", len(f.sent))
	}
}

func TestImportDroppedSignedOut(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	f.importErr = client.ErrUnauthorized
	s.ImportFiles([]string{writeOPML(t, "a.opml", "<opml/>"), writeOPML(t, "b.opml", "<opml/>")})
	f.finish(tt, s)
	if len(f.sent) != 1 || s.api != nil || s.busy {
		t.Fatalf("sent %d, api %v, busy %v", len(f.sent), s.api, s.busy)
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
		f.finish(tt, s)
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
		"Safari", "iPhone", "Sep 18", "3 h ago",
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
	if r := rect(t, tt, "3 h ago"); r.X+r.W != last.X+last.W {
		t.Errorf("last used ends at %v, want %v", r.X+r.W, last.X+last.W)
	}
	if b := rect(t, tt, "Sign out “Safari · iPhone”?"); b.X < last.X+last.W || b.W == 0 {
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
		"Safari · iPhone", "Signed in Sep 18 · last used 3 h ago",
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
	if err := tt.Click("Sign out “Safari · iPhone”?"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(s.text.RevokeBody) {
		t.Fatalf("no dialog in %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	if f.back != 0 || len(f.revoked) != 0 {
		t.Fatalf("back %d, revoked %v", f.back, f.revoked)
	}
	if err := tt.Click("Sign out “Unknown device”?"); err != nil {
		t.Fatal(err)
	}
	f.revokeErr = errors.New("offline")
	if err := tt.Click(s.text.RevokeConfirm); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !tt.HasText("Couldn’t sign out Unknown device.") || !tt.HasText("Unknown device") {
		t.Fatalf("texts %v", tt.Texts())
	}
	tt.Frame()
	f.revokeErr = &client.APIError{Status: 404, Code: "not_found"}
	if err := tt.Click("Sign out “Unknown device”?"); err != nil {
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
	other := &fake{settings: client.Settings{RSSHubBaseURLs: []string{"https://other.example.com"}, RetentionDays: 7}}
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
	// The AI page is longer than the default window; fonts differ by platform, so leave room for all of it.
	tt.SetSize(1000, 1800)
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

func TestOpenPage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		group int
	}{{"appearance", appearance}, {"fetching", fetching}, {"opml", opml}, {"ai", ai}, {"devices", devices}, {"activity", activity}} {
		s, f, tt := setup(t, "en", true)
		s.OpenPage(tc.name)
		f.settle(tt)
		if s.group != tc.group {
			t.Errorf("%s: group %d, want %d", tc.name, s.group, tc.group)
		}
	}
	s, f, tt := setup(t, "zh-Hans", true)
	s.OpenPage("ai")
	s.OpenPage("nowhere")
	f.settle(tt)
	if s.group != ai {
		t.Fatalf("an unknown page changed the group to %d", s.group)
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
		*p.AILanguage != "en" || p.RSSHubBaseURLs != nil || p.PromptSummary != nil {
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
		"ai_empty": s.text.AIUpstream, "ai_model_not_found": s.text.AIModelMissing, "": s.text.ErrorHint,
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

// TestPresets has a preset fill the address and hint the model, and leave the key and everything saved alone.
func TestPresets(t *testing.T) {
	s, f, tt := openAI(t, "en", true)
	edit(tt, func() { s.aiKey = "sk-typed" })
	press(t, tt, "Ollama")
	if s.aiBase != "http://127.0.0.1:11434/v1" || s.aiKey != "sk-typed" || s.aiModel != "small" || len(f.set) != 0 ||
		s.preset() != 0 || !s.aiDirty() || !tt.HasText(s.text.TestFirst) {
		t.Fatalf("base %q, key %q, model %q, set %v", s.aiBase, s.aiKey, s.aiModel, f.set)
	}
	press(t, tt, "LM Studio")
	if s.aiBase != "http://127.0.0.1:1234/v1" || s.preset() != 1 || s.aiKey != "sk-typed" {
		t.Fatalf("base %q, key %q", s.aiBase, s.aiKey)
	}
	// An address typed by hand that is a preset's counts as it, trailing slash or not.
	edit(tt, func() { s.aiBase = " http://127.0.0.1:11434/v1/ " })
	if s.preset() != 0 {
		t.Fatalf("preset %d for %q", s.preset(), s.aiBase)
	}
	edit(tt, func() { s.aiBase = "https://api.example.com/v1" })
	if s.preset() != -1 {
		t.Fatalf("preset %d for %q", s.preset(), s.aiBase)
	}

	// Saved, the preset sends the address and the key typed, and nothing else.
	press(t, tt, "Ollama")
	press(t, tt, s.text.Save)
	f.settle(tt)
	if len(f.set) != 1 || *f.set[0].AIBaseURL != "http://127.0.0.1:11434/v1" || *f.set[0].AIAPIKey != "sk-typed" ||
		f.set[0].AIModel != nil || f.set[0].AISummaryLimit != nil || f.set[0].AIChunkLimit != nil {
		t.Fatalf("set %+v", f.set)
	}
}

// TestPresetKeepsSavedKey is a preset picked with no key typed: the key saved is not part of the patch.
func TestPresetKeepsSavedKey(t *testing.T) {
	s, f, tt := openAI(t, "zh-Hans", true)
	press(t, tt, "LM Studio")
	press(t, tt, s.text.Save)
	f.settle(tt)
	if len(f.set) != 1 || f.set[0].AIAPIKey != nil || !s.settings.AIAPIKeySet || !tt.HasText(s.text.KeySet) {
		t.Fatalf("set %+v, key set %v", f.set, s.settings.AIAPIKeySet)
	}
}

// TestLimits saves the limits as typed, takes empty for the default, and refuses what is not a number.
func TestLimits(t *testing.T) {
	s, f, tt := openAI(t, "en", true)
	if s.aiLimits != [4]string{} || s.aiDirty() {
		t.Fatalf("limits %q", s.aiLimits)
	}
	edit(tt, func() { s.aiLimits = [4]string{"8000"} })
	press(t, tt, s.text.Save)
	f.settle(tt)
	if len(f.set) != 1 || *f.set[0].AISummaryLimit != 8000 || f.set[0].AIChunkLimit != nil || f.set[0].AIBaseURL != nil ||
		s.settings.AISummaryLimit != 8000 || s.aiLimits != [4]string{"8000"} || s.aiDirty() {
		t.Fatalf("set %+v, limits %q", f.set, s.aiLimits)
	}

	edit(tt, func() { s.aiLimits = [4]string{"8000", "many"} })
	press(t, tt, s.text.Save)
	f.settle(tt)
	if len(f.set) != 1 || !tt.HasText(s.text.InvalidLimit) || !slices.Contains(tt.Announcements(), s.text.InvalidLimit) {
		t.Fatalf("set %v, texts %v", f.set, tt.Texts())
	}

	// The server's refusal says the same.
	edit(tt, func() { s.aiLimits = [4]string{"8000", "9"} })
	f.setErr = &client.APIError{Status: 400, Code: "invalid_ai_limit"}
	press(t, tt, s.text.Save)
	f.settle(tt)
	if len(f.set) != 2 || !tt.HasText(s.text.InvalidLimit) {
		t.Fatalf("set %v, texts %v", f.set, tt.Texts())
	}
	f.setErr = nil

	// Emptied, the limit goes back to the default.
	edit(tt, func() { s.aiLimits = [4]string{" "} })
	press(t, tt, s.text.Save)
	f.settle(tt)
	if len(f.set) != 3 || *f.set[2].AISummaryLimit != 0 || s.settings.AISummaryLimit != 24000 || s.aiLimits != [4]string{} {
		t.Fatalf("set %+v, limits %q", f.set, s.aiLimits)
	}
}

// TestAutomaticCaps saves the caps on automatic work as typed.
func TestAutomaticCaps(t *testing.T) {
	s, f, tt := openAI(t, "en", true)
	edit(tt, func() { s.aiLimits = [4]string{"", "", "50", "5"} })
	press(t, tt, s.text.Save)
	f.settle(tt)
	if len(f.set) != 1 || *f.set[0].AIAutoDaily != 50 || *f.set[0].AIAutoFetch != 5 || f.set[0].AISummaryLimit != nil ||
		s.settings.AIAutoDailyLimit != 50 || s.settings.AIAutoFetchLimit != 5 || s.aiDirty() {
		t.Fatalf("set %+v, settings %+v", f.set, s.settings)
	}
}

// TestDestinationAndCap says where entry text goes and whether that is this computer, and says plainly when the
// daily cap is reached.
func TestDestinationAndCap(t *testing.T) {
	s, _, tt := openAI(t, "en", true)
	edit(tt, func() { s.settings.AIAutoCallsToday = 12 })
	remote := strings.ReplaceAll(s.text.DestinationRemote, "{address}", "api.example.com")
	usage := strings.NewReplacer("{used}", "12", "{limit}", "200").Replace(s.text.UsageToday)
	if !tt.HasText(remote) || !tt.HasText(usage) {
		t.Fatalf("texts %v", tt.Texts())
	}
	edit(tt, func() {
		s.settings.AIBaseURL, s.settings.AIEndpointLocal, s.settings.AIAutoCallsToday = "http://localhost:1234/v1", true, 200
	})
	local := strings.ReplaceAll(s.text.DestinationLocal, "{address}", "localhost:1234")
	reached := strings.ReplaceAll(s.text.CapReached, "{limit}", "200")
	if !tt.HasText(local) || !tt.HasText(reached) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

// TestChecks lists what a test found under its answer: the model is listed, and the context fits or is too small.
func TestChecks(t *testing.T) {
	s, f, tt := openAI(t, "en", true)
	yes, no, n := true, false, 128000
	f.check = client.AITest{ModelFound: &yes, ContextTokens: &n, ContextFits: &yes}
	press(t, tt, s.text.Test)
	f.settle(tt)
	fits := s.fill(s.text.CheckFits, "{n}", "128000")
	if !tt.HasText("Connected. small answered.") || !tt.HasText(s.text.CheckListed) || !tt.HasText(fits) ||
		len(s.aiChecks) != 2 || s.aiChecks[1].warn {
		t.Fatalf("checks %+v, texts %v", s.aiChecks, tt.Texts())
	}

	n = 4096
	f.check = client.AITest{ModelFound: &yes, ContextTokens: &n, ContextFits: &no}
	press(t, tt, s.text.Test)
	f.settle(tt)
	small := s.fill(s.text.CheckSmall, "{n}", "4096")
	if !tt.HasText("Connected. small answered.") || !tt.HasText(small) || !s.aiChecks[1].warn || tt.HasText(fits) {
		t.Fatalf("checks %+v, texts %v", s.aiChecks, tt.Texts())
	}

	// What the endpoint did not tell is left out, and a change clears the lines.
	f.check = client.AITest{}
	press(t, tt, s.text.Test)
	f.settle(tt)
	if len(s.aiChecks) != 0 || tt.HasText(s.text.CheckListed) {
		t.Fatalf("checks %+v", s.aiChecks)
	}
	f.check = client.AITest{ModelFound: &yes}
	press(t, tt, s.text.Test)
	f.settle(tt)
	r, _ := tt.Find(s.text.Model)
	tt.ClickAt(r.X+20, r.Y+r.H+28)
	tt.Type("-2")
	if len(s.aiChecks) != 0 || tt.HasText(s.text.CheckListed) {
		t.Fatalf("checks %+v after a change", s.aiChecks)
	}
}

func TestPrompts(t *testing.T) {
	s, f, tt := openAI(t, "en", true)
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

// TestPromptRestoreUndo has Restore default offer Undo, which puts back the exact text, and only while the box has
// not been edited again.
func TestPromptRestoreUndo(t *testing.T) {
	s, _, tt := openAI(t, "en", true)
	reset := s.text.PromptSummary + " · " + s.text.Reset
	toast := s.fill(s.text.PromptRestored, "{name}", s.text.PromptSummary)
	edited := "  Be brief.\nUse \"quotes\".  " + strings.Repeat("é", 100)
	edit(tt, func() { s.prompts[0] = edited })
	press(t, tt, reset)
	tt.Frame()
	if s.prompts[0] != summaryPrompt || !tt.HasText(toast) || !tt.HasText(s.text.Undo) || !tt.HasText(s.text.Dismiss) {
		t.Fatalf("prompt %q, texts %v", s.prompts[0], tt.Texts())
	}
	press(t, tt, s.text.Undo)
	tt.Frame()
	if s.prompts[0] != edited || tt.HasText(s.text.Undo) || s.restored != nil {
		t.Fatalf("prompt %q, texts %v", s.prompts[0], tt.Texts())
	}

	// Edited again after restoring: Undo leaves the new text alone.
	press(t, tt, reset)
	tt.Frame()
	edit(tt, func() { s.prompts[0] = "Something new." })
	press(t, tt, s.text.Undo)
	tt.Frame()
	if s.prompts[0] != "Something new." || tt.HasText(s.text.Undo) {
		t.Fatalf("prompt %q, texts %v", s.prompts[0], tt.Texts())
	}

	// Dismissed: nothing to undo; the default stays.
	edit(tt, func() { s.prompts[1] = "Keep it." })
	press(t, tt, s.text.PromptTranslate+" · "+s.text.Reset)
	tt.Frame()
	press(t, tt, s.text.Dismiss)
	tt.Frame()
	if s.prompts[1] != translatePrompt || tt.HasText(s.text.Undo) || s.restored != nil {
		t.Fatalf("prompt %q, texts %v", s.prompts[1], tt.Texts())
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
		s.text.LineSpacing, s.text.SpacingStandard, s.text.ChineseTypesetting, s.text.PreviewText} {
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
	want := prefs.Default()
	want.Theme, want.ReadingSize, want.LineSpacing = prefs.ThemeDark, prefs.SizeXLarge, prefs.SpacingRelaxed
	if len(f.changes) != 3 || f.prefs != want {
		t.Fatalf("changes %v", f.changes)
	}
	press(t, tt, s.text.ThemeSystem)
	if f.prefs.Theme != "" {
		t.Fatalf("theme %q", f.prefs.Theme)
	}
}

// pressNext presses the segment right after the one showing text, so a name that repeats in the card can be told apart
// by the segment before it.
func pressNext(t *testing.T, tt *ui.Tester, text string) {
	t.Helper()
	r, ok := tt.Find(text)
	if !ok {
		t.Fatalf("no %q in %v", text, tt.Texts())
	}
	tt.ClickAt(r.X+r.W+20, r.Y+r.H/2)
}

// TestViewAs shows the View as card has a row for each kind, defaulting to the first segment, and that each segment
// changes only its own kind's viewer.
func TestViewAs(t *testing.T) {
	s, f, tt := openAppearance(t, "en")
	tt.SetSize(900, 1600)
	tt.Frame()
	for _, want := range []string{s.text.ViewAs, s.text.ViewArticles, s.text.ViewVideos, s.text.ViewSocial,
		s.text.ViewImages, s.text.ViewCover, s.text.ViewWall, s.text.ViewAsHelp} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
	if f.prefs.ArticleViewer != prefs.ViewList || f.prefs.VideoViewer != prefs.ViewGrid ||
		f.prefs.SocialViewer != prefs.ViewCard || f.prefs.ImageViewer != prefs.ViewWall {
		t.Fatalf("defaults %+v", f.prefs)
	}
	press(t, tt, s.text.ViewCover)
	if f.prefs.VideoViewer != prefs.ViewCover {
		t.Fatalf("video %q", f.prefs.VideoViewer)
	}
	pressNext(t, tt, s.text.ViewCover)
	if f.prefs.VideoViewer != prefs.ViewList {
		t.Fatalf("video %q", f.prefs.VideoViewer)
	}
	pressNext(t, tt, s.text.ViewWall)
	if f.prefs.ImageViewer != prefs.ViewGrid {
		t.Fatalf("image %q", f.prefs.ImageViewer)
	}
	press(t, tt, s.text.ViewWall)
	if f.prefs.ImageViewer != prefs.ViewWall {
		t.Fatalf("image %q", f.prefs.ImageViewer)
	}
	// The first List and Card of the page are the Articles' and the Social's.
	pressNext(t, tt, s.text.ViewList)
	if f.prefs.ArticleViewer != prefs.ViewCompact {
		t.Fatalf("article %q", f.prefs.ArticleViewer)
	}
	pressNext(t, tt, s.text.ViewCard)
	if f.prefs.SocialViewer != prefs.ViewCompact {
		t.Fatalf("social %q", f.prefs.SocialViewer)
	}
	if f.prefs.VideoViewer != prefs.ViewList || f.prefs.ImageViewer != prefs.ViewWall {
		t.Fatalf("other kinds changed: %+v", f.prefs)
	}
}

// TestListDensity shows the Timeline card offers the list density, comfortable by default, and that choosing a
// segment changes only that preference.
func TestListDensity(t *testing.T) {
	s, f, tt := openAppearance(t, "en")
	tt.SetSize(900, 1600)
	tt.Frame()
	for _, want := range []string{s.text.Timeline, s.text.Density, s.text.DensityHelp, s.text.DensityComfortable} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
	if f.prefs.Density != prefs.DensityComfortable {
		t.Fatalf("default %q", f.prefs.Density)
	}
	before := f.prefs
	pressNext(t, tt, s.text.DensityComfortable)
	if f.prefs.Density != prefs.DensityCompact {
		t.Fatalf("density %q", f.prefs.Density)
	}
	before.Density = prefs.DensityCompact
	if f.prefs != before {
		t.Fatalf("other preferences changed: %+v", f.prefs)
	}
	press(t, tt, s.text.DensityComfortable)
	if f.prefs.Density != prefs.DensityComfortable {
		t.Fatalf("density %q", f.prefs.Density)
	}
}

// TestMarkReadWhen shows the Timeline card offers when entries are marked read, on opening by default, that the stay
// time shows only for "After staying", and that each choice changes only its own preference.
func TestMarkReadWhen(t *testing.T) {
	s, f, tt := openAppearance(t, "en")
	tt.SetSize(900, 1600)
	tt.Frame()
	for _, want := range []string{s.text.MarkRead, s.text.MarkReadHelpOpen, s.text.MarkReadOpen} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
	if tt.HasText(s.text.Dwell) {
		t.Fatalf("the stay time shows before it is chosen: %v", tt.Texts())
	}
	if f.prefs.MarkReadWhen != prefs.MarkOnOpen {
		t.Fatalf("default %q", f.prefs.MarkReadWhen)
	}
	before := f.prefs
	press(t, tt, s.text.MarkReadOpen)
	press(t, tt, s.text.MarkReadDwell)
	if f.prefs.MarkReadWhen != prefs.MarkAfterStay {
		t.Fatalf("when %q", f.prefs.MarkReadWhen)
	}
	before.MarkReadWhen = prefs.MarkAfterStay
	if f.prefs != before {
		t.Fatalf("other preferences changed: %+v", f.prefs)
	}
	for _, want := range []string{s.text.MarkReadHelpDwell, s.text.Dwell, s.text.DwellHelp, s.text.Seconds5} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
	press(t, tt, s.text.Seconds10)
	if f.prefs.MarkReadDwellSec != 10 || f.prefs.MarkReadWhen != prefs.MarkAfterStay {
		t.Fatalf("stay %+v", f.prefs)
	}
	press(t, tt, s.text.MarkReadDwell)
	press(t, tt, s.text.MarkReadNever)
	if f.prefs.MarkReadWhen != prefs.MarkNever || f.prefs.MarkReadDwellSec != 10 {
		t.Fatalf("never %+v", f.prefs)
	}
	if tt.HasText(s.text.Dwell) || !tt.HasText(s.text.MarkReadHelpNever) {
		t.Fatalf("the rows do not follow the choice: %v", tt.Texts())
	}
}

// TestGroupSameLinkSwitch shows the Timeline card offers the switch for showing an article once, on by default, that
// flipping it saves only that setting and has the timeline load again, and that a refusal puts it back.
func TestGroupSameLinkSwitch(t *testing.T) {
	s, f, tt := openAppearance(t, "en")
	tt.SetSize(900, 1600)
	tt.Frame()
	for _, want := range []string{s.text.GroupSameLink, s.text.GroupSameLinkHelp} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
	density, _ := tt.Find(s.text.Density)
	row, _ := tt.Find(s.text.GroupSameLink)
	if row.Y <= density.Y {
		t.Fatalf("switch %v is not after the density %v", row, density)
	}
	f.settings.GroupSameLink = true
	s.settings.GroupSameLink = true
	f.set, f.refreshed = nil, 0
	// The switch sits at the right of its label and help, centered on them.
	help, _ := tt.Find(s.text.GroupSameLinkHelp)
	toggle := func() {
		tt.ClickAt(row.X+row.W+30, (row.Y+help.Y+help.H)/2)
		f.settle(tt)
		tt.Frame()
	}
	toggle()
	if len(f.set) != 1 || f.set[0].GroupSameLink == nil || *f.set[0].GroupSameLink || f.settings.GroupSameLink {
		t.Fatalf("saved %+v, settings %+v", f.set, f.settings)
	}
	if f.refreshed != 1 {
		t.Fatalf("the timeline was refreshed %d times", f.refreshed)
	}
	f.setErr = errors.New("down")
	toggle()
	if s.settings.GroupSameLink || f.refreshed != 1 {
		t.Fatalf("a refused change stayed: %+v, refreshed %d", s.settings, f.refreshed)
	}
}

// TestChineseTypesettingSwitch shows the switch follows Line spacing and flips the setting.
func TestChineseTypesettingSwitch(t *testing.T) {
	s, f, tt := openAppearance(t, "en")
	spacing, ok := tt.Find(s.text.LineSpacing)
	if !ok {
		t.Fatalf("no %q in %v", s.text.LineSpacing, tt.Texts())
	}
	row, ok := tt.Find(s.text.ChineseTypesetting)
	if !ok || row.Y <= spacing.Y {
		t.Fatalf("switch %v after line spacing %v: %v", row, spacing, ok)
	}
	if !f.prefs.ChineseTypesetting {
		t.Fatal("off by default")
	}
	// The switch ends where the Line spacing choices end, centered on the label and its help, which may wrap.
	help, _ := tt.Find(s.text.ChineseTypesettingHelp)
	relaxed, _ := tt.Find(s.text.SpacingRelaxed)
	toggle := func() {
		tt.ClickAt(relaxed.X+relaxed.W-relaxed.H/2, (row.Y+help.Y+help.H)/2)
		tt.Frame()
	}
	toggle()
	if len(f.changes) != 1 || f.prefs.ChineseTypesetting {
		t.Fatalf("changes %v, prefs %+v", f.changes, f.prefs)
	}
	toggle()
	if !f.prefs.ChineseTypesetting {
		t.Fatalf("not back on: %+v", f.prefs)
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

// pickerFonts is the family list of the font picker tests: Menlo is installed twice over as far as case goes.
var pickerFonts = []string{"Arial", "Georgia", "Hiragino Sans", "Menlo", "Noto Sans", "Noto Sans SC", "Songti SC"}

// openFonts opens the Interface font picker with the families known.
func openFonts(t *testing.T, fonts []string) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	s, f, tt := openAppearance(t, "en")
	f.fonts = fonts
	tt.Frame()
	press(t, tt, s.text.SystemFont)
	if !s.uiFont.open {
		t.Fatalf("not open: %v", tt.Texts())
	}
	return s, f, tt
}

func TestFontPickerOpens(t *testing.T) {
	s, _, tt := openFonts(t, pickerFonts)
	if !tt.Focused(s.text.FontSearch) {
		t.Fatalf("focus is not in the search field")
	}
	for _, name := range pickerFonts {
		if !tt.HasText(name) {
			t.Errorf("missing %q in %v", name, tt.Texts())
		}
	}
	if s.uiFont.pinned != 1 || len(s.uiFont.shown) != 1+len(pickerFonts) || s.uiFont.hl != 0 {
		t.Fatalf("pinned %d, shown %d, highlight %d", s.uiFont.pinned, len(s.uiFont.shown), s.uiFont.hl)
	}
}

func TestFontPickerFilters(t *testing.T) {
	s, _, tt := openFonts(t, pickerFonts)
	tt.Type("SANS")
	tt.Frame()
	var got []string
	for _, o := range s.uiFont.shown {
		got = append(got, o.name)
	}
	if !slices.Equal(got, []string{"Hiragino Sans", "Noto Sans", "Noto Sans SC"}) {
		t.Fatalf("shown %v", got)
	}
	if tt.HasText("Menlo") || tt.HasText(s.text.FontNone) {
		t.Fatalf("texts %v", tt.Texts())
	}
	// A pinned row that does not match goes, a substring of a name anywhere matches.
	tt.Type("zz")
	tt.Frame()
	if len(s.uiFont.shown) != 0 || s.uiFont.hl != -1 {
		t.Fatalf("shown %v", s.uiFont.shown)
	}
}

func TestFontPickerNoRepeat(t *testing.T) {
	s, f, tt := openAppearance(t, "en")
	f.prefs.Font = "Georgia"
	f.fonts = pickerFonts
	tt.Frame()
	press(t, tt, "Georgia")
	seen := map[string]bool{}
	for _, o := range s.uiFont.shown {
		if seen[o.value] {
			t.Fatalf("%q twice in %v", o.value, s.uiFont.shown)
		}
		seen[o.value] = true
	}
	if s.uiFont.pinned != 2 || len(s.uiFont.shown) != 1+len(pickerFonts) {
		t.Fatalf("pinned %d, shown %d", s.uiFont.pinned, len(s.uiFont.shown))
	}
}

func TestFontPickerEmpty(t *testing.T) {
	s, _, tt := openFonts(t, pickerFonts)
	tt.Type("Fraktur")
	tt.Frame()
	if want := "No fonts match “Fraktur”"; !tt.HasText(want) {
		t.Fatalf("want %q in %v", want, tt.Texts())
	}
	if rule, status := s.uiFont.layout(true); rule || !status || s.uiFont.rows(true) != 1 {
		t.Fatalf("rule %v, status %v", rule, status)
	}
	zh := &Screen{text: translations["zh-Hans"]}
	if got := zh.fill(zh.text.FontNone, "{q}", "Fraktur"); got != "没有匹配「Fraktur」的字体" {
		t.Fatalf("zh %q", got)
	}
}

func TestFontPickerKeys(t *testing.T) {
	s, f, tt := openFonts(t, pickerFonts)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	if s.uiFont.hl != 2 {
		t.Fatalf("highlight %d", s.uiFont.hl)
	}
	tt.Key(0, ui.KeyEnd)
	if s.uiFont.hl != len(pickerFonts) {
		t.Fatalf("highlight %d", s.uiFont.hl)
	}
	tt.Key(0, ui.KeyDown)
	if s.uiFont.hl != len(pickerFonts) {
		t.Fatalf("it wrapped to %d", s.uiFont.hl)
	}
	tt.Key(0, ui.KeyHome)
	tt.Key(0, ui.KeyUp)
	if s.uiFont.hl != 0 {
		t.Fatalf("it wrapped to %d", s.uiFont.hl)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	if s.uiFont.open || f.prefs.Font != "Georgia" || len(f.changes) != 1 {
		t.Fatalf("open %v, font %q, changes %d", s.uiFont.open, f.prefs.Font, len(f.changes))
	}
	if f.back != 0 {
		t.Fatalf("back %d", f.back)
	}
	if !tt.Focused(s.text.UIFont) {
		t.Fatalf("focus did not return to the control")
	}
}

func TestFontPickerEscape(t *testing.T) {
	s, f, tt := openFonts(t, pickerFonts)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEscape)
	if s.uiFont.open || len(f.changes) != 0 || f.back != 0 {
		t.Fatalf("open %v, changes %d, back %d", s.uiFont.open, len(f.changes), f.back)
	}
	if !tt.Focused(s.text.UIFont) {
		t.Fatalf("focus did not return to the control")
	}
	tt.Key(0, ui.KeyEscape)
	if f.back != 1 {
		t.Fatalf("back %d", f.back)
	}
}

func TestFontPickerFilterThenChoose(t *testing.T) {
	s, f, tt := openFonts(t, pickerFonts)
	tt.Type("songti")
	tt.Key(0, ui.KeyEnter)
	if f.prefs.Font != "Songti SC" || s.uiFont.open {
		t.Fatalf("font %q, open %v", f.prefs.Font, s.uiFont.open)
	}
	// System font is the first row again, and chooses the empty family.
	press(t, tt, "Songti SC")
	tt.Key(0, ui.KeyHome)
	tt.Key(0, ui.KeyEnter)
	if f.prefs.Font != "" {
		t.Fatalf("font %q", f.prefs.Font)
	}
}

func TestFontPickerClick(t *testing.T) {
	_, f, tt := openFonts(t, pickerFonts)
	press(t, tt, "Georgia")
	if f.prefs.Font != "Georgia" {
		t.Fatalf("font %q", f.prefs.Font)
	}
}

func TestFontPickerLoading(t *testing.T) {
	s, f, tt := openAppearance(t, "en")
	press(t, tt, s.text.SystemFont)
	if !tt.HasText(s.text.FontLoading) || tt.HasText("Georgia") {
		t.Fatalf("texts %v", tt.Texts())
	}
	tt.Type("geo")
	tt.Frame()
	if !tt.HasText(s.text.FontLoading) || s.uiFont.pinned != 0 {
		t.Fatalf("texts %v", tt.Texts())
	}
	f.fonts = pickerFonts
	tt.Frame()
	if tt.HasText(s.text.FontLoading) || !tt.HasText("Georgia") || tt.HasText("Menlo") || s.uiFont.query != "geo" {
		t.Fatalf("texts %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	if f.prefs.Font != "Georgia" {
		t.Fatalf("font %q", f.prefs.Font)
	}
}

func TestFontPickerAnnounces(t *testing.T) {
	_, f, tt := openFonts(t, pickerFonts)
	told := func() []string { return tt.Announcements() }
	if slices.Contains(told(), "8 fonts") {
		t.Fatalf("announced before the pause: %v", told())
	}
	f.later = 400 * time.Millisecond
	tt.Frame()
	if !slices.Contains(told(), "8 fonts") {
		t.Fatalf("announcements %v", told())
	}
	tt.Type("georgia")
	f.later = 500 * time.Millisecond
	tt.Frame()
	f.later = 900 * time.Millisecond
	tt.Frame()
	if !slices.Contains(told(), "1 font") {
		t.Fatalf("announcements %v", told())
	}
}

func TestFontPickerSize(t *testing.T) {
	for _, scale := range []float32{1, 2} {
		s, _, tt := openAppearance(t, "en")
		tt.SetScale(scale)
		many := make([]string, 400)
		for i := range many {
			many[i] = "Family " + strconv.Itoa(1000+i)
		}
		slices.Sort(many)
		s.deps.Fonts = func() []string { return many }
		tt.Frame()
		control := rect(t, tt, s.text.SystemFont)
		press(t, tt, s.text.SystemFont)
		field, ok := tt.Find("Search fonts")
		if !ok {
			t.Fatalf("texts %v", tt.Texts())
		}
		if field.X > control.X+control.W+2*scale {
			t.Fatalf("scale %v: search field at %v, control at %v", scale, field, control)
		}
		built := 0
		for _, text := range tt.Texts() {
			if strings.HasPrefix(text, "Family ") {
				built++
			}
		}
		if built == 0 || built > 40 {
			t.Fatalf("scale %v: %d of 400 families built", scale, built)
		}
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
			for g := range len(pages) {
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

// openReadingFonts opens the Reading font picker with the families known.
func openReadingFonts(t *testing.T, p prefs.Preferences) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	s, f, tt := openAppearance(t, "en")
	f.fonts, f.prefs = pickerFonts, p
	tt.Frame()
	label := s.text.FollowInterface
	switch p.ReadingFont {
	case "":
	case "serif":
		label = s.text.Serif
	default:
		label = p.ReadingFont
	}
	press(t, tt, label)
	if !s.rdFont.open || s.uiFont.open {
		t.Fatalf("reading picker %v, interface picker %v: %v", s.rdFont.open, s.uiFont.open, tt.Texts())
	}
	return s, f, tt
}

func TestReadingFontFirst(t *testing.T) {
	s, _, tt := openAppearance(t, "en")
	font, ok1 := tt.Find(s.text.ReadingFont)
	size, ok2 := tt.Find(s.text.TextSize)
	if !ok1 || !ok2 || font.Y >= size.Y || !tt.HasText(s.text.FollowInterface) {
		t.Fatalf("font %v (%v), size %v (%v), texts %v", font, ok1, size, ok2, tt.Texts())
	}
}

func TestReadingFontPicker(t *testing.T) {
	s, f, tt := openReadingFonts(t, prefs.Default())
	if s.rdFont.pinned != 2 || len(s.rdFont.shown) != 2+len(pickerFonts) {
		t.Fatalf("pinned %d, shown %d", s.rdFont.pinned, len(s.rdFont.shown))
	}
	if !tt.HasText(s.text.Serif) {
		t.Fatalf("no Serif row in %v", tt.Texts())
	}
	press(t, tt, s.text.Serif)
	if f.prefs.ReadingFont != prefs.ReadingSerif || s.rdFont.open || len(f.changes) != 1 {
		t.Fatalf("font %q, open %v, changes %d", f.prefs.ReadingFont, s.rdFont.open, len(f.changes))
	}
	press(t, tt, s.text.Serif)
	press(t, tt, "Georgia")
	if f.prefs.ReadingFont != "Georgia" || f.prefs.Font != "" {
		t.Fatalf("reading font %q, interface font %q", f.prefs.ReadingFont, f.prefs.Font)
	}
	press(t, tt, "Georgia")
	if s.rdFont.pinned != 3 {
		t.Fatalf("the family in use is not pinned: %d", s.rdFont.pinned)
	}
	press(t, tt, s.text.FollowInterface)
	if f.prefs.ReadingFont != "" {
		t.Fatalf("reading font %q", f.prefs.ReadingFont)
	}
}

func TestReadingFontEscape(t *testing.T) {
	s, f, tt := openReadingFonts(t, prefs.Default())
	tt.Key(0, ui.KeyEscape)
	if s.rdFont.open || f.back != 0 || len(f.changes) != 0 {
		t.Fatalf("open %v, back %d, changes %d", s.rdFont.open, f.back, len(f.changes))
	}
	tt.Key(0, ui.KeyEscape)
	if f.back != 1 {
		t.Fatalf("back %d", f.back)
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

// "Back to the timeline" shows its name and has no tooltip.
func TestBackHasNoTooltip(t *testing.T) {
	s, _, tt := setup(t, "en", true)
	tt.SetSize(1400, 900)
	tt.Frame()
	if tipShows(t, tt, s.text.BackTimeline) {
		t.Errorf("%q has a tooltip", s.text.BackTimeline)
	}
}

// The title bar gets its hairline once the page is scrolled, and loses it at the top.
func TestBarEdge(t *testing.T) {
	s, _, tt := setup(t, "en", true)
	tt.SetSize(1000, 300)
	tt.Frame()
	if s.scroll.Y != 0 {
		t.Fatalf("the page opens at %v", s.scroll.Y)
	}
	tt.Scroll(600, 200, 0, 100)
	tt.Frame()
	if s.scroll.Y <= 0 {
		t.Fatal("the page does not scroll")
	}
	tt.Scroll(600, 200, 0, -1000)
	tt.Frame()
	if s.scroll.Y != 0 {
		t.Fatalf("the page is at %v after scrolling back", s.scroll.Y)
	}
}

func TestSubscriptionsInCountsOtherEncodings(t *testing.T) {
	const rest = `<outline text="Group"><outline text="b" xmlUrl="https://example.org/b"/></outline></body></opml>`
	for name, c := range map[string]struct {
		label, title string
		enc          encoding.Encoding // nil keeps the text as UTF-8
	}{
		"UTF-8":      {"UTF-8", "中文 é", nil},
		"GBK":        {"GBK", "中文 é", simplifiedchinese.GBK},
		"ISO-8859-1": {"ISO-8859-1", "é", charmap.ISO8859_1},
	} {
		data := []byte(`<?xml version="1.0" encoding="` + c.label + `"?><opml version="2.0"><body>` +
			`<outline text="` + c.title + `" xmlUrl="https://example.org/a"/>` + rest)
		if c.enc != nil {
			var err error
			if data, err = c.enc.NewEncoder().Bytes(data); err != nil {
				t.Fatal(err)
			}
		}
		if got := subscriptionsIn(data); got != 2 {
			t.Errorf("%s: %d subscriptions, want 2", name, got)
		}
	}
}

// TestEscapeInField has Escape in a text field give up the focus, not leave, and keep what was typed.
func TestEscapeInField(t *testing.T) {
	s, f, tt := openAI(t, "en", true)
	r, _ := tt.Find(s.text.Model)
	tt.ClickAt(r.X+20, r.Y+r.H+28)
	tt.Type("-2")
	typed := s.aiModel
	if !s.typing || typed == "small" {
		t.Fatalf("typing %v, model %q", s.typing, typed)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if f.back != 0 || s.aiModel != typed || s.typing {
		t.Fatalf("back %d, model %q, typing %v", f.back, s.aiModel, s.typing)
	}
	// The next Escape finds the page with a change not saved, and stays.
	tt.Key(0, ui.KeyEscape)
	if f.back != 0 || !slices.Contains(tt.Announcements(), s.text.Unsaved) {
		t.Fatalf("back %d, announcements %v", f.back, tt.Announcements())
	}
}

// TestEscapeUnsaved has Escape keep the page, and say so, while text on it is not saved, in either language.
func TestEscapeUnsaved(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		s, f, tt := openAI(t, language, true)
		edit(tt, func() { s.aiModel = "other" })
		tt.Key(0, ui.KeyEscape)
		if f.back != 0 || s.aiModel != "other" || !slices.Contains(tt.Announcements(), s.text.Unsaved) || s.text.Unsaved == "" {
			t.Fatalf("%s: back %d, model %q, announcements %v", language, f.back, s.aiModel, tt.Announcements())
		}
		// The back button still leaves.
		press(t, tt, s.text.BackTimeline)
		if f.back != 1 {
			t.Fatalf("%s: back %d", language, f.back)
		}
	}
	for name, change := range map[string]func(s *Screen){
		"prompt": func(s *Screen) { s.prompts[0] = "mine" },
		"rsshub": func(s *Screen) { s.base += "\nhttps://hub.example.com" },
		"muted":  func(s *Screen) { s.rules.muted = "spoiler" },
	} {
		s, f, tt := openAI(t, "en", true)
		edit(tt, func() { change(s) })
		tt.Key(0, ui.KeyEscape)
		if f.back != 0 {
			t.Errorf("%s: back %d", name, f.back)
		}
	}
}

// TestEscapeClean has Escape go back when nothing on the page is changed.
func TestEscapeClean(t *testing.T) {
	_, f, tt := openAI(t, "en", true)
	tt.Key(0, ui.KeyEscape)
	if f.back != 1 {
		t.Fatalf("back %d", f.back)
	}
}

// openDesktop shows the Appearance page of a desktop app on the system.
func openDesktop(t *testing.T, d *Desktop) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	s, f, _ := openAppearance(t, "en")
	s.deps.Desktop = d
	// Tall, so the card at the end of the page is in view.
	tt := ui.NewTester(func(c *ui.Context) { s.View(c); theme.Toasts(c) }, 1000, 2400)
	tt.Frame()
	return s, f, tt
}

// flip clicks the switch of a row of the Desktop card, which ends where the Line spacing choices end, centered on the
// label and its help.
func flip(t *testing.T, s *Screen, tt *ui.Tester, label, help string) {
	t.Helper()
	row, ok := tt.Find(label)
	text, ok2 := tt.Find(help)
	edge, ok3 := tt.Find(s.text.SpacingRelaxed)
	if !ok || !ok2 || !ok3 {
		t.Fatalf("no %q, %q or %q in %v", label, help, s.text.SpacingRelaxed, tt.Texts())
	}
	tt.ClickAt(edge.X+edge.W-edge.H/2, (row.Y+text.Y+text.H)/2)
	tt.Frame()
}

func TestDesktopCardRows(t *testing.T) {
	for _, tc := range []struct {
		name  string
		d     *Desktop
		shows []func(*Screen) string
		hides []func(*Screen) string
	}{
		{"mac", &Desktop{OS: "darwin", Bundled: true},
			[]func(*Screen) string{
				func(s *Screen) string { return s.text.Desktop }, func(s *Screen) string { return s.text.OpenAtLogin },
				func(s *Screen) string { return s.text.OpenAtLoginHelp }, func(s *Screen) string { return s.text.MenuBarIcon },
				func(s *Screen) string { return s.text.MenuBarIconHelp }, func(s *Screen) string { return s.text.DockBadge },
				func(s *Screen) string { return s.text.DockBadgeHelp }},
			[]func(*Screen) string{
				func(s *Screen) string { return s.text.TrayIcon }, func(s *Screen) string { return s.text.OpenAtLoginNeedsApp },
				func(s *Screen) string { return s.text.TrayFailed }}},
		{"windows", &Desktop{OS: "windows"},
			[]func(*Screen) string{
				func(s *Screen) string { return s.text.Desktop }, func(s *Screen) string { return s.text.OpenAtLogin },
				func(s *Screen) string { return s.text.OpenAtLoginHelpTray }, func(s *Screen) string { return s.text.TrayIcon },
				func(s *Screen) string { return s.text.TrayIconHelp }},
			[]func(*Screen) string{
				func(s *Screen) string { return s.text.MenuBarIcon }, func(s *Screen) string { return s.text.DockBadge },
				func(s *Screen) string { return s.text.OpenAtLoginNeedsApp }}},
		{"linux", &Desktop{OS: "linux"},
			[]func(*Screen) string{func(s *Screen) string { return s.text.TrayIcon }},
			[]func(*Screen) string{func(s *Screen) string { return s.text.DockBadge }}},
		{"server", nil, nil,
			[]func(*Screen) string{func(s *Screen) string { return s.text.Desktop }, func(s *Screen) string { return s.text.OpenAtLogin }}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, tt := openDesktop(t, tc.d)
			for _, text := range tc.shows {
				if want := text(s); !tt.HasText(want) {
					t.Errorf("no %q in %v", want, tt.Texts())
				}
			}
			for _, text := range tc.hides {
				if want := text(s); tt.HasText(want) {
					t.Errorf("unexpected %q", want)
				}
			}
		})
	}
}

func TestDesktopCardNeedsBundle(t *testing.T) {
	s, f, tt := openDesktop(t, &Desktop{OS: "darwin"})
	if !tt.HasText(s.text.OpenAtLoginNeedsApp) || tt.HasText(s.text.OpenAtLoginHelp) {
		t.Fatalf("texts %v", tt.Texts())
	}
	flip(t, s, tt, s.text.OpenAtLogin, s.text.OpenAtLoginNeedsApp)
	if len(f.changes) != 0 {
		t.Errorf("a disabled switch changed %v", f.changes)
	}
}

func TestDesktopCardSwitches(t *testing.T) {
	s, f, tt := openDesktop(t, &Desktop{OS: "darwin", Bundled: true})
	for _, tc := range []struct {
		label, help string
		on          func() bool
	}{
		{s.text.OpenAtLogin, s.text.OpenAtLoginHelp, func() bool { return f.prefs.OpenAtLogin }},
		{s.text.MenuBarIcon, s.text.MenuBarIconHelp, func() bool { return f.prefs.TrayIcon }},
		{s.text.DockBadge, s.text.DockBadgeHelp, func() bool { return f.prefs.DockBadge }},
	} {
		if tc.on() {
			t.Fatalf("%s is on by default", tc.label)
		}
		flip(t, s, tt, tc.label, tc.help)
		if !tc.on() {
			t.Fatalf("%s did not turn on: %+v", tc.label, f.prefs)
		}
		flip(t, s, tt, tc.label, tc.help)
		if tc.on() {
			t.Fatalf("%s did not turn off: %+v", tc.label, f.prefs)
		}
	}
}

func TestDesktopCardLoginFailed(t *testing.T) {
	s, f, tt := openDesktop(t, &Desktop{OS: "linux"})
	if tt.HasText(s.text.LoginFailed) {
		t.Fatal("the error shows before anything failed")
	}
	s.LoginFailed()
	tt.Frame()
	if !tt.HasText(s.text.LoginFailed) {
		t.Fatalf("texts %v", tt.Texts())
	}
	flip(t, s, tt, s.text.OpenAtLogin, s.text.OpenAtLoginHelpTray)
	if tt.HasText(s.text.LoginFailed) || !f.prefs.OpenAtLogin {
		t.Errorf("the error stayed after the switch was used: %+v", f.prefs)
	}
}

func TestDesktopCardTrayFailed(t *testing.T) {
	s, f, tt := openDesktop(t, &Desktop{OS: "linux"})
	s.TrayFailed()
	tt.Frame()
	if !tt.HasText(s.text.TrayFailed) {
		t.Fatalf("texts %v", tt.Texts())
	}
	flip(t, s, tt, s.text.TrayIcon, s.text.TrayIconHelp)
	if tt.HasText(s.text.TrayFailed) || !f.prefs.TrayIcon {
		t.Errorf("the error stayed after the switch was used: %+v", f.prefs)
	}
}
