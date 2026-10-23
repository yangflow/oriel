package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/yangflow/oriel/internal/fetch"
)

func TestDuplicateSubscription(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	if err := s.store.SetSetting(context.Background(), fetch.RSSHubSetting, "https://hub.example"); err != nil {
		t.Fatal(err)
	}
	add := func(body string) *map[string]any {
		t.Helper()
		w := do(s, "POST", "/api/sources", body, bearer(token))
		got := decode(t, w)
		got["status"] = float64(w.Code)
		return &got
	}
	first := *add(`{"url":"https://example.org/feed","kind":"article","title":"Blog"}`)
	route := *add(`{"rsshubPath":"/github/issue/a/b","kind":"social"}`)
	if first["status"] != 201.0 || route["status"] != 201.0 {
		t.Fatalf("first adds: %v %v", first, route)
	}

	for body, want := range map[string]any{
		`{"url":"http://www.example.org/feed/","kind":"article"}`:                     first["id"],
		`{"url":"https://hub.example/github/issue/a/b?utm_source=x","kind":"social"}`: route["id"],
		`{"rsshubPath":"/github/issue/a/b/","kind":"social"}`:                         route["id"],
	} {
		got := *add(body)
		if got["status"] != 409.0 || got["error"] != "source_exists" || got["sourceId"] != want {
			t.Errorf("%s: %v, want source %v", body, got, want)
		}
	}

	forced := *add(`{"url":"http://www.example.org/feed","kind":"article","allowDuplicate":true}`)
	if forced["status"] != 201.0 || forced["id"] == first["id"] {
		t.Errorf("forced: %v", forced)
	}
	if again := *add(`{"url":"http://www.example.org/feed","kind":"article","allowDuplicate":true}`); again["status"] != 409.0 {
		t.Errorf("the same address twice, forced: %v", again)
	}
}

func TestDuplicateAcrossImportAndDiscovery(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	do(s, "POST", "/api/sources", `{"url":"https://example.org/feed","kind":"article"}`, bearer(token))
	created := decode(t, do(s, "POST", "/api/sources", `{"rsshubPath":"/bilibili/user/video/1","kind":"video"}`, bearer(token)))

	file := `<?xml version="1.0"?><opml version="2.0"><body>
	<outline text="A" xmlUrl="http://www.example.org/feed/"/>
	<outline text="B" xmlUrl="https://example.org/feed?utm_source=x"/>
	<outline text="C" xmlUrl="https://example.org/new"/>
	<outline text="D" xmlUrl="http://example.org/new"/>
	</body></opml>`
	w := do(s, "POST", "/api/opml/import", file, bearer(token))
	if body := decode(t, w); w.Code != http.StatusOK || body["added"] != 1.0 || body["existing"] != 3.0 {
		t.Fatalf("import: %d %v", w.Code, body)
	}

	s.discover = finder(func(string) ([]fetch.Candidate, error) {
		return []fetch.Candidate{
			{URL: "https://www.example.org/feed", Origin: "feed"},
			{RSSHubPath: "/bilibili/user/video/1", Origin: "rule"},
			{RSSHubPath: "/bilibili/user/dynamic/1", Origin: "rule"},
		}, nil
	})
	w = do(s, "GET", "/api/discover?url="+url.QueryEscape("https://example.org/"), "", bearer(token))
	list := decode(t, w)["candidates"].([]any)
	var got []string
	for _, c := range list {
		got = append(got, fmt.Sprint(c.(map[string]any)["subscribedId"]))
	}
	if want := fmt.Sprintf("[1 %v 0]", created["id"]); fmt.Sprint(got) != want {
		t.Errorf("subscribedId = %v, want %s (%s)", got, want, strings.TrimSpace(w.Body.String()))
	}
}
