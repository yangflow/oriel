// Package settings is the settings screen, in the place of the timeline: the RSSHub instance and how long entries are
// kept, OPML import and export, the AI endpoint and prompts, the devices signed in to the server, and what the server
// is fetching with the space its database uses; see design/screens/settings.html. Everything
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
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/prefs"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/theme"
	"github.com/yangflow/oriel/internal/ui/typeset"
	"golang.org/x/net/html/charset"
)

//go:embed copy.json
var catalog []byte

type copy struct {
	Menu, Settings, ThisComputer, BackTimeline, ServerNote, Lead, Fetching, OPML, Devices, RSSHub                                                                                                                                                                                                                                           string
	RSSHubHelp, RSSHubPlaceholder, Save, Saved, Unsaved, RetentionSaved, InvalidBase, SaveFailed, Retention, RetentionHelp                                                                                                                                                                                                                  string
	InvalidBaseAny, TooManyBase                                                                                                                                                                                                                                                                                                             string
	Forever, Days, Week, Month, Months3, Months6, Year, OPMLHelp, ImportLabel, ImportHelp, ExportLabel, ExportHelp                                                                                                                                                                                                                          string
	OPMLFiles, NotAdded, Import, Export, Importing, Imported, ImportedOk, InvalidOPML, TooLarge, ReadFailed                                                                                                                                                                                                                                 string
	ImportFailed, Exported, ExportFailed, WriteFailed, Dismiss, InvalidURL, InvalidRoute, InvalidKind                                                                                                                                                                                                                                       string
	BackupLabel, BackupHelp, BackupButton, RestoreLabel, RestoreHelp, RestoreButton, BackupFiles, BackingUp, BackedUp                                                                                                                                                                                                                       string
	BackupFailed, Restoring, RestoreDone, RestoreNotBackup, RestoreCorrupt, RestoreNewer, RestoreTooLarge                                                                                                                                                                                                                                   string
	RestoreBusy, RestoreFailed, RestoreTitle, RestoreBody, RestoreConfirm                                                                                                                                                                                                                                                                   string
	AutoLabel, AutoHelp, AutoFolder, AutoChoose, AutoNoFolder, AutoFrequency, AutoDaily, AutoWeekly, AutoKeep                                                                                                                                                                                                                               string
	AutoCopies, AutoNever, AutoLast, AutoFailed, AutoRemote, AutoPlaceholder                                                                                                                                                                                                                                                                string
	AutoReasonMissing, AutoReasonNotDir, AutoReasonNoWrite, AutoReasonDisk, AutoReasonOther                                                                                                                                                                                                                                                 string
	AutoNeedFolder, AutoBadFolder, AutoInvalidKeep, AutoSaveFailed                                                                                                                                                                                                                                                                          string
	InvalidSource, DevicesHelp, LocalDevices, ThisDevice, UnknownDevice, Revoke, SignedIn, RevokeTitle                                                                                                                                                                                                                                      string
	RevokeBody, Cancel, RevokeConfirm, Revoked, RevokeFailed, Loading, Error, ErrorHint, Retry                                                                                                                                                                                                                                              string
	AI, AILead, Endpoint, EndpointHelp, EndpointPlaceholder, Model, ModelPlaceholder, APIKey, KeySet, KeyUnset                                                                                                                                                                                                                              string
	KeyHelp, KeyPlaceholder, KeyReplace, RemoveKey, KeyRemoved, Language, LanguageHelp, Test, Testing, Tested                                                                                                                                                                                                                               string
	TestFirst, AIUnauthorized, AIUnreachable, AITimeout, AIRateLimited, AIUpstream, AISaved, InvalidAIBase                                                                                                                                                                                                                                  string
	Prompts, PromptSummary, PromptTranslate, PromptHelp, Reset, PromptRestored, Undo, InvalidPrompt                                                                                                                                                                                                                                         string
	Appearance, AppearanceLead, Theme, ThemeHelp, ThemeSystem, ThemeLight, ThemeDark, UILanguage, UILanguageHelp                                                                                                                                                                                                                            string
	ReadingFont, ReadingFontHelp, FollowInterface, Serif                                                                                                                                                                                                                                                                                    string
	SystemChoice, UIFont, UIFontHelp, SystemFont, Reading, TextSize, TextSizeHelp, SizeSmall, SizeMedium                                                                                                                                                                                                                                    string
	SizeLarge, SizeXLarge, LineSpacing, LineSpacingHelp, SpacingCompact, SpacingStandard, SpacingRelaxed                                                                                                                                                                                                                                    string
	PreviewText, PreferenceFailed, ColDevice, ColPlatform, ColSigned, ColLastUsed, Connection                                                                                                                                                                                                                                               string
	Preset, SummaryLimit, ChunkLimit, LimitsHelp, DailyCap, FetchCap, CapsHelp, DestinationLocal, DestinationRemote, UsageToday, CapReached, CheckListed, CheckFits, CheckSmall, AIModelMissing, InvalidLimit                                                                                                                               string
	ChineseTypesetting, ChineseTypesettingHelp                                                                                                                                                                                                                                                                                              string
	FontSearch, FontList, FontNone, FontLoading, FontCountOne, FontCount, FontCurrent                                                                                                                                                                                                                                                       string
	ViewAs, ViewArticles, ViewVideos, ViewSocial, ViewImages, ViewList, ViewCompact, ViewCover, ViewGrid                                                                                                                                                                                                                                    string
	ViewWall, ViewCard, ViewAsHelp, Timeline, GroupSameLink, GroupSameLinkHelp, Density, DensityHelp, DensityComfortable, DensityCompact, MarkRead, MarkReadHelpOpen, MarkReadHelpDwell, MarkReadHelpScroll, MarkReadHelpNever, MarkReadOpen, MarkReadDwell, MarkReadScroll, MarkReadNever, Dwell, DwellHelp, Seconds3, Seconds5, Seconds10 string
	Activity, ActivityLead, Refresh, Storage, DBSize, Entries, Images, ImagesValue, Copies, CopiesValue, Compact, Compacting                                                                                                                                                                                                                string
	Compacted, CompactFailed, CompactHelp, FetchingNow, FetchingNone, Recent, RecentNone, RecentKeep                                                                                                                                                                                                                                        string
	Health, HealthHelp, HealthEmpty, MainBadge, NoData, NoAnswer, LastN, Cooling, CoolingSoon                                                                                                                                                                                                                                               string
	NewCount, NothingNew, FailedWord, ShowAttempts                                                                                                                                                                                                                                                                                          string
	Diagnostics, DiagnosticsHelp, ExportDiagnostics, ExportingDiagnostics, DiagnosticsFiles, DiagnosticsExported                                                                                                                                                                                                                            string
	DiagnosticsFailed, DiagnosticsMenu                                                                                                                                                                                                                                                                                                      string

	ImportingOf, Progress, ViewSources, Stopped, Lost, ImportBusy, BusyWhy        string
	StoppedMany, LostMany, SkippedWhy                                             string
	DropHelp, ImportedMany, NotOpmlWhy, TooLargeWhy, ReadWhy, FailedWhy, DropBusy string

	Rules, RulesLead, RulesNone, RulesNoneHelp, AddRule, RulesMax, RuleMore, RuleSwitch, RuleOffBadge, AllSubs     string
	RwKeywords, RwAuthor, RwOnly, ActStar, ActRead, MenuEdit, MenuDelete, RuleDeleted, RuleAddTitle, RuleEditTitle string
	RuleWhen, RuleWhenHelp, RuleSub, RuleKind, KindAny, RuleKeywords, RuleKeywordsHelp, RuleAuthor, RuleAuthorHint string
	RuleThen, RuleNote, ErrNoCondition, ErrNoAction, ErrKeywords, ErrAuthor, ErrSource, ErrTooMany, RuleClose      string
	MutedTitle, MutedHelp, MutedNone, MutedMany, MutedInvalid, RulesError, RulesErrorHint                          string

	ReaderLabel, ReaderHelp, ReaderButton, ReaderTitle, ReaderService, ReaderAddress, ReaderAddressHint    string
	ReaderUser, ReaderToken, ReaderPassword, ReaderMemory, ReaderConnect, ReaderClose, ReaderCancel        string
	ReaderImport, ReaderDone, ReaderTryAgain, ReaderProgress, ReaderProgressOpen, ReaderFound              string
	ReaderStageSubscriptions, ReaderStageStarred, ReaderStageRead, ReaderStageWriting                      string
	ReaderSources, ReaderStarred, ReaderRead, ReaderFolders, ReaderFoldersNote                             string
	ReaderResultSources, ReaderResultStarred, ReaderResultRead, ReaderResultUnmatched, ReaderResultFolders string
	ReaderSkipped, ReaderMore, ReaderUnknownSource, ReaderInvalidLink                                      string
	ReaderErrAddress, ReaderErrUnauthorized, ReaderErrUnreachable, ReaderErrRate, ReaderErrBad             string
	ReaderErrLarge, ReaderErrRedirect, ReaderErrBusy, ReaderErrOther                                       string
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

// DiagnosticsMenuLabel is the Help menu item that exports the diagnostics file, in the language of the system locale.
func DiagnosticsMenuLabel(systemLocale string) string {
	return translations[locale.Language(systemLocale)].DiagnosticsMenu
}

// retentions are the days entries are kept that the picker offers, 0 for forever.
var retentions = []int{0, 7, 30, 90, 180, 365}

// interfaceLanguages are the choices of the interface language, each named in itself; the empty code follows the system.
var interfaceLanguages = []struct{ code, name string }{{prefs.LanguageChinese, "简体中文"}, {prefs.LanguageEnglish, "English"}}

// languages are the languages summaries and translations can be written in, each named in itself.
var languages = []struct{ code, name string }{{"zh-Hans", "简体中文"}, {"en", "English"}}

// presets are the local servers the AI group offers to fill the endpoint for, each with a model name to hint at; an
// empty hint keeps the usual one.
var presets = []struct{ name, base, hint string }{
	{"Ollama", "http://127.0.0.1:11434/v1", "qwen3:8b"},
	{"LM Studio", "http://127.0.0.1:1234/v1", ""},
}

// The groups of the sidebar.
const (
	appearance = iota
	fetching
	rules
	opml
	ai
	devices
	activity
)

// pages are the names of the groups, in the order of the constants above, as the command palette calls them.
var pages = []string{appearance: "appearance", fetching: "fetching", rules: "rules", opml: "opml", ai: "ai", devices: "devices", activity: "activity"}

// API is what the screen reads and writes on the server; client.Session is one.
type API interface {
	Settings(ctx context.Context) (client.Settings, error)
	SetSettings(ctx context.Context, patch client.SettingsPatch) (client.Settings, error)
	Devices(ctx context.Context) ([]client.Device, error)
	Activity(ctx context.Context) (client.Activity, error)
	RSSHubHealth(ctx context.Context) (client.RSSHubHealth, error)
	Storage(ctx context.Context) (client.Storage, error)
	StartVacuum(ctx context.Context) (client.Vacuum, error)
	RevokeDevice(ctx context.Context, id int64) error
	StartOPMLImport(ctx context.Context, opml []byte) (client.OPMLImport, error)
	OPMLImportStatus(ctx context.Context) (client.OPMLImport, error)
	ExportOPML(ctx context.Context) ([]byte, error)
	Diagnostics(ctx context.Context) ([]byte, error)
	Backup(ctx context.Context, dst io.Writer) (int64, error)
	Restore(ctx context.Context, backup io.Reader) (client.Restored, error)
	TestAI(ctx context.Context) (client.AITest, error)
	StartImport(ctx context.Context, req client.ImportRequest) (client.ImportStatus, error)
	ImportStatus(ctx context.Context) (client.ImportStatus, error)
	ConfirmImport(ctx context.Context) (client.ImportStatus, error)
	CancelImport(ctx context.Context) (client.ImportStatus, error)
	Sources(ctx context.Context) (client.SourceList, error)
	Rules(ctx context.Context) ([]client.Rule, error)
	CreateRule(ctx context.Context, rule client.NewRule) (client.Rule, error)
	RestoreRule(ctx context.Context, rule client.Rule) (client.Rule, error)
	UpdateRule(ctx context.Context, id int64, rule client.NewRule) (client.Rule, error)
	SetRuleEnabled(ctx context.Context, id int64, enabled bool) (client.Rule, error)
	DeleteRule(ctx context.Context, id int64) error
	MutedCountAll(ctx context.Context, keywords string) (int, error)
}

// Deps is what the screen does outside itself. Go runs work away from the UI thread and then the function it
// returns on the UI thread; Now is the clock, time.Now by default. Open and Save ask for the OPML file to import and
// the one to export to, filter naming its kind; they block until the user answers, so they run in Go's work and
// return "" when cancelled; Folder is the same for the folder of automatic backups, and nil when it cannot be
// chosen here, as with a remote server. Back returns to the timeline, Imported tells it its content changed (subscriptions were added or restored, or articles
// are grouped differently), and Expired
// shows the connect screen because the server no longer takes the session. The appearance preferences belong to this
// device: Preferences gives them, Fonts the installed font families once they are known (nil until then), ReadingFont
// the font list of an article's body that they come to ("" for the interface font), and Change takes new ones, which
// apply at once and are saved in the background.
type Deps struct {
	Go       func(work func() (apply func()))
	Now      func() time.Time
	Open     func(filter string, extensions ...string) (string, error)
	Save     func(name, filter string) (string, error)
	Folder   func() (string, error)
	Back     func()
	Imported func()
	// ViewSources goes back to the timeline on all subscriptions, after an import that added some.
	ViewSources func()
	Expired     func()

	Preferences func() prefs.Preferences
	Fonts       func() []string
	ReadingFont func() string
	Change      func(prefs.Preferences)
}

// restoredToast is the ID of the toast of Restore default on a prompt.
const restoredToast = "settings.prompt-restored"

// How far something has loaded.
const (
	loading = iota
	loaded
	failed
)

// status is the line under a setting after it was saved, or why it was not; muted for a note, spinning while
// something is under way, warn for a hint that is not a failure.
type status struct {
	ok, muted, spin, warn bool
	text                  string
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
	backup bool // shown under the restore row, which also holds the result of a backup
	view   bool // an import that added subscriptions, which View subscriptions goes to
}

// Screen keeps the settings as loaded and the work under way.
type Screen struct {
	deps     Deps
	scroll   ui.ScrollState // the page of the group shown, for the bar's edge
	text     copy
	api      API
	server   string // the host of the server, "" for this computer
	signedIn bool   // the session has a token; a server without a password has no devices
	gen      int    // counts servers used, so the results of the one before are dropped
	loads    int    // counts loads, so a result the user moved on from is dropped
	group    int
	lang     string     // the language of the screen
	uiFont   fontPicker // the picker of the Interface font
	rdFont   fontPicker // the picker of the Reading font
	preview  struct {
		text   string
		pieces []typeset.Piece
		ok     bool
	} // the sample's pieces under Chinese typesetting, for the text they were made from

	state       int
	settings    client.Settings
	base        string // the RSSHub instances as typed, one per line
	saving      bool
	invalid     bool
	baseStatus  status
	focusBase   bool
	typing      bool // a text field has the focus, as of the last frame
	blur        bool // the next frame moves the focus to the back button
	retention   int
	keeping     bool // the retention is being saved
	keepStatus  status
	devices     []client.Device
	deviceState int

	activity    client.Activity
	storage     client.Storage
	actState    int
	health      client.RSSHubHealth
	healthShown bool      // the health was read; false hides the block
	healthAsked bool      // the health was asked for since the Fetching group was opened
	healthLoads int       // counts requests for the health, so a result a newer one has passed is dropped
	actLoads    int       // counts requests for the activity, so a result a newer one has passed is dropped
	actBusy     bool      // the activity is being fetched
	actAt       time.Time // when it was last asked for
	actOpen     int64     // the subscription whose attempts are shown, 0 for none
	vacStarting bool      // the request to compact is under way
	vacError    string    // why the request to compact failed
	diagBusy    bool      // the diagnostics file is being chosen and written
	diagStatus  status    // how the last export of it went

	aiBase, aiModel, aiKey string // as typed; the key saved is never sent back
	aiLanguage             string
	aiLimits               [4]string // the summary limit, chunk size, daily cap and per-fetch cap as typed; "" follows the default
	aiChecks               []status  // what the last test found besides the answer
	aiSaving               bool
	aiInvalid              bool // the server refused the endpoint
	focusAIBase            bool
	aiStatus               status
	testing                bool
	prompts                [2]string // the summary and translation prompts as typed
	promptSaving           bool
	promptStatus           status

	busy       bool       // a file is being chosen, imported or exported
	run        *importRun // the import of the files chosen or dropped, while it is under way
	working    string     // what the backup or restore under way says, shown with a spinner under the restore row
	confirming bool       // the dialog that asks to replace the data is open
	backupFile string     // the backup file the dialog is about
	autoFolder string     // the folder of automatic backups as typed, where it is typed
	autoKeep   string     // the number to keep as typed
	grouping   bool       // the choice to show an article once is being saved
	autoSaving bool       // a choice about automatic backups is being chosen or saved
	autoStatus status
	result     result
	reader     reader // the dialog that imports from another reader
	rules      rulesPage

	revoking bool
	doomed   client.Device
	announce string
	toast    string

	restored *restoration // the last "Restore default" of a prompt, until its toast closes
}

// restoration is a prompt put back to its default, with the text it had, which Undo returns.
type restoration struct {
	i      int
	name   string
	before string
	shown  bool
}

// New selects the best available translation, falling back to English. The screen shows nothing until Use.
func New(systemLocale string, deps Deps) *Screen {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Preferences == nil {
		deps.Preferences = prefs.Default
	}
	if deps.Fonts == nil {
		deps.Fonts = func() []string { return nil }
	}
	if deps.ReadingFont == nil {
		deps.ReadingFont = func() string { return "" }
	}
	if deps.Change == nil {
		deps.Change = func(prefs.Preferences) {}
	}
	s := &Screen{deps: deps}
	s.SetLanguage(systemLocale)
	return s
}

// SetLanguage shows the screen in the language of systemLocale, keeping the group open and what was typed.
func (s *Screen) SetLanguage(systemLocale string) {
	s.lang = locale.Language(systemLocale)
	s.text = translations[s.lang]
}

// Use makes the screen work on api, whose host is server ("" for this computer); signedIn is whether its session
// signed in with a password, without which the server has no devices.
func (s *Screen) Use(api API, server string, signedIn bool) {
	sameServer, deleted := server == s.server, s.rules.deleted
	s.api, s.server, s.signedIn = api, server, signedIn
	s.gen++
	s.group, s.busy, s.run, s.result, s.revoking = fetching, false, nil, result{}, false
	s.working, s.confirming, s.backupFile = "", false, ""
	s.autoSaving, s.autoStatus = false, status{}
	s.grouping = false
	s.saving, s.keeping, s.aiSaving, s.testing, s.promptSaving = false, false, false, false, false
	s.state, s.deviceState, s.restored = loading, loading, nil
	s.activity, s.storage, s.actState, s.actBusy, s.actOpen = client.Activity{}, client.Storage{}, loading, false, 0
	s.vacStarting, s.vacError, s.actAt = false, "", time.Time{}
	s.health, s.healthShown, s.healthAsked = client.RSSHubHealth{}, false, false
	s.reader = reader{}
	s.rules = rulesPage{state: loading}
	if sameServer { // signing in again keeps the Undo of a rule just deleted
		s.rules.deleted = deleted
	}
}

// OpenAI opens the screen on the AI group, from an entry point of summaries and translations.
func (s *Screen) OpenAI() {
	s.group = ai
	s.Open()
}

// OpenImport opens the screen on the import and export group, from the empty timeline of a first run.
func (s *Screen) OpenImport() {
	s.group = opml
	s.Open()
}

// OpenAppearance opens the screen on the appearance group, as it was when the interface language changed.
func (s *Screen) OpenAppearance() {
	s.group = appearance
	s.Open()
}

// OpenPage opens the screen on the group called name: "appearance", "fetching", "rules", "opml", "ai", "devices" or "activity", from the
// command palette of the timeline. Any other name opens it on the group it showed last.
func (s *Screen) OpenPage(name string) {
	if g := slices.Index(pages, name); g >= 0 {
		s.group = g
	}
	s.Open()
}

// PreferenceFailed tells the preferences could not be saved and went back to what they were.
func (s *Screen) PreferenceFailed() {
	s.toast = s.text.PreferenceFailed
}

// Open loads the settings and the devices again, as the screen is shown.
func (s *Screen) Open() {
	s.baseStatus, s.keepStatus, s.invalid, s.revoking = status{}, status{}, false, false
	s.aiStatus, s.promptStatus, s.aiInvalid, s.aiChecks = status{}, status{}, false, nil
	s.autoStatus = status{}
	if !s.busy {
		s.result = result{}
	}
	s.loadSettings()
	s.loadDevices()
	s.loadRules()
	s.actState, s.actAt = loading, time.Time{}
	s.healthAsked = false
}

func (s *Screen) expired(err error) bool {
	if !errors.Is(err, client.ErrUnauthorized) {
		return false
	}
	s.gen++
	s.api = nil
	s.busy, s.run, s.saving, s.keeping, s.revoking = false, nil, false, false, false
	s.working, s.confirming, s.backupFile = "", false, ""
	s.autoSaving = false
	s.aiSaving, s.testing, s.promptSaving = false, false, false
	s.actBusy, s.vacStarting = false, false
	s.health, s.healthShown = client.RSSHubHealth{}, false
	s.reader = reader{}
	s.rules = rulesPage{state: loading, deleted: s.rules.deleted} // the Undo of a deleted rule outlives the reset
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
			s.base, s.retention = strings.Join(settings.RSSHubBaseURLs, "\n"), settings.RetentionDays
			s.aiBase, s.aiModel, s.aiKey, s.aiLanguage = settings.AIBaseURL, settings.AIModel, "", settings.AILanguage
			s.aiLimits = savedLimits(settings)
			s.prompts = [2]string{settings.PromptSummary, settings.PromptTranslate}
			s.autoFolder, s.autoKeep = settings.AutoBackupFolder, strconv.Itoa(settings.AutoBackupKeep)
			s.rules.takeMuted(settings.MuteKeywords)
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

// baseList is the RSSHub instances in text: one per line, blank lines and trailing slashes dropped, a repeated
// address kept once, as the server keeps them.
func baseList(text string) []string {
	var list []string
	for line := range strings.SplitSeq(text, "\n") {
		base := strings.TrimRight(strings.TrimSpace(line), "/")
		if base != "" && !slices.ContainsFunc(list, func(have string) bool { return strings.EqualFold(have, base) }) {
			list = append(list, base)
		}
	}
	return list
}

// badBase is the number of the first line of text that is not a full http or https address with a host name, or 0.
func badBase(text string) int {
	for i, line := range slices.Collect(strings.SplitSeq(text, "\n")) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			return i + 1
		}
	}
	return 0
}

