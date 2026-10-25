package convo

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/outbox"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// §6: an idle agent gets the message at once; the transcript's user entry accepts it, the turn takes it, the stop
// ends it. Every event went out on its channel and passed Validate (the publisher checks).
func TestIdleAgentGetsTheMessage(t *testing.T) {
	r := newRig(t)
	r.open()
	if k := r.state().K; k != wire.StateUnknown {
		t.Fatalf("an imported tab starts %s; unknown is not idle", k)
	}
	r.settle(wire.StateReady)
	r.agentTakes()

	res := r.send("m1", "hello there", wire.ModeQueue)
	if res.R != wire.ResultQueued || res.Ch != wire.ConvChannel(r.id) || res.Seq == 0 {
		t.Fatalf("send: %+v", res)
	}
	r.tick(time.Second)
	if k := r.state().K; k != wire.StateWorking {
		t.Fatalf("after the user line the agent is %s", k)
	}
	m := r.message("m1")
	if m.State.K != wire.MsgInTurn || m.State.TurnID == "" {
		t.Fatalf("message: %+v", m.State)
	}
	r.hook("prompt-submit")
	r.say("Hi!")
	r.turnEnd()
	r.idle()
	r.hook("stop")
	if k := r.state().K; k != wire.StateReady {
		t.Fatalf("after stop: %s", k)
	}
	if got := r.trail("m1"); got != "queued{behind} → typing → in-input → accepted → in-turn → done" &&
		got != "typing → in-input → accepted → in-turn → done" {
		t.Fatalf("trail: %s", got)
	}
	if n := len(r.events(wire.EvTurnFinished)); n != 1 {
		t.Fatalf("%d turn_finished", n)
	}
	snap := r.snap()
	var user *wire.Entry
	for i, e := range snap.Entries {
		if e.T == wire.EntryUser {
			user = &snap.Entries[i]
		}
	}
	if user == nil || user.ClientID == nil || *user.ClientID != "m1" {
		t.Fatalf("the user entry does not carry the message: %+v", user)
	}
	if len(snap.Turns) != 1 || snap.Turns[0].Status != wire.TurnDone || snap.Turns[0].AnswerEntryID == nil {
		t.Fatalf("turns: %+v", snap.Turns)
	}
}

// §6: a busy agent: the message waits as agent-busy with no term, and goes when the turn ends.
func TestBusyAgentQueuesUntilTheTurnEnds(t *testing.T) {
	r := newRig(t)
	r.user("first")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.agentTakes()

	r.send("m1", "next please", wire.ModeQueue)
	m := r.message("m1")
	if m.State.K != wire.MsgQueued || m.State.Reason != wire.QueuedAgentBusy || m.State.Wait != nil {
		t.Fatalf("queued: %+v", m.State)
	}
	if len(r.pane.calls) != 0 {
		t.Fatalf("typed into a busy agent: %v", r.pane.calls)
	}
	r.say("done")
	r.turnEnd()
	r.idle()
	r.hook("stop")
	if m := r.message("m1"); m.State.K != wire.MsgInTurn {
		t.Fatalf("after the turn: %s (%v)", m.State.K, r.pane.calls)
	}
}

// §6: the agent asks: waiting-question with the question's id; a message is not typed over the dialog; the answer
// goes as keys and the gate is answered when the transcript shows the result.
func TestQuestionOpenAndAnswered(t *testing.T) {
	r := newRig(t)
	r.user("ask me")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)

	r.ask("toolu_q1")
	r.pane.screen = tui(t, "claude-ask-preview-question.txt")
	r.tick(time.Second)
	st := r.state()
	if st.K != wire.StateWaitingQuestion || st.QuestionID != "q:toolu_q1" {
		t.Fatalf("state: %+v", st)
	}
	snap := r.snap()
	if len(snap.Questions) != 1 || snap.Questions[0].State.K != wire.GateStateOpen || len(snap.Questions[0].Items) != 1 {
		t.Fatalf("questions: %+v", snap.Questions)
	}
	if n := len(r.events(wire.EvQuestionAsked)); n != 1 {
		t.Fatalf("%d question_asked", n)
	}

	bad := r.do(wire.AnswerCmd{OpID: "a0", ConversationID: r.id, QuestionID: "q:toolu_q1",
		Answers: []wire.QuestionAnswer{{I: 0, Choices: []int{5}}}})
	if bad.R != wire.ResultRefused || bad.Code != wire.RefuseOptionUnknown {
		t.Fatalf("an option it does not have: %+v", bad)
	}
	r.pane.onSend = func(call string) {
		if call == "keys:2" {
			r.idle()
		}
	}
	res := r.do(wire.AnswerCmd{OpID: "a1", ConversationID: r.id, QuestionID: "q:toolu_q1",
		Answers: []wire.QuestionAnswer{{I: 0, Choices: []int{1}}}})
	if res.R != wire.ResultAccepted {
		t.Fatalf("answer: %+v", res)
	}
	if g := r.snap().Questions[0].State; g.K != wire.GateStateAnswering || g.ByDevice != "dev-1" || g.Wait == nil {
		t.Fatalf("gate while the agent takes it: %+v", g)
	}
	again := r.do(wire.AnswerCmd{OpID: "a2", ConversationID: r.id, QuestionID: "q:toolu_q1",
		Answers: []wire.QuestionAnswer{{I: 0, Choices: []int{0}}}})
	if again.Code != wire.RefuseGateAnswered {
		t.Fatalf("a second answer: %+v", again)
	}
	r.result("toolu_q1", "User answered: Blue")
	r.busy()
	r.tick(time.Second)
	if g := r.snap().Questions[0].State; g.K != wire.GateStateAnswered || g.OpID != "a1" {
		t.Fatalf("gate after the result: %+v", g)
	}
	if k := r.state().K; k != wire.StateWorking {
		t.Fatalf("after the answer the agent is %s", k)
	}
}

// An answer whose keys did not take opens the gate again by its deadline: no endless "answering".
func TestAnswerThatDidNotTakeOpensTheGateAgain(t *testing.T) {
	r := newRig(t)
	r.user("ask me")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.ask("toolu_q1")
	r.pane.screen = tui(t, "claude-ask-preview-question.txt")
	r.tick(time.Second)

	r.pane.fail = "keys:"
	res := r.do(wire.AnswerCmd{OpID: "a1", ConversationID: r.id, QuestionID: "q:toolu_q1",
		Answers: []wire.QuestionAnswer{{I: 0, Choices: []int{0}}}})
	if res.R != wire.ResultAccepted {
		t.Fatalf("answer: %+v", res)
	}
	if g := r.snap().Questions[0].State; g.K != wire.GateStateOpen {
		t.Fatalf("keys failed, gate %s", g.K)
	}
	r.pane.fail = ""
	r.pane.onSend = nil
	res = r.do(wire.AnswerCmd{OpID: "a2", ConversationID: r.id, QuestionID: "q:toolu_q1",
		Answers: []wire.QuestionAnswer{{I: 0, Choices: []int{0}}}})
	if res.R != wire.ResultAccepted {
		t.Fatalf("second answer: %+v", res)
	}
	r.tick(time.Duration(wire.AnswerDeadline) * time.Millisecond)
	if g := r.snap().Questions[0].State; g.K != wire.GateStateOpen {
		t.Fatalf("past the answer's term the gate is %s", g.K)
	}
}

