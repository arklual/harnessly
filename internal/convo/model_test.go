package convo

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// agentSays plays Claude answering the commands typed into its box with the line it prints.
func (r *rig) agentSays(answers map[string]string) {
	r.pane.onSend = func(call string) {
		if line, ok := answers[strings.TrimPrefix(call, "line:")]; ok && strings.HasPrefix(call, "line:") {
			r.pane.screen = tui(r.t, "claude-idle-placeholder.ansi.txt") + "\n  ⎿  " + line + "\n"
		}
	}
}

func (r *rig) modelOf() *wire.Model {
	r.t.Helper()
	cs, err := r.rt.Conversations(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	for _, c := range cs.Conversations {
		if c.ID == r.id {
			return c.Model
		}
	}
	r.t.Fatal("no conversation")
	return nil
}

func TestAnIdleClaudeSwitchesModelAndEffort(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	low := wire.EffortLow
	r.agentSays(map[string]string{"/model sonnet": "Set model to Sonnet 5", "/effort low": "Set effort level to low"})
	res := r.do(wire.ModelCmd{OpID: "mo1", ID: r.id, Model: "sonnet", Effort: &low})
	if res.R != wire.ResultAccepted || res.Ch != wire.ChannelConvs || res.Rev == nil {
		t.Fatalf("%+v", res)
	}
	if got := r.pane.calls; !reflect.DeepEqual(got, []string{"line:/model sonnet", "line:/effort low"}) {
		t.Fatalf("%v", got)
	}
	if m := r.modelOf(); m == nil || m.ID != "sonnet" || m.Effort == nil || *m.Effort != wire.EffortLow {
		t.Fatalf("%+v", m)
	}
	// The record's change went out on convs, ahead of the seq the answer names.
	ups := r.events(wire.EvConversationUpsert)
	last := ups[len(ups)-1]
	if last.seq != res.Seq || last.e.(wire.ConversationUpsertEv).Conv.Model.ID != "sonnet" {
		t.Fatalf("answer seq %d, last upsert %+v", res.Seq, last)
	}
}

func TestOnlyTheEffortChangesWhenTheModelIsTheSame(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	r.agentSays(map[string]string{"/model opus": "Set model to Opus 5.5", "/effort high": "Set effort level to high"})
	high := wire.EffortHigh
	if res := r.do(wire.ModelCmd{OpID: "mo1", ID: r.id, Model: "opus"}); res.R != wire.ResultAccepted {
		t.Fatalf("%+v", res)
	}
	r.pane.calls = nil
	if res := r.do(wire.ModelCmd{OpID: "mo2", ID: r.id, Model: "opus", Effort: &high}); res.R != wire.ResultAccepted {
		t.Fatalf("%+v", res)
	}
	if got := r.pane.calls; !reflect.DeepEqual(got, []string{"line:/effort high"}) {
		t.Fatalf("%v", got)
	}
	r.pane.calls = nil
	if res := r.do(wire.ModelCmd{OpID: "mo3", ID: r.id, Model: "opus", Effort: &high}); res.R != wire.ResultAccepted || len(r.pane.calls) != 0 {
		t.Fatalf("a model already set is typed again: %+v %v", res, r.pane.calls)
	}
}

// agentPlays is a Claude that answers the slash commands it is given (answers, by the line) and takes any other line as
// a message: it leaves the box, the screen turns busy and the transcript gets the user line.
func (r *rig) agentPlays(answers map[string]string) {
	busy := tui(r.t, "claude-busy-spinner.ansi.txt")
	r.pane.onSend = func(call string) {
		text, ok := strings.CutPrefix(call, "line:")
		if !ok {
			return
		}
		if line, ok := answers[text]; ok {
			r.pane.screen = tui(r.t, "claude-idle-placeholder.ansi.txt") + "\n  ⎿  " + line + "\n"
			return
		}
		r.user(text)
		r.pane.screen = busy
	}
}

var switchAnswers = map[string]string{
	"/model sonnet": "Set model to Sonnet 5.5", "/effort low": "Set effort level to low",
	"/model opus": "Set model to Opus 5.5", "/effort high": "Set effort level to high",
}

// A model asked during a turn is not refused and does not make the person wait: the answer is at once, nothing is
// typed into the working agent, and the change goes in when the turn ends — before the message queued behind it.
func TestAModelAskedDuringATurnWaitsForItsEndAndGoesBeforeTheNextMessage(t *testing.T) {
	r := newRig(t)
	r.user("first")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.agentPlays(switchAnswers)
	low := wire.EffortLow
	res := r.do(wire.ModelCmd{OpID: "mo1", ID: r.id, Model: "sonnet", Effort: &low})
	if res.R != wire.ResultAccepted || res.Ch != wire.ChannelConvs || res.Rev == nil {
		t.Fatalf("%+v", res)
	}
	r.send("m1", "next please", wire.ModeQueue)
	rec := r.record()
	if p := rec.PendingModel; p == nil || p.ID != "sonnet" || p.Effort == nil || *p.Effort != low || p.Applying || rec.Model != nil {
		t.Fatalf("pending %+v, model %+v", rec.PendingModel, rec.Model)
	}
	if len(r.pane.calls) != 0 {
		t.Fatalf("typed into a working agent: %v", r.pane.calls)
	}
	// The record carried the pending change on convs, ahead of the seq the answer names.
	last := r.events(wire.EvConversationUpsert)
	if len(last) == 0 || last[len(last)-1].e.(wire.ConversationUpsertEv).Conv.PendingModel == nil {
		t.Fatalf("pending change not published")
	}

	r.say("done")
	r.turnEnd()
	r.idle()
	r.hook("stop")
	if got := r.pane.calls; !reflect.DeepEqual(got, []string{"line:/model sonnet", "line:/effort low", "line:next please"}) {
		t.Fatalf("order of typing: %v", got)
	}
	rec = r.record()
	if rec.PendingModel != nil || rec.Model == nil || rec.Model.ID != "sonnet" || *rec.Model.Effort != low {
		t.Fatalf("after: pending %+v, model %+v", rec.PendingModel, rec.Model)
	}
	if len(rec.ModelLog) != 1 || rec.ModelLog[0].From != nil || rec.ModelLog[0].To.ID != "sonnet" {
		t.Fatalf("log %+v", rec.ModelLog)
	}
}

// The latest ask replaces the one waiting; asking for what the agent runs on takes the waiting one back.
func TestAWaitingModelIsReplacedAndCanBeTakenBack(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	r.agentPlays(switchAnswers)
	if res := r.do(wire.ModelCmd{OpID: "mo0", ID: r.id, Model: "opus"}); res.R != wire.ResultAccepted {
		t.Fatalf("%+v", res)
	}
	r.pane.calls = nil
	r.busy()
	r.user("work")
	r.tick(time.Second)
	r.settle(wire.StateWorking)
	r.do(wire.ModelCmd{OpID: "mo1", ID: r.id, Model: "sonnet"})
	high := wire.EffortHigh
	r.do(wire.ModelCmd{OpID: "mo2", ID: r.id, Model: "opus", Effort: &high})
	if p := r.record().PendingModel; p == nil || p.ID != "opus" || *p.Effort != high {
		t.Fatalf("pending %+v", p)
	}
	// Back to opus, which the agent runs on, with the effort it has none on record for: still a change.
	r.do(wire.ModelCmd{OpID: "mo3", ID: r.id, Model: "opus", Effort: &high})
	r.do(wire.ModelCmd{OpID: "mo4", ID: r.id, Model: "opus"})
	if p := r.record().PendingModel; p != nil {
		t.Fatalf("the ask for the same model should take the waiting one back: %+v", p)
	}
	if len(r.pane.calls) != 0 {
		t.Fatalf("typed: %v", r.pane.calls)
	}
}

// An agent that is not started takes the change at once, as a record, and types nothing; a message then launches the
// agent, the change is applied the moment it is ready and the message goes after it — one chain, in order.
func TestAModelAskedOfAnAgentThatIsNotStartedIsAppliedAtLaunchBeforeTheFirstMessage(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	r.machine.probes["s1"] = Probe{Session: true, Shell: true, Resumable: true}
	r.tick(time.Second)
	if st := r.state(); st.K != wire.StateStopped || !st.Resumable {
		t.Fatalf("state %+v", st)
	}
	r.agentPlays(switchAnswers)
	high := wire.EffortHigh
	res := r.do(wire.ModelCmd{OpID: "mo1", ID: r.id, Model: "opus", Effort: &high})
	if res.R != wire.ResultAccepted || res.Rev == nil {
		t.Fatalf("%+v", res)
	}
	if p := r.record().PendingModel; p == nil || p.ID != "opus" {
		t.Fatalf("pending %+v", p)
	}
	if len(r.pane.calls) != 0 || len(r.launcher.resumed) != 0 {
		t.Fatalf("a change alone starts nothing: %v %v", r.pane.calls, r.launcher.resumed)
	}

	r.send("m1", "hello", wire.ModeQueue)
	if !reflect.DeepEqual(r.launcher.resumed, []string{r.id}) {
		t.Fatalf("the message should launch the agent: %v", r.launcher.resumed)
	}
	if st := r.state(); st.K != wire.StateStarting {
		t.Fatalf("state %+v", st)
	}
	if len(r.pane.calls) != 0 {
		t.Fatalf("typed into a starting agent: %v", r.pane.calls)
	}

	r.machine.probes["s1"] = Probe{Session: true, Foreground: true}
	r.rt.LaunchReady(r.id)
	if got := r.pane.calls; !reflect.DeepEqual(got, []string{"line:/model opus", "line:/effort high", "line:hello"}) {
		t.Fatalf("order of the chain: %v", got)
	}
	rec := r.record()
	if rec.PendingModel != nil || rec.Model == nil || rec.Model.ID != "opus" || len(rec.ModelLog) != 1 {
		t.Fatalf("after: %+v", rec)
	}
}

// A change the agent's box is not ready for is asked again soon and stays pending meanwhile; it is not lost.
func TestAWaitingModelIsAskedAgainWhenTheBoxIsNotReady(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	r.agentPlays(switchAnswers)
	r.busy()
	r.user("work")
	r.tick(time.Second)
	r.settle(wire.StateWorking)
	r.do(wire.ModelCmd{OpID: "mo1", ID: r.id, Model: "sonnet"})
	r.say("done")
	r.turnEnd()
	r.hook("stop") // the box still draws the spinner: the command is not typed into it
	if len(r.pane.calls) != 0 || r.record().PendingModel == nil {
		t.Fatalf("calls %v, pending %+v", r.pane.calls, r.record().PendingModel)
	}
	r.idle()
	for range 10 {
		r.tick(2 * time.Second)
	}
	if got := r.pane.calls; !reflect.DeepEqual(got, []string{"line:/model sonnet"}) {
		t.Fatalf("%v", got)
	}
	if rec := r.record(); rec.PendingModel != nil || rec.Model == nil || rec.Model.ID != "sonnet" {
		t.Fatalf("%+v", rec)
	}
}

func TestAModelTheAgentDoesNotTakeIsRefusedAndNotRecorded(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	res := r.do(wire.ModelCmd{OpID: "mo1", ID: r.id, Model: "ghost"}) // the screen never says «Set model to»
	if res.R != wire.ResultRefused || res.Code != wire.RefuseInternal {
		t.Fatalf("%+v", res)
	}
	if m := r.modelOf(); m != nil {
		t.Fatalf("recorded %+v", m)
	}
}

func TestACodexConversationHasNoModelCommand(t *testing.T) {
	r := newRig(t)
	r.rt.Import([]ImportTab{{ID: "tab-cx", Session: "s1", Title: "x", Provider: wire.ProviderCodex, Cwd: "/w", TranscriptPath: r.path}}, nil)
	if res := r.do(wire.ModelCmd{OpID: "mo1", ID: "tab-cx", Model: "gpt-5"}); res.R != wire.ResultRefused || res.Code != wire.RefuseUnsupported {
		t.Fatalf("%+v", res)
	}
}

func TestAnEffortTheModelDoesNotTakeIsRefusedBeforeAnyKey(t *testing.T) {
	r := newRig(t)
	r.rt.d.Models = func(wire.Provider) []wire.AgentModel {
		return []wire.AgentModel{{ID: "haiku", Label: "Haiku", Efforts: []wire.Effort{}}, {ID: "opus", Label: "Opus", Efforts: wire.Efforts}}
	}
	r.open()
	r.settle(wire.StateReady)
	low := wire.EffortLow
	if res := r.do(wire.ModelCmd{OpID: "mo1", ID: r.id, Model: "haiku", Effort: &low}); res.R != wire.ResultRefused || res.Code != wire.RefuseOptionUnknown {
		t.Fatalf("%+v", res)
	}
	if res := r.do(wire.ModelCmd{OpID: "mo2", ID: r.id, Model: "Opus;rm"}); res.R != wire.ResultRefused || res.Code != wire.RefuseBadRequest {
		t.Fatalf("%+v", res)
	}
	if len(r.pane.calls) != 0 {
		t.Fatalf("%v", r.pane.calls)
	}
}
