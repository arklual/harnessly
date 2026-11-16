package transcript

import (
	"strings"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// resultHeadMax is how much of a tool result's text a result entry carries: what an event of the feed shows
// (wire.ToolOutputMax). The feed keeps the entries it read, so the whole text of every result would stay in memory.
const resultHeadMax = 4096

// Tail lines: how many of an output's last non-empty lines a folded step shows, and how long each may be.
const (
	tailLines   = 3
	tailLineMax = 240
)

// OutputShape is how many lines an output has and its last few non-empty lines, each cut to tailLineMax runes. The
// tail is a copy: a slice of the text would keep the whole read alive.
func OutputShape(text string) (lines int, tail string) {
	all := strings.Split(strings.TrimRight(text, "\n"), "\n")
	var out []string
	for i := len(all) - 1; i >= 0 && len(out) < tailLines; i-- {
		ln := strings.TrimRight(all[i], " \t\r")
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if r := []rune(ln); len(r) > tailLineMax {
			ln = string(r[:tailLineMax]) + "…"
		}
		out = append([]string{ln}, out...)
	}
	return len(all), strings.Clone(strings.Join(out, "\n"))
}

// setResultText puts a result's text on its entry the way the feed shows it: its head (a copy, at most
// resultHeadMax bytes, cut at a line when it can), how many lines and bytes the whole has, and its last lines.
func setResultText(e *jsvalue.Object, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	lines, tail := OutputShape(text)
	head := text
	if len(head) > resultHeadMax {
		cut := resultHeadMax
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		if nl := strings.LastIndexByte(text[:cut], '\n'); nl > resultHeadMax/2 {
			cut = nl
		}
		head = text[:cut]
	}
	e.Set("text", strings.Clone(head))
	e.Set("textLines", float64(lines))
	e.Set("textBytes", float64(len(text)))
	e.Set("textTail", tail)
}
