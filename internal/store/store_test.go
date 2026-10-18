package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func open(t *testing.T, file string) *Store {
	t.Helper()
	s, err := Open(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrationsAreRepeatable(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "oriel.sqlite")
	open(t, file).Close()
	s := open(t, file)
	if runtime.GOOS != "windows" {
		for _, name := range []string{file, file + "-wal"} {
			if info, err := os.Stat(name); err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("%s: %v %v", filepath.Base(name), info.Mode(), err)
			}
		}
	}
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("user_version = %d", version)
	}
	var mode string
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v", mode, err)
	}
	if err := s.Writable(ctx); err != nil {
		t.Fatal(err)
	}
	var probes int
	s.db.QueryRowContext(ctx, "SELECT count(*) FROM settings").Scan(&probes)
	if probes != 0 {
		t.Fatal("the writability probe left a row behind")
	}
}

func TestNewerSchemaIsRejected(t *testing.T) {
	file := filepath.Join(t.TempDir(), "oriel.sqlite")
	s := open(t, file)
	if _, err := s.db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(context.Background(), file); err == nil {
		t.Fatal("opened a database from a newer build")
	}
}

func TestSchemaRules(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	exec := func(query string, args ...any) error {
		_, err := s.db.Exec(query, args...)
		return err
	}
	insertSource := "INSERT INTO sources (url, rsshub_path, kind, created_at, updated_at) VALUES (?, ?, ?, 1, 1)"
	if err := exec(insertSource, "https://example.org/feed", "", "article"); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]any{
		"duplicate feed":         {"https://example.org/feed", "", "article"},
		"fifth kind":             {"https://example.org/other", "", "link"},
		"url and rsshub":         {"https://example.org/both", "/x", "article"},
		"neither url nor rsshub": {"", "", "article"},
	} {
		if err := exec(insertSource, args...); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	insertEntry := "INSERT INTO entries (source_id, guid, title, search_text, images, created_at, updated_at) VALUES (1, ?, ?, ?, ?, 1, 1)"
	if err := exec(insertEntry, "a", "Native readers", "full text about trigrams", "[]"); err != nil {
		t.Fatal(err)
	}
	if err := exec(insertEntry, "a", "Duplicate", "", "[]"); err == nil {
		t.Error("duplicate guid was accepted")
	}
	if err := exec(insertEntry, "b", "Bad images", "", `{"url":"x"}`); err == nil {
		t.Error("images that are not an array were accepted")
	}

	match := func(query string) int {
		var n int
		if err := s.db.QueryRow("SELECT count(*) FROM entries_fts WHERE entries_fts MATCH ?", query).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if match("trigram") != 1 || match("reade") != 1 {
		t.Fatal("full-text index did not match inserted text")
	}
	if err := exec("UPDATE entries SET title = 'Renamed' WHERE guid = 'a'"); err != nil {
		t.Fatal(err)
	}
	if match("reade") != 0 || match("Renamed") != 1 {
		t.Fatal("full-text index did not follow the update")
	}
	if err := exec("DELETE FROM sources"); err != nil {
		t.Fatal(err)
	}
	if match("Renamed") != 0 {
		t.Fatal("deleting the source left its entries in the index")
	}
}

func TestSessions(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	start := time.UnixMilli(1_000_000)
	old, err := s.CreateSession(ctx, []byte("old"), "laptop", start)
	if err != nil {
		t.Fatal(err)
	}
	recent, err := s.CreateSession(ctx, []byte("recent"), "phone", start)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSession(ctx, []byte("recent"), "copy", start); err == nil {
		t.Fatal("two sessions share a token hash")
	}
	later := start.Add(time.Hour)
	if err := s.TouchSession(ctx, recent, later); err != nil {
		t.Fatal(err)
	}
	session, err := s.SessionByToken(ctx, []byte("recent"))
	if err != nil || session.ID != recent || !session.LastSeenAt.Equal(later) || session.UserAgent != "phone" {
		t.Fatalf("SessionByToken = %+v, %v", session, err)
	}
	listed, err := s.Sessions(ctx, start.Add(time.Minute))
	if err != nil || len(listed) != 1 || listed[0].ID != recent {
		t.Fatalf("Sessions = %+v, %v", listed, err)
	}
	if err := s.DeleteSessionsIdleSince(ctx, start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionByToken(ctx, []byte("old")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("idle session %d survived: %v", old, err)
	}
	if err := s.DeleteSession(ctx, recent); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSession(ctx, recent); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestOneProcessPerDatabase(t *testing.T) {
	file := filepath.Join(t.TempDir(), "oriel.sqlite")
	s := open(t, file)
	if _, err := Open(context.Background(), file); !errors.Is(err, ErrLocked) {
		t.Fatalf("second open: %v", err)
	}
	s.Close()
	open(t, file)
}

func TestSources(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	start := time.UnixMilli(1_000_000)
	blog, err := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "", start)
	if err != nil {
		t.Fatal(err)
	}
	if blog.FetchInterval != time.Hour || !blog.NextFetchAt.Equal(start) || !blog.LastFetchedAt.IsZero() || blog.UnreadCount != 0 {
		t.Fatalf("created %+v", blog)
	}
	if _, err := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "video", FetchInterval: time.Hour}, "", start); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate: %v", err)
	}
	hub, err := s.CreateSource(ctx, NewSource{RSSHubPath: "/github/issue/x", Kind: "social", FetchInterval: time.Hour}, "Alpha", start)
	if err != nil {
		t.Fatal(err)
	}
	for _, guid := range []string{"a", "b"} {
		if _, err := s.db.Exec("INSERT INTO entries (source_id, guid, created_at, updated_at) VALUES (?, ?, 1, 1)", hub.ID, guid); err != nil {
			t.Fatal(err)
		}
	}
	s.db.Exec("UPDATE entries SET is_read = 1 WHERE guid = 'a'")

	list, err := s.Sources(ctx)
	if err != nil || len(list) != 2 || list[0].ID != hub.ID || list[0].UnreadCount != 1 || list[1].ID != blog.ID {
		t.Fatalf("Sources = %+v, %v", list, err)
	}

	later := start.Add(time.Minute)
	title, kind, paused := "Renamed", "image", true
	updated, err := s.UpdateSource(ctx, blog.ID, SourcePatch{Title: &title, Kind: &kind, Paused: &paused}, later)
	if err != nil || updated.Title != title || updated.Kind != kind || !updated.Paused || !updated.UpdatedAt.Equal(later) || updated.FetchInterval != time.Hour {
		t.Fatalf("UpdateSource = %+v, %v", updated, err)
	}
	if same, err := s.UpdateSource(ctx, blog.ID, SourcePatch{}, later.Add(time.Hour)); err != nil || !same.UpdatedAt.Equal(later) {
		t.Fatalf("empty patch = %+v, %v", same, err)
	}
	if _, err := s.UpdateSource(ctx, 99, SourcePatch{Title: &title}, later); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}

	if err := s.MarkSourceDue(ctx, blog.ID, later); !errors.Is(err, ErrPaused) {
		t.Fatalf("due while paused: %v", err)
	}
	due := later.Add(time.Hour)
	if err := s.MarkSourceDue(ctx, hub.ID, due); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Source(ctx, hub.ID); !got.NextFetchAt.Equal(due) || !got.UpdatedAt.Equal(start) {
		t.Fatalf("after MarkSourceDue: %+v", got)
	}
	if err := s.MarkSourceDue(ctx, 99, due); !errors.Is(err, ErrNotFound) {
		t.Fatalf("due missing: %v", err)
	}

	s.db.Exec("UPDATE entries SET starred = 1")
	if err := s.DeleteSource(ctx, hub.ID); err != nil {
		t.Fatal(err)
	}
	var entries int
	s.db.QueryRow("SELECT count(*) FROM entries").Scan(&entries)
	if entries != 0 {
		t.Fatalf("%d entries survived their source", entries)
	}
	if err := s.DeleteSource(ctx, hub.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if _, err := s.Source(ctx, hub.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted source: %v", err)
	}
}

