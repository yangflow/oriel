package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestPaceMovesExpiredUnreadEntriesOutOfAllAndUnread(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.Now()
	paced, err := s.CreateSource(ctx, NewSource{RSSHubPath: "/paced", Kind: "article", FetchInterval: time.Hour}, "Paced", now)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := s.CreateSource(ctx, NewSource{RSSHubPath: "/plain", Kind: "article", FetchInterval: time.Hour}, "Plain", now)
	if err != nil {
		t.Fatal(err)
	}
	pace := 3 * 3600
	if got, err := s.UpdateSource(ctx, paced.ID, SourcePatch{PaceSec: &pace}, now); err != nil || got.PaceSec != pace {
		t.Fatalf("UpdateSource = %+v, %v", got, err)
	}
	add := func(id int64, guid string, age time.Duration, read, starred int, published bool) int64 {
		at := now.Add(-age).UnixMilli()
		var pub any
		if published {
			pub = at
		}
		res, err := s.db.ExecContext(ctx, `INSERT INTO entries (source_id, guid, title, published_at, created_at, updated_at, is_read, starred)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, guid, "quasar "+guid, pub, at, at, read, starred)
		if err != nil {
			t.Fatal(err)
		}
		entryID, _ := res.LastInsertId()
		return entryID
	}
	fresh := add(paced.ID, "fresh", time.Hour, 0, 0, true)
	late := add(paced.ID, "late", 2*time.Hour+30*time.Minute, 0, 0, true)
	old := add(paced.ID, "old", 4*time.Hour, 0, 0, true)
	oldFetched := add(paced.ID, "old-fetched", 4*time.Hour, 0, 0, false)
	oldRead := add(paced.ID, "old-read", 4*time.Hour, 1, 0, true)
	oldStarred := add(paced.ID, "old-starred", 4*time.Hour, 0, 1, true)
	other := add(plain.ID, "other", 400*time.Hour, 0, 0, true)

	ids := func(filter EntryFilter) map[int64]bool {
		list, err := s.Entries(ctx, filter, 100)
		if err != nil {
			t.Fatal(err)
		}
		got := map[int64]bool{}
		for _, e := range list {
			got[e.ID] = true
		}
		return got
	}
	check := func(name string, got map[int64]bool, want ...int64) {
		t.Helper()
		if len(got) != len(want) {
			t.Errorf("%s = %v, want %v", name, got, want)
			return
		}
		for _, id := range want {
			if !got[id] {
				t.Errorf("%s = %v, want %v", name, got, want)
				return
			}
		}
	}
	check("All", ids(EntryFilter{}), fresh, late, oldRead, oldStarred, other)
	check("Unread", ids(EntryFilter{Unread: true}), fresh, late, oldStarred, other)
	check("Starred", ids(EntryFilter{Starred: true}), oldStarred)
	check("source view", ids(EntryFilter{SourceID: paced.ID}), fresh, late, old, oldFetched, oldRead, oldStarred)
	check("source Unread", ids(EntryFilter{SourceID: paced.ID, Unread: true}), fresh, late, old, oldFetched, oldStarred)
	check("search", ids(EntryFilter{Query: "quasar"}), fresh, late, old, oldFetched, oldRead, oldStarred, other)

	// The unread count of the source leaves out the expired entries.
	if got, err := s.Source(ctx, paced.ID); err != nil || got.UnreadCount != 3 {
		t.Errorf("unread count = %d, %v, want 3", got.UnreadCount, err)
	}

	// The entries carry when they fade and leave: the last quarter of the pace.
	list, err := s.Entries(ctx, EntryFilter{SourceID: paced.ID}, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range list {
		switch e.ID {
		case fresh:
			if !e.ExpiresAt.Equal(e.PublishedAt.Add(3*time.Hour)) || !e.FadesAt.Equal(e.PublishedAt.Add(2*time.Hour+15*time.Minute)) {
				t.Errorf("fresh fades at %v and expires at %v", e.FadesAt, e.ExpiresAt)
			}
		case oldFetched:
			if !e.ExpiresAt.Equal(e.CreatedAt.Add(3 * time.Hour)) {
				t.Errorf("an entry without a published time expires at %v, created %v", e.ExpiresAt, e.CreatedAt)
			}
		case oldRead, oldStarred:
			if !e.FadesAt.IsZero() || !e.ExpiresAt.IsZero() {
				t.Errorf("entry %d is read or starred and still expires at %v", e.ID, e.ExpiresAt)
			}
		}
	}
	all, err := s.Entries(ctx, EntryFilter{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range all {
		if e.SourceID == plain.ID && (!e.FadesAt.IsZero() || !e.ExpiresAt.IsZero()) {
			t.Errorf("a source without a pace expires entry %d", e.ID)
		}
	}

	// Marking All as read leaves the expired entries alone; they are neither read nor deleted.
	marked, err := s.MarkRead(ctx, EntryFilter{}, now)
	if err != nil || len(marked) != 4 {
		t.Fatalf("MarkRead = %v, %v, want 4 entries", marked, err)
	}
	for _, id := range []int64{old, oldFetched} {
		e, err := s.Entry(ctx, id)
		if err != nil || e.IsRead {
			t.Errorf("expired entry %d = %+v, %v, want it unread", id, e, err)
		}
	}

	// Turning the pace off brings them back.
	zero := 0
	if _, err := s.UpdateSource(ctx, paced.ID, SourcePatch{PaceSec: &zero}, now); err != nil {
		t.Fatal(err)
	}
	check("Unread without a pace", ids(EntryFilter{Unread: true}), old, oldFetched)
}

func TestPaceDefaultsToOffAndRefusesNegativeValues(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	src, err := s.CreateSource(ctx, NewSource{RSSHubPath: "/a", Kind: "article", FetchInterval: time.Hour}, "", time.Now())
	if err != nil || src.PaceSec != 0 {
		t.Fatalf("CreateSource = %+v, %v", src, err)
	}
	bad := -1
	if _, err := s.UpdateSource(ctx, src.ID, SourcePatch{PaceSec: &bad}, time.Now()); err == nil {
		t.Error("a negative pace was accepted")
	}
}
