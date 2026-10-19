// Package settings is the settings screen, in the place of the timeline: the RSSHub instance and how long entries are
// kept, OPML import and export, the AI endpoint and prompts, and the devices signed in to the server; see
// design/screens/settings.html. Everything
// goes through the HTTP API; the settings belong to the server, so every device connected to it shares them.
package settings

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/theme"
)

//go:embed copy.json
var catalog []byte

type copy struct {
	Menu, Settings, ThisComputer, BackTimeline, ServerNote, Shortcut, Lead, Fetching, OPML, Devices, RSSHub        string
	RSSHubHelp, RSSHubPlaceholder, Save, Saved, RetentionSaved, InvalidBase, SaveFailed, Retention, RetentionHelp  string
	Forever, Days, Week, Month, Months3, Months6, Year, OPMLHelp, ImportLabel, ImportHelp, ExportLabel, ExportHelp string
	OPMLFiles, NotAdded, Import, Export, Importing, Imported, ImportedOk, InvalidOPML, TooLarge, ReadFailed        string
	ImportFailed, Exported, ExportFailed, WriteFailed, Dismiss, InvalidURL, InvalidRoute, InvalidKind              string
	InvalidSource, DevicesHelp, LocalDevices, ThisDevice, UnknownDevice, Revoke, SignedIn, RevokeTitle             string
	RevokeBody, Cancel, RevokeConfirm, Revoked, RevokeFailed, Loading, Error, ErrorHint, Retry, Now, Minutes       string
	Hours, Yesterday, DaysAgo, DateLayout, YearLayout                                                              string
	AI, AILead, Endpoint, EndpointHelp, EndpointPlaceholder, Model, ModelPlaceholder, APIKey, KeySet, KeyUnset     string
	KeyHelp, KeyPlaceholder, KeyReplace, RemoveKey, KeyRemoved, Language, LanguageHelp, Test, Testing, Tested      string
	TestFirst, AIUnauthorized, AIUnreachable, AITimeout, AIRateLimited, AIUpstream, AISaved, InvalidAIBase         string
	Prompts, PromptSummary, PromptTranslate, PromptHelp, Reset, InvalidPrompt                                      string
}

var translations = func() map[string]copy {
	var values map[string]copy
	if err := json.Unmarshal(catalog, &values); err != nil {
		panic(err)
	}
	return values
}()

// MenuLabel is the menu item that opens the screen, in the language of the system locale.
func MenuLabel(systemLocale string) string { return translations[locale.Language(systemLocale)].Menu }

// retentions are the days entries are kept that the picker offers, 0 for forever.
var retentions = []int{0, 7, 30, 90, 180, 365}

// languages are the languages summaries and translations can be written in, each named in itself.
var languages = []struct{ code, name string }{{"zh-Hans", "简体中文"}, {"en", "English"}}

// The groups of the sidebar.
const (
	fetching = iota
	opml
	ai
	devices
)

// API is what the screen reads and writes on the server; client.Session is one.
type API interface {
	Settings(ctx context.Context) (client.Settings, error)
	SetSettings(ctx context.Context, patch client.SettingsPatch) (client.Settings, error)
	Devices(ctx context.Context) ([]client.Device, error)
	RevokeDevice(ctx context.Context, id int64) error
	ImportOPML(ctx context.Context, opml []byte) (client.Imported, error)
	ExportOPML(ctx context.Context) ([]byte, error)
	TestAI(ctx context.Context) (string, error)
}

// Deps is what the screen does outside itself. Go runs work away from the UI thread and then the function it
// returns on the UI thread; Now is the clock, time.Now by default. Open and Save ask for the OPML file to import and
// the one to export to, filter naming its kind; they block until the user answers, so they run in Go's work and
// return "" when cancelled. Back returns to the timeline, Imported tells it subscriptions were added, and Expired
// shows the connect screen because the server no longer takes the session.
type Deps struct {
	Go       func(work func() (apply func()))
	Now      func() time.Time
	Open     func(filter string) (string, error)
	Save     func(name, filter string) (string, error)
	Back     func()
	Imported func()
	Expired  func()
}

// How far something has loaded.
const (
	loading = iota
	loaded
	failed
)

// status is the line under a setting after it was saved, or why it was not; muted for a note, spinning while
// something is under way.
type status struct {
	ok, muted, spin bool
	text            string
}

// The tones of the result of an import or export.
const (
	done = iota
	partly
	refused
)

type result struct {
	tone   int
	text   string
	fails  []client.ImportFail
	export bool // shown under the export row, else under the import row
}

// Screen keeps the settings as loaded and the work under way.
type Screen struct {
	deps     Deps
	text     copy
	api      API
	server   string // the host of the server, "" for this computer
	signedIn bool   // the session has a token; a server without a password has no devices
	gen      int    // counts servers used, so the results of the one before are dropped
	loads    int    // counts loads, so a result the user moved on from is dropped
	group    int

	state       int
	settings    client.Settings
	base        string // the RSSHub instance as typed
	saving      bool
	invalid     bool
	baseStatus  status
	focusBase   bool
	retention   int
	keeping     bool // the retention is being saved
	keepStatus  status
	devices     []client.Device
	deviceState int

	aiBase, aiModel, aiKey string // as typed; the key saved is never sent back
	aiLanguage             string
	aiSaving               bool
	aiInvalid              bool // the server refused the endpoint
	focusAIBase            bool
	aiStatus               status
	testing                bool
	prompts                [2]string // the summary and translation prompts as typed
	promptSaving           bool
	promptStatus           status

	busy      bool   // a file is being chosen, imported or exported
	importing string // the name of the file being imported
	result    result

	revoking bool
	doomed   client.Device
	announce string
	toast    string
}

// New selects the best available translation, falling back to English. The screen shows nothing until Use.
func New(systemLocale string, deps Deps) *Screen {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Screen{deps: deps, text: translations[locale.Language(systemLocale)]}
}

// Use makes the screen work on api, whose host is server ("" for this computer); signedIn is whether its session
// signed in with a password, without which the server has no devices.
func (s *Screen) Use(api API, server string, signedIn bool) {
	s.api, s.server, s.signedIn = api, server, signedIn
	s.gen++
	s.group, s.busy, s.importing, s.result, s.revoking = fetching, false, "", result{}, false
	s.saving, s.keeping, s.aiSaving, s.testing, s.promptSaving = false, false, false, false, false
	s.state, s.deviceState = loading, loading
}

// OpenAI opens the screen on the AI group, from an entry point of summaries and translations.
func (s *Screen) OpenAI() {
	s.group = ai
	s.Open()
}

// Open loads the settings and the devices again, as the screen is shown.
func (s *Screen) Open() {
	s.baseStatus, s.keepStatus, s.invalid, s.revoking = status{}, status{}, false, false
	s.aiStatus, s.promptStatus, s.aiInvalid = status{}, status{}, false
	if !s.busy {
		s.result = result{}
	}
	s.loadSettings()
	s.loadDevices()
}

