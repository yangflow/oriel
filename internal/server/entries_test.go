package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
		"source=x":        "invalid_source",
		"source=0":        "invalid_source",
		"kind=podcast":    "invalid_kind",
		"limit=0":         "invalid_limit",
		"limit=101":       "invalid_limit",
		"cursor=abc":      "invalid_cursor",
		"cursor=1.0":      "invalid_cursor",
		"order=up":        "invalid_order",
		"order=ASC":       "invalid_order",
		"maxMinutes=0":    "invalid_max_minutes",
		"maxMinutes=1441": "invalid_max_minutes",
		"maxMinutes=x":    "invalid_max_minutes",
		"maxMinutes=2.5":  "invalid_max_minutes",
		"since=0":         "invalid_since",
		"since=-5":        "invalid_since",
		"since=x":         "invalid_since",
		"since=1.5":       "invalid_since",
	} {
		w := do(s, "GET", "/api/entries?"+query, "", nil)
		if w.Code != http.StatusBadRequest || decode(t, w)["error"] != code {
			t.Errorf("%s: %d %s, want %s", query, w.Code, w.Body, code)
		}
	}
}

func TestTimelineOrder(t *testing.T) {
	s, c := newServer(t, "")
	seed(t, s, "article", 3, c.t)
	seed(t, s, "video", 2, c.t.Add(time.Minute))

	// Oldest first pages through the same entries in the opposite order, and new entries arrive at the end.
	var got []string
	target := "/api/entries?order=asc&limit=2"
	for pages := 0; ; pages++ {
		body := decode(t, do(s, "GET", target, "", nil))
		got = append(got, titles(t, body)...)
		if pages == 0 {
			seed(t, s, "social", 1, c.t.Add(time.Hour))
		}
		if body["nextCursor"] == nil {
			break
		}
		target = "/api/entries?order=asc&limit=2&cursor=" + body["nextCursor"].(string)
	}
	if fmt.Sprint(got) != "[article 0 article 1 article 2 video 0 video 1 social 0]" {
		t.Fatalf("oldest first: %v", got)
	}
	if got := titles(t, decode(t, do(s, "GET", "/api/entries?order=desc&limit=2", "", nil))); fmt.Sprint(got) != "[social 0 video 1]" {
		t.Fatalf("explicit newest first: %v", got)
	}
	// The cursor of an oldest-first page leads on to newer entries, and the filters stay.
	first := decode(t, do(s, "GET", "/api/entries?order=asc&kind=article&limit=1", "", nil))
	next := decode(t, do(s, "GET", "/api/entries?order=asc&kind=article&limit=5&cursor="+first["nextCursor"].(string), "", nil))
	if fmt.Sprint(titles(t, next)) != "[article 1 article 2]" || next["nextCursor"] != nil {
		t.Fatalf("after cursor: %v", next)
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
	readAt, ok := marked["readAt"].(float64)
	if !ok || readAt != float64(c.t.UnixMilli()) {
		t.Fatalf("readAt: %v", marked["readAt"])
	}
	// The first entry is unread and read again later by another action: the undo leaves it alone.
	firstID := int(marked["ids"].([]any)[0].(float64))
	c.t = c.t.Add(time.Second)
	do(s, "PATCH", fmt.Sprintf("/api/entries/%d", firstID), `{"isRead":false}`, bearer(token))
	do(s, "PATCH", fmt.Sprintf("/api/entries/%d", firstID), `{"isRead":true}`, bearer(token))
	undo, _ := json.Marshal(map[string]any{"ids": marked["ids"], "readAt": readAt})
	if w := do(s, "POST", "/api/entries/unread", string(undo), bearer(token)); w.Code != http.StatusOK {
		t.Fatalf("undo: %d %s", w.Code, w.Body)
	} else if got := decode(t, w); got["marked"] != 2.0 || slices.Contains(got["ids"].([]any), any(float64(firstID))) {
		t.Fatalf("undo: %v", got)
	}
	undo, _ = json.Marshal(map[string]any{"ids": marked["ids"]})
	if w := do(s, "POST", "/api/entries/unread", string(undo), bearer(token)); w.Code != http.StatusOK || decode(t, w)["marked"] != 1.0 {
		t.Fatalf("undo without a time: %d %s", w.Code, w.Body)
	}
	if got := titles(t, decode(t, do(s, "GET", "/api/entries?unread=1", "", bearer(token)))); len(got) != 5 {
		t.Fatalf("unread after undo: %v", got)
	}
	for _, body := range []string{`{"ids":[]}`, `{}`, `{"ids":"1"}`, `{"ids":[1],"readAt":-1}`} {
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
	// From an entry on: the entry itself and the newer ones, which with upTo bounds the entries below a video.
	for _, id := range []int{oldestVideo, newestVideo} {
		do(s, "PATCH", fmt.Sprintf("/api/entries/%d", id), `{"isRead":false}`, bearer(token))
	}
	w = do(s, "POST", "/api/entries/read", fmt.Sprintf(`{"from":%d,"upTo":%d,"kind":"video"}`, oldestVideo, oldestVideo), bearer(token))
	if got := decode(t, w); w.Code != http.StatusOK || got["marked"] != 1.0 || int(got["ids"].([]any)[0].(float64)) != oldestVideo {
		t.Fatalf("mark from an entry up to it: %d %s", w.Code, w.Body)
	}
	if w := do(s, "POST", "/api/entries/read", fmt.Sprintf(`{"from":%d,"kind":"video"}`, newestVideo), bearer(token)); decode(t, w)["marked"] != 1.0 {
		t.Fatalf("mark from the newest video: %s", w.Body)
	}
	for body, want := range map[string]int{`{"from":999}`: http.StatusNotFound, `{"upTo":999}`: http.StatusNotFound, `{"newerThan":999}`: http.StatusNotFound, `{"kind":"link"}`: http.StatusBadRequest} {
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

func TestMutedCount(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	ctx := context.Background()
	src, err := s.store.CreateSource(ctx, store.NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "", c.t)
	if err != nil {
		t.Fatal(err)
	}
	items := []store.FeedEntry{
		{GUID: "1", Title: "Go release notes"},
		{GUID: "2", Title: "Weekly digest", Summary: "Sponsored by ACME"},
		{GUID: "3", Title: "Rust news"},
	}
	if _, err := s.store.SaveFeed(ctx, store.FeedSource{ID: src.ID, Kind: "article"}, "", "", items, "", time.Time{}, c.t); err != nil {
		t.Fatal(err)
	}
	target := "/api/sources/" + strconv.FormatInt(src.ID, 10) + "/muted-count"
	count := func(body string) float64 {
		t.Helper()
		w := do(s, "POST", target, body, bearer(token))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body)
		}
		return decode(t, w)["count"].(float64)
	}
	if w := do(s, "POST", target, `{"muteKeywords":"rust"}`, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("without a token: %d", w.Code)
	}

	// Keywords that are not saved yet are counted as the timeline would; blank lines and padding do not matter.
	if n := count(`{"muteKeywords":" acme \n\n RUST "}`); n != 2 {
		t.Fatalf("count = %v", n)
	}
	if n := count(`{"muteKeywords":"python"}`); n != 0 {
		t.Fatalf("count = %v", n)
	}
	if n := count(`{"muteKeywords":" \n "}`); n != 0 {
		t.Fatalf("count = %v", n)
	}

	// Starred entries are not hidden, so they are not counted.
	var rust string
	for _, item := range decode(t, do(s, "GET", "/api/entries", "", bearer(token)))["entries"].([]any) {
		if entry := item.(map[string]any); entry["title"] == "Rust news" {
			rust = strconv.Itoa(int(entry["id"].(float64)))
		}
	}
	if w := do(s, "PATCH", "/api/entries/"+rust, `{"starred":true}`, bearer(token)); w.Code != http.StatusOK {
		t.Fatalf("star: %d", w.Code)
	}
	if n := count(`{"muteKeywords":"acme\nrust"}`); n != 1 {
		t.Fatalf("count with a starred entry = %v", n)
	}

	// Asking saves nothing.
	got := decode(t, do(s, "GET", "/api/sources/"+strconv.FormatInt(src.ID, 10), "", bearer(token)))
	if got["muteKeywords"] != "" {
		t.Fatalf("saved keywords: %v", got["muteKeywords"])
	}
	if w := do(s, "POST", "/api/sources/999/muted-count", `{"muteKeywords":"rust"}`, bearer(token)); w.Code != http.StatusNotFound || decode(t, w)["error"] != "not_found" {
		t.Fatalf("unknown source: %d %s", w.Code, w.Body)
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

func TestRetryExtractionAPI(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "test")
	seed(t, s, "article", 2, time.UnixMilli(1_700_000_000_000))
	list := decode(t, do(s, "GET", "/api/entries?kind=article", "", bearer(token)))["entries"].([]any)
	failed := strconv.Itoa(int(list[0].(map[string]any)["id"].(float64)))
	waiting := strconv.Itoa(int(list[1].(map[string]any)["id"].(float64)))
	id, _ := strconv.ParseInt(failed, 10, 64)
	if err := s.store.SaveExtraction(context.Background(), id, "", "", ""); err != nil {
		t.Fatal(err)
	}

	if w := do(s, "POST", "/api/entries/"+failed+"/extract", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("without a token: %d", w.Code)
	}
	w := do(s, "POST", "/api/entries/"+failed+"/extract", "", bearer(token))
	if body := decode(t, w); w.Code != http.StatusAccepted || body["extractStatus"] != "pending" || body["contentHtml"] != "<p>body</p>" {
		t.Fatalf("retry: %d %v", w.Code, body)
	}
	if got := decode(t, do(s, "GET", "/api/entries/"+failed, "", bearer(token))); got["extractStatus"] != "pending" {
		t.Fatalf("stored: %v", got)
	}
	if w := do(s, "POST", "/api/entries/"+waiting+"/extract", "", bearer(token)); w.Code != http.StatusAccepted {
		t.Fatalf("pending: %d", w.Code)
	}
	if w := do(s, "POST", "/api/entries/999/extract", "", bearer(token)); w.Code != http.StatusNotFound {
		t.Fatalf("missing: %d", w.Code)
	}
	if err := s.store.SaveExtraction(context.Background(), id, "<p>x</p>", "x", ""); err != nil {
		t.Fatal(err)
	}
	w = do(s, "POST", "/api/entries/"+failed+"/extract", "", bearer(token))
	if w.Code != http.StatusConflict || decode(t, w)["error"] != "not_retryable" {
		t.Fatalf("done: %d %s", w.Code, w.Body)
	}
}

func TestMarkReadOlderThan(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	article := seed(t, s, "article", 3, c.t)
	video := seed(t, s, "video", 2, c.t.Add(time.Hour))
	cut := func(d time.Duration) string { return strconv.FormatInt(c.t.Add(d).UnixMilli(), 10) }
	mark := func(body string) map[string]any {
		t.Helper()
		w := do(s, "POST", "/api/entries/read", body, bearer(token))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body)
		}
		return decode(t, w)
	}

	if w := do(s, "POST", "/api/entries/read", `{"olderThan":1}`, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("without session: %d", w.Code)
	}
	// The cut is strict: entries shown exactly at it stay unread.
	if got := mark(`{"olderThan":` + cut(0) + `}`); got["marked"] != 0.0 {
		t.Fatalf("nothing is older than the oldest: %v", got)
	}
	// The video is newer than the cut, so the scope narrows nothing it does not already exclude.
	if got := mark(fmt.Sprintf(`{"olderThan":%s,"kind":"video"}`, cut(30*time.Minute))); got["marked"] != 0.0 {
		t.Fatalf("kind scope: %v", got)
	}
	if got := mark(fmt.Sprintf(`{"olderThan":%s,"sourceId":%d}`, cut(2*time.Hour), video)); got["marked"] != 2.0 {
		t.Fatalf("source scope: %v", got)
	}
	if got := titles(t, decode(t, do(s, "GET", "/api/entries?unread=1", "", bearer(token)))); len(got) != 3 {
		t.Fatalf("unread after source scope: %v", got)
	}
	got := mark(`{"olderThan":` + cut(30*time.Minute) + `}`)
	ids := got["ids"].([]any)
	if got["marked"] != 3.0 || len(ids) != 3 {
		t.Fatalf("all scope: %v", got)
	}
	undo, _ := json.Marshal(map[string]any{"ids": ids})
	if w := do(s, "POST", "/api/entries/unread", string(undo), bearer(token)); decode(t, w)["marked"] != 3.0 {
		t.Fatalf("undo: %s", w.Body)
	}
	if got := mark(fmt.Sprintf(`{"olderThan":%s,"sourceId":%d,"kind":"article"}`, cut(30*time.Minute), article)); got["marked"] != 3.0 {
		t.Fatalf("source and kind: %v", got)
	}

	for _, bad := range []string{`{"olderThan":0}`, `{"olderThan":-5}`, `{"olderThan":"yesterday"}`, `{"olderThan":1.5}`} {
		w := do(s, "POST", "/api/entries/read", bad, bearer(token))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}
	if w := do(s, "POST", "/api/entries/read", `{"olderThan":0}`, bearer(token)); decode(t, w)["error"] != "invalid_older_than" {
		t.Errorf("error code: %s", w.Body)
	}
}

func TestTimeRange(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	seed(t, s, "article", 3, c.t)
	video := seed(t, s, "video", 2, c.t.Add(time.Hour))
	since := func(d time.Duration) string { return strconv.FormatInt(c.t.Add(d).UnixMilli(), 10) }
	list := func(query string) []string {
		t.Helper()
		w := do(s, "GET", "/api/entries?"+query, "", bearer(token))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", query, w.Code, w.Body)
		}
		return titles(t, decode(t, w))
	}
	// The bound is inclusive: an entry that arrived exactly at it is in the range.
	if got := list("since=" + since(0)); len(got) != 5 {
		t.Fatalf("since the first arrival: %v", got)
	}
	if got := list("since=" + since(30*time.Minute)); fmt.Sprint(got) != "[video 1 video 0]" {
		t.Fatalf("since: %v", got)
	}
	if got := list("since=" + since(30*time.Minute) + "&kind=article"); len(got) != 0 {
		t.Fatalf("since and kind: %v", got)
	}
	if got := list("since=" + since(2*time.Hour)); len(got) != 0 {
		t.Fatalf("empty range: %v", got)
	}

	mark := func(body string) map[string]any {
		t.Helper()
		w := do(s, "POST", "/api/entries/read", body, bearer(token))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body)
		}
		return decode(t, w)
	}
	if got := mark(`{"since":` + since(30*time.Minute) + `}`); got["marked"] != 2.0 {
		t.Fatalf("mark since: %v", got)
	}
	if got := list("unread=1"); len(got) != 3 {
		t.Fatalf("older entries stay unread: %v", got)
	}
	if got := mark(fmt.Sprintf(`{"since":%s,"sourceId":%d}`, since(0), video)); got["marked"] != 0.0 {
		t.Fatalf("mark since and source: %v", got)
	}
	for _, bad := range []string{`{"since":0}`, `{"since":-5}`, `{"since":"today"}`, `{"since":1.5}`} {
		if w := do(s, "POST", "/api/entries/read", bad, bearer(token)); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}
	if w := do(s, "POST", "/api/entries/read", `{"since":0}`, bearer(token)); decode(t, w)["error"] != "invalid_since" {
		t.Errorf("error code: %s", w.Body)
	}
}

