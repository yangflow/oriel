package importer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fakeToken = "fake-token-0000"

func testClient() *Client {
	c := NewClient()
	c.Sleep = func(ctx context.Context, d time.Duration) error { return ctx.Err() }
	return c
}

func noProgress(string, int, int) {}

func TestParseBase(t *testing.T) {
	for _, tc := range []struct {
		raw, service, want string
		ok                 bool
	}{
		{"https://news.example.com/", Miniflux, "https://news.example.com", true},
		{" http://192.168.1.5:8080/rss ", Miniflux, "http://192.168.1.5:8080/rss", true},
		{"https://rss.example.com/api/greader.php", FreshRSS, "https://rss.example.com", true},
		{"https://rss.example.com/p/api/greader.php/", FreshRSS, "https://rss.example.com/p", true},
		{"ftp://example.com", Miniflux, "", false},
		{"example.com", Miniflux, "", false},
		{"https://user:pw@example.com", Miniflux, "", false},
		{"https://example.com/?x=1", Miniflux, "", false},
		{"", Miniflux, "", false},
	} {
		got, err := ParseBase(tc.raw, tc.service)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("ParseBase(%q) = %q, %v", tc.raw, got, err)
		}
		if err != nil && Code(err) != ErrInvalidBase {
			t.Errorf("ParseBase(%q) code = %q", tc.raw, Code(err))
		}
	}
}

func minifluxServer(t *testing.T, starred, read int) *httptest.Server {
	t.Helper()
	page := func(w http.ResponseWriter, r *http.Request, total int, tag string, starredFlag bool, status string) {
		var offset, limit int
		fmt.Sscan(r.URL.Query().Get("offset"), &offset)
		fmt.Sscan(r.URL.Query().Get("limit"), &limit)
		var items []string
		for i := offset; i < min(offset+limit, total); i++ {
			items = append(items, fmt.Sprintf(`{"id":%d,"url":"https://a.example/%s/%d","title":"T%d","status":%q,"starred":%t,
				"published_at":"2024-05-01T10:00:00Z","content":"<p>x</p>","future":{"x":1},
				"feed":{"feed_url":"https://a.example/feed.xml","id":1}}`, i, tag, i, i, status, starredFlag))
		}
		fmt.Fprintf(w, `{"total":%d,"entries":[%s],"extra":true}`, total, strings.Join(items, ","))
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Auth-Token") != fakeToken {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/v1/feeds":
			fmt.Fprint(w, `[{"id":1,"feed_url":"https://a.example/feed.xml","site_url":"https://a.example","title":"A","category":{"id":3,"title":"News"},"unknown":1},
				{"id":2,"feed_url":"https://b.example/feed.xml","site_url":"","title":"B","category":{"title":"News"}},
				{"id":3,"feed_url":"","title":"Broken","category":{"title":""}}]`)
		case r.URL.Path == "/v1/entries" && r.URL.Query().Get("starred") == "true":
			page(w, r, starred, "s", true, "unread")
		case r.URL.Path == "/v1/entries" && r.URL.Query().Get("status") == "read":
			page(w, r, read, "r", false, "read")
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestMinifluxPagination(t *testing.T) {
	srv := minifluxServer(t, 230, 5)
	defer srv.Close()
	imp, _ := For(Miniflux)
	var last [3]int
	snap, err := imp.Fetch(context.Background(), testClient(), Request{Base: srv.URL, Credential: fakeToken},
		func(stage string, done, total int) { last = [3]int{len(stage), done, total} })
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Counts(); got != (Counts{Sources: 2, Folders: 1, Starred: 230, Read: 5}) {
		t.Fatalf("counts = %+v", got)
	}
	if last[1] != 5 {
		t.Fatalf("last progress = %v", last)
	}
	if snap.Feeds[0].Folders[0] != "News" || snap.Entries[0].Published.IsZero() || snap.Entries[0].ContentHTML == "" {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestMinifluxUnauthorized(t *testing.T) {
	srv := minifluxServer(t, 1, 1)
	defer srv.Close()
	imp, _ := For(Miniflux)
	_, err := imp.Fetch(context.Background(), testClient(), Request{Base: srv.URL, Credential: "wrong"}, noProgress)
	if Code(err) != ErrUnauthorized {
		t.Fatalf("err = %v", err)
	}
}

func TestMinifluxLimit(t *testing.T) {
	srv := minifluxServer(t, MaxEntries+300, 0)
	defer srv.Close()
	imp, _ := For(Miniflux)
	snap, err := imp.Fetch(context.Background(), testClient(), Request{Base: srv.URL, Credential: fakeToken}, noProgress)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Entries) != MaxEntries || len(snap.Notes) != 1 || snap.Notes[0].Kind != StageStarred {
		t.Fatalf("entries = %d, notes = %+v", len(snap.Entries), snap.Notes)
	}
}

func TestRateLimited(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()
	var waits []time.Duration
	c := testClient()
	c.Sleep = func(ctx context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	if _, err := c.Do(context.Background(), "GET", srv.URL, nil, nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || len(waits) != 2 || waits[0] != 2*time.Second {
		t.Fatalf("calls = %d, waits = %v", calls.Load(), waits)
	}

	always := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "9999")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer always.Close()
	waits = nil
	c.used = false
	_, err := c.Do(context.Background(), "GET", always.URL, nil, nil)
	if Code(err) != ErrRateLimited || len(waits) != maxRetries || waits[0] != maxRetryWait {
		t.Fatalf("err = %v, waits = %v", err, waits)
	}
}

func TestResponseTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", 100))
	}))
	defer srv.Close()
	c := testClient()
	c.MaxBody = 50
	if _, err := c.Do(context.Background(), "GET", srv.URL, nil, nil); Code(err) != ErrTooLarge {
		t.Fatalf("err = %v", err)
	}
}

