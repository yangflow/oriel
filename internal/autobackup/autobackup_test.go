package autobackup

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"
)

type fake struct {
	mu       sync.Mutex
	settings map[string]string
	fail     error
	partial  bool
	backups  int
}

func (f *fake) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.backups
}

func (f *fake) Setting(_ context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.settings[key], nil
}

func (f *fake) SetSetting(_ context.Context, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings[key] = value
	return nil
}

func (f *fake) Backup(_ context.Context, w io.Writer, _ string) error {
	f.mu.Lock()
	f.backups++
	fail, partial := f.fail, f.partial
	f.mu.Unlock()
	if partial {
		w.Write([]byte("half"))
	}
	if fail != nil {
		return fail
	}
	_, err := w.Write([]byte("backup"))
	return err
}

var start = time.Date(2026, 10, 22, 3, 0, 0, 0, time.Local)

func setup(t *testing.T, frequency string) (*Runner, *fake, string, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	f := &fake{settings: map[string]string{EnabledSetting: "1", FolderSetting: dir, FrequencySetting: frequency, KeepSetting: "3"}}
	clock := start
	r := New(f, "test")
	r.Now = func() time.Time { return clock }
	return r, f, dir, &clock
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestFirstBackupAndNaming(t *testing.T) {
	r, f, dir, _ := setup(t, "daily")
	r.Check(t.Context())
	want := "Oriel-2026-10-22-030000.oriel-backup"
	if got := names(t, dir); !slices.Equal(got, []string{want}) {
		t.Fatalf("files %v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, want)); string(b) != "backup" {
		t.Fatalf("content %q", b)
	}
	cfg, status, _ := Load(t.Context(), f)
	if !cfg.Enabled || status.LastFile != want || status.LastAt != start.UnixMilli() || status.FailedAt != 0 || status.Error != "" {
		t.Fatalf("status %+v", status)
	}
}

func TestDueDailyAndWeekly(t *testing.T) {
	for _, c := range []struct {
		frequency string
		after     time.Duration
		backups   int
	}{
		{"daily", 23 * time.Hour, 1},
		{"daily", 24 * time.Hour, 2},
		{"weekly", 6 * 24 * time.Hour, 1},
		{"weekly", 7 * 24 * time.Hour, 2},
		{"", 24 * time.Hour, 2},
	} {
		r, f, _, clock := setup(t, c.frequency)
		r.Check(t.Context())
		*clock = clock.Add(c.after)
		r.Check(t.Context())
		if f.backups != c.backups {
			t.Errorf("%s after %v: %d backups, want %d", c.frequency, c.after, f.backups, c.backups)
		}
	}
}

func TestOffAndBusyDoNothing(t *testing.T) {
	r, f, dir, _ := setup(t, "daily")
	f.settings[EnabledSetting] = ""
	r.Check(t.Context())
	f.settings[EnabledSetting] = "1"
	r.Busy = func() bool { return true }
	r.Check(t.Context())
	if len(names(t, dir)) != 0 || f.backups != 0 {
		t.Fatalf("backed up: %v", names(t, dir))
	}
	r.Busy = func() bool { return false }
	r.Check(t.Context())
	if f.backups != 1 {
		t.Fatalf("backups %d", f.backups)
	}
}

func TestFailureIsRecordedAndRetriedNextHour(t *testing.T) {
	r, f, dir, clock := setup(t, "daily")
	f.fail, f.partial = errors.New("boom"), true
	r.Check(t.Context())
	_, status, _ := Load(t.Context(), f)
	if status.FailedAt != start.UnixMilli() || status.Error != Failed || status.LastAt != 0 {
		t.Fatalf("status %+v", status)
	}
	if got := names(t, dir); len(got) != 0 {
		t.Fatalf("a failed backup left %v", got)
	}
	// Still failing at the next hourly check: one more try, not a loop.
	*clock = clock.Add(time.Hour)
	r.Check(t.Context())
	if f.backups != 2 {
		t.Fatalf("backups %d", f.backups)
	}
	f.fail, f.partial = nil, false
	*clock = clock.Add(time.Hour)
	r.Check(t.Context())
	_, status, _ = Load(t.Context(), f)
	if status.FailedAt != 0 || status.Error != "" || status.LastAt != clock.UnixMilli() || f.backups != 3 {
		t.Fatalf("status %+v", status)
	}
	if got := names(t, dir); len(got) != 1 || got[0] != "Oriel-2026-10-22-050000.oriel-backup" {
		t.Fatalf("files %v", got)
	}
}

func TestFailureCodes(t *testing.T) {
	r, f, dir, _ := setup(t, "daily")
	f.settings[FolderSetting] = filepath.Join(dir, "gone")
	r.Check(t.Context())
	if _, s, _ := Load(t.Context(), f); s.Error != FolderMissing {
		t.Fatalf("missing: %+v", s)
	}
	file := filepath.Join(dir, "plain")
	os.WriteFile(file, nil, 0o600)
	f.settings[FolderSetting] = file
	r.Check(t.Context())
	if _, s, _ := Load(t.Context(), f); s.Error != FolderNotDir {
		t.Fatalf("file: %+v", s)
	}
	f.settings[FolderSetting] = dir
	f.fail = errors.New("something else")
	r.Check(t.Context())
	if _, s, _ := Load(t.Context(), f); s.Error != Failed {
		t.Fatalf("other: %+v", s)
	}
	if classify(&os.PathError{Op: "write", Path: "x", Err: errors.New("x")}) != Failed || classify(os.ErrPermission) != FolderNotWritable {
		t.Fatal("classify")
	}
}

func TestRetentionKeepsTheNewestAndOnlyTouchesItsOwnFiles(t *testing.T) {
	r, _, dir, clock := setup(t, "daily")
	other := []string{"Oriel-2026-10-01.oriel-backup", "notes.txt", "Oriel-2026-10-01-0300.oriel-backup", "oriel-2026-10-01-030000.oriel-backup",
		"Oriel-2026-10-01-030000.oriel-backup.bak", ".oriel-backup-old.txt"}
	for _, name := range other {
		os.WriteFile(filepath.Join(dir, name), []byte("mine"), 0o600)
	}
	// A folder that looks like a backup is not a file of ours.
	os.Mkdir(filepath.Join(dir, "Oriel-2026-09-01-030000.oriel-backup"), 0o700)
	for range 5 {
		r.Check(t.Context())
		*clock = clock.Add(24 * time.Hour)
	}
	var kept []string
	for _, name := range names(t, dir) {
		if ours.MatchString(name) {
			kept = append(kept, name)
		}
	}
	want := []string{"Oriel-2026-09-01-030000.oriel-backup", "Oriel-2026-10-24-030000.oriel-backup", "Oriel-2026-10-25-030000.oriel-backup",
		"Oriel-2026-10-26-030000.oriel-backup"}
	if !slices.Equal(kept, want) {
		t.Fatalf("kept %v, want %v", kept, want)
	}
	for _, name := range other {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(b) != "mine" {
			t.Errorf("%s was touched: %v", name, err)
		}
	}
}

func TestStaleTempFilesAreRemovedFreshOnesKept(t *testing.T) {
	r, _, dir, _ := setup(t, "daily")
	stale, fresh := filepath.Join(dir, ".oriel-backup-111.tmp"), filepath.Join(dir, ".oriel-backup-222.tmp")
	os.WriteFile(stale, nil, 0o600)
	os.WriteFile(fresh, nil, 0o600)
	os.Chtimes(stale, start.Add(-2*time.Hour), start.Add(-2*time.Hour))
	os.Chtimes(fresh, start.Add(-time.Minute), start.Add(-time.Minute))
	r.Check(t.Context())
	got := names(t, dir)
	if slices.Contains(got, ".oriel-backup-111.tmp") || !slices.Contains(got, ".oriel-backup-222.tmp") {
		t.Fatalf("files %v", got)
	}
}

func TestCancelledBackupIsNotAFailure(t *testing.T) {
	r, f, dir, _ := setup(t, "daily")
	ctx, cancel := context.WithCancel(t.Context())
	f.fail, f.partial = context.Canceled, true
	cancel()
	r.Check(ctx)
	_, status, _ := Load(t.Context(), f)
	if status.FailedAt != 0 || len(names(t, dir)) != 0 {
		t.Fatalf("status %+v files %v", status, names(t, dir))
	}
}

func TestValidateFolder(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	os.WriteFile(file, nil, 0o600)
	for folder, want := range map[string]string{dir: "", filepath.Join(dir, "no"): FolderMissing, file: FolderNotDir} {
		if got := ValidateFolder(folder); got != want {
			t.Errorf("%s: %q, want %q", folder, got, want)
		}
	}
	if left := names(t, dir); !slices.Equal(left, []string{"f"}) {
		t.Fatalf("the check left %v", left)
	}
	// Windows ignores a directory's write bit, and root writes anywhere.
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		ro := filepath.Join(dir, "ro")
		os.Mkdir(ro, 0o500)
		if got := ValidateFolder(ro); got != FolderNotWritable {
			t.Errorf("read-only: %q", got)
		}
	}
}

func TestRunChecksAtStartAndWhenPoked(t *testing.T) {
	_, f, _, _ := setup(t, "daily")
	var mu sync.Mutex
	clock := start
	r := New(f, "test")
	r.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	wait := func(n int) {
		t.Helper()
		for range 500 {
			if f.count() >= n {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("no backup %d", n)
	}
	wait(1)
	mu.Lock()
	clock = clock.Add(48 * time.Hour)
	mu.Unlock()
	r.Poke()
	wait(2)
	cancel()
	<-done
}
