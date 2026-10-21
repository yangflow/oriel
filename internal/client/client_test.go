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
			w.Write([]byte(`{"entries":[{"id":7,"kind":"video","title":"T","publishedAt":null,"durationSec":95,"readingSec":600,
				"images":[{"url":"https://i/1.jpg","width":4,"height":3}],"createdAt":1700000000000}],"nextCursor":null}`))
		case "/api/sources":
			w.Write([]byte(`{"sources":[{"id":1,"title":"Feed","kind":"article","unreadCount":3,"lastError":"boom","fetchIntervalSec":900}]}`))
		case "/api/entries/7":
			w.Write([]byte(`{"id":7,"starred":true,"readProgress":0.5,"extractStatus":"ok","contentHtml":"<p>x</p>","extractedHtml":"<p>y</p>"}`))
		case "/api/entries/7/extract":
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"id":7,"extractStatus":"pending","contentHtml":"<p>x</p>"}`))
		case "/api/entries/read":
			w.Write([]byte(`{"marked":2,"ids":[7,8]}`))
		case "/api/entries/unread":
			w.Write([]byte(`{"marked":2}`))
		case "/api/image":
			w.Write([]byte("PNG"))
		case "/api/sources/5", "/api/sources/5/refresh":
			if r.Method == http.MethodPatch {
				w.Write([]byte(`{"id":5,"title":"New","kind":"video","paused":true,"lastFetchedAt":null}`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case "/api/sources/6/refresh":
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":"source_paused"}`))
		case "/api/settings":
			w.Write([]byte(`{"rsshubBaseUrl":"https://rsshub.app","retentionDays":30}`))
		}
	}))
	defer srv.Close()
	session := Session{Server: srv.URL, Token: "t1"}

	page, err := session.Entries(ctx, Query{Kind: "video", Unread: true, Search: "go fmt", MaxMinutes: 10, Limit: 50, Cursor: "1.2"})
	if err != nil || len(page.Entries) != 1 || page.Next != "" {
		t.Fatalf("Entries = %+v, %v", page, err)
	}
	if e := page.Entries[0]; e.DurationSec != 95 || e.ReadingSec != 600 || e.PublishedAt != 0 || e.Images[0].Width != 4 || e.CreatedAt != 1700000000000 {
		t.Errorf("entry = %+v", e)
	}
	var apiErr *APIError
	if _, err := session.Entries(ctx, Query{Search: "ab"}); !errors.As(err, &apiErr) || apiErr.Code != "query_too_short" {
		t.Errorf("short query: %v", err)
	}
	if article, err := session.RetryExtraction(ctx, 7); err != nil || article.ID != 7 || article.ExtractStatus != "pending" {
		t.Errorf("RetryExtraction = %+v, %v", article, err)
	}
	sources, err := session.Sources(ctx)
	if err != nil || len(sources) != 1 || sources[0].UnreadCount != 3 || sources[0].LastError != "boom" ||
		sources[0].FetchIntervalSec != 900 {
		t.Errorf("Sources = %+v, %v", sources, err)
	}
	if article, err := session.Entry(ctx, 7); err != nil || article.ID != 7 || article.ExtractStatus != "ok" ||
		article.ContentHTML != "<p>x</p>" || article.ExtractedHTML != "<p>y</p>" {
		t.Errorf("Entry = %+v, %v", article, err)
	}
	starred := true
	if entry, err := session.SetEntry(ctx, 7, EntryPatch{Starred: &starred}); err != nil || !entry.Starred {
		t.Errorf("SetEntry = %+v, %v", entry, err)
	}
	progress := 0.5
	if entry, err := session.SetEntry(ctx, 7, EntryPatch{ReadProgress: &progress}); err != nil || entry.ReadProgress == nil || *entry.ReadProgress != 0.5 {
		t.Errorf("SetEntry progress = %+v, %v", entry, err)
	}
	ids, err := session.MarkRead(ctx, ReadScope{Kind: "video", UpTo: 7})
	if err != nil || len(ids) != 2 || ids[0] != 7 || ids[1] != 8 {
		t.Errorf("MarkRead = %v, %v", ids, err)
	}
	if marked, err := session.MarkUnread(ctx, ids); err != nil || marked != 2 {
		t.Errorf("MarkUnread = %d, %v", marked, err)
	}
	if body, err := session.Image(ctx, "https://i/1.jpg?w=2&h=1"); err != nil || string(body) != "PNG" {
		t.Errorf("Image = %q, %v", body, err)
	}
	if _, err := session.AddSource(ctx, NewSource{RSSHubPath: "/x/y", Kind: "social"}); err != nil {
		t.Errorf("AddSource: %v", err)
	}
	title, paused := "New", true
	if src, err := session.UpdateSource(ctx, 5, SourcePatch{Title: &title, Paused: &paused}); err != nil ||
		src.Title != "New" || !src.Paused || src.LastFetchedAt != 0 {
		t.Errorf("UpdateSource = %+v, %v", src, err)
	}
	if err := session.RefreshSource(ctx, 5); err != nil {
		t.Errorf("RefreshSource: %v", err)
	}
	if err := session.RefreshSource(ctx, 6); !errors.As(err, &apiErr) || apiErr.Code != "source_paused" {
		t.Errorf("RefreshSource paused: %v", err)
	}
	if err := session.DeleteSource(ctx, 5); err != nil {
		t.Errorf("DeleteSource: %v", err)
	}
	if settings, err := session.Settings(ctx); err != nil || settings.RetentionDays != 30 {
		t.Errorf("Settings = %+v, %v", settings, err)
	}
	root := "https://rsshub.app"
	if settings, err := session.SetSettings(ctx, SettingsPatch{RSSHubBaseURL: &root}); err != nil ||
		settings.RSSHubBaseURL != root {
		t.Errorf("SetSettings = %+v, %v", settings, err)
	}
	want := []string{
		"GET /api/entries?cursor=1.2&kind=video&limit=50&maxMinutes=10&q=go+fmt&unread=1 ",
		"GET /api/entries?q=ab ",
		"POST /api/entries/7/extract ",
		"GET /api/sources ",
		"GET /api/entries/7 ",
		`PATCH /api/entries/7 {"starred":true}`,
		`PATCH /api/entries/7 {"readProgress":0.5}`,
		`POST /api/entries/read {"kind":"video","upTo":7}`,
		`POST /api/entries/unread {"ids":[7,8]}`,
		"GET /api/image?url=https%3A%2F%2Fi%2F1.jpg%3Fw%3D2%26h%3D1 ",
		`POST /api/sources {"rsshubPath":"/x/y","kind":"social"}`,
		`PATCH /api/sources/5 {"title":"New","paused":true}`,
		"POST /api/sources/5/refresh ",
		"POST /api/sources/6/refresh ",
		"DELETE /api/sources/5 ",
		"GET /api/settings ",
		`PATCH /api/settings {"rsshubBaseUrl":"https://rsshub.app"}`,
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

func TestSettingsAPI(t *testing.T) {
	ctx := context.Background()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.Path+" "+r.Header.Get("Content-Type")+" "+r.UserAgent()+" "+string(body))
		switch r.URL.Path {
		case "/api/sessions":
			w.Write([]byte(`{"sessions":[{"id":2,"userAgent":"Oriel/1.0 (macOS)","createdAt":1,"lastSeenAt":2,"current":true}]}`))
		case "/api/sessions/3":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"not_found"}`))
		case "/api/opml/import":
			w.Write([]byte(`{"added":2,"existing":1,"failed":[{"title":"G","address":"ftp://x","error":"invalid_url"}]}`))
		case "/api/opml/export":
			w.Header().Set("Content-Type", "text/x-opml")
			w.Write([]byte("<opml/>"))
		}
	}))
	defer srv.Close()
	SetVersion("1.0")
	session := Session{Server: srv.URL}

	devices, err := session.Devices(ctx)
	if err != nil || len(devices) != 1 || devices[0] != (Device{ID: 2, UserAgent: "Oriel/1.0 (macOS)", CreatedAt: 1,
		LastSeenAt: 2, Current: true}) {
		t.Errorf("Devices = %+v, %v", devices, err)
	}
	var apiErr *APIError
	if err := session.RevokeDevice(ctx, 3); !errors.As(err, &apiErr) || apiErr.Code != "not_found" {
		t.Errorf("RevokeDevice: %v", err)
	}
	imported, err := session.ImportOPML(ctx, []byte("<opml/>"))
	if err != nil || imported.Added != 2 || imported.Existing != 1 ||
		imported.Failed[0] != (ImportFail{Title: "G", Address: "ftp://x", Error: "invalid_url"}) {
		t.Errorf("ImportOPML = %+v, %v", imported, err)
	}
	if opml, err := session.ExportOPML(ctx); err != nil || string(opml) != "<opml/>" {
		t.Errorf("ExportOPML = %q, %v", opml, err)
	}
	agent := "Oriel/1.0 (" + platform() + ")"
	want := []string{
		"GET /api/sessions  " + agent + " ",
		"DELETE /api/sessions/3  " + agent + " ",
		"POST /api/opml/import text/x-opml " + agent + " <opml/>",
		"GET /api/opml/export  " + agent + " ",
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
}

func TestAIAPI(t *testing.T) {
	ctx := context.Background()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.Path+" "+string(body))
		switch r.URL.Path {
		case "/api/entries/7/ai":
			w.Write([]byte(`{"summary":{"status":"ok","text":"Short.","error":"","model":"m","updatedAt":1},"translation":null}`))
		case "/api/entries/7/translation":
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"status":"pending","text":"","error":"","model":"","updatedAt":null}`))
		case "/api/entries/8/summary":
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":"ai_not_configured"}`))
		case "/api/ai/test":
			w.Write([]byte(`{"model":"m-1"}`))
		case "/api/ai/selection":
			if strings.Contains(string(body), "too long") {
				w.WriteHeader(http.StatusBadGateway)
				w.Write([]byte(`{"error":"ai_timeout"}`))
				return
			}
			w.Write([]byte(`{"text":"你好","model":"m-1"}`))
		case "/api/settings":
			w.Write([]byte(`{"aiBaseUrl":"http://x/v1","aiModel":"m","aiApiKeySet":true,"aiLanguage":"en"}`))
		}
	}))
	defer srv.Close()
	session := Session{Server: srv.URL}

	results, err := session.AI(ctx, 7)
	if err != nil || results.Summary == nil || *results.Summary != (AIResult{Status: "ok", Text: "Short.", Model: "m"}) ||
		results.Translation != nil {
		t.Errorf("AI = %+v, %v", results, err)
	}
	if result, err := session.RequestAI(ctx, 7, "translation"); err != nil || result.Status != "pending" {
		t.Errorf("RequestAI = %+v, %v", result, err)
	}
	var apiErr *APIError
	if _, err := session.RequestAI(ctx, 8, "summary"); !errors.As(err, &apiErr) || apiErr.Code != "ai_not_configured" {
		t.Errorf("RequestAI not configured: %v", err)
	}
	if model, err := session.TestAI(ctx); err != nil || model != "m-1" {
		t.Errorf("TestAI = %q, %v", model, err)
	}
	if text, model, err := session.Selection(ctx, "translate", "hello", "Greetings"); err != nil || text != "你好" || model != "m-1" {
		t.Errorf("Selection = %q, %q, %v", text, model, err)
	}
	if _, _, err := session.Selection(ctx, "explain", "too long", ""); !errors.As(err, &apiErr) || apiErr.Code != "ai_timeout" {
		t.Errorf("Selection upstream failure: %v", err)
	}
	key := ""
	settings, err := session.SetSettings(ctx, SettingsPatch{AIAPIKey: &key})
	if err != nil || !settings.AIConfigured() || !settings.AIAPIKeySet || settings.AILanguage != "en" {
		t.Errorf("SetSettings = %+v, %v", settings, err)
	}
	on := true
	session.UpdateSource(ctx, 5, SourcePatch{AutoSummary: &on, PromptSummary: &key})
	want := []string{
		"GET /api/entries/7/ai ",
		"POST /api/entries/7/translation ",
		"POST /api/entries/8/summary ",
		"POST /api/ai/test ",
		`POST /api/ai/selection {"task":"translate","text":"hello","title":"Greetings"}`,
		`POST /api/ai/selection {"task":"explain","text":"too long","title":""}`,
		`PATCH /api/settings {"aiApiKey":""}`,
		`PATCH /api/sources/5 {"promptSummary":"","autoSummary":true}`,
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
}
