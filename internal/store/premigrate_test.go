package store

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// writeOld replaces the database at file with one at the schema before the latest migration, so that opening it has
// one migration to run, and saves the marker in settings.
func writeOld(t *testing.T, file, marker string) (old int) {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(file + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	old = len(names) - 1
	for _, name := range names[:old] {
		body, err := migrations.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, stmt := range []string{"PRAGMA user_version = " + strconv.Itoa(old), "INSERT INTO settings (key, value) VALUES ('marker', '" + marker + "')"} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	return old
}

// copyState opens the pre-migration copy on its own and returns its schema version and marker.
func copyState(t *testing.T, file string) (version int, marker string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(file)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT value FROM settings WHERE key = 'marker'").Scan(&marker); err != nil {
		t.Fatal(err)
	}
	return version, marker
}

func copies(t *testing.T, dir string) []string {
	t.Helper()
	found, err := filepath.Glob(filepath.Join(dir, "oriel.sqlite.pre-migrate*"))
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestOpenCopiesTheDatabaseBeforePendingMigrations(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "oriel.sqlite")

	s, err := Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	if got := copies(t, dir); len(got) != 0 {
		t.Fatalf("a new database got a copy: %v", got)
	}
	s.Close()
	s, err = Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	if got := copies(t, dir); len(got) != 0 {
		t.Fatalf("a current database got a copy: %v", got)
	}

	s.Close()
	old := writeOld(t, file, "first")
	s, err = Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	copyFile := file + PreMigrateSuffix
	if version, marker := copyState(t, copyFile); version != old || marker != "first" {
		t.Fatalf("copy is at version %d with marker %q", version, marker)
	}
	var now int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&now); err != nil || now <= old {
		t.Fatalf("database version %d, %v", now, err)
	}
	if info, err := os.Stat(copyFile); err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("copy mode: %v, %v", info, err)
	}

	// Opening it again, now current, leaves the copy as it was.
	s.Close()
	s, err = Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	if _, marker := copyState(t, copyFile); marker != "first" {
		t.Fatalf("marker = %q", marker)
	}

	// The next upgrade replaces it, and nothing else is left in the directory.
	s.Close()
	writeOld(t, file, "second")
	s, err = Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := copies(t, dir); len(got) != 1 || got[0] != copyFile {
		t.Fatalf("copies = %v", got)
	}
	if version, marker := copyState(t, copyFile); version != old || marker != "second" {
		t.Fatalf("copy is at version %d with marker %q", version, marker)
	}
	if entries, _ := filepath.Glob(filepath.Join(dir, scratchPrefix+"*")); len(entries) != 0 {
		t.Fatalf("scratch left: %v", entries)
	}
}

func TestOpenDoesNotMigrateWhenTheCopyFails(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "oriel.sqlite")
	s, err := Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	old := writeOld(t, file, "old")
	// A non-empty directory where the copy goes makes the rename fail.
	if err := os.Mkdir(file+PreMigrateSuffix, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(file+PreMigrateSuffix, "keep"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, file)
	if err == nil {
		s.Close()
		t.Fatal("Open migrated although the copy failed")
	}
	if strings.Contains(err.Error(), dir) {
		t.Fatalf("the error carries the local path: %v", err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(file)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != old {
		t.Fatalf("version = %d, %v", version, err)
	}
}

func TestPreMigrateInfoAndDelete(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "oriel.sqlite")
	s, err := Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.PreMigrateInfo(); got != nil || err != nil {
		t.Fatalf("new database: %v, %v", got, err)
	}
	s.Close()

	old := writeOld(t, file, "x")
	if s, err = Open(ctx, file); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	info, err := s.PreMigrateInfo()
	stat, statErr := os.Stat(file + PreMigrateSuffix)
	if err != nil || statErr != nil || info == nil || info.Version != old || info.Bytes != stat.Size() {
		t.Fatalf("info = %+v, %v (version %d)", info, err, old)
	}
	if err := s.DeletePreMigrate(); err != nil {
		t.Fatal(err)
	}
	if got, err := s.PreMigrateInfo(); got != nil || err != nil {
		t.Fatalf("after delete: %v, %v", got, err)
	}
	if err := s.DeletePreMigrate(); err != nil {
		t.Fatalf("second delete: %v", err)
	}

	// A directory of that name is not a copy: not reported, not removed.
	if err := os.Mkdir(file+PreMigrateSuffix, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := s.PreMigrateInfo(); got != nil || err != nil {
		t.Fatalf("directory: %v, %v", got, err)
	}
	if err := s.DeletePreMigrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file + PreMigrateSuffix); err != nil {
		t.Fatalf("directory removed: %v", err)
	}
}
