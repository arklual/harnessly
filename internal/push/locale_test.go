package push

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/ly-solutions/harnessly/internal/status"
)

func TestMain(m *testing.M) {
	// The expectations of the other tests are the previous runtime's texts, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}

func TestEnglishNotifications(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	p := writeJSONL(t,
		`{"type":"ai-title","aiTitle":"Push notifications","sessionId":"s"}`,
		user("make pushes", "2026-09-25T10:00:00.000Z"),
		assistant([]any{m{"type": "text", "text": "## Ready\n\nPushes **work** on `iPhone`."}}, "2026-09-25T10:05:00.000Z"),
	)
	if msg := BuildMessage("tab-1", tabEntry(p, nil), "done", tagPrefix, now); msg.Body != "Done — Ready Pushes work on iPhone." {
		t.Errorf("done: %+v", msg)
	}

	// A tool call waiting for permission, a codex approval, an unknown request.
	p = writeJSONL(t,
		user("clean", "2026-09-25T10:00:00.000Z"),
		assistant([]any{m{"type": "tool_use", "id": "t1", "name": "Bash", "input": m{"command": "rm -rf build"}}}, "2026-09-25T10:01:00.000Z"),
	)
	if msg := BuildMessage("tab-1", tabEntry(p, nil), "question", tagPrefix, now); msg.Body != "Allow: $ rm -rf build" {
		t.Errorf("tool permission: %+v", msg)
	}
	exec := BuildMessage("tab-3", tabEntry("/nonexistent.jsonl", func(e *status.Entry) {
		e.PermissionRequest = status.Some(json.RawMessage(`{"type":"ExecApprovalRequest","command":"pnpm build"}`))
	}), "question", tagPrefix, now)
	if exec.Body != "Allow the command: pnpm build" {
		t.Errorf("command: %+v", exec)
	}
	patch := BuildMessage("tab-3", tabEntry("/nonexistent.jsonl", func(e *status.Entry) {
		e.PermissionRequest = status.Some(json.RawMessage(`{"type":"ApplyPatchApprovalRequest","patches":[{"path":"/a/b.go"},{"path":"c.go"}]}`))
	}), "question", tagPrefix, now)
	if patch.Body != "Allow the edit: b.go, c.go" {
		t.Errorf("patch: %+v", patch)
	}
	if other := BuildMessage("tab-3", tabEntry("/nonexistent.jsonl", func(e *status.Entry) {
		e.PermissionRequest = status.Some(json.RawMessage(`{"type":"Other"}`))
	}), "question", tagPrefix, now); other.Body != "Question: Asks for permission" {
		t.Errorf("other: %+v", other)
	}

	// Fallbacks and a failed turn.
	if msg := BuildMessage("tab-2", tabEntry("/nonexistent.jsonl", nil), "question", tagPrefix, now); msg.Body != "The agent stopped and is waiting for your decision" {
		t.Errorf("fallback: %+v", msg)
	}
	failed := BuildMessage("tab-4", tabEntry("/nonexistent.jsonl", func(e *status.Entry) {
		e.TabName = ""
		e.LastAssistantMessage = status.Some("API Error 529")
		e.LastEvent = status.Some(status.LastEvent{Name: "stop-failure", At: 1, Seq: 1})
	}), "done", tagPrefix, now)
	if failed.Title != "Conversation" || failed.Body != "Error — API Error 529" {
		t.Errorf("failed: %+v", failed)
	}
	plain := BuildMessage("tab-5", tabEntry("", func(e *status.Entry) { e.AgentSessionID = status.Null[string]() }), "done", tagPrefix, now)
	if plain.Body != "Done — The agent finished its work" {
		t.Errorf("plain: %+v", plain)
	}
	if regexp.MustCompile(`[А-Яа-я]`).MatchString(plain.Body + failed.Body + exec.Body) {
		t.Error("an English notification has Russian text")
	}
}
