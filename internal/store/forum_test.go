package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMigrationAllowsTheForumKind(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "oriel.sqlite")
	s := open(t, file)
	old := newLogSource(t, s, "https://blog.example/feed")
	s.SaveFeed(ctx, FeedSource{ID: old.ID, Kind: "article"}, "", "", []FeedEntry{{GUID: "a", URL: "https://blog.example/a", Title: "A"}}, "", time.Time{}, time.Now())
	yes := true
	if _, err := s.UpdateEntry(ctx, mustEntryID(t, s, "a"), EntryPatch{IsRead: &yes}, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Put the database back to version 36 with the old constraint, the way a release before this one left it.
	db, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"PRAGMA writable_schema = ON",
		`UPDATE sqlite_schema SET sql = replace(sql, ', ''forum''))', '))') WHERE type = 'table' AND name = 'sources'`,
		"PRAGMA writable_schema = OFF",
		"PRAGMA user_version = 36",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err = Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// The migration kept every row: the entry, its read mark and the foreign key to its source.
	var entries, read int
	s.db.QueryRowContext(ctx, "SELECT count(*), sum(is_read) FROM entries WHERE source_id = ?", old.ID).Scan(&entries, &read)
	if entries != 1 || read != 1 {
		t.Fatalf("entries = %d, read = %d", entries, read)
	}
	var def string
	s.db.QueryRowContext(ctx, "SELECT sql FROM sqlite_schema WHERE name = 'sources'").Scan(&def)
	if !strings.Contains(def, "'image', 'forum'") || !strings.Contains(def, "deleted_at") {
		t.Fatalf("sources definition: %s", def)
	}

	// The constraint takes forum on every connection of the pool, and still refuses anything else.
	conns := make([]*sql.Conn, 3)
	for i := range conns {
		if conns[i], err = s.db.Conn(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for i, c := range conns {
		_, err := c.ExecContext(ctx, `INSERT INTO sources (url, kind, created_at, updated_at) VALUES (?, 'forum', 1, 1)`,
			"https://forum.example/"+strconv.Itoa(i))
		if err != nil {
			t.Fatalf("connection %d: %v", i, err)
		}
		c.Close()
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO sources (url, kind, created_at, updated_at) VALUES ('https://x.example', 'podcast', 1, 1)`); err == nil {
		t.Fatal("an unknown kind was accepted")
	}
	if fk := foreignKeyProblems(t, s); fk != 0 {
		t.Fatalf("%d foreign key problems", fk)
	}
}

func mustEntryID(t *testing.T, s *Store, guid string) int64 {
	t.Helper()
	var id int64
	if err := s.db.QueryRow("SELECT id FROM entries WHERE guid = ?", guid).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func foreignKeyProblems(t *testing.T, s *Store) int {
	t.Helper()
	rows, err := s.db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	return n
}

func TestEntriesFollowTheKindOfAForumSource(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	at := time.UnixMilli(1_000_000)
	src, err := s.CreateSource(ctx, NewSource{URL: "https://www.v2ex.com/index.xml", Kind: "forum", FetchInterval: time.Hour}, "V2EX", at)
	if err != nil {
		t.Fatal(err)
	}
	post := FeedEntry{GUID: "t1", URL: "https://www.v2ex.com/t/1", Title: "A post", ContentHTML: "<p>" + strings.Repeat("字", 800) + "</p>"}
	if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: "forum"}, "", "", []FeedEntry{post}, "", time.Time{}, at); err != nil {
		t.Fatal(err)
	}
	entry, err := s.Entry(ctx, mustEntryID(t, s, "t1"))
	if err != nil {
		t.Fatal(err)
	}
	// The body is the feed's: no extraction is queued, and the reading time comes from it.
	if entry.Kind != "forum" || entry.ExtractStatus != "skipped" {
		t.Fatalf("kind = %q, extract = %q", entry.Kind, entry.ExtractStatus)
	}
	if entry.ReadingSec == nil || *entry.ReadingSec != 120 {
		t.Fatalf("reading = %v", entry.ReadingSec)
	}
	list, err := s.Entries(ctx, EntryFilter{Kind: "forum"}, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("%+v %v", list, err)
	}

	// The copy on the entries follows the kind of the source both ways.
	kind := "article"
	if _, err := s.UpdateSource(ctx, src.ID, SourcePatch{Kind: &kind}, at); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Entries(ctx, EntryFilter{Kind: "forum"}, 10); len(list) != 0 {
		t.Fatalf("still forum: %+v", list)
	}
	kind = "forum"
	if _, err := s.UpdateSource(ctx, src.ID, SourcePatch{Kind: &kind}, at); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Entries(ctx, EntryFilter{Kind: "forum"}, 10); len(list) != 1 {
		t.Fatalf("not forum again: %+v", list)
	}
}
