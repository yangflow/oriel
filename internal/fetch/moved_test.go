package fetch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

type atomicString struct{ v atomic.Value }

func (a *atomicString) set(s string) { a.v.Store(s) }
func (a *atomicString) get() string {
	s, _ := a.v.Load().(string)
	return s
}

func movedFeed(title, icon string) string {
	image := ""
	if icon != "" {
		image = "<image><url>" + icon + "</url></image>"
	}
	return `<?xml version="1.0"?><rss version="2.0"><channel><title>` + title + `</title><link>https://blog.example.org/</link>` + image +
		`<item><title>One</title><link>https://blog.example.org/1</link><guid>one</guid></item></channel></rss>`
}

// poll runs one more fetch of everything due, a day after the one before.
func (f *fixture) poll() {
	f.t.Helper()
	f.now = f.now.Add(24 * time.Hour)
	f.poller.fetchDue(context.Background())
}

func (f *fixture) movedTo(id int64) string {
	f.t.Helper()
	src, err := f.store.Source(context.Background(), id)
	if err != nil {
		f.t.Fatal(err)
	}
	return src.MovedTo
}

// redirecting serves the feed at /new and answers /old with status and a Location of target (which may be on another
// host), or with the feed when status is 200.
func redirecting(status int, target func(r *http.Request) string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" && status != 200 {
			http.Redirect(w, r, target(r), status)
			return
		}
		w.Write([]byte(movedFeed("Blog", "")))
	}))
}

func TestPermanentRedirectIsSuggestedAfterThreeFetches(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusPermanentRedirect} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newFixture(t)
			srv := redirecting(status, func(*http.Request) string { return "/new" })
			defer srv.Close()
			id := f.add(store.NewSource{URL: srv.URL + "/old"})
			for i := 1; i <= 2; i++ {
				f.poll()
				if got := f.movedTo(id); got != "" {
					t.Fatalf("suggested after %d fetches: %q", i, got)
				}
			}
			f.poll()
			if got := f.movedTo(id); got != srv.URL+"/new" {
				t.Fatalf("after three fetches: %q", got)
			}
			// The subscribed address itself is not changed: only the user's answer does that.
			if got := f.text("SELECT url FROM sources WHERE id = ?", id); got != srv.URL+"/old" {
				t.Fatalf("url %q", got)
			}
		})
	}
}

func TestTemporaryRedirectIsNeverSuggested(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusSeeOther} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newFixture(t)
			srv := redirecting(status, func(*http.Request) string { return "/new" })
			defer srv.Close()
			id := f.add(store.NewSource{URL: srv.URL + "/old"})
			for range 4 {
				f.poll()
			}
			if got := f.movedTo(id); got != "" {
				t.Fatalf("suggested: %q", got)
			}
		})
	}
}

func TestMixedRedirectsAreNeverSuggested(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/old":
			http.Redirect(w, r, "/mid", http.StatusMovedPermanently)
		case "/mid":
			http.Redirect(w, r, "/new", http.StatusFound)
		default:
			w.Write([]byte(movedFeed("Blog", "")))
		}
	}))
	defer srv.Close()
	id := f.add(store.NewSource{URL: srv.URL + "/old"})
	for range 4 {
		f.poll()
	}
	if got := f.movedTo(id); got != "" {
		t.Fatalf("suggested: %q", got)
	}
}

func TestPermanentRedirectChainAndOtherHost(t *testing.T) {
	f := newFixture(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(movedFeed("Blog", "")))
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/old":
			http.Redirect(w, r, "/mid", http.StatusMovedPermanently)
		default:
			http.Redirect(w, r, target.URL+"/feed", http.StatusPermanentRedirect)
		}
	}))
	defer srv.Close()
	id := f.add(store.NewSource{URL: srv.URL + "/old"})
	for range 3 {
		f.poll()
	}
	if got := f.movedTo(id); got != target.URL+"/feed" {
		t.Fatalf("suggested %q", got)
	}
}

func TestRedirectCountIsResetByAPlainOrTemporaryFetch(t *testing.T) {
	f := newFixture(t)
	var mode atomicString
	mode.set("301")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			switch mode.get() {
			case "301":
				http.Redirect(w, r, "/new", http.StatusMovedPermanently)
				return
			case "302":
				http.Redirect(w, r, "/new", http.StatusFound)
				return
			}
		}
		w.Write([]byte(movedFeed("Blog", "")))
	}))
	defer srv.Close()
	id := f.add(store.NewSource{URL: srv.URL + "/old"})
	f.poll()
	f.poll()
	mode.set("302")
	f.poll()
	mode.set("301")
	f.poll()
	f.poll()
	if got := f.movedTo(id); got != "" {
		t.Fatalf("a temporary redirect did not reset: %q", got)
	}
	f.poll()
	if got := f.movedTo(id); got != srv.URL+"/new" {
		t.Fatalf("after three more: %q", got)
	}
	mode.set("200")
	f.poll()
	if got := f.movedTo(id); got != "" {
		t.Fatalf("a plain fetch kept the suggestion: %q", got)
	}
}

