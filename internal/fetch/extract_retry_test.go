package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

func TestRetryAtSchedule(t *testing.T) {
	now := time.UnixMilli(1_790_000_000_000)
	busy := &extractError{error: errors.New("status 503"), temporary: true}
	var got []time.Duration
	for attempt := 1; attempt < maxExtractAttempts; attempt++ {
		got = append(got, retryAt(busy, attempt, now).Sub(now))
	}
	want := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}
	if len(got) != len(want) {
		t.Fatalf("schedule %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("wait after failure %d: %v, want %v", i+1, got[i], want[i])
		}
	}
	if !retryAt(busy, maxExtractAttempts, now).IsZero() {
		t.Error("the last attempt retried")
	}

	for name, err := range map[string]error{
		"gone":      permanent(errors.New("status 404")),
		"plain":     errors.New("something"),
		"permanent": &extractError{error: errors.New("status 410")},
	} {
		if !retryAt(err, 1, now).IsZero() {
			t.Errorf("%s: retried", name)
		}
	}

	asked := &extractError{error: errors.New("status 429"), temporary: true, wait: 10 * time.Minute}
	if d := retryAt(asked, 1, now).Sub(now); d != 10*time.Minute {
		t.Errorf("Retry-After longer than the schedule: %v", d)
	}
	asked.wait = 10 * time.Second
	if d := retryAt(asked, 1, now).Sub(now); d != time.Minute {
		t.Errorf("Retry-After shorter than the schedule: %v", d)
	}
}

func TestTemporaryStatus(t *testing.T) {
	for code, want := range map[int]bool{
		400: false, 401: false, 403: false, 404: false, 410: false, 451: false,
		408: true, 425: true, 429: true, 500: true, 502: true, 503: true, 504: true,
	} {
		if temporaryStatus(code) != want {
			t.Errorf("%d: %v", code, !want)
		}
	}
}

// extractFixture has one article source whose pages are served by handler.
func extractFixture(t *testing.T, handler http.HandlerFunc, paths ...string) (*fixture, *httptest.Server) {
	t.Helper()
	f := newFixture(t)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	f.poller.guarded = guardedClient(func(netip.AddrPort) bool { return false })
	id := f.add(store.NewSource{URL: srv.URL + "/feed"})
	var entries []store.FeedEntry
	for _, path := range paths {
		entries = append(entries, store.FeedEntry{GUID: path, URL: srv.URL + path})
	}
	if _, err := f.store.SaveFeed(context.Background(), store.FeedSource{ID: id, Kind: "article"}, "", "", entries, "", time.Time{}, f.now); err != nil {
		t.Fatal(err)
	}
	return f, srv
}

func TestExtractRetriesAfterSiteRecovers(t *testing.T) {
	var up atomic.Bool
	var hits atomic.Int32
	f, _ := extractFixture(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !up.Load() {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		w.Write([]byte(articlePage))
	}, "/article")
	ctx := context.Background()
	start := f.now
	next := func() time.Duration {
		return time.UnixMilli(f.int("SELECT next_extract_at FROM entries")).Sub(start)
	}

	f.poller.extractAll(ctx)
	if hits.Load() != 1 || f.text("SELECT extract_status FROM entries") != "failed" || next() != time.Minute {
		t.Fatalf("first failure: %d hits, %s, next %v", hits.Load(), f.text("SELECT extract_status FROM entries"), next())
	}

	// Not due yet: nothing is requested.
	f.now = start.Add(30 * time.Second)
	f.poller.extractAll(ctx)
	if hits.Load() != 1 {
		t.Fatalf("asked again before the wait is over: %d", hits.Load())
	}

	// Due, but the site is still down: the wait grows.
	f.now = start.Add(time.Minute)
	f.poller.extractAll(ctx)
	if hits.Load() != 2 || f.int("SELECT extract_attempts FROM entries") != 2 || next() != 6*time.Minute {
		t.Fatalf("second failure: %d hits, next %v", hits.Load(), next())
	}

	// The site comes back and the next due round gets the text.
	up.Store(true)
	f.now = start.Add(6 * time.Minute)
	f.poller.extractAll(ctx)
	if f.text("SELECT extract_status FROM entries") != "ok" || f.text("SELECT extracted_html FROM entries") == "" {
		t.Fatalf("not recovered: %s", f.text("SELECT extract_status FROM entries"))
	}
	if f.int("SELECT coalesce(next_extract_at, 0) FROM entries") != 0 {
		t.Error("a finished extraction keeps a retry time")
	}
	f.now = start.Add(48 * time.Hour)
	f.poller.extractAll(ctx)
	if hits.Load() != 3 {
		t.Errorf("a finished extraction ran again: %d", hits.Load())
	}
}

