package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

// instance is a fake RSSHub instance that answers every route with one status and counts its requests.
type instance struct {
	*httptest.Server
	hits   atomic.Int32
	status atomic.Int32
	header atomic.Value // http.Header
}

func newInstance(t *testing.T, status int) *instance {
	t.Helper()
	i := &instance{}
	i.status.Store(int32(status))
	i.header.Store(http.Header{})
	i.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i.hits.Add(1)
		for k, v := range i.header.Load().(http.Header) {
			w.Header()[k] = v
		}
		if code := int(i.status.Load()); code != http.StatusOK {
			w.WriteHeader(code)
			return
		}
		w.Write([]byte(rssFeed))
	}))
	t.Cleanup(i.Close)
	return i
}

func (i *instance) host() string { return strings.TrimPrefix(i.URL, "http://") }

func (f *fixture) useInstances(addresses ...string) {
	f.t.Helper()
	if err := f.store.SetSetting(context.Background(), RSSHubSetting, strings.Join(addresses, "\n")); err != nil {
		f.t.Fatal(err)
	}
}

// due makes every source due again and clears the host spacing, as if time had passed there.
func (f *fixture) due() {
	f.t.Helper()
	if _, err := f.db.Exec("UPDATE sources SET next_fetch_at = 0"); err != nil {
		f.t.Fatal(err)
	}
	f.poller.hosts = hostGate{}
}

