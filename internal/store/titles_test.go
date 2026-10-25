package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// titleStore returns a store with one source whose title translation is on, and the feed source to fetch it with.
func titleStore(t *testing.T) (*Store, Source, FeedSource) {
	t.Helper()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	src, err := s.CreateSource(context.Background(), NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour, TranslateTitles: true}, "Example", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !src.TranslateTitles {
		t.Fatal("the source was created with the switch off")
	}
	return s, src, FeedSource{ID: src.ID, URL: src.URL, Kind: src.Kind, TranslateTitles: true}
}

func titleStatus(t *testing.T, s *Store, id int64) (status, translated string, attempts int) {
	t.Helper()
	err := s.db.QueryRow("SELECT title_tr_status, translated_title, title_tr_attempts FROM entries WHERE id = ?", id).Scan(&status, &translated, &attempts)
	if err != nil {
		t.Fatal(err)
	}
	return status, translated, attempts
}

func entryID(t *testing.T, s *Store, guid string) int64 {
	t.Helper()
	var id int64
	if err := s.db.QueryRow("SELECT id FROM entries WHERE guid = ?", guid).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSaveFeedQueuesTitlesUnlessTheyNeedNoTranslation(t *testing.T) {
	ctx := context.Background()
	s, _, feed := titleStore(t)
	now := time.Now()
	entries := []FeedEntry{
		{GUID: "foreign", Title: "Ein deutscher Titel", Language: "de"},
		{GUID: "unknown", Title: "Title of unknown language"},
		{GUID: "target", Title: "已经是中文", Language: "zh-Hans"},
		{GUID: "empty", Title: "", Language: "de"},
	}
	if _, err := s.SaveFeed(ctx, feed, "", "", entries, "zh-Hans", time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"foreign": "pending", "unknown": "pending", "target": "", "empty": ""}
	for guid, status := range want {
		if got, _, _ := titleStatus(t, s, entryID(t, s, guid)); got != status {
			t.Errorf("%s: status %q, want %q", guid, got, status)
		}
	}
	jobs, err := s.PendingTitles(ctx, 10, 3, now)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("PendingTitles = %+v, %v", jobs, err)
	}
}

