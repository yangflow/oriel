package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// BenchmarkSaveFeed measures SaveFeed with a 1000-item feed carrying both AI tasks: the first save of the feed,
// then the second save of the same items, which only finds them unchanged. Run it with
// go test -run '^$' -bench BenchmarkSaveFeed ./internal/store/
func BenchmarkSaveFeed(b *testing.B) {
	ctx := context.Background()
	items := make([]FeedEntry, 1000)
	for i := range items {
		items[i] = FeedEntry{GUID: fmt.Sprint("guid-", i), Title: fmt.Sprint("Entry ", i), URL: fmt.Sprint("https://example.com/", i),
			Summary: "<p>a short summary</p>", Language: "en"}
	}
	now := time.UnixMilli(1_000_000)
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		s, err := Open(ctx, filepath.Join(b.TempDir(), "oriel.sqlite"))
		if err != nil {
			b.Fatal(err)
		}
		src, err := s.CreateSource(ctx, NewSource{URL: "https://example.com/feed.xml", Kind: "article", FetchInterval: time.Hour}, "", now)
		if err != nil {
			b.Fatal(err)
		}
		feed := FeedSource{ID: src.ID, Kind: "article", AITasks: []string{TaskSummary, TaskTranslation}}
		b.StartTimer()
		for round := 0; round < 2; round++ {
			if _, err := s.SaveFeed(ctx, feed, "Feed", "", items, "zh-Hans", time.Time{}, now); err != nil {
				b.Fatal(err)
			}
		}
		b.StopTimer()
		s.Close()
	}
}
