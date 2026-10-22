package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// recordedCatalog is a trimmed /api/namespace answer with fields Oriel does not know and entries it cannot read.
const recordedCatalog = `{
  "github": {
    "name": "GitHub",
    "url": "github.com",
    "description": "unknown to us",
    "routes": {
      "/issue/:user/:repo/:state?": {
        "path": "/issue/:user/:repo/:state?",
        "name": "Repository Issues",
        "example": "/github/issue/DIYgod/RSSHub/open",
        "parameters": {"user": "GitHub username", "repo": {"description": "Repository name", "default": "x"}},
        "maintainers": ["DIYgod"],
        "features": {"requireConfig": false},
        "radar": [{"source": ["github.com/:user/:repo/issues", "github.com/:user/:repo/issues/:id"], "target": "/issue/:user/:repo"}]
      },
      "/trending/:since/:language?": {
        "path": ["/trending/:since/:language?"],
        "name": "Trending",
        "example": "/github/trending/daily",
        "parameters": {"since": "daily, weekly or monthly"},
        "radar": [{"source": ["github.com/trending"]}]
      },
      "/broken": {"path": 17, "name": ["not a string"]},
      "/user/followers/:user": {"name": "User Followers", "example": "/github/user/followers/DIYgod"}
    }
  },
  "twitter": {
    "name": "Twitter",
    "url": "x.com",
    "routes": {
      "/user/:id": {"path": "/twitter/user/:id", "name": "User timeline", "parameters": {"id": "username"},
        "radar": [{"source": ["*.twitter.com/:id", "x.com/:id"]}]}
    }
  },
  "gitlab": {"name": "GitLab", "url": "gitlab.com", "routes": {
    "/blog": {"name": "Blog"}
  }},
  "oops": "not a namespace"
}`

func catalogServer(t *testing.T, body string, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/namespace" {
			http.NotFound(w, r)
			return
		}
		if hits != nil {
			hits.Add(1)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRoutesParseTolerantly(t *testing.T) {
	cat, err := parseCatalog([]byte(recordedCatalog))
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.namespaces) != 3 {
		t.Fatalf("namespaces = %d", len(cat.namespaces))
	}
	issue := cat.namespaces[0].routes[0]
	if issue.Path != "/github/issue/:user/:repo/:state?" || issue.Name != "Repository Issues" ||
		issue.Example != "/github/issue/DIYgod/RSSHub/open" {
		t.Errorf("issue route = %+v", issue.Route)
	}
	want := []RouteParam{
		{"user", "GitHub username", false}, {"repo", "Repository name", false}, {"state", "", true},
	}
	if len(issue.Parameters) != 3 {
		t.Fatalf("parameters = %+v", issue.Parameters)
	}
	for i, p := range want {
		if issue.Parameters[i] != p {
			t.Errorf("parameter %d = %+v, want %+v", i, issue.Parameters[i], p)
		}
	}
	if got := len(cat.namespaces[0].routes); got != 3 {
		// The broken route is skipped; the others stay.
		t.Errorf("github routes = %d", got)
	}
	if p := cat.namespaces[1].routes[0].Path; p != "/twitter/user/:id" {
		t.Errorf("twitter path = %q", p)
	}
}

func TestRoutesBrokenCatalogs(t *testing.T) {
	for name, body := range map[string]string{
		"not json": "<html>nope</html>", "array": "[]", "empty": "{}", "no routes": `{"a":{"name":"A","routes":{}}}`,
	} {
		if _, err := parseCatalog([]byte(body)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func paths(rs []Route) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Path)
	}
	return out
}

