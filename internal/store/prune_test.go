package store

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// seedExpired adds a source with n old unstarred entries and one old starred one, and returns the source id.
func seedExpired(t *testing.T, s *Store, n int, now time.Time) int64 {
	t.Helper()
	ctx := context.Background()
	src, err := s.CreateSource(ctx, NewSource{RSSHubPath: "/bulk", Kind: "article", FetchInterval: time.Hour}, "", now)
	if err != nil {
		t.Fatal(err)
	}
	old := now.Add(-100 * 24 * time.Hour).UnixMilli()
	body := strings.Repeat("x", 2000)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= n; i++ {
		starred := 0
		if i == n {
			starred = 1
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO entries (source_id, guid, title, rss_content_html, published_at, created_at, updated_at, starred) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			src.ID, "g"+strconv.Itoa(i), "title", body, old, old, old, starred); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return src.ID
}

func TestPruneEntriesDeletesInBatchesAndFreesTheWriteLockBetweenThem(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.UnixMilli(10_000_000_000)
	id := seedExpired(t, s, 25, now)
	s.pruneBatch = 10
	batches := 0
	s.betweenBatches = func() {
		batches++
		// The write lock is free: a write from another goroutine goes through while the prune is between batches.
		done := make(chan error, 1)
		go func() {
			_, err := s.db.ExecContext(ctx, "INSERT INTO settings (key, value) VALUES ('between', 'x') ON CONFLICT (key) DO UPDATE SET value = 'y'")
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("write between batches: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("a write waited for the prune")
		}
	}
	n, err := s.PruneEntries(ctx, now, 30)
	if err != nil || n != 25 {
		t.Fatalf("deleted %d, %v", n, err)
	}
	if batches != 2 {
		t.Errorf("%d pauses between batches, want 2", batches)
	}
	var left, indexed int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM entries WHERE source_id = ?", id).Scan(&left); err != nil || left != 1 {
		t.Errorf("%d entries left, %v; want only the starred one", left, err)
	}
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM entries_fts WHERE entries_fts MATCH 'title'").Scan(&indexed); err != nil || indexed != 1 {
		t.Errorf("%d entries in the search index, %v; want the delete triggers to leave only the starred one", indexed, err)
	}
}

func TestPruneEntriesKeepsAnEntryStarredWhilePruning(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.UnixMilli(10_000_000_000)
	id := seedExpired(t, s, 25, now)
	s.pruneBatch = 10
	var late int64
	if err := s.db.QueryRowContext(ctx, "SELECT max(id) FROM entries WHERE starred = 0 AND source_id = ?", id).Scan(&late); err != nil {
		t.Fatal(err)
	}
	s.betweenBatches = func() {
		if _, err := s.db.ExecContext(ctx, "UPDATE entries SET starred = 1 WHERE id = ?", late); err != nil {
			t.Error(err)
		}
		s.betweenBatches = nil
	}
	n, err := s.PruneEntries(ctx, now, 30)
	if err != nil || n != 24 {
		t.Fatalf("deleted %d, %v; want 24", n, err)
	}
	var kept int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM entries WHERE id = ?", late).Scan(&kept); err != nil || kept != 1 {
		t.Errorf("the entry starred meanwhile: %d left, %v", kept, err)
	}
}

func TestPruneEntriesStopsWhenTheContextEnds(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.UnixMilli(10_000_000_000)
	seedExpired(t, s, 25, now)
	s.pruneBatch = 10
	ctx, cancel := context.WithCancel(context.Background())
	s.betweenBatches = cancel
	n, err := s.PruneEntries(ctx, now, 30)
	if !errors.Is(err, context.Canceled) || n != 10 {
		t.Fatalf("deleted %d, %v; want the first batch and context.Canceled", n, err)
	}
	var left int
	if err := s.db.QueryRow("SELECT count(*) FROM entries").Scan(&left); err != nil || left != 16 {
		t.Errorf("%d entries left, %v; want 16", left, err)
	}
}

// Without a sleep between batches a writer must still get through while a large prune runs.
func TestPruneEntriesDoesNotStarveAWriter(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.UnixMilli(10_000_000_000)
	seedExpired(t, s, 2000, now)
	s.pruneBatch = 10
	pruned := make(chan error, 1)
	go func() {
		_, err := s.PruneEntries(ctx, now, 30)
		pruned <- err
	}()
	var slowest time.Duration
	writes := 0
	for done := false; !done; writes++ {
		select {
		case err := <-pruned:
			if err != nil {
				t.Fatal(err)
			}
			done = true
			continue
		default:
		}
		start := time.Now()
		if _, err := s.db.ExecContext(ctx, "INSERT INTO settings (key, value) VALUES ('w', ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value", writes); err != nil {
			t.Fatal(err)
		}
		slowest = max(slowest, time.Since(start))
	}
	t.Logf("%d writes while pruning, the slowest %v", writes, slowest)
	if slowest > 2*time.Second {
		t.Errorf("a write waited %v for the prune", slowest)
	}
}
