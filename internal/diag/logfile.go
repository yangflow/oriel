package diag

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// The log file (docs/product.md, "日志文件"): LogName in the LogDir folder of the data directory, at most LogSize bytes
// each, LogFiles of them kept: LogName, LogName.1, LogName.2.
const (
	LogDir   = "logs"
	LogName  = "oriel.log"
	LogSize  = 5 << 20
	LogFiles = 3
)

const (
	// maxFileText is how many characters one value in the log file keeps, and maxStack how many a stack does.
	maxFileText = 1000
	maxStack    = 16 << 10
	// tailBytes and tailLines bound the part of the log file that the diagnostics file holds.
	tailBytes = 128 << 10
	tailLines = 300
)

// Rotator is the writer of the log file. It discards what it is given until Open succeeds, so the logger can be set up
// before the data directory is known.
type Rotator struct {
	mu   sync.Mutex
	max  int64
	path string
	file *os.File
	size int64
}

// NewRotator returns a closed rotator with the size cap of the log file.
func NewRotator() *Rotator { return &Rotator{max: LogSize} }

// Open creates the log folder in dataDir and opens the newest log file for appending.
func (r *Rotator) Open(dataDir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		r.file.Close()
		r.file = nil
	}
	dir := filepath.Join(dataDir, LogDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	r.path = filepath.Join(dir, LogName)
	return r.reopen(os.O_APPEND)
}

// reopen opens the log file with flag added to the flags that create and write it. The caller holds r.mu.
func (r *Rotator) reopen(flag int) error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|flag, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.file, r.size = f, info.Size()
	return nil
}

// Close closes the file; later writes are discarded.
func (r *Rotator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}

// Write appends one record, rotating first when it would not fit. A record longer than the cap is cut to it.
func (r *Rotator) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(p)
	if r.file == nil {
		return n, nil
	}
	if int64(len(p)) > r.max {
		p = append(p[:r.max-1:r.max-1], '\n')
	}
	if r.size > 0 && r.size+int64(len(p)) > r.max {
		r.rotate()
		if r.file == nil {
			return n, nil
		}
	}
	w, err := r.file.Write(p)
	r.size += int64(w)
	return n, err
}

// rotate renames the files up by one, dropping the oldest, and starts a new LogName. When a rename fails (another
// process holds the file on Windows), the file is cleared instead, which costs old records and never the cap; when
// even that fails the log stops. The caller holds r.mu.
func (r *Rotator) rotate() {
	r.file.Close()
	r.file = nil
	os.Remove(r.path + "." + strconv.Itoa(LogFiles-1))
	for i := LogFiles - 2; i >= 1; i-- {
		os.Rename(r.path+"."+strconv.Itoa(i), r.path+"."+strconv.Itoa(i+1))
	}
	os.Rename(r.path, r.path+".1")
	r.reopen(os.O_TRUNC)
}

// Tail returns the last lines of the newest log file in dataDir, oldest first, each redacted again. A data directory
// without a log file has none.
func Tail(dataDir string) ([]string, error) {
	if dataDir == "" {
		return nil, nil
	}
	f, err := os.Open(filepath.Join(dataDir, LogDir, LogName))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	offset := max(info.Size()-tailBytes, 0)
	data := make([]byte, info.Size()-offset)
	if n, err := f.ReadAt(data, offset); err != nil && err != io.EOF {
		return nil, err
	} else {
		data = data[:n]
	}
	if offset > 0 {
		// The cut falls inside a line: drop it.
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		} else {
			data = nil
		}
	}
	var lines []string
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(line) > 0 {
			lines = append(lines, redact(string(line), maxStack+maxFileText))
		}
	}
	if len(lines) > tailLines {
		lines = lines[len(lines)-tailLines:]
	}
	return lines, nil
}

// fileHandler hands redacted records to the handler of the log file: addresses cut to scheme and host, the home
// directory as "~", and no attribute whose name looks like a secret.
type fileHandler struct {
	next  slog.Handler
	group string // the names of the groups opened so far, each with a trailing dot
}

// NewFileHandler writes records as JSON lines to w, redacted the way the diagnostics file is.
func NewFileHandler(w io.Writer) slog.Handler {
	return &fileHandler{next: slog.NewJSONHandler(w, &slog.HandlerOptions{ReplaceAttr: UTC})}
}

// UTC writes the time of a record in UTC. It is the ReplaceAttr of the JSON handlers.
func UTC(_ []string, attr slog.Attr) slog.Attr {
	if attr.Key == slog.TimeKey {
		return slog.Time(attr.Key, attr.Value.Time().UTC())
	}
	return attr
}

func (h *fileHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *fileHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, redact(r.Message, maxFileText), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		if a, ok := h.clean(a, h.group); ok {
			out.AddAttrs(a)
		}
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h *fileHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var kept []slog.Attr
	for _, a := range attrs {
		if a, ok := h.clean(a, h.group); ok {
			kept = append(kept, a)
		}
	}
	return &fileHandler{next: h.next.WithAttrs(kept), group: h.group}
}

func (h *fileHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &fileHandler{next: h.next.WithGroup(name), group: h.group + name + "."}
}

// clean redacts the value of a, and reports false for an attribute that must not be written. prefix is the groups
// above it.
func (h *fileHandler) clean(a slog.Attr, prefix string) (slog.Attr, bool) {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() != slog.KindGroup && a.Key == "" || Sensitive(prefix+a.Key) {
		return slog.Attr{}, false
	}
	switch a.Value.Kind() {
	case slog.KindGroup:
		var members []slog.Attr
		for _, m := range a.Value.Group() {
			if m, ok := h.clean(m, prefix+a.Key+"."); ok {
				members = append(members, m)
			}
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(members...)}, len(members) > 0
	case slog.KindString, slog.KindAny:
		limit := maxFileText
		if a.Key == "stack" {
			limit = maxStack
		}
		var text string
		switch v := a.Value.Any().(type) {
		case string:
			text = v
		case error:
			text = v.Error()
		default:
			text = fmt.Sprint(v)
		}
		return slog.String(a.Key, redact(text, limit)), true
	}
	return a, true
}
