package store

import (
	"cmp"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// deleteForGood deletes a subscription and lets its undo window pass, so that its rows are gone.
func deleteForGood(t *testing.T, s *Store, id int64) {
	t.Helper()
	ctx, now := context.Background(), time.Now()
	if err := s.DeleteSource(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PurgeDeletedSources(ctx, now.Add(DeleteUndoWindow+time.Second)); err != nil {
		t.Fatal(err)
	}
}

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
	if version != 24 {
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

	all := due.Add(time.Hour)
	if n, err := s.MarkAllSourcesDue(ctx, all); err != nil || n != 1 {
		t.Fatalf("MarkAllSourcesDue = %d, %v; want only the unpaused source", n, err)
	}
	if got, _ := s.Source(ctx, hub.ID); !got.NextFetchAt.Equal(all) || !got.UpdatedAt.Equal(start) {
		t.Fatalf("after MarkAllSourcesDue: %+v", got)
	}
	if got, _ := s.Source(ctx, blog.ID); got.NextFetchAt.Equal(all) {
		t.Fatalf("paused source was marked due: %+v", got)
	}

	s.db.Exec("UPDATE entries SET starred = 1")
	deleteForGood(t, s, hub.ID)
	var entries int
	s.db.QueryRow("SELECT count(*) FROM entries").Scan(&entries)
	if entries != 0 {
		t.Fatalf("%d entries survived their source", entries)
	}
	if err := s.DeleteSource(ctx, hub.ID, start); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if _, err := s.Source(ctx, hub.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted source: %v", err)
	}
}

func TestReadProgress(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.UnixMilli(1_000_000)
	src, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "Blog", now)
	if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: src.Kind}, "", "", []FeedEntry{{GUID: "a", Title: "A"}}, "", time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	list, err := s.Entries(ctx, EntryFilter{}, 10)
	if err != nil || len(list) != 1 || list[0].ReadProgress != nil {
		t.Fatalf("a new entry has no progress: %+v, %v", list, err)
	}
	progress := 0.42
	got, err := s.UpdateEntry(ctx, list[0].ID, EntryPatch{ReadProgress: &progress}, now)
	if err != nil || got.ReadProgress == nil || *got.ReadProgress != 0.42 || got.IsRead {
		t.Fatalf("set progress: %+v, %v", got, err)
	}
	again, err := s.Entry(ctx, list[0].ID)
	if err != nil || again.ReadProgress == nil || *again.ReadProgress != 0.42 {
		t.Fatalf("round trip: %+v, %v", again, err)
	}
	if _, err := s.UpdateEntry(ctx, 9999, EntryPatch{ReadProgress: &progress}, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing entry: %v", err)
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
		if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: src.Kind}, "", "", items, "", time.Time{}, at); err != nil {
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
	marked, err := s.MarkRead(ctx, EntryFilter{UpTo: &upTo}, first)
	if err != nil || len(marked) != 2 {
		t.Fatalf("MarkRead = %v, %v", marked, err)
	}
	if unread, _ := s.Entries(ctx, EntryFilter{Unread: true}, 10); len(unread) != 1 || unread[0].ID != all[0].ID {
		t.Fatalf("unread after MarkRead: %v", ids(unread))
	}
	if n, err := s.MarkUnread(ctx, append(marked, 99), 0); err != nil || len(n) != 2 {
		t.Fatalf("MarkUnread = %v, %v", n, err)
	}
	if unread, _ := s.Entries(ctx, EntryFilter{Unread: true}, 10); len(unread) != 3 || !unread[1].ReadAt.IsZero() {
		t.Fatalf("unread after MarkUnread: %v", ids(unread))
	}
	// An undo with the time of the mark restores the entries it marked and leaves one read again since.
	if got, err := s.MarkRead(ctx, EntryFilter{UpTo: &upTo}, first); err != nil || len(got) != 2 {
		t.Fatalf("MarkRead again = %v, %v", got, err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE entries SET read_at = ? WHERE id = ?", first.UnixMilli()+5, marked[0]); err != nil {
		t.Fatal(err)
	}
	if n, err := s.MarkUnread(ctx, marked, first.UnixMilli()); err != nil || len(n) != 1 || n[0] != marked[1] {
		t.Fatalf("MarkUnread at the time of the mark = %v, %v", n, err)
	}
	if n, err := s.MarkUnread(ctx, marked, 0); err != nil || len(n) != 1 {
		t.Fatalf("MarkUnread without a time = %v, %v", n, err)
	}
	newest, err := s.EntryPosition(ctx, all[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	oldest, err := s.EntryPosition(ctx, all[2].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.MarkRead(ctx, EntryFilter{Newer: &oldest, UpTo: &upTo}, first); err != nil || len(got) != 1 || got[0] != all[1].ID {
		t.Fatalf("MarkRead between = %v, %v", got, err)
	}
	if got, err := s.MarkRead(ctx, EntryFilter{Newer: &newest}, first); err != nil || len(got) != 0 {
		t.Fatalf("MarkRead newer than the newest = %v, %v", got, err)
	}
	if got, err := s.MarkRead(ctx, EntryFilter{Newer: &upTo}, first); err != nil || len(got) != 1 || got[0] != all[0].ID {
		t.Fatalf("MarkRead newer = %v, %v", got, err)
	}
	if _, err := s.EntryPosition(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("position of missing entry: %v", err)
	}
}

func TestMarkReadOlderThan(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	start := time.UnixMilli(1_700_000_000_000)
	blog, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "Blog", start)
	tube, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/videos", Kind: "video", FetchInterval: time.Hour}, "Tube", start)
	feed := func(src Source, items ...FeedEntry) {
		t.Helper()
		if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: src.Kind}, "", "", items, "", time.Time{}, start); err != nil {
			t.Fatal(err)
		}
	}
	// "dated" was fetched just now but published long ago: the cut follows the shown time, not the fetch time.
	feed(blog, FeedEntry{GUID: "dated", Title: "Dated", PublishedAt: start.Add(-48 * time.Hour)},
		FeedEntry{GUID: "undated", Title: "Undated"}, FeedEntry{GUID: "fresh", Title: "Fresh", PublishedAt: start.Add(time.Hour)})
	feed(tube, FeedEntry{GUID: "v", Title: "Video", PublishedAt: start.Add(-72 * time.Hour)})

	titlesOf := func(ids []int64) string {
		var got []string
		for _, id := range ids {
			e, err := s.Entry(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, e.Title)
		}
		slices.Sort(got)
		return strings.Join(got, "|")
	}
	at := start.Add(2 * time.Hour)
	cut := start.Add(-24 * time.Hour).UnixMilli()
	got, err := s.MarkRead(ctx, EntryFilter{OlderThan: cut}, at)
	if err != nil || titlesOf(got) != "Dated|Video" {
		t.Fatalf("older than a day = %s, %v", titlesOf(got), err)
	}
	if n, err := s.MarkUnread(ctx, got, 0); err != nil || len(n) != 2 {
		t.Fatalf("undo = %v, %v", n, err)
	}
	if got, err := s.MarkRead(ctx, EntryFilter{OlderThan: cut, Kind: "video"}, at); err != nil || titlesOf(got) != "Video" {
		t.Fatalf("kind scope = %s, %v", titlesOf(got), err)
	}
	if got, err := s.MarkRead(ctx, EntryFilter{OlderThan: cut, SourceID: blog.ID}, at); err != nil || titlesOf(got) != "Dated" {
		t.Fatalf("source scope = %s, %v", titlesOf(got), err)
	}
	// Strictly before: an entry shown exactly at the cut stays.
	if got, err := s.MarkRead(ctx, EntryFilter{OlderThan: start.UnixMilli()}, at); err != nil || len(got) != 0 {
		t.Fatalf("strict cut = %s, %v", titlesOf(got), err)
	}
	if got, err := s.MarkRead(ctx, EntryFilter{OlderThan: start.UnixMilli() + 1}, at); err != nil || titlesOf(got) != "Undated" {
		t.Fatalf("undated entry shows its fetch time = %s, %v", titlesOf(got), err)
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

func TestMutedCount(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	ctx := context.Background()
	at := time.UnixMilli(1_700_000_000_000)
	src, err := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "", at)
	if err != nil {
		t.Fatal(err)
	}
	items := []FeedEntry{
		{GUID: "a", Title: "Rust news"},
		{GUID: "b", Title: "Weekly", Summary: "Sponsored by ACME"},
		{GUID: "c", Title: "Rust again"},
		{GUID: "d", Title: "Other"},
	}
	if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: src.Kind}, "", "", items, "", time.Time{}, at); err != nil {
		t.Fatal(err)
	}
	entries, err := s.Entries(ctx, EntryFilter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Title == "Rust again" {
			star := true
			if _, err := s.UpdateEntry(ctx, e.ID, EntryPatch{Starred: &star}, at); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, c := range []struct {
		keywords string
		want     int
	}{
		{"", 0},
		{"python", 0},
		{"rust", 1}, // the starred one stays
		{"rust\nacme", 2},
		{"ACME", 1},
	} {
		got, err := s.MutedCount(ctx, src.ID, c.keywords)
		if err != nil || got != c.want {
			t.Errorf("MutedCount(%q) = %d, %v; want %d", c.keywords, got, err, c.want)
		}
	}
	if _, err := s.MutedCount(ctx, src.ID+1, "rust"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown source: %v", err)
	}
}

func TestAIResults(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	ctx := context.Background()
	at := time.UnixMilli(1_700_000_000_000)
	src, err := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "social", FetchInterval: time.Hour}, "", at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: "social", AITasks: []string{TaskSummary}}, "", "",
		[]FeedEntry{{GUID: "a", Title: "A"}}, "", time.Time{}, at); err != nil {
		t.Fatal(err)
	}
	entries, _ := s.Entries(ctx, EntryFilter{}, 1)
	id := entries[0].ID
	if _, err := s.AIResults(ctx, id+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing entry: %v", err)
	}
	results, err := s.AIResults(ctx, id)
	if err != nil || len(results) != 1 || results[TaskSummary].Status != "pending" || results[TaskSummary].Requested {
		t.Fatalf("automatic task: %+v %v", results, err)
	}

	s.SaveAI(ctx, id, TaskSummary, "first", "m", "fp", "", at)
	s.SaveAI(ctx, id, TaskSummary, "", "", "", "ai_timeout", at)
	got, err := s.RequestAI(ctx, id, TaskSummary, at)
	if err != nil || got.Status != "pending" || got.Text != "first" || got.Error != "" || !got.Requested || got.Attempts != 2 || got.Fingerprint != "fp" {
		t.Fatalf("a new request keeps the last success: %+v %v", got, err)
	}
	// A result's entry is the owner: content changes leave it alone and deleting the entry removes it.
	if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: "social", AITasks: []string{TaskTranslation}}, "", "",
		[]FeedEntry{{GUID: "a", Title: "A2"}}, "", time.Time{}, at); err != nil {
		t.Fatal(err)
	}
	if results, _ := s.AIResults(ctx, id); len(results) != 1 {
		t.Fatal("an existing entry was queued again")
	}
	deleteForGood(t, s, src.ID)
	var n int
	s.db.QueryRow("SELECT count(*) FROM entry_ai").Scan(&n)
	if n != 0 {
		t.Fatal("results outlived their entry")
	}
}

