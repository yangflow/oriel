package subscriptions

import (
	"net/url"
	"regexp"
	"strings"
)

// platform names what an address was recognized as, for the line under the address field.
type platform int

const (
	feed  platform = iota // a feed address, as typed
	route                 // an RSSHub route, as typed or as a full address on the RSSHub instance
	youtubeChannel
	youtubeUser
	youtubePlaylist
	bilibili
	xUser
	githubRepo
	githubUser
	telegram
	weibo
)

// target is what an address subscribes to: URL, or Route on the RSSHub instance. Kind is the kind the platform
// suggests, "" for none; Name is what the description names, as the user of a platform or the host of a feed.
type target struct {
	URL, Route string
	Kind       string
	Platform   platform
	Name       string
}

var (
	numeric  = regexp.MustCompile(`^[0-9]+$`)
	username = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	// reserved are the pages of X under the root that are not users.
	reserved = map[string]bool{
		"home": true, "i": true, "search": true, "explore": true, "settings": true, "messages": true,
		"notifications": true, "login": true, "signup": true, "logout": true, "compose": true, "hashtag": true,
		"intent": true, "share": true, "tos": true, "privacy": true,
	}
)

// recognize turns what is typed in the address field into what it subscribes to, with base the RSSHub instance of
// the settings, "" when there is none. It reports false for an address the server would refuse.
func recognize(input, base string) (target, bool) {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "/") {
		if strings.HasPrefix(input, "//") {
			return target{}, false
		}
		return target{Route: input, Platform: route}, true
	}
	u, err := url.Parse(input)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return target{}, false
	}
	if base = strings.TrimRight(strings.TrimSpace(base), "/"); base != "" {
		if rest, ok := strings.CutPrefix(input, base); ok && strings.HasPrefix(rest, "/") &&
			!strings.HasPrefix(rest, "//") {
			return target{Route: rest, Platform: route}, true
		}
	}
	if t, ok := known(u); ok {
		return t, true
	}
	return target{URL: input, Platform: feed, Name: u.Hostname()}, true
}

// known recognizes the links of the platforms in docs/product.md; an address ending as a feed does is one already.
func known(u *url.URL) (target, bool) {
	if path := strings.ToLower(u.Path); strings.HasSuffix(path, ".atom") || strings.HasSuffix(path, ".rss") ||
		strings.HasSuffix(path, ".xml") {
		return target{}, false
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "m.")
	parts := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	switch host {
	case "youtube.com":
		switch {
		case len(parts) >= 2 && parts[0] == "channel" && strings.HasPrefix(parts[1], "UC"):
			return target{URL: "https://www.youtube.com/feeds/videos.xml?channel_id=" + url.QueryEscape(parts[1]),
				Kind: "video", Platform: youtubeChannel, Name: parts[1]}, true
		case len(parts) >= 1 && len(parts[0]) > 1 && strings.HasPrefix(parts[0], "@"):
			return target{Route: "/youtube/user/" + parts[0], Kind: "video", Platform: youtubeUser, Name: parts[0]},
				true
		case len(parts) == 1 && parts[0] == "playlist" && u.Query().Get("list") != "":
			list := u.Query().Get("list")
			return target{URL: "https://www.youtube.com/feeds/videos.xml?playlist_id=" + url.QueryEscape(list),
				Kind: "video", Platform: youtubePlaylist, Name: list}, true
		}
	case "space.bilibili.com":
		if len(parts) >= 1 && numeric.MatchString(parts[0]) {
			return target{Route: "/bilibili/user/video/" + parts[0], Kind: "video", Platform: bilibili,
				Name: parts[0]}, true
		}
	case "x.com", "twitter.com":
		if len(parts) >= 1 && username.MatchString(parts[0]) && !reserved[strings.ToLower(parts[0])] {
			return target{Route: "/twitter/user/" + parts[0], Kind: "social", Platform: xUser, Name: parts[0]}, true
		}
	case "github.com":
		switch {
		case len(parts) >= 2:
			repo := parts[0] + "/" + strings.TrimSuffix(parts[1], ".git")
			return target{URL: "https://github.com/" + repo + "/releases.atom", Kind: "article", Platform: githubRepo,
				Name: repo}, true
		case len(parts) == 1 && !strings.HasSuffix(parts[0], ".atom"):
			return target{URL: "https://github.com/" + parts[0] + ".atom", Kind: "article", Platform: githubUser,
				Name: parts[0]}, true
		}
	case "t.me":
		if len(parts) >= 2 && parts[0] == "s" {
			parts = parts[1:]
		}
		if len(parts) >= 1 && username.MatchString(parts[0]) {
			return target{Route: "/telegram/channel/" + parts[0], Kind: "social", Platform: telegram,
				Name: parts[0]}, true
		}
	case "weibo.com":
		if len(parts) >= 2 && parts[0] == "u" {
			parts = parts[1:]
		}
		if len(parts) >= 1 && numeric.MatchString(parts[0]) {
			return target{Route: "/weibo/user/" + parts[0], Kind: "social", Platform: weibo, Name: parts[0]}, true
		}
	}
	return target{}, false
}
