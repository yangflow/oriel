package server

import (
	"context"
	"encoding/json"
	"io"
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

	if w := do(s, "POST", "/api/ai/test", "", bearer(token)); w.Code != http.StatusOK || decode(t, w)["model"] != "m" ||
		decode(t, w)["reachable"] != true || decode(t, w)["modelFound"] != nil || decode(t, w)["contextFits"] != nil {
		t.Fatalf("test: %d %s", w.Code, w.Body)
	}
	status.Store(http.StatusUnauthorized)
	if w := do(s, "POST", "/api/ai/test", "", bearer(token)); w.Code != http.StatusBadGateway || decode(t, w)["error"] != "ai_unauthorized" {
		t.Fatalf("test unauthorized: %d %s", w.Code, w.Body)
	}
}

func TestAISelection(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	ctx := context.Background()
	entries, err := s.store.Entries(ctx, store.EntryFilter{SourceID: seed(t, s, "social", 1, c.t)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	post := func(body string) *httptest.ResponseRecorder {
		return do(s, "POST", "/api/ai/selection", body, bearer(token))
	}
	if w := do(s, "POST", "/api/ai/selection", `{"task":"translate","text":"hello"}`, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("without session: %d", w.Code)
	}
	if w := post(`{"task":"translate","text":"hello"}`); w.Code != http.StatusConflict || decode(t, w)["error"] != "ai_not_configured" {
		t.Fatalf("not configured: %d %s", w.Code, w.Body)
	}

	var status atomic.Int32
	status.Store(http.StatusOK)
	var asked []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		asked = append(asked, body)
		w.WriteHeader(int(status.Load()))
		json.NewEncoder(w).Encode(map[string]any{"model": "m", "choices": []any{map[string]any{"message": map[string]string{"content": " an answer "}}}})
	}))
	defer upstream.Close()
	s.store.SetSetting(ctx, ai.BaseURLSetting, upstream.URL)
	s.store.SetSetting(ctx, ai.ModelSetting, "m")

	for body, code := range map[string]string{
		`{"task":"summary","text":"hello"}`:                             "invalid_task",
		`{"text":"hello"}`:                                              "invalid_task",
		`{"task":"translate","text":"  \n "}`:                           "invalid_text",
		`{"task":"explain"}`:                                            "invalid_text",
		`{"task":"explain","text":"` + strings.Repeat("字", 2001) + `"}`: "text_too_long",
		`{"task":"explain","text":"hello","extra":1}`:                   "invalid_body",
	} {
		if w := post(body); w.Code != http.StatusBadRequest || decode(t, w)["error"] != code {
			t.Errorf("%.40s: %d %s, want %s", body, w.Code, w.Body, code)
		}
	}
	if len(asked) != 0 {
		t.Fatalf("a refused request reached the endpoint: %v", asked)
	}

	// 2000 characters are taken, with the spaces around them trimmed.
	edge := strings.Repeat("字", 2000)
	if w := post(`{"task":"translate","text":" ` + edge + ` "}`); w.Code != http.StatusOK {
		t.Fatalf("2000 characters: %d %s", w.Code, w.Body)
	}
	asked = nil

	w := post(`{"task":"translate","text":" hello world "}`)
	if body := decode(t, w); w.Code != http.StatusOK || body["text"] != "an answer" || body["model"] != "m" {
		t.Fatalf("translate: %d %s", w.Code, w.Body)
	}
	messages := asked[0]["messages"].([]any)
	system, user := messages[0].(map[string]any)["content"], messages[1].(map[string]any)["content"]
	if system != strings.ReplaceAll(ai.DefaultPromptTranslate, "{language}", "简体中文") || user != "hello world" {
		t.Errorf("translate sent %q and %q", system, user)
	}

	w = post(`{"task":"explain","text":"hello","title":"  Go   1.27 \n"}`)
	if body := decode(t, w); w.Code != http.StatusOK || body["text"] != "an answer" {
		t.Fatalf("explain: %d %s", w.Code, w.Body)
	}
	messages = asked[1]["messages"].([]any)
	system, user = messages[0].(map[string]any)["content"], messages[1].(map[string]any)["content"]
	if system != strings.ReplaceAll(ai.PromptExplain, "{language}", "简体中文") || user != "Article: Go 1.27\n\nSelected text: hello" {
		t.Errorf("explain sent %q and %q", system, user)
	}
	post(`{"task":"explain","text":"hello"}`)
	if user := asked[2]["messages"].([]any)[1].(map[string]any)["content"]; user != "hello" {
		t.Errorf("explain without a title sent %q", user)
	}
	s.store.SetSetting(ctx, ai.LanguageSetting, "en")
	post(`{"task":"explain","text":"hello"}`)
	if system := asked[3]["messages"].([]any)[0].(map[string]any)["content"]; system != strings.ReplaceAll(ai.PromptExplain, "{language}", "English") {
		t.Errorf("explain in English sent %q", system)
	}

	status.Store(http.StatusTooManyRequests)
	if w := post(`{"task":"translate","text":"hello"}`); w.Code != http.StatusBadGateway || decode(t, w)["error"] != "ai_rate_limited" {
		t.Fatalf("rate limited: %d %s", w.Code, w.Body)
	}
	status.Store(http.StatusUnauthorized)
	if w := post(`{"task":"translate","text":"hello"}`); w.Code != http.StatusBadGateway || decode(t, w)["error"] != "ai_unauthorized" {
		t.Fatalf("unauthorized: %d %s", w.Code, w.Body)
	}

	results, err := s.store.AIResults(ctx, entries[0].ID)
	if err != nil || len(results) != 0 {
		t.Errorf("results stored: %v, %v", results, err)
	}
}

