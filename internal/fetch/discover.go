package fetch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"

	"github.com/yangflow/oriel/internal/store"
)

// discoverTimeout bounds one discovery, which may fetch a page and probe the common paths.
const discoverTimeout = 20 * time.Second

var (
	// ErrInvalidAddress reports an address that is not an http or https address with a host.
	ErrInvalidAddress = errors.New("not a web address")
	// ErrPageUnavailable reports a page that could not be fetched and no feed was found elsewhere on the site.
	ErrPageUnavailable = errors.New("the page could not be fetched")
	// ErrNotFeed reports an address that answered with something other than a feed.
	ErrNotFeed = errors.New("the address is not a feed")
)

// previewTitles is how many of the latest titles a preview of a feed carries.
const previewTitles = 3

// FeedPreview is what a feed address holds, read without subscribing. UpdatedAt is the newest entry's date in Unix
// milliseconds and PerDay the entries a day the dates in the feed work out to; each is nil when the feed cannot tell.
type FeedPreview struct {
	URL       string         `json:"url"`
	Title     string         `json:"title"`
	Format    string         `json:"format"`
	UpdatedAt *int64         `json:"updatedAt"`
	PerDay    *float64       `json:"perDay"`
	Entries   []PreviewEntry `json:"entries"`
}

// commonPaths are tried on the site's root when a page declares no feed.
var commonPaths = []string{"/feed", "/rss", "/atom.xml", "/index.xml", "/feed.json"}

// alternateTypes are the media types of <link rel="alternate"> that name a feed.
var alternateTypes = map[string]bool{
	"application/rss+xml": true, "application/atom+xml": true, "application/feed+json": true,
}

// Discoverer finds the feeds of a web address. It fetches pages like an entry link: through the guarded client,
// with the same timeout and size limits.
type Discoverer struct {
	client    *http.Client
	network   *Network // nil connects directly
	userAgent string
}

// UseNetwork makes requests go through the global proxy n reads.
func (d *Discoverer) UseNetwork(n *Network) { d.network = n }

// NewDiscoverer returns a discoverer that identifies itself as Oriel/version.
func NewDiscoverer(version string) *Discoverer {
	return &Discoverer{
		client:    guardedClient(func(a netip.AddrPort) bool { return blocked(a.Addr()) }),
		userAgent: "Oriel/" + version,
	}
}

// Discover returns the feeds an address leads to, best first, none when it leads to no feed. The built-in rules are
// tried first and cost no request; then the page itself, its declared alternates and the common paths. It returns
// ErrInvalidAddress, ErrBlockedAddress when the address is not a public one, or ErrPageUnavailable.
func (d *Discoverer) Discover(ctx context.Context, address string) ([]Candidate, error) {
	found, err := d.discover(ctx, address)
	for i := range found {
		if found[i].Kind == "" && ForumFeed(found[i].URL, found[i].RSSHubPath) {
			found[i].Kind = "forum"
		}
	}
	return found, err
}

// Preview reads the feed at address without storing anything. It returns ErrInvalidAddress, ErrBlockedAddress when
// the address is not a public one, ErrPageUnavailable or ErrNotFeed.
func (d *Discoverer) Preview(ctx context.Context, address string) (FeedPreview, error) {
	address = strings.TrimSpace(address)
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return FeedPreview{}, ErrInvalidAddress
	}
	ctx, cancel := context.WithTimeout(ctx, discoverTimeout)
	defer cancel()
	body, kind, final, err := d.get(ctx, address)
	if errors.Is(err, ErrBlockedAddress) {
		return FeedPreview{}, err
	}
	if err != nil {
		return FeedPreview{}, fmt.Errorf("%w: %v", ErrPageUnavailable, err)
	}
	feed, err := parse(body, kind, final)
	if err != nil {
		return FeedPreview{}, fmt.Errorf("%w: %v", ErrNotFeed, err)
	}
	return previewOf(feed, final.String()), nil
}

