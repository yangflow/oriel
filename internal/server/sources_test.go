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

func TestSourceValidation(t *testing.T) {
	s, _ := newServer(t, "")
	for body, code := range map[string]string{
		`{"kind":"article"}`: "invalid_source",
		`{"url":"https://a.org/f","rsshubPath":"/x","kind":"article"}`:         "invalid_source",
		`{"url":"ftp://a.org/f","kind":"article"}`:                             "invalid_url",
		`{"url":"https:///f","kind":"article"}`:                                "invalid_url",
		`{"url":"a.org/feed","kind":"article"}`:                                "invalid_url",
		`{"rsshubPath":"github/x","kind":"article"}`:                           "invalid_rsshub_path",
		`{"rsshubPath":"//evil.org/x","kind":"article"}`:                       "invalid_rsshub_path",
		`{"url":"https://a.org/f","kind":"podcast"}`:                           "invalid_kind",
		`{"url":"https://a.org/f","kind":"article","fetchIntervalSec":59}`:     "invalid_interval",
		`{"url":"https://a.org/f","kind":"article","fetchIntervalSec":604801}`: "invalid_interval",
		`{"url":"https://a.org/f","kind":"article","fetchIntervalSec":1.5}`:    "invalid_body",
		`{"url":"https://a.org/f","kind":"article","etag":"x"}`:                "invalid_body",
		`{"url":"https://a.org/f","kind":"article"} {}`:                        "invalid_body",
		`not json`: "invalid_body",
	} {
		w := do(s, "POST", "/api/sources", body, nil)
		if w.Code != http.StatusBadRequest || decode(t, w)["error"] != code {
			t.Errorf("%s: %d %s, want %s", body, w.Code, w.Body, code)
		}
	}
	if w := do(s, "POST", "/api/sources", `{"url":"https://a.org/`+strings.Repeat("x", maxBody)+`","kind":"article"}`, nil); w.Code != http.StatusBadRequest {
		t.Errorf("oversized body: %d", w.Code)
	}
	if w := do(s, "GET", "/api/sources", "", nil); len(decode(t, w)["sources"].([]any)) != 0 {
		t.Fatalf("a rejected source was stored: %s", w.Body)
	}
}

