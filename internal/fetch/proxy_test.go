package fetch

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

func TestValidProxyURL(t *testing.T) {
	for _, ok := range []string{"http://127.0.0.1:7890", "https://proxy.example:443", "socks5://localhost:1080", "socks5h://[::1]:1080"} {
		if err := ValidProxyURL(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "proxy.example:8080", "ftp://proxy.example:21", "http://proxy.example", "http://proxy.example:0",
		"http://proxy.example:99999", "http://u:p@proxy.example:8080", "http://u@proxy.example:8080", "http://proxy.example:8080/path",
		"http://proxy.example:8080?x=1", "http://proxy.example:8080#x", "http://:8080", "http://" + strings.Repeat("a", 600) + ":80"} {
		if err := ValidProxyURL(bad); err == nil {
			t.Errorf("%q is accepted", bad)
		}
	}
}

// connectProxy is an HTTP proxy that tunnels CONNECT requests and records them.
type connectProxy struct {
	*httptest.Server
	mu      sync.Mutex
	targets []string
	auth    []string
	status  int // when not zero, every CONNECT is answered with it
}

func newConnectProxy(t *testing.T) *connectProxy {
	t.Helper()
	p := &connectProxy{}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.targets = append(p.targets, r.Host)
		p.auth = append(p.auth, r.Header.Get("Proxy-Authorization"))
		status := p.status
		p.mu.Unlock()
		if r.Method != http.MethodConnect {
			t.Errorf("proxy got %s %s", r.Method, r.URL)
			return
		}
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		upstream, err := net.Dial("tcp", r.Host)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		client, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			upstream.Close()
			return
		}
		client.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
		go func() { io.Copy(upstream, client); upstream.Close() }()
		go func() { io.Copy(client, upstream); client.Close() }()
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *connectProxy) seen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.targets...)
}

func (p *connectProxy) address() string { return "http://" + p.Listener.Addr().String() }

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "oriel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func setProxy(t *testing.T, st *store.Store, address, user, password string) {
	t.Helper()
	for key, value := range map[string]string{ProxySetting: address, ProxyUserSetting: user, ProxyPasswordSetting: password} {
		if err := st.SetSetting(context.Background(), key, value); err != nil {
			t.Fatal(err)
		}
	}
}

func body(t *testing.T, client *http.Client, address string) (string, error) {
	t.Helper()
	resp, err := client.Get(address)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return string(data), nil
}

func target(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "target") }))
	t.Cleanup(srv.Close)
	return srv
}

func TestGuardedThroughHTTPProxy(t *testing.T) {
	st := openStore(t)
	proxy := newConnectProxy(t)
	srv := target(t)
	setProxy(t, st, proxy.address(), "alice", "s3cret")
	n := NewNetwork(st)
	direct := guardedClient(func(netip.AddrPort) bool { return false })

	// The default guard refuses the private target before the proxy is asked, although the proxy itself is on loopback.
	client, err := n.Guarded(context.Background(), store.SourceProxy{}, direct)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := body(t, client, srv.URL); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("a private target through the proxy: %v", err)
	}
	if seen := proxy.seen(); len(seen) != 0 {
		t.Fatalf("the proxy was asked for %v", seen)
	}

	n.refuse = func(netip.AddrPort) bool { return false }
	n.guarded = map[store.SourceProxy]*http.Client{}
	client, err = n.Guarded(context.Background(), store.SourceProxy{}, direct)
	if err != nil {
		t.Fatal(err)
	}
	got, err := body(t, client, srv.URL)
	if err != nil || got != "target" {
		t.Fatalf("through the proxy: %q %v", got, err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:s3cret"))
	if seen := proxy.seen(); len(seen) != 1 || seen[0] != srv.Listener.Addr().String() || proxy.auth[0] != want {
		t.Fatalf("the proxy saw %v %v", seen, proxy.auth)
	}
}

func TestGuardedProxyChecksTheTargetBeforeConnecting(t *testing.T) {
	st := openStore(t)
	proxy := newConnectProxy(t)
	setProxy(t, st, proxy.address(), "", "")
	n := NewNetwork(st)
	direct := guardedClient(func(netip.AddrPort) bool { return false })
	client, err := n.Guarded(context.Background(), store.SourceProxy{}, direct)
	if err != nil {
		t.Fatal(err)
	}
	// "localhost" resolves to a loopback address: refused here, never handed to the proxy.
	if _, err := body(t, client, "http://localhost:9/x"); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("localhost through the proxy: %v", err)
	}
	// A literal private address and a name that does not resolve are refused too.
	for _, bad := range []string{"http://192.168.1.1/x", "http://[::1]/x", "http://nonexistent.invalid/x"} {
		if _, err := body(t, client, bad); err == nil {
			t.Errorf("%s was fetched", bad)
		}
	}
	if seen := proxy.seen(); len(seen) != 0 {
		t.Fatalf("the proxy was asked for %v", seen)
	}
}

