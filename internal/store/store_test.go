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
	if version != 4 {
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
	if n, err := s.MarkUnread(ctx, append(marked, 99)); err != nil || n != 2 {
		t.Fatalf("MarkUnread = %d, %v", n, err)
	}
	if unread, _ := s.Entries(ctx, EntryFilter{Unread: true}, 10); len(unread) != 3 || !unread[1].ReadAt.IsZero() {
		t.Fatalf("unread after MarkUnread: %v", ids(unread))
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
	if err := s.DeleteSource(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
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
