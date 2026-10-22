package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/ai"
	"github.com/yangflow/oriel/internal/store"
)

func TestDiagnosticsCommand(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "oriel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	src, err := st.CreateSource(ctx, store.NewSource{URL: "https://feeds.example.com/private/feed.xml?token=abc", Kind: "article",
		FetchInterval: time.Hour}, "My Private Diary", at)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FetchFailure(ctx, src.ID, `Get "https://feeds.example.com/private/feed.xml?token=abc": EOF`, at, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(ctx, ai.APIKeySetting, "sk-seeded-ai-key"); err != nil {
		t.Fatal(err)
	}

	// The data directory is in use, as when the desktop app is open: no file, and a hint.
	out := filepath.Join(t.TempDir(), "diagnostics.txt")
	if code := diagnostics([]string{"-data", dir, "-o", out}, new(bytes.Buffer)); code != 1 {
		t.Fatalf("exit code %d while the data directory is in use", code)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("a file was written")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	if code := diagnostics([]string{"-data", dir, "-o", out}, new(bytes.Buffer)); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(written)
	for _, secret := range []string{"sk-seeded-ai-key", "token=abc", "/private/feed.xml", "My Private Diary"} {
		if strings.Contains(text, secret) {
			t.Errorf("the file has %q:\n%s", secret, text)
		}
	}
	for _, want := range []string{"Mode: data directory", "not available", "host=feeds.example.com", "consecutive_failures=1"} {
		if !strings.Contains(text, want) {
			t.Errorf("the file lacks %q:\n%s", want, text)
		}
	}

	var stdout bytes.Buffer
	if code := diagnostics([]string{"-data", dir, "-o", "-"}, &stdout); code != 0 || !strings.HasPrefix(stdout.String(), "Oriel diagnostics\n") {
		t.Fatalf("standard output: %d %q", code, stdout.String())
	}
	if code := diagnostics([]string{"-data", dir}, new(bytes.Buffer)); code != 2 {
		t.Fatalf("exit code %d without -o", code)
	}
	if code := diagnostics([]string{"-data", t.TempDir(), "-o", out}, new(bytes.Buffer)); code != 1 {
		t.Fatalf("exit code %d without a database", code)
	}
}