// §6: Claude's permission menu: waiting-permission on the pending call, its options off the screen; the queue does
// not type over the menu; a decision presses the option's number.
func TestPermissionMenu(t *testing.T) {
	r := newRig(t)
	r.user("clean up")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.tool("toolu_rm", "rm -rf build")
	r.pane.screen = permissionScreen
	r.rt.Hook(r.id, Hook{Kind: "notification", Notification: "permission_prompt"})
	st := r.state()
	if st.K != wire.StateWaitingPermission || st.PermissionID != "p:toolu_rm" {
		t.Fatalf("state: %+v", st)
	}
	p := r.snap().Permissions[0]
	if len(p.Options) != 3 || p.Options[1].Scope != wire.ScopeAlways || p.Options[2].Scope != wire.ScopeDeny ||
		p.Tool == nil || *p.Tool != "Bash" {
		t.Fatalf("permission: %+v", p)
	}

	r.send("m1", "also this", wire.ModeQueue)
	if m := r.message("m1"); m.State.K != wire.MsgQueued || m.State.Reason != wire.QueuedBlockedMenu {
		t.Fatalf("queued over the menu: %+v", m.State)
	}
	r.pane.onSend = func(call string) {
		if call == "keys:1" {
			r.busy()
		}
	}
	res := r.do(wire.DecideCmd{OpID: "d1", ConversationID: r.id, PermissionID: "p:toolu_rm", OptionID: "1"})
	if res.R != wire.ResultAccepted || !strings.Contains(strings.Join(r.pane.calls, ","), "keys:1") {
		t.Fatalf("decide: %+v %v", res, r.pane.calls)
	}
	r.result("toolu_rm", "")
	r.tick(time.Second)
	if g := r.snap().Permissions[0].State; g.K != wire.GateStateAnswered {
		t.Fatalf("gate: %+v", g)
	}
	if k := r.state().K; k != wire.StateWorking {
		t.Fatalf("state %s", k)
	}
	for _, c := range r.pane.calls {
		if strings.Contains(c, "also this") {
			t.Fatalf("the message was typed over the menu: %v", r.pane.calls)
		}
	}
}

// §6: foreign text in the box: nothing is typed, the message waits blocked-text with its term, and goes once the
// box is clear.
func TestForeignTextBlocks(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	r.pane.screen = withInput(t, "someone else's draft")

	r.send("m1", "mine", wire.ModeQueue)
	m := r.message("m1")
	if m.State.K != wire.MsgQueued || m.State.Reason != wire.QueuedBlockedText || m.State.Wait == nil {
		t.Fatalf("over foreign text: %+v %v %s", m.State, r.pane.calls, r.trail("m1"))
	}
	if len(r.pane.calls) != 0 {
		t.Fatalf("typed: %v", r.pane.calls)
	}
	r.idle()
	r.agentTakes()
	for range 30 {
		if r.message("m1").State.K == wire.MsgInTurn {
			break
		}
		r.tick(time.Second)
	}
	if m := r.message("m1"); m.State.K != wire.MsgInTurn {
		t.Fatalf("after the box cleared: %+v (%v)", m.State, r.pane.calls)
	}
}

// §6: the prod screen of five pictures whose last reference the box wrapped mid-way: the pictures are the message's
// own, only the text is typed, nothing is pasted twice.
func TestWrappedPicturesAreOwn(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	c := r.rt.actor(r.id).c
	images := []string{"/u/1.png", "/u/2.png", "/u/3.png", "/u/4.png", "/u/5.png"}
	ch, code := c.box.Send(outbox.Draft{ClientID: "m1", Text: "look at these", Images: images, Mode: wire.ModeQueue,
		Kind: wire.KindPrompt, Attachments: []wire.AttachmentRef{}}, c.rec.State, r.clock.Now())
	if code != "" {
		t.Fatal(code)
	}
	r.pane.screen = tui(t, "claude-pictures-wrapped-mid-ref.ansi.txt")
	busy := tui(t, "claude-busy-spinner.ansi.txt")
	r.pane.onSend = func(call string) {
		if call == "keys:Enter" || strings.HasPrefix(call, "line:") {
			r.user("look at these")
			r.pane.screen = busy
		}
	}
	r.rt.on(r.rt.actor(r.id), func(c *conv, now time.Time) { c.change(ch, now); c.pump(now) })
	for _, call := range r.pane.calls {
		if strings.HasPrefix(call, "paste:/u/") {
			t.Fatalf("pasted a picture the box holds: %v", r.pane.calls)
		}
	}
	r.tick(time.Second)
	if m := r.message("m1"); m.State.K != wire.MsgInTurn {
		t.Fatalf("message: %+v (%v)", m.State, r.pane.calls)
	}
}

// inInputThenStop leaves m1 in input — the line went in, the agent wrote nothing yet — and stops the runtime.
func inInputThenStop(t *testing.T, r *rig) int {
	t.Helper()
	r.open()
	r.settle(wire.StateReady)
	r.pane.onSend = func(call string) {
		if strings.HasPrefix(call, "line:") {
			r.idle()
		}
	}
	r.send("m1", "do it", wire.ModeQueue)
	if k := r.message("m1").State.K; k != wire.MsgInInput {
		t.Fatalf("before the restart: %s", k)
	}
	r.rt.Stop()
	return len(r.pane.calls)
}

func (r *rig) restart() {
	r.t.Helper()
	r.rt = r.runtime()
	if err := r.rt.Start(context.Background()); err != nil {
		r.t.Fatal(err)
	}
}

// §6, §4.5: a restart while a message was in input finds its text in the box: the runtime comes back unknown and
// presses only Enter; the message is not typed twice.
func TestRestartMidTypingDoesNotRetype(t *testing.T) {
	r := newRig(t)
	typedBefore := inInputThenStop(t, r)

	r.pane.screen = withInput(t, "do it")
	r.pane.onSend = func(call string) {
		if call == "keys:Enter" {
			r.user("do it")
			r.busy()
		}
	}
	r.restart()
	for range 10 {
		if r.message("m1").State.K == wire.MsgInTurn {
			break
		}
		r.tick(time.Second)
	}
	after := r.pane.calls[typedBefore:]
	for _, c := range after {
		if strings.HasPrefix(c, "line:") || strings.HasPrefix(c, "paste:") {
			t.Fatalf("retyped after the restart: %v", after)
		}
	}
	if len(after) == 0 || after[0] != "keys:Enter" {
		t.Fatalf("keys after the restart: %v", after)
	}
	if m := r.message("m1"); m.State.K != wire.MsgInTurn {
		t.Fatalf("after the restart: %+v (%v)", m.State, after)
	}
}

// A restart that finds the user entry already written accepts the message without a key.
func TestRestartFindsTheEntry(t *testing.T) {
	r := newRig(t)
	typedBefore := inInputThenStop(t, r)
	r.clock.Sleep(time.Second)
	r.user("do it")
	r.busy()

	r.restart()
	m := r.message("m1")
	if m.State.K != wire.MsgAccepted && m.State.K != wire.MsgInTurn {
		t.Fatalf("after the restart: %+v", m.State)
	}
	if len(r.pane.calls) != typedBefore {
		t.Fatalf("keys after the restart: %v", r.pane.calls[typedBefore:])
	}
}

// A restart that finds neither the entry nor the text queues the message again at the head, and it is typed once.
func TestRestartWithNothingQueuesAgain(t *testing.T) {
	r := newRig(t)
	inInputThenStop(t, r)
	r.restart()
	if m := r.message("m1"); m.State.K != wire.MsgQueued && m.State.K != wire.MsgTyping && m.State.K != wire.MsgInInput {
		t.Fatalf("after the restart: %+v", m.State)
	}
}

