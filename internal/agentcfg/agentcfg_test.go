package agentcfg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

type machine struct {
	t    *testing.T
	home string
	st   *Store
}

func newMachine(t *testing.T) *machine {
	t.Helper()
	home := t.TempDir()
	m := &machine{t: t, home: home}
	m.st = New(Config{Home: home, ClaudeDir: filepath.Join(home, ".claude"), CodexDir: filepath.Join(home, ".codex"), Getenv: func(string) string { return "" }})
	return m
}

func (m *machine) write(rel, text string) string {
	m.t.Helper()
	path := filepath.Join(m.home, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		m.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		m.t.Fatal(err)
	}
	return path
}

func (m *machine) read(rel string) string {
	m.t.Helper()
	raw, err := os.ReadFile(filepath.Join(m.home, rel))
	if err != nil {
		m.t.Fatal(err)
	}
	return string(raw)
}

func (m *machine) get(p wire.Provider) wire.AgentSettings {
	m.t.Helper()
	got, err := m.st.AgentSettings(context.Background(), p)
	if err != nil {
		m.t.Fatal(err)
	}
	if err := got.Validate(); err != nil {
		m.t.Fatalf("out of contract: %v", err)
	}
	return got
}

func (m *machine) patch(p wire.Provider, rev string, patch wire.AgentSettingsPatch) (wire.AgentSettings, error) {
	m.t.Helper()
	return m.st.PatchAgentSettings(context.Background(), p, rev, patch)
}

func value(s wire.AgentSettings, key wire.AgentOptionKey) any {
	for _, o := range s.Options {
		if o.Key == key {
			return o.Value
		}
	}
	return "missing"
}

const claudeFile = `{
  "$schema": "https://json.schemastore.org/claude-code-settings.json",
  "env": {
    "BASH_DEFAULT_TIMEOUT_MS": "120000",
    "GITLAB_TOKEN": "glpat-abcdefgh12345678"
  },
  "includeCoAuthoredBy": false,
  "permissions": {
    "allow": [
      "Bash(npm run *)"
    ],
    "defaultMode": "bypassPermissions"
  },
  "model": "opus",
  "hooks": {
    "Stop": []
  },
  "futureKey": {"nested": [1, 2]}
}
`

func TestClaudeReadsWhatTheFileHolds(t *testing.T) {
	m := newMachine(t)
	m.write(".claude/settings.json", claudeFile)
	m.write(".claude.json", `{"oauthAccount":{"emailAddress":"person@example.com"},"mcpServers":{"angular-cli":{"command":"/usr/bin/npx","args":["-y","secret-arg"]},"tracker":{"type":"http","url":"https://tracker.example.com/mcp?key=secret"}}}`)
	m.write(".claude/.credentials.json", `{"claudeAiOauth":{"accessToken":"sk-ant-oat-secret","subscriptionType":"max"}}`)
	got := m.get(wire.ProviderClaude)
	if got.Path != "~/.claude/settings.json" {
		t.Errorf("path %q", got.Path)
	}
	if value(got, wire.OptModel) != "opus" || value(got, wire.OptCoauthor) != false || value(got, wire.OptPermissionMode) != "bypassPermissions" || value(got, wire.OptHooks) != nil || value(got, wire.OptEffort) != nil {
		t.Errorf("options %+v", got.Options)
	}
	if len(got.Rules.Allow) != 1 || len(got.Rules.Deny) != 0 {
		t.Errorf("rules %+v", got.Rules)
	}
	if len(got.Env) != 2 || got.Env[0].Value != "120000" || got.Env[1].Value != "••••5678" || !got.Env[1].Secret {
		t.Errorf("env %+v", got.Env)
	}
	if !got.Account.SignedIn || *got.Account.Method != wire.AccountSubscription || *got.Account.Plan != "Max" || *got.Account.Who != "person@example.com" {
		t.Errorf("account %+v", got.Account)
	}
	if len(got.Mcp) != 2 || got.Mcp[0].Target != "npx" || got.Mcp[1].Target != "tracker.example.com" || got.Mcp[1].Kind != wire.McpHTTP {
		t.Errorf("mcp %+v", got.Mcp)
	}
	whole := ""
	for _, e := range got.Env {
		whole += e.Value
	}
	for _, mcp := range got.Mcp {
		whole += mcp.Target
	}
	for _, secret := range []string{"glpat", "sk-ant", "secret-arg", "key=secret"} {
		if strings.Contains(whole, secret) {
			t.Errorf("%q left the machine", secret)
		}
	}
}