func (s *Screen) expired(err error) bool {
	if !errors.Is(err, client.ErrUnauthorized) {
		return false
	}
	s.gen++
	s.api = nil
	s.busy, s.saving, s.keeping, s.revoking = false, false, false, false
	s.aiSaving, s.testing, s.promptSaving = false, false, false
	s.deps.Expired()
	return true
}

func code(err error) string {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

func (s *Screen) loadSettings() {
	if s.api == nil {
		return
	}
	s.loads++
	s.state = loading
	gen, run, api := s.gen, s.loads, s.api
	s.deps.Go(func() func() {
		settings, err := api.Settings(context.Background())
		return func() {
			if gen != s.gen || run != s.loads || s.expired(err) {
				return
			}
			if err != nil {
				s.state = failed
				s.announce = s.text.Error
				return
			}
			s.state, s.settings = loaded, settings
			s.base, s.retention = settings.RSSHubBaseURL, settings.RetentionDays
			s.aiBase, s.aiModel, s.aiKey, s.aiLanguage = settings.AIBaseURL, settings.AIModel, "", settings.AILanguage
			s.prompts = [2]string{settings.PromptSummary, settings.PromptTranslate}
		}
	})
}

func (s *Screen) loadDevices() {
	if s.api == nil {
		return
	}
	if !s.signedIn {
		s.devices, s.deviceState = nil, loaded
		return
	}
	s.deviceState = loading
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		list, err := api.Devices(context.Background())
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			if err != nil {
				s.deviceState = failed
				return
			}
			s.deviceState = loaded
			s.devices = list
			slices.SortStableFunc(s.devices, func(a, b client.Device) int {
				if a.Current != b.Current {
					if a.Current {
						return -1
					}
					return 1
				}
				return 0
			})
		}
	})
}

// dirty reports whether the RSSHub instance typed is not the one saved.
func (s *Screen) dirty() bool { return strings.TrimSpace(s.base) != s.settings.RSSHubBaseURL }

func (s *Screen) saveBase() {
	if s.saving || s.api == nil || s.state != loaded || !s.dirty() {
		return
	}
	base := strings.TrimSpace(s.base)
	s.saving, s.baseStatus, s.invalid = true, status{}, false
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		settings, err := api.SetSettings(context.Background(), client.SettingsPatch{RSSHubBaseURL: &base})
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			s.saving = false
			switch {
			case err == nil:
				s.settings.RSSHubBaseURL = settings.RSSHubBaseURL
				s.base = settings.RSSHubBaseURL
				s.baseStatus = status{ok: true, text: s.text.Saved}
			case code(err) == "invalid_rsshub_base_url":
				s.invalid, s.focusBase = true, true
				s.baseStatus = status{text: s.text.InvalidBase}
			default:
				s.baseStatus = status{text: s.text.SaveFailed}
			}
			s.announce = s.baseStatus.text
		}
	})
}

func (s *Screen) saveRetention() {
	if s.api == nil {
		return
	}
	days := s.retention
	s.keeping, s.keepStatus = true, status{}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		settings, err := api.SetSettings(context.Background(), client.SettingsPatch{RetentionDays: &days})
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			s.keeping = false
			if err != nil {
				s.retention = s.settings.RetentionDays
				s.keepStatus = status{text: s.text.SaveFailed}
			} else {
				s.settings.RetentionDays = settings.RetentionDays
				s.retention = settings.RetentionDays
				s.keepStatus = status{ok: true, text: s.text.RetentionSaved}
			}
			s.announce = s.keepStatus.text
		}
	})
}

// aiDirty reports whether the AI endpoint as typed is not the one saved; a key typed is always a change.
func (s *Screen) aiDirty() bool {
	return strings.TrimSpace(s.aiBase) != s.settings.AIBaseURL || strings.TrimSpace(s.aiModel) != s.settings.AIModel ||
		strings.TrimSpace(s.aiKey) != "" || s.aiLanguage != s.settings.AILanguage
}

// saveAI saves the changes to the AI endpoint, or removes the key; only what changed is sent.
func (s *Screen) saveAI(removeKey bool) {
	if s.aiSaving || s.api == nil || s.state != loaded || !removeKey && !s.aiDirty() {
		return
	}
	var patch client.SettingsPatch
	if removeKey {
		patch.AIAPIKey = new(string)
	} else {
		if base := strings.TrimSpace(s.aiBase); base != s.settings.AIBaseURL {
			patch.AIBaseURL = &base
		}
		if model := strings.TrimSpace(s.aiModel); model != s.settings.AIModel {
			patch.AIModel = &model
		}
		if key := strings.TrimSpace(s.aiKey); key != "" {
			patch.AIAPIKey = &key
		}
		if s.aiLanguage != s.settings.AILanguage {
			language := s.aiLanguage
			patch.AILanguage = &language
		}
	}
	s.aiSaving, s.aiStatus, s.aiInvalid = true, status{}, false
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		settings, err := api.SetSettings(context.Background(), patch)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			s.aiSaving = false
			switch {
			case err == nil:
				s.settings.AIBaseURL, s.settings.AIModel = settings.AIBaseURL, settings.AIModel
				s.settings.AIAPIKeySet, s.settings.AILanguage = settings.AIAPIKeySet, settings.AILanguage
				if removeKey {
					s.aiStatus = status{ok: true, text: s.text.KeyRemoved}
				} else {
					s.aiBase, s.aiModel, s.aiKey, s.aiLanguage = settings.AIBaseURL, settings.AIModel, "", settings.AILanguage
					s.aiStatus = status{ok: true, text: s.text.AISaved}
				}
				s.announce = s.aiStatus.text
			case code(err) == "invalid_ai_base_url":
				s.aiInvalid, s.focusAIBase = true, true
				s.announce = s.text.InvalidAIBase
			default:
				s.aiStatus = status{text: s.text.SaveFailed}
				s.announce = s.aiStatus.text
			}
		}
	})
}

// test sends a short request to the AI endpoint saved.
func (s *Screen) test() {
	if s.testing || s.api == nil || !s.settings.AIConfigured() || s.aiDirty() {
		return
	}
	s.testing, s.aiStatus = true, status{}
	s.announce = s.text.Testing
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		model, err := api.TestAI(context.Background())
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			s.testing = false
			if err == nil {
				s.aiStatus = status{ok: true, text: s.fill(s.text.Tested, "{model}", model)}
			} else {
				s.aiStatus = status{text: s.reason(code(err))}
			}
			s.announce = s.aiStatus.text
		}
	})
}

// reason says why the AI endpoint failed a test, from the code the server gave.
func (s *Screen) reason(code string) string {
	switch code {
	case "":
		return s.text.ErrorHint
	case "ai_unauthorized":
		return s.text.AIUnauthorized
	case "ai_unreachable":
		return s.text.AIUnreachable
	case "ai_timeout":
		return s.text.AITimeout
	case "ai_rate_limited":
		return s.text.AIRateLimited
	}
	return s.text.AIUpstream
}

