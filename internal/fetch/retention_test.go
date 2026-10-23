package fetch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

func TestRetentionSetting(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for raw, want := range map[string]time.Duration{
		"": 0, "0": 0, "-3": 0, "soon": 0, "30": 30 * day, "99999": 3650 * day,
	} {
		f.store.SetSetting(ctx, RetentionSetting, raw)
		if got := f.poller.retention(ctx); got != want {
			t.Errorf("%q: %s, want %s", raw, got, want)
		}
	}
}

func TestPrune(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := f.add(store.NewSource{URL: "https://example.org/feed", Kind: "video"})
	src := store.FeedSource{ID: id, Kind: "video"}
	old, recent := f.now.Add(-40*day), f.now.Add(-5*day)
	save := func(added time.Time, entries ...store.FeedEntry) {
		if _, err := f.store.SaveFeed(ctx, src, "", "", entries, "", time.Time{}, added); err != nil {
			t.Fatal(err)
		}
	}
	save(f.now, store.FeedEntry{GUID: "old", PublishedAt: old}, store.FeedEntry{GUID: "old-starred", PublishedAt: old},
		store.FeedEntry{GUID: "old-read", PublishedAt: old}, store.FeedEntry{GUID: "recent", PublishedAt: recent})
	save(old, store.FeedEntry{GUID: "undated-old"})
	save(recent, store.FeedEntry{GUID: "undated-recent"})
	f.db.Exec("UPDATE entries SET starred = 1 WHERE guid = 'old-starred'")
	f.db.Exec("UPDATE entries SET is_read = 1 WHERE guid = 'old-read'")
	remaining := func() map[string]bool {
		rows, _ := f.db.Query("SELECT guid FROM entries")
		defer rows.Close()
		guids := map[string]bool{}
		for rows.Next() {
			var guid string
			rows.Scan(&guid)
			guids[guid] = true
		}
		return guids
	}

	f.poller.prune(ctx) // retention unset: keep everything
	if len(remaining()) != 6 {
		t.Fatalf("pruned without a retention period: %v", remaining())
	}
	f.now = f.now.Add(time.Hour) // the next hourly pass
	f.store.SetSetting(ctx, RetentionSetting, "30")
	f.poller.prune(ctx)
	got := remaining()
	if len(got) != 3 || !got["old-starred"] || !got["recent"] || !got["undated-recent"] {
		t.Fatalf("after pruning: %v", got)
	}

	// At most once an hour: a shorter period takes effect at the next hourly pass.
	f.store.SetSetting(ctx, RetentionSetting, "1")
	f.now = f.now.Add(30 * time.Minute)
	f.poller.prune(ctx)
	if len(remaining()) != 3 {
		t.Fatal("pruned again within an hour")
	}
	f.now = f.now.Add(30 * time.Minute)
	f.poller.prune(ctx)
	if got := remaining(); len(got) != 1 || !got["old-starred"] {
		t.Fatalf("an hour later: %v", got)
	}
}

func TestPurgeRemovesDeletedSourcesAfterTheUndoWindow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := f.add(store.NewSource{URL: "https://example.org/feed", Kind: "video"})
	if _, err := f.store.SaveFeed(ctx, store.FeedSource{ID: id, Kind: "video"}, "", "", []store.FeedEntry{{GUID: "a"}}, "", time.Time{}, f.now); err != nil {
		t.Fatal(err)
	}
	f.db.Exec("UPDATE entries SET starred = 1")
	if err := f.store.DeleteSource(ctx, id, f.now); err != nil {
		t.Fatal(err)
	}
	count := func(table string) (n int) {
		f.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n)
		return n
	}

	f.now = f.now.Add(store.DeleteUndoWindow - time.Second)
	f.poller.prune(ctx)
	if count("sources") != 1 {
		t.Fatal("purged a deleted source inside its undo window")
	}
	f.now = f.now.Add(time.Hour) // the next hourly pass, long after the window
	f.poller.prune(ctx)
	if count("sources") != 0 || count("entries") != 0 {
		t.Fatalf("%d sources and %d entries left after the window", count("sources"), count("entries"))
	}
}