// §6: Stop pauses the queue and presses Escape once; a second Stop does not press it again; the interrupt ends the
// turn and the queue waits for queue.resume.
func TestStopPausesTheQueue(t *testing.T) {
	r := newRig(t)
	r.user("long job")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.send("m1", "after that", wire.ModeQueue)

	res := r.do(wire.InterruptCmd{OpID: "i1", ConversationID: r.id})
	if res.R != wire.ResultAccepted {
		t.Fatalf("interrupt: %+v", res)
	}
	if again := r.do(wire.InterruptCmd{OpID: "i2", ConversationID: r.id}); again.Code != wire.RefuseNotWorking {
		t.Fatalf("a second stop: %+v", again)
	}
	escapes := 0
	for _, c := range r.pane.calls {
		if c == "keys:Escape" {
			escapes++
		}
	}
	if escapes != 1 {
		t.Fatalf("%d escapes: %v", escapes, r.pane.calls)
	}
	r.interruptLine()
	r.idle()
	r.tick(time.Second)
	if k := r.state().K; k != wire.StateIdle {
		t.Fatalf("after the interrupt: %s", k)
	}
	snap := r.snap()
	if snap.Queue.Paused == nil || snap.Queue.Paused.Reason != wire.PausedInterrupted {
		t.Fatalf("queue: %+v", snap.Queue)
	}
	if last := snap.Turns[len(snap.Turns)-1]; last.Status != wire.TurnInterrupted {
		t.Fatalf("turn: %+v", last)
	}
	if m := r.message("m1"); m.State.K != wire.MsgQueued || m.State.Reason != wire.QueuedPaused {
		t.Fatalf("message: %+v", m.State)
	}
	r.agentTakes()
	if res := r.do(wire.QueueResumeCmd{OpID: "r1", ConversationID: r.id}); res.R != wire.ResultAccepted {
		t.Fatalf("resume: %+v", res)
	}
	r.tick(time.Second)
	if m := r.message("m1"); m.State.K != wire.MsgInTurn {
		t.Fatalf("after resume: %+v (%v)", m.State, r.pane.calls)
	}
}

// §6: the queue's limit and the text's: refused with their codes, nothing queued.
func TestLimits(t *testing.T) {
	r := newRig(t)
	r.user("busy")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	for i := range wire.DefaultLimits.MaxQueue {
		id := "m" + itoa(int64(i))
		if res := r.send(id, "x "+id, wire.ModeQueue); res.R != wire.ResultQueued {
			t.Fatalf("%s: %+v", id, res)
		}
	}
	if res := r.send("over", "one more", wire.ModeQueue); res.Code != wire.RefuseQueueFull {
		t.Fatalf("over the queue: %+v", res)
	}
	long := strings.Repeat("a", wire.DefaultLimits.MaxText+1)
	if res := r.send("long", long, wire.ModeQueue); res.Code != wire.RefuseTextTooLong && res.Code != wire.RefuseBadRequest {
		t.Fatalf("a text too long: %+v", res)
	}
	if n := len(r.snap().Queue.Items); n != wire.DefaultLimits.MaxQueue {
		t.Fatalf("queue of %d", n)
	}
}

// §6: the machine's tmux session is gone: gone{tmux-lost}, the turn failed, the queue paused, a send refused.
func TestMachineGone(t *testing.T) {
	r := newRig(t)
	r.user("work")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.send("m1", "later", wire.ModeQueue)

	r.machine.probes["s1"] = Probe{}
	r.tick(time.Second)
	st := r.state()
	if st.K != wire.StateGone || st.Reason != string(wire.GoneTmuxLost) {
		t.Fatalf("state: %+v", st)
	}
	if n := len(r.events(wire.EvTurnFailed)); n != 1 {
		t.Fatalf("%d turn_failed", n)
	}
	if q := r.snap().Queue; q.Paused == nil || q.Paused.Reason != wire.PausedAgentGone {
		t.Fatalf("queue: %+v", q)
	}
	// A session not found may be tmux that failed to answer on a loaded machine: the message waits for the pane.
	if res := r.send("m2", "now?", wire.ModeQueue); res.Code != "" {
		t.Fatalf("send to a lost agent: %+v", res)
	}

	// The pane is there after all: the agent is back and the paused queue goes on.
	r.machine.probes["s1"] = Probe{Session: true, Foreground: true}
	r.tick(time.Second)
	if st := r.state(); st.K == wire.StateGone {
		t.Fatalf("state after the pane came back: %+v", st)
	}
	if q := r.snap().Queue; q.Paused != nil || len(q.Items) != 2 {
		t.Fatalf("queue after the pane came back: %+v", q)
	}
}

// A closed tab is gone for good: a send is refused.
func TestClosedTabRefusesSend(t *testing.T) {
	r := newRig(t)
	r.rt.d.Closer = &fakeCloser{}
	r.open()
	r.settle(wire.StateReady)
	r.do(wire.ConversationDeleteCmd{OpID: "del", ID: r.id})
	r.do(wire.ConversationRestoreCmd{OpID: "res", ID: r.id})
	if res := r.send("m1", "hi", wire.ModeQueue); res.Code != wire.RefuseAgentGone {
		t.Fatalf("send to a closed tab: %+v", res)
	}
}

// The op answers: the same opId and body give the same answer, another body conflict.
func TestOpIdempotency(t *testing.T) {
	r := newRig(t)
	r.open()
	first := r.do(wire.ConversationRenameCmd{OpID: "o1", ID: r.id, Title: "new name"})
	if first.R != wire.ResultAccepted || first.Ch != wire.ChannelConvs || first.Rev == nil {
		t.Fatalf("rename: %+v", first)
	}
	upserts := len(r.events(wire.EvConversationUpsert))
	again := r.do(wire.ConversationRenameCmd{OpID: "o1", ID: r.id, Title: "new name"})
	if again.Seq != first.Seq || *again.Rev != *first.Rev || len(r.events(wire.EvConversationUpsert)) != upserts {
		t.Fatalf("the same op again: %+v, %d upserts", again, len(r.events(wire.EvConversationUpsert))-upserts)
	}
	if other := r.do(wire.ConversationRenameCmd{OpID: "o1", ID: r.id, Title: "other"}); other.Code != wire.RefuseConflict {
		t.Fatalf("another body: %+v", other)
	}
	r.rt.Stop()
	r.rt = r.runtime()
	r.rt.Start(context.Background())
	if again := r.do(wire.ConversationRenameCmd{OpID: "o1", ID: r.id, Title: "new name"}); again.Seq != first.Seq {
		t.Fatalf("after a restart: %+v", again)
	}
}

// seen, pin, delete and restore: the convs channel carries each; a deleted conversation is out of the list and its
// snapshot, restore brings it back.
func TestRecordCommands(t *testing.T) {
	r := newRig(t)
	r.open()
	if res := r.do(wire.SeenCmd{OpID: "s1", ConversationID: wire.SeenAll, UpTo: wire.Ms(r.clock.Now().UnixMilli()) + 5}); res.R != wire.ResultAccepted {
		t.Fatalf("seen: %+v", res)
	}
	if n := len(r.events(wire.EvConversationSeen)); n != 1 {
		t.Fatalf("%d conversation_seen", n)
	}
	r.do(wire.ConversationPinCmd{OpID: "p1", ID: r.id, Pinned: true})
	if res := r.do(wire.ConversationDeleteCmd{OpID: "d1", ID: r.id}); res.R != wire.ResultAccepted {
		t.Fatalf("delete: %+v", res)
	}
	cs, _ := r.rt.Conversations(context.Background())
	if len(cs.Conversations) != 0 {
		t.Fatalf("a deleted conversation is listed")
	}
	if _, err := r.rt.Conversation(context.Background(), r.id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("snapshot of a deleted one: %v", err)
	}
	if res := r.do(wire.ConversationRestoreCmd{OpID: "r1", ID: r.id}); res.R != wire.ResultAccepted {
		t.Fatalf("restore: %+v", res)
	}
	cs, _ = r.rt.Conversations(context.Background())
	if len(cs.Conversations) != 1 || !cs.Conversations[0].Pinned || cs.Conversations[0].SeenAt != wire.Ms(r.clock.Now().UnixMilli())+5 {
		t.Fatalf("restored: %+v", cs.Conversations)
	}
}

