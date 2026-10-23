package store

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestMigrationMakesDefaultIntervalAuto(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "oriel.sqlite")
	s, err := Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	// Take the database back to what version 9 left: no fetch log, the columns of the later migrations gone, three sources with their intervals.
	for _, q := range []string{
		"DROP TRIGGER entries_star_insert",
		"DROP TRIGGER entries_star_on",
		"DROP TRIGGER entries_star_off",
		"DROP TABLE entry_image_copies",
		"DROP INDEX entries_copy_due",
		"DROP INDEX entries_link_key",
		"ALTER TABLE entries DROP COLUMN link_key",
		"ALTER TABLE entries DROP COLUMN copy_status",
		"ALTER TABLE entries DROP COLUMN copy_attempts",
		"ALTER TABLE entries DROP COLUMN next_copy_at",
		"DROP TABLE fetch_log",
		"ALTER TABLE sources DROP COLUMN websub_hub",
		"ALTER TABLE sources DROP COLUMN websub_topic",
		"ALTER TABLE sources DROP COLUMN websub_key",
		"ALTER TABLE sources DROP COLUMN websub_secret",
		"ALTER TABLE sources DROP COLUMN websub_state",
		"ALTER TABLE sources DROP COLUMN websub_expires_at",
		"ALTER TABLE sources DROP COLUMN websub_requested_at",
		"ALTER TABLE sources DROP COLUMN websub_base",
		"ALTER TABLE sources DROP COLUMN websub_next_at",
		"ALTER TABLE sources DROP COLUMN websub_failures",
		"ALTER TABLE sources DROP COLUMN websub_error",
		"ALTER TABLE sources DROP COLUMN rsshub_instance",
		"ALTER TABLE sources DROP COLUMN fetch_interval_auto",
		"ALTER TABLE sources DROP COLUMN suggested_kind",
		"ALTER TABLE sources DROP COLUMN extract_selector",
		"ALTER TABLE sources DROP COLUMN extract_referer",
		"ALTER TABLE sources DROP COLUMN extract_rewrites",
		"ALTER TABLE sources DROP COLUMN pace_sec",
		"ALTER TABLE sources DROP COLUMN keep_days",
		"ALTER TABLE sources DROP COLUMN last_error_kind",
		"DROP INDEX sources_deleted",
		"ALTER TABLE sources DROP COLUMN deleted_at",
		"INSERT INTO sources (url, kind, fetch_interval_sec, next_fetch_at, created_at, updated_at) VALUES ('https://a.org/a', 'article', 900, 0, 0, 0)",
		"INSERT INTO sources (url, kind, fetch_interval_sec, next_fetch_at, created_at, updated_at) VALUES ('https://a.org/b', 'article', 3600, 0, 0, 0)",
		"INSERT INTO sources (url, kind, fetch_interval_sec, next_fetch_at, created_at, updated_at) VALUES ('https://a.org/c', 'article', 901, 0, 0, 0)",
		"DROP TABLE entry_ai_chunks",
		"DROP TABLE ai_usage",
		"DROP TABLE rules",
		"DROP INDEX entries_extract_open",
		"DROP INDEX entry_ai_open",
		"CREATE INDEX entry_ai_status ON entry_ai (status)",
		"PRAGMA user_version = 9",
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = open(t, file)
	list, err := s.Sources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, src := range list {
		got[src.URL] = src.FetchIntervalAuto
		if src.SuggestedKind != "" {
			t.Errorf("%s starts with the suggestion %q", src.URL, src.SuggestedKind)
		}
	}
	if len(got) != 3 || !got["https://a.org/a"] || got["https://a.org/b"] || got["https://a.org/c"] {
		t.Fatalf("automatic sources %v: only the one at the old default of 900 s should be", got)
	}
}

func TestRecentPublished(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.UnixMilli(1_790_000_000_000)
	src, err := s.CreateSource(ctx, NewSource{URL: "https://a.org/f", Kind: "article", FetchInterval: time.Hour,
		FetchIntervalAuto: true}, "", now)
	if err != nil {
		t.Fatal(err)
	}
	var entries []FeedEntry
	for i, ago := range []time.Duration{3 * time.Hour, time.Hour, 2 * time.Hour, -time.Hour} {
		entries = append(entries, FeedEntry{GUID: strconv.Itoa(i), Title: "t", URL: "https://a.org/" + strconv.Itoa(i),
			PublishedAt: now.Add(-ago)})
	}
	entries = append(entries, FeedEntry{GUID: "undated", Title: "t", URL: "https://a.org/u"})
	if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: src.Kind}, "", "", entries, "", time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	got, err := s.RecentPublished(ctx, src.ID, now, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].Equal(now.Add(-time.Hour)) || !got[1].Equal(now.Add(-2*time.Hour)) {
		t.Fatalf("recent %v: want the two newest that are not in the future, newest first", got)
	}
}

func TestSuggestKindIsStoredNotApplied(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	src, err := s.CreateSource(ctx, NewSource{URL: "https://a.org/f", Kind: "article", FetchInterval: time.Hour}, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SuggestKind(ctx, src.ID, "video"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Source(ctx, src.ID)
	if err != nil || got.Kind != "article" || got.SuggestedKind != "video" {
		t.Fatalf("kind %q, suggested %q, %v", got.Kind, got.SuggestedKind, err)
	}
}