func TestAISelfCheck(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Write([]byte(`{"data":[{"id":"qwen3:8b","context_length":4096}]}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"model": "qwen3:8b", "choices": []any{map[string]any{"message": map[string]string{"content": "OK"}}}})
	}))
	defer upstream.Close()
	s.store.SetSetting(ctx, ai.BaseURLSetting, upstream.URL+"/v1")
	s.store.SetSetting(ctx, ai.ModelSetting, "qwen3:8b")
	s.store.SetSetting(ctx, ai.APIKeySetting, "stored-secret-value")

	w := do(s, "POST", "/api/ai/test", "", bearer(token))
	body := decode(t, w)
	if w.Code != http.StatusOK || body["reachable"] != true || body["modelFound"] != true || body["contextTokens"] != 4096.0 ||
		body["contextFits"] != false || strings.Contains(w.Body.String(), "stored-secret-value") {
		t.Fatalf("self-check: %d %s", w.Code, w.Body)
	}
	s.store.SetSetting(ctx, ai.ModelSetting, "missing")
	w = do(s, "POST", "/api/ai/test", "", bearer(token))
	if w.Code != http.StatusBadGateway || decode(t, w)["error"] != "ai_model_not_found" {
		t.Fatalf("missing model: %d %s", w.Code, w.Body)
	}
}

// A server read timeout does not cancel the context of a handler that is still running, but the write timeout cuts
// its answer off; so an AI route that waits for the endpoint extends the deadlines.
func TestAIRoutesOutlastServerTimeouts(t *testing.T) {
	answered := func(extend bool) (ctxCanceled bool, delivered bool) {
		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if extend {
				extendDeadlinesTo(w, time.Minute)
			}
			select {
			case <-r.Context().Done():
				ctxCanceled = true
			case <-time.After(400 * time.Millisecond):
			}
			w.Write([]byte("late"))
		}))
		srv.Config.ReadTimeout = 100 * time.Millisecond
		srv.Config.WriteTimeout = 100 * time.Millisecond
		srv.Start()
		defer srv.Close()
		resp, err := http.Post(srv.URL, "application/json", strings.NewReader("{}"))
		if err != nil {
			return ctxCanceled, false
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		return ctxCanceled, err == nil && string(body) == "late"
	}
	if canceled, delivered := answered(false); canceled || delivered {
		t.Errorf("without extension: context canceled %v, delivered %v", canceled, delivered)
	}
	if canceled, delivered := answered(true); canceled || !delivered {
		t.Errorf("with extension: context canceled %v, delivered %v", canceled, delivered)
	}
}

// The real routes of the self-check and of the selection answer an endpoint slower than the server's read and write
// timeouts, because they extend the deadlines.
func TestAIRoutesAnswerSlowEndpoint(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		if r.URL.Path == "/v1/models" {
			w.Write([]byte(`{"data":[{"id":"m"}]}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"model": "m", "choices": []any{map[string]any{"message": map[string]string{"content": "OK"}}}})
	}))
	defer upstream.Close()
	s.store.SetSetting(ctx, ai.BaseURLSetting, upstream.URL+"/v1")
	s.store.SetSetting(ctx, ai.ModelSetting, "m")
	front := httptest.NewUnstartedServer(s)
	front.Config.ReadTimeout = 100 * time.Millisecond
	front.Config.WriteTimeout = 100 * time.Millisecond
	front.Start()
	defer front.Close()
	for path, body := range map[string]string{
		"/api/ai/test":      "",
		"/api/ai/selection": `{"task":"translate","text":"hello"}`,
	} {
		req, err := http.NewRequest("POST", front.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %s %v", path, resp.StatusCode, raw, err)
		}
	}
}
