package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// HRN-945: a launch that names a permission mode starts in it whatever the person's settings or the machine say, and
// never with bypass allowed (Claude's plan mode stops asking then); fast mode goes in a copy of the settings, said
// either way.
func TestALaunchStartsInItsPermissionAndFastModes(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t)
	cfg.HooksPath = filepath.Join(dir, "hooks.json")
	if err := os.WriteFile(cfg.HooksPath, []byte(`{"hooks":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.SkipPermissions = func() bool { return true }
	for mode, flag := range map[wire.PermissionMode]string{
		wire.PermissionPlan: "--permission-mode plan", wire.PermissionAsk: "--permission-mode default",
		wire.PermissionBypass: "--permission-mode bypassPermissions",
	} {
		l := launch("opus", "high")
		l.Permission = mode
		line, err := cfg.Flags("", l, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(line, flag) || strings.Contains(line, "skip-permissions") {
			t.Fatalf("%s: %s", mode, line)
		}
		off := filepath.Join(dir, "hooks.fast-off.json")
		if !strings.Contains(line, "--settings '"+off+"'") {
			t.Fatalf("fast said off: %s", line)
		}
	}
	// Without a mode the machine's default stands, and the settings are the hooks' own.
	plain, err := cfg.Flags("", launch("opus", "high"), nil)
	if err != nil || !strings.Contains(plain, "--dangerously-skip-permissions") || strings.Contains(plain, "--permission-mode") ||
		!strings.Contains(plain, "--settings "+cfg.HooksPath+" ") {
		t.Fatalf("%s %v", plain, err)
	}
	l := launch("opus", "high")
	l.Fast, l.Permission = true, wire.PermissionAsk
	line, err := cfg.Flags("", l, nil)
	if err != nil {
		t.Fatal(err)
	}
	on := filepath.Join(dir, "hooks.fast-on.json")
	if !strings.Contains(line, "--settings '"+on+"'") {
		t.Fatalf("%s", line)
	}
	raw, _ := os.ReadFile(on)
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil || got["fastMode"] != true || got["hooks"] == nil {
		t.Fatalf("%s %v", raw, err)
	}
	l.Permission = "auto"
	if _, err := cfg.Flags("", l, nil); err == nil {
		t.Fatal("a mode the picker has not got went on the line")
	}
}
