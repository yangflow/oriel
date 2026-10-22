package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/fetch"
	"github.com/yangflow/oriel/internal/store"
)

const hubTopic = "https://blog.example/feed.xml"

// websubSource adds a source that asked its hub for a subscription at the clock's time.
func websubSource(t *testing.T, s *Server, c *clock) store.WebSub {
	t.Helper()
	ctx := context.Background()
	if err := s.store.SetSetting(ctx, fetch.WebSubSetting, "https://oriel.example.com"); err != nil {
		t.Fatal(err)
	}
	src, err := s.store.CreateSource(ctx, store.NewSource{URL: hubTopic, Kind: "article", FetchInterval: time.Hour}, "", c.t)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.SetWebSubFeed(ctx, src.ID, "https://hub.example/", hubTopic); err != nil {
		t.Fatal(err)
	}
	w, err := s.store.WebSubRequest(ctx, src.ID, "https://oriel.example.com", c.t)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func verifyURL(w store.WebSub, mode, topic, extra string) string {
	q := url.Values{"hub.mode": {mode}, "hub.topic": {topic}, "hub.challenge": {"abc123"}}
	return "/websub/" + w.Key + "?" + q.Encode() + extra
}

func signed(secret, body string) map[string]string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return map[string]string{"X-Hub-Signature": "sha256=" + hex.EncodeToString(mac.Sum(nil))}
}

func TestWebSubCallbackNeedsNoSessionButIsOptIn(t *testing.T) {
	s, c := newServer(t, "secret")
	w := websubSource(t, s, c)
	if got := do(s, "GET", verifyURL(w, "subscribe", hubTopic, ""), "", nil); got.Code != http.StatusOK || got.Body.String() != "abc123" {
		t.Fatalf("without session: %d %q", got.Code, got.Body)
	}
	// Off again: the route does not exist for anyone.
	s.store.SetSetting(context.Background(), fetch.WebSubSetting, "")
	if got := do(s, "GET", verifyURL(w, "subscribe", hubTopic, ""), "", nil); got.Code != http.StatusNotFound {
		t.Fatalf("while off: %d", got.Code)
	}
}

func TestWebSubVerification(t *testing.T) {
	s, c := newServer(t, "")
	w := websubSource(t, s, c)
	ctx := context.Background()

	for name, target := range map[string]string{
		"unknown key": "/websub/nope?hub.mode=subscribe&hub.topic=" + url.QueryEscape(hubTopic) + "&hub.challenge=x",
		"wrong topic": verifyURL(w, "subscribe", "https://other.example/feed", ""),
		"unsubscribe": verifyURL(w, "unsubscribe", hubTopic, ""),
		"no mode":     "/websub/" + w.Key + "?hub.topic=" + url.QueryEscape(hubTopic) + "&hub.challenge=x",
	} {
		if got := do(s, "GET", target, "", nil); got.Code != http.StatusNotFound {
			t.Errorf("%s: %d", name, got.Code)
		}
	}
	if got := do(s, "GET", "/websub/"+w.Key+"?hub.mode=subscribe&hub.topic="+url.QueryEscape(hubTopic), "", nil); got.Code != http.StatusBadRequest {
		t.Errorf("no challenge: %d", got.Code)
	}
	long := "/websub/" + w.Key + "?hub.mode=subscribe&hub.topic=" + url.QueryEscape(hubTopic) + "&hub.challenge=" + strings.Repeat("a", 1001)
	if got := do(s, "GET", long, "", nil); got.Code != http.StatusBadRequest {
		t.Errorf("long challenge: %d", got.Code)
	}
	if got, _ := s.store.WebSubByKey(ctx, w.Key); got.State != "pending" {
		t.Fatalf("a refused verification changed the state to %q", got.State)
	}

	got := do(s, "GET", verifyURL(w, "subscribe", hubTopic, "&hub.lease_seconds=1000"), "", nil)
	if got.Code != http.StatusOK || got.Body.String() != "abc123" || !strings.HasPrefix(got.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("verify: %d %q", got.Code, got.Body)
	}
	active, _ := s.store.WebSubByKey(ctx, w.Key)
	if active.State != "active" || !active.ExpiresAt.Equal(c.t.Add(1000*time.Second)) || !active.RequestedAt.IsZero() {
		t.Fatalf("after verify: %+v", active)
	}
	if !active.NextAt.Equal(c.t.Add(800 * time.Second)) {
		t.Fatalf("renewal at +%v, want 800s", active.NextAt.Sub(c.t))
	}
	// Without a request on file a verification is not accepted any more.
	if got := do(s, "GET", verifyURL(w, "subscribe", hubTopic, ""), "", nil); got.Code != http.StatusNotFound {
		t.Fatalf("unrequested verification: %d", got.Code)
	}
}

