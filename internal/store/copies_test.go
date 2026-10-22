package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// copyFixture makes a source with entries "a" and "b", and returns the store and the ids.
func copyFixture(t *testing.T) (*Store, Source, int64, int64, time.Time) {
	t.Helper()
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	at := time.UnixMilli(1_000_000)
	src, err := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "", at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: src.Kind}, "", "", []FeedEntry{
		{GUID: "a", Title: "a", URL: "https://example.org/a", ThumbnailURL: "https://example.org/a.png", ContentHTML: `<img src="/in.png">`},
		{GUID: "b", Title: "b", URL: "https://example.org/b"},
	}, "", time.Time{}, at); err != nil {
		t.Fatal(err)
	}
	id := func(guid string) int64 {
		var n int64
		if err := s.db.QueryRow("SELECT id FROM entries WHERE guid = ?", guid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// The full text of an article is fetched before its pictures are copied, so the fixture has it already.
	for _, n := range []int64{id("a"), id("b")} {
		if err := s.SaveExtraction(context.Background(), n, "<p>text</p>", "text", ""); err != nil {
			t.Fatal(err)
		}
	}
	return s, src, id("a"), id("b"), at
}

func star(t *testing.T, s *Store, id int64, on bool) {
	t.Helper()
	if _, err := s.UpdateEntry(context.Background(), id, EntryPatch{Starred: &on}, time.UnixMilli(2_000_000)); err != nil {
		t.Fatal(err)
	}
}

func TestStarringQueuesTheCopy(t *testing.T) {
	ctx := context.Background()
	s, _, a, b, at := copyFixture(t)
	if list, err := s.PendingCopies(ctx, at, 10); err != nil || len(list) != 0 {
		t.Fatalf("nothing is starred yet: %v, %v", list, err)
	}
	star(t, s, a, true)
	list, err := s.PendingCopies(ctx, at, 10)
	if err != nil || len(list) != 1 || list[0].ID != a || list[0].ThumbnailURL != "https://example.org/a.png" ||
		list[0].ContentHTML == "" || list[0].Attempts != 0 {
		t.Fatalf("pending = %+v, %v", list, err)
	}
	star(t, s, a, false)
	if list, _ := s.PendingCopies(ctx, at, 10); len(list) != 0 {
		t.Errorf("an unstarred entry is still pending: %+v", list)
	}
	_ = b
}

func TestCopyKeepsAndRemovesPictures(t *testing.T) {
	ctx := context.Background()
	s, _, a, b, at := copyFixture(t)
	star(t, s, a, true)
	star(t, s, b, true)
	if err := s.SaveImageCopy(ctx, a, "https://example.org/a.png", "image/png", make([]byte, 1000), at); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveImageCopy(ctx, a, "https://example.org/a.png", "image/png", make([]byte, 5), at); err != nil {
		t.Fatal(err) // a second save of the same picture keeps the first
	}
	if err := s.SaveImageCopy(ctx, b, "https://example.org/b.png", "image/jpeg", make([]byte, 500), at); err != nil {
		t.Fatal(err)
	}
	if n, size, err := s.CopyStats(ctx); err != nil || n != 2 || size != 1500 {
		t.Fatalf("stats = %d, %d, %v", n, size, err)
	}
	kind, body, ok, err := s.ImageCopy(ctx, "https://example.org/a.png")
	if err != nil || !ok || kind != "image/png" || len(body) != 1000 {
		t.Fatalf("copy = %q, %d bytes, %v, %v", kind, len(body), ok, err)
	}
	if _, _, ok, _ := s.ImageCopy(ctx, "https://example.org/none.png"); ok {
		t.Error("a picture never copied was found")
	}
	if have, _ := s.CopiedAddresses(ctx, a); len(have) != 1 || !have["https://example.org/a.png"] {
		t.Errorf("addresses = %v", have)
	}

	star(t, s, a, false)
	if _, _, ok, _ := s.ImageCopy(ctx, "https://example.org/a.png"); ok {
		t.Error("the copy outlived the star")
	}
	if n, size, _ := s.CopyStats(ctx); n != 1 || size != 500 {
		t.Errorf("after unstarring one: %d, %d", n, size)
	}
	// An entry that is no longer starred gets nothing, e.g. when it was unstarred while its pictures downloaded.
	if err := s.SaveImageCopy(ctx, a, "https://example.org/late.png", "image/png", []byte("x"), at); err != nil {
		t.Fatal(err)
	}
	if n, _, _ := s.CopyStats(ctx); n != 1 {
		t.Errorf("a late picture was stored: %d", n)
	}
}

func TestCopyRetryAndGiveUp(t *testing.T) {
	ctx := context.Background()
	s, _, a, _, at := copyFixture(t)
	star(t, s, a, true)
	if err := s.FinishCopy(ctx, a, false, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.PendingCopies(ctx, at, 10); len(list) != 0 {
		t.Errorf("a failed copy came back before its retry: %+v", list)
	}
	list, _ := s.PendingCopies(ctx, at.Add(time.Minute), 10)
	if len(list) != 1 || list[0].Attempts != 1 {
		t.Fatalf("due retry = %+v", list)
	}
	if err := s.FinishCopy(ctx, a, false, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.PendingCopies(ctx, at.Add(100*time.Hour), 10); len(list) != 0 {
		t.Errorf("a given-up copy came back: %+v", list)
	}
	star(t, s, a, false)
	star(t, s, a, true) // starring again starts over
	if list, _ := s.PendingCopies(ctx, at, 10); len(list) != 1 || list[0].Attempts != 0 {
		t.Errorf("starred again = %+v", list)
	}
	if err := s.FinishCopy(ctx, a, true, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.PendingCopies(ctx, at.Add(100*time.Hour), 10); len(list) != 0 {
		t.Errorf("a finished copy is pending: %+v", list)
	}
}

func TestStarringRetriesAFailedExtraction(t *testing.T) {
	ctx := context.Background()
	s, _, a, _, at := copyFixture(t)
	if err := s.FailExtraction(ctx, a, time.Time{}); err != nil {
		t.Fatal(err)
	}
	var status string
	s.db.QueryRow("SELECT extract_status FROM entries WHERE id = ?", a).Scan(&status)
	if status != "failed" {
		t.Fatalf("status %q", status)
	}
	star(t, s, a, true)
	s.db.QueryRow("SELECT extract_status FROM entries WHERE id = ?", a).Scan(&status)
	if status != "pending" {
		t.Errorf("a starred entry keeps its failed extraction: %q", status)
	}
	if list, _ := s.PendingExtractions(ctx, at, 10); len(list) == 0 {
		t.Error("the starred entry is not queued for its full text")
	}
}

func TestCopiesGoWithTheSource(t *testing.T) {
	ctx := context.Background()
	s, src, a, _, at := copyFixture(t)
	star(t, s, a, true)
	if err := s.SaveImageCopy(ctx, a, "https://example.org/a.png", "image/png", []byte("x"), at); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSource(ctx, src.ID, at); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.PendingCopies(ctx, at, 10); len(list) != 0 {
		t.Errorf("a deleted source's entry is still copied: %+v", list)
	}
	if _, err := s.PurgeDeletedSources(ctx, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n, size, _ := s.CopyStats(ctx); n != 0 || size != 0 {
		t.Errorf("copies left after the purge: %d, %d", n, size)
	}
}
