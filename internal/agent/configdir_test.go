package agent

import (
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/proc"
)

// the person's shell gives claude and codex CLAUDE_CONFIG_DIR and CODEX_HOME, the agent's service does not.
// The agent lists and serves the transcripts there too, and keeps the folders after the CLIs exit.
func TestRootsFollowTheCLIsEnvironment(t *testing.T) {
	home, other := t.TempDir(), t.TempDir()
	claudeDir, codexDir := filepath.Join(other, "claude"), filepath.Join(other, "codex")
	procs := proc.Procs{
		All: func() []int { return []int{10, 11, 12} },
		Argv: func(pid int) ([]string, bool) {
			return map[int][]string{10: {"/usr/local/bin/claude"}, 11: {"node", "/opt/codex/bin/codex"}, 12: {"bash"}}[pid], true
		},
		Environ: func(pid int) ([]string, bool) {
			return map[int][]string{
				10: {"CLAUDE_CONFIG_DIR=" + claudeDir},
				11: {"CODEX_HOME=" + codexDir},
				12: {"CLAUDE_CONFIG_DIR=/not/a/claude"},
			}[pid], true
		},
	}
	noEnv := func(string, string) string { return "" }
	running := true
	set := newRootSet(home, func() ([]string, []string) {
		if !running {
			return cliDirs(home, noEnv, proc.Procs{})
		}
		return cliDirs(home, noEnv, procs)
	})
	session := filepath.Join(claudeDir, "projects", "-home-coder-workspace-app", "s.jsonl")
	rollout := filepath.Join(codexDir, "sessions", "2026", "11", "04", "r.jsonl")
	writeFile(t, session, "{}\n")
	writeFile(t, rollout, "{}\n")
	writeFile(t, filepath.Join(home, ".claude", "projects", "p", "h.jsonl"), "{}\n")

	running = false
	set.Refresh()
	running = true
	files, err := ListTranscripts(set.Refresh())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.Path] = f.Provider
	}
	if got[session] != "claude" || got[rollout] != "codex" || got[filepath.Join(home, ".claude", "projects", "p", "h.jsonl")] != "claude" || len(got) != 3 {
		t.Fatalf("listed %v", got)
	}
	r := set.Get()
	if !r.IsTranscriptPath(session) || !r.IsTranscriptPath(rollout) || r.IsTranscriptPath("/not/a/claude/projects/p/s.jsonl") {
		t.Fatalf("roots %+v", r)
	}
	running = false
	if r := set.Refresh(); !r.IsTranscriptPath(session) {
		t.Fatalf("a folder seen once stays a root: %+v", r)
	}
}
