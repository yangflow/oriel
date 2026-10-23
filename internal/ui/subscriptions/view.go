// Package subscriptions adds, edits, pauses, refreshes and deletes subscriptions, in dialogs over the timeline; see
// design/screens/subscriptions.html. Everything goes through the HTTP API.
package subscriptions

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/theme"
)

//go:embed copy.json
var catalog []byte

type copy struct {
	Add, AddShort, AddContinue, Done, Adding, Cancel, Close, Save, Saving, Address, AddressPlaceholder, AddressHelp, ShowAs                                                        string
	Article, Video, Social, Image, Title, TitlePlaceholder, Every, Minutes, Hours, Days, FoundFeed, FoundRoute                                                                     string
	FoundYouTubeChannel, FoundYouTubeUser, FoundYouTubePlaylist, FoundBilibili, FoundX, FoundGitHubRepo                                                                            string
	FoundGitHubUser, FoundTelegram, FoundWeibo, ViaRSSHub, NeedsBase, RSSHubAddress, RSSHubPlaceholder, PublicNote, PublicNoInstance, UseMine, Blocked, BlockedPublic, FeedRemoved string
	BaseRequired, InvalidBase, InvalidAddress, Exists, ExistsUnknown, ShowIt, Unreachable, Failed, Added                                                                           string
	AddedSoFar, SearchingFeeds, FoundMany, NoneFound, SiteYouTube, SiteBilibili, SiteWeibo, SiteZhihu, SiteX                                                                       string
	SearchingRoutes, FoundRoutes, Preview, PreviewOf, PreviewEmpty, PreviewNotFound, NoCatalog, NoRoutes                                                                           string
	LabelVideos, LabelDynamic, LabelPosts, LabelActivities, LabelAnswers, LabelColumn, LabelTopic, LabelCollection, LabelQuestion                                                  string
	LabelChannel, LabelPlaylist                                                                                                                                                    string
	EditTitle, Refresh, Retry, Edit, Pause, Resume, Delete, ConfirmDelete, Checked, CheckedNow, NotChecked, NextIn                                                                 string
	NextSoon, FailedOnce, FailedTimes, Paused, PauseLabel, PauseHelp, Mute, MuteHelp, DeleteTitle, DeleteBody                                                                      string
	ServedBy, ServedByBackup, MuteHidden, MuteNone                                                                                                                                 string
	Keep, KeepFollow, KeepFollowing, KeepForever, KeepWeek, KeepMonth, KeepMonths3, KeepMonths6, KeepYear, KeepHelp                                                                string
	Extract, ExtractHelp, ExtractSelector, ExtractSelectorHelp, ExtractSelectorPlaceholder, ExtractSelectorInvalid                                                                 string
	ExtractReferer, ExtractRefererHelp, ExtractRefererPlaceholder, ExtractRefererInvalid                                                                                           string
	ExtractRewrites, ExtractRewritesHelp, ExtractRewritesPlaceholder, ExtractRewritesInvalid                                                                                       string
	Pace, PaceOff, PaceBreaking, PaceDaily, PaceWeekly, PaceHelp                                                                                                                   string
	Deleted, PausedNow, Resumed, Queued, ResumeFirst, Gone, ActionFailed, Separator                                                                                                string
	Undo, Dismiss, UndoFailed                                                                                                                                                      string
	AIGroup, AutoSummary, AutoTranslate, AutoHelp, PromptSummary, PromptTranslate, PromptPlaceholder, AIOff                                                                        string
	OpenSettings, MarkRead, MarkOlder1, MarkOlder7, MarkOlder30, Auto, AutoEvery, SuggestVideo, SuggestImage, UseKind                                                              string
	HealthTitle, HealthSub, HealthChecking, HealthError, HealthOk, HealthSilent, HealthDead, HealthFailing, HealthRare                                                             string
	HealthWhy, HealthNumbers, PauseSource, Unsubscribe, UnsubscribeSource                                                                                                          string
}

var translations = func() map[string]copy {
	var values map[string]copy
	if err := json.Unmarshal(catalog, &values); err != nil {
		panic(err)
	}
	return values
}()

// kinds are the kinds a subscription shows as, in the order of the picker.
var kinds = []string{"article", "video", "social", "image"}

// intervals are the fetch intervals offered, in seconds; 0 is Auto, the first and the default.
var intervals = []int64{0, 15 * 60, 30 * 60, 3600, 3 * 3600, 6 * 3600, 12 * 3600, 24 * 3600}

// keeps are the keep periods offered, in days; 0 follows the global setting, the first and the default, and -1 keeps
// the entries forever, the last.
var keeps = []int64{0, 7, 30, 90, 180, 365, -1}

// paces are the paces offered, in seconds: 0 is off, the first and the default, then 3 hours, 2 days and 7 days.
var paces = []int64{0, 3 * 3600, 2 * 24 * 3600, 7 * 24 * 3600}

// API is what the dialogs read and write on the server; client.Session is one.
type API interface {
	AddSource(ctx context.Context, src client.NewSource) (client.Source, error)
	UpdateSource(ctx context.Context, id int64, patch client.SourcePatch) (client.Source, error)
	DeleteSource(ctx context.Context, id int64) error
	RestoreSource(ctx context.Context, id int64) (client.Source, error)
	RefreshSource(ctx context.Context, id int64) error
	SourcesHealth(ctx context.Context) (client.Health, error)
	MutedCount(ctx context.Context, id int64, keywords string) (int, error)
	Settings(ctx context.Context) (client.Settings, error)
	SetSettings(ctx context.Context, patch client.SettingsPatch) (client.Settings, error)
	Discover(ctx context.Context, address string) ([]client.Candidate, error)
	FindRoutes(ctx context.Context, query string, refresh bool) (client.Routes, error)
	PreviewRoute(ctx context.Context, path string) (client.RoutePreview, error)
}

// Deps is what the dialogs do outside themselves. Go runs work away from the UI thread and then the function it
// returns on the UI thread; Now is the clock, time.Now by default. Sources are the subscriptions the timeline has,
// to name the one an address is already followed as. Added, Changed and Removed tell the timeline what the server
// took, Restored that a deleted subscription is back, Show chooses a subscription in it, OpenSettings opens the AI group of the settings, and Expired shows the
// connect screen because the server no longer takes the session. Icon returns a site icon once the timeline has
// decoded it, nil until then.
type Deps struct {
	Go           func(work func() (apply func()))
	Now          func() time.Time
	Sources      func() []client.Source
	Icon         func(address string) *ui.Bitmap
	Added        func(client.Source)
	Refreshed    func(client.Source) // src was queued to fetch now; the timeline waits for the result
	Changed      func(client.Source)
	Removed      func(id int64)
	Restored     func(client.Source)
	Show         func(id int64)
	MarkRead     func(src client.Source)
	MarkOlder    func(src client.Source, days int)
	OpenSettings func()
	// OpenRSSHub opens the settings on the group of the RSSHub instances.
	OpenRSSHub func()
	Expired    func()
}

// The settings of the add dialog, which say whether a route needs an RSSHub instance first.
const (
	settingsLoading = iota
	settingsLoaded
	settingsUnknown // they did not load; the server checks the subscription all the same
)

type field int

const (
	noField field = iota
	addressField
	baseField
	titleField
)

// The states of the lookup under the address field, which finds the feeds of a page address.
const (
	lookIdle      = iota // nothing to look up: no address, a platform link, a route or a feed address
	lookWaiting          // the address changed; the lookup starts after a pause in typing, or at Enter or Add
	lookSearching        // the server is looking
	lookDone             // found holds the result, which may be empty
	lookFailed           // the server could not look; the address is added as typed
)

// lookAfter is how long the address stays unchanged before the lookup starts.
const lookAfter = 600 * time.Millisecond

// lookup is the search for the feeds of address, the trimmed text of the address field it was made for.
type lookup struct {
	address string
	state   int
	typedAt time.Time // when the address last changed
	found   []client.Candidate
	chosen  int // the index in found that Add subscribes to
}

// adding is the add dialog.
type adding struct {
	open       bool
	form       ui.ScrollState
	address    string
	base       string // the RSSHub instance typed when the settings have none
	title      string
	kind       int // in kinds
	suggested  int // the kind the address suggests, which kind follows until another is picked
	interval   int64
	settings   int
	root       string // the RSSHub instances of the settings, one per line
	addressErr string
	exists     int64 // the subscription the address is already followed as, for "Show it"
	baseErr    string
	formErr    string
	busy       bool
	focus      field
	count      int    // the subscriptions added since the dialog opened, with "Add and continue"
	last       string // the name of the last of them
	look       lookup
	routes     finding
}

// editing is the edit dialog of src.
type editing struct {
	open     bool
	form     ui.ScrollState
	src      client.Source
	title    string
	kind     int
	interval int64
	keep     int64 // in keeps: days, 0 to follow the settings, -1 for forever
	global   int   // the keep period of the settings in days, 0 for forever
	pace     int64 // in paces: seconds an unread entry stays in All and Unread, 0 for off
	paused   bool
	mute     string
	extract  bool   // the Extraction group is open
	selector string // the content selector, the Referer and the link rewrites of the subscription
	referer  string
	rewrites string
	counted  string // the keywords the count was asked for
	hidden   int    // how many entries the keywords hide, once known
	known    bool
	auto     [2]bool   // summarize and translate new entries
	prompts  [2]string // the summary and translation prompts of the subscription, "" for the ones in the settings
	aiOff    bool      // the settings have no AI endpoint, so the switches do nothing yet
	primary  string    // the first RSSHub instance of the settings, "" until they are loaded
	formErr  string
	busy     bool
	focus    bool
}

// The states of the health dialog's list.
const (
	healthLoading = iota
	healthLoaded
	healthFailed
)

// reporting is the health dialog: the subscriptions the server flags as dead, failing or rarely read.
type reporting struct {
	open  bool
	form  ui.ScrollState
	state int
	days  int // the window the counts of each row cover
	items []client.SourceHealth
	err   string
}

// Screen keeps the dialogs and the work they started.
type Screen struct {
	deps     Deps
	text     copy
	api      API
	gen      int // counts servers used, so the results of the one before are dropped
	add      adding
	edit     editing
	report   reporting
	deleting bool
	muteSeq  atomic.Int64  // counts the questions about the mute keywords, so the work of an older one gives up
	muteWait time.Duration // the pause after typing before the count is asked for
	doomed   client.Source // the subscription being deleted
	announce string
	toast    string
	deleted  []*deletion // the toasts that offer to undo a delete
}

