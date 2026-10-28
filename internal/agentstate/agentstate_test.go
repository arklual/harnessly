package agentstate

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

const at = wire.Ms(1_000_000)

func ptr(s string) *string { return &s }

// states are one state of each kind, in the order of wire.AgentStateKinds; the starting one began at 0, so at is past
// its deadline.
var states = []wire.AgentState{
	{K: wire.StateStarting, Step: wire.StepMachine, Wait: &wire.Wait{SlowAt: wire.LaunchSlowMs, DeadlineAt: wire.LaunchDeadline}},
	{K: wire.StateWorking, Since: 10, TurnID: ptr("c:aa#0")},
	{K: wire.StateWaitingQuestion, Since: 10, QuestionID: "q:tu1", TurnID: ptr("c:aa#0")},
	{K: wire.StateWaitingPermission, Since: 10, PermissionID: "p:tu2", TurnID: ptr("c:aa#0")},
	{K: wire.StateReady, Since: 10},
	{K: wire.StateIdle, Since: 10},
	{K: wire.StateStopped, Since: 10, Resumable: true},
	{K: wire.StateLaunchFailed, Since: 10, Reason: string(wire.LaunchFailedNoProcess)},
	{K: wire.StateGone, Since: 10, Reason: string(wire.GoneTmuxLost)},
	{K: wire.StateUnknown, Since: 10},
}

// cells is the whole table, one row per input and one column per state in the order of states: "-" no change and
// nothing to do, else the kind it goes to and, after "+", its effects in order.
var cells = []struct {
	name string
	in   Input
	row  string
}{
	{"launch-requested", Input{K: LaunchRequested}, "starting · - · starting · starting · starting · starting · starting · starting · starting · starting"},
	{"launch-step", Input{K: LaunchStep, Step: wire.StepCli}, "starting · - · - · - · - · - · - · - · - · -"},
	{"launch-ready", Input{K: LaunchReady}, "idle+wake-queue · - · - · - · - · - · idle+wake-queue · idle+wake-queue · - · idle+wake-queue"},
	{"launch-failed", Input{K: LaunchFailed, Failure: wire.LaunchFailedCliMissing}, "launch-failed+pause-queue · - · - · - · - · - · - · - · - · -"},
	{"tick past the deadline", Input{K: Tick}, "launch-failed+pause-queue · - · - · - · - · - · - · - · - · -"},
	{"tick before it", Input{K: Tick, At: 1000}, "- · - · - · - · - · - · - · - · - · -"},
	{"session-start", Input{K: SessionStart}, "idle+wake-queue · - · - · - · idle · idle · idle+wake-queue · idle+wake-queue · - · idle+wake-queue"},
	{"prompt-submit", Input{K: PromptSubmit}, "working+prompt-submitted · working+prompt-submitted · working+prompt-submitted,gate-closed · working+prompt-submitted,gate-closed · working+prompt-submitted · working+prompt-submitted · - · - · - · working+prompt-submitted"},
	// Ready and idle too: a background agent asks after the turn ended.
	{"permission-prompt", Input{K: PermissionPrompt, GateID: "p:tab-1:7"}, "- · waiting-permission+permission-requested · - · - · waiting-permission+permission-requested · waiting-permission+permission-requested · - · - · - · waiting-permission+permission-requested"},
	{"pre-compact", Input{K: PreCompact}, "- · working+compaction-started · - · - · working+compaction-started · working+compaction-started · - · - · - · working+compaction-started"},
	{"post-compact", Input{K: PostCompact}, "- · working+compaction-finished · - · - · - · - · - · - · - · -"},
	{"stop", Input{K: Stop}, "- · ready+turn-finished,wake-queue,notify · ready+gate-closed,turn-finished,wake-queue,notify · ready+gate-closed,turn-finished,wake-queue,notify · - · - · - · - · - · -"},
	{"stop-failure", Input{K: StopFailure, TurnFailure: wire.TurnFailedRateLimit}, "- · ready+turn-failed,wake-queue,notify · ready+gate-closed,turn-failed,wake-queue,notify · ready+gate-closed,turn-failed,wake-queue,notify · - · - · - · - · - · -"},
	{"turn-opened", Input{K: TurnOpened, TurnID: "c:bb#0"}, "- · working+turn-started · - · - · working+turn-started · working+turn-started · - · - · - · working+turn-started"},
	{"turn-opened again", Input{K: TurnOpened, TurnID: "c:aa#0"}, "- · - · - · - · working+turn-started · working+turn-started · - · - · - · working+turn-started"},
	{"question-asked", Input{K: QuestionAsked, GateID: "q:tu9"}, "- · waiting-question+question-asked · - · - · - · - · - · - · - · waiting-question+question-asked"},
	{"plan-proposed", Input{K: PlanProposed, GateID: "p:tu8"}, "- · waiting-permission+plan-proposed · - · - · - · - · - · - · - · waiting-permission+plan-proposed"},
	{"gate-resolved", Input{K: GateResolved}, "- · - · working+gate-moved-on · working+gate-moved-on · - · - · - · - · - · -"},
	{"screen-busy", Input{K: ScreenBusy}, "- · - · - · - · - · - · working · - · - · working"},
	{"quiet, the turn ended", Input{K: Quiet, TurnEnded: true}, "- · ready+turn-finished,wake-queue · - · - · - · - · - · - · - · ready+wake-queue"},
	{"quiet, the turn open", Input{K: Quiet}, "- · idle+turn-finished,wake-queue · - · - · - · - · - · - · - · idle+wake-queue"},
	{"interrupted, queue paused", Input{K: Interrupted, PauseQueue: true}, "- · idle+turn-interrupted,pause-queue · idle+gate-closed,turn-interrupted,pause-queue · idle+gate-closed,turn-interrupted,pause-queue · - · - · - · - · - · -"},
	{"interrupted", Input{K: Interrupted}, "- · idle+turn-interrupted,wake-queue · idle+gate-closed,turn-interrupted,wake-queue · idle+gate-closed,turn-interrupted,wake-queue · - · - · - · - · - · -"},
	{"shell", Input{K: Shell, Resumable: true}, "- · stopped+turn-failed,wake-queue · stopped+gate-closed,turn-failed,wake-queue · stopped+gate-closed,turn-failed,wake-queue · stopped+wake-queue · stopped+wake-queue · stopped+wake-queue · - · - · stopped+wake-queue"},
	{"gone", Input{K: Gone, GoneReason: wire.GoneTabClosed}, "gone+pause-queue · gone+turn-failed,pause-queue · gone+gate-closed,turn-failed,pause-queue · gone+gate-closed,turn-failed,pause-queue · gone+pause-queue · gone+pause-queue · gone+pause-queue · gone+pause-queue · - · gone+pause-queue"},
	{"restart", Input{K: Restart}, "unknown · unknown · unknown · unknown · unknown · unknown · unknown · unknown · unknown · unknown"},
}

