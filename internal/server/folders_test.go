package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/yangflow/oriel/internal/store"
)

func TestFolderEndpoints(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	for _, r := range []struct{ method, target string }{
		{"GET", "/api/folders"}, {"POST", "/api/folders"}, {"PATCH", "/api/folders/1"}, {"DELETE", "/api/folders/1"},
	} {
		if w := do(s, r.method, r.target, `{"name":"x"}`, nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without session: %d", r.method, r.target, w.Code)
		}
	}
	h := bearer(token)

	w := do(s, "POST", "/api/folders", `{"name":" Tech "}`, h)
	made := decode(t, w)
	if w.Code != http.StatusCreated || made["name"] != "Tech" || made["sourceCount"] != 0.0 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	id := fmt.Sprint(int64(made["id"].(float64)))

	for body, want := range map[string]struct {
		status int
		code   string
	}{
		`{"name":"  "}`: {http.StatusBadRequest, "invalid_name"},
		`{"name":"` + strings.Repeat("a", 61) + `"}`: {http.StatusBadRequest, "invalid_name"},
		`{"name":"tech"}`: {http.StatusConflict, "folder_exists"},
	} {
		w := do(s, "POST", "/api/folders", body, h)
		got := decode(t, w)
		if w.Code != want.status || got["error"] != want.code {
			t.Fatalf("%.20s: %d %s", body, w.Code, w.Body)
		}
		if want.code == "folder_exists" && fmt.Sprint(int64(got["folderId"].(float64))) != id {
			t.Fatalf("the clash names the folder: %s", w.Body)
		}
	}

	if w := do(s, "PATCH", "/api/folders/"+id, `{"name":"News"}`, h); w.Code != http.StatusOK || decode(t, w)["name"] != "News" {
		t.Fatalf("rename: %d %s", w.Code, w.Body)
	}
	if w := do(s, "PATCH", "/api/folders/999", `{"name":"News"}`, h); w.Code != http.StatusNotFound {
		t.Fatalf("rename a missing folder: %d %s", w.Code, w.Body)
	}
	if w := do(s, "PATCH", "/api/folders/"+id, `{"name":""}`, h); w.Code != http.StatusBadRequest {
		t.Fatalf("rename to nothing: %d %s", w.Code, w.Body)
	}

	// A subscription moves in through PATCH and out with 0; the counts follow.
	do(s, "POST", "/api/sources", `{"url":"https://a.example/feed.xml","kind":"article"}`, h)
	srcID, err := s.store.SourceIDByFeedURL(context.Background(), "https://a.example/feed.xml")
	if err != nil {
		t.Fatal(err)
	}
	target := fmt.Sprintf("/api/sources/%d", srcID)
	if src := decode(t, do(s, "GET", target, "", h)); src["folderId"] != nil {
		t.Fatalf("a new source is in no folder: %v", src)
	}
	if w := do(s, "PATCH", target, `{"folderId":`+id+`}`, h); w.Code != http.StatusOK || fmt.Sprint(decode(t, w)["folderId"]) != id {
		t.Fatalf("move in: %d %s", w.Code, w.Body)
	}
	if w := do(s, "PATCH", target, `{"folderId":999}`, h); w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_folder" {
		t.Fatalf("move to a missing folder: %d %s", w.Code, w.Body)
	}
	list := decode(t, do(s, "GET", "/api/folders", "", h))["folders"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["sourceCount"] != 1.0 {
		t.Fatalf("list: %v", list)
	}
	if w := do(s, "PATCH", target, `{"folderId":0}`, h); w.Code != http.StatusOK || decode(t, w)["folderId"] != nil {
		t.Fatalf("move out: %d %s", w.Code, w.Body)
	}
	do(s, "PATCH", target, `{"folderId":`+id+`}`, h)

	// Deleting a folder keeps its subscriptions, which become unfiled.
	if w := do(s, "DELETE", "/api/folders/"+id, "", h); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if w := do(s, "DELETE", "/api/folders/"+id, "", h); w.Code != http.StatusNotFound {
		t.Fatalf("delete twice: %d %s", w.Code, w.Body)
	}
	if src := decode(t, do(s, "GET", target, "", h)); src["folderId"] != nil {
		t.Fatalf("the source stays, unfiled: %v", src)
	}
}

