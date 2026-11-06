package apps

import (
	"encoding/json"
	"strings"
	"testing"
)

func codexSnap() Snapshot {
	return Snapshot{
		Apps: []SnapshotApp{{ID: "acme.review", Name: "Ревью", Tools: &ToolPolicy{
			Deny: []string{"Bash(git push*)"}, Confirm: []string{"Edit(/w/ask/*)"}, Allow: []string{"Bash(git status*)"}}}},
		Protected: []string{"/data/apps"},
	}
}

func codexIn(tool, input string) []byte {
	return []byte(`{"hook_event_name":"PreToolUse","session_id":"s","cwd":"/w","tool_name":"` + tool + `","tool_input":` + input + `}`)
}

// decisionOf reads Codex's answer; "" is silence, the only way to let a call go on.
func decisionOf(t *testing.T, out []byte) (decision, reason string) {
	t.Helper()
	if len(out) == 0 {
		return "", ""
	}
	var a struct {
		H struct {
			Event  string `json:"hookEventName"`
			D      string `json:"permissionDecision"`
			Reason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &a); err != nil || a.H.Event != "PreToolUse" {
		t.Fatalf("not Codex's PreToolUse answer: %q %v", out, err)
	}
	return a.H.D, a.H.Reason
}

func TestCodexGuardAnswers(t *testing.T) {
	s := codexSnap()
	for _, c := range []struct{ name, tool, input, want string }{
		{"allow is silence", "Bash", `{"command":"git status"}`, ""},
		{"no rule is silence", "Bash", `{"command":"ls"}`, ""},
		{"deny", "Bash", `{"command":"git push origin"}`, "deny"},
		{"deny inside a line", "shell_command", `{"command":"git status && git push"}`, "deny"},
		{"argv of a shell", "shell", `{"command":["bash","-lc","git push"]}`, "deny"},
		{"exec_command cmd", "exec_command", `{"cmd":"git push"}`, "deny"},
		{"confirm becomes deny", "apply_patch", `{"command":"*** Begin Patch\n*** Update File: ask/a.go\n@@\n-a\n+b\n*** End Patch"}`, "deny"},
		{"a patch of the registry", "apply_patch", `{"command":"*** Begin Patch\n*** Add File: /data/apps/packages/x/app.json\n+x\n*** End Patch"}`, "deny"},
		{"a harmless patch", "apply_patch", `{"command":"*** Begin Patch\n*** Add File: ok/a.go\n+x\n*** End Patch"}`, ""},
		{"turning an app off", "Bash", `{"command":"harnessly app disable acme.review"}`, "deny"},
		{"a harmless tool", "update_plan", `{}`, ""},
		{"an mcp tool of no rule", "mcp__other__thing", `{}`, ""},
		{"a plugin of an app that is not on", "mcp__plugin_evil-app_srv__do", `{}`, "deny"},
		{"a plugin of an app that is on", "mcp__plugin_acme-review_srv__do", `{}`, ""},
	} {
		d, reason := decisionOf(t, s.CodexAnswer(codexIn(c.tool, c.input)))
		if d != c.want {
			t.Errorf("%s: %q (%s)", c.name, d, reason)
		}
		if d == "deny" && strings.TrimSpace(reason) == "" {
			t.Errorf("%s: Codex rejects a deny without a reason", c.name)
		}
	}
	// The confirm rule says why it is a refusal.
	_, reason := decisionOf(t, s.CodexAnswer(codexIn("apply_patch", `{"command":"*** Update File: ask/a.go"}`)))
	if !strings.Contains(reason, "no step where the person confirms") {
		t.Errorf("confirm reason: %s", reason)
	}
}

// A call the guard cannot read, or a tool it does not know, is never let through.
func TestCodexGuardFailsClosed(t *testing.T) {
	s := codexSnap()
	for _, c := range []struct{ name, in string }{
		{"not json", `nope`},
		{"empty", ``},
		{"no tool", `{"tool_input":{}}`},
		{"an unknown tool", string(codexIn("teleport", `{"to":"/"}`))},
		{"a shell call with no command", string(codexIn("shell", `{}`))},
		{"a command that is not text", string(codexIn("shell", `{"command":5}`))},
		{"an argv with a non-word", string(codexIn("shell", `{"command":["git",1]}`))},
		{"a patch with no files", string(codexIn("apply_patch", `{"command":"nothing"}`))},
		{"a relative patch with no folder", `{"tool_name":"apply_patch","tool_input":{"command":"*** Add File: a.go"}}`},
		{"an unreadable plugin name", string(codexIn("mcp__plugin_x__do", `{}`))},
		{"a read with no path", string(codexIn("read_file", `{}`))},
	} {
		if d, reason := decisionOf(t, s.CodexAnswer([]byte(c.in))); d != "deny" || reason == "" {
			t.Errorf("%s: %q %q", c.name, d, reason)
		}
	}
}

// The table is explicit: each Codex tool names the Claude tool the rules know.
func TestCodexToolTable(t *testing.T) {
	cases := []struct {
		tool, input string
		tools       []string
	}{
		{"shell_command", `{"command":"ls"}`, []string{"Bash"}},
		{"local_shell", `{"command":["ls","-l"]}`, []string{"Bash"}},
		{"unified_exec", `{"cmd":"ls"}`, []string{"Bash"}},
		{"view_image", `{"path":"img/a.png"}`, []string{"Read"}},
		{"web_search", `{"query":"go"}`, []string{"WebSearch"}},
		{"shell", `{"command":["ls","-l","a b"]}`, []string{"Bash"}},
		{"apply_patch", `{"input":"*** Update File: a.go\n*** Move to: b.go"}`, []string{"Edit", "Write", "Edit", "Write"}},
		{"read_file", `{"file_path":"a.go"}`, []string{"Read"}},
		{"grep_files", `{"path":"a"}`, []string{"Grep"}},
		{"mcp__harnessly_app__app_data_get", `{}`, []string{"mcp__harnessly-app__app_data_get"}},
		{"mcp__rt__x", `{}`, []string{"mcp__rt__x"}},
	}
	for _, c := range cases {
		calls, skip, problem := CodexCalls(codexIn(c.tool, c.input))
		if skip || problem != "" || len(calls) != len(c.tools) {
			t.Errorf("%s: %+v %v %q", c.tool, calls, skip, problem)
			continue
		}
		for i, call := range calls {
			if call.Tool != c.tools[i] {
				t.Errorf("%s: call %d is %s", c.tool, i, call.Tool)
			}
		}
	}
	// Relative paths are the call's folder's.
	calls, _, _ := CodexCalls(codexIn("apply_patch", `{"command":"*** Add File: sub/a.go"}`))
	if got := calls[0].Input["file_path"]; got != "/w/sub/a.go" {
		t.Errorf("path: %v", got)
	}
	for _, tool := range []string{"update_plan", "request_user_input"} {
		if _, skip, _ := CodexCalls(codexIn(tool, `{}`)); !skip {
			t.Errorf("%s needs no rule", tool)
		}
	}
	if _, skip, problem := CodexCalls(codexIn("write_stdin", `{"chars":""}`)); !skip || problem != "" {
		t.Error("polling writes nothing")
	}
	// Only [shell, -c|-lc|-cl, script] is the script; every other argv is one quoted line.
	for argv, want := range map[string]string{
		`["bash","-c","ok","extra"]`:          "bash -c ok extra",
		`["bash","-xc","git push"]`:           "bash -xc 'git push'",
		`["bash","-o","pipefail","-c","x y"]`: "bash -o pipefail -c 'x y'",
		`["bash","-lc","git push"]`:           "git push",
		`["sh","-cl","git push"]`:             "git push",
		`["python3","-c","git push"]`:         "python3 -c 'git push'",
	} {
		calls, _, problem := CodexCalls(codexIn("shell", `{"command":`+argv+`}`))
		if problem != "" || calls[0].Input["command"] != want {
			t.Errorf("%s: %v %q", argv, calls, problem)
		}
	}
}

// Input typed into a running process is judged by the snapshot: with rules for the shell it is refused whole, since a
// command can be given a character at a time; without any it goes on.
func TestCodexGuardStdin(t *testing.T) {
	typed := codexIn("write_stdin", `{"session_id":1,"chars":"c"}`)
	if d, reason := decisionOf(t, codexSnap().CodexAnswer(typed)); d != "deny" || !strings.Contains(reason, "Interactive input into a shell") {
		t.Errorf("with Bash rules: %q %s", d, reason)
	}
	// A line feed alone is a keystroke too.
	if d, _ := decisionOf(t, codexSnap().CodexAnswer(codexIn("write_stdin", `{"chars":"\n"}`))); d != "deny" {
		t.Error("whitespace is input")
	}
	protectedOnly := Snapshot{Apps: []SnapshotApp{{ID: "acme.a", Name: "A"}}, Protected: []string{"/data/apps"}}
	if d, _ := decisionOf(t, protectedOnly.CodexAnswer(typed)); d != "deny" {
		t.Error("protected folders alone make the shell matter")
	}
	noShell := Snapshot{Apps: []SnapshotApp{{ID: "acme.a", Name: "A", Tools: &ToolPolicy{Deny: []string{"WebSearch"}, Allow: []string{"mcp:*"}}}}}
	if d, _ := decisionOf(t, noShell.CodexAnswer(typed)); d != "" {
		t.Errorf("without rules for the shell: %q", d)
	}
	if d, _ := decisionOf(t, noShell.CodexAnswer(codexIn("write_stdin", `{"chars":""}`))); d != "" {
		t.Error("a poll writes nothing")
	}
}

// bash -c with words after the script, or other flags, is judged as the whole line, which the guard cannot read into.
func TestCodexGuardShellArgv(t *testing.T) {
	s := codexSnap()
	for _, argv := range []string{
		`["bash","-c","ok","extra"]`, `["bash","-xc","ok"]`, `["bash","-o","pipefail","-c","ok"]`, `["bash","-c","echo","x;","git","push"]`,
	} {
		if d, _ := decisionOf(t, s.CodexAnswer(codexIn("shell", `{"command":`+argv+`}`))); d != "deny" {
			t.Errorf("%s: %q", argv, d)
		}
	}
	if d, _ := decisionOf(t, s.CodexAnswer(codexIn("shell", `{"command":["bash","-lc","git status"]}`))); d != "" {
		t.Errorf("the plain form is its script: %q", d)
	}
}

// view_image and web_search are judged by the rules like Claude's Read and WebSearch.
func TestCodexGuardImageAndSearch(t *testing.T) {
	s := Snapshot{Apps: []SnapshotApp{{ID: "acme.a", Name: "A", Tools: &ToolPolicy{Deny: []string{"WebSearch", "Read(/secret/*)"}}}}}
	for _, c := range []struct{ tool, input, want string }{
		{"web_search", `{"query":"x"}`, "deny"},
		{"view_image", `{"path":"/secret/a.png"}`, "deny"},
		{"view_image", `{"path":"/ok/a.png"}`, ""},
		{"view_image", `{}`, "deny"},
	} {
		if d, _ := decisionOf(t, s.CodexAnswer(codexIn(c.tool, c.input))); d != c.want {
			t.Errorf("%s %s: %q", c.tool, c.input, d)
		}
	}
}
