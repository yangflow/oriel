package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

// seed adds a source of kind with n entries titled "<kind> 0" and up, the last the newest, and returns its ID.
func seed(t *testing.T, s *Server, kind string, n int, at time.Time) int64 {
	t.Helper()
	ctx := context.Background()
	src, err := s.store.CreateSource(ctx, store.NewSource{URL: "https://example.org/" + kind, Kind: kind, FetchInterval: time.Hour}, "", at)
	if err != nil {
		t.Fatal(err)
	}
	items := make([]store.FeedEntry, n)
	for i := range items {
		items[n-1-i] = store.FeedEntry{GUID: strconv.Itoa(i), Title: fmt.Sprintf("%s %d", kind, i), ContentHTML: "<p>body</p>"}
	}
	if _, err := s.store.SaveFeed(ctx, store.FeedSource{ID: src.ID, Kind: kind}, "", "", items, "", time.Time{}, at); err != nil {
		t.Fatal(err)
	}
	return src.ID
}

func titles(t *testing.T, body map[string]any) []string {
	t.Helper()
	var got []string
	for _, item := range body["entries"].([]any) {
		got = append(got, item.(map[string]any)["title"].(string))
	}
	return got
}

func TestEntryQueryValidation(t *testing.T) {
	s, _ := newServer(t, "")
	for query, code := range map[string]string{
		"source=x":             "invalid_source",
		"source=0":             "invalid_source",
		"kind=podcast":         "invalid_kind",
		"q=ab":                 "query_too_short",
		"q=%E4%B8%AD%E6%96%87": "query_too_short",
		"limit=0":              "invalid_limit",
		"limit=101":            "invalid_limit",
		"cursor=abc":           "invalid_cursor",
		"cursor=1.0":           "invalid_cursor",
		"maxMinutes=0":         "invalid_max_minutes",
		"maxMinutes=1441":      "invalid_max_minutes",
		"maxMinutes=x":         "invalid_max_minutes",
		"maxMinutes=2.5":       "invalid_max_minutes",
	} {
		w := do(s, "GET", "/api/entries?"+query, "", nil)
		if w.Code != http.StatusBadRequest || decode(t, w)["error"] != code {
			t.Errorf("%s: %d %s, want %s", query, w.Code, w.Body, code)
		}
	}
}

