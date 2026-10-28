// Package ratelimit is the hub's in-memory abuse limiter. It is per process, which is right for the one replica the
// hub runs as (docs/hub/architecture.md).
package ratelimit

import (
	"net/netip"
	"sync"
	"time"
)

// Limiter counts events per key in a sliding window.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	calls   int
	now     func() time.Time
}

type bucket struct {
	hits   []time.Time
	window time.Duration
}

// New makes a Limiter.
func New() *Limiter { return &Limiter{buckets: map[string]*bucket{}, now: time.Now} }

// WithClock replaces the clock; for tests.
func (l *Limiter) WithClock(now func() time.Time) *Limiter {
	l.now = now
	return l
}

// Allow records one event under key and says whether it fits in limit events per window. When it does not, it is not
// recorded and retryAfter says when the oldest event leaves the window.
func (l *Limiter) Allow(key string, limit int, window time.Duration) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if l.calls++; l.calls%512 == 0 {
		l.sweep(now)
	}
	b := l.buckets[key]
	if b == nil {
		b = &bucket{}
		l.buckets[key] = b
	}
	b.window = window
	keep := b.hits[:0]
	for _, t := range b.hits {
		if now.Sub(t) < window {
			keep = append(keep, t)
		}
	}
	b.hits = keep
	if len(b.hits) >= limit {
		return false, b.hits[0].Add(window).Sub(now)
	}
	b.hits = append(b.hits, now)
	return true, 0
}

// Peek is Allow without recording.
func (l *Limiter) Peek(key string, limit int, window time.Duration) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b := l.buckets[key]
	if b == nil {
		return true, 0
	}
	n := 0
	var oldest time.Time
	for _, t := range b.hits {
		if now.Sub(t) < window {
			if n == 0 {
				oldest = t
			}
			n++
		}
	}
	if n >= limit {
		return false, oldest.Add(window).Sub(now)
	}
	return true, 0
}

// Reset forgets key.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	delete(l.buckets, key)
	l.mu.Unlock()
}

func (l *Limiter) sweep(now time.Time) {
	for k, b := range l.buckets {
		if len(b.hits) == 0 || now.Sub(b.hits[len(b.hits)-1]) >= b.window {
			delete(l.buckets, k)
		}
	}
}

// Size is the number of keys held; for tests.
func (l *Limiter) Size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// IPKey is the key an address is limited under: an IPv4 address as it is, an IPv6 one by its /64, since one machine
// or one customer owns a whole /64 and rotating inside it would otherwise beat any per-address limit.
func IPKey(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	a = a.Unmap()
	if a.Is6() {
		return netip.PrefixFrom(a, 64).Masked().String()
	}
	return a.String()
}

// RetrySeconds rounds a wait up to whole seconds, at least one.
func RetrySeconds(d time.Duration) int {
	s := int((d + time.Second - 1) / time.Second)
	if s < 1 {
		return 1
	}
	return s
}
