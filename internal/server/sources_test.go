package server

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
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
	if w := do(s, "POST", "/api/sources", `{"rsshubPath":"/github/issue/x","kind":"social","fetchIntervalSec":3600}`, bearer(token)); w.Code != http.StatusCreated {
		t.Fatalf("rsshub: %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/sources", "", bearer(token))
	if list := decode(t, w)["sources"].([]any); len(list) != 2 || list[0].(map[string]any)["rsshubPath"] != "/github/issue/x" {
		t.Fatalf("list: %s", w.Body)
	}

	c.t = c.t.Add(time.Minute)
	w = do(s, "PATCH", "/api/sources/"+id, `{"title":"","kind":"image","muteKeywords":" ad \n\n  sponsor\r\n","paused":true}`, bearer(token))
	patched := decode(t, w)
	if w.Code != http.StatusOK || patched["title"] != "" || patched["kind"] != "image" || patched["muteKeywords"] != "ad\nsponsor" ||
		patched["paused"] != true || patched["updatedAt"] != float64(c.t.UnixMilli()) || patched["fetchIntervalSec"] != 900.0 {
		t.Fatalf("patch: %d %v", w.Code, patched)
	}
	for body, code := range map[string]string{
		`{"kind":"link"}`:           "invalid_kind",
		`{"fetchIntervalSec":30}`:   "invalid_interval",
		`{"url":"https://a.org/x"}`: "invalid_body",
	} {
		if w := do(s, "PATCH", "/api/sources/"+id, body, bearer(token)); w.Code != http.StatusBadRequest || decode(t, w)["error"] != code {
			t.Errorf("patch %s: %d %s", body, w.Code, w.Body)
		}
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