// New selects the best available translation, falling back to English.
func New(systemLocale string, deps Deps) *Screen {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Screen{deps: deps, text: translations[locale.Language(systemLocale)], muteWait: 300 * time.Millisecond}
}

// SetLanguage shows the screen in the language of systemLocale, keeping what it shows.
func (s *Screen) SetLanguage(systemLocale string) {
	s.text = translations[locale.Language(systemLocale)]
}

// Use makes the dialogs work on api, and closes the ones of the server before.
func (s *Screen) Use(api API) {
	s.api = api
	s.gen++
	s.add, s.edit, s.report, s.deleting = adding{}, editing{}, reporting{}, false
}

// Open reports whether a dialog shows, which takes the keys from the timeline.
func (s *Screen) Open() bool { return s.add.open || s.edit.open || s.report.open || s.deleting }

// Name is what names a subscription: its title, which is empty until the first fetch, else the host of its address,
// else its route.
func Name(src client.Source) string {
	if title := strings.TrimSpace(src.Title); title != "" {
		return title
	}
	if u, err := url.Parse(src.URL); err == nil && u.Hostname() != "" {
		return strings.TrimPrefix(u.Hostname(), "www.")
	}
	if src.RSSHubPath != "" {
		return src.RSSHubPath
	}
	return src.URL
}

// FailureReason is why the last fetch of src failed, to put after "Last fetch failed:": the server's text, or for a
// fetch the site's bot protection turned away (kind "blocked") a sentence in the user's language, with blockedPublic
// for the public rsshub.app and blocked for any other site. A 410 Gone (kind "gone") gets the sentence gone, which
// suggests pausing the source.
func FailureReason(src client.Source, blocked, blockedPublic, gone string) string {
	switch src.LastErrorKind {
	case "gone":
		return gone
	case "blocked":
	default:
		return src.LastError
	}
	if u, err := url.Parse(src.URL); err == nil && strings.EqualFold(u.Hostname(), PublicHost) {
		return blockedPublic
	}
	return blocked
}

func (s *Screen) fill(text, name string) string { return strings.ReplaceAll(text, "{name}", name) }

func (s *Screen) count(text string, n int) string {
	return strings.ReplaceAll(text, "{n}", strconv.Itoa(n))
}

func (s *Screen) expired(err error) bool {
	if !errors.Is(err, client.ErrUnauthorized) {
		return false
	}
	s.gen++
	s.api = nil
	s.add, s.edit, s.report, s.deleting = adding{}, editing{}, reporting{}, false
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

// problem says why a request failed: the server is not reached, or it failed.
func (s *Screen) problem(err error) string {
	if errors.Is(err, client.ErrUnreachable) {
		return s.text.Unreachable
	}
	return s.text.Failed
}

// Add opens the add dialog and loads the settings, for the RSSHub instance.
func (s *Screen) Add() {
	if s.api == nil {
		return
	}
	s.add = adding{open: true, interval: 0, focus: addressField}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		settings, err := api.Settings(context.Background())
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			if err != nil {
				s.add.settings = settingsUnknown
				return
			}
			s.add.settings, s.add.root = settingsLoaded, strings.Join(settings.RSSHubBaseURLs, "\n")
		}
	})
}

// needsBase reports whether t is a route while the settings have no RSSHub instance.
func (s *Screen) needsBase(t target) bool {
	return t.Route != "" && s.add.settings == settingsLoaded && strings.TrimSpace(s.add.root) == ""
}

func (s *Screen) canAdd() bool {
	a := s.add
	return !a.busy && s.api != nil && strings.TrimSpace(a.address) != "" && a.addressErr == "" &&
		a.settings != settingsLoading && a.look.state != lookSearching && a.routes.state != lookSearching &&
		s.routeReady()
}

// lookable returns the address to look feeds up for, "" when the field holds none: a platform link, a route and an
// address of a feed file already say what they subscribe to.
func (s *Screen) lookable() string {
	t, ok := recognize(s.add.address, s.add.root)
	if !ok || t.Platform != feed || t.URL == "" || t.Public {
		return ""
	}
	if u, err := url.Parse(t.URL); err == nil {
		switch strings.ToLower(path.Ext(u.Path)) {
		case ".atom", ".rss", ".xml", ".json":
			return ""
		}
	}
	return t.URL
}

// watch keeps the lookup in step with the address field: a new address starts a new lookup, which begins once the
// address has stayed as it is for lookAfter. It returns how long to wait for that, 0 when there is nothing to wait for.
func (s *Screen) watch() time.Duration {
	wait, more := s.watchLook(), s.watchRoutes()
	if wait == 0 || more > 0 && more < wait {
		return more
	}
	return wait
}

func (s *Screen) watchLook() time.Duration {
	a := &s.add
	if !a.open || s.api == nil {
		return 0
	}
	now := s.deps.Now()
	if address := s.lookable(); a.look.address != address {
		a.look = lookup{address: address, typedAt: now}
		if address != "" {
			a.look.state = lookWaiting
		}
	}
	if a.look.state != lookWaiting {
		return 0
	}
	if wait := lookAfter - now.Sub(a.look.typedAt); wait > 0 {
		return wait
	}
	s.search()
	return 0
}

// search asks the server for the feeds of the address of the lookup, in the background.
func (s *Screen) search() {
	a := &s.add
	a.look.state = lookSearching
	address, gen, api := a.look.address, s.gen, s.api
	s.deps.Go(func() func() {
		found, err := api.Discover(context.Background(), address)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			l := &s.add.look
			if l.address != address || l.state != lookSearching {
				return // the address changed meanwhile
			}
			if err != nil {
				l.state = lookFailed
				return
			}
			l.state, l.found, l.chosen = lookDone, found, 0
		}
	})
}

// picked is the feed the lookup found that Add subscribes to.
func (s *Screen) picked() (client.Candidate, bool) {
	l := s.add.look
	if l.state != lookDone || l.chosen < 0 || l.chosen >= len(l.found) {
		return client.Candidate{}, false
	}
	return l.found[l.chosen], true
}

// submit adds the address. With more it keeps the dialog open and clears the address for the next one.
func (s *Screen) submit(more bool) {
	a := &s.add
	s.watch()
	if !s.canAdd() {
		return
	}
	if a.look.state == lookWaiting { // Add or Enter does not wait out the pause; the feeds found come first
		s.search()
		return
	}
	t, ok := recognize(a.address, a.root)
	if _, chosen := s.chosenRoute(); !ok && !chosen {
		a.addressErr, a.focus, s.announce = s.text.InvalidAddress, addressField, s.text.InvalidAddress
		return
	}
	if cand, ok := s.picked(); ok {
		t.URL, t.Route = cand.URL, cand.RSSHubPath
	}
	if _, ok := s.chosenRoute(); ok {
		t.URL, t.Route = "", s.routePath()
	}
	base := ""
	if s.needsBase(t) {
		if base = strings.TrimSpace(a.base); base == "" {
			a.baseErr, a.focus, s.announce = s.text.BaseRequired, baseField, s.text.BaseRequired
			return
		}
	}
	a.busy, a.baseErr, a.formErr = true, "", ""
	src := client.NewSource{URL: t.URL, RSSHubPath: t.Route, Kind: kinds[a.kind], Title: strings.TrimSpace(a.title),
		FetchIntervalSec: a.interval} // 0 is Auto
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		ctx := context.Background()
		if base != "" {
			settings, err := api.SetSettings(ctx, client.SettingsPatch{RSSHubBaseURLs: &[]string{base}})
			if err != nil {
				return func() {
					if gen != s.gen || s.expired(err) {
						return
					}
					s.add.busy = false
					if code(err) == "invalid_rsshub_base_url" {
						s.add.baseErr, s.add.focus = s.text.InvalidBase, baseField
					} else {
						s.add.formErr = s.problem(err)
					}
					s.announce = s.add.baseErr + s.add.formErr
				}
			}
			base = strings.Join(settings.RSSHubBaseURLs, "\n")
		}
		added, err := api.AddSource(ctx, src)
		return func() {
			if gen != s.gen {
				return
			}
			a := &s.add
			a.busy = false
			if base != "" {
				a.root = base
			}
			if s.expired(err) {
				return
			}
			switch {
			case err == nil:
				s.deps.Added(added)
				if !more {
					a.open = false
					s.toast = s.fill(s.text.Added, Name(added))
					return
				}
				a.count++
				a.last = Name(added)
				a.address, a.title, a.kind, a.suggested = "", "", 0, 0
				a.addressErr, a.exists, a.formErr, a.focus = "", 0, "", addressField
				s.announce = s.addedLine()
			case code(err) == "source_exists":
				a.addressErr, a.exists = s.text.ExistsUnknown, 0
				var apiErr *client.APIError
				errors.As(err, &apiErr)
				for _, known := range s.deps.Sources() {
					if (apiErr != nil && apiErr.SourceID != 0 && known.ID == apiErr.SourceID) || (known.URL == src.URL && known.RSSHubPath == src.RSSHubPath) {
						a.addressErr, a.exists = s.fill(s.text.Exists, Name(known)), known.ID
					}
				}
				a.focus, s.announce = addressField, a.addressErr
			case code(err) == "invalid_url" || code(err) == "invalid_rsshub_path" || code(err) == "invalid_source":
				a.addressErr, a.focus, s.announce = s.text.InvalidAddress, addressField, s.text.InvalidAddress
			default:
				a.formErr = s.problem(err)
				s.announce = a.formErr
			}
		}
	})
}

// addedLine says what the dialog has added so far, "" before the first.
func (s *Screen) addedLine() string {
	switch a := s.add; {
	case a.count == 0:
		return ""
	case a.count == 1:
		return s.fill(s.text.Added, a.last)
	default:
		return strings.NewReplacer("{name}", a.last, "{n}", strconv.Itoa(a.count)).Replace(s.text.AddedSoFar)
	}
}

