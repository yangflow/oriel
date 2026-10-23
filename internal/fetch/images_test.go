package fetch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var pngHeader = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func TestImagesSniffAndRefuse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "" || !strings.HasPrefix(r.Header.Get("User-Agent"), "Oriel/") {
			http.Error(w, "headers", http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/pic":
			w.Header().Set("Content-Type", "text/plain") // the declared type is ignored
			w.Write(pngHeader)
		case "/svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Write([]byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
		case "/big":
			w.Write(pngHeader)
			w.Write(make([]byte, maxBytes))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	im := NewImages("test", "")
	im.guarded = guardedClient(func(netip.AddrPort) bool { return false })
	ctx := context.Background()

	kind, body, err := im.Get(ctx, srv.URL+"/pic")
	if err != nil || kind != "image/png" || len(body) != len(pngHeader) {
		t.Fatalf("pic = %q, %d bytes, %v", kind, len(body), err)
	}
	if _, _, err := im.Get(ctx, srv.URL+"/svg"); !errors.Is(err, ErrNotImage) {
		t.Errorf("svg: %v", err)
	}
	if _, _, err := im.Get(ctx, srv.URL+"/big"); !errors.Is(err, errTooLarge) {
		t.Errorf("big: %v", err)
	}
	if _, _, err := im.Get(ctx, srv.URL+"/gone"); err == nil {
		t.Error("404 was accepted")
	}
	for _, address := range []string{"file:///etc/passwd", "data:image/png;base64,AA", "http://", "::"} {
		if _, _, err := im.Get(ctx, address); err == nil {
			t.Errorf("%q was accepted", address)
		}
	}
}

func TestImagesStayOutside(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(pngHeader) }))
	defer srv.Close()
	if _, _, err := NewImages("test", "").Get(context.Background(), srv.URL); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("loopback image: %v", err)
	}
}

func newTestImages(t *testing.T) *Images {
	t.Helper()
	im := NewImages("test", "")
	im.guarded = guardedClient(func(netip.AddrPort) bool { return false })
	return im
}

func TestImagesCoalesceConcurrentRequests(t *testing.T) {
	var hits atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release
		w.Write(pngHeader)
	}))
	defer srv.Close()
	im := newTestImages(t)

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, body, err := im.Get(context.Background(), srv.URL+"/pic")
			if err == nil && len(body) != len(pngHeader) {
				err = errors.New("short body")
			}
			errs <- err
		}()
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		im.mu.Lock()
		w := 0
		if f := im.flights[srv.URL+"/pic"]; f != nil {
			w = f.waiters
		}
		im.mu.Unlock()
		if w == n {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d requests joined", w)
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("origin requests = %d, want 1", got)
	}
}

func TestImagesCancelledWaiterKeepsSharedDownload(t *testing.T) {
	var hits atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release
		w.Write(pngHeader)
	}))
	defer srv.Close()
	im := newTestImages(t)
	address := srv.URL + "/pic"

	waiters := func() int {
		im.mu.Lock()
		defer im.mu.Unlock()
		if f := im.flights[address]; f != nil {
			return f.waiters
		}
		return 0
	}
	wait := func(want int) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); waiters() != want; time.Sleep(time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("waiters = %d, want %d", waiters(), want)
			}
		}
	}

	type result struct {
		body []byte
		err  error
	}
	patient := make(chan result, 1)
	go func() {
		_, body, err := im.Get(context.Background(), address)
		patient <- result{body, err}
	}()
	wait(1)
	ctx, cancel := context.WithCancel(context.Background())
	quit := make(chan error, 1)
	go func() {
		_, _, err := im.Get(ctx, address)
		quit <- err
	}()
	wait(2)
	cancel()
	if err := <-quit; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request: %v", err)
	}
	wait(1)
	close(release)
	r := <-patient
	if r.err != nil || len(r.body) != len(pngHeader) {
		t.Fatalf("remaining request = %d bytes, %v", len(r.body), r.err)
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("origin requests = %d, want 1", got)
	}
}

func TestImagesLastWaiterLeavingCancelsDownload(t *testing.T) {
	started := make(chan struct{})
	gone := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(gone)
	}))
	defer srv.Close()
	im := newTestImages(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := im.Get(ctx, srv.URL+"/pic")
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Get: %v", err)
	}
	select {
	case <-gone:
	case <-time.After(5 * time.Second):
		t.Fatal("the abandoned download kept running")
	}
}

func TestImagesCapConcurrentDownloads(t *testing.T) {
	var inFlight, peak atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		<-release
		w.Write(pngHeader)
	}))
	defer srv.Close()
	im := newTestImages(t)

	const n = maxImageDownloads * 3
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := im.Get(context.Background(), fmt.Sprintf("%s/pic%d", srv.URL, i)); err != nil {
				t.Error(err)
			}
		}()
	}
	for deadline := time.Now().Add(5 * time.Second); inFlight.Load() < maxImageDownloads; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("only %d downloads started", inFlight.Load())
		}
	}
	time.Sleep(100 * time.Millisecond) // give any download over the cap time to show up
	if got := inFlight.Load(); got != maxImageDownloads {
		t.Errorf("in flight = %d, want %d", got, maxImageDownloads)
	}
	close(release)
	wg.Wait()
	if got := peak.Load(); got != maxImageDownloads {
		t.Errorf("peak = %d, want %d", got, maxImageDownloads)
	}
}

// A request that arrives after the last waiter left gets a fresh download, never the cancelled one.
func TestImagesRequestAfterAbandonedDownloadStartsAfresh(t *testing.T) {
	var hits atomic.Int32
	started := make(chan struct{}, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			started <- struct{}{}
			<-r.Context().Done()
			return
		}
		w.Write(pngHeader)
	}))
	defer srv.Close()
	im := newTestImages(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := im.Get(ctx, srv.URL+"/pic")
		done <- err
	}()
	<-started
	cancel()
	<-done
	if _, body, err := im.Get(context.Background(), srv.URL+"/pic"); err != nil || len(body) == 0 {
		t.Fatalf("Get after abandon: %d bytes, %v", len(body), err)
	}
	im.mu.Lock()
	left := len(im.flights)
	im.mu.Unlock()
	if left != 0 {
		t.Errorf("%d flights left in the map", left)
	}
}

// A request queued behind full slots stops waiting when its own context ends.
func TestImagesQueuedRequestHonorsItsContext(t *testing.T) {
	im := newTestImages(t)
	for range maxImageDownloads {
		im.slots <- struct{}{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := im.Get(ctx, "http://example.com/pic"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Get: %v", err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		im.mu.Lock()
		left := len(im.flights)
		im.mu.Unlock()
		if left == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the queued flight stayed in the map")
		}
	}
}