func TestProxyErrorsCarryNoProxyDetails(t *testing.T) {
	st := openStore(t)
	proxy := newConnectProxy(t)
	proxy.status = http.StatusProxyAuthRequired
	setProxy(t, st, proxy.address(), "alice", "s3cret")
	n := NewNetwork(st)
	n.refuse = func(netip.AddrPort) bool { return false }
	direct := guardedClient(func(netip.AddrPort) bool { return false })
	client, _ := n.Guarded(context.Background(), store.SourceProxy{}, direct)
	_, err := body(t, client, target(t).URL)
	if err == nil || !strings.Contains(err.Error(), "the proxy refused the connection: status 407") {
		t.Fatalf("a refusing proxy: %v", err)
	}

	// A proxy nobody listens on.
	dead, _ := net.Listen("tcp", "127.0.0.1:0")
	address := dead.Addr().String()
	dead.Close()
	setProxy(t, st, "http://"+address, "alice", "s3cret")
	n = NewNetwork(st)
	n.refuse = func(netip.AddrPort) bool { return false }
	client, _ = n.Guarded(context.Background(), store.SourceProxy{}, direct)
	_, err = body(t, client, target(t).URL)
	if err == nil || !strings.Contains(err.Error(), "cannot reach the proxy") {
		t.Fatalf("a dead proxy: %v", err)
	}
	for _, secret := range []string{address, "alice", "s3cret"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("the error has %q: %v", secret, err)
		}
	}
}

// socksProxy is a SOCKS5 proxy that accepts no authentication, or user name and password, and records the targets.
type socksProxy struct {
	net.Listener
	user, password string
	mu             sync.Mutex
	targets        []string
}

func newSocksProxy(t *testing.T, user, password string) *socksProxy {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &socksProxy{Listener: l, user: user, password: password}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go p.serve(c)
		}
	}()
	return p
}

func (p *socksProxy) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	head := make([]byte, 2)
	if _, err := io.ReadFull(r, head); err != nil {
		return
	}
	io.ReadFull(r, make([]byte, head[1]))
	if p.user == "" {
		c.Write([]byte{5, 0})
	} else {
		c.Write([]byte{5, 2})
		ver := make([]byte, 2)
		io.ReadFull(r, ver[:2])
		user := make([]byte, ver[1])
		io.ReadFull(r, user)
		n, _ := r.ReadByte()
		pass := make([]byte, n)
		io.ReadFull(r, pass)
		if string(user) != p.user || string(pass) != p.password {
			c.Write([]byte{1, 1})
			return
		}
		c.Write([]byte{1, 0})
	}
	req := make([]byte, 4)
	if _, err := io.ReadFull(r, req); err != nil {
		return
	}
	var host string
	switch req[3] {
	case 1:
		ip := make([]byte, 4)
		io.ReadFull(r, ip)
		host = net.IP(ip).String()
	case 3:
		n, _ := r.ReadByte()
		name := make([]byte, n)
		io.ReadFull(r, name)
		host = string(name)
	default:
		return
	}
	port := make([]byte, 2)
	io.ReadFull(r, port)
	address := net.JoinHostPort(host, strconv.Itoa(int(port[0])<<8|int(port[1])))
	p.mu.Lock()
	p.targets = append(p.targets, address)
	p.mu.Unlock()
	upstream, err := net.Dial("tcp", address)
	if err != nil {
		c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstream.Close()
	c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	go io.Copy(upstream, r)
	io.Copy(c, upstream)
}