func TestPurgeOfManyEntriesGivesTheDiskSpaceBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := f.add(store.NewSource{URL: "https://example.org/feed", Kind: "article"})
	var entries []store.FeedEntry
	for i := 0; i < store.MaintainAfter+50; i++ {
		entries = append(entries, store.FeedEntry{GUID: fmt.Sprint("g", i), Title: "t", ContentHTML: strings.Repeat("<p>text</p>", 400), PublishedAt: f.now})
	}
	if _, err := f.store.SaveFeed(ctx, store.FeedSource{ID: id, Kind: "article"}, "", "", entries, "", time.Time{}, f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteSource(ctx, id, f.now); err != nil {
		t.Fatal(err)
	}
	before, _ := f.store.Storage(ctx)
	f.now = f.now.Add(store.DeleteUndoWindow + time.Second)
	f.poller.prune(ctx)
	after, _ := f.store.Storage(ctx)
	if after.ReclaimableBytes != 0 || after.DatabaseBytes >= before.DatabaseBytes {
		t.Fatalf("after the purge: %+v (before %+v)", after, before)
	}
}

func TestDeletedSourcesAreNotFetched(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := f.add(store.NewSource{URL: "https://example.org/feed"})
	if err := f.store.DeleteSource(ctx, id, f.now); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Hour)
	f.poller.fetchDue(ctx)
	var attempts int
	f.db.QueryRow("SELECT count(*) FROM fetch_log").Scan(&attempts)
	if attempts != 0 {
		t.Fatalf("%d fetches of a deleted source", attempts)
	}
	if _, err := f.store.RestoreSource(ctx, id, f.now.Add(-time.Hour+time.Second)); err != nil {
		t.Fatal(err)
	}
	f.poller.fetchDue(ctx)
	f.db.QueryRow("SELECT count(*) FROM fetch_log").Scan(&attempts)
	if attempts != 1 {
		t.Fatalf("%d fetches after the restore, want 1", attempts)
	}
}

