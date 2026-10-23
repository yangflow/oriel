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
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yangflow/oriel/internal/ai"
	"github.com/yangflow/oriel/internal/diag"
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

	// A source that has failed for longFailure (counted from its last success) waits at least longBackoff between
	// tries, and so does one that answers 410 Gone.
	longFailure = 3 * 24 * time.Hour
	longBackoff = 6 * time.Hour

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

// ErrorBlocked is the stable reason code of a fetch that a site's bot protection turned away (a Cloudflare
// challenge). It is stored as the source's last_error_kind and sent as lastErrorKind; last_error keeps the text.
const ErrorBlocked = "blocked"

// blockedError is a failure that the site's bot protection caused. Retrying with other headers does not help and
// Oriel does not try to get around it.
type blockedError struct{ error }

func (e *blockedError) Unwrap() error { return e.error }

// ErrorGone is the stable reason code of a feed that answered 410 Gone: the site says it was removed for good.
const ErrorGone = "gone"

// goneError is the failure of a 410 answer. The source is not paused for it, only suggested to be.
type goneError struct{ error }

func (e *goneError) Unwrap() error { return e.error }

// errorKind is the stable code of the reason behind err, empty when it has none.
func errorKind(err error) string {
	var blocked *blockedError
	if errors.As(err, &blocked) {
		return ErrorBlocked
	}
	var gone *goneError
	if errors.As(err, &gone) {
		return ErrorGone
	}
	return ""
}

// errFetchPanic is the failure recorded on a source whose fetch panicked; the panic itself is in the log.
var errFetchPanic = errors.New("internal error while fetching")

// Poller fetches due sources, extracts article text and prunes old entries, each in its own loop.
type Poller struct {
	store     *store.Store
	client    *http.Client // feeds: addresses the user entered
	guarded   *http.Client // pages: addresses found inside feeds
	network   *Network     // the proxy to use; nil connects directly
	userAgent string
	now       func() time.Time
	spacing   time.Duration // between two feed fetches to one host
	hosts     hostGate      // limits and spaces out requests to one site, for feeds and extraction
	rsshub    instanceGate  // the RSSHub instances that are cooling down
	lastPrune time.Time     // only touched by prune, which runs in one loop
	vacuuming func() bool   // whether a vacuum is running; no new fetch starts while it is
	readHook  func()        // tests only: called before a refill reads the database

	mu         sync.Mutex    // guards the fields below; never held across a database call
	queue      []queued      // due sources not yet taken, oldest first
	loaded     time.Time     // when the queue was last read from the database
	refilling  chan struct{} // open while a refill is reading the database, closed when it is done
	generation int           // counts resets of the queue, so a refill that began before one is dropped
	inflight   map[int64]bool
	settled    map[int64]bool // sources whose fetch ended while a refill was reading, which that read may still list as due

	setMu  sync.Mutex // guards the cached RSSHub setting; never held across a database call
	setRaw string     // the RSSHub setting as last read
	setAt  time.Time  // when it was read, zero when nothing is cached
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
		vacuuming: st.Vacuuming,
	}
}

// UseNetwork makes the poller connect through the proxy settings n reads. Without it every request is direct.
func (p *Poller) UseNetwork(n *Network) { p.network = n }

// Run fetches, extracts and prunes until ctx is canceled, starting right away. Fetching is a pool of workers that
// keep taking due sources; extraction and pruning have their own loops, so none of the three waits for another.
func (p *Poller) Run(ctx context.Context) {
	// A queue left from an earlier run may name sources that no longer exist, for example after a restore.
	p.resetQueue()
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() { diag.Supervise(ctx, "fetch worker", func() { p.work(ctx, true) }) })
	}
	wg.Go(func() { every(ctx, extractEvery, p.extractAll) })
	wg.Go(func() { every(ctx, pruneCheck, p.prune) })
	wg.Go(func() { every(ctx, websubEvery, p.websubRound) })
	wg.Wait()
}

// resetQueue drops the queue and the cached RSSHub setting, so the next take reads both again. A refill that is
// under way is dropped when it finishes.
func (p *Poller) resetQueue() {
	p.mu.Lock()
	p.queue, p.loaded = nil, time.Time{}
	p.generation++
	p.endRefill()
	p.mu.Unlock()
	p.setMu.Lock()
	p.setAt = time.Time{}
	p.setMu.Unlock()
}

// endRefill releases the workers waiting for a refill. The caller holds p.mu.
func (p *Poller) endRefill() {
	if p.refilling != nil {
		close(p.refilling)
		p.refilling = nil
	}
}

