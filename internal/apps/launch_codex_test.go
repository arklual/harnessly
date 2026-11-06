package apps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A Codex chat with an app: the guard is the Codex form of the same hook over the same snapshot, the instructions carry the
// skills as files to read, the servers come by their plugin names, the launch has a token of its own and the wall.
func TestLauncherCodex(t *testing.T) {
	data := t.TempDir()
	reg := &Registry{Root: filepath.Join(data, "apps")}
	pkg := filepath.Join(t.TempDir(), "review")
	if err := Scaffold(pkg, "acme.review", "Ревью", "chat"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "instructions.md"), []byte("Review only.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mcp := `{"mcpServers":{"bank":{"type":"http","url":"https://bank.test/mcp","headers":{"X-Plain":"1"}}}}`
	if err := os.WriteFile(filepath.Join(pkg, ".mcp.json"), []byte(mcp), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatal(err)
	}
	var probed string
	l := &Launcher{Registry: reg, GuardCommand: "/bin/harnessly hook app-guard",
		GuardProbe: func(c string) error { probed = c; return nil },
		AgentToken: func(session string, _ time.Duration) (string, error) { return "agent." + session + ".1.mac", nil }}
	got, err := l.Codex("tab-c1", []string{"acme.review"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Guard, "/bin/harnessly hook app-guard --policy '") || !strings.HasSuffix(got.Guard, "' --agent codex") || probed != got.Guard {
		t.Fatalf("guard: %q probed %q", got.Guard, probed)
	}
	policy := strings.TrimSuffix(strings.TrimPrefix(got.Guard, "/bin/harnessly hook app-guard --policy '"), "' --agent codex")
	rawSnap, _ := os.ReadFile(policy)
	var snap Snapshot
	if json.Unmarshal(rawSnap, &snap) != nil || len(snap.Apps) != 1 || !SnapshotIntact(policy, rawSnap) {
		t.Fatalf("snapshot: %s", rawSnap)
	}
	if !strings.Contains(got.Instructions, "Review only.") || !strings.Contains(got.Instructions, "SKILL.md (read it before using)") ||
		!strings.Contains(got.Instructions, filepath.Join(pkg, "skills")) {
		t.Fatalf("instructions: %q", got.Instructions)
	}
	if len(got.Servers) != 1 || got.Servers[0].Name != "plugin_acme-review_bank" || got.Servers[0].URL != "https://bank.test/mcp" || got.Servers[0].Headers["X-Plain"] != "1" {
		t.Fatalf("servers: %+v", got.Servers)
	}
	token, _ := os.ReadFile(got.TokenFile)
	if !strings.HasPrefix(string(token), "agent.") || !strings.Contains(got.Env, "HARNESSLY_CLI_TOKEN_FILE=") {
		t.Fatalf("token %q env %q", token, got.Env)
	}
	grant, _ := os.ReadFile(filepath.Join(filepath.Dir(got.TokenFile), "grant.json"))
	if !strings.Contains(string(grant), "acme.review") {
		t.Fatalf("grant: %s", grant)
	}
	if _, err := l.Codex("tab-c1", []string{"acme.nope"}); err == nil {
		t.Fatal("an app not installed launched")
	}
	failing := &Launcher{Registry: reg, GuardProbe: func(string) error { return os.ErrDeadlineExceeded }}
	if _, err := failing.Codex("", []string{"acme.review"}); err == nil {
		t.Fatal("a guard that does not answer must stop the chat")
	}
}