func TestSourceLifecycle(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	if w := do(s, "GET", "/api/sources", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("without session: %d", w.Code)
	}

	w := do(s, "POST", "/api/sources", `{"url":"  https://example.org/feed ","kind":"article","title":" Blog "}`, bearer(token))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	blog := decode(t, w)
	created := float64(c.t.UnixMilli())
	if blog["url"] != "https://example.org/feed" || blog["title"] != "Blog" || blog["fetchIntervalSec"] != 900.0 ||
		blog["nextFetchAt"] != created || blog["lastFetchedAt"] != nil || blog["paused"] != false || blog["etag"] != nil {
		t.Fatalf("created %v", blog)
	}
	id := strconv.Itoa(int(blog["id"].(float64)))
	if w := do(s, "POST", "/api/sources", `{"url":"https://example.org/feed","kind":"video"}`, bearer(token)); w.Code != http.StatusConflict || decode(t, w)["error"] != "source_exists" {
		t.Fatalf("duplicate: %d %s", w.Code, w.Body)
	}
	if w := do(s, "POST", "/api/sources", `{"url":"https://Example.org/feed/","kind":"video"}`, bearer(token)); w.Code != http.StatusConflict || decode(t, w)["error"] != "source_exists" {
		t.Fatalf("trailing slash and host case: %d %s", w.Code, w.Body)
	}
	if w := do(s, "POST", "/api/sources", `{"rsshubPath":"/github/issue/x","kind":"social","fetchIntervalSec":3600}`, bearer(token)); w.Code != http.StatusCreated {
		t.Fatalf("rsshub: %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/sources", "", bearer(token))
	if list := decode(t, w)["sources"].([]any); len(list) != 2 || list[0].(map[string]any)["rsshubPath"] != "/github/issue/x" {
		t.Fatalf("list: %s", w.Body)
	}

	c.t = c.t.Add(time.Minute)
	w = do(s, "PATCH", "/api/sources/"+id, `{"title":"","kind":"image","muteKeywords":" ad \n\n  sponsor\r\n","paused":true,"keepDays":90,"paceSec":86400}`, bearer(token))
	patched := decode(t, w)
	if w.Code != http.StatusOK || patched["title"] != "" || patched["kind"] != "image" || patched["muteKeywords"] != "ad\nsponsor" ||
		patched["paused"] != true || patched["keepDays"] != 90.0 || patched["paceSec"] != 86400.0 || patched["updatedAt"] != float64(c.t.UnixMilli()) || patched["fetchIntervalSec"] != 900.0 {
		t.Fatalf("patch: %d %v", w.Code, patched)
	}
	for body, code := range map[string]string{
		`{"kind":"link"}`:                  "invalid_kind",
		`{"fetchIntervalSec":30}`:          "invalid_interval",
		`{"keepDays":-2}`:                  "invalid_keep_days",
		`{"keepDays":3651}`:                "invalid_keep_days",
		`{"paceSec":-1}`:                   "invalid_pace",
		`{"paceSec":60}`:                   "invalid_pace",
		`{"paceSec":2592001}`:              "invalid_pace",
		`{"extractSelector":"div["}`:       "invalid_extract_selector",
		`{"extractReferer":"example.org"}`: "invalid_extract_referer",
		`{"extractRewrites":"m.example.com => https://www.example.com/"}`: "invalid_extract_rewrites",
		`{"url":"https://a.org/x"}`:                                       "invalid_body",
	} {
		if w := do(s, "PATCH", "/api/sources/"+id, body, bearer(token)); w.Code != http.StatusBadRequest || decode(t, w)["error"] != code {
			t.Errorf("patch %s: %d %s", body, w.Code, w.Body)
		}
	}
	for body, code := range map[string]string{
		`{"proxy":"ftp://127.0.0.1:21"}`: "invalid_proxy",
		`{"proxy":"not a url"}`:          "invalid_proxy",
	} {
		if w := do(s, "PATCH", "/api/sources/"+id, body, bearer(token)); w.Code != http.StatusBadRequest || decode(t, w)["error"] != code {
			t.Errorf("patch %s: %d %s", body, w.Code, w.Body)
		}
	}
	w = do(s, "PATCH", "/api/sources/"+id, `{"proxy":" http://127.0.0.1:7890 ","proxyUser":"bob","proxyPassword":"pw-source-1"}`, bearer(token))
	if got := decode(t, w); w.Code != http.StatusOK || got["proxy"] != "http://127.0.0.1:7890" || got["proxyAuthSet"] != true ||
		strings.Contains(w.Body.String(), "bob") || strings.Contains(w.Body.String(), "pw-source-1") {
		t.Fatalf("proxy: %d %s", w.Code, w.Body)
	}
	if w := do(s, "GET", "/api/sources", "", bearer(token)); strings.Contains(w.Body.String(), "pw-source-1") || !strings.Contains(w.Body.String(), `"proxyAuthSet":true`) {
		t.Fatalf("list: %s", w.Body)
	}
	if got := decode(t, do(s, "PATCH", "/api/sources/"+id, `{"proxy":"direct"}`, bearer(token))); got["proxy"] != "direct" || got["proxyAuthSet"] != false {
		t.Fatalf("direct: %v", got)
	}
	if got := decode(t, do(s, "PATCH", "/api/sources/"+id, `{"proxy":""}`, bearer(token))); got["proxy"] != "" || got["keepDays"] != 90.0 {
		t.Fatalf("follow global: %v", got)
	}
	if got := decode(t, do(s, "PATCH", "/api/sources/"+id, `{"paceSec":0}`, bearer(token))); got["paceSec"] != 0.0 || got["keepDays"] != 90.0 {
		t.Fatalf("pace off: %v", got)
	}
	w = do(s, "PATCH", "/api/sources/"+id, `{"extractSelector":" .post ","extractReferer":" https://example.org/front ","extractRewrites":" m.example.com=>www.example.com \n\n"}`, bearer(token))
	if got := decode(t, w); w.Code != http.StatusOK || got["extractSelector"] != ".post" || got["extractReferer"] != "https://example.org/front" ||
		got["extractRewrites"] != "m.example.com => www.example.com" || got["paceSec"] != 0.0 {
		t.Fatalf("extraction rules: %d %v", w.Code, got)
	}
	if got := decode(t, do(s, "PATCH", "/api/sources/"+id, `{"extractSelector":""}`, bearer(token))); got["extractSelector"] != "" || got["extractReferer"] != "https://example.org/front" {
		t.Fatalf("clearing the selector only: %v", got)
	}
	if w := do(s, "POST", "/api/sources/"+id+"/refresh", "", bearer(token)); w.Code != http.StatusConflict || decode(t, w)["error"] != "source_paused" {
		t.Fatalf("refresh paused: %d %s", w.Code, w.Body)
	}
	do(s, "PATCH", "/api/sources/"+id, `{"paused":false}`, bearer(token))
	c.t = c.t.Add(time.Minute)
	if w := do(s, "POST", "/api/sources/"+id+"/refresh", "", bearer(token)); w.Code != http.StatusNoContent {
		t.Fatalf("refresh: %d %s", w.Code, w.Body)
	}
	if got := decode(t, do(s, "GET", "/api/sources/"+id, "", bearer(token))); got["nextFetchAt"] != float64(c.t.UnixMilli()) {
		t.Fatalf("after refresh: %v", got)
	}

	if w := do(s, "POST", "/api/sources/refresh", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("refresh all without a session: %d", w.Code)
	}
	c.t = c.t.Add(time.Minute)
	all := decode(t, do(s, "POST", "/api/sources/refresh", "", bearer(token)))
	if all["queued"] != 2.0 || all["at"] != float64(c.t.UnixMilli()) {
		t.Fatalf("refresh all: %v", all)
	}
	if got := decode(t, do(s, "GET", "/api/sources/"+id, "", bearer(token))); got["nextFetchAt"] != float64(c.t.UnixMilli()) {
		t.Fatalf("after refresh all: %v", got)
	}
	do(s, "PATCH", "/api/sources/"+id, `{"paused":true}`, bearer(token))
	c.t = c.t.Add(time.Minute)
	if all := decode(t, do(s, "POST", "/api/sources/refresh", "", bearer(token))); all["queued"] != 1.0 {
		t.Fatalf("refresh all with one paused: %v", all)
	}
	if got := decode(t, do(s, "GET", "/api/sources/"+id, "", bearer(token))); got["nextFetchAt"] == float64(c.t.UnixMilli()) {
		t.Fatalf("paused source marked due: %v", got)
	}
	do(s, "PATCH", "/api/sources/"+id, `{"paused":false}`, bearer(token))

	if w := do(s, "DELETE", "/api/sources/"+id, "", bearer(token)); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}
	for _, r := range []struct{ method, target, body string }{
		{"GET", "/api/sources/" + id, ""},
		{"PATCH", "/api/sources/" + id, `{"title":"x"}`},
		{"DELETE", "/api/sources/" + id, ""},
		{"POST", "/api/sources/" + id + "/refresh", ""},
	} {
		if w := do(s, r.method, r.target, r.body, bearer(token)); w.Code != http.StatusNotFound || decode(t, w)["error"] != "not_found" {
			t.Errorf("%s %s after delete: %d %s", r.method, r.target, w.Code, w.Body)
		}
	}
	if w := do(s, "GET", "/api/sources/x", "", bearer(token)); w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_id" {
		t.Fatalf("bad id: %d %s", w.Code, w.Body)
	}
}