// conversation.create: starting with its term; the first message goes with the launch as the agent's first prompt, never
// typed, and the agent's prompt-submit accepts it.
func TestCreateLaunchesWithTheFirstMessage(t *testing.T) {
	r := newRig(t)
	res := r.do(wire.ConversationCreateCmd{OpID: "c1", Cwd: "/w", Provider: wire.ProviderClaude,
		First: &wire.FirstMessage{ClientID: "first", Text: "start here", AttachmentIDs: []string{}}})
	if res.R != wire.ResultAccepted || res.ID == nil || !wire.ValidConvID(*res.ID) {
		t.Fatalf("create: %+v", res)
	}
	r.id = *res.ID
	st := r.state()
	if st.K != wire.StateStarting || st.Wait == nil {
		t.Fatalf("state: %+v", st)
	}
	if m := r.message("first"); m.State.K != wire.MsgInInput || m.State.Wait == nil {
		t.Fatalf("first: %+v", m.State)
	}
	if len(r.launcher.launched) != 1 || r.launcher.launched[0].First != "start here" {
		t.Fatalf("launched %+v", r.launcher.launched)
	}
	r.agentTakes()
	r.rt.Attach(r.id, "s1", r.path, "sess")
	r.tick(5 * time.Second)
	r.hook("session-start")
	r.tick(time.Second)
	// Nothing is typed into the starting terminal, whatever it shows.
	if m := r.message("first"); m.State.K != wire.MsgInInput || len(r.pane.calls) != 0 {
		t.Fatalf("first before the prompt is taken: %+v %v", m.State, r.pane.calls)
	}
	r.hook("prompt-submit")
	if m := r.message("first"); m.State.K != wire.MsgAccepted && m.State.K != wire.MsgInTurn {
		t.Fatalf("first after the agent took it: %+v", m.State)
	}
}

// A message sent to a starting agent after its launch (nothing carried it) waits queued{launching}; the prompt box on the
// screen makes the agent idle and the message is typed.
func TestMessageToAStartingAgentIsTypedWhenItIsReady(t *testing.T) {
	r := newRig(t)
	res := r.do(wire.ConversationCreateCmd{OpID: "c2", Cwd: "/w", Provider: wire.ProviderClaude})
	r.id = *res.ID
	r.send("m1", "start here", wire.ModeQueue)
	if m := r.message("m1"); m.State.K != wire.MsgQueued || m.State.Reason != wire.QueuedLaunching {
		t.Fatalf("first: %+v", m.State)
	}
	r.agentTakes()
	r.rt.Attach(r.id, "s1", r.path, "sess")
	r.tick(time.Second)
	// The prompt box just drawn is not ready yet: the agent counts once the screen has stood still.
	if st := r.state(); st.K != wire.StateStarting {
		t.Fatalf("ready on the first look of the box: %+v", st)
	}
	r.tick(BootSettle)
	if m := r.message("m1"); m.State.K != wire.MsgInTurn {
		t.Fatalf("first after the launch: %+v (%s, %v)", m.State, r.state().K, r.pane.calls)
	}
}

// A launch that fails takes the carried message back into the line: queued, paused, to be retried by the person.
func TestCarriedMessageGoesBackWhenTheLaunchFails(t *testing.T) {
	r := newRig(t)
	res := r.do(wire.ConversationCreateCmd{OpID: "c1", Cwd: "/w", Provider: wire.ProviderClaude,
		First: &wire.FirstMessage{ClientID: "first", Text: "start here", AttachmentIDs: []string{}}})
	r.id = *res.ID
	r.rt.LaunchFailed(r.id, wire.LaunchFailedCliMissing)
	r.tick(time.Second)
	if m := r.message("first"); m.State.K != wire.MsgQueued || m.State.Reason != wire.QueuedPaused {
		t.Fatalf("first: %+v", m.State)
	}
}

// A conversation whose first launch failed has no session to resume: «Continue sending» launches the agent anew with
// the first message of the line as its first prompt, instead of pausing the queue again.
func TestResumeAfterAFailedFirstLaunchStartsFresh(t *testing.T) {
	r := newRig(t)
	res := r.do(wire.ConversationCreateCmd{OpID: "c1", Cwd: "/w", Provider: wire.ProviderClaude,
		First: &wire.FirstMessage{ClientID: "first", Text: "start here", AttachmentIDs: []string{}}})
	r.id = *res.ID
	r.rt.LaunchFailed(r.id, wire.LaunchFailedCliMissing)
	r.tick(time.Second)
	if st := r.state(); st.K != wire.StateLaunchFailed || r.snap().Queue.Paused == nil {
		t.Fatalf("not failed and paused: %+v %+v", st, r.snap().Queue)
	}
	if res := r.do(wire.QueueResumeCmd{OpID: "r1", ConversationID: r.id}); res.R != wire.ResultAccepted {
		t.Fatalf("resume: %+v", res)
	}
	if st := r.state(); st.K != wire.StateStarting {
		t.Fatalf("state after resume: %+v", st)
	}
	if n := len(r.launcher.launched); n != 2 || r.launcher.launched[1].First != "start here" || len(r.launcher.resumed) != 0 {
		t.Fatalf("launched %+v resumed %v", r.launcher.launched, r.launcher.resumed)
	}
	if m := r.message("first"); m.State.K != wire.MsgInInput {
		t.Fatalf("first: %+v", m.State)
	}
	if r.snap().Queue.Paused != nil {
		t.Fatalf("queue still paused: %+v", r.snap().Queue)
	}
}

// A launch whose CLI exits at once leaves a bare shell in its pane: the launch fails once that shell stays past
// LaunchNoAgent, not at the far deadline, so «Continue sending» finds it failed and starts the agent anew.
func TestLaunchWithAShellLeftFailsAndResumeStartsFresh(t *testing.T) {
	r := newRig(t)
	res := r.do(wire.ConversationCreateCmd{OpID: "c1", Cwd: "/w", Provider: wire.ProviderClaude,
		First: &wire.FirstMessage{ClientID: "first", Text: "start here", AttachmentIDs: []string{}}})
	r.id = *res.ID
	if err := r.rt.Attach(r.id, "s1", "", ""); err != nil {
		t.Fatal(err)
	}
	r.machine.probes["s1"] = Probe{Session: true, Shell: true}
	r.tick(5 * time.Second)
	if st := r.state(); st.K != wire.StateStarting {
		t.Fatalf("a shell for a moment is the launch line: %+v", st)
	}
	r.tick(LaunchNoAgent)
	if st := r.state(); st.K != wire.StateLaunchFailed || st.Reason != string(wire.LaunchFailedExited) {
		t.Fatalf("state: %+v", st)
	}
	if res := r.do(wire.QueueResumeCmd{OpID: "r1", ConversationID: r.id}); res.R != wire.ResultAccepted {
		t.Fatalf("resume: %+v", res)
	}
	if n := len(r.launcher.launched); n != 2 || r.launcher.launched[1].First != "start here" {
		t.Fatalf("launched %+v", r.launcher.launched)
	}
}

// A new message to such a conversation starts it fresh too, with the oldest message of the line.
func TestMessageAfterAFailedFirstLaunchStartsFresh(t *testing.T) {
	r := newRig(t)
	res := r.do(wire.ConversationCreateCmd{OpID: "c1", Cwd: "/w", Provider: wire.ProviderClaude,
		First: &wire.FirstMessage{ClientID: "first", Text: "start here", AttachmentIDs: []string{}}})
	r.id = *res.ID
	r.rt.LaunchFailed(r.id, wire.LaunchFailedCliMissing)
	r.tick(time.Second)
	r.send("m2", "and this", wire.ModeQueue)
	if st := r.state(); st.K != wire.StateStarting {
		t.Fatalf("state after a message: %+v", st)
	}
	if n := len(r.launcher.launched); n != 2 || r.launcher.launched[1].First != "start here" {
		t.Fatalf("launched %+v", r.launcher.launched)
	}
	if m := r.message("m2"); m.State.K != wire.MsgQueued || m.State.Reason != wire.QueuedLaunching {
		t.Fatalf("m2: %+v", m.State)
	}
}

