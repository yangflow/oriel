package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

var healthEpoch = time.UnixMilli(1_790_000_000_000)

func answered(took time.Duration) healthSample {
	return healthSample{at: healthEpoch, took: took, answered: true}
}

func missed() healthSample { return healthSample{at: healthEpoch} }

func readOne(s *instanceStats, now time.Time) InstanceHealth {
	return s.read([]string{"http://a"}, now)[0]
}

func TestInstanceStatsKeepTheLatest50(t *testing.T) {
	var s instanceStats
	for range 30 {
		s.record("http://a", missed())
	}
	for range 50 {
		s.record("http://a", answered(time.Second))
	}
	h := readOne(&s, healthEpoch)
	if h.Requests != InstanceHealthKeep || h.SuccessRate == nil || *h.SuccessRate != 1 {
		t.Fatalf("%+v: the 30 failures are older than the latest 50 requests", h)
	}
	s.record("http://a", missed())
	if h = readOne(&s, healthEpoch); h.Requests != 50 || *h.SuccessRate != 49.0/50 {
		t.Errorf("%+v, want 49 of 50", h)
	}
}

func TestInstanceStatsDropRequestsOlderThan7Days(t *testing.T) {
	var s instanceStats
	s.record("http://a", healthSample{at: healthEpoch.Add(-8 * 24 * time.Hour), took: time.Second, answered: true})
	s.record("http://a", healthSample{at: healthEpoch.Add(-7 * 24 * time.Hour), took: time.Second})
	s.record("http://a", healthSample{at: healthEpoch.Add(-time.Hour), took: 200 * time.Millisecond, answered: true})
	h := readOne(&s, healthEpoch)
	if h.Requests != 2 || *h.SuccessRate != 0.5 || *h.MedianMs != 200 {
		t.Errorf("%+v: want the request of exactly 7 days and the recent one", h)
	}
	// Time passing alone ages requests out, with nothing recorded.
	h = readOne(&s, healthEpoch.Add(24*time.Hour))
	if h.Requests != 1 || *h.SuccessRate != 1 {
		t.Errorf("a day later: %+v", h)
	}
	h = readOne(&s, healthEpoch.Add(8*24*time.Hour))
	if h.Requests != 0 || h.SuccessRate != nil || h.MedianMs != nil {
		t.Errorf("all aged out: %+v", h)
	}
}

func TestInstanceStatsMedian(t *testing.T) {
	for name, tc := range map[string]struct {
		took []time.Duration
		want int64
	}{
		"one":              {[]time.Duration{300 * time.Millisecond}, 300},
		"odd":              {[]time.Duration{900 * time.Millisecond, 100 * time.Millisecond, 400 * time.Millisecond}, 400},
		"even":             {[]time.Duration{100 * time.Millisecond, 900 * time.Millisecond, 200 * time.Millisecond, 300 * time.Millisecond}, 250},
		"even rounds":      {[]time.Duration{100 * time.Millisecond, 201 * time.Millisecond}, 151},
		"even rounds up":   {[]time.Duration{time.Millisecond, 2 * time.Millisecond}, 2},
		"sub-millisecond":  {[]time.Duration{400 * time.Microsecond, 800 * time.Microsecond, 900 * time.Microsecond}, 1},
		"slow tail is out": {[]time.Duration{time.Second, 2 * time.Second, 30 * time.Second}, 2000},
	} {
		var s instanceStats
		for _, d := range tc.took {
			s.record("http://a", answered(d))
		}
		if h := readOne(&s, healthEpoch); h.MedianMs == nil || *h.MedianMs != tc.want {
			t.Errorf("%s: %+v, want %d ms", name, h, tc.want)
		}
	}
}