func TestRestoreSourceRoute(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	if w := do(s, "POST", "/api/sources/1/restore", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("restore without a session: %d", w.Code)
	}
	created := decode(t, do(s, "POST", "/api/sources", `{"url":"https://example.org/feed","kind":"article","title":"Blog"}`, bearer(token)))
	id := strconv.Itoa(int(created["id"].(float64)))
	if w := do(s, "POST", "/api/sources/"+id+"/restore", "", bearer(token)); w.Code != http.StatusNotFound {
		t.Fatalf("restore a source that is not deleted: %d", w.Code)
	}

	do(s, "DELETE", "/api/sources/"+id, "", bearer(token))
	c.t = c.t.Add(store.DeleteUndoWindow - time.Second)
	w := do(s, "POST", "/api/sources/"+id+"/restore", "", bearer(token))
	if got := decode(t, w); w.Code != http.StatusOK || got["title"] != "Blog" {
		t.Fatalf("restore: %d %s", w.Code, w.Body)
	}
	if w := do(s, "GET", "/api/sources/"+id, "", bearer(token)); w.Code != http.StatusOK {
		t.Fatalf("get after restore: %d", w.Code)
	}
	if w := do(s, "POST", "/api/sources/"+id+"/restore", "", bearer(token)); w.Code != http.StatusNotFound {
		t.Fatalf("second restore: %d", w.Code)
	}

	do(s, "DELETE", "/api/sources/"+id, "", bearer(token))
	c.t = c.t.Add(store.DeleteUndoWindow + time.Second)
	if w := do(s, "POST", "/api/sources/"+id+"/restore", "", bearer(token)); w.Code != http.StatusNotFound || decode(t, w)["error"] != "not_found" {
		t.Fatalf("restore after the window: %d %s", w.Code, w.Body)
	}
	if w := do(s, "POST", "/api/sources/999/restore", "", bearer(token)); w.Code != http.StatusNotFound {
		t.Fatalf("restore unknown: %d", w.Code)
	}
	if w := do(s, "POST", "/api/sources/x/restore", "", bearer(token)); w.Code != http.StatusBadRequest {
		t.Fatalf("restore bad id: %d", w.Code)
	}
}