func cell(st wire.AgentState, in Input) string {
	next, effects := Next(st, in)
	if fmt.Sprint(next) == fmt.Sprint(st) && len(effects) == 0 && !matches(st, in) {
		return "-"
	}
	s := string(next.K)
	if len(effects) > 0 {
		names := make([]string, len(effects))
		for i, e := range effects {
			names[i] = string(e.K)
		}
		s += "+" + strings.Join(names, ",")
	}
	return s
}

// matches says whether a row of the table takes in from st, so a row that keeps the state as it was still shows.
func matches(st wire.AgentState, in Input) bool {
	for _, r := range Rules {
		if r.In == in.K && (r.From == nil || slices.Contains(r.From, st.K)) && (r.Guard == nil || r.Guard(st, in)) {
			return true
		}
	}
	return false
}

func TestEveryCellOfTheTable(t *testing.T) {
	if len(states) != len(wire.AgentStateKinds) {
		t.Fatal("a state kind has no column")
	}
	for i, st := range states {
		if st.K != wire.AgentStateKinds[i] {
			t.Fatalf("column %d is %s", i, st.K)
		}
	}
	covered := map[InputKind]bool{}
	for _, c := range cells {
		covered[c.in.K] = true
		if c.in.At == 0 {
			c.in.At = at
		}
		want := strings.Split(c.row, " · ")
		if len(want) != len(states) {
			t.Fatalf("%s: %d cells", c.name, len(want))
		}
		for i, st := range states {
			if got := cell(st, c.in); got != want[i] {
				t.Errorf("%s from %s: got %s, want %s", c.name, st.K, got, want[i])
			}
		}
	}
	for _, k := range InputKinds {
		if !covered[k] {
			t.Errorf("input %s has no row", k)
		}
	}
}