// Edit opens the edit dialog of src.
func (s *Screen) Edit(src client.Source) {
	if s.api == nil {
		return
	}
	s.edit = editing{open: true, src: src, title: src.Title, kind: max(0, slices.Index(kinds, src.Kind)),
		interval: intervalChoice(src), keep: int64(src.KeepDays), pace: int64(src.PaceSec), paused: src.Paused, mute: src.MuteKeywords,
		extract:  src.ExtractSelector != "" || src.ExtractReferer != "" || src.ExtractRewrites != "",
		selector: src.ExtractSelector, referer: src.ExtractReferer, rewrites: src.ExtractRewrites,
		auto:    [2]bool{src.AutoSummary, src.AutoTranslate},
		prompts: [2]string{src.PromptSummary, src.PromptTranslate}, focus: true}
	gen, api, id := s.gen, s.api, src.ID
	s.deps.Go(func() func() {
		settings, err := api.Settings(context.Background())
		return func() {
			if gen != s.gen || s.expired(err) || err != nil || !s.edit.open || s.edit.src.ID != id {
				return
			}
			s.edit.aiOff = !settings.AIConfigured()
			s.edit.global = settings.RetentionDays
			if len(settings.RSSHubBaseURLs) > 0 {
				s.edit.primary = settings.RSSHubBaseURLs[0]
			}
		}
	})
}

func (s *Screen) save() {
	e := &s.edit
	if e.busy || s.api == nil {
		return
	}
	var patch client.SourcePatch
	changed := false
	if title := strings.TrimSpace(e.title); title != e.src.Title {
		patch.Title, changed = &title, true
	}
	if kind := kinds[e.kind]; kind != e.src.Kind {
		patch.Kind, changed = &kind, true
	}
	if interval := e.interval; interval != intervalChoice(e.src) {
		if interval == 0 {
			auto := true
			patch.FetchIntervalAuto = &auto
		} else {
			patch.FetchIntervalSec = &interval
		}
		changed = true
	}
	if keep := int(e.keep); keep != e.src.KeepDays {
		patch.KeepDays, changed = &keep, true
	}
	if pace := int(e.pace); pace != e.src.PaceSec {
		patch.PaceSec, changed = &pace, true
	}
	if paused := e.paused; paused != e.src.Paused {
		patch.Paused, changed = &paused, true
	}
	if mute := e.mute; mute != e.src.MuteKeywords {
		patch.MuteKeywords, changed = &mute, true
	}
	if selector := strings.TrimSpace(e.selector); selector != e.src.ExtractSelector {
		patch.ExtractSelector, changed = &selector, true
	}
	if referer := strings.TrimSpace(e.referer); referer != e.src.ExtractReferer {
		patch.ExtractReferer, changed = &referer, true
	}
	if rewrites := strings.TrimSpace(e.rewrites); rewrites != e.src.ExtractRewrites {
		patch.ExtractRewrites, changed = &rewrites, true
	}
	if auto := e.auto[0]; auto != e.src.AutoSummary {
		patch.AutoSummary, changed = &auto, true
	}
	if auto := e.auto[1]; auto != e.src.AutoTranslate {
		patch.AutoTranslate, changed = &auto, true
	}
	if prompt := strings.TrimSpace(e.prompts[0]); prompt != e.src.PromptSummary {
		patch.PromptSummary, changed = &prompt, true
	}
	if prompt := strings.TrimSpace(e.prompts[1]); prompt != e.src.PromptTranslate {
		patch.PromptTranslate, changed = &prompt, true
	}
	if !changed {
		e.open = false
		return
	}
	e.busy, e.formErr = true, ""
	gen, api, id := s.gen, s.api, e.src.ID
	s.deps.Go(func() func() {
		src, err := api.UpdateSource(context.Background(), id, patch)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			s.edit.busy = false
			switch {
			case err == nil:
				s.edit.open = false
				s.deps.Changed(src)
			case code(err) == "not_found":
				s.gone(id)
			default:
				s.edit.formErr = s.saveProblem(err)
				s.announce = s.edit.formErr
			}
		}
	})
}

// saveProblem says why saving a subscription failed: a rule of the Extraction group the server refuses, or the
// general reason.
func (s *Screen) saveProblem(err error) string {
	switch code(err) {
	case "invalid_extract_selector":
		return s.text.ExtractSelectorInvalid
	case "invalid_extract_referer":
		return s.text.ExtractRefererInvalid
	case "invalid_extract_rewrites":
		return s.text.ExtractRewritesInvalid
	}
	return s.problem(err)
}

// deletedToast is the ID of the toast that offers to undo deleting a subscription, followed by its ID.
const deletedToast = "subscriptions.deleted."

// undoToast is how long the toast offers Undo. The server takes the delete back for longer, see
// store.DeleteUndoWindow, so a click at the end of the toast still works.
const undoToast = 8 * time.Second

// deletion is a subscription just deleted, whose toast is on screen or about to be.
type deletion struct {
	src   client.Source
	shown bool
}

// gone drops a subscription the server no longer has.
func (s *Screen) gone(id int64) {
	if s.edit.src.ID == id {
		s.edit.open = false
	}
	s.dropHealth(id)
	s.toast = s.text.Gone
	s.deps.Removed(id)
}

// refreshText is what the refresh action of src reads: Try again while its last fetch failed.
func (s *Screen) refreshText(src client.Source) string {
	if src.LastError != "" {
		return s.text.Retry
	}
	return s.text.Refresh
}

// Refresh asks the server to fetch src now.
func (s *Screen) Refresh(src client.Source) {
	if s.api == nil {
		return
	}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		err := api.RefreshSource(context.Background(), src.ID)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			switch {
			case err == nil:
				s.toast = s.fill(s.text.Queued, Name(src))
				if s.deps.Refreshed != nil {
					s.deps.Refreshed(src)
				}
			case code(err) == "source_paused":
				s.toast = s.fill(s.text.ResumeFirst, Name(src))
			case code(err) == "not_found":
				s.gone(src.ID)
			default:
				s.toast = s.problem(err)
			}
		}
	})
}

// SetPaused pauses src, or resumes it.
func (s *Screen) SetPaused(src client.Source, paused bool) {
	if s.api == nil {
		return
	}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		changed, err := api.UpdateSource(context.Background(), src.ID, client.SourcePatch{Paused: &paused})
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			switch {
			case err == nil:
				s.deps.Changed(changed)
				if paused {
					s.dropHealth(src.ID) // a paused subscription is not in the list
				}
				s.toast = s.fill(s.text.Resumed, Name(changed))
				if paused {
					s.toast = s.fill(s.text.PausedNow, Name(changed))
				}
			case code(err) == "not_found":
				s.gone(src.ID)
			default:
				s.toast = s.text.ActionFailed
			}
		}
	})
}

// AskDelete asks whether to delete src, with its entries.
func (s *Screen) AskDelete(src client.Source) {
	if s.api == nil {
		return
	}
	s.edit.open = false
	s.doomed, s.deleting = src, true
}

func (s *Screen) remove() {
	if s.api == nil {
		return
	}
	gen, api, src := s.gen, s.api, s.doomed
	s.deps.Go(func() func() {
		err := api.DeleteSource(context.Background(), src.ID)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			switch {
			case err == nil:
				s.deleted = append(s.deleted, &deletion{src: src})
				s.dropHealth(src.ID)
				s.deps.Removed(src.ID)
			case code(err) == "not_found":
				s.gone(src.ID)
			default:
				s.toast = s.problem(err)
			}
		}
	})
}

// restore undoes the delete of d.src. The timeline gets the subscription back, with its entries.
func (s *Screen) restore(d *deletion) {
	if s.api == nil {
		return
	}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		src, err := api.RestoreSource(context.Background(), d.src.ID)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			switch {
			case err == nil:
				s.deps.Restored(src)
			case code(err) == "not_found":
				s.toast = s.text.UndoFailed
			default:
				s.toast = s.problem(err)
			}
		}
	})
}

// toasts offers Undo for each subscription just deleted. The toast is announced and takes no focus.
func (s *Screen) toasts(c *ui.Context) {
	for _, d := range s.deleted {
		if d.shown {
			continue
		}
		d.shown = true
		c.AddToast(ui.Toast{ID: deletedToast + strconv.FormatInt(d.src.ID, 10), Title: s.fill(s.text.Deleted, Name(d.src)),
			Timeout:  undoToast,
			Action:   s.text.Undo,
			OnAction: func() { s.restore(d) },
			OnClose: func() {
				s.deleted = slices.DeleteFunc(s.deleted, func(other *deletion) bool { return other == d })
			},
			Data: theme.ToastLook{Icon: icons.Trash2, Close: s.text.Dismiss}})
	}
}

// Menu fills the context menu of src in the sidebar.
func (s *Screen) Menu(m *ui.Menu, src client.Source) {
	if m.Item(s.refreshText(src)).Disabled(src.Paused).Chosen() {
		s.Refresh(src)
	}
	if m.Item(s.text.MarkRead).Disabled(src.UnreadCount == 0).Chosen() {
		s.deps.MarkRead(src)
	}
	for _, o := range []struct {
		label string
		days  int
	}{{s.text.MarkOlder1, 1}, {s.text.MarkOlder7, 7}, {s.text.MarkOlder30, 30}} {
		if m.Item(o.label).Disabled(src.UnreadCount == 0).Chosen() {
			s.deps.MarkOlder(src, o.days)
		}
	}
	if m.Item(s.text.Edit).Chosen() {
		s.Edit(src)
	}
	pause := s.text.Pause
	if src.Paused {
		pause = s.text.Resume
	}
	if m.Item(pause).Chosen() {
		s.SetPaused(src, !src.Paused)
	}
	m.Separator()
	if m.Item(s.text.Delete).Chosen() {
		s.AskDelete(src)
	}
}

// View draws the dialog that is open, over the timeline; it does no disk or network work, which runs through Deps.
func (s *Screen) View(c *ui.Context) {
	if s.add.open {
		s.addDialog(c)
	}
	if s.edit.open {
		s.editDialog(c)
	}
	if s.report.open && !s.deleting {
		s.healthDialog(c)
	}
	if s.deleting {
		name := Name(s.doomed)
		if ui.AlertDialog(c, &s.deleting, s.fill(s.text.DeleteTitle, name), s.text.DeleteBody, s.text.Cancel,
			s.text.ConfirmDelete) == 1 {
			s.remove()
		}
	}
	s.toasts(c)
	if s.announce != "" {
		c.Announce(s.announce)
		s.announce = ""
	}
	if s.toast != "" {
		c.Toast(s.toast)
		s.toast = ""
	}
}

// px converts a size of the mockup, drawn at a 13 px font, to the theme's text size.
func px(t *ui.Theme, v float32) float32 { return t.Rem(v / 13) }

