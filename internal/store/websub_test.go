package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func websubStore(t *testing.T) (*Store, Source, time.Time) {
	t.Helper()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.UnixMilli(1_790_000_000_000)
	src, err := s.CreateSource(context.Background(), NewSource{URL: "https://blog.example/feed.xml", Kind: "article", FetchInterval: time.Hour}, "", now)
	if err != nil {
		t.Fatal(err)
	}
	return s, src, now
}

func TestWebSubLifecycle(t *testing.T) {
	ctx := context.Background()
	s, src, now := websubStore(t)
	const hub, topic = "https://hub.example/", "https://blog.example/feed.xml"

	// A source without a hub is never due, and has no key to call back.
	if due, err := s.WebSubDue(ctx, now, "https://o.example", 10); err != nil || len(due) != 0 {
		t.Fatalf("due without hub: %v %v", due, err)
	}
	if _, err := s.WebSubByKey(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty key: %v", err)
	}

	if err := s.SetWebSubFeed(ctx, src.ID, hub, topic); err != nil {
		t.Fatal(err)
	}
	due, err := s.WebSubDue(ctx, now, "https://o.example", 10)
	if err != nil || len(due) != 1 || due[0].SourceID != src.ID || due[0].Hub != hub || due[0].Topic != topic {
		t.Fatalf("due: %+v %v", due, err)
	}
	if got, _ := s.Source(ctx, src.ID); got.WebSubHub != hub || !got.WebSubUntil.IsZero() {
		t.Fatalf("source: %+v", got)
	}

	w, err := s.WebSubRequest(ctx, src.ID, "https://o.example", now)
	if err != nil || len(w.Key) != 32 || len(w.Secret) != 64 || w.State != "pending" || !w.RequestedAt.Equal(now) ||
		!w.NextAt.Equal(now.Add(WebSubWait)) || w.Base != "https://o.example" {
		t.Fatalf("request: %+v %v", w, err)
	}
	if got, err := s.WebSubByKey(ctx, w.Key); err != nil || got.SourceID != src.ID {
		t.Fatalf("by key: %+v %v", got, err)
	}
	// A pending request is not due again until the wait is over.
	if due, _ := s.WebSubDue(ctx, now.Add(time.Minute), "https://o.example", 10); len(due) != 0 {
		t.Fatalf("due during the wait: %+v", due)
	}
	if due, _ := s.WebSubDue(ctx, now.Add(WebSubWait), "https://o.example", 10); len(due) != 1 {
		t.Fatalf("not due after the wait: %+v", due)
	}
	// The key and secret stay the same when the request is repeated.
	again, _ := s.WebSubRequest(ctx, src.ID, "https://o.example", now.Add(WebSubWait))
	if again.Key != w.Key || again.Secret != w.Secret {
		t.Fatal("key or secret changed")
	}

	expires := now.Add(time.Hour)
	if err := s.WebSubVerified(ctx, src.ID, expires, now.Add(48*time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Source(ctx, src.ID)
	if !got.WebSubUntil.Equal(expires) {
		t.Fatalf("until %v", got.WebSubUntil)
	}
	// A new public address makes an active subscription due at once.
	if due, _ := s.WebSubDue(ctx, now, "https://other.example", 10); len(due) != 1 {
		t.Fatalf("due after the address changed: %+v", due)
	}
	if due, _ := s.WebSubDue(ctx, now, "https://o.example", 10); len(due) != 0 {
		t.Fatalf("due before the renewal: %+v", due)
	}
	// A renewal keeps the subscription active while it is asked for.
	if w, _ := s.WebSubRequest(ctx, src.ID, "https://o.example", now.Add(48*time.Minute)); w.State != "active" {
		t.Fatalf("renewal state %q", w.State)
	}

	// The hub changes: the old subscription goes, the key stays.
	if err := s.SetWebSubFeed(ctx, src.ID, "https://hub2.example/", topic); err != nil {
		t.Fatal(err)
	}
	if after, _ := s.WebSubByKey(ctx, w.Key); after.State != "" || after.Hub != "https://hub2.example/" || !after.ExpiresAt.IsZero() {
		t.Fatalf("after hub change: %+v", after)
	}
	// The same pair changes nothing.
	s.WebSubRequest(ctx, src.ID, "https://o.example", now)
	s.SetWebSubFeed(ctx, src.ID, "https://hub2.example/", topic)
	if after, _ := s.WebSubByKey(ctx, w.Key); after.State != "pending" {
		t.Fatalf("same hub reset the state: %+v", after)
	}
	// The hub disappearing clears the topic too, and paused sources are skipped.
	s.SetWebSubFeed(ctx, src.ID, "", "")
	if after, _ := s.WebSubByKey(ctx, w.Key); after.Hub != "" || after.Topic != "" {
		t.Fatalf("hub not cleared: %+v", after)
	}
}

func TestWebSubFailureBacksOff(t *testing.T) {
	ctx := context.Background()
	s, src, now := websubStore(t)
	s.SetWebSubFeed(ctx, src.ID, "https://hub.example/", "https://blog.example/feed.xml")
	first, _ := s.WebSubRequest(ctx, src.ID, "https://o.example", now)

	want := []time.Duration{10 * time.Minute, 20 * time.Minute, 40 * time.Minute}
	for i, delay := range want {
		if err := s.WebSubFailed(ctx, src.ID, "refused", now); err != nil {
			t.Fatal(err)
		}
		w, _ := s.WebSubByKey(ctx, first.Key)
		if w.Failures != i+1 || w.Error != "refused" || w.State != "" || !w.NextAt.Equal(now.Add(delay)) {
			t.Fatalf("failure %d: %+v", i+1, w)
		}
	}
	for range 30 {
		s.WebSubFailed(ctx, src.ID, "refused", now)
	}
	if w, _ := s.WebSubByKey(ctx, first.Key); !w.NextAt.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("backoff is not capped at a day: %v", w.NextAt.Sub(now))
	}

	// A failed renewal leaves an active subscription active.
	s.WebSubVerified(ctx, src.ID, now.Add(time.Hour), now)
	s.WebSubFailed(ctx, src.ID, "refused", now)
	if w, _ := s.WebSubByKey(ctx, first.Key); w.State != "active" || w.Failures != 1 {
		t.Fatalf("failed renewal: %+v", w)
	}
}

func TestWebSubDueSkipsPausedAndLimits(t *testing.T) {
	ctx := context.Background()
	s, src, now := websubStore(t)
	other, err := s.CreateSource(ctx, NewSource{URL: "https://other.example/feed", Kind: "article", FetchInterval: time.Hour}, "", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{src.ID, other.ID} {
		s.SetWebSubFeed(ctx, id, "https://hub.example/", "https://topic.example/"+time.Duration(id).String())
	}
	paused := true
	s.UpdateSource(ctx, other.ID, SourcePatch{Paused: &paused}, now)
	if due, _ := s.WebSubDue(ctx, now, "https://o.example", 10); len(due) != 1 || due[0].SourceID != src.ID {
		t.Fatalf("paused source listed: %+v", due)
	}
	paused = false
	s.UpdateSource(ctx, other.ID, SourcePatch{Paused: &paused}, now)
	if due, _ := s.WebSubDue(ctx, now, "https://o.example", 1); len(due) != 1 {
		t.Fatalf("limit ignored: %+v", due)
	}
}
