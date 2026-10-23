package fetch

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	"github.com/yangflow/oriel/internal/store"
)

// Settings that hold the global fetch proxy. The user name and password are secrets like the AI API key: stored
// here, never sent by the API, logged or backed up.
const (
	ProxySetting         = "fetch_proxy"
	ProxyUserSetting     = "fetch_proxy_user"
	ProxyPasswordSetting = "fetch_proxy_password"
)

// MaxProxyField caps the proxy address, user name and password.
const MaxProxyField = 500

// globalTTL is how long a read of the global proxy settings is reused, so a change applies within seconds
// without a database read for every image.
const globalTTL = 2 * time.Second

// ValidProxyURL checks a proxy address: http, https, socks5 or socks5h, a host and a port, and nothing else.
// Credentials are not part of the address.
func ValidProxyURL(raw string) error {
	_, err := parseProxy(raw)
	return err
}

func parseProxy(raw string) (*url.URL, error) {
	if len(raw) > MaxProxyField {
		return nil, errors.New("the proxy address is too long")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("the proxy address is not a valid address")
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, errors.New("the proxy address must start with http://, https://, socks5:// or socks5h://")
	}
	if u.User != nil || strings.Contains(raw, "@") {
		return nil, errors.New("the proxy address must not carry a user name or password")
	}
	if u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || strings.Contains(raw, "#") {
		return nil, errors.New("the proxy address must be only scheme://host:port")
	}
	port, err := strconv.Atoi(u.Port())
	if u.Hostname() == "" || err != nil || port < 1 || port > 65535 {
		return nil, errors.New("the proxy address needs a host and a port")
	}
	return u, nil
}

// Network picks the HTTP client for a request: direct, or through the global or per-source proxy. A nil Network
// always connects the way the client given to it does.
type Network struct {
	store  *store.Store
	refuse func(netip.AddrPort) bool

	mu       sync.Mutex
	global   store.SourceProxy
	globalAt time.Time
	guarded  map[store.SourceProxy]*http.Client
	feeds    map[store.SourceProxy]*http.Client
}

// NewNetwork reads the global proxy from st's settings.
func NewNetwork(st *store.Store) *Network {
	return &Network{
		store:   st,
		refuse:  func(a netip.AddrPort) bool { return blocked(a.Addr()) },
		guarded: map[store.SourceProxy]*http.Client{},
		feeds:   map[store.SourceProxy]*http.Client{},
	}
}

// errBadProxy is what a stored proxy setting that no longer parses turns into. Such a request fails: falling back
// to a direct connection would show the user's address to the sites the proxy was meant to hide it from.
var errBadProxy = errors.New("the proxy setting is invalid")

