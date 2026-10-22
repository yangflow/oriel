package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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
