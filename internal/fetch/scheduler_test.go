package fetch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

// eventually waits, in short steps, until cond holds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// run starts the poller and returns a function that stops it and waits until it has returned.
func run(t *testing.T, p *Poller) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Error("Run did not stop")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func TestHostGateLimitsAndSpaces(t *testing.T) {
	var g hostGate
	start := time.UnixMilli(1_790_000_000_000)
	if !g.acquire("a", start, 0) || !g.acquire("a", start, 0) {
		t.Fatalf("the first %d requests to a host must run", hostLimit)
	}
	if g.acquire("a", start, 0) {
		t.Error("more requests than the limit at once")
	}
	if !g.acquire("b", start, 0) {
		t.Error("another host is held back")
	}
	g.release("a")
	if !g.acquire("a", start, 0) {
		t.Error("a freed place was not given out")
	}

	g = hostGate{}
	if !g.acquire("a", start, time.Second) {
		t.Fatal("first request")
	}
	g.release("a")
	if g.acquire("a", start.Add(999*time.Millisecond), time.Second) {
		t.Error("two requests closer than the spacing")
	}
	if !g.acquire("a", start.Add(time.Second), time.Second) {
		t.Error("the spacing is over but the host is held")
	}
	g.release("a")
	g.holdUntil("a", start.Add(time.Hour))
	if g.acquire("a", start.Add(time.Minute), time.Second) || !g.acquire("b", start.Add(time.Minute), time.Second) {
		t.Error("a hold must stop that host only")
	}
	if !g.acquire("", start, time.Hour) || !g.acquire("", start, time.Hour) {
		t.Error("a request without a host is never limited")
	}
}

func TestSlowSourceDoesNotDelayOthers(t *testing.T) {
	f := newFixture(t)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
		w.Write([]byte(rssFeed))
	}))
	defer srv.Close()
	slow := f.add(store.NewSource{URL: srv.URL + "/slow"}) // the oldest, so the first to be taken
	for i := range 12 {
		f.add(store.NewSource{URL: fmt.Sprintf("%s/fast%d", srv.URL, i)})
	}
	run(t, f.poller)
	eventually(t, "the fast sources", func() bool {
		return f.int("SELECT count(*) FROM sources WHERE last_success_at IS NOT NULL") == 12
	})
	if f.int("SELECT last_success_at IS NULL FROM sources WHERE id = ?", slow) != 1 {
		t.Fatal("the slow source is done before it was released")
	}
	close(release)
	eventually(t, "the slow source", func() bool {
		return f.int("SELECT last_success_at IS NOT NULL FROM sources WHERE id = ?", slow) == 1
	})
}

func TestExtractionDoesNotWaitForFetching(t *testing.T) {
	release := make(chan struct{})
	f, _ := extractFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/feed" {
			select {
			case <-release:
			case <-r.Context().Done():
			}
			w.Write([]byte(rssFeed))
			return
		}
		w.Write([]byte(articlePage))
	}, "/a")
	run(t, f.poller)
	eventually(t, "the extraction", func() bool {
		return f.text("SELECT extract_status FROM entries WHERE guid = '/a'") == "ok"
	})
	if f.int("SELECT last_success_at IS NULL FROM sources") != 1 {
		t.Fatal("the feed was not still being fetched")
	}
	close(release)
}

func TestRunStopsWhileFetching(t *testing.T) {
	f := newFixture(t)
	var started atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started.Add(1)
		<-r.Context().Done()
	}))
	defer srv.Close()
	for i := range 3 * hostLimit {
		f.add(store.NewSource{URL: fmt.Sprintf("%s/%d", srv.URL, i)})
	}
	stop := run(t, f.poller)
	eventually(t, "the host to be at its limit", func() bool { return started.Load() == hostLimit })
	stop()
	if started.Load() != hostLimit {
		t.Errorf("%d fetches started, workers keep going after the cancel", started.Load())
	}
	if f.int("SELECT count(*) FROM sources WHERE consecutive_failures > 0") != 0 {
		t.Error("a fetch cut short by shutdown was counted as a failure")
	}
}

// busyHost counts how many requests it serves at once.
type busyHost struct {
	*httptest.Server
	now, peak, total atomic.Int32
}

func newBusyHost(t *testing.T, status int, header http.Header) *busyHost {
	h := &busyHost{}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := h.now.Add(1)
		defer h.now.Add(-1)
		for {
			peak := h.peak.Load()
			if n <= peak || h.peak.CompareAndSwap(peak, n) {
				break
			}
		}
		h.total.Add(1)
		time.Sleep(5 * time.Millisecond)
		for k, v := range header {
			w.Header()[k] = v
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Write([]byte(rssFeed))
	}))
	t.Cleanup(h.Close)
	return h
}

func TestRSSHubRoutesRespectTheHostLimit(t *testing.T) {
	f := newFixture(t)
	hub := newBusyHost(t, http.StatusOK, nil)
	f.store.SetSetting(context.Background(), RSSHubSetting, hub.URL)
	for i := range 30 {
		f.add(store.NewSource{RSSHubPath: fmt.Sprintf("/route/%d", i)})
	}
	f.poller.fetchDue(context.Background())
	if hub.total.Load() != 30 {
		t.Fatalf("%d of 30 routes fetched", hub.total.Load())
	}
	if hub.peak.Load() > hostLimit {
		t.Errorf("up to %d requests at once to one RSSHub, want at most %d", hub.peak.Load(), hostLimit)
	}
}

