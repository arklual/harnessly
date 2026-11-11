package outbox

import (
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// hookRig is a rig whose agent's prompt-submit hook names the prompt it took, as Claude's does.
func hookRig(t *testing.T, screenText string) *rig {
	r := newRig(t, screenText, wire.StateIdle)
	r.box.HookConfirms = true
	return r
}

func (r *rig) hook(text string) {
	r.t.Helper()
	r.record(r.box.PromptSubmittedAs(screen.MessageHash(text), nil, false, r.clock.Now()))
}

// neverLeaves keeps the message in the box on the screen once it is typed, whatever Enter is pressed: a TUI that draws
// late. Clearing the box (C-u) takes it out.
func (r *rig) neverLeaves(text string) {
	shown := withInput(r.t, text)
	r.pane.onSend = func(call string) {
		switch {
		case strings.HasPrefix(call, "line:"):
			r.pane.view = func() string { return shown }
		case call == "keys:C-u":
			r.pane.view = nil
		}
	}
}

const day = 24 * time.Hour

// A screen drawn late shows the message in the box after every Enter though the agent took it. Its hook says so: the
// message is accepted and never typed a second time — whether the hook came while it was typed or after.
func TestALateScreenDoesNotTypeTheMessageAgain(t *testing.T) {
	t.Run("the hook comes after the try", func(t *testing.T) {
		r := hookRig(t, tui(t, "claude-idle-placeholder.ansi.txt"))
		r.neverLeaves("проверь логи")
		r.send("m1", "проверь логи", wire.ModeQueue)
		r.pump()
		if st := r.state("m1"); st.K != wire.MsgQueued || r.lines("проверь логи") != 1 {
			t.Fatalf("after a try the screen called stuck: %+v, typed %v", st, r.typed())
		}
		// The window is still open: nothing is typed while the agent's word may come.
		r.advance(ConfirmWindow - time.Second)
		if r.lines("проверь логи") != 1 {
			t.Fatalf("typed again inside the window: %v", r.typed())
		}
		r.hook("проверь логи")
		if st := r.state("m1"); st.K != wire.MsgAccepted {
			t.Fatalf("the hook did not accept it: %+v (%s)", st, r.trail("m1"))
		}
		r.pane.view = nil
		r.advance(day)
		if r.lines("проверь логи") != 1 || len(r.box.Queue().Items) != 0 {
			t.Fatalf("typed %d times, queue %v", r.lines("проверь логи"), r.box.Queue().Items)
		}
	})
	t.Run("the hook comes while it is typed", func(t *testing.T) {
		r := hookRig(t, tui(t, "claude-idle-placeholder.ansi.txt"))
		r.neverLeaves("проверь логи")
		late := r.pane.onSend
		r.pane.onSend = func(call string) {
			late(call)
			if strings.HasPrefix(call, "line:") {
				r.hook("проверь логи")
			}
		}
		r.send("m1", "проверь логи", wire.ModeQueue)
		r.pump()
		if st := r.state("m1"); st.K != wire.MsgAccepted {
			t.Fatalf("not accepted: %+v (%s)", st, r.trail("m1"))
		}
		r.pane.view, r.pane.onSend = nil, nil
		r.advance(day)
		if r.lines("проверь логи") != 1 {
			t.Fatalf("typed %d times", r.lines("проверь логи"))
		}
	})
}

// Two messages with the same words are two sends: each takes its own hook, both are typed, and one hook does not
// stand for both.
func TestTwoMessagesWithTheSameWordsAreBothDelivered(t *testing.T) {
	r := hookRig(t, tui(t, "claude-idle-placeholder.ansi.txt"))
	r.leaves()
	r.send("m1", "да", wire.ModeQueue)
	r.send("m2", "да", wire.ModeQueue)
	r.pump()
	if r.lines("да") != 1 || r.state("m1").K != wire.MsgInInput || r.state("m2").K != wire.MsgQueued {
		t.Fatalf("first: typed %v, m1 %s, m2 %s", r.typed(), r.state("m1").K, r.state("m2").K)
	}
	r.hook("да")
	if r.state("m1").K != wire.MsgAccepted || r.state("m2").K != wire.MsgQueued {
		t.Fatalf("one hook: m1 %s, m2 %s", r.state("m1").K, r.state("m2").K)
	}
	r.record(r.box.TurnStarted("c:aa#0", r.clock.Now()))
	r.record(r.box.TurnEnded("c:aa#0", r.clock.Now()))
	r.pump()
	if r.lines("да") != 2 || r.state("m2").K != wire.MsgInInput {
		t.Fatalf("second: typed %v, m2 %s", r.typed(), r.state("m2").K)
	}
	r.hook("да")
	if r.state("m2").K != wire.MsgAccepted {
		t.Fatalf("m2 after its hook: %s", r.trail("m2"))
	}
	s1, s2 := r.box.recs["m1"], r.box.recs["m2"]
	if s1.Send == 0 || s2.Send <= s1.Send || s1.HookSend != s1.Send || s2.HookSend != s2.Send {
		t.Fatalf("sends: m1 %d/%d, m2 %d/%d", s1.Send, s1.HookSend, s2.Send, s2.HookSend)
	}
	// A third hook with the same words is nobody's.
	r.hook("да")
	r.advance(day)
	if r.lines("да") != 2 {
		t.Fatalf("typed %d times", r.lines("да"))
	}
}

// The first of two same-worded messages seemed lost and went back to the line; its hook, late, is its own — not the
// second message's, which is then typed and confirmed by a hook of its own.
func TestALateHookConfirmsTheSendItWasFiredFor(t *testing.T) {
	r := hookRig(t, tui(t, "claude-idle-placeholder.ansi.txt"))
	r.neverLeaves("да")
	r.send("m1", "да", wire.ModeQueue)
	r.send("m2", "да", wire.ModeQueue)
	r.pump()
	if r.state("m1").K != wire.MsgQueued || r.lines("да") != 1 {
		t.Fatalf("m1 %+v typed %v", r.state("m1"), r.typed())
	}
	r.hook("да")
	if r.state("m1").K != wire.MsgAccepted || r.state("m2").K != wire.MsgQueued {
		t.Fatalf("m1 %s, m2 %s", r.state("m1").K, r.state("m2").K)
	}
	r.pane.view = nil
	r.leaves()
	r.record(r.box.TurnStarted("c:aa#0", r.clock.Now()))
	r.record(r.box.TurnEnded("c:aa#0", r.clock.Now()))
	r.pump()
	r.hook("да")
	if r.lines("да") != 2 || r.state("m2").K != wire.MsgAccepted {
		t.Fatalf("typed %v, m2 %s", r.typed(), r.trail("m2"))
	}
}

// With no hook and no transcript entry within the window the message is typed once more — once: after a second
// window without the agent's word it stalls and waits for the person.
func TestAnUnconfirmedMessageIsTypedOnceMore(t *testing.T) {
	r := hookRig(t, tui(t, "claude-idle-placeholder.ansi.txt"))
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	r.pane.onSend = func(call string) {
		if call == "keys:Enter" || call == "keys:C-u" || strings.HasPrefix(call, "line:") {
			r.pane.screen = idle
		}
	}
	r.send("m1", "проверь логи", wire.ModeQueue)
	r.send("m2", "и ещё одно", wire.ModeQueue)
	r.pump()
	// An empty box is not delivery: the message waits for the agent's word.
	if st := r.state("m1"); st.K != wire.MsgInInput {
		t.Fatalf("typed: %+v", st)
	}
	r.advance(ConfirmWindow - time.Second)
	if r.lines("проверь логи") != 1 {
		t.Fatalf("typed again inside the window: %v", r.typed())
	}
	r.advance(time.Second)
	if r.lines("проверь логи") != 2 || r.state("m1").K != wire.MsgInInput {
		t.Fatalf("after the window: typed %d times, %s", r.lines("проверь логи"), r.trail("m1"))
	}
	r.advance(ConfirmWindow)
	if st := r.state("m1"); st.K != wire.MsgQueued || st.Reason != wire.QueuedStalled {
		t.Fatalf("after the second window: %+v (%s)", st, r.trail("m1"))
	}
	for range 10 {
		r.advance(time.Hour)
	}
	if r.lines("проверь логи") != 2 || r.lines("и ещё одно") != 0 {
		t.Fatalf("typed %v", r.typed())
	}
	// The person's retry is theirs to make, and gets a retype of its own.
	r.record(must(r.box.Retry("m1", nil, r.clock.Now())))
	r.pump()
	if r.lines("проверь логи") != 3 || r.box.recs["m1"].Retyped {
		t.Fatalf("retry: typed %d times, retyped %v", r.lines("проверь логи"), r.box.recs["m1"].Retyped)
	}
}

// The transcript's entry confirms like the hook: found before the retype, the message is accepted, not typed.
func TestTheTranscriptEntryStopsTheRetype(t *testing.T) {
	r := hookRig(t, tui(t, "claude-idle-placeholder.ansi.txt"))
	r.leaves()
	var entry string
	r.box.Transcript = func(int64, wire.Ms, string) (string, bool) { return entry, entry != "" }
	r.send("m1", "проверь логи", wire.ModeQueue)
	r.pump()
	r.advance(ConfirmWindow - time.Second)
	entry = "c:bb#0"
	r.advance(day)
	if st := r.state("m1"); st.K != wire.MsgAccepted || st.EntryID == nil || *st.EntryID != "c:bb#0" || r.lines("проверь логи") != 1 {
		t.Fatalf("%+v, typed %d times", st, r.lines("проверь логи"))
	}
}

// A prompt the person typed in the terminal fires the hook with other words: it confirms no message of the queue.
// A hook that names no prompt is the message on its way's, as it was before the hook carried one.
func TestAHookOfOtherWordsConfirmsNothing(t *testing.T) {
	r := hookRig(t, tui(t, "claude-idle-placeholder.ansi.txt"))
	r.leaves()
	r.send("m1", "проверь логи", wire.ModeQueue)
	r.pump()
	r.hook("а это я набрал сам в терминале")
	if st := r.state("m1"); st.K != wire.MsgInInput {
		t.Fatalf("someone else's prompt accepted the message: %+v", st)
	}
	r.record(r.box.PromptSubmitted(nil, false, r.clock.Now()))
	if st := r.state("m1"); st.K != wire.MsgAccepted {
		t.Fatalf("a hook without a prompt: %+v", st)
	}
}

// An agent whose hook names no prompt keeps the old path: the screen decides, a text stuck in the box is typed again
// with its back-off, and a message nobody confirmed stalls without a retype.
func TestAnAgentWithoutThePromptHookKeepsTheOldPath(t *testing.T) {
	if ConfirmsByHook(wire.ProviderCodex) || !ConfirmsByHook(wire.ProviderClaude) {
		t.Fatal("which agents confirm by the hook")
	}
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	r.neverLeaves("проверь логи")
	r.send("m1", "проверь логи", wire.ModeQueue)
	r.pump()
	r.advance(retryBase)
	if r.lines("проверь логи") != 2 {
		t.Fatalf("a stuck text is typed again after its back-off: %d", r.lines("проверь логи"))
	}

	r = newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	r.leaves()
	r.send("m1", "проверь логи", wire.ModeQueue)
	r.pump()
	r.advance(ConfirmWindow)
	r.advance(day)
	if st := r.state("m1"); st.Reason != wire.QueuedStalled || r.lines("проверь логи") != 1 {
		t.Fatalf("%+v, typed %d times", st, r.lines("проверь логи"))
	}
}

// A try that pressed no key is no send: a hook with the message's words (the person typed them) is not held for it
// later, when it is typed at last.
func TestATryWithoutAKeyIsNoSend(t *testing.T) {
	r := hookRig(t, tui(t, "claude-idle-placeholder.ansi.txt"))
	r.leaves()
	r.send("m1", "да", wire.ModeQueue)
	look := typist.Look(r.pane, r.agent.Provider)
	r.agent.Screen = &look
	job, ch := r.box.Pump(r.agent, r.size, r.clock.Now())
	r.record(ch)
	r.record(r.box.Typed(job.ClientID, typist.Result{Outcome: typist.Wait}, r.clock.Now()))
	if r.box.recs["m1"].Send != 0 {
		t.Fatalf("send %d", r.box.recs["m1"].Send)
	}
	r.hook("да")
	if st := r.state("m1"); st.K != wire.MsgQueued {
		t.Fatalf("%+v", st)
	}
}