// A launch whose prompt box never shows fails at its deadline: launch-failed, the queue paused.
func TestLaunchDeadline(t *testing.T) {
	r := newRig(t)
	res := r.do(wire.ConversationCreateCmd{OpID: "c1", Cwd: "/w", Provider: wire.ProviderClaude})
	r.id = *res.ID
	r.tick(time.Duration(wire.LaunchDeadline) * time.Millisecond)
	if st := r.state(); st.K != wire.StateLaunchFailed || st.Reason != string(wire.LaunchFailedNoPromptBox) {
		t.Fatalf("state: %+v", st)
	}
}

// Legacy's queue file: its messages become the conversation's, legacy-<id>, a day-old one failed{expired}.
func TestImportLegacyQueue(t *testing.T) {
	r := newRig(t)
	file := r.dir + "/prompt-queue.json"
	now := r.clock.Now().UnixMilli()
	body := `{"` + r.id + `":[{"id":"a1","text":"first","images":[],"createdAt":` + itoa(now-1000) + `},` +
		`{"id":"a2","text":"old","images":[],"createdAt":` + itoa(now-25*3600*1000) + `},` +
		`{"id":"a3","text":"gave up","images":[],"createdAt":` + itoa(now-1000) + `,"status":"failed","reason":"x"},` +
		`{"id":7,"text":"broken"}]}`
	if err := writeFile(file, body); err != nil {
		t.Fatal(err)
	}
	queues, err := LegacyQueue(file)
	if err != nil {
		t.Fatal(err)
	}
	r.user("busy")
	r.busy()
	if n := r.rt.Import([]ImportTab{{ID: r.id, Session: "s1", Provider: wire.ProviderClaude, TranscriptPath: r.path}}, queues); n != 1 {
		t.Fatalf("imported %d", n)
	}
	if n := r.rt.Import([]ImportTab{{ID: r.id, Session: "s1", Provider: wire.ProviderClaude, TranscriptPath: r.path}}, queues); n != 0 {
		t.Fatalf("imported again %d", n)
	}
	snap := r.snap()
	if len(snap.Queue.Items) != 1 || snap.Queue.Items[0] != "legacy-a1" {
		t.Fatalf("queue: %+v", snap.Queue)
	}
	for _, id := range []string{"legacy-a2", "legacy-a3"} {
		if m := r.message(id); m.State.K != wire.MsgFailed || m.State.Code != wire.MsgFailedExpired {
			t.Fatalf("%s: %+v", id, m.State)
		}
	}
}

// A Legacy layout tab: Claude's ids from agentState or the older top-level keys, a panel without an agent skipped.
func TestTabFromLayout(t *testing.T) {
	tab := jsvalue.NewObject("id", "tab-x1", "sessionName", "ws-1-x1", "name", "fix", "panelType", "claude-code",
		"claudeJsonlPath", "/h/.claude/projects/p/0b8f3c3e-1111-4222-8333-444455556666.jsonl")
	got, ok := TabFromLayout(tab, "/w")
	if !ok || got.Provider != wire.ProviderClaude || got.Session != "ws-1-x1" ||
		got.AgentSessionID != "0b8f3c3e-1111-4222-8333-444455556666" || got.TranscriptPath == "" {
		t.Fatalf("claude tab: %+v %v", got, ok)
	}
	codex := jsvalue.NewObject("id", "tab-x2", "panelType", "codex-cli",
		"agentState", jsvalue.NewObject("providerId", "codex", "sessionId", "s-1", "jsonlPath", "/c.jsonl"))
	if got, ok := TabFromLayout(codex, "/w"); !ok || got.TranscriptPath != "/c.jsonl" || got.AgentSessionID != "s-1" {
		t.Fatalf("codex tab: %+v", got)
	}
	if _, ok := TabFromLayout(jsvalue.NewObject("id", "tab-x3", "panelType", "terminal"), "/w"); ok {
		t.Fatal("a terminal is not a conversation")
	}
}

// Security review: a decision presses a number only on the menu the person saw. The screen changed to
// another tool call's menu under the gate: no key goes in, the stale gate closes, and the screen's own menu opens a
// gate of its own.
func TestDecideOnAMenuThatChangedPressesNothing(t *testing.T) {
	r := newRig(t)
	r.user("clean up")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.tool("toolu_rm", "rm -rf build")
	r.pane.screen = permissionScreen
	r.rt.Hook(r.id, Hook{Kind: "notification", Notification: "permission_prompt"})
	if k := r.state().K; k != wire.StateWaitingPermission {
		t.Fatalf("state %s", k)
	}
	r.tool("toolu_git", "git push --force")
	r.pane.screen = strings.NewReplacer("rm -rf build", "git push --force", "rm commands", "git commands").Replace(permissionScreen)
	res := r.do(wire.DecideCmd{OpID: "d1", ConversationID: r.id, PermissionID: "p:toolu_rm", OptionID: "1"})
	if res.R != wire.ResultAccepted {
		t.Fatalf("decide: %+v", res)
	}
	for _, c := range r.pane.calls {
		if strings.HasPrefix(c, "keys:") {
			t.Fatalf("a key went into another menu: %v", r.pane.calls)
		}
	}
	var stale, fresh *wire.Permission
	for _, p := range r.snap().Permissions {
		p := p
		if p.ID == "p:toolu_rm" {
			stale = &p
		} else {
			fresh = &p
		}
	}
	if stale == nil || stale.State.K != wire.GateStateClosed || stale.State.Reason != wire.ClosedAgentMovedOn {
		t.Fatalf("the stale gate: %+v", stale)
	}
	if fresh == nil || fresh.State.K != wire.GateStateOpen || len(fresh.Options) != 3 {
		t.Fatalf("the screen's own gate: %+v", fresh)
	}
}

// The same call asks with other options: no key goes in either, and its gate opens again, saying the page's decision
// did not reach the menu so the person decides in the terminal.
func TestDecideOnTheSameCallWithOtherOptionsOpensTheGateAgain(t *testing.T) {
	r := newRig(t)
	r.user("clean up")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.tool("toolu_rm", "rm -rf build")
	r.pane.screen = permissionScreen
	r.rt.Hook(r.id, Hook{Kind: "notification", Notification: "permission_prompt"})
	r.pane.screen = strings.Replace(permissionScreen, "Yes, and don't ask again", "Yes, and allow all", 1)
	r.do(wire.DecideCmd{OpID: "d1", ConversationID: r.id, PermissionID: "p:toolu_rm", OptionID: "2"})
	for _, c := range r.pane.calls {
		if strings.HasPrefix(c, "keys:") {
			t.Fatalf("a key went in: %v", r.pane.calls)
		}
	}
	if g := r.snap().Permissions[0].State; g.K != wire.GateStateOpen || g.Refused == nil || *g.Refused != wire.GateRefusedMenuUnmatched {
		t.Fatalf("gate: %+v", g)
	}
}

