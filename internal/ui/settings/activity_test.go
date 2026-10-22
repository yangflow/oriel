package settings

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

func (f *fake) Activity(context.Context) (client.Activity, error) {
	f.reads++
	return f.activity, f.activityErr
}

func (f *fake) Storage(context.Context) (client.Storage, error) { return f.storage, f.storageErr }

func (f *fake) StartVacuum(context.Context) (client.Vacuum, error) {
	f.vacuums++
	return f.started, f.vacuumErr
}

func (f *fake) Diagnostics(context.Context) ([]byte, error) { return f.diag, f.diagErr }

// activityData is two subscriptions with attempts, one of them being fetched now.
func activityData() (client.Activity, client.Storage) {
	at := func(ago time.Duration) int64 { return now.Add(-ago).UnixMilli() }
	return client.Activity{
		Keep:     10,
		Fetching: []client.FetchingSource{{ID: 2, URL: "https://example.org/feed", Title: "Dispatch"}},
		Sources: []client.ActivitySource{
			{ID: 1, URL: "https://blog.example/feed", Title: "Blog", Attempts: []client.FetchAttempt{
				{At: at(2 * time.Minute), DurationMs: 1500, OK: true, Status: 200, NewEntries: 2},
				{At: at(3 * time.Hour), DurationMs: 90, Status: 503, Error: "status 503"},
			}},
			{ID: 2, RSSHubPath: "/dispatch/news", Attempts: []client.FetchAttempt{
				{At: at(10 * time.Minute), DurationMs: 25000, Error: "timeout waiting for the server"},
			}},
		},
	}, client.Storage{
		DatabaseBytes: 5 << 20, ReclaimableBytes: 1 << 20, Entries: 1234, Images: 3, ImageBytes: 2048, Copies: 7, CopyBytes: 3 << 20,
		Vacuum: client.Vacuum{State: client.VacuumIdle},
	}
}

// openActivity shows the Activity and storage group with the data above loaded.
func openActivity(t *testing.T, language string) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	s, f, tt := setup(t, language, false)
	f.activity, f.storage = activityData()
	tt.SetSize(1000, 1800)
	s.OpenPage("activity")
	tt.Frame()
	f.settle(tt)
	return s, f, tt
}

// later moves the clock on and draws, as the timer of the page does, and runs what that starts.
func (f *fake) after(tt *ui.Tester, d time.Duration) {
	f.later += d
	tt.Frame()
	f.settle(tt)
}

func TestActivityShows(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		s, f, tt := openActivity(t, language)
		if s.group != activity || f.reads != 1 {
			t.Fatalf("%s: group %d, %d reads", language, s.group, f.reads)
		}
		for _, text := range []string{
			s.text.ActivityLead, s.text.Refresh, s.text.Storage, s.text.DBSize, "5.0 MB", s.text.Entries, "1234", s.text.Images,
			s.fill(s.text.ImagesValue, "{n}", "3", "{size}", "2 KB"), s.text.Copies, s.fill(s.text.CopiesValue, "{n}", "7", "{size}", "3.0 MB"), s.text.Compact,
			s.fill(s.text.CompactHelp, "{size}", "1.0 MB"), s.text.FetchingNow, "Dispatch", s.text.Recent,
			"Blog", "/dispatch/news", s.fill(s.text.RecentKeep, "{n}", "10"),
			s.ago(now.Add(-2*time.Minute).UnixMilli(), now) + " · " + s.fill(s.text.NewCount, "{n}", "2"),
			s.ago(now.Add(-10*time.Minute).UnixMilli(), now) + " · " + s.text.FailedWord,
		} {
			if !tt.HasText(text) {
				t.Errorf("%s: no %q in %v", language, text, tt.Texts())
			}
		}
		if tt.HasText(s.text.FetchingNone) || tt.HasText(s.text.RecentNone) || tt.HasText("status 503") {
			t.Errorf("%s: shows what it should not: %v", language, tt.Texts())
		}
	}
}

