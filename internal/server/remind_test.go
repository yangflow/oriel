package server

import (
	"context"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
	"github.com/yangflow/oriel/internal/store/storetest"
)

func TestRemindMeLater(t *testing.T) {
	s, c := newServer(t, "secret")
	header := bearer(login(t, s, "laptop"))
	seed(t, s, "article", 3, c.t)
	list := func(query string) []string {
		t.Helper()
		w := do(s, "GET", "/api/entries"+query, "", header)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", query, w.Code, w.Body)
		}
		return titles(t, decode(t, w))
	}
	first := decode(t, do(s, "GET", "/api/entries?unread=1", "", header))["entries"].([]any)[0].(map[string]any)
	reminded := first["title"].(string)
	target := "/api/entries/" + strconv.Itoa(int(first["id"].(float64)))
	at := c.t.Add(time.Hour).UnixMilli()

	// Setting a reminder marks the entry read, so it leaves Unread and the counts until the time comes.
	w := do(s, "PATCH", target, `{"remindAt":`+strconv.FormatInt(at, 10)+`}`, header)
	if body := decode(t, w); w.Code != http.StatusOK || body["isRead"] != true || int64(body["remindAt"].(float64)) != at {
		t.Fatalf("set: %d %s", w.Code, w.Body)
	}
	if got := list("?unread=1"); len(got) != 2 || slices.Contains(got, reminded) {
		t.Errorf("Unread before the time = %q", got)
	}
	if total := decode(t, do(s, "GET", "/api/sources", "", header))["unreadTotal"]; total != 2.0 {
		t.Errorf("unreadTotal before the time = %v, want 2", total)
	}

	// When the time comes the entry is unread again and the first of Unread, ahead of newer arrivals.
	c.t = c.t.Add(30 * time.Minute)
	seed(t, s, "image", 1, c.t)
	c.t = c.t.Add(31 * time.Minute)
	got := list("?unread=1")
	if len(got) != 4 || got[0] != reminded {
		t.Errorf("Unread after the time = %q, want %q first", got, reminded)
	}
	if body := decode(t, do(s, "GET", target, "", header)); body["isRead"] != false || body["remindAt"] != nil {
		t.Errorf("after the time: isRead = %v, remindAt = %v", body["isRead"], body["remindAt"])
	}
	if total := decode(t, do(s, "GET", "/api/sources", "", header))["unreadTotal"]; total != 4.0 {
		t.Errorf("unreadTotal after the time = %v, want 4", total)
	}
}

func TestRemindCancelled(t *testing.T) {
	s, c := newServer(t, "secret")
	header := bearer(login(t, s, "laptop"))
	seed(t, s, "article", 3, c.t)
	ids := []string{}
	for _, item := range decode(t, do(s, "GET", "/api/entries", "", header))["entries"].([]any) {
		ids = append(ids, strconv.Itoa(int(item.(map[string]any)["id"].(float64))))
	}
	at := strconv.FormatInt(c.t.Add(time.Hour).UnixMilli(), 10)
	for _, id := range ids[:2] {
		if w := do(s, "PATCH", "/api/entries/"+id, `{"remindAt":`+at+`}`, header); w.Code != http.StatusOK {
			t.Fatalf("set: %d %s", w.Code, w.Body)
		}
	}
	// Clearing it keeps the entry read; marking it unread cancels it and the entry is unread right away.
	w := do(s, "PATCH", "/api/entries/"+ids[0], `{"remindAt":0}`, header)
	if body := decode(t, w); w.Code != http.StatusOK || body["isRead"] != true || body["remindAt"] != nil {
		t.Errorf("clear: %d %s", w.Code, w.Body)
	}
	w = do(s, "PATCH", "/api/entries/"+ids[1], `{"isRead":false}`, header)
	if body := decode(t, w); w.Code != http.StatusOK || body["isRead"] != false || body["remindAt"] != nil {
		t.Errorf("mark unread: %d %s", w.Code, w.Body)
	}
	// Neither comes back by itself later.
	c.t = c.t.Add(2 * time.Hour)
	if got := titles(t, decode(t, do(s, "GET", "/api/entries?unread=1", "", header))); len(got) != 2 {
		t.Errorf("Unread = %q, want the entry marked unread and the untouched one", got)
	}
}

func TestRemindValidation(t *testing.T) {
	s, c := newServer(t, "")
	seed(t, s, "article", 1, c.t)
	id := strconv.Itoa(int(decode(t, do(s, "GET", "/api/entries", "", nil))["entries"].([]any)[0].(map[string]any)["id"].(float64)))
	for _, body := range []string{
		`{"remindAt":` + strconv.FormatInt(c.t.UnixMilli(), 10) + `}`, // not in the future
		`{"remindAt":-5}`, // negative
		`{"remindAt":` + strconv.FormatInt(c.t.Add(400*24*time.Hour).UnixMilli(), 10) + `}`,         // too far ahead
		`{"isRead":false,"remindAt":` + strconv.FormatInt(c.t.Add(time.Hour).UnixMilli(), 10) + `}`, // unread and reminded
	} {
		w := do(s, "PATCH", "/api/entries/"+id, body, nil)
		if w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_remind_at" {
			t.Errorf("%s: %d %s", body, w.Code, w.Body)
		}
	}
}

// A reminder lives in the database: a restart before the time loses nothing, and one after it brings the entry back.
func TestRemindSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	file := storetest.Path(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	st, err := store.Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(st, Config{Version: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	c := &clock{time.UnixMilli(1_700_000_000_000)}
	s.now = c.now
	seed(t, s, "article", 2, c.t)
	first := decode(t, do(s, "GET", "/api/entries", "", nil))["entries"].([]any)[0].(map[string]any)
	id := strconv.Itoa(int(first["id"].(float64)))
	at := strconv.FormatInt(c.t.Add(time.Hour).UnixMilli(), 10)
	if w := do(s, "PATCH", "/api/entries/"+id, `{"remindAt":`+at+`}`, nil); w.Code != http.StatusOK {
		t.Fatalf("set: %d %s", w.Code, w.Body)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = store.Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s, err = New(st, Config{Version: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(90 * time.Minute)
	s.now = c.now
	got := titles(t, decode(t, do(s, "GET", "/api/entries?unread=1", "", nil)))
	if len(got) != 2 || got[0] != first["title"] {
		t.Errorf("Unread after the restart = %q, want %q first", got, first["title"])
	}
}
