package fetch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// maxExtractAttempts stops automatic retries of an article that keeps failing.
const maxExtractAttempts = 3

// extractAll extracts up to four waiting articles at once.
func (p *Poller) extractAll(ctx context.Context) {
	targets, err := p.store.PendingExtractions(ctx, concurrency, maxExtractAttempts)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot list pending extractions", "error", err)
		}
		return
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
				content = ""
			}
			if err := p.store.SaveExtraction(ctx, target.ID, content, text(content)); err != nil {
				slog.Error("Cannot save an extraction", "entry", target.ID, "error", err)
			}
		})
	}
	wg.Wait()
}

// extract downloads an article page through the guarded client and returns its main content as HTML.
func (p *Poller) extract(ctx context.Context, address string) (string, error) {
	page, err := url.Parse(address)
	if err != nil || (page.Scheme != "http" && page.Scheme != "https") {
		return "", fmt.Errorf("not a web address: %q", address)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", p.userAgent)
	resp, err := p.guarded.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxBytes {
		return "", errTooLarge
	}
	decoded, err := charset.NewReader(bytes.NewReader(body), resp.Header.Get("Content-Type"))
	if err != nil {
		return "", err
	}
	doc, err := html.Parse(decoded)
	if err != nil {
		return "", err
	}
	// Redirects change the page address that relative links resolve against.
	content, err := articleHTML(doc, resp.Request.URL)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text(content)) == "" {
		return "", fmt.Errorf("no readable content")
	}
	return content, nil
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
