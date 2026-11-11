package main

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// A chat that reads the transcript of the session a hook names is left alone; a new session (after /clear, a resume
// that forked) or a chat without a transcript yet is attached.
func TestFollowedTranscript(t *testing.T) {
	const a, b = "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	claude := wire.Conversation{AgentSessionID: a, TranscriptPath: "/h/.claude/projects/p/" + a + ".jsonl"}
	codex := wire.Conversation{AgentSessionID: a, TranscriptPath: "/h/.codex/sessions/2026/11/10/rollout-2026-11-10T10-00-00-" + a + ".jsonl"}
	for _, tc := range []struct {
		name           string
		rec            wire.Conversation
		session, named string
		want           bool
	}{
		{"the same Claude session", claude, a, "", true},
		{"the same Codex session, by its id", codex, a, "", true},
		{"the same Codex session, by its file", codex, a, codex.TranscriptPath, true},
		{"a new session", claude, b, "", false},
		{"the session known, its transcript not yet", wire.Conversation{AgentSessionID: a}, a, "", false},
		{"the id attached before the transcript of the new session", wire.Conversation{AgentSessionID: b, TranscriptPath: claude.TranscriptPath}, b, "", false},
		{"another file for the same session", codex, a, "/elsewhere/" + a + ".jsonl", false},
	} {
		if got := followed(tc.rec, tc.session, tc.named); got != tc.want {
			t.Errorf("%s: followed %v, want %v", tc.name, got, tc.want)
		}
	}
}
