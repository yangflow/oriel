package server

import (
	"context"
	"encoding/binary"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yangflow/oriel/internal/store"
	"github.com/yangflow/oriel/internal/store/storetest"
)

func TestPreMigrateCopy(t *testing.T) {
	file := storetest.Path(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	st, err := store.Open(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s, err := New(st, Config{Password: "secret", Version: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	token := login(t, s, "laptop")

	if w := do(s, "GET", "/api/storage", "", bearer(token)); decode(t, w)["premigrate"] != nil {
		t.Fatalf("no copy yet: %s", w.Body)
	}

	// A copy is a SQLite file, whose header keeps the schema version at byte 60.
	data := make([]byte, 4096)
	binary.BigEndian.PutUint32(data[60:], 17)
	if err := os.WriteFile(file+store.PreMigrateSuffix, data, 0o600); err != nil {
		t.Fatal(err)
	}
	w := do(s, "GET", "/api/storage", "", bearer(token))
	got, _ := decode(t, w)["premigrate"].(map[string]any)
	if got["bytes"] != 4096.0 || got["version"] != 17.0 || len(got) != 2 {
		t.Fatalf("copy: %s", w.Body)
	}
	if strings.Contains(w.Body.String(), filepath.Dir(file)) || strings.Contains(w.Body.String(), "pre-migrate") {
		t.Fatalf("the reply names a location: %s", w.Body)
	}

	if w := do(s, "DELETE", "/api/storage/premigrate", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("delete without session: %d", w.Code)
	}
	if _, err := os.Stat(file + store.PreMigrateSuffix); err != nil {
		t.Fatalf("an unauthenticated delete removed the copy: %v", err)
	}
	if w := do(s, "DELETE", "/api/storage/premigrate", "", bearer(token)); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(file + store.PreMigrateSuffix); !os.IsNotExist(err) {
		t.Fatalf("copy still there: %v", err)
	}
	if w := do(s, "GET", "/api/storage", "", bearer(token)); decode(t, w)["premigrate"] != nil {
		t.Fatalf("copy listed after delete: %s", w.Body)
	}
	if w := do(s, "DELETE", "/api/storage/premigrate", "", bearer(token)); w.Code != http.StatusNoContent {
		t.Fatalf("second delete: %d", w.Code)
	}
}
