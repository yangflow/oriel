package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestGroupKey(t *testing.T) {
	if groupKey("https://Example.org/a/b", "") != groupKey("http://www.example.org/a/b/", "") {
		t.Error("scheme, www, case of the host and a trailing slash must not matter")
	}
	if groupKey("https://example.org:443/a?utm_source=x&b=2&a=1", "") != groupKey("https://example.org/a?a=1&b=2&fbclid=z", "") {
		t.Error("tracking parameters and parameter order must not matter")
	}
	if groupKey("", "https://example.org/a") != groupKey("https://example.org/a", "") || groupKey("", "https://example.org/a") == "" {
		t.Error("an address as the GUID stands in for a missing link")
	}
	for _, link := range []string{"", "not a url", "ftp://example.org/a", "https://example.org", "https://example.org/", "https://example.org/?utm_source=x"} {
		if k := groupKey(link, "opaque-guid"); k != "" {
			t.Errorf("groupKey(%q) = %q, want none", link, k)
		}
	}
	if groupKey("https://example.org/A", "") == groupKey("https://example.org/a", "") {
		t.Error("the path is case sensitive")
	}
	if groupKey("https://example.org/a?id=1", "") == groupKey("https://example.org/a?id=2", "") {
		t.Error("other parameters tell articles apart")
	}
}

// groupedFixture holds one article carried by three sources (a, b, c, in this order of arrival), one more link in a
// and one in b.
func groupedFixture(t *testing.T) (*Store, []Source, map[string]int64) {
	t.Helper()
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	var srcs []Source
	base := time.Now()
	for i, name := range []string{"a", "b", "c"} {
		src, err := s.CreateSource(ctx, NewSource{URL: "https://" + name + ".example/feed", Kind: "article", FetchInterval: time.Hour}, name, base)
		if err != nil {
			t.Fatal(err)
		}
		items := []FeedEntry{{GUID: name + "-same", Title: name + "-same", URL: "https://news.example/story?utm_source=" + name}}
		switch name {
		case "a":
			items = append(items, FeedEntry{GUID: "a-own", Title: "a-own", URL: "https://news.example/own"})
		case "b":
			items = append(items, FeedEntry{GUID: "b-other", Title: "b-other", URL: "https://news.example/other"})
		}
		at := base.Add(time.Duration(i+1) * time.Minute)
		if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: "article"}, name, "", items, "", time.Time{}, at); err != nil {
			t.Fatal(err)
		}
		srcs = append(srcs, src)
	}
	ids := map[string]int64{}
	all, err := s.Entries(ctx, EntryFilter{}, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range all {
		ids[e.Title] = e.ID
	}
	return s, srcs, ids
}