func TestWebSubVerificationWindowAndLease(t *testing.T) {
	s, c := newServer(t, "")
	w := websubSource(t, s, c)
	c.t = c.t.Add(store.WebSubVerifyWindow + time.Second)
	if got := do(s, "GET", verifyURL(w, "subscribe", hubTopic, ""), "", nil); got.Code != http.StatusNotFound {
		t.Fatalf("late verification: %d", got.Code)
	}
	c.t = c.t.Add(-2 * time.Second)
	// No lease asked: a day. Too long a lease: 90 days. Too short: a minute.
	for _, tc := range []struct {
		extra string
		lease time.Duration
	}{
		{"", 24 * time.Hour},
		{"&hub.lease_seconds=999999999", 90 * 24 * time.Hour},
		{"&hub.lease_seconds=5", time.Minute},
	} {
		if _, err := s.store.WebSubRequest(context.Background(), w.SourceID, "https://oriel.example.com", c.t); err != nil {
			t.Fatal(err)
		}
		if got := do(s, "GET", verifyURL(w, "subscribe", hubTopic, tc.extra), "", nil); got.Code != http.StatusOK {
			t.Fatalf("%q: %d", tc.extra, got.Code)
		}
		if got, _ := s.store.WebSubByKey(context.Background(), w.Key); !got.ExpiresAt.Equal(c.t.Add(tc.lease)) {
			t.Errorf("%q: lease %v", tc.extra, got.ExpiresAt.Sub(c.t))
		}
	}
}

func TestWebSubDenied(t *testing.T) {
	s, c := newServer(t, "")
	w := websubSource(t, s, c)
	got := do(s, "GET", "/websub/"+w.Key+"?hub.mode=denied&hub.topic="+url.QueryEscape(hubTopic)+"&hub.reason=not+allowed", "", nil)
	if got.Code != http.StatusOK {
		t.Fatalf("denied: %d", got.Code)
	}
	denied, _ := s.store.WebSubByKey(context.Background(), w.Key)
	if denied.Failures != 1 || denied.Error != "not allowed" || denied.State != "" {
		t.Fatalf("after denial: %+v", denied)
	}
}

