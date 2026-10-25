package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

func TestTranslateTitlesOverTheAPI(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	header := bearer(token)
	id := seed(t, s, "article", 2, c.t)
	target := "/api/sources/" + strconv.FormatInt(id, 10)

	if got := decode(t, do(s, "GET", target, "", header))["translateTitles"]; got != false {
		t.Fatalf("a new source: translateTitles = %v", got)
	}
	if w := do(s, "PATCH", target, `{"translateTitles":true}`, header); w.Code != http.StatusOK || decode(t, w)["translateTitles"] != true {
		t.Fatalf("patch: %d %s", w.Code, w.Body)
	}
	// A patch that leaves the field out keeps it.
	if w := do(s, "PATCH", target, `{"paused":false}`, header); w.Code != http.StatusOK || decode(t, w)["translateTitles"] != true {
		t.Fatalf("patch without it: %d %s", w.Code, w.Body)
	}
	if w := do(s, "PATCH", target, `{"translateTitles":"yes"}`, header); w.Code != http.StatusBadRequest {
		t.Fatalf("a string: %d %s", w.Code, w.Body)
	}

	// Entries carry the translation only when there is one.
	first := decode(t, do(s, "GET", "/api/entries?source="+strconv.FormatInt(id, 10), "", header))["entries"].([]any)[0].(map[string]any)
	if _, ok := first["translatedTitle"]; ok {
		t.Errorf("an untranslated entry has translatedTitle %v", first["translatedTitle"])
	}
	ctx := context.Background()
	feed := store.FeedSource{ID: id, Kind: "article", TranslateTitles: true}
	if _, err := s.store.SaveFeed(ctx, feed, "", "", []store.FeedEntry{{GUID: "de", Title: "Ein Titel", Language: "de"}}, "zh-Hans", time.Time{}, c.t); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.store.PendingTitles(ctx, 10, 3, c.t)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("pending titles = %v, %v", jobs, err)
	}
	if err := s.store.SaveTitles(ctx, []store.TitleResult{{ID: jobs[0].ID, Title: jobs[0].Title, Text: "译文"}}, c.t); err != nil {
		t.Fatal(err)
	}
	body := decode(t, do(s, "GET", "/api/entries/"+strconv.FormatInt(jobs[0].ID, 10), "", header))
	if body["translatedTitle"] != "译文" {
		t.Errorf("entry = %v", body)
	}
	if w := do(s, "GET", "/api/entries?q="+"译文", "", header); w.Code != http.StatusOK || len(titles(t, decode(t, w))) != 1 {
		t.Errorf("search by the translation: %d %s", w.Code, w.Body)
	}
}

func TestTranslateTitlesInOPML(t *testing.T) {
	s, _ := newServer(t, "")
	file := `<opml version="2.0"><body>
<outline text="Foreign" xmlUrl="https://example.org/foreign" orielTranslateTitles="1"/>
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
			got[src.Title] = src.TranslateTitles
		}
		return got
	}
	if got := flags(s); !got["Foreign"] || got["Plain"] {
		t.Fatalf("imported flags = %v", got)
	}
	w := do(s, "GET", "/api/opml/export", "", nil)
	if n := strings.Count(w.Body.String(), `orielTranslateTitles="1"`); n != 1 {
		t.Fatalf("export writes the attribute %d times:\n%s", n, w.Body)
	}
	other, _ := newServer(t, "")
	do(other, "POST", "/api/opml/import", w.Body.String(), nil)
	if got := flags(other); !got["Foreign"] || got["Plain"] {
		t.Fatalf("flags after the round trip = %v", got)
	}
}
