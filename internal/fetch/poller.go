// Package fetch polls due sources and stores their items. It runs inside the server process.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yangflow/oriel/internal/ai"
	"github.com/yangflow/oriel/internal/store"
)

const (
	timeout  = 25 * time.Second
	maxBytes = 8 << 20

	// workers is how many feeds are fetched at once. This is a personal reader: a handful is enough for hundreds of
	// subscriptions, and more would only load the sites harder.
	workers = 5

	// hostLimit is the most requests (feed fetches and article extractions together) in flight to one host, and
	// feedSpacing the least time between the starts of two feed fetches to it. All RSSHub routes share one host.
	hostLimit   = 2
	feedSpacing = time.Second

	// An idle worker looks again after idleWait; the due list is read from the database at most every refillEvery
	// and holds at most dueLimit sources.
	idleWait    = 500 * time.Millisecond
	refillEvery = 2 * time.Second
	dueLimit    = 1000

	// The background loops: extraction rounds, and how often to check whether pruning is due.
	extractEvery = 5 * time.Second
	pruneCheck   = time.Minute

	// After a failure, probe every minute for the first five, then back off up to ten minutes.
	probeDelay   = time.Minute
	flatProbes   = 5
	backoffLimit = 10 * time.Minute

	// A Retry-After on 429 or 503 raises the wait, but never past a day.
	retryAfterLimit = 24 * time.Hour

	// RSSHubSetting is the setting that holds the RSSHub root address, such as http://localhost:1200.
	RSSHubSetting = "rsshub_base_url"
)

var errTooLarge = fmt.Errorf("response is larger than %d MiB", maxBytes>>20)

// Poller fetches due sources, extracts article text and prunes old entries, each in its own loop.
type Poller struct {
	store     *store.Store
	client    *http.Client // feeds: addresses the user entered
	guarded   *http.Client // pages: addresses found inside feeds
	userAgent string
	now       func() time.Time
	spacing   time.Duration // between two feed fetches to one host
	hosts     hostGate      // limits and spaces out requests to one site, for feeds and extraction
	lastPrune time.Time     // only touched by prune, which runs in one loop

	mu       sync.Mutex // guards the three fields below
	queue    []queued   // due sources not yet taken, oldest first
	loaded   time.Time  // when the queue was last read from the database
	inflight map[int64]bool
}

// queued is a due source with the host its fetch goes to, empty when that is not known.
type queued struct {
	src  store.FeedSource
	host string
}

// New returns a poller that identifies itself as Oriel/version.
func New(st *store.Store, version string) *Poller {
	return &Poller{
		store:     st,
		client:    &http.Client{Timeout: timeout},
		guarded:   guardedClient(func(a netip.AddrPort) bool { return blocked(a.Addr()) }),
		userAgent: "Oriel/" + version,
		now:       time.Now,
		spacing:   feedSpacing,
		inflight:  map[int64]bool{},
	}
}

// Run fetches, extracts and prunes until ctx is canceled, starting right away. Fetching is a pool of workers that
// keep taking due sources; extraction and pruning have their own loops, so none of the three waits for another.
func (p *Poller) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() { p.work(ctx, true) })
	}
	wg.Go(func() { every(ctx, extractEvery, p.extractAll) })
	wg.Go(func() { every(ctx, pruneCheck, p.prune) })
	wg.Wait()
}

// every calls f now and then on every interval until ctx is canceled.
func every(ctx context.Context, interval time.Duration, f func(context.Context)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		f(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// fetchDue fetches every due source that the host limits allow right now, with the pool of workers, and returns
// when none is left to take. It returns how many new entries the sources brought. Run does the same without
// stopping.
func (p *Poller) fetchDue(ctx context.Context) int {
	p.mu.Lock()
	p.queue, p.loaded = nil, time.Time{}
	p.mu.Unlock()
	var total atomic.Int64
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() { total.Add(int64(p.work(ctx, false))) })
	}
	wg.Wait()
	return int(total.Load())
}

// work takes due sources one after another and fetches them. When none can be taken it returns, or with idle set
// waits a moment and looks again until ctx is canceled. It returns how many new entries it brought.
func (p *Poller) work(ctx context.Context, idle bool) int {
	total := 0
	for ctx.Err() == nil {
		job, ok := p.take(ctx)
		if !ok {
			if !idle {
				return total
			}
			select {
			case <-ctx.Done():
			case <-time.After(idleWait):
			}
			continue
		}
		total += p.fetchSource(ctx, job)
		p.finish(job)
	}
	return total
}

// take claims the oldest due source whose host has room, or reports that there is none. Sources whose host is busy
// or held back stay in the queue, so one crowded host does not stop the others.
func (p *Poller) take(ctx context.Context) (queued, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if p.loaded.IsZero() || now.Sub(p.loaded) >= refillEvery {
		p.refill(ctx, now)
	}
	for i, job := range p.queue {
		if !p.hosts.acquire(job.host, now, p.spacing) {
			continue
		}
		p.queue = append(p.queue[:i], p.queue[i+1:]...)
		p.inflight[job.src.ID] = true
		return job, true
	}
	return queued{}, false
}

// refill reads the due sources again, leaving out those being fetched. The caller holds p.mu.
func (p *Poller) refill(ctx context.Context, now time.Time) {
	p.loaded = now
	sources, err := p.store.DueSources(ctx, now, dueLimit+len(p.inflight))
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot list due sources", "error", err)
		}
		return
	}
	root, err := p.store.Setting(ctx, RSSHubSetting)
	if err != nil && ctx.Err() == nil {
		slog.Error("Cannot read the RSSHub address", "error", err)
	}
	p.queue = p.queue[:0]
	for _, src := range sources {
		if p.inflight[src.ID] {
			continue
		}
		var host string
		if u, err := url.Parse(sourceAddress(src, root)); err == nil {
			host = u.Host
		}
		p.queue = append(p.queue, queued{src: src, host: host})
	}
}