func TestSaveFeedQueuesNothingWithTheSwitchOff(t *testing.T) {
	ctx := context.Background()
	s, _, feed := titleStore(t)
	feed.TranslateTitles = false
	if _, err := s.SaveFeed(ctx, feed, "", "", []FeedEntry{{GUID: "a", Title: "Titre", Language: "fr"}}, "zh-Hans", time.Time{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := titleStatus(t, s, entryID(t, s, "a")); status != "" {
		t.Fatalf("status = %q", status)
	}
}

func TestTitlesAreTranslatedOnceAndRequeuedWhenTheTitleChanges(t *testing.T) {
	ctx := context.Background()
	s, _, feed := titleStore(t)
	now := time.Now()
	item := FeedEntry{GUID: "a", Title: "Bonjour", Language: "fr"}
	if _, err := s.SaveFeed(ctx, feed, "", "", []FeedEntry{item}, "zh-Hans", time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	id := entryID(t, s, "a")
	jobs, err := s.PendingTitles(ctx, 10, 3, now)
	if err != nil || len(jobs) != 1 || jobs[0].ID != id || jobs[0].Title != "Bonjour" || jobs[0].Language != "fr" {
		t.Fatalf("PendingTitles = %+v, %v", jobs, err)
	}
	if err := s.SaveTitles(ctx, []TitleResult{{ID: id, Title: "Bonjour", Text: " 你好 "}}, now); err != nil {
		t.Fatal(err)
	}
	if status, text, attempts := titleStatus(t, s, id); status != "ok" || text != "你好" || attempts != 1 {
		t.Fatalf("after saving: %q %q %d", status, text, attempts)
	}
	// The same item again changes nothing and asks for nothing.
	if _, err := s.SaveFeed(ctx, feed, "", "", []FeedEntry{item}, "zh-Hans", time.Time{}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if jobs, err := s.PendingTitles(ctx, 10, 3, now); err != nil || len(jobs) != 0 {
		t.Fatalf("PendingTitles after a second fetch = %+v, %v", jobs, err)
	}
	if status, text, _ := titleStatus(t, s, id); status != "ok" || text != "你好" {
		t.Fatalf("after a second fetch: %q %q", status, text)
	}
	// A new title drops the old translation and queues the entry again.
	item.Title = "Bonsoir"
	if _, err := s.SaveFeed(ctx, feed, "", "", []FeedEntry{item}, "zh-Hans", time.Time{}, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if status, text, attempts := titleStatus(t, s, id); status != "pending" || text != "" || attempts != 0 {
		t.Fatalf("after the title changed: %q %q %d", status, text, attempts)
	}
	// A result for the old title is dropped.
	if err := s.SaveTitles(ctx, []TitleResult{{ID: id, Title: "Bonjour", Text: "你好"}}, now); err != nil {
		t.Fatal(err)
	}
	if status, text, _ := titleStatus(t, s, id); status != "pending" || text != "" {
		t.Fatalf("after a stale result: %q %q", status, text)
	}
}

func TestATranslationEqualToTheTitleIsStoredAsNone(t *testing.T) {
	ctx := context.Background()
	s, _, feed := titleStore(t)
	if _, err := s.SaveFeed(ctx, feed, "", "", []FeedEntry{{GUID: "a", Title: "iPhone 17"}}, "zh-Hans", time.Time{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	id := entryID(t, s, "a")
	if err := s.SaveTitles(ctx, []TitleResult{{ID: id, Title: "iPhone 17", Text: " iPhone 17"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if status, text, _ := titleStatus(t, s, id); status != "ok" || text != "" {
		t.Fatalf("%q %q", status, text)
	}
}

func TestFailedTitlesAreRetriedAfterTheDelayAndGiveUp(t *testing.T) {
	ctx := context.Background()
	s, _, feed := titleStore(t)
	now := time.Now()
	if _, err := s.SaveFeed(ctx, feed, "", "", []FeedEntry{{GUID: "a", Title: "Hola", Language: "es"}}, "zh-Hans", time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	id := entryID(t, s, "a")
	for try := 1; try <= 3; try++ {
		if err := s.FailTitle(ctx, id, now); err != nil {
			t.Fatal(err)
		}
		if status, _, attempts := titleStatus(t, s, id); status != "failed" || attempts != try {
			t.Fatalf("try %d: %q %d", try, status, attempts)
		}
		// Not before the delay has passed ...
		if jobs, err := s.PendingTitles(ctx, 10, 3, now.Add(-time.Minute)); err != nil || len(jobs) != 0 {
			t.Fatalf("try %d: listed too early: %+v, %v", try, jobs, err)
		}
		// ... and not once the attempts are used up.
		jobs, err := s.PendingTitles(ctx, 10, 3, now)
		if want := try < 3; (len(jobs) == 1) != want || err != nil {
			t.Fatalf("try %d: PendingTitles = %+v, %v", try, jobs, err)
		}
	}
}

func TestStopTitlesGivesUpThePendingOnes(t *testing.T) {
	ctx := context.Background()
	s, _, feed := titleStore(t)
	now := time.Now()
	if _, err := s.SaveFeed(ctx, feed, "", "", []FeedEntry{{GUID: "a", Title: "Hola", Language: "es"}}, "zh-Hans", time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	if n, err := s.StopTitles(ctx, 3, now); err != nil || n != 1 {
		t.Fatalf("StopTitles = %d, %v", n, err)
	}
	if jobs, err := s.PendingTitles(ctx, 10, 3, now.Add(time.Hour)); err != nil || len(jobs) != 0 {
		t.Fatalf("PendingTitles = %+v, %v", jobs, err)
	}
	if status, _, attempts := titleStatus(t, s, entryID(t, s, "a")); status != "failed" || attempts != 3 {
		t.Fatalf("%q %d", status, attempts)
	}
}

func TestSkipTitlesTakesThemOutOfTheQueue(t *testing.T) {
	ctx := context.Background()
	s, _, feed := titleStore(t)
	now := time.Now()
	if _, err := s.SaveFeed(ctx, feed, "", "", []FeedEntry{{GUID: "a", Title: "Hola"}}, "zh-Hans", time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	id := entryID(t, s, "a")
	if err := s.SkipTitles(ctx, []int64{id}); err != nil {
		t.Fatal(err)
	}
	if status, text, _ := titleStatus(t, s, id); status != "" || text != "" {
		t.Fatalf("%q %q", status, text)
	}
	if jobs, err := s.PendingTitles(ctx, 10, 3, now); err != nil || len(jobs) != 0 {
		t.Fatalf("PendingTitles = %+v, %v", jobs, err)
	}
}

func TestTurningTheSwitchOffClearsTranslationsAndTheQueue(t *testing.T) {
	ctx := context.Background()
	s, src, feed := titleStore(t)
	now := time.Now()
	if _, err := s.SaveFeed(ctx, feed, "", "", []FeedEntry{
		{GUID: "a", Title: "Hola", Language: "es"}, {GUID: "b", Title: "Adiós", Language: "es"},
	}, "zh-Hans", time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	a := entryID(t, s, "a")
	if err := s.SaveTitles(ctx, []TitleResult{{ID: a, Title: "Hola", Text: "你好"}}, now); err != nil {
		t.Fatal(err)
	}
	off := false
	if got, err := s.UpdateSource(ctx, src.ID, SourcePatch{TranslateTitles: &off}, now); err != nil || got.TranslateTitles {
		t.Fatalf("UpdateSource = %+v, %v", got, err)
	}
	for _, guid := range []string{"a", "b"} {
		if status, text, _ := titleStatus(t, s, entryID(t, s, guid)); status != "" || text != "" {
			t.Errorf("%s: %q %q", guid, status, text)
		}
	}
	list, err := s.Entries(ctx, EntryFilter{SourceID: src.ID}, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range list {
		if e.TranslatedTitle != "" {
			t.Errorf("entry %d still has a translated title", e.ID)
		}
	}
}

func TestSearchMatchesTheTranslatedTitle(t *testing.T) {
	ctx := context.Background()
	s, src, feed := titleStore(t)
	now := time.Now()
	if _, err := s.SaveFeed(ctx, feed, "", "", []FeedEntry{
		{GUID: "a", Title: "Guten Morgen", Language: "de"}, {GUID: "b", Title: "Etwas anderes", Language: "de"},
	}, "zh-Hans", time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	a := entryID(t, s, "a")
	if err := s.SaveTitles(ctx, []TitleResult{{ID: a, Title: "Guten Morgen", Text: "早上好，世界"}}, now); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"早上好", "世界", "早", "Morgen", "guten"} {
		list, err := s.Entries(ctx, EntryFilter{SourceID: src.ID, Query: query}, 10)
		if err != nil || len(list) != 1 || list[0].ID != a {
			t.Fatalf("%q: %+v, %v", query, list, err)
		}
		if list[0].TranslatedTitle != "早上好，世界" || list[0].Title != "Guten Morgen" {
			t.Fatalf("%q: the entry shows %q / %q", query, list[0].Title, list[0].TranslatedTitle)
		}
	}
	// An exclusion applies to the translation too.
	list, err := s.Entries(ctx, EntryFilter{SourceID: src.ID, Query: "-早上好"}, 10)
	if err != nil || len(list) != 1 || list[0].ID == a {
		t.Fatalf("-早上好: %+v, %v", list, err)
	}
}
