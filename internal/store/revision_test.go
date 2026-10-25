package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func revisionFixture(t *testing.T) (*Store, FeedSource, time.Time) {
	t.Helper()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.UnixMilli(1_700_000_000_000)
	src, err := s.CreateSource(context.Background(), NewSource{RSSHubPath: "/rev", Kind: "article", FetchInterval: time.Hour}, "Rev", now)
	if err != nil {
		t.Fatal(err)
	}
	return s, FeedSource{ID: src.ID, Kind: src.Kind}, now
}

func saveOne(t *testing.T, s *Store, src FeedSource, e FeedEntry, at time.Time) int64 {
	t.Helper()
	e.GUID = "g"
	if _, err := s.SaveFeed(context.Background(), src, "", "", []FeedEntry{e}, "", time.Time{}, at); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := s.db.QueryRow("SELECT id FROM entries WHERE source_id = ? AND guid = 'g'", src.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestParagraphsSplitBlocksAndSkipTrackingPictures(t *testing.T) {
	got := paragraphs(`<h2> Head  line </h2><p>One <b>bold</b>
	two</p><div>Three<br>Four</div><script>x()</script>` +
		`<p><img src="https://example.org/a.png"><img width="1" height="1" src="https://t.example/p.gif"></p><ul><li>Item</li></ul>`)
	want := []string{"Head line", "One bold two", "Three", "Four", "Item"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paragraphs = %q, want %q", got, want)
	}
	if got := paragraphs("  \n "); got != nil {
		t.Errorf("paragraphs of white space = %q", got)
	}
}

func TestSaveFeedKeepsTheEarlierVersionOnlyForASubstantiveChange(t *testing.T) {
	ctx := context.Background()
	s, src, now := revisionFixture(t)
	first := FeedEntry{Title: "Post", ContentHTML: "<p>Alpha</p><p>Beta</p>"}
	id := saveOne(t, s, src, first, now)
	if _, err := s.Revision(ctx, id); !errors.Is(err, ErrNoRevision) {
		t.Fatalf("Revision of a new entry = %v, want ErrNoRevision", err)
	}

	// White space and a tracking picture are not a change, and neither is the publication time.
	same := FeedEntry{Title: "  Post ", ContentHTML: "<p>Alpha </p>\n<p>Beta<img width=\"1\" height=\"1\" src=\"https://t.example/p.gif\"></p>",
		PublishedAt: now.Add(-time.Hour)}
	saveOne(t, s, src, same, now.Add(time.Minute))
	if _, err := s.Revision(ctx, id); !errors.Is(err, ErrNoRevision) {
		t.Fatalf("Revision after a change of white space = %v, want ErrNoRevision", err)
	}

	second := FeedEntry{Title: "Post", ContentHTML: "<p>Alpha</p><p>Gamma</p>"}
	saveOne(t, s, src, second, now.Add(2*time.Minute))
	rev, err := s.Revision(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rev.Paragraphs, []string{"Alpha", "Beta"}) || !reflect.DeepEqual(rev.CurrentParagraphs, []string{"Alpha", "Gamma"}) {
		t.Errorf("revision = %+v", rev)
	}
	if strings.TrimSpace(rev.Title) != "Post" || rev.CurrentTitle != "Post" {
		t.Errorf("titles = %q and %q", rev.Title, rev.CurrentTitle)
	}
	// The earlier version was fetched when the entry last changed (the whitespace-only fetch touched updated_at).
	if !rev.ReplacedAt.Equal(now.Add(2 * time.Minute)) {
		t.Errorf("ReplacedAt = %v, want %v", rev.ReplacedAt, now.Add(2*time.Minute))
	}
	if rev.FetchedAt.Before(now) || !rev.FetchedAt.Before(rev.ReplacedAt) {
		t.Errorf("FetchedAt = %v", rev.FetchedAt)
	}

	// Only the latest earlier version is kept, and a title change counts.
	saveOne(t, s, src, FeedEntry{Title: "Post, corrected", ContentHTML: "<p>Alpha</p><p>Gamma</p>"}, now.Add(3*time.Minute))
	rev, err = s.Revision(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rev.Paragraphs, []string{"Alpha", "Gamma"}) || rev.Title != "Post" || rev.CurrentTitle != "Post, corrected" {
		t.Errorf("second revision = %+v", rev)
	}
	var rows int
	if err := s.db.QueryRow("SELECT count(*) FROM entry_revisions").Scan(&rows); err != nil || rows != 1 {
		t.Errorf("%d revision rows, %v; want 1", rows, err)
	}
}

func TestEntryCarriesWhenItWasRevised(t *testing.T) {
	ctx := context.Background()
	s, src, now := revisionFixture(t)
	read := true
	id := saveOne(t, s, src, FeedEntry{Title: "Post", Summary: "old text"}, now)
	if _, err := s.UpdateEntry(ctx, id, EntryPatch{IsRead: &read}, now); err != nil {
		t.Fatal(err)
	}
	at := now.Add(time.Hour)
	saveOne(t, s, src, FeedEntry{Title: "Post", Summary: "new text"}, at)
	entry, err := s.Entry(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !entry.IsRead || !entry.RevisedAt.Equal(at) {
		t.Errorf("entry read=%v revisedAt=%v, want read and %v", entry.IsRead, entry.RevisedAt, at)
	}
	list, err := s.Entries(ctx, EntryFilter{}, 10)
	if err != nil || len(list) != 1 || !list[0].RevisedAt.Equal(at) {
		t.Errorf("list = %+v, %v", list, err)
	}
}

func TestRevisionGoesWithItsEntryWhenRetentionPrunes(t *testing.T) {
	ctx := context.Background()
	s, src, now := revisionFixture(t)
	id := saveOne(t, s, src, FeedEntry{Title: "Post", Summary: "old"}, now)
	saveOne(t, s, src, FeedEntry{Title: "Post", Summary: "new"}, now.Add(time.Minute))
	if _, err := s.Revision(ctx, id); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-100 * 24 * time.Hour).UnixMilli()
	if _, err := s.db.ExecContext(ctx, "UPDATE entries SET created_at = ?, published_at = ? WHERE id = ?", old, old, id); err != nil {
		t.Fatal(err)
	}
	if n, err := s.PruneEntries(ctx, now.Add(time.Hour), 30); err != nil || n != 1 {
		t.Fatalf("pruned %d, %v", n, err)
	}
	var rows int
	if err := s.db.QueryRow("SELECT count(*) FROM entry_revisions").Scan(&rows); err != nil || rows != 0 {
		t.Errorf("%d revision rows left, %v", rows, err)
	}
	if _, err := s.Revision(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("Revision of a pruned entry = %v, want ErrNotFound", err)
	}
}
