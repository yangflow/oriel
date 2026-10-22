package store

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestPruneEntriesUsesTheKeepPeriodOfEachSource(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.UnixMilli(10_000_000_000)
	day := 24 * time.Hour
	var ids [5]int64
	for i, path := range []string{"/follow", "/short", "/long", "/forever", "/also-follow"} {
		src, err := s.CreateSource(ctx, NewSource{RSSHubPath: path, Kind: "article", FetchInterval: time.Hour}, "", now)
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = src.ID
	}
	for id, days := range map[int64]int{ids[1]: 10, ids[2]: 90, ids[3]: -1} {
		src, err := s.UpdateSource(ctx, id, SourcePatch{KeepDays: &days}, now)
		if err != nil || src.KeepDays != days {
			t.Fatalf("UpdateSource = %+v, %v", src, err)
		}
	}
	add := func(id int64, guid string, age time.Duration, starred int) {
		at := now.Add(-age).UnixMilli()
		if _, err := s.db.ExecContext(ctx, "INSERT INTO entries (source_id, guid, published_at, created_at, updated_at, starred) VALUES (?, ?, ?, ?, ?, ?)",
			id, guid, at, at, at, starred); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range ids {
		suffix := "-" + strconv.FormatInt(id, 10)
		add(id, "d20"+suffix, 20*day, 0)
		add(id, "d60"+suffix, 60*day, 0)
		add(id, "d200"+suffix, 200*day, 0)
	}
	add(ids[1], "starred", 200*day, 1)
	count := func(id int64) int {
		var n int
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM entries WHERE source_id = ?", id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Global 30 days: the two followers keep the 20-day entry, the 10-day source keeps only its starred one, the
	// 90-day source loses the 200-day entry and the forever source loses nothing.
	if n, err := s.PruneEntries(ctx, now, 30); err != nil || n != 8 {
		t.Fatalf("deleted %d, %v", n, err)
	}
	for i, want := range []int{1, 1, 2, 3, 1} {
		if got := count(ids[i]); got != want {
			t.Errorf("source %d keeps %d entries, want %d", i, got, want)
		}
	}

	// A global period of 0 keeps everything of the sources that follow it; a source's own period still applies.
	if n, err := s.PruneEntries(ctx, now, 0); err != nil || n != 0 {
		t.Fatalf("deleted %d under a forever setting, %v", n, err)
	}
	days := 7
	if _, err := s.UpdateSource(ctx, ids[2], SourcePatch{KeepDays: &days}, now); err != nil {
		t.Fatal(err)
	}
	if n, err := s.PruneEntries(ctx, now, 0); err != nil || n != 2 {
		t.Fatalf("deleted %d for the 7-day source, %v", n, err)
	}
	if count(ids[2]) != 0 || count(ids[0]) != 1 || count(ids[3]) != 3 {
		t.Errorf("counts %d %d %d", count(ids[2]), count(ids[0]), count(ids[3]))
	}
}

func TestKeepDaysDefaultsToFollowingTheSettings(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	src, err := s.CreateSource(ctx, NewSource{RSSHubPath: "/x", Kind: "article", FetchInterval: time.Hour}, "", time.UnixMilli(1))
	if err != nil || src.KeepDays != 0 {
		t.Fatalf("new source keep_days = %d, %v", src.KeepDays, err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE sources SET keep_days = -2"); err == nil {
		t.Fatal("keep_days below -1 was accepted")
	}
}
