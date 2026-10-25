package server

import (
	"context"
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

func TestRevisionEndpoint(t *testing.T) {
	s, c := newServer(t, "secret")
	header := bearer(login(t, s, "laptop"))
	source := seed(t, s, "article", 1, c.t)
	entries := decode(t, do(s, "GET", "/api/entries", "", header))["entries"].([]any)
	id := int64(entries[0].(map[string]any)["id"].(float64))
	target := "/api/entries/" + strconv.FormatInt(id, 10) + "/revision"

	if w := do(s, "GET", target, "", header); w.Code != http.StatusNotFound {
		t.Fatalf("an entry never updated: %d %s", w.Code, w.Body)
	}
	if w := do(s, "GET", "/api/entries/999999/revision", "", header); w.Code != http.StatusNotFound {
		t.Fatalf("a missing entry: %d", w.Code)
	}
	if w := do(s, "GET", "/api/entries/x/revision", "", header); w.Code != http.StatusBadRequest {
		t.Fatalf("a bad id: %d", w.Code)
	}
	if w := do(s, "GET", target, "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("without a token: %d", w.Code)
	}
	if entry := decode(t, do(s, "GET", "/api/entries", "", header))["entries"].([]any)[0].(map[string]any); entry["revisedAt"] != nil {
		t.Fatalf("revisedAt before an update: %v", entry["revisedAt"])
	}

	later := c.t.Add(time.Hour)
	update := store.FeedEntry{GUID: "0", Title: "article 0", ContentHTML: "<p>body</p><p>added</p>"}
	if _, err := s.store.SaveFeed(context.Background(), store.FeedSource{ID: source, Kind: "article"}, "", "", []store.FeedEntry{update}, "", time.Time{}, later); err != nil {
		t.Fatal(err)
	}
	w := do(s, "GET", target, "", header)
	if w.Code != http.StatusOK {
		t.Fatalf("revision: %d %s", w.Code, w.Body)
	}
	body := decode(t, w)
	if body["fetchedAt"] != float64(c.t.UnixMilli()) || body["updatedAt"] != float64(later.UnixMilli()) {
		t.Errorf("times = %v and %v", body["fetchedAt"], body["updatedAt"])
	}
	previous := body["previous"].(map[string]any)
	current := body["current"].(map[string]any)
	if previous["title"] != "article 0" || !reflect.DeepEqual(previous["paragraphs"], []any{"body"}) ||
		!reflect.DeepEqual(current["paragraphs"], []any{"body", "added"}) {
		t.Errorf("previous = %v, current = %v", previous, current)
	}
	entry := decode(t, do(s, "GET", "/api/entries/"+strconv.FormatInt(id, 10), "", header))
	if entry["revisedAt"] != float64(later.UnixMilli()) {
		t.Errorf("revisedAt = %v", entry["revisedAt"])
	}
}
