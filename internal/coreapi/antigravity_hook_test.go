package coreapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/antigravity"
)

func TestAntigravityHooksRequireAuthorizedLaunch(t *testing.T) {
	const id = "12345678-1234-1234-1234-123456789abc"
	home := t.TempDir()
	for _, tc := range []struct {
		name                       string
		token, authorize, callback bool
		generation, expected, path string
	}{
		{"valid", true, true, true, "generation", id, antigravity.TranscriptPath(home, id)},
		{"no token", false, true, true, "generation", id, antigravity.TranscriptPath(home, id)},
		{"no callback", true, false, true, "generation", id, antigravity.TranscriptPath(home, id)},
		{"stale or wrong tab", true, true, false, "generation", id, antigravity.TranscriptPath(home, id)},
		{"no generation", true, true, true, "", id, antigravity.TranscriptPath(home, id)},
		{"nested agent", true, true, true, "generation", "87654321-1234-1234-1234-123456789abc", antigravity.TranscriptPath(home, id)},
		{"foreign path", true, true, true, "generation", id, "/tmp/transcript.jsonl"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := &fakeKeeper{applied: true}
			c := &Config{Keeper: k, AntigravityHome: home, TokenOK: func(*http.Request) bool { return tc.token }}
			if tc.authorize {
				c.AntigravityAuthorize = func(s, conversation, generation string, p antigravity.HookPayload) bool {
					return tc.callback && s == "tmux" && conversation == "chat" && generation == "generation" && p.SessionID == id
				}
			}
			r := httptest.NewRequest(http.MethodPost, "/api/status/hook?provider=antigravity&tmuxSession=tmux", nil)
			c.hook(r, map[string]any{"event": "SessionStart", "conversation": "chat", "generation": tc.generation, "expectedUUID": tc.expected, "payload": antigravity.HookPayload{SessionID: id, TranscriptPath: tc.path}})
			want := 0
			if tc.name == "valid" {
				want = 1
			}
			if len(k.named("meta")) != want || len(k.named("event")) != want {
				t.Fatalf("unexpected keeper calls %+v", k.calls)
			}
		})
	}
}

func TestAntigravityStopRequiresFullyIdle(t *testing.T) {
	const id = "12345678-1234-1234-1234-123456789abc"
	home := t.TempDir()
	for _, tc := range []struct {
		event string
		idle  bool
		kind  agents.EventKind
	}{
		{"SessionStart", false, agents.SessionStart}, {"PreInvocation", false, ""},
		{"PostInvocation", true, ""}, {"Stop", false, ""}, {"Stop", true, agents.Stop},
	} {
		k := &fakeKeeper{applied: true}
		c := &Config{Keeper: k, AntigravityHome: home, AntigravityAuthorize: func(string, string, string, antigravity.HookPayload) bool { return true }}
		r := httptest.NewRequest(http.MethodPost, "/api/status/hook?provider=antigravity&tmuxSession=tmux", nil)
		c.antigravityHook(r, map[string]any{"event": tc.event, "conversation": "chat", "generation": "gen", "payload": antigravity.HookPayload{SessionID: id, TranscriptPath: antigravity.TranscriptPath(home, id), FullyIdle: tc.idle, LastUserInput: "hello"}})
		events := k.named("event")
		if tc.kind == "" {
			if len(events) != 0 {
				t.Fatalf("%s finished turn", tc.event)
			}
			continue
		}
		if len(events) != 1 || events[0].args[2].(agents.Event).Kind != tc.kind || events[0].args[2].(agents.Event).AgentSession != id {
			t.Fatalf("events %+v", events)
		}
	}
}
