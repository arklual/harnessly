package imagegen

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The shell only launches this test executable; no real provider or login is used.
func TestAntigravityACPHelper(t *testing.T) {
	if os.Getenv("IMAGEGEN_AGY_HELPER") != "1" {
		return
	}
	mode := os.Getenv("IMAGEGEN_AGY_MODE")
	if len(os.Args) < 4 || os.Args[len(os.Args)-3] != "acp" || os.Args[len(os.Args)-2] != "--sandbox" || os.Args[len(os.Args)-1] != "--disable-telemetry" {
		os.Exit(20)
	}
	scan := bufio.NewScanner(os.Stdin)
	scan.Buffer(make([]byte, 4096), 4*1024*1024)
	send := func(v any) {
		if json.NewEncoder(os.Stdout).Encode(v) != nil {
			os.Exit(21)
		}
	}
	reply := func(id json.RawMessage, result any) {
		send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	for scan.Scan() {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(scan.Bytes(), &m) != nil {
			os.Exit(22)
		}
		switch m.Method {
		case "initialize":
			var p struct {
				Protocol int `json:"protocolVersion"`
				Caps     struct {
					FS       map[string]bool `json:"fs"`
					Terminal bool            `json:"terminal"`
				} `json:"clientCapabilities"`
				Client struct {
					Name string `json:"name"`
				} `json:"clientInfo"`
			}
			_ = json.Unmarshal(m.Params, &p)
			if p.Protocol != 1 || p.Client.Name != "harnessly" || p.Caps.Terminal || p.Caps.FS["readTextFile"] || p.Caps.FS["writeTextFile"] {
				os.Exit(23)
			}
			if mode == "auth" {
				send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32000, "message": "Authentication required"}})
				continue
			}
			reply(m.ID, map[string]any{"protocolVersion": 1})
		case "session/new":
			var p struct {
				Cwd string `json:"cwd"`
				MCP []any  `json:"mcpServers"`
			}
			_ = json.Unmarshal(m.Params, &p)
			cwd, _ := os.Getwd()
			if p.Cwd != cwd || p.MCP == nil || len(p.MCP) != 0 {
				os.Exit(24)
			}
			id := "valid_session-1"
			if mode == "invalid" {
				id = "../escape"
			}
			reply(m.ID, map[string]string{"sessionId": id})
		case "session/prompt":
			if !strings.Contains(string(m.Params), "invoke_subagent") || !strings.Contains(string(m.Params), "image-generator") || !strings.Contains(string(m.Params), "exactly 2 images") {
				os.Exit(25)
			}
			if mode == "exit" {
				os.Exit(0)
			}
			if mode == "permission" {
				send(map[string]any{"jsonrpc": "2.0", "id": 91, "method": "session/request_permission", "params": map[string]any{}})
				continue
			}
			if mode != "nodispatch" {
				send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "valid_session-1", "update": map[string]any{"sessionUpdate": "tool_call", "title": "invoke_subagent"}}})
			}
			send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "valid_session-1", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "No native dispatch available"}}}})
			reply(m.ID, map[string]string{"stopReason": "end_turn"})
			if mode == "success" {
				time.Sleep(100 * time.Millisecond)
				_ = os.WriteFile(filepath.Join(os.Getenv("HOME"), "ready"), []byte("ready"), 0600)
			}
		case "":
			if string(m.ID) == "91" {
				if !strings.Contains(string(m.Result), "cancelled") {
					os.Exit(26)
				}
				send(map[string]any{"jsonrpc": "2.0", "id": 92, "method": "fs/read_text_file", "params": map[string]any{}})
			} else if string(m.ID) == "92" {
				if !strings.Contains(string(m.Error), "-32601") {
					os.Exit(27)
				}
				send(map[string]any{"jsonrpc": "2.0", "id": 3, "error": map[string]any{"code": -32000, "message": "permission cancelled verified"}})
			}
		}
	}
	os.Exit(0)
}

func TestAntigravityACP(t *testing.T) {
	for _, tc := range []struct{ mode, want string }{
		{"success", ""}, {"auth", "agy acp --login"}, {"invalid", "invalid sessionId"}, {"permission", "permission cancelled verified"}, {"nodispatch", "No native dispatch available"}, {"timeout", "deadline exceeded"}, {"exit", "stream ended"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			home := t.TempDir()
			work := t.TempDir()
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			wrapper := filepath.Join(t.TempDir(), "agy")
			script := fmt.Sprintf("#!/bin/sh\nexec '%s' -test.run '^TestAntigravityACPHelper$' -- \"$@\"\n", strings.ReplaceAll(exe, "'", "'\\''"))
			if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			s := Service{AntigravityBinary: wrapper, AntigravityEnv: func() []string {
				return append(os.Environ(), "HOME="+home, "IMAGEGEN_AGY_HELPER=1", "IMAGEGEN_AGY_MODE="+tc.mode)
			}}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			calls := 0
			err = s.runAntigravity(ctx, Request{Prompt: "a bird", Count: 2, Shape: Landscape}, work, func(dir string) int {
				calls++
				want := filepath.Join(home, ".gemini", "antigravity-cli", "brain", "valid_session-1")
				if dir != want {
					t.Errorf("collector directory %q, want %q", dir, want)
				}
				if _, e := os.Stat(filepath.Join(home, "ready")); e == nil {
					return 2
				}
				return 0
			})
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				if calls < 2 {
					t.Error("did not watch asynchronous artifacts after end_turn")
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v, want %q", err, tc.want)
			}
			if (tc.mode == "invalid" || tc.mode == "auth") && calls != 0 {
				t.Error("collected without a valid session")
			}
		})
	}
}

func TestAntigravitySessionID(t *testing.T) {
	for _, id := range []string{"", ".", "..", "../outside", "a/b", `a\b`, "a b", strings.Repeat("a", 129), "é"} {
		if antigravitySessionID(id) {
			t.Errorf("accepted %q", id)
		}
	}
	for _, id := range []string{"a", "ABC-123_xyz", strings.Repeat("x", 128)} {
		if !antigravitySessionID(id) {
			t.Errorf("rejected %q", id)
		}
	}
}