func TestInstanceStatsMedianIsOfAnsweredRequestsOnly(t *testing.T) {
	var s instanceStats
	s.record("http://a", healthSample{at: healthEpoch, took: 30 * time.Second}) // timed out
	s.record("http://a", answered(100*time.Millisecond))
	s.record("http://a", answered(300*time.Millisecond))
	h := readOne(&s, healthEpoch)
	if h.Requests != 3 || *h.MedianMs != 200 || *h.SuccessRate != 2.0/3 {
		t.Errorf("%+v", h)
	}
}

func TestInstanceStatsNothingAnswered(t *testing.T) {
	var s instanceStats
	s.record("http://a", missed())
	h := readOne(&s, healthEpoch)
	if h.Requests != 1 || h.SuccessRate == nil || *h.SuccessRate != 0 || h.MedianMs != nil {
		t.Errorf("%+v: a rate of 0 and no latency", h)
	}
}

func TestInstanceStatsNoRequests(t *testing.T) {
	var s instanceStats
	got := s.read([]string{"http://a", "http://b"}, healthEpoch)
	if len(got) != 2 || got[0].URL != "http://a" || got[1].URL != "http://b" {
		t.Fatalf("%+v: want the instances in the order given", got)
	}
	for _, h := range got {
		if h.Requests != 0 || h.SuccessRate != nil || h.MedianMs != nil || h.CoolingUntil != nil {
			t.Errorf("%+v", h)
		}
	}
	if got := s.read(nil, healthEpoch); got == nil || len(got) != 0 {
		t.Errorf("no instances: %#v", got)
	}
}

func TestInstanceStatsForgetInstancesNotListed(t *testing.T) {
	var s instanceStats
	s.record("http://a", answered(time.Second))
	s.record("http://b", answered(time.Second))
	s.read([]string{"http://a"}, healthEpoch)
	if got := s.read([]string{"http://b"}, healthEpoch); got[0].Requests != 0 {
		t.Errorf("an instance taken out of the list kept its requests: %+v", got[0])
	}
}

func TestInstanceStatsAreSafeForConcurrentUse(t *testing.T) {
	var s instanceStats
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				s.record("http://a", answered(time.Millisecond))
				s.read([]string{"http://a"}, healthEpoch)
			}
		}()
	}
	wg.Wait()
	if h := readOne(&s, healthEpoch); h.Requests != InstanceHealthKeep {
		t.Errorf("%+v", h)
	}
}

// health reads what the poller says about the instances.
func (f *fixture) health(instances ...*instance) []InstanceHealth {
	var urls []string
	for _, i := range instances {
		urls = append(urls, i.URL)
	}
	return f.poller.InstanceHealth(urls)
}

func TestWhatCountsAsAnAnsweredRequest(t *testing.T) {
	for name, tc := range map[string]struct {
		status   int
		answered bool
	}{
		"200":                   {http.StatusOK, true},
		"304 not modified":      {http.StatusNotModified, true},
		"404 the route is gone": {http.StatusNotFound, true},
		"500":                   {http.StatusInternalServerError, false},
		"503":                   {http.StatusServiceUnavailable, false},
		"429":                   {http.StatusTooManyRequests, false},
	} {
		for _, many := range []bool{false, true} {
			f := newFixture(t)
			inst := newInstance(t, tc.status)
			list := []*instance{inst}
			if many {
				list = append(list, newInstance(t, http.StatusOK))
			}
			var urls []string
			for _, i := range list {
				urls = append(urls, i.URL)
			}
			f.useInstances(urls...)
			f.add(store.NewSource{RSSHubPath: "/a"})
			f.poller.fetchDue(context.Background())
			h := f.health(list...)[0]
			if h.Requests != 1 {
				t.Errorf("%s (several %v): %d requests, want 1", name, many, h.Requests)
				continue
			}
			if got := *h.SuccessRate == 1; got != tc.answered {
				t.Errorf("%s (several %v): answered %v, want %v", name, many, got, tc.answered)
			}
			if (h.MedianMs != nil) != tc.answered {
				t.Errorf("%s (several %v): median %v", name, many, h.MedianMs)
			}
		}
	}
}

