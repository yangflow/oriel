package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yangflow/oriel/internal/store"
)

// seenHeaders records the User-Agent and Referer of each request by path.
type seenHeaders struct {
	mu sync.Mutex
	by map[string][2]string
}

func (s *seenHeaders) note(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.by == nil {
		s.by = map[string][2]string{}
	}
	s.by[r.URL.Path] = [2]string{r.Header.Get("User-Agent"), r.Header.Get("Referer")}
}

func (s *seenHeaders) get(path string) [2]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.by[path]
}

func TestFeedFetchSendsTheSourceHeadersOnlyToItsOwnHost(t *testing.T) {
	f := newFixture(t)
	var seen seenHeaders
	var other *httptest.Server
	own := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.note(r)
		switch r.URL.Path {
		case "/feed":
			http.Redirect(w, r, other.URL+"/hop", http.StatusFound)
		default:
			w.Write([]byte(rssFeed))
		}
	}))
	defer own.Close()
	other = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.note(r)
		http.Redirect(w, r, own.URL+"/back", http.StatusFound)
	}))
	defer other.Close()

	id := f.add(store.NewSource{URL: own.URL + "/feed"})
	if _, err := f.store.UpdateSource(context.Background(), id, store.SourcePatch{
		FetchUserAgent: ptr("MyReader/1.0"), FetchReferer: ptr("https://example.org/start"),
	}, f.now); err != nil {
		t.Fatal(err)
	}
	if n := f.poller.fetchDue(context.Background()); n != 2 {
		t.Fatalf("created %d entries", n)
	}
	if got := seen.get("/feed"); got != [2]string{"MyReader/1.0", "https://example.org/start"} {
		t.Errorf("the first request: %q", got)
	}
	if got := seen.get("/hop"); got[0] != "Oriel/0.1.0" || got[1] != "" {
		t.Errorf("another host got %q", got)
	}
	if got := seen.get("/back"); got != [2]string{"MyReader/1.0", "https://example.org/start"} {
		t.Errorf("back on the own host: %q", got)
	}
}

func TestFeedFetchWithoutSourceHeadersKeepsTheDefaults(t *testing.T) {
	f := newFixture(t)
	var seen seenHeaders
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.note(r)
		w.Write([]byte(rssFeed))
	}))
	defer srv.Close()
	f.add(store.NewSource{URL: srv.URL + "/feed"})
	f.poller.fetchDue(context.Background())
	if got := seen.get("/feed"); got != [2]string{"Oriel/0.1.0", ""} {
		t.Errorf("headers %q", got)
	}
}

func TestExtractionSendsTheUserAgentOnlyToTheSourceHost(t *testing.T) {
	x := newRulesFixture(t, false)
	var elsewhere http.Header
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere = r.Header.Clone()
		w.Write([]byte(selectorPage))
	}))
	defer other.Close()

	// The entry is on the source's own host: the User-Agent goes, and the extraction Referer stays its own.
	status, _ := x.extract(store.SourcePatch{FetchUserAgent: ptr("MyReader/1.0"), FetchReferer: ptr("https://feed.example.org/"),
		ExtractReferer: ptr("https://news.example.org/front")}, x.srv.URL+"/article")
	if status != "ok" {
		t.Fatalf("status %s", status)
	}
	if len(x.seen) != 1 || x.seen[0].Get("User-Agent") != "MyReader/1.0" || x.seen[0].Get("Referer") != "https://news.example.org/front" {
		t.Errorf("own host: %v", x.seen)
	}

	// The entry is on another host than the feed: the default one.
	y := newRulesFixture(t, false)
	if status, _ := y.extract(store.SourcePatch{FetchUserAgent: ptr("MyReader/1.0")}, other.URL+"/article"); status != "ok" {
		t.Fatalf("status %s", status)
	}
	if elsewhere.Get("User-Agent") != "Oriel/0.1.0" {
		t.Errorf("another host: %q", elsewhere.Get("User-Agent"))
	}
}

func TestExtractionRedirectAwayDropsTheUserAgent(t *testing.T) {
	x := newRulesFixture(t, false)
	var elsewhere http.Header
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere = r.Header.Clone()
		w.Write([]byte(selectorPage))
	}))
	defer other.Close()
	x.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/article", http.StatusFound)
	})
	if status, _ := x.extract(store.SourcePatch{FetchUserAgent: ptr("MyReader/1.0")}, x.srv.URL+"/article"); status != "ok" {
		t.Fatalf("status %s", status)
	}
	if elsewhere.Get("User-Agent") != "Oriel/0.1.0" {
		t.Errorf("a redirect target got %q", elsewhere.Get("User-Agent"))
	}
}

func TestParseUserAgentAndFetchReferer(t *testing.T) {
	if got, err := ParseUserAgent("  Mozilla/5.0 (X11; Linux) Gecko/20100101  "); err != nil || got != "Mozilla/5.0 (X11; Linux) Gecko/20100101" {
		t.Errorf("ParseUserAgent = %q, %v", got, err)
	}
	if got, err := ParseUserAgent(" "); err != nil || got != "" {
		t.Errorf("empty user agent = %q, %v", got, err)
	}
	if _, err := ParseUserAgent(strings.Repeat("a", maxUserAgentLen)); err != nil {
		t.Errorf("the limit itself: %v", err)
	}
	for _, bad := range []string{strings.Repeat("a", maxUserAgentLen+1), "a\nb", "a\x00b", "agent é", "Cookie: a=b", " cookie:a=b", "Authorization: Bearer x"} {
		if _, err := ParseUserAgent(bad); !errors.Is(err, ErrInvalidUserAgent) {
			t.Errorf("user agent %q: %v", bad, err)
		}
	}
	if got, err := ParseFetchReferer(" https://example.org/front "); err != nil || got != "https://example.org/front" {
		t.Errorf("ParseFetchReferer = %q, %v", got, err)
	}
	if got, err := ParseFetchReferer(""); err != nil || got != "" {
		t.Errorf("empty referer = %q, %v", got, err)
	}
	for _, bad := range []string{"example.org", "ftp://example.org/", "https:///x", "https://user:pw@example.org/", "https://user@example.org/", "https://a.org/" + strings.Repeat("x", maxRefererLen)} {
		if _, err := ParseFetchReferer(bad); !errors.Is(err, ErrInvalidFetchReferer) {
			t.Errorf("referer %q: %v", bad, err)
		}
	}
}
