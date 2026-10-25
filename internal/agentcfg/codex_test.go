package agentcfg

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

const codexFile = `# my codex
approval_policy = "never"   # do not ask
sandbox_mode = "danger-full-access"
model = "gpt-5.6-sol"
model_reasoning_effort = "high"
service_tier = "default"
notice = """
[not-a-table]
model = "inside a string"
"""

[shell_environment_policy]
inherit = "core"
set = [
  "A=1",
  "B=2",
]

[projects."/home/coder/work"]
trust_level = "trusted"

[mcp_servers.docs]
url = "https://docs.example.com/mcp"

[mcp_servers.local]
command = "/usr/bin/node"
args = ["server.js", "--token=secret"]
enabled = false
`

func TestCodexReads(t *testing.T) {
	m := newMachine(t)
	m.write(".codex/config.toml", codexFile)
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"person@example.com","https://api.openai.com/auth":{"chatgpt_plan_type":"plus"}}`))
	m.write(".codex/auth.json", `{"auth_mode":"chatgpt","tokens":{"id_token":"x.`+payload+`.y","access_token":"secret"}}`)
	got := m.get(wire.ProviderCodex)
	if got.Path != "~/.codex/config.toml" || got.Rules != nil || len(got.Env) != 0 {
		t.Errorf("shape %+v", got)
	}
	if value(got, wire.OptModel) != "gpt-5.6-sol" || value(got, wire.OptApproval) != "never" || value(got, wire.OptFast) != false || value(got, wire.OptHistory) != nil || value(got, wire.OptSandboxMode) != "danger-full-access" {
		t.Errorf("options %+v", got.Options)
	}
	if !got.Account.SignedIn || *got.Account.Method != wire.AccountSubscription || *got.Account.Plan != "Plus" || *got.Account.Who != "person@example.com" {
		t.Errorf("account %+v", got.Account)
	}
	if len(got.Mcp) != 2 || got.Mcp[0].Name != "docs" || got.Mcp[0].Target != "docs.example.com" || !got.Mcp[0].Enabled || got.Mcp[1].Target != "node" || got.Mcp[1].Enabled {
		t.Errorf("mcp %+v", got.Mcp)
	}
}

func TestCodexWriteKeepsTheRestOfTheFile(t *testing.T) {
	m := newMachine(t)
	m.write(".codex/config.toml", codexFile)
	before := m.get(wire.ProviderCodex)
	got, err := m.patch(wire.ProviderCodex, before.Rev, wire.AgentSettingsPatch{
		Options: []wire.AgentOption{
			{Key: wire.OptModel, Value: "gpt-5.5"}, {Key: wire.OptApproval, Value: "on-request"}, {Key: wire.OptEffort, Value: nil},
			{Key: wire.OptFast, Value: true}, {Key: wire.OptNetwork, Value: true}, {Key: wire.OptHistory, Value: false}, {Key: wire.OptSearch, Value: "live"},
		},
		Mcp: &wire.AgentMcpToggle{Name: "docs", Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if value(got, wire.OptModel) != "gpt-5.5" || value(got, wire.OptEffort) != nil || value(got, wire.OptFast) != true || value(got, wire.OptNetwork) != true || value(got, wire.OptHistory) != false || got.Mcp[0].Enabled {
		t.Errorf("after %+v %+v", got.Options, got.Mcp)
	}
	text := m.read(".codex/config.toml")
	for _, keep := range []string{"# my codex", `approval_policy = "on-request"   # do not ask`, "[not-a-table]", `model = "inside a string"`, `set = [` + "\n" + `  "A=1",`, `[projects."/home/coder/work"]`, `args = ["server.js", "--token=secret"]`, "[mcp_servers.docs]\nurl = \"https://docs.example.com/mcp\"\nenabled = false"} {
		if !strings.Contains(text, keep) {
			t.Errorf("lost %q in:\n%s", keep, text)
		}
	}
	if strings.Contains(text, "model_reasoning_effort") || !strings.Contains(text, "[sandbox_workspace_write]\nnetwork_access = true") || !strings.Contains(text, "[history]\npersistence = \"none\"") {
		t.Errorf("changes:\n%s", text)
	}
	back, err := m.patch(wire.ProviderCodex, got.Rev, wire.AgentSettingsPatch{Mcp: &wire.AgentMcpToggle{Name: "local", Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	if !back.Mcp[1].Enabled || strings.Count(m.read(".codex/config.toml"), "enabled = false") != 1 {
		t.Errorf("mcp %+v", back.Mcp)
	}
}

func TestCodexWithoutAFile(t *testing.T) {
	m := newMachine(t)
	got := m.get(wire.ProviderCodex)
	if _, err := m.patch(wire.ProviderCodex, got.Rev, wire.AgentSettingsPatch{Options: []wire.AgentOption{{Key: wire.OptModel, Value: "gpt-5.5"}, {Key: wire.OptNetwork, Value: false}}}); err != nil {
		t.Fatal(err)
	}
	if text := m.read(".codex/config.toml"); text != "model = \"gpt-5.5\"\n\n[sandbox_workspace_write]\nnetwork_access = false\n" {
		t.Errorf("a new file: %q", text)
	}
}

func TestCodexRefusesWhatItCannotChangeSafely(t *testing.T) {
	m := newMachine(t)
	m.write(".codex/config.toml", "model = \"a\"\nmodel = \"b\"\n")
	if _, err := m.st.AgentSettings(context.Background(), wire.ProviderCodex); err == nil {
		t.Fatal("a file with a key twice is read")
	}
	m.write(".codex/config.toml", "sandbox_workspace_write.network_access = false\n")
	rev := m.get(wire.ProviderCodex).Rev
	if _, err := m.patch(wire.ProviderCodex, rev, wire.AgentSettingsPatch{Options: []wire.AgentOption{{Key: wire.OptNetwork, Value: true}}}); err == nil {
		t.Fatal("a dotted key was written over")
	}
	if m.read(".codex/config.toml") != "sandbox_workspace_write.network_access = false\n" {
		t.Fatal("the file was damaged")
	}
	if _, err := m.patch(wire.ProviderCodex, rev, wire.AgentSettingsPatch{Mcp: &wire.AgentMcpToggle{Name: "nothing", Enabled: false}}); err == nil {
		t.Fatal("a server that is not there was turned off")
	}
}

func TestTOMLEditor(t *testing.T) {
	d := parseTOMLText("a = 1\n\n[t]\nb = [\n  1,\n  2,\n] # tail\nc = 'x'\n")
	d.set([]string{"t"}, "b", "3")
	d.set([]string{"t"}, "d", "true")
	d.remove([]string{"t"}, "c")
	d.set(nil, "z", `"q"`)
	if got, want := d.String(), "a = 1\nz = \"q\"\n\n[t]\nb = 3\nd = true\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
