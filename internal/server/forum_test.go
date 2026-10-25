package server

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestAPIAcceptsAndReturnsTheForumKind(t *testing.T) {
	s, c := newServer(t, "")
	w := do(s, "POST", "/api/sources", `{"url":"https://linux.do/latest.rss","kind":"forum"}`, nil)
	created := decode(t, w)
	if w.Code != http.StatusCreated || created["kind"] != "forum" {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	seed(t, s, "forum", 2, c.t)
	seed(t, s, "article", 1, c.t)

	got := titles(t, decode(t, do(s, "GET", "/api/entries?kind=forum", "", nil)))
	slices.Sort(got)
	if strings.Join(got, ",") != "forum 0,forum 1" {
		t.Fatalf("forum entries: %q", got)
	}
	if w := do(s, "GET", "/api/entries?kind=forum", "", nil); !strings.Contains(w.Body.String(), `"kind":"forum"`) {
		t.Fatalf("entry kind: %s", w.Body)
	}

	// An article source can be moved to the community kind, and kinds that do not exist are still refused.
	id := "1"
	if w := do(s, "PATCH", "/api/sources/"+id, `{"kind":"forum"}`, nil); w.Code != http.StatusOK || decode(t, w)["kind"] != "forum" {
		t.Fatalf("patch: %d %s", w.Code, w.Body)
	}
	if w := do(s, "PATCH", "/api/sources/"+id, `{"kind":"community"}`, nil); w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_kind" {
		t.Fatalf("unknown kind: %d %s", w.Code, w.Body)
	}
}

func TestOPMLKeepsTheForumKind(t *testing.T) {
	file := `<opml version="2.0"><body>
    <outline text="LINUX DO" xmlUrl="https://linux.do/latest.rss" orielKind="forum"/>
  </body></opml>`
	s, _ := newServer(t, "")
	if body := decode(t, do(s, "POST", "/api/opml/import", file, nil)); body["added"] != 1.0 || len(body["failed"].([]any)) != 0 {
		t.Fatalf("import: %v", body)
	}
	if got := subscriptions(t, s, nil); len(got) != 1 || got[0] != "forum https://linux.do/latest.rss LINUX DO" {
		t.Fatalf("sources: %q", got)
	}
	w := do(s, "GET", "/api/opml/export", "", nil)
	if !strings.Contains(w.Body.String(), `orielKind="forum"`) {
		t.Fatalf("export: %s", w.Body)
	}
	again, _ := newServer(t, "")
	do(again, "POST", "/api/opml/import", w.Body.String(), nil)
	if got := subscriptions(t, again, nil); len(got) != 1 || got[0] != "forum https://linux.do/latest.rss LINUX DO" {
		t.Fatalf("round trip: %q", got)
	}
}
