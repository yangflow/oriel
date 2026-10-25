package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/diag"
	"github.com/yangflow/oriel/internal/store"
)

func TestBackend(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b, err := start(ctx, dir, "", diag.ModeHeadless, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(b.addr, "127.0.0.1:") {
		t.Fatalf("listening on %s without a password", b.addr)
	}
	resp, err := http.Get("http://" + b.addr + "/health")
	if err != nil {
		t.Fatal(err)
	}
	var health struct{ App, Version string }
	json.NewDecoder(resp.Body).Decode(&health)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || health.App != "oriel" || health.Version != version || version == "" {
		t.Fatalf("health: %d %+v", resp.StatusCode, health)
	}

	if _, err := start(ctx, dir, "", diag.ModeHeadless, 0, nil); !errors.Is(err, store.ErrLocked) {
		t.Fatalf("second backend on the same data: %v", err)
	}
	if err := b.close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := http.Get("http://" + b.addr + "/health"); err == nil {
		t.Fatal("still serving after close")
	}
	again, err := start(ctx, dir, "", diag.ModeHeadless, 0, nil)
	if err != nil {
		t.Fatalf("restart after close: %v", err)
	}
	again.close(ctx)
}

func TestServerTimeouts(t *testing.T) {
	b, err := start(context.Background(), t.TempDir(), "", diag.ModeHeadless, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.close(context.Background())
	// The write timeout must outlast the slowest answer, an AI test call of up to two minutes.
	if b.http.ReadTimeout <= 0 || b.http.IdleTimeout <= 0 || b.http.WriteTimeout <= 2*time.Minute {
		t.Fatalf("read %v, write %v, idle %v", b.http.ReadTimeout, b.http.WriteTimeout, b.http.IdleTimeout)
	}
}

// A request that outlives the shutdown deadline is cut off, its context cancelled, before the store is closed.
func TestCloseCutsOffSlowRequestsBeforeClosingTheStore(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "oriel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		st.Close()
		t.Fatal(err)
	}
	started, cancelled := make(chan struct{}), make(chan error, 1)
	b := &backend{store: st, background: &background{store: st}, addr: listener.Addr().String(), http: &http.Server{}}
	b.http.Handler = b.track(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		// Still running after the deadline: the store must be open until this handler returns.
		time.Sleep(200 * time.Millisecond)
		_, err := st.Setting(ctx, "any")
		cancelled <- err
	}))
	go b.http.Serve(listener)
	go func() {
		if resp, err := http.Get("http://" + b.addr + "/"); err == nil {
			resp.Body.Close()
		}
	}()
	<-started

	deadline, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	defer stop()
	if err := b.close(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close with a request past the deadline: %v", err)
	}
	select {
	case err := <-cancelled:
		if err != nil {
			t.Fatalf("the store was closed under a running handler: %v", err)
		}
	default:
		t.Fatal("close returned while the handler was still running")
	}
}

// loginFrom sends a login the server counts as wrong without deriving a key, the way a reverse proxy would, with the client in X-Forwarded-For.
func loginFrom(t *testing.T, addr, client string) int {
	t.Helper()
	req, err := http.NewRequest("POST", "http://"+addr+"/auth/login", strings.NewReader("not json"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Forwarded-For", client)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestLoginThrottleBehindTrustedProxy(t *testing.T) {
	for _, trusted := range []bool{true, false} {
		var proxies []netip.Prefix
		if trusted {
			proxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
		}
		b, err := start(context.Background(), t.TempDir(), "secret", diag.ModeHeadless, 0, proxies)
		if err != nil {
			t.Fatal(err)
		}
		addr := "127.0.0.1" + b.addr[strings.LastIndex(b.addr, ":"):]
		for i := 0; i < 6; i++ {
			loginFrom(t, addr, "203.0.113.1")
		}
		if got := loginFrom(t, addr, "203.0.113.1"); got != http.StatusTooManyRequests {
			t.Errorf("trusted %v: the guessing client got %d", trusted, got)
		}
		other := loginFrom(t, addr, "203.0.113.2")
		if want := map[bool]int{true: http.StatusUnauthorized, false: http.StatusTooManyRequests}[trusted]; other != want {
			t.Errorf("trusted %v: another client got %d, want %d", trusted, other, want)
		}
		b.close(context.Background())
	}
}

func TestTrackLogsAPanicAndCutsTheConnection(t *testing.T) {
	var out bytes.Buffer
	saved := slog.Default()
	defer slog.SetDefault(saved)
	slog.SetDefault(slog.New(diag.NewFileHandler(&out)))
	b := &backend{}
	server := httptest.NewServer(b.track(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			return
		}
		panic("handler broke")
	})))
	defer server.Close()
	if _, err := http.Get(server.URL + "/boom"); err == nil {
		t.Error("the connection was not cut off")
	}
	if resp, err := http.Get(server.URL + "/ok"); err != nil {
		t.Errorf("the server did not go on: %v", err)
	} else {
		resp.Body.Close()
	}
	text := out.String()
	if !strings.Contains(text, "handler broke") || !strings.Contains(text, `"where":"request"`) || !strings.Contains(text, "stack") {
		t.Errorf("the panic was not logged with its stack: %s", text)
	}
	if b.running.Load() != 0 {
		t.Errorf("%d requests still counted", b.running.Load())
	}
}

// A background task that ignores its cancellation does not hold the shutdown past the deadline, and the database is
// closed anyway.
func TestCloseGivesUpOnHungBackgroundWork(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "oriel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	defer close(release)
	work := &background{store: st}
	work.group.Go(func() { <-release })
	b := &backend{store: st, background: work, http: &http.Server{}}

	deadline, stop := context.WithTimeout(ctx, 200*time.Millisecond)
	defer stop()
	begin := time.Now()
	b.close(deadline)
	if took := time.Since(begin); took > 2*time.Second {
		t.Fatalf("close took %v with a hung task", took)
	}
	if _, err := st.Setting(ctx, "any"); err == nil {
		t.Fatal("the store is still open")
	}
}
