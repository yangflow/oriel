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
	r.Host = "127.0.0.1:7470"
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
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

func TestLoginLockout(t *testing.T) {
	s, c := newServer(t, "secret")
	wrong := func() *httptest.ResponseRecorder { return do(s, "POST", "/auth/login", `{"password":"wrong"}`, nil) }
	// The free attempts and the one that starts the lockout all answer 401.
	for i := 0; i <= freeAttempts; i++ {
		if w := wrong(); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i+1, w.Code)
		}
	}
	// Locked out: even the right password is turned away, without deriving a key.
	w := do(s, "POST", "/auth/login", `{"password":"secret"}`, nil)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "1" || decode(t, w)["error"] != "too_many_attempts" {
		t.Fatalf("locked: %d %v %s", w.Code, w.Header(), w.Body)
	}
	// Another address is not affected.
	r := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"password":"secret"}`))
	r.RemoteAddr = "192.0.2.77:4000"
	r.Header.Set("Content-Type", "application/json")
	other := httptest.NewRecorder()
	s.ServeHTTP(other, r)
	if other.Code != http.StatusOK {
		t.Fatalf("other address: %d", other.Code)
	}
	// After the lockout one more failure doubles it.
	c.t = c.t.Add(time.Second)
	if w := wrong(); w.Code != http.StatusUnauthorized {
		t.Fatalf("after lockout: %d", w.Code)
	}
	if w := wrong(); w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "2" {
		t.Fatalf("doubled: %d %v", w.Code, w.Header())
	}
	// The lockout is capped.
	for i := 0; i < 40; i++ {
		c.t = c.t.Add(maxLockout)
		wrong()
	}
	if w := wrong(); w.Header().Get("Retry-After") != strconv.Itoa(int(maxLockout/time.Second)) {
		t.Fatalf("cap: %d %v", w.Code, w.Header())
	}
}

func TestLoginSuccessResetsFailures(t *testing.T) {
	s, _ := newServer(t, "secret")
	for round := 0; round < 3; round++ {
		for i := 0; i < freeAttempts-1; i++ {
			do(s, "POST", "/auth/login", `{"password":"wrong"}`, nil)
		}
		login(t, s, "agent")
	}
	if len(s.throttle.failures) != 0 {
		t.Fatalf("records left: %d", len(s.throttle.failures))
	}
}

func TestLoginDerivationCap(t *testing.T) {
	s, _ := newServer(t, "secret")
	for i := 0; i < maxDerivations; i++ {
		if !s.throttle.acquire() {
			t.Fatalf("slot %d busy", i)
		}
	}
	w := do(s, "POST", "/auth/login", `{"password":"secret"}`, nil)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("full: %d %v", w.Code, w.Header())
	}
	s.throttle.release()
	login(t, s, "agent")
}

func TestThrottlePrunesOldRecords(t *testing.T) {
	th := newThrottle()
	now := time.UnixMilli(1_700_000_000_000)
	th.fail("192.0.2.1", now)
	th.fail("192.0.2.2", now.Add(2*forgetAfter))
	if _, ok := th.failures["192.0.2.1"]; ok || len(th.failures) != 1 {
		t.Fatalf("records: %v", th.failures)
	}
	for i := 0; i < 2*maxTracked; i++ {
		th.fail(strconv.Itoa(i), now.Add(2*forgetAfter))
	}
	if len(th.failures) > maxTracked {
		t.Fatalf("%d records", len(th.failures))
	}
}

func TestRequestOrigin(t *testing.T) {
	s, _ := newServer(t, "")
	post := func(host string, header map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/entries/read", strings.NewReader(`{"kind":"social"}`))
		r.Host = host
		r.Header.Set("Content-Type", "application/json")
		for k, v := range header {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	for name, tc := range map[string]struct {
		host   string
		header map[string]string
		status int
		code   string
	}{
		"no origin":         {"127.0.0.1:7470", nil, http.StatusOK, ""},
		"same origin":       {"127.0.0.1:7470", map[string]string{"Origin": "http://127.0.0.1:7470"}, http.StatusOK, ""},
		"localhost":         {"localhost:7470", map[string]string{"Origin": "http://localhost:7470"}, http.StatusOK, ""},
		"ipv6 loopback":     {"[::1]:7470", nil, http.StatusOK, ""},
		"ipv6 no port":      {"[::1]", map[string]string{"Origin": "http://[::1]"}, http.StatusOK, ""},
		"localhost dot":     {"localhost.:7470", nil, http.StatusOK, ""},
		"mixed case origin": {"localhost:7470", map[string]string{"Origin": "http://LocalHost:7470"}, http.StatusOK, ""},
		"other origin":      {"127.0.0.1:7470", map[string]string{"Origin": "https://example.org"}, http.StatusForbidden, "forbidden_origin"},
		"other port":        {"127.0.0.1:7470", map[string]string{"Origin": "http://127.0.0.1:3000"}, http.StatusForbidden, "forbidden_origin"},
		"null origin":       {"127.0.0.1:7470", map[string]string{"Origin": "null"}, http.StatusForbidden, "forbidden_origin"},
		"cross-site fetch":  {"127.0.0.1:7470", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden, "forbidden_origin"},
		"same-origin fetch": {"127.0.0.1:7470", map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusOK, ""},
		"other host":        {"rebind.example.org:7470", nil, http.StatusForbidden, "forbidden_host"},
		"other host origin": {"rebind.example.org:7470", map[string]string{"Origin": "http://rebind.example.org:7470"}, http.StatusForbidden, "forbidden_host"},
	} {
		t.Run(name, func(t *testing.T) {
			w := post(tc.host, tc.header)
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if tc.code != "" && decode(t, w)["error"] != tc.code {
				t.Fatalf("%s", w.Body)
			}
		})
	}
	if w := do(s, "GET", "/api/sources", "", map[string]string{"Origin": "https://example.org"}); w.Code != http.StatusOK {
		t.Errorf("GET with another origin: %d", w.Code)
	}
}

func TestRequestOriginWithPassword(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "agent")
	header := bearer(token)
	if w := do(s, "GET", "/api/sources", "", header); w.Code != http.StatusOK {
		t.Fatalf("%d", w.Code)
	}
	r := httptest.NewRequest("GET", "/api/sources", nil)
	r.Host = "oriel.example.org"
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("any host with a password: %d", w.Code)
	}
	header["Origin"] = "https://example.org"
	if w := do(s, "POST", "/api/entries/read", `{"kind":"social"}`, header); w.Code != http.StatusForbidden {
		t.Errorf("other origin with a password: %d", w.Code)
	}
}

func TestWebSubIgnoresHostRule(t *testing.T) {
	s, _ := newServer(t, "")
	r := httptest.NewRequest("POST", "/websub/unknown", strings.NewReader("x"))
	r.Host = "oriel.example.org"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound || decode(t, w)["error"] != "not_found" {
		t.Errorf("%d %s", w.Code, w.Body)
	}
}

func TestLoginNeedsJSONContentType(t *testing.T) {
	s, _ := newServer(t, "secret")
	r := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"password":"secret"}`))
	r.Host = "127.0.0.1:7470"
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || decode(t, w)["error"] != "wrong_password" {
		t.Errorf("%d %s", w.Code, w.Body)
	}
}

func TestJSONBodyNeedsContentType(t *testing.T) {
	s, _ := newServer(t, "")
	for contentType, status := range map[string]int{
		"text/plain":                      http.StatusUnsupportedMediaType,
		"multipart/form-data":             http.StatusUnsupportedMediaType,
		"":                                http.StatusUnsupportedMediaType,
		"application/json":                http.StatusOK,
		"application/json; charset=utf-8": http.StatusOK,
	} {
		r := httptest.NewRequest("POST", "/api/entries/read", strings.NewReader(`{"kind":"social"}`))
		r.Host = "127.0.0.1:7470"
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != status {
			t.Errorf("%q: %d %s", contentType, w.Code, w.Body)
		}
		if status != http.StatusOK && decode(t, w)["error"] != "unsupported_media_type" {
			t.Errorf("%q: %s", contentType, w.Body)
		}
	}
}
