package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

// upstream is a fake OpenAI-compatible endpoint that records what it was sent.
type upstream struct {
	*httptest.Server
	mu       sync.Mutex
	requests []chatRequest
	auth     []string
	answer   func(req chatRequest) (int, string) // status and message content
	models   func() (int, string)                // status and body of GET /v1/models; nil means no such route
	listAuth []string
	finish   string            // finish_reason of the reply, empty to leave it out
	header   map[string]string // extra headers of the chat reply
}

type chatRequest struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func newUpstream(t *testing.T) *upstream {
	u := &upstream{answer: func(req chatRequest) (int, string) { return http.StatusOK, " reply: " + req.Messages[1].Content + " " }}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			u.mu.Lock()
			models := u.models
			u.listAuth = append(u.listAuth, r.Header.Get("Authorization"))
			u.mu.Unlock()
			if models == nil {
				http.NotFound(w, r)
				return
			}
			status, body := models()
			w.WriteHeader(status)
			io.WriteString(w, body)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		u.mu.Lock()
		u.requests = append(u.requests, req)
		u.auth = append(u.auth, r.Header.Get("Authorization"))
		answer, finish, header := u.answer, u.finish, u.header
		u.mu.Unlock()
		status, content := answer(req)
		for k, v := range header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		choice := map[string]any{"message": map[string]string{"role": "assistant", "content": content}}
		if finish != "" {
			choice["finish_reason"] = finish
		}
		json.NewEncoder(w).Encode(map[string]any{"model": req.Model + "-0613", "choices": []any{choice}})
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *upstream) calls() []chatRequest {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.requests)
}

type fixture struct {
	t      *testing.T
	store  *store.Store
	worker *Worker
	now    time.Time
}

func newFixture(t *testing.T, endpoint string) *fixture {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "oriel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &fixture{t: t, store: st, now: time.UnixMilli(1_790_000_000_000)}
	f.worker = New(st, "0.1.0")
	f.worker.now = func() time.Time { return f.now }
	if endpoint != "" {
		f.set(BaseURLSetting, endpoint+"/v1")
		f.set(ModelSetting, "test-model")
		f.set(APIKeySetting, "test-key")
	}
	return f
}

func (f *fixture) set(key, value string) {
	f.t.Helper()
	if err := f.store.SetSetting(context.Background(), key, value); err != nil {
		f.t.Fatal(err)
	}
}

// entry adds a source of kind with one entry and returns the entry's ID. tasks are queued automatically.
func (f *fixture) entry(kind string, item store.FeedEntry, tasks ...string) int64 {
	f.t.Helper()
	ctx := context.Background()
	src, err := f.store.CreateSource(ctx, store.NewSource{URL: "https://example.org/" + item.GUID, Kind: kind, FetchInterval: time.Hour}, "", f.now)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.store.SaveFeed(ctx, store.FeedSource{ID: src.ID, Kind: kind, AITasks: tasks}, "", "", []store.FeedEntry{item}, "", time.Time{}, f.now); err != nil {
		f.t.Fatal(err)
	}
	entries, err := f.store.Entries(ctx, store.EntryFilter{SourceID: src.ID}, 1)
	if err != nil || len(entries) != 1 {
		f.t.Fatalf("entry not saved: %v", err)
	}
	return entries[0].ID
}

// drain runs the worker until nothing it can start is left.
func (f *fixture) drain() {
	f.t.Helper()
	for range 20 {
		var wg sync.WaitGroup
		f.worker.dispatch(context.Background(), &wg)
		wg.Wait()
		select {
		case <-f.worker.wake:
		default:
			return
		}
	}
	f.t.Fatal("the queue never settled")
}

func (f *fixture) result(id int64, task string) store.AIResult {
	f.t.Helper()
	results, err := f.store.AIResults(context.Background(), id)
	if err != nil {
		f.t.Fatal(err)
	}
	return results[task]
}

func TestSummaryIsStoredAndCached(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	ctx := context.Background()
	id := f.entry("social", store.FeedEntry{GUID: "a", Title: " Hello\n world ", ContentHTML: "<p>One <b>two</b></p><p>Three</p><script>x()</script>"})

	result, err := f.worker.Request(ctx, id, store.TaskSummary)
	if err != nil || result.Status != "pending" {
		t.Fatalf("request: %+v %v", result, err)
	}
	f.drain()
	calls := u.calls()
	if len(calls) != 1 {
		t.Fatalf("%d calls", len(calls))
	}
	call := calls[0]
	if call.Model != "test-model" || call.Stream || call.Messages[0].Role != "system" ||
		call.Messages[0].Content != strings.ReplaceAll(DefaultPromptSummary, "{language}", "简体中文") ||
		call.Messages[1].Content != "Hello world\n\nOne two\n\nThree" || u.auth[0] != "Bearer test-key" {
		t.Fatalf("call: %+v %q", call, u.auth[0])
	}
	got := f.result(id, store.TaskSummary)
	if got.Status != "ok" || got.Text != "reply: Hello world\n\nOne two\n\nThree" || got.Model != "test-model-0613" || got.Error != "" {
		t.Fatalf("result: %+v", got)
	}

	// The same input is answered from the store.
	if again, err := f.worker.Request(ctx, id, store.TaskSummary); err != nil || again.Status != "ok" {
		t.Fatalf("cached: %+v %v", again, err)
	}
	f.drain()
	if len(u.calls()) != 1 {
		t.Fatal("a cached result called the endpoint again")
	}

	// A different language changes the input, so the task runs again; the old text stays until then.
	f.set(LanguageSetting, "en")
	again, err := f.worker.Request(ctx, id, store.TaskSummary)
	if err != nil || again.Status != "pending" || again.Text != got.Text {
		t.Fatalf("after a settings change: %+v %v", again, err)
	}
	f.drain()
	if calls := u.calls(); len(calls) != 2 || !strings.Contains(calls[1].Messages[0].Content, "English") {
		t.Fatal("language change not sent")
	}
}