func TestClaudeWriteChangesOnlyWhatIsNamed(t *testing.T) {
	m := newMachine(t)
	m.write(".claude/settings.json", claudeFile)
	before := m.get(wire.ProviderClaude)
	got, err := m.patch(wire.ProviderClaude, before.Rev, wire.AgentSettingsPatch{
		Options:  []wire.AgentOption{{Key: wire.OptModel, Value: "sonnet"}, {Key: wire.OptEffort, Value: "high"}, {Key: wire.OptHooks, Value: false}, {Key: wire.OptSandbox, Value: true}, {Key: wire.OptCoauthor, Value: nil}, {Key: wire.OptBaseURL, Value: "https://llm.example.com"}},
		EnvSet:   []wire.AgentEnvSet{{Key: "NEW_VAR", Value: "1"}},
		EnvUnset: []string{"BASH_DEFAULT_TIMEOUT_MS"},
		Mcp:      &wire.AgentMcpToggle{Name: "tracker", Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rev == before.Rev || value(got, wire.OptModel) != "sonnet" || value(got, wire.OptHooks) != false || value(got, wire.OptSandbox) != true || value(got, wire.OptCoauthor) != nil || value(got, wire.OptBaseURL) != "https://llm.example.com" {
		t.Errorf("after %+v", got.Options)
	}
	want := `{
  "$schema": "https://json.schemastore.org/claude-code-settings.json",
  "env": {
    "GITLAB_TOKEN": "glpat-abcdefgh12345678",
    "ANTHROPIC_BASE_URL": "https://llm.example.com",
    "NEW_VAR": "1"
  },
  "permissions": {
    "allow": [
      "Bash(npm run *)"
    ],
    "defaultMode": "bypassPermissions",
    "deny": [
      "mcp__tracker"
    ]
  },
  "model": "sonnet",
  "hooks": {
    "Stop": []
  },
  "futureKey": {
    "nested": [
      1,
      2
    ]
  },
  "effortLevel": "high",
  "disableAllHooks": true,
  "sandbox": {
    "enabled": true
  }
}
`
	if text := m.read(".claude/settings.json"); text != want {
		t.Errorf("the file:\n%s\nwant:\n%s", text, want)
	}
	// The address is its own option: it is not in the list of variables.
	for _, e := range got.Env {
		if e.Key == "ANTHROPIC_BASE_URL" {
			t.Errorf("the address of the requests is listed twice")
		}
	}
	// Unsetting takes the emptied parents with it.
	back, err := m.patch(wire.ProviderClaude, got.Rev, wire.AgentSettingsPatch{Options: []wire.AgentOption{{Key: wire.OptSandbox, Value: nil}, {Key: wire.OptHooks, Value: true}, {Key: wire.OptBaseURL, Value: nil}}, Mcp: &wire.AgentMcpToggle{Name: "tracker", Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	if text := m.read(".claude/settings.json"); strings.Contains(text, "sandbox") || strings.Contains(text, "disableAllHooks") || strings.Contains(text, "deny") || strings.Contains(text, "ANTHROPIC_BASE_URL") {
		t.Errorf("leftovers:\n%s", text)
	}
	if value(back, wire.OptHooks) != nil {
		t.Errorf("hooks %v", value(back, wire.OptHooks))
	}
}

func TestClaudeWriteIsRefusedWhenTheFileMovedOrDoesNotRead(t *testing.T) {
	m := newMachine(t)
	path := m.write(".claude/settings.json", claudeFile)
	rev := m.get(wire.ProviderClaude).Rev
	if err := os.WriteFile(path, []byte(strings.Replace(claudeFile, `"opus"`, `"haiku"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	patch := wire.AgentSettingsPatch{Options: []wire.AgentOption{{Key: wire.OptModel, Value: "sonnet"}}}
	if _, err := m.patch(wire.ProviderClaude, rev, patch); !errors.Is(err, wire.ErrAgentSettingsConflict) {
		t.Fatalf("a moved file: %v", err)
	}
	broken := `{"model": "opus",`
	m.write(".claude/settings.json", broken)
	if _, err := m.st.AgentSettings(context.Background(), wire.ProviderClaude); err == nil {
		t.Fatal("a file that does not read is read")
	}
	if _, err := m.patch(wire.ProviderClaude, revOf([]byte(broken)), patch); !errors.Is(err, wire.ErrAgentSettingsRefused) {
		t.Fatalf("a file that does not read: %v", err)
	}
	if m.read(".claude/settings.json") != broken {
		t.Fatal("a file that does not read was written over")
	}
}

func TestClaudeWithoutAFileStartsOne(t *testing.T) {
	m := newMachine(t)
	got := m.get(wire.ProviderClaude)
	if got.Account.SignedIn || len(got.Env) != 0 || value(got, wire.OptModel) != nil {
		t.Errorf("an empty machine: %+v", got)
	}
	if _, err := m.patch(wire.ProviderClaude, got.Rev, wire.AgentSettingsPatch{Options: []wire.AgentOption{{Key: wire.OptThinking, Value: true}}}); err != nil {
		t.Fatal(err)
	}
	if text := m.read(".claude/settings.json"); text != "{\n  \"alwaysThinkingEnabled\": true\n}\n" {
		t.Errorf("a new file: %q", text)
	}
	if info, _ := os.Stat(filepath.Join(m.home, ".claude/settings.json")); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode())
	}
}

func TestAKeyInTheEnvironmentIsASignIn(t *testing.T) {
	m := newMachine(t)
	m.write(".claude/settings.json", `{"env":{"ANTHROPIC_API_KEY":"sk-ant-api03-secret"}}`)
	got := m.get(wire.ProviderClaude)
	if !got.Account.SignedIn || *got.Account.Method != wire.AccountKey || got.Account.Plan != nil || got.Account.Who != nil {
		t.Errorf("account %+v", got.Account)
	}
}
