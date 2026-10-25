// Package client talks to an Oriel server over its HTTP API, the only way the desktop UI reaches the data. It also
// keeps the signed-in session of a remote server in a file, never the password.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// DefaultPort is where `oriel serve` listens unless told otherwise; an address typed without a scheme or a port
// gets it.
const DefaultPort = 7470

// Errors a connection can end with. The UI maps each to its own message.
var (
	ErrUnreachable      = errors.New("server unreachable")
	ErrNotOriel         = errors.New("not an Oriel server")
	ErrUnwritable       = errors.New("server database unwritable")
	ErrPasswordRequired = errors.New("password required")
	ErrWrongPassword    = errors.New("wrong password")
	ErrUnauthorized     = errors.New("session expired")
	ErrTooManyAttempts  = errors.New("too many sign-in attempts")
)

// Server is an Oriel server found at an address.
type Server struct {
	URL  string // base URL, without a trailing slash
	Auth bool   // whether it asks for a password
}

// Session is what the desktop keeps of a connection: the server and the token of its session. The token is empty
// for a server without a password.
type Session struct {
	Server string `json:"server"`
	Token  string `json:"token,omitempty"`
}

var httpClient = &http.Client{Timeout: 15 * time.Second, Transport: agent{}}

// userAgent names the app to the server, which lists each signed-in device by it; SetVersion adds the version.
var userAgent = "Oriel (" + platform() + ")"

// appVersion is the version given to SetVersion, empty before that.
var appVersion string

// Version is the app's version as SetVersion was given it, empty when it was not.
func Version() string { return appVersion }

// SetVersion puts the app's version in the User-Agent of every request; call it before the first one.
func SetVersion(version string) {
	appVersion = version
	userAgent = "Oriel/" + version + " (" + platform() + ")"
}

func platform() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	}
	return runtime.GOOS
}

// agent sends every request with the app's User-Agent.
type agent struct{}

func (agent) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("User-Agent", userAgent)
	return http.DefaultTransport.RoundTrip(req)
}

// Address turns what the user typed into a base URL: "home.lan" becomes "http://home.lan:7470", while an address
// with a scheme keeps its default port.
func Address(input string) (string, error) {
	input = strings.TrimSpace(input)
	bare := !strings.Contains(input, "://")
	if bare {
		input = "http://" + input
	}
	u, err := url.Parse(input)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", fmt.Errorf("%w: invalid address %q", ErrUnreachable, input)
	}
	if bare && u.Port() == "" {
		u.Host = net.JoinHostPort(u.Hostname(), strconv.Itoa(DefaultPort))
	}
	u.Path, u.RawPath, u.RawQuery, u.Fragment, u.User = strings.TrimRight(u.Path, "/"), "", "", "", nil
	return u.String(), nil
}

