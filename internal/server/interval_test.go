package server

import (
	"context"
	"net/http"
	"strconv"
	"testing"
)

func TestSourceIntervalAuto(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	create := func(body string) map[string]any {
		t.Helper()
		w := do(s, "POST", "/api/sources", body, bearer(token))
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", body, w.Code, w.Body)
		}
		return decode(t, w)
	}
	auto := create(`{"url":"https://a.org/1","kind":"article"}`)
	if auto["fetchIntervalAuto"] != true || auto["fetchIntervalSec"] != 900.0 || auto["suggestedKind"] != "" {
		t.Fatalf("a new subscription is automatic at 15 min: %v", auto)
	}
	fixed := create(`{"url":"https://a.org/2","kind":"article","fetchIntervalSec":3600}`)
	if fixed["fetchIntervalAuto"] != false || fixed["fetchIntervalSec"] != 3600.0 {
		t.Fatalf("a given interval is fixed: %v", fixed)
	}
	if off := create(`{"url":"https://a.org/3","kind":"article","fetchIntervalAuto":false}`); off["fetchIntervalAuto"] != false {
		t.Fatalf("auto switched off at creation: %v", off)
	}
	if w := do(s, "POST", "/api/sources", `{"url":"https://a.org/4","kind":"article","fetchIntervalAuto":true,"fetchIntervalSec":3600}`,
		bearer(token)); w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_interval" {
		t.Fatalf("auto together with a fixed interval: %d %s", w.Code, w.Body)
	}

	id := strconv.Itoa(int(auto["id"].(float64)))
	patched := decode(t, do(s, "PATCH", "/api/sources/"+id, `{"fetchIntervalSec":7200}`, bearer(token)))
	if patched["fetchIntervalAuto"] != false || patched["fetchIntervalSec"] != 7200.0 {
		t.Fatalf("a fixed interval turns Auto off: %v", patched)
	}
	patched = decode(t, do(s, "PATCH", "/api/sources/"+id, `{"fetchIntervalAuto":true}`, bearer(token)))
	if patched["fetchIntervalAuto"] != true || patched["fetchIntervalSec"] != 7200.0 {
		t.Fatalf("Auto keeps the interval in use until the next fetch: %v", patched)
	}
	if w := do(s, "PATCH", "/api/sources/"+id, `{"fetchIntervalAuto":true,"fetchIntervalSec":3600}`, bearer(token)); w.Code != http.StatusBadRequest ||
		decode(t, w)["error"] != "invalid_interval" {
		t.Fatalf("auto together with a fixed interval: %d %s", w.Code, w.Body)
	}
}

func TestSourceSuggestedKind(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	w := do(s, "POST", "/api/sources", `{"url":"https://a.org/1","kind":"article"}`, bearer(token))
	id := int64(decode(t, w)["id"].(float64))
	if err := s.store.SuggestKind(context.Background(), id, "video"); err != nil {
		t.Fatal(err)
	}
	path := "/api/sources/" + strconv.FormatInt(id, 10)
	if got := decode(t, do(s, "GET", path, "", bearer(token))); got["suggestedKind"] != "video" || got["kind"] != "article" {
		t.Fatalf("suggestion %v", got)
	}
	// Once the kind is the suggested one the suggestion is gone from the reply.
	if got := decode(t, do(s, "PATCH", path, `{"kind":"video"}`, bearer(token))); got["suggestedKind"] != "" {
		t.Fatalf("suggestion stays after it is taken: %v", got)
	}
}
