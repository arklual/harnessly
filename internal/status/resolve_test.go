package status

import (
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/tmux"
)

// HRN-674: a new Claude writes its transcript only after the hook of the first prompt. The tab looks for it again
// soon after, instead of waiting for the next poll of the tabs (30-60 s) while the answer is already there.
func TestTranscriptOfANewSessionIsLookedForAgain(t *testing.T) {
	e := newEnv(t, claudeTab(Idle), &tmux.PaneInfo{Command: "claude", Path: "/srv/app", PID: 10})
	e.claude.setRunning(10, true)
	e.m.updateFromHook(session, EventPromptSubmit, "")
	time.Sleep(50 * time.Millisecond)
	if got := e.entry().JSONLPath; got != "" {
		t.Fatalf("no transcript on disk yet, got %q", got)
	}
	const transcript = "/home/coder/.claude/projects/-home-coder-workspace-app/s.jsonl"
	e.claude.mu.Lock()
	e.claude.detected = transcript
	e.claude.mu.Unlock()
	start := time.Now()
	waitFor(t, "the transcript", func() bool {
		e.m.mu.Lock()
		defer e.m.mu.Unlock()
		return e.m.tabs["t1"].JSONLPath == transcript
	})
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("found after %v: should be well before the poll", d)
	}
}
