package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

const selectorPage = `<!doctype html><html><head><meta charset="utf-8"><title>Post</title></head><body>
<div class="menu"><a href="/home">Home</a></div>
<div class="story"><script>track()</script><p>Chosen by the selector.</p><p>See <a href="more">more</a> and <img src="/pic.png"></p></div>
<div class="story"><p>Second match is ignored.</p></div>
<article><h1>Post</h1>
<p>Native apps draw their own text. This paragraph is long enough for Readability to keep it as the main content of the page.</p>
<p>A second paragraph with more words so the article clearly outweighs the navigation around it.</p>
</article></body></html>`

// rulesFixture serves selectorPage at /article and records the headers of every request.
type rulesFixture struct {
	*fixture
	srv  *httptest.Server
	mu   sync.Mutex
	seen []http.Header
	hits int
}

func newRulesFixture(t *testing.T, blockedPredicate bool) *rulesFixture {
	t.Helper()
	x := &rulesFixture{fixture: newFixture(t)}
	x.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		x.mu.Lock()
		x.hits++
		x.seen = append(x.seen, r.Header.Clone())
		x.mu.Unlock()
		if r.URL.Path != "/article" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(selectorPage))
	}))
	t.Cleanup(x.srv.Close)
	if blockedPredicate {
		x.poller.guarded = guardedClient(func(a netip.AddrPort) bool { return blocked(a.Addr()) })
	} else {
		x.poller.guarded = guardedClient(func(netip.AddrPort) bool { return false })
	}
	return x
}

// extract subscribes with the rules, saves one entry with the link and runs one round of extraction.
func (x *rulesFixture) extract(patch store.SourcePatch, link string) (status, content string) {
	x.t.Helper()
	ctx := context.Background()
	id := x.add(store.NewSource{URL: x.srv.URL + "/feed"})
	if _, err := x.store.UpdateSource(ctx, id, patch, x.now); err != nil {
		x.t.Fatal(err)
	}
	entry := store.FeedEntry{GUID: "e", URL: link}
	if _, err := x.store.SaveFeed(ctx, store.FeedSource{ID: id, Kind: "article"}, "", "", []store.FeedEntry{entry}, "", time.Time{}, x.now); err != nil {
		x.t.Fatal(err)
	}
	x.now = x.now.Add(hostSpacing)
	x.poller.extractAll(ctx)
	return x.text("SELECT extract_status FROM entries WHERE guid = 'e'"), x.text("SELECT extracted_html FROM entries WHERE guid = 'e'")
}

func ptr[T any](v T) *T { return &v }

func TestExtractWithSelector(t *testing.T) {
	x := newRulesFixture(t, false)
	status, content := x.extract(store.SourcePatch{ExtractSelector: ptr(".story")}, x.srv.URL+"/article")
	if status != "ok" || !strings.Contains(content, "Chosen by the selector.") {
		t.Fatalf("%s %q", status, content)
	}
	for _, unwanted := range []string{"Second match", "Native apps", "track()", "Home"} {
		if strings.Contains(content, unwanted) {
			t.Errorf("content has %q: %q", unwanted, content)
		}
	}
	if !strings.Contains(content, `href="`+x.srv.URL+`/more"`) || !strings.Contains(content, `src="`+x.srv.URL+`/pic.png"`) {
		t.Errorf("addresses not resolved: %q", content)
	}
}

func TestExtractSelectorMissFallsBackToAutomatic(t *testing.T) {
	x := newRulesFixture(t, false)
	status, content := x.extract(store.SourcePatch{ExtractSelector: ptr(".nothing-here")}, x.srv.URL+"/article")
	if status != "ok" || !strings.Contains(content, "Native apps draw their own text") || strings.Contains(content, "Chosen by the selector") {
		t.Fatalf("%s %q", status, content)
	}
}

func TestExtractSelectorOnEmptyElementFallsBack(t *testing.T) {
	x := newRulesFixture(t, false)
	status, content := x.extract(store.SourcePatch{ExtractSelector: ptr(".menu img, h1:empty, script")}, x.srv.URL+"/article")
	if status != "ok" || !strings.Contains(content, "Native apps draw their own text") {
		t.Fatalf("%s %q", status, content)
	}
}

func TestExtractSendsRefererOnlyWhenSet(t *testing.T) {
	x := newRulesFixture(t, false)
	if status, _ := x.extract(store.SourcePatch{ExtractReferer: ptr("https://news.example.org/front")}, x.srv.URL+"/article"); status != "ok" {
		t.Fatalf("status %s", status)
	}
	if len(x.seen) != 1 || x.seen[0].Get("Referer") != "https://news.example.org/front" {
		t.Fatalf("headers: %v", x.seen)
	}
	for name := range x.seen[0] {
		switch name {
		case "Referer", "User-Agent", "Accept-Encoding":
		default:
			t.Errorf("an extra header was sent: %s", name)
		}
	}

	y := newRulesFixture(t, false)
	y.extract(store.SourcePatch{}, y.srv.URL+"/article")
	if len(y.seen) != 1 || y.seen[0].Get("Referer") != "" {
		t.Errorf("a Referer without a rule: %v", y.seen)
	}
}

