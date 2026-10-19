package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
	"golang.org/x/net/html"
)

func TestSiteIcon(t *testing.T) {
	page, _ := url.Parse("https://blog.example.org/en/")
	for _, c := range []struct{ head, want string }{
		{`<link rel="icon" href="/favicon.ico"><link rel="icon" type="image/svg+xml" href="/icon">`, ""},
		{`<link rel="shortcut icon" href="/favicon.ico"><link rel="icon" href="logo.webp">`,
			"https://blog.example.org/en/logo.webp"},
		{`<link rel="icon" href="/a.webp"><link rel="icon" type="image/png" href="/b">`, "https://blog.example.org/b"},
		{`<link rel="icon" href="/a.png"><link rel="Apple-Touch-Icon" href="https://cdn.example.org/t.png">`,
			"https://cdn.example.org/t.png"},
		{`<link rel="apple-touch-icon" href="/t.svg"><link rel="stylesheet" href="/s.png">`, ""},
		{`<link rel="icon" href="javascript:alert(1)">`, ""},
	} {
		doc, err := html.Parse(strings.NewReader("<html><head>" + c.head + "</head></html>"))
		if err != nil {
			t.Fatal(err)
		}
		if got := siteIcon(doc, page); got != c.want {
			t.Errorf("%s: %q, want %q", c.head, got, c.want)
		}
	}
}

func TestParseFeedIcon(t *testing.T) {
	base, _ := url.Parse("https://example.org/feed")
	for feed, want := range map[string]string{
		`<rss version="2.0"><channel><title>A</title><image><url>/logo.png</url></image></channel></rss>`: "https://example.org/logo.png",
		`<feed xmlns="http://www.w3.org/2005/Atom"><title>A</title><icon>/i.webp</icon></feed>`:           "https://example.org/i.webp",
		`{"version": "https://jsonfeed.org/version/1.1", "title": "A", "icon": "/i.jpg", "items": []}`:    "https://example.org/i.jpg",
		`<rss version="2.0"><channel><title>A</title><image><url>/logo.svg</url></image></channel></rss>`: "",
	} {
		got, err := parse([]byte(feed), base)
		if err != nil {
			t.Fatal(err)
		}
		if got.iconURL != want {
			t.Errorf("%s: %q, want %q", feed, got.iconURL, want)
		}
	}
}

func TestPollFillsIcon(t *testing.T) {
	f := newFixture(t)
	var homepage atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/with-image":
			w.Write([]byte(`<rss version="2.0"><channel><title>A</title><link>` + srv.URL + `/home</link>
				<image><url>/feed.png</url></image></channel></rss>`))
		case "/plain":
			w.Write([]byte(`<rss version="2.0"><channel><title>B</title><link>` + srv.URL + `/home</link></channel></rss>`))
		case "/bare":
			w.Write([]byte(`<rss version="2.0"><channel><title>C</title><link>` + srv.URL + `/bare-home</link></channel></rss>`))
		case "/home":
			homepage.Add(1)
			w.Write([]byte(`<html><head><link rel="apple-touch-icon" href="/touch.png"></head></html>`))
		case "/bare-home":
			homepage.Add(1)
			w.Write([]byte(`<html><head><link rel="icon" href="/favicon.ico"></head></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f.poller.guarded = guardedClient(func(netip.AddrPort) bool { return false })
	withImage := f.add(store.NewSource{URL: srv.URL + "/with-image"})
	plain := f.add(store.NewSource{URL: srv.URL + "/plain"})
	bare := f.add(store.NewSource{URL: srv.URL + "/bare"})

	f.poller.fetchDue(context.Background())
	icon := func(id int64) string { return f.text("SELECT icon_url FROM sources WHERE id = ?", id) }
	if icon(withImage) != srv.URL+"/feed.png" || icon(plain) != srv.URL+"/touch.png" || icon(bare) != "" {
		t.Fatalf("icons %q %q %q", icon(withImage), icon(plain), icon(bare))
	}
	if homepage.Load() != 2 {
		t.Fatalf("%d homepage requests, want 2: a feed image needs none", homepage.Load())
	}

	f.now = f.now.Add(15 * time.Minute)
	f.poller.fetchDue(context.Background())
	if homepage.Load() != 2 {
		t.Fatal("a searched homepage was fetched again for the same site address")
	}
}

func TestPollIconSkipsBlockedAddresses(t *testing.T) {
	f := newFixture(t)
	var homepage atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/home" {
			homepage.Add(1)
			w.Write([]byte(`<html><head><link rel="icon" href="/i.png"></head></html>`))
			return
		}
		w.Write([]byte(`<rss version="2.0"><channel><title>A</title><link>` + srv.URL + `/home</link></channel></rss>`))
	}))
	defer srv.Close()
	f.poller.guarded = guardedClient(func(a netip.AddrPort) bool { return blocked(a.Addr()) })
	id := f.add(store.NewSource{URL: srv.URL + "/feed"})

	if n := f.poller.fetchDue(context.Background()); n != 0 || f.text("SELECT last_error FROM sources WHERE id = ?", id) != "" {
		t.Fatal("a blocked homepage failed the fetch")
	}
	if homepage.Load() != 0 || f.text("SELECT icon_url FROM sources WHERE id = ?", id) != "" {
		t.Fatal("the homepage on a loopback address was fetched")
	}
}
