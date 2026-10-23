package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

func TestImageCacheStats(t *testing.T) {
	origin := newCacheOrigin(t, 10)
	dir := filepath.Join(t.TempDir(), "image-cache")
	im := cachedImages(dir)
	ctx := context.Background()
	if n, size := im.CacheStats(); n != 0 || size != 0 {
		t.Fatalf("empty: %d, %d", n, size)
	}
	for _, name := range []string{"/a", "/b", "/a"} {
		if _, _, err := im.Get(ctx, origin.URL+name); err != nil {
			t.Fatal(err)
		}
	}
	var onDisk int64
	for _, name := range cacheFiles(t, dir) {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		onDisk += info.Size()
	}
	if n, size := im.CacheStats(); n != 2 || size != onDisk || size == 0 {
		t.Fatalf("stats: %d, %d (on disk %d)", n, size, onDisk)
	}
	if n, size := NewImages("test", "").CacheStats(); n != 0 || size != 0 {
		t.Fatalf("without a cache: %d, %d", n, size)
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
	im.cache.bg.Wait()
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

// Reads happen outside the lock: a read stuck on one key does not hold up a lookup of another.
func TestImageCacheSlowReadDoesNotBlockOtherKeys(t *testing.T) {
	dir := t.TempDir()
	c := newImageCache(dir)
	c.put("a", "image/png", pngHeader)
	c.put("b", "image/png", pngHeader)

	reading := make(chan struct{})
	release := make(chan struct{})
	var first atomic.Bool
	c.beforeDisk = func(op string) {
		if op == "read" && first.CompareAndSwap(false, true) {
			close(reading)
			<-release
		}
	}
	slow := make(chan bool)
	go func() {
		_, _, ok := c.get("a")
		slow <- ok
	}()
	<-reading
	other := make(chan bool)
	go func() {
		_, _, ok := c.get("b")
		other <- ok
	}()
	select {
	case ok := <-other:
		if !ok {
			t.Error("lookup of the other key missed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a lookup waited for a read of another key")
	}
	c.put("c", "image/png", pngHeader) // a put does not wait either
	close(release)
	if !<-slow {
		t.Error("slow read missed")
	}
}

// A hit changes the recency in memory only: the file is not touched.
func TestImageCacheHitKeepsFileTime(t *testing.T) {
	dir := t.TempDir()
	c := newImageCache(dir)
	c.put("a", "image/png", pngHeader)
	path := filepath.Join(dir, cacheKey("a"))
	old := time.Now().Add(-time.Hour)
	os.Chtimes(path, old, old)
	c.now = func() time.Time { return time.Now() }
	if _, _, ok := c.get("a"); !ok {
		t.Fatal("miss")
	}
	info, err := os.Stat(path)
	if err != nil || info.ModTime().Sub(old).Abs() > time.Second {
		t.Errorf("file time changed: %v, %v", info.ModTime(), err)
	}
}

// Eviction runs in the background: while it is stuck on its first removal, gets and puts go through.
func TestImageCacheEvictionDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	c := newImageCache(dir)
	now := time.Now()
	var tick atomic.Int64
	c.now = func() time.Time { return now.Add(time.Duration(tick.Add(1)) * time.Second) }
	body := make([]byte, 1000)
	c.maxBytes = 3500
	for _, name := range []string{"a", "b", "c"} {
		c.put(name, "image/png", body)
	}

	removing := make(chan struct{})
	release := make(chan struct{})
	var first atomic.Bool
	c.beforeDisk = func(op string) {
		if op == "remove" && first.CompareAndSwap(false, true) {
			close(removing)
			<-release
		}
	}
	finished := make(chan struct{})
	go func() {
		c.put("d", "image/png", body) // over the cap: starts a pass
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("put waited for eviction")
	}
	<-removing
	done := make(chan struct{})
	go func() {
		c.get("d")
		c.put("e", "image/png", body[:10])
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("get or put waited for a running eviction")
	}
	close(release)
	c.bg.Wait()
	if c.total > int64(float64(c.maxBytes)*imageCacheTrim) {
		t.Errorf("total %d after the pass", c.total)
	}
	for _, name := range []string{"d", "e"} {
		if _, _, ok := c.get(name); !ok {
			t.Errorf("%s was evicted", name)
		}
	}
	if len(cacheFiles(t, dir)) != len(c.entries) {
		t.Errorf("files %d, entries %d", len(cacheFiles(t, dir)), len(c.entries))
	}
}

// Gets, puts and eviction running together never surface an error: every get is a clean miss or a whole hit,
// and the index matches the directory once the passes are done.
func TestImageCacheConcurrentUse(t *testing.T) {
	dir := t.TempDir()
	c := newImageCache(dir)
	c.maxBytes = 5000
	body := make([]byte, 500)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				addr := strconv.Itoa((w*7 + i) % 20)
				if i%3 == 0 {
					c.put(addr, "image/png", body)
				} else if kind, got, ok := c.get(addr); ok && (kind != "image/png" || len(got) != len(body)) {
					t.Errorf("partial hit: %q, %d bytes", kind, len(got))
				}
			}
		}(w)
	}
	wg.Wait()
	c.bg.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
	var sum int64
	for _, e := range c.entries {
		sum += e.size
	}
	if sum != c.total || len(cacheFiles(t, dir)) != len(c.entries) {
		t.Errorf("index %d bytes, total %d, files %d, entries %d", sum, c.total, len(cacheFiles(t, dir)), len(c.entries))
	}
	if c.total > c.maxBytes {
		t.Errorf("total %d over the cap", c.total)
	}
}

// A file that vanished behind the cache's back is a miss and leaves the accounting at zero.
func TestImageCacheVanishedFileIsMiss(t *testing.T) {
	dir := t.TempDir()
	c := newImageCache(dir)
	c.put("a", "image/png", pngHeader)
	os.RemoveAll(dir)
	if _, _, ok := c.get("a"); ok {
		t.Error("hit on a deleted file")
	}
	if n, b := c.stats(); n != 0 || b != 0 {
		t.Errorf("stats %d, %d", n, b)
	}
	c.put("a", "image/png", pngHeader) // the directory is made again
	if _, _, ok := c.get("a"); !ok {
		t.Error("miss after the directory was recreated")
	}
}

// A late discard of an old copy leaves the copy a put stored in between, and a get that opens the file after
// the replace reads the whole new copy.
func TestImageCachePutDuringReadKeepsNewCopy(t *testing.T) {
	dir := t.TempDir()
	c := newImageCache(dir)
	c.put("a", "image/png", pngHeader)
	c.mu.Lock()
	oldSeq := c.entries[cacheKey("a")].seq
	c.mu.Unlock()
	c.put("a", "image/png", pngHeader) // replaces the copy a reader may still hold
	c.discard(cacheKey("a"), oldSeq)
	if _, _, ok := c.get("a"); !ok {
		t.Error("the new copy was discarded by a late discard")
	}
	if n, b := c.stats(); n != 1 || b == 0 {
		t.Errorf("stats %d, %d", n, b)
	}
}
