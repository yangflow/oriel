package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

var healthNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

// healthSource adds a source created age ago, with the given consecutive failures and last success.
func healthSource(t *testing.T, s *Store, title string, age time.Duration, failures int, success time.Duration) int64 {
	t.Helper()
	src, err := s.CreateSource(context.Background(), NewSource{URL: "https://" + title + ".example/feed", Kind: "article", FetchInterval: time.Hour}, title, healthNow.Add(-age))
	if err != nil {
		t.Fatal(err)
	}
	var lastSuccess any
	if success > 0 {
		lastSuccess = healthNow.Add(-success).UnixMilli()
	}
	if _, err := s.db.Exec("UPDATE sources SET consecutive_failures = ?, last_success_at = ?, last_error = ? WHERE id = ?",
		failures, lastSuccess, "HTTP 404 Not Found", src.ID); err != nil {
		t.Fatal(err)
	}
	return src.ID
}

// healthEntries adds n entries that arrived ago, read of them read, starred of the read ones starred.
func healthEntries(t *testing.T, s *Store, id int64, ago time.Duration, n, read, starred int) {
	t.Helper()
	for i := range n {
		isRead, isStarred := 0, 0
		if i < read {
			isRead = 1
		}
		if i < starred {
			isStarred = 1
		}
		at := healthNow.Add(-ago).UnixMilli()
		if _, err := s.db.Exec(`INSERT INTO entries (source_id, guid, title, is_read, starred, created_at, updated_at) VALUES (?, ?, 't', ?, ?, ?, ?)`,
			id, fmt.Sprintf("%d-%d-%d", id, ago, i), isRead, isStarred, at, at); err != nil {
			t.Fatal(err)
		}
	}
}

func healthByTitle(t *testing.T, s *Store) map[string]SourceHealth {
	t.Helper()
	list, err := s.SourcesHealth(context.Background(), healthNow)
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]SourceHealth{}
	for _, h := range list {
		byTitle[h.Title] = h
	}
	return byTitle
}

func TestSourcesHealthReasons(t *testing.T) {
	s := open(t, t.TempDir()+"/oriel.sqlite")
	old := 200 * day

	silent := healthSource(t, s, "silent", old, 0, 10*day)
	healthEntries(t, s, silent, 91*day, 3, 3, 0)

	quiet := healthSource(t, s, "quiet", old, 0, day)
	healthEntries(t, s, quiet, 89*day, 2, 2, 0)

	healthSource(t, s, "never", 100*day, 0, 0) // no entry ever: counted from the day it was added

	deadFailing := healthSource(t, s, "deadfailing", old, 5, 41*day)
	healthEntries(t, s, deadFailing, day, 1, 0, 0)

	failing := healthSource(t, s, "failing", old, 3, 5*day)
	healthEntries(t, s, failing, day, 1, 0, 0)

	someFailures := healthSource(t, s, "somefailures", old, 2, 5*day)
	healthEntries(t, s, someFailures, day, 1, 0, 0)

	neverSucceeded := healthSource(t, s, "neversucceeded", 31*day, 4, 0)
	healthEntries(t, s, neverSucceeded, day, 1, 0, 0)

	rare := healthSource(t, s, "rare", old, 0, day)
	healthEntries(t, s, rare, 2*day, 20, 1, 1)

	fine := healthSource(t, s, "fine", old, 0, day)
	healthEntries(t, s, fine, 2*day, 20, 2, 0)

	few := healthSource(t, s, "few", old, 0, day)
	healthEntries(t, s, few, 2*day, 9, 0, 0)

	both := healthSource(t, s, "both", old, 6, 50*day)
	healthEntries(t, s, both, 120*day, 1, 0, 0)

	got := healthByTitle(t, s)
	want := map[string]struct {
		state, cause string
		days         int
	}{
		"silent":         {HealthDead, HealthCauseSilent, 91},
		"never":          {HealthDead, HealthCauseSilent, 100},
		"deadfailing":    {HealthDead, HealthCauseFailing, 41},
		"neversucceeded": {HealthDead, HealthCauseFailing, 31},
		"both":           {HealthDead, HealthCauseFailing, 50},
		"failing":        {HealthFailing, "", 0},
		"rare":           {HealthRarelyRead, "", HealthWindowDays},
	}
	if len(got) != len(want) {
		t.Fatalf("flagged %d sources, want %d: %+v", len(got), len(want), got)
	}
	for title, w := range want {
		h, ok := got[title]
		if !ok {
			t.Errorf("%s is not flagged", title)
			continue
		}
		if h.State != w.state || h.Cause != w.cause || h.Days != w.days {
			t.Errorf("%s = %s/%s/%d, want %s/%s/%d", title, h.State, h.Cause, h.Days, w.state, w.cause, w.days)
		}
	}
	if h := got["failing"]; h.ConsecutiveFailures != 3 || h.LastError != "HTTP 404 Not Found" || h.URL == "" {
		t.Errorf("failing = %+v", h)
	}
	if h := got["rare"]; h.Recent != 20 || h.RecentRead != 1 || h.RecentStarred != 1 {
		t.Errorf("rare numbers = %d/%d/%d", h.Recent, h.RecentRead, h.RecentStarred)
	}
	for _, id := range []int64{quiet, someFailures, fine, few} {
		src, _ := s.Source(context.Background(), id)
		if _, ok := got[src.Title]; ok {
			t.Errorf("%s is flagged", src.Title)
		}
	}
}

