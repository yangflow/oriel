package settings

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

func TestBackUp(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	s.group = opml
	f.backup = []byte("backup bytes")
	f.save = filepath.Join(t.TempDir(), "mine.oriel-backup")
	tt.Frame()
	if err := tt.Click(s.text.BackupButton); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	written, err := os.ReadFile(f.save)
	if err != nil || string(written) != "backup bytes" {
		t.Fatalf("written %q, %v", written, err)
	}
	if !tt.HasText("Backed up to mine.oriel-backup.") || s.busy {
		t.Fatalf("busy %v, texts %v", s.busy, tt.Texts())
	}
	if f.filters[len(f.filters)-1] != s.text.BackupFiles {
		t.Fatalf("filters %v", f.filters)
	}

	// A server that fails midway leaves no half file behind.
	f.backupErr = errors.New("offline")
	f.save = filepath.Join(t.TempDir(), "again.oriel-backup")
	if err := tt.Click(s.text.BackupButton); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if _, err := os.Stat(f.save); err == nil {
		t.Fatal("a failed backup left its file")
	}
	if !tt.HasText(s.text.BackupFailed) || s.busy {
		t.Fatalf("busy %v, texts %v", s.busy, tt.Texts())
	}

	// A disk that refuses the file is told apart from a server that failed.
	f.backupErr = nil
	f.save = filepath.Join(t.TempDir(), "missing", "x.oriel-backup")
	if err := tt.Click(s.text.BackupButton); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !tt.HasText("x.oriel-backup could not be saved.") {
		t.Fatalf("texts %v", tt.Texts())
	}

	// Cancelling the save dialog changes nothing.
	f.save = ""
	if err := tt.Click(s.text.BackupButton); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if s.busy || tt.HasText("Backed up") {
		t.Fatalf("busy %v, texts %v", s.busy, tt.Texts())
	}
}

func TestRestore(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	s.group = opml
	file := filepath.Join(t.TempDir(), "mine.oriel-backup")
	if err := os.WriteFile(file, []byte("zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.open = file
	f.restored = client.Restored{Sources: 18, Entries: 2406}
	tt.Frame()

	// Nothing is read before the answer, and Cancel leaves everything as it was.
	if err := tt.Click(s.text.RestoreButton); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !tt.HasText(s.text.RestoreTitle) {
		t.Fatalf("no dialog in %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	f.settle(tt)
	if len(f.restoreSent) != 0 || s.busy || f.refreshed != 0 {
		t.Fatalf("sent %d, busy %v, refreshed %d", len(f.restoreSent), s.busy, f.refreshed)
	}

	if err := tt.Click(s.text.RestoreButton); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if err := tt.Click(s.text.RestoreConfirm); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Restoring mine.oriel-backup…") {
		t.Fatalf("texts %v", tt.Texts())
	}
	f.settle(tt)
	if len(f.restoreSent) != 1 || string(f.restoreSent[0]) != "zip" || f.refreshed != 1 || s.busy {
		t.Fatalf("sent %q, refreshed %d, busy %v", f.restoreSent, f.refreshed, s.busy)
	}
	if !tt.HasText("Restored 18 subscriptions and 2406 entries.") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestRestoreRefused(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	s.group = opml
	file := filepath.Join(t.TempDir(), "notes.zip")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.open = file
	tt.Frame()
	for _, c := range []struct {
		err  error
		text string
	}{
		{&client.APIError{Status: 400, Code: "invalid_backup"}, "notes.zip is not an Oriel backup. Nothing was changed."},
		{&client.APIError{Status: 422, Code: "corrupt_backup"}, "notes.zip is damaged. Nothing was changed."},
		{&client.APIError{Status: 422, Code: "backup_newer"}, "notes.zip was made by a newer version of Oriel. Update Oriel first. Nothing was changed."},
		{&client.APIError{Status: 413, Code: "backup_too_large"}, "notes.zip is too large to restore. Nothing was changed."},
		{&client.APIError{Status: 409, Code: "restore_running"}, s.text.RestoreBusy},
		{errors.New("offline"), "Couldn’t restore notes.zip. Nothing was changed. Check the connection and try again."},
	} {
		f.restoreErr = c.err
		if err := tt.Click(s.text.RestoreButton); err != nil {
			t.Fatal(err)
		}
		f.settle(tt)
		if err := tt.Click(s.text.RestoreConfirm); err != nil {
			t.Fatal(err)
		}
		f.settle(tt)
		if !tt.HasText(c.text) || s.busy || f.refreshed != 0 {
			t.Fatalf("%v: busy %v, refreshed %d, texts %v", c.err, s.busy, f.refreshed, tt.Texts())
		}
	}

	// A file that cannot be read is not sent at all.
	f.open = filepath.Join(t.TempDir(), "gone.oriel-backup")
	n := len(f.restoreSent)
	if err := tt.Click(s.text.RestoreButton); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if err := tt.Click(s.text.RestoreConfirm); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if len(f.restoreSent) != n || !tt.HasText("gone.oriel-backup could not be read.") {
		t.Fatalf("sent %d, texts %v", len(f.restoreSent), tt.Texts())
	}
}
