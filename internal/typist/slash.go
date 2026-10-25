package typist

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A slash command that changes the running agent's setting — Claude's /model and /effort — typed into its box under
// the pane lock the caller holds, and counted done once the screen says it was applied.

var (
	// ErrNotAtPrompt: the agent works, asks, or shows no input line, so a command typed now would go to the wrong place.
	ErrNotAtPrompt = errors.New("typist: the agent is not at its prompt")
	// ErrInputOccupied: the box holds text that is not the command's.
	ErrInputOccupied = errors.New("typist: the input box holds other text")
	// ErrCommandDeclined: the agent asked to confirm the change and the confirmation was answered "keep".
	ErrCommandDeclined = errors.New("typist: the agent kept the previous value")
	// ErrCommandRefused: the agent answered it will not do it (a model that does not take ultracode).
	ErrCommandRefused = errors.New("typist: the agent refused the command")
	// ErrCommandNotApplied: the command was typed and the screen never said it took effect.
	ErrCommandNotApplied = errors.New("typist: the agent did not confirm the command")
)

const (
	commandPoll = 75 * time.Millisecond
	// commandLimit is how long a command has to show its effect; commandAfterConfirm more once its confirmation was
	// answered (the agent re-reads the history before it says «Set model to»).
	commandLimit        = 2500 * time.Millisecond
	commandAfterConfirm = 1500 * time.Millisecond
)

// SetModel types «/model id» and waits for «Set model to»; Claude's «Switch model?» question is the person's consent
// already (the page asked), so its focused "Yes" is pressed.
func SetModel(p Pane, c Clock, id string) error {
	return slash(p, c, "/model "+id, answers{applied: screen.CountModelSet})
}

// SetEffort types «/effort level» and waits for «Set effort level to level». Since Claude Code 2.1.310 a level leaves
// ultracode as it is.
func SetEffort(p Pane, c Clock, effort wire.Effort) error {
	// The line names the level: an older «Set effort level to high» above (or scrolled off the pane meanwhile) must not
	// be mistaken for, or hide, this one.
	said := regexp.MustCompile(`Set effort level to ` + regexp.QuoteMeta(string(effort)) + `\b`)
	return slash(p, c, "/effort "+string(effort), answers{applied: func(plain string) int { return len(said.FindAllStringIndex(plain, -1)) }})
}

// SetUltracode types «/effort ultracode on|off» and waits for «Ultracode on (…)» or «Ultracode off.». A model or a
// setup that cannot run it answers «Ultracode isn't available on …»: ErrCommandRefused.
func SetUltracode(p Pane, c Clock, on bool) error {
	line := "/effort ultracode off"
	if on {
		line = "/effort ultracode on"
	}
	return slash(p, c, line, answers{
		applied: func(plain string) int { return screen.CountUltracode(plain, on) },
		refused: screen.CountUltracodeRefused,
	})
}

// answers are the lines that end a command: applied counts the lines saying it took, refused (nil for none) the
// lines saying the agent will not do it.
type answers struct {
	applied func(plain string) int
	refused func(plain string) int
}

func (a answers) refusals(plain string) int {
	if a.refused == nil {
		return 0
	}
	return a.refused(plain)
}

// slash types the command and leaves the box clean whatever came of it: a command that did not go through (its Enter
// lost, a question closed) is cleared out of the box, never left for the next message to find.
func slash(p Pane, c Clock, line string, a answers) error {
	err := slashOnce(p, c, line, a)
	if err != nil && !errors.Is(err, ErrNotAtPrompt) && !errors.Is(err, ErrInputOccupied) {
		ClearOwn(p, c, wire.ProviderClaude, line, false)
	}
	return err
}

// occupied is ErrInputOccupied with what the box was read to hold, for the journal to say what stood in the way.
func occupied(s Screen) error {
	text := s.Box
	if text == "" {
		text = s.Input
	}
	if rs := []rune(text); len(rs) > 80 {
		text = string(rs[:80]) + "…"
	}
	return fmt.Errorf("%w: %q", ErrInputOccupied, text)
}

func slashOnce(p Pane, c Clock, line string, a answers) error {
	before := Look(p, wire.ProviderClaude)
	switch {
	case !before.Readable || !before.InputLine || before.Busy || before.Menu != NoMenu || before.Panel:
		return ErrNotAtPrompt
	case before.Input != "" && HarnessLeftover(before, wire.ProviderClaude):
		// An earlier command of ours left in the box: it goes first.
		if !ClearOwn(p, c, wire.ProviderClaude, before.Box, false) {
			return occupied(before)
		}
		before = Look(p, wire.ProviderClaude)
	case before.Input != "":
		return occupied(before)
	}
	was, kept, refused := a.applied(before.Plain), screen.CountKept(before.Plain), a.refusals(before.Plain)
	_, echoed := screen.LastEcho(before.Plain, line)
	if err := p.TypeLine(line); err != nil {
		return err
	}
	var answered, declined bool
	for waited, limit := time.Duration(0), commandLimit; waited < limit; waited += commandPoll {
		c.Sleep(commandPoll)
		styled, ok := p.CaptureStyled()
		if !ok {
			continue
		}
		st := screen.Read(styled)
		if screen.IsLaunchConfirm(st.Plain) {
			if answered {
				continue // the question is still drawn; the key went in a moment ago
			}
			// The focused answer is read from the lines under the question, not from the whole menu: a short pane cuts
			// the question at the bottom and «2. No, go back» is not drawn (the menu reader then sees no menu at all,
			// and this used to press Escape: «Kept model as …»).
			seen, yes := screen.ConfirmFocus(st.Plain)
			if !seen {
				continue // drawn up to its title so far
			}
			if !yes {
				// Not a question to answer for the person: close it, and the command is not applied.
				_ = p.Keys("Escape")
				return ErrCommandNotApplied
			}
			if err := p.Keys("Enter"); err != nil {
				return err
			}
			answered, limit = true, limit+commandAfterConfirm
			continue
		}
		// The answer is read on a screen back at its empty prompt only. In a short pane Claude redraws older history
		// while its question closes, and a «Kept model as …» or «Set model to …» of before shows for a few frames.
		if !st.InputLine || st.Input != "" || st.Panel || st.Menu != nil {
			continue
		}
		// The answer under this command's own echo, when the screen before had no echo of the same line: in a short
		// pane an older answer scrolls off as this one comes, and the counts of answer lines stay the same.
		if answer, ok := screen.LastEcho(st.Plain, line); ok && !echoed {
			switch {
			case a.applied(answer) > 0:
				return nil
			case a.refusals(answer) > 0:
				return ErrCommandRefused
			case screen.CountKept(answer) > 0:
				declined = true
			}
			continue
		}
		if a.applied(st.Plain) > was {
			return nil
		}
		if a.refusals(st.Plain) > refused {
			return ErrCommandRefused
		}
		// «Kept model as …» is the agent's own word that the change did not take, whoever answered its question; it
		// is believed once the time is up without the change said, so a line of before drawn again does not end it.
		if screen.CountKept(st.Plain) > kept {
			declined = true
		}
	}
	if declined {
		return ErrCommandDeclined
	}
	return ErrCommandNotApplied
}
