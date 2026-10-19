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
	"strings"
	"sync"
	"time"

	"github.com/yangflow/oriel/internal/ai"
	"github.com/yangflow/oriel/internal/store"
)

const (
	tick        = 15 * time.Second
	concurrency = 4
	timeout     = 25 * time.Second
	maxBytes    = 8 << 20

	// After a failure, probe every minute for the first five, then back off up to ten minutes.
	probeDelay   = time.Minute
	flatProbes   = 5
	backoffLimit = 10 * time.Minute

	// RSSHubSetting is the setting that holds the RSSHub root address, such as http://localhost:1200.
	RSSHubSetting = "rsshub_base_url"
)

var errTooLarge = fmt.Errorf("response is larger than %d MiB", maxBytes>>20)

// Poller fetches due sources and extracts article text on a fixed tick.
type Poller struct {
	store     *store.Store
	client    *http.Client // feeds: addresses the user entered
	guarded   *http.Client // pages: addresses found inside feeds
	userAgent string
	now       func() time.Time
	lastPrune time.Time // only touched by Poll, which never runs concurrently
}

// New returns a poller that identifies itself as Oriel/version.
func New(st *store.Store, version string) *Poller {
	return &Poller{
		store:     st,
		client:    &http.Client{Timeout: timeout},
		guarded:   guardedClient(func(a netip.AddrPort) bool { return blocked(a.Addr()) }),
		userAgent: "Oriel/" + version,
		now:       time.Now,
	}
}

// Run polls until ctx is canceled, starting right away.
func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		p.Poll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Poll fetches up to four due sources at once, extracts up to four articles and prunes expired entries.
// It returns how many new entries the sources brought.
func (p *Poller) Poll(ctx context.Context) int {
	created := p.fetchDue(ctx)
	p.extractAll(ctx)
	p.prune(ctx)
	return created
}

func (p *Poller) fetchDue(ctx context.Context) int {
	sources, err := p.store.DueSources(ctx, p.now(), concurrency)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot list due sources", "error", err)
		}
		return 0
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for _, src := range sources {
		wg.Go(func() {
			n := p.fetchSource(ctx, src)
			mu.Lock()
			total += n
			mu.Unlock()
		})
	}
	wg.Wait()
	return total
}

// fetchSource fetches one source and records the outcome on it.
func (p *Poller) fetchSource(ctx context.Context, src store.FeedSource) int {
	created, err := p.fetchAndSave(ctx, src)
	if ctx.Err() != nil {
		return 0 // shutting down: leave the source due for the next start
	}
	now := p.now()
	if err != nil {
		if err := p.store.FetchFailure(ctx, src.ID, err.Error(), now, now.Add(retryDelay(src.ConsecutiveFailures+1))); err != nil {
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
		if root == "" {
			return 0, errors.New("the RSSHub address is not set")
		}
		address = strings.TrimRight(root, "/") + src.RSSHubPath
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
		return 0, statusError(resp, src)
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
	if len(src.AITasks) > 0 {
		configured, err := ai.Configured(ctx, p.store)
		if err != nil {
			return 0, err
		}
		if !configured {
			src.AITasks = nil
		}
	}
	created, err := p.store.SaveFeed(ctx, src, feed.title, feed.siteURL, feed.entries, expired, now)
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
