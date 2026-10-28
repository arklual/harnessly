// Package agentstate is the automaton of a conversation's agent: where it is (wire.AgentState) and what one input —
// a hook, a transcript entry, a look at the screen, the process under the pane, the launch, a command — makes of it.
//
// Next is a pure function over the Rules table: the same state and input give the same state and effects, nothing
// is read or written. The conversation's actor feeds it and carries the effects out (events, the queue, the gates).
// unknown is "not checked yet": nothing here treats it as idle.
package agentstate

import (
	"slices"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// InputKind names what happened.
type InputKind string

const (
	// The launch (internal/launch, prompts.LaunchPhase).
	LaunchRequested InputKind = "launch-requested"
	LaunchStep      InputKind = "launch-step"
	LaunchReady     InputKind = "launch-ready"
	LaunchFailed    InputKind = "launch-failed"
	// Tick is the clock: a starting agent past its deadline has failed to launch.
	Tick InputKind = "tick"

	// Hooks (agents/claude/hook.go, agents/codex/hook.go).
	SessionStart     InputKind = "session-start"
	PromptSubmit     InputKind = "prompt-submit"
	PermissionPrompt InputKind = "permission-prompt" // notification(permission_prompt), Codex PermissionRequest
	PreCompact       InputKind = "pre-compact"
	PostCompact      InputKind = "post-compact"
	Stop             InputKind = "stop"
	StopFailure      InputKind = "stop-failure"

	// The transcript and the screen.
	TurnOpened    InputKind = "turn-opened"    // a user entry that opens a turn
	QuestionAsked InputKind = "question-asked" // ask-user-question without a result, or the question dialog
	PlanProposed  InputKind = "plan-proposed"  // ExitPlanMode, or the plan menu
	GateResolved  InputKind = "gate-resolved"  // the gate's tool result, an entry after it, or its menu gone
	ScreenBusy    InputKind = "screen-busy"    // a probe of an unchecked agent sees its spinner
	Quiet         InputKind = "quiet"          // long silence and an input line without a spinner (decideStuck)

	// Commands and processes.
	Interrupted InputKind = "interrupted" // our Escape went in
	Shell       InputKind = "shell"       // a shell, not the agent, is in the pane's foreground
	Gone        InputKind = "gone"        // no tmux session, or the tab closed
	Restart     InputKind = "restart"     // the runtime started
)

// InputKinds is every InputKind, in the order of the table.
var InputKinds = []InputKind{LaunchRequested, LaunchStep, LaunchReady, LaunchFailed, Tick, SessionStart, PromptSubmit,
	PermissionPrompt, PreCompact, PostCompact, Stop, StopFailure, TurnOpened, QuestionAsked, PlanProposed, GateResolved,
	ScreenBusy, Quiet, Interrupted, Shell, Gone, Restart}

// Input is one thing that happened, at At; the other fields belong to some kinds only.
type Input struct {
	K  InputKind
	At wire.Ms

	Step        wire.LaunchStep      // launch-step
	Failure     wire.LaunchFailure   // launch-failed
	TurnID      string               // turn-opened
	GateID      string               // question-asked, permission-prompt, plan-proposed
	TurnFailure wire.TurnFailureCode // stop-failure
	TurnDetail  string               // stop-failure: what the agent said about it, "" nothing
	TurnEnded   bool                 // quiet: the transcript ends with the turn's end
	Resumable   bool                 // shell: the agent has a session to resume
	GoneReason  wire.GoneReason      // gone
	PauseQueue  bool                 // interrupted
}

// EffectKind names what the actor must do after a transition.
type EffectKind string

const (
	WakeQueue          EffectKind = "wake-queue"
	PauseQueue         EffectKind = "pause-queue"
	PromptSubmitted    EffectKind = "prompt-submitted" // the message in flight is accepted (in-input → accepted)
	TurnStarted        EffectKind = "turn-started"
	TurnFinished       EffectKind = "turn-finished"
	TurnFailed         EffectKind = "turn-failed"
	TurnInterrupted    EffectKind = "turn-interrupted"
	AskQuestion        EffectKind = "question-asked"
	RequestPermission  EffectKind = "permission-requested"
	ProposePlan        EffectKind = "plan-proposed"
	GateMovedOn        EffectKind = "gate-moved-on" // answered when we answered it, else closed{agent-moved-on}
	CloseGate          EffectKind = "gate-closed"
	CompactionStarted  EffectKind = "compaction-started"
	CompactionFinished EffectKind = "compaction-finished"
	NotifyPerson       EffectKind = "notify"
)

// Effect is one thing to do; ID is the gate or the turn it is about, Code and Reason its why.
type Effect struct {
	K      EffectKind
	ID     string
	Turn   wire.TurnFailureCode // turn-failed
	Detail string               // turn-failed: what the agent said about it
	Pause  wire.PauseReason     // pause-queue
	Reason wire.GateCloseReason // gate-closed
}

// Rule is one row of the table: an input taken in one of From (nil: any state) where Guard holds goes To ("" stays
// in the same kind, its fields kept) and asks for Effects. The first row that matches wins; no row, no change.
type Rule struct {
	In      InputKind
	From    []wire.AgentStateKind
	Guard   func(st wire.AgentState, in Input) bool
	To      wire.AgentStateKind
	Effects []EffectKind
}

var (
	live        = []wire.AgentStateKind{wire.StateWorking, wire.StateWaitingQuestion, wire.StateWaitingPermission}
	waiting     = []wire.AgentStateKind{wire.StateWaitingQuestion, wire.StateWaitingPermission}
	notWorking  = without(wire.StateWorking)
	notGone     = without(wire.StateGone)
	runningIdle = []wire.AgentStateKind{wire.StateIdle, wire.StateReady, wire.StateUnknown}
)

func without(k wire.AgentStateKind) []wire.AgentStateKind {
	return slices.DeleteFunc(slices.Clone(wire.AgentStateKinds), func(x wire.AgentStateKind) bool { return x == k })
}

func kinds(ks ...wire.AgentStateKind) []wire.AgentStateKind { return ks }

func pastDeadline(st wire.AgentState, in Input) bool {
	return st.Wait != nil && in.At >= st.Wait.DeadlineAt
}
func turnEnded(_ wire.AgentState, in Input) bool  { return in.TurnEnded }
func turnOpen(_ wire.AgentState, in Input) bool   { return !in.TurnEnded }
func pauses(_ wire.AgentState, in Input) bool     { return in.PauseQueue }
func newTurn(st wire.AgentState, in Input) bool   { return st.TurnID == nil || *st.TurnID != in.TurnID }
func compacting(st wire.AgentState, _ Input) bool { return st.Compacting }

// Rules is the table of §4.2 of the spec. Where it was silent the rows say so in their comment.
var Rules = []Rule{
	{In: LaunchRequested, From: notWorking, To: wire.StateStarting},
	{In: LaunchStep, From: kinds(wire.StateStarting), To: wire.StateStarting},
	// A launch-failed agent that comes up after all (a slow resume) is up: the deadline only stopped the wait.
	{In: LaunchReady, From: kinds(wire.StateStarting, wire.StateUnknown, wire.StateStopped, wire.StateLaunchFailed),
		To: wire.StateIdle, Effects: []EffectKind{WakeQueue}},
	{In: LaunchFailed, From: kinds(wire.StateStarting), To: wire.StateLaunchFailed, Effects: []EffectKind{PauseQueue}},
	{In: Tick, From: kinds(wire.StateStarting), Guard: pastDeadline, To: wire.StateLaunchFailed,
		Effects: []EffectKind{PauseQueue}},

	{In: SessionStart, From: kinds(wire.StateStarting, wire.StateUnknown, wire.StateStopped, wire.StateLaunchFailed),
		To: wire.StateIdle, Effects: []EffectKind{WakeQueue}},
	// /clear starts a new session of a waiting agent: still waiting, nothing to wake. Claude also sends it after a
	// compaction mid-turn (source "compact"): a working agent stays working, and one still marked compacting is done
	// with it (its PostCompact hook lost or never sent).
	{In: SessionStart, From: kinds(wire.StateIdle, wire.StateReady), To: wire.StateIdle},
	{In: SessionStart, From: kinds(wire.StateWorking), Guard: compacting, To: wire.StateWorking,
		Effects: []EffectKind{CompactionFinished}},

	// A steered message is submitted into the running turn: the agent stays working, the message is accepted.
	{In: PromptSubmit, From: kinds(wire.StateWorking), Effects: []EffectKind{PromptSubmitted}},
	{In: PromptSubmit, From: waiting, To: wire.StateWorking, Effects: []EffectKind{PromptSubmitted, CloseGate}},
	// A prompt submitted while the launch is still counted proves the agent is up.
	{In: PromptSubmit, From: append(kinds(wire.StateStarting), runningIdle...), To: wire.StateWorking,
		Effects: []EffectKind{PromptSubmitted}},

	{In: TurnOpened, From: kinds(wire.StateWorking), Guard: newTurn, To: wire.StateWorking,
		Effects: []EffectKind{TurnStarted}},
	// A turn opened without its prompt-submit (hooks missing or lost) still means the agent works.
	{In: TurnOpened, From: runningIdle, To: wire.StateWorking, Effects: []EffectKind{TurnStarted}},

	{In: QuestionAsked, From: kinds(wire.StateWorking, wire.StateUnknown), To: wire.StateWaitingQuestion,
		Effects: []EffectKind{AskQuestion}},
	// A background agent asks after the chat's turn ended: the chat waits for the answer all the same.
	{In: PermissionPrompt, From: kinds(wire.StateWorking, wire.StateUnknown, wire.StateIdle, wire.StateReady),
		To: wire.StateWaitingPermission, Effects: []EffectKind{RequestPermission}},
	// The spec has plan from working; a probe after a restart finds the plan menu of an unknown agent too.
	{In: PlanProposed, From: kinds(wire.StateWorking, wire.StateUnknown), To: wire.StateWaitingPermission,
		Effects: []EffectKind{ProposePlan}},
	{In: GateResolved, From: waiting, To: wire.StateWorking, Effects: []EffectKind{GateMovedOn}},

	{In: PreCompact, From: kinds(wire.StateWorking), To: wire.StateWorking, Effects: []EffectKind{CompactionStarted}},
	// A /compact typed into a waiting agent compacts without a prompt-submit: the agent works meanwhile.
	{In: PreCompact, From: runningIdle, To: wire.StateWorking, Effects: []EffectKind{CompactionStarted}},
	// PostCompact is also what the actor feeds when the transcript shows the compaction's line (convo.feedUpdateOf).
	{In: PostCompact, From: kinds(wire.StateWorking), To: wire.StateWorking, Effects: []EffectKind{CompactionFinished}},

	{In: Stop, From: kinds(wire.StateWorking), To: wire.StateReady,
		Effects: []EffectKind{TurnFinished, WakeQueue, NotifyPerson}},
	{In: Stop, From: waiting, To: wire.StateReady, Effects: []EffectKind{CloseGate, TurnFinished, WakeQueue, NotifyPerson}},
	{In: StopFailure, From: live, To: wire.StateReady, Effects: []EffectKind{CloseGate, TurnFailed, WakeQueue, NotifyPerson}},

	{In: Interrupted, From: live, Guard: pauses, To: wire.StateIdle, Effects: []EffectKind{CloseGate, TurnInterrupted, PauseQueue}},
	{In: Interrupted, From: live, To: wire.StateIdle, Effects: []EffectKind{CloseGate, TurnInterrupted, WakeQueue}},

	// The spinner on the screen is the agent at work, whatever an earlier look made of the pane.
	{In: ScreenBusy, From: kinds(wire.StateUnknown, wire.StateStopped), To: wire.StateWorking},
	{In: Quiet, From: kinds(wire.StateWorking), Guard: turnEnded, To: wire.StateReady, Effects: []EffectKind{TurnFinished, WakeQueue}},
	// Silence is not the person's stop: the turn ends as done, and opens again if the agent writes on (convo.resumeQuiet).
	{In: Quiet, From: kinds(wire.StateWorking), Guard: turnOpen, To: wire.StateIdle, Effects: []EffectKind{TurnFinished, WakeQueue}},
	{In: Quiet, From: kinds(wire.StateUnknown), Guard: turnEnded, To: wire.StateReady, Effects: []EffectKind{WakeQueue}},
	{In: Quiet, From: kinds(wire.StateUnknown), Guard: turnOpen, To: wire.StateIdle, Effects: []EffectKind{WakeQueue}},

	// A shell in the foreground of a launching pane is the launch itself (the CLI is typed into it): only a launch
	// failure ends a start.
	{In: Shell, From: live, To: wire.StateStopped, Effects: []EffectKind{CloseGate, TurnFailed, WakeQueue}},
	{In: Shell, From: kinds(wire.StateIdle, wire.StateReady, wire.StateUnknown, wire.StateStopped), To: wire.StateStopped,
		Effects: []EffectKind{WakeQueue}},
	{In: Gone, From: live, To: wire.StateGone, Effects: []EffectKind{CloseGate, TurnFailed, PauseQueue}},
	{In: Gone, From: notGone, To: wire.StateGone, Effects: []EffectKind{PauseQueue}},

	{In: Restart, To: wire.StateUnknown},
}

// Next is the state after in and what to do about it. An input no row takes changes nothing.
func Next(st wire.AgentState, in Input) (wire.AgentState, []Effect) {
	for _, r := range Rules {
		if r.In != in.K || r.From != nil && !slices.Contains(r.From, st.K) || r.Guard != nil && !r.Guard(st, in) {
			continue
		}
		next := st
		if r.To != "" {
			next = enter(st, r.To, in)
		}
		effects := make([]Effect, 0, len(r.Effects))
		for _, k := range r.Effects {
			// A gate is closed only where one is open: a working agent that exits has none.
			if (k == CloseGate || k == GateMovedOn) && gateOf(st) == "" {
				continue
			}
			effects = append(effects, effect(k, st, in))
		}
		return next, effects
	}
	return st, nil
}

// enter builds the state of kind k from the one before and the input: since moves only when the kind changes.
func enter(st wire.AgentState, k wire.AgentStateKind, in Input) wire.AgentState {
	next := wire.AgentState{K: k, Since: st.Since}
	if st.K != k {
		next.Since = in.At
	}
	switch k {
	case wire.StateStarting:
		next.Step, next.Wait = wire.StepMachine, &wire.Wait{SlowAt: in.At + wire.LaunchSlowMs, DeadlineAt: in.At + wire.LaunchDeadline}
		if st.K == wire.StateStarting {
			next.Wait = st.Wait
			next.Step = st.Step
		}
		if in.K == LaunchStep {
			next.Step = in.Step
		}
	case wire.StateWorking:
		next.TurnID, next.Compacting = st.TurnID, st.Compacting && st.K == wire.StateWorking
		if st.K != wire.StateWorking && !waitingKind(st.K) {
			next.TurnID = nil
		}
		switch in.K {
		case TurnOpened:
			id := in.TurnID
			next.TurnID = &id
		case PreCompact:
			next.Compacting = true
		case PostCompact, SessionStart:
			next.Compacting = false
		}
	case wire.StateWaitingQuestion:
		next.QuestionID, next.TurnID = in.GateID, st.TurnID
	case wire.StateWaitingPermission:
		next.PermissionID, next.TurnID = in.GateID, st.TurnID
	case wire.StateStopped:
		next.Resumable = in.Resumable
	case wire.StateLaunchFailed:
		next.Reason = string(in.Failure)
		if in.K == Tick {
			next.Reason = string(wire.LaunchFailedNoPromptBox)
		}
	case wire.StateGone:
		next.Reason = string(in.GoneReason)
	}
	return next
}

func waitingKind(k wire.AgentStateKind) bool {
	return k == wire.StateWaitingQuestion || k == wire.StateWaitingPermission
}

// effect fills one effect of a transition from st by in.
func effect(k EffectKind, st wire.AgentState, in Input) Effect {
	e := Effect{K: k}
	switch k {
	case PauseQueue:
		switch in.K {
		case Interrupted:
			e.Pause = wire.PausedInterrupted
		case Gone:
			e.Pause = wire.PausedAgentGone
		default:
			e.Pause = wire.PausedLaunchFailed
		}
	case TurnStarted:
		e.ID = in.TurnID
	case TurnFinished, TurnInterrupted:
		e.ID = turnOf(st)
	case TurnFailed:
		e.ID = turnOf(st)
		e.Turn, e.Detail = in.TurnFailure, in.TurnDetail
		if in.K != StopFailure {
			e.Turn = wire.TurnFailedAgentExited
		}
		if e.Turn == "" {
			e.Turn = wire.TurnFailedUnknown
		}
	case AskQuestion, RequestPermission, ProposePlan:
		e.ID = in.GateID
	case GateMovedOn, CloseGate:
		e.ID = gateOf(st)
		switch in.K {
		case Stop, StopFailure:
			e.Reason = wire.ClosedTurnEnded
		case Shell, Gone:
			e.Reason = wire.ClosedAgentGone
		default:
			e.Reason = wire.ClosedAgentMovedOn
		}
	}
	return e
}

// turnOf is the turn a state runs, "" none (an unknown agent's turn is not known to the automaton).
func turnOf(st wire.AgentState) string {
	if st.TurnID == nil {
		return ""
	}
	return *st.TurnID
}

// gateOf is the open gate of a waiting state, "" in the others.
func gateOf(st wire.AgentState) string {
	switch st.K {
	case wire.StateWaitingQuestion:
		return st.QuestionID
	case wire.StateWaitingPermission:
		return st.PermissionID
	}
	return ""
}