func TestInstancesParsesTheSetting(t *testing.T) {
	for raw, want := range map[string][]string{
		"":                      nil,
		"  \n\n ":               nil,
		"http://a":              {"http://a"},
		"http://a/":             {"http://a"},
		"http://a\nhttp://b/\n": {"http://a", "http://b"},
		" http://a \r\nhttp://B\nhttp://b/\nhttp://A": {"http://a", "http://B"},
	} {
		if got := Instances(raw); !reflect.DeepEqual(got, want) {
			t.Errorf("Instances(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestRouteFallsBackWhenAnInstanceIsUnavailable(t *testing.T) {
	for name, setup := range map[string]func(*instance){
		"server error": func(i *instance) { i.status.Store(http.StatusInternalServerError) },
		"unavailable":  func(i *instance) { i.status.Store(http.StatusServiceUnavailable) },
		"rate limited": func(i *instance) { i.status.Store(http.StatusTooManyRequests) },
		"no answer":    func(i *instance) { i.Close() },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			primary, fallback := newInstance(t, http.StatusOK), newInstance(t, http.StatusOK)
			setup(primary)
			f.useInstances(primary.URL, fallback.URL)
			id := f.add(store.NewSource{RSSHubPath: "/route"})

			if n := f.poller.fetchDue(context.Background()); n != 2 {
				t.Fatalf("created %d entries", n)
			}
			if fallback.hits.Load() != 1 {
				t.Errorf("the fallback served %d requests", fallback.hits.Load())
			}
			if f.int("SELECT consecutive_failures FROM sources WHERE id = ?", id) != 0 ||
				f.text("SELECT last_error FROM sources WHERE id = ?", id) != "" {
				t.Error("a route served by the fallback was counted as a failure")
			}
			if got := f.text("SELECT rsshub_instance FROM sources WHERE id = ?", id); got != fallback.URL {
				t.Errorf("rsshub_instance = %q, want %q", got, fallback.URL)
			}
		})
	}
}

func TestFailedInstanceCoolsDownForAllRoutes(t *testing.T) {
	f := newFixture(t)
	primary, fallback := newInstance(t, http.StatusInternalServerError), newInstance(t, http.StatusOK)
	f.useInstances(primary.URL, fallback.URL)
	for _, route := range []string{"/a", "/b"} {
		f.add(store.NewSource{RSSHubPath: route})
	}
	f.poller.fetchDue(context.Background())
	if fallback.hits.Load() != 2 {
		t.Fatalf("the fallback served %d of 2 routes", fallback.hits.Load())
	}
	// Workers may have started on the primary together; after the first failure none goes there.
	if primary.hits.Load() > int32(workers) || primary.hits.Load() < 1 {
		t.Fatalf("the primary got %d requests", primary.hits.Load())
	}
	hits := primary.hits.Load()

	f.now = f.now.Add(30 * time.Second)
	f.due()
	f.poller.fetchDue(context.Background())
	if primary.hits.Load() != hits {
		t.Errorf("the primary was tried while cooling down (%d requests, was %d)", primary.hits.Load(), hits)
	}
	if fallback.hits.Load() != 4 {
		t.Errorf("the fallback served %d routes, want 4", fallback.hits.Load())
	}

	// After the cool-down the primary is first again; it has recovered.
	primary.status.Store(http.StatusOK)
	f.now = f.now.Add(2 * time.Minute)
	f.due()
	f.poller.fetchDue(context.Background())
	if primary.hits.Load() != hits+2 || fallback.hits.Load() != 4 {
		t.Errorf("after the cool-down: primary %d requests (was %d), fallback %d", primary.hits.Load(), hits, fallback.hits.Load())
	}
	if got := f.text("SELECT rsshub_instance FROM sources WHERE rsshub_path = '/a'"); got != primary.URL {
		t.Errorf("rsshub_instance = %q, want the primary", got)
	}
}

func TestInstanceCooldownHonorsRetryAfter(t *testing.T) {
	f := newFixture(t)
	primary, fallback := newInstance(t, http.StatusTooManyRequests), newInstance(t, http.StatusOK)
	primary.header.Store(http.Header{"Retry-After": {"600"}})
	f.useInstances(primary.URL, fallback.URL)
	f.add(store.NewSource{RSSHubPath: "/route"})
	f.poller.fetchDue(context.Background())

	f.now = f.now.Add(9 * time.Minute)
	f.due()
	f.poller.fetchDue(context.Background())
	if primary.hits.Load() != 1 {
		t.Errorf("the primary was tried again inside its Retry-After (%d requests)", primary.hits.Load())
	}
	f.now = f.now.Add(2 * time.Minute)
	f.due()
	f.poller.fetchDue(context.Background())
	if primary.hits.Load() != 2 {
		t.Errorf("the primary was not tried after its Retry-After (%d requests)", primary.hits.Load())
	}
}

func TestInstanceBackoffGrowsWithItsFailures(t *testing.T) {
	var g instanceGate
	now := time.UnixMilli(1_790_000_000_000)
	var last time.Duration
	for n := 1; n <= flatProbes+3; n++ {
		last = g.fail("http://a", now, "down", 0).Sub(now)
		if last != retryDelay(n) {
			t.Fatalf("cool-down after %d failures is %s, want %s", n, last, retryDelay(n))
		}
	}
	if last <= probeDelay {
		t.Errorf("the cool-down stayed at %s", last)
	}
	if until := g.fail("http://b", now, "slow down", time.Hour); until.Sub(now) != time.Hour {
		t.Errorf("Retry-After ignored: %s", until.Sub(now))
	}
	if _, _, cooling := g.cooling("http://a", now.Add(last)); cooling {
		t.Error("still cooling down when the time is up")
	}
	g.ok("http://a")
	if _, _, cooling := g.cooling("http://a", now); cooling {
		t.Error("an instance that answered is still cooling down")
	}
	if until := g.fail("http://a", now, "down", 0); until.Sub(now) != probeDelay {
		t.Errorf("failures were not forgotten: %s", until.Sub(now))
	}
	// Backoff is per instance: the other kept its own state.
	if _, _, cooling := g.cooling("http://b", now); !cooling {
		t.Error("another instance's cool-down was lost")
	}
}

func TestRouteErrorDoesNotFallBack(t *testing.T) {
	f := newFixture(t)
	primary, fallback := newInstance(t, http.StatusNotFound), newInstance(t, http.StatusOK)
	f.useInstances(primary.URL, fallback.URL)
	id := f.add(store.NewSource{RSSHubPath: "/gone"})
	f.poller.fetchDue(context.Background())
	if fallback.hits.Load() != 0 {
		t.Error("a route that does not exist was tried on the next instance")
	}
	if got := f.text("SELECT last_error FROM sources WHERE id = ?", id); !strings.Contains(got, "no longer exists") ||
		!strings.HasPrefix(got, primary.host()) {
		t.Errorf("last_error = %q", got)
	}
	if f.int("SELECT consecutive_failures FROM sources WHERE id = ?", id) != 1 {
		t.Error("failure not counted")
	}
	if _, _, cooling := f.poller.rsshub.cooling(primary.URL, f.now); cooling {
		t.Error("an instance that answered 404 is cooling down")
	}
}

func TestSourceFailsOnlyWhenEveryInstanceFailed(t *testing.T) {
	f := newFixture(t)
	primary, fallback := newInstance(t, http.StatusInternalServerError), newInstance(t, http.StatusTooManyRequests)
	f.useInstances(primary.URL, fallback.URL)
	id := f.add(store.NewSource{RSSHubPath: "/route"})
	f.poller.fetchDue(context.Background())

	if f.int("SELECT consecutive_failures FROM sources WHERE id = ?", id) != 1 {
		t.Fatal("failure not counted once")
	}
	want := primary.host() + ": status 500; " + fallback.host() + ": status 429"
	if got := f.text("SELECT last_error FROM sources WHERE id = ?", id); got != want {
		t.Errorf("last_error = %q, want %q", got, want)
	}
	if f.int("SELECT next_fetch_at FROM sources WHERE id = ?", id) <= f.now.UnixMilli() {
		t.Error("the source was not scheduled for later")
	}

	// While both cool down, a source that comes due costs no request and no failure.
	hits := primary.hits.Load() + fallback.hits.Load()
	other := f.add(store.NewSource{RSSHubPath: "/other"})
	f.poller.fetchDue(context.Background())
	if primary.hits.Load()+fallback.hits.Load() != hits {
		t.Error("an instance that is cooling down got a request")
	}
	if f.int("SELECT consecutive_failures FROM sources WHERE id = ?", other) != 0 {
		t.Error("a route that could not be tried was counted as a failure")
	}

	// A success on the next round clears the failure and says who served.
	fallback.status.Store(http.StatusOK)
	f.now = f.now.Add(2 * time.Minute)
	f.due()
	f.poller.fetchDue(context.Background())
	if f.int("SELECT consecutive_failures FROM sources WHERE id = ?", id) != 0 ||
		f.text("SELECT rsshub_instance FROM sources WHERE id = ?", id) != fallback.URL {
		t.Error("the source did not recover on the fallback")
	}
}

func TestFallbackWaitsForItsHostsPlace(t *testing.T) {
	f := newFixture(t)
	primary, fallback := newInstance(t, http.StatusInternalServerError), newInstance(t, http.StatusOK)
	f.useInstances(primary.URL, fallback.URL)
	id := f.add(store.NewSource{RSSHubPath: "/route"})
	for range hostLimit {
		if !f.poller.hosts.acquire(fallback.host(), f.now, 0) {
			t.Fatal("cannot fill the fallback host")
		}
	}

	f.poller.fetchDue(context.Background())
	if fallback.hits.Load() != 0 {
		t.Error("the fallback was used above the host limit")
	}
	if f.int("SELECT consecutive_failures FROM sources WHERE id = ?", id) != 0 {
		t.Error("a fallback skipped for a busy host was counted as a failure")
	}
	if f.int("SELECT next_fetch_at FROM sources WHERE id = ?", id) > f.now.UnixMilli() {
		t.Error("the source is no longer due")
	}

	f.poller.hosts.release(fallback.host())
	f.now = f.now.Add(time.Second)
	f.poller.fetchDue(context.Background())
	if fallback.hits.Load() != 1 || f.int("SELECT count(*) FROM entries") != 2 {
		t.Errorf("the fallback served %d requests once it had room", fallback.hits.Load())
	}
	// Every place the route took has been given back.
	f.poller.hosts.release(fallback.host())
	f.poller.hosts.mu.Lock()
	defer f.poller.hosts.mu.Unlock()
	if len(f.poller.hosts.active) != 0 {
		t.Errorf("places still taken: %v", f.poller.hosts.active)
	}
}

func TestSingleInstanceFailsAsBefore(t *testing.T) {
	f := newFixture(t)
	only := newInstance(t, http.StatusInternalServerError)
	f.useInstances(only.URL + "/")
	id := f.add(store.NewSource{RSSHubPath: "/route"})
	f.poller.fetchDue(context.Background())
	if got := f.text("SELECT last_error FROM sources WHERE id = ?", id); got != "status 500" {
		t.Errorf("last_error = %q", got)
	}
	if _, _, cooling := f.poller.rsshub.cooling(only.URL, f.now); cooling {
		t.Error("a lone instance has a cool-down")
	}
}
