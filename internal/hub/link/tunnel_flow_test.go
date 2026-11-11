package link

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// tunnelLimit gives the test a memory for tunnels of its own, of this many bytes.
func tunnelLimit(t *testing.T, bytes int64) {
	t.Helper()
	old := tunnelMem
	tunnelMem = &tunnelMemory{}
	SetTunnelMemory(bytes)
	t.Cleanup(func() { tunnelMem = old })
}

// closeSeen waits until the agent is told the tunnel is closed. A window frame on the way fails the test: it is
// called for agents that know no window.
func closeSeen(t *testing.T, a *agent, tn *Tunnel) {
	t.Helper()
	for {
		select {
		case f := <-a.binary:
			if f[0] == frameWindow {
				t.Fatal("a window frame went to an agent that knows none")
			}
			if f[0] == frameClose && string(f[1:1+tunnelID]) == string(tn.id[:]) {
				return
			}
		case <-time.After(3 * time.Second):
			t.Fatal("the agent was not told of the close")
		}
	}
}

// flowAgent is how the fake agent answers an opening: as an agent that keeps to the window, or with old set as one
// that has never heard of it. offered is the window the hub offered last.
type flowAgent struct {
	old     atomic.Bool
	offered atomic.Int64
}

func newFlowAgent(a *agent) *flowAgent {
	f := &flowAgent{}
	a.handle = func(op string, req map[string]json.RawMessage) (any, string) {
		if op != "tunnel.open" {
			return map[string]any{}, ""
		}
		var spec struct{ Window int64 }
		_ = json.Unmarshal(req["spec"], &spec)
		f.offered.Store(spec.Window)
		if f.old.Load() {
			return map[string]bool{"opened": true}, ""
		}
		return map[string]bool{"opened": true, "flow": true}, ""
	}
	return f
}

// granted waits for window frames of the tunnel until they add up to at least want.
func granted(t *testing.T, a *agent, want int64) int64 {
	t.Helper()
	var sum int64
	for sum < want {
		select {
		case f := <-a.binary:
			if f[0] == frameWindow && len(f) == 1+tunnelID+4 {
				sum += int64(binary.BigEndian.Uint32(f[1+tunnelID:]))
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("granted %d of %d", sum, want)
		}
	}
	return sum
}

func send(t *testing.T, a *agent, tn *Tunnel, n int) {
	t.Helper()
	if err := a.conn.Write(context.Background(), websocket.MessageBinary, frameOf(frameData, tn.key, make([]byte, n))); err != nil {
		t.Fatal(err)
	}
}

func readN(t *testing.T, tn *Tunnel, n int) {
	t.Helper()
	_ = tn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.ReadFull(tn, make([]byte, n)); err != nil {
		t.Fatalf("read %d: %v", n, err)
	}
}

func waitCut(t *testing.T, tn *Tunnel) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	buf := make([]byte, 64*1024)
	for {
		_ = tn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		_, err := tn.Read(buf)
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the tunnel was not cut: %v", err)
		}
	}
}