// dirty reports whether the RSSHub instances typed are not the ones saved.
func (s *Screen) dirty() bool { return !slices.Equal(baseList(s.base), s.settings.RSSHubBaseURLs) }

// refuseBase shows that the list cannot be saved and puts the cursor back in it.
func (s *Screen) refuseBase(text string) {
	s.invalid, s.focusBase = true, true
	s.baseStatus = status{text: text}
	s.announce = text
}

func (s *Screen) saveBase() {
	if s.saving || s.api == nil || s.state != loaded || !s.dirty() {
		return
	}
	if line := badBase(s.base); line > 0 {
		s.refuseBase(strings.ReplaceAll(s.text.InvalidBase, "{n}", strconv.Itoa(line)))
		return
	}
	list := baseList(s.base)
	s.saving, s.baseStatus, s.invalid = true, status{}, false
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		settings, err := api.SetSettings(context.Background(), client.SettingsPatch{RSSHubBaseURLs: &list})
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			s.saving = false
			switch {
			case err == nil:
				s.settings.RSSHubBaseURL, s.settings.RSSHubBaseURLs = settings.RSSHubBaseURL, settings.RSSHubBaseURLs
				s.base = strings.Join(settings.RSSHubBaseURLs, "\n")
				s.baseStatus = status{ok: true, text: s.text.Saved}
				s.fetchHealth()
				s.announce = s.baseStatus.text
			case code(err) == "invalid_rsshub_base_url":
				s.refuseBase(s.text.InvalidBaseAny)
			case code(err) == "too_many_rsshub_base_urls":
				s.refuseBase(s.text.TooManyBase)
			default:
				s.baseStatus = status{text: s.text.SaveFailed}
				s.announce = s.baseStatus.text
			}
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

// saveGrouping saves whether the timeline shows an article once when several subscriptions carry it, and has the
// timeline load again when it is saved. The switch moves at once and goes back when the server refuses.
func (s *Screen) saveGrouping(on bool) {
	if s.api == nil || s.grouping {
		return
	}
	s.grouping = true
	s.settings.GroupSameLink = on
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		_, err := api.SetSettings(context.Background(), client.SettingsPatch{GroupSameLink: &on})
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			s.grouping = false
			if err != nil {
				s.settings.GroupSameLink = !on
				s.announce = s.text.SaveFailed
				return
			}
			s.deps.Imported()
		}
	})
}

// watch notes that a text field has the focus, for escape.
func (s *Screen) watch(focused bool) {
	s.typing = s.typing || focused
}

// unsaved reports whether any text on the page is not saved: the AI endpoint, the prompts, the RSSHub instances and
// the muted keywords.
func (s *Screen) unsaved() bool {
	return s.aiDirty() || s.promptsDirty() || s.dirty() || cleanLines(s.rules.muted) != cleanLines(s.settings.MuteKeywords)
}

// escape is Escape on the page. A text field with the focus gives it up, to the back button, and nothing leaves; with
// text not saved the page stays and says so, since going back would lose it; otherwise it goes back to the timeline.
func (s *Screen) escape() {
	switch {
	case s.typing:
		s.typing, s.blur = false, true
	case s.state == loaded && s.unsaved():
		s.announce = s.text.Unsaved
	default:
		s.deps.Back()
	}
}

// aiDirty reports whether the AI endpoint as typed is not the one saved; a key typed is always a change.
func (s *Screen) aiDirty() bool {
	return strings.TrimSpace(s.aiBase) != s.settings.AIBaseURL || strings.TrimSpace(s.aiModel) != s.settings.AIModel ||
		strings.TrimSpace(s.aiKey) != "" || s.aiLanguage != s.settings.AILanguage || s.limitsDirty()
}

// savedLimits are the limits saved as the fields show them: empty for the default.
func savedLimits(settings client.Settings) [4]string {
	text := func(v, def int) string {
		if v == def || v <= 0 {
			return ""
		}
		return strconv.Itoa(v)
	}
	return [4]string{text(settings.AISummaryLimit, settings.DefaultAISummaryLimit),
		text(settings.AIChunkLimit, settings.DefaultAIChunkLimit),
		text(settings.AIAutoDailyLimit, settings.DefaultAIAutoDaily),
		text(settings.AIAutoFetchLimit, settings.DefaultAIAutoFetch)}
}

// limitsDirty reports whether a limit as typed is not the one saved.
func (s *Screen) limitsDirty() bool {
	saved := savedLimits(s.settings)
	for i, typed := range s.aiLimits {
		if strings.TrimSpace(typed) != saved[i] {
			return true
		}
	}
	return false
}

// limit reads a limit as typed: 0 for empty, which follows the default, and false when it is not a number.
func limit(typed string) (int, bool) {
	typed = strings.TrimSpace(typed)
	if typed == "" {
		return 0, true
	}
	n, err := strconv.Atoi(typed)
	return n, err == nil && n > 0
}

// preset is the index of the preset whose address the endpoint holds, or -1.
func (s *Screen) preset() int {
	base := strings.TrimSuffix(strings.TrimSpace(s.aiBase), "/")
	return slices.IndexFunc(presets, func(p struct{ name, base, hint string }) bool { return p.base == base })
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
		saved := savedLimits(s.settings)
		fields := [4]**int{&patch.AISummaryLimit, &patch.AIChunkLimit, &patch.AIAutoDaily, &patch.AIAutoFetch}
		for i, typed := range s.aiLimits {
			if strings.TrimSpace(typed) == saved[i] {
				continue
			}
			n, ok := limit(typed)
			if !ok {
				s.aiStatus = status{text: s.text.InvalidLimit}
				s.announce = s.aiStatus.text
				return
			}
			*fields[i] = &n
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
				s.settings.AISummaryLimit, s.settings.AIChunkLimit = settings.AISummaryLimit, settings.AIChunkLimit
				s.settings.AIAutoDailyLimit, s.settings.AIAutoFetchLimit = settings.AIAutoDailyLimit, settings.AIAutoFetchLimit
				s.settings.AIAutoCallsToday, s.settings.AIEndpointLocal = settings.AIAutoCallsToday, settings.AIEndpointLocal
				if removeKey {
					s.aiStatus = status{ok: true, text: s.text.KeyRemoved}
				} else {
					s.aiBase, s.aiModel, s.aiKey, s.aiLanguage = settings.AIBaseURL, settings.AIModel, "", settings.AILanguage
					s.aiLimits = savedLimits(settings)
					s.aiStatus = status{ok: true, text: s.text.AISaved}
				}
				s.announce = s.aiStatus.text
			case code(err) == "invalid_ai_base_url":
				s.aiInvalid, s.focusAIBase = true, true
				s.announce = s.text.InvalidAIBase
			case code(err) == "invalid_ai_limit":
				s.aiStatus = status{text: s.text.InvalidLimit}
				s.announce = s.aiStatus.text
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
	s.testing, s.aiStatus, s.aiChecks = true, status{}, nil
	s.announce = s.text.Testing
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		found, err := api.TestAI(context.Background())
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			s.testing = false
			if err == nil {
				s.aiStatus = status{ok: true, text: s.fill(s.text.Tested, "{model}", found.Model)}
				s.aiChecks = s.checks(found)
			} else {
				s.aiStatus = status{text: s.reason(code(err))}
			}
			s.announce = s.aiStatus.text
		}
	})
}

