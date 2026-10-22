package settings

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

// openAuto shows the import and export group, which holds the automatic backup rows. A remote server is typed; the local
// one has the folder dialog.
func openAuto(t *testing.T, language string, local bool) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	s, f, tt := setup(t, language, true)
	if local {
		s.Use(f, "", true)
		s.Open()
	}
	tt.SetSize(1000, 1800)
	s.OpenImport()
	f.settle(tt)
	return s, f, tt
}

// toggleAuto clicks the switch, which sits just right of the label column, centered on the label and its help.
func toggleAuto(t *testing.T, s *Screen, tt *ui.Tester) {
	t.Helper()
	label, ok := tt.Find(s.text.AutoLabel)
	help, ok2 := tt.Find(s.text.AutoHelp)
	if !ok || !ok2 {
		t.Fatalf("no switch in %v", tt.Texts())
	}
	tt.ClickAt(label.X+label.W+20, (label.Y+help.Y+help.H)/2)
	tt.Frame()
}

func lastPatch(t *testing.T, f *fake) client.SettingsPatch {
	t.Helper()
	if len(f.set) == 0 {
		t.Fatal("nothing was saved")
	}
	return f.set[len(f.set)-1]
}

func TestAutoShowsRemote(t *testing.T) {
	s, _, tt := openAuto(t, "en", false)
	for _, text := range []string{s.text.AutoLabel, s.text.AutoFolder, s.text.AutoRemote, s.text.AutoFrequency, s.text.AutoDaily,
		s.text.AutoWeekly, s.text.AutoKeep, s.text.AutoNever} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
	if tt.HasText(s.text.AutoChoose) {
		t.Fatal("a folder chooser for a remote server")
	}
}

func TestAutoShowsLocal(t *testing.T) {
	s, _, tt := openAuto(t, "zh-Hans", true)
	for _, text := range []string{s.text.AutoLabel, s.text.AutoNoFolder, s.text.AutoChoose, s.text.AutoNever} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
	if tt.HasText(s.text.AutoRemote) {
		t.Fatal("the remote note on this computer")
	}
}

func TestAutoChooseFolder(t *testing.T) {
	s, f, tt := openAuto(t, "en", true)
	f.folder = "/var/backups/oriel"
	press(t, tt, s.text.AutoChoose)
	f.settle(tt)
	if f.folders != 1 || lastPatch(t, f).AutoBackupFolder == nil || *lastPatch(t, f).AutoBackupFolder != f.folder {
		t.Fatalf("dialog shown %d times, saved %v", f.folders, f.set)
	}
	if !tt.HasText(f.folder) || tt.HasText(s.text.AutoNoFolder) {
		t.Fatalf("texts %v", tt.Texts())
	}
	// Cancelling the dialog changes nothing.
	f.folder = ""
	saved := len(f.set)
	press(t, tt, s.text.AutoChoose)
	f.settle(tt)
	if len(f.set) != saved || !tt.HasText("/var/backups/oriel") {
		t.Fatalf("saved %v after cancelling", f.set)
	}
}

func TestAutoSwitch(t *testing.T) {
	s, f, tt := openAuto(t, "en", true)
	f.settings.AutoBackupFolder = "/var/backups/oriel"
	s.settings.AutoBackupFolder = "/var/backups/oriel"
	tt.Frame()
	toggleAuto(t, s, tt)
	f.settle(tt)
	if p := lastPatch(t, f); p.AutoBackupEnabled == nil || !*p.AutoBackupEnabled || !f.settings.AutoBackupEnabled {
		t.Fatalf("patch %+v, settings %+v", p, f.settings)
	}
	toggleAuto(t, s, tt)
	f.settle(tt)
	if p := lastPatch(t, f); p.AutoBackupEnabled == nil || *p.AutoBackupEnabled || f.settings.AutoBackupEnabled {
		t.Fatalf("patch %+v, settings %+v", p, f.settings)
	}
}

func TestAutoSwitchRefused(t *testing.T) {
	s, f, tt := openAuto(t, "en", true)
	f.setErr = &client.APIError{Status: 400, Code: "invalid_backup_folder"}
	toggleAuto(t, s, tt)
	f.settle(tt)
	if s.settings.AutoBackupEnabled || !tt.HasText(s.text.AutoNeedFolder) || !slices.Contains(tt.Announcements(), s.text.AutoNeedFolder) {
		t.Fatalf("enabled %v, texts %v", s.settings.AutoBackupEnabled, tt.Texts())
	}
}

func TestAutoFrequency(t *testing.T) {
	s, f, tt := openAuto(t, "en", true)
	press(t, tt, s.text.AutoWeekly)
	f.settle(tt)
	if p := lastPatch(t, f); p.AutoBackupFrequency == nil || *p.AutoBackupFrequency != "weekly" || f.settings.AutoBackupFrequency != "weekly" {
		t.Fatalf("patch %+v, settings %+v", p, f.settings)
	}
	press(t, tt, s.text.AutoDaily)
	f.settle(tt)
	if f.settings.AutoBackupFrequency != "daily" {
		t.Fatalf("frequency %q", f.settings.AutoBackupFrequency)
	}
}

