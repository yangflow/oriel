package store

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPendingExtractionsDueOnly(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	at := time.UnixMilli(1_000_000)
	src, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "", at)
	if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: src.Kind}, "", "", []FeedEntry{
		{GUID: "later", Title: "later"}, {GUID: "due", Title: "due"}, {GUID: "gaveup", Title: "gave up"}, {GUID: "fresh", Title: "fresh"},
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
	for guid, retry := range map[string]time.Time{"later": at.Add(time.Hour), "due": at.Add(time.Minute), "gaveup": {}} {
		if err := s.FailExtraction(ctx, id(guid), retry); err != nil {
			t.Fatal(err)
		}
	}
	guids := func(now time.Time) string {
		list, err := s.PendingExtractions(ctx, now, 10)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, target := range list {
			var guid string
			s.db.QueryRow("SELECT guid FROM entries WHERE id = ?", target.ID).Scan(&guid)
			names = append(names, guid+":"+strconv.Itoa(target.Attempts))
		}
		return strings.Join(names, ",")
	}
	if got := guids(at); got != "fresh:0" {
		t.Errorf("before any retry is due: %q", got)
	}
	if got := guids(at.Add(time.Minute)); got != "fresh:0,due:1" {
		t.Errorf("a minute later, the new entry first: %q", got)
	}
	if got := guids(at.Add(time.Hour)); got != "fresh:0,later:1,due:1" {
		t.Errorf("an hour later: %q", got)
	}
}
