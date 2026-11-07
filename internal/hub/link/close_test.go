package link

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Close ends the links and returns only when their routines are done: the Connected hook still running and the
// Disconnected hook of the link it ended. What they write then reaches the database before the hub closes it. A dial
// after Close is refused.
func TestCloseWaitsForTheLinksRoutines(t *testing.T) {
	h := newTestHub(t)
	started := make(chan struct{})
	var connected, disconnected atomic.Bool
	h.dialer.Hooks.Connected = func(l *Link, _ store.Machine) {
		close(started)
		<-l.done                          // the hello fails when the link ends
		time.Sleep(50 * time.Millisecond) // and the routine still writes something after that
		connected.Store(true)
	}
	h.dialer.Hooks.Disconnected = func(*Link, store.Machine, bool) {
		time.Sleep(50 * time.Millisecond)
		disconnected.Store(true)
	}
	conn, status, body := h.dialRaw(dialOpts{})
	if conn == nil {
		t.Fatalf("dial refused: %d %s", status, body)
	}
	t.Cleanup(func() { conn.CloseNow() })
	go func() {
		for {
			if _, _, err := conn.Read(context.Background()); err != nil {
				return
			}
		}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("the hub did not register the link")
	}

	h.dialer.Close()
	if !connected.Load() || !disconnected.Load() {
		t.Errorf("Close returned before the routines: connected %v, disconnected %v", connected.Load(), disconnected.Load())
	}
	if h.reg.Get(h.machine.ID) != nil {
		t.Error("the registry still holds the link")
	}
	if conn, status, _ := h.dialRaw(dialOpts{}); conn != nil || status != 503 {
		t.Errorf("a dial after Close: %d", status)
	}
}
