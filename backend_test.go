package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
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
	b, err := start(ctx, dir, "", diag.ModeHeadless, 0)
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

	if _, err := start(ctx, dir, "", diag.ModeHeadless, 0); !errors.Is(err, store.ErrLocked) {
		t.Fatalf("second backend on the same data: %v", err)
	}
	if err := b.close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := http.Get("http://" + b.addr + "/health"); err == nil {
		t.Fatal("still serving after close")
	}
	again, err := start(ctx, dir, "", diag.ModeHeadless, 0)
	if err != nil {
		t.Fatalf("restart after close: %v", err)
	}
	again.close(ctx)
}

func TestServerTimeouts(t *testing.T) {
	b, err := start(context.Background(), t.TempDir(), "", diag.ModeHeadless, 0)
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
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(release)
	b := &backend{store: st, background: &background{store: st}, addr: listener.Addr().String(), http: &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			select {
			case <-r.Context().Done():
				close(cancelled)
			case <-release:
			}
		}),
	}}
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
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the request was still running after close: its context was not cancelled")
	}
}