// A conversation that began in v2 learns its transcript late: the agent writes it after the first prompt and the sync
// finds it later still. A short turn can be over by then; the late transcript hands over its user entry to the message
// and its turn is not news: the conversation stays ready.
func TestATranscriptFoundAfterTheTurnEnded(t *testing.T) {
	r := newRig(t)
	r.rt.Import([]ImportTab{{ID: r.id, Session: "s1", Title: "work", Provider: wire.ProviderClaude, Cwd: "/w"}}, nil)
	r.settle(wire.StateReady)
	r.agentTakes()
	r.send("m1", "hello there", wire.ModeQueue)
	r.tick(time.Second)
	r.hook("prompt-submit")
	r.say("Hi!")
	r.turnEnd()
	r.idle()
	r.hook("stop")
	if k := r.state().K; k != wire.StateReady {
		t.Fatalf("after stop: %s", k)
	}

	r.rt.Attach(r.id, "s1", r.path, "")
	r.tick(time.Second)
	if k := r.state().K; k != wire.StateReady {
		t.Fatalf("the late transcript moved the conversation to %s", k)
	}
	snap := r.snap()
	if m := r.message("m1"); m.State.K != wire.MsgDone {
		t.Fatalf("the message is %s: %s", m.State.K, r.trail("m1"))
	}
	if len(snap.Turns) != 1 || snap.Turns[0].Status != wire.TurnDone {
		t.Fatalf("turns: %+v", snap.Turns)
	}
}

// The Stop hook can come before the transcript has the agent's last lines: the answer read after it still belongs to
// the turn the hook ended, with its counters, and the page is told the turn's record again.
func TestAnswerReadAfterTheStopHookStaysInItsTurn(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	r.agentTakes()
	r.send("m1", "hello there", wire.ModeQueue)
	r.tick(time.Second)
	r.hook("prompt-submit")
	r.hook("stop")
	r.idle()
	r.tick(time.Second)
	if k := r.state().K; k != wire.StateReady {
		t.Fatalf("after stop: %s", k)
	}
	r.say("Hi!")
	r.turnEnd()
	r.tick(time.Second)
	snap := r.snap()
	if len(snap.Turns) != 1 || snap.Turns[0].Status != wire.TurnDone || snap.Turns[0].AnswerEntryID == nil {
		t.Fatalf("turns: %+v", snap.Turns)
	}
	for _, e := range snap.Entries {
		if e.TurnID == nil || *e.TurnID != snap.Turns[0].ID {
			t.Fatalf("entry outside its turn: %+v", e)
		}
	}
	ended := r.events(wire.EvTurnFinished)
	if last := ended[len(ended)-1].e.(wire.TurnFinishedEv).Turn; last.AnswerEntryID == nil {
		t.Fatalf("the last turn_finished does not know the answer: %+v", last)
	}
	if k := r.state().K; k != wire.StateReady {
		t.Fatalf("the late answer moved the conversation to %s", k)
	}
}

// A Legacy tab keeps the title Legacy showed for it: not the tab's name «Claude Code». One an earlier import named after
// the tab gets that title too; a title the person typed in v2 stays.
func TestImportedTitlesAreLegacys(t *testing.T) {
	tab := jsvalue.NewObject("id", "tab-x1", "name", "Claude Code", "title", "acme", "panelType", "claude-code",
		"agentState", jsvalue.NewObject("providerId", "claude", "summary", "Доступ к dev-машине"))
	if got, _ := TabFromLayout(tab, "/w"); got.Title != "Доступ к dev-машине" || got.Name != "Claude Code" {
		t.Fatalf("from the summary: %+v", got)
	}
	bare := jsvalue.NewObject("id", "tab-x2", "name", "Claude Code", "title", "acme", "panelType", "claude-code")
	if got, _ := TabFromLayout(bare, "/w"); got.Title != "acme" {
		t.Fatalf("from the tab's title: %+v", got)
	}

	r := newRig(t)
	r.rt.Import([]ImportTab{{ID: r.id, Session: "s1", Title: "Claude Code", Name: "Claude Code", Provider: wire.ProviderClaude, Cwd: "/w"}}, nil)
	r.rt.call(context.Background(), r.id, func(c *conv, now time.Time) { c.rec.TitleSource = wire.TitleFromUser })
	r.rt.RetitleImported(ImportTab{ID: r.id, Title: "Почини сборку", Name: "Claude Code"})
	if c := r.rec(); c.Title != "Почини сборку" || c.TitleSource != wire.TitleFromAgent {
		t.Fatalf("named after the tab: %q %s", c.Title, c.TitleSource)
	}
	r.do(wire.ConversationRenameCmd{OpID: "rn", ID: r.id, Title: "Мой чат"})
	r.rt.RetitleImported(ImportTab{ID: r.id, Title: "Другое", Name: "Claude Code"})
	if c := r.rec(); c.Title != "Мой чат" {
		t.Fatalf("typed by the person: %q", c.Title)
	}
}

func (r *rig) rec() wire.Conversation {
	r.t.Helper()
	all, err := r.rt.Conversations(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	for _, c := range all.Conversations {
		if c.ID == r.id {
			return c
		}
	}
	r.t.Fatalf("no conversation %s", r.id)
	return wire.Conversation{}
}

// a working agent whose spinner the screen does not show (a long todo list pushed it up) goes quiet for the
// runtime, its summary under the turn. Its turn ends as done, never as stopped by the person, and the lines the agent writes on open it again.
func TestQuietAgentThatWritesOnWorksAgain(t *testing.T) {
	r := newRig(t)
	r.user("build it")
	r.tool("t1", "make")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)

	r.done()
	for range 20 {
		r.tick(time.Second)
	}
	if k := r.state().K; k != wire.StateIdle {
		t.Fatalf("after the silence: %s", k)
	}
	snap := r.snap()
	if len(snap.Turns) != 1 || snap.Turns[0].Status != wire.TurnDone {
		t.Fatalf("the quiet turn: %+v", snap.Turns)
	}

	r.result("t1", "ok")
	r.say("Built.")
	r.tick(time.Second)
	if k := r.state().K; k != wire.StateWorking {
		t.Fatalf("the agent writes on, the state is %s", k)
	}
	snap = r.snap()
	if len(snap.Turns) != 1 || snap.Turns[0].Status != wire.TurnLive || snap.Turns[0].EndedAt != nil {
		t.Fatalf("the turn did not open again: %+v", snap.Turns)
	}
	for _, e := range snap.Entries {
		if e.TurnID == nil || *e.TurnID != snap.Turns[0].ID {
			t.Fatalf("entry outside its turn: %+v", e)
		}
	}

	r.turnEnd()
	r.hook("stop")
	r.tick(time.Second)
	if k := r.state().K; k != wire.StateReady {
		t.Fatalf("after its stop: %s", k)
	}
	if st := r.snap().Turns[0].Status; st != wire.TurnDone {
		t.Fatalf("the turn ends as %s", st)
	}
}

// The spinner seen again after the silence opens the turn again too.
func TestQuietAgentSeenBusyWorksAgain(t *testing.T) {
	r := newRig(t)
	r.user("build it")
	r.tool("t1", "make")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.idle()
	for range 20 {
		r.tick(time.Second)
	}
	r.busy()
	r.tick(time.Second)
	if k := r.state().K; k != wire.StateWorking {
		t.Fatalf("the spinner is back, the state is %s", k)
	}
	if st := r.snap().Turns[0].Status; st != wire.TurnLive {
		t.Fatalf("the turn is %s", st)
	}
}

// Our Escape still stops: the turn is the person's stop and the screen at rest does not open it again.
func TestEscapedTurnStaysInterrupted(t *testing.T) {
	r := newRig(t)
	r.user("build it")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.interruptLine()
	r.idle()
	for range 20 {
		r.tick(time.Second)
	}
	if st := r.snap().Turns[0].Status; st != wire.TurnInterrupted {
		t.Fatalf("the escaped turn is %s", st)
	}
	if k := r.state().K; k != wire.StateIdle {
		t.Fatalf("after the escape: %s", k)
	}
}

