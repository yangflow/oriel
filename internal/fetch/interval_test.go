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

func TestAutoInterval(t *testing.T) {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	every := func(n int, gap time.Duration) []time.Time {
		times := make([]time.Time, n)
		for i := range times {
			times[i] = base.Add(-time.Duration(i) * gap)
		}
		return times
	}
	// Twenty-one entries: the gaps between the newest twenty are 1 h, and the older one is left out.
	recent := append(every(20, time.Hour), base.Add(-400*24*time.Hour))
	skewed := []time.Time{base, base.Add(-time.Hour), base.Add(-2 * time.Hour), base.Add(-3 * time.Hour),
		base.Add(-4 * time.Hour), base.Add(-100 * time.Hour)}
	for _, c := range []struct {
		name  string
		times []time.Time
		want  time.Duration
	}{
		{"none", nil, DefaultInterval},
		{"too few", every(AutoMinEntries-1, 6*time.Hour), DefaultInterval},
		{"the fewest", every(AutoMinEntries, 6*time.Hour), 6 * time.Hour},
		{"even count of gaps", every(7, 3*time.Hour), 3 * time.Hour},
		{"only the newest twenty count", recent, time.Hour},
		{"median ignores one long gap", skewed, time.Hour},
		{"short gaps take the least", every(10, time.Minute), DefaultInterval},
		{"long gaps take the most", every(6, 10*24*time.Hour), MaxAutoInterval},
		{"same moment", every(8, 0), DefaultInterval},
	} {
		if got := AutoInterval(c.times); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	shuffled := every(6, 2*time.Hour)
	shuffled[0], shuffled[5] = shuffled[5], shuffled[0]
	if got := AutoInterval(shuffled); got != 2*time.Hour {
		t.Errorf("order of the times matters: %v", got)
	}
}

func TestSuggestKind(t *testing.T) {
	video := store.FeedEntry{DurationSec: 600}
	pictures := store.FeedEntry{Images: make([]store.Image, 3)}
	plain := store.FeedEntry{}
	for _, c := range []struct {
		name    string
		entries []store.FeedEntry
		current string
		want    string
	}{
		{"too few entries", []store.FeedEntry{video, video}, "article", ""},
		{"mostly videos", []store.FeedEntry{video, video, plain}, "article", "video"},
		{"already video", []store.FeedEntry{video, video, video}, "video", ""},
		{"half is not most", []store.FeedEntry{video, plain, video, plain}, "article", ""},
		{"mostly pictures", []store.FeedEntry{pictures, pictures, plain}, "article", "image"},
		{"already image", []store.FeedEntry{pictures, pictures, pictures}, "image", ""},
		{"plain articles", []store.FeedEntry{plain, plain, plain}, "article", ""},
		{"videos win over pictures", []store.FeedEntry{video, video, video, pictures}, "social", "video"},
	} {
		if got := SuggestKind(c.entries, c.current); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// datedFeed is an RSS feed of n items published gap apart, the newest an hour before now.
func datedFeed(now time.Time, n int, gap time.Duration, extra string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel><title>Dated</title>`)
	for i := range n {
		at := now.Add(-time.Hour - time.Duration(i)*gap)
		fmt.Fprintf(&b, `<item><title>Post %d</title><link>https://example.com/%d</link><guid>g%d</guid><pubDate>%s</pubDate>%s</item>`,
			i, i, i, at.UTC().Format(time.RFC1123Z), extra)
	}
	b.WriteString(`</channel></rss>`)
	return b.String()
}

func serve(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	t.Cleanup(srv.Close)
	return srv.URL
}

func (f *fixture) schedule(id int64) (interval, next int64) {
	f.t.Helper()
	return f.int("SELECT fetch_interval_sec FROM sources WHERE id = ?", id),
		f.int("SELECT next_fetch_at FROM sources WHERE id = ?", id)
}

func TestPollAutoInterval(t *testing.T) {
	for _, c := range []struct {
		name string
		n    int
		gap  time.Duration
		want time.Duration
	}{
		{"follows the feed", 8, 3 * time.Hour, 3 * time.Hour},
		{"too little history", 4, 3 * time.Hour, DefaultInterval},
		{"clamped up", 8, time.Minute, DefaultInterval},
		{"clamped down", 8, 10 * 24 * time.Hour, MaxAutoInterval},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			id := f.add(store.NewSource{URL: serve(t, datedFeed(f.now, c.n, c.gap, "")), FetchIntervalAuto: true})
			f.poller.fetchDue(context.Background())
			interval, next := f.schedule(id)
			if interval != int64(c.want/time.Second) || next != f.now.Add(c.want).UnixMilli() {
				t.Fatalf("interval %d s, next in %v", interval, time.Duration(next-f.now.UnixMilli())*time.Millisecond)
			}
		})
	}
}

