package typist

import (
	"time"

	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// SetFast types «/fast on|off» and waits for «Fast mode ON» or «Fast mode OFF». A model or an account without the
// mode answers «Fast mode is not available …»: ErrCommandRefused.
func SetFast(p Pane, c Clock, on bool) error {
	line := "/fast off"
	if on {
		line = "/fast on"
	}
	return slash(p, c, line, answers{
		applied: func(plain string) int { return screen.CountFast(plain, on) },
		refused: screen.CountFastRefused,
	})
}

const (
	// modeCycleMax is how many presses of shift+tab may go round Claude's modes: default, accept edits, plan, auto,
	// bypass, and once more for a cycle that skipped none.
	modeCycleMax = 6
	// modeStepLimit is how long one press has to show on the footer.
	modeStepLimit = 1500 * time.Millisecond
)

// SetPermissionMode turns Claude's permission mode (screen.ClaudeMode*) with shift+tab, one press at a time, each
// read back off the footer under the box, until the footer says the mode asked for. A mode the cycle does not have (a
// Claude started without --allow-dangerously-skip-permissions has no bypass in it) is gone round to the mode it started
// in and ends in ErrCommandRefused, the agent left as it was; a press the footer does not show ends in
// ErrCommandNotApplied.
func SetPermissionMode(p Pane, c Clock, mode string) error {
	s := Look(p, wire.ProviderClaude)
	if !s.Readable || !s.InputLine || s.Busy || s.Menu != NoMenu || s.Panel {
		return ErrNotAtPrompt
	}
	start, ok := screen.ClaudePermissionMode(s.Plain)
	if !ok {
		return ErrNotAtPrompt
	}
	for cur, presses := start, 0; cur != mode; presses++ {
		if presses > 0 && cur == start {
			return ErrCommandRefused
		}
		if presses == modeCycleMax {
			return ErrCommandNotApplied
		}
		if err := p.Keys("BTab"); err != nil {
			return err
		}
		next, moved := cur, false
		for waited := time.Duration(0); waited < modeStepLimit && !moved; waited += commandPoll {
			c.Sleep(commandPoll)
			plain, ok := p.Capture()
			if !ok {
				continue
			}
			if m, seen := screen.ClaudePermissionMode(plain); seen && m != cur {
				next, moved = m, true
			}
		}
		if !moved {
			return ErrCommandNotApplied
		}
		cur = next
	}
	return nil
}

// PermissionModeNow is the mode Claude's footer says now, ok false when it cannot be read.
func PermissionModeNow(p Pane) (string, bool) {
	plain, ok := p.Capture()
	if !ok {
		return "", false
	}
	return screen.ClaudePermissionMode(plain)
}