func TestEntryLanguage(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	ctx := context.Background()
	at := time.UnixMilli(1_700_000_000_000)
	src, err := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "", at)
	if err != nil {
		t.Fatal(err)
	}
	feed := FeedSource{ID: src.ID, Kind: "article", AITasks: []string{TaskSummary, TaskTranslation}}
	items := []FeedEntry{
		{GUID: "en", Title: "English", Language: "en"},
		{GUID: "zh", Title: "Chinese", Language: "zh-Hans"},
		{GUID: "none", Title: "Unknown"},
	}
	// The target is Simplified Chinese: only the entry already in it skips the translation.
	if _, err := s.SaveFeed(ctx, feed, "", "", items, "zh-Hans", time.Time{}, at); err != nil {
		t.Fatal(err)
	}
	entries, _ := s.Entries(ctx, EntryFilter{}, 10)
	got := map[string]Entry{}
	for _, e := range entries {
		got[e.Title] = e
	}
	if got["English"].Language != "en" || got["Chinese"].Language != "zh-Hans" || got["Unknown"].Language != "" {
		t.Fatalf("languages: %q %q %q", got["English"].Language, got["Chinese"].Language, got["Unknown"].Language)
	}
	for guid, wantTranslation := range map[string]bool{"English": true, "Chinese": false, "Unknown": true} {
		results, err := s.AIResults(ctx, got[guid].ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := results[TaskTranslation]; ok != wantTranslation {
			t.Errorf("%s: translation queued = %v", guid, ok)
		}
		if _, ok := results[TaskSummary]; !ok {
			t.Errorf("%s: the summary is still queued", guid)
		}
	}
	// A later fetch that cannot tell the language keeps what is known; one that can replaces it.
	if _, err := s.SaveFeed(ctx, feed, "", "", []FeedEntry{{GUID: "en", Title: "English, edited"}, {GUID: "none", Title: "Unknown, edited", Language: "fr"}},
		"zh-Hans", time.Time{}, at); err != nil {
		t.Fatal(err)
	}
	if e, _ := s.Entry(ctx, got["English"].ID); e.Language != "en" {
		t.Fatalf("language lost: %q", e.Language)
	}
	if e, _ := s.Entry(ctx, got["Unknown"].ID); e.Language != "fr" {
		t.Fatalf("language not set: %q", e.Language)
	}
	// The extracted text sets the language, and an extraction that cannot tell leaves it.
	if err := s.SaveExtraction(ctx, got["Unknown"].ID, "<p>x</p>", "x", "de"); err != nil {
		t.Fatal(err)
	}
	if e, _ := s.Entry(ctx, got["Unknown"].ID); e.Language != "de" {
		t.Fatalf("extraction language: %q", e.Language)
	}
	if err := s.SaveExtraction(ctx, got["Unknown"].ID, "<p>x</p>", "x", ""); err != nil {
		t.Fatal(err)
	}
	if e, _ := s.Entry(ctx, got["Unknown"].ID); e.Language != "de" {
		t.Fatalf("extraction lost the language: %q", e.Language)
	}
	// An empty target never matches an unknown language.
	other, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/other", Kind: "article", FetchInterval: time.Hour}, "", at)
	if _, err := s.SaveFeed(ctx, FeedSource{ID: other.ID, Kind: "article", AITasks: []string{TaskTranslation}}, "", "",
		[]FeedEntry{{GUID: "x", Title: "X"}}, "", time.Time{}, at); err != nil {
		t.Fatal(err)
	}
	x, _ := s.Entries(ctx, EntryFilter{SourceID: other.ID}, 1)
	if results, _ := s.AIResults(ctx, x[0].ID); len(results) != 1 {
		t.Fatalf("unknown language skipped: %+v", results)
	}
}

