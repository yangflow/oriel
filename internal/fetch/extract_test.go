package fetch

import (
	"context"
	"errors"
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

func TestBlocked(t *testing.T) {
	for address, want := range map[string]bool{
		"127.0.0.1": true, "::1": true, "::ffff:127.0.0.1": true, "10.1.2.3": true, "172.16.0.1": true,
		"192.168.1.1": true, "169.254.169.254": true, "100.64.0.1": true, "0.0.0.0": true, "fe80::1": true,
		"fc00::1": true, "224.0.0.1": true, "255.255.255.255": true, "ff02::1": true,
		"8.8.8.8": false, "2606:4700::1111": false, "198.18.0.1": false, "172.32.0.1": false,
	} {
		if got := blocked(netip.MustParseAddr(address)); got != want {
			t.Errorf("%s: blocked = %v", address, got)
		}
	}
}

func TestGuardedClientChecksEveryConnection(t *testing.T) {
	inside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("secret")) }))
	defer inside.Close()
	outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, inside.URL, http.StatusFound)
	}))
	defer outside.Close()

	if _, err := guardedClient(func(a netip.AddrPort) bool { return blocked(a.Addr()) }).Get(inside.URL); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("loopback: %v", err)
	}
	// Treat only the outside server as public: its redirect must not reach the inside one.
	public := netip.MustParseAddrPort(strings.TrimPrefix(outside.URL, "http://"))
	client := guardedClient(func(a netip.AddrPort) bool { return a != public })
	if _, err := client.Get(outside.URL); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("redirect inside: %v", err)
	}
}

const articlePage = `<!doctype html><html><head><meta charset="utf-8"><title>Post</title></head><body>
<nav><a href="/">Home</a> <a href="/about">About</a></nav>
<article><h1>Post</h1>
<p>Native apps draw their own text. This paragraph is long enough for Readability to keep it as the main content of the page.</p>
<p>A second paragraph with a <a href="/related">relative link</a> and more words so the article clearly outweighs the navigation.</p>
</article><footer>Copyright</footer></body></html>`

const discoursePage = `<html><head><meta name="generator" content="Discourse 3.4"></head><body>
<div id="post_1" class="topic-body crawler-post"><div class="post" itemprop="text"><p>Opening post</p></div></div>
<div id="post_2" class="topic-body crawler-post"><div class="post" itemprop="text">
<aside class="quote"><blockquote>Opening post</blockquote></aside><p>Second post</p></div></div>
</body></html>`

