package server

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestHideFromAllOverTheAPI(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	header := bearer(token)
	kept := seed(t, s, "article", 2, c.t)
	hidden := seed(t, s, "image", 3, c.t)
	target := "/api/sources/" + strconv.FormatInt(hidden, 10)

	if got := decode(t, do(s, "GET", target, "", header))["hideFromAll"]; got != false {
		t.Fatalf("a new source: hideFromAll = %v", got)
	}
	if w := do(s, "PATCH", target, `{"hideFromAll":true}`, header); w.Code != http.StatusOK || decode(t, w)["hideFromAll"] != true {
		t.Fatalf("patch: %d %s", w.Code, w.Body)
	}
	// A patch that leaves the field out keeps it.
	if w := do(s, "PATCH", target, `{"paused":false}`, header); w.Code != http.StatusOK || decode(t, w)["hideFromAll"] != true {
		t.Fatalf("patch without it: %d %s", w.Code, w.Body)
	}

	list := func(query string) []string {
		t.Helper()
		w := do(s, "GET", "/api/entries"+query, "", header)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", query, w.Code, w.Body)
		}
		got := titles(t, decode(t, w))
		slices.Sort(got)
		return got
	}
	wantKept := []string{"article 0", "article 1"}
	if got := list(""); !slices.Equal(got, wantKept) {
		t.Errorf("All = %q", got)
	}
	if got := list("?unread=1"); !slices.Equal(got, wantKept) {
		t.Errorf("Unread = %q", got)
	}
	if got := list("?kind=image"); len(got) != 0 {
		t.Errorf("kind = %q", got)
	}
	if got := list("?source=" + strconv.FormatInt(hidden, 10)); len(got) != 3 {
		t.Errorf("source view = %q", got)
	}
	if got := list("?q=image"); len(got) != 3 {
		t.Errorf("search = %q", got)
	}

	// The totals leave the hidden source out; its own count stays.
	body := decode(t, do(s, "GET", "/api/sources", "", header))
	if body["unreadTotal"] != 2.0 {
		t.Errorf("unreadTotal = %v, want 2", body["unreadTotal"])
	}
	for _, item := range body["sources"].([]any) {
		src := item.(map[string]any)
		if int64(src["id"].(float64)) == hidden && src["unreadCount"] != 3.0 {
			t.Errorf("unreadCount of the hidden source = %v, want 3", src["unreadCount"])
		}
	}

	// Starred shows a hidden entry, and marking All read leaves the hidden source unread.
	first := decode(t, do(s, "GET", "/api/entries?source="+strconv.FormatInt(hidden, 10), "", header))["entries"].([]any)[0].(map[string]any)
	id := strconv.Itoa(int(first["id"].(float64)))
	if w := do(s, "PATCH", "/api/entries/"+id, `{"starred":true}`, header); w.Code != http.StatusOK {
		t.Fatalf("star: %d %s", w.Code, w.Body)
	}
	if got := list("?starred=1"); len(got) != 1 {
		t.Errorf("Starred = %q", got)
	}
	if w := do(s, "POST", "/api/entries/read", `{}`, header); w.Code != http.StatusOK {
		t.Fatalf("mark read: %d %s", w.Code, w.Body)
	}
	if src, err := s.store.Source(context.Background(), hidden); err != nil || src.UnreadCount != 3 {
		t.Errorf("hidden source after marking All read = %+v, %v", src, err)
	}
	if src, err := s.store.Source(context.Background(), kept); err != nil || src.UnreadCount != 0 {
		t.Errorf("shown source after marking All read = %+v, %v", src, err)
	}
}

func TestHideFromAllInOPML(t *testing.T) {
	s, _ := newServer(t, "")
	file := `<opml version="2.0"><body>
<outline text="Quiet" xmlUrl="https://example.org/quiet" orielHideFromAll="1"/>
<outline text="Loud" xmlUrl="https://example.org/loud" orielHideFromAll="0"/>
<outline text="Plain" xmlUrl="https://example.org/plain"/>
</body></opml>`
	if w := do(s, "POST", "/api/opml/import", file, nil); w.Code != http.StatusOK {
		t.Fatalf("import: %d %s", w.Code, w.Body)
	}
	flags := func(s *Server) map[string]bool {
		t.Helper()
		all, err := s.store.Sources(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, src := range all {
			got[src.Title] = src.HideFromAll
		}
		return got
	}
	if got := flags(s); !got["Quiet"] || got["Loud"] || got["Plain"] {
		t.Fatalf("imported flags = %v", got)
	}

	// An import never changes a subscription that already exists.
	again := `<opml version="2.0"><body><outline text="Quiet" xmlUrl="https://example.org/quiet"/>
<outline text="Plain" xmlUrl="https://example.org/plain" orielHideFromAll="1"/></body></opml>`
	do(s, "POST", "/api/opml/import", again, nil)
	if got := flags(s); !got["Quiet"] || got["Plain"] {
		t.Fatalf("flags after importing existing ones = %v", got)
	}

	w := do(s, "GET", "/api/opml/export", "", nil)
	if n := strings.Count(w.Body.String(), `orielHideFromAll="1"`); n != 1 || strings.Contains(w.Body.String(), `orielHideFromAll="0"`) {
		t.Fatalf("export writes the attribute %d times:\n%s", n, w.Body)
	}
	other, _ := newServer(t, "")
	do(other, "POST", "/api/opml/import", w.Body.String(), nil)
	if got := flags(other); !got["Quiet"] || got["Loud"] || got["Plain"] {
		t.Fatalf("flags after the round trip = %v", got)
	}
}
