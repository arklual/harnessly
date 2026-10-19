package prompts

import (
	"time"

	"harnessly/internal/screen"
)

// A message for Codex is pasted and submitted, then its prompt box is read: an Enter that lands while Codex starts its
// turn or its MCP servers is dropped, and the text would stay in the box until someone noticed. Mid-turn Codex takes
// the Enter as a steer of its running turn.

const (
	codexSubmitDelay = 250 * time.Millisecond
	codexPoll        = 150 * time.Millisecond
	codexRetryAfter  = 700 * time.Millisecond
	codexMaxEnters   = 3
	codexSettleLimit = 4000 * time.Millisecond
)

// CodexDriver types into one Codex pane.
type CodexDriver interface {
	Paste(text string) error
	Enter() error
	// Capture is the plain screen; false when it cannot be read.
	Capture() (string, bool)
}

// SubmitCodex pastes text (unless typed: it is in the box already), presses Enter after 250 ms, then reads the box every
// 150 ms for up to 4 s: gone means sent; still there, Enter again once 700 ms passed since the last one, at most three
// Enters in all.
func SubmitCodex(text string, typed bool, d CodexDriver, clock Clock) bool {
	if !typed {
		if d.Paste(text) != nil {
			return false
		}
		clock.Sleep(codexSubmitDelay)
	}
	if d.Enter() != nil {
		return false
	}
	enters, lastEnter := 1, clock.Now()
	deadline := clock.Now().Add(codexSettleLimit)
	for clock.Now().Before(deadline) {
		clock.Sleep(codexPoll)
		s, ok := d.Capture()
		if !ok {
			continue
		}
		if !screen.CodexStillInBox(s, text) {
			return true
		}
		if clock.Now().Sub(lastEnter) >= codexRetryAfter && enters < codexMaxEnters {
			if d.Enter() != nil {
				return false
			}
			enters++
			lastEnter = clock.Now()
		}
	}
	return false
}
