// Package tracker knows the addresses of pictures that only count visits: open pixels in newsletters and feeds,
// and analytics beacons. The list is small and kept in code; nothing is fetched to keep it current.
package tracker

import (
	"net/url"
	"strings"
)

// rule matches a host and its subdomains, and when prefix is set, only the paths that start with it. A rule
// without a host matches the path on any host.
type rule struct {
	host, prefix string
}

var rules = []rule{
	// Analytics and advertising beacons: every address on these hosts is a beacon.
	{host: "google-analytics.com"},
	{host: "doubleclick.net"},
	{host: "googletagmanager.com"},
	{host: "pixel.wp.com"},
	{host: "stats.wordpress.com"},
	{host: "feedsportal.com"},
	{host: "track.hubspot.com"},
	// Feed counters on hosts that also serve real files.
	{host: "feeds.feedburner.com", prefix: "/~r/"},
	{host: "feeds.feedburner.com", prefix: "/~ff/"},
	{host: "feedproxy.google.com", prefix: "/~r/"},
	{host: "feedproxy.google.com", prefix: "/~ff/"},
	// Open pixels of mail services.
	{host: "list-manage.com", prefix: "/track/open"},
	{host: "mandrillapp.com", prefix: "/track/open"},
	{host: "sendgrid.net", prefix: "/wf/open"},
	{host: "mailgun.org", prefix: "/o/"},
	{host: "pstmrk.it", prefix: "/open"},
	// The same pixel on a custom domain.
	{prefix: "/__utm.gif"},
	{prefix: "/__ptq.gif"},
	{prefix: "/api/v1/email/open"},
}

// Known reports whether u is the address of a known tracking picture. Only the host and the path count, so a
// query string cannot hide one and an ordinary picture on a shared host stays.
func Known(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	path := strings.ToLower(u.Path)
	for _, r := range rules {
		if r.host != "" && host != r.host && !strings.HasSuffix(host, "."+r.host) {
			continue
		}
		if strings.HasPrefix(path, r.prefix) {
			return true
		}
	}
	return convertKit(host)
}

// convertKit matches open.convertkit-mail2.com, open.kit-mail3.com and the like, whose number varies.
func convertKit(host string) bool {
	labels := strings.Split(host, ".")
	if len(labels) < 3 || labels[0] != "open" {
		return false
	}
	return strings.HasPrefix(labels[1], "convertkit-mail") || strings.HasPrefix(labels[1], "kit-mail")
}

// KnownAddress is Known for an address that has not been parsed; one that does not parse is not known.
func KnownAddress(address string) bool {
	u, err := url.Parse(address)
	return err == nil && Known(u)
}
