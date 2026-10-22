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
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"
)

// discoverTimeout bounds one discovery, which may fetch a page and probe the common paths.
const discoverTimeout = 20 * time.Second

var (
	// ErrInvalidAddress reports an address that is not an http or https address with a host.
	ErrInvalidAddress = errors.New("not a web address")
	// ErrPageUnavailable reports a page that could not be fetched and no feed was found elsewhere on the site.
	ErrPageUnavailable = errors.New("the page could not be fetched")
)

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
	userAgent string
}

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

	body, page, err := d.get(ctx, address)
	if errors.Is(err, ErrBlockedAddress) {
		return nil, err
	}
	if err == nil {
		if feed, perr := parse(body, page); perr == nil {
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
		body, final, perr := d.get(ctx, probe.String())
		if perr != nil {
			if ctx.Err() != nil {
				break
			}
			continue
		}
		feed, perr := parse(body, final)
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
func (d *Discoverer) get(ctx context.Context, address string) ([]byte, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", d.userAgent)
	req.Header.Set("Accept", "text/html, application/xhtml+xml, application/rss+xml, application/atom+xml, application/feed+json, application/xml;q=0.9, */*;q=0.8")
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if len(body) > maxBytes {
		return nil, nil, errTooLarge
	}
	return body, resp.Request.URL, nil
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
