package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// utc makes the server's local day the UTC day for a test.
func utc(t *testing.T) {
	t.Helper()
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
}

func foldedIDs(t *testing.T, s *Store, sourceID int64) map[int64]bool {
	t.Helper()
	rows, err := s.db.Query("SELECT id FROM entries WHERE source_id = ? AND folded = 1", sourceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got[id] = true
	}
	return got
}

func TestDailyCapFoldsTheEntriesOverTheCapOfEachLocalDay(t *testing.T) {
	utc(t)
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	src, err := s.CreateSource(ctx, NewSource{URL: "https://a.example/feed", Kind: "article", FetchInterval: time.Hour}, "A", now.Add(-60*day))
	if err != nil {
		t.Fatal(err)
	}
	add := func(at time.Time) int64 {
		res, err := s.db.Exec(`INSERT INTO entries (source_id, guid, title, created_at, updated_at) VALUES (?, ?, 't', ?, ?)`,
			src.ID, fmt.Sprint("g-", at.UnixMilli()), at.UnixMilli(), at.UnixMilli())
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	// Four entries today and, on the borders of the day before, 00:00 and 23:59:59 yesterday and 23:59:59 the day before.
	d := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	today := []int64{add(d.Add(time.Hour)), add(d.Add(2 * time.Hour)), add(d.Add(3 * time.Hour)), add(d.Add(4 * time.Hour))}
	add(d.Add(-24 * time.Hour))
	add(d.Add(-time.Second))
	add(d.Add(-24*time.Hour - time.Second))

	if got := foldedIDs(t, s, src.ID); len(got) != 0 {
		t.Fatalf("folded without a cap: %v", got)
	}
	two := 2
	if got, err := s.UpdateSource(ctx, src.ID, SourcePatch{DailyCap: &two}, now); err != nil || got.DailyCap != 2 {
		t.Fatalf("UpdateSource = %+v, %v", got, err)
	}
	got := foldedIDs(t, s, src.ID)
	if len(got) != 2 || !got[today[2]] || !got[today[3]] {
		t.Fatalf("folded = %v, want the 3rd and 4th of today", got)
	}

	// Unread counts and the lists leave the folded ones out; the source view and WithFolded keep them.
	if n, err := s.UnreadTotal(ctx, false); err != nil || n != 5 {
		t.Fatalf("UnreadTotal = %d, %v, want 5", n, err)
	}
	if again, err := s.Source(ctx, src.ID); err != nil || again.UnreadCount != 5 {
		t.Fatalf("source unread = %d, %v, want 5", again.UnreadCount, err)
	}
	list, err := s.Entries(ctx, EntryFilter{}, 100)
	if err != nil || len(list) != 5 {
		t.Fatalf("All = %d entries, %v, want 5", len(list), err)
	}
	list, err = s.Entries(ctx, EntryFilter{Unread: true, WithFolded: true}, 100)
	if err != nil || len(list) != 7 {
		t.Fatalf("Unread with folded = %d entries, %v, want 7", len(list), err)
	}
	for _, e := range list {
		if e.Folded != got[e.ID] {
			t.Errorf("entry %d Folded = %v", e.ID, e.Folded)
		}
	}
	list, err = s.Entries(ctx, EntryFilter{SourceID: src.ID}, 100)
	if err != nil || len(list) != 7 {
		t.Fatalf("source view = %d entries, %v, want 7", len(list), err)
	}
	for _, e := range list {
		if e.Folded {
			t.Errorf("entry %d is flagged folded in the source view", e.ID)
		}
	}

	// Marking All as read leaves the folded ones; marking the source reads them and clears the fold.
	if marked, err := s.MarkRead(ctx, EntryFilter{}, now); err != nil || len(marked) != 5 {
		t.Fatalf("MarkRead(All) = %v, %v, want 5", marked, err)
	}
	if got := foldedIDs(t, s, src.ID); len(got) != 2 {
		t.Fatalf("folded after mark-all = %v", got)
	}
	if marked, err := s.MarkRead(ctx, EntryFilter{SourceID: src.ID}, now); err != nil || len(marked) != 2 {
		t.Fatalf("MarkRead(source) = %v, %v, want 2", marked, err)
	}
	if got := foldedIDs(t, s, src.ID); len(got) != 0 {
		t.Fatalf("folded after reading = %v", got)
	}

	// Changing the cap folds again from scratch, and lifting it unfolds everything.
	if _, err := s.db.Exec("UPDATE entries SET is_read = 0"); err != nil {
		t.Fatal(err)
	}
	one := 1
	if _, err := s.UpdateSource(ctx, src.ID, SourcePatch{DailyCap: &one}, now); err != nil {
		t.Fatal(err)
	}
	if got := foldedIDs(t, s, src.ID); len(got) != 4 {
		t.Fatalf("folded under a cap of 1 = %v, want 4", got)
	}
	zero := 0
	if _, err := s.UpdateSource(ctx, src.ID, SourcePatch{DailyCap: &zero}, now); err != nil {
		t.Fatal(err)
	}
	if got := foldedIDs(t, s, src.ID); len(got) != 0 {
		t.Fatalf("folded without a cap = %v", got)
	}
}

func TestReadingOrStarringAFoldedEntryUnfoldsIt(t *testing.T) {
	utc(t)
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	src, err := s.CreateSource(ctx, NewSource{URL: "https://a.example/feed", Kind: "article", FetchInterval: time.Hour, DailyCap: 1}, "A", now)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for i := range 3 {
		at := now.Add(-time.Duration(i) * time.Hour).UnixMilli()
		res, err := s.db.Exec(`INSERT INTO entries (source_id, guid, title, created_at, updated_at) VALUES (?, ?, 't', ?, ?)`, src.ID, fmt.Sprint(i), at, at)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		ids = append(ids, id)
	}
	if err := refold(ctx, s.db, src.ID, 0); err != nil {
		t.Fatal(err)
	}
	if got := foldedIDs(t, s, src.ID); len(got) != 2 || got[ids[2]] {
		t.Fatalf("folded = %v, want the two newer entries", got)
	}
	yes := true
	if _, err := s.UpdateEntry(ctx, ids[0], EntryPatch{IsRead: &yes}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateEntry(ctx, ids[1], EntryPatch{Starred: &yes}, now); err != nil {
		t.Fatal(err)
	}
	if got := foldedIDs(t, s, src.ID); len(got) != 0 {
		t.Fatalf("folded = %v, want none", got)
	}
}

func TestSaveFeedFoldsNewEntriesOverTheCap(t *testing.T) {
	utc(t)
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	src, err := s.CreateSource(ctx, NewSource{URL: "https://a.example/feed", Kind: "article", FetchInterval: time.Hour, DailyCap: 2}, "A", now)
	if err != nil {
		t.Fatal(err)
	}
	var items []FeedEntry
	for i := range 5 {
		items = append(items, FeedEntry{GUID: fmt.Sprint("g", i), Title: fmt.Sprint("t", i), PublishedAt: now})
	}
	feed := FeedSource{ID: src.ID, URL: src.URL, Kind: "article"}
	if n, err := s.SaveFeed(ctx, feed, "A", "", items, "", time.Time{}, now); err != nil || n != 5 {
		t.Fatalf("SaveFeed = %d, %v", n, err)
	}
	if got := foldedIDs(t, s, src.ID); len(got) != 3 {
		t.Fatalf("folded = %v, want 3", got)
	}
	more := append(items, FeedEntry{GUID: "g5", Title: "t5", PublishedAt: now})
	if n, err := s.SaveFeed(ctx, feed, "A", "", more, "", time.Time{}, now.Add(time.Minute)); err != nil || n != 1 {
		t.Fatalf("SaveFeed = %d, %v", n, err)
	}
	if got := foldedIDs(t, s, src.ID); len(got) != 4 {
		t.Fatalf("folded = %v, want 4", got)
	}
}

func TestNoisySourceIsNamedAndDismissalHoldsUntilTheVolumeDoubles(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	busy := healthSource(t, s, "busy", 40*day, 0, day)
	healthEntries(t, s, busy, 2*day, 700, 0, 0) // 700 in 30 days: 23 a day, none read
	edge := healthSource(t, s, "edge", 40*day, 0, day)
	healthEntries(t, s, edge, 2*day, 600, 30, 0) // 20 a day and exactly 5 percent read
	over := healthSource(t, s, "over", 40*day, 0, day)
	healthEntries(t, s, over, 2*day, 600, 31, 0)
	few := healthSource(t, s, "few", 40*day, 0, day)
	healthEntries(t, s, few, 2*day, 599, 0, 0)
	young := healthSource(t, s, "young", 6*day, 0, day) // under 7 days old
	healthEntries(t, s, young, 2*day, 900, 0, 0)
	week := healthSource(t, s, "week", 7*day, 0, day) // judged over its 7 days: 140 needed
	healthEntries(t, s, week, 2*day, 140, 0, 0)
	capped := healthSource(t, s, "capped", 40*day, 0, day)
	healthEntries(t, s, capped, 2*day, 700, 0, 0)
	paused := healthSource(t, s, "paused", 40*day, 0, day)
	healthEntries(t, s, paused, 2*day, 700, 0, 0)
	five := 5
	if _, err := s.UpdateSource(ctx, capped, SourcePatch{DailyCap: &five}, healthNow); err != nil {
		t.Fatal(err)
	}
	yes := true
	if _, err := s.UpdateSource(ctx, paused, SourcePatch{Paused: &yes}, healthNow); err != nil {
		t.Fatal(err)
	}

	got := healthByTitle(t, s)
	for title, want := range map[string]string{"busy": HealthNoisy, "edge": HealthNoisy, "over": HealthRarelyRead, "few": HealthRarelyRead, "week": HealthNoisy} {
		if got[title].State != want {
			t.Errorf("%s state = %q, want %q", title, got[title].State, want)
		}
	}
	for _, title := range []string{"young", "capped", "paused"} {
		if h, found := got[title]; found && h.State == HealthNoisy {
			t.Errorf("%s is noisy: %+v", title, h)
		}
	}
	if h := got["busy"]; h.PerDay != 23 || h.Days != 30 {
		t.Errorf("busy = %+v, want 23 a day over 30 days", h)
	}
	if h := got["week"]; h.Days != 7 || h.PerDay != 20 {
		t.Errorf("week = %+v", h)
	}

	if err := s.DismissNoisy(ctx, few, healthNow); !errors.Is(err, ErrNotNoisy) {
		t.Errorf("DismissNoisy(few) = %v, want ErrNotNoisy", err)
	}
	if err := s.DismissNoisy(ctx, 9999, healthNow); !errors.Is(err, ErrNotFound) {
		t.Errorf("DismissNoisy(unknown) = %v, want ErrNotFound", err)
	}

	// Dismissing stores 23 a day; the source is named neither as noisy nor as rarely read until the volume doubles.
	if err := s.DismissNoisy(ctx, busy, healthNow); err != nil {
		t.Fatal(err)
	}
	if h, found := healthByTitle(t, s)["busy"]; found {
		t.Errorf("busy after dismissal = %+v, want none", h)
	}
	healthEntries(t, s, busy, 3*day, 600, 0, 0) // 1300 in 30 days: 43 a day, under 46
	if h, found := healthByTitle(t, s)["busy"]; found {
		t.Errorf("busy at 43 a day = %+v, want none", h)
	}
	healthEntries(t, s, busy, 4*day, 100, 0, 0) // 1400: 47 a day
	if h := healthByTitle(t, s)["busy"]; h.State != HealthNoisy || h.PerDay != 47 {
		t.Errorf("busy at 47 a day = %+v, want noisy", h)
	}
}
