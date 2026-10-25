package screen

import (
	"strings"
)

// codexLimitWords is how Codex's notice of the plan's usage limit starts.
const codexLimitWords = "hit your usage limit"

// codexLimitSpan is how many lines above its composer Codex's last notice may wrap over.
const codexLimitSpan = 8

// CodexLimitLine is Codex's notice of the usage limit when it is the last thing above the composer — the turn that
// just ended stopped on it: «■ You've hit your usage limit. … try again at 6:05 PM.», its wrapped lines joined. "" when
// the last thing above the composer is anything else (an older notice the agent has written past).
func CodexLimitLine(plain string) string {
	lines := strings.Split(lineTerminators.Replace(plain), "\n")
	box := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if composerLine(lines[i]) {
			box = i
			break
		}
	}
	if box < 0 {
		return ""
	}
	end := box
	for end > 0 && blank(lines[end-1]) {
		end--
	}
	for start := end - 1; start >= 0 && start >= end-codexLimitSpan; start-- {
		head := trimStart(lines[start])
		if blank(lines[start]) {
			return ""
		}
		if !strings.HasPrefix(head, "■") {
			continue
		}
		var parts []string
		for _, l := range lines[start:end] {
			parts = append(parts, strings.TrimSpace(l))
		}
		text := strings.TrimSpace(strings.TrimPrefix(strings.Join(parts, " "), "■"))
		if !strings.Contains(strings.ToLower(text), codexLimitWords) {
			return ""
		}
		return text
	}
	return ""
}