func TestMigrationAddsLanguageColumn(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	rows, err := s.db.Query("SELECT name, type, \"notnull\" FROM pragma_table_info('entries') WHERE name = 'language'")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("entries has no language column")
	}
	var name, kind string
	var notNull int
	if err := rows.Scan(&name, &kind, &notNull); err != nil || kind != "TEXT" || notNull != 0 {
		t.Fatalf("language column: %s %d %v", kind, notNull, err)
	}
}

type timelineEntry struct {
	id      int64
	kind    string
	unread  bool
	muted   bool
	created int64
}

// timelineFixture holds 120 entries across an article, an image and a video source, listed from the newest. Every
// third entry is unread, and the video source mutes the keyword "spam".
func timelineFixture(t *testing.T, s *Store) (sources map[string]Source, entries []timelineEntry) {
	t.Helper()
	ctx := context.Background()
	now := time.UnixMilli(1_000)
	sources = map[string]Source{}
	for _, kind := range []string{"article", "image", "video"} {
		src, err := s.CreateSource(ctx, NewSource{URL: "https://example.org/" + kind, Kind: kind, FetchInterval: time.Hour}, kind, now)
		if err != nil {
			t.Fatal(err)
		}
		sources[kind] = src
	}
	spam := "spam"
	if _, err := s.UpdateSource(ctx, sources["video"].ID, SourcePatch{MuteKeywords: &spam}, now); err != nil {
		t.Fatal(err)
	}
	kinds := []string{"article", "article", "image", "video", "article"}
	for i := range 120 {
		kind := kinds[i%len(kinds)]
		title := "entry"
		if kind == "video" && i%2 == 0 {
			title = "spam entry"
		}
		created := int64(1_000_000 - i/2*10) // pairs share created_at, so the ID breaks the tie
		unread := i%3 == 0
		result, err := s.db.ExecContext(ctx, "INSERT INTO entries (source_id, guid, title, is_read, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
			sources[kind].ID, strconv.Itoa(i), title, !unread, created, created)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		entries = append(entries, timelineEntry{id, kind, unread, title != "entry", created})
	}
	slices.SortFunc(entries, func(a, b timelineEntry) int {
		return cmp.Or(cmp.Compare(b.created, a.created), cmp.Compare(b.id, a.id))
	})
	return sources, entries
}

