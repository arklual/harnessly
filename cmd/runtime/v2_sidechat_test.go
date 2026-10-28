package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/sessions"
	"github.com/ly-solutions/harnessly/internal/sidechat"
	"github.com/ly-solutions/harnessly/internal/wire"
)

const sideChatUUID = "425055ec-bbc8-4e3c-87d0-36fa74c1cccb"

// A side question of a v2 conversation: the conversation is the tab (its session and folder are the agent's own), the
// refusals are the Legacy ones, the frames are the Legacy ones.
func TestV2SideChat(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	proj := filepath.Join(home, ".claude", "projects", sessions.ProjectName(cwd))
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	recs := map[string]wire.Conversation{
		"tab-nosess": {ID: "tab-nosess", Provider: wire.ProviderClaude, Cwd: cwd},
		"tab-codex":  {ID: "tab-codex", Provider: wire.ProviderCodex, Cwd: cwd, AgentSessionID: sideChatUUID},
		"tab-nofile": {ID: "tab-nofile", Provider: wire.ProviderClaude, Cwd: cwd, AgentSessionID: sideChatUUID},
		"tab-ok":     {ID: "tab-ok", Provider: wire.ProviderClaude, Cwd: cwd, AgentSessionID: sideChatUUID},
	}
	var ran []sidechat.Run
	envDir := filepath.Join(t.TempDir(), "agent-env")
	writeEnvFiles(t, envDir, map[string]string{"claude": "ANTHROPIC_API_KEY='from-the-agent'\n"})
	c := &v2SideChat{
		record: func(id string) (wire.Conversation, bool) { r, ok := recs[id]; return r, ok },
		home:   home,
		envDir: envDir,
		ask: func(ctx context.Context, run sidechat.Run, on func(sidechat.Event)) {
			ran = append(ran, run)
			on(sidechat.Event{Type: "delta", Text: "Hi"})
			on(sidechat.Event{Type: "done", Text: "Hi"})
		},
	}
	post := func(conv, body string) (int, string, error) {
		req := httptest.NewRequest("POST", "/api/v2/conversations/"+conv+"/side-chat", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		err := c.Serve(w, req, conv)
		return w.Code, w.Body.String(), err
	}
	if _, _, err := post("tab-none", `{"question":"q"}`); err != convo.ErrNotFound {
		t.Fatalf("unknown: %v", err)
	}
	for _, tc := range []struct {
		conv, body, code string
		status           int
	}{
		{"tab-ok", `not json`, "bad-request", 400},
		{"tab-ok", `{"question":""}`, "bad-request", 400},
		{"tab-codex", `{"question":"q"}`, "unsupported", 422},
		{"tab-nosess", `{"question":"q"}`, "no-session", 409},
		{"tab-nofile", `{"question":"q"}`, "no-session", 409},
	} {
		status, body, err := post(tc.conv, tc.body)
		if err != nil || status != tc.status || !strings.Contains(body, `"code":"`+tc.code+`"`) {
			t.Errorf("%s %s: %d %q %v", tc.conv, tc.body, status, body, err)
		}
	}
	if len(ran) != 0 {
		t.Fatalf("a refused question ran: %v", ran)
	}

	// The session file exists: the question runs on a fork of that session in the conversation's folder, whatever tab the
	// body names.
	if err := os.WriteFile(filepath.Join(proj, sideChatUUID+".jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, body, err := post("tab-ok", `{"tabId":"tab-other","question":"What is this?","model":"sonnet"}`)
	want := "data: {\"type\":\"delta\",\"text\":\"Hi\"}\n\ndata: {\"type\":\"done\",\"text\":\"Hi\"}\n\n"
	if err != nil || status != 200 || body != want {
		t.Fatalf("%d %q %v", status, body, err)
	}
	if len(ran) != 1 || ran[0].SessionID != sideChatUUID || ran[0].Cwd != cwd || ran[0].Model != "sonnet" || ran[0].ProjectDir != proj ||
		!strings.Contains(ran[0].Prompt, "What is this?") {
		t.Fatalf("%+v", ran)
	}
	// The key the machine agent keeps for Claude reaches the question's process.
	if !slices.Contains(ran[0].BaseEnv, "ANTHROPIC_API_KEY=from-the-agent") {
		t.Errorf("the env files did not reach the side chat: %v", ran[0].BaseEnv)
	}

	// One question runs per conversation.
	release := sidechat.Claim("tab-ok")
	if release == nil {
		t.Fatal("claim")
	}
	status, body, _ = post("tab-ok", `{"question":"q"}`)
	release()
	if status != 409 || !strings.Contains(body, `"code":"busy"`) {
		t.Fatalf("busy: %d %q", status, body)
	}
	if sidechat.Claim("tab-ok") == nil {
		t.Fatal("the question did not let go")
	}
}
