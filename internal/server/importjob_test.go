package server

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/importer"
	"github.com/yangflow/oriel/internal/store"
)

const importToken = "fake-import-token-4242"

// otherReader serves a small Miniflux: two feeds (one with an address Oriel refuses), two starred entries of the first
// feed and one of a feed it does not list, and two read entries of the first feed. block, when set, holds the entry
// calls until the request ends.
func otherReader(t *testing.T, block <-chan struct{}) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Auth-Token") != importToken {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		entry := func(url, status string, starred bool, feed string) string {
			return fmt.Sprintf(`{"url":%q,"title":"T","status":%q,"starred":%t,"published_at":"2024-05-01T10:00:00Z","content":"<p>x</p>","feed":{"feed_url":%q}}`,
				url, status, starred, feed)
		}
		const a = "https://a.example/feed.xml"
		switch {
		case r.URL.Path == "/v1/feeds":
			fmt.Fprintf(w, `[{"feed_url":%q,"title":"A","category":{"title":"News"}},{"feed_url":"ftp://b.example/feed","title":"B","category":{"title":"Tech"}}]`, a)
		case r.URL.Query().Get("starred") == "true":
			if block != nil {
				select {
				case <-block:
				case <-r.Context().Done():
					return
				}
			}
			fmt.Fprintf(w, `{"total":3,"entries":[%s,%s,%s]}`, entry("https://a.example/s/1", "unread", true, a),
				entry("https://a.example/r/1", "read", true, a), entry("https://c.example/s/3", "unread", true, "https://c.example/feed"))
		default:
			fmt.Fprintf(w, `{"total":2,"entries":[%s,%s]}`, entry("https://a.example/r/1", "read", false, a), entry("https://a.example/r/2", "read", false, a))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func importServer(t *testing.T) (*Server, map[string]string) {
	t.Helper()
	s, _ := newServer(t, "secret")
	s.importClient = func() *importer.Client {
		c := importer.NewClient()
		c.Spacing = 0
		return c
	}
	return s, bearer(login(t, s, "laptop"))
}

func startBody(srv *httptest.Server, token string) string {
	return fmt.Sprintf(`{"service":"miniflux","baseUrl":%q,"credential":%q}`, srv.URL+"/", token)
}

func waitPhase(t *testing.T, s *Server, header map[string]string, phases ...string) map[string]any {
	t.Helper()
	for range 500 {
		body := decode(t, do(s, "GET", "/api/import", "", header))
		for _, p := range phases {
			if body["phase"] == p {
				return body
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no phase of %v", phases)
	return nil
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestImportFromReader(t *testing.T) {
	logs := &syncBuffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })

	s, token := importServer(t)
	srv := otherReader(t, nil)
	for _, r := range []struct{ method, target string }{{"POST", "/api/import/start"}, {"GET", "/api/import"}, {"POST", "/api/import/confirm"}, {"POST", "/api/import/cancel"}} {
		if w := do(s, r.method, r.target, "{}", nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s without session: %d", r.target, w.Code)
		}
	}
	if body := decode(t, do(s, "GET", "/api/import", "", token)); body["phase"] != "idle" {
		t.Fatalf("first status: %v", body)
	}
	for body, code := range map[string]string{
		`{"service":"feedly","baseUrl":"https://x.example","credential":"k"}`:   "invalid_service",
		`{"service":"miniflux","baseUrl":"ftp://x.example","credential":"k"}`:   "invalid_base_url",
		`{"service":"miniflux","baseUrl":"https://x.example","credential":" "}`: "missing_credential",
	} {
		if w := do(s, "POST", "/api/import/start", body, token); w.Code != http.StatusBadRequest || decode(t, w)["error"] != code {
			t.Fatalf("%s: %d %s", code, w.Code, w.Body)
		}
	}
	if w := do(s, "POST", "/api/import/confirm", "", token); w.Code != http.StatusConflict {
		t.Fatalf("confirm without a preview: %d", w.Code)
	}

	// An existing subscription with an entry that is stored and unread.
	do(s, "POST", "/api/sources", `{"url":"https://a.example/feed.xml","kind":"article"}`, token)
	id, err := s.store.SourceIDByFeedURL(context.Background(), "https://a.example/feed.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.ImportEntries(context.Background(), id, []store.ImportedEntry{{URL: "https://a.example/r/1", Starred: true}}, s.now()); err != nil {
		t.Fatal(err)
	}

	w := do(s, "POST", "/api/import/start", startBody(srv, importToken), token)
	if w.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	preview := waitPhase(t, s, token, "ready", "failed")
	if preview["phase"] != "ready" || preview["counts"].(map[string]any)["sources"] != 2.0 ||
		preview["counts"].(map[string]any)["folders"] != 2.0 || preview["counts"].(map[string]any)["starred"] != 3.0 ||
		preview["counts"].(map[string]any)["read"] != 2.0 {
		t.Fatalf("preview: %v", preview)
	}
	if got := subscriptions(t, s, token); len(got) != 1 {
		t.Fatalf("the preview wrote subscriptions: %v", got)
	}
	if w := do(s, "POST", "/api/import/start", startBody(srv, importToken), token); w.Code != http.StatusConflict || decode(t, w)["error"] != "import_busy" {
		t.Fatalf("second start: %d %s", w.Code, w.Body)
	}

	if w := do(s, "POST", "/api/import/confirm", "", token); w.Code != http.StatusOK {
		t.Fatalf("confirm: %d %s", w.Code, w.Body)
	}
	done := waitPhase(t, s, token, "done", "failed")
	result, _ := done["result"].(map[string]any)
	want := map[string]float64{"addedSources": 0, "existingSources": 1, "starredAdded": 1, "starredExisting": 1, "readMarked": 1, "unmatchedRead": 1, "foldersSkipped": 2, "skippedTotal": 2}
	for k, v := range want {
		if result[k] != v {
			t.Fatalf("%s = %v, want %v (%v)", k, result[k], v, done)
		}
	}
	skipped := result["skipped"].([]any)
	if len(skipped) != 2 || skipped[0].(map[string]any)["error"] != "invalid_url" || skipped[1].(map[string]any)["error"] != "unknown_source" {
		t.Fatalf("skipped = %v", skipped)
	}

	// The same import again adds nothing.
	do(s, "POST", "/api/import/start", startBody(srv, importToken), token)
	waitPhase(t, s, token, "ready")
	do(s, "POST", "/api/import/confirm", "", token)
	again := waitPhase(t, s, token, "done")["result"].(map[string]any)
	for _, k := range []string{"addedSources", "starredAdded", "readMarked"} {
		if again[k] != 0.0 {
			t.Fatalf("second import %s = %v", k, again[k])
		}
	}

	// The credential is in no log, response or stored row.
	entries := do(s, "GET", "/api/entries", "", token).Body.String()
	if !strings.Contains(entries, "a.example/s/1") {
		t.Fatalf("starred entry not stored: %s", entries)
	}
	for name, text := range map[string]string{
		"logs":     logs.String(),
		"entries":  entries,
		"sources":  do(s, "GET", "/api/sources", "", token).Body.String(),
		"settings": do(s, "GET", "/api/settings", "", token).Body.String(),
		"status":   do(s, "GET", "/api/import", "", token).Body.String(),
		"sessions": do(s, "GET", "/api/sessions", "", token).Body.String(),
		"start":    w.Body.String(),
	} {
		if strings.Contains(text, importToken) {
			t.Errorf("the credential is in the %s", name)
		}
	}
}

func TestImportFailsWithACode(t *testing.T) {
	logs := &syncBuffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	s, token := importServer(t)
	srv := otherReader(t, nil)
	const wrong = "fake-wrong-token-7777"
	do(s, "POST", "/api/import/start", startBody(srv, wrong), token)
	failed := waitPhase(t, s, token, "failed")
	if failed["error"] != "unauthorized" {
		t.Fatalf("status = %v", failed)
	}
	for name, text := range map[string]string{"logs": logs.String(), "status": do(s, "GET", "/api/import", "", token).Body.String()} {
		if strings.Contains(text, wrong) {
			t.Errorf("the credential is in the %s", name)
		}
	}
	if w := do(s, "POST", "/api/import/confirm", "", token); w.Code != http.StatusConflict {
		t.Fatalf("confirm after failure: %d", w.Code)
	}
	// Cancel clears a finished job; a new one can start.
	if body := decode(t, do(s, "POST", "/api/import/cancel", "", token)); body["phase"] != "idle" {
		t.Fatalf("cancel: %v", body)
	}
	do(s, "POST", "/api/import/start", startBody(srv, importToken), token)
	waitPhase(t, s, token, "ready")
}

func TestImportCancel(t *testing.T) {
	s, token := importServer(t)
	block := make(chan struct{})
	srv := otherReader(t, block)
	do(s, "POST", "/api/import/start", startBody(srv, importToken), token)
	waitPhase(t, s, token, "fetching")
	if body := decode(t, do(s, "POST", "/api/import/cancel", "", token)); body["phase"] != "canceled" {
		t.Fatalf("cancel: %v", body)
	}
	if w := do(s, "POST", "/api/import/confirm", "", token); w.Code != http.StatusConflict {
		t.Fatalf("confirm after cancel: %d", w.Code)
	}
	time.Sleep(50 * time.Millisecond)
	if body := decode(t, do(s, "GET", "/api/import", "", token)); body["phase"] != "canceled" {
		t.Fatalf("a canceled import came back: %v", body)
	}
	if got := subscriptions(t, s, token); len(got) != 0 {
		t.Fatalf("a canceled import wrote: %v", got)
	}

	// Cancelling a preview drops it; nothing is written.
	close(block)
	do(s, "POST", "/api/import/start", startBody(srv, importToken), token)
	waitPhase(t, s, token, "ready")
	do(s, "POST", "/api/import/cancel", "", token)
	if w := do(s, "POST", "/api/import/confirm", "", token); w.Code != http.StatusConflict {
		t.Fatalf("confirm after cancelling the preview: %d", w.Code)
	}
	if got := subscriptions(t, s, token); len(got) != 0 {
		t.Fatalf("a canceled preview wrote: %v", got)
	}
}