func TestSourcePromptAndTranslationChunks(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	ctx := context.Background()
	f.set(PromptTranslateSetting, "Global {language}")
	long := strings.Repeat("a", 4000)
	id := f.entry("social", store.FeedEntry{GUID: "b", Title: "T", Summary: long + "\n\n" + long + "\n\n" + strings.Repeat("b", 7000)})
	prompt := "Mine in {language}"
	src, _ := f.store.Sources(ctx)
	if _, err := f.store.UpdateSource(ctx, src[0].ID, store.SourcePatch{PromptTranslate: &prompt}, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.worker.Request(ctx, id, store.TaskTranslation); err != nil {
		t.Fatal(err)
	}
	f.drain()
	calls := u.calls()
	var sent []string
	for _, call := range calls {
		if call.Messages[0].Content != "Mine in 简体中文" {
			t.Fatalf("prompt %q", call.Messages[0].Content)
		}
		sent = append(sent, call.Messages[1].Content)
	}
	want := []string{"T\n\n" + long, long, strings.Repeat("b", 6000), strings.Repeat("b", 1000)}
	if !slices.Equal(sent, want) {
		t.Fatalf("chunks: %d calls, sizes %v", len(sent), lengths(sent))
	}
	got := f.result(id, store.TaskTranslation)
	if got.Status != "ok" || got.Text != "reply: T\n\n"+long+"\n\nreply: "+long+"\n\nreply: "+strings.Repeat("b", 6000)+"\n\nreply: "+strings.Repeat("b", 1000) {
		t.Fatal("chunks not joined in order")
	}
}

func lengths(parts []string) []int {
	var n []int
	for _, p := range parts {
		n = append(n, len(p))
	}
	return n
}

func TestInput(t *testing.T) {
	cfg := Config{Model: "m", Language: "en", PromptSummary: DefaultPromptSummary, PromptTranslate: DefaultPromptTranslate, SummaryLimit: DefaultSummary, ChunkLimit: DefaultChunk}
	in := store.AIInput{Title: "T", Kind: "article", ExtractStatus: "ok", ExtractedHTML: "<div>Full<br>text</div>", ContentHTML: "<p>feed</p>", Summary: "short"}
	if j, ready := prepare(cfg, in, store.TaskSummary); !ready || j.chunks[0] != "T\n\nFull\n\ntext" {
		t.Fatalf("extracted text first: %q", j.chunks)
	}
	in.ExtractedHTML = ""
	if j, _ := prepare(cfg, in, store.TaskSummary); j.chunks[0] != "T\n\nfeed" {
		t.Fatalf("feed content next: %q", j.chunks)
	}
	in.ExtractStatus = "pending"
	if _, ready := prepare(cfg, in, store.TaskSummary); ready {
		t.Fatal("an article waiting for its full text was sent")
	}
	if j, _ := prepare(cfg, store.AIInput{Kind: "social", Summary: strings.Repeat("é", 30000)}, store.TaskSummary); len([]rune(j.chunks[0])) != 24000 {
		t.Fatal("summary input not cut at 24000 characters")
	}
	if j, _ := prepare(cfg, store.AIInput{Kind: "social", Summary: " \n "}, store.TaskSummary); len(j.chunks) != 0 {
		t.Fatal("empty input has text")
	}
}

func TestEmptyEntryFails(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	id := f.entry("image", store.FeedEntry{GUID: "c"})
	f.worker.Request(context.Background(), id, store.TaskSummary)
	f.drain()
	if got := f.result(id, store.TaskSummary); got.Status != "failed" || got.Error != "ai_empty" || len(u.calls()) != 0 {
		t.Fatalf("empty entry: %+v", got)
	}
}

func TestErrors(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	ctx := context.Background()
	for status, want := range map[int]error{
		http.StatusUnauthorized:        ErrUnauthorized,
		http.StatusForbidden:           ErrUnauthorized,
		http.StatusTooManyRequests:     ErrRateLimited,
		http.StatusInternalServerError: ErrUpstream,
	} {
		u.answer = func(chatRequest) (int, string) { return status, "x" }
		if _, err := f.worker.Test(ctx); !errors.Is(err, want) {
			t.Errorf("status %d: %v", status, err)
		}
	}
	u.answer = func(chatRequest) (int, string) { return http.StatusOK, "  " }
	if _, err := f.worker.Test(ctx); err != ErrUpstream {
		t.Errorf("empty reply: %v", err)
	}
	u.answer = func(chatRequest) (int, string) { return http.StatusOK, "OK" }
	if got, err := f.worker.Test(ctx); err != nil || got.Model != "test-model-0613" || got.ModelFound != nil || got.ContextFits != nil {
		t.Errorf("test: %+v %v", got, err)
	}

	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	f.set(BaseURLSetting, closed.URL)
	if _, err := f.worker.Test(ctx); err != ErrUnreachable {
		t.Errorf("closed endpoint: %v", err)
	}
}

func TestInputLimits(t *testing.T) {
	cfg := Config{Model: "m", Language: "en", PromptSummary: DefaultPromptSummary, PromptTranslate: DefaultPromptTranslate, SummaryLimit: 600, ChunkLimit: 500}
	in := store.AIInput{Kind: "social", Summary: strings.Repeat("a", 2000)}
	if j, _ := prepare(cfg, in, store.TaskSummary); len(j.chunks) != 1 || len([]rune(j.chunks[0])) != 600 {
		t.Errorf("summary cut at the configured limit: %v", lengths(j.chunks))
	}
	if j, _ := prepare(cfg, in, store.TaskTranslation); len(j.chunks) != 4 || len(j.chunks[0]) != 500 {
		t.Errorf("translation pieces of the configured size: %v", lengths(j.chunks))
	}
}

func TestLoadLimits(t *testing.T) {
	f := newFixture(t, "")
	ctx := context.Background()
	if c, err := Load(ctx, f.store); err != nil || c.SummaryLimit != DefaultSummary || c.ChunkLimit != DefaultChunk {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	f.set(SummaryLimitSetting, "4000")
	f.set(ChunkLimitSetting, "1500")
	if c, _ := Load(ctx, f.store); c.SummaryLimit != 4000 || c.ChunkLimit != 1500 {
		t.Errorf("stored: %+v", c)
	}
	f.set(SummaryLimitSetting, "many")
	f.set(ChunkLimitSetting, "3")
	if c, _ := Load(ctx, f.store); c.SummaryLimit != DefaultSummary || c.ChunkLimit != DefaultChunk {
		t.Errorf("unreadable or out of range falls back: %+v", c)
	}
}

func TestSelfCheck(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	ctx := context.Background()
	list := func(entry string) func() (int, string) {
		return func() (int, string) { return http.StatusOK, `{"data":[` + entry + `]}` }
	}

	u.models = list(`{"id":"other"},{"id":"Test-Model:latest","loaded_context_length":32768}`)
	got, err := f.worker.Test(ctx)
	if err != nil || got.ModelFound == nil || !*got.ModelFound || got.ContextTokens == nil || *got.ContextTokens != 32768 ||
		got.ContextFits == nil || !*got.ContextFits || got.Model != "test-model-0613" {
		t.Fatalf("listed, fits: %+v %v", got, err)
	}
	if len(u.listAuth) != 1 || u.listAuth[0] != "Bearer test-key" {
		t.Errorf("the list is asked with the key: %v", u.listAuth)
	}

	u.models = list(`{"id":"test-model","context_length":4096}`)
	if got, err = f.worker.Test(ctx); err != nil || got.ContextFits == nil || *got.ContextFits || *got.ContextTokens != 4096 {
		t.Errorf("too small a context is a hint, not a failure: %+v %v", got, err)
	}
	f.set(SummaryLimitSetting, "2000")
	f.set(ChunkLimitSetting, "1000")
	if got, err = f.worker.Test(ctx); err != nil || got.ContextFits == nil || !*got.ContextFits {
		t.Errorf("lower limits fit: %+v %v", got, err)
	}

	u.models = list(`{"id":"test-model"}`)
	if got, err = f.worker.Test(ctx); err != nil || got.ModelFound == nil || got.ContextTokens != nil || got.ContextFits != nil {
		t.Errorf("a list without contexts: %+v %v", got, err)
	}

	before := len(u.calls())
	u.models = list(`{"id":"another"}`)
	if _, err = f.worker.Test(ctx); err != ErrModelNotFound || len(u.calls()) != before {
		t.Errorf("model missing from the list: %v, %d chat calls", err, len(u.calls())-before)
	}

	u.models = func() (int, string) { return http.StatusUnauthorized, `{"error":"bad key test-key"}` }
	if _, err = f.worker.Test(ctx); err != ErrUnauthorized {
		t.Errorf("list turns the key down: %v", err)
	}
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		u.models = func() (int, string) { return status, "x" }
		if got, err = f.worker.Test(ctx); err != nil || got.ModelFound != nil {
			t.Errorf("a list that does not work is skipped (%d): %+v %v", status, got, err)
		}
	}
	u.models = func() (int, string) { return http.StatusOK, "not json" }
	if got, err = f.worker.Test(ctx); err != nil || got.ModelFound != nil {
		t.Errorf("an unreadable list is skipped: %+v %v", got, err)
	}

	u.models = nil
	u.answer = func(chatRequest) (int, string) { return http.StatusNotFound, "x" }
	if _, err = f.worker.Test(ctx); err != ErrModelNotFound {
		t.Errorf("chat 404: %v", err)
	}
}

func TestSelfCheckLoopbackAndNoKeyInErrors(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	if !strings.HasPrefix(u.URL, "http://127.0.0.1:") {
		t.Fatalf("the fake endpoint is not on the loopback address: %s", u.URL)
	}
	u.answer = func(chatRequest) (int, string) { return http.StatusUnauthorized, "denied test-key" }
	_, err := f.worker.Test(context.Background())
	if err != ErrUnauthorized || strings.Contains(err.Error(), "test-key") {
		t.Errorf("error: %v", err)
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	f.set(BaseURLSetting, closed.URL)
	if _, err = f.worker.Test(context.Background()); err != ErrUnreachable || strings.Contains(err.Error(), "test-key") {
		t.Errorf("unreachable error: %v", err)
	}
}

func TestSelfCheckTimeout(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	defer close(release)
	f := newFixture(t, slow.URL)
	f.worker.timeout = 50 * time.Millisecond
	if _, err := f.worker.Test(context.Background()); err != ErrTimeout {
		t.Errorf("slow endpoint: %v", err)
	}
}

func TestTimeout(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	defer close(release)
	f := newFixture(t, slow.URL)
	f.worker.timeout = 50 * time.Millisecond
	id := f.entry("social", store.FeedEntry{GUID: "d", Title: "T"})
	f.worker.Request(context.Background(), id, store.TaskSummary)
	f.drain()
	if got := f.result(id, store.TaskSummary); got.Status != "failed" || got.Error != "ai_timeout" {
		t.Fatalf("timeout: %+v", got)
	}
}

func TestNotConfigured(t *testing.T) {
	f := newFixture(t, "")
	id := f.entry("social", store.FeedEntry{GUID: "e", Title: "T"})
	if _, err := f.worker.Request(context.Background(), id, store.TaskSummary); err != ErrNotConfigured {
		t.Fatalf("request: %v", err)
	}
	if _, err := f.worker.Test(context.Background()); err != ErrNotConfigured {
		t.Fatalf("test: %v", err)
	}
	if _, err := f.worker.Request(context.Background(), id+1, store.TaskSummary); err != ErrNotConfigured {
		t.Fatalf("not configured comes before a missing entry: %v", err)
	}
	f.set(BaseURLSetting, "http://localhost:1/v1")
	f.set(ModelSetting, "m")
	if _, err := f.worker.Request(context.Background(), id+1, store.TaskSummary); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing entry: %v", err)
	}
}

func TestAutomaticQueue(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	ctx := context.Background()
	failing := true
	u.answer = func(req chatRequest) (int, string) {
		if failing {
			return http.StatusBadGateway, ""
		}
		return http.StatusOK, "done"
	}
	auto := f.entry("social", store.FeedEntry{GUID: "f", Title: "Auto"}, store.TaskSummary)
	waiting := f.entry("article", store.FeedEntry{GUID: "g", Title: "Article"}, store.TaskSummary)

	// Automatic failures are retried at least five minutes apart, three times in all.
	for attempt := 1; attempt <= 4; attempt++ {
		f.drain()
		f.drain()
		want := min(attempt, 3)
		if got := f.result(auto, store.TaskSummary); got.Status != "failed" || got.Error != "ai_upstream" || got.Attempts != want {
			t.Fatalf("attempt %d: %+v", attempt, got)
		}
		f.now = f.now.Add(5 * time.Minute)
	}
	// The article waits for its full text.
	if got := f.result(waiting, store.TaskSummary); got.Status != "pending" || len(u.calls()) != 3 {
		t.Fatalf("article sent before extraction: %+v, %d calls", got, len(u.calls()))
	}

	// A user request is not held back by the cap, and comes before automatic work.
	failing = false
	f.store.SaveExtraction(ctx, waiting, "", "", "")
	if _, err := f.worker.Request(ctx, auto, store.TaskSummary); err != nil {
		t.Fatal(err)
	}
	jobs, _ := f.store.AIJobs(ctx, 10, maxAttempts, f.now)
	if len(jobs) != 2 || jobs[0].EntryID != auto {
		t.Fatalf("user request not first: %+v", jobs)
	}
	f.drain()
	if f.result(auto, store.TaskSummary).Status != "ok" || f.result(waiting, store.TaskSummary).Status != "ok" {
		t.Fatal("queue not done")
	}
}

func TestShutdownKeepsJobQueued(t *testing.T) {
	started := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body) // the server notices a closed connection only after the body is read
		close(started)
		<-r.Context().Done()
	}))
	defer slow.Close()
	f := newFixture(t, slow.URL)
	id := f.entry("social", store.FeedEntry{GUID: "h", Title: "T"}, store.TaskSummary)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		f.worker.Run(ctx)
		close(done)
	}()
	<-started
	cancel()
	<-done
	if got := f.result(id, store.TaskSummary); got.Status != "pending" || got.Attempts != 0 {
		t.Fatalf("shutdown counted an attempt: %+v", got)
	}
}