func TestGuardedThroughSocks5Proxy(t *testing.T) {
	st := openStore(t)
	srv := target(t)
	proxy := newSocksProxy(t, "bob", "hunter2")
	direct := guardedClient(func(netip.AddrPort) bool { return false })

	setProxy(t, st, "socks5://"+proxy.Addr().String(), "bob", "hunter2")
	n := NewNetwork(st)
	n.refuse = func(netip.AddrPort) bool { return false }
	client, _ := n.Guarded(context.Background(), store.SourceProxy{}, direct)
	got, err := body(t, client, srv.URL)
	if err != nil || got != "target" {
		t.Fatalf("through SOCKS5: %q %v", got, err)
	}
	proxy.mu.Lock()
	seen := append([]string(nil), proxy.targets...)
	proxy.mu.Unlock()
	if len(seen) != 1 || seen[0] != srv.Listener.Addr().String() {
		t.Fatalf("the proxy saw %v", seen)
	}

	// The wrong password fails without showing it.
	setProxy(t, st, "socks5://"+proxy.Addr().String(), "bob", "wrong-pass")
	n = NewNetwork(st)
	n.refuse = func(netip.AddrPort) bool { return false }
	client, _ = n.Guarded(context.Background(), store.SourceProxy{}, direct)
	_, err = body(t, client, srv.URL)
	if err == nil || strings.Contains(err.Error(), "wrong-pass") || strings.Contains(err.Error(), proxy.Addr().String()) {
		t.Fatalf("a rejected SOCKS5 login: %v", err)
	}

	// The default guard still refuses a private target.
	n = NewNetwork(st)
	client, _ = n.Guarded(context.Background(), store.SourceProxy{}, direct)
	if _, err := body(t, client, srv.URL); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("a private target through SOCKS5: %v", err)
	}
}

func TestEffectiveProxy(t *testing.T) {
	st := openStore(t)
	global := newConnectProxy(t)
	own := newConnectProxy(t)
	srv := target(t)
	setProxy(t, st, global.address(), "", "")
	n := NewNetwork(st)
	n.refuse = func(netip.AddrPort) bool { return false }
	direct := guardedClient(func(netip.AddrPort) bool { return false })
	get := func(per store.SourceProxy) {
		t.Helper()
		client, err := n.Guarded(context.Background(), per, direct)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := body(t, client, srv.URL); err != nil || got != "target" {
			t.Fatalf("%q %v", got, err)
		}
	}
	get(store.SourceProxy{})                       // follows the global proxy
	get(store.SourceProxy{URL: own.address()})     // its own proxy
	get(store.SourceProxy{URL: store.ProxyDirect}) // direct
	if len(global.seen()) != 1 || len(own.seen()) != 1 {
		t.Fatalf("global %v, own %v", global.seen(), own.seen())
	}

	// A stored address that no longer parses fails the request rather than connecting directly.
	if _, err := n.Guarded(context.Background(), store.SourceProxy{URL: "nonsense"}, direct); !errors.Is(err, errBadProxy) {
		t.Fatalf("a bad stored proxy: %v", err)
	}

	// A change of the global proxy applies after the cache runs out.
	setProxy(t, st, "", "", "")
	n.mu.Lock()
	n.globalAt = time.Time{}
	n.mu.Unlock()
	if client, err := n.Guarded(context.Background(), store.SourceProxy{}, direct); err != nil || client != direct {
		t.Fatalf("no proxy should give the direct client: %v", err)
	}

	var none *Network
	if client, err := none.Guarded(context.Background(), store.SourceProxy{URL: own.address()}, direct); err != nil || client != direct {
		t.Fatal("a nil network must give the direct client")
	}
}

// forwardProxy answers plain HTTP requests sent to it with the absolute address, like an HTTP proxy does.
func forwardProxy(t *testing.T, got *[]string, auth *[]string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*got = append(*got, r.URL.String())
		*auth = append(*auth, r.Header.Get("Proxy-Authorization"))
		mu.Unlock()
		io.WriteString(w, "via proxy")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFeedClientThroughProxy(t *testing.T) {
	st := openStore(t)
	var got, auth []string
	proxy := forwardProxy(t, &got, &auth)
	local := target(t)
	setProxy(t, st, proxy.URL, "alice", "s3cret")
	n := NewNetwork(st)
	direct := &http.Client{Timeout: time.Second}

	client, err := n.Feed(context.Background(), store.SourceProxy{}, direct)
	if err != nil {
		t.Fatal(err)
	}
	if text, err := body(t, client, "http://feeds.example.test/rss"); err != nil || text != "via proxy" {
		t.Fatalf("a public feed: %q %v", text, err)
	}
	if len(got) != 1 || got[0] != "http://feeds.example.test/rss" ||
		auth[0] != "Basic "+base64.StdEncoding.EncodeToString([]byte("alice:s3cret")) {
		t.Fatalf("the proxy saw %v %v", got, auth)
	}
	// A self-hosted RSSHub on this machine or its network is reached directly.
	if text, err := body(t, client, local.URL); err != nil || text != "target" {
		t.Fatalf("a local feed: %q %v", text, err)
	}
	if len(got) != 1 {
		t.Fatalf("the proxy was asked for a local address: %v", got)
	}
	// A source set to direct ignores the global proxy.
	client, _ = n.Feed(context.Background(), store.SourceProxy{URL: store.ProxyDirect}, direct)
	if _, err := body(t, client, "http://feeds.example.test/rss"); err == nil || len(got) != 1 {
		t.Fatalf("a direct source used the proxy: %v %v", err, got)
	}

	// A failing proxy does not leak its address or credentials.
	proxy.Close()
	client, _ = n.Feed(context.Background(), store.SourceProxy{}, direct)
	_, err = body(t, client, "http://feeds.example.test/rss")
	if err == nil {
		t.Fatal("a closed proxy answered")
	}
	for _, secret := range []string{proxy.Listener.Addr().String(), "alice", "s3cret"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("the error has %q: %v", secret, err)
		}
	}
}

