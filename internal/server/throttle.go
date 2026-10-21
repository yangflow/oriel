package server

import (
	"net"
	"net/http"
	"strconv"
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
)

// throttle slows down repeated wrong passwords per client address and caps the key derivations in flight.
type throttle struct {
	mu       sync.Mutex
	failures map[string]*failure
	slots    chan struct{}
}

type failure struct {
	count int
	until time.Time // locked out until then
	last  time.Time
}

func newThrottle() *throttle {
	return &throttle{failures: map[string]*failure{}, slots: make(chan struct{}, maxDerivations)}
}

// clientKey is the address a login came from, without the port.
func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// wait returns how long the client is still locked out, 0 if it may try.
func (t *throttle) wait(key string, now time.Time) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
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
			return
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

// prune drops records that are no longer locked and have been quiet for a while.
func (t *throttle) prune(now time.Time) {
	for key, f := range t.failures {
		if now.After(f.until) && now.Sub(f.last) > forgetAfter {
			delete(t.failures, key)
		}
	}
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
