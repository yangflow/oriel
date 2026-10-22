package fetch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

func TestWebSubLinks(t *testing.T) {
	base, _ := url.Parse("https://example.com/feeds/main.xml")
	atom := `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom">
		<link rel="hub" href="https://hub.example/"/><link rel="self" href="/feeds/main.xml"/>
		<entry><link rel="hub" href="https://evil.example/"/><link rel="self" href="https://evil.example/self"/></entry></feed>`
	rss := `<?xml version="1.0"?><rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom"><channel>
		<atom:link rel="self" href="https://example.com/rss"/><atom:link href="https://hub.example/rss" rel="hub"/>
		<item><atom:link rel="hub" href="https://evil.example/"/></item></channel></rss>`
	jsonFeed := `{"version":"https://jsonfeed.org/version/1.1","feed_url":"https://example.com/feed.json",
		"hubs":[{"type":"WebSub","url":"https://hub.example/json"}],"items":[]}`
	for _, tc := range []struct {
		name, body, hub, self string
		header                http.Header
	}{
		{"atom feed level only", atom, "https://hub.example/", "https://example.com/feeds/main.xml", nil},
		{"rss with atom links", rss, "https://hub.example/rss", "https://example.com/rss", nil},
		{"json feed", jsonFeed, "https://hub.example/json", "https://example.com/feed.json", nil},
		{"no hub", `<rss version="2.0"><channel><title>x</title></channel></rss>`, "", "", nil},
		{"link header wins", atom, "https://hub.example/header", "https://example.com/feeds/main.xml",
			http.Header{"Link": {`<https://hub.example/header>; rel="hub", <https://other.example/self>; rel="alternate"`}}},
		{"relative hub in header", rss, "https://example.com/hub", "https://example.com/rss",
			http.Header{"Link": {`</hub>; rel=hub`}}},
		{"not http", `{"hubs":[{"type":"WebSub","url":"ftp://hub.example/"}]}`, "", "", nil},
	} {
		hub, self := websubLinks(tc.header, []byte(tc.body), base)
		if hub != tc.hub || self != tc.self {
			t.Errorf("%s: hub %q self %q, want %q %q", tc.name, hub, self, tc.hub, tc.self)
		}
	}
}

// hubTransport stands for the hub: it records the subscription requests and answers with status.
type hubTransport struct {
	status int
	forms  []url.Values
}

