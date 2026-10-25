package typist

import (
	"errors"
	"testing"
	"time"
)

// An Escape whose key did not go in is pressed again, backing off, and is given up only after interruptTries.
func TestAnInterruptIsPressedAgainWhenItsKeyFails(t *testing.T) {
	p := newPane("")
	p.fail = map[string]error{"keys:Escape": errors.New("tmux: busy")}
	c := &unfailingClock{fakeClock: newClock(), after: 3, p: p}
	if err := Interrupt(p, c); err != nil {
		t.Fatalf("given up: %v", err)
	}
	p.fail = map[string]error{"keys:Escape": errors.New("tmux: gone")}
	if err := Interrupt(p, newClock()); err == nil {
		t.Fatal("a key that never goes in reported pressed")
	}
}

// unfailingClock lets the pane's Escape go in after a number of waits.
type unfailingClock struct {
	*fakeClock
	after int
	p     *fakePane
}

func (c *unfailingClock) Sleep(d time.Duration) {
	c.fakeClock.Sleep(d)
	if c.after--; c.after == 0 {
		c.p.mu.Lock()
		delete(c.p.fail, "keys:Escape")
		c.p.mu.Unlock()
	}
}