func TestActivityOpensOneSourceAtATime(t *testing.T) {
	s, _, tt := openActivity(t, "en")
	blog := s.fill(s.text.ShowAttempts, "{name}", "Blog")
	route := s.fill(s.text.ShowAttempts, "{name}", "/dispatch/news")

	press(t, tt, blog)
	for _, text := range []string{"200", "1.5 s", "503", "90 ms", "status 503"} {
		if !tt.HasText(text) {
			t.Errorf("Blog open: no %q in %v", text, tt.Texts())
		}
	}
	if tt.HasText("timeout waiting for the server") {
		t.Errorf("a closed source shows its attempts")
	}

	press(t, tt, route)
	if !tt.HasText("timeout waiting for the server") || !tt.HasText("25.0 s") || !tt.HasText("–") {
		t.Errorf("route open: %v", tt.Texts())
	}
	if tt.HasText("status 503") {
		t.Errorf("two sources are open: %v", tt.Texts())
	}

	press(t, tt, route)
	if tt.HasText("timeout waiting for the server") {
		t.Errorf("the source did not close")
	}
}

func TestActivityNothingYet(t *testing.T) {
	s, f, tt := setup(t, "en", false)
	f.storage = client.Storage{DatabaseBytes: 4096, Vacuum: client.Vacuum{State: client.VacuumIdle}}
	f.activity = client.Activity{Keep: 10}
	s.OpenPage("activity")
	tt.Frame()
	f.settle(tt)
	for _, text := range []string{s.text.FetchingNone, s.text.RecentNone} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
	if tt.HasText(s.fill(s.text.RecentKeep, "{n}", "10")) {
		t.Errorf("the keep note shows without a list")
	}
}

func TestActivityFromTheSidebar(t *testing.T) {
	s, f, tt := setup(t, "en", false)
	f.activity, f.storage = activityData()
	press(t, tt, s.text.Activity)
	f.settle(tt)
	if s.group != activity || f.reads != 1 || !tt.HasText(s.text.Storage) {
		t.Fatalf("group %d, %d reads, texts %v", s.group, f.reads, tt.Texts())
	}
}

func TestActivityIsReadOnlyWhileShown(t *testing.T) {
	s, f, tt := setup(t, "en", false)
	tt.Frame()
	f.after(tt, time.Hour)
	if f.reads != 0 {
		t.Fatalf("read %d times on another group", f.reads)
	}
	s.OpenPage("activity")
	tt.Frame()
	f.settle(tt)
	if f.reads != 1 {
		t.Fatalf("read %d times on opening", f.reads)
	}
	f.after(tt, activityEvery-time.Second)
	if f.reads != 1 {
		t.Fatalf("read again after %v: %d", activityEvery-time.Second, f.reads)
	}
	f.after(tt, time.Second)
	if f.reads != 2 {
		t.Fatalf("not read again after %v: %d", activityEvery, f.reads)
	}

	f.activity.Fetching = nil
	f.activity.Sources = nil
	press(t, tt, s.text.Refresh)
	f.settle(tt)
	if f.reads != 3 || !tt.HasText(s.text.RecentNone) {
		t.Errorf("Refresh: %d reads, texts %v", f.reads, tt.Texts())
	}

	press(t, tt, s.text.Appearance)
	f.settle(tt)
	f.after(tt, time.Hour)
	if f.reads != 3 {
		t.Errorf("read on another group: %d", f.reads)
	}
}

func TestActivityDropsAStaleRead(t *testing.T) {
	s, f, tt := openActivity(t, "en")
	f.storage.Entries = 1111
	press(t, tt, s.text.Refresh)
	old := f.queue[0]
	f.queue = nil
	stale := old()

	f.storage.Entries = 2222
	press(t, tt, s.text.Refresh)
	stale()
	tt.Frame()
	if tt.HasText("1111") {
		t.Fatalf("a read passed by a newer one was shown")
	}
	f.settle(tt)
	if !tt.HasText("2222") {
		t.Errorf("the newer read was not shown: %v", tt.Texts())
	}
}

func TestActivityFailure(t *testing.T) {
	s, f, tt := setup(t, "en", false)
	f.activity, f.storage = activityData()
	f.activityErr = errors.New("down")
	s.OpenPage("activity")
	tt.Frame()
	f.settle(tt)
	if !tt.HasText(s.text.Error) || tt.HasText(s.text.Storage) {
		t.Fatalf("failed load: %v", tt.Texts())
	}
	f.after(tt, time.Hour)
	if f.reads != 1 {
		t.Errorf("a failed page read again by itself: %d", f.reads)
	}

	f.activityErr = nil
	press(t, tt, s.text.Retry)
	f.settle(tt)
	if !tt.HasText(s.text.Storage) || tt.HasText(s.text.Error) {
		t.Fatalf("after Retry: %v", tt.Texts())
	}

	f.storageErr = errors.New("down")
	f.after(tt, activityEvery)
	if !tt.HasText(s.text.Storage) || !tt.HasText("Blog") || tt.HasText(s.text.Error) {
		t.Errorf("a failed refresh took away what was shown: %v", tt.Texts())
	}
}

