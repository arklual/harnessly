package agent

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

type replicaRig struct {
	t      *testing.T
	r      *replica
	mu     sync.Mutex
	frames []any
	online bool
	asked  int
	pages  map[string][]string // conv → page bodies, newest page first
}

func newReplicaRig(t *testing.T) *replicaRig {
	g := &replicaRig{t: t, online: true, pages: map[string][]string{}}
	g.r = &replica{
		emit:      func(ev any) { g.mu.Lock(); g.frames = append(g.frames, ev); g.mu.Unlock() },
		connected: func() bool { return g.online },
		askConvs:  func() { g.asked++ },
		pause:     time.Millisecond,
		chans:     map[string]*replicaChan{},
	}
	g.r.pages = func(conv, before string, limit int) (json.RawMessage, error) {
		list := g.pages[conv]
		if len(list) == 0 {
			return nil, fmt.Errorf("no such chat")
		}
		at := 0
		if before != "" {
			_, _ = fmt.Sscanf(before, "b%d", &at)
		}
		if at >= len(list) {
			return nil, fmt.Errorf("no such page")
		}
		return json.RawMessage(list[at]), nil
	}
	g.r.entries = func(conv, turn string) (json.RawMessage, error) {
		return json.RawMessage(`{"turnId":"` + turn + `","entries":[{"id":"step-` + turn + `"}]}`), nil
	}
	return g
}

func (g *replicaRig) events() []evV2Events {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []evV2Events
	for _, f := range g.frames {
		if e, ok := f.(evV2Events); ok {
			out = append(out, e)
		}
	}
	return out
}

