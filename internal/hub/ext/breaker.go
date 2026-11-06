package ext

import (
	"sync"
	"time"
)

// Breaker is a circuit breaker: after Failures calls in a row that failed it lets nothing through for Pause (a call
// then answers ErrCircuitOpen at once, so a dead adapter does not hold a request for the whole timeout and the attempts),
// then lets one call try; if that one succeeds the circuit closes, if it fails the pause starts again.
type Breaker struct {
	failures int
	pause    time.Duration
	now      func() time.Time

	mu       sync.Mutex
	count    int
	openedAt time.Time
	open     bool
	probing  bool
}

// BreakerState is what the breaker is doing.
type BreakerState string

const (
	StateClosed   BreakerState = "closed"
	StateOpen     BreakerState = "open"
	StateHalfOpen BreakerState = "half-open"
)

// NewBreaker makes a breaker; zero values mean 5 failures and a 30 second pause.
func NewBreaker(failures int, pause time.Duration) *Breaker {
	if failures == 0 {
		failures = 5
	}
	if pause <= 0 {
		pause = 30 * time.Second
	}
	return &Breaker{failures: failures, pause: pause, now: time.Now}
}

// Allow says whether a call may go. After the pause it lets exactly one call through (the probe).
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.open {
		return true
	}
	if b.now().Sub(b.openedAt) < b.pause || b.probing {
		return false
	}
	b.probing = true
	return true
}

// Record tells the breaker how an allowed call ended.
func (b *Breaker) Record(ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.probing = false
	if ok {
		b.count, b.open = 0, false
		return
	}
	b.count++
	if b.open || b.count >= b.failures {
		b.open, b.openedAt = true, b.now()
	}
}

// State is the state now.
func (b *Breaker) State() BreakerState {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case !b.open:
		return StateClosed
	case b.probing || b.now().Sub(b.openedAt) >= b.pause:
		return StateHalfOpen
	}
	return StateOpen
}