func TestSameLinkShowsOnce(t *testing.T) {
	ctx := context.Background()
	s, srcs, ids := groupedFixture(t)
	plain, _ := s.Entries(ctx, EntryFilter{}, 50)
	if len(plain) != 5 {
		t.Fatalf("without grouping: %d rows", len(plain))
	}
	list, err := s.Entries(ctx, EntryFilter{Group: true}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("grouped: %d rows, want 3", len(list))
	}
	for _, e := range list {
		if e.Title == "b-same" || e.Title == "c-same" {
			t.Errorf("%s shows as a row of its own", e.Title)
		}
		if e.Title != "a-same" && len(e.Group) != 0 {
			t.Errorf("%s has a group", e.Title)
		}
		if e.Title != "a-same" {
			continue
		}
		if len(e.Group) != 2 || e.Group[0].ID != ids["b-same"] || e.Group[1].ID != ids["c-same"] || e.Group[0].SourceTitle != "b" {
			t.Errorf("group = %+v", e.Group)
		}
	}
	// A single source or a search shows every copy and no group.
	for _, f := range []EntryFilter{{SourceID: srcs[1].ID, Group: true}, {Query: "same", Group: true}} {
		got, err := s.Entries(ctx, f, 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range got {
			if len(e.Group) != 0 {
				t.Errorf("%+v groups", f)
			}
		}
		if f.SourceID != 0 && len(got) != 2 {
			t.Errorf("one source: %d rows", len(got))
		}
		if f.Query != "" && len(got) != 3 {
			t.Errorf("search: %d rows", len(got))
		}
	}
}

func TestGroupCountsOnce(t *testing.T) {
	ctx := context.Background()
	s, _, _ := groupedFixture(t)
	if n, err := s.UnreadTotal(ctx, false); err != nil || n != 5 {
		t.Fatalf("ungrouped = %d %v", n, err)
	}
	if n, err := s.UnreadTotal(ctx, true); err != nil || n != 3 {
		t.Fatalf("grouped = %d %v", n, err)
	}
}

func TestReadAndStarApplyToTheGroup(t *testing.T) {
	ctx := context.Background()
	s, _, ids := groupedFixture(t)
	now := time.Now()
	yes, no := true, false
	// Reading the row marks every visible copy.
	e, err := s.UpdateEntry(ctx, ids["a-same"], EntryPatch{IsRead: &yes, Group: true}, now)
	if err != nil || !e.IsRead {
		t.Fatalf("%+v %v", e, err)
	}
	if n := count(t, s, "SELECT count(*) FROM entries WHERE guid LIKE '%-same' AND is_read = 1"); n != 3 {
		t.Fatalf("%d copies read, want 3", n)
	}
	if n, _ := s.UnreadTotal(ctx, true); n != 2 {
		t.Fatalf("unread = %d", n)
	}
	if list, _ := s.Entries(ctx, EntryFilter{Unread: true, Group: true}, 50); len(list) != 2 {
		t.Fatalf("unread rows = %d", len(list))
	}
	// Without Group only the one entry changes; the group still reads as read while any copy is.
	if _, err := s.UpdateEntry(ctx, ids["b-same"], EntryPatch{IsRead: &no}, now); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Entry(ctx, ids["a-same"])
	got, err = s.WithGroup(ctx, got)
	if err != nil || !got.IsRead || len(got.Group) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	// Starring any member shows the group as starred.
	if _, err := s.UpdateEntry(ctx, ids["c-same"], EntryPatch{Starred: &yes}, now); err != nil {
		t.Fatal(err)
	}
	list, err := s.Entries(ctx, EntryFilter{Starred: true, Group: true}, 50)
	if err != nil || len(list) != 1 || list[0].ID != ids["a-same"] || !list[0].Starred {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestMarkAllReadMarksWholeGroups(t *testing.T) {
	ctx := context.Background()
	s, _, _ := groupedFixture(t)
	if _, err := s.MarkRead(ctx, EntryFilter{Unread: true, Group: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, "SELECT count(*) FROM entries WHERE is_read = 0"); n != 0 {
		t.Fatalf("%d entries left unread", n)
	}
}

func TestDifferentLinksStaySeparate(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	a, _ := s.CreateSource(ctx, NewSource{URL: "https://a.example/feed", Kind: "article", FetchInterval: time.Hour}, "a", time.Now())
	b, _ := s.CreateSource(ctx, NewSource{URL: "https://b.example/feed", Kind: "article", FetchInterval: time.Hour}, "b", time.Now())
	for _, p := range []struct {
		src  Source
		link string
	}{{a, "https://x.example/p?id=1"}, {b, "https://x.example/p?id=2"}, {a, "https://x.example/q"}, {b, "https://x.example/Q"}} {
		if _, err := s.SaveFeed(ctx, FeedSource{ID: p.src.ID, Kind: "article"}, "", "", []FeedEntry{{GUID: p.link, Title: p.link, URL: p.link}}, "", time.Time{}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.Entries(ctx, EntryFilter{Group: true}, 50)
	if err != nil || len(list) != 4 {
		t.Fatalf("%d rows %v", len(list), err)
	}
}

func TestMutedCopiesDoNotLeadAGroup(t *testing.T) {
	ctx := context.Background()
	s, srcs, ids := groupedFixture(t)
	muted := "a-same"
	if _, err := s.UpdateSource(ctx, srcs[0].ID, SourcePatch{MuteKeywords: &muted}, time.Now()); err != nil {
		t.Fatal(err)
	}
	list, err := s.Entries(ctx, EntryFilter{Group: true}, 50)
	if err != nil {
		t.Fatal(err)
	}
	var main *Entry
	for i, e := range list {
		if e.Title == "a-same" {
			t.Fatal("the muted copy is shown")
		}
		if e.ID == ids["b-same"] {
			main = &list[i]
		}
	}
	if main == nil || len(main.Group) != 1 || main.Group[0].ID != ids["c-same"] {
		t.Fatalf("the next visible copy should lead: %+v", main)
	}
	if n, _ := s.UnreadTotal(ctx, true); n != 3 {
		t.Fatalf("unread = %d", n)
	}
}

func TestGroupSameLinkSetting(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	if on, err := s.GroupSameLink(ctx); err != nil || !on {
		t.Fatalf("default = %v %v", on, err)
	}
	if err := s.SetSetting(ctx, GroupSameLinkSetting, "0"); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.GroupSameLink(ctx); on {
		t.Fatal("still on")
	}
}