func TestRoutesMatchByHost(t *testing.T) {
	srv := catalogServer(t, recordedCatalog, nil)
	r := NewRoutes("test")
	got := r.Find(context.Background(), []string{srv.URL}, "https://www.github.com/rsshub/rsshub/issues", false)
	if !got.Available || got.Instance != srv.URL {
		t.Fatalf("result = %+v", got)
	}
	if len(got.Routes) == 0 || got.Routes[0].Path != "/github/issue/:user/:repo/:state?" {
		t.Fatalf("routes = %v", paths(got.Routes))
	}
	first := got.Routes[0]
	if first.Values["user"] != "rsshub" || first.Values["repo"] != "rsshub" || len(first.Values) != 2 {
		t.Errorf("values = %v", first.Values)
	}
	// Host-only matches follow, and the namespace's own host matches routes without radar last.
	want := []string{"/github/issue/:user/:repo/:state?", "/github/trending/:since/:language?", "/github/user/followers/:user"}
	if g := paths(got.Routes); len(g) != 3 || g[1] != want[1] || g[2] != want[2] {
		t.Errorf("order = %v", g)
	}

	// A wildcard subdomain and a scheme-less host.
	got = r.Find(context.Background(), []string{srv.URL}, "mobile.twitter.com/jack", false)
	if len(got.Routes) != 1 || got.Routes[0].Path != "/twitter/user/:id" || got.Routes[0].Values["id"] != "jack" {
		t.Errorf("twitter = %+v", got.Routes)
	}
	got = r.Find(context.Background(), []string{srv.URL}, "https://example.org/x", false)
	if !got.Available || got.Total != 0 || got.Routes == nil || len(got.Routes) != 0 {
		t.Errorf("unmatched = %+v", got)
	}
}

func TestRoutesMatchByName(t *testing.T) {
	srv := catalogServer(t, recordedCatalog, nil)
	r := NewRoutes("test")
	got := r.Find(context.Background(), []string{srv.URL}, "GitHub", false)
	if got.Total != 3 || got.Routes[0].Namespace != "github" {
		t.Errorf("github = %+v", got)
	}
	got = r.Find(context.Background(), []string{srv.URL}, "git", false)
	if got.Total != 4 {
		t.Errorf("git total = %d", got.Total)
	}
	got = r.Find(context.Background(), []string{srv.URL}, "TIMELINE", false)
	if len(got.Routes) != 1 || got.Routes[0].Namespace != "twitter" {
		t.Errorf("route name = %+v", got.Routes)
	}
	got = r.Find(context.Background(), []string{srv.URL}, "followers", false)
	if len(got.Routes) != 1 || got.Routes[0].Name != "User Followers" {
		t.Errorf("followers = %+v", got.Routes)
	}
	if got = r.Find(context.Background(), []string{srv.URL}, "  ", false); got.Total != 0 {
		t.Errorf("blank = %+v", got)
	}
}

func TestRoutesLimitsCandidates(t *testing.T) {
	body := `{"big":{"name":"Big","routes":{`
	for i := range 15 {
		if i > 0 {
			body += ","
		}
		body += `"/r` + string(rune('a'+i)) + `":{"name":"Route"}`
	}
	body += `}}}`
	srv := catalogServer(t, body, nil)
	got := NewRoutes("test").Find(context.Background(), []string{srv.URL}, "big", false)
	if got.Total != 15 || len(got.Routes) != maxRoutes {
		t.Errorf("total = %d, routes = %d", got.Total, len(got.Routes))
	}
}

func TestRoutesUnavailable(t *testing.T) {
	missing := httptest.NewServer(http.NotFoundHandler())
	defer missing.Close()
	broken := catalogServer(t, "not json", nil)
	r := NewRoutes("test")
	for name, instances := range map[string][]string{
		"none": nil, "missing": {missing.URL}, "broken": {broken.URL}, "unreachable": {"http://127.0.0.1:1"},
	} {
		got := r.Find(context.Background(), instances, "github", false)
		if got.Available || got.Instance != "" || got.Routes == nil || len(got.Routes) != 0 {
			t.Errorf("%s: %+v", name, got)
		}
	}
	// The first instance that answers is the one used.
	good := catalogServer(t, recordedCatalog, nil)
	got := r.Find(context.Background(), []string{missing.URL, good.URL}, "github", false)
	if !got.Available || got.Instance != good.URL {
		t.Errorf("fallback = %+v", got)
	}
}