func TestExtract(t *testing.T) {
	f := newFixture(t)
	var gbkHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/article":
			w.Write([]byte(articlePage))
		case "/moved":
			http.Redirect(w, r, "/article", http.StatusMovedPermanently)
		case "/t/topic/42", "/t/topic/42/2":
			w.Write([]byte(discoursePage))
		case "/gbk":
			gbkHits.Add(1)
			w.Header().Set("Content-Type", "text/html; charset=gbk")
			// "中文段落" in GBK, repeated so Readability keeps it.
			w.Write([]byte("<html><body><article><p>" + strings.Repeat("\xd6\xd0\xce\xc4\xb6\xce\xc2\xe4", 40) + "</p></article></body></html>"))
		case "/empty":
			w.Write([]byte("<html><body></body></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f.poller.guarded = guardedClient(func(netip.AddrPort) bool { return false })

	ctx := context.Background()
	id := f.add(store.NewSource{URL: srv.URL + "/feed"})
	src := store.FeedSource{ID: id, Kind: "article"}
	var entries []store.FeedEntry
	for _, path := range []string{"/article", "/moved", "/t/topic/42", "/t/topic/42/2", "/gbk", "/empty", "/missing"} {
		entries = append(entries, store.FeedEntry{GUID: path, URL: srv.URL + path})
	}
	if _, err := f.store.SaveFeed(ctx, src, "", "", entries, "", time.Time{}, f.now); err != nil {
		t.Fatal(err)
	}
	for range 8 {
		f.now = f.now.Add(hostSpacing) // one request to a site per spacing
		f.poller.extractAll(ctx)
	}

	html := func(guid string) string { return f.text("SELECT extracted_html FROM entries WHERE guid = ?", guid) }
	status := func(guid string) string { return f.text("SELECT extract_status FROM entries WHERE guid = ?", guid) }
	for _, guid := range []string{"/article", "/moved"} {
		if status(guid) != "ok" || !strings.Contains(html(guid), "Native apps draw their own text") ||
			strings.Contains(html(guid), "Copyright") || !strings.Contains(html(guid), `href="`+srv.URL+`/related"`) {
			t.Errorf("%s: %s %q", guid, status(guid), html(guid))
		}
	}
	if !strings.Contains(f.text("SELECT search_text FROM entries WHERE guid = '/article'"), "relative link and more words") {
		t.Error("search text missing")
	}
	if html("/t/topic/42") != "<p>Opening post</p>" || strings.TrimSpace(html("/t/topic/42/2")) != "<p>Second post</p>" {
		t.Errorf("discourse: %q / %q", html("/t/topic/42"), html("/t/topic/42/2"))
	}
	if !strings.Contains(html("/gbk"), "中文段落") {
		t.Errorf("gbk: %q", html("/gbk"))
	}
	for _, guid := range []string{"/empty", "/missing"} {
		if status(guid) != "failed" {
			t.Errorf("%s: %s", guid, status(guid))
		}
	}

	// A page that is gone or has no article fails for good: it is not tried again.
	for range 5 {
		f.now = f.now.Add(24 * time.Hour)
		f.poller.extractAll(ctx)
	}
	for _, guid := range []string{"/missing", "/empty"} {
		if n := f.int("SELECT extract_attempts FROM entries WHERE guid = ?", guid); n != 1 {
			t.Errorf("%s attempts: %d", guid, n)
		}
	}
	if gbkHits.Load() != 1 {
		t.Errorf("a finished extraction ran again: %d", gbkHits.Load())
	}
	if targets, _ := f.store.PendingExtractions(ctx, f.now, 10); len(targets) != 0 {
		t.Errorf("still pending: %+v", targets)
	}
}

func TestExtractSkipsOtherKinds(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := f.add(store.NewSource{URL: "https://example.org/feed"})
	f.store.SaveFeed(ctx, store.FeedSource{ID: id, Kind: "article"}, "", "", []store.FeedEntry{{GUID: "a", URL: "https://example.org/a"}}, "", time.Time{}, f.now)
	f.db.Exec("UPDATE sources SET kind = 'video'")
	if targets, _ := f.store.PendingExtractions(ctx, f.now, 10); len(targets) != 0 {
		t.Fatalf("a video source's entry is waiting for extraction: %+v", targets)
	}
}

func TestDiscoursePostNumber(t *testing.T) {
	for link, want := range map[string]string{
		"https://forum.example.org/t/topic/42":        "Opening post",
		"https://forum.example.org/t/topic/42/2":      "Second post",
		"https://forum.example.org/t/topic/42#post_2": "Second post",
		"https://forum.example.org/t/topic/42/9":      "Opening post",
	} {
		doc := mustHTML(t, discoursePage)
		page, _ := url.Parse(link)
		if got := discoursePost(doc, page); !strings.Contains(got, want) {
			t.Errorf("%s: %q", link, got)
		}
	}
}

func mustHTML(t *testing.T, page string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// A redirect chain ends after a few hops, and a redirect to a scheme other than http and https is refused, so a server
// cannot point the guarded client at a file or another protocol.
func TestGuardedClientRedirectLimits(t *testing.T) {
	var hops atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loop":
			hops.Add(1)
			http.Redirect(w, r, srv.URL+"/loop", http.StatusFound)
		case "/chain":
			n := hops.Add(1)
			if n > maxRedirects {
				w.Write([]byte("done"))
				return
			}
			http.Redirect(w, r, srv.URL+"/chain", http.StatusFound)
		case "/file":
			w.Header().Set("Location", "file:///etc/passwd")
			w.WriteHeader(http.StatusFound)
		case "/ftp":
			w.Header().Set("Location", "ftp://example.org/x")
			w.WriteHeader(http.StatusFound)
		}
	}))
	defer srv.Close()
	client := guardedClient(func(netip.AddrPort) bool { return false })

	if _, err := client.Get(srv.URL + "/loop"); err == nil || !strings.Contains(err.Error(), "redirects") {
		t.Fatalf("endless redirects: %v", err)
	}
	if n := int(hops.Load()); n != maxRedirects+1 {
		t.Errorf("followed %d requests, want %d", n, maxRedirects+1)
	}
	hops.Store(0)
	// maxRedirects redirects are followed; the page after them is read.
	resp, err := client.Get(srv.URL + "/chain")
	if err != nil {
		t.Fatalf("a chain of %d redirects: %v", maxRedirects, err)
	}
	resp.Body.Close()
	for _, path := range []string{"/file", "/ftp"} {
		if _, err := client.Get(srv.URL + path); err == nil || !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("%s: %v", path, err)
		}
	}
}
