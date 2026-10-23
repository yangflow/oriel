package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestDuplicateFeedAddresses(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.UnixMilli(1_000_000)
	create := func(url string) error {
		_, err := s.CreateSource(ctx, NewSource{URL: url, Kind: "article", FetchInterval: time.Hour}, "", now)
		return err
	}
	if err := create("https://example.org/feed"); err != nil {
		t.Fatal(err)
	}
	for _, dup := range []string{"https://example.org/feed/", "HTTPS://Example.ORG/feed", "https://example.org:443/feed", "https://example.org/feed#top", " https://example.org/feed "} {
		if err := create(dup); !errors.Is(err, ErrExists) {
			t.Fatalf("%q: %v", dup, err)
		}
	}
	for _, other := range []string{"https://example.org/feed?x=1", "https://example.org:8443/feed", "https://example.org/other"} {
		if err := create(other); err != nil {
			t.Fatalf("%q: %v", other, err)
		}
	}
	// An empty path equals "/", and the address stays as given.
	if err := create("http://EXAMPLE.org:80/"); err != nil {
		t.Fatal(err)
	}
	if err := create("http://example.org"); !errors.Is(err, ErrExists) {
		t.Fatalf("empty path: %v", err)
	}
	sources, err := s.Sources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, src := range sources {
		found = found || src.URL == "http://EXAMPLE.org:80/"
	}
	if !found {
		t.Fatal("the stored address was rewritten")
	}
}

func TestChangedGUIDKeepsTheEntry(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.UnixMilli(1_000_000)
	src, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "Blog", now)
	other, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/other", Kind: "article", FetchInterval: time.Hour}, "Other", now)
	save := func(src Source, items []FeedEntry, at int64) int {
		t.Helper()
		n, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: src.Kind}, "", "", items, "", time.Time{}, time.UnixMilli(at))
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	save(src, []FeedEntry{{GUID: "old-1", Title: "One", URL: "https://example.org/p/1"}, {GUID: "old-2", Title: "Two", URL: "https://example.org/p/2"}}, 2_000_000)
	save(other, []FeedEntry{{GUID: "x", Title: "Elsewhere", URL: "https://example.org/p/3"}}, 2_000_000)
	list, _ := s.Entries(ctx, EntryFilter{SourceID: src.ID}, 10)
	read, star := true, true
	for _, e := range list {
		if e.Title == "One" {
			if _, err := s.UpdateEntry(ctx, e.ID, EntryPatch{IsRead: &read, Starred: &star}, now); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Every GUID changes. The links differ only in case and a trailing slash, and /p/3 exists only in another source.
	n := save(src, []FeedEntry{
		{GUID: "new-3", Title: "Three", URL: "https://example.org/p/3"},
		{GUID: "new-2", Title: "Two", URL: "https://EXAMPLE.org/p/2/"},
		{GUID: "new-1", Title: "One, edited", URL: "https://example.org/p/1"},
	}, 3_000_000)
	if n != 1 {
		t.Fatalf("new entries: %d, want 1 (the link unknown in this source)", n)
	}
	list, _ = s.Entries(ctx, EntryFilter{SourceID: src.ID}, 10)
	if len(list) != 3 {
		t.Fatalf("entries: %d", len(list))
	}
	for _, e := range list {
		var updated int64
		if err := s.db.QueryRow("SELECT updated_at FROM entries WHERE id = ?", e.ID).Scan(&updated); err != nil {
			t.Fatal(err)
		}
		switch e.Title {
		case "One, edited":
			if !e.IsRead || !e.Starred || updated != 3_000_000 || e.CreatedAt.UnixMilli() != 2_000_000 {
				t.Fatalf("renamed entry lost state: %+v, updated %d", e, updated)
			}
		case "Two":
			if e.CreatedAt.UnixMilli() != 2_000_000 {
				t.Fatalf("entry two was recreated: %+v", e)
			}
		}
	}
	// The new GUID now matches directly.
	if n := save(src, []FeedEntry{{GUID: "new-1", Title: "One, edited", URL: "https://example.org/p/1"}}, 4_000_000); n != 0 {
		t.Fatalf("refetch: %d new", n)
	}
	if list, _ := s.Entries(ctx, EntryFilter{SourceID: other.ID}, 10); len(list) != 1 {
		t.Fatalf("other source: %d", len(list))
	}
}

func TestSharedLinkIsNotMerged(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.UnixMilli(1_000_000)
	src, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "social", FetchInterval: time.Hour}, "Feed", now)
	home := "https://example.org/"
	save := func(items []FeedEntry) int {
		t.Helper()
		n, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: src.Kind}, "", "", items, "", time.Time{}, now)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	count := func() int {
		list, _ := s.Entries(ctx, EntryFilter{SourceID: src.ID}, 20)
		return len(list)
	}
	// Two items of one fetch share a link with one stored entry whose GUID changed: both are new, none is merged.
	save([]FeedEntry{{GUID: "a", Title: "A", URL: home}})
	if n := save([]FeedEntry{{GUID: "b", Title: "B", URL: home}, {GUID: "c", Title: "C", URL: home}}); n != 2 || count() != 3 {
		t.Fatalf("shared link: %d new, %d entries", n, count())
	}
	// The source now has three entries with the link, so a single new item with it is ambiguous and stays new.
	if n := save([]FeedEntry{{GUID: "d", Title: "D", URL: home}}); n != 1 || count() != 4 {
		t.Fatalf("ambiguous link: %d new, %d entries", n, count())
	}
	// An item whose stored twin is still in the fetch is a separate entry.
	if n := save([]FeedEntry{{GUID: "p", Title: "P", URL: "https://example.org/p"}}); n != 1 {
		t.Fatalf("p: %d new", n)
	}
	if n := save([]FeedEntry{{GUID: "p", Title: "P", URL: "https://example.org/p"}, {GUID: "q", Title: "Q", URL: "https://example.org/p/"}}); n != 1 || count() != 6 {
		t.Fatalf("twin in the fetch: %d new, %d entries", n, count())
	}
}

func TestSaveFeedGivesAIRowsByEntry(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.UnixMilli(1_000_000)
	src, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "Feed", now)
	feed := FeedSource{ID: src.ID, Kind: src.Kind, AITasks: []string{TaskSummary}}
	aiRows := func() int {
		t.Helper()
		var n int
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM entry_ai").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// A GUID twice in one fetch is one new entry with one result per task.
	items := []FeedEntry{{GUID: "a", Title: "A", URL: "https://example.org/a"}, {GUID: "a", Title: "A again", URL: "https://example.org/a"},
		{GUID: "b", Title: "B", URL: "https://example.org/b"}}
	if n, err := s.SaveFeed(ctx, feed, "", "", items, "", time.Time{}, now); err != nil || n != 2 || aiRows() != 2 {
		t.Fatalf("first save: %d new, %d results, %v", n, aiRows(), err)
	}
	// Unchanged and changed existing entries get no new results.
	items[2].Title = "B, edited"
	if n, err := s.SaveFeed(ctx, feed, "", "", items, "", time.Time{}, now.Add(time.Hour)); err != nil || n != 0 || aiRows() != 2 {
		t.Fatalf("second save: %d new, %d results, %v", n, aiRows(), err)
	}
}
