package screen

import "strings"

const (
	queuePlaceholder = "Press up to edit queued messages"
	// The hint sits under the last queued message, above the spinner and its tip: 6 lines above the input on Claude
	// 2.1.288 and 2.1.296.
	hintLinesAboveInput = 10
)

var queueHintRE = re(`^` + sp + `+ctrl\+x ctrl\+s to send now` + sp + `*$`)

// SendNowFrame is what one look at the screen tells "send now".
type SendNowFrame struct {
	// Input is the typed text of the input line; InputLine whether one is on screen.
	Input     string
	InputLine bool
	Menu      bool
	// Queued: Claude shows a waiting queue in its prompt box.
	Queued bool
}

// QueueInPromptBox: Claude's prompt box shows queued messages — its input line holds the "Press up to edit queued
// messages" placeholder, or one of the 10 lines above it is the "ctrl+x ctrl+s to send now" hint. Only the prompt box
// counts: the agent's own output may quote the same words anywhere above it.
func QueueInPromptBox(styled string) bool {
	lines := strings.Split(strings.ReplaceAll(styled, "\r", ""), "\n")
	for i := range lines {
		lines[i] = StripANSI(lines[i])
	}
	input := lastIndex(lines, func(line string) bool {
		_, ok := inputMarker(line, false)
		return ok
	})
	if input < 0 {
		return false
	}
	if strings.Contains(lines[input], queuePlaceholder) {
		return true
	}
	for _, line := range lines[max(0, input-hintLinesAboveInput):input] {
		if queueHintRE.MatchString(line) {
			return true
		}
	}
	return false
}

// ObserveSendNow reads a styled capture for "send now": the input line, an open menu, and the queue.
func ObserveSendNow(styled string) SendNowFrame {
	s := Read(styled)
	return SendNowFrame{Input: s.Input, InputLine: s.InputLine, Menu: s.Menu != nil, Queued: QueueInPromptBox(styled)}
}

// interruptedRE is Claude's mark under a turn the person stopped.
var interruptedRE = re(`Interrupted` + sp + `·` + sp + `What should Claude do instead\?`)

// InterruptedLast: the newest turn on a plain screen ended by the person's stop — Claude's «Interrupted · What should
// Claude do instead?» comes after the last line of the agent's output (● …); the turn's summary, a spinner and the
// prompt lines below it do not count.
func InterruptedLast(plain string) bool {
	lines := strings.Split(plain, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		switch l := strings.TrimSpace(lines[i]); {
		case interruptedRE.MatchString(l):
			return true
		case strings.HasPrefix(l, "● "):
			return false
		}
	}
	return false
}

// turnDoneRE is Claude's summary under a finished turn: "✻ Brewed for 8s · done 6:06 PM", "✻ Worked for 1m 3s".
var turnDoneRE = re(`^[·✢✳✶✻✽*]` + sp + `+` + nsp + `+(?: ` + nsp + `+){0,2} for \d+[hms]`)

// TurnDoneLast: Claude says on a plain screen that its newest turn is over — the turn's summary or the stop mark lies
// between the person's last message (❯ …) and the input box. A turn waiting on the model shows neither, spinner or
// not, so a quiet screen without them is still a turn at work.
func TurnDoneLast(plain string) bool {
	lines := strings.Split(plain, "\n")
	box := len(lines) - 1
	for ; box >= 0 && !strings.HasPrefix(lines[box], "❯"); box-- {
	}
	for i := box - 1; i >= 0; i-- {
		switch l := strings.TrimSpace(lines[i]); {
		case turnDoneRE.MatchString(l) || interruptedRE.MatchString(l):
			return true
		case strings.HasPrefix(lines[i], "❯"):
			return false
		}
	}
	return false
}