func TestIsLocalEndpoint(t *testing.T) {
	for base, want := range map[string]bool{
		"http://localhost:11434/v1":    true,
		"http://LocalHost/v1":          true,
		"http://127.0.0.1:1234/v1":     true,
		"http://[::1]:8080/v1":         true,
		"https://api.openai.com/v1":    false,
		"http://192.168.1.20:11434/v1": false,
		"http://localhost.example.com": false,
		"":                             false,
		"not a url":                    false,
	} {
		if got := IsLocalEndpoint(base); got != want {
			t.Errorf("%q: %v, want %v", base, got, want)
		}
	}
}

// The daily cap stops automatic work and says so, while a request of the user is neither counted nor capped.
func TestDailyCapStopsAutomaticWork(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	ctx := context.Background()
	f.set(AutoDailySetting, "2")
	var ids []int64
	for _, guid := range []string{"a", "b", "c", "d"} {
		ids = append(ids, f.entry("social", store.FeedEntry{GUID: guid, Title: guid}, store.TaskSummary))
	}
	f.drain()
	f.drain()
	if len(u.calls()) != 2 {
		t.Fatalf("%d requests with a cap of 2", len(u.calls()))
	}
	done, stopped := 0, 0
	for _, id := range ids {
		switch got := f.result(id, store.TaskSummary); {
		case got.Status == "ok":
			done++
		case got.Status == "failed" && got.Error == string(ErrCapReached) && got.Attempts == 0:
			stopped++
		default:
			t.Fatalf("unexpected result %+v", got)
		}
	}
	if done != 2 || stopped != 2 {
		t.Fatalf("done %d, stopped %d", done, stopped)
	}
	if used, _ := f.store.AIUsage(ctx, f.now); used != 2 {
		t.Fatalf("usage %d", used)
	}

	// Stopped work is not retried the same day, but the user can still ask, and that is not counted.
	f.now = f.now.Add(10 * time.Minute)
	f.drain()
	var stoppedID int64
	for _, id := range ids {
		if f.result(id, store.TaskSummary).Status == "failed" {
			stoppedID = id
		}
	}
	if len(u.calls()) != 2 {
		t.Fatalf("stopped work was retried: %d requests", len(u.calls()))
	}
	if _, err := f.worker.Request(ctx, stoppedID, store.TaskSummary); err != nil {
		t.Fatal(err)
	}
	f.drain()
	if got := f.result(stoppedID, store.TaskSummary); got.Status != "ok" || len(u.calls()) != 3 {
		t.Fatalf("manual request: %+v, %d requests", got, len(u.calls()))
	}
	if used, _ := f.store.AIUsage(ctx, f.now); used != 2 {
		t.Fatalf("a manual request was counted: %d", used)
	}
}

