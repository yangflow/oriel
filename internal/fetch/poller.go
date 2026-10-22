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

	// RSSHubSetting is the setting that holds the RSSHub root addresses, one per line, such as http://localhost:1200.
	// The first is the primary, the others are tried in order when it fails.
	RSSHubSetting = "rsshub_base_url"

	// MaxInstances is how many RSSHub addresses the setting may hold.
	MaxInstances = 5
)

var errTooLarge = fmt.Errorf("response is larger than %d MiB", maxBytes>>20)

// errDeferred is the outcome of a fetch that could not be tried at all now: every RSSHub instance is cooling down
// or its host is busy. It is not a failure of the source, which stays due.
var errDeferred = errors.New("no RSSHub instance is available now")

// Instances lists the RSSHub root addresses in a setting value in priority order: one per line, blank lines and
// trailing slashes dropped, a repeated address kept once at its first place.
func Instances(raw string) []string {
	var list []string
	seen := map[string]bool{}
	for line := range strings.SplitSeq(raw, "\n") {
		base := strings.TrimRight(strings.TrimSpace(line), "/")
		if base == "" || seen[strings.ToLower(base)] {
			continue
		}
		seen[strings.ToLower(base)] = true
		list = append(list, base)
	}
	return list
}

// instanceGate remembers which RSSHub instances are cooling down after a failure. Backoff belongs to the instance,
// not to the sources: while one is cooling, every route goes to the next instance without waiting for a timeout.
type instanceGate struct {
	mu    sync.Mutex
	state map[string]instanceState
}

type instanceState struct {
	failures int       // in a row
	until    time.Time // cooling down until
	reason   string    // the last failure
}

// cooling reports whether the instance is cooling down at now, until when and why.
func (g *instanceGate) cooling(base string, now time.Time) (time.Time, string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.state[base]
	return st.until, st.reason, now.Before(st.until)
}

// fail starts or extends the cool-down of an instance: the wait of a source with that many failures in a row, or
// the Retry-After the instance asked for when that is longer. It returns when the cool-down ends.
func (g *instanceGate) fail(base string, now time.Time, reason string, asked time.Duration) time.Time {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state == nil {
		g.state = map[string]instanceState{}
	}
	st := g.state[base]
	st.failures++
	st.until = now.Add(max(retryDelay(st.failures), asked))
	st.reason = reason
	g.state[base] = st
	return st.until
}

// ok forgets the failures of an instance that answered.
func (g *instanceGate) ok(base string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.state, base)
}

// first is the instance a route is tried on first: the earliest in the list that is not cooling down, or when all
// are, the one whose cool-down ends first.
func (g *instanceGate) first(instances []string, now time.Time) string {
	var soonest string
	var end time.Time
	for _, base := range instances {
		until, _, cooling := g.cooling(base, now)
		if !cooling {
			return base
		}
		if soonest == "" || until.Before(end) {
			soonest, end = base, until
		}
	}
	return soonest
}

// label names an instance in a failure reason: its address without the scheme.
func label(base string) string {
	if u, err := url.Parse(base); err == nil && u.Host != "" {
		return u.Host + u.Path
	}
	return base
}

// unavailableError is a failure of the instance rather than of the route: no answer, a 5xx or a 429. The same
// route may work on another instance.
type unavailableError struct{ error }

func (e *unavailableError) Unwrap() error { return e.error }

