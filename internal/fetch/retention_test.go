package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

const day = 24 * time.Hour

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
		if _, err := f.store.SaveFeed(ctx, src, "", "", entries, time.Time{}, added); err != nil {
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
	f.store.SaveFeed(ctx, store.FeedSource{ID: id, Kind: "social"}, "", "", []store.FeedEntry{{GUID: "kept", PublishedAt: f.now.Add(-90 * day)}}, time.Time{}, f.now)

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
