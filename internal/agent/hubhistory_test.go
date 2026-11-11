package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

var historyEvents = []string{hubproto.EventHistoryEvents, hubproto.EventHistoryBackfill}

func startHistory(c *hubConn, cursors map[string]any) {
	c.mustCall(hubproto.OpHistoryStart, map[string]any{"enabled": true, "cursors": cursors, "known": map[string]any{}, "pending": map[string]any{}})
}

// An observe link keeps the chats with the cluster unless the operator opts in on the machine.
func TestHistoryOnAnObserveLinkNeedsTheOptIn(t *testing.T) {
	t.Setenv(historyOptIn, "")
	o := startHubAgent(t, rigOptions{observe: true})
	c := o.connect()
	hello := decode[hubproto.Hello](t, c.mustCall("hello", nil))
	if contains(hello.Features, hubproto.FeatureHistory) {
		t.Fatalf("the chats are offered without the opt-in: %v", hello.Features)
	}
	if _, e := c.call(hubproto.OpHistoryStart, map[string]any{"enabled": true}); e == "" {
		t.Fatal("history.start taken without the opt-in")
	}
	if o.agent.hubReplica.enabled {
		t.Fatal("the hub's replica was switched on without the opt-in")
	}

	t.Setenv(historyOptIn, "on")
	o2 := startHubAgent(t, rigOptions{observe: true})
	c2 := o2.connect()
	hello = decode[hubproto.Hello](t, c2.mustCall("hello", nil))
	if !contains(hello.Features, hubproto.FeatureHistory) {
		t.Fatalf("the opt-in did not offer the chats: %v", hello.Features)
	}
	// An own link's machine still offers them with no variable.
	t.Setenv(historyOptIn, "")
	own := startHubAgent(t, rigOptions{})
	if h := decode[hubproto.Hello](t, own.connect().mustCall("hello", nil)); !contains(h.Features, hubproto.FeatureHistory) {
		t.Fatalf("a hub's own machine lost its chats: %v", h.Features)
	}
}

// The kill switch takes the chats off an observe link, and the file taken away brings them back.
func TestHistoryOnAnObserveLinkStopsAtTheKillSwitch(t *testing.T) {
	t.Setenv(historyOptIn, "on")
	o := startHubAgent(t, rigOptions{observe: true})
	c := o.connect()
	c.mustCall("hello", nil)
	if !o.agent.hub.historyServed() {
		t.Fatal("not served with the opt-in")
	}
	off := o.agent.hub.policy.SwitchFiles()[0]
	if err := os.MkdirAll(filepath.Dir(off), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, off, "")
	o.agent.ReloadPolicy()
	if o.agent.hub.historyServed() {
		t.Fatal("served under the kill switch")
	}
	if err := os.Remove(off); err != nil {
		t.Fatal(err)
	}
	o.agent.ReloadPolicy()
	if !o.agent.hub.historyServed() {
		t.Fatal("not served again after the switch is off")
	}
}

// Frames of the hub's replica reach the hub under protocol 2's names, after its history.start and not before.
func TestHistoryReachesTheHubOfAnObserveLink(t *testing.T) {
	t.Setenv(historyOptIn, "on")
	o := startHubAgent(t, rigOptions{observe: true})
	c := o.connect()
	c.mustCall("hello", nil)
	c.mustCall(hubproto.OpLinkSubscribe, map[string]any{"events": historyEvents})
	rep := o.agent.hubReplica
	rep.hello("ep-1")
	rep.event("convs", 1, json.RawMessage(`{"type":"conversation_upsert","conv":{"id":"tab-a","rev":1}}`))
	if ev, _ := c.waitEvent(hubproto.EventHistoryEvents, 0, 300*time.Millisecond); ev != nil {
		t.Fatalf("a chat went before history.start: %v", ev)
	}
	startHistory(c, map[string]any{})
	rep.hello("ep-1")
	rep.event("convs", 1, json.RawMessage(`{"type":"conversation_upsert","conv":{"id":"tab-a","rev":1}}`))
	ev, _ := c.waitEvent(hubproto.EventHistoryEvents, 0, 10*time.Second)
	if ev == nil || string(ev["ch"]) != `"convs"` || string(ev["fromSeq"]) != "1" {
		t.Fatalf("history.events %v", ev)
	}
}

type clusterTap struct {
	mu     sync.Mutex
	frames []evV2Events
}

func (k *clusterTap) emit(ev any) {
	if e, ok := ev.(evV2Events); ok {
		k.mu.Lock()
		k.frames = append(k.frames, e)
		k.mu.Unlock()
	}
}

