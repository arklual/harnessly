// Package chatguard is what a machine's runtime does about the chat's typing while the machine's local policy keeps the
// terminal switched off for the hub.
//
// The machine agent's route filter refuses the routes of a terminal, but the chat page's own routes (a message, a
// queued message, "send now", an answer) type into the tmux pane of a conversation, and a pane is a terminal when the
// agent in it has gone: the text would be read by a shell and run. Claude Code's bash mode (a message that starts with
// "!") runs the rest of the line as a command without asking either. The filter cannot see a pane, so the agent tells
// the runtime when the terminal is off (POST <agent API>/policy, see cmd/runtime/hub_routes.go), and the runtime refuses
// at the source: text goes only into a pane where an agent runs, and never as a bash-mode line.
package chatguard

import (
	"errors"
	"strings"
	"sync/atomic"
	"unicode"

	"github.com/ly-solutions/harnessly/internal/tmux"
	"github.com/ly-solutions/harnessly/internal/typist"
)

// ErrTerminalOff is what a refused write says.
var ErrTerminalOff = errors.New("the terminal is switched off on this machine: text goes only into a running agent, and not as a shell command")

// Guard says whether the terminal is switched off. The zero value, and a nil *Guard, is on: a runtime without a hub
// asks nobody. A runtime a hub carries starts with it off (New(true)) until the machine agent has said what the policy
// is, so that the minutes after a restart are not an open door.
type Guard struct{ off atomic.Bool }

// New makes a Guard that starts off or on.
func New(off bool) *Guard {
	g := &Guard{}
	g.off.Store(off)
	return g
}

// Off says whether the terminal is switched off.
func (g *Guard) Off() bool { return g != nil && g.off.Load() }

// Set records what the machine agent said.
func (g *Guard) Set(off bool) {
	if g != nil {
		g.off.Store(off)
	}
}

// Func is Off as a function, for the places that take one (nil for a nil Guard).
func (g *Guard) Func() func() bool {
	if g == nil {
		return nil
	}
	return g.Off
}

// BashMode says that an agent reads the text as a shell command: Claude Code (and Codex) enter their bash mode on a
// "!" at the start of the input. Whitespace and invisible format characters before it are skipped, since a paste or an
// editor may trim them, and it costs nothing to refuse a line that merely looks like one.
func BashMode(text string) bool {
	rest := strings.TrimLeftFunc(text, func(r rune) bool { return unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) })
	return strings.HasPrefix(rest, "!")
}

// AgentInForeground says that a program other than a bare shell runs in the foreground of the pane; false when tmux did
// not say (an empty command), as the answer of a loaded machine proves nothing.
func AgentInForeground(command string) bool { return command != "" && !tmux.SafeShells[command] }

// pane refuses to write into a pane whose foreground is a shell (or unknown) while the terminal is off.
type pane struct {
	typist.Pane
	g          *Guard
	foreground func() string
}

func (p pane) allowed() error {
	if !p.g.Off() || AgentInForeground(p.foreground()) {
		return nil
	}
	return ErrTerminalOff
}

func (p pane) Paste(text string) error {
	if err := p.allowed(); err != nil {
		return err
	}
	return p.Pane.Paste(text)
}

func (p pane) TypeLine(text string) error {
	if err := p.allowed(); err != nil {
		return err
	}
	return p.Pane.TypeLine(text)
}

func (p pane) Keys(keys ...string) error {
	if err := p.allowed(); err != nil {
		return err
	}
	return p.Pane.Keys(keys...)
}

// Guarded is the pane as the conversations type into it: every write is checked against the pane's foreground program
// (foreground reads it) while the terminal is off, and refused with ErrTerminalOff when that is a shell. With the
// terminal on it is the pane itself.
func Guarded(p typist.Pane, g *Guard, foreground func() string) typist.Pane {
	if p == nil || g == nil {
		return p
	}
	return pane{Pane: p, g: g, foreground: foreground}
}