func TestLocalHost(t *testing.T) {
	for host, want := range map[string]bool{"localhost": true, "rsshub.localhost": true, "127.0.0.1": true, "10.1.2.3": true,
		"192.168.0.9": true, "::1": true, "100.64.0.5": true, "8.8.8.8": false, "rsshub.example": false} {
		if localHost(host) != want {
			t.Errorf("localHost(%q) = %v", host, !want)
		}
	}
}

func TestPollerFetchesThroughTheProxy(t *testing.T) {
	f := newFixture(t)
	var got, auth []string
	proxy := forwardProxy(t, &got, &auth)
	f.poller.UseNetwork(NewNetwork(f.store))
	setProxy(t, f.store, proxy.URL, "", "")
	id := f.add(store.NewSource{URL: "http://feeds.example.test/rss"})
	f.poller.fetchDue(context.Background())
	if len(got) != 1 || got[0] != "http://feeds.example.test/rss" {
		t.Fatalf("the proxy saw %v", got)
	}
	// The proxy answered with text that is not a feed; what matters is that the fetch went through it.
	if f.text("SELECT last_error FROM sources WHERE id = ?", id) == "" {
		t.Fatal("expected a parse error from the proxy's answer")
	}
}

// A proxy given by name fails with the reason only: not the address the name resolved to, which the network errors
// would otherwise carry.
func TestProxyByNameLeaksNoResolvedAddress(t *testing.T) {
	dead, _ := net.Listen("tcp", "127.0.0.1:0")
	port := strconv.Itoa(dead.Addr().(*net.TCPAddr).Port)
	dead.Close()
	for _, scheme := range []string{"http", "socks5"} {
		st := openStore(t)
		setProxy(t, st, scheme+"://localhost:"+port, "alice", "s3cret")
		n := NewNetwork(st)
		n.refuse = func(netip.AddrPort) bool { return false }
		guarded, _ := n.Guarded(context.Background(), store.SourceProxy{}, guardedClient(func(netip.AddrPort) bool { return false }))
		feed, _ := n.Feed(context.Background(), store.SourceProxy{}, &http.Client{Timeout: time.Second})
		for name, c := range map[string]*http.Client{"guarded": guarded, "feed": feed} {
			address := target(t).URL
			if name == "feed" {
				address = "http://feeds.example.test/rss"
			}
			_, err := body(t, c, address)
			if err == nil || !strings.Contains(err.Error(), "the proxy") {
				t.Fatalf("%s %s: %v", scheme, name, err)
			}
			for _, secret := range []string{port, "localhost", "127.0.0.1:" + port, "[::1]", "alice", "s3cret"} {
				if strings.Contains(strings.ReplaceAll(err.Error(), address, ""), secret) {
					t.Errorf("%s %s: the error has %q: %v", scheme, name, secret, err)
				}
			}
		}
	}
}

// The exemption for a local host holds for the address the user typed, not for where a feed redirects to.
func TestFeedClientRefusesRedirectToLocalHost(t *testing.T) {
	st := openStore(t)
	local := target(t)
	// The proxy answers the public feed's address with a redirect to the local server.
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, local.URL, http.StatusFound) }))
	t.Cleanup(redirect.Close)
	setProxy(t, st, redirect.URL, "", "")
	client, _ := NewNetwork(st).Feed(context.Background(), store.SourceProxy{}, &http.Client{Timeout: time.Second})
	if _, err := body(t, client, "http://feeds.example.test/rss"); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("a redirect to a local host: %v", err)
	}
}