func (k *clusterTap) got() []evV2Events {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]evV2Events(nil), k.frames...)
}

// runClusterStream feeds a cluster's replica of an agent the same events, acks and a gap, with the hub's replica
// doing whatever `hub` says beside it, and returns what the cluster was sent and where its replica stands.
func runClusterStream(t *testing.T, hub func(o *hubRig, c *hubConn)) ([]evV2Events, replicaChan, int) {
	t.Helper()
	t.Setenv(historyOptIn, "on")
	o := startHubAgent(t, rigOptions{observe: true})
	c := o.connect()
	c.mustCall("hello", nil)
	c.mustCall(hubproto.OpLinkSubscribe, map[string]any{"events": historyEvents})
	tap := &clusterTap{}
	cl := o.agent.replica
	cl.emit, cl.connected = tap.emit, func() bool { return true }
	cl.hello("ep-9")
	cl.start(true, map[string]cursor{"conv:a": {Epoch: "ep-9", Seq: 2}}, nil, nil)
	cl.hello("ep-9")

	ev := func(i int) json.RawMessage { return json.RawMessage(`{"type":"x","n":` + string(rune('0'+i)) + `}`) }
	for i := 3; i <= 5; i++ {
		cl.event("conv:a", int64(i), ev(i))
	}
	hub(o, c)
	cl.ack("conv:a", "ep-9", 4, false)
	for i := 6; i <= 8; i++ {
		cl.event("conv:a", int64(i), ev(i))
	}
	cl.ack("conv:a", "ep-9", 5, true)
	hub(o, c)
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return tap.got(), *cl.chans["conv:a"], cl.generation
}

// The cluster's stream is what it was, whatever the hub's replica does beside it: it starts, acks, hits a hole, and
// loses its link. The hub's own protocol never moves the cluster's cursors, acks or baselines.
func TestHistoryLeavesTheClustersStreamAlone(t *testing.T) {
	alone, aloneChan, aloneGen := runClusterStream(t, func(*hubRig, *hubConn) {})
	if len(alone) == 0 {
		t.Fatal("the cluster's stream is empty: the test proves nothing")
	}

	withHub, hubChan, hubGen := runClusterStream(t, func(o *hubRig, c *hubConn) {
		startHistory(c, map[string]any{"conv:a": map[string]any{"epoch": "ep-9", "seq": 7}})
		hr := o.agent.hubReplica
		hr.hello("ep-9")
		for i := 1; i <= 3; i++ {
			hr.event("conv:a", int64(i), json.RawMessage(`{"type":"y"}`))
		}
		c.mustCall(hubproto.OpHistoryAck, map[string]any{"ch": "conv:a", "epoch": "ep-9", "ackSeq": 2, "gap": true})
		c.mustCall(hubproto.OpHistoryAck, map[string]any{"ch": "conv:a", "epoch": "ep-9", "ackSeq": 3})
	})
	if !reflect.DeepEqual(alone, withHub) {
		t.Fatalf("the cluster's frames changed with a hub beside it:\nalone %+v\nhub   %+v", alone, withHub)
	}
	if !reflect.DeepEqual(aloneChan, hubChan) || aloneGen != hubGen {
		t.Fatalf("the cluster's replica moved: %+v gen %d, with a hub %+v gen %d", aloneChan, aloneGen, hubChan, hubGen)
	}

	// The hub's link goes away in the middle of it: the cluster's stream is the same again.
	var once sync.Once
	dropped, droppedChan, droppedGen := runClusterStream(t, func(o *hubRig, c *hubConn) {
		once.Do(func() { dropHub(t, o, c) })
	})
	if !reflect.DeepEqual(alone, dropped) || !reflect.DeepEqual(aloneChan, droppedChan) || aloneGen != droppedGen {
		t.Fatalf("the cluster's stream changed when the hub's link went:\nalone %+v\ndrop  %+v", alone, dropped)
	}
}

func dropHub(t *testing.T, o *hubRig, c *hubConn) {
	startHistory(c, map[string]any{})
	o.agent.hubReplica.hello("ep-9")
	for _, l := range o.agent.hub.hubLinks() {
		l.closeAfterFlush(websocket.StatusGoingAway, "test")
	}
	waitFor(t, func() bool { return len(o.agent.hub.hubLinks()) == 0 })
	o.agent.hubReplica.event("conv:a", 1, json.RawMessage(`{"type":"y"}`))
}
