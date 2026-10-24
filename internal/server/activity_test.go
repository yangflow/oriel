package server

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/fetch"
	"github.com/yangflow/oriel/internal/store"
)

type cachedImages struct {
	images
	count int
	bytes int64
}

func (c cachedImages) CacheStats() (int, int64) { return c.count, c.bytes }

func TestActivityRoute(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	if w := do(s, "GET", "/api/activity", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("without session: %d", w.Code)
	}
	w := do(s, "GET", "/api/activity", "", bearer(token))
	body := decode(t, w)
	if w.Code != http.StatusOK || body["keep"] != float64(store.FetchLogKeep) ||
		len(body["fetching"].([]any)) != 0 || len(body["sources"].([]any)) != 0 {
		t.Fatalf("empty: %d %s", w.Code, w.Body)
	}

	a := decode(t, do(s, "POST", "/api/sources", `{"url":"https://example.org/feed","kind":"article","title":"Blog"}`, bearer(token)))
	b := decode(t, do(s, "POST", "/api/sources", `{"rsshubPath":"/x/y","kind":"social","title":"Route"}`, bearer(token)))
	idA, idB := int64(a["id"].(float64)), int64(b["id"].(float64))
	ctx := context.Background()
	if err := s.store.LogFetch(ctx, store.FetchAttempt{SourceID: idA, At: c.t, Duration: 1500 * time.Millisecond, OK: true, Status: 200, NewEntries: 4}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.LogFetch(ctx, store.FetchAttempt{SourceID: idB, At: c.t.Add(time.Minute), Duration: 90 * time.Millisecond, Status: 503, Error: "status 503"}); err != nil {
		t.Fatal(err)
	}
	s.fetching = func() []int64 { return []int64{idB, 9999} }

	w = do(s, "GET", "/api/activity", "", bearer(token))
	body = decode(t, w)
	fetching := body["fetching"].([]any)
	if w.Code != http.StatusOK || len(fetching) != 1 {
		t.Fatalf("fetching: %d %s", w.Code, w.Body)
	}
	if f := fetching[0].(map[string]any); f["id"] != float64(idB) || f["title"] != "Route" || f["rsshubPath"] != "/x/y" {
		t.Fatalf("fetching[0] = %v", f)
	}
	list := body["sources"].([]any)
	if len(list) != 2 {
		t.Fatalf("sources: %s", w.Body)
	}
	first := list[0].(map[string]any)
	if first["id"] != float64(idB) || first["paused"] != false {
		t.Fatalf("first = %v", first)
	}
	bad := first["attempts"].([]any)[0].(map[string]any)
	if bad["ok"] != false || bad["status"] != 503.0 || bad["error"] != "status 503" || bad["durationMs"] != 90.0 ||
		bad["at"] != float64(c.t.Add(time.Minute).UnixMilli()) || bad["newEntries"] != 0.0 {
		t.Fatalf("bad = %v", bad)
	}
	good := list[1].(map[string]any)["attempts"].([]any)[0].(map[string]any)
	if good["ok"] != true || good["status"] != 200.0 || good["newEntries"] != 4.0 || good["durationMs"] != 1500.0 {
		t.Fatalf("good = %v", good)
	}

	do(s, "PATCH", "/api/sources/"+strconv.FormatInt(idA, 10), `{"paused":true}`, bearer(token))
	list = decode(t, do(s, "GET", "/api/activity", "", bearer(token)))["sources"].([]any)
	if p := list[1].(map[string]any); p["id"] != float64(idA) || p["paused"] != true {
		t.Fatalf("paused source: %v", p)
	}
}

func TestStorageRouteAndVacuum(t *testing.T) {
	s, c := newServer(t, "secret")
	s.images = cachedImages{images: images{}, count: 7, bytes: 12345}
	token := login(t, s, "laptop")
	if w := do(s, "GET", "/api/storage", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("without session: %d", w.Code)
	}
	if w := do(s, "POST", "/api/storage/vacuum", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("vacuum without session: %d", w.Code)
	}
	w := do(s, "GET", "/api/storage", "", bearer(token))
	body := decode(t, w)
	vac := body["vacuum"].(map[string]any)
	if w.Code != http.StatusOK || body["databaseBytes"].(float64) <= 0 || body["entries"] != 0.0 ||
		body["images"] != 7.0 || body["imageBytes"] != 12345.0 || vac["state"] != "idle" {
		t.Fatalf("storage: %d %s", w.Code, w.Body)
	}

	w = do(s, "POST", "/api/storage/vacuum", "", bearer(token))
	started := decode(t, w)["vacuum"].(map[string]any)
	if w.Code != http.StatusAccepted || started["startedAt"] != float64(c.t.UnixMilli()) {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		vac = decode(t, do(s, "GET", "/api/storage", "", bearer(token)))["vacuum"].(map[string]any)
		if vac["state"] != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the vacuum did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if vac["state"] != "done" || vac["error"] != "" || vac["freedBytes"].(float64) < 0 || vac["finishedAt"] != float64(c.t.UnixMilli()) {
		t.Fatalf("after: %v", vac)
	}

	// A second one can start once the first is over, but not while one runs.
	s.vacuumMu.Lock()
	s.vacuum.State = "running"
	s.vacuumMu.Unlock()
	w = do(s, "POST", "/api/storage/vacuum", "", bearer(token))
	if w.Code != http.StatusConflict || decode(t, w)["error"] != "vacuum_running" {
		t.Fatalf("while running: %d %s", w.Code, w.Body)
	}
}

func TestStorageWithoutImageCache(t *testing.T) {
	s, _ := newServer(t, "")
	body := decode(t, do(s, "GET", "/api/storage", "", nil))
	if body["images"] != 0.0 || body["imageBytes"] != 0.0 || body["copies"] != 0.0 || body["copyBytes"] != 0.0 {
		t.Fatalf("storage: %v", body)
	}
}

func TestRSSHubHealthRoute(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	if w := do(s, "GET", "/api/rsshub/health", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("without session: %d", w.Code)
	}

	w := do(s, "GET", "/api/rsshub/health", "", bearer(token))
	body := decode(t, w)
	if w.Code != http.StatusOK || body["keep"] != float64(fetch.InstanceHealthKeep) || body["days"] != float64(fetch.InstanceHealthDays) {
		t.Fatalf("no instances: %d %s", w.Code, w.Body)
	}
	if list, ok := body["instances"].([]any); !ok || len(list) != 0 {
		t.Fatalf("instances = %#v, want an empty array", body["instances"])
	}

	ctx := context.Background()
	if err := s.store.SetSetting(ctx, fetch.RSSHubSetting, "https://a.example\nhttps://b.example\n"); err != nil {
		t.Fatal(err)
	}
	w = do(s, "GET", "/api/rsshub/health", "", bearer(token))
	list := decode(t, w)["instances"].([]any)
	if len(list) != 2 {
		t.Fatalf("without the hook: %s", w.Body)
	}
	for i, url := range []string{"https://a.example", "https://b.example"} {
		row := list[i].(map[string]any)
		if row["url"] != url || row["requests"] != float64(0) || row["successRate"] != nil || row["medianMs"] != nil || row["coolingUntil"] != nil {
			t.Errorf("row %d = %v: want zero requests and null figures", i, row)
		}
	}

	rate, median := 0.96, int64(420)
	until := time.Date(2026, 5, 1, 12, 30, 0, 0, time.UTC)
	var asked []string
	s.instanceHealth = func(instances []string) []fetch.InstanceHealth {
		asked = instances
		return []fetch.InstanceHealth{
			{URL: "https://b.example", Requests: 3, CoolingUntil: &until},
			{URL: "https://a.example", Requests: 50, SuccessRate: &rate, MedianMs: &median},
			{URL: "https://gone.example", Requests: 9},
		}
	}
	w = do(s, "GET", "/api/rsshub/health", "", bearer(token))
	list = decode(t, w)["instances"].([]any)
	if len(asked) != 2 || asked[0] != "https://a.example" || len(list) != 2 {
		t.Fatalf("asked %v, got %s", asked, w.Body)
	}
	a, b := list[0].(map[string]any), list[1].(map[string]any)
	if a["url"] != "https://a.example" || a["requests"] != float64(50) || a["successRate"] != 0.96 || a["medianMs"] != float64(420) || a["coolingUntil"] != nil {
		t.Errorf("a = %v", a)
	}
	if b["url"] != "https://b.example" || b["requests"] != float64(3) || b["successRate"] != nil || b["medianMs"] != nil || b["coolingUntil"] != float64(until.UnixMilli()) {
		t.Errorf("b = %v", b)
	}
}
