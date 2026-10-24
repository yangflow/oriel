package fetch

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/ai"
	"github.com/yangflow/oriel/internal/store"
	"github.com/yangflow/oriel/internal/store/storetest"
)

type fixture struct {
	t      *testing.T
	store  *store.Store
	db     *sql.DB // a second connection for assertions
	poller *Poller
	now    time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	file := filepath.Join(t.TempDir(), "oriel.sqlite")
	st, err := store.Open(context.Background(), storetest.Path(t, file))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	db, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f := &fixture{t: t, store: st, db: db, now: time.UnixMilli(1_790_000_000_000)}
	f.poller = New(st, "0.1.0")
	f.poller.now = func() time.Time { return f.now }
	f.poller.guarded = &http.Client{Transport: offline{}}
	f.poller.spacing = 0 // tests share one local host; the spacing has its own tests
	return f
}

// offline fails every request, so tests never reach the network through links inside feeds.
type offline struct{}

func (offline) RoundTrip(*http.Request) (*http.Response, error) { return nil, errors.New("offline") }

func (f *fixture) add(src store.NewSource) int64 {
	f.t.Helper()
	if src.Kind == "" {
		src.Kind = "article"
	}
	if src.FetchInterval == 0 {
		src.FetchInterval = 15 * time.Minute
	}
	created, err := f.store.CreateSource(context.Background(), src, "", f.now)
	if err != nil {
		f.t.Fatal(err)
	}
	return created.ID
}