// pages reads the whole timeline of filter in pages of size, the way a client pages with After.
func pages(t *testing.T, s *Store, filter EntryFilter, size int) (got []int64) {
	t.Helper()
	for {
		page, err := s.Entries(context.Background(), filter, size)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page {
			got = append(got, e.ID)
		}
		if len(page) < size {
			return got
		}
		last := page[len(page)-1]
		filter.After = &Position{last.CreatedAt.UnixMilli(), last.ID}
	}
}

func TestTimelineFilters(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	sources, entries := timelineFixture(t, s)
	want := func(keep func(timelineEntry) bool) (ids []int64) {
		for _, e := range entries {
			if !e.muted && keep(e) {
				ids = append(ids, e.id)
			}
		}
		return ids
	}
	for _, c := range []struct {
		name   string
		filter EntryFilter
		keep   func(timelineEntry) bool
	}{
		{"all", EntryFilter{}, func(timelineEntry) bool { return true }},
		{"unread", EntryFilter{Unread: true}, func(e timelineEntry) bool { return e.unread }},
		{"image", EntryFilter{Kind: "image"}, func(e timelineEntry) bool { return e.kind == "image" }},
		{"video", EntryFilter{Kind: "video"}, func(e timelineEntry) bool { return e.kind == "video" }},
		{"unread image", EntryFilter{Kind: "image", Unread: true}, func(e timelineEntry) bool { return e.kind == "image" && e.unread }},
		{"source", EntryFilter{SourceID: sources["article"].ID}, func(e timelineEntry) bool { return e.kind == "article" }},
		{"source unread", EntryFilter{SourceID: sources["article"].ID, Unread: true}, func(e timelineEntry) bool { return e.kind == "article" && e.unread }},
	} {
		for _, size := range []int{1, 7, 50, 500} {
			if got := pages(t, s, c.filter, size); !slices.Equal(got, want(c.keep)) {
				t.Errorf("%s, pages of %d: got %v, want %v", c.name, size, got, want(c.keep))
			}
		}
	}
}

