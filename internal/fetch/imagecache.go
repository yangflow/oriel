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
// total under the cap, by recency kept in memory (after a restart, by the time each file was stored); a file
// older than the TTL counts as a miss. Deleting the directory is always safe.
//
// mu guards only the index. File reads, the sort of an eviction pass and the pass as a whole stay outside it;
// each unlink is the one piece of disk work done under it, so it cannot race a store of the same key.
type imageCache struct {
	dir      string
	maxBytes int64
	ttl      time.Duration
	now      func() time.Time

	mu      sync.Mutex
	scanned bool
	entries map[string]cacheEntry
	total   int64
	seq     uint64

	evicting bool           // an eviction pass is running or queued
	bg       sync.WaitGroup // eviction passes, so tests can wait for them

	// beforeDisk, set only by tests, runs before a file read and before each eviction unlink, outside mu.
	beforeDisk func(op string)
}

type cacheEntry struct {
	size   int64
	access time.Time
	seq    uint64 // which stored copy this is, so a late discard never removes a newer one
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

// stats returns how many pictures the cache holds and the bytes they take.
func (c *imageCache) stats() (count int, bytes int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scan()
	return len(c.entries), c.total
}

// drop forgets an index entry. The caller holds mu.
func (c *imageCache) drop(key string) {
	if e, ok := c.entries[key]; ok {
		c.total -= e.size
		delete(c.entries, key)
	}
}

// remove deletes the file of an index entry and forgets the entry. A file that is already gone counts as
// removed; one that cannot be deleted (Windows refuses while a reader has it open) stays in the index, so the
// accounting still matches the disk. It reports whether the entry is gone. The caller holds mu.
func (c *imageCache) remove(key string) bool {
	err := os.Remove(filepath.Join(c.dir, key))
	if err != nil && !os.IsNotExist(err) {
		return false
	}
	c.drop(key)
	return true
}

// disk runs the test hook, if any, just before a piece of disk work that must happen outside mu.
func (c *imageCache) disk(op string) {
	if c.beforeDisk != nil {
		c.beforeDisk(op)
	}
}

// get returns the stored picture for address, or false on a miss, an expired file or a file it cannot read.
// mu covers only the index: the file is read outside it, and recency is kept in memory.
func (c *imageCache) get(address string) (string, []byte, bool) {
	key := cacheKey(address)
	c.mu.Lock()
	c.scan()
	entry, ok := c.entries[key]
	c.mu.Unlock()
	if !ok {
		return "", nil, false
	}
	path := filepath.Join(c.dir, key)
	c.disk("read")
	raw, err := os.ReadFile(path)
	kind, stored, body, valid := decodeCacheFile(raw)
	now := c.now()
	if err != nil || !valid || now.Sub(stored) > c.ttl {
		c.discard(key, entry.seq)
		return "", nil, false
	}
	c.mu.Lock()
	if cur, ok := c.entries[key]; ok && cur.seq == entry.seq {
		cur.access = now
		c.entries[key] = cur
	}
	c.mu.Unlock()
	return kind, body, true
}

// discard removes a file that could not be used, unless a newer copy has replaced the one that was read.
func (c *imageCache) discard(key string, seq uint64) {
	c.mu.Lock()
	cur, ok := c.entries[key]
	if ok && cur.seq != seq {
		c.mu.Unlock()
		return
	}
	c.remove(key)
	c.mu.Unlock()
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
	c.mu.Lock()
	c.scan() // before the temp file exists: the first scan removes leftover temp files
	c.mu.Unlock()
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
	if err == nil {
		err = os.Rename(tmp.Name(), filepath.Join(c.dir, key))
	}
	if err != nil {
		c.mu.Unlock()
		os.Remove(tmp.Name())
		slog.Warn("Image cache write failed", "error", err)
		return
	}
	if old, ok := c.entries[key]; ok {
		c.total -= old.size
	}
	c.seq++
	c.entries[key] = cacheEntry{size: int64(len(data)), access: now, seq: c.seq}
	c.total += int64(len(data))
	if c.total > c.maxBytes && !c.evicting {
		c.evicting = true
		c.bg.Add(1)
		go c.evictPass(key)
	}
	c.mu.Unlock()
}

// evictPass runs in the background once the cap is passed: it deletes the least recently used files until the
// total is under the trimmed cap. The file written by the put that started it is kept. The sort works on a
// snapshot, outside mu; each file is then removed under mu only if it is still the same stored copy, so gets
// and puts wait for one unlink at most.
func (c *imageCache) evictPass(keep string) {
	defer c.bg.Done()
	target := int64(float64(c.maxBytes) * imageCacheTrim)
	for {
		removed := 0
		c.mu.Lock()
		type candidate struct {
			key string
			cacheEntry
		}
		list := make([]candidate, 0, len(c.entries))
		for key, e := range c.entries {
			if key != keep {
				list = append(list, candidate{key, e})
			}
		}
		c.mu.Unlock()
		sort.Slice(list, func(i, j int) bool { return list[i].access.Before(list[j].access) })
		for _, item := range list {
			c.disk("remove")
			c.mu.Lock()
			if c.total <= target {
				c.mu.Unlock()
				break
			}
			if cur, ok := c.entries[item.key]; ok && cur.seq == item.seq {
				if c.remove(item.key) {
					removed++
				}
			}
			c.mu.Unlock()
		}
		c.mu.Lock()
		if c.total <= c.maxBytes || removed == 0 {
			c.evicting = false
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
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