// defaults are the prompts the server uses until they are changed.
func (s *Screen) defaults() [2]string {
	return [2]string{s.settings.DefaultPromptSummary, s.settings.DefaultPromptTranslate}
}

// promptsDirty reports whether a prompt as typed is not the one saved.
func (s *Screen) promptsDirty() bool {
	saved := [2]string{s.settings.PromptSummary, s.settings.PromptTranslate}
	for i, p := range s.prompts {
		if strings.TrimSpace(p) != saved[i] {
			return true
		}
	}
	return false
}

// savePrompts saves the prompts changed; one that is the default, or empty, is saved as "" to follow the default.
func (s *Screen) savePrompts() {
	if s.promptSaving || s.api == nil || s.state != loaded || !s.promptsDirty() {
		return
	}
	var patch client.SettingsPatch
	saved := [2]string{s.settings.PromptSummary, s.settings.PromptTranslate}
	fields := [2]**string{&patch.PromptSummary, &patch.PromptTranslate}
	for i, p := range s.prompts {
		p = strings.TrimSpace(p)
		if p == saved[i] {
			continue
		}
		if p == s.defaults()[i] {
			p = ""
		}
		*fields[i] = &p
	}
	s.promptSaving, s.promptStatus = true, status{}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		settings, err := api.SetSettings(context.Background(), patch)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			s.promptSaving = false
			switch {
			case err == nil:
				s.settings.PromptSummary, s.settings.PromptTranslate = settings.PromptSummary, settings.PromptTranslate
				s.prompts = [2]string{settings.PromptSummary, settings.PromptTranslate}
				s.promptStatus = status{ok: true, text: s.text.AISaved}
			case code(err) == "invalid_prompt":
				s.promptStatus = status{text: s.text.InvalidPrompt}
			default:
				s.promptStatus = status{text: s.text.SaveFailed}
			}
			s.announce = s.promptStatus.text
		}
	})
}

var (
	errTooLarge = errors.New("file too large")
	errRead     = errors.New("file unreadable")
)

// readOPML reads the file at path, refusing one larger than the server takes before sending it.
func readOPML(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errRead
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, client.MaxOPML+1))
	switch {
	case err != nil:
		return nil, errRead
	case len(data) > client.MaxOPML:
		return nil, errTooLarge
	}
	return data, nil
}

func (s *Screen) fill(text string, pairs ...string) string {
	return strings.NewReplacer(pairs...).Replace(text)
}

func (s *Screen) chooseImport() {
	if s.busy || s.api == nil {
		return
	}
	s.busy, s.result = true, result{}
	gen, api, filter := s.gen, s.api, s.text.OPMLFiles
	s.deps.Go(func() func() {
		path, err := s.deps.Open(filter)
		if err != nil || path == "" {
			return func() {
				if gen == s.gen {
					s.busy = false
				}
			}
		}
		name := filepath.Base(path)
		s.deps.Go(func() func() {
			data, err := readOPML(path)
			var imported client.Imported
			if err == nil {
				imported, err = api.ImportOPML(context.Background(), data)
			}
			return func() { s.imported(gen, name, imported, err) }
		})
		return func() {
			if gen == s.gen && s.busy {
				s.importing = name
				s.announce = s.fill(s.text.Importing, "{file}", name)
			}
		}
	})
}

func (s *Screen) imported(gen int, name string, imported client.Imported, err error) {
	if gen != s.gen || s.expired(err) {
		return
	}
	s.busy, s.importing = false, ""
	file := "{file}"
	switch {
	case err == nil:
		r := result{tone: done, text: s.fill(s.text.ImportedOk, file, name, "{added}", strconv.Itoa(imported.Added),
			"{existing}", strconv.Itoa(imported.Existing))}
		if len(imported.Failed) > 0 {
			r = result{tone: partly, text: s.fill(s.text.Imported, file, name, "{added}", strconv.Itoa(imported.Added),
				"{existing}", strconv.Itoa(imported.Existing), "{failed}", strconv.Itoa(len(imported.Failed))),
				fails: imported.Failed}
		}
		s.result = r
		if imported.Added > 0 {
			s.deps.Imported()
		}
	case errors.Is(err, errTooLarge) || code(err) == "opml_too_large":
		s.result = result{tone: refused, text: s.fill(s.text.TooLarge, file, name)}
	case errors.Is(err, errRead):
		s.result = result{tone: refused, text: s.fill(s.text.ReadFailed, file, name)}
	case code(err) == "invalid_opml":
		s.result = result{tone: refused, text: s.fill(s.text.InvalidOPML, file, name)}
	default:
		s.result = result{tone: refused, text: s.fill(s.text.ImportFailed, file, name)}
	}
	s.announce = s.result.text
}

// subscriptionsIn counts the subscriptions of an OPML file: the outlines with a feed address or an RSSHub route.
func subscriptionsIn(data []byte) int {
	n := 0
	d := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := d.Token()
		if err != nil {
			return n
		}
		if e, ok := token.(xml.StartElement); ok && e.Name.Local == "outline" {
			for _, a := range e.Attr {
				if (a.Name.Local == "xmlUrl" || a.Name.Local == "orielRsshubPath") && a.Value != "" {
					n++
					break
				}
			}
		}
	}
}

func (s *Screen) export() {
	if s.busy || s.api == nil {
		return
	}
	s.busy, s.result = true, result{}
	gen, api, filter := s.gen, s.api, s.text.OPMLFiles
	s.deps.Go(func() func() {
		path, err := s.deps.Save("Oriel.opml", filter)
		if err != nil || path == "" {
			return func() {
				if gen == s.gen {
					s.busy = false
				}
			}
		}
		name := filepath.Base(path)
		data, err := api.ExportOPML(context.Background())
		written := err == nil && os.WriteFile(path, data, 0o644) == nil
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			s.busy = false
			switch {
			case err != nil:
				s.result = result{tone: refused, text: s.text.ExportFailed}
			case !written:
				s.result = result{tone: refused, text: s.fill(s.text.WriteFailed, "{file}", name)}
			default:
				s.result = result{tone: done, text: s.fill(s.text.Exported, "{n}", strconv.Itoa(subscriptionsIn(data)),
					"{file}", name)}
			}
			s.result.export = true
			s.announce = s.result.text
		}
	})
}

func (s *Screen) revoke() {
	if s.api == nil {
		return
	}
	gen, api, d := s.gen, s.api, s.doomed
	name, _ := s.device(d)
	s.deps.Go(func() func() {
		err := api.RevokeDevice(context.Background(), d.ID)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			if err != nil && code(err) != "not_found" {
				s.toast = s.fill(s.text.RevokeFailed, "{name}", name)
				return
			}
			s.devices = slices.DeleteFunc(s.devices, func(x client.Device) bool { return x.ID == d.ID })
			s.toast = s.fill(s.text.Revoked, "{name}", name)
		}
	})
}