func TestEntryKindFollowsSource(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	sources, _ := timelineFixture(t, s)
	before, _ := s.Entries(ctx, EntryFilter{Kind: "image"}, 500)
	if len(before) == 0 {
		t.Fatal("no image entries")
	}
	video := "video"
	if _, err := s.UpdateSource(ctx, sources["image"].ID, SourcePatch{Kind: &video}, time.UnixMilli(2_000)); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Entries(ctx, EntryFilter{Kind: "image"}, 500); len(got) != 0 {
		t.Fatalf("%d entries stayed image after the source changed", len(got))
	}
	moved, _ := s.Entries(ctx, EntryFilter{Kind: "video", SourceID: sources["image"].ID}, 500)
	if len(moved) != len(before) || moved[0].Kind != "video" {
		t.Fatalf("moved %d of %d entries", len(moved), len(before))
	}
	if _, err := s.SaveFeed(ctx, FeedSource{ID: sources["image"].ID, Kind: "video"}, "", "", []FeedEntry{{GUID: "new", Title: "new"}}, "", time.Time{}, time.UnixMilli(3_000)); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Entries(ctx, EntryFilter{Kind: "video", SourceID: sources["image"].ID}, 500); len(got) != len(before)+1 {
		t.Fatalf("a new entry did not take the source's kind: %d", len(got))
	}
}

