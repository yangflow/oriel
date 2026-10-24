package fetch

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
	"github.com/yangflow/oriel/internal/store/storetest"
)

// fakePictures answers by address: a picture, or the error given for it. Anything else is a 404.
type fakePictures struct {
	mu    sync.Mutex
	known map[string]error
	asked []string
}

func (f *fakePictures) Get(_ context.Context, address string) (string, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, address)
	err, ok := f.known[address]
	if !ok {
		return "", nil, &StatusError{Code: 404}
	}
	if err != nil {
		return "", nil, err
	}
	return "image/png", append([]byte(nil), pngHeader...), nil
}

type copyFixture struct {
	store *store.Store
	id    int64
	now   time.Time
}

// newCopyFixture stores one starred article, with its full text already extracted, whose thumbnail, feed image,
// full text and content name the pictures below.
func newCopyFixture(t *testing.T) *copyFixture {
	t.Helper()
	ctx := context.Background()
	st := storetest.Open(t)
	now := time.UnixMilli(1_790_000_000_000)
	src, err := st.CreateSource(ctx, store.NewSource{URL: "https://example.org/feed", Kind: "article", FetchInterval: time.Hour}, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveFeed(ctx, store.FeedSource{ID: src.ID, Kind: src.Kind}, "", "", []store.FeedEntry{{
		GUID: "g", Title: "t", URL: "https://example.org/post/1", ThumbnailURL: "https://cdn.example.org/thumb.png",
		Images:      []store.Image{{URL: "https://cdn.example.org/feed.png"}},
		ContentHTML: `<p><img src="rel.png"><img src="https://cdn.example.org/pixel.png" width="1" height="1">`,
	}}, "", time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	var id int64
	if list, err := st.PendingExtractions(ctx, now, 10); err != nil || len(list) != 1 {
		t.Fatalf("extractions %v, %v", list, err)
	} else {
		id = list[0].ID
	}
	if err := st.SaveExtraction(ctx, id, `<p><img src="/full.png"><img src="https://www.google-analytics.com/collect"><img data-src="lazy.png"></p>`, "text", ""); err != nil {
		t.Fatal(err)
	}
	star := true
	if _, err := st.UpdateEntry(ctx, id, store.EntryPatch{Starred: &star}, now); err != nil {
		t.Fatal(err)
	}
	return &copyFixture{store: st, id: id, now: now}
}

func (f *copyFixture) copier(pics pictures) *Copier {
	return &Copier{store: f.store, images: pics, now: func() time.Time { return f.now }}
}

func (f *copyFixture) copied(t *testing.T) map[string]bool {
	t.Helper()
	have, err := f.store.CopiedAddresses(context.Background(), f.id)
	if err != nil {
		t.Fatal(err)
	}
	return have
}

var allPictures = []string{
	"https://cdn.example.org/thumb.png", "https://cdn.example.org/feed.png", "https://example.org/full.png",
	"https://example.org/post/lazy.png", "https://example.org/post/rel.png",
}

func TestCopyAddresses(t *testing.T) {
	f := newCopyFixture(t)
	list, err := f.store.PendingCopies(context.Background(), f.now, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("pending %v, %v", list, err)
	}
	// Thumbnail, feed images, then the full text, then the content; trackers, pixels and duplicates are left out.
	want := []string{
		"https://cdn.example.org/thumb.png", "https://cdn.example.org/feed.png", "https://example.org/full.png",
		"https://example.org/post/lazy.png", "https://example.org/post/rel.png",
	}
	if got := copyAddresses(list[0]); !reflect.DeepEqual(got, want) {
		t.Errorf("addresses\n got %v\nwant %v", got, want)
	}

	var many strings.Builder
	for i := range 50 {
		fmt.Fprintf(&many, `<img src="https://example.org/%d.png">`, i)
	}
	got := copyAddresses(store.CopyTarget{URL: "https://example.org/x", ContentHTML: many.String()})
	if len(got) != MaxCopyImages || got[0] != "https://example.org/0.png" {
		t.Errorf("%d addresses, first %q", len(got), got[0])
	}
}

func TestCopierStoresThePictures(t *testing.T) {
	f := newCopyFixture(t)
	pics := &fakePictures{known: map[string]error{}}
	for _, a := range allPictures {
		pics.known[a] = nil
	}
	f.copier(pics).round(context.Background())

	have := f.copied(t)
	if len(have) != len(allPictures) {
		t.Fatalf("copied %v", have)
	}
	for _, a := range pics.asked {
		if strings.Contains(a, "google-analytics") || strings.Contains(a, "pixel") {
			t.Errorf("a tracking picture was fetched: %s", a)
		}
	}
	if n, size, _ := f.store.CopyStats(context.Background()); n != len(allPictures) || size != int64(len(allPictures)*len(pngHeader)) {
		t.Errorf("stats %d, %d", n, size)
	}
	if list, _ := f.store.PendingCopies(context.Background(), f.now.Add(100*time.Hour), 10); len(list) != 0 {
		t.Errorf("a finished copy is pending: %+v", list)
	}
	asked := len(pics.asked)
	f.copier(pics).round(context.Background())
	if len(pics.asked) != asked {
		t.Error("a finished copy was downloaded again")
	}
}

func TestCopierRetriesAFailedPicture(t *testing.T) {
	ctx := context.Background()
	f := newCopyFixture(t)
	pics := &fakePictures{known: map[string]error{}}
	for _, a := range allPictures {
		pics.known[a] = nil
	}
	pics.known["https://example.org/full.png"] = &StatusError{Code: 503}
	pics.known["https://cdn.example.org/feed.png"] = &StatusError{Code: 404} // gone for good: skipped
	pics.known["https://example.org/post/lazy.png"] = ErrNotImage            // skipped too
	c := f.copier(pics)
	c.round(ctx)

	if have := f.copied(t); len(have) != 2 || have["https://example.org/full.png"] {
		t.Fatalf("after the first try: %v", have)
	}
	if list, _ := f.store.PendingCopies(ctx, f.now, 10); len(list) != 0 {
		t.Fatalf("retried at once: %+v", list)
	}
	list, _ := f.store.PendingCopies(ctx, f.now.Add(time.Minute), 10)
	if len(list) != 1 || list[0].Attempts != 1 {
		t.Fatalf("not due after a minute: %+v", list)
	}

	// The retry downloads only what is missing and finishes.
	pics.known["https://example.org/full.png"] = nil
	pics.asked = nil
	f.now = f.now.Add(time.Minute)
	c.round(ctx)
	if len(pics.asked) != 3 || !f.copied(t)["https://example.org/full.png"] {
		t.Fatalf("retry asked %v, copied %v", pics.asked, f.copied(t))
	}
	if list, _ := f.store.PendingCopies(ctx, f.now.Add(100*time.Hour), 10); len(list) != 0 {
		t.Errorf("still pending: %+v", list)
	}
}

func TestCopierGivesUp(t *testing.T) {
	ctx := context.Background()
	f := newCopyFixture(t)
	pics := &fakePictures{known: map[string]error{"https://cdn.example.org/thumb.png": errors.New("connection reset")}}
	c := f.copier(pics)
	var waits []time.Duration
	for range maxCopyAttempts {
		start := f.now
		c.round(ctx)
		list, _ := f.store.PendingCopies(ctx, f.now.Add(1000*time.Hour), 10)
		if len(list) == 0 {
			break
		}
		for _, d := range []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour} {
			if l, _ := f.store.PendingCopies(ctx, start.Add(d), 10); len(l) == 1 {
				waits = append(waits, d)
				f.now = start.Add(d)
				break
			}
		}
	}
	want := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}
	if !reflect.DeepEqual(waits, want) {
		t.Errorf("waits %v, want %v", waits, want)
	}
	if list, _ := f.store.PendingCopies(ctx, f.now.Add(1000*time.Hour), 10); len(list) != 0 {
		t.Errorf("still retried after %d attempts: %+v", maxCopyAttempts, list)
	}
}

func TestCopierSkipsAnEntryUnstarredMeanwhile(t *testing.T) {
	ctx := context.Background()
	f := newCopyFixture(t)
	pics := &fakePictures{known: map[string]error{}}
	for _, a := range allPictures {
		pics.known[a] = nil
	}
	off := false
	if _, err := f.store.UpdateEntry(ctx, f.id, store.EntryPatch{Starred: &off}, f.now); err != nil {
		t.Fatal(err)
	}
	f.copier(pics).round(ctx)
	if n, _, _ := f.store.CopyStats(ctx); n != 0 || len(pics.asked) != 0 {
		t.Errorf("an unstarred entry was copied: %d copies, %v", n, pics.asked)
	}
}

func TestTemporaryPictureError(t *testing.T) {
	for err, want := range map[error]bool{
		ErrBlockedAddress: false, ErrNotImage: false, errTooLarge: false, errNotWebAddress: false,
		&StatusError{Code: 404}: false, &StatusError{Code: 503}: true, errors.New("timeout"): true,
	} {
		if got := temporaryPictureError(err); got != want {
			t.Errorf("%v: %v", err, got)
		}
	}
}