// compactTurn starts the rig on a working agent mid-turn: a prompt, a tool run, the spinner on screen.
func compactTurn(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	r.user("build it")
	r.tool("t1", "make")
	r.result("t1", "ok")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	return r
}

// stepsSaid is the say entries the conv channel published, by text, with their turns.
func (r *rig) stepsSaid() map[string]string {
	out := map[string]string{}
	for _, p := range r.events(wire.EvStep) {
		ev := p.e.(wire.StepEv)
		if ev.Entry.T == wire.EntrySay && ev.Entry.TurnID != nil {
			out[ev.Entry.MD] = *ev.Entry.TurnID
		}
	}
	return out
}

// assertNoSummaryAsUser: the compaction's summary is no message of the person's.
func (r *rig) assertNoSummaryAsUser() {
	r.t.Helper()
	for _, e := range r.snap().Entries {
		if e.T == wire.EntryUser && strings.Contains(e.Text, "continued from a previous conversation") {
			r.t.Fatalf("the compaction summary shows as a user message: %+v", e)
		}
	}
}

// auto-compact посреди хода. Claude compacts for ~50 s: PreCompact, nothing written to the transcript, the
// screen showing only "✻ Compacting conversation…" (a narrow pane: no "esc to interrupt"). Then compact_boundary, the
// summary and the agent's new lines. The turn stays open throughout, the new lines land in it, and Stop ends it.
func TestAutoCompactMidTurn(t *testing.T) {
	r := compactTurn(t)
	turn := r.snap().Turns[0].ID

	r.hook("pre-compact")
	if st := r.state(); st.K != wire.StateWorking || !st.Compacting {
		t.Fatalf("after pre-compact: %+v", st)
	}
	r.compactingScreen()
	for i := range 50 {
		r.tick(time.Second)
		if k := r.state().K; k != wire.StateWorking {
			t.Fatalf("%ds into the compaction the state is %s", i+1, k)
		}
	}
	if n := len(r.events(wire.EvTurnFinished)); n != 0 {
		t.Fatalf("the turn finished %d times while compacting", n)
	}

	r.compactBoundary(48000)
	r.compactSummary()
	r.hook("post-compact")
	r.hook("session-start")
	r.say("Back to it after the compaction.")
	r.tool("t2", "make test")
	r.busy()
	r.tick(time.Second)

	st := r.state()
	if st.K != wire.StateWorking || st.Compacting || st.TurnID == nil || *st.TurnID != turn {
		t.Fatalf("after the compaction: %+v", st)
	}
	snap := r.snap()
	if len(snap.Turns) != 1 || snap.Turns[0].ID != turn || snap.Turns[0].Status != wire.TurnLive {
		t.Fatalf("turns after the compaction: %+v", snap.Turns)
	}
	var compaction bool
	for _, e := range snap.Entries {
		if e.TurnID == nil || *e.TurnID != turn {
			t.Fatalf("entry outside the turn: %+v", e)
		}
		if e.T == wire.EntryCompaction {
			compaction = e.TokensBefore != nil && *e.TokensBefore == 604330
		}
	}
	if !compaction {
		t.Fatalf("no compaction entry with its tokens: %+v", snap.Entries)
	}
	r.assertNoSummaryAsUser()
	if got := r.stepsSaid()["Back to it after the compaction."]; got != turn {
		t.Fatalf("the line after the compaction went out in turn %q", got)
	}

	r.result("t2", "ok")
	r.say("Done.")
	r.turnEnd()
	r.hook("stop")
	r.idle()
	r.tick(time.Second)
	if k := r.state().K; k != wire.StateReady {
		t.Fatalf("after its stop: %s", k)
	}
	if fin := r.events(wire.EvTurnFinished); len(fin) != 1 || fin[0].e.(wire.TurnFinishedEv).Turn.Status != wire.TurnDone {
		t.Fatalf("the turn finished as %+v", fin)
	}
}

// Compacting holds the silence off even when the screen says nothing (the spinner unread), and the transcript's
// compaction line ends it when the PostCompact hook never comes: silence counts again after it.
func TestCompactingHoldsQuietUntilTheBoundary(t *testing.T) {
	r := compactTurn(t)
	r.hook("pre-compact")
	r.done()
	for range 60 {
		r.tick(time.Second)
	}
	if st := r.state(); st.K != wire.StateWorking || !st.Compacting || r.snap().Turns[0].Status != wire.TurnLive {
		t.Fatalf("a minute into the compaction: %+v", st)
	}

	r.compactBoundary(60000)
	r.compactSummary()
	r.tick(time.Second)
	if st := r.state(); st.K != wire.StateWorking || st.Compacting {
		t.Fatalf("the boundary did not end the compaction: %+v", st)
	}
	r.assertNoSummaryAsUser()

	for range 20 {
		r.tick(time.Second)
	}
	if k := r.state().K; k != wire.StateIdle {
		t.Fatalf("silence after the compaction: %s", k)
	}
}

// A SessionStart (source "compact") ends a compaction whose PostCompact is lost.
func TestSessionStartEndsCompaction(t *testing.T) {
	r := compactTurn(t)
	r.hook("pre-compact")
	r.hook("session-start")
	if st := r.state(); st.K != wire.StateWorking || st.Compacting {
		t.Fatalf("after session-start: %+v", st)
	}
}

// A compaction whose end no one reports holds the silence off for CompactHold, not forever.
func TestCompactHoldLetsGo(t *testing.T) {
	r := compactTurn(t)
	r.hook("pre-compact")
	r.done()
	for range 9 {
		r.tick(time.Minute)
	}
	if k := r.state().K; k != wire.StateWorking {
		t.Fatalf("within the hold: %s", k)
	}
	r.tick(time.Minute)
	for range 20 {
		r.tick(time.Second)
	}
	if k := r.state().K; k != wire.StateIdle {
		t.Fatalf("past the hold: %s", k)
	}
}

// No compaction hooks and no spinner read (an older Claude, a pane that hides it): the silence and a summary end the turn, and the
// lines after the compaction open it again — the same turn, every line shown.
func TestQuietTurnReopensAfterCompaction(t *testing.T) {
	r := compactTurn(t)
	turn := r.snap().Turns[0].ID
	r.done()
	for range 50 {
		r.tick(time.Second)
	}
	if k := r.state().K; k != wire.StateIdle {
		t.Fatalf("after 50 s of silence: %s", k)
	}

	r.compactBoundary(48000)
	r.compactSummary()
	r.say("Back to it after the compaction.")
	r.tick(time.Second)
	st := r.state()
	if st.K != wire.StateWorking || st.TurnID == nil || *st.TurnID != turn {
		t.Fatalf("the lines after the compaction did not open the turn again: %+v", st)
	}
	snap := r.snap()
	if len(snap.Turns) != 1 || snap.Turns[0].Status != wire.TurnLive {
		t.Fatalf("turns: %+v", snap.Turns)
	}
	if started := r.events(wire.EvTurnStarted); len(started) == 0 || started[len(started)-1].e.(wire.TurnStartedEv).Turn.ID != turn {
		t.Fatalf("no turn_started for the reopened turn: %+v", started)
	}
	if got := r.stepsSaid()["Back to it after the compaction."]; got != turn {
		t.Fatalf("the line after the compaction went out in turn %q", got)
	}
	r.assertNoSummaryAsUser()

	r.say("Done.")
	r.turnEnd()
	r.hook("stop")
	r.tick(time.Second)
	if k := r.state().K; k != wire.StateReady {
		t.Fatalf("after its stop: %s", k)
	}
	if s := r.snap().Turns[0].Status; s != wire.TurnDone {
		t.Fatalf("the turn ends as %s", s)
	}
}