func TestTimelineQueriesUseIndexes(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	after := &Position{CreatedAt: 500, ID: 5}
	for _, c := range []struct {
		name   string
		filter EntryFilter
		index  string
	}{
		{"all", EntryFilter{}, "entries_timeline"},
		{"all, later page", EntryFilter{After: after}, "entries_timeline"},
		{"unread", EntryFilter{Unread: true}, "entries_unread_timeline"},
		{"unread, later page", EntryFilter{Unread: true, After: after}, "entries_unread_timeline"},
		{"kind", EntryFilter{Kind: "image"}, "entries_kind_timeline"},
		{"kind, later page", EntryFilter{Kind: "image", After: after}, "entries_kind_timeline"},
		{"unread kind", EntryFilter{Kind: "image", Unread: true}, "entries_kind_unread_timeline"},
		{"unread kind, later page", EntryFilter{Kind: "image", Unread: true, After: after}, "entries_kind_unread_timeline"},
		{"source", EntryFilter{SourceID: 1}, "entries_source_timeline"},
		{"source unread", EntryFilter{SourceID: 1, Unread: true}, "entries_source_timeline"},
	} {
		query, args := c.filter.timelineQuery()
		rows, err := s.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, append(args, 50)...)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		text := strings.Join(plan, "\n")
		if !strings.Contains(text, "INDEX "+c.index) || strings.Contains(text, "TEMP B-TREE") || strings.Contains(text, "SCAN s") {
			t.Errorf("%s: want %s and no sorting, got\n%s", c.name, c.index, text)
		}
	}
}

func TestConcurrentWritersWaitForEachOther(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	at := time.UnixMilli(1_000_000)
	var sources []Source
	for i := range 4 {
		src, err := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed" + strconv.Itoa(i), Kind: "article", FetchInterval: time.Hour}, "", at)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, src)
	}
	errs := make(chan error, 400)
	var wg sync.WaitGroup
	for w := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			src := sources[w%len(sources)]
			for i := range 15 {
				items := []FeedEntry{{GUID: "g" + strconv.Itoa(w) + "-" + strconv.Itoa(i), Title: "t", PublishedAt: at}}
				if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: "article"}, "", "", items, "", time.Time{}, at); err != nil {
					errs <- err
				}
				if _, err := s.MarkRead(ctx, EntryFilter{SourceID: src.ID}, at); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// Search text is one literal phrase: operators and punctuation of the full-text syntax match nothing special and
// never fail the query.
func TestSearchTextIsLiteral(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	start := time.UnixMilli(1_000_000)
	blog, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "Blog", start)
	if _, err := s.SaveFeed(ctx, FeedSource{ID: blog.ID, Kind: blog.Kind}, "", "", []FeedEntry{
		{GUID: "1", Title: "Cats OR dogs", Summary: "a quiet note"},
		{GUID: "2", Title: "Plain cats", Summary: "NEAR the end"},
		{GUID: "3", Title: "C++ and f(x): a -b test", Summary: "wild*card"},
	}, "", time.Time{}, start); err != nil {
		t.Fatal(err)
	}
	search := func(query string) (titles []string) {
		t.Helper()
		got, err := s.Entries(ctx, EntryFilter{Query: query}, 10)
		if err != nil {
			t.Fatalf("%q: %v", query, err)
		}
		for _, e := range got {
			titles = append(titles, e.Title)
		}
		return titles
	}
	for query, want := range map[string]string{
		"cats OR dogs": "Cats OR dogs", // OR is a word, not "cats or dogs"
		"dogs OR cats": "",             // the phrase order matters
		"NEAR the":     "Plain cats",
		"cats":         "Cats OR dogs|Plain cats",
		"f(x):":        "C++ and f(x): a -b test",
		"wild*":        "C++ and f(x): a -b test",
		"nothing here": "",
		`"`:            "",
		`""`:           "",
		`cats" OR "a`:  "",
		"title:cats":   "",
		"NOT cats":     "",
		"c++ and":      "C++ and f(x): a -b test",
		"CATS":         "Cats OR dogs|Plain cats",
		"quiet note":   "Cats OR dogs",
		"note quiet":   "",
	} {
		if got := strings.Join(search(query), "|"); got != want {
			t.Errorf("search %q = %q, want %q", query, got, want)
		}
	}
}