func (f *fixture) int(query string, args ...any) int64 {
	f.t.Helper()
	var n int64
	if err := f.db.QueryRow(query, args...).Scan(&n); err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (f *fixture) text(query string, args ...any) string {
	f.t.Helper()
	var s string
	if err := f.db.QueryRow(query, args...).Scan(&s); err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
	return s
}

func TestPollStoresAndRefetches(t *testing.T) {
	f := newFixture(t)
	var requests atomic.Int32
	var lastIfNoneMatch atomic.Value
	lastIfNoneMatch.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		lastIfNoneMatch.Store(r.Header.Get("If-None-Match"))
		if r.Header.Get("User-Agent") != "Oriel/0.1.0" {
			t.Errorf("user agent %q", r.Header.Get("User-Agent"))
		}
		if r.Header.Get("If-None-Match") == `"v2"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		if requests.Load() > 1 {
			w.Header().Set("ETag", `"v2"`)
		}
		w.Write([]byte(rssFeed))
	}))
	defer srv.Close()
	id := f.add(store.NewSource{URL: srv.URL + "/feed.xml"})

	if n := f.poller.fetchDue(context.Background()); n != 2 {
		t.Fatalf("first poll created %d entries", n)
	}
	if f.int("SELECT count(*) FROM entries WHERE extract_status = 'pending'") != 2 {
		t.Fatal("new article entries are not pending extraction")
	}
	if f.text("SELECT title FROM sources WHERE id = ?", id) != "Example Blog" ||
		f.text("SELECT etag FROM sources WHERE id = ?", id) != `"v1"` {
		t.Fatal("source title or ETag not recorded")
	}
	// The newest item of the feed is inserted last, so it sorts first by (created_at, id) descending.
	if f.text("SELECT guid FROM entries ORDER BY created_at DESC, id DESC LIMIT 1") != "post-1" {
		t.Fatal("feed order lost")
	}
	if n := f.poller.fetchDue(context.Background()); n != 0 || requests.Load() != 1 {
		t.Fatalf("polled a source that is not due: %d new, %d requests", n, requests.Load())
	}

	f.db.Exec("UPDATE entries SET is_read = 1, starred = 1")
	updated := f.int("SELECT max(updated_at) FROM entries")
	f.now = f.now.Add(15 * time.Minute)
	if n := f.poller.fetchDue(context.Background()); n != 0 {
		t.Fatalf("refetch created %d entries", n)
	}
	if lastIfNoneMatch.Load() != `"v1"` {
		t.Fatalf("If-None-Match = %q", lastIfNoneMatch.Load())
	}
	if f.int("SELECT count(*) FROM entries") != 2 || f.int("SELECT count(*) FROM entries WHERE is_read = 1 AND starred = 1") != 2 {
		t.Fatal("refetch duplicated entries or reset their state")
	}
	if f.int("SELECT max(updated_at) FROM entries") != updated {
		t.Fatal("unchanged entries got a new updated_at")
	}

	f.now = f.now.Add(15 * time.Minute)
	f.poller.fetchDue(context.Background())
	if f.int("SELECT last_fetched_at FROM sources WHERE id = ?", id) != f.now.UnixMilli() ||
		f.int("SELECT next_fetch_at FROM sources WHERE id = ?", id) != f.now.Add(15*time.Minute).UnixMilli() {
		t.Fatal("304 was not recorded as a success")
	}
}

func TestPollQueuesAI(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	feed := rssFeed
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(feed)) }))
	defer srv.Close()
	id := f.add(store.NewSource{URL: srv.URL})
	on := true
	if _, err := f.store.UpdateSource(ctx, id, store.SourcePatch{AutoSummary: &on, AutoTranslate: &on}, f.now); err != nil {
		t.Fatal(err)
	}
	// Without an endpoint and a model nothing is queued.
	f.poller.fetchDue(ctx)
	if f.int("SELECT count(*) FROM entry_ai") != 0 {
		t.Fatal("queued AI tasks while AI is not configured")
	}

	f.store.SetSetting(ctx, ai.BaseURLSetting, "http://localhost:11434/v1")
	f.store.SetSetting(ctx, ai.ModelSetting, "local")
	feed = strings.Replace(strings.Replace(rssFeed, "post-1", "post-3", 1), "<link>/posts/1</link>", "<link>/posts/3</link>", 1)
	f.now = f.now.Add(15 * time.Minute)
	if n := f.poller.fetchDue(ctx); n != 1 {
		t.Fatalf("second poll created %d entries", n)
	}
	// Only the new entry gets both tasks; entries that came before the switch are not filled in.
	if f.int("SELECT count(*) FROM entry_ai WHERE status = 'pending' AND requested = 0") != 2 ||
		f.int("SELECT count(DISTINCT e.guid) FROM entry_ai a JOIN entries e ON e.id = a.entry_id WHERE e.guid = 'post-3'") != 1 {
		t.Fatal("new entry not queued for summary and translation")
	}
}

func TestPollChangedGUID(t *testing.T) {
	f := newFixture(t)
	feed := rssFeed
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(feed)) }))
	defer srv.Close()
	f.add(store.NewSource{URL: srv.URL})
	f.poller.fetchDue(context.Background())
	if _, err := f.db.Exec("UPDATE entries SET is_read = 1, starred = 1 WHERE guid = 'post-1'"); err != nil {
		t.Fatal(err)
	}
	// The feed gives the same article another GUID: it stays one starred, read entry.
	feed = strings.Replace(rssFeed, "post-1", "post-1-renamed", 1)
	f.now = f.now.Add(15 * time.Minute)
	if n := f.poller.fetchDue(context.Background()); n != 0 {
		t.Fatalf("a changed GUID created %d entries", n)
	}
	if f.int("SELECT count(*) FROM entries") != 2 ||
		f.int("SELECT count(*) FROM entries WHERE guid = 'post-1-renamed' AND is_read = 1 AND starred = 1") != 1 {
		t.Fatal("the entry was not kept under the new GUID")
	}
}

func TestPollKinds(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(atomFeed)) }))
	defer srv.Close()
	f.add(store.NewSource{URL: srv.URL, Kind: "video"})
	f.poller.fetchDue(context.Background())
	if f.text("SELECT extract_status FROM entries") != "skipped" || f.text("SELECT platform FROM entries") != "youtube" ||
		f.text("SELECT images FROM entries") != `[{"url":"https://i.ytimg.com/vi/xyz/full.jpg","width":1280,"height":720}]` {
		t.Fatal("video entry stored wrongly")
	}
}

func TestPollFailuresBackOff(t *testing.T) {
	f := newFixture(t)
	status := http.StatusInternalServerError
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Write([]byte(rssFeed))
	}))
	defer srv.Close()
	id := f.add(store.NewSource{URL: srv.URL})
	for failures, wait := range map[int]time.Duration{1: time.Minute, 5: time.Minute, 6: 2 * time.Minute, 8: 8 * time.Minute, 9: 10 * time.Minute, 20: 10 * time.Minute} {
		if got := retryDelay(failures); got != wait {
			t.Errorf("retryDelay(%d) = %s, want %s", failures, got, wait)
		}
	}
	for i := 1; i <= 6; i++ {
		f.poller.fetchDue(context.Background())
		if f.int("SELECT consecutive_failures FROM sources WHERE id = ?", id) != int64(i) {
			t.Fatalf("after %d failures", i)
		}
		next := time.UnixMilli(f.int("SELECT next_fetch_at FROM sources WHERE id = ?", id))
		if next.Sub(f.now) != retryDelay(i) {
			t.Fatalf("failure %d waits %s", i, next.Sub(f.now))
		}
		f.now = next
	}
	if f.text("SELECT last_error FROM sources WHERE id = ?", id) != "status 500" {
		t.Fatal("last_error not recorded")
	}
	status = http.StatusOK
	f.poller.fetchDue(context.Background())
	if f.int("SELECT consecutive_failures FROM sources WHERE id = ?", id) != 0 || f.text("SELECT last_error FROM sources WHERE id = ?", id) != "" {
		t.Fatal("success did not clear the failures")
	}
}

func TestPollRetryAfter(t *testing.T) {
	f := newFixture(t)
	status, header := http.StatusTooManyRequests, ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if header != "" {
			w.Header().Set("Retry-After", header)
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	id := f.add(store.NewSource{URL: srv.URL})
	cases := []struct {
		name   string
		status int
		header string
		want   time.Duration
	}{
		{"seconds", http.StatusTooManyRequests, "300", 5 * time.Minute},
		{"seconds on 503", http.StatusServiceUnavailable, "120", 2 * time.Minute},
		{"http date", http.StatusServiceUnavailable, f.now.Add(90 * time.Minute).UTC().Format(http.TimeFormat), 90 * time.Minute},
		{"shorter than the schedule", http.StatusTooManyRequests, "5", time.Minute},
		{"capped at a day", http.StatusTooManyRequests, "99999999", 24 * time.Hour},
		{"date capped at a day", http.StatusTooManyRequests, f.now.Add(30 * 24 * time.Hour).UTC().Format(http.TimeFormat), 24 * time.Hour},
		{"huge number", http.StatusTooManyRequests, "99999999999999999999", time.Minute},
		{"invalid", http.StatusTooManyRequests, "soon", time.Minute},
		{"negative", http.StatusTooManyRequests, "-30", time.Minute},
		{"date in the past", http.StatusTooManyRequests, f.now.Add(-time.Hour).UTC().Format(http.TimeFormat), time.Minute},
		{"missing", http.StatusTooManyRequests, "", time.Minute},
		{"ignored on 500", http.StatusInternalServerError, "300", time.Minute},
		{"ignored on 404", http.StatusNotFound, "300", time.Minute},
	}
	for _, c := range cases {
		status, header = c.status, c.header
		if _, err := f.db.Exec("UPDATE sources SET consecutive_failures = 0, next_fetch_at = 0 WHERE id = ?", id); err != nil {
			t.Fatal(err)
		}
		f.poller.hosts = hostGate{}
		f.poller.fetchDue(context.Background())
		next := time.UnixMilli(f.int("SELECT next_fetch_at FROM sources WHERE id = ?", id))
		if got := next.Sub(f.now); got != c.want {
			t.Errorf("%s: waits %s, want %s", c.name, got, c.want)
		}
		if f.int("SELECT consecutive_failures FROM sources WHERE id = ?", id) != 1 {
			t.Errorf("%s: failure not counted", c.name)
		}
	}

	// A later failure never waits less than the schedule says.
	status, header = http.StatusTooManyRequests, "30"
	f.db.Exec("UPDATE sources SET consecutive_failures = 7, next_fetch_at = 0 WHERE id = ?", id)
	f.poller.hosts = hostGate{}
	f.poller.fetchDue(context.Background())
	if got := time.UnixMilli(f.int("SELECT next_fetch_at FROM sources WHERE id = ?", id)).Sub(f.now); got != retryDelay(8) {
		t.Errorf("long schedule: waits %s, want %s", got, retryDelay(8))
	}
	if got := f.text("SELECT last_error FROM sources WHERE id = ?", id); got != "status 429" {
		t.Errorf("last_error = %q", got)
	}
}

func TestPollErrors(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/html":
			w.Write([]byte("<html><body>hello</body></html>"))
		case "/huge":
			w.Write([]byte(strings.Repeat(" ", maxBytes+1)))
		case "/cloudflare":
			w.Header().Set("Cf-Mitigated", "challenge")
			w.WriteHeader(http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f.store.SetSetting(context.Background(), RSSHubSetting, srv.URL+"/")
	cases := map[int64]string{
		f.add(store.NewSource{URL: srv.URL + "/html"}):       "not a feed",
		f.add(store.NewSource{URL: srv.URL + "/huge"}):       "larger than 8 MiB",
		f.add(store.NewSource{URL: srv.URL + "/cloudflare"}): "blocked by Cloudflare",
		f.add(store.NewSource{RSSHubPath: "/gone/route"}):    "RSSHub route no longer exists",
	}
	f.poller.fetchDue(context.Background())
	for id, want := range cases {
		if got := f.text("SELECT last_error FROM sources WHERE id = ?", id); !strings.Contains(got, want) {
			t.Errorf("source %d: %q, want %q", id, got, want)
		}
	}

	f.db.Exec("DELETE FROM sources")
	f.store.SetSetting(context.Background(), RSSHubSetting, "")
	id := f.add(store.NewSource{RSSHubPath: "/any"})
	f.poller.fetchDue(context.Background())
	if got := f.text("SELECT last_error FROM sources WHERE id = ?", id); got != "the RSSHub address is not set" {
		t.Errorf("without RSSHub: %q", got)
	}
}

func TestPollGoneRSSHubRoute(t *testing.T) {
	for name, instances := range map[string]int{"one instance": 1, "two instances": 2} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusGone)
			}))
			defer srv.Close()
			bases := srv.URL + "/"
			if instances == 2 {
				bases += "\n" + srv.URL + "/second/"
			}
			f.store.SetSetting(context.Background(), RSSHubSetting, bases)
			id := f.add(store.NewSource{RSSHubPath: "/removed/route"})
			f.poller.fetchDue(context.Background())
			if got := f.text("SELECT last_error_kind FROM sources WHERE id = ?", id); got != ErrorGone {
				t.Errorf("last_error_kind = %q, want %q", got, ErrorGone)
			}
			if got := requests.Load(); got != 1 {
				t.Errorf("%d requests, want 1: a 410 is the route's answer and tries no other instance", got)
			}
			if got := time.UnixMilli(f.int("SELECT next_fetch_at FROM sources WHERE id = ?", id)).Sub(f.now); got != longBackoff {
				t.Errorf("waits %s, want %s", got, longBackoff)
			}
		})
	}
}

func TestPollSkipsPausedSources(t *testing.T) {
	f := newFixture(t)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	defer srv.Close()
	id := f.add(store.NewSource{URL: srv.URL})
	f.db.Exec("UPDATE sources SET paused = 1 WHERE id = ?", id)
	f.poller.fetchDue(context.Background())
	if requests.Load() != 0 {
		t.Fatal("fetched a paused source")
	}
}

func TestRunStopsWithContext(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		f.poller.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}

// A fetch that panics is a failed one: the other sources are fetched, the panicking one waits for its retry and is
// not taken again at once.
func TestPanickingFetchCountsAsAFailure(t *testing.T) {
	f := newFixture(t)
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(rssFeed)) }))
	defer good.Close()
	broken := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer broken.Close()
	f.poller.client = &http.Client{Transport: panicTo{host: strings.TrimPrefix(broken.URL, "http://")}}
	okID := f.add(store.NewSource{URL: good.URL + "/feed.xml"})
	badID := f.add(store.NewSource{URL: broken.URL + "/feed.xml"})

	if n := f.poller.fetchDue(context.Background()); n != 2 {
		t.Fatalf("fetchDue brought %d entries, want 2 from the source that works", n)
	}
	if got := f.int("SELECT consecutive_failures FROM sources WHERE id = ?", badID); got != 1 {
		t.Errorf("failures %d, want 1", got)
	}
	if got := f.int("SELECT consecutive_failures FROM sources WHERE id = ?", okID); got != 0 {
		t.Errorf("the working source has %d failures", got)
	}
	if n := f.poller.fetchDue(context.Background()); n != 0 {
		t.Errorf("the second round brought %d entries; the panicking source was taken again at once", n)
	}
	if len(f.poller.Fetching()) != 0 {
		t.Errorf("sources still marked as fetching: %v", f.poller.Fetching())
	}
}

// panicTo panics for requests to one host and passes the rest on.
type panicTo struct{ host string }

func (p panicTo) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host == p.host {
		panic("transport broke")
	}
	return http.DefaultTransport.RoundTrip(r)
}

func TestPollLongFailureBacksOffFurther(t *testing.T) {
	f := newFixture(t)
	status := http.StatusInternalServerError
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Write([]byte(rssFeed))
	}))
	defer srv.Close()
	id := f.add(store.NewSource{URL: srv.URL})
	wait := func() time.Duration {
		f.t.Helper()
		f.db.Exec("UPDATE sources SET next_fetch_at = 0 WHERE id = ?", id)
		f.poller.fetchDue(context.Background())
		return time.UnixMilli(f.int("SELECT next_fetch_at FROM sources WHERE id = ?", id)).Sub(f.now)
	}

	f.db.Exec("UPDATE sources SET consecutive_failures = 40 WHERE id = ?", id)
	f.now = f.now.Add(longFailure - time.Minute)
	if got := wait(); got != backoffLimit {
		t.Fatalf("just under %s of failures waits %s, want %s", longFailure, got, backoffLimit)
	}
	f.now = f.now.Add(time.Minute)
	if got := wait(); got != longBackoff {
		t.Fatalf("after %s of failures waits %s, want %s", longFailure, got, longBackoff)
	}
	if f.int("SELECT consecutive_failures FROM sources WHERE id = ?", id) != 42 {
		t.Fatal("the longer wait changed the failure count")
	}

	// A source that was only idle for days (few failures) is not on the long schedule.
	f.db.Exec("UPDATE sources SET consecutive_failures = 1 WHERE id = ?", id)
	if got := wait(); got != probeDelay {
		t.Fatalf("second failure after a long idle waits %s, want %s", got, probeDelay)
	}

	// One success resets the count and the streak: the next failure is back on the short schedule.
	status = http.StatusOK
	wait()
	status = http.StatusInternalServerError
	if got := wait(); got != probeDelay {
		t.Fatalf("failure after a success waits %s, want %s", got, probeDelay)
	}
}

func TestPollGone(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer srv.Close()
	id := f.add(store.NewSource{URL: srv.URL})
	f.poller.fetchDue(context.Background())
	if got := f.text("SELECT last_error_kind FROM sources WHERE id = ?", id); got != ErrorGone {
		t.Errorf("last_error_kind = %q, want %q", got, ErrorGone)
	}
	if got := f.text("SELECT last_error FROM sources WHERE id = ?", id); got != "status 410: the feed was removed (Gone)" {
		t.Errorf("last_error = %q", got)
	}
	if got := time.UnixMilli(f.int("SELECT next_fetch_at FROM sources WHERE id = ?", id)).Sub(f.now); got != longBackoff {
		t.Errorf("first 410 waits %s, want %s", got, longBackoff)
	}
	if f.int("SELECT paused FROM sources WHERE id = ?", id) != 0 {
		t.Error("a 410 paused the source")
	}
}