// previewOf reduces a parsed feed to its preview: its latest titled entries, newest first when every entry is dated
// and in the feed's own order otherwise, and the pace its dates work out to. A burst shorter than a day counts as a day.
func previewOf(feed parsed, address string) FeedPreview {
	out := FeedPreview{URL: address, Title: feed.title, Format: feed.format, Entries: []PreviewEntry{}}
	entries := slices.Clone(feed.entries)
	var newest, oldest time.Time
	dated := 0
	for _, e := range entries {
		if e.PublishedAt.IsZero() {
			continue
		}
		dated++
		if newest.IsZero() || e.PublishedAt.After(newest) {
			newest = e.PublishedAt
		}
		if oldest.IsZero() || e.PublishedAt.Before(oldest) {
			oldest = e.PublishedAt
		}
	}
	if dated == len(entries) {
		slices.SortStableFunc(entries, func(a, b store.FeedEntry) int { return b.PublishedAt.Compare(a.PublishedAt) })
	}
	if dated > 0 {
		ms := newest.UnixMilli()
		out.UpdatedAt = &ms
	}
	if span := newest.Sub(oldest); dated >= 2 && span > 0 {
		perDay := float64(dated-1) / max(span.Hours()/24, 1)
		out.PerDay = &perDay
	}
	for _, e := range entries {
		if e.Title == "" {
			continue
		}
		entry := PreviewEntry{Title: e.Title, URL: e.URL}
		if !e.PublishedAt.IsZero() {
			ms := e.PublishedAt.UnixMilli()
			entry.PublishedAt = &ms
		}
		out.Entries = append(out.Entries, entry)
		if len(out.Entries) == previewTitles {
			break
		}
	}
	return out
}

func (d *Discoverer) discover(ctx context.Context, address string) ([]Candidate, error) {
	address = strings.TrimSpace(address)
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, ErrInvalidAddress
	}
	if found := matchRules(u); len(found) > 0 {
		return found, nil
	}
	ctx, cancel := context.WithTimeout(ctx, discoverTimeout)
	defer cancel()

	body, kind, page, err := d.get(ctx, address)
	if errors.Is(err, ErrBlockedAddress) {
		return nil, err
	}
	if err == nil {
		if feed, perr := parse(body, kind, page); perr == nil {
			return []Candidate{{URL: page.String(), Title: feed.title, Origin: "feed"}}, nil
		}
		if doc, derr := parseHTML(body); derr == nil {
			if found := alternates(doc, page); len(found) > 0 {
				return found, nil
			}
		}
	}
	if err != nil && ctx.Err() != nil {
		return nil, ErrPageUnavailable
	}
	root := *u
	if err == nil {
		root = *page
	}
	root.Path, root.RawQuery, root.Fragment = "", "", ""
	var found []Candidate
	seen := map[string]bool{}
	for _, path := range commonPaths {
		probe := root
		probe.Path = path
		body, kind, final, perr := d.get(ctx, probe.String())
		if perr != nil {
			if ctx.Err() != nil {
				break
			}
			continue
		}
		feed, perr := parse(body, kind, final)
		if perr != nil || seen[final.String()] {
			continue
		}
		seen[final.String()] = true
		found = append(found, Candidate{URL: final.String(), Title: feed.title, Origin: "common"})
	}
	if len(found) == 0 && err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPageUnavailable, err)
	}
	return found, nil
}

// get downloads an address through the guarded client and returns the body with the address reached after
// redirects. The body is limited to maxBytes.
func (d *Discoverer) get(ctx context.Context, address string) ([]byte, string, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, "", nil, err
	}
	req.Header.Set("User-Agent", d.userAgent)
	req.Header.Set("Accept", "text/html, application/xhtml+xml, application/rss+xml, application/atom+xml, application/feed+json, application/xml;q=0.9, */*;q=0.8")
	client, err := d.network.Guarded(ctx, store.SourceProxy{}, d.client)
	if err != nil {
		return nil, "", nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", nil, err
	}
	if len(body) > maxBytes {
		return nil, "", nil, errTooLarge
	}
	return body, resp.Header.Get("Content-Type"), resp.Request.URL, nil
}

func parseHTML(body []byte) (*html.Node, error) {
	decoded, err := charset.NewReader(bytes.NewReader(body), "text/html")
	if err != nil {
		return nil, err
	}
	return html.Parse(decoded)
}

// alternates returns the feeds a page declares with <link rel="alternate">, in page order, once each.
func alternates(doc *html.Node, page *url.URL) []Candidate {
	var found []Candidate
	seen := map[string]bool{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Link {
			rel := " " + strings.ToLower(strings.Join(strings.Fields(attr(n, "rel")), " ")) + " "
			kind := strings.ToLower(strings.TrimSpace(attr(n, "type")))
			if address := absolute(page, attr(n, "href")); strings.Contains(rel, " alternate ") &&
				alternateTypes[kind] && address != "" && !seen[address] {
				seen[address] = true
				found = append(found, Candidate{URL: address, Title: strings.TrimSpace(attr(n, "title")), Origin: "alternate"})
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return found
}