// A search finds neither entries of a deleted source nor muted ones, unless they are starred.
func TestSearchSkipsHiddenEntries(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	start := time.UnixMilli(1_000_000)
	keep, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/a", Kind: "article", FetchInterval: time.Hour}, "A", start)
	gone, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/b", Kind: "article", FetchInterval: time.Hour}, "B", start)
	for _, src := range []Source{keep, gone} {
		if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: src.Kind}, "", "", []FeedEntry{
			{GUID: "x", Title: "Quasar news"}, {GUID: "y", Title: "Quasar gossip"},
		}, "", time.Time{}, start); err != nil {
			t.Fatal(err)
		}
	}
	count := func() int {
		got, err := s.Entries(ctx, EntryFilter{Query: "quasar"}, 10)
		if err != nil {
			t.Fatal(err)
		}
		return len(got)
	}
	if n := count(); n != 4 {
		t.Fatalf("all = %d", n)
	}
	if err := s.DeleteSource(ctx, gone.ID, start); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 2 {
		t.Fatalf("after delete = %d", n)
	}
	if err := s.SetSetting(ctx, MuteSetting, "gossip"); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 1 {
		t.Fatalf("after mute = %d", n)
	}
}

// The cursors combine with each other and with the direction; entries created at the same time are ordered by ID.
func TestEntryCursorCombinations(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	start := time.UnixMilli(1_000_000)
	blog, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "Blog", start)
	// a, b and c arrive together; d and e later.
	for i, batch := range [][]string{{"a", "b", "c"}, {"d"}, {"e"}} {
		var items []FeedEntry
		for _, guid := range batch {
			items = append(items, FeedEntry{GUID: guid, Title: guid})
		}
		if _, err := s.SaveFeed(ctx, FeedSource{ID: blog.ID, Kind: blog.Kind}, "", "", items, "", time.Time{}, start.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	titles := func(f EntryFilter) string {
		t.Helper()
		got, err := s.Entries(ctx, f, 10)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range got {
			out = append(out, e.Title)
		}
		return strings.Join(out, "")
	}
	all, _ := s.Entries(ctx, EntryFilter{Oldest: true}, 10)
	if len(all) != 5 {
		t.Fatalf("setup: %d entries", len(all))
	}
	at := func(title string) *Position {
		for _, e := range all {
			if e.Title == title {
				return &Position{e.CreatedAt.UnixMilli(), e.ID}
			}
		}
		t.Fatalf("no entry %q", title)
		return nil
	}
	if got := titles(EntryFilter{Oldest: true}); got != "cbade" {
		t.Fatalf("oldest first = %s", got)
	}
	if got := titles(EntryFilter{}); got != "edabc" {
		t.Fatalf("newest first = %s", got)
	}
	for name, c := range map[string]struct {
		filter EntryFilter
		want   string
	}{
		"after, newest first":        {EntryFilter{After: at("d")}, "abc"},
		"after, oldest first":        {EntryFilter{After: at("b"), Oldest: true}, "ade"},
		"after inside a tie":         {EntryFilter{After: at("a")}, "bc"},
		"after the oldest":           {EntryFilter{After: at("c")}, ""},
		"after the newest, oldest":   {EntryFilter{After: at("e"), Oldest: true}, ""},
		"up to":                      {EntryFilter{UpTo: at("a")}, "abc"},
		"up to and after":            {EntryFilter{UpTo: at("d"), After: at("b")}, "c"},
		"newer":                      {EntryFilter{Newer: at("c")}, "edab"},
		"newer, oldest first":        {EntryFilter{Newer: at("b"), Oldest: true}, "ade"},
		"newer and up to":            {EntryFilter{Newer: at("a"), UpTo: at("d")}, "d"},
		"newer than the newest":      {EntryFilter{Newer: at("e")}, ""},
		"empty window":               {EntryFilter{Newer: at("d"), UpTo: at("c")}, ""},
		"after and newer, one place": {EntryFilter{After: at("d"), Newer: at("b")}, "a"},
	} {
		if got := titles(c.filter); got != c.want {
			t.Errorf("%s = %q, want %q", name, got, c.want)
		}
	}
	// Paging through a tie visits every entry once, in both directions.
	for _, oldest := range []bool{false, true} {
		seen := pages(t, s, EntryFilter{Oldest: oldest}, 2)
		if len(seen) != 5 {
			t.Errorf("oldest=%v: pages saw %v", oldest, seen)
		}
	}
}

func TestMarkReadSkipsHiddenAndReadEntries(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	start := time.UnixMilli(1_700_000_000_000)
	a, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/a", Kind: "article", FetchInterval: time.Hour}, "A", start)
	b, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/b", Kind: "article", FetchInterval: time.Hour}, "B", start)
	save := func(src Source, items ...FeedEntry) {
		t.Helper()
		if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: src.Kind}, "", "", items, "", time.Time{}, start); err != nil {
			t.Fatal(err)
		}
	}
	save(a, FeedEntry{GUID: "1", Title: "One"}, FeedEntry{GUID: "2", Title: "Two"}, FeedEntry{GUID: "3", Title: "Spam three"})
	save(b, FeedEntry{GUID: "4", Title: "Four"})
	all, _ := s.Entries(ctx, EntryFilter{SourceID: a.ID}, 10)
	byTitle := map[string]int64{}
	for _, e := range all {
		byTitle[e.Title] = e.ID
	}
	read := true
	if _, err := s.UpdateEntry(ctx, byTitle["One"], EntryPatch{IsRead: &read}, start); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, MuteSetting, "spam"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSource(ctx, b.ID, start); err != nil {
		t.Fatal(err)
	}
	got, err := s.MarkRead(ctx, EntryFilter{}, start.Add(time.Minute))
	if err != nil || len(got) != 1 || got[0] != byTitle["Two"] {
		t.Fatalf("MarkRead = %v, %v; only Two is unread and visible", got, err)
	}
	// Nothing is left: the result is an empty list, not nil, and the read time of One is untouched.
	got, err = s.MarkRead(ctx, EntryFilter{}, start.Add(2*time.Minute))
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("second MarkRead = %#v, %v", got, err)
	}
	one, _ := s.Entry(ctx, byTitle["One"])
	if !one.ReadAt.Equal(start) {
		t.Fatalf("One read at %v", one.ReadAt)
	}
	if spam, _ := s.Entry(ctx, byTitle["Spam three"]); spam.IsRead {
		t.Fatal("a muted entry was marked read")
	}
}