func TestSourcesHealthRoute(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	if w := do(s, "GET", "/api/sources/health", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("without session: %d", w.Code)
	}
	w := do(s, "GET", "/api/sources/health", "", bearer(token))
	if body := decode(t, w); w.Code != http.StatusOK || body["windowDays"] != 30.0 || len(body["sources"].([]any)) != 0 {
		t.Fatalf("empty: %d %s", w.Code, w.Body)
	}

	created := decode(t, do(s, "POST", "/api/sources", `{"url":"https://example.org/feed","kind":"video","title":"Quiet"}`, bearer(token)))
	id := strconv.Itoa(int(created["id"].(float64)))
	do(s, "POST", "/api/sources", `{"rsshubPath":"/x/y","kind":"social","title":"Young"}`, bearer(token))
	c.t = c.t.Add(100 * 24 * time.Hour)
	token = login(t, s, "laptop")
	do(s, "POST", "/api/sources", `{"rsshubPath":"/new/one","kind":"social","title":"New"}`, bearer(token))

	w = do(s, "GET", "/api/sources/health", "", bearer(token))
	list := decode(t, w)["sources"].([]any)
	if w.Code != http.StatusOK || len(list) != 2 {
		t.Fatalf("health: %d %s", w.Code, w.Body)
	}
	first := list[0].(map[string]any)
	if first["title"] != "Quiet" || first["state"] != "dead" || first["cause"] != "silent" || first["days"] != 100.0 ||
		first["kind"] != "video" || first["url"] != "https://example.org/feed" || first["recent"] != 0.0 || first["lastError"] != "" {
		t.Fatalf("first = %v", first)
	}
	if _, ok := first["etag"]; ok {
		t.Fatalf("first = %v", first)
	}

	do(s, "PATCH", "/api/sources/"+id, `{"paused":true}`, bearer(token))
	if list := decode(t, do(s, "GET", "/api/sources/health", "", bearer(token)))["sources"].([]any); len(list) != 1 || list[0].(map[string]any)["title"] != "Young" {
		t.Fatalf("paused source listed: %v", list)
	}
}

