package prompts

import (
	"regexp"
	"time"

	"harnessly/internal/screen"
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
	submitter, submits := d.(Submitter)
	// An earlier try may have left this message's pictures in the box ([Image #1]) before the agent confirmed them, or
	// (Codex) its text: they are ours, so the box counts as empty and nothing is pasted twice.
	ownPictures := view != nil && len(item.Images) > 0 && view.Input != "" && imageTokensOnly.MatchString(view.Input)
	ownText := view != nil && submits && view.Input != "" && screen.CodexStillInBox(plain, item.Text)
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
	if typer, ok := d.(LineTyper); ok && len(item.Images) == 0 && SubmitsAsTypedLine(item.Text) {
		if typer.TypeLine(item.Text) != nil {
			return OutcomeFailed
		}
		return OutcomeSent
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
	return OutcomeSent
}
