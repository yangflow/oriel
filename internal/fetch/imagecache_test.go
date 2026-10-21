package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type cacheOrigin struct {
	*httptest.Server
	hits atomic.Int32
}

// newCacheOrigin serves /a, /b and /c as pictures of the given size and /bad as text.
func newCacheOrigin(t *testing.T, size int) *cacheOrigin {
	t.Helper()
	o := &cacheOrigin{}
	o.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.hits.Add(1)
		if r.URL.Path == "/bad" {
			w.Write([]byte("plain text"))
			return
		}
		w.Write(append(append([]byte{}, pngHeader...), make([]byte, size)...))
	}))
	t.Cleanup(o.Close)
	return o
}

func cachedImages(dir string) *Images {
	im := NewImages("test", dir)
	im.guarded = guardedClient(func(netip.AddrPort) bool { return false })
	return im
}

func cacheFiles(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, item := range list {
		names = append(names, item.Name())
	}
	return names
}

func TestImageCacheHitAndMiss(t *testing.T) {
	origin := newCacheOrigin(t, 10)
	dir := filepath.Join(t.TempDir(), "image-cache")
	im := cachedImages(dir)
	ctx := context.Background()

	kind, first, err := im.Get(ctx, origin.URL+"/a")
	if err != nil || kind != "image/png" || origin.hits.Load() != 1 {
		t.Fatalf("miss = %q, %v, origin hits %d", kind, err, origin.hits.Load())
	}
	kind, second, err := im.Get(ctx, origin.URL+"/a")
	if err != nil || kind != "image/png" || string(second) != string(first) || origin.hits.Load() != 1 {
		t.Fatalf("hit = %q, %v, origin hits %d", kind, err, origin.hits.Load())
	}
	if _, _, err := im.Get(ctx, origin.URL+"/b"); err != nil || origin.hits.Load() != 2 {
		t.Fatalf("other address: %v, origin hits %d", err, origin.hits.Load())
	}
	if names := cacheFiles(t, dir); len(names) != 2 || len(names[0]) != 64 {
		t.Errorf("files = %v", names)
	}
}

func TestImageCacheExpiry(t *testing.T) {
	origin := newCacheOrigin(t, 10)
	im := cachedImages(t.TempDir())
	now := time.Now()
	im.cache.now = func() time.Time { return now }
	ctx := context.Background()

	im.Get(ctx, origin.URL+"/a")
	now = now.Add(imageCacheTTL - time.Hour)
	im.Get(ctx, origin.URL+"/a")
	if origin.hits.Load() != 1 {
		t.Fatalf("fresh copy missed: origin hits %d", origin.hits.Load())
	}
	now = now.Add(2 * time.Hour) // a hit does not renew the TTL
	im.Get(ctx, origin.URL+"/a")
	if origin.hits.Load() != 2 {
		t.Fatalf("expired copy served: origin hits %d", origin.hits.Load())
	}
	im.Get(ctx, origin.URL+"/a")
	if origin.hits.Load() != 2 {
		t.Errorf("replaced copy missed: origin hits %d", origin.hits.Load())
	}
}

func TestImageCacheEvictsLeastRecentlyUsed(t *testing.T) {
	origin := newCacheOrigin(t, 1000)
	dir := t.TempDir()
	im := cachedImages(dir)
	now := time.Now()
	im.cache.now = func() time.Time { now = now.Add(time.Second); return now }
	im.cache.maxBytes = 3500 // three pictures fit, four do not
	ctx := context.Background()

	im.Get(ctx, origin.URL+"/a")
	im.Get(ctx, origin.URL+"/b")
	im.Get(ctx, origin.URL+"/c")
	im.Get(ctx, origin.URL+"/a") // a is now newer than b
	if origin.hits.Load() != 3 {
		t.Fatalf("origin hits %d", origin.hits.Load())
	}
	im.Get(ctx, origin.URL+"/d") // over the cap: b and c go first, down to 90%
	if im.cache.total > im.cache.maxBytes {
		t.Fatalf("total %d over the cap", im.cache.total)
	}
	before := origin.hits.Load()
	im.Get(ctx, origin.URL+"/a")
	im.Get(ctx, origin.URL+"/d")
	if origin.hits.Load() != before {
		t.Errorf("recently used files were evicted")
	}
	im.Get(ctx, origin.URL+"/b")
	if origin.hits.Load() != before+1 {
		t.Errorf("least recently used file was kept")
	}
}

func TestImageCacheSkipsFailures(t *testing.T) {
	origin := newCacheOrigin(t, 10)
	dir := t.TempDir()
	im := cachedImages(dir)
	ctx := context.Background()

	for range 2 {
		if _, _, err := im.Get(ctx, origin.URL+"/bad"); err == nil {
			t.Fatal("text was accepted")
		}
	}
	if origin.hits.Load() != 2 || len(cacheFiles(t, dir)) != 0 {
		t.Errorf("failure cached: origin hits %d, files %v", origin.hits.Load(), cacheFiles(t, dir))
	}
}

func TestImageCacheRefusesTrackersFirst(t *testing.T) {
	dir := t.TempDir()
	im := cachedImages(dir)
	address := "https://www.google-analytics.com/collect?v=1&tid=UA-1"
	im.cache.put(address, "image/png", pngHeader)
	if _, _, err := im.Get(context.Background(), address); err == nil {
		t.Fatal("tracker served from the cache")
	}
}

func TestImageCacheWritesAtomically(t *testing.T) {
	origin := newCacheOrigin(t, 10)
	dir := t.TempDir()
	im := cachedImages(dir)
	ctx := context.Background()
	done := make(chan struct{})
	for range 8 {
		go func() {
			im.Get(ctx, origin.URL+"/a")
			done <- struct{}{}
		}()
	}
	for range 8 {
		<-done
	}
	for _, name := range cacheFiles(t, dir) {
		if strings.HasSuffix(name, tempSuffix) {
			t.Errorf("temp file left: %s", name)
		}
	}
	if len(cacheFiles(t, dir)) != 1 || im.cache.total != im.cache.entries[cacheKey(origin.URL+"/a")].size {
		t.Errorf("files %v, total %d", cacheFiles(t, dir), im.cache.total)
	}
}

func TestImageCacheSurvivesRestart(t *testing.T) {
	origin := newCacheOrigin(t, 10)
	dir := t.TempDir()
	cachedImages(dir).Get(context.Background(), origin.URL+"/a")
	os.WriteFile(filepath.Join(dir, "put-1"+tempSuffix), []byte("half"), 0o600)

	restarted := cachedImages(dir)
	if _, _, err := restarted.Get(context.Background(), origin.URL+"/a"); err != nil || origin.hits.Load() != 1 {
		t.Fatalf("restart: %v, origin hits %d", err, origin.hits.Load())
	}
	if names := cacheFiles(t, dir); len(names) != 1 {
		t.Errorf("stale temp file kept: %v", names)
	}
}

func TestImageCacheDropsDamagedFile(t *testing.T) {
	origin := newCacheOrigin(t, 10)
	dir := t.TempDir()
	im := cachedImages(dir)
	im.Get(context.Background(), origin.URL+"/a")
	os.WriteFile(filepath.Join(dir, cacheKey(origin.URL+"/a")), []byte("garbage"), 0o600)
	if _, _, err := im.Get(context.Background(), origin.URL+"/a"); err != nil || origin.hits.Load() != 2 {
		t.Errorf("damaged file: %v, origin hits %d", err, origin.hits.Load())
	}
}