// every calls f now and then on every interval until ctx is canceled.
func every(ctx context.Context, interval time.Duration, f func(context.Context)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		diag.Guard("fetch loop", func() { f(ctx) })
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
	p.resetQueue()
	var total atomic.Int64
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			diag.Guard("fetch worker", func() { total.Add(int64(p.work(ctx, false))) })
		})
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
		total += p.fetchGuarded(ctx, job)
	}
	return total
}

// fetchGuarded fetches one source and releases it. A panic is logged with its stack and costs only this fetch; it
// counts as a failed one, so a source that always panics is tried on the retry schedule and not again at once.
func (p *Poller) fetchGuarded(ctx context.Context, job queued) (added int) {
	defer p.finish(job)
	if diag.Guard("fetch source", func() { added = p.fetchSource(ctx, job) }) {
		added = 0
		now := p.now()
		wait := retryDelay(job.src.ConsecutiveFailures + 1)
		if err := p.store.FetchFailure(ctx, job.src.ID, errFetchPanic.Error(), "", now, now.Add(wait)); err != nil && ctx.Err() == nil {
			slog.Error("Cannot record a fetch failure", "source", job.src.ID, "error", err)
		}
	}
	return added
}

// take claims the oldest due source whose host has room, or reports that there is none. Sources whose host is busy
// or held back stay in the queue, so one crowded host does not stop the others. The database is read without p.mu:
// while one worker refills the queue, the others go on with the queue they have.
func (p *Poller) take(ctx context.Context) (queued, bool) {
	if p.vacuuming() {
		return queued{}, false // a vacuum holds the write lock; a fetch started now would only wait and fail
	}
	now := p.now()
	p.mu.Lock()
	if p.refilling != nil && len(p.queue) == 0 {
		// Another worker is reading the due list and there is nothing else to take: wait for it, without p.mu.
		done := p.refilling
		p.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return queued{}, false
		}
		p.mu.Lock()
	}
	if p.refilling == nil && (p.loaded.IsZero() || now.Sub(p.loaded) >= refillEvery) {
		p.refilling = make(chan struct{})
		p.loaded = now
		p.settled = nil
		gen, busy := p.generation, len(p.inflight)
		p.mu.Unlock()
		sources, root, ok := p.readDue(ctx, now, busy)
		p.mu.Lock()
		if p.generation == gen {
			if ok {
				p.fill(sources, root)
			}
			p.endRefill()
		}
	}
	defer p.mu.Unlock()
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

// readDue reads the due sources and the first RSSHub root from the database. The caller does not hold p.mu. busy is
// how many sources were being fetched, which the query leaves room for.
func (p *Poller) readDue(ctx context.Context, now time.Time, busy int) ([]store.FeedSource, string, bool) {
	if p.readHook != nil {
		p.readHook()
	}
	sources, err := p.store.DueSources(ctx, now, dueLimit+busy)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot list due sources", "error", err)
		}
		return nil, "", false
	}
	raw, err := p.rsshubSetting(ctx, now)
	if err != nil && ctx.Err() == nil {
		slog.Error("Cannot read the RSSHub address", "error", err)
	}
	return sources, p.rsshub.first(Instances(raw), now), true
}

// fill replaces the queue with the sources not being fetched now. The caller holds p.mu.
func (p *Poller) fill(sources []store.FeedSource, root string) {
	p.queue = p.queue[:0]
	for _, src := range sources {
		if p.inflight[src.ID] || p.settled[src.ID] {
			continue
		}
		var host string
		if u, err := url.Parse(sourceAddress(src, root)); err == nil {
			host = u.Host
		}
		p.queue = append(p.queue, queued{src: src, host: host})
	}
}

// rsshubSetting returns the RSSHub setting, read from the database at most once every refillEvery and shared by the
// refill and the fetches, so a changed setting is picked up within that time. A failed read is not kept.
func (p *Poller) rsshubSetting(ctx context.Context, now time.Time) (string, error) {
	p.setMu.Lock()
	raw, at := p.setRaw, p.setAt
	p.setMu.Unlock()
	if !at.IsZero() && now.Sub(at) < refillEvery {
		return raw, nil
	}
	raw, err := p.store.Setting(ctx, RSSHubSetting)
	if err != nil {
		return "", err
	}
	p.setMu.Lock()
	p.setRaw, p.setAt = raw, now
	p.setMu.Unlock()
	return raw, nil
}