// The count starts from zero on a new day.
func TestDailyCapResetsTomorrow(t *testing.T) {
	f := newFixture(t, "")
	ctx := context.Background()
	now := time.Date(2026, 3, 1, 23, 0, 0, 0, time.UTC)
	for range 2 {
		if ok, err := f.store.ReserveAICall(ctx, 2, now); err != nil || !ok {
			t.Fatalf("reserve: %v, %v", ok, err)
		}
	}
	if ok, _ := f.store.ReserveAICall(ctx, 2, now); ok {
		t.Fatal("a third request fit under a cap of 2")
	}
	if used, _ := f.store.AIUsage(ctx, now); used != 2 {
		t.Fatalf("usage %d", used)
	}
	if ok, _ := f.store.ReserveAICall(ctx, 2, now.Add(2*time.Hour)); !ok {
		t.Fatal("the next day is capped")
	}
}

// Only the newest entries of one fetch get automatic tasks; the rest are marked as stopped by the cap.
func TestFetchCap(t *testing.T) {
	f := newFixture(t, "")
	ctx := context.Background()
	src, err := f.store.CreateSource(ctx, store.NewSource{URL: "https://example.org/feed", Kind: "social", FetchInterval: time.Hour}, "", f.now)
	if err != nil {
		t.Fatal(err)
	}
	var items []store.FeedEntry
	for i := range 5 {
		items = append(items, store.FeedEntry{GUID: strconv.Itoa(i), Title: "t", PublishedAt: f.now.Add(time.Duration(i) * time.Minute)})
	}
	fs := store.FeedSource{ID: src.ID, Kind: "social", AITasks: []string{store.TaskSummary}, AIFetchLimit: 2}
	if _, err := f.store.SaveFeed(ctx, fs, "", "", items, "", time.Time{}, f.now); err != nil {
		t.Fatal(err)
	}
	entries, err := f.store.Entries(ctx, store.EntryFilter{SourceID: src.ID}, 10)
	if err != nil || len(entries) != 5 {
		t.Fatalf("entries %d, %v", len(entries), err)
	}
	pending, stopped := 0, 0
	for _, e := range entries {
		switch got := f.result(e.ID, store.TaskSummary); {
		case got.Status == "pending":
			pending++
		case got.Status == "failed" && got.Error == string(ErrCapReached):
			stopped++
		}
	}
	if pending != 2 || stopped != 3 {
		t.Fatalf("pending %d, stopped %d", pending, stopped)
	}
}