// checks are the lines under a passed test: the model is in the endpoint's list, and its context fits the limits.
// Whatever the endpoint did not tell is left out.
func (s *Screen) checks(found client.AITest) []status {
	var lines []status
	if found.ModelFound != nil && *found.ModelFound {
		lines = append(lines, status{ok: true, text: s.text.CheckListed})
	}
	if found.ContextTokens != nil && found.ContextFits != nil {
		n := strconv.Itoa(*found.ContextTokens)
		if *found.ContextFits {
			lines = append(lines, status{ok: true, text: s.fill(s.text.CheckFits, "{n}", n)})
		} else {
			lines = append(lines, status{warn: true, text: s.fill(s.text.CheckSmall, "{n}", n)})
		}
	}
	return lines
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
	case "ai_model_not_found":
		return s.text.AIModelMissing
	case "invalid_ai_limit":
		return s.text.InvalidLimit
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

// importEvery is how often the screen asks the server how an import goes.
const importEvery = 250 * time.Millisecond

// importQuarters is the size of the file from which the count is announced when it passes a quarter.
const importQuarters = 20

// importRun is the import of the files chosen or dropped, one after the other, each as a job of the server.
type importRun struct {
	paths, names []string
	drop         bool // the files were dropped, so their extension is checked
	next         int  // the file being imported
	outcomes     []fileOutcome
	job          client.OPMLImport // the job of this file, as of the last answer
	starting     bool              // the request that starts the job is under way
	polling      bool              // a request for the state of the job is under way
	at           time.Time         // when the job was last asked about
	lost         int               // how many answers in a row failed or were of another job
	said         int               // the quarters of this file announced
}

func (s *Screen) chooseImport() {
	if s.busy || s.api == nil {
		return
	}
	s.busy, s.result = true, result{}
	gen, filter := s.gen, s.text.OPMLFiles
	s.deps.Go(func() func() {
		path, err := s.deps.Open(filter)
		return func() {
			if gen != s.gen {
				return
			}
			if err != nil || path == "" {
				s.busy = false
				return
			}
			s.beginImport([]string{path}, false)
		}
	})
}

// beginImport starts importing the files at paths, one after the other; s.busy is set.
func (s *Screen) beginImport(paths []string, drop bool) {
	names := make([]string, len(paths))
	for i, path := range paths {
		names[i] = filepath.Base(path)
	}
	r := &importRun{paths: paths, names: names, drop: drop}
	s.run = r
	s.announce = s.importLabel(r)
	s.importFile(r)
}

// importLabel says which file is being imported, and which of several.
func (s *Screen) importLabel(r *importRun) string {
	if len(r.paths) == 1 {
		return s.fill(s.text.Importing, "{file}", r.names[0])
	}
	return s.fill(s.text.ImportingOf, "{file}", r.names[r.next], "{i}", strconv.Itoa(r.next+1), "{n}", strconv.Itoa(len(r.paths)))
}

// importFile reads the file r.next and has the server start importing it.
func (s *Screen) importFile(r *importRun) {
	i := r.next
	path, drop := r.paths[i], r.drop
	r.starting = true
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		var job client.OPMLImport
		var err error
		var data []byte
		if ext := strings.ToLower(filepath.Ext(path)); drop && ext != ".opml" && ext != ".xml" {
			err = errNotOPML
		} else if data, err = readOPML(path); err == nil {
			job, err = api.StartOPMLImport(context.Background(), data)
		}
		return func() {
			if gen != s.gen || r != s.run {
				return
			}
			s.started(r, job, err)
		}
	})
}

// started takes the answer to the request that starts the job of a file.
func (s *Screen) started(r *importRun, job client.OPMLImport, err error) {
	r.starting = false
	if s.expired(err) {
		return
	}
	if err != nil {
		s.fileEnded(r, fileOutcome{name: r.names[r.next], err: err})
		return
	}
	r.job, r.at, r.lost, r.said = job, s.deps.Now(), 0, 0
	if job.Phase != client.OPMLRunning {
		s.polled(r, job, nil)
	}
}

// watchImport asks for the state of the job once its time has come, and wakes the screen for the next time. It runs
// while the page is drawn; an import goes on without it and shows where it has got to when the page is back.
func (s *Screen) watchImport(c *ui.Context) {
	r := s.run
	if s.api == nil || r == nil || r.starting || r.polling || r.job.Phase != client.OPMLRunning {
		return
	}
	if wait := importEvery - s.deps.Now().Sub(r.at); wait > 0 {
		c.After(wait)
		return
	}
	r.polling, r.at = true, s.deps.Now()
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		job, err := api.OPMLImportStatus(context.Background())
		return func() {
			if gen != s.gen || r != s.run {
				return
			}
			r.polling, r.at = false, s.deps.Now()
			if s.expired(err) {
				return
			}
			s.polled(r, job, err)
		}
	})
}

// polled takes the state the server reports for the job of a file. More than three answers in a row that fail, or
// that are of another job, which another device started, mean the screen has lost the job.
func (s *Screen) polled(r *importRun, job client.OPMLImport, err error) {
	name := r.names[r.next]
	if err != nil || job.ID != r.job.ID {
		if r.lost++; r.lost > 3 {
			s.fileEnded(r, fileOutcome{name: name, err: errLost})
		}
		return
	}
	r.lost, r.job = 0, job
	switch job.Phase {
	case client.OPMLDone, client.OPMLFailed:
		o := fileOutcome{name: name, stopped: job.Phase == client.OPMLFailed}
		if job.Result != nil {
			o.imported = *job.Result
		}
		s.fileEnded(r, o)
	default:
		// A long file is announced as it passes a quarter, not at every count.
		if total := job.Progress.Total; total >= importQuarters {
			if q := job.Progress.Done * 4 / total; q >= 1 && q <= 3 && q > r.said {
				r.said = q
				s.announce = s.fill(s.text.Progress, "{done}", strconv.Itoa(job.Progress.Done), "{total}", strconv.Itoa(total))
			}
		}
	}
}

// fileEnded records how a file came out and goes on to the next one, or shows the result after the last.
func (s *Screen) fileEnded(r *importRun, o fileOutcome) {
	r.outcomes = append(r.outcomes, o)
	r.job, r.starting, r.polling, r.lost, r.said = client.OPMLImport{}, false, false, 0, 0
	// A file that stopped or was lost ends the run: the files after it are not imported.
	halted := o.stopped || errors.Is(o.err, errLost)
	if r.next++; r.next < len(r.paths) && !halted {
		s.importFile(r)
		return
	}
	for ; r.next < len(r.paths); r.next++ {
		r.outcomes = append(r.outcomes, fileOutcome{name: r.names[r.next], err: errSkipped})
	}
	s.busy, s.run = false, nil
	if len(r.paths) == 1 {
		s.imported(o)
	} else {
		s.importedFiles(r.outcomes)
	}
}

// imported shows the result of one file.
func (s *Screen) imported(o fileOutcome) {
	file, name, imported := "{file}", o.name, o.imported
	counts := []string{file, name, "{added}", strconv.Itoa(imported.Added), "{existing}", strconv.Itoa(imported.Existing)}
	switch {
	case o.err == nil && o.stopped:
		s.result = result{tone: partly, text: s.fill(s.text.Stopped, counts...), fails: imported.Failed, view: imported.Added > 0}
	case o.err == nil && len(imported.Failed) > 0:
		s.result = result{tone: partly, text: s.fill(s.text.Imported, append(counts, "{failed}", strconv.Itoa(len(imported.Failed)))...),
			fails: imported.Failed, view: imported.Added > 0}
	case o.err == nil:
		s.result = result{tone: done, text: s.fill(s.text.ImportedOk, counts...), view: imported.Added > 0}
	case errors.Is(o.err, errLost):
		s.result = result{tone: partly, text: s.fill(s.text.Lost, file, name)}
	case errors.Is(o.err, errTooLarge) || code(o.err) == "opml_too_large":
		s.result = result{tone: refused, text: s.fill(s.text.TooLarge, file, name)}
	case errors.Is(o.err, errRead):
		s.result = result{tone: refused, text: s.fill(s.text.ReadFailed, file, name)}
	case errors.Is(o.err, errNotOPML) || code(o.err) == "invalid_opml":
		s.result = result{tone: refused, text: s.fill(s.text.InvalidOPML, file, name)}
	case code(o.err) == "opml_import_busy":
		s.result = result{tone: refused, text: s.text.ImportBusy}
	default:
		s.result = result{tone: refused, text: s.fill(s.text.ImportFailed, file, name)}
	}
	s.announce = s.result.text
	if imported.Added > 0 && o.err == nil {
		s.deps.Imported()
	}
}

// fileOutcome is what importing one file came to. A job that stopped part way has stopped set and the counts up to
// where it stopped in imported.
type fileOutcome struct {
	name     string
	imported client.Imported
	err      error
	stopped  bool
}

var (
	errNotOPML = errors.New("not an OPML file")
	errLost    = errors.New("lost the import job")
	errSkipped = errors.New("not imported, an earlier file stopped")
)

// busyDrop reports whether files dropped now are turned away, because an import, backup or restore is running or a
// dialog is open; it says so with a toast.
func (s *Screen) busyDrop() bool {
	if s.busy || s.confirming || s.revoking || s.reader.open || s.rules.dialog.open {
		s.toast = s.text.DropBusy
		return true
	}
	return false
}

// ImportFiles imports the OPML files dropped on the window, from the shell once it has opened the screen, and shows
// one result for all of them. Files that are not OPML, are too large or cannot be read are listed with the
// subscriptions that could not be added.
func (s *Screen) ImportFiles(paths []string) {
	if s.api == nil || len(paths) == 0 {
		return
	}
	s.group = opml
	if s.busyDrop() {
		return
	}
	s.busy, s.result = true, result{}
	s.beginImport(paths, true)
}

// importedFiles shows the result of several files together: the counts of all of them, and a list of the
// subscriptions that could not be added and of the files that could not be imported.
func (s *Screen) importedFiles(outcomes []fileOutcome) {
	var added, existing int
	var fails []client.ImportFail
	halt, haltLost := 0, false // the file that stopped or was lost, counted from 1
	for i, o := range outcomes {
		if halt == 0 && (o.stopped || errors.Is(o.err, errLost)) {
			halt, haltLost = i+1, errors.Is(o.err, errLost)
		}
		added += o.imported.Added
		existing += o.imported.Existing
		fails = append(fails, o.imported.Failed...)
		if (o.err == nil && !o.stopped) || errors.Is(o.err, errLost) {
			continue
		}
		why := "import_failed"
		switch {
		case errors.Is(o.err, errSkipped):
			why = "skipped"
		case errors.Is(o.err, errNotOPML) || code(o.err) == "invalid_opml":
			why = "not_opml"
		case errors.Is(o.err, errTooLarge) || code(o.err) == "opml_too_large":
			why = "too_large"
		case errors.Is(o.err, errRead):
			why = "unreadable"
		case code(o.err) == "opml_import_busy":
			why = "busy"
		}
		fails = append(fails, client.ImportFail{Title: o.name, Error: why})
	}
	tone := done
	switch {
	case len(fails) > 0 && added+existing == 0:
		tone = refused
	case len(fails) > 0:
		tone = partly
	}
	text := s.text.ImportedMany
	if halt > 0 {
		tone, text = partly, s.text.StoppedMany
		if haltLost {
			text = s.text.LostMany
		}
	}
	s.result = result{tone: tone, fails: fails, view: added > 0, text: s.fill(text, "{n}", strconv.Itoa(len(outcomes)), "{i}", strconv.Itoa(halt),
		"{added}", strconv.Itoa(added), "{existing}", strconv.Itoa(existing), "{failed}", strconv.Itoa(len(fails)))}
	s.announce = s.result.text
	if added > 0 {
		s.deps.Imported()
	}
}

// subscriptionsIn counts the subscriptions of an OPML file: the outlines with a feed address or an RSSHub route.
func subscriptionsIn(data []byte) int {
	n := 0
	d := xml.NewDecoder(bytes.NewReader(data))
	d.CharsetReader = charset.NewReaderLabel
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
				s.result = result{tone: done, text: s.fill(locale.Count(s.text.Exported, "n", subscriptionsIn(data)),
					"{file}", name)}
			}
			s.result.export = true
			s.announce = s.result.text
		}
	})
}

// backupName is the name a backup file is suggested under: Oriel and the day, with the extension of the format.
func (s *Screen) backupName() string {
	return "Oriel-" + s.deps.Now().Format("2006-01-02") + ".oriel-backup"
}

// back up asks where to save and has the server stream everything to that file.
func (s *Screen) backUp() {
	if s.busy || s.api == nil {
		return
	}
	s.busy, s.result = true, result{}
	gen, api, filter, suggested := s.gen, s.api, s.text.BackupFiles, s.backupName()
	s.deps.Go(func() func() {
		path, err := s.deps.Save(suggested, filter)
		if err != nil || path == "" {
			return func() {
				if gen == s.gen {
					s.busy = false
				}
			}
		}
		name := filepath.Base(path)
		s.deps.Go(func() func() {
			created, err := writeBackup(api, path)
			return func() {
				if gen != s.gen || s.expired(err) {
					return
				}
				s.busy, s.working = false, ""
				switch {
				case err == nil:
					s.result = result{tone: done, text: s.fill(s.text.BackedUp, "{file}", name)}
				case !created:
					s.result = result{tone: refused, text: s.fill(s.text.WriteFailed, "{file}", name)}
				default:
					s.result = result{tone: refused, text: s.text.BackupFailed}
				}
				s.result.backup = true
				s.announce = s.result.text
			}
		})
		return func() {
			if gen == s.gen && s.busy {
				s.working = s.text.BackingUp
				s.announce = s.text.BackingUp
			}
		}
	})
}

// writeBackup saves the server's backup at path and removes the file again when it did not complete. created says
// whether the disk took the file, to tell a failure of the disk from one of the server.
func writeBackup(api API, path string) (created bool, err error) {
	f, err := os.Create(path)
	if err != nil {
		return false, err
	}
	_, err = api.Backup(context.Background(), f)
	if closeErr := f.Close(); err == nil && closeErr != nil {
		err, created = closeErr, false
	} else {
		created = true
	}
	if err != nil {
		os.Remove(path)
	}
	return created, err
}

