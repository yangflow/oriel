package store

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReadingSeconds(t *testing.T) {
	for _, c := range []struct {
		name                     string
		kind                     string
		duration                 int
		search, content, summary string
		want                     int // 0 means unknown, as no estimate here is a whole 0
		known                    bool
	}{
		{name: "chinese", kind: "article", search: strings.Repeat("字", 4000), want: 600, known: true},
		{name: "english", kind: "article", search: strings.Repeat("word ", 2200), want: 600, known: true},
		{name: "mixed adds", kind: "article", search: strings.Repeat("字", 2000) + strings.Repeat(" word", 1100), want: 600, known: true},
		{name: "kana count as characters", kind: "article", search: strings.Repeat("あア", 100), want: 30, known: true},
		{name: "hangul counts as words", kind: "article", search: strings.Repeat("안녕 ", 110), want: 30, known: true},
		{name: "punctuation and numbers", kind: "article", search: "Hello, world! 3.14 — ok", want: 1, known: true},
		{name: "short stays in seconds", kind: "article", search: strings.Repeat("字", 100), want: 15, known: true},
		{name: "59 seconds", kind: "article", search: strings.Repeat("字", 393), want: 59, known: true},
		{name: "just short of a minute rounds to one", kind: "article", search: strings.Repeat("字", 397), want: 60, known: true},
		{name: "89 seconds is a minute", kind: "article", search: strings.Repeat("字", 593), want: 60, known: true},
		{name: "90 seconds is two minutes", kind: "article", search: strings.Repeat("字", 600), want: 120, known: true},
		{name: "duration wins and is not rounded", kind: "article", duration: 1234, search: "words", want: 1234, known: true},
		{name: "video keeps its duration", kind: "video", duration: 95, content: "<p>text</p>", want: 95, known: true},
		{name: "video without duration is unknown", kind: "video", content: "<p>a long description</p>"},
		{name: "image without duration is unknown", kind: "image", summary: "caption"},
		{name: "extracted text beats the feed", kind: "article", search: strings.Repeat("字", 800), content: "<p>short</p>", want: 120, known: true},
		{name: "feed content beats the summary", kind: "social", content: "<p>" + strings.Repeat("字", 800) + "</p>", summary: "short", want: 120, known: true},
		{name: "summary alone", kind: "article", summary: strings.Repeat("字", 800), want: 120, known: true},
		{name: "html is stripped", kind: "article", content: "<script>var a = 1 2 3 4 5 6 7 8 9;</script><style>p{color:red}</style><p>one</p><img src=x alt='many words here'><p>two</p>", want: 1, known: true},
		{name: "empty html falls back to the summary", kind: "article", content: "<p> </p><img src=x>", summary: strings.Repeat("字", 800), want: 120, known: true},
		{name: "no text is unknown", kind: "article", content: "<p>  </p>", summary: " "},
		{name: "nothing is unknown", kind: "social"},
	} {
		got, ok := readingSeconds(c.kind, c.duration, c.search, c.content, c.summary)
		if ok != c.known || got != c.want {
			t.Errorf("%s: got %d, %v, want %d, %v", c.name, got, ok, c.want, c.known)
		}
	}
}