// Poller fetches due sources, extracts article text and prunes old entries, each in its own loop.
type Poller struct {
	store     *store.Store
	client    *http.Client // feeds: addresses the user entered
	guarded   *http.Client // pages: addresses found inside feeds
	userAgent string
	now       func() time.Time
	spacing   time.Duration // between two feed fetches to one host
	hosts     hostGate      // limits and spaces out requests to one site, for feeds and extraction
	rsshub    instanceGate  // the RSSHub instances that are cooling down
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
	wg.Go(func() { every(ctx, websubEvery, p.websubRound) })
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
	raw, err := p.store.Setting(ctx, RSSHubSetting)
	if err != nil && ctx.Err() == nil {
		slog.Error("Cannot read the RSSHub address", "error", err)
	}
	instances := Instances(raw)
	root := p.rsshub.first(instances, now)
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
	created, err := p.fetchAndSave(ctx, src, job.host)
	if ctx.Err() != nil || errors.Is(err, errDeferred) {
		return 0 // shutting down, or nothing could be tried: leave the source due
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

// fetchAndSave fetches a source and stores what it brought. host is the host whose place the caller holds.
func (p *Poller) fetchAndSave(ctx context.Context, src store.FeedSource, host string) (int, error) {
	if src.RSSHubPath == "" {
		return p.fetchFrom(ctx, src, src.URL, "")
	}
	raw, err := p.store.Setting(ctx, RSSHubSetting)
	if err != nil {
		return 0, err
	}
	instances := Instances(raw)
	if len(instances) == 0 {
		return 0, errors.New("the RSSHub address is not set")
	}
	return p.fetchRoute(ctx, src, instances, host)
}

// fetchRoute fetches an RSSHub route from the instances in order and stops at the first that answers. An instance
// that does not answer, or answers 5xx or 429, goes into a cool-down and the route goes on to the next; any other
// failure belongs to the route and ends the attempt. A fallback instance on another host takes its own place at
// that host, and is skipped when the host has none free. The source fails only when every instance did; when some
// could not be tried (cooling down or busy) and none answered, nothing is recorded and the source stays due.
// With one instance there is no one to fall back to, so it has no cool-down and its errors pass through as they are.
// held is the host whose place the caller holds already.
func (p *Poller) fetchRoute(ctx context.Context, src store.FeedSource, instances []string, held string) (int, error) {
	if len(instances) == 1 {
		return p.fetchFrom(ctx, src, sourceAddress(src, instances[0]), instances[0])
	}
	var reasons []string
	var soonest time.Duration // until the first failed instance can be tried again
	tried, busy := 0, false
	for _, base := range instances {
		now := p.now()
		if until, reason, cooling := p.rsshub.cooling(base, now); cooling {
			reasons = append(reasons, label(base)+": "+reason)
			soonest = shortest(soonest, until.Sub(now))
			continue
		}
		address := sourceAddress(src, base)
		var host string
		if u, err := url.Parse(address); err == nil {
			host = u.Host
		}
		owned := host != "" && host == held
		if owned {
			held = "" // the place is used once; a second instance on the same host takes its own
		} else if !p.hosts.acquire(host, now, p.spacing) {
			busy = true
			continue
		}
		created, err := p.fetchFrom(ctx, src, address, base)
		if !owned {
			p.hosts.release(host)
		}
		var unavailable *unavailableError
		switch {
		case err == nil:
			p.rsshub.ok(base)
			return created, nil
		case ctx.Err() != nil:
			return 0, err
		case !errors.As(err, &unavailable):
			p.rsshub.ok(base) // it answered: the route is what failed
			return 0, fmt.Errorf("%s: %w", label(base), err)
		}
		var asked *retryAfterError
		var wait time.Duration
		if errors.As(err, &asked) {
			wait = asked.wait
		}
		until := p.rsshub.fail(base, now, err.Error(), wait)
		p.hosts.holdUntil(host, until)
		tried++
		reasons = append(reasons, label(base)+": "+err.Error())
		soonest = shortest(soonest, until.Sub(now))
	}
	if tried == 0 || busy {
		return 0, errDeferred
	}
	err := fmt.Errorf("%s", strings.Join(reasons, "; "))
	return 0, &retryAfterError{error: err, wait: soonest}
}

// shortest is the smaller of two waits, where zero means none yet.
func shortest(a, b time.Duration) time.Duration {
	if a == 0 {
		return b
	}
	return min(a, b)
}

// fetchFrom fetches the feed at address and stores it. instance is the RSSHub instance it belongs to, empty for a
// source that is not a route.
func (p *Poller) fetchFrom(ctx context.Context, src store.FeedSource, address, instance string) (int, error) {
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
		return 0, unavailable(ctx, instance, err)
	}
	defer resp.Body.Close()
	etag, lastModified := resp.Header.Get("ETag"), resp.Header.Get("Last-Modified")
	now := p.now()
	switch {
	case resp.StatusCode == http.StatusNotModified:
		return 0, p.recordSuccess(ctx, src, etag, lastModified, instance, now)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		err := statusError(resp, src)
		if wait := retryAfter(resp, now); wait > 0 {
			err = &retryAfterError{error: err, wait: wait}
		}
		if instance != "" && (resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests) {
			err = &unavailableError{err}
		}
		return 0, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return 0, unavailable(ctx, instance, err)
	}
	if len(body) > maxBytes {
		return 0, errTooLarge
	}
	feed, err := parse(body, base)
	if err != nil {
		return 0, fmt.Errorf("not a feed: %w", err)
	}
	if err := p.noteWebSub(ctx, src, resp.Header, body, base); err != nil {
		return 0, err
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
	if src.FirstSuccess {
		if err := p.store.SuggestKind(ctx, src.ID, SuggestKind(feed.entries, src.Kind)); err != nil {
			return 0, err
		}
	}
	return created, p.recordSuccess(ctx, src, etag, lastModified, instance, now)
}

// recordSuccess records a fetch that reached the feed and schedules the next one. An automatic source first gets its
// interval worked out again from the entries it holds now.
func (p *Poller) recordSuccess(ctx context.Context, src store.FeedSource, etag, lastModified, instance string, now time.Time) error {
	interval, stored := src.FetchInterval, time.Duration(0)
	if src.FetchIntervalAuto {
		published, err := p.store.RecentPublished(ctx, src.ID, now, AutoSample)
		if err != nil {
			return err
		}
		interval = AutoInterval(published)
		stored = interval
	}
	next, err := p.nextFetch(ctx, src, interval, now)
	if err != nil {
		return err
	}
	return p.store.FetchSuccess(ctx, src.ID, etag, lastModified, instance, now, next, stored)
}

// unavailable marks a network failure of an RSSHub instance, so that the route goes on to the next one. A canceled
// context and a failure of a source that is not a route stay as they are.
func unavailable(ctx context.Context, instance string, err error) error {
	if instance == "" || ctx.Err() != nil {
		return err
	}
	return &unavailableError{err}
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