func TestSourceLastErrorKind(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	created := decode(t, do(s, "POST", "/api/sources", `{"url":"https://example.org/feed","kind":"article"}`, bearer(token)))
	id := int64(created["id"].(float64))
	if created["lastErrorKind"] != "" {
		t.Fatalf("new source: %v", created)
	}
	at := time.UnixMilli(1_700_000_000_000)
	if err := s.store.FetchFailure(t.Context(), id, "status 403: blocked by Cloudflare", "blocked", at, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	list := decode(t, do(s, "GET", "/api/sources", "", bearer(token)))["sources"].([]any)
	first := list[0].(map[string]any)
	if first["lastErrorKind"] != "blocked" || first["lastError"] != "status 403: blocked by Cloudflare" {
		t.Fatalf("source = %v", first)
	}
}

func TestSourceRoutesRejectBadIDs(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	for _, r := range []struct{ method, target, body string }{
		{"PATCH", "/api/sources/x", `{"title":"t"}`},
		{"DELETE", "/api/sources/x", ""},
		{"POST", "/api/sources/x/restore", ""},
		{"POST", "/api/sources/x/refresh", ""},
		{"POST", "/api/sources/x/muted-count", `{"muteKeywords":"a"}`},
		{"GET", "/api/sources/1.5", ""},
		{"GET", "/api/sources/99999999999999999999", ""},
	} {
		if w := do(s, r.method, r.target, r.body, bearer(token)); w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_id" {
			t.Errorf("%s %s: %d %s", r.method, r.target, w.Code, w.Body)
		}
	}
	for _, r := range []struct{ method, target, body string }{
		{"PATCH", "/api/sources/999", `{"title":"t"}`},
		{"PATCH", "/api/sources/999", `{}`},
		{"POST", "/api/sources/999/restore", ""},
		{"POST", "/api/sources/999/refresh", ""},
	} {
		if w := do(s, r.method, r.target, r.body, bearer(token)); w.Code != http.StatusNotFound || decode(t, w)["error"] != "not_found" {
			t.Errorf("%s %s %s: %d %s", r.method, r.target, r.body, w.Code, w.Body)
		}
	}
}

// A source's AI options are saved as given, an empty PATCH changes nothing, not even updatedAt, and a field of the
// wrong type is refused.
func TestSourcePatchAIOptions(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	created := decode(t, do(s, "POST", "/api/sources", `{"url":"https://example.org/feed","kind":"article","title":"Blog"}`, bearer(token)))
	id := strconv.Itoa(int(created["id"].(float64)))
	if created["autoSummary"] != false || created["autoTranslate"] != false || created["promptSummary"] != "" || created["promptTranslate"] != "" {
		t.Fatalf("defaults: %v", created)
	}
	c.t = c.t.Add(time.Minute)
	w := do(s, "PATCH", "/api/sources/"+id, `{"autoSummary":true,"autoTranslate":true,"promptSummary":"Short","promptTranslate":"Plain"}`, bearer(token))
	got := decode(t, w)
	if w.Code != http.StatusOK || got["autoSummary"] != true || got["autoTranslate"] != true || got["promptSummary"] != "Short" ||
		got["promptTranslate"] != "Plain" || got["updatedAt"] != float64(c.t.UnixMilli()) {
		t.Fatalf("patch: %d %v", w.Code, got)
	}
	c.t = c.t.Add(time.Minute)
	if again := decode(t, do(s, "PATCH", "/api/sources/"+id, `{}`, bearer(token))); again["updatedAt"] != got["updatedAt"] || again["autoSummary"] != true {
		t.Fatalf("empty patch: %v", again)
	}
	if one := decode(t, do(s, "PATCH", "/api/sources/"+id, `{"autoSummary":false}`, bearer(token))); one["autoSummary"] != false ||
		one["autoTranslate"] != true || one["promptSummary"] != "Short" {
		t.Fatalf("only the given field changes: %v", one)
	}
	if clear := decode(t, do(s, "PATCH", "/api/sources/"+id, `{"promptSummary":"","promptTranslate":""}`, bearer(token))); clear["promptSummary"] != "" || clear["promptTranslate"] != "" {
		t.Fatalf("clear prompts: %v", clear)
	}
	for _, body := range []string{`{"autoSummary":"yes"}`, `{"paused":1}`, `{"title":5}`, `{"keepDays":"90"}`, `{"muteKeywords":["a"]}`} {
		if w := do(s, "PATCH", "/api/sources/"+id, body, bearer(token)); w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_body" {
			t.Errorf("%s: %d %s", body, w.Code, w.Body)
		}
	}
	// The edges the validation allows: -1 keeps entries for ever, 3650 days is the most, 0 follows the global setting.
	for body, want := range map[string]float64{`{"keepDays":-1}`: -1, `{"keepDays":3650}`: 3650, `{"keepDays":0}`: 0, `{"paceSec":3600}`: 3600} {
		w := do(s, "PATCH", "/api/sources/"+id, body, bearer(token))
		field := "keepDays"
		if strings.Contains(body, "pace") {
			field = "paceSec"
		}
		if got := decode(t, w); w.Code != http.StatusOK || got[field] != want {
			t.Errorf("%s: %d %v", body, w.Code, got)
		}
	}
	// A title of spaces clears the title and leaves the kind alone.
	if got := decode(t, do(s, "PATCH", "/api/sources/"+id, `{"title":"   "}`, bearer(token))); got["title"] != "" || got["kind"] != "article" {
		t.Fatalf("blank title: %v", got)
	}
}