// A retry of a translation sends only the chunks that did not finish.
func TestRetryKeepsFinishedChunks(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	ctx := context.Background()
	long := strings.Repeat("a", 4000)
	id := f.entry("social", store.FeedEntry{GUID: "r", Title: "T", Summary: long + "\n\n" + long + "\n\n" + strings.Repeat("b", 7000)})
	failing := true
	u.answer = func(req chatRequest) (int, string) {
		if failing && strings.HasPrefix(req.Messages[1].Content, "bbbb") {
			return http.StatusBadGateway, ""
		}
		return http.StatusOK, "ok:" + req.Messages[1].Content[:1]
	}
	if _, err := f.worker.Request(ctx, id, store.TaskTranslation); err != nil {
		t.Fatal(err)
	}
	f.drain()
	if got := f.result(id, store.TaskTranslation); got.Status != "failed" || len(u.calls()) != 3 {
		t.Fatalf("first try: %+v, %d requests", got, len(u.calls()))
	}

	failing = false
	if _, err := f.worker.Request(ctx, id, store.TaskTranslation); err != nil {
		t.Fatal(err)
	}
	f.drain()
	calls := u.calls()
	if got := f.result(id, store.TaskTranslation); got.Status != "ok" || len(calls) != 5 {
		t.Fatalf("retry: %+v, %d requests", got, len(calls))
	}
	// The two chunks that finished are not sent again: the retry sends the failed one, then the last.
	if !strings.HasPrefix(calls[3].Messages[1].Content, "bbbb") || len(calls[3].Messages[1].Content) != 6000 ||
		len(calls[4].Messages[1].Content) != 1000 {
		t.Fatalf("retry sent %d and %d characters", len(calls[3].Messages[1].Content), len(calls[4].Messages[1].Content))
	}
	if got := f.result(id, store.TaskTranslation); got.Text != "ok:T\n\nok:a\n\nok:b\n\nok:b" {
		t.Fatalf("text %q", got.Text)
	}
}

