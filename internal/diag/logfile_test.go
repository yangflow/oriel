package diag

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

func openRotator(t *testing.T, max int64) (*Rotator, string) {
	t.Helper()
	dir := t.TempDir()
	r := NewRotator()
	r.max = max
	if err := r.Open(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r, dir
}

func TestRotatorStaysWithinItsCap(t *testing.T) {
	r, dir := openRotator(t, 100)
	line := strings.Repeat("x", 39) + "\n" // 40 bytes: two fit in a file, the third rotates
	for range 20 {
		if _, err := r.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	logs := filepath.Join(dir, LogDir)
	entries, err := os.ReadDir(logs)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != LogFiles {
		t.Fatalf("%d files kept, want %d", len(entries), LogFiles)
	}
	for _, e := range entries {
		info, _ := e.Info()
		if info.Size() > 100 {
			t.Errorf("%s has %d bytes", e.Name(), info.Size())
		}
		if info.Mode().Perm() != 0o600 && os.PathSeparator == '/' {
			t.Errorf("%s has mode %v", e.Name(), info.Mode().Perm())
		}
	}
	for _, name := range []string{LogName, LogName + ".1", LogName + ".2"} {
		if _, err := os.Stat(filepath.Join(logs, name)); err != nil {
			t.Error(err)
		}
	}
}

func TestRotatorKeepsTheNewestRecordsInOrder(t *testing.T) {
	r, dir := openRotator(t, 30)
	for _, line := range []string{"one\n", "two\n", "three\n", "four\n", "five\n", "six\n", "seven\n", "eight\n", "nine\n", "ten\n"} {
		r.Write([]byte(line + strings.Repeat("-", 9) + "\n"))
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, LogDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if !strings.HasPrefix(read(LogName), "ten") && !strings.HasPrefix(read(LogName), "nine") {
		t.Errorf("the current file is %q", read(LogName))
	}
	if strings.Contains(read(LogName+".2"), "one") {
		t.Errorf("the oldest records were not dropped: %q", read(LogName+".2"))
	}
	if !strings.Contains(read(LogName+".1"), "eight") && !strings.Contains(read(LogName+".1"), "nine") {
		t.Errorf(".1 is %q", read(LogName+".1"))
	}
}

func TestRotatorAppendsAfterARestartAndCutsLongRecords(t *testing.T) {
	r, dir := openRotator(t, 100)
	r.Write([]byte("first\n"))
	r.Close()
	again := NewRotator()
	again.max = 100
	if err := again.Open(dir); err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	again.Write([]byte("second\n"))
	again.Write(bytes.Repeat([]byte("y"), 500))
	got, _ := os.ReadFile(filepath.Join(dir, LogDir, LogName))
	older, _ := os.ReadFile(filepath.Join(dir, LogDir, LogName+".1"))
	if string(older) != "first\nsecond\n" || !strings.HasPrefix(string(got), "yyy") {
		t.Errorf("log is %q after %q", got, older)
	}
	for _, name := range []string{LogName, LogName + ".1"} {
		if b, err := os.ReadFile(filepath.Join(dir, LogDir, name)); err == nil && len(b) > 100 {
			t.Errorf("%s has %d bytes", name, len(b))
		}
	}
}

func TestClosedRotatorDiscards(t *testing.T) {
	r := NewRotator()
	if n, err := r.Write([]byte("lost\n")); n != 5 || err != nil {
		t.Fatalf("Write = %d, %v", n, err)
	}
}

func TestFileHandlerRedacts(t *testing.T) {
	saved := home
	defer func() { home = saved }()
	home = filepath.Join(string(filepath.Separator)+"home", "someone")
	var out bytes.Buffer
	log := slog.New(NewFileHandler(&out)).With("api_key", "sk-123").WithGroup("req")
	log.Info("Fetched https://user:pw@feeds.example.com:8443/a/b?token=abc",
		"url", "https://b.example/p?q=1", "password", "pw", "n", 3,
		"error", errors.New("open "+filepath.Join(home, "Oriel", "x")+": denied token=t0k3n"),
		slog.Group("auth", slog.String("cookie", "c00kie"), slog.String("ok", "fine")))
	text := out.String()
	for _, secret := range []string{"sk-123", "user:pw", "/a/b", "token=abc", "/p?q=1", "pw\"", "someone", "t0k3n", "c00kie"} {
		if strings.Contains(text, secret) {
			t.Errorf("the log has %q: %s", secret, text)
		}
	}
	for _, want := range []string{`"level":"INFO"`, "https://feeds.example.com:8443", `"url":"https://b.example"`, `"n":3`, "~", `"ok":"fine"`, "token=[redacted]"} {
		if !strings.Contains(text, want) {
			t.Errorf("the log lacks %q: %s", want, text)
		}
	}
	if !strings.Contains(text, `Z"`) {
		t.Errorf("the time is not UTC: %s", text)
	}
}

func TestFileHandlerKeepsAWholeStack(t *testing.T) {
	var out bytes.Buffer
	stack := strings.Repeat("goroutine 1 [running]:\nmain.f()\n\t/src/main.go:1 +0x1\n", 40)
	slog.New(NewFileHandler(&out)).Error("Panic", "stack", stack, "other", strings.Repeat("z", 5000))
	text := out.String()
	if strings.Count(text, "main.f()") != 40 {
		t.Errorf("the stack lost frames: %d", strings.Count(text, "main.f()"))
	}
	if strings.Contains(text, strings.Repeat("z", maxFileText+1)) {
		t.Errorf("another value kept more than %d characters", maxFileText)
	}
}

func TestPanicIsLoggedWithItsStack(t *testing.T) {
	var out bytes.Buffer
	saved := slog.Default()
	defer slog.SetDefault(saved)
	slog.SetDefault(slog.New(NewFileHandler(&out)))
	if !Guard("test work", func() { panic("boom token=abc") }) {
		t.Fatal("the panic was not reported")
	}
	text := out.String()
	for _, want := range []string{`"msg":"Panic"`, `"where":"test work"`, "boom", "TestPanicIsLoggedWithItsStack", `"level":"ERROR"`} {
		if !strings.Contains(text, want) {
			t.Errorf("the record lacks %q: %s", want, text)
		}
	}
	if strings.Contains(text, "token=abc") {
		t.Errorf("the panic value is not redacted: %s", text)
	}
	if Guard("quiet", func() {}) {
		t.Error("work that did not panic was reported")
	}
	func() {
		defer Recover("deferred")
		panic("again")
	}()
	if !strings.Contains(out.String(), `"where":"deferred"`) {
		t.Errorf("Recover logged nothing: %s", out.String())
	}
}

func TestSuperviseRestartsAfterAPanic(t *testing.T) {
	var out bytes.Buffer
	saved := slog.Default()
	defer slog.SetDefault(saved)
	slog.SetDefault(slog.New(NewFileHandler(&out)))
	ctx, cancel := context.WithCancel(context.Background())
	runs := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		Supervise(ctx, "loop", func() {
			runs++
			if runs == 1 {
				cancel() // the restart wait ends at once, since ctx is over
				panic("first")
			}
		})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Supervise did not return once its context ended")
	}
	if runs != 1 || !strings.Contains(out.String(), "first") {
		t.Errorf("runs = %d, log %s", runs, out.String())
	}
	runs = 0
	Supervise(context.Background(), "once", func() { runs++ })
	if runs != 1 {
		t.Errorf("work that returns ran %d times", runs)
	}
}

func TestTailAndTheDiagnosticsFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "oriel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	at := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)

	if lines, err := Tail(dir); err != nil || lines != nil {
		t.Fatalf("Tail without a log = %q, %v", lines, err)
	}
	body, _ := Build(ctx, st, Info{Version: "1.0.0", Mode: ModeDataDir, DataDir: dir, Now: at})
	if !strings.Contains(string(body), "Log file (the last lines of logs/oriel.log, at most 300)\nnone\n") {
		t.Errorf("a missing log is not said:\n%s", body)
	}

	// A file that someone edited, or an older version wrote, may hold what the handler would have cut.
	if err := os.MkdirAll(filepath.Join(dir, LogDir), 0o700); err != nil {
		t.Fatal(err)
	}
	var written bytes.Buffer
	for i := range 400 {
		written.WriteString(`{"msg":"line ` + string(rune('a'+i%26)) + `","n":` + strings.Repeat("1", 1+i%3) + "}\n")
	}
	written.WriteString(`{"msg":"leak https://u:pw@h.example/p?x=1 api_key=sk-9 Bearer abc.def"}` + "\n")
	if err := os.WriteFile(filepath.Join(dir, LogDir, LogName), written.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := Tail(dir)
	if err != nil || len(lines) != tailLines {
		t.Fatalf("Tail = %d lines, %v", len(lines), err)
	}
	body, _ = Build(ctx, st, Info{Version: "1.0.0", Mode: ModeDataDir, DataDir: dir, Now: at})
	text := string(body)
	for _, secret := range []string{"u:pw", "/p?x=1", "sk-9", "abc.def"} {
		if strings.Contains(text, secret) {
			t.Errorf("the diagnostics file has %q:\n%s", secret, text)
		}
	}
	if !strings.Contains(text, "https://h.example") || !strings.Contains(text, "api_key=[redacted]") {
		t.Errorf("the log is not in the file:\n%s", text)
	}
}

func TestTailOfABigFileDropsTheCutLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, LogDir), 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"msg":"` + strings.Repeat("m", 90) + "\"}\n"
	if err := os.WriteFile(filepath.Join(dir, LogDir, LogName), []byte(strings.Repeat(line, 5000)), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := Tail(dir)
	if err != nil || len(lines) != tailLines {
		t.Fatalf("Tail = %d lines, %v", len(lines), err)
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, `{"msg":"`) {
			t.Fatalf("a cut line: %q", l)
		}
	}
}