// device names a device by the User-Agent it signed in with, as "Oriel · macOS" or "Safari · iPhone", and chooses
// its glyph.
func (s *Screen) device(d client.Device) (string, *ui.SVG) {
	ua := strings.TrimSpace(d.UserAgent)
	if ua == "" {
		return s.text.UnknownDevice, icons.Laptop
	}
	platform := ""
	if rest, ok := strings.CutPrefix(ua, "Oriel"); ok && (rest == "" || rest[0] == '/' || rest[0] == ' ') {
		if _, inside, ok := strings.Cut(rest, "("); ok {
			platform, _, _ = strings.Cut(inside, ")")
		}
		if platform == "" {
			return "Oriel", icons.Laptop
		}
		return "Oriel · " + platform, glyph(platform)
	}
	for _, p := range []struct{ mark, name string }{
		{"iPhone", "iPhone"}, {"iPad", "iPad"}, {"Android", "Android"}, {"Windows", "Windows"},
		{"Macintosh", "macOS"}, {"Mac OS X", "macOS"}, {"CrOS", "ChromeOS"}, {"Linux", "Linux"},
	} {
		if strings.Contains(ua, p.mark) {
			platform = p.name
			break
		}
	}
	browser := ""
	for _, b := range []struct{ mark, name string }{
		{"Edg", "Edge"}, {"Firefox/", "Firefox"}, {"FxiOS/", "Firefox"}, {"CriOS/", "Chrome"}, {"Chrome/", "Chrome"},
		{"Safari/", "Safari"},
	} {
		if strings.Contains(ua, b.mark) {
			browser = b.name
			break
		}
	}
	if platform == "" || browser == "" {
		return ua, icons.Laptop
	}
	return browser + " · " + platform, glyph(platform)
}

func glyph(platform string) *ui.SVG {
	switch platform {
	case "iPhone", "iPad", "Android":
		return icons.Smartphone
	case "Windows", "Linux", "ChromeOS":
		return icons.Monitor
	}
	return icons.Laptop
}

// date is the day of a time, with the year when it is not this one.
func (s *Screen) date(ms int64, now time.Time) string {
	at := time.UnixMilli(ms).In(now.Location())
	if at.Year() == now.Year() {
		return at.Format(s.text.DateLayout)
	}
	return at.Format(s.text.YearLayout)
}

// ago is how long ago a time was, within a week, else its day.
func (s *Screen) ago(ms int64, now time.Time) string {
	at := time.UnixMilli(ms).In(now.Location())
	d := now.Sub(at)
	day := func(t time.Time) time.Time { y, m, d := t.Date(); return time.Date(y, m, d, 12, 0, 0, 0, time.UTC) }
	days := int(day(now).Sub(day(at)).Hours() / 24)
	n := func(text string, v int) string { return s.fill(text, "{n}", strconv.Itoa(v)) }
	switch {
	case d < time.Minute:
		return s.text.Now
	case d < time.Hour:
		return n(s.text.Minutes, int(d/time.Minute))
	case days == 0:
		return n(s.text.Hours, int(d/time.Hour))
	case days == 1:
		return s.text.Yesterday
	case days < 7:
		return n(s.text.DaysAgo, days)
	}
	return s.date(ms, now)
}

func (s *Screen) groupName(g int) string {
	switch g {
	case opml:
		return s.text.OPML
	case ai:
		return s.text.AI
	case devices:
		return s.text.Devices
	}
	return s.text.Fetching
}

// keep names a retention in days.
func (s *Screen) keep(days int) string {
	switch days {
	case 0:
		return s.text.Forever
	case 7:
		return s.text.Week
	case 30:
		return s.text.Month
	case 90:
		return s.text.Months3
	case 180:
		return s.text.Months6
	case 365:
		return s.text.Year
	}
	return s.fill(s.text.Days, "{n}", strconv.Itoa(days))
}

// px converts a size of the mockup, drawn at a 13 px font, to the theme's text size.
func px(t *ui.Theme, v float32) float32 { return t.Rem(v / 13) }

// View draws the settings; it does no disk or network work, which runs through Deps.
func (s *Screen) View(c *ui.Context) {
	t := c.Theme()
	if !s.revoking {
		c.OnShortcut(0, ui.KeyEscape, s.deps.Back)
	}
	width, _ := c.Size()
	compact := width < 760
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(t.Background).Children(func() {
		s.sidebar(c, compact)
		ui.Box(c).Width(1).Background(t.Border)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).FillWidth().Height(px(t, 48)).AlignItems(ui.Center).Padding(0, t.Space(3), 0, t.Space(5)).
				Children(func() {
					ui.Text(c, s.groupName(s.group)).FontSize(px(t, 15)).FontWeight(600).SingleLine().MinWidth(0).
						Shrink(1).Role(ui.RoleHeading).Level(1)
				})
			ui.Box(c).FillWidth().Height(1).Background(t.Border)
			s.content(c, compact)
		})
	})
	if s.revoking {
		name, _ := s.device(s.doomed)
		if ui.AlertDialog(c, &s.revoking, s.fill(s.text.RevokeTitle, "{name}", name), s.text.RevokeBody,
			s.text.Cancel, s.text.RevokeConfirm) == 1 {
			s.revoke()
		}
	}
	if s.announce != "" {
		c.Announce(s.announce)
		s.announce = ""
	}
	if s.toast != "" {
		c.Toast(s.toast)
		s.toast = ""
	}
}

func (s *Screen) sidebar(c *ui.Context, compact bool) {
	t := c.Theme()
	width := px(t, 212)
	if compact {
		width = px(t, 184)
	}
	ui.Column(c).Width(width).Background(t.Surface).Children(func() {
		ui.Row(c).FillWidth().Height(px(t, 48)).AlignItems(ui.Center).PaddingX(t.Space(2)).Children(func() {
			label := s.text.BackTimeline
			tip := strings.NewReplacer("{action}", label, "{key}", "Esc").Replace(s.text.Shortcut)
			b := ui.ButtonBase(c).Height(px(t, 28)).MinWidth(0).Shrink(1).Gap(2).Padding(0, t.Space(2), 0, t.Space(1)).
				Radius(t.Space(1.5)).Label(label).Tooltip(tip)
			color := t.TextMuted
			if b.Hovered() {
				b.Background(t.SurfaceHover)
				color = t.Text
			}
			b.Children(func() {
				ui.Icon(c, icons.ChevronLeft).FontSize(px(t, 16)).TextColor(color)
				ui.Text(c, label).SingleLine().MinWidth(0).Shrink(1).TextColor(color)
			})
			if b.Clicked() {
				s.deps.Back()
			}
		})
		ui.Box(c).FillWidth().Height(1).Background(t.Border)
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(t.Space(2)).Label(s.text.Settings).Children(func() {
				ui.Text(c, s.text.Settings).SingleLine().FontSize(px(t, 11)).FontWeight(600).TextColor(t.TextMuted).
					Margin(t.Space(3), t.Space(2), t.Space(1), t.Space(2))
				for g, icon := range []*ui.SVG{icons.Rss, icons.ArrowUpDown, icons.Sparkles, icons.Laptop} {
					s.navItem(c, g, icon)
				}
			})
		})
		s.serverBar(c)
	})
}

