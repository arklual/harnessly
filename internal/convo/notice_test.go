package convo

import (
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// notices records what the chat's actor told Deps.Notify, in order.
func (r *rig) notices() *[]Notice {
	got := &[]Notice{}
	r.deps = func(d *Deps) { d.Notify = func(n Notice) { *got = append(*got, n) } }
	r.rt = r.runtime()
	return got
}

// kindsOf names the notices other than the states, in order.
func kindsOf(ns []Notice) string {
	var ks []string
	for _, n := range ns {
		if n.Kind != NoticeState {
			ks = append(ks, string(n.Kind))
		}
	}
	return strings.Join(ks, ",")
}

// statesOf are the agent states the state notices carried, in order.
func statesOf(ns []Notice) string {
	var ks []string
	for _, n := range ns {
		if n.Kind == NoticeState {
			ks = append(ks, string(n.Conv.State.K))
		}
	}
	return strings.Join(ks, ",")
}

func pushes(ns []Notice) int {
	n := 0
	for _, x := range ns {
		if x.Push() {
			n++
		}
	}
	return n
}

// One turn that ends with Stop tells the person once, even when the Stop hook comes twice; the turn's end is told
// once for the folder's snapshot, with the record as it stands after it.
func TestNoticeOneStopOnePush(t *testing.T) {
	r := newRig(t)
	got := r.notices()
	r.working("hello there")
	r.hook("prompt-submit")
	r.say("Hi!")
	r.turnEnd()
	r.idle()
	r.hook("stop")
	r.hook("stop")
	r.tick(time.Second)
	if k := kindsOf(*got); k != "turn-ended,done" {
		t.Fatalf("notices: %s", k)
	}
	if !strings.HasSuffix(statesOf(*got), "working,ready") {
		t.Fatalf("states told: %s", statesOf(*got))
	}
	var done Notice
	for _, n := range *got {
		if n.Kind == NoticeDone {
			done = n
		}
	}
	if done.Conv.State.K != wire.StateReady || done.Conv.Session != "s1" || done.Conv.TranscriptPath != r.path {
		t.Fatalf("the done notice carries %+v", done.Conv.State)
	}
}

// A turn that fails tells the person it failed, once.
func TestNoticeStopFailure(t *testing.T) {
	r := newRig(t)
	got := r.notices()
	r.working("hello there")
	r.hook("prompt-submit")
	r.idle()
	r.hook("stop-failure")
	if k := kindsOf(*got); k != "turn-ended,failed" || pushes(*got) != 1 {
		t.Fatalf("notices: %s", k)
	}
}

// The agent's permission menu tells the person once; the person's own stop ends the turn without a push.
func TestNoticeQuestionAndStop(t *testing.T) {
	r := newRig(t)
	got := r.notices()
	r.user("clean up")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.tool("toolu_rm", "rm -rf build")
	r.pane.screen = permissionScreen
	if err := r.rt.Hook(r.id, Hook{Kind: "notification", Notification: "permission_prompt"}); err != nil {
		t.Fatal(err)
	}
	if err := r.rt.Hook(r.id, Hook{Kind: "notification", Notification: "permission_prompt"}); err != nil {
		t.Fatal(err)
	}
	if k := kindsOf(*got); k != "question" {
		t.Fatalf("after the menu: %s", k)
	}
	if res := r.do(wire.InterruptCmd{OpID: "i1", ConversationID: r.id}); res.R != wire.ResultAccepted {
		t.Fatalf("interrupt: %+v", res)
	}
	r.interruptLine()
	r.idle()
	r.tick(time.Second)
	if k := r.state().K; k != wire.StateIdle {
		t.Fatalf("after the stop: %s", k)
	}
	if k := kindsOf(*got); k != "question,turn-ended" || pushes(*got) != 1 {
		t.Fatalf("notices: %s", k)
	}
}
