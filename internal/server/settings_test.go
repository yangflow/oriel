package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/yangflow/oriel/internal/fetch"
)

func TestSettings(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	ctx := context.Background()
	for _, method := range []string{"GET", "PATCH"} {
		if w := do(s, method, "/api/settings", `{}`, nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s without session: %d", method, w.Code)
		}
	}
	// A secret in the settings table never leaves the server.
	s.store.SetSetting(ctx, "ai_api_key", "stored-secret-value")
	if w := do(s, "GET", "/api/settings", "", bearer(token)); w.Code != http.StatusOK ||
		strings.TrimSpace(w.Body.String()) != `{"rsshubBaseUrl":"","retentionDays":0}` {
		t.Fatalf("defaults: %d %s", w.Code, w.Body)
	}

	w := do(s, "PATCH", "/api/settings", `{"rsshubBaseUrl":" http://localhost:1200/ ","retentionDays":30}`, bearer(token))
	if body := decode(t, w); w.Code != http.StatusOK || body["rsshubBaseUrl"] != "http://localhost:1200/" || body["retentionDays"] != 30.0 {
		t.Fatalf("patch: %d %v", w.Code, body)
	}
	// The fetch loop reads these keys on every run, so new values apply from the next fetch and prune.
	if root, _ := s.store.Setting(ctx, fetch.RSSHubSetting); root != "http://localhost:1200/" {
		t.Fatalf("stored root = %q", root)
	}
	if days, _ := s.store.Setting(ctx, fetch.RetentionSetting); days != "30" {
		t.Fatalf("stored retention = %q", days)
	}
	if body := decode(t, do(s, "PATCH", "/api/settings", `{"retentionDays":0}`, bearer(token))); body["rsshubBaseUrl"] != "http://localhost:1200/" || body["retentionDays"] != 0.0 {
		t.Fatalf("only the given field changes: %v", body)
	}
	if body := decode(t, do(s, "PATCH", "/api/settings", `{"rsshubBaseUrl":""}`, bearer(token))); body["rsshubBaseUrl"] != "" {
		t.Fatalf("clear root: %v", body)
	}

	for body, code := range map[string]string{
		`{"rsshubBaseUrl":"ftp://example.org"}`:          "invalid_rsshub_base_url",
		`{"rsshubBaseUrl":"localhost:1200"}`:             "invalid_rsshub_base_url",
		`{"retentionDays":-1}`:                           "invalid_retention_days",
		`{"retentionDays":3651}`:                         "invalid_retention_days",
		`{"retentionDays":"30"}`:                         "invalid_body",
		`{"retentionDays":1.5}`:                          "invalid_body",
		`{"aiApiKey":"x"}`:                               "invalid_body",
		`{"retentionDays":7,"rsshubBaseUrl":"file:///"}`: "invalid_rsshub_base_url",
	} {
		w := do(s, "PATCH", "/api/settings", body, bearer(token))
		if w.Code != http.StatusBadRequest || decode(t, w)["error"] != code {
			t.Errorf("%s: %d %s", body, w.Code, w.Body)
		}
	}
	// A rejected request changes nothing, not even its valid fields.
	if days, _ := s.store.Setting(ctx, fetch.RetentionSetting); days != "0" {
		t.Fatalf("rejected patch stored retention %q", days)
	}

	// A stored value the fetch loop reads as forever reads back as 0.
	s.store.SetSetting(ctx, fetch.RetentionSetting, "a week")
	if body := decode(t, do(s, "GET", "/api/settings", "", bearer(token))); body["retentionDays"] != 0.0 {
		t.Fatalf("unreadable retention: %v", body)
	}
}
