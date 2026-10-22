package prompts

import (
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/screen"
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

// DeliveryScreen is what a delivery reads off the screen; Input is meaningful only with InputLine. Box is the whole
// input box (its wrapped lines too), Pictures the [Image #n] references in it; OnlyPictures: it holds nothing else.
type DeliveryScreen struct {
	Busy, Menu   bool
	Input        string
	InputLine    bool
	Box          string
	Pictures     int
	OnlyPictures bool
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
	v := &DeliveryScreen{Busy: st.Busy, Menu: st.Menu != nil, Input: st.Input, InputLine: st.InputLine}
	// The input line reads empty with its placeholder shown; only typed input has a box worth reading.
	if st.Input != "" {
		v.Box = inputBoxText(st.Plain)
		v.Pictures, v.OnlyPictures = CountImageRefs(v.Box), imageTokensOnly.MatchString(v.Box)
	}
	return v
}

var boxRule = regexp.MustCompile(`^` + sp + `*─{3,}` + sp + `*$`)

// inputBoxText is the text of Claude's input box on a plain screen: its input line (❯ at the line's start) and the lines
// it wraps onto, up to the box's bottom rule, joined by spaces.
func inputBoxText(plain string) string {
	lines := strings.Split(plain, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		rest, ok := strings.CutPrefix(lines[i], "❯")
		if !ok {
			continue
		}
		parts := []string{rest}
		for _, l := range lines[i+1:] {
			if boxRule.MatchString(l) {
				break
			}
			parts = append(parts, l)
		}
		return strings.Join(parts, " ")
	}
	return ""
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
	ActionGaveUp        AgentAction = "gave-up"
	ActionDeliver       AgentAction = "deliver"
	ActionResume        AgentAction = "resume"
	ActionUnrecoverable AgentAction = "unrecoverable"
)

// QueueAgentAction: a launch under way is waited for; one that gave up will not come up by itself, so its messages are
// given up too; a running agent takes the message; a gone one is resumed with its session, and without a session the
// message cannot go anywhere.
func QueueAgentAction(launching, launchGaveUp, agentRunning, hasSession bool) AgentAction {
	switch {
	case launching:
		return ActionLaunching
	case launchGaveUp:
		return ActionGaveUp
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
