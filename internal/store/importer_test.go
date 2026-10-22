package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSourceIDByFeedURL(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	src, err := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "", time.UnixMilli(1_000_000))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"https://example.org/feed", "HTTPS://Example.ORG/feed/", " https://example.org/feed#x "} {
		if id, err := s.SourceIDByFeedURL(ctx, raw); err != nil || id != src.ID {
			t.Fatalf("%q: %d, %v", raw, id, err)
		}
	}
	if _, err := s.SourceIDByFeedURL(ctx, "https://example.org/other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other: %v", err)
	}
}

func TestImportEntries(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.UnixMilli(2_000_000_000_000)
	src, err := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "", now)
	if err != nil {
		t.Fatal(err)
	}
	// Two stored entries, one by GUID and one under a GUID of the feed's own.
	for _, row := range []struct{ guid, url string }{
		{"https://example.org/a", "https://example.org/a"},
		{"urn:uuid:b", "https://example.org/b/"},
	} {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO entries (source_id, guid, title, url, extract_status, images, created_at, updated_at)
			VALUES (?, ?, 't', ?, 'ok', '[]', ?, ?)`, src.ID, row.guid, row.url, now.UnixMilli(), now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	published := now.Add(-48 * time.Hour)
	in := []ImportedEntry{
		{URL: "https://example.org/a", Starred: true, Read: true},
		{URL: "https://example.org/b", Read: true},
		{URL: "https://Example.org/new/", Title: "New", Starred: true, Read: true, PublishedAt: published, ContentHTML: "<p>hello world</p>"},
		{URL: "https://example.org/gone", Read: true},
		{URL: "  ", Starred: true},
	}
	got, err := s.ImportEntries(ctx, src.ID, in, now)
	if err != nil {
		t.Fatal(err)
	}
	want := []ImportResult{
		{ImportExisting, true, true},
		{ImportExisting, false, true},
		{ImportAdded, true, true},
		{ImportUnmatched, false, false},
		{ImportInvalid, false, false},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("result %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	list, err := s.Entries(ctx, EntryFilter{SourceID: src.ID}, 10)
	if err != nil || len(list) != 3 {
		t.Fatalf("entries = %d, %v", len(list), err)
	}
	for _, e := range list {
		if !e.IsRead {
			t.Fatalf("%s is unread", e.URL)
		}
		if e.URL == "https://Example.org/new/" && (!e.Starred || !e.CreatedAt.Equal(published) || e.ExtractStatus != "pending") {
			t.Fatalf("new entry = %+v", e)
		}
	}

	// A second run changes and adds nothing.
	again, err := s.ImportEntries(ctx, src.ID, in, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range again {
		if r.StarSet || r.ReadSet || r.Outcome == ImportAdded {
			t.Fatalf("second run result %d = %+v", i, r)
		}
	}
	if list, _ := s.Entries(ctx, EntryFilter{SourceID: src.ID}, 10); len(list) != 3 {
		t.Fatalf("entries after the second run = %d", len(list))
	}

	// State is never taken away: an entry unstarred and unread there stays as it is here.
	if _, err := s.ImportEntries(ctx, src.ID, []ImportedEntry{{URL: "https://example.org/a"}}, now); err != nil {
		t.Fatal(err)
	}
	if e, _ := s.Entries(ctx, EntryFilter{SourceID: src.ID, Starred: true}, 10); len(e) != 2 {
		t.Fatalf("starred = %d", len(e))
	}
	if _, err := s.ImportEntries(ctx, 999, in, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing source: %v", err)
	}
}
