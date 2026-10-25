package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A conversation started on ultracode gets the mode from its settings: Claude Code reads one --settings, so it is a
// copy of the hooks settings with the `ultracode` key, and the hooks still run.
func TestALaunchOnUltracodeReadsACopyOfTheHooksSettingsWithTheKey(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t)
	cfg.HooksPath = filepath.Join(dir, "hooks.json")
	if err := os.WriteFile(cfg.HooksPath, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/x"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	l := launch("sonnet", "medium")
	l.Ultracode = true
	line, err := cfg.Flags("", l, nil)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(dir, "hooks.ultracode.json")
	if !strings.Contains(line, "--settings "+copyPath+" ") || !strings.Contains(line, "--effort medium") || strings.Contains(line, "--effort ultracode") {
		t.Fatalf("%s", line)
	}
	raw, err := os.ReadFile(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil || got["ultracode"] != true || got["hooks"] == nil {
		t.Fatalf("%s %v", raw, err)
	}
	// Without the mode: the hooks settings themselves.
	plain, err := cfg.Flags("", launch("sonnet", "medium"), nil)
	if err != nil || !strings.Contains(plain, "--settings "+cfg.HooksPath+" ") {
		t.Fatalf("%s %v", plain, err)
	}
	// Hooks settings that cannot be read stop the launch: no session starts without its hooks.
	cfg.HooksPath = filepath.Join(dir, "missing.json")
	if _, err := cfg.Flags("", l, nil); err == nil {
		t.Fatal("launched without hooks")
	}
}