func TestMarkUnreadEdges(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	start := time.UnixMilli(1_700_000_000_000)
	blog, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/a", Kind: "article", FetchInterval: time.Hour}, "A", start)
	if _, err := s.SaveFeed(ctx, FeedSource{ID: blog.ID, Kind: blog.Kind}, "", "", []FeedEntry{{GUID: "1", Title: "One"}, {GUID: "2", Title: "Two"}}, "", time.Time{}, start); err != nil {
		t.Fatal(err)
	}
	marked, err := s.MarkRead(ctx, EntryFilter{}, start.Add(time.Minute))
	if err != nil || len(marked) != 2 {
		t.Fatalf("MarkRead = %v, %v", marked, err)
	}
	for _, ids := range [][]int64{nil, {}, {0, -1, 9999}} {
		if got, err := s.MarkUnread(ctx, ids, 0); err != nil || got == nil || len(got) != 0 {
			t.Errorf("MarkUnread(%v) = %#v, %v", ids, got, err)
		}
	}
	// The undo only touches entries read at the time it names.
	if got, err := s.MarkUnread(ctx, marked, start.UnixMilli()); err != nil || len(got) != 0 {
		t.Fatalf("stale undo = %v, %v", got, err)
	}
	// Unknown IDs and a repeated ID are skipped; the entry changes once.
	got, err := s.MarkUnread(ctx, []int64{9999, marked[0], marked[0]}, start.Add(time.Minute).UnixMilli())
	if err != nil || len(got) != 1 || got[0] != marked[0] {
		t.Fatalf("undo = %v, %v", got, err)
	}
	entry, _ := s.Entry(ctx, marked[0])
	if entry.IsRead || !entry.ReadAt.IsZero() {
		t.Fatalf("still read: %+v", entry)
	}
	if other, _ := s.Entry(ctx, marked[1]); !other.IsRead {
		t.Fatal("an entry outside the undo changed")
	}
}