func kindIcon(kind string) *ui.SVG {
	switch kind {
	case "video":
		return icons.Play
	case "social":
		return icons.MessageCircle
	case "image":
		return icons.Image
	}
	return icons.Newspaper
}

func (s *Screen) kindName(kind string) string {
	switch kind {
	case "video":
		return s.text.Video
	case "social":
		return s.text.Social
	case "image":
		return s.text.Image
	}
	return s.text.Article
}

// sheet gives a raised surface its rim and its shadow: a hairline in ink 12% in light, a 1 DIP white 8% line and a
// deeper shadow in dark; with high contrast the rim is Border.
func sheet(c *ui.Context, e ui.Element, offset, blur float32) {
	t := c.Theme()
	switch {
	case c.Preferences().HighContrast:
		e.Border(1, t.Border)
	case t.Dark:
		e.Border(1, ui.RGBA(255, 255, 255, 0.08))
	default:
		e.Border(0.5, theme.Ink(t, 0.12))
	}
	if t.Dark {
		e.Shadow(0, offset, blur, 0, ui.RGBA(0, 0, 0, 0.55))
	} else {
		e.Shadow(0, offset, blur, 0, ui.RGBA(28, 24, 18, 0.25))
	}
}

// dialog draws a dialog of the mockup: a header, a form that scrolls when the window is too low, and a footer. A line
// under the header shows once the form is scrolled, and one above the footer while more of it is below.
func (s *Screen) dialog(c *ui.Context, open *bool, scroll *ui.ScrollState, width float32, title string, header, form,
	footer func()) {
	t := c.Theme()
	_, height := c.Size()
	ui.DialogBase(c, open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(28, 24, 18, 0.32)).Padding(t.Space(4))
		panel.Column().Width(px(t, width)).MaxHeight(height - t.Space(8)).Radius(16).Background(theme.RaisedFill(t)).
			Label(title)
		sheet(c, panel, 20, 50)
		edge := func(shown bool) {
			color := t.Border
			if !shown {
				color = color.Alpha(0)
			}
			ui.Box(c).FillWidth().Height(1).Background(color)
		}
		header()
		edge(scroll.Y > 0)
		ui.Scroll(c).TrackScroll(scroll).FillWidth().Shrink(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Space(3.5)).Padding(t.Space(3.5), t.Space(5), t.Space(1), t.Space(5)).
				Children(form)
		})
		edge(scroll.Y < scroll.MaxY)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Padding(t.Space(4), t.Space(5), t.Space(4.5),
			t.Space(5)).Children(footer)
	})
}

func (s *Screen) header(c *ui.Context, open *bool, lead func(), title, sub string) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Padding(t.Space(4.5), t.Space(5), t.Space(1),
		t.Space(5)).Children(func() {
		lead()
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, title).FontSize(px(t, 15)).FontWeight(600).Role(ui.RoleHeading).Level(2)
			if sub != "" {
				ui.Text(c, sub).FontSize(px(t, 12)).TextColor(t.TextMuted).SingleLine()
			}
		})
		x := theme.Fade(ui.ButtonBase(c).Size(px(t, 28), px(t, 28)).Radius(8).AlignSelf(ui.Start).
			Label(s.text.Close).Tooltip(s.text.Close))
		color := t.TextMuted
		switch {
		case x.Pressed():
			x.Background(t.SurfacePressed)
			color = t.Text
		case x.Hovered():
			x.Background(t.SurfaceHover)
			color = t.Text
		}
		x.Children(func() { ui.Icon(c, icons.X).FontSize(px(t, 16)).TextColor(color) })
		if x.Clicked() {
			*open = false
		}
	})
}

func label(c *ui.Context, text string) {
	t := c.Theme()
	ui.Text(c, text).FontSize(px(t, 12)).FontWeight(500)
}

func help(c *ui.Context, text string) {
	t := c.Theme()
	ui.Text(c, text).FontSize(px(t, 12)).TextColor(t.TextMuted).Wrap()
}

// fieldEdge draws the hairline of a field, ink 12% (theme.LineStrong); with high contrast it is Border at 1 DIP.
func fieldEdge(c *ui.Context, e ui.Element) {
	t := c.Theme()
	if c.Preferences().HighContrast {
		e.Border(1, t.Border)
		return
	}
	e.Border(0.5, theme.LineStrong(t))
}

// box is the frame of a text field: its leading icon, if any, and the input built by input.
func box(c *ui.Context, key string, lead *ui.SVG, invalid bool, fill ui.Color, input func()) {
	t := c.Theme()
	b := ui.Row(c).Key(key).FillWidth().Height(px(t, 32)).AlignItems(ui.Center).Gap(t.Space(2)).
		PaddingX(t.Space(2.5)).Radius(t.Radius)
	fieldEdge(c, b)
	b.Background(fill)
	color := t.TextMuted
	switch focused := b.FocusWithin(); {
	case invalid:
		b.Border(1.5, t.Danger)
		color = t.Danger
		if focused {
			b.Shadow(0, 0, 0, 2, theme.Soft(t, t.Danger))
		}
	case focused:
		b.Border(0, ui.Color{}).Background(theme.RaisedFill(t)).Shadow(0, 0, 0, 2, t.Focus)
	}
	b.Children(func() {
		if lead != nil {
			ui.Icon(c, lead).FontSize(px(t, 16)).TextColor(color)
		}
		input()
	})
}

func errorLine(c *ui.Context, text string, link string) bool {
	t := c.Theme()
	clicked := false
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(t.Space(1.5)).Children(func() {
		ui.Icon(c, icons.CircleAlert).FontSize(px(t, 14)).TextColor(t.Danger).Margin(1, 0, 0, 0)
		ui.Text(c, text).FontSize(px(t, 12)).TextColor(t.Danger).Shrink(1).Wrap()
		if link != "" {
			clicked = ui.Link(c, link, "").FontSize(px(t, 12)).FontWeight(500).Clicked()
		}
	})
	return clicked
}

const (
	secondary = iota
	primary
	quiet // a destructive action that is not the default, in the danger color without a fill
	small // in the health line
)

func button(c *ui.Context, style int, disabled bool) ui.Element {
	t := c.Theme()
	b := theme.Fade(ui.ButtonBase(c).Height(px(t, 32)).Gap(t.Space(1.5)).PaddingX(px(t, 14)).Radius(t.Radius).
		Disabled(disabled))
	track := theme.Track(t)
	fill, color := track, t.Text
	switch style {
	case primary:
		fill, color = t.Accent, t.AccentText
	case quiet:
		fill, color = ui.Color{}, t.Danger
		b.PaddingX(t.Space(2))
	case small:
		b.Height(px(t, 26)).PaddingX(px(t, 10))
	}
	switch {
	case disabled && style == quiet:
		color = t.TextMuted
	case disabled:
		fill, color = t.Surface, t.TextMuted
	case b.Pressed() && style == primary:
		fill = t.AccentPressed
	case b.Hovered() && style == primary:
		fill = t.AccentHover
	case b.Pressed() && style == quiet:
		fill = theme.Soft(t, t.Danger).Mix(theme.Ink(t, 1), 0.05)
	case b.Hovered() && style == quiet:
		fill = theme.Soft(t, t.Danger)
	case b.Pressed():
		fill = track.Mix(theme.Ink(t, 1), 0.1)
	case b.Hovered():
		fill = track.Mix(theme.Ink(t, 1), 0.06)
	}
	if c.Preferences().HighContrast && style != quiet {
		b.Border(1, t.Border)
	}
	return b.Background(fill).TextColor(color)
}

// textButton fills a button with an optional icon and its text, a spinner for the icon while busy, in the color
// button gave it.
func textButton(c *ui.Context, b ui.Element, style int, icon *ui.SVG, text string, busy bool) {
	t := c.Theme()
	size := px(t, 14)
	b.Children(func() {
		if busy {
			spin := ui.Icon(c, icons.LoaderCircle).FontSize(size)
			spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
		} else if icon != nil {
			ui.Icon(c, icon).FontSize(size)
		}
		text := ui.Text(c, text).NoWrap()
		if style == small {
			text.FontSize(px(t, 12))
		}
		text.FontWeight(500)
	})
}

func tile(c *ui.Context, icon *ui.SVG) {
	t := c.Theme()
	ui.Box(c).Size(px(t, 32), px(t, 32)).Radius(8).Center().Background(theme.Soft(t, t.Accent)).Children(func() {
		ui.Icon(c, icon).FontSize(px(t, 18)).TextColor(t.Accent)
	})
}

// glyph is the site icon of src, in a tile with a rim, or its monogram while the icon is not decoded or when it has
// none.
func (s *Screen) glyph(c *ui.Context, kind string, src client.Source) {
	t := c.Theme()
	var icon *ui.Bitmap
	if src.IconURL != "" && s.deps.Icon != nil {
		icon = s.deps.Icon(src.IconURL)
	}
	if icon == nil {
		monogram(c, kind, Name(src))
		return
	}
	ui.Box(c).Size(px(t, 32), px(t, 32)).Radius(8).Clip().Children(func() {
		ui.Image(c, icon).Fit(ui.Cover).Fill()
		ui.Box(c).Absolute().Fill().Radius(8).Border(0.5, theme.Ink(t, 0.2))
	})
}

func monogram(c *ui.Context, kind, name string) {
	t := c.Theme()
	letter := "·"
	if r, _ := utf8.DecodeRuneInString(strings.TrimLeft(name, "@#·/ ")); r != utf8.RuneError {
		letter = strings.ToUpper(string(r))
	}
	ui.Box(c).Size(px(t, 32), px(t, 32)).Radius(8).Center().Background(theme.Track(t)).Children(func() {
		ui.Text(c, letter).FontSize(px(t, 15)).FontWeight(600).TextColor(theme.Text2(t))
	})
}

