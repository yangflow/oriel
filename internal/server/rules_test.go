package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

func TestRulesAPI(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	src, err := s.store.CreateSource(context.Background(), store.NewSource{URL: "https://example.org/r", Kind: "article", FetchInterval: time.Hour}, "", c.t)
	if err != nil {
		t.Fatal(err)
	}
	sid := strconv.FormatInt(src.ID, 10)

	for _, r := range []struct{ method, target string }{{"GET", "/api/rules"}, {"POST", "/api/rules"}, {"PATCH", "/api/rules/1"}, {"DELETE", "/api/rules/1"}, {"POST", "/api/muted-count"}, {"POST", "/api/rules/match-count"}} {
		if w := do(s, r.method, r.target, "{}", nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without a session: %d", r.method, r.target, w.Code)
		}
	}

	w := do(s, "GET", "/api/rules", "", bearer(token))
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"rules":[]}` {
		t.Fatalf("empty list: %d %s", w.Code, w.Body)
	}

	w = do(s, "POST", "/api/rules", `{"sourceId":`+sid+`,"kind":"article","keywords":" go \n\nrust","author":" Ann ","star":true}`, bearer(token))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	rule := decode(t, w)
	if rule["keywords"] != "go\nrust" || rule["author"] != "Ann" || rule["enabled"] != true || rule["markRead"] != false ||
		rule["star"] != true || fmt.Sprint(rule["sourceIds"]) != fmt.Sprint([]any{float64(src.ID)}) || rule["createdAt"] != float64(c.t.UnixMilli()) {
		t.Fatalf("created rule: %v", rule)
	}
	id := strconv.Itoa(int(rule["id"].(float64)))

	w = do(s, "PATCH", "/api/rules/"+id, `{"enabled":false,"markRead":true}`, bearer(token))
	got := decode(t, w)
	if w.Code != http.StatusOK || got["enabled"] != false || got["markRead"] != true || got["keywords"] != "go\nrust" || fmt.Sprint(got["sourceIds"]) != fmt.Sprint([]any{float64(src.ID)}) {
		t.Fatalf("patch: %d %v", w.Code, got)
	}
	w = do(s, "PATCH", "/api/rules/"+id, `{"sourceId":null}`, bearer(token))
	if got := decode(t, w); w.Code != http.StatusOK || fmt.Sprint(got["sourceIds"]) != "[]" {
		t.Fatalf("clear the source the old way: %d %v", w.Code, got)
	}

	list := decode(t, do(s, "GET", "/api/rules", "", bearer(token)))["rules"].([]any)
	if len(list) != 1 {
		t.Fatalf("list: %v", list)
	}

	for _, c := range []struct {
		method, target, body string
		status               int
		code                 string
	}{
		{"POST", "/api/rules", `{"sourceId":9999,"star":true}`, 400, "invalid_rule_source"},
		{"POST", "/api/rules", `{"kind":"podcast","keywords":"x","star":true}`, 400, "invalid_rule_kind"},
		{"POST", "/api/rules", `{"keywords":"` + strings.Repeat("x", 101) + `","star":true}`, 400, "invalid_rule_keywords"},
		{"POST", "/api/rules", `{"author":"` + strings.Repeat("x", 101) + `","star":true}`, 400, "invalid_rule_author"},
		{"POST", "/api/rules", `{"star":true}`, 400, "rule_no_condition"},
		{"POST", "/api/rules", `{"keywords":"x"}`, 400, "rule_no_action"},
		{"POST", "/api/rules", `{"keywords":"x","star":true,"extra":1}`, 400, "invalid_body"},
		{"PATCH", "/api/rules/" + id, `{"sourceId":"x"}`, 400, "invalid_body"},
		{"PATCH", "/api/rules/" + id, `{"sourceIds":"x"}`, 400, "invalid_body"},
		{"POST", "/api/rules", `{"sourceIds":[9999],"star":true}`, 400, "invalid_rule_source"},
		{"POST", "/api/rules", `{"excludeKeywords":"x","star":true}`, 400, "rule_no_condition"},
		{"POST", "/api/rules", `{"keywords":"x","excludeKeywords":"` + strings.Repeat("x", 101) + `","star":true}`, 400, "invalid_rule_keywords"},
		{"POST", "/api/rules", `{"sourceIds":` + idList(store.MaxRuleSources+1) + `,"star":true}`, 400, "too_many_rule_sources"},
		{"POST", "/api/rules/match-count", `{"kind":"podcast"}`, 400, "invalid_rule_kind"},
		{"POST", "/api/rules/match-count", `{"sourceIds":[9999]}`, 400, "invalid_rule_source"},
		{"PATCH", "/api/rules/" + id, `{"star":false,"markRead":false}`, 400, "rule_no_action"},
		{"PATCH", "/api/rules/" + id, `{"keywords":"","author":"","kind":""}`, 400, "rule_no_condition"},
		{"PATCH", "/api/rules/9999", `{"star":true}`, 404, "not_found"},
		{"DELETE", "/api/rules/9999", "", 404, "not_found"},
	} {
		w := do(s, c.method, c.target, c.body, bearer(token))
		if w.Code != c.status || decode(t, w)["error"] != c.code {
			t.Errorf("%s %s %s: %d %s, want %s", c.method, c.target, c.body, w.Code, w.Body, c.code)
		}
	}

	if w := do(s, "DELETE", "/api/rules/"+id, "", bearer(token)); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if list := decode(t, do(s, "GET", "/api/rules", "", bearer(token)))["rules"].([]any); len(list) != 0 {
		t.Fatalf("after delete: %v", list)
	}

	for i := 0; i < store.MaxRules; i++ {
		if w := do(s, "POST", "/api/rules", `{"keywords":"k`+strconv.Itoa(i)+`","star":true}`, bearer(token)); w.Code != http.StatusCreated {
			t.Fatalf("rule %d: %d %s", i, w.Code, w.Body)
		}
	}
	w = do(s, "POST", "/api/rules", `{"keywords":"more","star":true}`, bearer(token))
	if w.Code != http.StatusConflict || decode(t, w)["error"] != "too_many_rules" {
		t.Fatalf("the 51st rule: %d %s", w.Code, w.Body)
	}
}

func TestGlobalMuteKeywordsAPI(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	ctx := context.Background()
	src, err := s.store.CreateSource(ctx, store.NewSource{URL: "https://example.org/m", Kind: "article", FetchInterval: time.Hour}, "", c.t)
	if err != nil {
		t.Fatal(err)
	}
	items := []store.FeedEntry{{GUID: "1", Title: "Sponsored"}, {GUID: "2", Title: "Plain"}}
	if _, err := s.store.SaveFeed(ctx, store.FeedSource{ID: src.ID, Kind: "article"}, "", "", items, "", time.Time{}, c.t); err != nil {
		t.Fatal(err)
	}

	if got := decode(t, do(s, "GET", "/api/settings", "", bearer(token)))["muteKeywords"]; got != "" {
		t.Fatalf("default: %v", got)
	}
	w := do(s, "POST", "/api/muted-count", `{"muteKeywords":" sponsored \n\n"}`, bearer(token))
	if got := decode(t, w); w.Code != http.StatusOK || got["count"] != float64(1) {
		t.Fatalf("count: %d %v", w.Code, got)
	}
	if got := decode(t, do(s, "GET", "/api/settings", "", bearer(token)))["muteKeywords"]; got != "" {
		t.Fatalf("asking saved the list: %v", got)
	}

	w = do(s, "PATCH", "/api/settings", `{"muteKeywords":" sponsored \n\n ad "}`, bearer(token))
	if w.Code != http.StatusOK || decode(t, w)["muteKeywords"] != "sponsored\nad" {
		t.Fatalf("patch: %d %s", w.Code, w.Body)
	}
	if got := decode(t, do(s, "GET", "/api/settings", "", bearer(token)))["muteKeywords"]; got != "sponsored\nad" {
		t.Fatalf("round trip: %v", got)
	}
	if w := do(s, "GET", "/api/entries", "", bearer(token)); strings.Contains(w.Body.String(), "Sponsored") {
		t.Fatalf("the muted entry is listed: %s", w.Body)
	}

	tooLong := `{"muteKeywords":"` + strings.Repeat("x", 101) + `"}`
	tooMany := `{"muteKeywords":"` + strings.Repeat("k\\n", 201) + `"}`
	for _, body := range []string{tooLong, tooMany} {
		for _, target := range []struct{ method, path string }{{"PATCH", "/api/settings"}, {"POST", "/api/muted-count"}} {
			w := do(s, target.method, target.path, body, bearer(token))
			if w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_mute_keywords" {
				t.Errorf("%s: %d %s", target.path, w.Code, w.Body)
			}
		}
	}

	if w := do(s, "PATCH", "/api/settings", `{"muteKeywords":""}`, bearer(token)); w.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", w.Code, w.Body)
	}
	if w := do(s, "GET", "/api/entries", "", bearer(token)); !strings.Contains(w.Body.String(), "Sponsored") {
		t.Fatalf("the entry stays hidden: %s", w.Body)
	}
}

func TestRulesRestoreAndSourceUndoWindow(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	ctx := context.Background()
	src, err := s.store.CreateSource(ctx, store.NewSource{URL: "https://example.org/u", Kind: "article", FetchInterval: time.Hour}, "", c.t)
	if err != nil {
		t.Fatal(err)
	}
	sid := strconv.FormatInt(src.ID, 10)
	var ids []string
	for _, k := range []string{"a", "b", "c"} {
		w := do(s, "POST", "/api/rules", `{"sourceId":`+sid+`,"keywords":"`+k+`","star":true}`, bearer(token))
		ids = append(ids, strconv.Itoa(int(decode(t, w)["id"].(float64))))
		c.t = c.t.Add(time.Second)
	}
	mid := decode(t, do(s, "GET", "/api/rules", "", bearer(token)))["rules"].([]any)[1].(map[string]any)
	if w := do(s, "DELETE", "/api/rules/"+ids[1], "", bearer(token)); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}
	body := `{"id":` + ids[1] + `,"createdAt":` + strconv.FormatInt(int64(mid["createdAt"].(float64)), 10) + `,"sourceId":` + sid + `,"keywords":"b","star":true}`
	if w := do(s, "POST", "/api/rules", body, bearer(token)); w.Code != http.StatusCreated {
		t.Fatalf("restore: %d %s", w.Code, w.Body)
	}
	list := decode(t, do(s, "GET", "/api/rules", "", bearer(token)))["rules"].([]any)
	if len(list) != 3 || list[1].(map[string]any)["keywords"] != "b" || list[1].(map[string]any)["id"] != mid["id"] {
		t.Fatalf("order after the restore: %v", list)
	}

	if err := s.store.DeleteSource(ctx, src.ID, c.t); err != nil {
		t.Fatal(err)
	}
	if w := do(s, "PATCH", "/api/rules/"+ids[0], `{"enabled":false}`, bearer(token)); w.Code != http.StatusOK {
		t.Fatalf("toggle in the undo window: %d %s", w.Code, w.Body)
	}
	do(s, "DELETE", "/api/rules/"+ids[2], "", bearer(token))
	if w := do(s, "POST", "/api/rules", `{"id":`+ids[2]+`,"sourceId":`+sid+`,"keywords":"c","star":true}`, bearer(token)); w.Code != http.StatusCreated {
		t.Fatalf("undo in the undo window: %d %s", w.Code, w.Body)
	}
}

func TestRuleSeveralSourcesExcludeAndCount(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	ctx := context.Background()
	var ids []int64
	for _, name := range []string{"a", "b", "c"} {
		src, err := s.store.CreateSource(ctx, store.NewSource{URL: "https://example.org/" + name, Kind: "article", FetchInterval: time.Hour}, "", c.t)
		if err != nil {
			t.Fatal(err)
		}
		entries := []store.FeedEntry{
			{GUID: "1", Title: "Go weekly", URL: "https://example.org/" + name + "/1", PublishedAt: c.t},
			{GUID: "2", Title: "Go weekly sponsored", URL: "https://example.org/" + name + "/2", PublishedAt: c.t},
		}
		if _, err := s.store.SaveFeed(ctx, store.FeedSource{ID: src.ID, Kind: src.Kind}, "", "", entries, "", time.Time{}, c.t); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, src.ID)
	}
	two := fmt.Sprintf("[%d,%d]", ids[0], ids[1])

	w := do(s, "POST", "/api/rules", `{"sourceIds":`+two+`,"keywords":"go","excludeKeywords":" sponsored ","star":true}`, bearer(token))
	rule := decode(t, w)
	if w.Code != http.StatusCreated || fmt.Sprint(rule["sourceIds"]) != fmt.Sprint([]any{float64(ids[0]), float64(ids[1])}) || rule["excludeKeywords"] != "sponsored" {
		t.Fatalf("create: %d %v", w.Code, rule)
	}
	id := strconv.Itoa(int(rule["id"].(float64)))
	if w := do(s, "PATCH", "/api/rules/"+id, `{"excludeKeywords":"","sourceIds":[]}`, bearer(token)); w.Code != http.StatusOK ||
		decode(t, w)["excludeKeywords"] != "" {
		t.Fatalf("patch: %d %s", w.Code, w.Body)
	}

	for _, c := range []struct{ body, want string }{
		{`{"keywords":"go"}`, `{"count":6}`},
		{`{"keywords":"go","excludeKeywords":"sponsored"}`, `{"count":3}`},
		{`{"sourceIds":` + two + `,"keywords":"go","excludeKeywords":"sponsored"}`, `{"count":2}`},
		{`{"sourceIds":` + two + `}`, `{"count":4}`},
		{`{"excludeKeywords":"sponsored"}`, `{"count":0}`},
		{`{}`, `{"count":0}`},
	} {
		w := do(s, "POST", "/api/rules/match-count", c.body, bearer(token))
		if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != c.want {
			t.Errorf("match-count %s: %d %s, want %s", c.body, w.Code, w.Body, c.want)
		}
	}
	if list := decode(t, do(s, "GET", "/api/rules", "", bearer(token)))["rules"].([]any); len(list) != 1 {
		t.Fatalf("counting saved something: %v", list)
	}
}

// idList is a JSON array of the numbers 1 to n.
func idList(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = strconv.Itoa(i + 1)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
