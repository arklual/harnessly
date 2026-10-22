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

// imageTokensOnly: a box of picture references alone, one wrapped midway included (see imageRef).
var imageTokensOnly = regexp.MustCompile(`^` + sp + `*(?:\[Image` + sp + `+#\d+\]` + sp + `*)+$`)

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
	_, submits := d.(Submitter)
	// An earlier try may have left this message's pictures in the box ([Image #1]) before the agent confirmed them, all
	// or the first of them, or (Codex) its text: they are ours, so the box counts as empty and only what is missing is
	// pasted. More pictures than the message has, or text after them that is not the message, are someone else's.
	ownPictures := view != nil && len(item.Images) > 0 && view.OnlyPictures && view.Pictures <= len(item.Images)
	// Claude: a message an earlier try typed whose Enter was lost sits in the box, after its pictures when it has them
	// (they may fill the first line); it is ours too, and only its Enter is pressed again — it is never typed twice.
	ownText := view != nil && view.Input != "" && (submits && screen.CodexStillInBox(plain, item.Text) ||
		!submits && !blank(item.Text) && screen.ClaudeStillInBox(view.Box, item.Text))
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
	// A try that failed let nothing reach the agent for sure: its claim is let go on every such branch, so the retry
	// types the message (or finds it in the box as its own) instead of taking the claim for a send.
	landed := 0
	if ownPictures {
		landed = view.Pictures
	}
	out := typeClaimed(item, d, landed, ownText)
	if out == OutcomeFailed {
		reg.Release(item.ID)
	}
	return out
}

// typeClaimed types what the box does not have yet: landed pictures of the message are there already.
func typeClaimed(item Item, d DeliveryDriver, landed int, ownText bool) Outcome {
	if len(item.Images) > landed && !ownText {
		if !d.PasteImages(item.Images[landed:]) {
			return OutcomeFailed
		}
	}
	if submitter, ok := d.(Submitter); ok {
		// Nothing reached the agent on a failure: a retry finds the text in the box as its own and only submits it.
		if !submitter.Submit(item.Text, ownText) {
			return OutcomeFailed
		}
		return OutcomeSent
	}
	if ownText {
		if d.Enter() != nil {
			return OutcomeFailed
		}
		return confirmLeftBox(item, d)
	}
	if typer, ok := d.(LineTyper); ok && len(item.Images) == 0 && SubmitsAsTypedLine(item.Text) {
		if typer.TypeLine(item.Text) != nil {
			return OutcomeFailed
		}
		return confirmLeftBox(item, d)
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
	return confirmLeftBox(item, d)
}

const (
	confirmPoll      = 150 * time.Millisecond
	confirmRetry     = 700 * time.Millisecond
	confirmMaxEnters = 3
	confirmLimit     = 4000 * time.Millisecond
)

// confirmLeftBox: a message counts as sent only once it has left Claude Code's input line. The line is read every
// 150 ms for up to 4 s; while the message is still on it, Enter is pressed again 700 ms after the last one, three
// Enters in all. Still there after that: not sent — Deliver lets the duplicate claim go, so the next try finds the text
// as its own and presses only its Enter. A screen that cannot be read is not held against the send.
func confirmLeftBox(item Item, d DeliveryDriver) Outcome {
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
		if !screen.ClaudeStillInBox(deliveryScreen(screen.Read(styled)).Box, item.Text) {
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
	return OutcomeFailed
}