// chooseRestore asks which backup to restore and then, before anything is replaced, whether to go on.
func (s *Screen) chooseRestore() {
	if s.busy || s.api == nil {
		return
	}
	s.busy, s.result = true, result{}
	gen, filter := s.gen, s.text.BackupFiles
	s.deps.Go(func() func() {
		path, err := s.deps.Open(filter, "oriel-backup")
		return func() {
			if gen != s.gen {
				return
			}
			if err != nil || path == "" {
				s.busy = false
				return
			}
			s.backupFile, s.confirming = path, true
		}
	})
}

// restore uploads the chosen backup, which replaces the server's data.
func (s *Screen) restore() {
	if s.api == nil || s.backupFile == "" {
		return
	}
	gen, api, path := s.gen, s.api, s.backupFile
	name := filepath.Base(path)
	s.working = s.fill(s.text.Restoring, "{file}", name)
	s.announce = s.working
	s.deps.Go(func() func() {
		var restored client.Restored
		f, err := os.Open(path)
		if err != nil {
			err = errRead
		} else {
			defer f.Close()
			restored, err = api.Restore(context.Background(), f)
		}
		return func() { s.restoreDone(gen, name, restored, err) }
	})
}

func (s *Screen) restoreDone(gen int, name string, restored client.Restored, err error) {
	if gen != s.gen || s.expired(err) {
		return
	}
	s.busy, s.working, s.backupFile = false, "", ""
	file := "{file}"
	switch {
	case err == nil:
		// The settings and the devices are the restored ones now; Open clears the result, so it comes first.
		s.Open()
		s.result = result{tone: done, text: locale.Count(locale.Count(s.text.RestoreDone, "sources", restored.Sources), "entries", restored.Entries)}
		s.deps.Imported()
	case errors.Is(err, errRead):
		s.result = result{tone: refused, text: s.fill(s.text.ReadFailed, file, name)}
	case code(err) == "invalid_backup":
		s.result = result{tone: refused, text: s.fill(s.text.RestoreNotBackup, file, name)}
	case code(err) == "corrupt_backup":
		s.result = result{tone: refused, text: s.fill(s.text.RestoreCorrupt, file, name)}
	case code(err) == "backup_newer":
		s.result = result{tone: refused, text: s.fill(s.text.RestoreNewer, file, name)}
	case code(err) == "backup_too_large":
		s.result = result{tone: refused, text: s.fill(s.text.RestoreTooLarge, file, name)}
	case code(err) == "restore_running" || code(err) == "vacuum_running":
		s.result = result{tone: refused, text: s.text.RestoreBusy}
	default:
		s.result = result{tone: refused, text: s.fill(s.text.RestoreFailed, file, name)}
	}
	s.result.backup = true
	s.announce = s.result.text
}

// restoreDialog asks to replace the data with the chosen backup. Cancel is the default; the file is not read before
// the answer.
func (s *Screen) restoreDialog(c *ui.Context) {
	if !s.confirming {
		return
	}
	switch ui.AlertDialog(c, &s.confirming, s.text.RestoreTitle,
		s.fill(s.text.RestoreBody, "{file}", filepath.Base(s.backupFile)), s.text.Cancel, s.text.RestoreConfirm) {
	case 1:
		s.restore()
	default:
		if !s.confirming {
			s.busy, s.backupFile = false, ""
		}
	}
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
	name, platform, icon := s.deviceParts(d)
	if platform != "" {
		name += " · " + platform
	}
	return name, icon
}

// deviceParts is the client and the platform a device signed in with, apart; the platform is empty when the
// User-Agent does not tell it.
func (s *Screen) deviceParts(d client.Device) (name, platform string, icon *ui.SVG) {
	ua := strings.TrimSpace(d.UserAgent)
	if ua == "" {
		return s.text.UnknownDevice, "", icons.Laptop
	}
	if rest, ok := strings.CutPrefix(ua, "Oriel"); ok && (rest == "" || rest[0] == '/' || rest[0] == ' ') {
		if _, inside, ok := strings.Cut(rest, "("); ok {
			platform, _, _ = strings.Cut(inside, ")")
		}
		if platform == "" {
			return "Oriel", "", icons.Laptop
		}
		return "Oriel", platform, glyph(platform)
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
		return ua, "", icons.Laptop
	}
	return browser, platform, glyph(platform)
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
	return locale.Date(s.lang, now, time.UnixMilli(ms))
}

// ago is how long ago a time was, within a week, else its day.
func (s *Screen) ago(ms int64, now time.Time) string {
	return locale.Ago(s.lang, now, time.UnixMilli(ms))
}

func (s *Screen) groupName(g int) string {
	switch g {
	case appearance:
		return s.text.Appearance
	case rules:
		return s.text.Rules
	case opml:
		return s.text.OPML
	case ai:
		return s.text.AI
	case devices:
		return s.text.Devices
	case activity:
		return s.text.Activity
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
	return locale.Count(s.text.Days, "n", days)
}

// View draws the settings; it does no disk or network work, which runs through Deps.
func (s *Screen) View(c *ui.Context) {
	t := c.Theme()
	s.typing = false
	if !s.revoking && !s.reader.open && !s.rules.dialog.open && !s.uiFont.open && !s.rdFont.open {
		c.OnShortcut(0, ui.KeyEscape, s.escape)
	}
	width, _ := c.Size()
	compact := width < 760
	room := s.room(c, width, compact)
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(t.Background).Children(func() {
		s.sidebar(c, compact)
		// The title row floats over the page, which scrolls under it and fades under the scroll edge.
		bar := c.TitleBar()
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			s.content(c, compact, room)
			theme.Edge(c, s.scroll.Y > 0)
			ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(theme.Bar).AlignItems(ui.Center).
				Padding(0, t.Space(3)+bar.Right, 0, s.titleLeft(t, compact, room)).DragWindow().Children(func() {
				ui.Text(c, s.groupName(s.group)).FontSize(theme.Px(t, 15)).FontWeight(600).SingleLine().MinWidth(0).
					Shrink(1).Role(ui.RoleHeading).Level(1)
			})
		})
	})
	if s.revoking {
		name, _ := s.device(s.doomed)
		if ui.AlertDialog(c, &s.revoking, s.fill(s.text.RevokeTitle, "{name}", name), s.text.RevokeBody,
			s.text.Cancel, s.text.RevokeConfirm) == 1 {
			s.revoke()
		}
	}
	s.readerDialog(c)
	s.ruleDialog(c)
	s.restoreDialog(c)
	if s.announce != "" {
		c.Announce(s.announce)
		s.announce = ""
	}
	if s.toast != "" {
		c.Toast(s.toast)
		s.toast = ""
	}
	s.restoreToast(c)
	s.deletedToast(c)
}

// restoreToast shows the toast of Restore default on a prompt, with Undo.
func (s *Screen) restoreToast(c *ui.Context) {
	r := s.restored
	if r == nil || r.shown {
		return
	}
	r.shown = true
	c.AddToast(ui.Toast{ID: restoredToast, Title: s.fill(s.text.PromptRestored, "{name}", r.name), Timeout: 8 * time.Second,
		Action: s.text.Undo, OnAction: func() { s.undoRestore(r) },
		OnClose: func() {
			if s.restored == r {
				s.restored = nil
			}
		},
		Data: theme.ToastLook{Icon: icons.RotateCcw, Close: s.text.Dismiss}})
}

// undoRestore puts back the text a prompt had before Restore default, unless it has been edited since.
func (s *Screen) undoRestore(r *restoration) {
	if s.restored != r {
		return
	}
	s.restored = nil
	if s.prompts[r.i] == s.defaults()[r.i] {
		s.prompts[r.i] = r.before
	}
}

// sidebarWidth is the sidebar's width.
func sidebarWidth(t *ui.Theme, compact bool) float32 {
	if compact {
		return theme.Px(t, 184)
	}
	return theme.Px(t, 216)
}

// room is the width of the main area, which is what is left of the window after the sidebar and its margin; the
// layout cannot measure it, and the page widths below depend on it.
func (s *Screen) room(c *ui.Context, width float32, compact bool) float32 {
	return width - sidebarWidth(c.Theme(), compact) - 8
}

func (s *Screen) sidebar(c *ui.Context, compact bool) {
	main := c.Theme()
	width := sidebarWidth(main, compact)
	pane := ui.Column(c).Width(width).Margin(8, 0, 8, 8)
	theme.Pane(c, pane).Children(func() {
		// The pane has a theme of its own, light on dark in both schemes; the main one is back after it.
		c.SetTheme(theme.PaneTheme(main))
		defer c.SetTheme(main)
		t := c.Theme()
		// The first 40 are the window buttons' and drag the window.
		ui.Box(c).FillWidth().Height(40).DragWindow()
		ui.Row(c).FillWidth().Height(theme.Px(t, 36)).AlignItems(ui.Center).PaddingX(t.Space(2)).Children(func() {
			label := s.text.BackTimeline
			b := ui.ButtonBase(c).Height(theme.Px(t, 28)).MinWidth(0).Shrink(1).Gap(2).Padding(0, t.Space(2), 0, t.Space(1)).
				Radius(t.Space(1.5)).Label(label)
			color := t.TextMuted
			if b.Pressed() {
				b.Background(t.SurfacePressed)
				color = t.Text
			} else if b.Hovered() {
				b.Background(t.SurfaceHover)
				color = t.Text
			}
			b.Children(func() {
				ui.Icon(c, icons.ChevronLeft).FontSize(theme.IconMedium(t)).TextColor(color)
				ui.Text(c, label).SingleLine().MinWidth(0).Shrink(1).TextColor(color)
			})
			if s.blur {
				b.Focus()
				s.blur = false
			}
			if b.Clicked() {
				s.deps.Back()
			}
		})
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(t.Space(2)).Label(s.text.Settings).Children(func() {
				ui.Text(c, s.text.Settings).SingleLine().FontSize(theme.Px(t, 11)).FontWeight(500).TextColor(t.TextMuted).
					Margin(t.Space(3), t.Space(2), t.Space(1), t.Space(2))
				for g, icon := range []*ui.SVG{icons.Palette, icons.Rss, icons.ListFilter, icons.ArrowUpDown, icons.Sparkles, icons.Laptop, icons.Database} {
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
	item := ui.ButtonBase(c).Key(g).FillWidth().Height(theme.Px(t, 30)).Gap(theme.Px(t, 10)).PaddingX(theme.Px(t, 10)).
		Radius(t.Space(2)).Label(name)
	iconColor, nameColor := t.TextMuted, theme.Text2(t)
	switch {
	case g == s.group:
		item.Background(theme.Chosen(t))
		iconColor, nameColor = t.Accent, t.Text
	case item.Pressed():
		item.Background(t.SurfacePressed)
	case item.Hovered():
		item.Background(t.SurfaceHover)
		iconColor = theme.Text2(t)
	}
	theme.Fade(item)
	item.Children(func() {
		ui.Icon(c, icon).FontSize(theme.IconMedium(t)).TextColor(iconColor)
		label := ui.Text(c, name).Grow(1).MinWidth(0).SingleLine().TextColor(nameColor)
		if g == s.group {
			label.FontWeight(500)
		}
	})
	if item.Clicked() {
		if g != s.group {
			s.healthAsked = false
		}
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
	ui.Row(c).FillWidth().Height(theme.Px(t, 40)).AlignItems(ui.Center).Gap(t.Space(2)).PaddingX(t.Space(4)).
		Tooltip(s.text.ServerNote).Children(func() {
		ui.Icon(c, icon).FontSize(theme.IconMedium(t)).TextColor(t.TextMuted)
		ui.Text(c, name).Grow(1).MinWidth(0).SingleLine().FontSize(theme.Px(t, 12)).TextColor(t.TextMuted)
		ui.Box(c).Size(6, 6).Radius(theme.Pill(6)).Background(t.Success)
	})
}

func (s *Screen) content(c *ui.Context, compact bool, room float32) {
	state := s.state
	switch s.group {
	case appearance, opml:
		state = loaded
	case rules:
		if state == loaded {
			state = s.rules.state
		}
	case devices:
		state = s.deviceState
	case activity:
		state = s.actState
		s.watchActivity(c)
	}
	switch state {
	case failed:
		s.failure(c)
		return
	case loading:
		s.page(c, compact, formWidth(c.Theme()), func() { s.skeleton(c) })
		return
	}
	s.page(c, compact, s.pageWidth(c.Theme(), room), func() {
		switch s.group {
		case appearance:
			s.appearancePage(c)
		case rules:
			s.rulesPage(c)
		case opml:
			s.opmlPage(c)
		case ai:
			s.aiPage(c, s.twoColumns(c.Theme(), room))
		case devices:
			s.devicesPage(c, s.wideTable(c.Theme(), room))
		case activity:
			s.activityPage(c)
		default:
			s.fetchingPage(c)
		}
	})
}

// Page widths, padding included. A form is 600 wide; the devices table is 880 wide once the main area holds it
// (tableRoom), and the AI page is two columns up to 1040 wide once the main area holds them (splitRoom).
const (
	tableRoom = 656
	splitRoom = 976
)

func formWidth(t *ui.Theme) float32 { return theme.Px(t, 600) }

// pageWidth is the widest the page of the current group goes, for the room the main area has.
func (s *Screen) pageWidth(t *ui.Theme, room float32) float32 {
	switch {
	case s.wideTable(t, room):
		return theme.Px(t, 880)
	case s.twoColumns(t, room):
		return theme.Px(t, 1040)
	}
	return formWidth(t)
}

// wideTable is whether the devices come as a table.
func (s *Screen) wideTable(t *ui.Theme, room float32) bool {
	return s.group == devices && s.signedIn && room >= theme.Px(t, tableRoom)
}

// twoColumns is whether the AI page puts the connection and the prompts side by side.
func (s *Screen) twoColumns(t *ui.Theme, room float32) bool {
	return s.group == ai && room >= theme.Px(t, splitRoom)
}

// titleLeft is the title's left padding, which puts it on the left edge of the centered page.
func (s *Screen) titleLeft(t *ui.Theme, compact bool, room float32) float32 {
	pad := theme.Px(t, 32)
	if compact {
		pad = theme.Px(t, 20)
	}
	col := min(room, s.pageWidth(t, room))
	return max(pad, (room-col)/2+pad)
}

func (s *Screen) page(c *ui.Context, compact bool, maxWidth float32, children func()) {
	t := c.Theme()
	ui.Scroll(c).TrackScroll(&s.scroll).Grow(1).FillWidth().ScrollbarInsets(theme.Bar, 0, 0, 0).Children(func() {
		p := ui.Column(c).FillWidth().MaxWidth(maxWidth).Margin(0, ui.Auto)
		if compact {
			p.Padding(theme.Bar+theme.Px(t, 20), theme.Px(t, 20), theme.Px(t, 40), theme.Px(t, 20))
		} else {
			p.Padding(theme.Bar+theme.Px(t, 32), theme.Px(t, 32), theme.Px(t, 48), theme.Px(t, 32))
		}
		p.Children(children)
	})
}

func (s *Screen) skeleton(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().Label(s.text.Loading).Role(ui.RoleProgress).Children(func() {
		ui.Box(c).Width(theme.Px(t, 320)).MaxWidth(theme.Px(t, 360)).Height(theme.Px(t, 12)).Radius(theme.Pill(theme.Px(t, 12))).Background(theme.Track(t)).
			Margin(theme.Px(t, 10), 0)
		ui.Box(c).FillWidth().Height(theme.Px(t, 160)).Radius(10).Background(theme.Track(t)).Margin(t.Space(4), 0, 0, 0)
	})
}

func (s *Screen) failure(c *ui.Context) {
	t := c.Theme()
	title, hint := s.text.Error, s.text.ErrorHint
	if s.group == rules {
		title, hint = s.text.RulesError, s.text.RulesErrorHint
	}
	ui.Column(c).Grow(1).FillWidth().Center().Gap(t.Space(3)).Padding(theme.Bar+t.Space(6), t.Space(6), t.Space(6)).
		Role(ui.RoleStatus).
		Children(func() {
			ui.Box(c).Size(t.Space(12), t.Space(12)).Radius(t.Space(3)).Center().Background(theme.Soft(t, t.Danger)).
				Children(func() { ui.Icon(c, icons.WifiOff).FontSize(theme.IconLarge(t)).TextColor(t.Danger) })
			ui.Text(c, title).FontSize(theme.Px(t, 15)).FontWeight(600).Role(ui.RoleHeading).Level(2)
			ui.Text(c, hint).MaxWidth(theme.Px(t, 320)).TextColor(t.TextMuted).Wrap()
			retry := theme.Button{Style: theme.Secondary, Icon: icons.RefreshCw, Text: s.text.Retry}.Draw(c)
			if retry.Clicked() {
				switch s.group {
				case rules:
					s.loadRules()
					s.loadSettings()
				case devices:
					s.loadDevices()
				case activity:
					s.fetchActivity()
				default:
					s.loadSettings()
				}
			}
		})
}

func lead(c *ui.Context, text string) {
	t := c.Theme()
	ui.Text(c, text).TextColor(t.TextMuted).Wrap().MaxWidth(theme.Px(t, 600)).Margin(0, 0, theme.Px(t, 20), 0)
}

// card draws rows on a tinted Surface card, an inset hairline between each.
func card(c *ui.Context, rows ...func()) {
	t := c.Theme()
	ui.Column(c).FillWidth().Radius(12).Background(t.Surface).Children(func() {
		for i, row := range rows {
			if i > 0 {
				ui.Box(c).FillWidth().Height(1).Margin(0, theme.Px(t, 20)).Background(t.Border)
			}
			ui.Column(c).Key(i).FillWidth().Padding(theme.Px(t, 16), theme.Px(t, 20)).Children(row)
		}
	})
}

// labeled draws a setting's label and help beside its control.
func labeled(c *ui.Context, label, help string, control func()) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Text(c, label).FontWeight(500)
			if help != "" {
				ui.Text(c, help).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Wrap().Margin(theme.Px(t, 4), 0, 0, 0)
			}
		})
		control()
	})
}

func statusLine(c *ui.Context, st status) {
	if st.text == "" {
		return
	}
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(theme.Px(t, 6)).Margin(theme.Px(t, 6), 0, 0, 0).Role(ui.RoleStatus).
		Children(func() { statusText(c, st) })
}

// statusText is the icon and text of a status.
func statusText(c *ui.Context, st status) {
	t := c.Theme()
	icon, color := icons.CircleAlert, t.Danger
	iconColor := color
	switch {
	case st.warn:
		color, iconColor = t.Text, t.Warning
	case st.spin:
		icon, color = icons.LoaderCircle, t.TextMuted
	case st.muted:
		icon, color = nil, t.TextMuted
	case st.ok:
		icon, color = icons.Check, t.Success
	}
	if st.spin || st.muted || st.ok {
		iconColor = color
	}
	if icon != nil {
		i := ui.Icon(c, icon).FontSize(theme.IconSmall(t)).TextColor(iconColor).Margin(1, 0, 0, 0)
		if st.spin {
			i.Rotate(i.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
		}
	}
	ui.Text(c, st.text).FontSize(theme.Px(t, 12)).TextColor(color).Shrink(1).Wrap()
}

// coded is help text with one placeholder drawn as code, such as an address or a route.
func coded(c *ui.Context, text, placeholder, code string) ui.Element {
	t := c.Theme()
	before, after, _ := strings.Cut(text, placeholder)
	return ui.RichText(c,
		ui.Span{Text: before, Color: t.TextMuted},
		ui.Span{Text: code, Font: "monospace", Color: t.TextMuted},
		ui.Span{Text: after, Color: t.TextMuted},
	).FontSize(theme.Px(t, 12)).Wrap()
}

func (s *Screen) fetchingPage(c *ui.Context) {
	t := c.Theme()
	s.watchHealth()
	lead(c, s.text.Lead)
	card(c, func() {
		ui.Text(c, s.text.RSSHub).FontWeight(500)
		coded(c, s.text.RSSHubHelp, "{route}", "/github/issue/golang/go").Margin(theme.Px(t, 4), 0, 0, 0)
		s.baseField(c)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Margin(theme.Px(t, 8), 0, 0, 0).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Start).Gap(theme.Px(t, 6)).Role(ui.RoleStatus).Children(func() {
				if s.baseStatus.text != "" {
					statusText(c, s.baseStatus)
				}
			})
			dirty := s.dirty()
			style := theme.Secondary
			if dirty {
				style = theme.Primary
			}
			save := theme.Button{Style: style, Disabled: !dirty || s.saving, Text: s.text.Save, Busy: s.saving}.Draw(c)
			if save.Clicked() {
				s.saveBase()
			}
		})
		s.healthBlock(c)
	}, func() {
		labeled(c, s.text.Retention, s.text.RetentionHelp, func() { s.retentionSelect(c) })
		statusLine(c, s.keepStatus)
	})
}