func TestPollFixedIntervalStays(t *testing.T) {
	f := newFixture(t)
	id := f.add(store.NewSource{URL: serve(t, datedFeed(f.now, 8, 3*time.Hour, "")), FetchInterval: 20 * time.Minute})
	f.poller.fetchDue(context.Background())
	interval, next := f.schedule(id)
	if interval != 20*60 || next != f.now.Add(20*time.Minute).UnixMilli() {
		t.Fatalf("interval %d s, next %d", interval, next)
	}
}

func TestPollSwitchingBetweenFixedAndAuto(t *testing.T) {
	f := newFixture(t)
	id := f.add(store.NewSource{URL: serve(t, datedFeed(f.now, 8, 3*time.Hour, "")), FetchIntervalAuto: true})
	f.poller.fetchDue(context.Background())
	if interval, _ := f.schedule(id); interval != 3*3600 {
		t.Fatalf("interval %d s", interval)
	}
	fixed, auto, on := time.Hour, false, true
	if _, err := f.store.UpdateSource(context.Background(), id,
		store.SourcePatch{FetchInterval: &fixed, FetchIntervalAuto: &auto}, f.now); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(4 * time.Hour)
	f.poller.fetchDue(context.Background())
	if interval, _ := f.schedule(id); interval != 3600 {
		t.Fatalf("interval %d s after switching to a fixed hour", interval)
	}
	// Back to Auto: the interval in use stays until the next fetch works it out again.
	if _, err := f.store.UpdateSource(context.Background(), id, store.SourcePatch{FetchIntervalAuto: &on}, f.now); err != nil {
		t.Fatal(err)
	}
	if interval, _ := f.schedule(id); interval != 3600 {
		t.Fatalf("interval %d s right after switching back", interval)
	}
	f.now = f.now.Add(2 * time.Hour)
	f.poller.fetchDue(context.Background())
	if interval, _ := f.schedule(id); interval != 3*3600 {
		t.Fatalf("Auto did not work the interval out again: %d s", interval)
	}
}

func TestPollSuggestsKindOnceAndNeverApplies(t *testing.T) {
	f := newFixture(t)
	body := datedFeed(f.now, 4, time.Hour, `<itunes:duration>10:00</itunes:duration>`)
	id := f.add(store.NewSource{URL: serve(t, body), Kind: "article"})
	f.poller.fetchDue(context.Background())
	if kind := f.text("SELECT kind FROM sources WHERE id = ?", id); kind != "article" {
		t.Fatalf("the suggestion changed the kind to %q", kind)
	}
	if suggested := f.text("SELECT suggested_kind FROM sources WHERE id = ?", id); suggested != "video" {
		t.Fatalf("suggested %q", suggested)
	}
	// Only the first successful fetch suggests; a later one leaves what is stored.
	f.db.Exec("UPDATE sources SET suggested_kind = '' WHERE id = ?", id)
	f.now = f.now.Add(time.Hour)
	f.poller.fetchDue(context.Background())
	if f.text("SELECT suggested_kind FROM sources WHERE id = ?", id) != "" {
		t.Fatal("a later fetch suggested again")
	}
}

func TestPollSuggestsNothingForPlainArticles(t *testing.T) {
	f := newFixture(t)
	id := f.add(store.NewSource{URL: serve(t, datedFeed(f.now, 4, time.Hour, "")), Kind: "article"})
	f.poller.fetchDue(context.Background())
	if f.text("SELECT suggested_kind FROM sources WHERE id = ?", id) != "" {
		t.Fatal("suggested a kind for plain articles")
	}
}