func readingOf(t *testing.T, s *Store, guid string) *int {
	t.Helper()
	var id int64
	if err := s.db.QueryRow("SELECT id FROM entries WHERE guid = ?", guid).Scan(&id); err != nil {
		t.Fatal(err)
	}
	entry, err := s.Entry(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return entry.ReadingSec
}

func TestReadingTimeIsKept(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	at := time.UnixMilli(1_000_000)
	blog, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "", at)
	feed := func(items ...FeedEntry) {
		t.Helper()
		if _, err := s.SaveFeed(ctx, FeedSource{ID: blog.ID, Kind: "article"}, "", "", items, "", time.Time{}, at); err != nil {
			t.Fatal(err)
		}
	}
	want := func(guid string, sec int) {
		t.Helper()
		if got := readingOf(t, s, guid); got == nil || *got != sec {
			t.Fatalf("%s: reading = %v, want %d", guid, got, sec)
		}
	}
	feed(FeedEntry{GUID: "a", Title: "A", ContentHTML: "<p>" + strings.Repeat("字", 800) + "</p>"}, FeedEntry{GUID: "b", Title: "B"})
	want("a", 120)
	if got := readingOf(t, s, "b"); got != nil {
		t.Fatalf("empty entry: %d", *got)
	}
	// Changed content is estimated again.
	feed(FeedEntry{GUID: "a", Title: "A", ContentHTML: "<p>" + strings.Repeat("字", 1200) + "</p>"})
	want("a", 180)

	var id int64
	s.db.QueryRow("SELECT id FROM entries WHERE guid = 'a'").Scan(&id)
	// A failed extraction leaves the estimate; a good one replaces the feed's content with the full text.
	if err := s.SaveExtraction(ctx, id, "", "", ""); err != nil {
		t.Fatal(err)
	}
	want("a", 180)
	if err := s.SaveExtraction(ctx, id, "<p>x</p>", strings.Repeat("字", 4000), ""); err != nil {
		t.Fatal(err)
	}
	want("a", 600)
	// A refetch with the same feed content keeps the estimate of the full text.
	feed(FeedEntry{GUID: "a", Title: "A", ContentHTML: "<p>" + strings.Repeat("字", 1200) + "</p>", Summary: "changed"})
	want("a", 600)
}

func TestMaxReadingSecFilter(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	at := time.UnixMilli(1_000_000)
	blog, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "", at)
	pics, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/pics", Kind: "image", FetchInterval: time.Hour}, "", at)
	tube, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/tube", Kind: "video", FetchInterval: time.Hour}, "", at)
	save := func(src Source, items ...FeedEntry) {
		t.Helper()
		if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: src.Kind}, "", "", items, "", time.Time{}, at); err != nil {
			t.Fatal(err)
		}
	}
	save(blog,
		FeedEntry{GUID: "5", Title: "five", ContentHTML: strings.Repeat("字", 2000)},
		FeedEntry{GUID: "11", Title: "eleven", ContentHTML: strings.Repeat("字", 4400)},
		FeedEntry{GUID: "short", Title: "short", ContentHTML: "字字字"},
		FeedEntry{GUID: "none", Title: "none"})
	save(pics, FeedEntry{GUID: "pic", Title: "pic", Summary: "caption"})
	save(tube, FeedEntry{GUID: "v10", Title: "v10", DurationSec: 600}, FeedEntry{GUID: "v10s", Title: "v10s", DurationSec: 601}, FeedEntry{GUID: "v?", Title: "v?"})

	titles := func(f EntryFilter) string {
		t.Helper()
		got, err := s.Entries(ctx, f, 50)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range got {
			names = append(names, e.Title)
		}
		return strings.Join(names, ",")
	}
	for _, c := range []struct {
		filter EntryFilter
		want   string
	}{
		{EntryFilter{MaxReadingSec: 300}, "short,five"},
		{EntryFilter{MaxReadingSec: 600}, "v10,short,five"},
		{EntryFilter{MaxReadingSec: 600, Kind: "video"}, "v10"},
		{EntryFilter{MaxReadingSec: 3600, SourceID: blog.ID}, "eleven,short,five"},
		{EntryFilter{MaxReadingSec: 3600, Unread: true, Starred: true}, ""},
	} {
		got := titles(c.filter)
		// Entries saved together share a created_at, so the order inside a group is by ID, newest first.
		if sorted(got) != sorted(c.want) {
			t.Errorf("%+v: got %q, want %q", c.filter, got, c.want)
		}
	}
	all, _ := s.Entries(ctx, EntryFilter{}, 50)
	if len(all) != 8 {
		t.Fatalf("no budget lists %d, want 8", len(all))
	}
}

func sorted(csv string) string {
	parts := strings.Split(csv, ",")
	for i := range parts {
		for j := i + 1; j < len(parts); j++ {
			if parts[j] < parts[i] {
				parts[i], parts[j] = parts[j], parts[i]
			}
		}
	}
	return strings.Join(parts, ",")
}

