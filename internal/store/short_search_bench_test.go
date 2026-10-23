package store

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// BenchmarkShortSearch measures a search of two characters that matches nothing over 100000 entries, the worst case:
// the scan reads every row. Run it with go test -run '^$' -bench BenchmarkShortSearch ./internal/store/
func BenchmarkShortSearch(b *testing.B) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(b.TempDir(), "oriel.sqlite"))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	now := time.UnixMilli(1_000_000)
	src, err := s.CreateSource(ctx, NewSource{URL: "https://example.com/feed.xml", Kind: "article", FetchInterval: time.Hour}, "", now)
	if err != nil {
		b.Fatal(err)
	}
	summary := "<p>" + strings.Repeat("a typical summary of an article about nothing in particular ", 8) + "</p>"
	for batch := 0; batch < 100; batch++ {
		items := make([]FeedEntry, 1000)
		for i := range items {
			n := batch*1000 + i
			items[i] = FeedEntry{GUID: fmt.Sprint("guid-", n), Title: fmt.Sprint("An entry titled number ", n), URL: fmt.Sprint("https://example.com/", n), Summary: summary}
		}
		if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: "article"}, "Feed", "", items, "", time.Time{}, now.Add(time.Duration(batch)*time.Second)); err != nil {
			b.Fatal(err)
		}
	}
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM entries").Scan(&count); err != nil || count != 100000 {
		b.Fatalf("%d entries, %v", count, err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		list, err := s.Entries(ctx, EntryFilter{Query: "zq"}, 201)
		if err != nil {
			b.Fatal(err)
		}
		if len(list) != 0 {
			b.Fatalf("%d entries", len(list))
		}
	}
}