func (g *replicaRig) backfills() []evV2Backfill {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []evV2Backfill
	for _, f := range g.frames {
		if e, ok := f.(evV2Backfill); ok {
			out = append(out, e)
		}
	}
	return out
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func (g *replicaRig) until(check func() bool) {
	g.t.Helper()
	for i := 0; i < 500 && !check(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if !check() {
		g.t.Fatal("condition not reached")
	}
}

func TestReplicaSendsNothingUntilTheClusterSwitchesItOn(t *testing.T) {
	g := newReplicaRig(t)
	g.r.hello("ep-1")
	g.r.event("convs", 1, raw(`{"type":"conversation_seen"}`))
	if len(g.events()) != 0 || g.asked != 0 {
		t.Fatalf("sent while off: %v asked %d", g.events(), g.asked)
	}
	g.r.start(true, nil, nil)
	if g.asked != 1 {
		t.Fatalf("the chat list is asked for once the switch is on: %d", g.asked)
	}
	g.r.start(false, nil, nil)
	g.r.event("convs", 2, raw(`{}`))
	if len(g.events()) != 0 {
		t.Fatal("sent after it was switched off")
	}
}

func TestReplicaSendsEventsInOrderAndKeepsThemUntilAcknowledged(t *testing.T) {
	g := newReplicaRig(t)
	g.r.hello("ep-1")
	g.r.start(true, nil, nil)
	g.r.event("convs", 1, raw(`{"n":1}`))
	g.r.event("convs", 2, raw(`{"n":2}`))
	got := g.events()
	if len(got) != 2 || got[0].FromSeq != 1 || got[1].FromSeq != 2 || got[0].Epoch != "ep-1" || got[0].Reset {
		t.Fatalf("frames %+v", got)
	}
	// The cluster did not take the second: it says where it stands, and the agent sends from there again.
	g.r.ack("convs", "ep-1", 1, true)
	got = g.events()
	if len(got) != 3 || got[2].FromSeq != 2 || got[2].Reset || len(got[2].Events) != 1 {
		t.Fatalf("resend %+v", got)
	}
	g.r.ack("convs", "ep-1", 2, false)
	if len(g.r.chans["convs"].events) != 0 {
		t.Fatal("an acknowledged event is kept")
	}
}

func TestReplicaStartsANewBaselineWhenItCannotResend(t *testing.T) {
	g := newReplicaRig(t)
	g.r.hello("ep-1")
	g.r.start(true, nil, nil)
	for seq := int64(1); seq <= 3; seq++ {
		g.r.event("convs", seq, raw(`{}`))
	}
	g.r.ack("convs", "ep-1", 3, false)
	g.r.event("convs", 4, raw(`{}`))
	g.r.ack("convs", "ep-1", 4, false)
	// The cluster is behind what was forgotten (it was restored from a backup): the frame is a baseline.
	g.r.event("convs", 5, raw(`{}`))
	g.r.ack("convs", "ep-1", 1, true)
	last := g.events()[len(g.events())-1]
	if last.FromSeq != 5 || !last.Reset {
		t.Fatalf("expected a baseline from 5: %+v", last)
	}
}

func TestReplicaResumesFromWhereTheClusterStands(t *testing.T) {
	g := newReplicaRig(t)
	g.r.hello("ep-1")
	g.r.start(true, map[string]cursor{"convs": {Epoch: "ep-1", Seq: 7}, "conv:tab-a": {Epoch: "ep-0", Seq: 3}}, nil)
	g.r.event("convs", 8, raw(`{}`))
	g.r.event("conv:tab-a", 1, raw(`{}`))
	g.r.event("conv:tab-b", 9, raw(`{}`))
	got := g.events()
	if len(got) != 3 {
		t.Fatalf("frames %+v", got)
	}
	if got[0].Reset || got[0].FromSeq != 8 {
		t.Fatalf("same epoch, next in line: %+v", got[0])
	}
	if got[1].Reset || got[1].FromSeq != 1 {
		t.Fatalf("another epoch starts the count again: %+v", got[1])
	}
	if got[2].Reset || got[2].FromSeq != 9 {
		t.Fatalf("a chat the cluster never had starts where it is: %+v", got[2])
	}
	g.r.start(true, map[string]cursor{"convs": {Epoch: "ep-1", Seq: 2}}, nil)
	g.r.event("convs", 20, raw(`{}`))
	last := g.events()[3]
	if !last.Reset || last.FromSeq != 20 {
		t.Fatalf("a hole between the cluster and the first event kept is a baseline: %+v", last)
	}
}

func TestReplicaTakesASkippedSeqAsABrokenRun(t *testing.T) {
	g := newReplicaRig(t)
	g.r.hello("ep-1")
	g.r.start(true, nil, nil)
	g.r.event("convs", 1, raw(`{}`))
	g.r.event("convs", 5, raw(`{}`)) // the runtime closed the replica for being behind and it came back
	got := g.events()
	if len(got) != 2 || !got[1].Reset || got[1].FromSeq != 5 {
		t.Fatalf("frames %+v", got)
	}
}

func TestReplicaHoldsWhileNoClusterLinkIsUp(t *testing.T) {
	g := newReplicaRig(t)
	g.r.hello("ep-1")
	g.r.start(true, nil, nil)
	g.online = false
	g.r.event("convs", 1, raw(`{}`))
	if len(g.events()) != 0 {
		t.Fatal("sent with no link")
	}
	g.online = true
	g.r.start(true, nil, nil) // the link is back: the cluster says where it stands, the next event carries on
	g.r.event("convs", 2, raw(`{}`))
	if got := g.events(); len(got) != 1 || got[0].FromSeq != 2 {
		t.Fatalf("frames %+v", got)
	}
}

func TestReplicaBacksUpTheChatsTheClusterLacks(t *testing.T) {
	g := newReplicaRig(t)
	g.r.hello("ep-1")
	page := func(before string, turns ...string) string {
		b := `null`
		if before != "" {
			b = `"` + before + `"`
		}
		ts := ""
		for i, id := range turns {
			if i > 0 {
				ts += ","
			}
			ts += `{"id":"` + id + `"}`
		}
		return `{"turns":[` + ts + `],"entries":[{"id":"` + turns[0] + `"}],"beforeTurnId":` + b + `}`
	}
	g.pages["tab-new"] = []string{page("b1", "c:t3#0", "c:t4#0"), page("", "c:t1#0", "c:t2#0")}
	g.pages["tab-changed"] = []string{page("b1", "c:u3#0"), page("", "c:u1#0")}
	g.pages["tab-same"] = []string{page("", "c:s1#0")}
	g.r.start(true, nil, map[string]int64{"tab-changed": 4, "tab-same": 9})
	g.r.convs([]json.RawMessage{raw(`{"id":"tab-new","rev":1}`), raw(`{"id":"tab-changed","rev":5}`), raw(`{"id":"tab-same","rev":9}`)})
	g.until(func() bool {
		n := 0
		for _, b := range g.backfills() {
			if b.Ch == "conv:tab-new" || b.Ch == "conv:tab-changed" {
				n++
			}
		}
		return n >= 6 // new: 2 pages and the steps of the 2 turns of its newest page; changed: its newest page and its turn's steps
	})
	byChat := map[string]int{}
	for _, b := range g.backfills() {
		byChat[b.Ch]++
		for _, e := range b.Events {
			var head struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(e, &head) != nil || head.Type == "" {
				t.Fatalf("an event without a type: %s", e)
			}
		}
	}
	if byChat["convs"] != 1 {
		t.Fatalf("the chat list goes in one frame: %v", byChat)
	}
	if byChat["conv:tab-same"] != 0 {
		t.Fatalf("a chat at its revision is not read again: %v", byChat)
	}
	if byChat["conv:tab-new"] < 3 || byChat["conv:tab-changed"] < 2 {
		t.Fatalf("pages %v", byChat)
	}
}

func TestFeedResetCarriesAPageAsAnEvent(t *testing.T) {
	ev, ok := feedReset(raw(`{"turns":[{"id":"c:a#0"}],"entries":[],"beforeTurnId":null}`))
	if !ok {
		t.Fatal("a page is an event")
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(ev, &got); err != nil || string(got["type"]) != `"feed_reset"` || got["turns"] == nil {
		t.Fatalf("%s %v", ev, err)
	}
	if _, ok := feedReset(raw(`{"error":"x"}`)); ok {
		t.Fatal("an answer that is not a page")
	}
}