func TestEntries(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	start := time.UnixMilli(1_000_000)
	blog, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "Blog", start)
	tube, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/videos", Kind: "video", FetchInterval: time.Hour}, "", start)
	feed := func(src Source, at time.Time, items ...FeedEntry) {
		t.Helper()
		if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: src.Kind}, "", "", items, time.Time{}, at); err != nil {
			t.Fatal(err)
		}
	}
	feed(blog, start, FeedEntry{GUID: "b1", Title: "Native readers", Images: []Image{{URL: "https://example.org/a.png", Width: 4}}},
		FeedEntry{GUID: "b2", Title: "Older post"})
	feed(tube, start.Add(time.Minute), FeedEntry{GUID: "v1", Title: "A video", DurationSec: 90})

	ids := func(entries []Entry) (got []string) {
		for _, e := range entries {
			got = append(got, e.Title)
		}
		return got
	}
	all, err := s.Entries(ctx, EntryFilter{}, 10)
	if err != nil || strings.Join(ids(all), "|") != "A video|Native readers|Older post" {
		t.Fatalf("Entries = %v, %v", ids(all), err)
	}
	if all[0].Kind != "video" || all[0].SourceTitle != "https://example.org/videos" || all[0].DurationSec != 90 ||
		all[1].SourceTitle != "Blog" || len(all[1].Images) != 1 || all[1].Images[0].Width != 4 || all[1].ExtractStatus != "pending" {
		t.Fatalf("fields: %+v", all[:2])
	}
	after := Position{all[0].CreatedAt.UnixMilli(), all[0].ID}
	if page, _ := s.Entries(ctx, EntryFilter{After: &after}, 1); len(page) != 1 || page[0].ID != all[1].ID {
		t.Fatalf("second page: %v", ids(page))
	}
	if got, _ := s.Entries(ctx, EntryFilter{Kind: "article", Query: `NATIVE "READ`}, 10); len(got) != 0 {
		t.Fatalf("a quote matched: %v", ids(got))
	}
	if got, _ := s.Entries(ctx, EntryFilter{Kind: "article", Query: "NATIVE READ"}, 10); len(got) != 1 || got[0].Title != "Native readers" {
		t.Fatalf("search: %v", ids(got))
	}
	if got, _ := s.Entries(ctx, EntryFilter{SourceID: tube.ID}, 10); len(got) != 1 {
		t.Fatalf("by source: %v", ids(got))
	}

	readTrue, readFalse, star := true, false, true
	first := start.Add(time.Hour)
	read, err := s.UpdateEntry(ctx, all[1].ID, EntryPatch{IsRead: &readTrue, Starred: &star}, first)
	if err != nil || !read.IsRead || !read.Starred || !read.ReadAt.Equal(first) {
		t.Fatalf("mark read: %+v, %v", read, err)
	}
	if again, _ := s.UpdateEntry(ctx, all[1].ID, EntryPatch{IsRead: &readTrue}, first.Add(time.Hour)); !again.ReadAt.Equal(first) {
		t.Fatalf("read twice moved read_at: %v", again.ReadAt)
	}
	if unread, _ := s.UpdateEntry(ctx, all[1].ID, EntryPatch{IsRead: &readFalse}, first); unread.IsRead || !unread.ReadAt.IsZero() || !unread.Starred {
		t.Fatalf("mark unread: %+v", unread)
	}
	if _, err := s.UpdateEntry(ctx, 99, EntryPatch{IsRead: &readTrue}, first); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing entry: %v", err)
	}
	var updated int64
	s.db.QueryRow("SELECT updated_at FROM entries WHERE id = ?", all[1].ID).Scan(&updated)
	if updated != start.UnixMilli() {
		t.Fatal("reading state changed updated_at")
	}
	if starred, _ := s.Entries(ctx, EntryFilter{Starred: true}, 10); len(starred) != 1 {
		t.Fatalf("starred: %v", ids(starred))
	}

	upTo, err := s.EntryPosition(ctx, all[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.MarkRead(ctx, EntryFilter{UpTo: &upTo}, first); err != nil || n != 2 {
		t.Fatalf("MarkRead = %d, %v", n, err)
	}
	if unread, _ := s.Entries(ctx, EntryFilter{Unread: true}, 10); len(unread) != 1 || unread[0].ID != all[0].ID {
		t.Fatalf("unread after MarkRead: %v", ids(unread))
	}
	if _, err := s.EntryPosition(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("position of missing entry: %v", err)
	}
}

func TestMuted(t *testing.T) {
	for _, c := range []struct {
		keywords, title, summary string
		want                     bool
	}{
		{"", "anything", "", false},
		{"rust", "Rust news", "", true},
		{"ÄRGER", "Viel ärger", "", true},
		{"go\nacme", "Weekly", "Sponsored by ACME", true},
		{"广告", "今日推广", "软文广告", true},
		{"python", "Rust news", "", false},
	} {
		if got := muted(c.keywords, c.title, c.summary); got != c.want {
			t.Errorf("muted(%q, %q, %q) = %v", c.keywords, c.title, c.summary, got)
		}
	}
}