// field draws a text input editing value, width wide or growing for 0, a raised fill with a hairline edge, the
// focus ring while focused and a danger edge while invalid; input names and handles the input.
func field(c *ui.Context, key string, value *string, width float32, invalid bool, input func(in ui.Element)) (focused bool) {
	t := c.Theme()
	b := ui.Row(c).Key(key).MinWidth(0).Height(theme.Px(t, 32)).AlignItems(ui.Center).PaddingX(theme.Px(t, 10)).
		Radius(t.Radius).Background(theme.RaisedFill(t))
	if width > 0 {
		b.Width(width).Shrink(1)
	} else {
		b.Grow(1)
	}
	focused = b.FocusWithin()
	edge(c, b)
	switch {
	case invalid:
		b.Border(1.5, t.Danger)
		if focused {
			b.Shadow(0, 0, 0, 2, theme.Soft(t, t.Danger))
		}
	case focused:
		b.Shadow(0, 0, 0, 2, t.Focus)
	}
	b.Children(func() { input(ui.TextInputBase(c, value).Grow(1).MinWidth(0).FocusRing(false)) })
	return focused
}

// edge gives a field or a text area its hairline, a Border line with high contrast.
func edge(c *ui.Context, e ui.Element) {
	t := c.Theme()
	if c.Preferences().HighContrast {
		e.Border(1, t.Border)
		return
	}
	e.Border(0.5, theme.Ink(t, 0.12))
}

// trigger styles the button of a drop-down: a Track trough, a little darker under the pointer, with the focus ring
// while focused and a Border rim with high contrast.
func trigger(c *ui.Context, e ui.Element) {
	t := c.Theme()
	fill := theme.Track(t)
	if e.Hovered() {
		fill = fill.Mix(theme.Ink(t, 1), 0.06)
	}
	e.Background(fill)
	if c.Preferences().HighContrast {
		e.Border(1, t.Border)
	}
	if e.Focused() {
		e.Shadow(0, 0, 0, 2, t.Focus)
	}
}

// baseField is the list of RSSHub instances, one address per line. Enter starts a new line, so only Save saves.
func (s *Screen) baseField(c *ui.Context) {
	t := c.Theme()
	before := s.base
	area := ui.TextArea(c.Key("base"), &s.base).FillWidth().Lines(2, 6).Margin(theme.Px(t, 12), 0, 0, 0).Font("monospace").
		FontSize(theme.Px(t, 12)).Label(s.text.RSSHub).Placeholder(s.text.RSSHubPlaceholder).Disabled(s.saving)
	area.Background(theme.RaisedFill(t)).Radius(t.Radius)
	edge(c, area)
	switch {
	case s.invalid:
		area.Border(1.5, t.Danger).Error(s.baseStatus.text)
		if area.Focused() {
			area.Shadow(0, 0, 0, 2, theme.Soft(t, t.Danger))
		}
	case area.Focused():
		area.Shadow(0, 0, 0, 2, t.Focus)
	}
	s.watch(area.Focused())
	if s.focusBase {
		area.Focus()
		s.focusBase = false
	}
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
	sel.Trigger.MinWidth(theme.Px(t, 160)).Height(theme.Px(t, 32)).Gap(t.Space(2)).Padding(0, t.Space(2), 0, theme.Px(t, 10)).
		Radius(t.Radius).Label(s.text.Retention).Disabled(s.keeping)
	trigger(c, sel.Trigger)
	sel.Trigger.Children(func() {
		ui.Text(c, s.keep(s.retention)).Grow(1).MinWidth(0).SingleLine()
		ui.Icon(c, icons.ChevronDown).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Margin(4, 0, 0, 0).Padding(4).MinWidth(theme.Px(t, 160))
		theme.Sheet(c, panel, theme.Popup)
		for _, v := range options {
			item := sel.Item(v).FillWidth().Height(theme.Px(t, 28)).Gap(t.Space(2)).PaddingX(t.Space(2)).Radius(6)
			if item.Highlighted() {
				item.Background(theme.Chosen(t))
			}
			item.Children(func() {
				ui.Text(c, s.keep(v)).Grow(1).NoWrap()
				if v == s.retention {
					ui.Icon(c, icons.Check).FontSize(theme.IconSmall(t)).TextColor(t.Accent)
				}
			})
		}
	})
	if sel.Trigger.Changed() {
		s.saveRetention()
	}
}

// aiPage is the connection and the prompts, one under the other or, with room, side by side.
func (s *Screen) aiPage(c *ui.Context, two bool) {
	t := c.Theme()
	lead(c, s.text.AILead)
	if !two {
		s.aiConnection(c, 0)
		s.aiPrompts(c, theme.Px(t, 28), false)
		return
	}
	ui.Grid(c).FillWidth().Columns(2).GapX(theme.Px(t, 24)).AlignItems(ui.Start).Children(func() {
		ui.Column(c).MinWidth(0).Children(func() { s.aiConnection(c, 0) })
		ui.Column(c).MinWidth(0).Children(func() { s.aiPrompts(c, 0, true) })
	})
}

// sectionTitle is the small heading over a card.
func sectionTitle(c *ui.Context, text string, top float32) {
	t := c.Theme()
	ui.Text(c, text).FontSize(theme.Px(t, 12)).FontWeight(600).TextColor(t.TextMuted).
		Margin(top, 0, t.Space(2), 0).Role(ui.RoleHeading).Level(2)
}

// aiConnection is the endpoint, model, key and language, with Test and Save.
func (s *Screen) aiConnection(c *ui.Context, top float32) {
	t := c.Theme()
	sectionTitle(c, s.text.Connection, top)
	before := [...]string{s.aiBase, s.aiModel, s.aiKey, s.aiLanguage, s.aiLimits[0], s.aiLimits[1], s.aiLimits[2], s.aiLimits[3]}
	submit := func(in ui.Element) {
		if in.Submitted() {
			s.saveAI(false)
		}
	}
	card(c, func() {
		ui.Text(c, s.text.Endpoint).FontWeight(500)
		coded(c, s.text.EndpointHelp, "{address}", s.text.EndpointPlaceholder).Margin(theme.Px(t, 4), 0, 0, 0)
		if line := s.destination(); line != "" {
			ui.Text(c, line).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Wrap().Margin(theme.Px(t, 4), 0, 0, 0)
		}
		ui.Row(c).FillWidth().Margin(theme.Px(t, 12), 0, 0, 0).Children(func() {
			s.watch(field(c, "endpoint", &s.aiBase, 0, s.aiInvalid, func(in ui.Element) {
				in.Font("monospace").Label(s.text.Endpoint).Placeholder(s.text.EndpointPlaceholder).Disabled(s.aiSaving)
				if s.aiInvalid {
					in.Error(s.text.InvalidAIBase)
				}
				if s.focusAIBase {
					in.Focus()
					s.focusAIBase = false
				}
				submit(in)
			}))
		})
		if s.aiInvalid {
			statusLine(c, status{text: s.text.InvalidAIBase})
		}
		s.presetRow(c)
	}, func() {
		ui.Text(c, s.text.Model).FontWeight(500)
		hint := s.text.ModelPlaceholder
		if i := s.preset(); i >= 0 && presets[i].hint != "" {
			hint = presets[i].hint
		}
		ui.Row(c).FillWidth().Margin(theme.Px(t, 12), 0, 0, 0).Children(func() {
			s.watch(field(c, "model", &s.aiModel, 0, false, func(in ui.Element) {
				in.Font("monospace").Label(s.text.Model).Placeholder(hint).Disabled(s.aiSaving)
				submit(in)
			}))
		})
	}, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
			ui.Text(c, s.text.APIKey).FontWeight(500).Shrink(1).MinWidth(0)
			badge, fill, color := s.text.KeyUnset, theme.Track(t), theme.Text2(t)
			if s.settings.AIAPIKeySet {
				badge, fill, color = s.text.KeySet, theme.Soft(t, t.Accent), t.Accent
			}
			ui.Box(c).Height(theme.Px(t, 18)).PaddingX(theme.Px(t, 6)).Radius(theme.Pill(theme.Px(t, 18))).Center().Background(fill).Children(func() {
				ui.Text(c, badge).FontSize(theme.Px(t, 11)).FontWeight(500).TextColor(color).NoWrap()
			})
		})
		ui.Text(c, s.text.KeyHelp).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Wrap().Margin(theme.Px(t, 4), 0, 0, 0)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Margin(theme.Px(t, 12), 0, 0, 0).Children(func() {
			placeholder := s.text.KeyPlaceholder
			if s.settings.AIAPIKeySet {
				placeholder = s.text.KeyReplace
			}
			s.watch(field(c, "key", &s.aiKey, 0, false, func(in ui.Element) {
				in.Password().Label(s.text.APIKey).Placeholder(placeholder).Description(s.text.KeyHelp).
					Disabled(s.aiSaving)
				submit(in)
			}))
			if s.settings.AIAPIKeySet {
				remove := theme.Button{Style: theme.Secondary, Disabled: s.aiSaving, Text: s.text.RemoveKey}.Draw(c)
				if remove.Clicked() {
					s.saveAI(true)
				}
			}
		})
	}, func() {
		labeled(c, s.text.Language, s.text.LanguageHelp, func() { s.languageSelect(c) })
	}, func() {
		s.limitFields(c)
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
			ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Start).Gap(theme.Px(t, 6)).Role(ui.RoleStatus).Children(func() {
				if st.text != "" {
					statusText(c, st)
				}
			})
			test := theme.Button{Disabled: !s.settings.AIConfigured() || dirty || s.testing || s.aiSaving, Icon: icons.Plug,
				Text: s.text.Test}.Draw(c)
			if test.Clicked() {
				s.test()
			}
			style := theme.Secondary
			if dirty {
				style = theme.Primary
			}
			save := theme.Button{Style: style, Disabled: !dirty || s.aiSaving, Text: s.text.Save, Busy: s.aiSaving}.Draw(c)
			if save.Clicked() {
				s.saveAI(false)
			}
		})
		for _, st := range s.aiChecks {
			statusLine(c, st)
		}
	})
	if after := [...]string{s.aiBase, s.aiModel, s.aiKey, s.aiLanguage, s.aiLimits[0], s.aiLimits[1], s.aiLimits[2], s.aiLimits[3]}; after != before {
		s.aiInvalid, s.aiStatus, s.aiChecks = s.aiInvalid && after[0] == before[0], status{}, nil
	}
}