func TestRoutesCache(t *testing.T) {
	var hits atomic.Int32
	srv := catalogServer(t, recordedCatalog, &hits)
	r := NewRoutes("test")
	now := time.Now()
	r.now = func() time.Time { return now }
	find := func(refresh bool) { r.Find(context.Background(), []string{srv.URL}, "github", refresh) }
	find(false)
	find(false)
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}
	find(true)
	if hits.Load() != 2 {
		t.Errorf("refresh: hits = %d, want 2", hits.Load())
	}
	now = now.Add(catalogTTL + time.Second)
	find(false)
	if hits.Load() != 3 {
		t.Errorf("expired: hits = %d, want 3", hits.Load())
	}

	// A failed read is remembered for a short while only.
	var down atomic.Int32
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		down.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer bad.Close()
	ask := func() { r.Find(context.Background(), []string{bad.URL}, "github", false) }
	ask()
	ask()
	if down.Load() != 1 {
		t.Errorf("failed reads = %d, want 1", down.Load())
	}
	now = now.Add(catalogFailureTTL + time.Second)
	ask()
	if down.Load() != 2 {
		t.Errorf("failed reads after the wait = %d, want 2", down.Load())
	}
}

const previewFeed = `<?xml version="1.0"?><rss version="2.0"><channel><title>Issues</title><link>https://example.org/</link>
<item><title>One</title><link>https://example.org/1</link><pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate></item>
<item><title>Two</title><link>https://example.org/2</link></item>
<item><title>Three</title><link>https://example.org/3</link></item>
<item><title>Four</title><link>https://example.org/4</link></item>
<item><title>Five</title><link>https://example.org/5</link></item>
<item><title>Six</title><link>https://example.org/6</link></item>
</channel></rss>`

func TestRoutesPreview(t *testing.T) {
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		switch r.URL.Path {
		case "/ok/x":
			_, _ = w.Write([]byte(previewFeed))
		case "/html":
			_, _ = w.Write([]byte("<html><body>hello</body></html>"))
		case "/busy":
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	r := NewRoutes("test")
	ctx := context.Background()

	got, err := r.Preview(ctx, []string{srv.URL}, "/ok/x")
	if err != nil {
		t.Fatal(err)
	}
	if got.Instance != srv.URL || got.Title != "Issues" || len(got.Entries) != 5 || got.Entries[0].Title != "One" {
		t.Errorf("preview = %+v", got)
	}
	if got.Entries[0].PublishedAt == nil || *got.Entries[0].PublishedAt != 1136214245000 || got.Entries[1].PublishedAt != nil {
		t.Errorf("dates = %v, %v", got.Entries[0].PublishedAt, got.Entries[1].PublishedAt)
	}

	if _, err := r.Preview(ctx, nil, "/ok/x"); !errors.Is(err, ErrNoInstance) {
		t.Errorf("no instance: %v", err)
	}
	if _, err := r.Preview(ctx, []string{srv.URL}, "/missing"); !errors.Is(err, ErrRouteNotFound) {
		t.Errorf("404: %v", err)
	}
	if _, err := r.Preview(ctx, []string{srv.URL}, "/html"); !errors.Is(err, ErrNotAFeed) {
		t.Errorf("html: %v", err)
	}
	// An unreachable or busy instance hands over to the next; a 404 does not.
	if got, err := r.Preview(ctx, []string{"http://127.0.0.1:1", srv.URL}, "/ok/x"); err != nil || got.Instance != srv.URL {
		t.Errorf("failover: %+v, %v", got, err)
	}
	if _, err := r.Preview(ctx, []string{srv.URL, srv.URL}, "/busy"); !errors.Is(err, ErrRouteUnavailable) {
		t.Errorf("busy: %v", err)
	}
	asked.Store(0)
	if _, err := r.Preview(ctx, []string{srv.URL, srv.URL}, "/missing"); !errors.Is(err, ErrRouteNotFound) || asked.Load() != 1 {
		t.Errorf("404 stops: %v after %d requests", err, asked.Load())
	}
}
