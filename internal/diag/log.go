package diag

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
)

// Keep is how many recent records the buffer holds.
const Keep = 200

// ring is the buffer that handlers made from one another share.
type ring struct {
	mu    sync.Mutex
	lines []string
	keep  int
}

func (r *ring) add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.lines) == r.keep {
		copy(r.lines, r.lines[1:])
		r.lines = r.lines[:len(r.lines)-1]
	}
	r.lines = append(r.lines, line)
}

// Handler passes records on to the handler it wraps and keeps the last Keep records at Warn and above in memory, as
// lines that were redacted when they were written. Info is not kept: it carries addresses and the data directory.
type Handler struct {
	next  slog.Handler
	ring  *ring
	attrs []slog.Attr // the attributes of WithAttrs, group prefix applied
	group string      // the prefix of WithGroup, with a trailing dot
}

// NewHandler wraps next and keeps the last keep records.
func NewHandler(next slog.Handler, keep int) *Handler {
	return &Handler{next: next, ring: &ring{keep: keep}}
}

// Recent returns the kept lines, oldest first.
func (h *Handler) Recent() []string {
	h.ring.mu.Lock()
	defer h.ring.mu.Unlock()
	return append([]string(nil), h.ring.lines...)
}

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn || h.next.Enabled(ctx, level)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		h.ring.add(h.line(r))
	}
	if !h.next.Enabled(ctx, r.Level) {
		return nil
	}
	return h.next.Handle(ctx, r)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := *h
	out.next = h.next.WithAttrs(attrs)
	out.attrs = append(append([]slog.Attr(nil), h.attrs...), prefixed(h.group, attrs)...)
	return &out
}

func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	out := *h
	out.next = h.next.WithGroup(name)
	out.group = h.group + name + "."
	return &out
}

func prefixed(group string, attrs []slog.Attr) []slog.Attr {
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		a.Key = group + a.Key
		out[i] = a
	}
	return out
}

// line is a record as one redacted line: time, level, message, then name=value for each attribute that is not
// sensitive.
func (h *Handler) line(r slog.Record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %s", r.Time.UTC().Format("2006-01-02T15:04:05Z"), r.Level, Redact(r.Message))
	for _, a := range h.attrs {
		writeAttr(&b, a)
	}
	r.Attrs(func(a slog.Attr) bool {
		a.Key = h.group + a.Key
		writeAttr(&b, a)
		return true
	})
	return strings.NewReplacer("\n", " ", "\r", " ").Replace(b.String())
}

func writeAttr(b *strings.Builder, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		for _, member := range a.Value.Group() {
			if a.Key != "" {
				member.Key = a.Key + "." + member.Key
			}
			writeAttr(b, member)
		}
		return
	}
	if a.Key == "" || Sensitive(a.Key) {
		return
	}
	text := a.Value.String()
	if err, ok := a.Value.Any().(error); ok && a.Value.Kind() == slog.KindAny {
		text = err.Error()
	}
	text = Redact(text)
	if text == "" || strings.ContainsAny(text, " \"=") {
		text = strconv.Quote(text)
	}
	b.WriteString(" " + a.Key + "=" + text)
}
