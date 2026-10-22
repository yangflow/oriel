package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// deleteFixture has two subscriptions with one entry each; the first entry is starred.
func deleteFixture(t *testing.T) (s *Store, gone, kept Source, now time.Time) {
	t.Helper()
	ctx := context.Background()
	s = open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now = time.UnixMilli(1_790_000_000_000)
	var err error
	gone, err = s.CreateSource(ctx, NewSource{URL: "https://a.org/feed", Kind: "article", FetchInterval: time.Hour}, "Gone", now)
	if err != nil {
		t.Fatal(err)
	}
	kept, err = s.CreateSource(ctx, NewSource{URL: "https://b.org/feed", Kind: "article", FetchInterval: time.Hour}, "Kept", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range []Source{gone, kept} {
		items := []FeedEntry{{GUID: "g", Title: "zebra " + src.Title, URL: src.URL + "/1"}}
		if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: src.Kind}, "", "", items, "", time.Time{}, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE entries SET starred = 1 WHERE source_id = ?", gone.ID); err != nil {
		t.Fatal(err)
	}
	return s, gone, kept, now
}

func TestDeletedSourceDisappearsEverywhere(t *testing.T) {
	ctx := context.Background()
	s, gone, kept, now := deleteFixture(t)
	if err := s.DeleteSource(ctx, gone.ID, now); err != nil {
		t.Fatal(err)
	}
	for name, filter := range map[string]EntryFilter{
		"all": {}, "unread": {Unread: true}, "starred": {Starred: true}, "source": {SourceID: gone.ID},
		"kind": {Kind: "article"}, "search": {Query: "zebra"},
	} {
		list, err := s.Entries(ctx, filter, 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range list {
			if e.SourceID == gone.ID {
				t.Errorf("%s still lists an entry of the deleted source", name)
			}
		}
	}
	list, _ := s.Entries(ctx, EntryFilter{}, 10)
	if len(list) != 1 || list[0].SourceID != kept.ID {
		t.Fatalf("entries = %+v", list)
	}
	var entryID int64
	s.db.QueryRow("SELECT id FROM entries WHERE source_id = ?", gone.ID).Scan(&entryID)
	if _, err := s.Entry(ctx, entryID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Entry: %v", err)
	}
	read := true
	if _, err := s.UpdateEntry(ctx, entryID, EntryPatch{IsRead: &read}, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateEntry: %v", err)
	}
	if ids, err := s.MarkRead(ctx, EntryFilter{}, now); err != nil || len(ids) != 1 {
		t.Errorf("MarkRead = %v, %v; want only the kept entry", ids, err)
	}
	sources, _ := s.Sources(ctx)
	if len(sources) != 1 || sources[0].ID != kept.ID {
		t.Errorf("Sources = %+v", sources)
	}
	if _, err := s.Source(ctx, gone.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Source: %v", err)
	}
	if _, err := s.UpdateSource(ctx, gone.ID, SourcePatch{}, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateSource: %v", err)
	}
	if err := s.MarkSourceDue(ctx, gone.ID, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("MarkSourceDue: %v", err)
	}
	if n, _ := s.MarkAllSourcesDue(ctx, now); n != 1 {
		t.Errorf("MarkAllSourcesDue = %d", n)
	}
	due, _ := s.DueSources(ctx, now.Add(24*time.Hour), 10)
	if len(due) != 1 || due[0].ID != kept.ID {
		t.Errorf("DueSources = %+v: the fetch loop must skip a deleted source", due)
	}
	health, _ := s.SourcesHealth(ctx, now.Add(time.Hour))
	for _, h := range health {
		if h.ID == gone.ID {
			t.Error("SourcesHealth lists the deleted source")
		}
	}
	pending, _ := s.PendingExtractions(ctx, now.Add(time.Hour), 10)
	for _, p := range pending {
		if p.ID == entryID {
			t.Error("PendingExtractions lists an entry of the deleted source")
		}
	}
	if _, err := s.AIInput(ctx, entryID); !errors.Is(err, ErrNotFound) {
		t.Errorf("AIInput: %v", err)
	}
	if err := s.DeleteSource(ctx, gone.ID, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
}

func TestRestoreSourceWithinTheWindow(t *testing.T) {
	ctx := context.Background()
	s, gone, _, now := deleteFixture(t)
	if err := s.DeleteSource(ctx, gone.ID, now); err != nil {
		t.Fatal(err)
	}
	got, err := s.RestoreSource(ctx, gone.ID, now.Add(DeleteUndoWindow-time.Second))
	if err != nil || got.ID != gone.ID || got.Title != "Gone" {
		t.Fatalf("RestoreSource = %+v, %v", got, err)
	}
	list, _ := s.Entries(ctx, EntryFilter{Starred: true}, 10)
	if len(list) != 1 || list[0].SourceID != gone.ID {
		t.Fatalf("the starred entry did not come back: %+v", list)
	}
	if _, err := s.RestoreSource(ctx, gone.ID, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("restoring a source that is not deleted: %v", err)
	}
	if _, err := s.RestoreSource(ctx, 999, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("restoring an unknown source: %v", err)
	}
}

func TestRestoreSourceAfterTheWindow(t *testing.T) {
	ctx := context.Background()
	s, gone, _, now := deleteFixture(t)
	if err := s.DeleteSource(ctx, gone.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RestoreSource(ctx, gone.ID, now.Add(DeleteUndoWindow+time.Second)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("restore after the window: %v", err)
	}
}

func TestPurgeDeletedSources(t *testing.T) {
	ctx := context.Background()
	s, gone, kept, now := deleteFixture(t)
	if err := s.DeleteSource(ctx, gone.ID, now); err != nil {
		t.Fatal(err)
	}
	if n, err := s.PurgeDeletedSources(ctx, now.Add(DeleteUndoWindow-time.Second)); err != nil || n != 0 {
		t.Fatalf("purge inside the window = %d, %v", n, err)
	}
	if n, err := s.PurgeDeletedSources(ctx, now.Add(DeleteUndoWindow+time.Second)); err != nil || n != 1 {
		t.Fatalf("purge after the window = %d, %v", n, err)
	}
	var sources, entries int
	s.db.QueryRow("SELECT count(*) FROM sources").Scan(&sources)
	s.db.QueryRow("SELECT count(*) FROM entries WHERE source_id = ?", gone.ID).Scan(&entries)
	if sources != 1 || entries != 0 {
		t.Fatalf("%d sources left, %d entries of the purged one (starred ones go too)", sources, entries)
	}
	if _, err := s.Source(ctx, kept.ID); err != nil {
		t.Fatal(err)
	}
}

func TestAddingADeletedFeedAgainPurgesTheOldOne(t *testing.T) {
	ctx := context.Background()
	s, gone, _, now := deleteFixture(t)
	if err := s.DeleteSource(ctx, gone.ID, now); err != nil {
		t.Fatal(err)
	}
	again, err := s.CreateSource(ctx, NewSource{URL: "https://a.org/feed", Kind: "article", FetchInterval: time.Hour}, "Again", now.Add(time.Second))
	if err != nil {
		t.Fatalf("re-adding a deleted feed: %v", err)
	}
	if again.ID == gone.ID {
		t.Fatal("the new subscription reused the deleted one")
	}
	var old int
	s.db.QueryRow("SELECT count(*) FROM sources WHERE id = ?", gone.ID).Scan(&old)
	if old != 0 {
		t.Fatal("the deleted subscription was not purged")
	}
	if _, err := s.RestoreSource(ctx, gone.ID, now.Add(2*time.Second)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("restore after re-adding: %v", err)
	}
	if _, err := s.CreateSource(ctx, NewSource{URL: "https://a.org/feed", Kind: "article", FetchInterval: time.Hour}, "Twice", now); !errors.Is(err, ErrExists) {
		t.Fatalf("a live duplicate must still be refused: %v", err)
	}
}
