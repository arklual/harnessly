package link

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Bulk events that the hub does not take in time are dropped: the link stays, the other events are handled meanwhile,
// and the hub is told of the loss once the queue is taken.
func TestBulkEventsBehindAreDroppedAndTheLinkStays(t *testing.T) {
	h := newTestHub(t)
	release := make(chan struct{})
	var bulk, lost atomic.Int32
	h.dialer.Hooks.Bulk = func(name string) bool { return name == "history.events" }
	h.dialer.Hooks.BulkLost = func(*Link, store.Machine) { lost.Add(1) }
	h.dialer.Hooks.Event = func(_ *Link, _ store.Machine, ev Event) {
		if ev.Name == "history.events" {
			<-release
			bulk.Add(1)
			return
		}
		h.events <- ev
	}
	a, l := h.agent(dialOpts{})
	const sent = bulkQueue + 100
	for i := range sent {
		a.emit(t, fmt.Sprintf(`{"event":"history.events","n":%d}`, i))
	}
	a.emit(t, `{"event":"policy"}`)
	select {
	case ev := <-h.events:
		if ev.Name != "policy" {
			t.Fatalf("event %q", ev.Name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("an ordinary event waited behind the bulk ones")
	}
	if l.Closed() {
		t.Fatal("the link was closed for being behind")
	}
	if lost.Load() != 0 {
		t.Error("told of the loss before the queue was taken")
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for lost.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if lost.Load() != 1 {
		t.Fatalf("told of the loss %d times, want once", lost.Load())
	}
	if got := bulk.Load(); got < bulkQueue || got >= sent {
		t.Errorf("%d of %d bulk events handled, want the queued %d or one more", got, sent, bulkQueue)
	}
	if l.Closed() {
		t.Error("the link was closed")
	}
}

// The other events keep the old rule: a link whose events nobody takes is closed, and the agent calls again.
func TestOrdinaryEventsBehindStillCloseTheLink(t *testing.T) {
	h := newTestHub(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	h.dialer.Hooks.Event = func(*Link, store.Machine, Event) { <-release }
	a, l := h.agent(dialOpts{})
	for range eventQueue + 50 {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := a.conn.Write(ctx, websocket.MessageText, []byte(`{"event":"policy"}`))
		cancel()
		if err != nil {
			break
		}
	}
	select {
	case <-l.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the link was not closed")
	}
}
