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
		`{"kind":"link"}`:           "invalid_kind",
		`{"fetchIntervalSec":30}`:   "invalid_interval",
		`{"keepDays":-2}`:           "invalid_keep_days",
		`{"keepDays":3651}`:         "invalid_keep_days",
		`{"paceSec":-1}`:            "invalid_pace",
		`{"paceSec":60}`:            "invalid_pace",
		`{"paceSec":2592001}`:       "invalid_pace",
		`{"url":"https://a.org/x"}`: "invalid_body",
	} {
		if w := do(s, "PATCH", "/api/sources/"+id, body, bearer(token)); w.Code != http.StatusBadRequest || decode(t, w)["error"] != code {
			t.Errorf("patch %s: %d %s", body, w.Code, w.Body)
		}
	}
	if got := decode(t, do(s, "PATCH", "/api/sources/"+id, `{"paceSec":0}`, bearer(token))); got["paceSec"] != 0.0 || got["keepDays"] != 90.0 {
		t.Fatalf("pace off: %v", got)
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