func (s *Screen) navItem(c *ui.Context, g int, icon *ui.SVG) {
	t := c.Theme()
	name := s.groupName(g)
	item := ui.ButtonBase(c).Key(g).FillWidth().Height(px(t, 28)).Gap(t.Space(2)).PaddingX(t.Space(2)).
		Radius(t.Space(1.5)).Label(name)
	color := t.TextMuted
	switch {
	case g == s.group:
		item.Background(t.SurfacePressed)
		color = t.Text
	case item.Hovered():
		item.Background(t.SurfaceHover)
	}
	item.Children(func() {
		ui.Icon(c, icon).FontSize(px(t, 16)).TextColor(color)
		ui.Text(c, name).Grow(1).MinWidth(0).SingleLine()
	})
	if item.Clicked() {
		s.group = g
	}
}

// serverBar says whose settings these are; unlike the timeline's, it does not open the connect screen.
func (s *Screen) serverBar(c *ui.Context) {
	t := c.Theme()
	name, icon := s.text.ThisComputer, icons.Laptop
	if s.server != "" {
		name, icon = s.server, icons.Server
	}
	ui.Box(c).FillWidth().Height(1).Background(t.Border)
	ui.Row(c).FillWidth().Height(px(t, 40)).AlignItems(ui.Center).Gap(t.Space(2)).PaddingX(t.Space(4)).
		Tooltip(s.text.ServerNote).Children(func() {
		ui.Icon(c, icon).FontSize(px(t, 16)).TextColor(t.TextMuted)
		ui.Text(c, name).Grow(1).MinWidth(0).SingleLine().FontSize(px(t, 12)).TextColor(t.TextMuted)
		ui.Box(c).Size(6, 6).Radius(3).Background(t.Success)
	})
}

func (s *Screen) content(c *ui.Context, compact bool) {
	state := s.state
	switch s.group {
	case opml:
		state = loaded
	case devices:
		state = s.deviceState
	}
	switch state {
	case failed:
		s.failure(c)
		return
	case loading:
		s.page(c, compact, func() { s.skeleton(c) })
		return
	}
	s.page(c, compact, func() {
		switch s.group {
		case opml:
			s.opmlPage(c)
		case ai:
			s.aiPage(c)
		case devices:
			s.devicesPage(c)
		default:
			s.fetchingPage(c)
		}
	})
}

func (s *Screen) page(c *ui.Context, compact bool, children func()) {
	t := c.Theme()
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		p := ui.Column(c).FillWidth().MaxWidth(px(t, 600)).Margin(0, ui.Auto)
		if compact {
			p.Padding(px(t, 20), px(t, 20), px(t, 40), px(t, 20))
		} else {
			p.Padding(px(t, 24), px(t, 32), px(t, 48), px(t, 32))
		}
		p.Children(children)
	})
}

func (s *Screen) skeleton(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().Label(s.text.Loading).Role(ui.RoleProgress).Children(func() {
		ui.Box(c).Width(px(t, 320)).MaxWidth(px(t, 360)).Height(px(t, 12)).Radius(6).Background(t.Surface).
			Margin(px(t, 10), 0)
		ui.Box(c).FillWidth().Height(px(t, 160)).Radius(10).Background(t.Surface).Margin(t.Space(4), 0, 0, 0)
	})
}

func (s *Screen) failure(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Grow(1).FillWidth().Center().Gap(t.Space(3)).Padding(t.Space(6)).Role(ui.RoleStatus).
		Children(func() {
			ui.Box(c).Size(t.Space(12), t.Space(12)).Radius(t.Space(3)).Center().Background(theme.Soft(t, t.Danger)).
				Children(func() { ui.Icon(c, icons.WifiOff).FontSize(t.Space(6)).TextColor(t.Danger) })
			ui.Text(c, s.text.Error).FontSize(px(t, 15)).FontWeight(600).Role(ui.RoleHeading).Level(2)
			ui.Text(c, s.text.ErrorHint).MaxWidth(px(t, 320)).TextColor(t.TextMuted).Wrap()
			retry := button(c, secondary, false)
			textButton(c, retry, secondary, icons.RefreshCw, s.text.Retry, false)
			if retry.Clicked() {
				if s.group == devices {
					s.loadDevices()
				} else {
					s.loadSettings()
				}
			}
		})
}

func lead(c *ui.Context, text string) {
	t := c.Theme()
	ui.Text(c, text).TextColor(t.TextMuted).Wrap().Margin(0, 0, t.Space(4), 0)
}

// card draws rows in a bordered card, a line between each.
func card(c *ui.Context, rows ...func()) {
	t := c.Theme()
	ui.Column(c).FillWidth().Radius(10).Border(1, t.Border).Background(t.Background).Children(func() {
		for i, row := range rows {
			if i > 0 {
				ui.Box(c).FillWidth().Height(1).Background(t.Border)
			}
			ui.Column(c).Key(i).FillWidth().Padding(px(t, 14), t.Space(4)).Children(row)
		}
	})
}

// labeled draws a setting's label and help beside its control.
func labeled(c *ui.Context, label, help string, control func()) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Text(c, label).FontWeight(500)
			ui.Text(c, help).FontSize(px(t, 12)).TextColor(t.TextMuted).Wrap().Margin(2, 0, 0, 0)
		})
		control()
	})
}

func statusLine(c *ui.Context, st status) {
	if st.text == "" {
		return
	}
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(px(t, 6)).Margin(px(t, 6), 0, 0, 0).Role(ui.RoleStatus).
		Children(func() { statusText(c, st) })
}

