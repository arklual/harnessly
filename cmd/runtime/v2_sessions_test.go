package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/sessions"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func TestV2SessionsOfBothAgentsLeaveOutTheHeldOnes(t *testing.T) {
	home := t.TempDir()
	put := func(path, body string, at time.Time) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	claudeLine := func(cwd, text string) string {
		return `{"type":"user","cwd":"` + cwd + `","timestamp":"2026-10-20T10:00:00Z","message":{"content":"` + text + `"}}` + "\n"
	}
	now := time.Now()
	claudeDir := filepath.Join(home, ".claude", "projects", sessions.ProjectName("/w/a"))
	put(filepath.Join(claudeDir, "00000001-0000-4000-8000-000000000000.jsonl"), claudeLine("/w/a", "claude new"), now.Add(-time.Hour))
	put(filepath.Join(claudeDir, "00000002-0000-4000-8000-000000000000.jsonl"), claudeLine("/w/a", "claude held"), now.Add(-time.Minute))
	today := sessions.CodexDayDir(filepath.Join(home, ".codex", "sessions"), now)
	put(filepath.Join(today, "rollout-2026-10-23T10-00-00-00000003-0000-4000-8000-000000000000.jsonl"),
		`{"type":"session_meta","timestamp":"2026-10-23T10:00:00Z","payload":{"id":"00000003-0000-4000-8000-000000000000","cwd":"/w/b"}}`+"\n"+
			`{"type":"event_msg","payload":{"type":"user_message","message":"codex middle"}}`+"\n", now.Add(-30*time.Minute))

	s := newV2Sessions(home, func() map[string]string { return map[string]string{"00000002-0000-4000-8000-000000000000": "tab-held"} })
	got, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].SessionID != "00000003-0000-4000-8000-000000000000" || got[1].SessionID != "00000001-0000-4000-8000-000000000000" {
		t.Fatalf("%+v", got)
	}
	if got[0].Provider != wire.ProviderCodex || got[0].Title != "codex middle" || got[0].Cwd != "/w/b" || got[1].Provider != wire.ProviderClaude || got[1].Turns != 1 {
		t.Fatalf("%+v", got)
	}
	if err := wire.AgentSessions(got).Validate(); err != nil {
		t.Fatal(err)
	}
	if f, ok := s.Find(wire.ProviderClaude, "00000002-0000-4000-8000-000000000000"); !ok || f.Cwd != "/w/a" || f.TranscriptPath != filepath.Join(claudeDir, "00000002-0000-4000-8000-000000000000.jsonl") {
		t.Fatalf("find: %+v %v", f, ok)
	}
	if _, ok := s.Find(wire.ProviderCodex, "00000001-0000-4000-8000-000000000000"); ok {
		t.Fatal("a Claude session found among Codex's")
	}
}