// presetRow is the buttons that fill the endpoint with the address of a server on this computer. They change only
// what is typed: the key stays, and nothing is saved until Save.
func (s *Screen) presetRow(c *ui.Context) {
	t := c.Theme()
	picked := s.preset()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Margin(theme.Px(t, 10), 0, 0, 0).Role(ui.RoleGroup).
		Label(s.text.Preset).Children(func() {
		ui.Text(c, s.text.Preset).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Margin(0, theme.Px(t, 4), 0, 0)
		for i, p := range presets {
			chosen := i == picked
			b := theme.Button{Small: true, On: chosen, Disabled: s.aiSaving, Text: p.name, Checked: &chosen}.Draw(c)
			if b.Clicked() {
				s.aiBase, s.aiInvalid = p.base, false
			}
		}
	})
}

// limitFields are the summary limit and the chunk size in characters, and the two caps on automatic work; empty
// follows the default. Below them, the sentence about the daily cap, once it is reached.
func (s *Screen) limitFields(c *ui.Context) {
	t := c.Theme()
	names := [4]string{s.text.SummaryLimit, s.text.ChunkLimit, s.text.DailyCap, s.text.FetchCap}
	defaults := [4]int{s.settings.DefaultAISummaryLimit, s.settings.DefaultAIChunkLimit,
		s.settings.DefaultAIAutoDaily, s.settings.DefaultAIAutoFetch}
	for row := 0; row < 2; row++ {
		margin := float32(0)
		if row > 0 {
			margin = theme.Px(t, 12)
		}
		ui.Row(c).FillWidth().Gap(t.Space(3)).Margin(margin, 0, 0, 0).Children(func() {
			for i := row * 2; i < row*2+2; i++ {
				ui.Column(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
					ui.Text(c, names[i]).FontWeight(500)
					ui.Row(c).FillWidth().Margin(theme.Px(t, 8), 0, 0, 0).Children(func() {
						s.watch(field(c, "limit"+strconv.Itoa(i), &s.aiLimits[i], 0, false, func(in ui.Element) {
							in.Font("monospace").Label(names[i]).Placeholder(strconv.Itoa(defaults[i])).Disabled(s.aiSaving)
							if in.Submitted() {
								s.saveAI(false)
							}
						}))
					})
				})
			}
		})
		help := s.text.LimitsHelp
		if row == 1 {
			help = s.text.CapsHelp
		}
		ui.Text(c, help).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Wrap().Margin(theme.Px(t, 4), 0, 0, 0)
	}
	if st := s.capStatus(); st.text != "" {
		statusLine(c, st)
	}
}

// capStatus is the plain sentence about the automatic requests of today: the count, and a warning once the daily
// cap is reached. It is empty while AI is not set up.
func (s *Screen) capStatus() status {
	set := s.settings
	if !set.AIConfigured() || set.AIAutoDailyLimit <= 0 {
		return status{}
	}
	text, warn := s.text.UsageToday, false
	if set.AIAutoCallsToday >= set.AIAutoDailyLimit {
		text, warn = s.text.CapReached, true
	}
	text = strings.NewReplacer("{used}", strconv.Itoa(set.AIAutoCallsToday), "{limit}", strconv.Itoa(set.AIAutoDailyLimit)).Replace(text)
	return status{muted: !warn, warn: warn, text: text}
}

// destination says where entry text goes once AI is set up, and whether that is this computer.
func (s *Screen) destination() string {
	if !s.settings.AIConfigured() {
		return ""
	}
	address := s.settings.AIBaseURL
	if u, err := url.Parse(address); err == nil && u.Host != "" {
		address = u.Host
	}
	text := s.text.DestinationRemote
	if s.settings.AIEndpointLocal {
		text = s.text.DestinationLocal
	}
	return strings.ReplaceAll(text, "{address}", address)
}

// aiPrompts is the summary and translation prompts, with Save.
func (s *Screen) aiPrompts(c *ui.Context, top float32, two bool) {
	t := c.Theme()
	sectionTitle(c, s.text.Prompts, top)
	typed := s.prompts
	card(c, func() { s.promptField(c, 0, s.text.PromptSummary, two) },
		func() { s.promptField(c, 1, s.text.PromptTranslate, two) },
		func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
				coded(c, s.text.PromptHelp, "{language}", "{language}").Grow(1).MinWidth(0)
				dirty := s.promptsDirty()
				style := theme.Secondary
				if dirty {
					style = theme.Primary
				}
				save := theme.Button{Style: style, Disabled: !dirty || s.promptSaving, Text: s.text.Save, Busy: s.promptSaving,
					Label: s.text.Prompts + " · " + s.text.Save}.Draw(c)
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
func (s *Screen) promptField(c *ui.Context, i int, label string, two bool) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
		ui.Text(c, label).FontWeight(500).Grow(1).MinWidth(0)
		reset := theme.Button{Small: true, Disabled: s.promptSaving || strings.TrimSpace(s.prompts[i]) == s.defaults()[i],
			Icon: icons.RotateCcw, Text: s.text.Reset, Label: label + " · " + s.text.Reset}.Draw(c)
		if reset.Clicked() {
			s.restored = &restoration{i: i, name: label, before: s.prompts[i]}
			s.prompts[i] = s.defaults()[i]
		}
	})
	area := ui.TextArea(c.Key(i), &s.prompts[i]).FillWidth().Lines(3, 10).Margin(theme.Px(t, 12), 0, 0, 0).Font("monospace").
		FontSize(theme.Px(t, 12)).Label(label).Disabled(s.promptSaving)
	if two {
		area.MinHeight(theme.Px(t, 120))
	}
	area.Background(theme.RaisedFill(t)).Radius(t.Radius)
	edge(c, area)
	if area.Focused() {
		area.Shadow(0, 0, 0, 2, t.Focus)
	}
	s.watch(area.Focused())
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
	sel.Trigger.MinWidth(theme.Px(t, 160)).Height(theme.Px(t, 32)).Gap(t.Space(2)).Padding(0, t.Space(2), 0, theme.Px(t, 10)).
		Radius(t.Radius).Label(s.text.Language).Disabled(s.aiSaving)
	trigger(c, sel.Trigger)
	sel.Trigger.Children(func() {
		ui.Text(c, name(s.aiLanguage)).Grow(1).MinWidth(0).SingleLine()
		ui.Icon(c, icons.ChevronDown).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Margin(4, 0, 0, 0).Padding(4).MinWidth(theme.Px(t, 160))
		theme.Sheet(c, panel, theme.Popup)
		for _, l := range languages {
			item := sel.Item(l.code).FillWidth().Height(theme.Px(t, 28)).Gap(t.Space(2)).PaddingX(t.Space(2)).Radius(6)
			if item.Highlighted() {
				item.Background(theme.Chosen(t))
			}
			item.Children(func() {
				ui.Text(c, l.name).Grow(1).NoWrap()
				if l.code == s.aiLanguage {
					ui.Icon(c, icons.Check).FontSize(theme.IconSmall(t)).TextColor(t.Accent)
				}
			})
		}
	})
}

// appearancePage is the preferences of this device. Each choice goes to Change at once; the page reads the
// preferences again each frame, so a choice that could not be saved shows as it was.
func (s *Screen) appearancePage(c *ui.Context) {
	t := c.Theme()
	p := s.deps.Preferences()
	lead(c, s.text.AppearanceLead)
	card(c, func() {
		labeled(c, s.text.Theme, s.text.ThemeHelp, func() {
			themes := []string{"", prefs.ThemeLight, prefs.ThemeDark}
			names := []string{s.text.ThemeSystem, s.text.ThemeLight, s.text.ThemeDark}
			if i, changed := s.segments(c, s.text.Theme, names, slices.Index(themes, p.Theme)); changed {
				p.Theme = themes[i]
				s.deps.Change(p)
			}
		})
	}, func() {
		labeled(c, s.text.UILanguage, s.text.UILanguageHelp, func() {
			options := []choice{{value: "", name: s.text.SystemChoice}}
			for _, l := range interfaceLanguages {
				options = append(options, choice{value: l.code, name: l.name})
			}
			if v, changed := s.pick(c, p.Language, s.text.UILanguage, options); changed {
				p.Language = v
				s.deps.Change(p)
			}
		})
	}, func() {
		labeled(c, s.text.UIFont, s.text.UIFontHelp, func() {
			// The pinned rows of the Interface font: the system's, and the family in use.
			pinned := []choice{{value: "", name: s.text.SystemFont}}
			fonts := s.deps.Fonts()
			if p.Font != "" && (fonts == nil || slices.Contains(fonts, p.Font)) {
				pinned = append(pinned, choice{value: p.Font, name: p.Font, family: p.Font})
			}
			if v, changed := s.fontPick(c, &s.uiFont, s.text.UIFont, p.Font, pinned, fonts); changed {
				p.Font = v
				s.deps.Change(p)
			}
		})
	})
	ui.Text(c, s.text.Reading).FontSize(theme.Px(t, 11)).FontWeight(500).TextColor(t.TextMuted).
		Margin(theme.Px(t, 28), 0, t.Space(2), 0).Role(ui.RoleHeading).Level(2)
	card(c, func() {
		labeled(c, s.text.ReadingFont, s.text.ReadingFontHelp, func() {
			// The pinned rows of the Reading font: Follow interface, Serif in its own face, and the family in use.
			fonts := s.deps.Fonts()
			serif := prefs.Preferences{ReadingFont: prefs.ReadingSerif}.ReadingFace(runtime.GOOS, fonts, "")
			pinned := []choice{{value: "", name: s.text.FollowInterface},
				{value: prefs.ReadingSerif, name: s.text.Serif, family: serif}}
			if p.ReadingFont != "" && p.ReadingFont != prefs.ReadingSerif &&
				(fonts == nil || slices.Contains(fonts, p.ReadingFont)) {
				pinned = append(pinned, choice{value: p.ReadingFont, name: p.ReadingFont, family: p.ReadingFont})
			}
			if v, changed := s.fontPick(c, &s.rdFont, s.text.ReadingFont, p.ReadingFont, pinned, fonts); changed {
				p.ReadingFont = v
				s.deps.Change(p)
			}
		})
	}, func() {
		labeled(c, s.text.TextSize, s.text.TextSizeHelp, func() {
			names := []string{s.text.SizeSmall, s.text.SizeMedium, s.text.SizeLarge, s.text.SizeXLarge}
			if i, changed := s.segments(c, s.text.TextSize, names, slices.Index(prefs.Sizes, p.ReadingSize)); changed {
				p.ReadingSize = prefs.Sizes[i]
				s.deps.Change(p)
			}
		})
	}, func() {
		labeled(c, s.text.LineSpacing, s.text.LineSpacingHelp, func() {
			names := []string{s.text.SpacingCompact, s.text.SpacingStandard, s.text.SpacingRelaxed}
			if i, changed := s.segments(c, s.text.LineSpacing, names, slices.Index(prefs.Spacings, p.LineSpacing)); changed {
				p.LineSpacing = prefs.Spacings[i]
				s.deps.Change(p)
			}
		})
	}, func() {
		labeled(c, s.text.ChineseTypesetting, s.text.ChineseTypesettingHelp, func() {
			on := p.ChineseTypesetting
			ui.Switch(c, &on).Label(s.text.ChineseTypesetting).Description(s.text.ChineseTypesettingHelp)
			if on != p.ChineseTypesetting {
				p.ChineseTypesetting = on
				s.deps.Change(p)
			}
		})
	}, func() {
		ui.Column(c).FillWidth().Padding(theme.Px(t, 12), theme.Px(t, 14)).Radius(8).Background(t.Background).Children(func() {
			size := 16 * float32(p.Scale())
			sample := s.sample(c, p.ChineseTypesetting && s.lang == prefs.LanguageChinese, size).FontSize(size).
				TextColor(theme.Prose(t)).LineHeight(float32(p.Leading(s.lang == prefs.LanguageChinese))).Wrap()
			if face := s.deps.ReadingFont(); face != "" {
				sample.Font(face)
			}
		})
	})
	ui.Text(c, s.text.Timeline).FontSize(theme.Px(t, 11)).FontWeight(500).TextColor(t.TextMuted).
		Margin(theme.Px(t, 28), 0, t.Space(2), 0).Role(ui.RoleHeading).Level(2)
	rows := []func(){func() {
		labeled(c, s.text.Density, s.text.DensityHelp, func() {
			names := []string{s.text.DensityComfortable, s.text.DensityCompact}
			if i, changed := s.segments(c, s.text.Density, names, slices.Index(prefs.Densities, p.Density)); changed {
				p.Density = prefs.Densities[i]
				s.deps.Change(p)
			}
		})
	}, func() {
		when, _ := p.MarkRead()
		names := map[string]string{prefs.MarkOnOpen: s.text.MarkReadOpen, prefs.MarkAfterStay: s.text.MarkReadDwell,
			prefs.MarkOnScroll: s.text.MarkReadScroll, prefs.MarkNever: s.text.MarkReadNever}
		helps := map[string]string{prefs.MarkOnOpen: s.text.MarkReadHelpOpen, prefs.MarkAfterStay: s.text.MarkReadHelpDwell,
			prefs.MarkOnScroll: s.text.MarkReadHelpScroll, prefs.MarkNever: s.text.MarkReadHelpNever}
		labeled(c, s.text.MarkRead, helps[when], func() {
			options := make([]choice, 0, len(prefs.MarkReadChoices))
			for _, w := range prefs.MarkReadChoices {
				options = append(options, choice{value: w, name: names[w]})
			}
			if v, changed := s.pick(c, when, s.text.MarkRead, options); changed {
				p.MarkReadWhen = v
				s.deps.Change(p)
			}
		})
	}}
	if when, _ := p.MarkRead(); when == prefs.MarkAfterStay {
		rows = append(rows, func() {
			_, stay := p.MarkRead()
			labeled(c, s.text.Dwell, s.text.DwellHelp, func() {
				names := []string{s.text.Seconds3, s.text.Seconds5, s.text.Seconds10}
				if i, changed := s.segments(c, s.text.Dwell, names, slices.Index(prefs.DwellSeconds, int(stay/time.Second))); changed {
					p.MarkReadDwellSec = prefs.DwellSeconds[i]
					s.deps.Change(p)
				}
			})
		})
	}
	rows = append(rows, func() {
		labeled(c, s.text.GroupSameLink, s.text.GroupSameLinkHelp, func() {
			on := s.settings.GroupSameLink
			ui.Switch(c, &on).Label(s.text.GroupSameLink).Description(s.text.GroupSameLinkHelp).
				Disabled(s.state != loaded || s.grouping)
			if on != s.settings.GroupSameLink {
				s.saveGrouping(on)
			}
		})
	})
	card(c, rows...)
	ui.Text(c, s.text.ViewAs).FontSize(theme.Px(t, 11)).FontWeight(500).TextColor(t.TextMuted).
		Margin(theme.Px(t, 28), 0, t.Space(2), 0).Role(ui.RoleHeading).Level(2)
	card(c, func() {
		labeled(c, s.text.ViewArticles, "", func() {
			if v, changed := s.viewer(c, s.text.ViewArticles, p.ArticleViewer, prefs.ArticleViewers); changed {
				p.ArticleViewer = v
				s.deps.Change(p)
			}
		})
	}, func() {
		labeled(c, s.text.ViewVideos, "", func() {
			if v, changed := s.viewer(c, s.text.ViewVideos, p.VideoViewer, prefs.VideoViewers); changed {
				p.VideoViewer = v
				s.deps.Change(p)
			}
		})
	}, func() {
		labeled(c, s.text.ViewSocial, "", func() {
			if v, changed := s.viewer(c, s.text.ViewSocial, p.SocialViewer, prefs.SocialViewers); changed {
				p.SocialViewer = v
				s.deps.Change(p)
			}
		})
	}, func() {
		labeled(c, s.text.ViewImages, "", func() {
			if v, changed := s.viewer(c, s.text.ViewImages, p.ImageViewer, prefs.ImageViewers); changed {
				p.ImageViewer = v
				s.deps.Change(p)
			}
		})
	}, func() {
		ui.Text(c, s.text.ViewAsHelp).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Wrap()
	})
}