// Fetching returns the ids of the sources being fetched now, in order.
func (p *Poller) Fetching() []int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := make([]int64, 0, len(p.inflight))
	for id := range p.inflight {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// statusKey carries the place where fetchFrom leaves the HTTP status it received, for the fetch log.
type statusKey struct{}

// finish gives back the host's place and the source's turn, after its outcome is recorded.
func (p *Poller) finish(job queued) {
	p.hosts.release(job.host)
	p.mu.Lock()
	delete(p.inflight, job.src.ID)
	if p.refilling != nil {
		if p.settled == nil {
			p.settled = map[int64]bool{}
		}
		p.settled[job.src.ID] = true
	}
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
	started := p.now()
	var status int
	created, err := p.fetchAndSave(context.WithValue(ctx, statusKey{}, &status), src, job.host)
	if ctx.Err() != nil || errors.Is(err, errDeferred) {
		return 0 // shutting down, or nothing could be tried: leave the source due
	}
	now := p.now()
	attempt := store.FetchAttempt{SourceID: src.ID, At: now, Duration: max(now.Sub(started), 0), OK: err == nil, Status: status, NewEntries: created}
	if err != nil {
		attempt.Error = err.Error()
	}
	if logErr := p.store.LogFetch(ctx, attempt); logErr != nil && ctx.Err() == nil {
		slog.Error("Cannot log a fetch", "source", src.ID, "error", logErr)
	}
	if err != nil {
		wait := failureDelay(src, err, now)
		var asked *retryAfterError
		if errors.As(err, &asked) {
			wait = max(wait, asked.wait)
			// The site asked for quiet: its other sources wait too, and no other site does.
			p.hosts.holdUntil(job.host, now.Add(asked.wait))
		}
		if err := p.store.FetchFailure(ctx, src.ID, err.Error(), errorKind(err), now, now.Add(wait)); err != nil {
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
	raw, err := p.rsshubSetting(ctx, p.now())
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
	var blocked bool
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
		blocked = blocked || errorKind(err) == ErrorBlocked
		until := p.rsshub.fail(base, now, err.Error(), wait)
		p.hosts.holdUntil(host, until)
		tried++
		reasons = append(reasons, label(base)+": "+err.Error())
		soonest = shortest(soonest, until.Sub(now))
	}
	if tried == 0 || busy {
		return 0, errDeferred
	}
	var err error = fmt.Errorf("%s", strings.Join(reasons, "; "))
	if blocked {
		err = &blockedError{err}
	}
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
	client, err := p.network.Feed(ctx, src.Proxy, p.client)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, unavailable(ctx, instance, err)
	}
	defer resp.Body.Close()
	if status, ok := ctx.Value(statusKey{}).(*int); ok {
		*status = resp.StatusCode
	}
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
	feed, err := parse(body, resp.Header.Get("Content-Type"), base)
	if err != nil {
		return 0, fmt.Errorf("not a feed: %w", err)
	}
	if err := p.noteWebSub(ctx, src, resp.Header, body, base); err != nil {
		return 0, err
	}
	var expired time.Time
	if keep := keepFor(src, p.retention(ctx)); keep > 0 {
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
		src.AIFetchLimit = cfg.AutoFetchLimit
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

// cloudflareChallenge reports whether a 403, 429 or 503 is Cloudflare's challenge to an automated client: marked
// with cf-mitigated, or sent by Cloudflare as an HTML page where a feed was asked for.
func cloudflareChallenge(resp *http.Response) bool {
	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable:
	default:
		return false
	}
	if strings.EqualFold(resp.Header.Get("Cf-Mitigated"), "challenge") {
		return true
	}
	return strings.Contains(strings.ToLower(resp.Header.Get("Server")), "cloudflare") &&
		strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html")
}

// statusError names the likely cause for the statuses users can act on.
func statusError(resp *http.Response, src store.FeedSource) error {
	if cloudflareChallenge(resp) {
		return &blockedError{fmt.Errorf("status %d: blocked by Cloudflare", resp.StatusCode)}
	}
	if resp.StatusCode == http.StatusNotFound && src.RSSHubPath != "" {
		return errors.New("status 404: this RSSHub route no longer exists")
	}
	if resp.StatusCode == http.StatusGone {
		return &goneError{errors.New("status 410: the feed was removed (Gone)")}
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

// failureDelay is the wait after src failed with err once more: the back-off for its failure count, raised to
// longBackoff for a source that answered 410, or that is past its flat probes and has had no success for longFailure.
// The failure count guards against a source that was only paused or offline for days and fails once on return.
func failureDelay(src store.FeedSource, err error, now time.Time) time.Duration {
	wait := retryDelay(src.ConsecutiveFailures + 1)
	if errorKind(err) == ErrorGone || (src.ConsecutiveFailures >= flatProbes && !src.FailingSince.IsZero() && now.Sub(src.FailingSince) >= longFailure) {
		wait = max(wait, longBackoff)
	}
	return wait
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