func TestActivityExpired(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	f.activityErr = client.ErrUnauthorized
	s.OpenPage("activity")
	tt.Frame()
	f.settle(tt)
	if f.expired != 1 {
		t.Fatalf("expired %d times", f.expired)
	}
}

func TestCompactRunsInTheBackground(t *testing.T) {
	s, f, tt := openActivity(t, "en")
	f.started = client.Vacuum{State: client.VacuumRunning, StartedAt: now.UnixMilli()}
	press(t, tt, s.text.Compact)
	if !tt.HasText(s.text.Compacting) {
		t.Fatalf("starting: texts %v", tt.Texts())
	}
	f.settle(tt)
	if f.vacuums != 1 {
		t.Fatalf("%d requests", f.vacuums)
	}
	f.storage.Vacuum = f.started
	if !tt.HasText(s.text.Compacting) {
		t.Fatalf("running: %v", tt.Texts())
	}
	// The button is busy, so there is nothing to press again.
	if f.vacuums != 1 {
		t.Fatalf("started %d compactions", f.vacuums)
	}

	// While it runs the page looks every 2 seconds instead of 5.
	reads := f.reads
	f.after(tt, vacuumEvery-time.Second)
	if f.reads != reads {
		t.Fatalf("read after %v", vacuumEvery-time.Second)
	}
	f.after(tt, time.Second)
	if f.reads != reads+1 || !tt.HasText(s.text.Compacting) {
		t.Fatalf("running read: %d reads, texts %v", f.reads, tt.Texts())
	}

	f.storage.Vacuum = client.Vacuum{State: client.VacuumDone, FreedBytes: 1 << 20}
	f.storage.DatabaseBytes, f.storage.ReclaimableBytes = 4<<20, 0
	f.after(tt, vacuumEvery)
	if !tt.HasText(s.fill(s.text.Compacted, "{size}", "1.0 MB")) || !tt.HasText("4.0 MB") || tt.HasText(s.text.Compacting) {
		t.Fatalf("done: %v", tt.Texts())
	}
	reads = f.reads
	f.after(tt, vacuumEvery)
	if f.reads != reads {
		t.Errorf("a finished compaction is still polled every %v", vacuumEvery)
	}
}

func TestCompactFailures(t *testing.T) {
	s, f, tt := openActivity(t, "en")
	f.vacuumErr = errors.New("connection refused")
	press(t, tt, s.text.Compact)
	f.settle(tt)
	if !tt.HasText(s.fill(s.text.CompactFailed, "{reason}", "connection refused")) {
		t.Fatalf("a request that failed: %v", tt.Texts())
	}
	if f.vacuums != 1 {
		t.Fatalf("%d requests", f.vacuums)
	}
	press(t, tt, s.text.Compact)
	f.settle(tt)
	if f.vacuums != 2 {
		t.Errorf("Compact did not come back after a failure")
	}

	f.vacuumErr = nil
	f.started = client.Vacuum{State: client.VacuumRunning, StartedAt: now.UnixMilli()}
	f.storage.Vacuum = client.Vacuum{State: client.VacuumFailed, Error: "database or disk is full"}
	press(t, tt, s.text.Compact)
	f.settle(tt)
	f.after(tt, vacuumEvery)
	if !tt.HasText(s.fill(s.text.CompactFailed, "{reason}", "database or disk is full")) {
		t.Fatalf("a compaction that failed: %v", tt.Texts())
	}
}

func TestCompactStartedElsewhere(t *testing.T) {
	s, f, tt := openActivity(t, "en")
	f.vacuumErr = &client.APIError{Status: 409, Code: "vacuum_running"}
	f.storage.Vacuum = client.Vacuum{State: client.VacuumRunning, StartedAt: now.UnixMilli()}
	press(t, tt, s.text.Compact)
	f.settle(tt)
	f.settle(tt)
	if !tt.HasText(s.text.Compacting) || tt.HasText(s.fill(s.text.CompactFailed, "{reason}", "vacuum_running")) {
		t.Fatalf("a compaction of another device: %v", tt.Texts())
	}
	if f.reads < 2 {
		t.Errorf("the state was not read at once: %d reads", f.reads)
	}
}

