// Package client talks to an Oriel server over its HTTP API, the only way the desktop UI reaches the data. It also
// keeps the signed-in session of a remote server in a file, never the password.
package client

import (
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

var httpClient = &http.Client{Timeout: 15 * time.Second}

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
	IsRead       bool    `json:"isRead"`
	Starred      bool    `json:"starred"`
	CreatedAt    int64   `json:"createdAt"`
}

// Source is a subscription as the timeline shows it.
type Source struct {
	ID          int64  `json:"id"`
	URL         string `json:"url"`
	RSSHubPath  string `json:"rsshubPath"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	LastError   string `json:"lastError"`
	Paused      bool   `json:"paused"`
	UnreadCount int    `json:"unreadCount"`
}

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

// imageClient waits longer than httpClient: the server itself may take 25 seconds to get a picture.
var imageClient = &http.Client{Timeout: 40 * time.Second}

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

// Image gets a picture found inside a feed through the server's image relay.
func (s Session) Image(ctx context.Context, address string) ([]byte, error) {
	resp, err := s.send(ctx, imageClient, http.MethodGet, "/api/image?url="+url.QueryEscape(address), nil)
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

// call sends a JSON request and decodes the JSON reply into out.
func (s Session) call(ctx context.Context, method, path string, in, out any) error {
	resp, err := s.send(ctx, httpClient, method, path, in)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out); err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	return nil
}

// send makes a request with the session's token and turns an error status into an error.
func (s Session) send(ctx context.Context, client *http.Client, method, path string, in any) (*http.Response, error) {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, s.Server+path, body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
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
