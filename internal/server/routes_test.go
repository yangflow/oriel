package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/yangflow/oriel/internal/fetch"
)

// instance is a fake RSSHub with a catalog that has fields Oriel does not know, one feed and one page that is no feed.
func instance(t *testing.T, catalog string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/namespace":
			if catalog == "" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(catalog))
		case "/github/issue/rsshub/rsshub":
			_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>Issues</title>
<item><title>One</title><link>https://example.org/1</link><pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate></item>
<item><title>Two</title><link>https://example.org/2</link></item></channel></rss>`))
		case "/html":
			_, _ = w.Write([]byte("<html></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

const serverCatalog = `{"github":{"name":"GitHub","url":"github.com","future":{"a":1},"routes":{
  "/issue/:user/:repo":{"path":"/issue/:user/:repo","name":"Repository Issues","example":"/github/issue/rsshub/rsshub",
    "parameters":{"user":"GitHub user","repo":"Repository"},"unknown":[1,2],
    "radar":[{"source":["github.com/:user/:repo/issues"]}]},
  "/broken":{"name":["x"]}}}}`

func setInstance(t *testing.T, s *Server, raw string) {
	t.Helper()
	if err := s.store.SetSetting(context.Background(), fetch.RSSHubSetting, raw); err != nil {
		t.Fatal(err)
	}
}

func TestFindRoutes(t *testing.T) {
	s, _ := newServer(t, "")
	srv := instance(t, serverCatalog)
	setInstance(t, s, srv.URL+"/")
	get := func(q string) map[string]any {
		w := do(s, "GET", "/api/routes?q="+url.QueryEscape(q), "", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("routes %q = %d %s", q, w.Code, w.Body.String())
		}
		return decode(t, w)
	}

	body := get("https://github.com/rsshub/rsshub/issues")
	list := body["routes"].([]any)
	if body["available"] != true || body["instance"] != srv.URL || body["total"] != 1.0 || len(list) != 1 {
		t.Fatalf("by host = %v", body)
	}
	route := list[0].(map[string]any)
	params := route["parameters"].([]any)
	values := route["values"].(map[string]any)
	if route["path"] != "/github/issue/:user/:repo" || route["name"] != "Repository Issues" || route["namespace"] != "github" ||
		route["namespaceName"] != "GitHub" || route["example"] != "/github/issue/rsshub/rsshub" || len(params) != 2 ||
		params[0].(map[string]any)["name"] != "user" || params[0].(map[string]any)["description"] != "GitHub user" ||
		values["user"] != "rsshub" || values["repo"] != "rsshub" {
		t.Errorf("route = %v", route)
	}

	body = get("issues")
	if list := body["routes"].([]any); len(list) != 1 || len(list[0].(map[string]any)["values"].(map[string]any)) != 0 {
		t.Errorf("by name = %v", body)
	}
	if body = get("nothing like it"); body["available"] != true || len(body["routes"].([]any)) != 0 {
		t.Errorf("no match = %v", body)
	}
}

func TestFindRoutesWithoutCatalog(t *testing.T) {
	s, _ := newServer(t, "")
	// No instance set, an instance without the catalog, and one whose catalog is not JSON are all quiet.
	old := instance(t, "")
	broken := instance(t, "<html>")
	for name, raw := range map[string]string{"none": "", "old": old.URL, "broken": broken.URL} {
		setInstance(t, s, raw)
		w := do(s, "GET", "/api/routes?q=github", "", nil)
		body := decode(t, w)
		if list, ok := body["routes"].([]any); w.Code != http.StatusOK || body["available"] != false || !ok || len(list) != 0 ||
			body["instance"] != "" {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
}

func TestFindRoutesRefresh(t *testing.T) {
	s, _ := newServer(t, "")
	catalog := serverCatalog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(catalog)) }))
	defer srv.Close()
	setInstance(t, s, srv.URL)
	total := func(target string) float64 { return decode(t, do(s, "GET", target, "", nil))["total"].(float64) }
	if total("/api/routes?q=issues") != 1 {
		t.Fatal("first read")
	}
	catalog = `{"x":{"name":"X","routes":{"/issues":{"name":"Issues"},"/more-issues":{"name":"More"}}}}`
	if total("/api/routes?q=issues") != 1 {
		t.Error("the cached catalog was not used")
	}
	if got := total("/api/routes?q=issues&refresh=1"); got != 2 {
		t.Errorf("after refresh total = %v, want 2", got)
	}
}

func TestFindRoutesQuery(t *testing.T) {
	s, _ := newServer(t, "")
	for _, target := range []string{"/api/routes", "/api/routes?q=", "/api/routes?q=%20%20"} {
		w := do(s, "GET", target, "", nil)
		if w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_query" {
			t.Errorf("%s = %d %s", target, w.Code, w.Body.String())
		}
	}
}

func TestPreviewRoute(t *testing.T) {
	s, _ := newServer(t, "")
	srv := instance(t, serverCatalog)
	setInstance(t, s, srv.URL)
	preview := func(path string) *httptest.ResponseRecorder {
		return do(s, "GET", "/api/routes/preview?path="+url.QueryEscape(path), "", nil)
	}

	w := preview("/github/issue/rsshub/rsshub")
	body := decode(t, w)
	entries, _ := body["entries"].([]any)
	if w.Code != http.StatusOK || body["title"] != "Issues" || body["instance"] != srv.URL || len(entries) != 2 {
		t.Fatalf("preview = %d %s", w.Code, w.Body.String())
	}
	first, second := entries[0].(map[string]any), entries[1].(map[string]any)
	if first["title"] != "One" || first["url"] != "https://example.org/1" || first["publishedAt"] != 1136214245000.0 ||
		second["publishedAt"] != nil {
		t.Errorf("entries = %v", entries)
	}
	// Nothing is subscribed.
	if list, _ := s.store.Sources(context.Background()); len(list) != 0 {
		t.Errorf("sources = %v", list)
	}

	for path, want := range map[string]struct {
		status int
		code   string
	}{
		"github/x":     {http.StatusBadRequest, "invalid_rsshub_path"},
		"//evil.org/x": {http.StatusBadRequest, "invalid_rsshub_path"},
		"":             {http.StatusBadRequest, "invalid_rsshub_path"},
		"/missing":     {http.StatusBadGateway, "route_not_found"},
		"/html":        {http.StatusBadGateway, "not_a_feed"},
	} {
		w := preview(path)
		if w.Code != want.status || decode(t, w)["error"] != want.code {
			t.Errorf("%q = %d %s, want %d %s", path, w.Code, w.Body.String(), want.status, want.code)
		}
	}

	setInstance(t, s, "http://127.0.0.1:1")
	if w := preview("/x"); w.Code != http.StatusBadGateway || decode(t, w)["error"] != "route_unavailable" {
		t.Errorf("unreachable = %d %s", w.Code, w.Body.String())
	}
	setInstance(t, s, "")
	if w := preview("/x"); w.Code != http.StatusUnprocessableEntity || decode(t, w)["error"] != "no_rsshub_instance" {
		t.Errorf("no instance = %d %s", w.Code, w.Body.String())
	}
}

func TestRoutesNeedSession(t *testing.T) {
	s, _ := newServer(t, "secret")
	for _, target := range []string{"/api/routes?q=github", "/api/routes/preview?path=/x"} {
		if w := do(s, "GET", target, "", nil); w.Code != http.StatusUnauthorized {
			t.Errorf("%s = %d, want 401", target, w.Code)
		}
	}
}