func TestWebSubPush(t *testing.T) {
	s, c := newServer(t, "")
	w := websubSource(t, s, c)
	ctx := context.Background()
	body := `<feed>new</feed>`
	due := func() int64 {
		src, err := s.store.Source(ctx, w.SourceID)
		if err != nil {
			t.Fatal(err)
		}
		return src.NextFetchAt.UnixMilli()
	}
	push := func(header map[string]string) int { return do(s, "POST", "/websub/"+w.Key, body, header).Code }

	// Not verified yet: no push is taken.
	if got := push(signed(w.Secret, body)); got != http.StatusNotFound {
		t.Fatalf("push before verification: %d", got)
	}
	if err := s.store.WebSubVerified(ctx, w.SourceID, c.t.Add(time.Hour), c.t.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// The source is not due for another hour.
	if err := s.store.FetchSuccess(ctx, w.SourceID, "", "", "", c.t, c.t.Add(time.Hour), 0); err != nil {
		t.Fatal(err)
	}

	// Bad or missing signatures get a success answer and change nothing.
	for name, header := range map[string]map[string]string{
		"missing":         nil,
		"wrong secret":    signed("other", body),
		"unknown algo":    {"X-Hub-Signature": "md5=00"},
		"not hex":         {"X-Hub-Signature": "sha256=zz"},
		"no algo":         {"X-Hub-Signature": "abcd"},
		"signed old body": signed(w.Secret, "other body"),
	} {
		if got := push(header); got != http.StatusAccepted {
			t.Errorf("%s: %d", name, got)
		}
		if due() != c.t.Add(time.Hour).UnixMilli() {
			t.Fatalf("%s made the source due", name)
		}
	}

	if got := push(signed(w.Secret, body)); got != http.StatusAccepted {
		t.Fatalf("signed push: %d", got)
	}
	if due() != c.t.UnixMilli() {
		t.Fatalf("a signed push did not make the source due: %v", time.UnixMilli(due()).Sub(c.t))
	}

	// A paused source is left alone, and its callback is gone.
	paused := true
	s.store.UpdateSource(ctx, w.SourceID, store.SourcePatch{Paused: &paused}, c.t)
	if got := push(signed(w.Secret, body)); got != http.StatusNotFound {
		t.Fatalf("paused: %d", got)
	}
	paused = false
	s.store.UpdateSource(ctx, w.SourceID, store.SourcePatch{Paused: &paused}, c.t)

	// A lapsed lease takes no push.
	c.t = c.t.Add(2 * time.Hour)
	if got := push(signed(w.Secret, body)); got != http.StatusNotFound {
		t.Fatalf("expired lease: %d", got)
	}
	// Too large a body.
	c.t = c.t.Add(-2 * time.Hour)
	if got := do(s, "POST", "/websub/"+w.Key, strings.Repeat("a", maxPush+1), signed(w.Secret, "x")); got.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large push: %d", got.Code)
	}
}

func TestWebSubSettingsAndSource(t *testing.T) {
	s, c := newServer(t, "secret")
	token := login(t, s, "laptop")
	for _, bad := range []string{"http://oriel.example.com", "oriel.example.com", "https://", "https://u:p@oriel.example.com",
		"https://oriel.example.com/?x=1", "https://oriel.example.com/#a", "ftp://oriel.example.com"} {
		if got := do(s, "PATCH", "/api/settings", `{"websubBaseUrl":"`+bad+`"}`, bearer(token)); got.Code != http.StatusBadRequest || decode(t, got)["error"] != "invalid_websub_base_url" {
			t.Errorf("%q: %d %s", bad, got.Code, got.Body)
		}
	}
	got := do(s, "PATCH", "/api/settings", `{"websubBaseUrl":" https://oriel.example.com/sub/ "}`, bearer(token))
	if got.Code != http.StatusOK || decode(t, got)["websubBaseUrl"] != "https://oriel.example.com/sub" {
		t.Fatalf("set: %d %s", got.Code, got.Body)
	}
	if v, _ := s.store.Setting(context.Background(), fetch.WebSubSetting); v != "https://oriel.example.com/sub" {
		t.Fatalf("stored %q", v)
	}

	w := websubSource(t, s, c)
	if err := s.store.WebSubVerified(context.Background(), w.SourceID, c.t.Add(time.Hour), c.t); err != nil {
		t.Fatal(err)
	}
	src := decode(t, do(s, "GET", "/api/sources/"+strconv.FormatInt(w.SourceID, 10), "", bearer(token)))
	// The clock of the test is far in the past of the real one, so only the hub shows; see below for the active flag.
	if src["websubHub"] != "https://hub.example/" {
		t.Fatalf("source: %v", src)
	}
	if _, ok := src["websubActive"]; !ok {
		t.Fatalf("no websubActive in %v", src)
	}
	if got := do(s, "PATCH", "/api/settings", `{"websubBaseUrl":""}`, bearer(token)); got.Code != http.StatusOK || decode(t, got)["websubBaseUrl"] != "" {
		t.Fatalf("clear: %d %s", got.Code, got.Body)
	}
}
