package hlcli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents/antigravity"
)

func TestAntigravityHookConfigNativeSchema(t *testing.T) {
	data, err := AntigravityHookConfig("/opt/harnessly's cli")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]map[string][]struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"SessionStart", "PreInvocation", "PostInvocation", "Stop"} {
		handlers := config["harnessly-native"][event]
		if len(handlers) != 1 || handlers[0].Type != "command" || handlers[0].Timeout != 5 || !strings.HasSuffix(handlers[0].Command, " hook antigravity "+event) {
			t.Fatalf("%s: %+v", event, handlers)
		}
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "--token") {
		t.Fatal("credential in hooks")
	}
	if _, err := AntigravityHookConfig("relative"); err == nil {
		t.Fatal("relative executable accepted")
	}
}

func TestAntigravityHookForwarding(t *testing.T) {
	const id = "12345678-1234-1234-1234-123456789abc"
	home := t.TempDir()
	for _, tc := range []struct {
		name, conversation, generation, expected, input string
		want                                            bool
	}{
		{"valid", "chat", "gen", id, "", true},
		{"outside launch", "", "", id, "", false},
		{"missing generation", "chat", "", id, "", false},
		{"nested", "chat", "gen", "87654321-1234-1234-1234-123456789abc", "", false},
		{"malformed", "chat", "gen", id, "{}", false},
		{"oversized", "chat", "gen", id, strings.Repeat("x", (64<<10)+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path != "/api/status/hook" || r.URL.Query().Get("provider") != "antigravity" || r.URL.Query().Get("tmuxSession") != "tmux" || r.Header.Get("x-cli-token") != "secret" {
					t.Error("incorrect authenticated route")
				}
				var report struct {
					Generation string                  `json:"generation"`
					Payload    antigravity.HookPayload `json:"payload"`
				}
				if json.NewDecoder(r.Body).Decode(&report) != nil || report.Generation != "gen" || report.Payload.SessionID != id {
					t.Error("incorrect report")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			input := tc.input
			if input == "" {
				data, _ := json.Marshal(antigravity.HookPayload{SessionID: id, TranscriptPath: antigravity.TranscriptPath(home, id), FullyIdle: true})
				input = string(data)
			}
			env := map[string]string{"HARNESSLY_URL": server.URL, "HARNESSLY_CLI_TOKEN": "secret", "HARNESSLY_SESSION": "tmux", "HARNESSLY_ANTIGRAVITY_CONVERSATION": tc.conversation, "HARNESSLY_ANTIGRAVITY_GENERATION": tc.generation, "HARNESSLY_ANTIGRAVITY_EXPECTED_UUID": tc.expected}
			code := Run([]string{"hook", "antigravity", "Stop"}, Env{Home: home, Getenv: func(k string) string { return env[k] }, Stdin: strings.NewReader(input)})
			want := 0
			if tc.want {
				want = 1
			}
			if code != 0 || requests != want {
				t.Fatalf("code %d requests %d", code, requests)
			}
		})
	}
}
