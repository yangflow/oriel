package fetch

import (
	"context"
	"log/slog"
	"net/url"
	"path"
	"strings"

	"github.com/yangflow/oriel/internal/store"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// fillIcon gives a source without an icon the feed's own image, or else the best icon its homepage links to.
// A homepage is searched once per site address, found or not; a failed search never fails the fetch.
func (p *Poller) fillIcon(ctx context.Context, src store.FeedSource, feed parsed) error {
	site := src.SiteURL
	if site == "" {
		site = feed.siteURL
	}
	icon := feed.iconURL
	if icon == "" {
		if site == "" || site == src.IconCheckedSite {
			return nil
		}
		doc, page, err := p.page(ctx, site)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.Info("No site icon", "source", src.ID, "error", err)
		} else {
			icon = siteIcon(doc, page)
		}
	}
	return p.store.SetSourceIcon(ctx, src.ID, icon, site)
}

// siteIcon picks the icon a homepage links to: an apple-touch-icon first, then a PNG icon, then any other
// raster icon. SVG and ICO are skipped because the image proxy does not serve them.
func siteIcon(doc *html.Node, page *url.URL) string {
	best, bestRank := "", 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Link {
			rel := " " + strings.ToLower(strings.Join(strings.Fields(attr(n, "rel")), " ")) + " "
			kind := strings.ToLower(strings.TrimSpace(attr(n, "type")))
			icon := absolute(page, attr(n, "href"))
			rank := 0
			switch {
			case !usableIcon(icon, kind):
			case strings.Contains(rel, " apple-touch-icon ") || strings.Contains(rel, " apple-touch-icon-precomposed "):
				rank = 3
			case !strings.Contains(rel, " icon "):
			case kind == "image/png" || strings.HasSuffix(strings.ToLower(iconPath(icon)), ".png"):
				rank = 2
			default:
				rank = 1
			}
			if rank > bestRank {
				best, bestRank = icon, rank
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return best
}

// usableIcon reports whether an icon address, with its declared media type if any, is one the image proxy
// can show: a web address that is not SVG or ICO.
func usableIcon(address, kind string) bool {
	if address == "" {
		return false
	}
	switch kind {
	case "image/svg+xml", "image/x-icon", "image/vnd.microsoft.icon":
		return false
	}
	ext := strings.ToLower(path.Ext(iconPath(address)))
	return ext != ".svg" && ext != ".svgz" && ext != ".ico"
}

func iconPath(address string) string {
	u, err := url.Parse(address)
	if err != nil {
		return ""
	}
	return u.Path
}