func TestRSSHubRoutesAreSpacedOut(t *testing.T) {
	f := newFixture(t)
	f.poller.spacing = feedSpacing
	hub := newBusyHost(t, http.StatusOK, nil)
	f.store.SetSetting(context.Background(), RSSHubSetting, hub.URL)
	for i := range 8 {
		f.add(store.NewSource{RSSHubPath: fmt.Sprintf("/route/%d", i)})
	}
	start := f.now
	f.poller.fetchDue(context.Background())
	if hub.total.Load() != 1 {
		t.Fatalf("%d requests in the same instant", hub.total.Load())
	}
	f.now = start.Add(feedSpacing - time.Millisecond)
	f.poller.fetchDue(context.Background())
	if hub.total.Load() != 1 {
		t.Fatalf("%d requests closer than the spacing", hub.total.Load())
	}
	f.now = start
	for i := 2; i <= 8; i++ {
		f.now = f.now.Add(feedSpacing)
		f.poller.fetchDue(context.Background())
		if hub.total.Load() != int32(i) {
			t.Fatalf("%d requests after %d spacings", hub.total.Load(), i)
		}
	}
	if f.int("SELECT count(*) FROM sources WHERE consecutive_failures > 0") != 0 {
		t.Error("waiting for the host's turn counted as a failure")
	}
}

func TestTooManyRequestsDelaysOnlyThatHost(t *testing.T) {
	f := newFixture(t)
	limited := newBusyHost(t, http.StatusTooManyRequests, http.Header{"Retry-After": {"300"}})
	healthy := newBusyHost(t, http.StatusOK, nil)
	for i := range 4 {
		f.add(store.NewSource{URL: fmt.Sprintf("%s/%d", limited.URL, i)})
		f.add(store.NewSource{URL: fmt.Sprintf("%s/%d", healthy.URL, i)})
	}
	start := f.now
	f.poller.fetchDue(context.Background())
	if healthy.total.Load() != 4 {
		t.Errorf("%d of 4 healthy sources fetched", healthy.total.Load())
	}
	if limited.total.Load() < 1 || limited.total.Load() > hostLimit {
		t.Errorf("%d requests to a host that asked for quiet", limited.total.Load())
	}
	// The quiet host's other sources are still due, not failed.
	if f.int("SELECT count(*) FROM sources WHERE consecutive_failures > 0") > hostLimit ||
		f.int("SELECT count(*) FROM sources WHERE next_fetch_at <= ?", start.UnixMilli()) < 2 {
		t.Error("the quiet host's other sources were not left due")
	}
	asked := limited.total.Load()
	f.now = start.Add(time.Minute)
	f.poller.fetchDue(context.Background())
	if limited.total.Load() != asked {
		t.Error("the host was asked again during its quiet time")
	}
	f.now = start.Add(5 * time.Minute)
	f.poller.fetchDue(context.Background())
	if limited.total.Load() <= asked {
		t.Error("the host was not asked again after its quiet time")
	}
}

func TestManySubscriptionsStayOnSchedule(t *testing.T) {
	f := newFixture(t)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("If-None-Match") != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"1"`)
		w.Write([]byte(rssFeed))
	}))
	defer srv.Close()
	const sources = 500
	for i := range sources {
		f.add(store.NewSource{URL: fmt.Sprintf("%s/%d", srv.URL, i)})
	}
	for round := 1; round <= 4; round++ {
		f.poller.fetchDue(context.Background())
		if got := requests.Load(); got != int32(round*sources) {
			t.Fatalf("round %d: %d requests, want %d", round, got, round*sources)
		}
		if f.int("SELECT count(*) FROM sources WHERE next_fetch_at <= ?", f.now.UnixMilli()) != 0 {
			t.Fatalf("round %d: sources are still due after a full pass", round)
		}
		f.now = f.now.Add(15 * time.Minute)
	}
}

func TestSlowDueReadDoesNotBlockScheduling(t *testing.T) {
	f := newFixture(t)
	f.add(store.NewSource{URL: "https://example.org/feed"})
	reading, release := make(chan struct{}), make(chan struct{})
	f.poller.readHook = func() {
		close(reading)
		<-release
	}
	taken := make(chan bool)
	go func() {
		_, ok := f.poller.take(context.Background())
		taken <- ok
	}()
	<-reading // a refill is now inside its database read

	calls := make(chan struct{})
	go func() {
		f.poller.Fetching()
		f.poller.finish(queued{src: store.FeedSource{ID: 99}})
		close(calls)
	}()
	select {
	case <-calls:
	case <-time.After(5 * time.Second):
		t.Fatal("Fetching and finish wait for the database read")
	}
	close(release)
	if ok := <-taken; !ok {
		t.Error("the due source was not taken after the read")
	}
}

func TestRSSHubSettingIsReadOncePerRefillInterval(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.useInstances("http://one.example")
	if got, _ := f.poller.rsshubSetting(ctx, f.now); got != "http://one.example" {
		t.Fatalf("setting = %q", got)
	}
	f.useInstances("http://two.example")
	if got, _ := f.poller.rsshubSetting(ctx, f.now.Add(refillEvery/2)); got != "http://one.example" {
		t.Errorf("setting = %q, want the cached value within the interval", got)
	}
	if got, _ := f.poller.rsshubSetting(ctx, f.now.Add(refillEvery)); got != "http://two.example" {
		t.Errorf("setting = %q, want the changed value after the interval", got)
	}
}
