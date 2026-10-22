package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/ai"
	"github.com/yangflow/oriel/internal/diag"
	"github.com/yangflow/oriel/internal/store"
)

func TestDiagnostics(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "oriel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	logs := diag.NewHandler(slog.NewTextHandler(io.Discard, nil), diag.Keep)
	s, err := New(st, Config{Password: "secret", Version: "1.2.3", Log: logs, Mode: diag.ModeHeadless})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	src, err := st.CreateSource(ctx, store.NewSource{URL: "https://feeds.example.com/private/feed.xml?token=abc", Kind: "article",
		FetchInterval: time.Hour}, "My Private Diary", at)
	if err != nil {
		t.Fatal(err)
	}
	failure := `Get "https://feeds.example.com/private/feed.xml?token=abc": EOF`
	if err := st.FetchFailure(ctx, src.ID, failure, "", at, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(ctx, ai.APIKeySetting, "sk-seeded-ai-key"); err != nil {
		t.Fatal(err)
	}
	log := slog.New(logs)
	log.Warn("Cannot reach https://api.example.com/v1/chat?token=abc&user=me", "authorization", "Bearer sk-seeded-ai-key",
		"address", "https://feeds.example.com/private/feed.xml?token=abc", "error", failure)
	log.Info("Serving", "data", "/home/someone/oriel-data")

	if w := do(s, "GET", "/api/diagnostics", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("without a session: %d", w.Code)
	}
	session := login(t, s, "test")
	w := do(s, "GET", "/api/diagnostics", "", bearer(session))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("%d %q", w.Code, w.Header().Get("Content-Type"))
	}
	body := w.Body.String()
	for _, secret := range []string{"secret", session, "sk-seeded-ai-key", "token=abc", "/private/feed.xml", "My Private Diary",
		"user=me", "/v1/chat", "someone", "Serving", "Bearer"} {
		if strings.Contains(body, secret) {
			t.Errorf("the file has %q:\n%s", secret, body)
		}
	}
	for _, want := range []string{"Version: 1.2.3", "Mode: headless", "WARN Cannot reach https://api.example.com",
		"host=feeds.example.com", "consecutive_failures=1", "last_success_at=never"} {
		if !strings.Contains(body, want) {
			t.Errorf("the file lacks %q:\n%s", want, body)
		}
	}
}
