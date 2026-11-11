package link

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

func (a *agent) emit(t *testing.T, frame string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
		t.Fatal(err)
	}
}

// A handler that panics on what a machine sent loses that event, not the hub: the next event is handled and the link
// stays.
func TestPanicInAnEventHandlerDoesNotEndTheLink(t *testing.T) {
	h := newTestHub(t)
	h.dialer.Hooks.Event = func(_ *Link, _ store.Machine, ev Event) {
		if ev.Name == "bad" {
			panic("a handler that trusted the frame")
		}
		h.events <- ev
	}
	a, l := h.agent(dialOpts{})
	a.emit(t, `{"event":"bad"}`)
	a.emit(t, `{"event":"good"}`)
	select {
	case ev := <-h.events:
		if ev.Name != "good" {
			t.Fatalf("event %q", ev.Name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the event after the panic was not handled")
	}
	if l.Closed() {
		t.Error("the link was closed")
	}
}