func TestRotatorTruncatesWhenRenamingFails(t *testing.T) {
	r, dir := openRotator(t, 100)
	logs := filepath.Join(dir, LogDir)
	// Folders with content cannot be replaced by a rename, which stands in for a file another process holds open.
	for _, name := range []string{LogName + ".1", LogName + ".2"} {
		if err := os.MkdirAll(filepath.Join(logs, name, "x"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	line := strings.Repeat("x", 39) + "\n"
	for range 10 {
		if _, err := r.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(filepath.Join(logs, LogName))
	if err != nil || info.Size() == 0 || info.Size() > 100 {
		t.Errorf("the log after a failed rename: %v, %v", info, err)
	}
}

func TestRotatorWritesFromManyGoroutines(t *testing.T) {
	r, dir := openRotator(t, 1000)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				r.Write([]byte(strings.Repeat("y", 49) + "\n"))
			}
		})
	}
	wg.Wait()
	entries, _ := os.ReadDir(filepath.Join(dir, LogDir))
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, LogDir, e.Name()))
		if len(data) > 1000 || len(data)%50 != 0 {
			t.Errorf("%s has %d bytes, which is a cut or torn record", e.Name(), len(data))
		}
	}
}

func TestRedactWritesAWindowsHomeInSlashes(t *testing.T) {
	saved := home
	defer func() { home = saved }()
	home = `C:\Users\someone`
	if got := Redact("C:/Users/someone/go/pkg/mod/x.go and " + home + `\Oriel`); strings.Contains(got, "someone") {
		t.Errorf("the home directory stays: %s", got)
	}
}
