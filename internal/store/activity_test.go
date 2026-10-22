package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newLogSource(t *testing.T, s *Store, url string) Source {
	t.Helper()
	src, err := s.CreateSource(context.Background(), NewSource{URL: url, Kind: "article", FetchInterval: time.Hour}, "Blog "+url, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func TestLogFetchKeepsTheNewestAttempts(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	a := newLogSource(t, s, "https://a.example/feed")
	b := newLogSource(t, s, "https://b.example/feed")
	at := time.Now().Truncate(time.Millisecond)
	for i := 0; i < FetchLogKeep+5; i++ {
		if err := s.LogFetch(ctx, FetchAttempt{SourceID: a.ID, At: at.Add(time.Duration(i) * time.Minute), Duration: 250 * time.Millisecond, OK: true, Status: 200, NewEntries: i}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.LogFetch(ctx, FetchAttempt{SourceID: b.ID, At: at, Duration: time.Second, Status: 503, Error: "status 503"}); err != nil {
		t.Fatal(err)
	}
	list, err := s.RecentFetches(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("%v %v", list, err)
	}
	// b was logged last, so it comes first.
	if list[0].ID != b.ID || list[1].ID != a.ID {
		t.Fatalf("order: %d %d", list[0].ID, list[1].ID)
	}
	if got := list[1].Attempts; len(got) != FetchLogKeep || got[0].NewEntries != FetchLogKeep+4 || got[FetchLogKeep-1].NewEntries != 5 {
		t.Fatalf("kept: %+v", got)
	}
	first := list[1].Attempts[0]
	if !first.OK || first.Status != 200 || first.Duration != 250*time.Millisecond || !first.At.Equal(at.Add(14*time.Minute)) {
		t.Fatalf("attempt: %+v", first)
	}
	if f := list[0].Attempts[0]; f.OK || f.Status != 503 || f.Error != "status 503" {
		t.Fatalf("failed attempt: %+v", f)
	}
	if list[1].URL != a.URL || list[1].Title != a.Title {
		t.Fatalf("identity: %+v", list[1])
	}
}

func TestLogFetchCutsLongErrors(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	src := newLogSource(t, s, "https://a.example/feed")
	if err := s.LogFetch(ctx, FetchAttempt{SourceID: src.ID, At: time.Now(), Error: strings.Repeat("错", 1000)}); err != nil {
		t.Fatal(err)
	}
	list, err := s.RecentFetches(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("%v %v", list, err)
	}
	if got := []rune(list[0].Attempts[0].Error); len(got) != fetchLogError {
		t.Fatalf("error length = %d", len(got))
	}
}

func TestDeletingASourceDropsItsAttempts(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	src := newLogSource(t, s, "https://a.example/feed")
	if list, err := s.RecentFetches(ctx); err != nil || len(list) != 0 {
		t.Fatalf("empty log: %v %v", list, err)
	}
	if err := s.LogFetch(ctx, FetchAttempt{SourceID: src.ID, At: time.Now(), OK: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSource(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	if list, err := s.RecentFetches(ctx); err != nil || len(list) != 0 {
		t.Fatalf("after delete: %v %v", list, err)
	}
}

func TestSourceIdentities(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	a := newLogSource(t, s, "https://a.example/feed")
	newLogSource(t, s, "https://b.example/feed")
	c := newLogSource(t, s, "https://c.example/feed")
	list, err := s.SourceIdentities(ctx, []int64{c.ID, a.ID, 999})
	if err != nil || len(list) != 2 || list[0].ID != a.ID || list[1].ID != c.ID || list[1].URL != c.URL {
		t.Fatalf("%+v %v", list, err)
	}
	if list, err := s.SourceIdentities(ctx, nil); err != nil || len(list) != 0 {
		t.Fatalf("none: %v %v", list, err)
	}
}

func TestStorageAndVacuum(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "oriel.sqlite")
	s := open(t, file)
	src := newLogSource(t, s, "https://a.example/feed")
	var entries []FeedEntry
	for i := 0; i < 300; i++ {
		entries = append(entries, FeedEntry{GUID: "g" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + time.Duration(i).String(), Title: "t", URL: "https://a.example/" + time.Duration(i).String(), ContentHTML: strings.Repeat("<p>text</p>", 400), PublishedAt: time.Now()})
	}
	if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, URL: src.URL, Kind: "article"}, "Blog", "", entries, "", time.Time{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	full, err := s.Storage(ctx)
	if err != nil || full.Entries != 300 || full.DatabaseBytes <= 0 {
		t.Fatalf("%+v %v", full, err)
	}
	if err := s.DeleteSource(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	freed, err := s.Storage(ctx)
	if err != nil || freed.Entries != 0 || freed.ReclaimableBytes <= 0 {
		t.Fatalf("after delete: %+v %v", freed, err)
	}
	if err := s.Vacuum(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := s.Storage(ctx)
	if err != nil || after.ReclaimableBytes != 0 || after.DatabaseBytes >= full.DatabaseBytes {
		t.Fatalf("after vacuum: %+v (before %+v) %v", after, full, err)
	}
	if info, err := os.Stat(file); err != nil || info.Size() != after.DatabaseBytes {
		t.Fatalf("file size %v, %v", info, err)
	}
	if s.Vacuuming() {
		t.Fatal("still vacuuming")
	}
}

func TestOnlyOneVacuumRuns(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	s.vacuuming.Store(true)
	if err := s.Vacuum(context.Background()); !errors.Is(err, ErrVacuumRunning) {
		t.Fatalf("second vacuum: %v", err)
	}
	if !s.Vacuuming() {
		t.Fatal("the running flag was cleared by the refused call")
	}
}