func TestRedirectToOtherHostRefused(t *testing.T) {
	var leaked atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(true)
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/v1/feeds", http.StatusFound)
	}))
	defer srv.Close()
	imp, _ := For(Miniflux)
	_, err := imp.Fetch(context.Background(), testClient(), Request{Base: srv.URL, Credential: fakeToken}, noProgress)
	if Code(err) != ErrRedirected || leaked.Load() {
		t.Fatalf("err = %v, leaked = %v", err, leaked.Load())
	}
	if strings.Contains(err.Error(), fakeToken) || strings.Contains(err.Error(), other.URL) {
		t.Fatalf("error leaks: %v", err)
	}
}

func TestUnreachableHidesTransportError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()
	imp, _ := For(Miniflux)
	_, err := imp.Fetch(context.Background(), testClient(), Request{Base: base, Credential: fakeToken}, noProgress)
	if Code(err) != ErrUnreachable || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("err = %v", err)
	}
}

func freshServer(t *testing.T, starredPages int) *httptest.Server {
	t.Helper()
	const auth = "fake-auth-9999"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const base = "/api/greader.php"
		if r.URL.Path == base+"/accounts/ClientLogin" {
			r.ParseForm()
			if r.Method != "POST" || r.PostForm.Get("Email") != "me" || r.PostForm.Get("Passwd") != fakeToken {
				http.Error(w, "Error=BadAuthentication", http.StatusUnauthorized)
				return
			}
			fmt.Fprintf(w, "SID=x\nLSID=y\nAuth=%s\n", auth)
			return
		}
		if r.Header.Get("Authorization") != "GoogleLogin auth="+auth {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case base + "/reader/api/0/subscription/list":
			fmt.Fprint(w, `{"subscriptions":[{"id":"feed/1","title":"A","url":"https://a.example/feed.xml","htmlUrl":"https://a.example",
				"categories":[{"id":"user/-/label/Tech","label":"Tech"}],"iconUrl":"x"}]}`)
		case base + "/reader/api/0/tag/list":
			fmt.Fprint(w, `{"tags":[{"id":"user/-/state/com.google/starred"},{"id":"user/-/label/Tech","type":"folder"},{"id":"user/-/label/Empty","type":"folder"}]}`)
		case base + "/reader/api/0/stream/contents/user/-/state/com.google/starred":
			c := r.URL.Query().Get("c")
			n := 0
			fmt.Sscan(c, &n)
			cont := ""
			if n+1 < starredPages {
				cont = fmt.Sprint(n + 1)
			}
			fmt.Fprintf(w, `{"items":[{"id":"i%d","title":"S%d","published":1714557600,"canonical":[{"href":"https://a.example/s/%d"}],
				"summary":{"content":"<p>s</p>"},"categories":["user/-/state/com.google/starred","user/-/state/com.google/read"],
				"origin":{"streamId":"feed/1"}}],"continuation":%q}`, n, n, n, cont)
		case base + "/reader/api/0/stream/contents/user/-/state/com.google/read":
			fmt.Fprint(w, `{"items":[
				{"title":"R","alternate":[{"href":"https://a.example/r/1"}],"origin":{"streamId":"feed/1"}},
				{"title":"S0 again","canonical":[{"href":"https://a.example/s/0"}],"origin":{"streamId":"feed/1"}},
				{"title":"Lost","canonical":[{"href":"https://z.example/1"}],"origin":{"streamId":"feed/99"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestFreshRSS(t *testing.T) {
	srv := freshServer(t, 3)
	defer srv.Close()
	imp, _ := For(FreshRSS)
	snap, err := imp.Fetch(context.Background(), testClient(), Request{Base: srv.URL, Username: "me", Credential: fakeToken}, noProgress)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Counts(); got != (Counts{Sources: 1, Folders: 2, Starred: 3, Read: 4}) {
		t.Fatalf("counts = %+v (%+v)", got, snap.Entries)
	}
	if len(snap.Entries) != 4 || snap.Entries[0].Published.Unix() != 1714557600 || snap.Entries[0].ContentHTML == "" {
		t.Fatalf("entries = %+v", snap.Entries)
	}
}

func TestFreshRSSUnauthorized(t *testing.T) {
	srv := freshServer(t, 1)
	defer srv.Close()
	imp, _ := For(FreshRSS)
	_, err := imp.Fetch(context.Background(), testClient(), Request{Base: srv.URL, Username: "me", Credential: "wrong"}, noProgress)
	if Code(err) != ErrUnauthorized {
		t.Fatalf("err = %v", err)
	}
}

func TestCancelStopsFetch(t *testing.T) {
	srv := minifluxServer(t, 1000, 0)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	imp, _ := For(Miniflux)
	_, err := imp.Fetch(ctx, testClient(), Request{Base: srv.URL, Credential: fakeToken},
		func(stage string, done, total int) {
			if stage == StageStarred {
				cancel()
			}
		})
	if err != context.Canceled {
		t.Fatalf("err = %v", err)
	}
}

func TestMergeKeepsContentOfStarredOnly(t *testing.T) {
	var m merge
	m.add(Entry{FeedURL: "f", Link: "a", ContentHTML: "<p>a</p>", Starred: true})
	m.add(Entry{FeedURL: "f", Link: "a", ContentHTML: "<p>a</p>", Read: true})
	m.add(Entry{FeedURL: "f", Link: "b", ContentHTML: "<p>b</p>", Read: true})
	if len(m.entries) != 2 || m.entries[0].ContentHTML == "" || !m.entries[0].Read || m.entries[1].ContentHTML != "" {
		t.Fatalf("entries = %+v", m.entries)
	}
}
