package convo

import (
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// times past that the turn cannot act on do not make the actor's clock due again at once — it would turn
// without a pause, a core of the runtime for one chat.
func TestWakeSkipsTimesTheTurnCannotMove(t *testing.T) {
	r := newRig(t)
	r.open()
	a := r.rt.actor(convID)
	if a == nil {
		t.Fatal("no actor")
	}
	c := a.c
	now := r.clock.Now()
	due := func() bool { return !c.wake(now).After(now) }
	if due() {
		t.Fatal("due to begin with")
	}

	// A deleted chat with a title due: watchTitle names no deleted chat and leaves the time as it is.
	c.titleDueAt = now.Add(-time.Minute)
	c.deletedAt = wire.Ms(now.UnixMilli())
	c.tick(now)
	if due() {
		t.Fatal("a deleted chat's title time keeps the clock due")
	}
	c.deletedAt, c.titleDueAt = 0, time.Time{}

	// A model change whose retry time is past while the agent is not at its prompt: the agent's state holds it.
	c.rec.PendingModel = &wire.PendingModel{ID: "opus"}
	c.pendingAt = now.Add(-time.Second)
	if due() {
		t.Fatal("a past retry time of a model change keeps the clock due")
	}
	c.pendingAt = now.Add(time.Second)
	if w := c.wake(now); !w.Equal(now.Add(time.Second)) {
		t.Fatalf("a model change's retry ahead is not woken for: %v", w.Sub(now))
	}
}
