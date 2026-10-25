package handoff

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/feed"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Commands of a turn an excerpt names at most, and the lines and characters of a command's output it keeps.
const (
	maxCommands    = 6
	outputLines    = 8
	outputChars    = 600
	omittedMarkRaw = "_… %d turns in between are left out …_"
)

// commandTools are the tools whose steps are commands: Claude's Bash, Codex's shells.
var commandTools = []string{"Bash", "exec", "exec_command", "shell", "bash", "command"}

// Excerpt is the context of a conversation within budget tokens: the person's first request and as many of the latest
// turns as fit, oldest first, with the roles kept and the texts whole — the person's messages, the agent's replies, and
// the key commands of a turn with the end of their output. A turn that does not fit with its commands goes without
// them; one that does not fit at all ends the excerpt there. Reasoning, tool state and attachments stay out. kept is how
// many turns it holds, the first one included.
func Excerpt(turns []feed.WholeTurn, agent wire.Provider, budget int) (text string, kept int) {
	if len(turns) == 0 {
		return "", 0
	}
	first := renderTurn(turns[0], agent, false)
	used := Tokens(first)
	var tail []string
	from := len(turns)
fill:
	for i := len(turns) - 1; i >= 1; i-- {
		full := renderTurn(turns[i], agent, true)
		lean := renderTurn(turns[i], agent, false)
		switch {
		case used+Tokens(full) <= budget:
			tail, used = append(tail, full), used+Tokens(full)
		case used+Tokens(lean) <= budget:
			tail, used = append(tail, lean), used+Tokens(lean)
		default:
			break fill
		}
		from = i
	}
	if len(tail) == len(turns)-1 && len(turns) > 1 {
		// Everything fits: the first turn goes with its commands too, when they fit.
		if full := renderTurn(turns[0], agent, true); used-Tokens(first)+Tokens(full) <= budget {
			first = full
		}
	}
	slices.Reverse(tail)
	parts := []string{summaryLine(len(turns), len(tail)), first}
	if from > 1 {
		parts = append(parts, fmt.Sprintf(omittedMarkRaw, from-1))
	}
	parts = append(parts, tail...)
	text = strings.Join(parts, "\n\n")
	if Tokens(text) > budget {
		// The first request alone is over the budget: it is cut, the only text that ever is.
		text = clipRunes(text, budget*4) + "\n\n_… cut to fit …_"
	}
	return text, 1 + len(tail)
}

// renderTurn is a turn as the excerpt has it; withWork adds the turn's key commands and the files it changed.
func renderTurn(t feed.WholeTurn, agent wire.Provider, withWork bool) string {
	var b strings.Builder
	name := "### " + Agent(agent)
	section := func(head, body string) {
		if body = strings.TrimSpace(body); body == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(head + "\n" + body)
	}
	var commands []wire.Entry
	for i, e := range t.Entries {
		switch e.T {
		case wire.EntryUser:
			head := "### The person"
			if i > 0 {
				head = "### The person (during the turn)"
			}
			section(head, e.Text)
		case wire.EntryPeer:
			for _, p := range e.Peers {
				section("### A message from another agent ("+p.From+")", p.MD)
			}
		case wire.EntrySubagent:
			if i == 0 {
				section("### A subagent returned", e.Description)
			}
		case wire.EntrySay:
			isAnswer := t.Turn.AnswerEntryID != nil && *t.Turn.AnswerEntryID == e.ID
			if withWork || isAnswer {
				section(name, e.MD)
			}
		case wire.EntryTool:
			if slices.Contains(commandTools, e.Tool) {
				commands = append(commands, e)
			}
		}
	}
	if withWork {
		section(name+" · key commands", commandLines(commands))
		section(name+" · files", fileLines(t.Turn.Files))
	}
	switch t.Turn.Status {
	case wire.TurnInterrupted:
		b.WriteString("\n\n_The turn was stopped._")
	case wire.TurnFailed:
		b.WriteString("\n\n_The turn failed._")
	}
	return strings.TrimSpace(b.String())
}

// summaryLine says how much of the conversation the excerpt holds.
func summaryLine(all, tail int) string {
	switch {
	case all == 1:
		return "_The conversation has one turn; it is here._"
	case tail == all-1:
		return fmt.Sprintf("_%d turns in all; every one is here._", all)
	case tail == 0:
		return fmt.Sprintf("_%d turns in all; only the first request is here._", all)
	}
	return fmt.Sprintf("_%d turns in all; the first request and the last %d are here._", all, tail)
}

// commandLines are a turn's key commands: the failed ones and the last ones, maxCommands at most, each with the end of
// its output when it failed or is the last of the turn.
func commandLines(cs []wire.Entry) string {
	if len(cs) == 0 {
		return ""
	}
	pick := map[int]bool{len(cs) - 1: true}
	for i := len(cs) - 1; i >= 0 && len(pick) < maxCommands; i-- {
		if cs[i].OK != nil && !*cs[i].OK {
			pick[i] = true
		}
	}
	for i := len(cs) - 1; i >= 0 && len(pick) < maxCommands; i-- {
		pick[i] = true
	}
	var lines []string
	if skipped := len(cs) - len(pick); skipped > 0 {
		lines = append(lines, fmt.Sprintf("- _%d more commands_", skipped))
	}
	for i, c := range cs {
		if !pick[i] {
			continue
		}
		cmd := strings.TrimSpace(strings.TrimPrefix(firstLine(c.Summary), "$ "))
		state := "running"
		if c.OK != nil && *c.OK {
			state = "ok"
		} else if c.OK != nil {
			state = "failed"
		}
		lines = append(lines, "- `"+strings.ReplaceAll(cmd, "`", "'")+"` → "+state)
		if out := outputTail(c); out != "" && (state == "failed" || i == len(cs)-1) {
			lines = append(lines, "  ```\n  "+strings.ReplaceAll(out, "\n", "\n  ")+"\n  ```")
		}
	}
	return strings.Join(lines, "\n")
}

func outputTail(c wire.Entry) string {
	if c.Output == nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(c.Output.Text, "\n "), "\n")
	if len(lines) > outputLines {
		lines = lines[len(lines)-outputLines:]
	}
	out := strings.ReplaceAll(strings.Join(lines, "\n"), "```", "'''")
	if r := []rune(out); len(r) > outputChars {
		out = "…" + string(r[len(r)-outputChars:])
	}
	return strings.TrimSpace(out)
}

func fileLines(fs []wire.TurnFile) string {
	var out []string
	for _, f := range fs {
		if f.Op != wire.FileRead {
			out = append(out, "- "+f.Path+" ("+string(f.Op)+")")
		}
	}
	return strings.Join(out, "\n")
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
