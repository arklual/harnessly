package prompts

import (
	"regexp"
	"strings"
	"time"

	"harnessly/internal/jscompat"
	"harnessly/internal/screen"
)

// Agent work states the package reads (the status machine owns them).
const (
	StateIdle           = "idle"
	StateBusy           = "busy"
	StateNeedsInput     = "needs-input"
	StateReadyForReview = "ready-for-review"
	StateUnknown        = "unknown"
	StateInactive       = "inactive"
)

// Agent CLIs.
const (
	ProviderClaude = "claude"
	ProviderCodex  = "codex"
)

// LongPromptText is the longest text Claude Code takes as typed; a longer typed burst reads as a paste too.
const LongPromptText = 800

// NeedsEditor: text Claude Code would fold into pasted content if it came as a paste — multi-line or long — goes in
// through its external editor instead.
func NeedsEditor(text string) bool {
	return strings.Contains(text, "\n") || jscompat.Len(text) > LongPromptText
}

var (
	modeOpener     = regexp.MustCompile(`^[!#/&]`)
	completionTail = regexp.MustCompile(`(?:^|` + sp + `)[@/]` + nsp + `*$`)
)

// SubmitsAsTypedLine: a line Claude Code takes as typed and submits on an Enter 50 ms behind it — short and single,
// not opening a mode with its first character (! shell, # memory, / command, & background) and not ending in an @ or
// / word, where Claude's completion list is open and would take the Enter.
func SubmitsAsTypedLine(text string) bool {
	line := jscompat.Trim(text)
	return line != "" && !NeedsEditor(text) && !modeOpener.MatchString(line) && !completionTail.MatchString(line)
}

// Decision is whether a message may be typed now.
type Decision string

const (
	DecisionSend    Decision = "send"
	DecisionWait    Decision = "wait"
	DecisionBlocked Decision = "blocked"
)

// DeliveryScreen is what a delivery reads off the screen; Input is meaningful only with InputLine.
type DeliveryScreen struct {
	Busy, Menu bool
	Input      string
	InputLine  bool
}

// Deliverable decides from the screen: nothing readable or no input line, or a working agent → wait; a menu (it would
// take the keys as an answer) or text in the box (it would go along with ours) → blocked; else send.
func Deliverable(s *DeliveryScreen) Decision {
	if s == nil || !s.InputLine || s.Busy {
		return DecisionWait
	}
	if s.Menu || !blank(s.Input) {
		return DecisionBlocked
	}
	return DecisionSend
}

func deliveryScreen(st screen.State) *DeliveryScreen {
	return &DeliveryScreen{Busy: st.Busy, Menu: st.Menu != nil, Input: st.Input, InputLine: st.InputLine}
}

// MayDeliverQueue: only an agent waiting for a prompt takes the queue — not a working one, not one whose launch is
// under way, never a gone one (the text would land in the shell).
func MayDeliverQueue(cliState string, launching bool) bool {
	return !launching && (cliState == StateIdle || cliState == StateReadyForReview)
}

// AgentAction is what a tab with a queued message needs from its agent.
type AgentAction string

const (
	ActionLaunching     AgentAction = "launching"
	ActionDeliver       AgentAction = "deliver"
	ActionResume        AgentAction = "resume"
	ActionUnrecoverable AgentAction = "unrecoverable"
)

// QueueAgentAction: a launch under way or one that gave up is left alone; a running agent takes the message; a gone one
// is resumed with its session, and without a session the message cannot go anywhere.
func QueueAgentAction(launching, launchGaveUp, agentRunning, hasSession bool) AgentAction {
	switch {
	case launching || launchGaveUp:
		return ActionLaunching
	case agentRunning:
		return ActionDeliver
	case hasSession:
		return ActionResume
	}
	return ActionUnrecoverable
}

// Launch phases of an agent typed into its pane, until its prompt box takes input.
const (
	PhaseStarting = "starting"
	PhaseReady    = "ready"
	PhaseFailed   = "failed"
)

const (
	// LaunchNoProcess is how long a launch whose process never showed up is given.
	LaunchNoProcess = 15 * time.Second
	// LaunchTimeout is how long a CLI may take to draw its prompt box (a big resumed transcript is slow).
	LaunchTimeout = 150 * time.Second
)

// LaunchPhase: process and prompt box → ready; a process seen and gone → failed at once; no process after 15 s, or no
// prompt box after 150 s → failed; else starting.
func LaunchPhase(elapsed time.Duration, running, seenRunning, promptReady bool) string {
	switch {
	case running && promptReady:
		return PhaseReady
	case seenRunning && !running:
		return PhaseFailed
	case !running && elapsed >= LaunchNoProcess:
		return PhaseFailed
	case elapsed >= LaunchTimeout:
		return PhaseFailed
	}
	return PhaseStarting
}
