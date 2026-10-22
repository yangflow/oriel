package diag

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

func TestRedact(t *testing.T) {
	for in, want := range map[string]string{
		`Get "https://user:pw@feeds.example.com:8443/a/b?token=abc#x": EOF`: `Get "https://feeds.example.com:8443": EOF`,
		"see http://host/path and https://other.example/q?a=b":              "see http://host and https://other.example",
		"retry with api_key=abc123 now":                                     "retry with api_key=[redacted] now",
		"header Authorization: Bearer abc.def-ghi":                          "header Authorization: Bearer [redacted]",
		"plain text": "plain text",
	} {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
	long := Redact(strings.Repeat("あ", 400))
	if n := len([]rune(long)); n != maxText+1 || !strings.HasSuffix(long, "…") {
		t.Errorf("long text has %d characters", n)
	}
}

func TestHandler(t *testing.T) {
	var out bytes.Buffer
	h := NewHandler(slog.NewTextHandler(&out, nil), 3)
	log := slog.New(h).With("feed", "https://a.example/feed?token=x").WithGroup("req")
	log.Info("not kept", "data", "/home/someone")
	log.Warn("first", "Session_Token", "t0k3n", "password", "pw", "url", "https://b.example/p?q=1", "error", errors.New("boom https://c.example/x?y=1"))
	for i := range 3 {
		log.Error("again", "n", i)
	}
	lines := h.Recent()
	if len(lines) != 3 || strings.Contains(strings.Join(lines, "\n"), "first") {
		t.Fatalf("the ring keeps %d lines: %q", len(lines), lines)
	}
	if !strings.Contains(out.String(), "not kept") || !strings.Contains(out.String(), "t0k3n") {
		t.Fatalf("the wrapped handler lost its records: %s", out.String())
	}
	log.Warn("x", "Session_Token", "t0k3n", "password", "pw", "url", "https://b.example/p?q=1", "error", errors.New("boom https://c.example/x?y=1"))
	last := h.Recent()[2]
	for _, secret := range []string{"t0k3n", "pw", "token=x", "/p?q=1", "/x?y=1", "someone"} {
		if strings.Contains(last, secret) {
			t.Errorf("line has %q: %s", secret, last)
		}
	}
	for _, want := range []string{"WARN x", "req.url=https://b.example", "req.error=", "boom https://c.example", "feed=https://a.example"} {
		if !strings.Contains(last, want) {
			t.Errorf("line lacks %q: %s", want, last)
		}
	}
}

func TestBuildFromDataDirectory(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "oriel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	at := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	if _, err := st.CreateSource(ctx, store.NewSource{RSSHubPath: "/github/issue/golang/go?key=abc", Kind: "article",
		FetchInterval: time.Hour}, "", at); err != nil {
		t.Fatal(err)
	}
	body, err := Build(ctx, st, Info{Version: "1.0.0", Mode: ModeDataDir, Now: at})
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"Generated: 2026-10-01T08:00:00Z", "Mode: data directory", "Sources: 1", "not available", "host=rsshub:/github "} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "golang") || strings.Contains(text, "key=abc") {
		t.Errorf("the route leaks:\n%s", text)
	}

	quiet := NewHandler(slog.NewTextHandler(&bytes.Buffer{}, nil), Keep)
	body, _ = Build(ctx, st, Info{Version: "1.0.0", Mode: ModeDesktop, Now: at, Log: quiet})
	if !strings.Contains(string(body), "\nnone\n") {
		t.Errorf("an empty log is not said:\n%s", body)
	}
}
