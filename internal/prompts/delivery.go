package prompts

import (
	"regexp"
	"time"

	"github.com/ly-solutions/harnessly/internal/screen"
)

// Delivery outcomes of one message.
type Outcome string

const (
	OutcomeSent    Outcome = "sent"
	OutcomeWait    Outcome = "wait"
	OutcomeBlocked Outcome = "blocked"
	OutcomeFailed  Outcome = "failed"
)

// DeliveryDriver types into one agent's pane.
type DeliveryDriver interface {
	// Capture is the pane with its styles; false when it cannot be read.
	Capture() (string, bool)
	// PasteImages pastes picture paths and says whether the agent showed a reference for each.
	PasteImages(paths []string) bool
	// PasteText puts text into the prompt box without submitting it.
	PasteText(text string) error
	Enter() error
	Sleep(d time.Duration)
}

// QuestionCloser is a driver that can close the agent's question dialog by «Chat about this»: a message typed while
// the agent asks a question is the person's answer in words, not something to hold behind the dialog.
type QuestionCloser interface {
	ChatAboutQuestion() bool
}

// LineTyper is a driver that can type a short line with its Enter right behind it, skipping a paste's settle.
type LineTyper interface {
	TypeLine(text string) error
}

// Submitter is a driver that submits text itself and watches it leave the box (Codex). typed: the text is already in
// the box from an earlier try, only the submit is missing.
type Submitter interface {
	Submit(text string, typed bool) bool
}

// pasteSettle: Claude Code reads a paste as one block; an Enter right on its heels can land before the block is in.
const pasteSettle = 250 * time.Millisecond

var imageTokensOnly = regexp.MustCompile(`^` + sp + `*(?:\[Image #\d+\]` + sp + `*)+$`)

// Deliver types one queued message: pictures first (each must show up), then the text and its Enter. The screen decides
// whether it may happen now; steer ignores a working agent (its turn takes the text) but not a menu or foreign text.
// A message whose id went in within the duplicate window counts as sent and is not typed again. target names the tab
// in the dispatch log.
func Deliver(item Item, d DeliveryDriver, reg *Registry, steer bool, target string) Outcome {
	var view *DeliveryScreen
	plain := ""
	if styled, ok := d.Capture(); ok {
		st := screen.Read(styled)
		view, plain = deliveryScreen(st), st.Plain
	}
	// The agent asks a question (its dialog, not a permission prompt) and the person wrote instead of picking: the
	// dialog is closed by «Chat about this» and the message goes in as the reply.
	if view != nil && view.Menu && !blank(item.Text) && screen.IsQuestionOnScreen(plain) {
		if closer, ok := d.(QuestionCloser); ok && closer.ChatAboutQuestion() {
			view, plain = nil, ""
			if styled, ok := d.Capture(); ok {
				st := screen.Read(styled)
				view, plain = deliveryScreen(st), st.Plain
			}
		}
	}
	submitter, submits := d.(Submitter)
	// An earlier try may have left this message's pictures in the box ([Image #1]) before the agent confirmed them, or
	// (Codex) its text: they are ours, so the box counts as empty and nothing is pasted twice.
	ownPictures := view != nil && len(item.Images) > 0 && view.Input != "" && imageTokensOnly.MatchString(view.Input)
	// Claude: a message an earlier try typed whose Enter was lost sits on the input line; it is ours too, and only its
	// Enter is pressed again — it is never typed a second time.
	ownText := view != nil && view.Input != "" && (submits && screen.CodexStillInBox(plain, item.Text) ||
		!submits && !blank(item.Text) && screen.ClaudeStillInBox(view.Input, item.Text))
	if view != nil {
		if ownPictures || ownText {
			view.Input = ""
		}
		if steer {
			view.Busy = false
		}
	}
	if dec := Deliverable(view); dec != DecisionSend {
		return Outcome(dec)
	}
	route := RouteQueue
	if steer {
		route = RouteSteer
	}
	if !reg.Claim(item.ID, route, target, item.Text) {
		return OutcomeSent
	}
	if len(item.Images) > 0 && !ownPictures && !ownText {
		if !d.PasteImages(item.Images) {
			reg.Release(item.ID)
			return OutcomeFailed
		}
	}
	if submits {
		if !submitter.Submit(item.Text, ownText) {
			// Nothing reached the agent: a retry finds the text in the box as its own and only submits it.
			reg.Release(item.ID)
			return OutcomeFailed
		}
		return OutcomeSent
	}
	if ownText {
		if d.Enter() != nil {
			reg.Release(item.ID)
			return OutcomeFailed
		}
		return confirmLeftBox(item, d, reg)
	}
	if typer, ok := d.(LineTyper); ok && len(item.Images) == 0 && SubmitsAsTypedLine(item.Text) {
		if typer.TypeLine(item.Text) != nil {
			return OutcomeFailed
		}
		return confirmLeftBox(item, d, reg)
	}
	if !blank(item.Text) {
		text := item.Text
		if len(item.Images) > 0 {
			text = " " + text
		}
		if d.PasteText(text) != nil {
			return OutcomeFailed
		}
		d.Sleep(pasteSettle)
	}
	if d.Enter() != nil {
		return OutcomeFailed
	}
	return confirmLeftBox(item, d, reg)
}

const (
	confirmPoll      = 150 * time.Millisecond
	confirmRetry     = 700 * time.Millisecond
	confirmMaxEnters = 3
	confirmLimit     = 4000 * time.Millisecond
)

// confirmLeftBox: a message counts as sent only once it has left Claude Code's input line. The line is read every
// 150 ms for up to 4 s; while the message is still on it, Enter is pressed again 700 ms after the last one, three
// Enters in all. Still there after that: not sent — the duplicate claim is let go, so the next try finds the text as
// its own and presses only its Enter. A screen that cannot be read is not held against the send.
func confirmLeftBox(item Item, d DeliveryDriver, reg *Registry) Outcome {
	if blank(item.Text) {
		return OutcomeSent
	}
	enters, sinceEnter, read := 1, time.Duration(0), false
	for waited := time.Duration(0); waited < confirmLimit; waited += confirmPoll {
		d.Sleep(confirmPoll)
		sinceEnter += confirmPoll
		styled, ok := d.Capture()
		if !ok {
			continue
		}
		read = true
		if !screen.ClaudeStillInBox(screen.Read(styled).Input, item.Text) {
			return OutcomeSent
		}
		if sinceEnter >= confirmRetry && enters < confirmMaxEnters {
			if d.Enter() != nil {
				break
			}
			enters++
			sinceEnter = 0
		}
	}
	if !read {
		return OutcomeSent
	}
	reg.Release(item.ID)
	return OutcomeFailed
}
