// Package roots names the folders the agents' transcripts may be in: the ones under the home, the ones the runtime's
// CLAUDE_CONFIG_DIR and CODEX_HOME name, and the ones the detectors saw a running Claude or Codex use.
package roots

import (
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agents/claude"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
)

// Transcripts are Claude's projects folders and Codex's sessions folders.
func Transcripts(home string) (claudeProjects, codexSessions []string) {
	return claude.NewDetector(home).ProjectsDirs(), codex.NewDetector(home).SessionsRoots()
}

// All is Transcripts in one list.
func All(home string) []string {
	c, x := Transcripts(home)
	return append(c, x...)
}

// Allowed: p is a .jsonl under one of the folders, the path resolved first.
func Allowed(home, p string) bool {
	abs, err := filepath.Abs(p)
	if err != nil || !strings.HasSuffix(abs, ".jsonl") {
		return false
	}
	for _, r := range All(home) {
		if strings.HasPrefix(abs, r+"/") {
			return true
		}
	}
	return false
}
