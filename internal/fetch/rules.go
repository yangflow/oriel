package fetch

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Candidate is a feed found for a web address: URL, or RSSHubPath on the user's RSSHub instance. Origin says how
// it was found: "rule" for a built-in rule, "feed" when the address itself is a feed, "alternate" for a page's
// <link rel="alternate"> and "common" for a common path. A candidate from a rule also names the site, what the feed
// carries (Label) and whom it is about (Subject), so a client can word it in the user's language.
type Candidate struct {
	URL        string `json:"url"`
	RSSHubPath string `json:"rsshubPath"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
	Origin     string `json:"origin"`
	Site       string `json:"site,omitempty"`
	Label      string `json:"label,omitempty"`
	Subject    string `json:"subject,omitempty"`
}

// rule maps the page of a site to feeds. Its path segments are anchored regular expressions, one per leading
// segment of the page's path, and exactly one holds the capture, the subject, unless query names a query
// parameter that holds it. {} in an output stands for the subject. Hosts have "www." and "m." removed.
type rule struct {
	hosts []string
	path  []string
	query string
	out   []output
	segs  []*regexp.Regexp
}

// output is one feed of a rule.
type output struct {
	url, route string
	site       string
	label      string
	kind       string
}

const (
	digits   = `^([0-9]+)$`
	anything = `^([^/]+)$`
)

// rules are shipped in the binary, in the manner of RSSHub Radar; nothing is asked of Radar or any other service at
// run time. X's reserved pages are rejected in match.
var rules = compile([]rule{
	{hosts: []string{"youtube.com"}, path: []string{`^channel$`, `^(UC[A-Za-z0-9_-]+)$`},
		out: []output{{url: "https://www.youtube.com/feeds/videos.xml?channel_id={}", site: "youtube", label: "channel", kind: "video"}}},
	{hosts: []string{"youtube.com"}, path: []string{`^(@[^/@]+)$`},
		out: []output{{route: "/youtube/user/{}", site: "youtube", label: "channel", kind: "video"}}},
	{hosts: []string{"youtube.com"}, path: []string{`^playlist$`}, query: "list",
		out: []output{{url: "https://www.youtube.com/feeds/videos.xml?playlist_id={}", site: "youtube", label: "playlist", kind: "video"}}},
	{hosts: []string{"space.bilibili.com"}, path: []string{digits}, out: []output{
		{route: "/bilibili/user/video/{}", site: "bilibili", label: "videos", kind: "video"},
		{route: "/bilibili/user/dynamic/{}", site: "bilibili", label: "dynamic", kind: "social"},
	}},
	{hosts: []string{"weibo.com", "weibo.cn"}, path: []string{`^u$`, digits},
		out: []output{{route: "/weibo/user/{}", site: "weibo", label: "posts", kind: "social"}}},
	{hosts: []string{"weibo.com"}, path: []string{digits},
		out: []output{{route: "/weibo/user/{}", site: "weibo", label: "posts", kind: "social"}}},
	{hosts: []string{"weibo.cn"}, path: []string{`^profile$`, digits},
		out: []output{{route: "/weibo/user/{}", site: "weibo", label: "posts", kind: "social"}}},
	{hosts: []string{"zhihu.com"}, path: []string{`^people$`, anything}, out: []output{
		{route: "/zhihu/people/activities/{}", site: "zhihu", label: "activities", kind: "article"},
		{route: "/zhihu/people/answers/{}", site: "zhihu", label: "answers", kind: "article"},
	}},
	{hosts: []string{"zhihu.com"}, path: []string{`^column$`, anything},
		out: []output{{route: "/zhihu/zhuanlan/{}", site: "zhihu", label: "column", kind: "article"}}},
	{hosts: []string{"zhuanlan.zhihu.com"}, path: []string{`^([A-Za-z0-9_-]+)$`},
		out: []output{{route: "/zhihu/zhuanlan/{}", site: "zhihu", label: "column", kind: "article"}}},
	{hosts: []string{"zhihu.com"}, path: []string{`^topic$`, digits},
		out: []output{{route: "/zhihu/topic/{}", site: "zhihu", label: "topic", kind: "article"}}},
	{hosts: []string{"zhihu.com"}, path: []string{`^collection$`, digits},
		out: []output{{route: "/zhihu/collection/{}", site: "zhihu", label: "collection", kind: "article"}}},
	{hosts: []string{"zhihu.com"}, path: []string{`^question$`, digits},
		out: []output{{route: "/zhihu/question/{}", site: "zhihu", label: "question", kind: "article"}}},
	{hosts: []string{"x.com", "twitter.com"}, path: []string{`^([A-Za-z0-9_]+)$`},
		out: []output{{route: "/twitter/user/{}", site: "x", label: "posts", kind: "social"}}},
	{hosts: []string{"v2ex.com"}, path: []string{`^go$`, anything},
		out: []output{{url: "https://www.v2ex.com/feed/{}.xml", site: "v2ex", label: "node", kind: "forum"}}},
	{hosts: []string{"linux.do"}, path: []string{`^(latest|top)$`},
		out: []output{{url: "https://linux.do/{}.rss", site: "linuxdo", label: "topics", kind: "forum"}}},
	{hosts: []string{"linux.do"}, path: []string{`^tag$`, anything},
		out: []output{{url: "https://linux.do/tag/{}.rss", site: "linuxdo", label: "tag", kind: "forum"}}},
})

// reservedX are the pages of X under the root that are not users.
var reservedX = map[string]bool{
	"home": true, "i": true, "search": true, "explore": true, "settings": true, "messages": true,
	"notifications": true, "login": true, "signup": true, "logout": true, "compose": true, "hashtag": true,
	"intent": true, "share": true, "tos": true, "privacy": true,
}

func compile(list []rule) []rule {
	for i := range list {
		for _, s := range list[i].path {
			list[i].segs = append(list[i].segs, regexp.MustCompile(s))
		}
	}
	return list
}

// matchRules returns the feeds the built-in rules give for a page address, none when no rule fits.
func matchRules(u *url.URL) []Candidate {
	host := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."), "m.")
	parts := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	for _, r := range rules {
		if !slices.Contains(r.hosts, host) {
			continue
		}
		subject, ok := r.subject(parts, u.Query())
		if !ok || (host == "x.com" || host == "twitter.com") && reservedX[strings.ToLower(subject)] {
			continue
		}
		var found []Candidate
		for _, o := range r.out {
			c := Candidate{Kind: o.kind, Origin: "rule", Site: o.site, Label: o.label, Subject: subject}
			if o.url != "" {
				c.URL = strings.ReplaceAll(o.url, "{}", url.QueryEscape(subject))
			} else {
				c.RSSHubPath = strings.ReplaceAll(o.route, "{}", subject)
			}
			found = append(found, c)
		}
		return found
	}
	return nil
}

// subject matches the path of a page and returns the captured subject.
func (r rule) subject(parts []string, query url.Values) (string, bool) {
	if len(parts) < len(r.segs) {
		return "", false
	}
	subject := ""
	for i, re := range r.segs {
		m := re.FindStringSubmatch(parts[i])
		if m == nil {
			return "", false
		}
		if len(m) > 1 {
			subject = m[1]
		}
	}
	if r.query != "" {
		subject = query.Get(r.query)
	}
	return subject, subject != ""
}
