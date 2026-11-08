package store

import (
	"context"
	"testing"
	"time"
)

func TestDeleteReadEntries(t *testing.T) {
	ctx := context.Background()
	s, src, now := revisionFixture(t)
	feed := []FeedEntry{{GUID: "read", Title: "read"}, {GUID: "unread", Title: "unread"}, {GUID: "star", Title: "star"}}
	save := func(at time.Time, items []FeedEntry) int {
		t.Helper()
		n, err := s.SaveFeed(ctx, src, "", "", items, "", time.Time{}, at)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := save(now, feed); n != 3 {
		t.Fatalf("created %d", n)
	}
	list, err := s.Entries(ctx, EntryFilter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	byGUID := map[string]int64{}
	for _, e := range list {
		byGUID[e.Title] = e.ID
	}
	for _, patch := range []struct {
		guid string
		p    EntryPatch
	}{{"read", EntryPatch{IsRead: &yes}}, {"star", EntryPatch{IsRead: &yes, Starred: &yes}}} {
		if _, err := s.UpdateEntry(ctx, byGUID[patch.guid], patch.p, now); err != nil {
			t.Fatal(err)
		}
	}

	if n, err := s.ReadEntryCount(ctx); err != nil || n != 1 {
		t.Fatalf("count %d, %v", n, err)
	}
	if n, err := s.DeleteReadEntries(ctx, now); err != nil || n != 1 {
		t.Fatalf("deleted %d, %v", n, err)
	}
	left := map[string]bool{}
	list, _ = s.Entries(ctx, EntryFilter{}, 10)
	for _, e := range list {
		left[e.Title] = true
	}
	if len(left) != 2 || !left["unread"] || !left["star"] {
		t.Fatalf("left %v", left)
	}

	// The feed still has it: it does not come back, and the memory of it is kept fresh.
	seen := now.Add(20 * 24 * time.Hour)
	if n := save(seen, feed); n != 0 {
		t.Fatalf("came back: %d created", n)
	}
	if err := s.PruneRemovedEntries(ctx, seen.Add(20*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n := save(seen.Add(21*24*time.Hour), feed); n != 0 {
		t.Fatalf("forgotten while the feed carries it: %d created", n)
	}

	// Once the feed has not carried it for the whole period the memory goes.
	if err := s.PruneRemovedEntries(ctx, seen.Add(60*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := s.db.QueryRow("SELECT count(*) FROM removed_entries").Scan(&rows); err != nil || rows != 0 {
		t.Errorf("%d remembered, %v", rows, err)
	}
}

func TestDeleteReadEntriesLeavesADeletedSubscription(t *testing.T) {
	ctx := context.Background()
	s, src, now := revisionFixture(t)
	id := saveOne(t, s, src, FeedEntry{Title: "Post"}, now)
	yes := true
	if _, err := s.UpdateEntry(ctx, id, EntryPatch{IsRead: &yes}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE sources SET deleted_at = ? WHERE id = ?", now.UnixMilli(), src.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := s.DeleteReadEntries(ctx, now); err != nil || n != 0 {
		t.Fatalf("deleted %d, %v", n, err)
	}
}
