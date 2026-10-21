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
	"golang.org/x/net/html/charset"

	"github.com/yangflow/oriel/internal/store"
)

const (
	// maxExtractAttempts stops automatic retries of an article that keeps failing.
	maxExtractAttempts = 6

	// hostSpacing is the least time between two extraction requests to one site.
	hostSpacing = 5 * time.Second

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

// hostGate spaces out the requests to one site.
type hostGate struct {
	mu   sync.Mutex
	next map[string]time.Time
}

// reserve takes the host's turn when it is free at now, and holds the host for hostSpacing.
func (g *hostGate) reserve(host string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if now.Before(g.next[host]) {
		return false
	}
	g.holdLocked(host, now.Add(hostSpacing))
	return true
}

// holdUntil keeps requests to the host back until at.
func (g *hostGate) holdUntil(host string, at time.Time) {
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

// extractAll extracts up to four waiting articles at once, at most one per site in a round.
func (p *Poller) extractAll(ctx context.Context) {
	now := p.now()
	waiting, err := p.store.PendingExtractions(ctx, now, candidates)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot list pending extractions", "error", err)
		}
		return
	}
	var targets []store.ExtractTarget
	for _, target := range waiting {
		if len(targets) == concurrency {
			break
		}
		if u, err := url.Parse(target.URL); err == nil && u.Host != "" && !p.hosts.reserve(u.Host, now) {
			continue
		}
		targets = append(targets, target)
	}
	var wg sync.WaitGroup
	for _, target := range targets {
		wg.Go(func() {
			content, err := p.extract(ctx, target.URL)
			if ctx.Err() != nil {
				return // shutting down: the attempt does not count
			}
			if err != nil {
				slog.Info("Extraction failed", "entry", target.ID, "error", err)
				var failure *extractError
				if u, perr := url.Parse(target.URL); perr == nil && errors.As(err, &failure) && failure.wait > 0 {
					p.hosts.holdUntil(u.Host, now.Add(failure.wait))
				}
				if err := p.store.FailExtraction(ctx, target.ID, retryAt(err, target.Attempts+1, now)); err != nil {
					slog.Error("Cannot save an extraction", "entry", target.ID, "error", err)
				}
				return
			}
			plain := text(content)
			if err := p.store.SaveExtraction(ctx, target.ID, content, plain, entryLanguage(target.Title, plain)); err != nil {
				slog.Error("Cannot save an extraction", "entry", target.ID, "error", err)
			}
		})
	}
	wg.Wait()
}

// extract downloads an article page through the guarded client and returns its main content as HTML.
func (p *Poller) extract(ctx context.Context, address string) (string, error) {
	doc, page, err := p.page(ctx, address)
	if err != nil {
		return "", err
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
// reached after redirects, which relative links resolve against.
func (p *Poller) page(ctx context.Context, address string) (*html.Node, *url.URL, error) {
	page, err := url.Parse(address)
	if err != nil || (page.Scheme != "http" && page.Scheme != "https") {
		return nil, nil, permanent(fmt.Errorf("not a web address: %q", address))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", p.userAgent)
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
