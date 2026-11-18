package agenteval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Codex's own helper agent counts as a subagent, as Claude's Agent tool does.
func TestCodexSpawnAgentIsAnAgentCall(t *testing.T) {
	if name, _ := codexTool("spawn_agent", `{"task":"x"}`); name != "Agent" {
		t.Fatalf("spawn_agent → %q", name)
	}
	if name, _ := codexTool("apply_patch", `{}`); name != "Edit" {
		t.Fatalf("apply_patch → %q", name)
	}
}

// The person's Codex chat is the session that started first, not the newest: a delegated chat writes later.
func TestTheFirstCodexRolloutIsThePersons(t *testing.T) {
	dir := t.TempDir()
	day := filepath.Join(dir, "2026", "11", "17")
	if err := os.MkdirAll(day, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"rollout-2026-11-17T20-42-05-a.jsonl", "rollout-2026-11-17T20-43-10-b.jsonl"} {
		if err := os.WriteFile(filepath.Join(day, n), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := filepath.Base(firstRollout(dir)); got != "rollout-2026-11-17T20-42-05-a.jsonl" {
		t.Fatalf("first rollout: %s", got)
	}
}

// A code-mode command written as a template literal is still a Bash call with its command.
func TestCodeModeTemplateCommandIsRead(t *testing.T) {
	script := "const out = await Promise.all(prompts.map(async ([name,prompt]) => {\n" +
		"  const r = await tools.exec_command({cmd:`harnessly tab create -t codex-cli -n ${name} --notify --prompt ${JSON.stringify(prompt)}`,yield_time_ms:10000});\n" +
		"  return r;\n}));"
	calls := codeModeCalls(script)
	if len(calls) != 1 {
		t.Fatalf("calls: %+v", calls)
	}
	name, args := codexTool(calls[0].name, calls[0].args)
	var x struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(args, &x)
	if name != "Bash" || !strings.HasPrefix(x.Command, "harnessly tab create -t codex-cli") || !strings.Contains(x.Command, "--prompt") {
		t.Fatalf("%s %s", name, args)
	}
}
