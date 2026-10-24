package fetch

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

const (
	maxUserAgentLen = 500
	maxRedirectsAny = 10 // the limit net/http applies when a client has no CheckRedirect
)

var (
	// ErrInvalidUserAgent is a User-Agent that is too long, has characters other than printable ASCII, or is a
	// cookie or credential in disguise.
	ErrInvalidUserAgent = errors.New("invalid user agent")
	// ErrInvalidFetchReferer is a Referer that is not an absolute http or https address with a host, has a user name
	// or password, or is too long.
	ErrInvalidFetchReferer = errors.New("invalid fetch referer")
)

// ParseUserAgent checks the User-Agent a subscription sends, trimmed of surrounding space, and returns the trimmed
// text. An empty one is valid and means the default. A value that starts like a Cookie or Authorization header is
// refused: a subscription carries no cookie and no credential here.
func ParseUserAgent(agent string) (string, error) {
	agent = strings.TrimSpace(agent)
	if len(agent) > maxUserAgentLen {
		return "", ErrInvalidUserAgent
	}
	for _, r := range agent {
		if r < 0x20 || r > 0x7e {
			return "", ErrInvalidUserAgent
		}
	}
	lower := strings.ToLower(agent)
	if strings.HasPrefix(lower, "cookie:") || strings.HasPrefix(lower, "authorization:") {
		return "", ErrInvalidUserAgent
	}
	return agent, nil
}

// ParseFetchReferer checks the Referer a subscription sends to its own host, trimmed of surrounding space, and returns
// the trimmed text. An empty one is valid and means none is sent. Unlike ParseReferer, it refuses a user name or
// password in the address.
func ParseFetchReferer(referer string) (string, error) {
	referer, err := ParseReferer(referer)
	if err != nil {
		return "", ErrInvalidFetchReferer
	}
	if referer != "" {
		if u, err := url.Parse(referer); err != nil || u.User != nil {
			return "", ErrInvalidFetchReferer
		}
	}
	return referer, nil
}

// sameHost reports whether a request goes to the host (with its port) of the subscription's own address.
func sameHost(req *http.Request, own *url.URL) bool {
	return own != nil && strings.EqualFold(req.URL.Host, own.Host)
}

// onRedirect returns a copy of client that runs apply on the request of every redirect before the client's own
// redirect check. net/http copies the User-Agent and Referer of the first request onto each redirect, whatever its
// host, so a header meant for one host needs apply to take it back off.
func onRedirect(client *http.Client, apply func(*http.Request)) *http.Client {
	c := *client
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		apply(req)
		if client.CheckRedirect != nil {
			return client.CheckRedirect(req, via)
		}
		if len(via) >= maxRedirectsAny {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &c
}