func TestBackfillReadingTime(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	at := time.UnixMilli(1_000_000)
	blog, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "", at)
	tube, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/tube", Kind: "video", FetchInterval: time.Hour}, "", at)
	var ids []int64
	insert := func(src Source, i int, content string, duration any) {
		t.Helper()
		result, err := s.db.ExecContext(ctx,
			"INSERT INTO entries (source_id, guid, title, rss_content_html, duration_sec, created_at, updated_at) VALUES (?, ?, 't', ?, ?, 1, 1)",
			src.ID, strconv.Itoa(i), content, duration)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		ids = append(ids, id)
	}
	for i := range 450 {
		insert(blog, i, strings.Repeat("字", 800), nil)
	}
	insert(tube, 450, "", 300)
	insert(blog, 451, "<p></p>", nil)

	// Rows written before the column existed have no estimate yet.
	if got := readingOf(t, s, "0"); got != nil {
		t.Fatalf("before: %d", *got)
	}
	n, err := s.backfillReadingBatch(ctx, 200)
	if err != nil || n != 200 {
		t.Fatalf("first batch: %d, %v", n, err)
	}
	if got := readingOf(t, s, "199"); got == nil || *got != 120 {
		t.Fatalf("done entry: %v", got)
	}
	if got := readingOf(t, s, "200"); got != nil {
		t.Fatalf("entry beyond the batch: %d", *got)
	}
	if v, _ := s.Setting(ctx, readingBackfillKey); v != strconv.FormatInt(ids[199], 10) {
		t.Fatalf("progress = %q, want %d", v, ids[199])
	}
	// The next run carries on from the stored progress.
	if err := s.BackfillReadingTime(ctx); err != nil {
		t.Fatal(err)
	}
	if got := readingOf(t, s, "449"); got == nil || *got != 120 {
		t.Fatalf("last text entry: %v", got)
	}
	if got := readingOf(t, s, "450"); got == nil || *got != 300 {
		t.Fatalf("video: %v", got)
	}
	if got := readingOf(t, s, "451"); got != nil {
		t.Fatalf("empty entry: %d", *got)
	}
	if v, _ := s.Setting(ctx, readingBackfillKey); v != strconv.FormatInt(ids[len(ids)-1], 10) {
		t.Fatalf("final progress = %q", v)
	}
	if n, err := s.backfillReadingBatch(ctx, 200); err != nil || n != 0 {
		t.Fatalf("after the end: %d, %v", n, err)
	}
}

func TestRetryExtraction(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	at := time.UnixMilli(1_000_000)
	src, _ := s.CreateSource(ctx, NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "", at)
	if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, Kind: src.Kind}, "", "", []FeedEntry{
		{GUID: "failed", Title: "failed"}, {GUID: "done", Title: "done"}, {GUID: "waiting", Title: "waiting"},
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
	if err := s.SaveExtraction(ctx, id("failed"), "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveExtraction(ctx, id("done"), "<p>x</p>", "x", ""); err != nil {
		t.Fatal(err)
	}
	targets := func() int {
		list, err := s.PendingExtractions(ctx, at, 10)
		if err != nil {
			t.Fatal(err)
		}
		return len(list)
	}
	if targets() != 1 {
		t.Fatalf("only the waiting entry is a target, got %d", targets())
	}

	entry, err := s.RetryExtraction(ctx, id("failed"))
	if err != nil || entry.ExtractStatus != "pending" {
		t.Fatalf("RetryExtraction = %+v, %v", entry, err)
	}
	var attempts int
	s.db.QueryRow("SELECT extract_attempts FROM entries WHERE guid = 'failed'").Scan(&attempts)
	if attempts != 0 || targets() != 2 {
		t.Fatalf("attempts %d, targets %d: the retry has all its attempts again", attempts, targets())
	}
	// Asking again while it waits changes nothing, and is not an error.
	if entry, err := s.RetryExtraction(ctx, id("waiting")); err != nil || entry.ExtractStatus != "pending" {
		t.Fatalf("pending: %+v, %v", entry, err)
	}
	// A finished extraction is not thrown away.
	if _, err := s.RetryExtraction(ctx, id("done")); !errors.Is(err, ErrNotRetryable) {
		t.Fatalf("ok: %v", err)
	}
	var status string
	s.db.QueryRow("SELECT extract_status FROM entries WHERE guid = 'done'").Scan(&status)
	if status != "ok" {
		t.Fatalf("status %q", status)
	}
	if _, err := s.RetryExtraction(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}