// A PreCompact after the silence ended the turn opens it again: the agent compacts mid-turn, it is at work.
func TestPreCompactReopensQuietTurn(t *testing.T) {
	r := compactTurn(t)
	turn := r.snap().Turns[0].ID
	r.done()
	for range 20 {
		r.tick(time.Second)
	}
	if k := r.state().K; k != wire.StateIdle {
		t.Fatalf("after the silence: %s", k)
	}
	r.hook("pre-compact")
	st := r.state()
	if st.K != wire.StateWorking || !st.Compacting || st.TurnID == nil || *st.TurnID != turn {
		t.Fatalf("after pre-compact: %+v", st)
	}
	if s := r.snap().Turns[0].Status; s != wire.TurnLive {
		t.Fatalf("the turn is %s", s)
	}
	for range 50 {
		r.tick(time.Second)
	}
	if k := r.state().K; k != wire.StateWorking {
		t.Fatalf("during the compaction: %s", k)
	}
}

// A chat no one picked a model for shows the model its agent answers with; one picked stays as picked.
func TestModelFromTheTranscript(t *testing.T) {
	r := newRig(t)
	r.user("hi")
	r.line(`"type":"assistant","message":{"role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Hello"}],"usage":{"input_tokens":3,"output_tokens":1}}`)
	r.open()
	r.settle(wire.StateWorking)
	cs, err := r.rt.Conversations(context.Background())
	if err != nil || len(cs.Conversations) != 1 {
		t.Fatalf("conversations: %v", err)
	}
	if m := cs.Conversations[0].Model; m == nil || m.ID != "claude-opus-5-5" || m.Effort != nil {
		t.Fatalf("model: %+v", m)
	}
}

// The window is as full as the agent's last answer was asked with.
func TestContextIsTheLastAnswersInput(t *testing.T) {
	r := newRig(t)
	r.user("hi")
	r.line(`"type":"assistant","message":{"role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Hello"}],"usage":{"input_tokens":12,"cache_creation_input_tokens":1000,"cache_read_input_tokens":40000,"output_tokens":5}}`)
	r.open()
	r.settle(wire.StateWorking)
	got, err := r.rt.Context(context.Background(), r.id)
	if err != nil || got.UsedTokens != 41012 || got.LimitTokens != wire.ContextWindow {
		t.Fatalf("context: %+v %v", got, err)
	}
}

// A send the page repeats — its answer was lost, a gateway answered 502 in front of the runtime, the runtime restarted
// in a rollout — is the same message: the same answer, no second copy in the queue, no second event. A refusal that is
// only «no time» or «not now» (timeout, internal) is not kept as the answer, so the repeat is tried again.
func TestASendRepeatedByThePageIsTheSameMessage(t *testing.T) {
	r := newRig(t)
	r.open()
	first := r.send("m1", "once", wire.ModeQueue)
	if first.R != wire.ResultAccepted && first.R != wire.ResultQueued {
		t.Fatalf("send: %+v", first)
	}
	events := len(r.events(wire.EvMessageUpsert)) + len(r.events(wire.EvPromptState))
	for i, step := range []string{"again", "after a restart"} {
		if step == "after a restart" {
			r.rt.Stop()
			r.rt = r.runtime()
			r.rt.Start(context.Background())
		}
		again := r.send("m1", "once", wire.ModeQueue)
		if again.R != first.R || again.Seq != first.Seq || again.Ch != first.Ch {
			t.Fatalf("%d (%s): %+v, the first was %+v", i, step, again, first)
		}
		if step == "again" {
			if n := len(r.events(wire.EvMessageUpsert)) + len(r.events(wire.EvPromptState)); n != events {
				t.Fatalf("the repeat published %d more events", n-events)
			}
		}
	}
	count := 0
	for _, m := range r.snap().Messages {
		if m.ClientID == "m1" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d copies of the message", count)
	}
}

// Claude ends a turn whose call to the model was refused (the login expired) with a plain stop and the refusal as its
// last line: the turn failed with what Claude said, it is not a finished turn with nothing in it.
func TestATurnThatStopsOnTheModelsRefusalFailed(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	r.agentTakes()
	r.send("m1", "read a file", wire.ModeQueue)
	r.tick(time.Second)
	r.hook("prompt-submit")
	r.line(`"type":"assistant","isApiErrorMessage":true,"error":"authentication_failed","message":{"role":"assistant","model":"<synthetic>","content":[{"type":"text","text":"Login expired · Please run /login"}]}`)
	r.idle()
	r.hook("stop")
	if k := r.state().K; k != wire.StateReady {
		t.Fatalf("after stop: %s", k)
	}
	snap := r.snap()
	if len(snap.Turns) != 1 || snap.Turns[0].Status != wire.TurnFailed || snap.Turns[0].Failure == nil {
		t.Fatalf("turns: %+v", snap.Turns)
	}
	f := snap.Turns[0].Failure
	if f.Code != wire.TurnFailedApiError || f.Detail == nil || *f.Detail != "Login expired · Please run /login" {
		t.Fatalf("failure: %+v", f)
	}
	if n := len(r.events(wire.EvTurnFailed)); n != 1 {
		t.Fatalf("%d turn_failed", n)
	}
}

// The same refusal when the transcript ends the turn itself (its turn_duration line) before any Stop hook.
func TestATurnTheTranscriptEndsOnTheModelsRefusalFailed(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	r.agentTakes()
	r.send("m1", "read a file", wire.ModeQueue)
	r.tick(time.Second)
	r.hook("prompt-submit")
	r.line(`"type":"assistant","isApiErrorMessage":true,"error":"authentication_failed","message":{"role":"assistant","model":"<synthetic>","content":[{"type":"text","text":"Login expired · Please run /login"}]}`)
	r.line(`"type":"system","subtype":"turn_duration","durationMs":400`)
	r.idle()
	r.tick(time.Second)
	if k := r.state().K; k != wire.StateReady {
		t.Fatalf("after stop: %s", k)
	}
	snap := r.snap()
	if len(snap.Turns) != 1 || snap.Turns[0].Status != wire.TurnFailed || snap.Turns[0].Failure == nil {
		t.Fatalf("turns: %+v", snap.Turns)
	}
	f := snap.Turns[0].Failure
	if f.Code != wire.TurnFailedApiError || f.Detail == nil || *f.Detail != "Login expired · Please run /login" {
		t.Fatalf("failure: %+v", f)
	}
	if n := len(r.events(wire.EvTurnFailed)); n != 1 {
		t.Fatalf("%d turn_failed", n)
	}
}

// A conversation delegated by another one names it as its parent and takes the title it was given; a parent the
// runtime does not have is refused.
func TestCreateUnderAParent(t *testing.T) {
	r := newRig(t)
	parent := r.do(wire.ConversationCreateCmd{OpID: "p", Cwd: "/w", Provider: wire.ProviderClaude})
	res := r.do(wire.ConversationCreateCmd{OpID: "c", Cwd: "/w", Provider: wire.ProviderCodex, ParentID: *parent.ID,
		Title: "  Review the parser  ", First: &wire.FirstMessage{ClientID: "f", Text: "look at parse.go", AttachmentIDs: []string{}}})
	rec, ok := r.rt.Record(*res.ID)
	if !ok || rec.ParentID == nil || *rec.ParentID != *parent.ID {
		t.Fatalf("parent: %+v", rec.ParentID)
	}
	if rec.Title != "Review the parser" || rec.TitleSource != wire.TitleFromUser {
		t.Fatalf("title: %q %s", rec.Title, rec.TitleSource)
	}
	if err := rec.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := r.do(wire.ConversationCreateCmd{OpID: "x", Cwd: "/w", Provider: wire.ProviderClaude, ParentID: "tab-nope"}); got.R != wire.ResultRefused || got.Code != wire.RefuseNotFound {
		t.Fatalf("unknown parent: %+v", got)
	}
}
