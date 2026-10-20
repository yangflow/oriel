package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/ai"
	"github.com/yangflow/oriel/internal/store"
)

func TestAI(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	ctx := context.Background()
	entries, err := s.store.Entries(ctx, store.EntryFilter{SourceID: seed(t, s, "social", 1, c.t)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(entries[0].ID, 10)
	for _, route := range []string{"GET /api/entries/" + id + "/ai", "POST /api/entries/" + id + "/summary", "POST /api/entries/" + id + "/translation", "POST /api/ai/test"} {
		method, path, _ := strings.Cut(route, " ")
		if w := do(s, method, path, "", nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s without session: %d", route, w.Code)
		}
	}
	if w := do(s, "POST", "/api/entries/"+id+"/summary", "", bearer(token)); w.Code != http.StatusConflict || decode(t, w)["error"] != "ai_not_configured" {
		t.Fatalf("not configured: %d %s", w.Code, w.Body)
	}
	if w := do(s, "POST", "/api/ai/test", "", bearer(token)); w.Code != http.StatusConflict || decode(t, w)["error"] != "ai_not_configured" {
		t.Fatalf("test not configured: %d %s", w.Code, w.Body)
	}
	if w := do(s, "GET", "/api/entries/"+id+"/ai", "", bearer(token)); w.Code != http.StatusOK || w.Body.String() != "{\"summary\":null,\"translation\":null,\"targetLanguage\":\"zh-Hans\"}\n" {
		t.Fatalf("no results: %d %s", w.Code, w.Body)
	}
	s.store.SetSetting(ctx, ai.LanguageSetting, "en")
	if got := decode(t, do(s, "GET", "/api/entries/"+id+"/ai", "", bearer(token))); got["targetLanguage"] != "en" {
		t.Fatalf("target language: %v", got)
	}
	s.store.SetSetting(ctx, ai.LanguageSetting, "zh-Hans")

	var status atomic.Int32
	status.Store(http.StatusOK)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(status.Load()))
		json.NewEncoder(w).Encode(map[string]any{"model": "m", "choices": []any{map[string]any{"message": map[string]string{"content": "a summary"}}}})
	}))
	defer upstream.Close()
	s.store.SetSetting(ctx, ai.BaseURLSetting, upstream.URL)
	s.store.SetSetting(ctx, ai.ModelSetting, "m")

	for path, code := range map[string]string{"/api/entries/x/summary": "invalid_id", "/api/entries/999/summary": "not_found", "/api/entries/999/translation": "not_found"} {
		if w := do(s, "POST", path, "", bearer(token)); decode(t, w)["error"] != code {
			t.Errorf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	if w := do(s, "GET", "/api/entries/999/ai", "", bearer(token)); w.Code != http.StatusNotFound {
		t.Fatalf("missing entry: %d", w.Code)
	}

	w := do(s, "POST", "/api/entries/"+id+"/summary", "", bearer(token))
	if body := decode(t, w); w.Code != http.StatusAccepted || body["status"] != "pending" || body["text"] != "" || body["error"] != "" {
		t.Fatalf("queued: %d %v", w.Code, body)
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		s.ai.Run(runCtx)
		close(done)
	}()
	defer func() {
		stop()
		<-done
	}()
	var summary map[string]any
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		body := decode(t, do(s, "GET", "/api/entries/"+id+"/ai", "", bearer(token)))
		if summary, _ = body["summary"].(map[string]any); summary != nil && summary["status"] == "ok" {
			if body["translation"] != nil {
				t.Fatalf("translation appeared: %v", body)
			}
			break
		}
	}
	if summary["text"] != "a summary" || summary["model"] != "m" || summary["updatedAt"] == nil {
		t.Fatalf("summary: %v", summary)
	}
	if w := do(s, "POST", "/api/entries/"+id+"/summary", "", bearer(token)); w.Code != http.StatusOK || decode(t, w)["text"] != "a summary" {
		t.Fatalf("cached: %d %s", w.Code, w.Body)
	}

	if w := do(s, "POST", "/api/ai/test", "", bearer(token)); w.Code != http.StatusOK || decode(t, w)["model"] != "m" {
		t.Fatalf("test: %d %s", w.Code, w.Body)
	}
	status.Store(http.StatusUnauthorized)
	if w := do(s, "POST", "/api/ai/test", "", bearer(token)); w.Code != http.StatusBadGateway || decode(t, w)["error"] != "ai_unauthorized" {
		t.Fatalf("test unauthorized: %d %s", w.Code, w.Body)
	}
}