func (s *Screen) addDialog(c *ui.Context) {
	a := &s.add
	t, valid := recognize(a.address, a.root)
	if wait := s.watch(); wait > 0 {
		c.After(wait)
	}
	if cand, ok := s.picked(); ok {
		t.URL, t.Route = cand.URL, cand.RSSHubPath
		if cand.Kind != "" {
			t.Kind = cand.Kind
		}
		if cand.Origin == "rule" {
			t.Platform = route
		}
	}
	if suggested := max(0, slices.Index(kinds, t.Kind)); a.kind == a.suggested {
		a.kind, a.suggested = suggested, suggested
	}
	s.syncRoute()
	if a.open {
		c.OnShortcut(ui.Cmd, ui.KeyEnter, func() { s.submit(true) })
	}
	s.dialog(c, &a.open, &a.form, 440, s.text.Add, func() {
		s.header(c, &a.open, func() { tile(c, icons.Rss) }, s.text.Add, "")
	}, func() {
		if line := s.addedLine(); line != "" {
			s.addedCard(c, line)
		}
		s.addressField(c, t, valid)
		s.kindPicker(c, &a.kind)
		s.pair(c, func() {
			s.titleField(c, &a.title, a.busy, a.focus == titleField, func() { s.submit(false) })
		}, func() {
			s.intervalSelect(c, &a.interval, 0, a.busy)
		})
		if a.formErr != "" {
			errorLine(c, a.formErr, "")
		}
	}, func() {
		ui.Box(c).Grow(1)
		cancel := button(c, secondary, false)
		done := s.text.Cancel
		if a.count > 0 {
			done = s.text.Done
		}
		textButton(c, cancel, secondary, nil, done, false)
		if cancel.Clicked() {
			a.open = false
		}
		more := button(c, secondary, !s.canAdd() && !a.busy)
		textButton(c, more, secondary, nil, s.text.AddContinue, false)
		if more.Clicked() {
			s.submit(true)
		}
		add := button(c, primary, !s.canAdd() && !a.busy)
		text := s.text.AddShort
		if a.busy {
			text = s.text.Adding
		}
		textButton(c, add, primary, nil, text, a.busy)
		if add.Clicked() {
			s.submit(false)
		}
	})
}

func (s *Screen) addressField(c *ui.Context, t target, valid bool) {
	th := c.Theme()
	a := &s.add
	ui.Column(c).FillWidth().Gap(th.Space(1)).Children(func() {
		label(c, s.text.Address)
		before := a.address
		empty := strings.TrimSpace(a.address) == ""
		needsBase := valid && !empty && s.needsBase(t)
		what, where := s.describe(t)
		cand, hasCand := s.picked()
		if hasCand {
			what, where = s.describeCandidate(cand)
		}
		shown := s.routeView()
		public := !empty && valid && t.Public && !hasCand
		looking := !empty && valid && !public && (a.look.state == lookWaiting || a.look.state == lookSearching)
		box(c, "address", icons.Link, a.addressErr != "", th.Surface, func() {
			in := ui.TextInputBase(c, &a.address).Grow(1).MinWidth(0).FocusRing(false).Label(s.text.Address).
				Placeholder(s.text.AddressPlaceholder).Disabled(a.busy).
				OnTextKey(func(k ui.TextKey) bool { // the input takes Enter itself, so the shortcut comes here
					if k.Key == ui.KeyEnter && k.Mods == ui.Cmd {
						s.submit(true)
						return true
					}
					return false
				})
			switch {
			case a.addressErr != "":
				in.Error(a.addressErr)
			case shown != routeNone:
				in.Description(s.routeNote(shown))
			case empty || !valid:
				in.Description(s.text.AddressHelp)
			case needsBase:
				in.Description(s.text.NeedsBase)
			case public:
				in.Description(s.text.PublicNote)
			case looking:
				in.Description(s.text.SearchingFeeds)
			case a.look.state == lookDone && !hasCand:
				in.Description(s.text.NoneFound)
			default:
				in.Description(what + s.text.Separator + where)
			}
			if a.focus == addressField {
				in.Focus()
				a.focus = noField
			}
			if in.Submitted() {
				s.submit(false)
			}
		})
		if a.address != before {
			a.addressErr, a.exists, a.formErr = "", 0, ""
		}
		switch {
		case a.addressErr != "":
			show := ""
			if a.exists != 0 {
				show = s.text.ShowIt
			}
			if errorLine(c, a.addressErr, show) {
				a.open = false
				s.deps.Show(a.exists)
			}
		case shown == routeSearching:
			s.searching(c, s.text.SearchingRoutes)
		case shown == routeList:
			s.routeList(c)
		case shown != routeNone:
			help(c, s.routeNote(shown))
		case empty || !valid:
			help(c, s.text.AddressHelp)
		case needsBase:
			s.baseCard(c)
		case public:
			s.publicCard(c)
		case looking:
			s.searching(c, s.text.SearchingFeeds)
		case a.look.state == lookDone && !hasCand:
			help(c, s.text.NoneFound)
		case a.look.state == lookDone && len(a.look.found) > 1:
			s.pickList(c)
		default:
			s.found(c, t, what, where)
		}
	})
}

// describeCandidate says what a feed found for the address subscribes to, and where it is.
func (s *Screen) describeCandidate(cand client.Candidate) (what, where string) {
	where = cand.URL
	if cand.RSSHubPath != "" {
		where = s.text.ViaRSSHub + " " + cand.RSSHubPath
	}
	if cand.Site == "" {
		what = strings.TrimSpace(cand.Title)
		if u, err := url.Parse(cand.URL); what == "" && err == nil {
			what = strings.TrimPrefix(u.Hostname(), "www.")
		}
		return what, where
	}
	site := map[string]string{"youtube": s.text.SiteYouTube, "bilibili": s.text.SiteBilibili, "weibo": s.text.SiteWeibo,
		"zhihu": s.text.SiteZhihu, "x": s.text.SiteX}[cand.Site]
	label := map[string]string{"videos": s.text.LabelVideos, "dynamic": s.text.LabelDynamic, "posts": s.text.LabelPosts,
		"activities": s.text.LabelActivities, "answers": s.text.LabelAnswers, "column": s.text.LabelColumn,
		"topic": s.text.LabelTopic, "collection": s.text.LabelCollection, "question": s.text.LabelQuestion,
		"channel": s.text.LabelChannel, "playlist": s.text.LabelPlaylist}[cand.Label]
	return site + s.text.Separator + s.fill(label, cand.Subject), where
}

// searching is the line under the address field while the feeds of a page address are looked up.
func (s *Screen) searching(c *ui.Context, text string) {
	th := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(th.Space(1.5)).Role(ui.RoleStatus).Label(text).
		Children(func() {
			spin := ui.Icon(c, icons.LoaderCircle).FontSize(px(th, 12)).TextColor(th.TextMuted)
			spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
			help(c, text)
		})
}

// pickList lists the feeds found for the address, for the user to choose one.
func (s *Screen) pickList(c *ui.Context) {
	th := c.Theme()
	a := &s.add
	head := s.count(s.text.FoundMany, len(a.look.found))
	ui.Column(c).FillWidth().Gap(px(th, 6)).Children(func() {
		help(c, head)
		ui.RadioGroup(c, func() {
			for i, cand := range a.look.found {
				what, where := s.describeCandidate(cand)
				on := a.look.chosen == i
				row := ui.RadioBase(c, &a.look.chosen, i).FillWidth().AlignItems(ui.Center).Gap(px(th, 10)).
					Padding(th.Space(2), px(th, 10)).Radius(th.Radius).Label(what + s.text.Separator + where).Disabled(a.busy)
				if on {
					row.Background(theme.Soft(th, th.Accent))
				} else {
					row.Border(1, th.Border)
				}
				row.Children(func() {
					ui.Icon(c, s.candidateIcon(cand)).FontSize(px(th, 16)).TextColor(th.Accent)
					ui.Column(c).Grow(1).MinWidth(0).Children(func() {
						ui.Text(c, what).FontWeight(600).SingleLine()
						ui.Text(c, where).Font("monospace").FontSize(px(th, 12)).TextColor(th.TextMuted).SingleLine()
					})
					if on {
						ui.Icon(c, icons.Check).FontSize(px(th, 16)).TextColor(th.Success)
					}
				})
			}
		}).FillWidth().Gap(px(th, 6)).Label(head)
	})
}

func (s *Screen) candidateIcon(cand client.Candidate) *ui.SVG {
	if cand.Origin == "rule" {
		return kindIcon(cand.Kind)
	}
	return icons.Rss
}

// describe says what t subscribes to, and the address or route it is.
func (s *Screen) describe(t target) (what, where string) {
	text := map[platform]string{
		feed: s.text.FoundFeed, route: s.text.FoundRoute, youtubeChannel: s.text.FoundYouTubeChannel,
		youtubeUser: s.text.FoundYouTubeUser, youtubePlaylist: s.text.FoundYouTubePlaylist,
		bilibili: s.text.FoundBilibili, xUser: s.text.FoundX, githubRepo: s.text.FoundGitHubRepo,
		githubUser: s.text.FoundGitHubUser, telegram: s.text.FoundTelegram, weibo: s.text.FoundWeibo,
	}[t.Platform]
	what = s.fill(text, t.Name)
	switch {
	case t.URL != "":
		where = t.URL
	case t.Platform == route:
		where = t.Route
	default:
		where = s.text.ViaRSSHub + " " + t.Route
	}
	return what, where
}

func (s *Screen) found(c *ui.Context, t target, what, where string) {
	th := c.Theme()
	icon := icons.Rss
	if t.Platform != feed {
		icon = kindIcon(kinds[s.add.kind])
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(px(th, 10)).Padding(th.Space(2), px(th, 10)).Radius(th.Radius).
		Background(theme.Soft(th, th.Accent)).Role(ui.RoleStatus).Label(what + s.text.Separator + where).
		Children(func() {
			ui.Icon(c, icon).FontSize(px(th, 16)).TextColor(th.Accent)
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, what).FontWeight(600).SingleLine()
				ui.Text(c, where).Font("monospace").FontSize(px(th, 12)).TextColor(th.TextMuted).SingleLine()
			})
			ui.Icon(c, icons.Check).FontSize(px(th, 16)).TextColor(th.Success)
		})
}

// addedCard says what "Add and continue" has added so far, above the address for the next one.
func (s *Screen) addedCard(c *ui.Context, line string) {
	th := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(px(th, 10)).Padding(th.Space(2), px(th, 10)).Radius(th.Radius).
		Background(theme.Soft(th, th.Success)).Role(ui.RoleStatus).Label(line).Children(func() {
		ui.Icon(c, icons.Check).FontSize(px(th, 16)).TextColor(th.Success)
		ui.Text(c, line).Grow(1).MinWidth(0).FontWeight(500).SingleLine()
	})
}

