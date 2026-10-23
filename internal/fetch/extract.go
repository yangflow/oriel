package fetch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"

	"github.com/yangflow/oriel/internal/diag"
	"github.com/yangflow/oriel/internal/store"
)

const (
	// maxExtractAttempts stops automatic retries of an article that keeps failing.
	maxExtractAttempts = 6

	// hostSpacing is the least time between two extraction requests to one site.
	hostSpacing = 5 * time.Second

	// extractBatch is how many articles one round extracts at once.
	extractBatch = 4

	// candidates is how many waiting articles one round looks at, so that sites held back by hostSpacing
	// do not keep the others from running.
	candidates = 50
)

// extractDelays is the wait after the first, second, ... failed attempt at a temporary error.
var extractDelays = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

// extractError is a failed extraction. A temporary one (the site is down or busy) is tried again later; a permanent
// one (the page is gone, is not HTML or has no article) is not.
type extractError struct {
	error
	temporary bool
	wait      time.Duration // what the server asked for with Retry-After
}

func (e *extractError) Unwrap() error { return e.error }

func permanent(err error) error { return &extractError{error: err} }

// retryAt is when to try again after the attempt-th failed attempt, or the zero time when to give up. A Retry-After
// longer than the schedule's wait wins.
func retryAt(err error, attempt int, now time.Time) time.Time {
	var failure *extractError
	if !errors.As(err, &failure) || !failure.temporary || attempt >= maxExtractAttempts {
		return time.Time{}
	}
	return now.Add(max(extractDelays[min(attempt, len(extractDelays))-1], failure.wait))
}

// hostGate limits and spaces out the requests to one site. Feed fetches and article extractions share it, so a
// site never sees more than hostLimit requests at once from either.
type hostGate struct {
	mu     sync.Mutex
	next   map[string]time.Time // the earliest start of the next request
	active map[string]int       // requests in flight
}

// acquire takes a place at the host when one is free and the host is not held until later, and holds the host for
// spacing from now. Every acquire that succeeded must be released. An empty host is never limited.
func (g *hostGate) acquire(host string, now time.Time, spacing time.Duration) bool {
	if host == "" {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if now.Before(g.next[host]) || g.active[host] >= hostLimit {
		return false
	}
	if g.active == nil {
		g.active = map[string]int{}
	}
	g.active[host]++
	g.holdLocked(host, now.Add(spacing))
	return true
}

// release gives back a place taken by acquire.
func (g *hostGate) release(host string) {
	if host == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active[host]--; g.active[host] <= 0 {
		delete(g.active, host)
	}
}

// holdUntil keeps requests to the host back until at.
func (g *hostGate) holdUntil(host string, at time.Time) {
	if host == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.holdLocked(host, at)
}

func (g *hostGate) holdLocked(host string, at time.Time) {
	if g.next == nil {
		g.next = map[string]time.Time{}
	}
	if at.After(g.next[host]) {
		g.next[host] = at
	}
}

// extractAll keeps up to extractBatch articles being extracted, and holds each site to the hostGate rule: requests at
// least hostSpacing apart and at most hostLimit at once, counted together with the feed fetches. A finished page frees
// its place at once for the next waiting article, so one slow page never holds back the others. It returns when
// nothing is running and nothing more can start right now. An article gets one attempt per call.
func (p *Poller) extractAll(ctx context.Context) {
	var wg sync.WaitGroup
	defer wg.Wait()
	done := make(chan struct{}, extractBatch)
	tried := map[int64]bool{}
	running := 0
	for {
		if running < extractBatch {
			running += p.startExtractions(ctx, extractBatch-running, tried, &wg, done)
		}
		if running == 0 {
			return
		}
		select {
		case <-done:
			running--
		case <-ctx.Done():
			return
		}
	}
}

// startExtractions starts up to limit extractions of waiting articles that are not in tried and returns how many it
// started. Each one sends on done when it ends.
func (p *Poller) startExtractions(ctx context.Context, limit int, tried map[int64]bool, wg *sync.WaitGroup, done chan<- struct{}) int {
	if ctx.Err() != nil {
		return 0
	}
	now := p.now()
	waiting, err := p.store.PendingExtractions(ctx, now, candidates)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot list pending extractions", "error", err)
		}
		return 0
	}
	started := 0
	for _, target := range waiting {
		if started == limit {
			break
		}
		if tried[target.ID] {
			continue
		}
		address, err := targetAddress(target)
		if err != nil {
			// The source's own rules gave no address to ask for, and another try would give the same.
			tried[target.ID] = true
			slog.Info("Extraction failed", "entry", target.ID, "error", err)
			if err := p.store.FailExtraction(ctx, target.ID, time.Time{}); err != nil {
				slog.Error("Cannot save an extraction", "entry", target.ID, "error", err)
			}
			continue
		}
		var host string
		if u, err := url.Parse(address); err == nil {
			host = u.Host
		}
		if !p.hosts.acquire(host, now, hostSpacing) {
			continue
		}
		tried[target.ID] = true
		started++
		wg.Go(func() {
			defer func() { done <- struct{}{} }()
			defer p.hosts.release(host)
			defer diag.Recover("extract article")
			p.extractOne(ctx, target, address, host)
		})
	}
	return started
}

