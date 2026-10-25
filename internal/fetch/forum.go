package fetch

import (
	"net/url"
	"slices"
	"strings"

	"github.com/yangflow/oriel/internal/store"
)

// forumHosts are the community sites known by name, www. and subdomains of them included. A feed of any of them is a
// forum's.
var forumHosts = []string{"v2ex.com", "linux.do"}

// forumRoutes are the first segments of the RSSHub routes of communities.
var forumRoutes = []string{"v2ex", "linuxdo", "discourse"}

// discourseRoots are the first path segments of the feeds a Discourse forum serves at "<page>.rss": /latest.rss,
// /top.rss, /c/<category>/<id>.rss, /tag/<tag>.rss, /t/<topic>/<id>.rss and the like.
var discourseRoots = []string{"latest", "top", "posts", "c", "tag", "tags", "t", "u"}

// ForumFeed reports whether the address of a feed, or the RSSHub route of one, belongs to a community: a known site
// (V2EX, LINUX DO), a Discourse feed by the shape of its path, or an RSSHub route of such a site. See docs/product.md
// "形态建议".
func ForumFeed(address, route string) bool {
	if route != "" {
		first, _, _ := strings.Cut(strings.TrimPrefix(route, "/"), "/")
		return slices.Contains(forumRoutes, strings.ToLower(first))
	}
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range forumHosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	parts := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	if len(parts) == 0 || !strings.HasSuffix(parts[len(parts)-1], ".rss") {
		return false
	}
	return slices.Contains(discourseRoots, strings.ToLower(strings.TrimSuffix(parts[0], ".rss")))
}

// SuggestKindFor is the kind a source is suggested after its first fetch: forum for a community's feed, else what the
// entries look like (SuggestKind). It is "" when the source already has that kind.
func SuggestKindFor(src store.FeedSource, entries []store.FeedEntry) string {
	if ForumFeed(src.URL, src.RSSHubPath) {
		if src.Kind == "forum" {
			return ""
		}
		return "forum"
	}
	return SuggestKind(entries, src.Kind)
}
