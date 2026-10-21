package server

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/yangflow/oriel/internal/fetch"
)

const importFile = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Elsewhere</title></head>
  <body>
    <outline text="Blog" title="Blog" type="rss" xmlUrl=" https://example.org/feed "/>
    <outline text="Tech">
      <outline text="Deep">
        <outline text="Talks" xmlUrl="https://example.org/talks" orielKind="video"/>
      </outline>
      <outline text="Again" xmlUrl="https://Example.org:443/feed/"/>
      <outline text="Old" xmlUrl="https://example.org/old"/>
      <outline text="Gopher" xmlUrl="ftp://example.org/feed"/>
      <outline text="Odd" xmlUrl="https://example.org/odd" orielKind="podcast"/>
    </outline>
    <outline text="/github/issue/yangflow/oriel" orielRsshubPath="/github/issue/yangflow/oriel" orielKind="social"
      xmlUrl="https://rsshub.example.org/github/issue/yangflow/oriel"/>
  </body>
</opml>`

// subscriptions lists every source as "kind address title", sorted.
func subscriptions(t *testing.T, s *Server, header map[string]string) []string {
	t.Helper()
	var got []string
	for _, item := range decode(t, do(s, "GET", "/api/sources", "", header))["sources"].([]any) {
		src := item.(map[string]any)
		got = append(got, fmt.Sprintf("%s %s%s %s", src["kind"], src["url"], src["rsshubPath"], src["title"]))
	}
	slices.Sort(got)
	return got
}

func TestOPMLImport(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	for _, r := range []struct{ method, target string }{{"POST", "/api/opml/import"}, {"GET", "/api/opml/export"}} {
		if w := do(s, r.method, r.target, importFile, nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s without session: %d", r.target, w.Code)
		}
	}
	do(s, "POST", "/api/sources", `{"url":"https://example.org/old","kind":"article"}`, bearer(token))

	w := do(s, "POST", "/api/opml/import", importFile, bearer(token))
	body := decode(t, w)
	if w.Code != http.StatusOK || body["added"] != 3.0 || body["existing"] != 2.0 {
		t.Fatalf("import: %d %v", w.Code, body)
	}
	if got := fmt.Sprint(body["failed"]); got != "[map[address:ftp://example.org/feed error:invalid_url title:Gopher] "+
		"map[address:https://example.org/odd error:invalid_kind title:Odd]]" {
		t.Fatalf("failed: %s", got)
	}
	if got := subscriptions(t, s, bearer(token)); fmt.Sprint(got) != "[article https://example.org/feed Blog article https://example.org/old  "+
		"social /github/issue/yangflow/oriel  video https://example.org/talks Talks]" {
		t.Fatalf("sources: %q", got)
	}

	for body, want := range map[string]struct {
		status int
		code   string
	}{
		`<rss version="2.0"><channel></channel></rss>`: {http.StatusBadRequest, "invalid_opml"},
		`not xml`: {http.StatusBadRequest, "invalid_opml"},
		"<opml><body>" + strings.Repeat(" ", maxOPML) + "</body></opml>": {http.StatusRequestEntityTooLarge, "opml_too_large"},
	} {
		w := do(s, "POST", "/api/opml/import", body, bearer(token))
		if w.Code != want.status || decode(t, w)["error"] != want.code {
			t.Errorf("%.30s: %d %s", body, w.Code, w.Body)
		}
	}
}

func TestOPMLRoundTrip(t *testing.T) {
	s, _ := newServer(t, "")
	do(s, "POST", "/api/opml/import", importFile, nil)
	s.store.SetSetting(context.Background(), fetch.RSSHubSetting, "http://localhost:1200/")
	w := do(s, "GET", "/api/opml/export", "", nil)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/x-opml") ||
		!strings.Contains(w.Body.String(), `xmlUrl="http://localhost:1200/github/issue/yangflow/oriel"`) {
		t.Fatalf("export: %d %v %s", w.Code, w.Header(), w.Body)
	}

	again, _ := newServer(t, "")
	if body := decode(t, do(again, "POST", "/api/opml/import", w.Body.String(), nil)); body["added"] != 4.0 || len(body["failed"].([]any)) != 0 {
		t.Fatalf("reimport: %v", body)
	}
	if got, want := subscriptions(t, again, nil), subscriptions(t, s, nil); !slices.Equal(got, want) {
		t.Fatalf("round trip:\n got %q\nwant %q", got, want)
	}
}
