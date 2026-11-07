package outbox

import (
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// Gathering stops at GatherMaxText: a message that would grow past it with another one leaves that one in the
// line, to be typed after it, whole.
func TestGatherStopsAtGatherMaxText(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	long := strings.Repeat("а", 5_000)
	r.send("m1", long, wire.ModeQueue)
	r.send("m2", long, wire.ModeQueue)
	r.send("m3", "now", wire.ModeNow)
	m3, _ := r.box.Message("m3")
	if m3.Text != long+"\n\nnow" {
		t.Fatalf("m3: %d units", len(m3.Text))
	}
	if st := r.state("m2"); st.K != wire.MsgQueued {
		t.Fatalf("m2: %+v", st)
	}
	if r.state("m1").K != wire.MsgCancelled {
		t.Fatalf("m1: %+v", r.state("m1"))
	}
}

// 07.11: a long steered paste sat folded in Claude's box («[Pasted text #1 +843 lines]»), every Enter pressed
// again went nowhere, and the page said «in the agent's queue» for 30 minutes. Past its last Enter it is stalled at
// once: back in the line, the box cleared, the person decides.
func TestASteeredPasteClaudeNeverTakesIsStalledAfterItsEnters(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.pane.onSend = func(string) {}
	start := r.clock.Now()
	r.send("a", labText(), wire.ModeSteer)
	r.pump()
	if st := r.state("a"); st.K != wire.MsgInInput {
		t.Fatalf("a: %s", r.trail("a"))
	}
	stuck := withInput(t, labText()[:120])
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	r.pane.screen = stuck
	r.pane.onSend = func(call string) {
		if call != "keys:Enter" && strings.HasPrefix(call, "keys:") {
			r.pane.screen = idle // clearing the box takes it out
		}
	}
	for i := 0; i < 200 && r.state("a").K == wire.MsgInInput; i++ {
		r.advance(steerRecheckEvery)
	}
	st := r.state("a")
	if st.K != wire.MsgQueued || st.Reason != wire.QueuedStalled {
		t.Fatalf("a: %s\n%v", r.trail("a"), r.typed())
	}
	if took := r.clock.Now().Sub(start); took > 5*time.Minute {
		t.Fatalf("stalled only after %v", took)
	}
}

// 07.11: a steered message of a picture alone was read by Claude (its queued_command «[Image #29]»), yet the
// page said «in the agent's queue» for 30 minutes: no words, so no entry was taken for it. An entry of picture
// references alone is its.
func TestASteeredPictureAloneIsTakenByItsPictureEntry(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.leaves()
	pic := wire.AttachmentRef{ID: "att-1", Name: "shot.png", Kind: wire.AttachmentImage, Mime: "image/png"}
	ch, code := r.box.Send(Draft{ClientID: "p", Mode: wire.ModeSteer, Kind: wire.KindPrompt,
		Attachments: []wire.AttachmentRef{pic}, Images: []string{"/up/shot.png"}}, r.agent.State, r.clock.Now())
	r.record(ch)
	if code != "" {
		t.Fatal(code)
	}
	r.pump()
	if st := r.state("p"); st.K != wire.MsgInInput {
		t.Fatalf("p: %s %v", r.trail("p"), r.typed())
	}
	if id, _ := r.box.UserEntryText("c:bbbb#0", "a words entry of someone else", 1<<40, nil, r.clock.Now()); id != "" {
		t.Fatalf("an entry with words took it: %s", id)
	}
	id, ch := r.box.UserEntryText("c:aaaa#0", "[Image #29]", 1<<40, nil, r.clock.Now())
	r.record(ch)
	if id != "p" || r.state("p").K != wire.MsgAccepted {
		t.Fatalf("%q %s", id, r.trail("p"))
	}
}