// statusText is the icon and text of a status.
func statusText(c *ui.Context, st status) {
	t := c.Theme()
	icon, color := icons.CircleAlert, t.Danger
	switch {
	case st.spin:
		icon, color = icons.LoaderCircle, t.TextMuted
	case st.muted:
		icon, color = nil, t.TextMuted
	case st.ok:
		icon, color = icons.Check, t.Success
	}
	if icon != nil {
		i := ui.Icon(c, icon).FontSize(px(t, 14)).TextColor(color).Margin(1, 0, 0, 0)
		if st.spin {
			i.Rotate(i.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
		}
	}
	ui.Text(c, st.text).FontSize(px(t, 12)).TextColor(color).Shrink(1).Wrap()
}

// coded is help text with one placeholder drawn as code, such as an address or a route.
func coded(c *ui.Context, text, placeholder, code string) ui.Element {
	t := c.Theme()
	before, after, _ := strings.Cut(text, placeholder)
	return ui.RichText(c,
		ui.Span{Text: before, Color: t.TextMuted},
		ui.Span{Text: code, Font: "monospace", Color: t.TextMuted},
		ui.Span{Text: after, Color: t.TextMuted},
	).FontSize(px(t, 12)).Wrap()
}

func (s *Screen) fetchingPage(c *ui.Context) {
	t := c.Theme()
	lead(c, s.text.Lead)
	card(c, func() {
		ui.Text(c, s.text.RSSHub).FontWeight(500)
		coded(c, s.text.RSSHubHelp, "{route}", "/github/issue/golang/go").Margin(2, 0, 0, 0)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Margin(px(t, 10), 0, 0, 0).Children(func() {
			s.baseField(c)
			dirty := s.dirty()
			style := secondary
			if dirty {
				style = primary
			}
			save := button(c, style, !dirty || s.saving)
			textButton(c, save, style, nil, s.text.Save, s.saving)
			if save.Clicked() {
				s.saveBase()
			}
		})
		statusLine(c, s.baseStatus)
	}, func() {
		labeled(c, s.text.Retention, s.text.RetentionHelp, func() { s.retentionSelect(c) })
		statusLine(c, s.keepStatus)
	})
}

// field draws a text input editing value, width wide or growing for 0, its border in the danger color while invalid;
// input names and handles the input.
func field(c *ui.Context, key string, value *string, width float32, invalid bool, input func(in ui.Element)) {
	t := c.Theme()
	b := ui.Row(c).Key(key).MinWidth(0).Height(px(t, 32)).AlignItems(ui.Center).PaddingX(px(t, 10)).
		Radius(t.Radius).Border(1, t.Border).Background(t.Background)
	if width > 0 {
		b.Width(width).Shrink(1)
	} else {
		b.Grow(1)
	}
	switch focused := b.FocusWithin(); {
	case invalid:
		b.BorderColor(t.Danger)
		if focused {
			b.Shadow(0, 0, 0, 3, theme.Soft(t, t.Danger))
		}
	case focused:
		b.BorderColor(t.Accent).Shadow(0, 0, 0, 3, t.Focus)
	}
	b.Children(func() { input(ui.TextInputBase(c, value).Grow(1).MinWidth(0).FocusRing(false)) })
}

func (s *Screen) baseField(c *ui.Context) {
	before := s.base
	field(c, "base", &s.base, 0, s.invalid, func(in ui.Element) {
		in.Font("monospace").Label(s.text.RSSHub).Placeholder(s.text.RSSHubPlaceholder).Disabled(s.saving)
		if s.invalid {
			in.Error(s.text.InvalidBase)
		}
		if s.focusBase {
			in.Focus()
			s.focusBase = false
		}
		if in.Submitted() {
			s.saveBase()
		}
	})
	if s.base != before {
		s.invalid, s.baseStatus = false, status{}
	}
}

func (s *Screen) retentionSelect(c *ui.Context) {
	t := c.Theme()
	options := retentions
	if !slices.Contains(options, s.retention) {
		options = append(slices.Clone(options), s.retention)
		slices.Sort(options[1:])
	}
	sel := ui.SelectBase(c, &s.retention)
	sel.Trigger.MinWidth(px(t, 160)).Height(px(t, 32)).Gap(t.Space(2)).Padding(0, t.Space(2), 0, px(t, 10)).
		Radius(t.Radius).Border(1, t.Border).Background(t.Background).Label(s.text.Retention).Disabled(s.keeping)
	if sel.Trigger.Focused() {
		sel.Trigger.BorderColor(t.Accent).Shadow(0, 0, 0, 3, t.Focus)
	} else if sel.Trigger.Hovered() {
		sel.Trigger.Background(t.SurfaceHover)
	}
	sel.Trigger.Children(func() {
		ui.Text(c, s.keep(s.retention)).Grow(1).MinWidth(0).SingleLine()
		ui.Icon(c, icons.ChevronDown).FontSize(px(t, 14)).TextColor(t.TextMuted)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Margin(4, 0, 0, 0).Padding(4).MinWidth(px(t, 160)).Radius(t.Radius).Background(t.Background).
			Border(1, t.Border).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.18))
		for _, v := range options {
			item := sel.Item(v).FillWidth().Height(px(t, 28)).Gap(t.Space(2)).PaddingX(t.Space(2)).Radius(5)
			color := t.Text
			if item.Highlighted() {
				item.Background(t.Accent)
				color = t.AccentText
			}
			item.Children(func() {
				ui.Text(c, s.keep(v)).Grow(1).NoWrap().TextColor(color)
				if v == s.retention {
					ui.Icon(c, icons.Check).FontSize(px(t, 14)).TextColor(color)
				}
			})
		}
	})
	if sel.Trigger.Changed() {
		s.saveRetention()
	}
}

