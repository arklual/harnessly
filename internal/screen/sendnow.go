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
