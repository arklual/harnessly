package agents

import (
	"strings"

	"harnessly/internal/jscompat"
	"harnessly/internal/proc"
)

// Match is how a process is recognised as an agent's.
type Match int

const (
	// MatchSubstring: the agent's name anywhere in the command line, as the previous runtime did. `vim CLAUDE.md` does
	// not match (case), but `less ~/.claude/x` or a shell the agent started under ~/.claude does.
	MatchSubstring Match = iota
	// MatchArgv: the agent's name in the program's file name, or, for an interpreter (node, python…), in the script's
	// path. Covers the CLIs, their versioned binaries, an npm install's cli.js, a launcher script and `exec -a claude`;
	// a file argument of another program (an editor, a pager, a shell's script) does not count.
	MatchArgv
)

var interpreters = map[string]bool{"node": true, "nodejs": true, "python": true, "python3": true, "bun": true, "deno": true}

// ArgvNames tells whether word is in the program's file name or the interpreted script's path.
func ArgvNames(argv []string, word string) bool {
	if len(argv) == 0 {
		return false
	}
	prog := jscompat.Basename(argv[0], "")
	if strings.Contains(prog, word) {
		return true
	}
	if !interpreters[prog] && !strings.HasPrefix(prog, "python3.") {
		return false
	}
	for _, a := range argv[1:] {
		if strings.HasPrefix(a, "-") {
			continue
		}
		return strings.Contains(a, word)
	}
	return false
}

// Is tells whether the process pid is word's (an agent's name) under this rule; a process that cannot be read is not.
func (m Match) Is(p proc.Procs, pid int, word string) bool {
	if m == MatchArgv {
		argv, ok := p.ArgvOf(pid)
		return ok && ArgvNames(argv, word)
	}
	args, ok := p.ArgsOf(pid)
	return ok && strings.Contains(args, word)
}

// IsArgs applies the rule to a command line already read (Argv split from it for MatchArgv).
func (m Match) IsArgs(args string, word string) bool {
	if m == MatchArgv {
		return ArgvNames(strings.Fields(args), word)
	}
	return strings.Contains(args, word)
}
