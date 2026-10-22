package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

func TestPollLogsEachAttempt(t *testing.T) {
	f := newFixture(t)
	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		w.Write([]byte(rssFeed))
	}))
	defer srv.Close()
	id := f.add(store.NewSource{URL: srv.URL + "/feed.xml"})

	f.poller.fetchDue(context.Background())
	fail.Store(true)
	f.now = f.now.Add(time.Hour)
	f.poller.fetchDue(context.Background())

	list, err := f.store.RecentFetches(context.Background())
	if err != nil || len(list) != 1 || list[0].ID != id || len(list[0].Attempts) != 2 {
		t.Fatalf("%+v %v", list, err)
	}
	bad, good := list[0].Attempts[0], list[0].Attempts[1]
	if bad.OK || bad.Status != http.StatusBadGateway || bad.Error == "" || !bad.At.Equal(f.now) {
		t.Fatalf("failed attempt: %+v", bad)
	}
	if !good.OK || good.Status != http.StatusOK || good.NewEntries != 2 || good.Error != "" {
		t.Fatalf("good attempt: %+v", good)
	}
}

func TestCanceledFetchIsNotLogged(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	}))
	defer srv.Close()
	f.add(store.NewSource{URL: srv.URL + "/feed.xml"})
	f.poller.fetchDue(ctx)
	if list, err := f.store.RecentFetches(context.Background()); err != nil || len(list) != 0 {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestFetchingListsSourcesInFlight(t *testing.T) {
	f := newFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.Write([]byte(rssFeed))
	}))
	defer srv.Close()
	id := f.add(store.NewSource{URL: srv.URL + "/feed.xml"})
	if got := f.poller.Fetching(); len(got) != 0 {
		t.Fatalf("idle: %v", got)
	}
	done := make(chan struct{})
	go func() { f.poller.fetchDue(context.Background()); close(done) }()
	<-entered
	if got := f.poller.Fetching(); len(got) != 1 || got[0] != id {
		t.Fatalf("fetching: %v", got)
	}
	close(release)
	<-done
	if got := f.poller.Fetching(); len(got) != 0 {
		t.Fatalf("after: %v", got)
	}
}

func TestNoNewFetchDuringVacuum(t *testing.T) {
	f := newFixture(t)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Write([]byte(rssFeed))
	}))
	defer srv.Close()
	f.add(store.NewSource{URL: srv.URL + "/feed.xml"})
	var vacuuming atomic.Bool
	vacuuming.Store(true)
	f.poller.vacuuming = vacuuming.Load
	f.poller.fetchDue(context.Background())
	if requests.Load() != 0 {
		t.Fatalf("%d requests during a vacuum", requests.Load())
	}
	vacuuming.Store(false)
	f.poller.fetchDue(context.Background())
	if requests.Load() != 1 {
		t.Fatalf("%d requests after it", requests.Load())
	}
}
