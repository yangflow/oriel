package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

func seed(t *testing.T, dir, url string) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(dir, databaseFile))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.CreateSource(context.Background(), store.NewSource{URL: url, Kind: "article", FetchInterval: time.Hour}, "", time.Now()); err != nil {
		t.Fatal(err)
	}
}

func sourceCount(t *testing.T, dir string) int {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(dir, databaseFile))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sources, err := st.Sources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return len(sources)
}

func run(command func(context.Context, []string, io.Writer, io.Writer) int, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := command(context.Background(), args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestBackupAndRestoreCommands(t *testing.T) {
	from, to := t.TempDir(), t.TempDir()
	seed(t, from, "https://example.org/feed")
	file := filepath.Join(t.TempDir(), "all.oriel-backup")

	if code, out, msg := run(backupCommand, "-data", from, "-o", file); code != 0 || !strings.Contains(out, file) {
		t.Fatalf("backup: %d %q %q", code, out, msg)
	}
	if code, _, msg := run(backupCommand, "-data", from, "-o", file); code != 1 || !strings.Contains(msg, "Cannot create") {
		t.Fatalf("second backup over the file: %d %q", code, msg)
	}

	if code, out, msg := run(restoreCommand, "-data", to, file); code != 0 || !strings.Contains(out, "Restored 1 sources") {
		t.Fatalf("restore: %d %q %q", code, out, msg)
	}
	if sourceCount(t, to) != 1 {
		t.Fatal("the restored data has no source")
	}

	// Data that is already there is only replaced with -force, and a failed restore leaves it alone.
	seed(t, to, "https://example.org/other")
	if code, _, msg := run(restoreCommand, "-data", to, file); code != 1 || !strings.Contains(msg, "-force") {
		t.Fatalf("restore over data: %d %q", code, msg)
	}
	if sourceCount(t, to) != 2 {
		t.Fatal("a refused restore changed the data")
	}
	bad := filepath.Join(t.TempDir(), "bad.oriel-backup")
	if err := os.WriteFile(bad, []byte("not a backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, msg := run(restoreCommand, "-data", to, "-force", bad); code != 1 || !strings.Contains(msg, "not an Oriel backup") {
		t.Fatalf("restore of text: %d %q", code, msg)
	}
	if sourceCount(t, to) != 2 {
		t.Fatal("a failed restore changed the data")
	}
	if code, _, msg := run(restoreCommand, "-data", to, "-force", file); code != 0 {
		t.Fatalf("forced restore: %d %q", code, msg)
	}
	if sourceCount(t, to) != 1 {
		t.Fatal("the forced restore did not replace the data")
	}
}

func TestBackupCommandNeedsAnOutputFile(t *testing.T) {
	if code, _, _ := run(backupCommand, "-data", t.TempDir()); code != 2 {
		t.Fatalf("code %d", code)
	}
	if code, _, _ := run(restoreCommand, "-data", t.TempDir()); code != 2 {
		t.Fatalf("code %d", code)
	}
	missing := filepath.Join(t.TempDir(), "none.oriel-backup")
	if code, _, _ := run(backupCommand, "-data", t.TempDir(), "-o", missing); code != 1 {
		t.Fatalf("code %d", code)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Fatal("a failed backup left its file behind")
	}
}

func TestRestoreCommandRefusesRunningData(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(dir, databaseFile))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if code, _, msg := run(restoreCommand, "-data", dir, filepath.Join(dir, "x")); code != 1 {
		t.Fatalf("%d %q", code, msg)
	}
	file := filepath.Join(t.TempDir(), "some")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, msg := run(restoreCommand, "-data", dir, file); code != 1 || !strings.Contains(msg, "Quit it first") {
		t.Fatalf("%d %q", code, msg)
	}
}

func TestHealthcheckCommand(t *testing.T) {
	reply := func(status int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(status)
			io.WriteString(w, body)
		}))
	}
	portOf := func(s *httptest.Server) string {
		_, port, _ := net.SplitHostPort(s.Listener.Addr().String())
		return port
	}

	up := reply(http.StatusOK, `{"app":"oriel","version":"1","auth":true,"database":"ok"}`)
	defer up.Close()
	if code, out, msg := run(healthcheckCommand, "-port", portOf(up)); code != 0 || out != "ok\n" || msg != "" {
		t.Fatalf("up: %d %q %q", code, out, msg)
	}

	unwritable := reply(http.StatusServiceUnavailable, `{"app":"oriel","database":"unwritable"}`)
	defer unwritable.Close()
	if code, _, msg := run(healthcheckCommand, "-port", portOf(unwritable)); code != 1 || !strings.Contains(msg, "database unwritable") {
		t.Fatalf("unwritable database: %d %q", code, msg)
	}

	other := reply(http.StatusOK, `<html>hello</html>`)
	defer other.Close()
	if code, _, msg := run(healthcheckCommand, "-port", portOf(other)); code != 1 || !strings.Contains(msg, "not from Oriel") {
		t.Fatalf("another program on the port: %d %q", code, msg)
	}

	down := reply(http.StatusOK, `{"app":"oriel"}`)
	port := portOf(down)
	down.Close()
	if code, _, msg := run(healthcheckCommand, "-port", port); code != 1 || !strings.Contains(msg, "no answer") {
		t.Fatalf("down: %d %q", code, msg)
	}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	if code, _, msg := run(healthcheckCommand, "-port", portOf(slow), "-timeout", "100ms"); code != 1 || !strings.Contains(msg, "no answer") {
		t.Fatalf("no reply in time: %d %q", code, msg)
	}

	for _, args := range [][]string{{"-port", "0"}, {"-port", "70000"}, {"-timeout", "0s"}, {"extra"}} {
		if code, _, _ := run(healthcheckCommand, args...); code != 2 {
			t.Fatalf("%v: %d, want 2", args, code)
		}
	}
}
