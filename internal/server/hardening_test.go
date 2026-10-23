package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthSharesTheWriteProbe(t *testing.T) {
	s, clk := newServer(t, "")
	calls := 0
	var failing error
	probe := func(context.Context) error { calls++; return failing }
	for i := 0; i < 3; i++ {
		if err := s.probe.check(context.Background(), clk.now, probe); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("%d probes within the window", calls)
	}
	failing = errors.New("read-only")
	clk.t = clk.t.Add(probeEvery - time.Second)
	if err := s.probe.check(context.Background(), clk.now, probe); err != nil || calls != 1 {
		t.Fatalf("cached: %v, %d probes", err, calls)
	}
	clk.t = clk.t.Add(time.Second)
	if err := s.probe.check(context.Background(), clk.now, probe); err == nil || calls != 2 {
		t.Fatalf("after the window: %v, %d probes", err, calls)
	}
}

func TestHealthUnreadableDatabase(t *testing.T) {
	s, _ := newServer(t, "")
	s.store.Close()
	w := do(s, "GET", "/health", "", nil)
	if body := decode(t, w); w.Code != http.StatusServiceUnavailable || body["database"] != "unwritable" || body["app"] != "oriel" {
		t.Fatalf("%d %v", w.Code, body)
	}
}

func TestThrottleEvictsAtTheCap(t *testing.T) {
	th := newThrottle()
	now := time.UnixMilli(1_700_000_000_000)
	for i := 0; i < maxTracked; i++ {
		th.fail(strconv.Itoa(i), now.Add(time.Duration(i)*time.Millisecond))
	}
	// A locked record outlives quieter unlocked ones.
	for i := 0; i < freeAttempts+1; i++ {
		th.fail("0", now.Add(time.Second))
	}
	later := now.Add(2 * time.Second)
	th.fail("new", later)
	if len(th.failures) != maxTracked-evictBatch+1 {
		t.Fatalf("%d records", len(th.failures))
	}
	if _, ok := th.failures["1"]; ok {
		t.Fatal("the quietest unlocked record stayed")
	}
	if _, ok := th.failures["0"]; !ok {
		t.Fatal("a locked record was evicted")
	}
	for i := 0; i < freeAttempts+1; i++ {
		th.fail("new", later)
	}
	if th.wait("new", later) <= 0 {
		t.Fatal("a new address is not throttled at the cap")
	}
}

func TestThrottlePrunesWithoutFailures(t *testing.T) {
	th := newThrottle()
	now := time.UnixMilli(1_700_000_000_000)
	th.fail("192.0.2.1", now)
	th.wait("192.0.2.9", now.Add(2*forgetAfter))
	if len(th.failures) != 0 {
		t.Fatalf("records: %v", th.failures)
	}
}

