package server

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// freeAttempts is how many wrong passwords a client address may send before it is locked out.
	freeAttempts = 5
	// firstLockout is the lockout after the first attempt over the free ones; each further failure doubles it.
	firstLockout = time.Second
	// maxLockout caps the lockout, so an owner who mistyped is never shut out for long.
	maxLockout = 15 * time.Minute
	// forgetAfter is how long a quiet address is remembered before its failures are forgotten.
	forgetAfter = time.Hour
	// maxDerivations is how many password keys may be derived at once; a login that finds them all busy is turned away.
	maxDerivations = 2
	// maxTracked bounds the failure records kept in memory.
	maxTracked = 10_000
	// pruneEvery is the longest time the expired records stay when requests keep coming, wrong passwords or not.
	pruneEvery = time.Minute
)

// throttle slows down repeated wrong passwords per client address and caps the key derivations in flight.
type throttle struct {
	mu       sync.Mutex
	failures map[string]*failure
	slots    chan struct{}
	pruned   time.Time
}

type failure struct {
	count int
	until time.Time // locked out until then
	last  time.Time
}

func newThrottle() *throttle {
	return &throttle{failures: map[string]*failure{}, slots: make(chan struct{}, maxDerivations)}
}

// ErrProxyFormat is what ParseProxies returns for an item that is neither an IP address nor a CIDR range.
var ErrProxyFormat = errors.New("not an IP address or CIDR range")

// ParseProxies reads a comma-separated list of IP addresses and CIDR ranges, the reverse proxies whose
// X-Forwarded-For is believed. An empty list is no proxy. A /0 range is refused: it would trust every client.
func ParseProxies(list string) ([]netip.Prefix, error) {
	var proxies []netip.Prefix
	for _, item := range strings.Split(list, ",") {
		if item = strings.TrimSpace(item); item == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			addr, addrErr := netip.ParseAddr(item)
			if addrErr != nil {
				return nil, fmt.Errorf("%w: %s", ErrProxyFormat, item)
			}
			prefix = netip.PrefixFrom(addr.WithZone(""), addr.BitLen())
		}
		if prefix.Addr().Is4In6() {
			// ::ffff:10.0.0.0/104 is 10.0.0.0/8: the same address as the peer shows after Unmap.
			if prefix.Bits() < 96 {
				return nil, errors.New("range too wide for an IPv4-mapped address: " + item)
			}
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
		}
		if prefix.Bits() == 0 {
			return nil, errors.New("a /0 range would trust every client: " + item)
		}
		proxies = append(proxies, prefix.Masked())
	}
	return proxies, nil
}

// normalize is the form of an address that is compared and used as a key: IPv4 unmapped, no zone.
func normalize(addr netip.Addr) netip.Addr { return addr.Unmap().WithZone("") }

func trusted(proxies []netip.Prefix, addr netip.Addr) bool {
	addr = normalize(addr)
	for _, p := range proxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// parseHop reads one X-Forwarded-For item: an address, with or without a port, IPv6 with or without brackets.
func parseHop(item string) (netip.Addr, bool) {
	item = strings.TrimSpace(item)
	if ap, err := netip.ParseAddrPort(item); err == nil {
		return normalize(ap.Addr()), true
	}
	addr, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(item, "["), "]"))
	return normalize(addr), err == nil
}

// clientKey is the address a login came from, without the port. When RemoteAddr is a configured proxy it is the
// rightmost X-Forwarded-For hop that is not a proxy itself. A missing or unreadable header, or a chain of nothing but
// proxies, leaves the address of the peer.
func clientKey(r *http.Request, proxies []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = normalize(peer)
	if len(proxies) == 0 {
		return peer.String()
	}
	if !trusted(proxies, peer) {
		return peer.String()
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop, ok := parseHop(hops[i])
		if !ok {
			return peer.String()
		}
		if !trusted(proxies, hop) {
			return hop.String()
		}
	}
	return peer.String()
}

// wait returns how long the client is still locked out, 0 if it may try.
func (t *throttle) wait(key string, now time.Time) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prune(now)
	if f := t.failures[key]; f != nil && now.Before(f.until) {
		return f.until.Sub(now)
	}
	return 0
}

// fail records a wrong password and starts or lengthens the lockout once the free attempts are used.
func (t *throttle) fail(key string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prune(now)
	f := t.failures[key]
	if f == nil {
		if len(t.failures) >= maxTracked {
			t.evict(now)
		}
		f = &failure{}
		t.failures[key] = f
	}
	f.count++
	f.last = now
	if over := f.count - freeAttempts; over > 0 {
		lockout := maxLockout
		if over <= 20 && firstLockout<<(over-1) < maxLockout {
			lockout = firstLockout << (over - 1)
		}
		f.until = now.Add(lockout)
	}
}

// succeed forgets the failures of a client that signed in.
func (t *throttle) succeed(key string) {
	t.mu.Lock()
	delete(t.failures, key)
	t.mu.Unlock()
}

// prune drops records that are no longer locked and have been quiet for a while. It scans at most once per
// pruneEvery, whoever asks, so it does not depend on failures coming in.
func (t *throttle) prune(now time.Time) {
	if now.Sub(t.pruned) < pruneEvery {
		return
	}
	t.pruned = now
	for key, f := range t.failures {
		if now.After(f.until) && now.Sub(f.last) > forgetAfter {
			delete(t.failures, key)
		}
	}
}

// evict makes room at the cap by dropping the record that has been quiet longest, one that is not locked out
// before any that is.
func (t *throttle) evict(now time.Time) {
	var oldest string
	var found, oldestLocked bool
	var oldestLast time.Time
	for key, f := range t.failures {
		locked := now.Before(f.until)
		if !found || (oldestLocked && !locked) || (locked == oldestLocked && f.last.Before(oldestLast)) {
			oldest, oldestLast, oldestLocked, found = key, f.last, locked, true
		}
	}
	delete(t.failures, oldest)
}

// acquire takes a derivation slot without waiting; false means all are busy.
func (t *throttle) acquire() bool {
	select {
	case t.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (t *throttle) release() { <-t.slots }

// tooMany answers 429 and tells the client when to come back.
func tooMany(w http.ResponseWriter, wait time.Duration) {
	seconds := int((wait + time.Second - 1) / time.Second)
	w.Header().Set("Retry-After", strconv.Itoa(max(seconds, 1)))
	fail(w, http.StatusTooManyRequests, "too_many_attempts")
}