func TestTruncatedReplyFails(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	f.set(ChunkLimitSetting, "500")
	ctx := context.Background()
	u.finish = "length"
	if _, err := f.worker.Test(ctx); !errors.Is(err, ErrUpstream) {
		t.Fatalf("self-check of a truncated reply: %v", err)
	}
	long := strings.Repeat("First paragraph of the article.\n\n", 40)
	id := f.entry("social", store.FeedEntry{GUID: "len", Title: "T", ContentHTML: "<p>" + long + "</p>"}, store.TaskTranslation)
	f.drain()
	got := f.result(id, store.TaskTranslation)
	if got.Status != "failed" || got.Error != "ai_upstream" || got.Text != "" || got.Attempts != 1 {
		t.Fatalf("truncated translation: %+v", got)
	}
	if n := len(u.calls()); n != 2 { // the self-check and the first chunk
		t.Fatalf("%d calls for a truncated first chunk", n)
	}
	// The normal retry runs it again once the endpoint answers in full.
	u.finish = "stop"
	f.now = f.now.Add(retryDelay)
	f.drain()
	if got := f.result(id, store.TaskTranslation); got.Status != "ok" {
		t.Fatalf("after retry: %+v", got)
	}
}

func TestThinkingBlockIsStripped(t *testing.T) {
	for in, want := range map[string]string{
		"<think>hmm\nmore</think>\n\nThe summary.": "The summary.",
		"  <think></think>Done ":                   "Done",
		"Answer <think>kept</think>":               "Answer <think>kept</think>",
		"<think>never closed":                      "<think>never closed",
		"<think>only thoughts</think>":             "",
	} {
		if got := stripThinking(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	u.answer = func(chatRequest) (int, string) { return http.StatusOK, "<think>x</think>" }
	if _, err := f.worker.Test(context.Background()); !errors.Is(err, ErrUpstream) {
		t.Fatalf("only a thinking block: %v", err)
	}
	u.answer = func(chatRequest) (int, string) { return http.StatusOK, "<think>x</think> Fine" }
	id := f.entry("social", store.FeedEntry{GUID: "think", Title: "T"})
	f.worker.Request(context.Background(), id, store.TaskSummary)
	f.drain()
	if got := f.result(id, store.TaskSummary); got.Status != "ok" || got.Text != "Fine" {
		t.Fatalf("summary: %+v", got)
	}
}

func TestOutageDoesNotUseUpAttempts(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	// An overloaded endpoint answers 429 without a Retry-After; a refused connection is covered below.
	u.answer = func(chatRequest) (int, string) { return http.StatusTooManyRequests, "" }
	var ids []int64
	for i := range 4 {
		ids = append(ids, f.entry("social", store.FeedEntry{GUID: "o" + strconv.Itoa(i), Title: "T"}, store.TaskSummary))
	}
	start := f.now
	for f.now.Sub(start) < 15*time.Minute {
		f.drain()
		f.now = f.now.Add(15 * time.Second)
	}
	// Two jobs start together, then the worker waits 1 minute, 2, 4 and 8: six calls in 15 minutes at most.
	if n := len(u.calls()); n > 6 {
		t.Fatalf("%d calls during the outage", n)
	}
	for _, id := range ids {
		if got := f.result(id, store.TaskSummary); got.Attempts != 0 || got.Error == "" && got.Status != "pending" {
			t.Fatalf("attempts used up: %+v", got)
		}
	}
	u.answer = func(chatRequest) (int, string) { return http.StatusOK, "done" }
	f.now = f.now.Add(maxBackoff)
	for range 10 {
		f.drain()
		f.now = f.now.Add(retryDelay)
	}
	for _, id := range ids {
		if got := f.result(id, store.TaskSummary); got.Status != "ok" {
			t.Fatalf("not done after the outage: %+v", got)
		}
	}
	if f.worker.paused(Config{BaseURL: u.URL + "/v1", Model: "test-model", APIKey: "test-key"}) {
		t.Fatal("still waiting after a success")
	}
}

func TestUnreachableEndpointDoesNotUseUpAttempts(t *testing.T) {
	f := newFixture(t, "http://127.0.0.1:1")
	id := f.entry("social", store.FeedEntry{GUID: "u1", Title: "T"}, store.TaskSummary)
	start := f.now
	for f.now.Sub(start) < 15*time.Minute {
		f.drain()
		f.now = f.now.Add(15 * time.Second)
	}
	if got := f.result(id, store.TaskSummary); got.Status != "failed" || got.Error != "ai_unreachable" || got.Attempts != 0 {
		t.Fatalf("after a 15-minute outage: %+v", got)
	}
}

func TestBackoffDoublesAndHonorsRetryAfter(t *testing.T) {
	f := newFixture(t, "http://127.0.0.1:1")
	cfg, _ := Load(context.Background(), f.store)
	for i, want := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 30 * time.Minute, 30 * time.Minute} {
		f.now = f.now.Add(time.Hour)
		f.worker.failed(cfg, f.now, ErrUnreachable, ErrUnreachable, false)
		if got := f.worker.health.until.Sub(f.now); got != want {
			t.Fatalf("failure %d waits %v, want %v", i+1, got, want)
		}
	}
	f.worker.succeeded(cfg)
	f.worker.failed(cfg, f.now, rateLimit{after: 10 * time.Minute}, ErrRateLimited, false)
	if got := f.worker.health.until.Sub(f.now); got != 10*time.Minute {
		t.Fatalf("Retry-After: %v", got)
	}
	f.worker.succeeded(cfg)
	f.worker.failed(cfg, f.now, rateLimit{after: 3 * time.Hour}, ErrRateLimited, false)
	if got := f.worker.health.until.Sub(f.now); got != maxBackoff {
		t.Fatalf("long Retry-After: %v", got)
	}
	if got := retryAfter("120", f.now); got != 2*time.Minute {
		t.Fatalf("seconds: %v", got)
	}
	if got := retryAfter(f.now.Add(90*time.Second).UTC().Format(http.TimeFormat), f.now); got < 89*time.Second || got > 91*time.Second {
		t.Fatalf("date: %v", got)
	}
	if got := retryAfter("99999999999999", f.now); got != maxBackoff {
		t.Fatalf("huge: %v", got)
	}
	if got := retryAfter("-5", f.now); got != 0 {
		t.Fatalf("negative: %v", got)
	}
	if got := retryAfter("soon", f.now); got != 0 {
		t.Fatalf("junk: %v", got)
	}
}

func TestRateLimitRetryAfterPausesWorker(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	u.header = map[string]string{"Retry-After": "600"}
	u.answer = func(chatRequest) (int, string) { return http.StatusTooManyRequests, "" }
	first := f.entry("social", store.FeedEntry{GUID: "r1", Title: "T"}, store.TaskSummary)
	f.drain()
	second := f.entry("social", store.FeedEntry{GUID: "r2", Title: "T"}, store.TaskSummary)
	f.now = f.now.Add(9 * time.Minute)
	f.drain()
	if n := len(u.calls()); n != 1 {
		t.Fatalf("%d calls before Retry-After passed", n)
	}
	if got := f.result(second, store.TaskSummary); got.Status != "pending" {
		t.Fatalf("second: %+v", got)
	}
	// A request by the user is not held back, and shows the reason.
	if _, err := f.worker.Request(context.Background(), second, store.TaskSummary); err != nil {
		t.Fatal(err)
	}
	f.drain()
	if got := f.result(second, store.TaskSummary); got.Status != "failed" || got.Error != "ai_rate_limited" || got.Attempts != 0 {
		t.Fatalf("requested while limited: %+v", got)
	}
	if got := f.result(first, store.TaskSummary); got.Attempts != 0 {
		t.Fatalf("first: %+v", got)
	}
}

func TestWrongKeyPausesUntilSettingsChange(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	good := false
	u.answer = func(chatRequest) (int, string) {
		if !good {
			return http.StatusUnauthorized, ""
		}
		return http.StatusOK, "done"
	}
	var ids []int64
	for i := range 4 {
		ids = append(ids, f.entry("social", store.FeedEntry{GUID: "k" + strconv.Itoa(i), Title: "T"}, store.TaskSummary))
	}
	for range 100 {
		f.drain()
		f.now = f.now.Add(time.Hour)
	}
	if n := len(u.calls()); n > 2 {
		t.Fatalf("%d calls with a refused key", n)
	}
	for _, id := range ids {
		if got := f.result(id, store.TaskSummary); got.Attempts != 0 || got.Status == "ok" {
			t.Fatalf("after a wrong key: %+v", got)
		}
	}
	// A new key ends the wait, and the entries still have all their tries.
	good = true
	f.set(APIKeySetting, "other-key")
	for range 6 {
		f.drain()
		f.now = f.now.Add(retryDelay)
	}
	for _, id := range ids {
		if got := f.result(id, store.TaskSummary); got.Status != "ok" {
			t.Fatalf("after the key changed: %+v", got)
		}
	}
}

func TestSelfCheckSharesOneBudget(t *testing.T) {
	u := newUpstream(t)
	u.models = func() (int, string) {
		time.Sleep(120 * time.Millisecond)
		return http.StatusOK, `{"data":[{"id":"test-model"}]}`
	}
	u.answer = func(chatRequest) (int, string) {
		time.Sleep(120 * time.Millisecond)
		return http.StatusOK, "OK"
	}
	f := newFixture(t, u.URL)
	f.worker.timeout = 200 * time.Millisecond // each step fits alone
	f.worker.budget = 200 * time.Millisecond  // but not both
	if _, err := f.worker.Test(context.Background()); !errors.Is(err, ErrTimeout) {
		t.Fatalf("two slow steps: %v", err)
	}
	f.worker.budget = time.Second
	if _, err := f.worker.Test(context.Background()); err != nil {
		t.Fatalf("within the budget: %v", err)
	}
}

// A timeout on an endpoint that is otherwise working may be the entry's doing, so it uses up an attempt and an
// entry that always times out is given up on.
func TestTimeoutOfHealthyEndpointCountsAttempts(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	defer close(release)
	f := newFixture(t, slow.URL)
	f.worker.timeout = 30 * time.Millisecond
	id := f.entry("social", store.FeedEntry{GUID: "slow", Title: "T"}, store.TaskSummary)
	for range 12 {
		f.drain()
		f.now = f.now.Add(maxBackoff)
	}
	if got := f.result(id, store.TaskSummary); got.Status != "failed" || got.Error != "ai_timeout" || got.Attempts != maxAttempts {
		t.Fatalf("always timing out: %+v", got)
	}
}

// A missing model counts the same way; an endpoint already waiting does not charge the entry.
func TestModelNotFoundCountsOnlyWhenHealthy(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	u.answer = func(chatRequest) (int, string) { return http.StatusNotFound, "" }
	id := f.entry("social", store.FeedEntry{GUID: "m", Title: "T"}, store.TaskSummary)
	f.drain()
	if got := f.result(id, store.TaskSummary); got.Attempts != 1 || got.Error != "ai_model_not_found" {
		t.Fatalf("healthy endpoint: %+v", got)
	}
	cfg, _ := Load(context.Background(), f.store)
	if !f.worker.paused(cfg) {
		t.Fatal("not waiting after the failure")
	}
	// While it waits, the user's retry does not use up an attempt.
	if _, err := f.worker.Request(context.Background(), id, store.TaskSummary); err != nil {
		t.Fatal(err)
	}
	f.drain()
	if got := f.result(id, store.TaskSummary); got.Attempts != 1 {
		t.Fatalf("while waiting: %+v", got)
	}
}

// A working self-check or selection shows the endpoint is back, so automatic work goes on at once.
func TestTestAndSelectionClearThePause(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	cfg, _ := Load(context.Background(), f.store)
	for name, check := range map[string]func() error{
		"test": func() error { _, err := f.worker.Test(context.Background()); return err },
		"selection": func() error {
			_, _, err := f.worker.Selection(context.Background(), SelectionTranslate, "hello", "")
			return err
		},
	} {
		f.worker.failed(cfg, f.now, ErrUnreachable, ErrUnreachable, false)
		if !f.worker.paused(cfg) {
			t.Fatalf("%s: not waiting", name)
		}
		if err := check(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if f.worker.paused(cfg) {
			t.Fatalf("%s: still waiting after a success", name)
		}
	}
}

// A success with other settings says nothing about the endpoint that is waiting.
func TestSuccessOfOtherSettingsKeepsThePause(t *testing.T) {
	f := newFixture(t, "http://127.0.0.1:1")
	cfg, _ := Load(context.Background(), f.store)
	f.worker.failed(cfg, f.now, ErrUnreachable, ErrUnreachable, false)
	other := cfg
	other.Model = "other-model"
	f.worker.succeeded(other)
	if !f.worker.paused(cfg) {
		t.Fatal("the wait ended by a success of another model")
	}
	f.worker.succeeded(cfg)
	if f.worker.paused(cfg) {
		t.Fatal("the wait went on after its own success")
	}
}

// Calls the user starts by hand while the endpoint fails do not double the wait.
func TestRequestedFailuresDoNotDoubleTheWait(t *testing.T) {
	f := newFixture(t, "http://127.0.0.1:1")
	cfg, _ := Load(context.Background(), f.store)
	f.worker.failed(cfg, f.now, ErrUnreachable, ErrUnreachable, false)
	for range 5 {
		f.now = f.now.Add(time.Second)
		f.worker.failed(cfg, f.now, ErrUnreachable, ErrUnreachable, true)
	}
	if got := f.worker.health.backoff; got != minBackoff {
		t.Fatalf("backoff after hand-made failures: %v", got)
	}
	// With no wait yet, a hand-made failure starts it.
	g := newFixture(t, "http://127.0.0.1:1")
	g.worker.failed(cfg, g.now, ErrUnreachable, ErrUnreachable, true)
	if got := g.worker.health.backoff; got != minBackoff {
		t.Fatalf("first failure: %v", got)
	}
}

// A task the user asked for while it runs is not stopped by the cap that the automatic run hits.
func TestCapDoesNotStopPromotedTask(t *testing.T) {
	u := newUpstream(t)
	f := newFixture(t, u.URL)
	ctx := context.Background()
	id := f.entry("social", store.FeedEntry{GUID: "p", Title: "T"}, store.TaskSummary)
	got, err := f.worker.Request(ctx, id, store.TaskSummary)
	if err != nil || !got.Requested {
		t.Fatalf("request of a queued automatic task: %+v, %v", got, err)
	}
	if err := f.store.StopAI(ctx, id, store.TaskSummary, f.now); err != nil {
		t.Fatal(err)
	}
	if got := f.result(id, store.TaskSummary); got.Status != "pending" {
		t.Fatalf("stopped although the user asked: %+v", got)
	}
}