func TestRedirectCountSurvivesFailuresAndNotModified(t *testing.T) {
	f := newFixture(t)
	var mode atomicString
	mode.set("ok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			if mode.get() == "fail" {
				http.Error(w, "down", http.StatusServiceUnavailable)
				return
			}
			http.Redirect(w, r, "/new", http.StatusMovedPermanently)
			return
		}
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Write([]byte(movedFeed("Blog", "")))
	}))
	defer srv.Close()
	id := f.add(store.NewSource{URL: srv.URL + "/old"})
	f.poll()
	mode.set("fail")
	f.poll()
	f.poll()
	if got := f.movedTo(id); got != "" {
		t.Fatalf("a failure counted: %q", got)
	}
	mode.set("ok")
	f.poll() // 304 after the redirect: a success
	f.poll()
	if got := f.movedTo(id); got != srv.URL+"/new" {
		t.Fatalf("after failures: %q", got)
	}
	mode.set("fail")
	f.poll()
	if got := f.movedTo(id); got == "" {
		t.Fatal("a failure removed the suggestion")
	}
}

func TestRedirectWithCredentialsIsNeverSuggested(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new", http.StatusMovedPermanently)
			return
		}
		w.Write([]byte(movedFeed("Blog", "")))
	}))
	defer srv.Close()
	withUser := strings.Replace(srv.URL, "http://", "http://u:pw@", 1) + "/old"
	id := f.add(store.NewSource{URL: withUser})
	for range 4 {
		f.poll()
	}
	if got := f.movedTo(id); got != "" {
		t.Fatalf("suggested for an address with credentials: %q", got)
	}
}

func TestRedirectToAnAddressWithCredentialsIsNeverSuggested(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "http://u:pw@"+r.Host+"/new", http.StatusMovedPermanently)
			return
		}
		w.Write([]byte(movedFeed("Blog", "")))
	}))
	defer srv.Close()
	id := f.add(store.NewSource{URL: srv.URL + "/old"})
	for range 4 {
		f.poll()
	}
	if got := f.movedTo(id); got != "" {
		t.Fatalf("suggested an address with credentials: %q", got)
	}
}

func TestDismissedRedirectIsNotCountedAgain(t *testing.T) {
	f := newFixture(t)
	var to atomicString
	to.set("/new")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, to.get(), http.StatusMovedPermanently)
			return
		}
		w.Write([]byte(movedFeed("Blog", "")))
	}))
	defer srv.Close()
	id := f.add(store.NewSource{URL: srv.URL + "/old"})
	for range 3 {
		f.poll()
	}
	if _, err := f.store.DismissMove(context.Background(), id, srv.URL+"/new", f.now); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		f.poll()
	}
	if got := f.movedTo(id); got != "" {
		t.Fatalf("the dismissed address came back: %q", got)
	}
	to.set("/newer")
	for range 3 {
		f.poll()
	}
	if got := f.movedTo(id); got != srv.URL+"/newer" {
		t.Fatalf("a different address: %q", got)
	}
}

func TestFeedTitleFollowsAndIconUpdates(t *testing.T) {
	f := newFixture(t)
	var title, icon atomicString
	title.set("First name")
	icon.set("https://blog.example.org/one.png")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(movedFeed(title.get(), icon.get())))
	}))
	defer srv.Close()
	id := f.add(store.NewSource{URL: srv.URL + "/feed"})
	f.poll()
	if got := f.text("SELECT title FROM sources WHERE id = ?", id); got != "First name" {
		t.Fatalf("title %q", got)
	}
	if got := f.text("SELECT icon_url FROM sources WHERE id = ?", id); got != "https://blog.example.org/one.png" {
		t.Fatalf("icon %q", got)
	}
	title.set("Second name")
	icon.set("https://blog.example.org/two.png")
	f.poll()
	if got := f.text("SELECT title FROM sources WHERE id = ?", id); got != "Second name" {
		t.Fatalf("followed title %q", got)
	}
	if got := f.text("SELECT icon_url FROM sources WHERE id = ?", id); got != "https://blog.example.org/two.png" {
		t.Fatalf("updated icon %q", got)
	}
	// A feed with no icon leaves the icon alone.
	icon.set("")
	title.set("Third name")
	f.poll()
	if got := f.text("SELECT icon_url FROM sources WHERE id = ?", id); got != "https://blog.example.org/two.png" {
		t.Fatalf("icon after a feed without one: %q", got)
	}
	// The user's own title stays.
	f.db.Exec("UPDATE sources SET title = 'Mine' WHERE id = ?", id)
	title.set("Fourth name")
	f.poll()
	if got := f.text("SELECT title FROM sources WHERE id = ?", id); got != "Mine" {
		t.Fatalf("user title %q", got)
	}
}