func TestSourcesHealthLeavesOutPausedAndYoungSources(t *testing.T) {
	s := open(t, t.TempDir()+"/oriel.sqlite")
	ctx := context.Background()

	paused := healthSource(t, s, "paused", 300*day, 9, 200*day)
	if _, err := s.UpdateSource(ctx, paused, SourcePatch{Paused: new(true)}, healthNow); err != nil {
		t.Fatal(err)
	}
	healthSource(t, s, "youngsilent", 80*day, 0, 0)
	healthSource(t, s, "youngfailing", 10*day, 8, 0)
	youngUnread := healthSource(t, s, "youngunread", 20*day, 0, day)
	healthEntries(t, s, youngUnread, day, 50, 0, 0)

	if got := healthByTitle(t, s); len(got) != 1 || got["youngfailing"].State != HealthFailing {
		t.Fatalf("flagged %+v, want only youngfailing, failing", got)
	}
}

func TestSourcesHealthCountsBulkReadAndSkipsMutedEntries(t *testing.T) {
	s := open(t, t.TempDir()+"/oriel.sqlite")
	ctx := context.Background()
	id := healthSource(t, s, "bulk", 100*day, 0, day)
	healthEntries(t, s, id, 3*day, 20, 0, 0)

	if got := healthByTitle(t, s); got["bulk"].State != HealthRarelyRead {
		t.Fatalf("unread = %+v", got)
	}
	if _, err := s.MarkRead(ctx, EntryFilter{SourceID: id}, healthNow); err != nil {
		t.Fatal(err)
	}
	if got := healthByTitle(t, s); len(got) != 0 {
		t.Fatalf("after marking all read: %+v", got)
	}

	muted := healthSource(t, s, "muted", 100*day, 0, day)
	healthEntries(t, s, muted, 3*day, 1, 1, 0)
	healthEntries(t, s, muted, 2*day, 20, 0, 0)
	if _, err := s.db.Exec("UPDATE entries SET title = 'spam' WHERE source_id = ? AND is_read = 0", muted); err != nil {
		t.Fatal(err)
	}
	if got := healthByTitle(t, s); got["muted"].State != HealthRarelyRead || got["muted"].Recent != 21 {
		t.Fatalf("muted before = %+v", got["muted"])
	}
	if _, err := s.db.Exec("UPDATE sources SET mute_keywords = 'spam' WHERE id = ?", muted); err != nil {
		t.Fatal(err)
	}
	if got := healthByTitle(t, s); len(got) != 0 {
		t.Fatalf("the unread entries are muted and cannot be read: %+v", got)
	}
}

func TestSourcesHealthOrder(t *testing.T) {
	s := open(t, t.TempDir()+"/oriel.sqlite")
	rare := healthSource(t, s, "a-rare", 100*day, 0, day)
	healthEntries(t, s, rare, day, 12, 0, 0)
	for _, title := range []string{"c-failing", "B-failing"} {
		healthEntries(t, s, healthSource(t, s, title, 100*day, 3, day), day, 1, 0, 0)
	}
	dead := healthSource(t, s, "z-dead", 200*day, 0, day)
	healthEntries(t, s, dead, 100*day, 1, 1, 0)

	list, err := s.SourcesHealth(context.Background(), healthNow)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, h := range list {
		titles = append(titles, h.Title)
	}
	want := []string{"z-dead", "B-failing", "c-failing", "a-rare"}
	if fmt.Sprint(titles) != fmt.Sprint(want) {
		t.Fatalf("order = %v, want %v", titles, want)
	}
}

func TestSourcesHealthEmpty(t *testing.T) {
	s := open(t, t.TempDir()+"/oriel.sqlite")
	list, err := s.SourcesHealth(context.Background(), healthNow)
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("list = %v, %v", list, err)
	}
}