func TestTimeline(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	if w := do(s, "GET", "/api/entries", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("without session: %d", w.Code)
	}
	seed(t, s, "article", 3, c.t)
	video := seed(t, s, "video", 2, c.t.Add(time.Minute))

	// Pages follow the cursor without gaps or repeats, even when newer entries arrive in between.
	var got []string
	target := "/api/entries?limit=2"
	for pages := 0; ; pages++ {
		body := decode(t, do(s, "GET", target, "", bearer(token)))
		got = append(got, titles(t, body)...)
		if pages == 0 {
			seed(t, s, "social", 1, c.t.Add(time.Hour))
		}
		if body["nextCursor"] == nil {
			break
		}
		target = "/api/entries?limit=2&cursor=" + body["nextCursor"].(string)
	}
	if fmt.Sprint(got) != "[video 1 video 0 article 2 article 1 article 0]" {
		t.Fatalf("pages: %v", got)
	}
	if got := titles(t, decode(t, do(s, "GET", "/api/entries?kind=social", "", bearer(token)))); fmt.Sprint(got) != "[social 0]" {
		t.Fatalf("new entry on top: %v", got)
	}
	if got := titles(t, decode(t, do(s, "GET", "/api/entries?source="+strconv.FormatInt(video, 10), "", bearer(token)))); len(got) != 2 {
		t.Fatalf("by source: %v", got)
	}
	if got := titles(t, decode(t, do(s, "GET", "/api/entries?q=+ARTICLE+2+", "", bearer(token)))); fmt.Sprint(got) != "[article 2]" {
		t.Fatalf("search: %v", got)
	}

	list := decode(t, do(s, "GET", "/api/entries?kind=article", "", bearer(token)))["entries"].([]any)
	first := list[0].(map[string]any)
	if first["sourceTitle"] != "https://example.org/article" || first["publishedAt"] != nil || first["durationSec"] != nil ||
		first["readAt"] != nil || first["contentHtml"] != nil || len(first["images"].([]any)) != 0 {
		t.Fatalf("list item: %v", first)
	}
	id := strconv.Itoa(int(first["id"].(float64)))

	c.t = c.t.Add(2 * time.Hour)
	w := do(s, "PATCH", "/api/entries/"+id, `{"isRead":true,"starred":true}`, bearer(token))
	entry := decode(t, w)
	if w.Code != http.StatusOK || entry["isRead"] != true || entry["starred"] != true || entry["readAt"] != float64(c.t.UnixMilli()) ||
		entry["contentHtml"] != "<p>body</p>" || entry["extractedHtml"] != "" {
		t.Fatalf("patch: %d %v", w.Code, entry)
	}
	if got := decode(t, do(s, "GET", "/api/entries/"+id, "", bearer(token))); got["starred"] != true {
		t.Fatalf("get: %v", got)
	}
	if got := titles(t, decode(t, do(s, "GET", "/api/entries?starred=1", "", bearer(token)))); fmt.Sprint(got) != "[article 2]" {
		t.Fatalf("starred: %v", got)
	}
	if w := do(s, "PATCH", "/api/entries/"+id, `{"isRead":"yes"}`, bearer(token)); w.Code != http.StatusBadRequest {
		t.Fatalf("bad patch: %d", w.Code)
	}
	if _, set := decode(t, do(s, "GET", "/api/entries/"+id, "", bearer(token)))["readProgress"]; set {
		t.Fatal("readProgress is present before any scroll")
	}
	for _, edge := range []string{"0", "0.37", "1"} {
		w := do(s, "PATCH", "/api/entries/"+id, `{"readProgress":`+edge+`}`, bearer(token))
		want, _ := strconv.ParseFloat(edge, 64)
		if got := decode(t, w); w.Code != http.StatusOK || got["readProgress"] != want {
			t.Fatalf("progress %s: %d %v", edge, w.Code, got)
		}
	}
	for _, bad := range []string{"-0.1", "1.01", `"half"`} {
		w := do(s, "PATCH", "/api/entries/"+id, `{"readProgress":`+bad+`}`, bearer(token))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("progress %s: %d", bad, w.Code)
		}
	}
	if w := do(s, "PATCH", "/api/entries/"+id, `{"readProgress":2}`, bearer(token)); decode(t, w)["error"] != "invalid_progress" {
		t.Fatalf("error code: %v", w.Body.String())
	}

	// Mark read up to the oldest video: the newer social entry and the newer video stay unread.
	videos := decode(t, do(s, "GET", "/api/entries?kind=video", "", bearer(token)))["entries"].([]any)
	oldestVideo := int(videos[1].(map[string]any)["id"].(float64))
	w = do(s, "POST", "/api/entries/read", fmt.Sprintf(`{"upTo":%d}`, oldestVideo), bearer(token))
	marked := decode(t, w)
	if w.Code != http.StatusOK || marked["marked"] != 3.0 || len(marked["ids"].([]any)) != 3 {
		t.Fatalf("mark read: %d %s", w.Code, w.Body)
	}
	if got := titles(t, decode(t, do(s, "GET", "/api/entries?unread=1", "", bearer(token)))); fmt.Sprint(got) != "[social 0 video 1]" {
		t.Fatalf("unread: %v", got)
	}
	undo, _ := json.Marshal(map[string]any{"ids": marked["ids"]})
	if w := do(s, "POST", "/api/entries/unread", string(undo), bearer(token)); w.Code != http.StatusOK || decode(t, w)["marked"] != 3.0 {
		t.Fatalf("undo: %d %s", w.Code, w.Body)
	}
	if got := titles(t, decode(t, do(s, "GET", "/api/entries?unread=1", "", bearer(token)))); len(got) != 5 {
		t.Fatalf("unread after undo: %v", got)
	}
	for _, body := range []string{`{"ids":[]}`, `{}`, `{"ids":"1"}`} {
		if w := do(s, "POST", "/api/entries/unread", body, bearer(token)); w.Code != http.StatusBadRequest {
			t.Errorf("unread %s: %d", body, w.Code)
		}
	}
	do(s, "POST", "/api/entries/read", fmt.Sprintf(`{"upTo":%d}`, oldestVideo), bearer(token))
	if w := do(s, "POST", "/api/entries/read", `{"kind":"social"}`, bearer(token)); decode(t, w)["marked"] != 1.0 {
		t.Fatalf("mark social read: %s", w.Body)
	}
	if w := do(s, "POST", "/api/entries/read", "", bearer(token)); w.Code != http.StatusOK || decode(t, w)["marked"] != 1.0 {
		t.Fatalf("mark all read: %d %s", w.Code, w.Body)
	}
	do(s, "POST", "/api/entries/unread", string(undo), bearer(token))
	// Newer than the oldest video and up to the newest video: the entries in between, not the bounds.
	newestVideo := int(videos[0].(map[string]any)["id"].(float64))
	w = do(s, "POST", "/api/entries/read", fmt.Sprintf(`{"newerThan":%d,"upTo":%d}`, oldestVideo, newestVideo), bearer(token))
	if got := decode(t, w); w.Code != http.StatusOK || got["marked"] != float64(len(got["ids"].([]any))) {
		t.Fatalf("mark between: %d %s", w.Code, w.Body)
	}
	for _, id := range decode(t, w)["ids"].([]any) {
		if int(id.(float64)) == oldestVideo {
			t.Fatalf("newerThan marked its own entry: %s", w.Body)
		}
	}
	if w := do(s, "POST", "/api/entries/read", fmt.Sprintf(`{"newerThan":%d,"kind":"video"}`, newestVideo), bearer(token)); decode(t, w)["marked"] != 0.0 {
		t.Fatalf("nothing newer than the newest video: %s", w.Body)
	}
	for body, want := range map[string]int{`{"upTo":999}`: http.StatusNotFound, `{"newerThan":999}`: http.StatusNotFound, `{"kind":"link"}`: http.StatusBadRequest} {
		if w := do(s, "POST", "/api/entries/read", body, bearer(token)); w.Code != want {
			t.Errorf("%s: %d", body, w.Code)
		}
	}
	for _, r := range []struct{ method, target, body string }{
		{"GET", "/api/entries/999", ""},
		{"PATCH", "/api/entries/999", `{"isRead":true}`},
	} {
		if w := do(s, r.method, r.target, r.body, bearer(token)); w.Code != http.StatusNotFound {
			t.Errorf("%s %s: %d", r.method, r.target, w.Code)
		}
	}
}

