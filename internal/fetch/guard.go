package fetch

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

const maxRedirects = 5

// ErrBlockedAddress reports a connection to a loopback, private, link-local or reserved address.
var ErrBlockedAddress = errors.New("refusing to connect to a private or reserved address")

// blockedPrefixes are ranges that netip has no predicate for. 198.18.0.0/15 stays allowed:
// proxies such as Clash in TUN mode resolve every name into it.
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),     // "this network"
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT
	netip.MustParsePrefix("240.0.0.0/4"),   // reserved, includes broadcast
}

// blocked reports whether an address must not be reached from links found inside feeds.
func blocked(addr netip.Addr) bool {
	addr = addr.Unmap() // ::ffff:127.0.0.1 is 127.0.0.1
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return true
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// guardedClient fetches addresses taken from feed content. refuse runs on the address actually
// dialed, after DNS resolution and for every redirect, so neither a DNS answer nor a redirect can
// lead it inside the network. It connects directly: through a proxy the real destination is unknown.
func guardedClient(refuse func(netip.AddrPort) bool) *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			addrPort, err := netip.ParseAddrPort(address)
			if err != nil {
				return err
			}
			if refuse(addrPort) {
				return fmt.Errorf("%w: %s", ErrBlockedAddress, addrPort.Addr())
			}
			return nil
		},
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
		MaxIdleConnsPerHost:   2,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return fmt.Errorf("more than %d redirects", maxRedirects)
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to %s is not allowed", req.URL.Scheme)
			}
			return nil
		},
	}
}
