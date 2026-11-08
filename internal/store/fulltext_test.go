package store

import (
	"context"
	"testing"
)

// A source with the full text off leaves new entries skipped, and starring one asks for its full text after all.
func TestFullTextSwitch(t *testing.T) {
	ctx := context.Background()
	s, src, now := revisionFixture(t)
	status := func(title string) (int64, string) {
		t.Helper()
		list, err := s.Entries(ctx, EntryFilter{}, 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range list {
			if e.Title == title {
				return e.ID, e.ExtractStatus
			}
		}
		t.Fatalf("no entry %q", title)
		return 0, ""
	}
	if _, err := s.SaveFeed(ctx, src, "", "", []FeedEntry{{GUID: "on", Title: "on"}}, "", now, now); err != nil {
		t.Fatal(err)
	}
	if _, got := status("on"); got != "pending" {
		t.Fatalf("with the switch on: %q", got)
	}
	off := false
	if _, err := s.UpdateSource(ctx, src.ID, SourcePatch{FullText: &off}, now); err != nil {
		t.Fatal(err)
	}
	if read, err := s.Source(ctx, src.ID); err != nil || read.FullText {
		t.Fatalf("source %+v, %v", read, err)
	}
	if _, got := status("on"); got != "pending" {
		t.Fatalf("turning it off changed an entry already there: %q", got)
	}
	due, err := s.DueSources(ctx, now.AddDate(1, 0, 0), 10)
	if err != nil || len(due) != 1 || !due[0].SkipFullText {
		t.Fatalf("due %+v, %v", due, err)
	}
	if _, err := s.SaveFeed(ctx, due[0], "", "", []FeedEntry{{GUID: "off", Title: "off"}}, "", now, now); err != nil {
		t.Fatal(err)
	}
	id, got := status("off")
	if got != "skipped" {
		t.Fatalf("with the switch off: %q", got)
	}
	yes := true
	if _, err := s.UpdateEntry(ctx, id, EntryPatch{Starred: &yes}, now); err != nil {
		t.Fatal(err)
	}
	if _, got := status("off"); got != "pending" {
		t.Fatalf("after starring: %q", got)
	}
}
