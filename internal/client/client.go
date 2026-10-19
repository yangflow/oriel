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

// SetVersion puts the app's version in the User-Agent of every request; call it before the first one.
func SetVersion(version string) { userAgent = "Oriel/" + version + " (" + platform() + ")" }

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

// APIError is an error the server answered with, by its stable code such as "query_too_short".
type APIError struct {
	Status int
	Code   string
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
	ID           int64   `json:"id"`
	SourceID     int64   `json:"sourceId"`
	SourceTitle  string  `json:"sourceTitle"`
	Kind         string  `json:"kind"`
	Title        string  `json:"title"`
	URL          string  `json:"url"`
	Author       string  `json:"author"`
	PublishedAt  int64   `json:"publishedAt"`
	Summary      string  `json:"summary"`
	ThumbnailURL string  `json:"thumbnailUrl"`
	DurationSec  int     `json:"durationSec"`
	Platform     string  `json:"platform"`
	Images       []Image `json:"images"`
	// ExtractStatus is how getting the full text of an article went: "pending", "ok", "failed", or "skipped" for
	// the other kinds.
	ExtractStatus string `json:"extractStatus"`
	IsRead        bool   `json:"isRead"`
	Starred       bool   `json:"starred"`
	CreatedAt     int64  `json:"createdAt"`
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
	Title               string `json:"title"`
	Kind                string `json:"kind"`
	FetchIntervalSec    int64  `json:"fetchIntervalSec"`
	NextFetchAt         int64  `json:"nextFetchAt"`
	LastFetchedAt       int64  `json:"lastFetchedAt"`
	LastSuccessAt       int64  `json:"lastSuccessAt"`
	LastError           string `json:"lastError"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	MuteKeywords        string `json:"muteKeywords"`
	Paused              bool   `json:"paused"`
	UnreadCount         int    `json:"unreadCount"`
	PromptSummary       string `json:"promptSummary"`
	PromptTranslate     string `json:"promptTranslate"`
	AutoSummary         bool   `json:"autoSummary"`
	AutoTranslate       bool   `json:"autoTranslate"`
}

// NewSource is a subscription to add: exactly one of URL and RSSHubPath. An empty Title takes the feed's, and a zero
// FetchIntervalSec the server's default.
type NewSource struct {
	URL              string `json:"url,omitempty"`
	RSSHubPath       string `json:"rsshubPath,omitempty"`
	Kind             string `json:"kind"`
	Title            string `json:"title,omitempty"`
	FetchIntervalSec int64  `json:"fetchIntervalSec,omitempty"`
}

// SourcePatch changes the fields of a subscription that are not nil; its address cannot change.
type SourcePatch struct {
	Title            *string `json:"title,omitempty"`
	Kind             *string `json:"kind,omitempty"`
	FetchIntervalSec *int64  `json:"fetchIntervalSec,omitempty"`
	MuteKeywords     *string `json:"muteKeywords,omitempty"`
	Paused           *bool   `json:"paused,omitempty"`
	PromptSummary    *string `json:"promptSummary,omitempty"`
	PromptTranslate  *string `json:"promptTranslate,omitempty"`
	AutoSummary      *bool   `json:"autoSummary,omitempty"`
	AutoTranslate    *bool   `json:"autoTranslate,omitempty"`
}

// Settings are the server's settings; RetentionDays is 0 to keep entries forever. The API key is never sent, only
// whether one is set; the prompts are the defaults until changed, which the Default fields hold.
type Settings struct {
	RSSHubBaseURL          string `json:"rsshubBaseUrl"`
	RetentionDays          int    `json:"retentionDays"`
	AIBaseURL              string `json:"aiBaseUrl"`
	AIModel                string `json:"aiModel"`
	AIAPIKeySet            bool   `json:"aiApiKeySet"`
	AILanguage             string `json:"aiLanguage"`
	PromptSummary          string `json:"promptSummary"`
	PromptTranslate        string `json:"promptTranslate"`
	DefaultPromptSummary   string `json:"defaultPromptSummary"`
	DefaultPromptTranslate string `json:"defaultPromptTranslate"`
}

// AIConfigured reports whether the settings name an AI endpoint and a model.
func (s Settings) AIConfigured() bool { return s.AIBaseURL != "" && s.AIModel != "" }

// SettingsPatch changes the settings that are not nil. AIAPIKey replaces the key, and "" removes it; an empty
// prompt restores the default.
type SettingsPatch struct {
	RSSHubBaseURL   *string `json:"rsshubBaseUrl,omitempty"`
	RetentionDays   *int    `json:"retentionDays,omitempty"`
	AIBaseURL       *string `json:"aiBaseUrl,omitempty"`
	AIModel         *string `json:"aiModel,omitempty"`
	AIAPIKey        *string `json:"aiApiKey,omitempty"`
	AILanguage      *string `json:"aiLanguage,omitempty"`
	PromptSummary   *string `json:"promptSummary,omitempty"`
	PromptTranslate *string `json:"promptTranslate,omitempty"`
}

// AIResult is a summary or translation of an entry: Status is "pending", "ok" or "failed", Error the code of a
// failure. A pending result keeps the text of the one before.
type AIResult struct {
	Status string `json:"status"`
	Text   string `json:"text"`
	Error  string `json:"error"`
	Model  string `json:"model"`
}

// AIResults are the results of an entry, nil when there is none.
type AIResults struct {
	Summary     *AIResult `json:"summary"`
	Translation *AIResult `json:"translation"`
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

// MaxOPML is the largest OPML file the server imports.
const MaxOPML = 1 << 20

// Query selects a page of the timeline; the zero value is the first page of everything.
type Query struct {
	Source  int64
	Kind    string
	Unread  bool
	Starred bool
	Search  string
	Limit   int
	Cursor  string
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
}

// ReadScope says which entries "mark all as read" marks: a source, a kind, and those up to an entry.
type ReadScope struct {
	SourceID int64  `json:"sourceId,omitempty"`
	Kind     string `json:"kind,omitempty"`
	UpTo     int64  `json:"upTo,omitempty"`
}

// maxImage is the largest picture the server relays.
const maxImage = 8 << 20

// aiClient waits for the server's own AI call, which may take 120 seconds.
var aiClient = &http.Client{Timeout: 130 * time.Second, Transport: agent{}}

// imageClient waits longer than httpClient: the server itself may take 25 seconds to get a picture.
var imageClient = &http.Client{Timeout: 40 * time.Second, Transport: agent{}}

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

// Sources lists every subscription with its unread count.
func (s Session) Sources(ctx context.Context) ([]Source, error) {
	var reply struct {
		Sources []Source `json:"sources"`
	}
	err := s.call(ctx, http.MethodGet, "/api/sources", nil, &reply)
	return reply.Sources, err
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

// DeleteSource removes a subscription with all its entries, starred ones included.
func (s Session) DeleteSource(ctx context.Context, id int64) error {
	return s.call(ctx, http.MethodDelete, "/api/sources/"+strconv.FormatInt(id, 10), nil, nil)
}

// RefreshSource asks the server to fetch a subscription now.
func (s Session) RefreshSource(ctx context.Context, id int64) error {
	return s.call(ctx, http.MethodPost, "/api/sources/"+strconv.FormatInt(id, 10)+"/refresh", nil, nil)
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

// ImportOPML adds the subscriptions of an OPML file, at most MaxOPML bytes.
func (s Session) ImportOPML(ctx context.Context, opml []byte) (Imported, error) {
	resp, err := s.send(ctx, httpClient, http.MethodPost, "/api/opml/import", "text/x-opml", bytes.NewReader(opml))
	if err != nil {
		return Imported{}, err
	}
	defer resp.Body.Close()
	var imported Imported
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&imported); err != nil {
		return Imported{}, fmt.Errorf("import: %w", err)
	}
	return imported, nil
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

// Entry returns an entry with its text.
func (s Session) Entry(ctx context.Context, id int64) (Article, error) {
	var article Article
	err := s.call(ctx, http.MethodGet, "/api/entries/"+strconv.FormatInt(id, 10), nil, &article)
	return article, err
}

// SetEntry changes whether an entry is read or starred and returns it as changed.
func (s Session) SetEntry(ctx context.Context, id int64, patch EntryPatch) (Entry, error) {
	var entry Entry
	err := s.call(ctx, http.MethodPatch, "/api/entries/"+strconv.FormatInt(id, 10), patch, &entry)
	return entry, err
}

// MarkRead marks the unread entries of a scope as read and returns how many it marked.
func (s Session) MarkRead(ctx context.Context, scope ReadScope) (int, error) {
	var reply struct {
		Marked int `json:"marked"`
	}
	err := s.call(ctx, http.MethodPost, "/api/entries/read", scope, &reply)
	return reply.Marked, err
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

// TestAI sends a short request to the AI endpoint with the saved settings and returns the model that answered.
func (s Session) TestAI(ctx context.Context) (string, error) {
	resp, err := s.send(ctx, aiClient, http.MethodPost, "/api/ai/test", "", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var reply struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&reply); err != nil {
		return "", fmt.Errorf("test AI: %w", err)
	}
	return reply.Model, nil
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
		Error string `json:"error"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&reply)
	return nil, &APIError{Status: resp.StatusCode, Code: reply.Error}
}
