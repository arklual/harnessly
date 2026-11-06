// Package renderkit holds the pinned versions of the render kit: the headless Chrome, ffmpeg and fonts the built-in
// design engine draws pictures and video with (docs/render-kit.md).
package renderkit

import (
	_ "embed"
	"strings"
)

// File is versions.env.
//
//go:embed versions.env
var File []byte

// Pins are the KEY=VALUE lines of versions.env.
func Pins() map[string]string { return Parse(string(File)) }

// Parse reads KEY=VALUE lines; comments and blank lines are skipped.
func Parse(s string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// Major is the leading number of a version or pattern, past an epoch: "7:5.1.*" is 5, "156.*" is 156. -1 when none.
func Major(v string) int {
	if _, rest, ok := strings.Cut(v, ":"); ok {
		v = rest
	}
	n, seen := 0, false
	for _, r := range v {
		if r < '0' || r > '9' {
			break
		}
		n, seen = n*10+int(r-'0'), true
	}
	if !seen {
		return -1
	}
	return n
}