func TestExtractRewritesTheLinkBeforeTheRequest(t *testing.T) {
	x := newRulesFixture(t, false)
	host := strings.TrimPrefix(x.srv.URL, "http://")
	rules := "gone.example.test => " + host + "\nhttp://" + host + "/amp/ => http://" + host + "/"
	status, content := x.extract(store.SourcePatch{ExtractRewrites: ptr(rules)}, "http://GONE.example.test/amp/article")
	if status != "ok" || !strings.Contains(content, "Native apps draw their own text") {
		t.Fatalf("%s %q", status, content)
	}
	// The entry keeps its own link.
	if got := x.text("SELECT url FROM entries WHERE guid = 'e'"); got != "http://GONE.example.test/amp/article" {
		t.Errorf("entry link = %q", got)
	}
}

func TestExtractRewriteToABlockedAddressFails(t *testing.T) {
	x := newRulesFixture(t, true)
	status, content := x.extract(store.SourcePatch{ExtractRewrites: ptr("https://news.example.org/ => " + x.srv.URL + "/")}, "https://news.example.org/article")
	if status != "failed" || content != "" {
		t.Fatalf("%s %q", status, content)
	}
	if x.hits != 0 {
		t.Errorf("the blocked address was reached %d times", x.hits)
	}
	if n := x.int("SELECT extract_attempts FROM entries WHERE guid = 'e'"); n != 1 {
		t.Errorf("attempts = %d", n)
	}
	if next := x.int("SELECT coalesce(next_extract_at, 0) FROM entries WHERE guid = 'e'"); next != 0 {
		t.Errorf("a blocked address is tried again at %d", next)
	}
}

func TestExtractRewriteWithoutAWebAddressFailsForGood(t *testing.T) {
	x := newRulesFixture(t, false)
	status, _ := x.extract(store.SourcePatch{ExtractRewrites: ptr("https://news.example.org/ => ftp://files.example.org/")}, "https://news.example.org/article")
	if status != "failed" || x.hits != 0 {
		t.Fatalf("%s, %d requests", status, x.hits)
	}
}

func TestParseRewrites(t *testing.T) {
	got, err := ParseRewrites("  m.example.com=>www.example.com \n\n https://a.org/amp/ =>  https://a.org/ \r\n")
	if err != nil || got != "m.example.com => www.example.com\nhttps://a.org/amp/ => https://a.org/" {
		t.Errorf("ParseRewrites = %q, %v", got, err)
	}
	if got, err := ParseRewrites(" \n"); err != nil || got != "" {
		t.Errorf("empty: %q, %v", got, err)
	}
	for name, text := range map[string]string{
		"no arrow":        "m.example.com",
		"empty side":      "m.example.com =>",
		"mixed":           "m.example.com => https://www.example.com/",
		"a path":          "example.com/a => example.com/b",
		"two arrows":      "a.org => b.org => c.org",
		"not a web start": "ftp://a.org/ => ftp://b.org/",
		"too many":        strings.Repeat("a.org => b.org\n", maxRewrites+1),
		"too long":        "a.org => " + strings.Repeat("b", maxRewriteLen),
	} {
		if _, err := ParseRewrites(text); !errors.Is(err, ErrInvalidRewrites) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := ParseRewrites(strings.Repeat("a.org => b.org\n", maxRewrites)); err != nil {
		t.Errorf("the limit itself: %v", err)
	}
}

func TestRewriteLink(t *testing.T) {
	rules, err := parseRewrites("m.example.com => www.example.com\nhttps://www.example.com/amp/ => https://www.example.com/\nexample.net => example.org")
	if err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{
		"https://m.example.com/amp/post?a=1#x": "https://www.example.com/post?a=1#x",
		"https://M.Example.com/news":           "https://www.example.com/news",
		"https://www.example.com/amp/post":     "https://www.example.com/post",
		"https://other.org/amp/post":           "https://other.org/amp/post",
		"https://example.net:8080/a":           "https://example.net:8080/a",
	} {
		if got, err := rewriteLink(in, rules); err != nil || got != want {
			t.Errorf("rewriteLink(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad, _ := parseRewrites("https://a.org/ => http:///x")
	if _, err := rewriteLink("https://a.org/p", bad); err == nil {
		t.Error("a rewrite without a host was accepted")
	}
}

func TestParseSelectorAndReferer(t *testing.T) {
	if got, err := ParseSelector("  article > .post  "); err != nil || got != "article > .post" {
		t.Errorf("ParseSelector = %q, %v", got, err)
	}
	if got, err := ParseSelector(" "); err != nil || got != "" {
		t.Errorf("empty selector = %q, %v", got, err)
	}
	for _, bad := range []string{"div[", ">>", strings.Repeat("a", maxSelectorLen+1)} {
		if _, err := ParseSelector(bad); !errors.Is(err, ErrInvalidSelector) {
			t.Errorf("selector %q: %v", bad, err)
		}
	}
	if got, err := ParseReferer(" https://example.org/front "); err != nil || got != "https://example.org/front" {
		t.Errorf("ParseReferer = %q, %v", got, err)
	}
	if got, err := ParseReferer(""); err != nil || got != "" {
		t.Errorf("empty referer = %q, %v", got, err)
	}
	for _, bad := range []string{"example.org", "ftp://example.org/", "https:///x", "/relative", "https://a.org/" + strings.Repeat("x", maxRefererLen)} {
		if _, err := ParseReferer(bad); !errors.Is(err, ErrInvalidReferer) {
			t.Errorf("referer %q: %v", bad, err)
		}
	}
}
