package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAddress(t *testing.T) {
	for input, want := range map[string]string{
		"home.lan":                   "http://home.lan:7470",
		" 192.168.1.20 ":             "http://192.168.1.20:7470",
		"home.lan:8080":              "http://home.lan:8080",
		"https://oriel.example.com/": "https://oriel.example.com",
		"http://home.lan/oriel/?x=1": "http://home.lan/oriel",
		"[::1]":                      "http://[::1]:7470",
	} {
		got, err := Address(input)
		if err != nil || got != want {
			t.Errorf("Address(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"", "   ", "ftp://home.lan", "http://"} {
		if _, err := Address(input); !errors.Is(err, ErrUnreachable) {
			t.Errorf("Address(%q) = %v; want ErrUnreachable", input, err)
		}
	}
}

// fake answers like `oriel serve` with the password "secret", which hands out the token "t1".
func fake(t *testing.T, health string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(health))
	})
	mux.HandleFunc("POST /auth/login", func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		if string(buf[:n]) != `{"password":"secret"}` {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"wrong_password"}`))
			return
		}
		w.Write([]byte(`{"token":"t1"}`))
	})
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t1" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		w.Write([]byte(`{}`))
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func TestProbe(t *testing.T) {
	ctx := context.Background()
	s := fake(t, `{"app":"oriel","version":"0.8.0","auth":true,"database":"ok"}`)
	server, err := Probe(ctx, s.URL+"/")
	if err != nil || server.URL != s.URL || !server.Auth {
		t.Fatalf("Probe = %+v, %v", server, err)
	}
	if _, err := Probe(ctx, fake(t, `<html>router</html>`).URL); !errors.Is(err, ErrNotOriel) {
		t.Errorf("not JSON: %v", err)
	}
	if _, err := Probe(ctx, fake(t, `{"app":"other"}`).URL); !errors.Is(err, ErrNotOriel) {
		t.Errorf("other app: %v", err)
	}
	if _, err := Probe(ctx, fake(t, `{"app":"oriel","database":"unwritable"}`).URL); !errors.Is(err, ErrUnwritable) {
		t.Errorf("unwritable: %v", err)
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	if _, err := Probe(ctx, closed.URL); !errors.Is(err, ErrUnreachable) {
		t.Errorf("closed: %v", err)
	}
}

func TestLoginAndCheck(t *testing.T) {
	ctx := context.Background()
	s := fake(t, `{"app":"oriel","auth":true,"database":"ok"}`)
	server := Server{URL: s.URL, Auth: true}
	if _, err := Login(ctx, server, ""); !errors.Is(err, ErrPasswordRequired) {
		t.Errorf("empty password: %v", err)
	}
	if _, err := Login(ctx, server, "nope"); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("wrong password: %v", err)
	}
	session, err := Login(ctx, server, "secret")
	if err != nil || session != (Session{Server: s.URL, Token: "t1"}) {
		t.Fatalf("Login = %+v, %v", session, err)
	}
	if err := Check(ctx, session); err != nil {
		t.Errorf("Check: %v", err)
	}
	if err := Check(ctx, Session{Server: s.URL, Token: "old"}); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("expired: %v", err)
	}

	open, err := Login(ctx, Server{URL: s.URL}, "ignored")
	if err != nil || open != (Session{Server: s.URL}) {
		t.Errorf("without password = %+v, %v", open, err)
	}
}

func TestSessionFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remote.json")
	if _, ok, err := LoadSession(path); ok || err != nil {
		t.Fatalf("missing file: ok %v, %v", ok, err)
	}
	want := Session{Server: "http://home.lan:7470", Token: "t1"}
	if err := SaveSession(path, want); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("mode %v, %v", info.Mode().Perm(), err)
		}
	}
	got, ok, err := LoadSession(path)
	if !ok || err != nil || got != want {
		t.Errorf("LoadSession = %+v, %v, %v", got, ok, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("left %d files behind", len(entries))
	}
	if err := ForgetSession(path); err != nil {
		t.Fatal(err)
	}
	if err := ForgetSession(path); err != nil {
		t.Errorf("forget twice: %v", err)
	}
	os.WriteFile(path, []byte("{"), 0o600)
	if _, ok, err := LoadSession(path); ok || err == nil {
		t.Errorf("broken file: ok %v, %v", ok, err)
	}
}

func TestTimelineAPI(t *testing.T) {
	ctx := context.Background()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" "+string(body))
		switch r.URL.Path {
		case "/api/entries":
			if r.URL.Query().Get("q") == "ab" {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"query_too_short"}`))
				return
			}
			w.Write([]byte(`{"entries":[{"id":7,"kind":"video","title":"T","publishedAt":null,"durationSec":95,
				"images":[{"url":"https://i/1.jpg","width":4,"height":3}],"createdAt":1700000000000}],"nextCursor":null}`))
		case "/api/sources":
			w.Write([]byte(`{"sources":[{"id":1,"title":"Feed","kind":"article","unreadCount":3,"lastError":"boom","fetchIntervalSec":900}]}`))
		case "/api/entries/7":
			w.Write([]byte(`{"id":7,"starred":true,"contentHtml":"<p>x</p>"}`))
		case "/api/entries/read":
			w.Write([]byte(`{"marked":4}`))
		case "/api/image":
			w.Write([]byte("PNG"))
		}
	}))
	defer srv.Close()
	session := Session{Server: srv.URL, Token: "t1"}

	page, err := session.Entries(ctx, Query{Kind: "video", Unread: true, Search: "go fmt", Limit: 50, Cursor: "1.2"})
	if err != nil || len(page.Entries) != 1 || page.Next != "" {
		t.Fatalf("Entries = %+v, %v", page, err)
	}
	if e := page.Entries[0]; e.DurationSec != 95 || e.PublishedAt != 0 || e.Images[0].Width != 4 || e.CreatedAt != 1700000000000 {
		t.Errorf("entry = %+v", e)
	}
	var apiErr *APIError
	if _, err := session.Entries(ctx, Query{Search: "ab"}); !errors.As(err, &apiErr) || apiErr.Code != "query_too_short" {
		t.Errorf("short query: %v", err)
	}
	sources, err := session.Sources(ctx)
	if err != nil || len(sources) != 1 || sources[0].UnreadCount != 3 || sources[0].LastError != "boom" {
		t.Errorf("Sources = %+v, %v", sources, err)
	}
	starred := true
	if entry, err := session.SetEntry(ctx, 7, EntryPatch{Starred: &starred}); err != nil || !entry.Starred {
		t.Errorf("SetEntry = %+v, %v", entry, err)
	}
	if marked, err := session.MarkRead(ctx, ReadScope{Kind: "video", UpTo: 7}); err != nil || marked != 4 {
		t.Errorf("MarkRead = %d, %v", marked, err)
	}
	if body, err := session.Image(ctx, "https://i/1.jpg?w=2&h=1"); err != nil || string(body) != "PNG" {
		t.Errorf("Image = %q, %v", body, err)
	}
	want := []string{
		"GET /api/entries?cursor=1.2&kind=video&limit=50&q=go+fmt&unread=1 ",
		"GET /api/entries?q=ab ",
		"GET /api/sources ",
		`PATCH /api/entries/7 {"starred":true}`,
		`POST /api/entries/read {"kind":"video","upTo":7}`,
		"GET /api/image?url=https%3A%2F%2Fi%2F1.jpg%3Fw%3D2%26h%3D1 ",
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
	if _, err := (Session{Server: srv.URL, Token: "old"}).Sources(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("expired session: %v", err)
	}
	if _, err := (Session{Server: "http://127.0.0.1:1"}).Sources(ctx); !errors.Is(err, ErrUnreachable) {
		t.Errorf("no server: %v", err)
	}
}
