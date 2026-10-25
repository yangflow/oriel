package store

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHideFromAllKeepsASourceOutOfAllAndTheKindTimelines(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.Now()
	hidden, err := s.CreateSource(ctx, NewSource{RSSHubPath: "/hidden", Kind: "article", FetchInterval: time.Hour}, "Hidden", now)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := s.CreateSource(ctx, NewSource{RSSHubPath: "/plain", Kind: "article", FetchInterval: time.Hour}, "Plain", now)
	if err != nil {
		t.Fatal(err)
	}
	if hidden.HideFromAll {
		t.Fatal("a new source is hidden from All")
	}
	on := true
	if got, err := s.UpdateSource(ctx, hidden.ID, SourcePatch{HideFromAll: &on}, now); err != nil || !got.HideFromAll {
		t.Fatalf("UpdateSource = %+v, %v", got, err)
	}
	add := func(id int64, guid, link string, read, starred int) int64 {
		at := now.Add(-time.Hour).UnixMilli()
		res, err := s.db.ExecContext(ctx, `INSERT INTO entries (source_id, guid, title, url, link_key, created_at, updated_at, is_read, starred)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, guid, "quasar "+guid, link, link, at, at, read, starred)
		if err != nil {
			t.Fatal(err)
		}
		entryID, _ := res.LastInsertId()
		return entryID
	}
	hiddenUnread := add(hidden.ID, "h1", "", 0, 0)
	hiddenUnread2 := add(hidden.ID, "h2", "", 0, 0)
	hiddenStarred := add(hidden.ID, "h3", "", 0, 1)
	visible := add(plain.ID, "p1", "", 0, 0)
	// One article both sources carry: the hidden copy arrived first.
	sharedHidden := add(hidden.ID, "s-h", "https://example.org/a", 0, 0)
	time.Sleep(2 * time.Millisecond)
	sharedPlain := add(plain.ID, "s-p", "https://example.org/a", 0, 0)

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
	for _, group := range []bool{false, true} {
		name := map[bool]string{false: " ungrouped", true: " grouped"}[group]
		check("All"+name, ids(EntryFilter{Group: group}), visible, sharedPlain)
		check("Unread"+name, ids(EntryFilter{Unread: true, Group: group}), visible, sharedPlain)
		check("kind"+name, ids(EntryFilter{Kind: "article", Group: group}), visible, sharedPlain)
		check("Starred"+name, ids(EntryFilter{Starred: true, Group: group}), hiddenStarred)
		check("search"+name, ids(EntryFilter{Query: "quasar", Group: group}),
			hiddenUnread, hiddenUnread2, hiddenStarred, visible, sharedHidden, sharedPlain)
	}
	check("source view", ids(EntryFilter{SourceID: hidden.ID, Group: true}), hiddenUnread, hiddenUnread2, hiddenStarred, sharedHidden)
	check("source Unread", ids(EntryFilter{SourceID: hidden.ID, Unread: true}), hiddenUnread, hiddenUnread2, hiddenStarred, sharedHidden)

	// The article carried by both sources is one row from the visible source, with no hidden copy among the others.
	list, err := s.Entries(ctx, EntryFilter{Group: true}, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range list {
		if len(e.Group) != 0 {
			t.Errorf("entry %d lists %v as other copies, want none", e.ID, e.Group)
		}
	}

	// The unread totals leave the hidden source out; its own count stays.
	for _, group := range []bool{false, true} {
		if got, err := s.UnreadTotal(ctx, group); err != nil || got != 2 {
			t.Errorf("UnreadTotal(group=%v) = %d, %v, want 2", group, got, err)
		}
	}
	if got, err := s.Source(ctx, hidden.ID); err != nil || got.UnreadCount != 4 {
		t.Errorf("hidden source unread = %d, %v, want 4", got.UnreadCount, err)
	}
	all, err := s.Sources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range all {
		if src.ID == hidden.ID && src.UnreadCount != 4 {
			t.Errorf("listed unread of the hidden source = %d, want 4", src.UnreadCount)
		}
	}

	// Marking All or a kind as read leaves the hidden source's entries unread, grouped or not.
	for _, filter := range []EntryFilter{{}, {Group: true}, {Kind: "article", Group: true}} {
		marked, err := s.MarkRead(ctx, filter, now)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range marked {
			switch id {
			case hiddenUnread, hiddenUnread2, hiddenStarred, sharedHidden:
				t.Errorf("MarkRead(%+v) marked the hidden entry %d", filter, id)
			}
		}
		if _, err := s.MarkUnread(ctx, marked, 0); err != nil {
			t.Fatal(err)
		}
	}
	// Reading one copy as a group leaves the hidden copy alone.
	read := true
	if _, err := s.UpdateEntry(ctx, sharedPlain, EntryPatch{IsRead: &read, Group: true}, now); err != nil {
		t.Fatal(err)
	}
	if e, err := s.Entry(ctx, sharedHidden); err != nil || e.IsRead {
		t.Errorf("hidden copy = %+v, %v, want it unread", e, err)
	}
	// The source's own view marks its entries.
	marked, err := s.MarkRead(ctx, EntryFilter{SourceID: hidden.ID}, now)
	if err != nil || len(marked) != 4 {
		t.Errorf("MarkRead of the hidden source = %v, %v, want 4 entries", marked, err)
	}

	// Showing it again brings the entries back.
	off := false
	if _, err := s.UpdateSource(ctx, hidden.ID, SourcePatch{HideFromAll: &off}, now); err != nil {
		t.Fatal(err)
	}
	check("All after showing", ids(EntryFilter{}), hiddenUnread, hiddenUnread2, hiddenStarred, visible, sharedHidden, sharedPlain)
}

func TestHideFromAllKeepsItsIndexes(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	for _, c := range []struct {
		name   string
		filter EntryFilter
		index  string
	}{
		{"all", EntryFilter{}, "entries_timeline"},
		{"all, grouped", EntryFilter{Group: true}, "entries_timeline"},
		{"unread", EntryFilter{Unread: true}, "entries_unread_timeline"},
		{"unread, grouped", EntryFilter{Unread: true, Group: true}, "entries_unread_timeline"},
		{"kind", EntryFilter{Kind: "image"}, "entries_kind_timeline"},
		{"unread kind", EntryFilter{Kind: "image", Unread: true}, "entries_kind_unread_timeline"},
	} {
		query, args := c.filter.timelineQuery()
		text := queryPlan(t, s, query, append(args, 50)...)
		if !strings.Contains(text, "INDEX "+c.index) || strings.Contains(text, "TEMP B-TREE") || strings.Contains(text, "SCAN s") {
			t.Errorf("%s: want %s and no sorting, got\n%s", c.name, c.index, text)
		}
	}
	where, args := EntryFilter{Unread: true, Group: true}.where()
	text := queryPlan(t, s, "SELECT count(*) FROM entries e JOIN sources s ON s.id = e.source_id WHERE "+where, args...)
	if !strings.Contains(text, "INDEX entries_unread_timeline") {
		t.Errorf("the unread total does not use the unread index:\n%s", text)
	}
}

func TestHideFromAllSurvivesABackup(t *testing.T) {
	ctx := context.Background()
	s, _ := seeded(t)
	all, err := s.Sources(ctx)
	if err != nil || len(all) == 0 {
		t.Fatalf("%v %v", all, err)
	}
	on := true
	if _, err := s.UpdateSource(ctx, all[0].ID, SourcePatch{HideFromAll: &on}, time.Now()); err != nil {
		t.Fatal(err)
	}
	data := backupOf(t, s)

	other := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	r, err := other.PrepareRestore(ctx, bytes.NewReader(data), MaxBackup)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Apply(ctx)
	r.Discard()
	if err != nil {
		t.Fatal(err)
	}
	if count(t, other, "SELECT count(*) FROM sources WHERE hide_from_all = 1") != 1 {
		t.Fatal("the switch was not restored")
	}
}
