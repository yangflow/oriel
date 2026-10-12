package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

func init() { passwordRounds = 1 }

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newServer(t *testing.T, password string) (*Server, *clock) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "oriel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s, err := New(st, Config{Password: password, Version: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	c := &clock{time.UnixMilli(1_700_000_000_000)}
	s.now = c.now
	return s, c
}

func do(s *Server, method, target, body string, header map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range header {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	return body
}

func login(t *testing.T, s *Server, agent string) string {
	t.Helper()
	w := do(s, "POST", "/auth/login", `{"password":"secret"}`, map[string]string{"User-Agent": agent})
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	return decode(t, w)["token"].(string)
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func TestListenAddr(t *testing.T) {
	if got := ListenAddr("", 2470); got != "127.0.0.1:2470" {
		t.Errorf("without password: %s", got)
	}
	if got := ListenAddr("secret", 2470); got != ":2470" {
		t.Errorf("with password: %s", got)
	}
}

func TestHealth(t *testing.T) {
	for _, password := range []string{"", "secret"} {
		s, _ := newServer(t, password)
		w := do(s, "GET", "/health", "", nil)
		body := decode(t, w)
		if w.Code != http.StatusOK || body["app"] != "oriel" || body["database"] != "ok" || body["auth"] != (password != "") {
			t.Errorf("password %q: %d %v", password, w.Code, body)
		}
	}
}

func TestWithoutPasswordNoSignIn(t *testing.T) {
	s, _ := newServer(t, "")
	if w := do(s, "GET", "/api/sessions", "", nil); w.Code != http.StatusOK {
		t.Fatalf("sessions: %d", w.Code)
	}
	w := do(s, "POST", "/auth/login", `{"password":"x"}`, nil)
	if w.Code != http.StatusBadRequest || decode(t, w)["error"] != "auth_disabled" {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
}

func TestLoginFailures(t *testing.T) {
	s, _ := newServer(t, "secret")
	for _, body := range []string{`{"password":"wrong"}`, `{"password":`, `not json`, ``} {
		w := do(s, "POST", "/auth/login", body, nil)
		if w.Code != http.StatusUnauthorized || decode(t, w)["error"] != "wrong_password" {
			t.Errorf("%q: %d %s", body, w.Code, w.Body)
		}
	}
	for _, header := range []map[string]string{nil, bearer("forged"), {"Cookie": cookieName + "=forged"}} {
		if w := do(s, "GET", "/api/sessions", "", header); w.Code != http.StatusUnauthorized {
			t.Errorf("%v: %d", header, w.Code)
		}
	}
}

func TestSessionLifecycle(t *testing.T) {
	s, c := newServer(t, "secret")
	w := do(s, "POST", "/auth/login", `{"password":"secret"}`, map[string]string{"X-Forwarded-Proto": "https"})
	cookie := w.Result().Cookies()[0]
	if cookie.Name != cookieName || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie: %+v", cookie)
	}
	laptop := decode(t, w)["token"].(string)
	if cookie.Value != laptop {
		t.Fatal("cookie and token differ")
	}
	phone := login(t, s, "phone")

	w = do(s, "GET", "/api/sessions", "", map[string]string{"Cookie": cookieName + "=" + laptop})
	sessions := decode(t, w)["sessions"].([]any)
	if w.Code != http.StatusOK || len(sessions) != 2 {
		t.Fatalf("sessions: %d %v", w.Code, sessions)
	}
	var phoneID string
	for _, item := range sessions {
		session := item.(map[string]any)
		if session["userAgent"] == "phone" {
			phoneID = strconv.Itoa(int(session["id"].(float64)))
			if session["current"] != false {
				t.Fatal("phone session marked current for the laptop")
			}
		}
	}

	if w := do(s, "DELETE", "/api/sessions/"+phoneID, "", bearer(laptop)); w.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", w.Code, w.Body)
	}
	if w := do(s, "GET", "/api/sessions", "", bearer(phone)); w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session still works: %d", w.Code)
	}
	if w := do(s, "DELETE", "/api/sessions/"+phoneID, "", bearer(laptop)); w.Code != http.StatusNotFound {
		t.Fatalf("second revoke: %d", w.Code)
	}
	if w := do(s, "DELETE", "/api/sessions/x", "", bearer(laptop)); w.Code != http.StatusBadRequest {
		t.Fatalf("bad id: %d", w.Code)
	}

	// Use within 30 days keeps the session alive; 30 idle days end it.
	c.t = c.t.Add(29 * 24 * time.Hour)
	if w := do(s, "GET", "/api/sessions", "", bearer(laptop)); w.Code != http.StatusOK {
		t.Fatalf("after 29 days: %d", w.Code)
	}
	c.t = c.t.Add(29 * 24 * time.Hour)
	if w := do(s, "GET", "/api/sessions", "", bearer(laptop)); w.Code != http.StatusOK {
		t.Fatalf("29 days after last use: %d", w.Code)
	}
	c.t = c.t.Add(30 * 24 * time.Hour)
	if w := do(s, "GET", "/api/sessions", "", bearer(laptop)); w.Code != http.StatusUnauthorized {
		t.Fatalf("30 idle days: %d", w.Code)
	}

	token := login(t, s, "tablet")
	if w := do(s, "POST", "/auth/logout", "", bearer(token)); w.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", w.Code)
	}
	if w := do(s, "GET", "/api/sessions", "", bearer(token)); w.Code != http.StatusUnauthorized {
		t.Fatalf("after logout: %d", w.Code)
	}
}

func TestUnknownRoute(t *testing.T) {
	s, _ := newServer(t, "")
	w := do(s, "GET", "/nope", "", nil)
	if w.Code != http.StatusNotFound || decode(t, w)["error"] != "not_found" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