// effective is the proxy a request uses: the source's own, direct, or the global one. The zero value means none.
func (n *Network) effective(ctx context.Context, per store.SourceProxy) (store.SourceProxy, error) {
	if per.URL != "" {
		return per, nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if time.Since(n.globalAt) < globalTTL {
		return n.global, nil
	}
	var g store.SourceProxy
	for key, dst := range map[string]*string{ProxySetting: &g.URL, ProxyUserSetting: &g.User, ProxyPasswordSetting: &g.Password} {
		v, err := n.store.Setting(ctx, key)
		if err != nil {
			return store.SourceProxy{}, fmt.Errorf("read the proxy setting: %w", err)
		}
		*dst = v
	}
	n.global, n.globalAt = g, time.Now()
	return g, nil
}

// Guarded returns the client for addresses found inside feeds. direct is used when no proxy applies (it is also
// what tests replace). The SSRF check holds either way.
func (n *Network) Guarded(ctx context.Context, per store.SourceProxy, direct *http.Client) (*http.Client, error) {
	if n == nil {
		return direct, nil
	}
	eff, err := n.effective(ctx, per)
	if err != nil {
		return nil, err
	}
	if eff.URL == "" || eff.URL == store.ProxyDirect {
		return direct, nil
	}
	return n.cached(n.guarded, eff, func(u *url.URL) *http.Client {
		return guardedClientVia(n.refuse, tunnel(u, eff, n.refuse))
	})
}

// Feed returns the client for addresses the user typed: feeds, RSSHub instances and their route catalogs. With no
// proxy it is direct, which keeps honoring HTTP_PROXY and HTTPS_PROXY; a source set to direct ignores those too.
func (n *Network) Feed(ctx context.Context, per store.SourceProxy, direct *http.Client) (*http.Client, error) {
	if n == nil {
		return direct, nil
	}
	eff, err := n.effective(ctx, per)
	if err != nil {
		return nil, err
	}
	if eff.URL == "" {
		return direct, nil
	}
	if eff.URL == store.ProxyDirect {
		return n.cached(n.feeds, eff, func(*url.URL) *http.Client {
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = nil
			return &http.Client{Timeout: direct.Timeout, Transport: transport}
		})
	}
	return n.cached(n.feeds, eff, func(u *url.URL) *http.Client {
		withAuth := *u
		if eff.User != "" || eff.Password != "" {
			withAuth.User = url.UserPassword(eff.User, eff.Password)
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = func(req *http.Request) (*url.URL, error) {
			if localHost(req.URL.Hostname()) {
				return nil, nil
			}
			return &withAuth, nil
		}
		return &http.Client{
			Timeout:   direct.Timeout,
			Transport: &scrubbing{next: transport, secrets: secretsOf(u, eff)},
		}
	})
}

// cached builds the client of one proxy once. Clients keep idle connections, so the map stays small.
func (n *Network) cached(m map[store.SourceProxy]*http.Client, eff store.SourceProxy, build func(*url.URL) *http.Client) (*http.Client, error) {
	var u *url.URL
	if eff.URL != store.ProxyDirect {
		var err error
		if u, err = parseProxy(eff.URL); err != nil {
			return nil, errBadProxy
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if c, ok := m[eff]; ok {
		return c, nil
	}
	if len(m) >= 16 {
		for k, c := range m {
			c.CloseIdleConnections()
			delete(m, k)
		}
	}
	c := build(u)
	m[eff] = c
	return c, nil
}

// localHost reports a host the user typed that is on this machine or its network: a proxy cannot reach it and
// should not be asked to.
func localHost(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && blocked(addr)
}

// tunnel dials a target through the proxy. It resolves the target here first and refuses when any answer is
// refused, the same rule as the direct guard, then hands the host name to the proxy, so a proxy that resolves
// names itself (a fake-IP proxy) keeps working. The proxy may resolve the name again to another answer.
func tunnel(u *url.URL, cred store.SourceProxy, refuse func(netip.AddrPort) bool) func(context.Context, string, string) (net.Conn, error) {
	secrets := secretsOf(u, cred)
	return func(ctx context.Context, _, address string) (net.Conn, error) {
		host, portText, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		port, err := strconv.ParseUint(portText, 10, 16)
		if err != nil {
			return nil, err
		}
		if err := checkTarget(ctx, host, uint16(port), refuse); err != nil {
			return nil, err
		}
		var conn net.Conn
		if strings.HasPrefix(u.Scheme, "socks5") {
			conn, err = socksDial(ctx, u, cred, address)
		} else {
			conn, err = connectDial(ctx, u, cred, address)
		}
		if err != nil {
			return nil, scrub(err, secrets)
		}
		return conn, nil
	}
}

// checkTarget refuses a target whose address, or any of whose resolved addresses, the guard refuses. A name that
// does not resolve is refused too: letting the proxy resolve it would skip the check.
func checkTarget(ctx context.Context, host string, port uint16, refuse func(netip.AddrPort) bool) error {
	var addrs []netip.Addr
	if ip, err := netip.ParseAddr(strings.SplitN(host, "%", 2)[0]); err == nil {
		addrs = []netip.Addr{ip}
	} else {
		if addrs, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host); err != nil {
			return fmt.Errorf("resolve %s: %w", host, err)
		}
		if len(addrs) == 0 {
			return fmt.Errorf("resolve %s: no addresses", host)
		}
	}
	for _, a := range addrs {
		if refuse(netip.AddrPortFrom(a, port)) {
			return fmt.Errorf("%w: %s", ErrBlockedAddress, a)
		}
	}
	return nil
}

func socksDial(ctx context.Context, u *url.URL, cred store.SourceProxy, address string) (net.Conn, error) {
	var auth *proxy.Auth
	if cred.User != "" || cred.Password != "" {
		auth = &proxy.Auth{User: cred.User, Password: cred.Password}
	}
	dialer, err := proxy.SOCKS5("tcp", u.Host, auth, &net.Dialer{Timeout: 10 * time.Second})
	if err != nil {
		return nil, err
	}
	conn, err := dialer.(proxy.ContextDialer).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("the proxy failed: %w", err)
	}
	return conn, nil
}

// connectDial opens a tunnel with an HTTP CONNECT request, to an http or https proxy.
func connectDial(ctx context.Context, u *url.URL, cred store.SourceProxy, address string) (net.Conn, error) {
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", u.Host)
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) {
			err = op.Err // "connection refused", without the proxy's address
		}
		return nil, fmt.Errorf("cannot reach the proxy: %w", err)
	}
	fail := func(format string, args ...any) (net.Conn, error) {
		conn.Close()
		return nil, fmt.Errorf(format, args...)
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetDeadline(deadline)
	if u.Scheme == "https" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: u.Hostname()})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return fail("cannot reach the proxy: %w", err)
		}
		conn = tlsConn
	}
	request := "CONNECT " + address + " HTTP/1.1\r\nHost: " + address + "\r\n"
	if cred.User != "" || cred.Password != "" {
		request += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(cred.User+":"+cred.Password)) + "\r\n"
	}
	if _, err := conn.Write([]byte(request + "\r\n")); err != nil {
		return fail("cannot reach the proxy: %w", err)
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return fail("the proxy sent no valid answer: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fail("the proxy refused the connection: status %d", resp.StatusCode)
	}
	conn.SetDeadline(time.Time{})
	if reader.Buffered() > 0 {
		return &bufferedConn{Conn: conn, r: reader}, nil
	}
	return conn, nil
}

// bufferedConn serves the bytes the CONNECT answer's reader already took ahead of the connection.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// scrubbed is an error whose text has the proxy's address and credentials taken out, while errors.Is and
// errors.As still reach the original.
type scrubbed struct {
	msg string
	err error
}

func (e *scrubbed) Error() string { return e.msg }
func (e *scrubbed) Unwrap() error { return e.err }

// secretsOf lists what must never appear in an error: the proxy's address and its credentials.
func secretsOf(u *url.URL, cred store.SourceProxy) []string {
	return []string{u.Host, cred.User, cred.Password}
}

func scrub(err error, secrets []string) error {
	msg := err.Error()
	for _, s := range secrets {
		if s != "" {
			msg = strings.ReplaceAll(msg, s, "the proxy")
		}
	}
	if msg == err.Error() {
		return err
	}
	return &scrubbed{msg: msg, err: err}
}

// scrubbing removes the proxy's address and credentials from the errors of a transport that proxies by itself.
type scrubbing struct {
	next    http.RoundTripper
	secrets []string
}

func (s *scrubbing) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := s.next.RoundTrip(req)
	if err != nil {
		return nil, scrub(err, s.secrets)
	}
	return resp, nil
}
