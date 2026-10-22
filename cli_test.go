package main

import (
	"bytes"
	"context"
	"io"
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
