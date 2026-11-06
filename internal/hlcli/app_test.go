package hlcli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
)

func TestAppGuardHook(t *testing.T) {
	dir := t.TempDir()
	snap := apps.Snapshot{
		Apps:      []apps.SnapshotApp{{ID: "acme.review", Name: "Ревью", Tools: &apps.ToolPolicy{Deny: []string{"Bash(git push*)"}, Confirm: []string{"Edit"}}}},
		Protected: []string{"/data/apps"},
	}
	raw, _ := json.Marshal(snap)
	sum := sha256.Sum256(raw)
	policy := filepath.Join(dir, "policy-"+hex.EncodeToString(sum[:8])+".json")
	if err := os.WriteFile(policy, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(path, stdin string) (int, string) {
		var out bytes.Buffer
		code := Run([]string{"hook", "app-guard", "--policy", path}, Env{Getenv: func(string) string { return "" }, Stdin: strings.NewReader(stdin), Stdout: &out})
		return code, out.String()
	}
	decision := func(out string) string {
		var a struct {
			H struct {
				D string `json:"permissionDecision"`
			} `json:"hookSpecificOutput"`
		}
		_ = json.Unmarshal([]byte(out), &a)
		return a.H.D
	}

	for _, c := range []struct{ name, call, want string }{
		{"denied", `{"tool_name":"Bash","tool_input":{"command":"git push origin"}}`, "deny"},
		{"denied inside a line", `{"tool_name":"Bash","tool_input":{"command":"git status && git push"}}`, "deny"},
		{"a line it cannot read", `{"tool_name":"Bash","tool_input":{"command":"sh -c 'git push'"}}`, "ask"},
		{"confirm", `{"tool_name":"Edit","tool_input":{"file_path":"/w/a.go"}}`, "ask"},
		{"no rule", `{"tool_name":"Bash","tool_input":{"command":"git status"}}`, ""},
		{"turning an app off from inside", `{"tool_name":"Bash","tool_input":{"command":"harnessly app disable acme.review"}}`, "deny"},
		{"editing a package", `{"tool_name":"Write","tool_input":{"file_path":"/data/apps/packages/x/app.json"}}`, "deny"},
		{"an unreadable call", `not json`, "deny"},
	} {
		code, out := run(policy, c.call)
		if code != 0 || decision(out) != c.want || (c.want == "" && out != "") {
			t.Errorf("%s: %d %q", c.name, code, out)
		}
	}
	tampered := filepath.Join(dir, "policy-0000000000000000.json")
	_ = os.WriteFile(tampered, []byte(`{"apps":[]}`), 0o600)
	if code, out := run(tampered, `{"tool_name":"Bash","tool_input":{"command":"git push"}}`); code != 0 || decision(out) != "deny" {
		t.Fatalf("a changed snapshot refuses: %d %q", code, out)
	}
	if code, out := run(filepath.Join(dir, "gone.json"), `{"tool_name":"Read","tool_input":{}}`); code != 0 || decision(out) != "deny" {
		t.Fatalf("no snapshot refuses: %d %q", code, out)
	}
	var out bytes.Buffer
	if code := Run([]string{"hook", "app-guard"}, Env{Getenv: func(string) string { return "" }, Stdin: strings.NewReader(`{}`), Stdout: &out, Stderr: &out}); code == 0 {
		t.Fatal("app-guard without --policy should be a usage error")
	}
}

// The same hook for Codex (--agent codex): Codex's tools and Codex's answer, from the same snapshot.
func TestAppGuardHookCodex(t *testing.T) {
	dir := t.TempDir()
	snap := apps.Snapshot{
		Apps:      []apps.SnapshotApp{{ID: "acme.review", Name: "Ревью", Tools: &apps.ToolPolicy{Deny: []string{"Bash(git push*)"}, Confirm: []string{"Edit"}}}},
		Protected: []string{"/data/apps"},
	}
	raw, _ := json.Marshal(snap)
	sum := sha256.Sum256(raw)
	policy := filepath.Join(dir, "policy-"+hex.EncodeToString(sum[:8])+".json")
	if err := os.WriteFile(policy, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(path, stdin string) (int, string) {
		var out bytes.Buffer
		code := Run([]string{"hook", "app-guard", "--policy", path, "--agent", "codex"}, Env{Getenv: func(string) string { return "" }, Stdin: strings.NewReader(stdin), Stdout: &out})
		return code, out.String()
	}
	for _, c := range []struct{ name, in, want string }{
		{"deny", `{"cwd":"/w","tool_name":"Bash","tool_input":{"command":"git push"}}`, "deny"},
		{"confirm is a refusal", `{"cwd":"/w","tool_name":"apply_patch","tool_input":{"command":"*** Update File: a.go"}}`, "deny"},
		{"no rule", `{"cwd":"/w","tool_name":"shell","tool_input":{"command":["bash","-lc","git status"]}}`, ""},
		{"unknown tool", `{"cwd":"/w","tool_name":"teleport","tool_input":{}}`, "deny"},
		{"unreadable", `not json`, "deny"},
	} {
		code, out := run(policy, c.in)
		var a struct {
			H struct {
				D string `json:"permissionDecision"`
				R string `json:"permissionDecisionReason"`
			} `json:"hookSpecificOutput"`
		}
		_ = json.Unmarshal([]byte(out), &a)
		if code != 0 || a.H.D != c.want || (c.want == "" && out != "") || (c.want == "deny" && a.H.R == "") {
			t.Errorf("%s: %d %q", c.name, code, out)
		}
	}
	if code, out := run(filepath.Join(dir, "gone.json"), `{"tool_name":"update_plan","tool_input":{}}`); code != 0 || !strings.Contains(out, `"deny"`) {
		t.Fatalf("no snapshot refuses: %d %q", code, out)
	}
}

func TestAppGuardJournalsRefusal(t *testing.T) {
	dir := t.TempDir()
	snap := apps.Snapshot{Apps: []apps.SnapshotApp{{ID: "acme.review", Name: "Ревью", Tools: &apps.ToolPolicy{Deny: []string{"Bash(git push*)"}, Confirm: []string{"Edit"}}}}}
	raw, _ := json.Marshal(snap)
	sum := sha256.Sum256(raw)
	policy := filepath.Join(dir, "policy-"+hex.EncodeToString(sum[:8])+".json")
	if err := os.WriteFile(policy, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		if r.URL.Path == "/api/cli/apps/decision" {
			got = append(got, b)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	env := map[string]string{"HARNESSLY_URL": srv.URL, "HARNESSLY_CLI_TOKEN": "t", "HARNESSLY_SESSION": "hs-1"}
	run := func(stdin string) string {
		var out bytes.Buffer
		Run([]string{"hook", "app-guard", "--policy", policy}, Env{Getenv: func(k string) string { return env[k] }, Stdin: strings.NewReader(stdin), Stdout: &out})
		return out.String()
	}
	out := run(`{"session_id":"s1","tool_name":"Bash","tool_input":{"command":"git push origin"}}`)
	if !strings.Contains(out, `[app-guard \"Ревью\" \"Bash(git push*)\"]`) && !strings.Contains(out, `[app-guard \u0022`) {
		t.Errorf("the reason does not carry the mark: %s", out)
	}
	run(`{"session_id":"s1","tool_name":"Edit","tool_input":{"file_path":"/w/a.go"}}`)
	run(`{"session_id":"s1","tool_name":"Bash","tool_input":{"command":"git status"}}`)
	if len(got) != 2 || got[0]["decision"] != "deny" || got[0]["rule"] != "Bash(git push*)" || got[0]["app"] != "acme.review" ||
		got[0]["session"] != "hs-1" || got[0]["agentSession"] != "s1" || got[1]["decision"] != "ask" {
		t.Errorf("journaled %v", got)
	}
	if _, has := got[0]["command"]; has || strings.Contains(fmt.Sprint(got[0]), "git push origin") {
		t.Errorf("the call's input went out: %v", got[0])
	}
}
