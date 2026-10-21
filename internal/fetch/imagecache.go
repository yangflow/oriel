package fetch

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	imageCacheMaxBytes = 256 << 20
	imageCacheTTL      = 30 * 24 * time.Hour
	// imageCacheTrim is the share of the cap an eviction pass shrinks the cache to, so the next writes do not
	// each trigger another pass.
	imageCacheTrim = 0.9
	tempSuffix     = ".tmp"
)

// imageCache keeps proxied pictures on disk, one file per address named by the SHA-256 of the address. A file
// holds the content type, the time it was stored and the bytes. Evicting the least recently used file keeps the
// total under the cap; a file older than the TTL counts as a miss. Deleting the directory is always safe.
type imageCache struct {
	dir      string
	maxBytes int64
	ttl      time.Duration
	now      func() time.Time

	mu      sync.Mutex
	scanned bool
	entries map[string]cacheEntry
	total   int64
}

type cacheEntry struct {
	size   int64
	access time.Time
}

func newImageCache(dir string) *imageCache {
	return &imageCache{dir: dir, maxBytes: imageCacheMaxBytes, ttl: imageCacheTTL, now: time.Now}
}

func cacheKey(address string) string {
	sum := sha256.Sum256([]byte(address))
	return hex.EncodeToString(sum[:])
}

// scan reads the directory once, on first use, to learn what earlier runs left. Temp files from a crashed
// write are removed. The caller holds mu.
func (c *imageCache) scan() {
	if c.scanned {
		return
	}
	c.scanned = true
	c.entries = map[string]cacheEntry{}
	list, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	for _, item := range list {
		name := item.Name()
		if item.IsDir() {
			continue
		}
		if strings.HasSuffix(name, tempSuffix) {
			os.Remove(filepath.Join(c.dir, name))
			continue
		}
		info, err := item.Info()
		if err != nil {
			continue
		}
		c.entries[name] = cacheEntry{size: info.Size(), access: info.ModTime()}
		c.total += info.Size()
	}
}

// drop forgets a file and removes it. The caller holds mu.
func (c *imageCache) drop(key string) {
	if e, ok := c.entries[key]; ok {
		c.total -= e.size
		delete(c.entries, key)
	}
	os.Remove(filepath.Join(c.dir, key))
}

// get returns the stored picture for address, or false on a miss, an expired file or a file it cannot read.
func (c *imageCache) get(address string) (string, []byte, bool) {
	key := cacheKey(address)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scan()
	if _, ok := c.entries[key]; !ok {
		return "", nil, false
	}
	path := filepath.Join(c.dir, key)
	raw, err := os.ReadFile(path)
	kind, stored, body, ok := decodeCacheFile(raw)
	if err != nil || !ok || c.now().Sub(stored) > c.ttl {
		c.drop(key)
		return "", nil, false
	}
	now := c.now()
	os.Chtimes(path, now, now)
	c.entries[key] = cacheEntry{size: int64(len(raw)), access: now}
	return kind, body, true
}

// put stores a picture through a temp file in the same directory and a rename, so a reader never sees a half
// file. A failure to store is logged and otherwise ignored: the cache is only an optimization.
func (c *imageCache) put(address, kind string, body []byte) {
	key := cacheKey(address)
	now := c.now()
	data := encodeCacheFile(kind, now, body)
	if int64(len(data)) > c.maxBytes {
		return
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		slog.Warn("Image cache unavailable", "error", err)
		return
	}
	tmp, err := os.CreateTemp(c.dir, "put-*"+tempSuffix)
	if err != nil {
		slog.Warn("Image cache unavailable", "error", err)
		return
	}
	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chtimes(tmp.Name(), now, now)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		c.scan()
		err = os.Rename(tmp.Name(), filepath.Join(c.dir, key))
	}
	if err != nil {
		os.Remove(tmp.Name())
		slog.Warn("Image cache write failed", "error", err)
		return
	}
	if old, ok := c.entries[key]; ok {
		c.total -= old.size
	}
	c.entries[key] = cacheEntry{size: int64(len(data)), access: now}
	c.total += int64(len(data))
	c.evict(key)
}

// evict deletes the least recently used files until the total is under the trimmed cap, once the cap is
// passed. The file just written is kept. The caller holds mu.
func (c *imageCache) evict(keep string) {
	if c.total <= c.maxBytes {
		return
	}
	keys := make([]string, 0, len(c.entries))
	for key := range c.entries {
		if key != keep {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return c.entries[keys[i]].access.Before(c.entries[keys[j]].access) })
	target := int64(float64(c.maxBytes) * imageCacheTrim)
	for _, key := range keys {
		if c.total <= target {
			return
		}
		c.drop(key)
	}
}

// A cache file is the content type, a line feed, the store time in Unix nanoseconds, a line feed and the bytes.
func encodeCacheFile(kind string, stored time.Time, body []byte) []byte {
	head := kind + "\n" + strconv.FormatInt(stored.UnixNano(), 10) + "\n"
	return append([]byte(head), body...)
}

func decodeCacheFile(raw []byte) (kind string, stored time.Time, body []byte, ok bool) {
	kindEnd := bytes.IndexByte(raw, '\n')
	if kindEnd < 0 {
		return "", time.Time{}, nil, false
	}
	rest := raw[kindEnd+1:]
	timeEnd := bytes.IndexByte(rest, '\n')
	if timeEnd < 0 {
		return "", time.Time{}, nil, false
	}
	nanos, err := strconv.ParseInt(string(rest[:timeEnd]), 10, 64)
	if err != nil {
		return "", time.Time{}, nil, false
	}
	kind = string(raw[:kindEnd])
	if !imageTypes[kind] {
		return "", time.Time{}, nil, false
	}
	return kind, time.Unix(0, nanos), rest[timeEnd+1:], true
}
