package prompts

import (
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/screen"
)

// "Send now" for a message waiting in Claude Code's own queue: Claude's chord ctrl+x ctrl+s hands a running Bash or MCP
// call to the background ("it was not interrupted") and delivers every queued message. While the model streams text the
// same chord cuts the answer short, so it is pressed only while the transcript shows a tool that can be handed over.
// Escape is never pressed: it cancels the tool.

// SendNowStep is what to do about the message on one look.
type SendNowStep string

const (
	StepDelivered SendNowStep = "delivered"
	StepPress     SendNowStep = "press"
	StepMenu      SendNowStep = "menu"
	StepConflict  SendNowStep = "conflict"
	StepUnseen    SendNowStep = "unseen"
	StepWriting   SendNowStep = "writing"
	StepTool      SendNowStep = "tool"
)

const (
	sendNowPoll        = 50 * time.Millisecond
	sendNowAcceptLimit = 5000 * time.Millisecond
	sendNowUnseenLimit = 1500 * time.Millisecond
)

// The chord as tmux key names.
var sendNowChord = []string{"C-x", "C-s"}

// isHandOverTool: verified on Claude 2.1.296, a Bash command and an MCP call go to the background.
func isHandOverTool(tool string) bool { return tool == "Bash" || strings.HasPrefix(tool, "mcp__") }

// DecideSendNow, in order: the message already left the queue (steered: it is in the transcript as a prompt) →
// delivered; a menu → menu; text in the input box (the chord would send it too) → conflict; not steered and no queue on
// screen → unseen; no tool running (the model is writing) → writing; a tool that cannot be handed over → tool; else
// press.
func DecideSendNow(frame screen.SendNowFrame, probe Probe, steered bool) SendNowStep {
	switch {
	case steered && probe.Delivered, !steered && !probe.Waiting:
		return StepDelivered
	case frame.Menu:
		return StepMenu
	case frame.Input != "":
		return StepConflict
	case !steered && !frame.Queued:
		return StepUnseen
	case len(probe.Running) == 0:
		return StepWriting
	}
	for _, t := range probe.Running {
		if !isHandOverTool(t) {
			return StepTool
		}
	}
	return StepPress
}

// SendNowResult is how "send now" ended: OK with outcome delivered or sent, or a status with a message for the person;
// Waits says the agent reads the message at its next step by itself.
type SendNowResult struct {
	OK      bool
	Outcome string
	Status  int
	Message string
	Waits   bool
}

func sendNowFailure(status int, message string, waits bool) SendNowResult {
	return SendNowResult{Status: status, Message: message, Waits: waits}
}

// SendNowDriver reads and types into one pane.
type SendNowDriver interface {
	// Capture is the pane with its styles; false when it cannot be read.
	Capture() (string, bool)
	// ReadTranscript is the agent's transcript as JSONL lines in file order; false when the tab has none.
	ReadTranscript() ([]string, bool)
	// SendKeys sends the keys in one go, in order.
	SendKeys(keys ...string) error
}

type sendNowLook struct {
	frame screen.SendNowFrame
	probe Probe
}

func answerFor(step SendNowStep, probe Probe) (SendNowResult, bool) {
	switch step {
	case StepPress:
		return SendNowResult{}, false
	case StepDelivered:
		return SendNowResult{OK: true, Outcome: "delivered"}, true
	case StepMenu:
		return sendNowFailure(409, msgMenu.String(), false), true
	case StepConflict:
		return sendNowFailure(409, msgConflict.String(), false), true
	case StepUnseen:
		return sendNowFailure(409, msgUnseen.String(), false), true
	case StepWriting:
		return sendNowFailure(409, msgWriting.String(), true), true
	}
	tool := ""
	for _, t := range probe.Running {
		if !isHandOverTool(t) {
			tool = t
			break
		}
	}
	return sendNowFailure(409, msgTool(tool), true), true
}

// SendQueuedNow presses the chord for text, the queued message as Claude recorded it. steeredAtMs > 0: the platform typed
// it into the running turn at that moment (ms epoch), and it counts as waiting until the transcript has it as a prompt.
//
// It looks (screen and transcript), waiting up to 1.5 s while the screen does not show a queue the transcript knows,
// looks once more right before the keys, presses the chord once, then polls every 50 ms for up to 5 s until the queue
// leaves the screen (steered: until the transcript has the prompt).
func SendQueuedNow(text string, d SendNowDriver, clock Clock, steeredAtMs int64) SendNowResult {
	steered := steeredAtMs > 0
	look := func() (sendNowLook, *SendNowResult) {
		styled, ok := d.Capture()
		lines, haveLines := d.ReadTranscript()
		if !ok {
			r := sendNowFailure(500, msgNoScreen.String(), false)
			return sendNowLook{}, &r
		}
		probe, known := Probe{}, false
		if haveLines {
			probe, known = ProbeTranscript(lines, text, float64(steeredAtMs))
		}
		if !known {
			r := sendNowFailure(500, msgNoTranscript.String(), false)
			return sendNowLook{}, &r
		}
		return sendNowLook{frame: screen.ObserveSendNow(styled), probe: probe}, nil
	}
	answer := func(l sendNowLook, failed *SendNowResult) (SendNowResult, bool) {
		if failed != nil {
			return *failed, true
		}
		return answerFor(DecideSendNow(l.frame, l.probe, steered), l.probe)
	}

	// The transcript can know of the queue a moment before the screen shows it.
	start := clock.Now()
	seen, failed := look()
	for failed == nil && DecideSendNow(seen.frame, seen.probe, steered) == StepUnseen && clock.Now().Sub(start) < sendNowUnseenLimit {
		clock.Sleep(sendNowPoll)
		seen, failed = look()
	}
	if r, done := answer(seen, failed); done {
		return r
	}
	// Once more right before the keys: the tool may have ended, or the agent taken the queue, in between.
	if r, done := answer(look()); done {
		return r
	}
	if d.SendKeys(sendNowChord...) != nil {
		return sendNowFailure(500, msgSendNowFailed.String(), false)
	}
	deadline := clock.Now().Add(sendNowAcceptLimit)
	for {
		clock.Sleep(sendNowPoll)
		if steered {
			if l, failed := look(); failed == nil {
				if l.frame.Menu {
					return sendNowFailure(409, msgMenu.String(), false)
				}
				if l.probe.Delivered {
					return SendNowResult{OK: true, Outcome: "sent"}
				}
			}
		} else if styled, ok := d.Capture(); ok {
			frame := screen.ObserveSendNow(styled)
			if frame.Menu {
				return sendNowFailure(409, msgMenu.String(), false)
			}
			if !frame.Queued {
				return SendNowResult{OK: true, Outcome: "sent"}
			}
		}
		if !clock.Now().Before(deadline) {
			return sendNowFailure(500, msgNotAccepted.String(), false)
		}
	}
}
