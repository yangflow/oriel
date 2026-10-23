package server

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

func addSource(t *testing.T, s *Server, url string) {
	t.Helper()
	if _, err := s.store.CreateSource(context.Background(), store.NewSource{URL: url, Kind: "article", FetchInterval: time.Hour}, "", time.UnixMilli(1_700_000_000_000)); err != nil {
		t.Fatal(err)
	}
}

func sourceList(t *testing.T, s *Server, token string) string {
	t.Helper()
	w := do(s, "GET", "/api/sources", "", bearer(token))
	if w.Code != http.StatusOK {
		t.Fatalf("sources: %d %s", w.Code, w.Body)
	}
	return w.Body.String()
}

func TestBackupAndRestoreThroughTheAPI(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	for _, r := range []struct{ method, target string }{{"GET", "/api/backup"}, {"POST", "/api/restore"}} {
		if w := do(s, r.method, r.target, "", nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s without session: %d", r.target, w.Code)
		}
	}
	addSource(t, s, "https://example.org/feed")
	addSource(t, s, "https://example.org/talks")

	w := do(s, "GET", "/api/backup", "", bearer(token))
	disposition := w.Header().Get("Content-Disposition")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/zip" ||
		!strings.HasPrefix(disposition, `attachment; filename="Oriel-`) || !strings.HasSuffix(disposition, `.oriel-backup"`) {
		t.Fatalf("backup: %d %v", w.Code, w.Header())
	}
	file := w.Body.String()
	if _, err := zip.NewReader(strings.NewReader(file), int64(len(file))); err != nil {
		t.Fatal(err)
	}

	// Another server, with its own data and session, takes the backup in.
	other, _ := newServer(t, "secret")
	mine := login(t, other, "desktop")
	addSource(t, other, "https://elsewhere.example/feed")
	var events []string
	other.pause = func(context.Context) (func(), error) {
		events = append(events, "pause")
		return func() { events = append(events, "resume") }, nil
	}
	w = do(other, "POST", "/api/restore", file, bearer(mine))
	body := decode(t, w)
	if w.Code != http.StatusOK || body["sources"] != 2.0 || body["entries"] != 0.0 {
		t.Fatalf("restore: %d %v", w.Code, body)
	}
	if strings.Join(events, " ") != "pause resume" {
		t.Fatalf("events %v", events)
	}
	got := sourceList(t, other, mine)
	if !strings.Contains(got, "https://example.org/feed") || strings.Contains(got, "elsewhere") {
		t.Fatalf("sources after restore: %s", got)
	}
}

func TestRestoreRefusals(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	addSource(t, s, "https://example.org/feed")
	paused := false
	s.pause = func(context.Context) (func(), error) { paused = true; return func() {}, nil }

	for _, tc := range []struct{ name, body string }{{"not a backup", "just text"}, {"empty", ""}} {
		w := do(s, "POST", "/api/restore", tc.body, bearer(token))
		if w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_backup" {
			t.Fatalf("%s: %d %s", tc.name, w.Code, w.Body)
		}
	}
	if paused {
		t.Fatal("a refused file stopped the background work")
	}
	if got := sourceList(t, s, token); !strings.Contains(got, "https://example.org/feed") {
		t.Fatalf("data changed: %s", got)
	}

	s.restoring.Lock()
	w := do(s, "POST", "/api/restore", "x", bearer(token))
	s.restoring.Unlock()
	if w.Code != http.StatusConflict || decode(t, w)["error"] != "restore_running" {
		t.Fatalf("second restore: %d %s", w.Code, w.Body)
	}
}

func TestRestoreRefusesANewerBackup(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	m, _ := zw.Create("manifest.json")
	m.Write([]byte(`{"format":99,"app":"oriel","version":"9.9.9","schema":99}`))
	d, _ := zw.Create("oriel.sqlite")
	d.Write([]byte("whatever"))
	zw.Close()

	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	w := do(s, "POST", "/api/restore", buf.String(), bearer(token))
	if w.Code != http.StatusUnprocessableEntity || decode(t, w)["error"] != "backup_newer" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestBackupFailureBeforeAnyByteIsAJSONError(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "oriel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(st, Config{Version: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	w := do(s, "GET", "/api/backup", "", nil)
	if w.Code != http.StatusInternalServerError || decode(t, w)["error"] != "internal" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
