// Package screen reads what a coding agent's terminal UI (Claude Code, Codex) shows in a captured tmux pane: whether
// it is working, which menu or permission prompt is open, what the input line holds, whether a queued message waits,
// and which keys answer an AskUserQuestion dialog. Every function is pure: the caller captures the pane (plain, or
// styled with SGR attributes where a function says so) and decides what to do with the answer.
//
// The patterns follow the agents' screens as captured in testdata. Whitespace and "any character" mean what they mean
// in the regular expressions the previous runtime used (JavaScript's \s and .), so both read a screen the same way.
package screen

import (
	"regexp"
	"strings"

	"harnessly/internal/jscompat"
)

const (
	// sp is JavaScript's \s, nsp its \S.
	sp     = `[` + spBody + `]`
	spBody = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`
	nsp    = `[^` + spBody + `]`
	// dot is JavaScript's . without the s flag: anything but a line terminator.
	dot = `[^\n\r\x{2028}\x{2029}]`
)

// re compiles a pattern; patterns spell JavaScript's \s, \S and . with sp, nsp and dot.
func re(pattern string) *regexp.Regexp { return regexp.MustCompile(pattern) }

// ansiRE matches CSI sequences, OSC strings (ended by BEL or ST) and two-byte escapes.
var ansiRE = re(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\x5c-_]`)

// StripANSI removes terminal escape sequences from a styled capture.
func StripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func isSpace(r rune) bool { return jscompat.IsSpace(r) }

func trim(s string) string { return jscompat.Trim(s) }

func trimStart(s string) string { return strings.TrimLeftFunc(s, isSpace) }

func blank(s string) bool { return trim(s) == "" }

// leadingSpaces counts the whitespace a line starts with.
func leadingSpaces(line string) int {
	n := 0
	for _, r := range line {
		if !isSpace(r) {
			break
		}
		n++
	}
	return n
}

// indentedText: the line starts with whitespace followed by something else.
func indentedText(line string) bool {
	rest := trimStart(line)
	return rest != "" && len(rest) < len(line)
}

// startsWithSpace: the first character is whitespace.
func startsWithSpace(line string) bool {
	for _, r := range line {
		return isSpace(r)
	}
	return false
}

// trimEndLines splits a screen into lines without carriage returns and drops the blank lines at its bottom.
func trimEndLines(screen string) []string {
	lines := strings.Split(strings.ReplaceAll(screen, "\r", ""), "\n")
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// tail is the last count lines.
func tail(lines []string, count int) []string {
	if len(lines) > count {
		return lines[len(lines)-count:]
	}
	return lines
}

// trimmedNonEmpty is every line trimmed, blank ones left out.
func trimmedNonEmpty(screen string) []string {
	var out []string
	for _, line := range strings.Split(screen, "\n") {
		if t := trim(line); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// collapse folds every whitespace run into one space and trims the ends.
func collapse(s string) string { return trim(spaceRunRE.ReplaceAllString(s, " ")) }

var spaceRunRE = re(sp + `+`)

// lineTerminators turns the other line terminators into \n, so a multi-line pattern starts a line where JavaScript would.
var lineTerminators = strings.NewReplacer("\r", "\n", " ", "\n", " ", "\n")

func lastIndex(lines []string, ok func(string) bool) int {
	for i := len(lines) - 1; i >= 0; i-- {
		if ok(lines[i]) {
			return i
		}
	}
	return -1
}