func TestCreateSourceEdges(t *testing.T) {
	s, _ := newServer(t, "")
	for body, want := range map[string]int{
		`{"url":"https://a.org/f","kind":"article","fetchIntervalSec":60}`:     http.StatusCreated,
		`{"url":"https://a.org/g","kind":"article","fetchIntervalSec":604800}`: http.StatusCreated,
		`{"url":"https://a.org/h","kind":"article","fetchIntervalSec":0}`:      http.StatusBadRequest,
		`{"url":"https://a.org/i","kind":"article","fetchIntervalSec":-60}`:    http.StatusBadRequest,
		`{"url":"https://a.org/j","kind":""}`:                                  http.StatusBadRequest,
		`{"url":"   ","kind":"article"}`:                                       http.StatusBadRequest,
		`{"url":"https://a.org/k","rsshubPath":"","kind":"video"}`:             http.StatusCreated,
		`{"rsshubPath":" /x/y ","kind":"image"}`:                               http.StatusCreated,
		`{"rsshubPath":"   ","kind":"image"}`:                                  http.StatusBadRequest,
		`{"url":"http://[::1]:1200/feed","kind":"article"}`:                    http.StatusCreated,
		`{"url":"https://:443/feed","kind":"article"}`:                         http.StatusBadRequest,
	} {
		if w := do(s, "POST", "/api/sources", body, nil); w.Code != want {
			t.Errorf("%s: %d %s, want %d", body, w.Code, w.Body, want)
		}
	}
	// An rsshub path is saved trimmed; the same path again is a duplicate.
	if w := do(s, "POST", "/api/sources", `{"rsshubPath":"/x/y","kind":"image"}`, nil); w.Code != http.StatusConflict {
		t.Errorf("duplicate rsshub path: %d %s", w.Code, w.Body)
	}
}

