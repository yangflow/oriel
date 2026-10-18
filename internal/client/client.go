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
