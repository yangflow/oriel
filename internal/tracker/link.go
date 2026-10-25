package tracker

import (
	"net/url"
	"strings"
)

// trackingParams are the query parameters, matched by their whole name, that only say where a click came from.
var trackingParams = map[string]bool{
	"fbclid": true, "gclid": true, "dclid": true, "msclkid": true, "yclid": true, "mc_cid": true, "mc_eid": true,
	"igshid": true, "_hsenc": true, "_hsmi": true, "mkt_tok": true,
}

// feedRefs are the values of "ref" that name a feed reader. Any other "ref" can be a real parameter of the page.
var feedRefs = map[string]bool{
	"rss": true, "feed": true, "feeds": true, "feedly": true, "atom": true,
}

// Param reports whether the query parameter name=value only tracks where a link was followed from: a name that
// starts with "utm_", one of the known names, or "ref" with the value of a feed reader. Names compare without case.
func Param(name, value string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "utm_") || trackingParams[lower] {
		return true
	}
	return lower == "ref" && feedRefs[strings.ToLower(value)]
}

// StripLink returns link without its tracking parameters (see Param). The other parameters keep their order and
// their spelling, the fragment stays, and a query that ends up empty loses its "?". A link that is not an http or
// https address, or does not parse, and a link without tracking parameters come back unchanged. This is for the
// moment a link is used (opened, copied, exported, compared); the address stored with the entry is never rewritten.
func StripLink(link string) string {
	u, err := url.Parse(link)
	if err != nil || u.Host == "" || (!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) {
		return link
	}
	head, fragment, hasFragment := strings.Cut(link, "#")
	head, query, hasQuery := strings.Cut(head, "?")
	if !hasQuery {
		return link
	}
	pieces := strings.Split(query, "&")
	kept := make([]string, 0, len(pieces))
	for _, piece := range pieces {
		name, value, _ := strings.Cut(piece, "=")
		if !Param(unescape(name), unescape(value)) {
			kept = append(kept, piece)
		}
	}
	if len(kept) == len(pieces) {
		return link
	}
	out := head
	if len(kept) > 0 {
		out += "?" + strings.Join(kept, "&")
	}
	if hasFragment {
		out += "#" + fragment
	}
	return out
}

func unescape(s string) string {
	if v, err := url.QueryUnescape(s); err == nil {
		return v
	}
	return s
}