// viewer is the segmented control of one kind's viewer; it returns the viewer chosen and whether the user just chose it.
func (s *Screen) viewer(c *ui.Context, label, current string, viewers []string) (string, bool) {
	names := make([]string, len(viewers))
	for i, v := range viewers {
		switch v {
		case prefs.ViewList:
			names[i] = s.text.ViewList
		case prefs.ViewCompact:
			names[i] = s.text.ViewCompact
		case prefs.ViewCover:
			names[i] = s.text.ViewCover
		case prefs.ViewGrid:
			names[i] = s.text.ViewGrid
		case prefs.ViewWall:
			names[i] = s.text.ViewWall
		case prefs.ViewCard:
			names[i] = s.text.ViewCard
		}
	}
	i, changed := s.segments(c, label, names, slices.Index(viewers, current))
	return viewers[i], changed
}

// sample is the sample paragraph of the Reading card, spaced by the rules of Chinese typesetting when chinese is set,
// which is when it is Chinese and the switch is on; size is the font size it will have.
func (s *Screen) sample(c *ui.Context, chinese bool, size float32) ui.Element {
	text := s.text.PreviewText
	if !chinese {
		return ui.Text(c, text)
	}
	if !s.preview.ok || s.preview.text != text {
		s.preview.text, s.preview.pieces, s.preview.ok = text, typeset.Text(text), true
	}
	if s.preview.pieces == nil {
		return ui.Text(c, text)
	}
	return ui.RichText(c).Children(func() {
		for n, piece := range s.preview.pieces {
			e := ui.Text(c, piece.Text).Key(n)
			if piece.Em != 0 {
				e.LetterSpacing(piece.Em * size)
			}
		}
	})
}

// segments is a row of exclusive choices by name; it returns the index chosen and whether the user just chose it.
func (s *Screen) segments(c *ui.Context, label string, names []string, current int) (int, bool) {
	t := c.Theme()
	chosen := max(current, 0)
	seg := ui.SegmentedBase(c, &chosen, len(names))
	seg.Track.Padding(3).Gap(2).Radius(10).Background(theme.Track(t)).Label(label).Children(func() {
		for i, name := range names {
			b := seg.Segment(i).Height(theme.Px(t, 26)).PaddingX(theme.Px(t, 12)).Radius(6).Label(name)
			color := theme.Text2(t)
			switch {
			case i == chosen:
				theme.Raised(t, b)
				color = t.Text
			case b.Hovered():
				color = t.Text
			}
			b.Children(func() {
				text := ui.Text(c, name).NoWrap().TextColor(color)
				if i == chosen {
					text.FontWeight(500)
				}
			})
		}
	})
	return chosen, seg.Track.Changed()
}

// choice is an option of pick: the value kept, its name, and the font family the name is drawn in, "" for the
// interface font.
type choice struct{ value, name, family string }

// pick is a drop-down of options, the first the default; it returns the value chosen and whether the user just chose
// it. A current value that is not among the options shows as the first.
func (s *Screen) pick(c *ui.Context, current, label string, options []choice) (string, bool) {
	t := c.Theme()
	shown := options[0]
	for _, o := range options {
		if o.value == current {
			shown = o
		}
	}
	value := shown.value
	sel := ui.SelectBase(c, &value)
	sel.Trigger.MinWidth(theme.Px(t, 160)).MaxWidth(theme.Px(t, 240)).Height(theme.Px(t, 32)).Gap(t.Space(2)).
		Padding(0, t.Space(2), 0, theme.Px(t, 10)).Radius(t.Radius).Label(label)
	trigger(c, sel.Trigger)
	sel.Trigger.Children(func() {
		name := ui.Text(c, shown.name).Grow(1).MinWidth(0).SingleLine()
		if shown.family != "" {
			name.Font(shown.family)
		}
		ui.Icon(c, icons.ChevronDown).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Margin(4, 0, 0, 0).Padding(4).MinWidth(theme.Px(t, 160))
		theme.Sheet(c, panel, theme.Popup)
		ui.Scroll(c).MaxHeight(theme.Px(t, 320)).Children(func() {
			for _, o := range options {
				item := sel.Item(o.value).FillWidth().Height(theme.Px(t, 28)).Gap(t.Space(2)).PaddingX(t.Space(2)).Radius(6)
				if item.Highlighted() {
					item.Background(theme.Chosen(t))
				}
				item.Children(func() {
					name := ui.Text(c, o.name).Grow(1).NoWrap()
					if o.family != "" {
						name.Font(o.family)
					}
					if o.value == shown.value {
						ui.Icon(c, icons.Check).FontSize(theme.IconSmall(t)).TextColor(t.Accent)
					}
				})
			}
		})
	})
	return value, sel.Trigger.Changed()
}

func (s *Screen) opmlPage(c *ui.Context) {
	t := c.Theme()
	lead(c, s.text.OPMLHelp)
	card(c, func() {
		zone := ui.Column(c).FillWidth().Radius(theme.Px(t, 10))
		over := zone.FileDragOver()
		if over {
			zone.Background(theme.Soft(t, t.Accent)).Border(1.5, t.Accent).BorderStyle(ui.BorderDashed).
				Padding(theme.Px(t, 12))
		}
		if paths := zone.DroppedFiles(); len(paths) > 0 {
			s.ImportFiles(paths)
		}
		zone.Children(func() {
			help := s.text.ImportHelp
			if over {
				help = s.text.DropHelp
			}
			labeled(c, s.text.ImportLabel, help, func() {
				b := theme.Button{Style: theme.Secondary, Disabled: s.busy, Icon: icons.Upload, Text: s.text.Import}.Draw(c)
				if b.Clicked() {
					s.chooseImport()
				}
			})
		})
		s.importProgress(c)
		if s.result.text != "" && !s.result.export && !s.result.backup {
			s.resultBox(c)
		}
	}, func() {
		labeled(c, s.text.ReaderLabel, s.text.ReaderHelp, func() {
			b := theme.Button{Style: theme.Secondary, Disabled: s.busy, Icon: icons.ArrowUpDown, Text: s.text.ReaderButton}.Draw(c)
			if b.Clicked() {
				s.openReader()
			}
		})
	}, func() {
		labeled(c, s.text.ExportLabel, s.text.ExportHelp, func() {
			b := theme.Button{Style: theme.Secondary, Disabled: s.busy, Icon: icons.Download, Text: s.text.Export}.Draw(c)
			if b.Clicked() {
				s.export()
			}
		})
		if s.result.text != "" && s.result.export {
			s.resultBox(c)
		}
	}, func() {
		labeled(c, s.text.BackupLabel, s.text.BackupHelp, func() {
			b := theme.Button{Style: theme.Secondary, Disabled: s.busy, Icon: icons.Database, Text: s.text.BackupButton}.Draw(c)
			if b.Clicked() {
				s.backUp()
			}
		})
	}, func() {
		labeled(c, s.text.RestoreLabel, s.text.RestoreHelp, func() {
			b := theme.Button{Style: theme.Secondary, Disabled: s.busy, Icon: icons.RotateCcw, Text: s.text.RestoreButton}.Draw(c)
			if b.Clicked() {
				s.chooseRestore()
			}
		})
		if s.working != "" {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, 6)).Margin(theme.Px(t, 6), 0, 0, 0).Role(ui.RoleStatus).
				Children(func() {
					spin := ui.Icon(c, icons.LoaderCircle).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
					spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
					ui.Text(c, s.working).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Shrink(1).Wrap()
				})
		}
		if s.result.text != "" && s.result.backup {
			s.resultBox(c)
		}
	}, func() { s.autoRows(c) })
}

// autoRemote reports that the folder of automatic backups is on another computer, which a dialog here cannot browse,
// so it is typed.
func (s *Screen) autoRemote() bool { return s.server != "" || s.deps.Folder == nil }

// saveAuto saves a choice about automatic backups. The server checks the folder, and a refusal is shown under the row
// while the typed values stay for the user to fix.
func (s *Screen) saveAuto(patch client.SettingsPatch) {
	if s.api == nil || s.autoSaving {
		return
	}
	s.autoSaving, s.autoStatus = true, status{}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		saved, err := api.SetSettings(context.Background(), patch)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			s.autoSaving = false
			if err != nil {
				s.autoStatus = status{text: s.autoError(code(err))}
				s.announce = s.autoStatus.text
				return
			}
			s.settings.AutoBackupEnabled, s.settings.AutoBackupFolder = saved.AutoBackupEnabled, saved.AutoBackupFolder
			s.settings.AutoBackupFrequency, s.settings.AutoBackupKeep = saved.AutoBackupFrequency, saved.AutoBackupKeep
			s.settings.AutoBackupLastAt, s.settings.AutoBackupLastFile = saved.AutoBackupLastAt, saved.AutoBackupLastFile
			s.settings.AutoBackupFailedAt, s.settings.AutoBackupError = saved.AutoBackupFailedAt, saved.AutoBackupError
			s.autoFolder, s.autoKeep = saved.AutoBackupFolder, strconv.Itoa(saved.AutoBackupKeep)
		}
	})
}

// autoError is why a choice about automatic backups was refused.
func (s *Screen) autoError(code string) string {
	switch code {
	case "backup_folder_missing":
		return s.fill(s.text.AutoBadFolder, "{reason}", s.text.AutoReasonMissing)
	case "backup_folder_not_directory":
		return s.fill(s.text.AutoBadFolder, "{reason}", s.text.AutoReasonNotDir)
	case "backup_folder_not_writable":
		return s.fill(s.text.AutoBadFolder, "{reason}", s.text.AutoReasonNoWrite)
	case "invalid_backup_folder":
		return s.text.AutoNeedFolder
	case "invalid_backup_keep":
		return s.text.AutoInvalidKeep
	}
	return s.text.AutoSaveFailed
}

// autoReason is why the last automatic backup failed.
func (s *Screen) autoReason(code string) string {
	switch code {
	case "folder_missing":
		return s.text.AutoReasonMissing
	case "folder_not_directory":
		return s.text.AutoReasonNotDir
	case "folder_not_writable":
		return s.text.AutoReasonNoWrite
	case "disk_full":
		return s.text.AutoReasonDisk
	}
	return s.text.AutoReasonOther
}

// chooseFolder asks for the folder of automatic backups and saves it. The dialog blocks, so it runs in Go's work.
func (s *Screen) chooseFolder() {
	if s.api == nil || s.autoSaving || s.deps.Folder == nil {
		return
	}
	s.autoSaving, s.autoStatus = true, status{}
	gen := s.gen
	s.deps.Go(func() func() {
		path, err := s.deps.Folder()
		return func() {
			if gen != s.gen {
				return
			}
			s.autoSaving = false
			if err != nil || path == "" {
				return
			}
			s.saveAuto(client.SettingsPatch{AutoBackupFolder: &path})
		}
	})
}

// commitAuto saves the folder and the number to keep as typed, whichever differs from what is saved. A number that is
// not one in range is refused here.
func (s *Screen) commitAuto() {
	var patch client.SettingsPatch
	if folder := strings.TrimSpace(s.autoFolder); folder != s.settings.AutoBackupFolder {
		patch.AutoBackupFolder = &folder
	}
	keep, err := strconv.Atoi(strings.TrimSpace(s.autoKeep))
	switch {
	case err != nil || keep < 1 || keep > 100:
		s.autoStatus = status{text: s.text.AutoInvalidKeep}
		s.announce = s.autoStatus.text
		return
	case keep != s.settings.AutoBackupKeep:
		patch.AutoBackupKeep = &keep
	}
	if patch.AutoBackupFolder != nil || patch.AutoBackupKeep != nil {
		s.saveAuto(patch)
	}
}

