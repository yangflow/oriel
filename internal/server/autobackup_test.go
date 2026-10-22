package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/autobackup"
)

func TestAutoBackupSettingsDefaultsAndValidation(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	ctx := context.Background()
	body := decode(t, do(s, "GET", "/api/settings", "", bearer(token)))
	if body["autoBackupEnabled"] != false || body["autoBackupFolder"] != "" || body["autoBackupFrequency"] != "daily" ||
		body["autoBackupKeep"] != 7.0 || body["autoBackupLastAt"] != 0.0 || body["autoBackupLastFile"] != "" ||
		body["autoBackupFailedAt"] != 0.0 || body["autoBackupError"] != "" {
		t.Fatalf("defaults: %v", body)
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "plain")
	os.WriteFile(file, nil, 0o600)
	for _, c := range []struct{ patch, code string }{
		{`{"autoBackupFrequency":"hourly"}`, "invalid_backup_frequency"},
		{`{"autoBackupKeep":0}`, "invalid_backup_keep"},
		{`{"autoBackupKeep":101}`, "invalid_backup_keep"},
		{`{"autoBackupFolder":"relative/path"}`, "invalid_backup_folder"},
		{`{"autoBackupEnabled":true}`, "invalid_backup_folder"},
		{`{"autoBackupFolder":` + quote(filepath.Join(dir, "gone")) + `}`, "backup_folder_missing"},
		{`{"autoBackupFolder":` + quote(file) + `}`, "backup_folder_not_directory"},
		// One bad field and none of the request applies.
		{`{"autoBackupKeep":5,"autoBackupFrequency":"never"}`, "invalid_backup_frequency"},
	} {
		w := do(s, "PATCH", "/api/settings", c.patch, bearer(token))
		if w.Code != http.StatusBadRequest || decode(t, w)["error"] != c.code {
			t.Errorf("%s: %d %s, want %s", c.patch, w.Code, w.Body, c.code)
		}
	}
	if keep, _ := s.store.Setting(ctx, autobackup.KeepSetting); keep != "" {
		t.Fatalf("a refused request stored keep %q", keep)
	}

	w := do(s, "PATCH", "/api/settings", `{"autoBackupFolder":`+quote(" "+dir+" ")+`,"autoBackupFrequency":"weekly","autoBackupKeep":12}`, bearer(token))
	body = decode(t, w)
	if w.Code != http.StatusOK || body["autoBackupFolder"] != dir || body["autoBackupFrequency"] != "weekly" || body["autoBackupKeep"] != 12.0 ||
		body["autoBackupEnabled"] != false {
		t.Fatalf("patch: %d %v", w.Code, body)
	}
	if left, _ := os.ReadDir(dir); len(left) != 1 {
		t.Fatalf("the writable check left files in %s", dir)
	}
	// Turning it off is not checked: the folder may be gone by then.
	do(s, "PATCH", "/api/settings", `{"autoBackupEnabled":true}`, bearer(token))
	os.RemoveAll(dir)
	body = decode(t, do(s, "PATCH", "/api/settings", `{"autoBackupEnabled":false}`, bearer(token)))
	if body["autoBackupEnabled"] != false || body["autoBackupFolder"] != dir {
		t.Fatalf("off: %v", body)
	}
	if w := do(s, "PATCH", "/api/settings", `{"autoBackupEnabled":true}`, bearer(token)); decode(t, w)["error"] != "backup_folder_missing" {
		t.Fatalf("on with a gone folder: %s", w.Body)
	}
	// Clearing the folder while it is on is refused.
	dir = t.TempDir()
	do(s, "PATCH", "/api/settings", `{"autoBackupFolder":`+quote(dir)+`,"autoBackupEnabled":true}`, bearer(token))
	if w := do(s, "PATCH", "/api/settings", `{"autoBackupFolder":""}`, bearer(token)); decode(t, w)["error"] != "invalid_backup_folder" {
		t.Fatalf("clear while on: %s", w.Body)
	}
}

func TestAutoBackupStatusInSettings(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	ctx := context.Background()
	s.store.SetSetting(ctx, autobackup.LastAtSetting, "1700000000000")
	s.store.SetSetting(ctx, autobackup.LastFileSetting, "Oriel-2023-11-14-221320.oriel-backup")
	s.store.SetSetting(ctx, autobackup.FailedAtSetting, "1700003600000")
	s.store.SetSetting(ctx, autobackup.ErrorSetting, "disk_full")
	s.store.SetSetting(ctx, autobackup.KeepSetting, "junk")
	body := decode(t, do(s, "GET", "/api/settings", "", bearer(token)))
	if body["autoBackupLastAt"] != 1700000000000.0 || body["autoBackupLastFile"] != "Oriel-2023-11-14-221320.oriel-backup" ||
		body["autoBackupFailedAt"] != 1700003600000.0 || body["autoBackupError"] != "disk_full" || body["autoBackupKeep"] != 7.0 {
		t.Fatalf("status: %v", body)
	}
}

func TestChangingAutoBackupSettingsChecksNow(t *testing.T) {
	s, _ := newServer(t, "")
	s.auto = autobackup.New(s.store, "test")
	s.auto.Busy = s.maintenance
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.auto.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	dir := t.TempDir()
	if w := do(s, "PATCH", "/api/settings", `{"autoBackupFolder":`+quote(dir)+`,"autoBackupEnabled":true}`, nil); w.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", w.Code, w.Body)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		body := decode(t, do(s, "GET", "/api/settings", "", nil))
		name, _ := body["autoBackupLastFile"].(string)
		if strings.HasSuffix(name, ".oriel-backup") {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no backup: %v", body)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestMaintenanceStopsAutomaticBackups(t *testing.T) {
	s, _ := newServer(t, "")
	if s.maintenance() {
		t.Fatal("busy while idle")
	}
	s.restoring.Lock()
	if !s.maintenance() {
		t.Fatal("not busy during a restore")
	}
	s.restoring.Unlock()
	s.vacuumMu.Lock()
	s.vacuum.State = "running"
	s.vacuumMu.Unlock()
	if !s.maintenance() {
		t.Fatal("not busy during a vacuum")
	}
}

func quote(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}
