package vaultsvc

import (
	"sync"
	"time"
)

type rule struct {
	limit  int
	window time.Duration
}

// limiter: sliding windows per key, every rule at once. A refused attempt takes no slot.
type limiter struct {
	mu     sync.Mutex
	rules  []rule
	stamps map[string][]time.Time
}

const maxKeys = 10_000

func newLimiter(r []rule) *limiter { return &limiter{rules: r, stamps: map[string][]time.Time{}} }

func (l *limiter) take(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var longest time.Duration
	for _, r := range l.rules {
		if r.window > longest {
			longest = r.window
		}
	}
	for k, list := range l.stamps {
		if len(list) == 0 || now.Sub(list[len(list)-1]) >= longest {
			delete(l.stamps, k)
		}
	}
	var recent []time.Time
	for _, at := range l.stamps[key] {
		if now.Sub(at) < longest {
			recent = append(recent, at)
		}
	}
	var wait time.Duration
	for _, r := range l.rules {
		var in []time.Time
		for _, at := range recent {
			if now.Sub(at) < r.window {
				in = append(in, at)
			}
		}
		if len(in) >= r.limit {
			if w := in[len(in)-r.limit].Add(r.window).Sub(now); w > wait {
				wait = w
			}
		}
	}
	if wait > 0 {
		l.stamps[key] = recent
		return false, wait
	}
	if len(l.stamps) >= maxKeys && l.stamps[key] == nil {
		return false, time.Second
	}
	l.stamps[key] = append(recent, now)
	return true, 0
}
