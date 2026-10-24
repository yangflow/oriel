// Package storetest opens test databases from one migrated template instead of migrating an empty file in every
// test. Migrating through the SQLite driver costs seconds under the race detector. The template is what store.Open
// and Close leave behind on a new file, so a copy is exactly an up-to-date database and Open skips the backup and the
// migrations. A package's TestMain calls Run to remove the template; tests about opening, migrating or backing up
// keep calling store.Open on a new file.
package storetest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/yangflow/oriel/internal/store"
)

var (
	once     sync.Once
	dir      string
	template string
	buildErr error
)

// Run runs the tests of a package and removes the template afterwards: os.Exit(storetest.Run(m)).
func Run(m *testing.M) int {
	code := m.Run()
	if dir != "" {
		os.RemoveAll(dir)
	}
	return code
}

func build() {
	dir, buildErr = os.MkdirTemp("", "oriel-template-")
	if buildErr != nil {
		return
	}
	template = filepath.Join(dir, "oriel.sqlite")
	st, err := store.Open(context.Background(), template)
	if err != nil {
		buildErr = err
		return
	}
	if buildErr = st.Close(); buildErr != nil {
		return
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(template + suffix); err == nil {
			buildErr = fmt.Errorf("the template database left %s behind", suffix)
			return
		}
	}
}

// Path puts a copy of the migrated template at file, which must not exist yet, and returns file. Open the copy with
// store.Open.
func Path(tb testing.TB, file string) string {
	tb.Helper()
	once.Do(build)
	if buildErr != nil {
		tb.Fatal(buildErr)
	}
	data, err := os.ReadFile(template)
	if err != nil {
		tb.Fatal(err)
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		tb.Fatal(err)
	}
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		tb.Fatal(err)
	}
	return file
}

// Open opens a new migrated database in the test's temporary directory and closes it when the test ends.
func Open(tb testing.TB) *store.Store {
	tb.Helper()
	st, err := store.Open(context.Background(), Path(tb, filepath.Join(tb.TempDir(), "oriel.sqlite")))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { st.Close() })
	return st
}