// finish gives back the host's place and the source's turn, after its outcome is recorded.
func (p *Poller) finish(job queued) {
	p.hosts.release(job.host)
	p.mu.Lock()
	delete(p.inflight, job.src.ID)
	p.mu.Unlock()
}

// sourceAddress is where a source is fetched from, given the RSSHub root. It is empty for an RSSHub source
// without a root.
func sourceAddress(src store.FeedSource, root string) string {
	if src.RSSHubPath == "" {
		return src.URL
	}
	if root == "" {
		return ""
	}
	return strings.TrimRight(root, "/") + src.RSSHubPath
}

// fetchSource fetches one source and records the outcome on it.
func (p *Poller) fetchSource(ctx context.Context, job queued) int {
	src := job.src
	created, err := p.fetchAndSave(ctx, src)
	if ctx.Err() != nil {
		return 0 // shutting down: leave the source due for the next start
	}
	now := p.now()
	if err != nil {
		wait := retryDelay(src.ConsecutiveFailures + 1)
		var asked *retryAfterError
		if errors.As(err, &asked) {
			wait = max(wait, asked.wait)
			// The site asked for quiet: its other sources wait too, and no other site does.
			p.hosts.holdUntil(job.host, now.Add(asked.wait))
		}
		if err := p.store.FetchFailure(ctx, src.ID, err.Error(), now, now.Add(wait)); err != nil {
			slog.Error("Cannot record a fetch failure", "source", src.ID, "error", err)
		}
		return 0
	}
	return created
}

func (p *Poller) fetchAndSave(ctx context.Context, src store.FeedSource) (int, error) {
	address := src.URL
	if src.RSSHubPath != "" {
		root, err := p.store.Setting(ctx, RSSHubSetting)
		if err != nil {
			return 0, err
		}
		if address = sourceAddress(src, root); address == "" {
			return 0, errors.New("the RSSHub address is not set")
		}
	}
	base, err := url.Parse(address)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", p.userAgent)
	if src.ETag != "" {
		req.Header.Set("If-None-Match", src.ETag)
	}
	if src.LastModified != "" {
		req.Header.Set("If-Modified-Since", src.LastModified)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	etag, lastModified := resp.Header.Get("ETag"), resp.Header.Get("Last-Modified")
	now := p.now()
	next := now.Add(src.FetchInterval)
	switch {
	case resp.StatusCode == http.StatusNotModified:
		return 0, p.store.FetchSuccess(ctx, src.ID, etag, lastModified, now, next)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		err := statusError(resp, src)
		if wait := retryAfter(resp, now); wait > 0 {
			err = &retryAfterError{error: err, wait: wait}
		}
		return 0, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return 0, err
	}
	if len(body) > maxBytes {
		return 0, errTooLarge
	}
	feed, err := parse(body, base)
	if err != nil {
		return 0, fmt.Errorf("not a feed: %w", err)
	}
	var expired time.Time
	if keep := p.retention(ctx); keep > 0 {
		expired = now.Add(-keep)
	}
	var target string
	if len(src.AITasks) > 0 {
		cfg, err := ai.Load(ctx, p.store)
		if err != nil {
			return 0, err
		}
		if !cfg.Configured() {
			src.AITasks = nil
		}
		target = cfg.Language
	}
	created, err := p.store.SaveFeed(ctx, src, feed.title, feed.siteURL, feed.entries, target, expired, now)
	if err != nil {
		return 0, err
	}
	if src.IconURL == "" {
		if err := p.fillIcon(ctx, src, feed); err != nil {
			return 0, err
		}
	}
	return created, p.store.FetchSuccess(ctx, src.ID, etag, lastModified, now, next)
}

// statusError names the likely cause for the statuses users can act on.
func statusError(resp *http.Response, src store.FeedSource) error {
	server := strings.ToLower(resp.Header.Get("Server"))
	if (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) &&
		(strings.EqualFold(resp.Header.Get("Cf-Mitigated"), "challenge") || strings.Contains(server, "cloudflare")) {
		return fmt.Errorf("status %d: blocked by Cloudflare", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound && src.RSSHubPath != "" {
		return errors.New("status 404: this RSSHub route no longer exists")
	}
	return fmt.Errorf("status %d", resp.StatusCode)
}

// retryAfterError is a failure after which the server asked for a wait.
type retryAfterError struct {
	error
	wait time.Duration
}

func (e *retryAfterError) Unwrap() error { return e.error }

// retryAfter reads the Retry-After of a 429 or 503, given as seconds or an HTTP date, capped at a day.
// It returns 0 for other statuses and for values that are missing, invalid or not in the future.
func retryAfter(resp *http.Response, now time.Time) time.Duration {
	if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusServiceUnavailable {
		return 0
	}
	value := strings.TrimSpace(resp.Header.Get("Retry-After"))
	var wait time.Duration
	if seconds, err := strconv.ParseUint(value, 10, 63); err == nil {
		wait = time.Duration(min(seconds, uint64(retryAfterLimit/time.Second))) * time.Second
	} else if at, err := http.ParseTime(value); err == nil {
		wait = min(at.Sub(now), retryAfterLimit)
	}
	return max(wait, 0)
}

// retryDelay is the wait after the given number of consecutive failures.
func retryDelay(failures int) time.Duration {
	if failures <= flatProbes {
		return probeDelay
	}
	delay := probeDelay
	for i := flatProbes; i < failures && delay < backoffLimit; i++ {
		delay *= 2
	}
	return min(delay, backoffLimit)
}