func TestMoveSuggestionRoutes(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	ctx := context.Background()
	old := decode(t, do(s, "POST", "/api/sources", `{"url":"http://blog.example.org/feed","kind":"article","title":"Blog"}`, bearer(token)))
	id := strconv.Itoa(int(old["id"].(float64)))
	taken := decode(t, do(s, "POST", "/api/sources", `{"url":"https://elsewhere.example.org/feed","kind":"article"}`, bearer(token)))
	suggest := func(to string) {
		t.Helper()
		for range store.MovedSeenNeeded {
			if err := s.store.NoteRedirect(ctx, int64(old["id"].(float64)), to, c.t); err != nil {
				t.Fatal(err)
			}
		}
	}
	const target = "https://blog.example.org/feed.atom"
	body := `{"url":"` + target + `"}`

	if w := do(s, "POST", "/api/sources/"+id+"/move", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("without session: %d", w.Code)
	}
	if w := do(s, "POST", "/api/sources/"+id+"/move", body, bearer(token)); w.Code != http.StatusConflict || decode(t, w)["error"] != "move_changed" {
		t.Fatalf("no suggestion: %d %s", w.Code, w.Body)
	}
	if w := do(s, "POST", "/api/sources/"+id+"/dismiss-move", body, bearer(token)); w.Code != http.StatusConflict || decode(t, w)["error"] != "move_changed" {
		t.Fatalf("dismiss with no suggestion: %d %s", w.Code, w.Body)
	}
	if w := do(s, "POST", "/api/sources/999/move", body, bearer(token)); w.Code != http.StatusNotFound {
		t.Fatalf("unknown source: %d %s", w.Code, w.Body)
	}

	suggest(target)
	list := decode(t, do(s, "GET", "/api/sources", "", bearer(token)))["sources"].([]any)
	var listed map[string]any
	for _, item := range list {
		if item.(map[string]any)["title"] == "Blog" {
			listed = item.(map[string]any)
		}
	}
	if listed["movedTo"] != target {
		t.Fatalf("listed %v", listed)
	}
	health := decode(t, do(s, "GET", "/api/sources/health", "", bearer(token)))["sources"].([]any)
	if len(health) != 1 || health[0].(map[string]any)["state"] != "moved" || health[0].(map[string]any)["movedTo"] != target {
		t.Fatalf("health %v", health)
	}

	if w := do(s, "POST", "/api/sources/"+id+"/move", `{"url":"https://other.example.org/feed"}`, bearer(token)); w.Code != http.StatusConflict || decode(t, w)["error"] != "move_changed" {
		t.Fatalf("another address: %d %s", w.Code, w.Body)
	}
	dismissed := do(s, "POST", "/api/sources/"+id+"/dismiss-move", body, bearer(token))
	if got := decode(t, dismissed); dismissed.Code != http.StatusOK || got["movedTo"] != nil && got["movedTo"] != "" {
		t.Fatalf("dismiss: %d %s", dismissed.Code, dismissed.Body)
	}
	if health := decode(t, do(s, "GET", "/api/sources/health", "", bearer(token)))["sources"].([]any); len(health) != 0 {
		t.Fatalf("dismissed source still in health: %v", health)
	}

	// An address the user already subscribes to is refused, naming that source.
	suggest("https://elsewhere.example.org/feed")
	w := do(s, "POST", "/api/sources/"+id+"/move", `{"url":"https://elsewhere.example.org/feed"}`, bearer(token))
	if got := decode(t, w); w.Code != http.StatusConflict || got["error"] != "source_exists" || got["sourceId"] != taken["id"] {
		t.Fatalf("taken: %d %s", w.Code, w.Body)
	}

	// The dismissed address is not suggested again; a different one is.
	const target2 = "https://blog.example.org/atom.xml"
	suggest(target2)
	w = do(s, "POST", "/api/sources/"+id+"/move", `{"url":"`+target2+`"}`, bearer(token))
	if got := decode(t, w); w.Code != http.StatusOK || got["url"] != target2 || got["movedTo"] != nil && got["movedTo"] != "" || got["title"] != "Blog" {
		t.Fatalf("accept: %d %s", w.Code, w.Body)
	}
}

