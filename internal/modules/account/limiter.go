package account

import (
	"net/netip"
	"sync"
	"time"
)

// Limiter counts failures per key in a fixed window and blocks a key that reached the
// limit until its window ends. It is per process; with several replicas each one
// counts on its own, which only makes the limit proportionally looser.
type Limiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	hits    map[string]*bucket
	sweepAt int
}

type bucket struct {
	n     int
	start time.Time
}

// NewLimiter blocks a key after max failures within window.
func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, now: time.Now, hits: map[string]*bucket{}, sweepAt: 1024}
}

// Blocked reports whether key is over the limit and how long until it is not.
func (l *Limiter) Blocked(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.hits[key]
	if b == nil {
		return 0, false
	}
	left := b.start.Add(l.window).Sub(l.now())
	if left <= 0 {
		delete(l.hits, key)
		return 0, false
	}
	return left, b.n >= l.max
}

// Take counts one attempt for key unless key is already at the limit, in one step,
// and reports like Blocked. Callers that check before expensive work (a password
// hash) and count a failure after it must use this instead of Blocked and Fail:
// requests arriving together would all pass Blocked before any of them was counted,
// so a burst got as many tries as it had requests, whatever the limit (2026-10-30
// review: 200 concurrent logins, 200 password checks, limit 10). Release takes the
// attempt back when it turns out not to count.
func (l *Limiter) Take(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if b := l.hits[key]; b != nil && now.Sub(b.start) < l.window && b.n >= l.max {
		return b.start.Add(l.window).Sub(now), true
	}
	l.add(key, now)
	return 0, false
}

// Release forgets one attempt counted by Take (or Fail) in key's current window.
func (l *Limiter) Release(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if b := l.hits[key]; b != nil && b.n > 0 && l.now().Sub(b.start) < l.window {
		b.n--
	}
}

// Fail records one failure for key.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.add(key, l.now())
}

func (l *Limiter) add(key string, now time.Time) {
	b := l.hits[key]
	if b == nil || now.Sub(b.start) >= l.window {
		b = &bucket{start: now}
		l.hits[key] = b
	}
	b.n++
	if len(l.hits) >= l.sweepAt {
		// Bound memory under a flood of distinct keys: drop finished windows, and let
		// the map grow again only relative to what is still live.
		for k, v := range l.hits {
			if now.Sub(v.start) >= l.window {
				delete(l.hits, k)
			}
		}
		l.sweepAt = max(1024, 2*len(l.hits))
	}
}

// Count is the number of events recorded for key in its current window.
func (l *Limiter) Count(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.hits[key]
	if b == nil || l.now().Sub(b.start) >= l.window {
		return 0
	}
	return b.n
}

// Reset forgets key, e.g. after a successful login.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	delete(l.hits, key)
	l.mu.Unlock()
}

// ClientKey is the limiter key for a client address (see ipKey), for limits kept
// outside this package.
func ClientKey(ip string) string { return ipKey(ip) }

// ipKey groups IPv6 clients by /64, the smallest block one subscriber usually gets,
// so rotating addresses inside it does not reset the count.
func ipKey(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	a = a.Unmap()
	if a.Is6() {
		p, _ := a.Prefix(64)
		return p.String()
	}
	return a.String()
}