// TestShortSearch answers a query of one or two characters by LIKE over the title and the summary: any case, the
// characters %, _ and \ taken as themselves, the same filters and mute rules as the index, and at most 200 entries
// in one reply marked limited.
func TestShortSearch(t *testing.T) {
	s, c := newServer(t, "")
	ctx := context.Background()
	src, err := s.store.CreateSource(ctx, store.NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "", c.t)
	if err != nil {
		t.Fatal(err)
	}
	items := []store.FeedEntry{
		{GUID: "1", Title: "苹果发布新款芯片"},
		{GUID: "2", Title: "Weekly", Summary: "芯片短缺"},
		{GUID: "3", Title: "100% done"},
		{GUID: "4", Title: "snake_case"},
		{GUID: "5", Title: `back\slash`},
		{GUID: "6", Title: "Go release"},
		{GUID: "7", Title: "Nothing here"},
		{GUID: "8", Title: "梨子"},
		{GUID: "9", Title: "Muted GO post", Summary: "spam"},
		{GUID: "10", Title: "party 🎉"},
		{GUID: "11", Title: "Tab中文"},
	}
	if _, err := s.store.SaveFeed(ctx, store.FeedSource{ID: src.ID, Kind: "article"}, "", "", items, "", time.Time{}, c.t); err != nil {
		t.Fatal(err)
	}
	if w := do(s, "PATCH", "/api/sources/"+strconv.FormatInt(src.ID, 10), `{"muteKeywords":"spam"}`, nil); w.Code != http.StatusOK {
		t.Fatalf("mute: %d %s", w.Code, w.Body)
	}
	search := func(q string) (map[string]any, []string) {
		t.Helper()
		w := do(s, "GET", "/api/entries?q="+url.QueryEscape(q), "", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%q: %d %s", q, w.Code, w.Body)
		}
		body := decode(t, w)
		got := titles(t, body)
		slices.Sort(got)
		return body, got
	}
	for q, want := range map[string]string{
		"芯片":  "[Weekly 苹果发布新款芯片]", // title and summary
		"苹":   "[苹果发布新款芯片]",
		"go":  "[Go release]", // any case; the muted entry stays out
		"GO":  "[Go release]",
		"%":   "[100% done]",
		"_":   "[snake_case]",
		`\`:   `[back\slash]`,
		"x_":  "[]", // _ is not "any character"
		"%e":  "[]", // nor is %
		"zz":  "[]",
		"梨子":  "[梨子]",
		"🎉":   "[party 🎉]", // one character of four bytes
		"b中":  "[Tab中文]",   // mixed, two characters
		"中文":  "[Tab中文]",   // six bytes, two characters
		" 梨 ": "[梨子]",      // trimmed like any query
	} {
		body, got := search(q)
		if fmt.Sprint(got) != want || body["limited"] != true || body["nextCursor"] != nil {
			t.Errorf("%q: %v, limited %v, next %v; want %s", q, got, body["limited"], body["nextCursor"], want)
		}
	}
	// Three characters still go through the index and are not marked.
	if body, got := search("b中文"); fmt.Sprint(got) != "[Tab中文]" || body["limited"] != nil {
		t.Errorf("three characters: %v, limited %v", got, body["limited"])
	}
	if body, got := search("Weekly"); fmt.Sprint(got) != "[Weekly]" || body["limited"] != nil {
		t.Errorf("indexed: %v, limited %v", got, body["limited"])
	}

	// The other filters hold: unread, kind.
	id := decode(t, do(s, "GET", "/api/entries?q=%E8%8B%B9", "", nil))["entries"].([]any)[0].(map[string]any)["id"].(float64)
	if w := do(s, "PATCH", "/api/entries/"+strconv.Itoa(int(id)), `{"isRead":true}`, nil); w.Code != http.StatusOK {
		t.Fatalf("read: %d", w.Code)
	}
	if got := titles(t, decode(t, do(s, "GET", "/api/entries?unread=1&q=%E8%8B%B9", "", nil))); len(got) != 0 {
		t.Errorf("unread: %v", got)
	}
	if got := titles(t, decode(t, do(s, "GET", "/api/entries?kind=video&q=%E8%8B%B9", "", nil))); len(got) != 0 {
		t.Errorf("kind: %v", got)
	}
}

// TestShortSearchLimit stops a short search at 200 entries, newest first or oldest first, in one reply.
func TestShortSearchLimit(t *testing.T) {
	s, c := newServer(t, "")
	ctx := context.Background()
	src, err := s.store.CreateSource(ctx, store.NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "", c.t)
	if err != nil {
		t.Fatal(err)
	}
	var items []store.FeedEntry
	for i := range 230 {
		items = append(items, store.FeedEntry{GUID: strconv.Itoa(i), Title: fmt.Sprintf("苹果 %03d", i)})
	}
	if _, err := s.store.SaveFeed(ctx, store.FeedSource{ID: src.ID, Kind: "article"}, "", "", items, "", time.Time{}, c.t); err != nil {
		t.Fatal(err)
	}
	for _, order := range []string{"desc", "asc"} {
		// limit does not matter: a short search is one reply.
		body := decode(t, do(s, "GET", "/api/entries?limit=5&order="+order+"&q=%E8%8B%B9", "", nil))
		got := titles(t, body)
		if len(got) != 200 || body["limited"] != true || body["nextCursor"] != nil {
			t.Fatalf("%s: %d entries, limited %v, next %v", order, len(got), body["limited"], body["nextCursor"])
		}
		if want := map[string]string{"desc": "苹果 000", "asc": "苹果 229"}[order]; got[0] != want {
			t.Errorf("%s: starts with %q, want %q", order, got[0], want)
		}
	}
}

// TestSearchSyntax checks the operators over the API: an operators-only search pages like the timeline and is not
// limited, a short term makes the reply limited, and after: and before: begin their days in the zone of the tz
// parameter, falling back to the zone of the server, with no error, when it is missing, unknown or Local.
func TestSearchSyntax(t *testing.T) {
	s, c := newServer(t, "")
	ctx := context.Background()
	blog, _ := s.store.CreateSource(ctx, store.NewSource{URL: "https://example.org/blog", Kind: "article", FetchInterval: time.Hour}, "Blog", c.t)
	tube, _ := s.store.CreateSource(ctx, store.NewSource{URL: "https://example.org/tube", Kind: "video", FetchInterval: time.Hour}, "Tube", c.t)
	at := func(day, hour int) time.Time { return time.Date(2026, 2, day, hour, 0, 0, 0, time.UTC) }
	for src, items := range map[store.Source][]store.FeedEntry{
		blog: {{GUID: "1", Title: "Early post", PublishedAt: at(10, 1)}, {GUID: "2", Title: "Late post", PublishedAt: at(10, 20)}, {GUID: "3", Title: "Third post", PublishedAt: at(11, 5)}},
		tube: {{GUID: "4", Title: "A clip", PublishedAt: at(10, 12)}},
	} {
		if _, err := s.store.SaveFeed(ctx, store.FeedSource{ID: src.ID, Kind: src.Kind}, "", "", items, "", time.Time{}, c.t); err != nil {
			t.Fatal(err)
		}
	}
	get := func(target string) (map[string]any, []string) {
		t.Helper()
		w := do(s, "GET", target, "", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", target, w.Code, w.Body)
		}
		body := decode(t, w)
		got := titles(t, body)
		slices.Sort(got)
		return body, got
	}
	q := func(query, extra string) string { return "/api/entries?q=" + url.QueryEscape(query) + extra }

	body, got := get(q("kind:article -late", "&limit=1"))
	if len(got) != 1 || body["nextCursor"] == nil || body["limited"] != nil {
		t.Errorf("operators only: %v, next %v, limited %v", got, body["nextCursor"], body["limited"])
	}
	if body, got := get(q(`from:blog "post" -ar`, "")); fmt.Sprint(got) != "[Late post Third post]" || body["limited"] != true {
		t.Errorf("short exclusion: %v, limited %v", got, body["limited"])
	}

	// The days are those of the zone asked for; without one they are those of the server, here UTC.
	local := time.Local
	time.Local = time.UTC
	defer func() { time.Local = local }()
	day := q("after:2026-02-10 before:2026-02-11", "")
	for _, c := range []struct{ tz, want string }{
		{"&tz=Asia/Shanghai", "[A clip Early post]"},
		{"&tz=Pacific/Honolulu", "[A clip Late post Third post]"},
		{"", "[A clip Early post Late post]"},
		{"&tz=Local", "[A clip Early post Late post]"},
		{"&tz=Not/AZone", "[A clip Early post Late post]"},
		{"&tz=..%2F..%2Fetc%2Fpasswd", "[A clip Early post Late post]"},
	} {
		if _, got := get(day + c.tz); fmt.Sprint(got) != c.want {
			t.Errorf("tz %q: %v, want %s", c.tz, got, c.want)
		}
	}
}