func TestMuteKeywords(t *testing.T) {
	s, c := newServer(t, "")
	ctx := context.Background()
	src, err := s.store.CreateSource(ctx, store.NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "", c.t)
	if err != nil {
		t.Fatal(err)
	}
	items := []store.FeedEntry{
		{GUID: "1", Title: "Go release notes"},
		{GUID: "2", Title: "Weekly digest", Summary: "Sponsored by ACME"},
		{GUID: "3", Title: "Rust news"},
		{GUID: "4", Title: "Weekly sponsored post", Summary: "Thanks to Acme"},
	}
	if _, err := s.store.SaveFeed(ctx, store.FeedSource{ID: src.ID, Kind: "article"}, "", "", items, "", time.Time{}, c.t); err != nil {
		t.Fatal(err)
	}
	source := "/api/sources/" + strconv.FormatInt(src.ID, 10)
	shown := func(target string) []string {
		t.Helper()
		got := titles(t, decode(t, do(s, "GET", target, "", nil)))
		slices.Sort(got)
		return got
	}
	unread := func() float64 {
		t.Helper()
		return decode(t, do(s, "GET", source, "", nil))["unreadCount"].(float64)
	}
	idOf := func(title string) string {
		t.Helper()
		for _, item := range decode(t, do(s, "GET", "/api/entries", "", nil))["entries"].([]any) {
			if entry := item.(map[string]any); entry["title"] == title {
				return strconv.Itoa(int(entry["id"].(float64)))
			}
		}
		t.Fatalf("no entry %q", title)
		return ""
	}
	digest, rust := idOf("Weekly digest"), idOf("Rust news")
	if w := do(s, "PATCH", "/api/entries/"+digest, `{"starred":true}`, nil); w.Code != http.StatusOK {
		t.Fatalf("star: %d", w.Code)
	}

	// Keywords match the title or the summary, any case, one per line; starred entries stay.
	if w := do(s, "PATCH", source, `{"muteKeywords":" acme \n\n RUST "}`, nil); w.Code != http.StatusOK || decode(t, w)["muteKeywords"] != "acme\nRUST" {
		t.Fatalf("mute: %d %s", w.Code, w.Body)
	}
	if got := shown("/api/entries"); fmt.Sprint(got) != "[Go release notes Weekly digest]" {
		t.Fatalf("muted timeline: %v", got)
	}
	if got := shown("/api/entries?q=news"); len(got) != 0 {
		t.Fatalf("muted search: %v", got)
	}
	if got := decode(t, do(s, "GET", "/api/entries/"+rust, "", nil)); got["title"] != "Rust news" {
		t.Fatalf("muted entry by id: %v", got)
	}
	if n := unread(); n != 2 {
		t.Fatalf("muted unreadCount = %v", n)
	}
	if w := do(s, "POST", "/api/entries/read", "", nil); decode(t, w)["marked"] != 2.0 {
		t.Fatalf("mark read: %s", w.Body)
	}

	// A keyword that matches nothing hides nothing.
	if w := do(s, "PATCH", source, `{"muteKeywords":"python"}`, nil); w.Code != http.StatusOK {
		t.Fatalf("change: %d", w.Code)
	}
	if got := shown("/api/entries?unread=1"); fmt.Sprint(got) != "[Rust news Weekly sponsored post]" {
		t.Fatalf("not marked while muted: %v", got)
	}
	if n := unread(); n != 2 {
		t.Fatalf("unreadCount = %v", n)
	}

	// Clearing the keywords brings every entry back; muting deleted none.
	do(s, "PATCH", source, `{"muteKeywords":""}`, nil)
	if got := shown("/api/entries"); len(got) != 4 {
		t.Fatalf("cleared: %v", got)
	}
}