// extractOne extracts one article and saves the result. Hold and retry times count from the moment of the failure,
// not from when the attempt started.
func (p *Poller) extractOne(ctx context.Context, target store.ExtractTarget, address, host string) {
	content, err := p.extract(ctx, address, target.Selector, target.Referer)
	if ctx.Err() != nil {
		return // shutting down: the attempt does not count
	}
	if err != nil {
		slog.Info("Extraction failed", "entry", target.ID, "error", err)
		failed := p.now()
		var failure *extractError
		if errors.As(err, &failure) && failure.wait > 0 {
			p.hosts.holdUntil(host, failed.Add(failure.wait))
		}
		if err := p.store.FailExtraction(ctx, target.ID, retryAt(err, target.Attempts+1, failed)); err != nil {
			slog.Error("Cannot save an extraction", "entry", target.ID, "error", err)
		}
		return
	}
	plain := text(content)
	if err := p.store.SaveExtraction(ctx, target.ID, content, plain, entryLanguage(target.Title, plain)); err != nil {
		slog.Error("Cannot save an extraction", "entry", target.ID, "error", err)
	}
}

// targetAddress is the page to request for an entry: its link after the rewrite rules of its source.
func targetAddress(target store.ExtractTarget) (string, error) {
	rules, err := parseRewrites(target.Rewrites)
	if err != nil {
		return "", err
	}
	return rewriteLink(target.URL, rules)
}

// extract downloads an article page through the guarded client and returns its main content as HTML. A selector, when
// the page has a readable element for it, names the content; otherwise the page is extracted automatically. A
// referer, when not empty, is sent with the request.
func (p *Poller) extract(ctx context.Context, address, selector, referer string) (string, error) {
	doc, page, err := p.page(ctx, address, referer)
	if err != nil {
		return "", err
	}
	if selector != "" {
		if content := selectedHTML(doc, page, selector); content != "" {
			return content, nil
		}
		slog.Info("Content selector found nothing readable, extracting automatically", "address", address)
	}
	content, err := articleHTML(doc, page)
	if err != nil {
		return "", permanent(err)
	}
	if strings.TrimSpace(text(content)) == "" {
		return "", permanent(errors.New("no readable content"))
	}
	return content, nil
}

