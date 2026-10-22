package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yangflow/oriel/internal/store"
)

// challenge answers like Cloudflare's challenge page for a client it takes for a bot.
func challenge(status int, marked bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if marked {
			w.Header().Set("Cf-Mitigated", "challenge")
		} else {
			w.Header().Set("Server", "cloudflare")
		}
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.WriteHeader(status)
		w.Write([]byte("<html><title>Just a moment...</title></html>"))
	}
}

func TestBlockedByChallenge(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		name string
		h    http.HandlerFunc
		want string
	}{
		{"marked 403", challenge(http.StatusForbidden, true), "blocked"},
		{"marked 503", challenge(http.StatusServiceUnavailable, true), "blocked"},
		{"html page from cloudflare", challenge(http.StatusForbidden, false), "blocked"},
		{"plain 403", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }, ""},
		{"cloudflare 403 without a page", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Server", "cloudflare")
			w.WriteHeader(http.StatusForbidden)
		}, ""},
	}
	for _, c := range cases {
		srv := httptest.NewServer(c.h)
		defer srv.Close()
		id := f.add(store.NewSource{URL: srv.URL})
		f.poller.fetchDue(context.Background())
		if got := f.text("SELECT last_error_kind FROM sources WHERE id = ?", id); got != c.want {
			t.Errorf("%s: kind %q, want %q", c.name, got, c.want)
		}
		if c.want != "" {
			if got := f.text("SELECT last_error FROM sources WHERE id = ?", id); got == "" || got[len(got)-len("blocked by Cloudflare"):] != "blocked by Cloudflare" {
				t.Errorf("%s: last_error = %q, want the raw text", c.name, got)
			}
		}
	}
}

func TestBlockedKindIsClearedBySuccess(t *testing.T) {
	f := newFixture(t)
	in := newInstance(t, http.StatusForbidden)
	in.header.Store(http.Header{"Cf-Mitigated": {"challenge"}})
	f.useInstances(in.URL)
	id := f.add(store.NewSource{RSSHubPath: "/any"})
	f.poller.fetchDue(context.Background())
	if got := f.text("SELECT last_error_kind FROM sources WHERE id = ?", id); got != ErrorBlocked {
		t.Fatalf("kind = %q", got)
	}
	in.status.Store(http.StatusOK)
	in.header.Store(http.Header{})
	f.due()
	f.poller.fetchDue(context.Background())
	if got := f.text("SELECT last_error_kind || last_error FROM sources WHERE id = ?", id); got != "" {
		t.Errorf("after a success: %q", got)
	}
}

func TestBlockedKindSurvivesFallbackInstances(t *testing.T) {
	f := newFixture(t)
	first := newInstance(t, http.StatusServiceUnavailable)
	first.header.Store(http.Header{"Cf-Mitigated": {"challenge"}})
	second := newInstance(t, http.StatusInternalServerError)
	f.useInstances(first.URL, second.URL)
	id := f.add(store.NewSource{RSSHubPath: "/any"})
	f.poller.fetchDue(context.Background())
	if got := f.text("SELECT last_error_kind FROM sources WHERE id = ?", id); got != ErrorBlocked {
		t.Errorf("kind = %q, last_error = %q", got, f.text("SELECT last_error FROM sources WHERE id = ?", id))
	}
}