func TestEntryLanguageField(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	ctx := context.Background()
	src, err := s.store.CreateSource(ctx, store.NewSource{URL: "https://example.org/lang", Kind: "article", FetchInterval: time.Hour}, "", c.t)
	if err != nil {
		t.Fatal(err)
	}
	items := []store.FeedEntry{{GUID: "a", Title: "Known", Language: "zh-Hant"}, {GUID: "b", Title: "Unknown"}}
	if _, err := s.store.SaveFeed(ctx, store.FeedSource{ID: src.ID, Kind: "article"}, "", "", items, "", time.Time{}, c.t); err != nil {
		t.Fatal(err)
	}
	list := decode(t, do(s, "GET", "/api/entries?source="+strconv.FormatInt(src.ID, 10), "", bearer(token)))["entries"].([]any)
	got := map[string]any{}
	var id string
	for _, item := range list {
		m := item.(map[string]any)
		got[m["title"].(string)] = m["language"]
		if m["title"] == "Known" {
			id = strconv.Itoa(int(m["id"].(float64)))
		}
	}
	if got["Known"] != "zh-Hant" || got["Unknown"] != "" {
		t.Fatalf("languages: %v", got)
	}
	if one := decode(t, do(s, "GET", "/api/entries/"+id, "", bearer(token))); one["language"] != "zh-Hant" {
		t.Fatalf("entry: %v", one)
	}
}