func TestEveryRuleIsReachable(t *testing.T) {
	for i, r := range Rules {
		if !slices.Contains(InputKinds, r.In) {
			t.Errorf("rule %d takes an unknown input %s", i, r.In)
		}
		for _, k := range r.From {
			if !slices.Contains(wire.AgentStateKinds, k) {
				t.Errorf("rule %d from an unknown state %s", i, k)
			}
		}
	}
}

func TestEveryStateItMakesIsValid(t *testing.T) {
	for _, c := range cells {
		in := c.in
		if in.At == 0 {
			in.At = at
		}
		for _, st := range states {
			next, _ := Next(st, in)
			if err := next.Validate(); err != nil {
				t.Errorf("%s from %s: %v", c.name, st.K, err)
			}
		}
	}
}

func TestUnknownIsNotIdle(t *testing.T) {
	// Only a look at the agent (a quiet screen, a hook, the launch) brings an unchecked agent to idle.
	for _, c := range cells {
		in := c.in
		if in.At == 0 {
			in.At = at
		}
		next, _ := Next(states[9], in)
		if next.K == wire.StateIdle && in.K != Quiet && in.K != LaunchReady && in.K != SessionStart {
			t.Errorf("%s makes unknown idle", c.name)
		}
	}
}

func TestStartingHasATerm(t *testing.T) {
	st, _ := Next(wire.AgentState{K: wire.StateIdle, Since: 1}, Input{K: LaunchRequested, At: 5000})
	if st.K != wire.StateStarting || st.Since != 5000 || st.Step != wire.StepMachine ||
		*st.Wait != (wire.Wait{SlowAt: 5000 + wire.LaunchSlowMs, DeadlineAt: 5000 + wire.LaunchDeadline}) {
		t.Fatalf("%+v %+v", st, st.Wait)
	}
	st, _ = Next(st, Input{K: LaunchStep, Step: wire.StepPrompt, At: 9000})
	if st.Step != wire.StepPrompt || st.Since != 5000 || st.Wait.DeadlineAt != 5000+wire.LaunchDeadline {
		t.Fatalf("a step moved the term: %+v %+v", st, st.Wait)
	}
	if next, eff := Next(st, Input{K: Tick, At: 5000 + wire.LaunchDeadline - 1}); next.K != wire.StateStarting || eff != nil {
		t.Fatalf("failed before its deadline: %+v", next)
	}
	next, eff := Next(st, Input{K: Tick, At: 5000 + wire.LaunchDeadline})
	if next.K != wire.StateLaunchFailed || next.Reason != string(wire.LaunchFailedNoPromptBox) ||
		len(eff) != 1 || eff[0].Pause != wire.PausedLaunchFailed {
		t.Fatalf("%+v %+v", next, eff)
	}
	next, _ = Next(st, Input{K: LaunchFailed, Failure: wire.LaunchFailedLoginRequired, At: 6000})
	if next.Reason != string(wire.LaunchFailedLoginRequired) || next.Since != 6000 {
		t.Fatalf("%+v", next)
	}
}

func TestATurnKeepsItsIDThroughGatesAndCompaction(t *testing.T) {
	st := wire.AgentState{K: wire.StateIdle}
	steps := []Input{
		{K: PromptSubmit, At: 1}, {K: TurnOpened, TurnID: "c:aa#0", At: 2}, {K: PreCompact, At: 3},
		{K: QuestionAsked, GateID: "q:x", At: 4}, {K: GateResolved, At: 5}, {K: PostCompact, At: 6},
	}
	for _, in := range steps {
		st, _ = Next(st, in)
	}
	if st.K != wire.StateWorking || st.TurnID == nil || *st.TurnID != "c:aa#0" || st.Compacting || st.Since != 5 {
		t.Fatalf("%+v", st)
	}
	st, _ = Next(st, Input{K: PreCompact, At: 7})
	if !st.Compacting {
		t.Fatal("pre-compact did not mark compaction")
	}
	st, _ = Next(st, Input{K: Stop, At: 8})
	if st.K != wire.StateReady || st.TurnID != nil || st.Compacting || st.Since != 8 {
		t.Fatalf("%+v", st)
	}
	st, _ = Next(st, Input{K: PromptSubmit, At: 9})
	if st.TurnID != nil {
		t.Fatal("a new turn took the old turn's id")
	}
}

