package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestFireRemindersBringsEntriesBackToTheTopOfUnread(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.Now().Truncate(time.Millisecond)
	src, err := s.CreateSource(ctx, NewSource{RSSHubPath: "/r", Kind: "article", FetchInterval: time.Hour}, "R", now)
	if err != nil {
		t.Fatal(err)
	}
	add := func(guid string, at time.Time) int64 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO entries (source_id, guid, title, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			src.ID, guid, guid, at.UnixMilli(), at.UnixMilli())
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	old := add("old", now.Add(-3*time.Hour))
	add("newer", now.Add(-time.Hour))
	at := now.Add(time.Hour).UnixMilli()
	got, err := s.UpdateEntry(ctx, old, EntryPatch{RemindAt: &at}, now)
	if err != nil || !got.IsRead || got.RemindAt.UnixMilli() != at {
		t.Fatalf("UpdateEntry = %+v, %v", got, err)
	}
	if n, err := s.FireReminders(ctx, now); err != nil || n != 0 {
		t.Fatalf("FireReminders before the time = %d, %v", n, err)
	}
	if n, err := s.FireReminders(ctx, now.Add(2*time.Hour)); err != nil || n != 1 {
		t.Fatalf("FireReminders after the time = %d, %v", n, err)
	}
	list, err := s.Entries(ctx, EntryFilter{Unread: true}, 10)
	if err != nil || len(list) != 2 || list[0].ID != old || !list[0].RemindAt.IsZero() {
		t.Fatalf("Unread = %+v, %v", list, err)
	}
}