// page downloads a web page through the guarded client and parses it. The returned address is the one
// reached after redirects, which relative links resolve against. A referer, when not empty, is the only header sent
// besides the User-Agent.
func (p *Poller) page(ctx context.Context, address, referer string) (*html.Node, *url.URL, error) {
	page, err := url.Parse(address)
	if err != nil || (page.Scheme != "http" && page.Scheme != "https") {
		return nil, nil, permanent(fmt.Errorf("not a web address: %q", address))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", p.userAgent)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := p.guarded.Do(req)
	if err != nil {
		// A blocked address will not open up by waiting; a network failure may pass.
		return nil, nil, &extractError{error: err, temporary: !errors.Is(err, ErrBlockedAddress)}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, &extractError{
			error:     fmt.Errorf("status %d", resp.StatusCode),
			temporary: temporaryStatus(resp.StatusCode),
			wait:      retryAfter(resp, p.now()),
		}
	}
	if kind, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err == nil &&
		kind != "text/html" && kind != "application/xhtml+xml" {
		return nil, nil, permanent(fmt.Errorf("not a web page: %s", kind))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, nil, &extractError{error: err, temporary: true}
	}
	if len(body) > maxBytes {
		return nil, nil, permanent(errTooLarge)
	}
	decoded, err := charset.NewReader(bytes.NewReader(body), resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, nil, permanent(err)
	}
	doc, err := html.Parse(decoded)
	if err != nil {
		return nil, nil, permanent(err)
	}
	return doc, resp.Request.URL, nil
}

// temporaryStatus reports whether a site may answer differently later: it is busy, throttling or failing.
func temporaryStatus(code int) bool {
	return code == http.StatusRequestTimeout || code == http.StatusTooEarly || code == http.StatusTooManyRequests || code >= 500
}

// articleHTML picks the post a Discourse link points to, and runs Readability on any other page.
func articleHTML(doc *html.Node, page *url.URL) (string, error) {
	if post := discoursePost(doc, page); post != "" {
		return post, nil
	}
	article, err := readability.FromDocument(doc, page)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := article.RenderHTML(&b); err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
}

// selectedHTML returns the inside of the first element of the page that the selector matches, with the relative
// addresses in it resolved against the page and scripts and styles dropped. It is empty when nothing matches or the
// element has no readable text.
func selectedHTML(doc *html.Node, page *url.URL, selector string) string {
	compiled, err := cascadia.Parse(selector)
	if err != nil {
		return ""
	}
	element := cascadia.Query(doc, compiled)
	if element == nil {
		return ""
	}
	resolveLinks(element, page)
	var b strings.Builder
	for child := element.FirstChild; child != nil; child = child.NextSibling {
		html.Render(&b, child)
	}
	content := strings.TrimSpace(b.String())
	if text(content) == "" {
		return ""
	}
	return content
}

// resolveLinks makes the addresses under n absolute and removes the elements that only carry code.
func resolveLinks(n *html.Node, page *url.URL) {
	for child := n.FirstChild; child != nil; {
		next := child.NextSibling
		switch child.DataAtom {
		case atom.Script, atom.Style, atom.Noscript, atom.Template:
			n.RemoveChild(child)
		default:
			resolveLinks(child, page)
		}
		child = next
	}
	if n.Type != html.ElementNode {
		return
	}
	for i, a := range n.Attr {
		if a.Key != "href" && a.Key != "src" {
			continue
		}
		if ref, err := url.Parse(strings.TrimSpace(a.Val)); err == nil {
			n.Attr[i].Val = page.ResolveReference(ref).String()
		}
	}
}

var (
	discourseGenerator = cascadia.MustCompile(`meta[name="generator"]`)
	discourseFirstPost = cascadia.MustCompile(`.topic-body.crawler-post`)
	discourseText      = cascadia.MustCompile(`.post[itemprop="text"]`)
	discourseHashPost  = regexp.MustCompile(`^post_(\d+)$`)
)

// discoursePost returns one post of a Discourse topic page, whose reply stream Readability would
// keep whole. Links name the post as #post_N or /t/<slug>/<topic>/<N>; a bare topic means the first.
func discoursePost(doc *html.Node, page *url.URL) string {
	generator := cascadia.Query(doc, discourseGenerator)
	if generator == nil || !strings.HasPrefix(attr(generator, "content"), "Discourse") || cascadia.Query(doc, discourseFirstPost) == nil {
		return ""
	}
	number := 1
	if m := discourseHashPost.FindStringSubmatch(page.Fragment); m != nil {
		number, _ = strconv.Atoi(m[1])
	} else if segments := strings.Split(strings.Trim(page.Path, "/"), "/"); len(segments) >= 4 && segments[len(segments)-4] == "t" {
		if n, err := strconv.Atoi(segments[len(segments)-1]); err == nil {
			number = n
		}
	}
	post := cascadia.Query(doc, cascadia.MustCompile(fmt.Sprintf("#post_%d", number)))
	if post == nil {
		post = cascadia.Query(doc, discourseFirstPost)
	}
	body := cascadia.Query(post, discourseText)
	if body == nil {
		return ""
	}
	var b strings.Builder
	for child := body.FirstChild; child != nil; child = child.NextSibling {
		html.Render(&b, child)
	}
	return withoutQuotes(b.String())
}