func (s *Screen) aiPage(c *ui.Context) {
	t := c.Theme()
	lead(c, s.text.AILead)
	before := [...]string{s.aiBase, s.aiModel, s.aiKey, s.aiLanguage}
	submit := func(in ui.Element) {
		if in.Submitted() {
			s.saveAI(false)
		}
	}
	card(c, func() {
		ui.Text(c, s.text.Endpoint).FontWeight(500)
		coded(c, s.text.EndpointHelp, "{address}", s.text.EndpointPlaceholder).Margin(2, 0, 0, 0)
		ui.Row(c).FillWidth().Margin(px(t, 10), 0, 0, 0).Children(func() {
			field(c, "endpoint", &s.aiBase, 0, s.aiInvalid, func(in ui.Element) {
				in.Font("monospace").Label(s.text.Endpoint).Placeholder(s.text.EndpointPlaceholder).Disabled(s.aiSaving)
				if s.aiInvalid {
					in.Error(s.text.InvalidAIBase)
				}
				if s.focusAIBase {
					in.Focus()
					s.focusAIBase = false
				}
				submit(in)
			})
		})
		if s.aiInvalid {
			statusLine(c, status{text: s.text.InvalidAIBase})
		}
	}, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
			ui.Text(c, s.text.Model).FontWeight(500).Grow(1).MinWidth(0)
			field(c, "model", &s.aiModel, px(t, 260), false, func(in ui.Element) {
				in.Font("monospace").Label(s.text.Model).Placeholder(s.text.ModelPlaceholder).Disabled(s.aiSaving)
				submit(in)
			})
		})
	}, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
			ui.Text(c, s.text.APIKey).FontWeight(500).Shrink(1).MinWidth(0)
			badge, fill, color := s.text.KeyUnset, t.Surface, t.TextMuted
			if s.settings.AIAPIKeySet {
				badge, fill, color = s.text.KeySet, theme.Soft(t, t.Accent), t.Accent
			}
			ui.Box(c).Height(px(t, 18)).PaddingX(px(t, 6)).Radius(px(t, 9)).Center().Background(fill).Children(func() {
				ui.Text(c, badge).FontSize(px(t, 11)).FontWeight(500).TextColor(color).NoWrap()
			})
		})
		ui.Text(c, s.text.KeyHelp).FontSize(px(t, 12)).TextColor(t.TextMuted).Wrap().Margin(2, 0, 0, 0)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Margin(px(t, 10), 0, 0, 0).Children(func() {
			placeholder := s.text.KeyPlaceholder
			if s.settings.AIAPIKeySet {
				placeholder = s.text.KeyReplace
			}
			field(c, "key", &s.aiKey, 0, false, func(in ui.Element) {
				in.Password().Label(s.text.APIKey).Placeholder(placeholder).Description(s.text.KeyHelp).
					Disabled(s.aiSaving)
				submit(in)
			})
			if s.settings.AIAPIKeySet {
				remove := button(c, secondary, s.aiSaving)
				textButton(c, remove, secondary, nil, s.text.RemoveKey, false)
				if remove.Clicked() {
					s.saveAI(true)
				}
			}
		})
	}, func() {
		labeled(c, s.text.Language, s.text.LanguageHelp, func() { s.languageSelect(c) })
	}, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
			dirty := s.aiDirty()
			st := s.aiStatus
			switch {
			case s.testing:
				st = status{spin: true, text: s.text.Testing}
			case st.text == "" && dirty && !s.aiInvalid && s.settings.AIConfigured():
				st = status{muted: true, text: s.text.TestFirst}
			}
			ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Start).Gap(px(t, 6)).Role(ui.RoleStatus).Children(func() {
				if st.text != "" {
					statusText(c, st)
				}
			})
			test := button(c, secondary, !s.settings.AIConfigured() || dirty || s.testing || s.aiSaving)
			textButton(c, test, secondary, icons.Plug, s.text.Test, false)
			if test.Clicked() {
				s.test()
			}
			style := secondary
			if dirty {
				style = primary
			}
			save := button(c, style, !dirty || s.aiSaving)
			textButton(c, save, style, nil, s.text.Save, s.aiSaving)
			if save.Clicked() {
				s.saveAI(false)
			}
		})
	})
	if after := [...]string{s.aiBase, s.aiModel, s.aiKey, s.aiLanguage}; after != before {
		s.aiInvalid, s.aiStatus = s.aiInvalid && after[0] == before[0], status{}
	}
	ui.Text(c, s.text.Prompts).FontSize(px(t, 12)).FontWeight(600).TextColor(t.TextMuted).
		Margin(px(t, 20), 0, t.Space(2), 0).Role(ui.RoleHeading).Level(2)
	typed := s.prompts
	card(c, func() { s.promptField(c, 0, s.text.PromptSummary) }, func() { s.promptField(c, 1, s.text.PromptTranslate) },
		func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
				coded(c, s.text.PromptHelp, "{language}", "{language}").Grow(1).MinWidth(0)
				dirty := s.promptsDirty()
				style := secondary
				if dirty {
					style = primary
				}
				save := button(c, style, !dirty || s.promptSaving).Label(s.text.Prompts + " · " + s.text.Save)
				textButton(c, save, style, nil, s.text.Save, s.promptSaving)
				if save.Clicked() {
					s.savePrompts()
				}
			})
			statusLine(c, s.promptStatus)
		})
	if s.prompts != typed {
		s.promptStatus = status{}
	}
}

// promptField is a prompt, with Restore default while it is not the default.
func (s *Screen) promptField(c *ui.Context, i int, label string) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
		ui.Text(c, label).FontWeight(500).Grow(1).MinWidth(0)
		reset := button(c, small, s.promptSaving || strings.TrimSpace(s.prompts[i]) == s.defaults()[i]).
			Label(label + " · " + s.text.Reset)
		textButton(c, reset, small, icons.RotateCcw, s.text.Reset, false)
		if reset.Clicked() {
			s.prompts[i] = s.defaults()[i]
		}
	})
	ui.TextArea(c.Key(i), &s.prompts[i]).FillWidth().Lines(3, 10).Margin(px(t, 10), 0, 0, 0).Font("monospace").
		FontSize(px(t, 12.5)).Label(label).Disabled(s.promptSaving)
}

func (s *Screen) languageSelect(c *ui.Context) {
	t := c.Theme()
	name := func(code string) string {
		for _, l := range languages {
			if l.code == code {
				return l.name
			}
		}
		return code
	}
	sel := ui.SelectBase(c, &s.aiLanguage)
	sel.Trigger.MinWidth(px(t, 160)).Height(px(t, 32)).Gap(t.Space(2)).Padding(0, t.Space(2), 0, px(t, 10)).
		Radius(t.Radius).Border(1, t.Border).Background(t.Background).Label(s.text.Language).Disabled(s.aiSaving)
	if sel.Trigger.Focused() {
		sel.Trigger.BorderColor(t.Accent).Shadow(0, 0, 0, 3, t.Focus)
	} else if sel.Trigger.Hovered() {
		sel.Trigger.Background(t.SurfaceHover)
	}
	sel.Trigger.Children(func() {
		ui.Text(c, name(s.aiLanguage)).Grow(1).MinWidth(0).SingleLine()
		ui.Icon(c, icons.ChevronDown).FontSize(px(t, 14)).TextColor(t.TextMuted)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Margin(4, 0, 0, 0).Padding(4).MinWidth(px(t, 160)).Radius(t.Radius).Background(t.Background).
			Border(1, t.Border).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.18))
		for _, l := range languages {
			item := sel.Item(l.code).FillWidth().Height(px(t, 28)).Gap(t.Space(2)).PaddingX(t.Space(2)).Radius(5)
			color := t.Text
			if item.Highlighted() {
				item.Background(t.Accent)
				color = t.AccentText
			}
			item.Children(func() {
				ui.Text(c, l.name).Grow(1).NoWrap().TextColor(color)
				if l.code == s.aiLanguage {
					ui.Icon(c, icons.Check).FontSize(px(t, 14)).TextColor(color)
				}
			})
		}
	})
}

func (s *Screen) opmlPage(c *ui.Context) {
	t := c.Theme()
	lead(c, s.text.OPMLHelp)
	card(c, func() {
		labeled(c, s.text.ImportLabel, s.text.ImportHelp, func() {
			b := button(c, secondary, s.busy)
			textButton(c, b, secondary, icons.Upload, s.text.Import, false)
			if b.Clicked() {
				s.chooseImport()
			}
		})
		if s.importing != "" {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(px(t, 6)).Margin(px(t, 6), 0, 0, 0).Role(ui.RoleStatus).
				Children(func() {
					spin := ui.Icon(c, icons.LoaderCircle).FontSize(px(t, 14)).TextColor(t.TextMuted)
					spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
					ui.Text(c, s.fill(s.text.Importing, "{file}", s.importing)).FontSize(px(t, 12)).
						TextColor(t.TextMuted).Shrink(1).Wrap()
				})
		}
		if s.result.text != "" && !s.result.export {
			s.resultBox(c)
		}
	}, func() {
		labeled(c, s.text.ExportLabel, s.text.ExportHelp, func() {
			b := button(c, secondary, s.busy)
			textButton(c, b, secondary, icons.Download, s.text.Export, false)
			if b.Clicked() {
				s.export()
			}
		})
		if s.result.text != "" && s.result.export {
			s.resultBox(c)
		}
	})
}