// publicCard warns that the public RSSHub instance blocks Oriel. With an instance of the user's own set it offers to
// swap the address for its route, only when pressed; without one it leads to the settings to add it.
func (s *Screen) publicCard(c *ui.Context) {
	t := c.Theme()
	a := &s.add
	own := strings.TrimSpace(a.root) != ""
	text := s.text.PublicNoInstance
	if own {
		text = s.text.PublicNote
	}
	ui.Column(c).FillWidth().Gap(t.Space(2)).Padding(px(t, 10)).Radius(t.Radius).
		Background(theme.Soft(t, t.Warning)).Role(ui.RoleStatus).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(t.Space(1.5)).Children(func() {
			ui.Icon(c, icons.TriangleAlert).FontSize(px(t, 14)).TextColor(t.Warning).Margin(2, 0, 0, 0)
			ui.Text(c, text).FontWeight(500).TextColor(t.Warning).Shrink(1).Wrap()
		})
		use := button(c, small, false)
		if own {
			textButton(c, use, small, nil, s.text.UseMine, false)
			if use.Clicked() {
				a.address = publicRoute(a.address)
				a.focus = addressField
			}
			return
		}
		textButton(c, use, small, nil, s.text.OpenSettings, false)
		if use.Clicked() {
			a.open = false
			if s.deps.OpenRSSHub != nil {
				s.deps.OpenRSSHub()
			}
		}
	})
}

// baseCard asks for the RSSHub instance a route needs, which the settings do not have yet.
func (s *Screen) baseCard(c *ui.Context) {
	t := c.Theme()
	a := &s.add
	ui.Column(c).FillWidth().Gap(t.Space(2)).Padding(px(t, 10)).Radius(t.Radius).
		Background(theme.Soft(t, t.Warning)).Role(ui.RoleStatus).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(t.Space(1.5)).Children(func() {
			ui.Icon(c, icons.TriangleAlert).FontSize(px(t, 14)).TextColor(t.Warning).Margin(2, 0, 0, 0)
			ui.Text(c, s.text.NeedsBase).FontWeight(500).TextColor(t.Warning).Shrink(1).Wrap()
		})
		before := a.base
		box(c, "base", icons.Link, a.baseErr != "", theme.RaisedFill(t), func() {
			in := ui.TextInputBase(c, &a.base).Grow(1).MinWidth(0).FocusRing(false).Label(s.text.RSSHubAddress).
				Placeholder(s.text.RSSHubPlaceholder).Disabled(a.busy)
			if a.baseErr != "" {
				in.Error(a.baseErr)
			}
			if a.focus == baseField {
				in.Focus()
				a.focus = noField
			}
			if in.Submitted() {
				s.submit(false)
			}
		})
		if a.base != before {
			a.baseErr = ""
		}
		if a.baseErr != "" {
			errorLine(c, a.baseErr, "")
		}
	})
}

func (s *Screen) kindPicker(c *ui.Context, value *int) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
		label(c, s.text.ShowAs)
		seg := ui.SegmentedBase(c, value, len(kinds))
		seg.Track.FillWidth().Padding(3).Gap(2).Radius(9).Background(theme.Track(t)).Label(s.text.ShowAs).
			Children(func() {
				for i, kind := range kinds {
					name := s.kindName(kind)
					b := theme.Fade(seg.Segment(i).Grow(1).Basis(0).Height(px(t, 28)).Gap(t.Space(1.5)).Radius(7).
						Label(name))
					color, iconColor := t.TextMuted, t.TextMuted
					switch {
					case i == *value:
						if t.Dark {
							b.Background(theme.Ink(t, 0.12))
						} else {
							theme.Raised(t, b)
						}
						color, iconColor = t.Text, t.Accent
					case b.Hovered():
						color, iconColor = t.Text, t.Text
					}
					b.Children(func() {
						ui.Icon(c, kindIcon(kind)).FontSize(px(t, 14)).TextColor(iconColor)
						text := ui.Text(c, name).NoWrap().TextColor(color)
						if i == *value {
							text.FontWeight(500)
						}
					})
				}
			})
	})
}

// kindSuggestion is the line under the kind picker when the first fetch found the feed to look like another kind than
// the one chosen. The button only chooses the kind in the dialog; Save stores it.
func (s *Screen) kindSuggestion(c *ui.Context) {
	e := &s.edit
	i := slices.Index(kinds, e.src.SuggestedKind)
	if i < 0 || i == e.kind {
		return
	}
	text := s.text.SuggestVideo
	if e.src.SuggestedKind == "image" {
		text = s.text.SuggestImage
	}
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Padding(t.Space(2), t.Space(3)).Radius(8).
		Background(t.Surface).Label(text).Children(func() {
		ui.Icon(c, kindIcon(e.src.SuggestedKind)).FontSize(px(t, 14)).TextColor(t.TextMuted)
		ui.Text(c, text).FontSize(px(t, 12)).TextColor(t.TextMuted).Grow(1).MinWidth(0).Wrap()
		use := button(c, small, e.busy)
		textButton(c, use, small, nil, strings.ReplaceAll(s.text.UseKind, "{kind}", s.kindName(e.src.SuggestedKind)), false)
		if use.Clicked() {
			e.kind = i
		}
	})
}

func (s *Screen) pair(c *ui.Context, left, right func()) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(t.Space(3)).Children(func() {
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(t.Space(1)).Children(left)
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(t.Space(1)).Children(right)
	})
}

func (s *Screen) titleField(c *ui.Context, value *string, busy, focus bool, submit func()) {
	t := c.Theme()
	label(c, s.text.Title)
	box(c, "title", nil, false, t.Surface, func() {
		in := ui.TextInputBase(c, value).Grow(1).MinWidth(0).FocusRing(false).Label(s.text.Title).
			Placeholder(s.text.TitlePlaceholder).Disabled(busy)
		if focus {
			in.Focus()
		}
		if in.Submitted() {
			submit()
		}
	})
}

// intervalChoice is the choice in the interval select that src is on: 0 for Auto, else its fixed seconds.
func intervalChoice(src client.Source) int64 {
	if src.FetchIntervalAuto {
		return 0
	}
	return src.FetchIntervalSec
}

// every names a fetch interval in seconds, 0 being Auto.
func (s *Screen) every(sec int64) string {
	switch {
	case sec == 0:
		return s.text.Auto
	case sec%(24*3600) == 0:
		return s.count(s.text.Days, int(sec/(24*3600)))
	case sec%3600 == 0:
		return s.count(s.text.Hours, int(sec/3600))
	}
	return s.count(s.text.Minutes, int((sec+30)/60))
}

// span names a length of time, at least a minute, in its largest whole unit.
func (s *Screen) span(d time.Duration) string {
	switch {
	case d < time.Hour:
		return s.count(s.text.Minutes, max(1, int(d/time.Minute)))
	case d < 24*time.Hour:
		return s.count(s.text.Hours, int(d/time.Hour))
	}
	return s.count(s.text.Days, int(d/(24*time.Hour)))
}

// intervalSelect is the select of the check interval. Chosen as Auto it shows the interval Auto works out now when
// that is known (autoNow, seconds), as in "Auto · 6 h".
func (s *Screen) intervalSelect(c *ui.Context, value *int64, autoNow int64, busy bool) {
	options := intervals
	if !slices.Contains(options, *value) {
		options = append(slices.Clone(options), *value)
		slices.Sort(options)
	}
	shown := s.every(*value)
	if *value == 0 && autoNow > 0 {
		shown = strings.ReplaceAll(s.text.AutoEvery, "{every}", s.every(autoNow))
	}
	s.choiceSelect(c, value, options, s.text.Every, shown, s.every, busy)
}

// keepName names a keep period in days: 0 follows the settings and -1 is forever.
func (s *Screen) keepName(days int64) string {
	switch days {
	case 0:
		return s.text.KeepFollow
	case -1:
		return s.text.KeepForever
	case 7:
		return s.text.KeepWeek
	case 30:
		return s.text.KeepMonth
	case 90:
		return s.text.KeepMonths3
	case 180:
		return s.text.KeepMonths6
	case 365:
		return s.text.KeepYear
	}
	return s.count(s.text.Days, int(days))
}

// keepField is the keep period of the subscription's entries, with the line that says what it does. Chosen as
// following the settings it shows the period the settings have, as in "Follow settings · 3 months".
func (s *Screen) keepField(c *ui.Context) {
	t := c.Theme()
	e := &s.edit
	options := keeps
	if !slices.Contains(options, e.keep) {
		options = slices.Insert(slices.Clone(options), len(options)-1, e.keep)
		slices.SortFunc(options[1:len(options)-1], func(a, b int64) int { return int(a - b) })
	}
	shown := s.keepName(e.keep)
	if e.keep == 0 {
		global := s.text.KeepForever
		if e.global > 0 {
			global = s.keepName(int64(e.global))
		}
		shown = strings.ReplaceAll(s.text.KeepFollowing, "{keep}", global)
	}
	ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
		s.choiceSelect(c, &e.keep, options, s.text.Keep, shown, s.keepName, e.busy)
		help(c, s.text.KeepHelp)
	})
}

// paceName names a pace in seconds: 0 is off, the presets by name and any other number as hours or days.
func (s *Screen) paceName(sec int64) string {
	switch sec {
	case 0:
		return s.text.PaceOff
	case paces[1]:
		return s.text.PaceBreaking
	case paces[2]:
		return s.text.PaceDaily
	case paces[3]:
		return s.text.PaceWeekly
	}
	return s.every(sec)
}

// paceField is the pace of the subscription, with the line that says what it does.
func (s *Screen) paceField(c *ui.Context) {
	t := c.Theme()
	e := &s.edit
	options := paces
	if !slices.Contains(options, e.pace) {
		options = append(slices.Clone(options), e.pace)
		slices.Sort(options[1:])
	}
	ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
		s.choiceSelect(c, &e.pace, options, s.text.Pace, s.paceName(e.pace), s.paceName, e.busy)
		help(c, s.text.PaceHelp)
	})
}