func TestSourceFetchHeaders(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	blog := decode(t, do(s, "POST", "/api/sources", `{"url":"https://example.org/feed","kind":"article"}`, bearer(token)))
	id := strconv.Itoa(int(blog["id"].(float64)))
	route := decode(t, do(s, "POST", "/api/sources", `{"rsshubPath":"/github/issue/x","kind":"social"}`, bearer(token)))
	routeID := strconv.Itoa(int(route["id"].(float64)))
	if blog["fetchUserAgent"] != "" || blog["fetchReferer"] != "" {
		t.Fatalf("a new source has headers: %v", blog)
	}

	for body, code := range map[string]string{
		`{"fetchUserAgent":"Cookie: a=b"}`:                      "invalid_fetch_user_agent",
		`{"fetchUserAgent":" authorization: Bearer x"}`:         "invalid_fetch_user_agent",
		`{"fetchUserAgent":"two\nlines"}`:                       "invalid_fetch_user_agent",
		`{"fetchUserAgent":"` + strings.Repeat("a", 501) + `"}`: "invalid_fetch_user_agent",
		`{"fetchReferer":"example.org"}`:                        "invalid_fetch_referer",
		`{"fetchReferer":"https://user:pw@example.org/"}`:       "invalid_fetch_referer",
	} {
		if w := do(s, "PATCH", "/api/sources/"+id, body, bearer(token)); w.Code != http.StatusBadRequest || decode(t, w)["error"] != code {
			t.Errorf("patch %s: %d %s", body, w.Code, w.Body)
		}
	}
	w := do(s, "PATCH", "/api/sources/"+id, `{"fetchUserAgent":" MyReader/1.0 ","fetchReferer":" https://example.org/start "}`, bearer(token))
	if got := decode(t, w); w.Code != http.StatusOK || got["fetchUserAgent"] != "MyReader/1.0" || got["fetchReferer"] != "https://example.org/start" {
		t.Fatalf("headers: %d %s", w.Code, w.Body)
	}
	if got := decode(t, do(s, "PATCH", "/api/sources/"+id, `{"fetchUserAgent":""}`, bearer(token))); got["fetchUserAgent"] != "" || got["fetchReferer"] != "https://example.org/start" {
		t.Fatalf("clearing only the User-Agent: %v", got)
	}

	// A route is served by RSSHub instances, not by the subscription's own site.
	for _, body := range []string{`{"fetchUserAgent":"MyReader/1.0"}`, `{"fetchReferer":"https://example.org/"}`} {
		if w := do(s, "PATCH", "/api/sources/"+routeID, body, bearer(token)); w.Code != http.StatusBadRequest || decode(t, w)["error"] != "fetch_headers_unsupported" {
			t.Errorf("route %s: %d %s", body, w.Code, w.Body)
		}
	}
	if w := do(s, "PATCH", "/api/sources/"+routeID, `{"fetchUserAgent":"","fetchReferer":""}`, bearer(token)); w.Code != http.StatusOK {
		t.Errorf("clearing on a route: %d %s", w.Code, w.Body)
	}
	if w := do(s, "PATCH", "/api/sources/9999", `{"fetchUserAgent":"MyReader/1.0"}`, bearer(token)); w.Code != http.StatusNotFound {
		t.Errorf("missing source: %d %s", w.Code, w.Body)
	}
}

func TestSourceOpenTarget(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	blog := decode(t, do(s, "POST", "/api/sources", `{"url":"https://example.org/feed","kind":"article"}`, bearer(token)))
	id := strconv.Itoa(int(blog["id"].(float64)))
	if blog["openTarget"] != "follow" {
		t.Fatalf("a new source opens %v, want follow", blog["openTarget"])
	}
	for _, body := range []string{`{"openTarget":"tab"}`, `{"openTarget":""}`, `{"openTarget":"Reader"}`} {
		if w := do(s, "PATCH", "/api/sources/"+id, body, bearer(token)); w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_open_target" {
			t.Errorf("patch %s: %d %s", body, w.Code, w.Body)
		}
	}
	for _, v := range []string{"reader", "browser", "follow"} {
		w := do(s, "PATCH", "/api/sources/"+id, `{"openTarget":"`+v+`"}`, bearer(token))
		if got := decode(t, w); w.Code != http.StatusOK || got["openTarget"] != v {
			t.Fatalf("patch %s: %d %s", v, w.Code, w.Body)
		}
	}
	// A patch without the field leaves it alone.
	do(s, "PATCH", "/api/sources/"+id, `{"openTarget":"reader"}`, bearer(token))
	if got := decode(t, do(s, "PATCH", "/api/sources/"+id, `{"title":"Blog"}`, bearer(token))); got["openTarget"] != "reader" {
		t.Errorf("an unrelated patch changed the target: %v", got)
	}
}