// Probe checks that an Oriel server answers at the address and can open its data.
func Probe(ctx context.Context, address string) (Server, error) {
	base, err := Address(address)
	if err != nil {
		return Server{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	if err != nil {
		return Server{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return Server{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	var health struct {
		App      string `json:"app"`
		Auth     bool   `json:"auth"`
		Database string `json:"database"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&health); err != nil || health.App != "oriel" {
		return Server{}, ErrNotOriel
	}
	if health.Database != "ok" {
		return Server{}, ErrUnwritable
	}
	return Server{URL: base, Auth: health.Auth}, nil
}

// Login signs in to a server found by Probe. A server without a password needs none and gives no token.
func Login(ctx context.Context, server Server, password string) (Session, error) {
	if !server.Auth {
		return Session{Server: server.URL}, nil
	}
	if password == "" {
		return Session{}, ErrPasswordRequired
	}
	body, _ := json.Marshal(map[string]string{"password": password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/auth/login", strings.NewReader(string(body)))
	if err != nil {
		return Session{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return Session{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	var reply struct {
		Token string `json:"token"`
		Error string `json:"error"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&reply)
	switch {
	case resp.StatusCode == http.StatusOK && reply.Token != "":
		return Session{Server: server.URL, Token: reply.Token}, nil
	case reply.Error == "wrong_password":
		return Session{}, ErrWrongPassword
	case resp.StatusCode == http.StatusTooManyRequests:
		return Session{}, ErrTooManyAttempts
	case reply.Error == "auth_disabled":
		return Session{Server: server.URL}, nil
	}
	return Session{}, fmt.Errorf("login: status %d %s", resp.StatusCode, reply.Error)
}

// Check asks the server whether the session is still valid.
func Check(ctx context.Context, session Session) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, session.Server+"/api/settings", nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if session.Token != "" {
		req.Header.Set("Authorization", "Bearer "+session.Token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrUnauthorized
	case resp.StatusCode >= 300:
		return fmt.Errorf("check: status %d", resp.StatusCode)
	}
	return nil
}

// LoadSession reads the saved session; ok is false when there is none.
func LoadSession(path string) (session Session, ok bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	if err := json.Unmarshal(data, &session); err != nil || session.Server == "" {
		return Session{}, false, fmt.Errorf("read %s: invalid session", filepath.Base(path))
	}
	return session, true, nil
}

// SaveSession writes the session readable by the user only, replacing the file at once.
func SaveSession(path string, session Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".remote-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ForgetSession removes the saved session, if any.
func ForgetSession(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// APIError is an error the server answered with, by its stable code such as "invalid_cursor".
type APIError struct {
	Status int
	Code   string
	// SourceID is the subscription a "source_exists" answer names, 0 when it names none.
	SourceID int64
}

func (e *APIError) Error() string { return fmt.Sprintf("status %d %s", e.Status, e.Code) }

// Image is a picture of a social or image entry; Width and Height are 0 when the feed does not give them.
type Image struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Entry is an entry of the timeline. Times are Unix milliseconds, 0 when unknown.
type Entry struct {
	ID          int64  `json:"id"`
	SourceID    int64  `json:"sourceId"`
	SourceTitle string `json:"sourceTitle"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Author      string `json:"author"`
	PublishedAt int64  `json:"publishedAt"`
	Summary     string `json:"summary"`
	// AISummary is the start of the entry's stored AI summary, only in a list and only when one is ok; "" otherwise.
	AISummary    string `json:"aiSummary"`
	ThumbnailURL string `json:"thumbnailUrl"`
	DurationSec  int    `json:"durationSec"`
	Platform     string `json:"platform"`
	// Language is the BCP 47 code of the entry's language, "" when it is unknown.
	Language string `json:"language"`
	// ReadingSec is the server's estimate of the time the entry takes, in seconds: whole minutes from a minute on,
	// the duration for media. 0 when there is no estimate or it is under a second.
	ReadingSec int `json:"readingSec"`
	// ReadProgress is how far the article was read, from 0 to 1 of its scrollable height; nil when it was never scrolled.
	ReadProgress *float64 `json:"readProgress,omitempty"`
	Images       []Image  `json:"images"`
	// ExtractStatus is how getting the full text of an article went: "pending", "ok", "failed", or "skipped" for
	// the other kinds.
	ExtractStatus string `json:"extractStatus"`
	IsRead        bool   `json:"isRead"`
	Starred       bool   `json:"starred"`
	CreatedAt     int64  `json:"createdAt"`
	// RemindAt, in Unix milliseconds, is when a reminded entry comes back as unread; 0 when it has no reminder.
	RemindAt int64 `json:"remindAt,omitempty"`
	// FadesAt and ExpiresAt, in Unix milliseconds, are when an unread entry of a source with a pace starts to fade
	// and leaves All and Unread; 0 when it has none.
	FadesAt   int64 `json:"fadesAt,omitempty"`
	ExpiresAt int64 `json:"expiresAt,omitempty"`
	// Group lists the other copies of the article when several sources carry its link; nil when there are none.
	Group *Group `json:"group,omitempty"`
}

// Group is the other copies of an article that several sources carry; By is how they were found ("link").
type Group struct {
	By      string        `json:"by"`
	Entries []GroupMember `json:"entries"`
}

// GroupMember is one other copy of an article.
type GroupMember struct {
	ID          int64  `json:"id"`
	SourceID    int64  `json:"sourceId"`
	SourceTitle string `json:"sourceTitle"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	IsRead      bool   `json:"isRead"`
	Starred     bool   `json:"starred"`
	CreatedAt   int64  `json:"createdAt"`
}

// Article is an entry with its text: ContentHTML is what the feed carries and ExtractedHTML the text taken from
// the page, "" when there is none.
type Article struct {
	Entry
	ContentHTML   string `json:"contentHtml"`
	ExtractedHTML string `json:"extractedHtml"`
}

// Source is a subscription. Times are Unix milliseconds, 0 when it has not been fetched.
type Source struct {
	ID                  int64  `json:"id"`
	URL                 string `json:"url"`
	RSSHubPath          string `json:"rsshubPath"`
	RSSHubInstance      string `json:"rsshubInstance"`
	Title               string `json:"title"`
	IconURL             string `json:"iconUrl"`
	Kind                string `json:"kind"`
	FetchIntervalSec    int64  `json:"fetchIntervalSec"`
	FetchIntervalAuto   bool   `json:"fetchIntervalAuto"`
	SuggestedKind       string `json:"suggestedKind"`
	NextFetchAt         int64  `json:"nextFetchAt"`
	LastFetchedAt       int64  `json:"lastFetchedAt"`
	LastSuccessAt       int64  `json:"lastSuccessAt"`
	LastError           string `json:"lastError"`
	LastErrorKind       string `json:"lastErrorKind"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	MuteKeywords        string `json:"muteKeywords"`
	Paused              bool   `json:"paused"`
	KeepDays            int    `json:"keepDays"`
	PaceSec             int    `json:"paceSec"`
	HideFromAll         bool   `json:"hideFromAll"`
	ExtractSelector     string `json:"extractSelector"`
	ExtractReferer      string `json:"extractReferer"`
	OpenTarget          string `json:"openTarget"`
	FetchUserAgent      string `json:"fetchUserAgent"`
	FetchReferer        string `json:"fetchReferer"`
	ExtractRewrites     string `json:"extractRewrites"`
	UnreadCount         int    `json:"unreadCount"`
	PromptSummary       string `json:"promptSummary"`
	PromptTranslate     string `json:"promptTranslate"`
	AutoSummary         bool   `json:"autoSummary"`
	AutoTranslate       bool   `json:"autoTranslate"`
	// MovedTo is the address the site moved to for good, waiting for the user to accept or dismiss it; empty for none.
	MovedTo string `json:"movedTo"`
}

// NewSource is a subscription to add: exactly one of URL and RSSHubPath. An empty Title takes the feed's, and a zero
// FetchIntervalSec the automatic interval; a positive FetchIntervalSec is a fixed one.
type NewSource struct {
	URL              string `json:"url,omitempty"`
	RSSHubPath       string `json:"rsshubPath,omitempty"`
	Kind             string `json:"kind"`
	Title            string `json:"title,omitempty"`
	FetchIntervalSec int64  `json:"fetchIntervalSec,omitempty"`
	// AllowDuplicate adds the subscription although another one follows the same feed; the server then skips the
	// comparison of the addresses (an identical spelling is still refused).
	AllowDuplicate bool `json:"allowDuplicate,omitempty"`
}

// SourcePatch changes the fields of a subscription that are not nil; its address cannot change.
type SourcePatch struct {
	Title            *string `json:"title,omitempty"`
	Kind             *string `json:"kind,omitempty"`
	FetchIntervalSec *int64  `json:"fetchIntervalSec,omitempty"`
	// FetchIntervalAuto true switches to the automatic interval; a FetchIntervalSec sets a fixed one (and is
	// refused together with true).
	FetchIntervalAuto *bool   `json:"fetchIntervalAuto,omitempty"`
	MuteKeywords      *string `json:"muteKeywords,omitempty"`
	Paused            *bool   `json:"paused,omitempty"`
	// KeepDays is the keep period of the source's entries: 0 follows the global setting, -1 keeps them forever,
	// else a number of days up to 3650. Starred entries are always kept.
	KeepDays *int `json:"keepDays,omitempty"`
	// PaceSec is how long an unread entry stays in All and Unread, in seconds: 0 turns it off, else 3600 to 2592000.
	PaceSec *int `json:"paceSec,omitempty"`
	// HideFromAll keeps the source out of All and the kind timelines and their counts; it stays in its own view,
	// in search and in Starred.
	HideFromAll *bool `json:"hideFromAll,omitempty"`
	// ExtractSelector, ExtractReferer and ExtractRewrites are the source's extraction rules; an empty string clears one.
	ExtractSelector *string `json:"extractSelector,omitempty"`
	ExtractReferer  *string `json:"extractReferer,omitempty"`
	// OpenTarget is where a click on the source's entries goes: "follow", "reader" or "browser".
	OpenTarget *string `json:"openTarget,omitempty"`
	// FetchUserAgent and FetchReferer are the headers sent to the subscription's own host; an empty string clears one.
	FetchUserAgent  *string `json:"fetchUserAgent,omitempty"`
	FetchReferer    *string `json:"fetchReferer,omitempty"`
	ExtractRewrites *string `json:"extractRewrites,omitempty"`
	PromptSummary   *string `json:"promptSummary,omitempty"`
	PromptTranslate *string `json:"promptTranslate,omitempty"`
	AutoSummary     *bool   `json:"autoSummary,omitempty"`
	AutoTranslate   *bool   `json:"autoTranslate,omitempty"`
}

// Settings are the server's settings; RetentionDays is 0 to keep entries forever. The API key is never sent, only
// whether one is set; the prompts are the defaults until changed, which the Default fields hold.
type Settings struct {
	RSSHubBaseURL          string   `json:"rsshubBaseUrl"`
	RSSHubBaseURLs         []string `json:"rsshubBaseUrls"`
	RetentionDays          int      `json:"retentionDays"`
	AIBaseURL              string   `json:"aiBaseUrl"`
	AIModel                string   `json:"aiModel"`
	AIAPIKeySet            bool     `json:"aiApiKeySet"`
	AILanguage             string   `json:"aiLanguage"`
	AISummaryLimit         int      `json:"aiSummaryLimit"`
	AIChunkLimit           int      `json:"aiChunkLimit"`
	DefaultAISummaryLimit  int      `json:"defaultAiSummaryLimit"`
	DefaultAIChunkLimit    int      `json:"defaultAiChunkLimit"`
	AIAutoDailyLimit       int      `json:"aiAutoDailyLimit"`
	AIAutoFetchLimit       int      `json:"aiAutoFetchLimit"`
	DefaultAIAutoDaily     int      `json:"defaultAiAutoDailyLimit"`
	DefaultAIAutoFetch     int      `json:"defaultAiAutoFetchLimit"`
	AIAutoCallsToday       int      `json:"aiAutoCallsToday"`
	AIEndpointLocal        bool     `json:"aiEndpointLocal"`
	PromptSummary          string   `json:"promptSummary"`
	PromptTranslate        string   `json:"promptTranslate"`
	DefaultPromptSummary   string   `json:"defaultPromptSummary"`
	DefaultPromptTranslate string   `json:"defaultPromptTranslate"`
	// GroupSameLink shows an article once in the timeline when several sources carry it.
	GroupSameLink bool `json:"groupSameLink"`
	// Automatic backup: the choices, and the outcome of the last tries in Unix milliseconds (0 for never) with the
	// code of the last failure.
	AutoBackupEnabled   bool   `json:"autoBackupEnabled"`
	AutoBackupFolder    string `json:"autoBackupFolder"`
	AutoBackupFrequency string `json:"autoBackupFrequency"`
	AutoBackupKeep      int    `json:"autoBackupKeep"`
	AutoBackupLastAt    int64  `json:"autoBackupLastAt"`
	AutoBackupLastFile  string `json:"autoBackupLastFile"`
	AutoBackupFailedAt  int64  `json:"autoBackupFailedAt"`
	AutoBackupError     string `json:"autoBackupError"`
	// MuteKeywords hides, in every subscription, the entries that contain one of its lines.
	MuteKeywords string `json:"muteKeywords"`
}

// AIConfigured reports whether the settings name an AI endpoint and a model.
func (s Settings) AIConfigured() bool { return s.AIBaseURL != "" && s.AIModel != "" }

// SettingsPatch changes the settings that are not nil. AIAPIKey replaces the key, and "" removes it; an empty
// prompt restores the default, and so does a limit of 0.
type SettingsPatch struct {
	RSSHubBaseURL   *string   `json:"rsshubBaseUrl,omitempty"`
	RSSHubBaseURLs  *[]string `json:"rsshubBaseUrls,omitempty"`
	RetentionDays   *int      `json:"retentionDays,omitempty"`
	AIBaseURL       *string   `json:"aiBaseUrl,omitempty"`
	AIModel         *string   `json:"aiModel,omitempty"`
	AIAPIKey        *string   `json:"aiApiKey,omitempty"`
	AILanguage      *string   `json:"aiLanguage,omitempty"`
	AISummaryLimit  *int      `json:"aiSummaryLimit,omitempty"`
	AIChunkLimit    *int      `json:"aiChunkLimit,omitempty"`
	AIAutoDaily     *int      `json:"aiAutoDailyLimit,omitempty"`
	AIAutoFetch     *int      `json:"aiAutoFetchLimit,omitempty"`
	PromptSummary   *string   `json:"promptSummary,omitempty"`
	PromptTranslate *string   `json:"promptTranslate,omitempty"`
	GroupSameLink   *bool     `json:"groupSameLink,omitempty"`

	AutoBackupEnabled   *bool   `json:"autoBackupEnabled,omitempty"`
	AutoBackupFolder    *string `json:"autoBackupFolder,omitempty"`
	AutoBackupFrequency *string `json:"autoBackupFrequency,omitempty"`
	AutoBackupKeep      *int    `json:"autoBackupKeep,omitempty"`

	MuteKeywords *string `json:"muteKeywords,omitempty"`
}

// AIResult is a summary or translation of an entry: Status is "pending", "ok" or "failed", Error the code of a
// failure. A pending result keeps the text of the one before.
type AIResult struct {
	Status string `json:"status"`
	Text   string `json:"text"`
	Error  string `json:"error"`
	Model  string `json:"model"`
}

// AIResults are the results of an entry, nil when there is none, and the language translations are made in.
type AIResults struct {
	Summary        *AIResult `json:"summary"`
	Translation    *AIResult `json:"translation"`
	TargetLanguage string    `json:"targetLanguage"`
}

// Device is a session signed in to the server, named by the User-Agent it signed in with. Times are Unix
// milliseconds; Current marks the session of this app.
type Device struct {
	ID         int64  `json:"id"`
	UserAgent  string `json:"userAgent"`
	CreatedAt  int64  `json:"createdAt"`
	LastSeenAt int64  `json:"lastSeenAt"`
	Current    bool   `json:"current"`
}

// Imported is what an OPML import did: the subscriptions added, those already followed, and those refused.
type Imported struct {
	Added    int          `json:"added"`
	Existing int          `json:"existing"`
	Failed   []ImportFail `json:"failed"`
}

// ImportFail is a subscription of an OPML file the server refused, with the code of its reason, such as
// "invalid_url".
type ImportFail struct {
	Title   string `json:"title"`
	Address string `json:"address"`
	Error   string `json:"error"`
}

// The phases of an OPML import job.
const (
	OPMLIdle    = "idle"
	OPMLRunning = "running"
	OPMLDone    = "done"
	OPMLFailed  = "failed"
)

// OPMLImport is an OPML import job. ID names the job, so that a client reads only its own. Progress counts the
// subscriptions of the file handled so far, of all of them. Result is there when the job has ended, and holds the
// counts up to where it stopped when Phase is OPMLFailed, with Error its code.
type OPMLImport struct {
	ID       string `json:"id"`
	Phase    string `json:"phase"`
	Progress struct {
		Done  int `json:"done"`
		Total int `json:"total"`
	} `json:"progress"`
	Result *Imported `json:"result"`
	Error  string    `json:"error"`
}

// MaxOPML is the largest OPML file the server imports.
const MaxOPML = 1 << 20

// The services Session.StartImport can import from.
const (
	ServiceMiniflux = "miniflux"
	ServiceFreshRSS = "freshrss"
)

// ImportRequest names the service to import from and how to reach it. Credential is the API token (Miniflux) or API
// password (FreshRSS); the server keeps it in memory for the import only.
type ImportRequest struct {
	Service    string `json:"service"`
	BaseURL    string `json:"baseUrl"`
	Username   string `json:"username,omitempty"`
	Credential string `json:"credential"`
}

// ImportProgress is the stage an import is in ("subscriptions", "starred", "read" or "writing") and how far it is;
// Total is 0 when the service does not say.
type ImportProgress struct {
	Stage string `json:"stage"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

// ImportCounts is what a preview found in the other reader.
type ImportCounts struct {
	Sources int `json:"sources"`
	Folders int `json:"folders"`
	Starred int `json:"starred"`
	Read    int `json:"read"`
}

// ImportSkip is something an import left out, with the code of its reason. Kind is "source" or "starred".
type ImportSkip struct {
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Address string `json:"address"`
	Error   string `json:"error"`
}

// ImportResult is what an import wrote. Skipped lists at most 100 of the SkippedTotal items left out.
type ImportResult struct {
	AddedSources    int          `json:"addedSources"`
	ExistingSources int          `json:"existingSources"`
	StarredAdded    int          `json:"starredAdded"`
	StarredExisting int          `json:"starredExisting"`
	ReadMarked      int          `json:"readMarked"`
	UnmatchedRead   int          `json:"unmatchedRead"`
	FoldersSkipped  int          `json:"foldersSkipped"`
	Skipped         []ImportSkip `json:"skipped"`
	SkippedTotal    int          `json:"skippedTotal"`
}

// ImportStatus is the import job. Phase is "idle", "fetching", "ready" (the preview waits to be confirmed),
// "writing", "done", "failed" or "canceled"; Error is the code of a failure.
type ImportStatus struct {
	Phase    string         `json:"phase"`
	Service  string         `json:"service"`
	Progress ImportProgress `json:"progress"`
	Counts   ImportCounts   `json:"counts"`
	Result   *ImportResult  `json:"result"`
	Error    string         `json:"error"`
}

// Query selects a page of the timeline; the zero value is the first page of everything.
type Query struct {
	Source  int64
	Kind    string
	Unread  bool
	Starred bool
	Search  string
	// MaxMinutes keeps the entries that take at most this many minutes and have an estimate; 0 does not filter.
	MaxMinutes int
	// Since keeps the entries that arrived (created_at) at or after this time in Unix milliseconds; 0 does not filter.
	Since int64
	// Oldest lists the oldest entries first; the cursor of a page then leads to newer entries.
	Oldest bool
	Limit  int
	Cursor string
}

// Page is one page of entries; Next is empty on the last page.
type Page struct {
	Entries []Entry `json:"entries"`
	Next    string  `json:"nextCursor"`
}

// EntryPatch changes the fields that are not nil.
type EntryPatch struct {
	IsRead  *bool `json:"isRead,omitempty"`
	Starred *bool `json:"starred,omitempty"`
	// ReadProgress is how far the article was read, 0 to 1; the server refuses anything else with invalid_progress.
	ReadProgress *float64 `json:"readProgress,omitempty"`
	// RemindAt sets a reminder at this time in Unix milliseconds, which marks the entry read until then; 0 clears it.
	// The server refuses a time that is not in the future with invalid_remind_at.
	RemindAt *int64 `json:"remindAt,omitempty"`
}

// ReadScope says which entries "mark as read" marks: a source, a kind, those up to an entry (it and the older ones),
// newer than another entry, from an entry on (it and the newer ones), and those shown as older than a time.
type ReadScope struct {
	SourceID int64  `json:"sourceId,omitempty"`
	Kind     string `json:"kind,omitempty"`
	UpTo     int64  `json:"upTo,omitempty"`
	Newer    int64  `json:"newerThan,omitempty"`
	From     int64  `json:"from,omitempty"`
	// OlderThan is a time in Unix milliseconds; the server compares it with the time a row shows and refuses zero or less
	// with invalid_older_than.
	OlderThan int64 `json:"olderThan,omitempty"`
	// Since is a time in Unix milliseconds; only entries that arrived at or after it are marked (the timeline's time
	// range). The server refuses zero or less with invalid_since.
	Since int64 `json:"since,omitempty"`
}

// maxImage is the largest picture the server relays.
const maxImage = 8 << 20

// aiClient waits for the server's own AI call: at most 120 seconds, the budget of the self-check and the limit of a
// single call, with a margin.
var aiClient = &http.Client{Timeout: 130 * time.Second, Transport: agent{}}

// discoverClient waits longer than httpClient: the server may spend 20 seconds looking through pages.
var discoverClient = &http.Client{Timeout: 30 * time.Second, Transport: agent{}}

// imageClient waits longer than httpClient: the server itself may take 25 seconds to get a picture.
var imageClient = &http.Client{Timeout: 40 * time.Second, Transport: agent{}}

// zoneName is the name of the zone of this computer; the tests replace it.
var zoneName = func() string { return time.Local.String() }

// Entries returns a page of the timeline.
func (s Session) Entries(ctx context.Context, q Query) (Page, error) {
	values := url.Values{}
	if q.Source != 0 {
		values.Set("source", strconv.FormatInt(q.Source, 10))
	}
	if q.Kind != "" {
		values.Set("kind", q.Kind)
	}
	if q.Unread {
		values.Set("unread", "1")
	}
	if q.Starred {
		values.Set("starred", "1")
	}
	if q.Search != "" {
		values.Set("q", q.Search)
		// The server cannot know where the reader's days begin, which after: and before: need.
		if zone := zoneName(); zone != "" && zone != "Local" {
			values.Set("tz", zone)
		}
	}
	if q.MaxMinutes != 0 {
		values.Set("maxMinutes", strconv.Itoa(q.MaxMinutes))
	}
	if q.Since != 0 {
		values.Set("since", strconv.FormatInt(q.Since, 10))
	}
	if q.Oldest {
		values.Set("order", "asc")
	}
	if q.Limit != 0 {
		values.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Cursor != "" {
		values.Set("cursor", q.Cursor)
	}
	var page Page
	err := s.call(ctx, http.MethodGet, "/api/entries?"+values.Encode(), nil, &page)
	return page, err
}

// SourceList is the subscriptions with their unread counts, and the unread entries of All: with articles shown once
// that is fewer than the counts of the sources add up to.
type SourceList struct {
	Sources     []Source `json:"sources"`
	UnreadTotal int      `json:"unreadTotal"`
}

// Sources lists every subscription with its unread count.
func (s Session) Sources(ctx context.Context) (SourceList, error) {
	var reply SourceList
	err := s.call(ctx, http.MethodGet, "/api/sources", nil, &reply)
	return reply, err
}

// Health reasons a subscription can be listed for, and the causes of a dead one.
const (
	HealthDead        = "dead"
	HealthFailing     = "failing"
	HealthMoved       = "moved"
	HealthRarelyRead  = "rarelyRead"
	HealthCauseSilent = "silent"
)

// SourceHealth is a subscription the server flags as dead, failing or rarely read.
type SourceHealth struct {
	ID         int64  `json:"id"`
	URL        string `json:"url"`
	RSSHubPath string `json:"rsshubPath"`
	Title      string `json:"title"`
	IconURL    string `json:"iconUrl"`
	Kind       string `json:"kind"`
	// State is HealthDead, HealthFailing, HealthMoved or HealthRarelyRead; Cause tells a dead one that kept failing ("failing")
	// from one that fetches fine but has nothing new (HealthCauseSilent).
	State string `json:"state"`
	Cause string `json:"cause"`
	// Days counts the days without a success or without a new entry; ConsecutiveFailures the failed fetches in a row.
	Days                int    `json:"days"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	LastError           string `json:"lastError"`
	// MovedTo is the address a HealthMoved subscription moved to, empty otherwise.
	MovedTo string `json:"movedTo"`
	// Recent, RecentRead and RecentStarred count the entries of the last window days.
	Recent        int `json:"recent"`
	RecentRead    int `json:"recentRead"`
	RecentStarred int `json:"recentStarred"`
}

// Health is the list of subscriptions worth a second look, and the length of the window its counts cover.
type Health struct {
	WindowDays int            `json:"windowDays"`
	Sources    []SourceHealth `json:"sources"`
}

// SourcesHealth asks the server which subscriptions are dead, failing or rarely read.
func (s Session) SourcesHealth(ctx context.Context) (Health, error) {
	var health Health
	err := s.call(ctx, http.MethodGet, "/api/sources/health", nil, &health)
	return health, err
}

// FetchAttempt is the result of one fetch of a subscription. At is Unix milliseconds; Status is the last HTTP
// status received, 0 when there was none.
type FetchAttempt struct {
	At         int64  `json:"at"`
	DurationMs int64  `json:"durationMs"`
	OK         bool   `json:"ok"`
	Status     int    `json:"status"`
	NewEntries int    `json:"newEntries"`
	Error      string `json:"error"`
}

// ActivitySource is a subscription with its recent fetch attempts, newest first.
type ActivitySource struct {
	ID         int64          `json:"id"`
	URL        string         `json:"url"`
	RSSHubPath string         `json:"rsshubPath"`
	Title      string         `json:"title"`
	Paused     bool           `json:"paused"`
	Attempts   []FetchAttempt `json:"attempts"`
}

// FetchingSource is a subscription being fetched right now.
type FetchingSource struct {
	ID         int64  `json:"id"`
	URL        string `json:"url"`
	RSSHubPath string `json:"rsshubPath"`
	Title      string `json:"title"`
}

// Activity is what is being fetched now and how the recent fetches of each subscription went. Keep is how many
// attempts the server keeps for each subscription.
type Activity struct {
	Keep     int              `json:"keep"`
	Fetching []FetchingSource `json:"fetching"`
	Sources  []ActivitySource `json:"sources"`
}

// Activity asks the server what it is fetching and how the recent fetches went.
func (s Session) Activity(ctx context.Context) (Activity, error) {
	var activity Activity
	err := s.call(ctx, http.MethodGet, "/api/activity", nil, &activity)
	return activity, err
}

// InstanceHealth is how the latest requests to one RSSHub instance went. SuccessRate (0 to 1), MedianMs and
// CoolingUntil (Unix milliseconds) are nil when there is nothing to say.
type InstanceHealth struct {
	URL          string   `json:"url"`
	Requests     int      `json:"requests"`
	SuccessRate  *float64 `json:"successRate"`
	MedianMs     *int64   `json:"medianMs"`
	CoolingUntil *int64   `json:"coolingUntil"`
}

// RSSHubHealth is the health of the RSSHub instances in the settings, in their order. The server counts the latest
// Keep requests that are at most Days old.
type RSSHubHealth struct {
	Keep      int              `json:"keep"`
	Days      int              `json:"days"`
	Instances []InstanceHealth `json:"instances"`
}

// RSSHubHealth asks the server how the latest requests to each RSSHub instance went.
func (s Session) RSSHubHealth(ctx context.Context) (RSSHubHealth, error) {
	var health RSSHubHealth
	err := s.call(ctx, http.MethodGet, "/api/rsshub/health", nil, &health)
	return health, err
}

// The states of a Vacuum.
const (
	VacuumIdle    = "idle"
	VacuumRunning = "running"
	VacuumDone    = "done"
	VacuumFailed  = "failed"
)

// Vacuum is the state of the database compaction on the server. Times are Unix milliseconds, 0 when not yet.
type Vacuum struct {
	State      string `json:"state"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt"`
	FreedBytes int64  `json:"freedBytes"`
	Error      string `json:"error"`
}

// Storage is what the server's data takes up, and the state of the last vacuum.
type Storage struct {
	DatabaseBytes    int64  `json:"databaseBytes"`
	ReclaimableBytes int64  `json:"reclaimableBytes"`
	Entries          int64  `json:"entries"`
	Images           int    `json:"images"`
	ImageBytes       int64  `json:"imageBytes"`
	Copies           int    `json:"copies"`
	CopyBytes        int64  `json:"copyBytes"`
	Vacuum           Vacuum `json:"vacuum"`
	// PreMigrate is the copy of the database made before the last migration; nil when there is none.
	PreMigrate *PreMigrate `json:"premigrate"`
}

// PreMigrate is the copy of the database the server keeps from before its last migration: its size and the schema
// version it was made from (0 when unknown).
type PreMigrate struct {
	Bytes   int64 `json:"bytes"`
	Version int   `json:"version"`
}

// DeletePreMigrate deletes the copy made before the last migration. One that is already gone is not an error.
func (s Session) DeletePreMigrate(ctx context.Context) error {
	return s.call(ctx, http.MethodDelete, "/api/storage/premigrate", nil, nil)
}

// Storage asks the server how big its database and image cache are.
func (s Session) Storage(ctx context.Context) (Storage, error) {
	var storage Storage
	err := s.call(ctx, http.MethodGet, "/api/storage", nil, &storage)
	return storage, err
}

// StartVacuum asks the server to compact its database in the background and returns the state it started in. A vacuum
// already running is an *APIError with the code "vacuum_running".
func (s Session) StartVacuum(ctx context.Context) (Vacuum, error) {
	var started struct {
		Vacuum Vacuum `json:"vacuum"`
	}
	err := s.call(ctx, http.MethodPost, "/api/storage/vacuum", nil, &started)
	return started.Vacuum, err
}

// AddSource subscribes to a feed or an RSSHub route and returns the subscription.
func (s Session) AddSource(ctx context.Context, src NewSource) (Source, error) {
	var added Source
	err := s.call(ctx, http.MethodPost, "/api/sources", src, &added)
	return added, err
}

// UpdateSource changes a subscription and returns it as changed.
func (s Session) UpdateSource(ctx context.Context, id int64, patch SourcePatch) (Source, error) {
	var changed Source
	err := s.call(ctx, http.MethodPatch, "/api/sources/"+strconv.FormatInt(id, 10), patch, &changed)
	return changed, err
}

// MutedCount is how many entries of a subscription the keywords, one per line, would hide now; they are not saved.
func (s Session) MutedCount(ctx context.Context, id int64, keywords string) (int, error) {
	var reply struct {
		Count int `json:"count"`
	}
	body := struct {
		MuteKeywords string `json:"muteKeywords"`
	}{keywords}
	err := s.call(ctx, http.MethodPost, "/api/sources/"+strconv.FormatInt(id, 10)+"/muted-count", body, &reply)
	return reply.Count, err
}

// MutedCountAll is how many entries the global keywords, one per line, would hide now; they are not saved.
func (s Session) MutedCountAll(ctx context.Context, keywords string) (int, error) {
	var reply struct {
		Count int `json:"count"`
	}
	body := struct {
		MuteKeywords string `json:"muteKeywords"`
	}{keywords}
	err := s.call(ctx, http.MethodPost, "/api/muted-count", body, &reply)
	return reply.Count, err
}

// Rule acts on the new entries that match its conditions: all of the filled ones. No SourceIDs is every
// subscription, Kind "" any kind; Exclude is words (one per line) that keep an entry from matching. It stars them,
// marks them read, or both.
type Rule struct {
	ID        int64   `json:"id"`
	SourceIDs []int64 `json:"sourceIds"`
	Kind      string  `json:"kind"`
	Keywords  string  `json:"keywords"`
	Exclude   string  `json:"excludeKeywords"`
	Author    string  `json:"author"`
	Star      bool    `json:"star"`
	MarkRead  bool    `json:"markRead"`
	Enabled   bool    `json:"enabled"`
	CreatedAt int64   `json:"createdAt"`
}

// NewRule is a rule to add, or the whole of a rule to replace one with: the ID and creation time are the server's.
type NewRule struct {
	SourceIDs []int64 `json:"sourceIds"`
	Kind      string  `json:"kind"`
	Keywords  string  `json:"keywords"`
	Exclude   string  `json:"excludeKeywords"`
	Author    string  `json:"author"`
	Star      bool    `json:"star"`
	MarkRead  bool    `json:"markRead"`
	Enabled   bool    `json:"enabled"`
}

// RuleConditions are the conditions of a rule that has not been saved, for RuleMatchCount.
type RuleConditions struct {
	SourceIDs []int64 `json:"sourceIds"`
	Kind      string  `json:"kind"`
	Keywords  string  `json:"keywords"`
	Exclude   string  `json:"excludeKeywords"`
	Author    string  `json:"author"`
}

// RuleMatchCount is how many entries you already have that the conditions match now; nothing is saved or changed.
// Without a condition it is 0.
func (s Session) RuleMatchCount(ctx context.Context, conditions RuleConditions) (int, error) {
	var reply struct {
		Count int `json:"count"`
	}
	err := s.call(ctx, http.MethodPost, "/api/rules/match-count", conditions, &reply)
	return reply.Count, err
}

// Rules lists the rules, the oldest first.
func (s Session) Rules(ctx context.Context) ([]Rule, error) {
	var reply struct {
		Rules []Rule `json:"rules"`
	}
	err := s.call(ctx, http.MethodGet, "/api/rules", nil, &reply)
	return reply.Rules, err
}

// CreateRule adds a rule and returns it. The fifty-first is an *APIError with the code "too_many_rules".
func (s Session) CreateRule(ctx context.Context, rule NewRule) (Rule, error) {
	var created Rule
	err := s.call(ctx, http.MethodPost, "/api/rules", rule, &created)
	return created, err
}

// RestoreRule adds a deleted rule back with its old ID and creation time, so that it takes its old place in the list.
// The server gives it a new ID when the old one is taken.
func (s Session) RestoreRule(ctx context.Context, rule Rule) (Rule, error) {
	body := struct {
		ID        int64 `json:"id"`
		CreatedAt int64 `json:"createdAt"`
		NewRule
	}{rule.ID, rule.CreatedAt, NewRule{SourceIDs: rule.SourceIDs, Kind: rule.Kind, Keywords: rule.Keywords,
		Exclude: rule.Exclude, Author: rule.Author, Star: rule.Star, MarkRead: rule.MarkRead, Enabled: rule.Enabled}}
	var created Rule
	err := s.call(ctx, http.MethodPost, "/api/rules", body, &created)
	return created, err
}

// UpdateRule replaces the conditions and actions of a rule, and returns it as changed.
func (s Session) UpdateRule(ctx context.Context, id int64, rule NewRule) (Rule, error) {
	var changed Rule
	err := s.call(ctx, http.MethodPatch, "/api/rules/"+strconv.FormatInt(id, 10), rule, &changed)
	return changed, err
}

// SetRuleEnabled turns a rule on or off.
func (s Session) SetRuleEnabled(ctx context.Context, id int64, enabled bool) (Rule, error) {
	var changed Rule
	body := struct {
		Enabled bool `json:"enabled"`
	}{enabled}
	err := s.call(ctx, http.MethodPatch, "/api/rules/"+strconv.FormatInt(id, 10), body, &changed)
	return changed, err
}

// DeleteRule deletes a rule.
func (s Session) DeleteRule(ctx context.Context, id int64) error {
	return s.call(ctx, http.MethodDelete, "/api/rules/"+strconv.FormatInt(id, 10), nil, nil)
}

// DeleteSource deletes a subscription with its entries, starred ones included. RestoreSource undoes it for a short
// while; after that the server removes it for good.
func (s Session) DeleteSource(ctx context.Context, id int64) error {
	return s.call(ctx, http.MethodDelete, "/api/sources/"+strconv.FormatInt(id, 10), nil, nil)
}

// RestoreSource undoes DeleteSource within the server's undo window and returns the subscription. After the window
// the error code is not_found.
func (s Session) RestoreSource(ctx context.Context, id int64) (Source, error) {
	var src Source
	err := s.call(ctx, http.MethodPost, "/api/sources/"+strconv.FormatInt(id, 10)+"/restore", nil, &src)
	return src, err
}

// MoveSource accepts the suggestion to use the address the site moved to: movedTo is the address the user saw, and the
// subscription keeps its entries. The error code is "move_changed" when the suggestion is not that address any more, and
// "source_exists" (with SourceID) when another subscription already has it.
func (s Session) MoveSource(ctx context.Context, id int64, movedTo string) (Source, error) {
	var src Source
	err := s.call(ctx, http.MethodPost, "/api/sources/"+strconv.FormatInt(id, 10)+"/move", map[string]string{"url": movedTo}, &src)
	return src, err
}

// DismissMove turns the suggested address down; the errors are those of MoveSource but "source_exists".
func (s Session) DismissMove(ctx context.Context, id int64, movedTo string) (Source, error) {
	var src Source
	err := s.call(ctx, http.MethodPost, "/api/sources/"+strconv.FormatInt(id, 10)+"/dismiss-move", map[string]string{"url": movedTo}, &src)
	return src, err
}

// RefreshSource asks the server to fetch a subscription now.
func (s Session) RefreshSource(ctx context.Context, id int64) error {
	return s.call(ctx, http.MethodPost, "/api/sources/"+strconv.FormatInt(id, 10)+"/refresh", nil, nil)
}

// RefreshResult says how many subscriptions RefreshAll queued and the server
// time, in milliseconds, at which it queued them.
type RefreshResult struct {
	Queued int   `json:"queued"`
	At     int64 `json:"at"`
}

// RefreshAll marks every unpaused subscription due now; the server fetches
// them in the background.
func (s Session) RefreshAll(ctx context.Context) (RefreshResult, error) {
	var out RefreshResult
	err := s.call(ctx, http.MethodPost, "/api/sources/refresh", nil, &out)
	return out, err
}

// Settings returns the server's settings.
func (s Session) Settings(ctx context.Context) (Settings, error) {
	var settings Settings
	err := s.call(ctx, http.MethodGet, "/api/settings", nil, &settings)
	return settings, err
}

// SetSettings changes the server's settings and returns them all.
func (s Session) SetSettings(ctx context.Context, patch SettingsPatch) (Settings, error) {
	var settings Settings
	err := s.call(ctx, http.MethodPatch, "/api/settings", patch, &settings)
	return settings, err
}

// Devices lists the sessions signed in to the server; a server without a password has none.
func (s Session) Devices(ctx context.Context) ([]Device, error) {
	var reply struct {
		Sessions []Device `json:"sessions"`
	}
	err := s.call(ctx, http.MethodGet, "/api/sessions", nil, &reply)
	return reply.Sessions, err
}

// RevokeDevice signs a session out of the server.
func (s Session) RevokeDevice(ctx context.Context, id int64) error {
	return s.call(ctx, http.MethodDelete, "/api/sessions/"+strconv.FormatInt(id, 10), nil, nil)
}

// StartOPMLImport starts the import of an OPML file, at most MaxOPML bytes, and returns the job. The file is checked
// at once: a file that is too large or is not OPML is an error (opml_too_large, invalid_opml) and starts no job, and
// an import already running is opml_import_busy.
func (s Session) StartOPMLImport(ctx context.Context, opml []byte) (OPMLImport, error) {
	resp, err := s.send(ctx, httpClient, http.MethodPost, "/api/opml/import/start", "text/x-opml", bytes.NewReader(opml))
	if err != nil {
		return OPMLImport{}, err
	}
	defer resp.Body.Close()
	var job OPMLImport
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&job); err != nil {
		return OPMLImport{}, fmt.Errorf("import: %w", err)
	}
	return job, nil
}

// OPMLImportStatus returns the OPML import job, the one running or the one that ended last.
func (s Session) OPMLImportStatus(ctx context.Context) (OPMLImport, error) {
	var job OPMLImport
	err := s.call(ctx, http.MethodGet, "/api/opml/import", nil, &job)
	return job, err
}

// ExportOPML returns every subscription as an OPML file.
func (s Session) ExportOPML(ctx context.Context) ([]byte, error) {
	resp, err := s.send(ctx, httpClient, http.MethodGet, "/api/opml/export", "", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	return body, nil
}

// Diagnostics returns the diagnostics file of the server: text without secrets, full addresses or entry content.
func (s Session) Diagnostics(ctx context.Context) ([]byte, error) {
	resp, err := s.send(ctx, httpClient, http.MethodGet, "/api/diagnostics", "", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	return body, nil
}

// backupClient waits as long as the server may work on a backup or a restore, which grows with the data.
var backupClient = &http.Client{Timeout: 30 * time.Minute, Transport: agent{}}

// Backup streams a backup of all the server's data into dst and returns how many bytes it wrote.
func (s Session) Backup(ctx context.Context, dst io.Writer) (int64, error) {
	resp, err := s.send(ctx, backupClient, http.MethodGet, "/api/backup", "", nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	n, err := io.Copy(dst, resp.Body)
	if err != nil {
		return n, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	return n, nil
}

// Restored is what a restore brought in.
type Restored struct {
	Sources int `json:"sources"`
	Entries int `json:"entries"`
}

// Restore uploads a backup, which replaces all the server's data. A refused backup is an *APIError whose Code is
// invalid_backup, corrupt_backup, backup_newer, backup_too_large, restore_running or vacuum_running, and leaves the
// data as it was.
func (s Session) Restore(ctx context.Context, backup io.Reader) (Restored, error) {
	resp, err := s.send(ctx, backupClient, http.MethodPost, "/api/restore", "application/zip", backup)
	if err != nil {
		return Restored{}, err
	}
	defer resp.Body.Close()
	var restored Restored
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&restored); err != nil {
		return Restored{}, fmt.Errorf("restore: %w", err)
	}
	return restored, nil
}

// StartImport has the server read another reader's subscriptions, stars and read state into a preview. Nothing is
// written until ConfirmImport.
func (s Session) StartImport(ctx context.Context, req ImportRequest) (ImportStatus, error) {
	var status ImportStatus
	err := s.call(ctx, http.MethodPost, "/api/import/start", req, &status)
	return status, err
}

// ImportStatus returns the import job.
func (s Session) ImportStatus(ctx context.Context) (ImportStatus, error) {
	var status ImportStatus
	err := s.call(ctx, http.MethodGet, "/api/import", nil, &status)
	return status, err
}

// ConfirmImport writes the preview that is ready.
func (s Session) ConfirmImport(ctx context.Context) (ImportStatus, error) {
	var status ImportStatus
	err := s.call(ctx, http.MethodPost, "/api/import/confirm", nil, &status)
	return status, err
}

// CancelImport stops the import and drops its preview, or clears a finished one.
func (s Session) CancelImport(ctx context.Context) (ImportStatus, error) {
	var status ImportStatus
	err := s.call(ctx, http.MethodPost, "/api/import/cancel", nil, &status)
	return status, err
}

// Entry returns an entry with its text.
func (s Session) Entry(ctx context.Context, id int64) (Article, error) {
	var article Article
	err := s.call(ctx, http.MethodGet, "/api/entries/"+strconv.FormatInt(id, 10), nil, &article)
	return article, err
}

// RetryExtraction asks the server to get an entry's full text again, with all the attempts back, and returns the
// entry as it now stands. The text arrives later: the entry's ExtractStatus is "pending" until then.
func (s Session) RetryExtraction(ctx context.Context, id int64) (Article, error) {
	var article Article
	err := s.call(ctx, http.MethodPost, "/api/entries/"+strconv.FormatInt(id, 10)+"/extract", nil, &article)
	return article, err
}

// SetEntry changes whether an entry is read or starred, or how far it was read, and returns it as changed.
func (s Session) SetEntry(ctx context.Context, id int64, patch EntryPatch) (Entry, error) {
	var entry Entry
	err := s.call(ctx, http.MethodPatch, "/api/entries/"+strconv.FormatInt(id, 10), patch, &entry)
	return entry, err
}

// Marked is what a bulk mark as read changed: the IDs of the entries that were unread, and the time it read them, in
// Unix milliseconds. MarkUnread takes both to undo it.
type Marked struct {
	IDs    []int64 `json:"ids"`
	ReadAt int64   `json:"readAt"`
}

// MarkRead marks the unread entries of a scope as read and returns what it changed.
func (s Session) MarkRead(ctx context.Context, scope ReadScope) (Marked, error) {
	var reply Marked
	err := s.call(ctx, http.MethodPost, "/api/entries/read", scope, &reply)
	return reply, err
}

// MarkUnread undoes a MarkRead: it marks the entries of m unread again, except those read again since, and returns
// the IDs of those it changed.
func (s Session) MarkUnread(ctx context.Context, m Marked) ([]int64, error) {
	var reply struct {
		IDs []int64 `json:"ids"`
	}
	err := s.call(ctx, http.MethodPost, "/api/entries/unread", m, &reply)
	return reply.IDs, err
}

// AI returns the summary and the translation of an entry.
func (s Session) AI(ctx context.Context, id int64) (AIResults, error) {
	var results AIResults
	err := s.call(ctx, http.MethodGet, "/api/entries/"+strconv.FormatInt(id, 10)+"/ai", nil, &results)
	return results, err
}

// RequestAI asks for the summary of an entry, task "summary", or its translation, task "translation", and returns
// the result as it is: pending while it is made. An unconfigured endpoint is the APIError "ai_not_configured".
func (s Session) RequestAI(ctx context.Context, id int64, task string) (AIResult, error) {
	var result AIResult
	err := s.call(ctx, http.MethodPost, "/api/entries/"+strconv.FormatInt(id, 10)+"/"+task, nil, &result)
	return result, err
}

// AITest is what the server's self-check of the AI endpoint found. The pointers are nil when the endpoint did not
// say: ModelFound whether its list has the model, ContextTokens the model's context length, ContextFits whether the
// summary limit and chunk size fit in it.
type AITest struct {
	Model         string `json:"model"`
	ModelFound    *bool  `json:"modelFound"`
	ContextTokens *int   `json:"contextTokens"`
	ContextFits   *bool  `json:"contextFits"`
}

// TestAI has the server check the AI endpoint with the saved settings and returns what it found.
func (s Session) TestAI(ctx context.Context) (AITest, error) {
	resp, err := s.send(ctx, aiClient, http.MethodPost, "/api/ai/test", "", nil)
	if err != nil {
		return AITest{}, err
	}
	defer resp.Body.Close()
	var reply AITest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&reply); err != nil {
		return AITest{}, fmt.Errorf("test AI: %w", err)
	}
	return reply, nil
}

// Selection has the server translate (task "translate") or explain ("explain") a text selected while reading, and
// returns the answer and the model that gave it. title, the article the text is from, is context and may be empty.
// The server answers at once and stores nothing; it may take as long as the AI endpoint does.
func (s Session) Selection(ctx context.Context, task, text, title string) (string, string, error) {
	payload, err := json.Marshal(map[string]string{"task": task, "text": text, "title": title})
	if err != nil {
		return "", "", fmt.Errorf("selection: %w", err)
	}
	resp, err := s.send(ctx, aiClient, http.MethodPost, "/api/ai/selection", "application/json", bytes.NewReader(payload))
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var reply struct {
		Text  string `json:"text"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&reply); err != nil {
		return "", "", fmt.Errorf("selection: %w", err)
	}
	return reply.Text, reply.Model, nil
}

// Image gets a picture found inside a feed through the server's image relay.
func (s Session) Image(ctx context.Context, address string) ([]byte, error) {
	resp, err := s.send(ctx, imageClient, http.MethodGet, "/api/image?url="+url.QueryEscape(address), "", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImage+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if len(body) > maxImage {
		return nil, fmt.Errorf("image is larger than %d MiB", maxImage>>20)
	}
	return body, nil
}

// Candidate is one feed a web address leads to. It carries either a feed URL or an RSSHub route; Site, Label and
// Subject describe a route found by a rule so the screen can name it in the user's language.
type Candidate struct {
	URL        string `json:"url"`
	RSSHubPath string `json:"rsshubPath"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
	Origin     string `json:"origin"`
	Site       string `json:"site"`
	Label      string `json:"label"`
	Subject    string `json:"subject"`
	// SubscribedID is the subscription that already follows this feed, 0 when none does.
	SubscribedID int64 `json:"subscribedId"`
}

// Discover asks the server which feeds a web address leads to. An empty list means it found none.
func (s Session) Discover(ctx context.Context, address string) ([]Candidate, error) {
	var out struct {
		Candidates []Candidate `json:"candidates"`
	}
	resp, err := s.send(ctx, discoverClient, http.MethodGet, "/api/discover?url="+url.QueryEscape(address), "", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("discover: %w", err)
	}
	return out.Candidates, nil
}

// RouteParam is one parameter of a route template, in the order it appears in the path.
type RouteParam struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Optional    bool   `json:"optional"`
}

// Route is a route of the user's RSSHub instance. Values are the parameters the server read from a pasted link.
type Route struct {
	Path          string            `json:"path"`
	Name          string            `json:"name"`
	Namespace     string            `json:"namespace"`
	NamespaceName string            `json:"namespaceName"`
	Example       string            `json:"example"`
	Parameters    []RouteParam      `json:"parameters"`
	Values        map[string]string `json:"values"`
}

// Routes is the answer of a route search. Available is false when the instance has no readable catalog; Total is
// the number of matches, of which Routes holds the best few.
type Routes struct {
	Available bool    `json:"available"`
	Instance  string  `json:"instance"`
	Total     int     `json:"total"`
	Routes    []Route `json:"routes"`
}

// PreviewEntry is one entry of a previewed route; PublishedAt is Unix milliseconds, nil when it has none.
type PreviewEntry struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	PublishedAt *int64 `json:"publishedAt"`
}

// RoutePreview is the first entries of a route, fetched without subscribing.
type RoutePreview struct {
	Instance string         `json:"instance"`
	Title    string         `json:"title"`
	Entries  []PreviewEntry `json:"entries"`
}

// FindRoutes asks the server for the routes of the RSSHub instance that fit a pasted link or a name. refresh makes
// it read the instance's catalog again.
func (s Session) FindRoutes(ctx context.Context, query string, refresh bool) (Routes, error) {
	target := "/api/routes?q=" + url.QueryEscape(query)
	if refresh {
		target += "&refresh=1"
	}
	var out Routes
	resp, err := s.send(ctx, discoverClient, http.MethodGet, target, "", nil)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return Routes{}, fmt.Errorf("routes: %w", err)
	}
	return out, nil
}

// PreviewRoute asks the server for the first entries of a route, such as /github/issue/rsshub/rsshub, without
// subscribing to it.
func (s Session) PreviewRoute(ctx context.Context, path string) (RoutePreview, error) {
	var out RoutePreview
	resp, err := s.send(ctx, discoverClient, http.MethodGet, "/api/routes/preview?path="+url.QueryEscape(path), "", nil)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return RoutePreview{}, fmt.Errorf("route preview: %w", err)
	}
	return out, nil
}

// call sends a JSON request and decodes the JSON reply into out, or drops the reply for a nil out.
func (s Session) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	contentType := ""
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body, contentType = strings.NewReader(string(data)), "application/json"
	}
	resp, err := s.send(ctx, httpClient, method, path, contentType, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out); err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	return nil
}

// send makes a request with the session's token and turns an error status into an error.
func (s Session) send(ctx context.Context, client *http.Client, method, path, contentType string,
	body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.Server+path, body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if s.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.Token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if resp.StatusCode < 300 {
		return resp, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	var reply struct {
		Error    string `json:"error"`
		SourceID int64  `json:"sourceId"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&reply)
	return nil, &APIError{Status: resp.StatusCode, Code: reply.Error, SourceID: reply.SourceID}
}
