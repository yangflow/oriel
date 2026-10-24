package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

const (
	oldAddress = "http://old.example/feed.atom"
	newAddress = "https://blog.example/feed.atom"
)

func movedStore(t *testing.T) (*Store, Source, time.Time) {
	t.Helper()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.UnixMilli(1_790_000_000_000)
	src, err := s.CreateSource(context.Background(), NewSource{URL: oldAddress, Kind: "article", FetchInterval: time.Hour}, "Blog", now)
	if err != nil {
		t.Fatal(err)
	}
	return s, src, now
}

func movedTo(t *testing.T, s *Store, id int64) string {
	t.Helper()
	src, err := s.Source(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return src.MovedTo
}

func note(t *testing.T, s *Store, id int64, final string, now time.Time) {
	t.Helper()
	if err := s.NoteRedirect(context.Background(), id, final, now); err != nil {
		t.Fatal(err)
	}
}

func TestNoteRedirectSuggestsAfterThreeInARow(t *testing.T) {
	s, src, now := movedStore(t)
	note(t, s, src.ID, newAddress, now)
	note(t, s, src.ID, newAddress, now)
	if got := movedTo(t, s, src.ID); got != "" {
		t.Fatalf("suggested after two: %q", got)
	}
	later := now.Add(time.Hour)
	note(t, s, src.ID, newAddress, later)
	if got := movedTo(t, s, src.ID); got != newAddress {
		t.Fatalf("after three: %q", got)
	}
	got, _ := s.Source(context.Background(), src.ID)
	if !got.UpdatedAt.Equal(later) {
		t.Fatalf("updated_at %v, want %v, so that clients see the suggestion", got.UpdatedAt, later)
	}
	// More of the same keeps it, without a change to show.
	note(t, s, src.ID, newAddress, later.Add(time.Hour))
	got, _ = s.Source(context.Background(), src.ID)
	if got.MovedTo != newAddress || !got.UpdatedAt.Equal(later) {
		t.Fatalf("after four: %+v", got)
	}
}

func TestNoteRedirectResets(t *testing.T) {
	s, src, now := movedStore(t)
	for _, reset := range []string{""} {
		note(t, s, src.ID, newAddress, now)
		note(t, s, src.ID, newAddress, now)
		note(t, s, src.ID, reset, now) // a plain or temporary redirect gives no address
		note(t, s, src.ID, newAddress, now)
		if got := movedTo(t, s, src.ID); got != "" {
			t.Fatalf("the count survived a reset: %q", got)
		}
	}
	// A suggestion that stands goes with the first fetch that is not permanently redirected.
	note(t, s, src.ID, newAddress, now)
	note(t, s, src.ID, newAddress, now)
	if movedTo(t, s, src.ID) != newAddress {
		t.Fatal("no suggestion")
	}
	note(t, s, src.ID, "", now)
	if got := movedTo(t, s, src.ID); got != "" {
		t.Fatalf("still suggested: %q", got)
	}
	// Another address restarts at one.
	note(t, s, src.ID, newAddress, now)
	note(t, s, src.ID, newAddress, now)
	note(t, s, src.ID, "https://other.example/feed", now)
	note(t, s, src.ID, newAddress, now)
	if got := movedTo(t, s, src.ID); got != "" {
		t.Fatalf("a different address kept the count: %q", got)
	}
}

func TestNoteRedirectIgnoresFailures(t *testing.T) {
	s, src, now := movedStore(t)
	note(t, s, src.ID, newAddress, now)
	note(t, s, src.ID, newAddress, now)
	if err := s.FetchFailure(context.Background(), src.ID, "timeout", "", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	note(t, s, src.ID, newAddress, now)
	if got := movedTo(t, s, src.ID); got != newAddress {
		t.Fatalf("a failure between fetches reset the count: %q", got)
	}
	if err := s.FetchFailure(context.Background(), src.ID, "timeout", "", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := movedTo(t, s, src.ID); got != newAddress {
		t.Fatalf("a failure removed the suggestion: %q", got)
	}
}

func suggest(t *testing.T, s *Store, id int64, address string, now time.Time) {
	t.Helper()
	for range MovedSeenNeeded {
		note(t, s, id, address, now)
	}
	if got := movedTo(t, s, id); got != address {
		t.Fatalf("no suggestion of %q: %q", address, got)
	}
}

func TestDismissMove(t *testing.T) {
	ctx := context.Background()
	s, src, now := movedStore(t)
	suggest(t, s, src.ID, newAddress, now)
	if _, err := s.DismissMove(ctx, src.ID, "https://elsewhere.example/feed", now); !errors.Is(err, ErrMoveChanged) {
		t.Fatalf("dismissing another address: %v", err)
	}
	got, err := s.DismissMove(ctx, src.ID, newAddress, now)
	if err != nil || got.MovedTo != "" || got.URL != oldAddress {
		t.Fatalf("dismiss: %+v %v", got, err)
	}
	// The dismissed address is not counted again.
	for range 5 {
		note(t, s, src.ID, newAddress, now)
	}
	if got := movedTo(t, s, src.ID); got != "" {
		t.Fatalf("a dismissed address came back: %q", got)
	}
	// A different new address starts a count of its own.
	const another = "https://blog.example/v2/feed.atom"
	suggest(t, s, src.ID, another, now)
	if _, err := s.DismissMove(ctx, src.ID, newAddress, now); !errors.Is(err, ErrMoveChanged) {
		t.Fatalf("dismissing with an old suggestion: %v", err)
	}
	if _, err := s.DismissMove(ctx, 999, newAddress, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dismissing a missing source: %v", err)
	}
}

func TestAcceptMoveKeepsEntriesAndState(t *testing.T) {
	ctx := context.Background()
	s, src, now := movedStore(t)
	if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, URL: src.URL, Kind: "article"}, "Blog", "", []FeedEntry{
		{GUID: "a", Title: "A", URL: "https://blog.example/a", PublishedAt: now},
		{GUID: "b", Title: "B", URL: "https://blog.example/b", PublishedAt: now},
	}, "", time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	entries, err := s.Entries(ctx, EntryFilter{SourceID: src.ID}, 10)
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries %v %v", entries, err)
	}
	yes := true
	if _, err := s.UpdateEntry(ctx, entries[0].ID, EntryPatch{IsRead: &yes, Starred: &yes}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE sources SET etag = 'x', last_modified = 'y' WHERE id = ?", src.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := s.AcceptMove(ctx, src.ID, newAddress, now); !errors.Is(err, ErrMoveChanged) {
		t.Fatalf("accepting with no suggestion: %v", err)
	}
	suggest(t, s, src.ID, newAddress, now)
	if _, err := s.AcceptMove(ctx, src.ID, "https://elsewhere.example/feed", now); !errors.Is(err, ErrMoveChanged) {
		t.Fatalf("accepting another address: %v", err)
	}
	later := now.Add(time.Hour)
	moved, err := s.AcceptMove(ctx, src.ID, newAddress, later)
	if err != nil {
		t.Fatal(err)
	}
	if moved.URL != newAddress || moved.MovedTo != "" || !moved.UpdatedAt.Equal(later) || moved.ID != src.ID {
		t.Fatalf("moved: %+v", moved)
	}
	var etag, modified string
	var next int64
	if err := s.db.QueryRow("SELECT etag, last_modified, next_fetch_at FROM sources WHERE id = ?", src.ID).Scan(&etag, &modified, &next); err != nil {
		t.Fatal(err)
	}
	if etag != "" || modified != "" || next != later.UnixMilli() {
		t.Fatalf("validators %q %q, next %d", etag, modified, next)
	}
	after, err := s.Entries(ctx, EntryFilter{SourceID: src.ID}, 10)
	if err != nil || len(after) != 2 {
		t.Fatalf("entries after: %v %v", after, err)
	}
	for _, e := range after {
		if e.ID == entries[0].ID && (!e.IsRead || !e.Starred) {
			t.Fatalf("the read and starred state was lost: %+v", e)
		}
	}
	// The same address is not accepted twice.
	if _, err := s.AcceptMove(ctx, src.ID, newAddress, later); !errors.Is(err, ErrMoveChanged) {
		t.Fatalf("accepting twice: %v", err)
	}
}

func TestAcceptMoveRefusesAnAddressAlreadySubscribed(t *testing.T) {
	ctx := context.Background()
	s, src, now := movedStore(t)
	other, err := s.CreateSource(ctx, NewSource{URL: newAddress, Kind: "article", FetchInterval: time.Hour}, "Blog (new)", now)
	if err != nil {
		t.Fatal(err)
	}
	suggest(t, s, src.ID, newAddress, now)
	_, err = s.AcceptMove(ctx, src.ID, newAddress, now)
	var exists *ExistsError
	if !errors.As(err, &exists) || exists.ID != other.ID || !errors.Is(err, ErrExists) {
		t.Fatalf("accept: %v", err)
	}
	got, _ := s.Source(ctx, src.ID)
	if got.URL != oldAddress || got.MovedTo != newAddress {
		t.Fatalf("a refused move changed the source: %+v", got)
	}
}

func TestHealthListsAMovedSource(t *testing.T) {
	ctx := context.Background()
	s, src, _ := movedStore(t)
	if _, err := s.db.Exec("UPDATE sources SET created_at = ? WHERE id = ?", healthNow.Add(-200*day).UnixMilli(), src.ID); err != nil {
		t.Fatal(err)
	}
	healthEntries(t, s, src.ID, day, 3, 3, 0)
	if _, err := s.db.Exec("UPDATE sources SET last_success_at = ? WHERE id = ?", healthNow.Add(-time.Hour).UnixMilli(), src.ID); err != nil {
		t.Fatal(err)
	}
	if got := healthByTitle(t, s); len(got) != 0 {
		t.Fatalf("flagged without a suggestion: %+v", got)
	}
	suggest(t, s, src.ID, newAddress, healthNow)
	h := healthByTitle(t, s)["Blog"]
	if h.State != HealthMoved || h.MovedTo != newAddress {
		t.Fatalf("health: %+v", h)
	}
	// A failing source shows its own state, without the move.
	if _, err := s.db.Exec("UPDATE sources SET consecutive_failures = 3, last_error = 'x' WHERE id = ?", src.ID); err != nil {
		t.Fatal(err)
	}
	if h := healthByTitle(t, s)["Blog"]; h.State != HealthFailing || h.MovedTo != "" {
		t.Fatalf("failing health: %+v", h)
	}
	if _, err := s.DismissMove(ctx, src.ID, newAddress, healthNow); err != nil {
		t.Fatal(err)
	}
}

func titles(t *testing.T, s *Store, id int64) (title, feedTitle string) {
	t.Helper()
	if err := s.db.QueryRow("SELECT title, feed_title FROM sources WHERE id = ?", id).Scan(&title, &feedTitle); err != nil {
		t.Fatal(err)
	}
	return
}

func TestSaveFeedFollowsTheFeedTitle(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.UnixMilli(1_790_000_000_000)
	save := func(src Source, title string) {
		t.Helper()
		if _, err := s.SaveFeed(ctx, FeedSource{ID: src.ID, URL: src.URL, Kind: "article"}, title, "", nil, "", time.Time{}, now); err != nil {
			t.Fatal(err)
		}
	}
	mk := func(url, title string) Source {
		src, err := s.CreateSource(ctx, NewSource{URL: url, Kind: "article", FetchInterval: time.Hour}, title, now)
		if err != nil {
			t.Fatal(err)
		}
		return src
	}

	// An empty title fills in and then follows renames of the feed.
	followed := mk("https://a.example/feed", "")
	save(followed, "Old name")
	if title, feed := titles(t, s, followed.ID); title != "Old name" || feed != "Old name" {
		t.Fatalf("filled: %q %q", title, feed)
	}
	save(followed, "New name")
	if title, feed := titles(t, s, followed.ID); title != "New name" || feed != "New name" {
		t.Fatalf("followed: %q %q", title, feed)
	}
	// A feed that gives no title changes nothing.
	save(followed, "")
	if title, _ := titles(t, s, followed.ID); title != "New name" {
		t.Fatalf("an empty feed title: %q", title)
	}
	// Once the user renames it, the feed's title no longer replaces theirs, but is still remembered.
	if _, err := s.db.Exec("UPDATE sources SET title = 'Mine' WHERE id = ?", followed.ID); err != nil {
		t.Fatal(err)
	}
	save(followed, "Newer name")
	if title, feed := titles(t, s, followed.ID); title != "Mine" || feed != "New name" {
		t.Fatalf("renamed by the user: %q %q", title, feed)
	}

	// Upgrade: a source from before feed_title has none; a title that differs from the feed's is the user's.
	renamed := mk("https://b.example/feed", "My own")
	save(renamed, "Feed name")
	if title, feed := titles(t, s, renamed.ID); title != "My own" || feed != "" {
		t.Fatalf("old renamed source: %q %q", title, feed)
	}
	// ... and one that equals it starts to follow.
	same := mk("https://c.example/feed", "Feed name")
	save(same, "Feed name")
	save(same, "Feed name 2")
	if title, feed := titles(t, s, same.ID); title != "Feed name 2" || feed != "Feed name 2" {
		t.Fatalf("old following source: %q %q", title, feed)
	}
}

func TestSetFeedIcon(t *testing.T) {
	ctx := context.Background()
	s, src, _ := movedStore(t)
	if err := s.SetSourceIcon(ctx, src.ID, "https://blog.example/old.png", "https://blog.example/"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFeedIcon(ctx, src.ID, "https://blog.example/new.png"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Source(ctx, src.ID); got.IconURL != "https://blog.example/new.png" {
		t.Fatalf("icon %q", got.IconURL)
	}
}