func (h *hubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodPost { // the site icon lookup goes through the same client
		return nil, errors.New("offline")
	}
	body, _ := io.ReadAll(r.Body)
	form, _ := url.ParseQuery(string(body))
	form.Set("method", r.Method)
	form.Set("url", r.URL.String())
	h.forms = append(h.forms, form)
	return &http.Response{StatusCode: h.status, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
}

func TestWebSubSubscribesAndFallsBackToPolling(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Link", `<https://hub.example/sub>; rel="hub"`)
		w.Write([]byte(rssFeed))
	}))
	defer srv.Close()
	hub := &hubTransport{status: http.StatusAccepted}
	f.poller.guarded = &http.Client{Transport: hub}
	id := f.add(store.NewSource{URL: srv.URL})

	// Off by default: the hub is not even recorded and no request goes out.
	f.poller.fetchDue(ctx)
	f.poller.websubRound(ctx)
	if got := f.text("SELECT websub_hub FROM sources WHERE id = ?", id); got != "" || len(hub.forms) != 0 {
		t.Fatalf("WebSub is off but hub = %q, requests = %d", got, len(hub.forms))
	}

	if err := f.store.SetSetting(ctx, WebSubSetting, "https://oriel.example.com/"); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Hour)
	f.poller.fetchDue(ctx)
	if got := f.text("SELECT websub_hub FROM sources WHERE id = ?", id); got != "https://hub.example/sub" {
		t.Fatalf("hub = %q", got)
	}
	if got := f.text("SELECT websub_topic FROM sources WHERE id = ?", id); got != srv.URL {
		t.Fatalf("topic = %q, want the fetched address", got)
	}

	f.poller.websubRound(ctx)
	if len(hub.forms) != 1 {
		t.Fatalf("subscription requests = %d", len(hub.forms))
	}
	form := hub.forms[0]
	w, err := f.store.WebSubByKey(ctx, strings.TrimPrefix(form.Get("hub.callback"), "https://oriel.example.com/websub/"))
	if err != nil {
		t.Fatalf("callback %q: %v", form.Get("hub.callback"), err)
	}
	if form.Get("method") != "POST" || form.Get("url") != "https://hub.example/sub" || form.Get("hub.mode") != "subscribe" ||
		form.Get("hub.topic") != srv.URL || form.Get("hub.secret") != w.Secret || w.Secret == "" ||
		form.Get("hub.lease_seconds") != "864000" || w.State != "pending" {
		t.Fatalf("request %v, stored %+v", form, w)
	}

	// Nothing more goes out while the hub is expected to verify.
	f.poller.websubRound(ctx)
	if len(hub.forms) != 1 {
		t.Fatalf("asked again before the wait: %d", len(hub.forms))
	}

	// Without a verification the request is repeated after the wait.
	f.now = f.now.Add(store.WebSubWait)
	f.poller.websubRound(ctx)
	if len(hub.forms) != 2 || hub.forms[1].Get("hub.callback") != form.Get("hub.callback") {
		t.Fatalf("requests after the wait: %v", hub.forms)
	}

	// Verified: the source is polled only as a safety net, never past the lease.
	expires := f.now.Add(10 * 24 * time.Hour)
	if err := f.store.WebSubVerified(ctx, id, expires, f.now.Add(8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(20 * time.Minute)
	f.poller.fetchDue(ctx)
	if next := f.int("SELECT next_fetch_at FROM sources WHERE id = ?", id); next != f.now.Add(websubSafety).UnixMilli() {
		t.Fatalf("next fetch %v, want in %v", time.UnixMilli(next).Sub(f.now), websubSafety)
	}
	if _, err := f.db.Exec("UPDATE sources SET websub_expires_at = ? WHERE id = ?", f.now.Add(2*time.Hour).UnixMilli(), id); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(websubSafety)
	f.poller.fetchDue(ctx)
	if next := f.int("SELECT next_fetch_at FROM sources WHERE id = ?", id); next != f.now.Add(15*time.Minute).UnixMilli() {
		t.Fatalf("with an expired lease the next fetch is in %v", time.UnixMilli(next).Sub(f.now))
	}

	// Turning WebSub off goes back to the interval at once.
	if _, err := f.db.Exec("UPDATE sources SET websub_expires_at = ? WHERE id = ?", f.now.Add(time.Hour*100).UnixMilli(), id); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetSetting(ctx, WebSubSetting, ""); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Hour)
	f.poller.fetchDue(ctx)
	if next := f.int("SELECT next_fetch_at FROM sources WHERE id = ?", id); next != f.now.Add(15*time.Minute).UnixMilli() {
		t.Fatalf("with WebSub off the next fetch is in %v", time.UnixMilli(next).Sub(f.now))
	}
}

func TestWebSubRefusedByHubBacksOff(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Link", `<https://hub.example/sub>; rel="hub"`)
		w.Write([]byte(rssFeed))
	}))
	defer srv.Close()
	hub := &hubTransport{status: http.StatusBadRequest}
	f.poller.guarded = &http.Client{Transport: hub}
	f.store.SetSetting(ctx, WebSubSetting, "https://oriel.example.com")
	id := f.add(store.NewSource{URL: srv.URL})
	f.poller.fetchDue(ctx)

	f.poller.websubRound(ctx)
	if f.int("SELECT websub_failures FROM sources WHERE id = ?", id) != 1 ||
		f.text("SELECT websub_state FROM sources WHERE id = ?", id) != "" ||
		!strings.Contains(f.text("SELECT websub_error FROM sources WHERE id = ?", id), "400") {
		t.Fatal("the refusal was not recorded")
	}
	if next := f.int("SELECT websub_next_at FROM sources WHERE id = ?", id); next != f.now.Add(store.WebSubWait).UnixMilli() {
		t.Fatalf("retry in %v", time.UnixMilli(next).Sub(f.now))
	}

	// After the backoff the request goes out again.
	hub.status = http.StatusAccepted
	f.now = f.now.Add(time.Hour)
	f.poller.websubRound(ctx)
	if len(hub.forms) != 2 {
		t.Fatalf("requests = %d", len(hub.forms))
	}
}

func TestWebSubHubGuarded(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Link", `<http://127.0.0.1:1/hub>; rel="hub"`)
		w.Write([]byte(rssFeed))
	}))
	defer srv.Close()
	// The real guarded client refuses a hub on a private address, and the failure is recorded.
	f.poller.guarded = New(f.store, "0.1.0").guarded
	f.store.SetSetting(ctx, WebSubSetting, "https://oriel.example.com")
	id := f.add(store.NewSource{URL: srv.URL})
	f.poller.fetchDue(ctx)
	f.poller.websubRound(ctx)
	if f.int("SELECT websub_failures FROM sources WHERE id = ?", id) != 1 {
		t.Fatalf("a hub on a private address was contacted: %s", f.text("SELECT websub_error FROM sources WHERE id = ?", id))
	}
}