func TestAutoKeep(t *testing.T) {
	s, f, tt := openAuto(t, "en", true)
	edit(tt, func() { s.autoKeep = "12" })
	s.commitAuto()
	f.settle(tt)
	if p := lastPatch(t, f); p.AutoBackupKeep == nil || *p.AutoBackupKeep != 12 || p.AutoBackupFolder != nil || f.settings.AutoBackupKeep != 12 {
		t.Fatalf("patch %+v, settings %+v", p, f.settings)
	}
	for _, bad := range []string{"0", "101", "many", ""} {
		saved := len(f.set)
		edit(tt, func() { s.autoKeep = bad })
		s.commitAuto()
		f.settle(tt)
		tt.Frame()
		if len(f.set) != saved || !tt.HasText(s.text.AutoInvalidKeep) {
			t.Fatalf("keep %q: saved %v, texts %v", bad, f.set, tt.Texts())
		}
	}
}

func TestAutoRemoteFolder(t *testing.T) {
	s, f, tt := openAuto(t, "en", false)
	edit(tt, func() { s.autoFolder = " /srv/oriel " })
	s.commitAuto()
	f.settle(tt)
	if p := lastPatch(t, f); p.AutoBackupFolder == nil || *p.AutoBackupFolder != "/srv/oriel" || p.AutoBackupKeep != nil {
		t.Fatalf("patch %+v", p)
	}
	// Turning it on with a folder typed but not yet saved saves both.
	edit(tt, func() { s.autoFolder = "/srv/other" })
	toggleAuto(t, s, tt)
	f.settle(tt)
	p := lastPatch(t, f)
	if p.AutoBackupEnabled == nil || !*p.AutoBackupEnabled || p.AutoBackupFolder == nil || *p.AutoBackupFolder != "/srv/other" {
		t.Fatalf("patch %+v", p)
	}
}

func TestAutoFolderRefused(t *testing.T) {
	for code, reason := range map[string]func(*Screen) string{
		"backup_folder_missing":       func(s *Screen) string { return s.text.AutoReasonMissing },
		"backup_folder_not_directory": func(s *Screen) string { return s.text.AutoReasonNotDir },
		"backup_folder_not_writable":  func(s *Screen) string { return s.text.AutoReasonNoWrite },
	} {
		s, f, tt := openAuto(t, "en", false)
		f.setErr = &client.APIError{Status: 400, Code: code}
		edit(tt, func() { s.autoFolder = "/nowhere" })
		s.commitAuto()
		f.settle(tt)
		tt.Frame()
		want := strings.ReplaceAll(s.text.AutoBadFolder, "{reason}", reason(s))
		if !tt.HasText(want) || s.autoFolder != "/nowhere" || s.settings.AutoBackupFolder != "" {
			t.Errorf("%s: want %q, folder %q, texts %v", code, want, s.autoFolder, tt.Texts())
		}
	}
	s, f, tt := openAuto(t, "en", false)
	f.setErr = &client.APIError{Status: 500, Code: "internal"}
	edit(tt, func() { s.autoFolder = "/x" })
	s.commitAuto()
	f.settle(tt)
	if !tt.HasText(s.text.AutoSaveFailed) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestAutoStatus(t *testing.T) {
	s, f, tt := openAuto(t, "en", true)
	f.settings.AutoBackupEnabled, f.settings.AutoBackupFolder = true, "/var/backups/oriel"
	f.settings.AutoBackupLastAt = now.Add(-3 * time.Hour).UnixMilli()
	f.settings.AutoBackupLastFile = "Oriel-2026-10-18-090000.oriel-backup"
	s.OpenImport()
	s.Open()
	f.settle(tt)
	s.OpenImport()
	f.settle(tt)
	if !containsText(tt, "Oriel-2026-10-18-090000.oriel-backup") {
		t.Fatalf("no last backup in %v", tt.Texts())
	}
	if containsText(tt, s.text.AutoNever) {
		t.Fatal("never, after a backup")
	}
	f.settings.AutoBackupFailedAt = now.UnixMilli()
	f.settings.AutoBackupError = "folder_missing"
	s.Open()
	f.settle(tt)
	s.OpenImport()
	f.settle(tt)
	want := strings.ReplaceAll(s.text.AutoFailed, "{reason}", s.text.AutoReasonMissing)
	if !containsText(tt, want) {
		t.Fatalf("no %q in %v", want, tt.Texts())
	}
}

// containsText reports whether any text shown contains part.
func containsText(tt *ui.Tester, part string) bool {
	return slices.ContainsFunc(tt.Texts(), func(text string) bool { return strings.Contains(text, part) })
}