func (s *Screen) resultBox(c *ui.Context) {
	t := c.Theme()
	r := s.result
	fill, icon, iconColor, color := t.Surface, icons.Check, t.Success, t.Text
	switch r.tone {
	case partly:
		fill, icon, iconColor = theme.Soft(t, t.Warning), icons.TriangleAlert, t.Warning
	case refused:
		fill, icon, iconColor, color = theme.Soft(t, t.Danger), icons.CircleAlert, t.Danger, t.Danger
	}
	ui.Column(c).FillWidth().Margin(t.Space(3), 0, 0, 0).Padding(px(t, 10), t.Space(3)).Radius(t.Radius).
		Background(fill).Role(ui.RoleStatus).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(t.Space(2)).Children(func() {
			ui.Icon(c, icon).FontSize(px(t, 14)).TextColor(iconColor).Margin(2, 0, 0, 0)
			ui.Text(c, r.text).Grow(1).Shrink(1).MinWidth(0).FontSize(px(t, 12.5)).TextColor(color).Wrap()
			x := ui.ButtonBase(c).Size(px(t, 20), px(t, 20)).Radius(5).Label(s.text.Dismiss).Tooltip(s.text.Dismiss)
			xColor := t.TextMuted
			if x.Hovered() {
				x.Background(t.SurfaceHover)
				xColor = t.Text
			}
			x.Children(func() { ui.Icon(c, icons.X).FontSize(px(t, 13)).TextColor(xColor) })
			if x.Clicked() {
				s.result = result{}
			}
		})
		if len(r.fails) == 0 {
			return
		}
		ui.Column(c).FillWidth().Padding(t.Space(2), 0, 0, px(t, 22)).Label(s.text.NotAdded).Role(ui.RoleList).
			Children(func() {
				for i, f := range r.fails {
					s.failRow(c, i, f)
				}
			})
	})
}

func (s *Screen) failRow(c *ui.Context, i int, f client.ImportFail) {
	t := c.Theme()
	why := map[string]string{
		"invalid_url": s.text.InvalidURL, "invalid_rsshub_path": s.text.InvalidRoute,
		"invalid_kind": s.text.InvalidKind, "invalid_source": s.text.InvalidSource,
	}[f.Error]
	if why == "" {
		why = f.Error
	}
	ui.Row(c).Key(i).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Padding(px(t, 3), 0).Role(ui.RoleListItem).
		Children(func() {
			spans := []ui.Span{}
			if title := strings.TrimSpace(f.Title); title != "" {
				spans = append(spans, ui.Span{Text: title + " · "})
			}
			spans = append(spans, ui.Span{Text: f.Address, Font: "monospace", Size: px(t, 12)})
			ui.RichText(c, spans...).Grow(1).MinWidth(0).FontSize(px(t, 12.5)).SingleLine()
			ui.Text(c, why).FontSize(px(t, 12.5)).TextColor(t.TextMuted).NoWrap()
		})
}

func (s *Screen) devicesPage(c *ui.Context) {
	t := c.Theme()
	if !s.signedIn {
		card(c, func() {
			ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(px(t, 10)).Children(func() {
				ui.Icon(c, icons.Laptop).FontSize(px(t, 16)).TextColor(t.TextMuted).Margin(1, 0, 0, 0)
				ui.Text(c, s.text.LocalDevices).TextColor(t.TextMuted).Shrink(1).Wrap()
			})
		})
		return
	}
	lead(c, s.text.DevicesHelp)
	now := s.deps.Now()
	rows := make([]func(), len(s.devices))
	for i, d := range s.devices {
		rows[i] = func() { s.deviceRow(c, d, now) }
	}
	card(c, rows...)
}

func (s *Screen) deviceRow(c *ui.Context, d client.Device, now time.Time) {
	t := c.Theme()
	name, icon := s.device(d)
	when := s.fill(s.text.SignedIn, "{date}", s.date(d.CreatedAt, now), "{seen}", s.ago(d.LastSeenAt, now))
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
		ui.Box(c).Size(px(t, 32), px(t, 32)).Radius(8).Center().Background(t.Surface).Children(func() {
			ui.Icon(c, icon).FontSize(px(t, 16)).TextColor(t.TextMuted)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(px(t, 6)).Children(func() {
				ui.Text(c, name).FontWeight(500).SingleLine().MinWidth(0).Shrink(1)
				if d.Current {
					ui.Box(c).Height(px(t, 18)).PaddingX(px(t, 6)).Radius(px(t, 9)).Center().
						Background(theme.Soft(t, t.Accent)).Children(func() {
						ui.Text(c, s.text.ThisDevice).FontSize(px(t, 11)).FontWeight(500).TextColor(t.Accent).NoWrap()
					})
				}
			})
			ui.Text(c, when).FontSize(px(t, 12)).TextColor(t.TextMuted).SingleLine()
		})
		if !d.Current {
			b := button(c, danger, false).Label(s.fill(s.text.RevokeTitle, "{name}", name))
			textButton(c, b, danger, nil, s.text.Revoke, false)
			if b.Clicked() {
				s.doomed, s.revoking = d, true
			}
		}
	})
}

const (
	secondary = iota
	primary
	danger // small, in the danger color
	small
)

func button(c *ui.Context, style int, disabled bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Height(px(t, 32)).Gap(t.Space(1.5)).PaddingX(px(t, 14)).Radius(t.Radius).Disabled(disabled)
	fill, border := t.Background, t.Border
	switch style {
	case primary:
		fill, border = t.Accent, t.Accent
	case danger, small:
		b.Height(px(t, 26)).PaddingX(px(t, 10))
	}
	switch {
	case disabled:
		b.Opacity(0.45)
	case b.Pressed() && style == primary:
		fill, border = t.AccentPressed, t.AccentPressed
	case b.Hovered() && style == primary:
		fill, border = t.AccentHover, t.AccentHover
	case b.Pressed():
		fill = t.SurfacePressed
	case b.Hovered():
		fill = t.SurfaceHover
	}
	return b.Background(fill).Border(1, border)
}

// textButton fills a button with an optional icon and its text, a spinner for the icon while busy.
func textButton(c *ui.Context, b ui.Element, style int, icon *ui.SVG, text string, busy bool) {
	t := c.Theme()
	color := t.Text
	switch style {
	case primary:
		color = t.AccentText
	case danger:
		color = t.Danger
	}
	b.Children(func() {
		if busy {
			spin := ui.Icon(c, icons.LoaderCircle).FontSize(px(t, 15)).TextColor(color)
			spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
		} else if icon != nil {
			ui.Icon(c, icon).FontSize(px(t, 15)).TextColor(color)
		}
		label := ui.Text(c, text).NoWrap().TextColor(color)
		switch style {
		case primary:
			label.FontWeight(500)
		case danger, small:
			label.FontSize(px(t, 12))
		}
	})
}