func TestExtractHonorsRetryAfter(t *testing.T) {
	f, _ := extractFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1200")
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}, "/article")
	start := f.now
	f.poller.extractAll(context.Background())
	if d := time.UnixMilli(f.int("SELECT next_extract_at FROM entries")).Sub(start); d != 20*time.Minute {
		t.Errorf("next attempt in %v, want the 20 minutes the site asked for", d)
	}
}

func TestExtractGivesUpAfterTemporaryErrorsLast(t *testing.T) {
	var hits atomic.Int32
	f, _ := extractFixture(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "down", http.StatusServiceUnavailable)
	}, "/article")
	for range 2 * maxExtractAttempts {
		f.poller.extractAll(context.Background())
		f.now = f.now.Add(7 * time.Hour)
	}
	if hits.Load() != maxExtractAttempts || f.int("SELECT extract_attempts FROM entries") != maxExtractAttempts {
		t.Errorf("%d requests", hits.Load())
	}
	if f.int("SELECT coalesce(next_extract_at, 0) FROM entries") != 0 {
		t.Error("an entry out of attempts is still scheduled")
	}
}

func TestExtractSkipsPermanentErrors(t *testing.T) {
	var hits atomic.Int32
	f, _ := extractFixture(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/gone":
			http.Error(w, "gone", http.StatusGone)
		case "/pdf":
			w.Header().Set("Content-Type", "application/pdf")
			w.Write([]byte("%PDF-1.7"))
		}
	}, "/gone", "/pdf")
	for range 3 {
		f.poller.extractAll(context.Background())
		f.now = f.now.Add(24 * time.Hour)
	}
	if hits.Load() != 2 {
		t.Errorf("%d requests for two permanent failures", hits.Load())
	}
	if f.int("SELECT count(*) FROM entries WHERE extract_status = 'failed' AND next_extract_at IS NULL") != 2 {
		t.Error("permanent failures are scheduled again")
	}
}

func TestExtractSpacesOutRequestsToOneHost(t *testing.T) {
	var hits atomic.Int32
	f, _ := extractFixture(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(articlePage))
	}, "/a", "/b", "/c")
	ctx := context.Background()
	start := f.now

	f.poller.extractAll(ctx)
	if hits.Load() != 1 {
		t.Fatalf("one round sent %d requests to one site", hits.Load())
	}
	f.poller.extractAll(ctx) // the same instant: the host is still held
	if hits.Load() != 1 {
		t.Fatalf("no time passed, yet %d requests", hits.Load())
	}
	f.now = start.Add(hostSpacing)
	f.poller.extractAll(ctx)
	f.now = start.Add(2 * hostSpacing)
	f.poller.extractAll(ctx)
	if hits.Load() != 3 || f.int("SELECT count(*) FROM entries WHERE extract_status = 'ok'") != 3 {
		t.Errorf("%d requests, entries not all done", hits.Load())
	}
}

func TestExtractHoldsHostWhileRetryAfter(t *testing.T) {
	var hits atomic.Int32
	f, _ := extractFixture(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "300")
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}, "/a", "/b")
	start := f.now
	f.poller.extractAll(context.Background())
	// The other article waits for the site's own time, not just the spacing.
	f.now = start.Add(time.Minute)
	f.poller.extractAll(context.Background())
	if hits.Load() != 1 {
		t.Errorf("%d requests while the site asked to wait", hits.Load())
	}
	f.now = start.Add(5 * time.Minute)
	f.poller.extractAll(context.Background())
	if hits.Load() != 2 {
		t.Errorf("%d requests after the wait", hits.Load())
	}
}