// extractGroup is the Extraction disclosure: the content selector, the Referer and the link rewrites that steer the
// full text of the subscription. It is closed unless a rule is set, and an empty field leaves the automatic behavior.
func (s *Screen) extractGroup(c *ui.Context) {
	t := c.Theme()
	e := &s.edit
	ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
		head := ui.ButtonBase(c).Key("extract").FillWidth().Height(px(t, 28)).AlignItems(ui.Center).Gap(t.Space(1.5)).
			Radius(t.Radius).Label(s.text.Extract).Role(ui.RoleDisclosure).Expanded(e.extract)
		if head.Hovered() {
			head.Background(t.SurfaceHover)
		}
		theme.Fade(head)
		head.Children(func() {
			chevron := icons.ChevronRight
			if e.extract {
				chevron = icons.ChevronDown
			}
			ui.Icon(c, chevron).FontSize(px(t, 14)).TextColor(t.TextMuted)
			ui.Text(c, s.text.Extract).FontSize(px(t, 12)).FontWeight(500)
		})
		if head.Clicked() {
			e.extract = !e.extract
		}
		if !e.extract {
			help(c, s.text.ExtractHelp)
			return
		}
		s.extractInput(c, "selector", &e.selector, s.text.ExtractSelector, s.text.ExtractSelectorPlaceholder, s.text.ExtractSelectorHelp)
		s.extractInput(c, "referer", &e.referer, s.text.ExtractReferer, s.text.ExtractRefererPlaceholder, s.text.ExtractRefererHelp)
		ui.Column(c.Key("rewrites")).FillWidth().Gap(t.Space(1)).Children(func() {
			label(c, s.text.ExtractRewrites)
			area := ui.TextArea(c, &e.rewrites).FillWidth().Lines(3, 8).Font("monospace").FontSize(px(t, 12.5)).
				Label(s.text.ExtractRewrites).Description(s.text.ExtractRewritesHelp).
				Placeholder(s.text.ExtractRewritesPlaceholder).Disabled(e.busy)
			if area.Focused() {
				area.Background(theme.RaisedFill(t))
			}
			help(c, s.text.ExtractRewritesHelp)
		})
	})
}

// extractInput is one single-line field of the Extraction group, with its help line.
func (s *Screen) extractInput(c *ui.Context, key string, value *string, labelText, placeholder, helpText string) {
	t := c.Theme()
	ui.Column(c.Key(key)).FillWidth().Gap(t.Space(1)).Children(func() {
		label(c, labelText)
		box(c, key, nil, false, t.Surface, func() {
			ui.TextInputBase(c, value).Grow(1).MinWidth(0).FocusRing(false).Label(labelText).Description(helpText).
				Placeholder(placeholder).Disabled(s.edit.busy)
		})
		help(c, helpText)
	})
}

// choiceSelect is a select of whole numbers named by name, with the label above it; shown is what the closed select
// reads for the current value.
func (s *Screen) choiceSelect(c *ui.Context, value *int64, options []int64, labelText, shown string, name func(int64) string, busy bool) {
	t := c.Theme()
	label(c, labelText)
	sel := ui.SelectBase(c, value)
	sel.Trigger.FillWidth().Height(px(t, 32)).Gap(t.Space(2)).PaddingX(t.Space(2.5)).Radius(t.Radius).
		Background(t.Surface).Label(labelText).Disabled(busy)
	fieldEdge(c, sel.Trigger)
	if sel.Trigger.Focused() {
		sel.Trigger.Border(0, ui.Color{}).Background(theme.RaisedFill(t)).Shadow(0, 0, 0, 2, t.Focus)
	} else if sel.Trigger.Hovered() {
		sel.Trigger.Background(t.SurfaceHover)
	}
	sel.Trigger.Children(func() {
		ui.Text(c, shown).Grow(1).MinWidth(0).SingleLine()
		ui.Icon(c, icons.ChevronDown).FontSize(px(t, 14)).TextColor(t.TextMuted)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Margin(4, 0, 0, 0).Padding(4).MinWidth(px(t, 160)).Radius(10).Background(theme.RaisedFill(t))
		sheet(c, panel, 8, 24)
		for _, v := range options {
			item := sel.Item(v).FillWidth().Height(px(t, 28)).Gap(t.Space(2)).PaddingX(t.Space(2)).Radius(6)
			if item.Highlighted() {
				item.Background(theme.Chosen(t))
			}
			item.Children(func() {
				ui.Text(c, name(v)).Grow(1).NoWrap().TextColor(t.Text)
				if v == *value {
					ui.Icon(c, icons.Check).FontSize(px(t, 14)).TextColor(t.Accent)
				}
			})
		}
	})
}

func (s *Screen) editDialog(c *ui.Context) {
	e := &s.edit
	where := e.src.URL
	if where == "" {
		where = e.src.RSSHubPath
	}
	s.dialog(c, &e.open, &e.form, 480, s.text.EditTitle, func() {
		s.header(c, &e.open, func() { s.glyph(c, kinds[e.kind], e.src) }, s.text.EditTitle, where)
	}, func() {
		s.health(c)
		s.servedBy(c)
		focus := e.focus
		e.focus = false
		s.pair(c, func() {
			s.titleField(c, &e.title, e.busy, focus, s.save)
		}, func() {
			s.intervalSelect(c, &e.interval, e.src.FetchIntervalSec, e.busy)
		})
		s.kindPicker(c, &e.kind)
		s.kindSuggestion(c)
		s.keepField(c)
		s.paceField(c)
		s.extractGroup(c)
		s.switchRow(c, "pause", &e.paused, s.text.PauseLabel, s.text.PauseHelp)
		s.muteField(c)
		s.aiGroup(c)
		if e.formErr != "" {
			errorLine(c, e.formErr, "")
		}
	}, func() {
		remove := button(c, quiet, e.busy)
		textButton(c, remove, quiet, icons.Trash2, s.text.Delete, false)
		if remove.Clicked() {
			s.AskDelete(e.src)
		}
		ui.Box(c).Grow(1)
		cancel := button(c, secondary, false)
		textButton(c, cancel, secondary, nil, s.text.Cancel, false)
		if cancel.Clicked() {
			e.open = false
		}
		save := button(c, primary, e.busy)
		text := s.text.Save
		if e.busy {
			text = s.text.Saving
		}
		textButton(c, save, primary, nil, text, e.busy)
		if save.Clicked() {
			s.save()
		}
	})
}

// healthText says how the fetching of src goes, and whether it is failing.
func (s *Screen) healthText(src client.Source, now time.Time) (string, bool) {
	switch {
	case src.Paused:
		return s.text.Paused, false
	case src.LastError != "":
		why := FailureReason(src, s.text.Blocked, s.text.BlockedPublic, s.text.FeedRemoved)
		if src.ConsecutiveFailures > 1 {
			return strings.ReplaceAll(s.count(s.text.FailedTimes, src.ConsecutiveFailures), "{error}", why), true
		}
		return strings.ReplaceAll(s.text.FailedOnce, "{error}", why), true
	case src.LastFetchedAt == 0:
		return s.text.NotChecked, false
	}
	checked := s.text.CheckedNow
	if ago := now.Sub(time.UnixMilli(src.LastFetchedAt)); ago >= time.Minute {
		checked = strings.ReplaceAll(s.text.Checked, "{t}", s.span(ago))
	}
	next := s.text.NextSoon
	if in := time.UnixMilli(src.NextFetchAt).Sub(now); in >= time.Minute {
		next = strings.ReplaceAll(s.text.NextIn, "{t}", s.span(in))
	}
	return checked + s.text.Separator + next, false
}

// servedBy names the RSSHub instance that served the last fetch of a route, host only, and says so when it is not
// the first of the settings. It shows nothing for a feed or a route that has not been fetched.
func (s *Screen) servedBy(c *ui.Context) {
	src := s.edit.src
	if src.RSSHubPath == "" || src.RSSHubInstance == "" {
		return
	}
	host, text := src.RSSHubInstance, s.text.ServedBy
	if u, err := url.Parse(src.RSSHubInstance); err == nil && u.Host != "" {
		host = u.Host
	}
	if primary := strings.TrimRight(s.edit.primary, "/"); primary != "" && !strings.EqualFold(primary, src.RSSHubInstance) {
		text = s.text.ServedByBackup
	}
	help(c, strings.ReplaceAll(text, "{host}", host))
}

func (s *Screen) health(c *ui.Context) {
	t := c.Theme()
	src := s.edit.src
	text, bad := s.healthText(src, s.deps.Now())
	icon, color, fill := icons.Check, t.TextMuted, t.Surface
	switch {
	case src.Paused:
		icon = icons.Pause
	case bad:
		icon, color, fill = icons.CircleAlert, t.Warning, theme.Soft(t, t.Warning)
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Padding(t.Space(2), px(t, 10)).Radius(t.Radius).
		Background(fill).Children(func() {
		ui.Icon(c, icon).FontSize(px(t, 14)).TextColor(color)
		ui.Text(c, text).Grow(1).Shrink(1).MinWidth(0).FontSize(px(t, 12)).TextColor(color).Wrap().
			Role(ui.RoleStatus)
		if !src.Paused {
			refresh := button(c, small, false)
			textButton(c, refresh, small, icons.RefreshCw, s.refreshText(src), false)
			if refresh.Clicked() {
				s.Refresh(src)
			}
		}
	})
}

// switchRow is a switch with its label and help, which turn it too; help may be empty.
func (s *Screen) switchRow(c *ui.Context, key string, value *bool, text, helpText string) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(px(t, 10)).Children(func() {
		column := ui.Column(c).Key(key).Grow(1).MinWidth(0).Children(func() {
			label(c, text)
			if helpText != "" {
				help(c, helpText)
			}
		})
		if column.Clicked() && !s.edit.busy {
			*value = !*value
		}
		sw := ui.Switch(c, value).Label(text).Disabled(s.edit.busy)
		if helpText != "" {
			sw.Description(helpText)
		}
	})
}