func waitUsed(t *testing.T, want int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for TunnelMemory().Used != want {
		if time.Now().After(deadline) {
			t.Fatalf("tunnels hold %d, want %d", TunnelMemory().Used, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTunnelWindowIsGrantedAsTheReaderTakes(t *testing.T) {
	h := newTestHub(t)
	a, l := h.agent(dialOpts{})
	fa := newFlowAgent(a)
	tunnelLimit(t, DefaultTunnelMemory)
	const base = 0
	tn, err := l.OpenTunnel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := fa.offered.Load(); got != tunnelWindowStart {
		t.Fatalf("offered a window of %d", got)
	}
	if got := TunnelMemory().Used - base; got != tunnelWindowStart {
		t.Fatalf("an open tunnel holds %d", got)
	}

	// The agent fills the window; nothing is granted until the reader takes half of it.
	send(t, a, tn, tunnelWindowStart)
	readN(t, tn, tunnelWindowStart/4)
	select {
	case f := <-a.binary:
		t.Fatalf("granted before half the window was read: kind %d", f[0])
	case <-time.After(200 * time.Millisecond):
	}
	readN(t, tn, tunnelWindowStart/4)
	if got := granted(t, a, 1); got != tunnelWindowStart/2 {
		t.Fatalf("granted %d for half a window read", got)
	}

	// A reader that empties the queue gets a wider window, and the hub holds exactly the window.
	readN(t, tn, tunnelWindowStart/2)
	if got := granted(t, a, 1); got != tunnelWindowStart/2+tunnelWindowStart {
		t.Fatalf("granted %d after the queue emptied", got)
	}
	if got := TunnelMemory().Used - base; got != 2*tunnelWindowStart {
		t.Fatalf("a grown tunnel holds %d", got)
	}

	// Sending past the window costs the agent the tunnel, not the link.
	cut := TunnelMemory().Cut
	send(t, a, tn, 2*tunnelWindowStart+1)
	waitCut(t, tn)
	if TunnelMemory().Cut != cut+1 {
		t.Errorf("cut tunnels: %d", TunnelMemory().Cut-cut)
	}
	if _, err := l.Call(context.Background(), "hello", nil); err != nil {
		t.Errorf("the link died with the tunnel: %v", err)
	}
	_ = tn.Close()
	waitUsed(t, base)
}

func TestTunnelWindowShrinksWhenMemoryIsTight(t *testing.T) {
	h := newTestHub(t)
	a, l := h.agent(dialOpts{})
	newFlowAgent(a)
	const base = 0
	tunnelLimit(t, 4*tunnelWindowStart)
	tn, err := l.OpenTunnel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tn.Close()
	// Grow to two windows' worth.
	send(t, a, tn, tunnelWindowStart)
	readN(t, tn, tunnelWindowStart)
	granted(t, a, 2*tunnelWindowStart)
	if got := TunnelMemory().Used - base; got != 2*tunnelWindowStart {
		t.Fatalf("holds %d", got)
	}
	// Another tunnel takes the budget past three quarters: what this one reads next is given back, not granted.
	other, err := l.OpenTunnel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	send(t, a, tn, 2*tunnelWindowStart)
	readN(t, tn, 2*tunnelWindowStart)
	if got := granted(t, a, 1); got != tunnelWindowStart {
		t.Fatalf("granted %d while memory is tight", got)
	}
	if got := TunnelMemory().Used - base; got != 2*tunnelWindowStart {
		t.Fatalf("two tunnels hold %d", got)
	}
}

func TestTunnelMemoryCeiling(t *testing.T) {
	h := newTestHub(t)
	a, l := h.agent(dialOpts{})
	fa := newFlowAgent(a)
	tunnelLimit(t, 2*tunnelWindowStart)

	// No more tunnels than the memory has room for.
	first, err := l.OpenTunnel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := l.OpenTunnel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.OpenTunnel(context.Background()); !errors.Is(err, ErrTunnelMemory) {
		t.Fatalf("a third tunnel: %v", err)
	}
	if got := TunnelMemory(); got.Refused != 1 || got.Used != got.Limit {
		t.Errorf("%+v", got)
	}
	_ = first.Close()
	_ = second.Close()
	waitUsed(t, 0)
	for len(a.binary) > 0 {
		<-a.binary
	}

	// An agent without a window fills the memory and loses the tunnel there, long before the queue's own bound.
	fa.old.Store(true)
	old, err := l.OpenTunnel(context.Background())
	if err != nil {
		t.Fatalf("after the memory was freed: %v", err)
	}
	waitUsed(t, 0) // an idle tunnel of such an agent holds nothing
	send(t, a, old, 2*tunnelWindowStart)
	waitUsed(t, 2*tunnelWindowStart)
	send(t, a, old, 1)
	// Reading would make room, so the reader comes only after the byte that did not fit.
	for deadline := time.Now().Add(5 * time.Second); TunnelMemory().Cut != 1; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("cut tunnels: %d", TunnelMemory().Cut)
		}
	}
	waitUsed(t, 0)
	waitCut(t, old)
	closeSeen(t, a, old)
	if _, err := l.Call(context.Background(), "hello", nil); err != nil {
		t.Errorf("the link died with the tunnel: %v", err)
	}
}

func TestAgentWithoutWindowIsReadAsBefore(t *testing.T) {
	h := newTestHub(t)
	a, l := h.agent(dialOpts{})
	tunnelLimit(t, DefaultTunnelMemory)
	const base = 0
	tn, err := l.OpenTunnel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Far more than a window, nobody reading yet: it is all kept and then read whole.
	const total = 4 << 20
	for sent := 0; sent < total; sent += 1 << 20 {
		send(t, a, tn, 1<<20)
	}
	waitUsed(t, base+total)
	readN(t, tn, total)
	waitUsed(t, base)
	_ = tn.Close()
	closeSeen(t, a, tn)
}
