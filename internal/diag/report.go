package diag

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

// Modes of the program that writes the file.
const (
	ModeDesktop  = "desktop"
	ModeHeadless = "headless"
	ModeDataDir  = "data directory"
)

const (
	timeLayout   = "2006-01-02T15:04:05Z"
	noErrors     = "none"
	notAvailable = "not available: this file was written from a data directory, not from a running server"
)

// Info is what the file says besides the database.
type Info struct {
	Version string
	Mode    string
	// Log holds the recent errors of the running server; nil for a file written from a data directory.
	Log *Handler
	Now time.Time
}

// Build writes the diagnostics file. It reads only counts and the failure state of each subscription; titles, entries,
// settings, sessions and the AI key are never read.
func Build(ctx context.Context, st *store.Store, info Info) ([]byte, error) {
	storage, err := st.Storage(ctx)
	if err != nil {
		return nil, err
	}
	sources, err := st.Sources(ctx)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(sources, func(a, b store.Source) int { return int(a.ID - b.ID) })

	var b bytes.Buffer
	fmt.Fprintf(&b, "Oriel diagnostics\nGenerated: %s\nVersion: %s\nOS: %s/%s\nMode: %s\n\n",
		info.Now.UTC().Format(timeLayout), Redact(info.Version), runtime.GOOS, runtime.GOARCH, info.Mode)
	fmt.Fprintf(&b, "Data\nDatabase bytes: %d\nSources: %d\nEntries: %d\n\n", storage.DatabaseBytes, len(sources), storage.Entries)

	fmt.Fprintf(&b, "Recent errors (warnings and errors since the server started, at most %d)\n", Keep)
	if info.Log == nil {
		b.WriteString(notAvailable + "\n")
	} else if lines := info.Log.Recent(); len(lines) == 0 {
		b.WriteString(noErrors + "\n")
	} else {
		for _, line := range lines {
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\nSources\n")
	for _, src := range sources {
		success := "never"
		if !src.LastSuccessAt.IsZero() {
			success = src.LastSuccessAt.UTC().Format(timeLayout)
		}
		fmt.Fprintf(&b, "id=%d host=%s paused=%t consecutive_failures=%d last_success_at=%s", src.ID, host(src), src.Paused,
			src.ConsecutiveFailures, success)
		if src.LastError != "" {
			b.WriteString(" last_error=" + strconv.Quote(strings.NewReplacer("\n", " ", "\r", " ").Replace(Redact(src.LastError))))
		}
		b.WriteString("\n")
	}
	return b.Bytes(), nil
}

// host is the only thing the file says about where a subscription comes from: the host of its address, or "rsshub:"
// and the first part of its route.
func host(src store.Source) string {
	if src.RSSHubPath != "" {
		first, _, _ := strings.Cut(strings.TrimPrefix(src.RSSHubPath, "/"), "/")
		return "rsshub:/" + Redact(first)
	}
	u, err := url.Parse(src.URL)
	if err != nil || u.Host == "" {
		return "unknown"
	}
	return strings.ToLower(u.Host)
}
