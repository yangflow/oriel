package store

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// TestSearchSyntax runs the operators of the search against two subscriptions of different kinds and times.
func TestSearchSyntax(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	blog, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/blog", Kind: "article", FetchInterval: time.Hour}, "The Go Blog", now)
	tube, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/tube", Kind: "video", FetchInterval: time.Hour}, "Tube_100%", now)
	save := func(src Source, entries ...FeedEntry) {
		t.Helper()
		if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: src.Kind}, "", "", entries, "", time.Time{}, now); err != nil {
			t.Fatal(err)
		}
	}
	at := func(day, hour int) time.Time { return time.Date(2026, 2, day, hour, 0, 0, 0, time.UTC) }
	save(blog,
		FeedEntry{GUID: "1", Title: "Generics in practice", Summary: "type parameters", PublishedAt: at(10, 12)},
		FeedEntry{GUID: "2", Title: "Generics sale announced", Summary: "big sale of ads", PublishedAt: at(20, 23)},
		FeedEntry{GUID: "3", Title: "苹果发布会", Summary: "新品", PublishedAt: at(1, 0)},
	)
	save(tube, FeedEntry{GUID: "4", Title: "Generics talk", PublishedAt: at(15, 8)})
	sydney, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Skip("no time zone database")
	}
	search := func(query string, loc *time.Location) (titles []string) {
		t.Helper()
		got, err := s.Entries(ctx, EntryFilter{Query: query, Location: loc}, 20)
		if err != nil {
			t.Fatalf("%q: %v", query, err)
		}
		for _, e := range got {
			titles = append(titles, e.Title)
		}
		slices.Sort(titles)
		return titles
	}
	for _, c := range []struct {
		query string
		loc   *time.Location
		want  []string
	}{
		{`generics`, time.UTC, []string{"Generics in practice", "Generics sale announced", "Generics talk"}},
		{`generics practice`, time.UTC, []string{"Generics in practice"}},
		{`"generics in"`, time.UTC, []string{"Generics in practice"}},
		{`generics -sale`, time.UTC, []string{"Generics in practice", "Generics talk"}},
		{`generics -"sale of"`, time.UTC, []string{"Generics in practice", "Generics talk"}},
		{`-generics`, time.UTC, []string{"苹果发布会"}},
		{`generics -ad`, time.UTC, []string{"Generics in practice", "Generics talk"}},
		{`-苹果`, time.UTC, []string{"Generics in practice", "Generics sale announced", "Generics talk"}},
		{`from:blog`, time.UTC, []string{"Generics in practice", "Generics sale announced", "苹果发布会"}},
		{`from:"go blog" generics`, time.UTC, []string{"Generics in practice", "Generics sale announced"}},
		{`from:GO from:tube`, time.UTC, []string{"Generics in practice", "Generics sale announced", "Generics talk", "苹果发布会"}},
		{`from:100%`, time.UTC, []string{"Generics talk"}},
		{`from:_`, time.UTC, []string{"Generics talk"}},
		{`from:nobody`, time.UTC, nil},
		{`kind:video`, time.UTC, []string{"Generics talk"}},
		{`kind:video kind:article generics`, time.UTC, []string{"Generics in practice", "Generics sale announced", "Generics talk"}},
		{`kind:article generics`, time.UTC, []string{"Generics in practice", "Generics sale announced"}},
		{`after:2026-02-15 generics`, time.UTC, []string{"Generics sale announced", "Generics talk"}},
		{`before:2026-02-15 generics`, time.UTC, []string{"Generics in practice"}},
		{`after:2026-02-10 before:2026-02-11`, time.UTC, []string{"Generics in practice"}},
		{`after:2026-02-20 before:2026-02-20`, time.UTC, nil},
		{`after:2026-02-01 before:2026-02-01`, time.UTC, nil},
		{`after:2026-02-01 before:2026-02-02`, time.UTC, []string{"苹果发布会"}},
		// The same days begin earlier in Sydney (UTC+11 in February), so the entry at 23:00 UTC on the 20th is the 21st.
		{`after:2026-02-21 before:2026-02-22`, sydney, []string{"Generics sale announced"}},
		{`after:2026-02-21 before:2026-02-22`, time.UTC, nil},
		{`苹果`, time.UTC, []string{"苹果发布会"}},
		{`苹果 kind:article -新品`, time.UTC, nil},
		{`苹果 kind:article -新`, time.UTC, nil},
		{`from:blog 发布 -sale`, time.UTC, []string{"苹果发布会"}},
		{`kind:podcast after:soon`, time.UTC, nil},
		{`generics from: -`, time.UTC, nil},
	} {
		if got := search(c.query, c.loc); !slices.Equal(got, c.want) {
			t.Errorf("search %q in %v = %q, want %q", c.query, c.loc, got, c.want)
		}
	}
}