func TestExpiredItemsAreNotFetchedAgain(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.store.SetSetting(ctx, RetentionSetting, "30")
	published := func(age time.Duration) string { return f.now.Add(-age).UTC().Format(time.RFC1123Z) }
	feed := `<rss version="2.0"><channel><title>T</title>` +
		`<item><guid>fresh</guid><pubDate>` + published(2*day) + `</pubDate></item>` +
		`<item><guid>expired</guid><pubDate>` + published(60*day) + `</pubDate></item>` +
		`<item><guid>undated</guid></item></channel></rss>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(feed)) }))
	defer srv.Close()
	id := f.add(store.NewSource{URL: srv.URL, Kind: "social"})
	// An entry stored before the retention period was set stays until pruning removes it.
	f.store.SaveFeed(ctx, store.FeedSource{ID: id, Kind: "social"}, "", "", []store.FeedEntry{{GUID: "kept", PublishedAt: f.now.Add(-90 * day)}}, "", time.Time{}, f.now)

	if n := f.poller.fetchDue(ctx); n != 2 {
		t.Fatalf("created %d entries, want fresh and undated", n)
	}
	if f.int("SELECT count(*) FROM entries WHERE guid = 'expired'") != 0 {
		t.Fatal("an item older than the retention period was added")
	}
	if f.int("SELECT count(*) FROM entries WHERE guid = 'kept'") != 1 {
		t.Fatal("fetching removed an existing entry")
	}
}

func TestKeepFor(t *testing.T) {
	for _, c := range []struct {
		keep   int
		global time.Duration
		want   time.Duration
	}{
		{0, 30 * day, 30 * day}, {0, 0, 0}, {7, 30 * day, 7 * day}, {90, 30 * day, 90 * day},
		{90, 0, 90 * day}, {-1, 30 * day, 0},
	} {
		if got := keepFor(store.FeedSource{KeepDays: c.keep}, c.global); got != c.want {
			t.Errorf("keep %d, global %s: %s, want %s", c.keep, c.global, got, c.want)
		}
	}
}

func TestPruneUsesTheKeepPeriodOfTheSource(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	follow := f.add(store.NewSource{URL: "https://example.org/follow", Kind: "article"})
	short := f.add(store.NewSource{URL: "https://example.org/short", Kind: "article"})
	long := f.add(store.NewSource{URL: "https://example.org/long", Kind: "article"})
	for id, days := range map[int64]int{short: 7, long: 90} {
		if _, err := f.store.UpdateSource(ctx, id, store.SourcePatch{KeepDays: &days}, f.now); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []int64{follow, short, long} {
		src := store.FeedSource{ID: id, Kind: "article"}
		entries := []store.FeedEntry{{GUID: "d3", PublishedAt: f.now.Add(-3 * day)}, {GUID: "d20", PublishedAt: f.now.Add(-20 * day)},
			{GUID: "d60", PublishedAt: f.now.Add(-60 * day)}}
		if _, err := f.store.SaveFeed(ctx, src, "", "", entries, "", time.Time{}, f.now); err != nil {
			t.Fatal(err)
		}
	}
	f.db.Exec("UPDATE entries SET starred = 1 WHERE source_id = ? AND guid = 'd60'", short)

	// With the global setting unset, only the sources with a period of their own are pruned.
	f.poller.prune(ctx)
	if f.int("SELECT count(*) FROM entries WHERE source_id = ?", follow) != 3 {
		t.Fatal("a source following an unset setting lost entries")
	}
	if f.int("SELECT count(*) FROM entries WHERE source_id = ?", short) != 2 || f.int("SELECT count(*) FROM entries WHERE source_id = ? AND guid = 'd60'", short) != 1 {
		t.Fatal("the 7-day source kept the wrong entries")
	}
	if f.int("SELECT count(*) FROM entries WHERE source_id = ?", long) != 3 {
		t.Fatal("the 90-day source lost entries")
	}

	f.store.SetSetting(ctx, RetentionSetting, "30")
	f.now = f.now.Add(time.Hour)
	f.poller.prune(ctx)
	if f.int("SELECT count(*) FROM entries WHERE source_id = ?", follow) != 2 || f.int("SELECT count(*) FROM entries WHERE source_id = ?", long) != 3 {
		t.Fatal("the global period did not apply to the follower only")
	}
}

func TestExpiredItemsFollowTheKeepPeriodOfTheSource(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.store.SetSetting(ctx, RetentionSetting, "30")
	published := func(age time.Duration) string { return f.now.Add(-age).UTC().Format(time.RFC1123Z) }
	feed := `<rss version="2.0"><channel><title>T</title>` +
		`<item><guid>d3</guid><pubDate>` + published(3*day) + `</pubDate></item>` +
		`<item><guid>d20</guid><pubDate>` + published(20*day) + `</pubDate></item>` +
		`<item><guid>d60</guid><pubDate>` + published(60*day) + `</pubDate></item></channel></rss>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(feed)) }))
	defer srv.Close()
	id := f.add(store.NewSource{URL: srv.URL, Kind: "social"})
	days := 7
	if _, err := f.store.UpdateSource(ctx, id, store.SourcePatch{KeepDays: &days}, f.now); err != nil {
		t.Fatal(err)
	}
	if n := f.poller.fetchDue(ctx); n != 1 {
		t.Fatalf("created %d entries under a 7-day period, want 1", n)
	}
	if f.int("SELECT count(*) FROM entries WHERE guid = 'd3'") != 1 {
		t.Fatal("the recent item was skipped")
	}

	// A longer period than the global one lets the older items in.
	days = 90
	if _, err := f.store.UpdateSource(ctx, id, store.SourcePatch{KeepDays: &days}, f.now); err != nil {
		t.Fatal(err)
	}
	f.due()
	if n := f.poller.fetchDue(ctx); n != 2 {
		t.Fatalf("created %d entries under a 90-day period, want 2", n)
	}
}
