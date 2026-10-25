package server

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDailyCapOverTheAPI(t *testing.T) {
	s, c := newServer(t, "secret")
	header := bearer(login(t, s, "laptop"))
	id := seed(t, s, "article", 5, c.t)
	target := "/api/sources/" + strconv.FormatInt(id, 10)

	if got := decode(t, do(s, "GET", target, "", header))["dailyCap"]; got != 0.0 {
		t.Fatalf("a new source: dailyCap = %v", got)
	}
	for _, body := range []string{`{"dailyCap":-1}`, `{"dailyCap":1001}`} {
		if w := do(s, "PATCH", target, body, header); w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_daily_cap" {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body)
		}
	}
	if w := do(s, "PATCH", target, `{"dailyCap":2}`, header); w.Code != http.StatusOK || decode(t, w)["dailyCap"] != 2.0 {
		t.Fatalf("patch: %d %s", w.Code, w.Body)
	}
	// A patch that leaves the field out keeps it.
	if w := do(s, "PATCH", target, `{"paused":false}`, header); w.Code != http.StatusOK || decode(t, w)["dailyCap"] != 2.0 {
		t.Fatalf("patch without it: %d %s", w.Code, w.Body)
	}

	folded := func(query string) (total, marked int) {
		t.Helper()
		w := do(s, "GET", "/api/entries"+query, "", header)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", query, w.Code, w.Body)
		}
		for _, item := range decode(t, w)["entries"].([]any) {
			total++
			if item.(map[string]any)["folded"] == true {
				marked++
			}
		}
		return total, marked
	}
	for _, query := range []string{"", "?unread=1", "?kind=article"} {
		if total, marked := folded(query); total != 5 || marked != 3 {
			t.Errorf("%q: %d entries, %d folded, want 5 and 3", query, total, marked)
		}
	}
	// The source view and a search show every entry as it is, and Starred has none.
	for _, query := range []string{"?source=" + strconv.FormatInt(id, 10), "?q=article"} {
		if total, marked := folded(query); total != 5 || marked != 0 {
			t.Errorf("%q: %d entries, %d folded, want 5 and 0", query, total, marked)
		}
	}

	body := decode(t, do(s, "GET", "/api/sources", "", header))
	if body["unreadTotal"] != 2.0 || body["sources"].([]any)[0].(map[string]any)["unreadCount"] != 2.0 {
		t.Errorf("unread counts = %v", body)
	}

	// Marking All read leaves the folded entries; marking the subscription reads them too.
	if w := do(s, "POST", "/api/entries/read", `{}`, header); w.Code != http.StatusOK {
		t.Fatalf("mark all: %d %s", w.Code, w.Body)
	}
	if total, marked := folded("?unread=1"); total != 3 || marked != 3 {
		t.Errorf("after marking All: %d entries, %d folded, want 3 and 3", total, marked)
	}
	if w := do(s, "POST", "/api/entries/read", `{"sourceId":`+strconv.FormatInt(id, 10)+`}`, header); w.Code != http.StatusOK {
		t.Fatalf("mark source: %d %s", w.Code, w.Body)
	}
	if total, _ := folded("?unread=1"); total != 0 {
		t.Errorf("after marking the subscription: %d unread", total)
	}
}

func TestNoisyDismissOverTheAPI(t *testing.T) {
	s, c := newServer(t, "secret")
	header := bearer(login(t, s, "laptop"))
	id := seed(t, s, "article", 250, c.t)
	target := "/api/sources/" + strconv.FormatInt(id, 10) + "/noisy/dismiss"

	if w := do(s, "POST", target, "", header); w.Code != http.StatusConflict || decode(t, w)["error"] != "not_noisy" {
		t.Fatalf("a young source: %d %s", w.Code, w.Body)
	}
	if w := do(s, "POST", "/api/sources/9999/noisy/dismiss", "", header); w.Code != http.StatusNotFound {
		t.Fatalf("unknown: %d %s", w.Code, w.Body)
	}
	if w := do(s, "POST", "/api/sources/x/noisy/dismiss", "", header); w.Code != http.StatusBadRequest {
		t.Fatalf("bad id: %d %s", w.Code, w.Body)
	}
	if w := do(s, "POST", target, "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("without a session: %d", w.Code)
	}

	c.t = c.t.Add(10 * 24 * time.Hour)
	header = bearer(login(t, s, "laptop"))
	w := do(s, "GET", "/api/sources/health", "", header)
	list := decode(t, w)["sources"].([]any)
	if len(list) != 1 {
		t.Fatalf("health: %d %s", w.Code, w.Body)
	}
	first := list[0].(map[string]any)
	if first["state"] != "noisy" || first["perDay"] != 25.0 || first["days"] != 10.0 || first["paceSec"] != 0.0 || first["dailyCap"] != 0.0 {
		t.Fatalf("health = %v", first)
	}

	if w := do(s, "POST", target, "", header); w.Code != http.StatusNoContent {
		t.Fatalf("dismiss: %d %s", w.Code, w.Body)
	}
	if list := decode(t, do(s, "GET", "/api/sources/health", "", header))["sources"].([]any); len(list) != 0 {
		t.Fatalf("dismissed source listed: %v", list)
	}
	if w := do(s, "POST", target, "", header); w.Code != http.StatusConflict {
		t.Fatalf("dismissing twice: %d %s", w.Code, w.Body)
	}
}

func TestDailyCapInOPML(t *testing.T) {
	s, _ := newServer(t, "")
	file := `<opml version="2.0"><body>
<outline text="Loud" xmlUrl="https://example.org/loud" orielDailyCap="20"/>
<outline text="Zero" xmlUrl="https://example.org/zero" orielDailyCap="0"/>
<outline text="Huge" xmlUrl="https://example.org/huge" orielDailyCap="5000"/>
<outline text="Plain" xmlUrl="https://example.org/plain"/>
</body></opml>`
	if w := do(s, "POST", "/api/opml/import", file, nil); w.Code != http.StatusOK {
		t.Fatalf("import: %d %s", w.Code, w.Body)
	}
	caps := func(s *Server) map[string]int {
		t.Helper()
		all, err := s.store.Sources(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]int{}
		for _, src := range all {
			got[src.Title] = src.DailyCap
		}
		return got
	}
	if got := caps(s); got["Loud"] != 20 || got["Zero"] != 0 || got["Huge"] != 0 || got["Plain"] != 0 {
		t.Fatalf("imported caps = %v", got)
	}

	w := do(s, "GET", "/api/opml/export", "", nil)
	if n := strings.Count(w.Body.String(), "orielDailyCap="); n != 1 || !strings.Contains(w.Body.String(), `orielDailyCap="20"`) {
		t.Fatalf("export writes the attribute %d times:\n%s", n, w.Body)
	}
	other, _ := newServer(t, "")
	do(other, "POST", "/api/opml/import", w.Body.String(), nil)
	if got := caps(other); got["Loud"] != 20 || got["Plain"] != 0 {
		t.Fatalf("caps after the round trip = %v", got)
	}
}
