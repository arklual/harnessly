package outbox

import (
	"reflect"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// «Сразу» while several messages wait sends them all as one, in the line's order, now: their words joined by a
// blank line, their attachments and pictures with them; the line is empty after it.
func TestSendNowTakesTheWholeQueueAsOneMessage(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.leaves()
	r.send("m1", "first", wire.ModeQueue)
	pic := wire.AttachmentRef{ID: "att-1", Name: "shot.png", Kind: wire.AttachmentImage, Mime: "image/png"}
	ch, code := r.box.Send(Draft{ClientID: "m2", Text: "second", Mode: wire.ModeQueue, Kind: wire.KindPrompt,
		Attachments: []wire.AttachmentRef{pic}, Images: []string{"/up/shot.png"}}, r.agent.State, r.clock.Now())
	r.record(ch)
	if code != "" {
		t.Fatal(code)
	}
	r.send("c1", "/compact", wire.ModeQueue) // a command goes alone
	r.box.recs["c1"].Message.Kind = wire.KindCommand
	if code := r.send("m3", "third", wire.ModeSteer); code != "" {
		t.Fatal(code)
	}
	m3, _ := r.box.Message("m3")
	if m3.Text != "first\n\nsecond\n\nthird" || !reflect.DeepEqual(m3.Attachments, []wire.AttachmentRef{pic}) ||
		!reflect.DeepEqual(r.box.recs["m3"].Images, []string{"/up/shot.png"}) {
		t.Fatalf("%+v %v", m3, r.box.recs["m3"].Images)
	}
	for _, id := range []string{"m1", "m2"} {
		if st := r.state(id); st.K != wire.MsgCancelled {
			t.Fatalf("%s: %+v", id, st)
		}
	}
	if q := r.box.Queue(); !reflect.DeepEqual(q.Items, []string{"m3", "c1"}) {
		t.Fatalf("%v", q.Items)
	}
}

// ↑ on a queued message sends every message that waits with it, in the line's order.
func TestPromoteTakesTheOthersInTheLinesOrder(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.leaves()
	r.send("m1", "one", wire.ModeQueue)
	r.send("m2", "two", wire.ModeQueue)
	r.send("m3", "three", wire.ModeQueue)
	ch, code := r.box.Promote("m2", wire.PromoteToSteer, r.agent.State, r.clock.Now())
	r.record(ch)
	if code != "" {
		t.Fatal(code)
	}
	m2, _ := r.box.Message("m2")
	if m2.Text != "one\n\ntwo\n\nthree" || m2.Mode != wire.ModeSteer {
		t.Fatalf("%+v", m2)
	}
	if q := r.box.Queue(); !reflect.DeepEqual(q.Items, []string{"m2"}) {
		t.Fatalf("%v", q.Items)
	}
	r.pump()
	if st := r.state("m2"); st.K != wire.MsgInInput {
		t.Fatalf("%+v %v", st, r.typed())
	}
	if r.state("m1").K != wire.MsgCancelled || r.state("m3").K != wire.MsgCancelled {
		t.Fatal("the others are still in the line")
	}
}

// A message that would grow past the limit with another one leaves that one in the line.
func TestGatherKeepsTheLimits(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.box.limits.MaxText = 12
	r.send("m1", "a long one", wire.ModeQueue)
	r.send("m2", "short", wire.ModeQueue)
	r.send("m3", "now", wire.ModeNow)
	m3, _ := r.box.Message("m3")
	if m3.Text != "short\n\nnow" || r.state("m1").K != wire.MsgQueued {
		t.Fatalf("%q %+v", m3.Text, r.state("m1"))
	}
}

// A bash-mode line in flight is taken by its command entry; another command's entry does not take it.
func TestABashModeLineIsAcceptedByItsCommandEntry(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	r.leaves()
	r.send("b1", "! tctl create -f role.yaml", wire.ModeQueue)
	r.box.recs["b1"].Message.Kind = wire.KindCommand
	r.pump()
	if st := r.state("b1"); st.K != wire.MsgInInput {
		t.Fatalf("%+v", st)
	}
	if id, _ := r.box.UserEntryText("c:aaaa#0", "/compact", 10, nil, r.clock.Now()); id != "" {
		t.Fatalf("another command took it: %s", id)
	}
	id, ch := r.box.UserEntryText("c:bbbb#0", "! tctl create -f role.yaml", 10, nil, r.clock.Now())
	r.record(ch)
	if id != "b1" || r.state("b1").K != wire.MsgAccepted {
		t.Fatalf("%s %+v", id, r.state("b1"))
	}
}