// autoRows is the automatic backup row: the switch, the folder, how often, how many to keep and how the last one went.
func (s *Screen) autoRows(c *ui.Context) {
	t := c.Theme()
	st := s.settings
	labeled(c, s.text.AutoLabel, s.text.AutoHelp, func() {
		on := st.AutoBackupEnabled
		ui.Switch(c, &on).Label(s.text.AutoLabel).Description(s.text.AutoHelp).Disabled(s.autoSaving)
		if on != st.AutoBackupEnabled {
			patch := client.SettingsPatch{AutoBackupEnabled: &on}
			if folder := strings.TrimSpace(s.autoFolder); on && s.autoRemote() && folder != st.AutoBackupFolder {
				patch.AutoBackupFolder = &folder
			}
			s.saveAuto(patch)
		}
	})
	line := func(name string, control func()) {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, 12)).Margin(theme.Px(t, 10), 0, 0, 0).MinHeight(theme.Px(t, 32)).
			Children(func() {
				ui.Text(c, name).Width(theme.Px(t, 96)).Shrink(0)
				control()
			})
	}
	line(s.text.AutoFolder, func() {
		switch {
		case s.autoRemote():
			s.watch(field(c, "autofolder", &s.autoFolder, 0, false, func(in ui.Element) {
				in.Font("monospace").Label(s.text.AutoFolder).Placeholder(s.text.AutoPlaceholder).Disabled(s.autoSaving)
				if in.Submitted() {
					s.commitAuto()
				}
			}))
		case st.AutoBackupFolder == "":
			ui.Text(c, s.text.AutoNoFolder).Grow(1).MinWidth(0).TextColor(t.TextMuted)
		default:
			ui.Text(c, st.AutoBackupFolder).Font("monospace").NoWrap().Grow(1).MinWidth(0)
		}
		if !s.autoRemote() {
			b := theme.Button{Small: true, Disabled: s.autoSaving, Icon: icons.Folder, Text: s.text.AutoChoose}.Draw(c)
			if b.Clicked() {
				s.chooseFolder()
			}
		}
	})
	if s.autoRemote() {
		ui.Text(c, s.text.AutoRemote).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Wrap().Margin(theme.Px(t, 6), 0, 0, theme.Px(t, 108))
	}
	line(s.text.AutoFrequency, func() {
		frequencies := []string{"daily", "weekly"}
		names := []string{s.text.AutoDaily, s.text.AutoWeekly}
		if i, changed := s.segments(c, s.text.AutoFrequency, names, slices.Index(frequencies, st.AutoBackupFrequency)); changed &&
			frequencies[i] != st.AutoBackupFrequency {
			s.saveAuto(client.SettingsPatch{AutoBackupFrequency: &frequencies[i]})
		}
	})
	line(s.text.AutoKeep, func() {
		s.watch(field(c, "autokeep", &s.autoKeep, theme.Px(t, 64), false, func(in ui.Element) {
			in.Font("monospace").Label(s.text.AutoKeep).Disabled(s.autoSaving)
			if in.Submitted() {
				s.commitAuto()
			}
		}))
		ui.Text(c, s.text.AutoCopies).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted)
	})
	now := s.deps.Now()
	switch {
	case s.autoStatus.text != "":
		statusLine(c, s.autoStatus)
	case st.AutoBackupFailedAt != 0:
		statusLine(c, status{warn: true, text: s.fill(s.text.AutoFailed, "{reason}", s.autoReason(st.AutoBackupError))})
	case st.AutoBackupLastAt != 0:
		statusLine(c, status{muted: true, text: s.fill(s.text.AutoLast, "{time}", s.ago(st.AutoBackupLastAt, now),
			"{file}", st.AutoBackupLastFile)})
	default:
		statusLine(c, status{muted: true, text: s.text.AutoNever})
	}
}

// importProgress is the status row of the import under way: which file, a bar and how many subscriptions are done.
func (s *Screen) importProgress(c *ui.Context) {
	r := s.run
	if r == nil {
		return
	}
	s.watchImport(c)
	t := c.Theme()
	p := r.job.Progress
	ui.Column(c).FillWidth().Margin(theme.Px(t, 6), 0, 0, 0).Gap(theme.Px(t, 6)).Role(ui.RoleStatus).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, 6)).Children(func() {
			spin := ui.Icon(c, icons.LoaderCircle).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
			spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
			ui.Text(c, s.importLabel(r)).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Shrink(1).Wrap()
		})
		if p.Total <= 0 {
			return
		}
		how := s.fill(s.text.Progress, "{done}", strconv.Itoa(p.Done), "{total}", strconv.Itoa(p.Total))
		ui.Progress(c, float64(p.Done)/float64(p.Total)).FillWidth().Height(theme.Px(t, 4)).Label(how)
		ui.Text(c, how).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted)
	})
}

func (s *Screen) resultBox(c *ui.Context) {
	t := c.Theme()
	r := s.result
	fill, icon, iconColor, color := theme.Ink(t, 0.045), icons.Check, t.Success, t.Text
	switch r.tone {
	case partly:
		fill, icon, iconColor = theme.Soft(t, t.Warning), icons.TriangleAlert, t.Warning
	case refused:
		fill, icon, iconColor = theme.Soft(t, t.Danger), icons.CircleAlert, t.Danger
	}
	ui.Column(c).FillWidth().Margin(t.Space(3), 0, 0, 0).Padding(theme.Px(t, 10), t.Space(3)).Radius(t.Radius).
		Background(fill).Role(ui.RoleStatus).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(t.Space(2)).Children(func() {
			ui.Icon(c, icon).FontSize(theme.IconSmall(t)).TextColor(iconColor).Margin(2, 0, 0, 0)
			ui.Text(c, r.text).Grow(1).Shrink(1).MinWidth(0).FontSize(theme.Px(t, 12)).TextColor(color).Wrap()
			if r.view {
				s.viewSources(c)
			}
			x := ui.ButtonBase(c).Label(s.text.Dismiss).Tooltip(s.text.Dismiss)
			xColor := t.TextMuted
			var xFill ui.Color
			if x.Hovered() {
				xFill = t.SurfaceHover
				xColor = t.Text
			}
			mx, my := theme.Hit{W: theme.Px(t, 20), H: theme.Px(t, 20), Radius: 6, Fill: xFill}.Apply(c, x, func() {
				ui.Icon(c, icons.X).FontSize(theme.IconSmall(t)).TextColor(xColor)
			})
			x.Margin(my, mx, my, mx)
			if x.Clicked() {
				s.result = result{}
			}
		})
		if len(r.fails) == 0 {
			return
		}
		ui.Column(c).FillWidth().Padding(t.Space(2), 0, 0, theme.Px(t, 22)).Label(s.text.NotAdded).Role(ui.RoleList).
			Children(func() {
				for i, f := range r.fails {
					s.failRow(c, i, f)
				}
			})
	})
}

// viewSources is the quiet button of an import result that goes to the timeline on all subscriptions.
func (s *Screen) viewSources(c *ui.Context) {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(s.text.ViewSources)
	color := t.Accent
	var fill ui.Color
	if b.Hovered() {
		fill = theme.Soft(t, t.Accent)
	}
	mx, my := theme.Hit{H: theme.Px(t, 22), Radius: 6, Fill: fill, PadX: theme.Px(t, 6)}.Apply(c, b, func() {
		ui.Text(c, s.text.ViewSources).FontSize(theme.Px(t, 12)).FontWeight(500).TextColor(color).NoWrap()
	})
	b.Margin(my, mx, my, mx)
	if b.Clicked() && s.deps.ViewSources != nil {
		s.deps.ViewSources()
	}
}

func (s *Screen) failRow(c *ui.Context, i int, f client.ImportFail) {
	t := c.Theme()
	why := map[string]string{
		"invalid_url": s.text.InvalidURL, "invalid_rsshub_path": s.text.InvalidRoute,
		"invalid_kind": s.text.InvalidKind, "invalid_source": s.text.InvalidSource,
		"not_opml": s.text.NotOpmlWhy, "too_large": s.text.TooLargeWhy, "unreadable": s.text.ReadWhy,
		"import_failed": s.text.FailedWhy, "busy": s.text.BusyWhy, "skipped": s.text.SkippedWhy,
	}[f.Error]
	if why == "" {
		why = f.Error
	}
	ui.Row(c).Key(i).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Padding(theme.Px(t, 3), 0).Role(ui.RoleListItem).
		Children(func() {
			spans := []ui.Span{}
			if title := strings.TrimSpace(f.Title); title != "" {
				if f.Address != "" {
					title += " · "
				}
				spans = append(spans, ui.Span{Text: title})
			}
			if f.Address != "" {
				spans = append(spans, ui.Span{Text: f.Address, Font: "monospace", Size: theme.Px(t, 12)})
			}
			ui.RichText(c, spans...).Grow(1).MinWidth(0).FontSize(theme.Px(t, 12)).SingleLine()
			ui.Text(c, why).FontSize(theme.Px(t, 12)).TextColor(theme.Text2(t)).NoWrap()
		})
}

// devicesPage is the signed-in devices, as a table where there is room and as stacked rows where there is not.
func (s *Screen) devicesPage(c *ui.Context, table bool) {
	t := c.Theme()
	if !s.signedIn {
		card(c, func() {
			ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(theme.Px(t, 10)).Children(func() {
				ui.Icon(c, icons.Laptop).FontSize(theme.IconMedium(t)).TextColor(t.TextMuted).Margin(1, 0, 0, 0)
				ui.Text(c, s.text.LocalDevices).TextColor(t.TextMuted).Shrink(1).Wrap()
			})
		})
		return
	}
	lead(c, s.text.DevicesHelp)
	now := s.deps.Now()
	if table {
		s.deviceTable(c, now)
		return
	}
	rows := make([]func(), len(s.devices))
	for i, d := range s.devices {
		rows[i] = func() { s.deviceRow(c, d, now) }
	}
	card(c, rows...)
}

// deviceTable draws the devices in columns: the device, its platform, when it signed in, when it was last used
// and Revoke. Every row is a grid of the same tracks, so the columns line up.
func (s *Screen) deviceTable(c *ui.Context, now time.Time) {
	t := c.Theme()
	tracks := func(g ui.Element) ui.Element {
		return g.FillWidth().ColumnTracks(ui.Fr(1), ui.Fixed(theme.Px(t, 88)), ui.Fixed(theme.Px(t, 96)), ui.Fixed(theme.Px(t, 116)),
			ui.Fixed(theme.Px(t, 96))).GapX(theme.Px(t, 16)).AlignItems(ui.Center).Padding(0, theme.Px(t, 20))
	}
	ui.Column(c).FillWidth().Radius(12).Background(t.Surface).Role(ui.RoleTable).Label(s.text.Devices).Children(func() {
		tracks(ui.Grid(c)).Height(theme.Px(t, 36)).Role(ui.RoleRow).Children(func() {
			head := func(text string, align ui.Align) {
				ui.Text(c, text).FontSize(theme.Px(t, 12)).FontWeight(500).TextColor(t.TextMuted).SingleLine().
					TextAlign(align).Role(ui.RoleColumnHeader)
			}
			head(s.text.ColDevice, ui.Start)
			head(s.text.ColPlatform, ui.Start)
			head(s.text.ColSigned, ui.Start)
			head(s.text.ColLastUsed, ui.End)
			ui.Box(c).Label(s.text.Revoke)
		})
		for _, d := range s.devices {
			ui.Column(c.Key(d.ID)).FillWidth().Children(func() {
				ui.Box(c).FillWidth().Height(1).Margin(0, theme.Px(t, 20)).Background(t.Border)
				tracks(ui.Grid(c)).Height(theme.Px(t, 48)).Role(ui.RoleRow).Children(func() { s.deviceCells(c, d, now) })
			})
		}
	})
}

// deviceCells are the five cells of a device's row in the table.
func (s *Screen) deviceCells(c *ui.Context, d client.Device, now time.Time) {
	t := c.Theme()
	name, platform, icon := s.deviceParts(d)
	muted := func(text string) ui.Element {
		return ui.Text(c, text).FontSize(theme.Px(t, 13)).TextColor(t.TextMuted).SingleLine()
	}
	ui.Row(c).MinWidth(0).AlignItems(ui.Center).Gap(theme.Px(t, 12)).Children(func() {
		ui.Box(c).Size(theme.Px(t, 28), theme.Px(t, 28)).Radius(8).Center().Background(theme.Track(t)).Children(func() {
			ui.Icon(c, icon).FontSize(theme.IconSmall(t)).TextColor(theme.Text2(t))
		})
		ui.Text(c, name).FontWeight(500).SingleLine().MinWidth(0).Shrink(1)
		if d.Current {
			ui.Box(c).Height(theme.Px(t, 18)).PaddingX(theme.Px(t, 6)).Radius(theme.Pill(theme.Px(t, 18))).Center().
				Background(theme.Soft(t, t.Accent)).Children(func() {
				ui.Text(c, s.text.ThisDevice).FontSize(theme.Px(t, 11)).FontWeight(500).TextColor(t.Accent).NoWrap()
			})
		}
	})
	muted(platform)
	muted(s.date(d.CreatedAt, now))
	muted(s.ago(d.LastSeenAt, now)).TextAlign(ui.End)
	ui.Row(c).JustifySelf(ui.End).Children(func() {
		if d.Current {
			return
		}
		full, _ := s.device(d)
		b := theme.Button{Style: theme.Danger, Small: true, Text: s.text.Revoke,
			Label: s.fill(s.text.RevokeTitle, "{name}", full)}.Draw(c)
		if b.Clicked() {
			s.doomed, s.revoking = d, true
		}
	})
}

func (s *Screen) deviceRow(c *ui.Context, d client.Device, now time.Time) {
	t := c.Theme()
	name, icon := s.device(d)
	when := s.fill(s.text.SignedIn, "{date}", s.date(d.CreatedAt, now), "{seen}", s.ago(d.LastSeenAt, now))
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
		ui.Box(c).Size(theme.Px(t, 32), theme.Px(t, 32)).Radius(8).Center().Background(theme.Track(t)).Children(func() {
			ui.Icon(c, icon).FontSize(theme.IconMedium(t)).TextColor(theme.Text2(t))
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, 6)).Children(func() {
				ui.Text(c, name).FontWeight(500).SingleLine().MinWidth(0).Shrink(1)
				if d.Current {
					ui.Box(c).Height(theme.Px(t, 18)).PaddingX(theme.Px(t, 6)).Radius(theme.Pill(theme.Px(t, 18))).Center().
						Background(theme.Soft(t, t.Accent)).Children(func() {
						ui.Text(c, s.text.ThisDevice).FontSize(theme.Px(t, 11)).FontWeight(500).TextColor(t.Accent).NoWrap()
					})
				}
			})
			ui.Text(c, when).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).SingleLine()
		})
		if !d.Current {
			b := theme.Button{Style: theme.Danger, Small: true, Text: s.text.Revoke,
				Label: s.fill(s.text.RevokeTitle, "{name}", name)}.Draw(c)
			if b.Clicked() {
				s.doomed, s.revoking = d, true
			}
		}
	})
}
