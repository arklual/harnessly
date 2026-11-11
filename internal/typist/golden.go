package typist

import (
	"embed"
	"fmt"
	"strings"

	"github.com/ly-solutions/harnessly/internal/cliversion"
	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The reference screens the runtime carries with it: real captures of the agent's CLI, one or more per checked
// version, each with what the reader must say of it. The machine's self-test reads them with the reader of the
// release it runs, so a release whose reader no longer knows the screens it was checked against fails before it
// serves a chat. The versions they are of are the rows of cliversion's table.
//
//go:embed testdata/tui/claude-2.1.310-turn-done.ansi.txt
//go:embed testdata/tui/claude-2.1.316-plan.ansi.txt
//go:embed testdata/tui/claude-2.1.322-idle-placeholder.ansi.txt
//go:embed testdata/tui/claude-2.1.322-input-draft.ansi.txt
//go:embed testdata/tui/claude-2.1.322-question.ansi.txt
//go:embed testdata/tui/claude-2.1.322-turn-done.ansi.txt
var goldenFS embed.FS

// Golden is one reference screen and what the reader must see on it ("" from Want: it does).
type Golden struct {
	Provider wire.Provider
	Version  string
	Name     string
	Want     func(Screen) string
}

func (g Golden) file() string {
	return "testdata/tui/" + string(g.Provider) + "-" + g.Version + "-" + g.Name + ".ansi.txt"
}

func wantReady(s Screen) string {
	if !s.PromptReady() {
		return fmt.Sprintf("the idle input line is not seen (readable %v, input line %v, busy %v, menu %q)", s.Readable, s.InputLine, s.Busy, s.Menu)
	}
	return ""
}

func wantEmptyBox(s Screen) string {
	if why := wantReady(s); why != "" {
		return why
	}
	if s.Input != "" {
		return fmt.Sprintf("the empty box reads as %q", s.Input)
	}
	return ""
}

func wantInput(text string) func(Screen) string {
	return func(s Screen) string {
		if why := wantReady(s); why != "" {
			return why
		}
		if s.Input != text {
			return fmt.Sprintf("the box reads as %q, not %q", s.Input, text)
		}
		return ""
	}
}

func wantMenu(kind MenuKind, options ...string) func(Screen) string {
	return func(s Screen) string {
		if !s.Readable || s.Menu != kind {
			return fmt.Sprintf("menu %q, not %q", s.Menu, kind)
		}
		for i, want := range options {
			if i >= len(s.Options) || !strings.Contains(screen.StripNumberPrefix(s.Options[i]), want) {
				return fmt.Sprintf("option %d is not %q: %s", i+1, want, strings.Join(s.Options, " | "))
			}
		}
		return ""
	}
}

// Goldens are the reference screens of the self-test.
func Goldens() []Golden {
	c := wire.ProviderClaude
	return []Golden{
		{c, "2.1.310", "turn-done", wantReady},
		{c, "2.1.316", "plan", wantMenu(MenuPlan)},
		{c, "2.1.322", "idle-placeholder", wantEmptyBox},
		{c, "2.1.322", "input-draft", wantInput("a draft the person left in the box")},
		{c, "2.1.322", "question", wantMenu(MenuQuestion, "Alpha", "Bravo")},
		{c, "2.1.322", "turn-done", wantEmptyBox},
	}
}

// CheckGoldens reads every reference screen and returns what the reader got wrong, one line per screen; nil when it
// reads them all.
func CheckGoldens() []string { return checkGoldens(Read) }

func checkGoldens(read func(string, wire.Provider) Screen) []string {
	var bad []string
	for _, g := range Goldens() {
		raw, err := goldenFS.ReadFile(g.file())
		if err != nil {
			bad = append(bad, g.Version+" "+g.Name+": "+err.Error())
			continue
		}
		if why := g.Want(read(string(raw), g.Provider)); why != "" {
			bad = append(bad, g.Version+" "+g.Name+": "+why)
		}
	}
	return bad
}

// TypeChecked is Type for a CLI whose version the screen reading was checked against, and its cautious form for one
// it was not (cliversion.Cautious). On an unchecked version the reader may take one screen for another, so no key is
// pressed on the strength of a guess:
//
//   - a screen read as the person's left-open picker or as a question dialog is not closed (no Esc, no «Chat about
//     this»): the message waits as blocked by a menu, and the person answers the card or the terminal;
//   - a box that reads empty after the Enter is not taken as proof that Claude queued the text: the chord that hands
//     the running tool to the background is not pressed. Whether the agent took the message is said by its hook and
//     its transcript, as for every message.
//
// Everything else — what is typed, when it waits, how it is confirmed — is Type's.
func TypeChecked(m Message, provider wire.Provider, p Pane, c Clock) Result {
	if !cliversion.Cautious(string(provider)) {
		return Type(m, provider, p, c)
	}
	if !m.ClearOnly {
		view := Look(p, provider)
		if plan := Decide(m, view, provider); plan.Dismiss != screen.NoAside || plan.ChatAbout {
			return Result{Outcome: BlockedMenu, Before: view}
		}
	}
	m.HandOver = false
	return Type(m, provider, p, c)
}