func TestCompactRunningOnOpen(t *testing.T) {
	s, f, tt := setup(t, "en", false)
	f.activity, f.storage = activityData()
	f.storage.Vacuum = client.Vacuum{State: client.VacuumRunning, StartedAt: now.UnixMilli()}
	s.OpenPage("activity")
	tt.Frame()
	f.settle(tt)
	if !tt.HasText(s.text.Compacting) || f.vacuums != 0 {
		t.Fatalf("texts %v, %d requests", tt.Texts(), f.vacuums)
	}
}

func TestActivityFormats(t *testing.T) {
	s := New("en", Deps{})
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1 KB", 1536: "2 KB", 5 << 20: "5.0 MB",
		3<<30 + 1<<29: "3.5 GB", 2 << 40: "2.0 TB", 5 << 50: "5120.0 TB"} {
		if got := s.bytes(n); got != want {
			t.Errorf("bytes(%d) = %q, want %q", n, got, want)
		}
	}
	for ms, want := range map[int64]string{0: "0 ms", 999: "999 ms", 1000: "1.0 s", 25000: "25.0 s"} {
		if got := s.duration(ms); got != want {
			t.Errorf("duration(%d) = %q, want %q", ms, got, want)
		}
	}
	if got := sourceName("", "", "https://x.example/feed"); got != "https://x.example/feed" {
		t.Errorf("sourceName = %q", got)
	}
}

func TestExportDiagnostics(t *testing.T) {
	for language, want := range map[string]struct{ button, done, failed, unsaved string }{
		"en":      {"Export diagnostics…", "Exported to Oriel-diagnostics.txt.", "Couldn’t export. Check the connection and try again.", "Oriel-diagnostics.txt could not be saved."},
		"zh-Hans": {"导出诊断文件…", "已导出到 Oriel-diagnostics.txt。", "没能导出。检查一下连接再试。", "Oriel-diagnostics.txt 没能保存。"},
	} {
		t.Run(language, func(t *testing.T) {
			_, f, tt := openActivity(t, language)
			f.diag = []byte("Oriel diagnostics\n")
			dir := t.TempDir()
			f.save = filepath.Join(dir, "Oriel-diagnostics.txt")
			if err := tt.Click(want.button); err != nil {
				t.Fatal(err)
			}
			f.settle(tt)
			written, err := os.ReadFile(f.save)
			if err != nil || string(written) != string(f.diag) {
				t.Fatalf("written %q, %v", written, err)
			}
			if !tt.HasText(want.done) {
				t.Fatalf("texts %v", tt.Texts())
			}
			if len(f.filters) == 0 || f.names[len(f.names)-1] != "Oriel-diagnostics.txt" {
				t.Fatalf("save asked for %v", f.names)
			}

			f.save = filepath.Join(dir, "missing", "Oriel-diagnostics.txt")
			if err := tt.Click(want.button); err != nil {
				t.Fatal(err)
			}
			f.settle(tt)
			if !tt.HasText(want.unsaved) {
				t.Fatalf("texts %v", tt.Texts())
			}

			f.diagErr = errors.New("offline")
			if err := tt.Click(want.button); err != nil {
				t.Fatal(err)
			}
			f.settle(tt)
			if !tt.HasText(want.failed) {
				t.Fatalf("texts %v", tt.Texts())
			}

			f.diagErr, f.save = nil, ""
			if err := tt.Click(want.button); err != nil {
				t.Fatal(err)
			}
			f.settle(tt)
			if tt.HasText(want.failed) || tt.HasText(want.done) {
				t.Fatalf("a cancelled dialog shows a result: %v", tt.Texts())
			}
		})
	}
}

func TestExportDiagnosticsFromTheMenu(t *testing.T) {
	s, f, tt := setup(t, "en", false)
	f.activity, f.storage = activityData()
	f.diag, f.save = []byte("Oriel diagnostics\n"), filepath.Join(t.TempDir(), "d.txt")
	tt.SetSize(1000, 1800)
	s.OpenPage("activity")
	s.ExportDiagnostics()
	tt.Frame()
	f.settle(tt)
	if _, err := os.Stat(f.save); err != nil || !tt.HasText("Exported to d.txt.") {
		t.Fatalf("saved %v, texts %v", err, tt.Texts())
	}
	if DiagnosticsMenuLabel("zh-Hans-CN") != "导出诊断文件…" || DiagnosticsMenuLabel("fr") != "Export Diagnostics…" {
		t.Fatal("menu labels")
	}
}