func TestEffectsSayWhy(t *testing.T) {
	q := states[2]
	cases := []struct {
		in   Input
		want Effect
	}{
		{Input{K: Stop}, Effect{K: CloseGate, ID: "q:tu1", Reason: wire.ClosedTurnEnded}},
		{Input{K: PromptSubmit}, Effect{K: CloseGate, ID: "q:tu1", Reason: wire.ClosedAgentMovedOn}},
		{Input{K: Gone, GoneReason: wire.GoneTmuxLost}, Effect{K: CloseGate, ID: "q:tu1", Reason: wire.ClosedAgentGone}},
		{Input{K: Gone}, Effect{K: TurnFailed, ID: "c:aa#0", Turn: wire.TurnFailedAgentExited}},
		{Input{K: Gone}, Effect{K: PauseQueue, Pause: wire.PausedAgentGone}},
		{Input{K: Shell}, Effect{K: TurnFailed, ID: "c:aa#0", Turn: wire.TurnFailedAgentExited}},
		{Input{K: StopFailure, TurnFailure: wire.TurnFailedContextOverflow}, Effect{K: TurnFailed, ID: "c:aa#0", Turn: wire.TurnFailedContextOverflow}},
		{Input{K: StopFailure}, Effect{K: TurnFailed, ID: "c:aa#0", Turn: wire.TurnFailedUnknown}},
		{Input{K: Interrupted, PauseQueue: true}, Effect{K: PauseQueue, Pause: wire.PausedInterrupted}},
		{Input{K: GateResolved}, Effect{K: GateMovedOn, ID: "q:tu1", Reason: wire.ClosedAgentMovedOn}},
	}
	for _, c := range cases {
		c.in.At = at
		_, effects := Next(q, c.in)
		if !slices.Contains(effects, c.want) {
			t.Errorf("%s: %+v has no %+v", c.in.K, effects, c.want)
		}
	}
	_, effects := Next(states[1], Input{K: TurnOpened, TurnID: "c:cc#1", At: at})
	if effects[0] != (Effect{K: TurnStarted, ID: "c:cc#1"}) {
		t.Errorf("%+v", effects)
	}
	next, _ := Next(states[1], Input{K: Shell, Resumable: false, At: at})
	if next.Resumable {
		t.Error("a stopped agent without a session is resumable")
	}
}

func TestNotificationsOtherThanPermissionChangeNothing(t *testing.T) {
	// The actor feeds only notification(permission_prompt) as PermissionPrompt; idle_prompt and the rest are no input.
	// Dismiss is no input either: reading a conversation moves seenAt, never the agent's state.
	for _, k := range InputKinds {
		if strings.Contains(string(k), "notification") || strings.Contains(string(k), "dismiss") {
			t.Errorf("input %s", k)
		}
	}
}

// Claude's SessionStart(compact) ends a compaction whose PostCompact was lost; the turn goes on.
func TestSessionStartEndsACompaction(t *testing.T) {
	id := "c:aa#0"
	st := wire.AgentState{K: wire.StateWorking, TurnID: &id, Compacting: true, Since: 1}
	st, effects := Next(st, Input{K: SessionStart, At: 2})
	if st.K != wire.StateWorking || st.Compacting || st.TurnID == nil || *st.TurnID != id || st.Since != 1 {
		t.Fatalf("%+v", st)
	}
	if len(effects) != 1 || effects[0].K != CompactionFinished {
		t.Fatalf("effects %+v", effects)
	}
	// Not compacting: a SessionStart of a working agent changes nothing, as before.
	if again, effects := Next(st, Input{K: SessionStart, At: 3}); !reflect.DeepEqual(again, st) || len(effects) != 0 {
		t.Fatalf("%+v %+v", again, effects)
	}
}