func TestFolderTimeline(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	h := bearer(token)
	ctx := context.Background()

	folder := int64(decode(t, do(s, "POST", "/api/folders", `{"name":"Tech"}`, h))["id"].(float64))
	for i, feed := range []string{"a", "b", "c"} {
		url := "https://" + feed + ".example/feed.xml"
		do(s, "POST", "/api/sources", `{"url":"`+url+`","kind":"article"}`, h)
		id, err := s.store.SourceIDByFeedURL(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.store.ImportEntries(ctx, id, []store.ImportedEntry{{URL: "https://" + feed + ".example/1", Starred: true}}, s.now()); err != nil {
			t.Fatal(err)
		}
		if i < 2 {
			do(s, "PATCH", fmt.Sprintf("/api/sources/%d", id), fmt.Sprintf(`{"folderId":%d}`, folder), h)
		}
	}
	entries := func(query string) []string {
		var urls []string
		for _, e := range decode(t, do(s, "GET", "/api/entries"+query, "", h))["entries"].([]any) {
			urls = append(urls, e.(map[string]any)["url"].(string))
		}
		return urls
	}
	if got := entries(""); len(got) != 3 {
		t.Fatalf("all: %v", got)
	}
	if got := entries(fmt.Sprintf("?folder=%d", folder)); len(got) != 2 {
		t.Fatalf("folder: %v", got)
	}
	if got := entries(fmt.Sprintf("?folder=%d&kind=social", folder)); len(got) != 0 {
		t.Fatalf("folder and another kind: %v", got)
	}
	for _, q := range []string{"?folder=999", "?folder=abc", "?folder=-1"} {
		if w := do(s, "GET", "/api/entries"+q, "", h); w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_folder" {
			t.Fatalf("%s: %d %s", q, w.Code, w.Body)
		}
	}

	if w := do(s, "POST", "/api/entries/read", `{"folder":999}`, h); w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_folder" {
		t.Fatalf("mark read in a missing folder: %d %s", w.Code, w.Body)
	}
	if w := do(s, "POST", "/api/entries/read", fmt.Sprintf(`{"folder":%d}`, folder), h); w.Code != http.StatusOK {
		t.Fatalf("mark read: %d %s", w.Code, w.Body)
	}
	if got := entries("?unread=1"); len(got) != 1 || got[0] != "https://c.example/1" {
		t.Fatalf("only the unfiled one stays unread: %v", got)
	}
}

func TestOPMLFolders(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	h := bearer(token)
	ctx := context.Background()

	// An existing unfiled subscription is filed by the import; one already in a folder stays there.
	do(s, "POST", "/api/sources", `{"url":"https://u.example/feed","kind":"article"}`, h)
	do(s, "POST", "/api/sources", `{"url":"https://k.example/feed","kind":"article"}`, h)
	keep := int64(decode(t, do(s, "POST", "/api/folders", `{"name":"Keep"}`, h))["id"].(float64))
	kid, err := s.store.SourceIDByFeedURL(ctx, "https://k.example/feed")
	if err != nil {
		t.Fatal(err)
	}
	do(s, "PATCH", fmt.Sprintf("/api/sources/%d", kid), fmt.Sprintf(`{"folderId":%d}`, keep), h)

	file := `<?xml version="1.0"?><opml version="2.0"><body>
  <outline text="News">
    <outline text="Deep"><outline text="n1" type="rss" xmlUrl="https://n1.example/feed"/></outline>
    <outline text="n2" type="rss" xmlUrl="https://n2.example/feed"/>
  </outline>
  <outline text="u" type="rss" xmlUrl="https://u.example/feed"/>
  <outline text="Other"><outline text="k" type="rss" xmlUrl="https://k.example/feed"/></outline>
  <outline text="loose" type="rss" xmlUrl="https://loose.example/feed"/>
</body></opml>`
	w := do(s, "POST", "/api/opml/import", file, h)
	res := decode(t, w)
	if w.Code != http.StatusOK || res["added"] != 3.0 || res["existing"] != 2.0 || res["folders"] != 2.0 {
		t.Fatalf("import: %d %s", w.Code, w.Body)
	}
	folderOf := map[string]string{}
	names := map[float64]string{}
	for _, f := range decode(t, do(s, "GET", "/api/folders", "", h))["folders"].([]any) {
		names[f.(map[string]any)["id"].(float64)] = f.(map[string]any)["name"].(string)
	}
	for _, item := range decode(t, do(s, "GET", "/api/sources", "", h))["sources"].([]any) {
		src := item.(map[string]any)
		id, _ := src["folderId"].(float64)
		folderOf[src["url"].(string)] = names[id]
	}
	want := map[string]string{
		"https://n1.example/feed": "News", "https://n2.example/feed": "News", "https://u.example/feed": "",
		"https://k.example/feed": "Keep", "https://loose.example/feed": "",
	}
	// u was in no folder and sits outside any group in the file, so it stays unfiled.
	for url, folder := range want {
		if folderOf[url] != folder {
			t.Errorf("%s is in %q, want %q", url, folderOf[url], folder)
		}
	}

	// The export writes one outline level per folder, and importing it elsewhere rebuilds the folders.
	out := do(s, "GET", "/api/opml/export", "", h).Body.String()
	if strings.Contains(out, `text="Deep"`) || !strings.Contains(out, `text="News"`) || !strings.Contains(out, `text="Keep"`) {
		t.Fatalf("export: %s", out)
	}
	other, _ := newServer(t, "secret")
	h2 := bearer(login(t, other, "laptop"))
	again := decode(t, do(other, "POST", "/api/opml/import", out, h2))
	if again["added"] != 5.0 || again["folders"] != 2.0 {
		t.Fatalf("round trip: %v", again)
	}
	var back []string
	for _, f := range decode(t, do(other, "GET", "/api/folders", "", h2))["folders"].([]any) {
		back = append(back, fmt.Sprintf("%s:%v", f.(map[string]any)["name"], f.(map[string]any)["sourceCount"]))
	}
	if fmt.Sprint(back) != "[Keep:1 News:2]" {
		t.Fatalf("folders after the round trip: %v", back)
	}
}