func TestReadingTime(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	ctx := context.Background()
	src, err := s.store.CreateSource(ctx, store.NewSource{URL: "https://example.org/read", Kind: "article", FetchInterval: time.Hour}, "", c.t)
	if err != nil {
		t.Fatal(err)
	}
	items := []store.FeedEntry{
		{GUID: "long", Title: "Long", ContentHTML: strings.Repeat("字", 4400)},
		{GUID: "short", Title: "Short", ContentHTML: strings.Repeat("字", 800)},
		{GUID: "empty", Title: "Empty"},
	}
	if _, err := s.store.SaveFeed(ctx, store.FeedSource{ID: src.ID, Kind: "article"}, "", "", items, "", time.Time{}, c.t); err != nil {
		t.Fatal(err)
	}
	list := func(query string) map[string]any {
		out := map[string]any{}
		for _, item := range decode(t, do(s, "GET", "/api/entries"+query, "", bearer(token)))["entries"].([]any) {
			m := item.(map[string]any)
			sec, present := m["readingSec"]
			if !present {
				t.Fatalf("%s has no readingSec key", m["title"])
			}
			out[m["title"].(string)] = sec
		}
		return out
	}
	all := list("")
	if len(all) != 3 || all["Long"] != 660.0 || all["Short"] != 120.0 || all["Empty"] != nil {
		t.Fatalf("readingSec: %v", all)
	}
	if got := list("?maxMinutes=10"); len(got) != 1 || got["Short"] != 120.0 {
		t.Fatalf("maxMinutes=10: %v", got)
	}
	if got := list("?maxMinutes=11"); len(got) != 2 || got["Long"] != 660.0 {
		t.Fatalf("maxMinutes=11: %v", got)
	}
	if got := list("?maxMinutes=1440&unread=1&kind=video"); len(got) != 0 {
		t.Fatalf("stacked filters: %v", got)
	}
	id := strconv.Itoa(int(decode(t, do(s, "GET", "/api/entries?q=Short", "", bearer(token)))["entries"].([]any)[0].(map[string]any)["id"].(float64)))
	if one := decode(t, do(s, "GET", "/api/entries/"+id, "", bearer(token))); one["readingSec"] != 120.0 {
		t.Fatalf("entry: %v", one)
	}
}

func TestListCarriesOkAISummary(t *testing.T) {
	s, c := newServer(t, "")
	seed(t, s, "article", 6, c.t)
	ctx := context.Background()
	body := decode(t, do(s, "GET", "/api/entries", "", nil))
	ids := map[string]int64{}
	for _, item := range body["entries"].([]any) {
		entry := item.(map[string]any)
		ids[entry["title"].(string)] = int64(entry["id"].(float64))
	}
	save := func(title, task, text, code string) {
		t.Helper()
		id := ids[title]
		if _, err := s.store.RequestAI(ctx, id, task, c.t); err != nil {
			t.Fatal(err)
		}
		if err := s.store.SaveAI(ctx, id, task, text, "m", "f", code, c.t); err != nil {
			t.Fatal(err)
		}
	}
	long := strings.Repeat("word ", 100)
	save("article 0", store.TaskSummary, "  Short\n\nsummary.  ", "")
	save("article 1", store.TaskSummary, long, "")
	save("article 2", store.TaskSummary, strings.Repeat("字", 400), "")
	save("article 3", store.TaskSummary, "", "ai_upstream")
	save("article 4", store.TaskTranslation, "Only a translation.", "")
	save("article 5", store.TaskSummary, "Old summary.", "")
	if _, err := s.store.RequestAI(ctx, ids["article 5"], store.TaskSummary, c.t); err != nil {
		t.Fatal(err)
	}

	got := map[string]any{}
	for _, item := range decode(t, do(s, "GET", "/api/entries", "", nil))["entries"].([]any) {
		entry := item.(map[string]any)
		if text, ok := entry["aiSummary"]; ok {
			got[entry["title"].(string)] = text
		}
	}
	want := map[string]any{
		"article 0": "Short summary.",
		"article 1": strings.TrimSpace(strings.Repeat("word ", 60)) + "…",
		"article 2": strings.Repeat("字", maxListAISummary) + "…",
	}
	if len(got) != len(want) {
		t.Fatalf("aiSummary on %v, want %v", got, want)
	}
	for title, text := range want {
		if got[title] != text {
			t.Errorf("%s: aiSummary %q, want %q", title, got[title], text)
		}
	}

	one := decode(t, do(s, "GET", "/api/entries/"+strconv.FormatInt(ids["article 0"], 10), "", nil))
	if _, ok := one["aiSummary"]; ok {
		t.Errorf("a single entry carries aiSummary: %v", one)
	}
}