func TestFailedFirstTryBeforeAFallbackCounts(t *testing.T) {
	f := newFixture(t)
	primary, fallback := newInstance(t, http.StatusInternalServerError), newInstance(t, http.StatusOK)
	f.useInstances(primary.URL, fallback.URL)
	f.add(store.NewSource{RSSHubPath: "/a"})
	f.poller.fetchDue(context.Background())
	got := f.health(primary, fallback)
	if got[0].Requests != 1 || *got[0].SuccessRate != 0 || got[1].Requests != 1 || *got[1].SuccessRate != 1 {
		t.Errorf("%+v", got)
	}
}

func TestNoResponseCountsAsNotAnswered(t *testing.T) {
	f := newFixture(t)
	dead := newInstance(t, http.StatusOK)
	dead.Close()
	f.useInstances(dead.URL)
	f.add(store.NewSource{RSSHubPath: "/a"})
	f.poller.fetchDue(context.Background())
	if h := f.health(dead)[0]; h.Requests != 1 || *h.SuccessRate != 0 || h.MedianMs != nil {
		t.Errorf("%+v", h)
	}
}

func TestCooldownSkipIsNotARequest(t *testing.T) {
	f := newFixture(t)
	primary, fallback := newInstance(t, http.StatusInternalServerError), newInstance(t, http.StatusOK)
	f.useInstances(primary.URL, fallback.URL)
	f.add(store.NewSource{RSSHubPath: "/a"})
	f.poller.fetchDue(context.Background())
	if h := f.health(primary, fallback)[0]; h.Requests != 1 || h.CoolingUntil == nil || !h.CoolingUntil.After(f.now) {
		t.Fatalf("after the failure: %+v", h)
	}

	f.now = f.now.Add(30 * time.Second)
	f.due()
	f.poller.fetchDue(context.Background())
	got := f.health(primary, fallback)
	if got[0].Requests != 1 || got[0].CoolingUntil == nil {
		t.Errorf("a route skipped the cooling primary and still counted a request: %+v", got[0])
	}
	if got[1].Requests != 2 {
		t.Errorf("the fallback served 2 routes: %+v", got[1])
	}

	f.now = f.now.Add(10 * time.Minute)
	if h := f.health(primary, fallback)[0]; h.CoolingUntil != nil || h.Requests != 1 {
		t.Errorf("after the cool-down: %+v", h)
	}
}

func TestCanceledRequestIsNotCounted(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	}))
	defer srv.Close()
	f.useInstances(srv.URL)
	f.add(store.NewSource{RSSHubPath: "/a"})
	f.poller.fetchDue(ctx)
	if got := f.poller.InstanceHealth([]string{srv.URL}); got[0].Requests != 0 {
		t.Errorf("a request canceled on shutdown was counted: %+v", got[0])
	}
}

func TestFullURLSubscriptionsAreNotCounted(t *testing.T) {
	f := newFixture(t)
	srv := newInstance(t, http.StatusOK)
	f.useInstances(srv.URL)
	f.add(store.NewSource{URL: srv.URL + "/feed.xml"})
	f.poller.fetchDue(context.Background())
	if srv.hits.Load() != 1 {
		t.Fatalf("the feed was fetched %d times", srv.hits.Load())
	}
	if h := f.health(srv)[0]; h.Requests != 0 {
		t.Errorf("a feed address, not a route, was counted: %+v", h)
	}
}

func TestRequestDurationRunsFromSendingToTheResult(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.now = f.now.Add(1500 * time.Millisecond)
		w.Write([]byte(rssFeed))
	}))
	defer srv.Close()
	f.useInstances(srv.URL)
	f.add(store.NewSource{RSSHubPath: "/a"})
	f.poller.fetchDue(context.Background())
	if got := f.poller.InstanceHealth([]string{srv.URL}); got[0].MedianMs == nil || *got[0].MedianMs != 1500 {
		t.Errorf("%+v", got[0])
	}
}