func TestClientKeyTrustedProxies(t *testing.T) {
	proxies, err := ParseProxies("10.0.0.0/8, 192.0.2.7")
	if err != nil || len(proxies) != 2 {
		t.Fatalf("%v %v", proxies, err)
	}
	for _, bad := range []string{"nope", "10.0.0.0/99"} {
		if _, err := ParseProxies(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if p, err := ParseProxies(""); err != nil || p != nil {
		t.Errorf("empty: %v %v", p, err)
	}
	mapped, err := ParseProxies("::ffff:10.0.0.0/104, ::ffff:192.0.2.7")
	if err != nil || len(mapped) != 2 || mapped[0].String() != "10.0.0.0/8" || mapped[1].String() != "192.0.2.7/32" {
		t.Errorf("mapped: %v %v", mapped, err)
	}
	for _, bad := range []string{"0.0.0.0/0", "::/0", "::ffff:0.0.0.0/64"} {
		if _, err := ParseProxies(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if p, err := ParseProxies(" 10.0.0.1 , ,192.0.2.7,"); err != nil || len(p) != 2 {
		t.Errorf("spaces and empty items: %v %v", p, err)
	}
	cases := []struct {
		name, remote string
		xff          []string
		proxies      bool
		want         string
	}{
		{"default ignores the header", "10.1.2.3:5000", []string{"203.0.113.9"}, false, "10.1.2.3"},
		{"proxy uses the header", "10.1.2.3:5000", []string{"203.0.113.9"}, true, "203.0.113.9"},
		{"rightmost untrusted hop", "10.1.2.3:5000", []string{"198.51.100.1, 203.0.113.9, 10.9.9.9"}, true, "203.0.113.9"},
		{"several headers", "192.0.2.7:1", []string{"198.51.100.1", "203.0.113.9"}, true, "203.0.113.9"},
		{"all trusted keeps the peer", "10.1.2.3:5000", []string{"10.5.5.5, 10.6.6.6"}, true, "10.1.2.3"},
		{"hop with a port", "10.1.2.3:5000", []string{"203.0.113.9:4711"}, true, "203.0.113.9"},
		{"ipv6 hop with brackets and port", "10.1.2.3:5000", []string{"[2001:db8::1]:443"}, true, "2001:db8::1"},
		{"ipv6 hop with brackets", "10.1.2.3:5000", []string{"[2001:db8::1]"}, true, "2001:db8::1"},
		{"zone is dropped", "10.1.2.3:5000", []string{"fe80::1%eth0"}, true, "fe80::1"},
		{"zone does not hide a proxy", "10.1.2.3:5000", []string{"203.0.113.9, ::ffff:10.9.9.9%eth0"}, true, "203.0.113.9"},
		{"mapped hop is unmapped", "10.1.2.3:5000", []string{"::ffff:203.0.113.9"}, true, "203.0.113.9"},
		{"mapped peer", "[::ffff:10.1.2.3]:5000", []string{"203.0.113.9"}, true, "203.0.113.9"},
		{"empty items", "10.1.2.3:5000", []string{"203.0.113.9, , "}, true, "10.1.2.3"},
		{"whitespace", "10.1.2.3:5000", []string{"  203.0.113.9  ,  10.9.9.9 "}, true, "203.0.113.9"},
		{"untrusted peer cannot forge", "198.51.100.50:5000", []string{"203.0.113.9"}, true, "198.51.100.50"},
		{"no header", "10.1.2.3:5000", nil, true, "10.1.2.3"},
		{"unreadable header", "10.1.2.3:5000", []string{"garbage"}, true, "10.1.2.3"},
		{"ipv6 peer", "[2001:db8::1]:5000", []string{"203.0.113.9"}, true, "2001:db8::1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "/auth/login", nil)
		r.RemoteAddr = c.remote
		for _, v := range c.xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		p := proxies
		if !c.proxies {
			p = nil
		}
		if got := clientKey(r, p); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestHealthBusyProbeIsWritable(t *testing.T) {
	for _, err := range []error{context.DeadlineExceeded, errors.New("database is locked (5) (SQLITE_BUSY)")} {
		s, _ := newServer(t, "")
		s.writable = func(context.Context) error { return err }
		if w := do(s, "GET", "/health", "", nil); w.Code != http.StatusOK || decode(t, w)["database"] != "ok" {
			t.Errorf("%v: %d", err, w.Code)
		}
	}
}

func TestHealthRealWriteFailure(t *testing.T) {
	s, _ := newServer(t, "")
	s.writable = func(context.Context) error { return errors.New("attempt to write a readonly database") }
	w := do(s, "GET", "/health", "", nil)
	if body := decode(t, w); w.Code != http.StatusServiceUnavailable || body["database"] != "unwritable" || body["app"] != "oriel" {
		t.Fatalf("%d %v", w.Code, body)
	}
}

// blockedProbe is a probe that runs until released; started closes when it begins.
func blockedProbe(result error) (probe func(context.Context) error, started, release chan struct{}, calls *int32) {
	started, release, calls = make(chan struct{}), make(chan struct{}), new(int32)
	var once sync.Once
	probe = func(context.Context) error {
		atomic.AddInt32(calls, 1)
		once.Do(func() { close(started) })
		<-release
		return result
	}
	return
}

func healthCodes(s *Server, n int) []int {
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = do(s, "GET", "/health", "", nil).Code
		}()
	}
	wg.Wait()
	return codes
}

func TestHealthConcurrentDoesNotWaitWithAResult(t *testing.T) {
	for _, prior := range []error{nil, errors.New("attempt to write a readonly database")} {
		s, clk := newServer(t, "")
		s.writable = func(context.Context) error { return prior }
		want := http.StatusOK
		if prior != nil {
			want = http.StatusServiceUnavailable
		}
		if code := do(s, "GET", "/health", "", nil).Code; code != want {
			t.Fatalf("first: %d", code)
		}
		clk.t = clk.t.Add(probeEvery)
		probe, started, release, calls := blockedProbe(prior)
		s.writable = probe
		first := make(chan int)
		go func() { first <- do(s, "GET", "/health", "", nil).Code }()
		<-started
		for _, code := range healthCodes(s, 20) { // returns while the probe is still running
			if code != want {
				t.Errorf("prior %v: concurrent %d", prior, code)
			}
		}
		close(release)
		if code := <-first; code != want {
			t.Errorf("prior %v: first %d", prior, code)
		}
		if *calls != 1 {
			t.Errorf("%d probes", *calls)
		}
	}
}

func TestHealthFirstProbeFailureReachesEveryRequest(t *testing.T) {
	s, _ := newServer(t, "")
	probe, started, release, calls := blockedProbe(errors.New("attempt to write a readonly database"))
	s.writable = probe
	first := make(chan int)
	go func() { first <- do(s, "GET", "/health", "", nil).Code }()
	<-started
	codes := make(chan []int)
	go func() { codes <- healthCodes(s, 20) }()
	time.Sleep(50 * time.Millisecond) // let the waiters queue behind the probe
	select {
	case <-codes:
		t.Fatal("requests answered before the first probe ended")
	default:
	}
	close(release)
	for _, code := range append(<-codes, <-first) {
		if code != http.StatusServiceUnavailable {
			t.Errorf("got %d", code)
		}
	}
	if *calls != 1 {
		t.Errorf("%d probes", *calls)
	}
}

func TestHealthProbeIsStampedWhenItEnds(t *testing.T) {
	s, clk := newServer(t, "")
	calls := 0
	probe := func(context.Context) error { calls++; clk.t = clk.t.Add(6 * time.Second); return nil }
	s.probe.check(context.Background(), clk.now, probe)
	clk.t = clk.t.Add(probeEvery - time.Second)
	s.probe.check(context.Background(), clk.now, probe)
	if calls != 1 {
		t.Fatalf("%d probes: the window began before the probe ended", calls)
	}
	clk.t = clk.t.Add(time.Second)
	s.probe.check(context.Background(), clk.now, probe)
	if calls != 2 {
		t.Fatalf("%d probes", calls)
	}
}

func TestHealthPanickingProbeDoesNotStick(t *testing.T) {
	s, clk := newServer(t, "")
	func() {
		defer func() { recover() }()
		s.probe.check(context.Background(), clk.now, func(context.Context) error { panic("boom") })
	}()
	ran := false
	s.probe.check(context.Background(), clk.now, func(context.Context) error { ran = true; return nil })
	if !ran {
		t.Fatal("the probe stayed marked as running")
	}
}

func TestClientKeyWithoutProxiesNormalizesThePeer(t *testing.T) {
	for remote, want := range map[string]string{
		"[fe80::1%eth0]:5000":    "fe80::1",
		"[::ffff:10.1.2.3]:5000": "10.1.2.3",
		"10.1.2.3:5000":          "10.1.2.3",
	} {
		r := httptest.NewRequest("POST", "/auth/login", nil)
		r.RemoteAddr = remote
		if got := clientKey(r, nil); got != want {
			t.Errorf("%s: %s, want %s", remote, got, want)
		}
	}
}

func TestParseProxiesErrors(t *testing.T) {
	if _, err := ParseProxies("nope"); !errors.Is(err, ErrProxyFormat) {
		t.Errorf("format: %v", err)
	}
	for _, bad := range []string{"0.0.0.0/0", "::ffff:0.0.0.0/64"} {
		if _, err := ParseProxies(bad); err == nil || errors.Is(err, ErrProxyFormat) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestThrottleEvictsWhenAllAreLocked(t *testing.T) {
	th := newThrottle()
	now := time.UnixMilli(1_700_000_000_000)
	for i := 0; i < maxTracked; i++ {
		at := now.Add(time.Duration(i) * time.Millisecond)
		for j := 0; j < freeAttempts+1; j++ {
			th.fail(fmt.Sprint(i), at)
		}
	}
	th.fail("new", now.Add(time.Second))
	if len(th.failures) != maxTracked-evictBatch+1 {
		t.Fatalf("%d records", len(th.failures))
	}
	if _, ok := th.failures["0"]; ok {
		t.Fatal("the oldest locked record stayed")
	}
	if _, ok := th.failures["new"]; !ok {
		t.Fatal("the new address is not tracked")
	}
}
