package devtools

import (
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// Which lines of a dev server's output are problems: Next, Vite, Angular CLI, webpack, esbuild, tsc and plain Node, by
// the words and marks they print. Stack frames and code frames after a problem belong to it.

type LogLine struct {
	Text  string `json:"text"`
	Level string `json:"level"`
}

var (
	ansiRe     = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)|\x1b[@-Z\\\\-_]")
	errorMarks = []*regexp.Regexp{
		regexp.MustCompile(`\bERROR\b`),
		regexp.MustCompile(`\bERR!`),
		regexp.MustCompile(`\b[A-Z][A-Za-z]*(Error|Exception)\b(:|` + jscompat.SpaceClass + `*\[)`),
		regexp.MustCompile(`(?i)\berror( TS\d+)?` + jscompat.SpaceClass + `*:`),
		regexp.MustCompile(`(?i)^` + jscompat.SpaceClass + `*(error|fatal)\b`),
		regexp.MustCompile(`[✘✖⨯]`),
		regexp.MustCompile(`(?i)Failed to compile|Compilation failed|Build failed|Internal server error|Module not found|Cannot find module`),
		regexp.MustCompile(`\bUnhandled\b|\bUncaught\b|\bFATAL\b|\bELIFECYCLE\b|\bEADDRINUSE\b`),
	}
	warnMarks = []*regexp.Regexp{
		regexp.MustCompile(`\bWARN(ING)?\b`),
		regexp.MustCompile(`(?i)\bwarn(ing)?` + jscompat.SpaceClass + `*:`),
		regexp.MustCompile(`(?i)^` + jscompat.SpaceClass + `*warn(ing)?\b`),
		regexp.MustCompile(`[⚠▲]`),
		regexp.MustCompile(`(?i)\bdeprecat(ed|ion)\b`),
	}
	successMarks = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bcompiled successfully\b`),
		regexp.MustCompile(`^` + jscompat.SpaceClass + `*[✓✔]` + jscompat.SpaceClass),
		regexp.MustCompile(`(?i)Application bundle generation complete`),
		regexp.MustCompile(`(?i)\bFound 0 errors\b`),
		regexp.MustCompile(`(?i)\bready in \d`),
	}
	continuation  = regexp.MustCompile(`^` + jscompat.SpaceClass + `+(at` + jscompat.SpaceClass + `|>?` + jscompat.SpaceClass + `*\d*` + jscompat.SpaceClass + `*\||\^)|^` + jscompat.SpaceClass + `+[^\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}].*:\d+:\d+\)?$`)
	trailingSpace = regexp.MustCompile(jscompat.SpaceClass + `+$`)
)

const maxFolded = 12

func anyMatch(marks []*regexp.Regexp, line string) bool {
	for _, m := range marks {
		if m.MatchString(line) {
			return true
		}
	}
	return false
}

func classifyLine(line string) string {
	switch {
	case anyMatch(errorMarks, line):
		return "error"
	case anyMatch(warnMarks, line):
		return "warn"
	case anyMatch(successMarks, line):
		return "success"
	}
	return "info"
}

func levelOf(kind string) string {
	if kind == "error" || kind == "warn" {
		return kind
	}
	return "info"
}

func splitLines(text string) []string {
	text = ansiRe.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = trailingSpace.ReplaceAllString(l, "")
	}
	return lines
}

// LastLines is the last limit lines as they are, each with its level.
func LastLines(text string, limit int) []LogLine {
	lines := splitLines(text)
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	out := make([]LogLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, LogLine{Text: l, Level: levelOf(classifyLine(l))})
	}
	return out
}

// ProblemLines is the last limit problems, each with its stack or code frame, and the successes between them,
// consecutive ones as one.
func ProblemLines(text string, limit int) []LogLine {
	found := []LogLine{}
	open := -1
	folded := 0
	for _, line := range splitLines(text) {
		if jscompat.Trim(line) == "" {
			open = -1
			continue
		}
		kind := classifyLine(line)
		if open >= 0 && kind != "success" && continuation.MatchString(line) {
			if folded < maxFolded {
				found[open].Text += "\n" + line
				folded++
			}
			continue
		}
		open = -1
		if kind == "info" {
			continue
		}
		entry := LogLine{Text: jscompat.Trim(line), Level: levelOf(kind)}
		if kind == "success" {
			if n := len(found); n > 0 && found[n-1].Level == "info" {
				found[n-1] = entry
			} else {
				found = append(found, entry)
			}
			continue
		}
		found = append(found, entry)
		open, folded = len(found)-1, 0
	}
	if len(found) > limit {
		found = found[len(found)-limit:]
	}
	if len(found) > 0 && found[0].Level == "info" {
		found = found[1:]
	}
	return found
}
