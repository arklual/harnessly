// Package status keeps the work state of every tab: what its agent is doing (idle, busy, waiting for an answer, done),
// learnt from the agent's hooks, its transcript, its screen and its process tree, and pushed to every status socket.
package status

import "time"

// Work states of a tab.
const (
	Idle           = "idle"
	Busy           = "busy"
	Inactive       = "inactive"
	ReadyForReview = "ready-for-review"
	NeedsInput     = "needs-input"
	Cancelled      = "cancelled"
	Unknown        = "unknown"
)

// Event names that move the state.
const (
	EventSessionStart = "session-start"
	EventPromptSubmit = "prompt-submit"
	EventNotification = "notification"
	EventStop         = "stop"
	EventStopFailure  = "stop-failure"
	EventInterrupt    = "interrupt"
	EventPreCompact   = "pre-compact"
	EventPostCompact  = "post-compact"
)

// deriveState is the state an event leads to; a cancelled tab keeps its state.
func deriveState(event, fallback string) string {
	if event == "" || fallback == Cancelled {
		return fallback
	}
	switch event {
	case EventSessionStart, EventInterrupt:
		return Idle
	case EventPromptSubmit:
		return Busy
	case EventNotification:
		return NeedsInput
	case EventStop, EventStopFailure:
		return ReadyForReview
	}
	return fallback
}

// transcriptEventMargin: a transcript line this much after the event that opened a menu means the menu was answered.
const transcriptEventMargin = 1000

// inputAnsweredByTranscript: no hook fires once a question or permission menu is answered; the transcript growing past
// the event that opened it is the only witness that the agent works again.
func inputAnsweredByTranscript(state string, lastEntryTs *float64, lastEventAt float64) bool {
	return state == NeedsInput && lastEntryTs != nil && *lastEntryTs > lastEventAt+transcriptEventMargin
}

// Stuck watchdog timings: a busy tab is examined after this long without a hook or a transcript line, at most this
// often. A Claude started without the platform hooks never reports a turn's end, so it is read sooner and more often.
const (
	stuckQuiet           = 90 * time.Second
	stuckRecheck         = 30 * time.Second
	stuckUnhookedQuiet   = 15 * time.Second
	stuckUnhookedRecheck = 10 * time.Second
	stuckBackoffMax      = 5 * time.Minute
	stuckMinCols         = 60 // smaller panes wrap the prompt box and spinner, so the screen can't be read reliably
	stuckMinRows         = 12
)

type stuckScreen struct{ busy, menu, promptReady bool }

type stuckTranscript struct{ idle, stale, hasAnswer bool }

type stuckInput struct {
	state      string // busy or unknown
	quiet      time.Duration
	quietLimit time.Duration
	screen     *stuckScreen
	transcript *stuckTranscript
}

// decideStuck settles a busy or unknown tab whose hooks went missing, only on two witnesses: the screen shows the
// prompt box without a spinner and the transcript says the turn ended. "" keeps the state.
func decideStuck(in stuckInput) string {
	limit := in.quietLimit
	if limit == 0 {
		limit = stuckQuiet
	}
	if in.screen == nil || in.state == Busy && in.quiet < limit {
		return ""
	}
	if in.screen.menu {
		return NeedsInput
	}
	if in.screen.busy || !in.screen.promptReady {
		return ""
	}
	if in.transcript != nil && in.transcript.idle && in.transcript.hasAnswer {
		return ReadyForReview
	}
	if in.transcript == nil || in.transcript.idle {
		return Idle
	}
	return ""
}

var safeShells = map[string]bool{"bash": true, "zsh": true, "fish": true, "sh": true, "dash": true}

// shellForeground: an agent that still runs keeps its own command in the pane's foreground.
func shellForeground(command string) bool { return command == "" || safeShells[command] }

// mayMarkGone: three witnesses must agree before a tab is shown without its agent — the foreground is a shell, the
// poll found no agent, and a fresh look found none either (a look that failed decides nothing). One look used to be
// enough, and a loaded machine showed working agents as gone every few minutes.
func mayMarkGone(foreground string, polledRunning bool, freshRunning *bool) bool {
	return shellForeground(foreground) && !polledRunning && freshRunning != nil && !*freshRunning
}

// healedState is the state an inactive tab whose agent is in fact there gets back.
func healedState(turnRunning bool, lastEvent string, dismissed bool) string {
	if turnRunning {
		return Busy
	}
	if (lastEvent == EventStop || lastEvent == EventStopFailure) && !dismissed {
		return ReadyForReview
	}
	return Idle
}

// Launch watch: a launch whose process never came, or came and went, is noticed after launchNoProcess; a resumed
// long transcript takes a while, and past launchTimeout the agent is treated as not coming up.
const (
	launchNoProcess = 15 * time.Second
	launchTimeout   = 150 * time.Second
)

type launchProbe struct {
	elapsed                        time.Duration
	running, seenRunning, promptOK bool
}

func launchPhase(p launchProbe) string {
	switch {
	case p.running && p.promptOK:
		return "ready"
	case p.seenRunning && !p.running, !p.running && p.elapsed >= launchNoProcess, p.elapsed >= launchTimeout:
		return "failed"
	}
	return "starting"
}

// turnEnd: a turn ended, the moment the session cwd is snapshotted for rewinding.
func turnEnd(prev, next string) bool {
	return (prev == Busy || prev == NeedsInput) && (next == ReadyForReview || next == Idle)
}