// aiGroup is the subscription's summaries and translations: switches that queue new entries, and prompts that
// replace the ones in the settings; while AI is not set up it says so and leads to the settings.
func (s *Screen) aiGroup(c *ui.Context) {
	t := c.Theme()
	e := &s.edit
	ui.Box(c).FillWidth().Height(1).Margin(t.Space(1.5), 0, 0, 0).Background(t.Border)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(1.5)).Children(func() {
		ui.Icon(c, icons.Sparkles).FontSize(px(t, 14)).TextColor(t.TextMuted)
		ui.Text(c, s.text.AIGroup).FontSize(px(t, 12)).FontWeight(600).TextColor(t.TextMuted).
			Role(ui.RoleHeading).Level(3)
	})
	if e.aiOff {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Padding(t.Space(2), px(t, 10)).Radius(t.Radius).
			Background(t.Surface).Children(func() {
			ui.Icon(c, icons.CircleAlert).FontSize(px(t, 14)).TextColor(t.TextMuted)
			ui.Text(c, s.text.AIOff).Grow(1).Shrink(1).MinWidth(0).FontSize(px(t, 12)).TextColor(t.TextMuted).Wrap().
				Role(ui.RoleStatus)
			open := button(c, small, false)
			textButton(c, open, small, nil, s.text.OpenSettings, false)
			if open.Clicked() {
				e.open = false
				if s.deps.OpenSettings != nil {
					s.deps.OpenSettings()
				}
			}
		})
	}
	s.switchRow(c, "summary", &e.auto[0], s.text.AutoSummary, s.text.AutoHelp)
	s.switchRow(c, "translate", &e.auto[1], s.text.AutoTranslate, "")
	for i, text := range [2]string{s.text.PromptSummary, s.text.PromptTranslate} {
		ui.Column(c.Key(text)).FillWidth().Gap(t.Space(1)).Children(func() {
			label(c, text)
			area := ui.TextArea(c, &e.prompts[i]).FillWidth().Lines(3, 8).Font("monospace").FontSize(px(t, 12.5)).
				Label(text).Placeholder(s.text.PromptPlaceholder).Disabled(e.busy)
			if area.Focused() {
				area.Background(theme.RaisedFill(t))
			}
		})
	}
}

func (s *Screen) muteField(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
		label(c, s.text.Mute)
		area := ui.TextArea(c, &s.edit.mute).FillWidth().Lines(3, 6).Label(s.text.Mute).Description(s.text.MuteHelp).
			Disabled(s.edit.busy)
		if area.Focused() {
			area.Background(theme.RaisedFill(t))
		}
		help(c, s.text.MuteHelp)
		s.syncMuted()
		if e := s.edit; e.known {
			text := s.text.MuteNone
			if e.hidden > 0 {
				text = s.count(s.text.MuteHidden, e.hidden)
			}
			ui.Text(c, text).FontSize(px(t, 12)).FontFeatures("tnum").TextColor(t.TextMuted).Wrap().Role(ui.RoleStatus)
		}
	})
}

// syncMuted asks the server how many entries the keywords in the box hide, once the box has changed and stayed so
// for muteWait. The answer of an older question is dropped. Nothing is asked for an empty box: it hides nothing.
func (s *Screen) syncMuted() {
	e := &s.edit
	if e.mute == e.counted || s.api == nil {
		return
	}
	e.counted = e.mute
	seq := s.muteSeq.Add(1)
	if strings.TrimSpace(e.mute) == "" {
		e.known = false
		return
	}
	gen, api, id, keywords, wait := s.gen, s.api, e.src.ID, e.mute, s.muteWait
	s.deps.Go(func() func() {
		time.Sleep(wait)
		if s.muteSeq.Load() != seq {
			return func() {}
		}
		n, err := api.MutedCount(context.Background(), id, keywords)
		return func() {
			if gen != s.gen || s.expired(err) || !s.edit.open || s.edit.src.ID != id || s.muteSeq.Load() != seq {
				return
			}
			s.edit.hidden, s.edit.known = n, err == nil
		}
	})
}

// Health opens the health dialog and asks the server for the subscriptions it lists.
func (s *Screen) Health() {
	if s.api == nil {
		return
	}
	s.report = reporting{open: true}
	s.loadHealth()
}

func (s *Screen) loadHealth() {
	s.report.state, s.report.err = healthLoading, ""
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		health, err := api.SourcesHealth(context.Background())
		return func() {
			if gen != s.gen || s.expired(err) || !s.report.open {
				return
			}
			r := &s.report
			if err != nil {
				r.state, r.err = healthFailed, s.text.HealthError
				s.announce = r.err
				return
			}
			r.state, r.days, r.items = healthLoaded, health.WindowDays, health.Sources
			if len(r.items) == 0 {
				s.announce = s.text.HealthOk
			}
		}
	})
}

// dropHealth takes a subscription off the health list, which it leaves once paused or deleted.
func (s *Screen) dropHealth(id int64) {
	s.report.items = slices.DeleteFunc(s.report.items, func(h client.SourceHealth) bool { return h.ID == id })
}

// healthSource is the subscription a row of the health list stands for.
func healthSource(h client.SourceHealth) client.Source {
	return client.Source{ID: h.ID, URL: h.URL, RSSHubPath: h.RSSHubPath, Title: h.Title, IconURL: h.IconURL, Kind: h.Kind}
}

// healthReason says why h is listed, with the last error when the fetch is what fails.
func (s *Screen) healthReason(h client.SourceHealth) string {
	switch {
	case h.State == client.HealthRarelyRead:
		text := strings.ReplaceAll(s.count(s.text.HealthRare, h.Recent), "{days}", strconv.Itoa(s.report.days))
		return strings.ReplaceAll(text, "{read}", strconv.Itoa(h.RecentRead))
	case h.State == client.HealthDead && h.Cause == client.HealthCauseSilent:
		return s.count(s.text.HealthSilent, h.Days)
	}
	reason := s.count(s.text.HealthFailing, h.ConsecutiveFailures)
	if h.State == client.HealthDead {
		reason = s.count(s.text.HealthDead, h.Days)
	}
	if h.LastError == "" {
		return reason
	}
	return strings.ReplaceAll(strings.ReplaceAll(s.text.HealthWhy, "{reason}", reason), "{error}", h.LastError)
}

func (s *Screen) healthNumbers(h client.SourceHealth) string {
	text := strings.ReplaceAll(s.text.HealthNumbers, "{days}", strconv.Itoa(s.report.days))
	text = strings.ReplaceAll(text, "{recent}", strconv.Itoa(h.Recent))
	text = strings.ReplaceAll(text, "{read}", strconv.Itoa(h.RecentRead))
	return strings.ReplaceAll(text, "{starred}", strconv.Itoa(h.RecentStarred))
}

func (s *Screen) healthDialog(c *ui.Context) {
	r := &s.report
	s.dialog(c, &r.open, &r.form, 480, s.text.HealthTitle, func() {
		s.header(c, &r.open, func() { tile(c, icons.HeartPulse) }, s.text.HealthTitle, s.text.HealthSub)
	}, func() {
		t := c.Theme()
		switch {
		case r.state == healthLoading:
			ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Padding(t.Space(8), 0).Role(ui.RoleStatus).
				Label(s.text.HealthChecking).Children(func() {
				spin := ui.Icon(c, icons.LoaderCircle).FontSize(px(t, 20)).TextColor(t.TextMuted)
				spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
				ui.Text(c, s.text.HealthChecking).TextColor(t.TextMuted)
			})
		case r.state == healthFailed:
			ui.Column(c).FillWidth().Padding(t.Space(6), 0).Children(func() {
				if errorLine(c, r.err, s.text.Retry) {
					s.loadHealth()
				}
			})
		case len(r.items) == 0:
			ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Padding(t.Space(8), 0).Role(ui.RoleStatus).
				Label(s.text.HealthOk).Children(func() {
				ui.Icon(c, icons.CircleCheckBig).FontSize(px(t, 28)).TextColor(t.Success)
				ui.Text(c, s.text.HealthOk).FontWeight(500)
			})
		default:
			ui.Column(c).FillWidth().Children(func() {
				for i, h := range r.items {
					if i > 0 {
						ui.Box(c).FillWidth().Height(1).Background(t.Border)
					}
					s.healthRow(c, h)
				}
			})
		}
	}, func() {
		ui.Box(c).Grow(1)
		closeButton := button(c, secondary, false)
		textButton(c, closeButton, secondary, nil, s.text.Close, false)
		if closeButton.Clicked() {
			r.open = false
		}
	})
}

func (s *Screen) healthRow(c *ui.Context, h client.SourceHealth) {
	t := c.Theme()
	src := healthSource(h)
	name := Name(src)
	icon, color := icons.CircleAlert, t.Warning
	switch h.State {
	case client.HealthRarelyRead:
		icon, color = icons.Eye, t.TextMuted
	case client.HealthFailing:
		icon = icons.TriangleAlert
	}
	ui.Row(c).Key("health"+strconv.FormatInt(h.ID, 10)).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).
		Padding(t.Space(3), 0).Children(func() {
		s.smallGlyph(c, src)
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(t.Space(0.5)).Children(func() {
			ui.Text(c, name).FontWeight(500).SingleLine()
			ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(t.Space(1.5)).Children(func() {
				ui.Icon(c, icon).FontSize(px(t, 14)).TextColor(color).Margin(2, 0, 0, 0)
				ui.Text(c, s.healthReason(h)).FontSize(px(t, 12)).TextColor(color).Shrink(1).MinWidth(0).Wrap()
			})
			ui.Text(c, s.healthNumbers(h)).FontSize(px(t, 12)).TextColor(t.TextMuted).SingleLine()
		})
		ui.Row(c).Gap(t.Space(1)).Children(func() {
			pause := button(c, small, false).Label(s.fill(s.text.PauseSource, name))
			textButton(c, pause, small, icons.Pause, s.text.Pause, false)
			if pause.Clicked() {
				s.SetPaused(src, true)
			}
			remove := button(c, quiet, false).Height(px(t, 26)).Label(s.fill(s.text.UnsubscribeSource, name))
			textButton(c, remove, small, icons.Trash2, s.text.Unsubscribe, false)
			if remove.Clicked() {
				s.AskDelete(src)
			}
		})
	})
}

// smallGlyph is the site icon of src at 16 DIP, or its monogram while the icon is not decoded or when it has none.
func (s *Screen) smallGlyph(c *ui.Context, src client.Source) {
	t := c.Theme()
	var icon *ui.Bitmap
	if src.IconURL != "" && s.deps.Icon != nil {
		icon = s.deps.Icon(src.IconURL)
	}
	if icon != nil {
		ui.Box(c).Size(px(t, 16), px(t, 16)).Radius(4).Clip().Children(func() {
			ui.Image(c, icon).Fit(ui.Cover).Fill()
		})
		return
	}
	letter := "·"
	if r, _ := utf8.DecodeRuneInString(strings.TrimLeft(Name(src), "@#·/ ")); r != utf8.RuneError {
		letter = strings.ToUpper(string(r))
	}
	ui.Box(c).Size(px(t, 16), px(t, 16)).Radius(4).Center().Background(t.SurfacePressed).Children(func() {
		ui.Text(c, letter).FontSize(px(t, 10)).FontWeight(600)
	})
}
