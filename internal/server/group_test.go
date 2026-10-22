package server

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

// groupSeed makes two sources carry one article under different tracking parameters, plus one article of their own.
func groupSeed(t *testing.T, s *Server, at time.Time) {
	t.Helper()
	ctx := context.Background()
	for i, name := range []string{"first", "second"} {
		src, err := s.store.CreateSource(ctx, store.NewSource{URL: "https://" + name + ".example/feed", Kind: "article", FetchInterval: time.Hour}, name, at)
		if err != nil {
			t.Fatal(err)
		}
		items := []store.FeedEntry{{GUID: name + "-same", Title: name + " same", URL: "https://news.example/story?utm_source=" + name}}
		if i == 1 {
			items = append(items, store.FeedEntry{GUID: "own", Title: "own", URL: "https://news.example/own"})
		}
		if _, err := s.store.SaveFeed(ctx, store.FeedSource{ID: src.ID, Kind: "article"}, name, "", items, "", time.Time{}, at.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTimelineGroupsTheSameLink(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	groupSeed(t, s, c.t)

	list := func() []any {
		t.Helper()
		w := do(s, "GET", "/api/entries", "", bearer(token))
		if w.Code != http.StatusOK {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		return decode(t, w)["entries"].([]any)
	}
	unread := func() float64 {
		t.Helper()
		return decode(t, do(s, "GET", "/api/sources", "", bearer(token)))["unreadTotal"].(float64)
	}

	rows := list()
	if len(rows) != 2 || unread() != 2 {
		t.Fatalf("%d rows, %v unread", len(rows), unread())
	}
	var main map[string]any
	for _, r := range rows {
		row := r.(map[string]any)
		if row["title"] == "first same" {
			main = row
		} else if _, has := row["group"]; has {
			t.Fatalf("a lone entry carries a group: %v", row)
		}
	}
	if main == nil {
		t.Fatal("the earliest arrival should lead")
	}
	group := main["group"].(map[string]any)
	members := group["entries"].([]any)
	if group["by"] != "link" || len(members) != 1 {
		t.Fatalf("group = %v", group)
	}
	member := members[0].(map[string]any)
	if member["sourceTitle"] != "second" || member["title"] != "second same" || member["isRead"] != false || member["starred"] != false {
		t.Fatalf("member = %v", member)
	}

	// Reading the row reads the whole group, and the reply shows it.
	id := strconv.FormatInt(int64(main["id"].(float64)), 10)
	w := do(s, "PATCH", "/api/entries/"+id, `{"isRead":true}`, bearer(token))
	if body := decode(t, w); w.Code != http.StatusOK || body["isRead"] != true || body["group"].(map[string]any)["entries"].([]any)[0].(map[string]any)["isRead"] != true {
		t.Fatalf("%d %v", w.Code, body)
	}
	if unread() != 1 {
		t.Fatalf("unread = %v", unread())
	}
	if w := do(s, "GET", "/api/entries/"+id, "", bearer(token)); decode(t, w)["group"] == nil {
		t.Fatal("the entry reply lacks its group")
	}

	// Mark all read reaches the copies as well.
	if w := do(s, "POST", "/api/entries/read", `{}`, bearer(token)); w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if unread() != 0 {
		t.Fatalf("unread = %v", unread())
	}
}

func TestGroupSameLinkOffShowsEveryCopy(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	groupSeed(t, s, c.t)

	if w := do(s, "PATCH", "/api/settings", `{"groupSameLink":false}`, bearer(token)); w.Code != http.StatusOK || decode(t, w)["groupSameLink"] != false {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	body := decode(t, do(s, "GET", "/api/entries", "", bearer(token)))
	rows := body["entries"].([]any)
	if len(rows) != 3 {
		t.Fatalf("%d rows", len(rows))
	}
	for _, r := range rows {
		if _, has := r.(map[string]any)["group"]; has {
			t.Fatalf("a group with the setting off: %v", r)
		}
	}
	if n := decode(t, do(s, "GET", "/api/sources", "", bearer(token)))["unreadTotal"]; n != 3.0 {
		t.Fatalf("unread = %v", n)
	}
	if w := do(s, "PATCH", "/api/settings", `{"groupSameLink":true}`, bearer(token)); decode(t, w)["groupSameLink"] != true {
		t.Fatal("not back on")
	}
}
