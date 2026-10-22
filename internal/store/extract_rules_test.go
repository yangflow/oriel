package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractRulesRoundTripAndReachPendingExtractions(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	at := time.UnixMilli(1_000_000)
	src, err := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "", at)
	if err != nil {
		t.Fatal(err)
	}
	if src.ExtractSelector != "" || src.ExtractReferer != "" || src.ExtractRewrites != "" {
		t.Fatalf("a new source has rules: %+v", src)
	}
	if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: src.Kind}, "", "", []FeedEntry{{GUID: "a", Title: "a"}}, "", time.Time{}, at); err != nil {
		t.Fatal(err)
	}

	selector, referer, rewrites := "article .post", "https://example.org/", "www.example.org => example.org"
	got, err := s.UpdateSource(ctx, src.ID, SourcePatch{ExtractSelector: &selector, ExtractReferer: &referer, ExtractRewrites: &rewrites}, at)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExtractSelector != selector || got.ExtractReferer != referer || got.ExtractRewrites != rewrites {
		t.Fatalf("UpdateSource = %+v", got)
	}
	list, err := s.PendingExtractions(ctx, at, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("PendingExtractions = %+v, %v", list, err)
	}
	if list[0].Selector != selector || list[0].Referer != referer || list[0].Rewrites != rewrites {
		t.Errorf("target rules = %+v", list[0])
	}

	empty := ""
	got, err = s.UpdateSource(ctx, src.ID, SourcePatch{ExtractSelector: &empty}, at)
	if err != nil || got.ExtractSelector != "" || got.ExtractReferer != referer || got.ExtractRewrites != rewrites {
		t.Errorf("clearing only the selector: %+v, %v", got, err)
	}
}
