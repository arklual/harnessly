package codex

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// HRN-945: Codex starts in the permission mode and the service tier the chat chose; without them its config stands.
func TestCodexStartsInItsPermissionAndFastModes(t *testing.T) {
	c := LaunchConfig{Hooks: &HookConfig{Script: "/data/codex-hook.sh", UserConfig: filepath.Join(t.TempDir(), "none.toml")}, MCP: testMCP,
		SkipPermissions: func() bool { return true }}
	args := func(l *agents.Launch) string {
		t.Helper()
		got, _, err := c.RuntimeArgs("", "", l)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(got, " ")
	}
	cases := map[wire.PermissionMode]string{
		wire.PermissionBypass: "--yolo -c service_tier=\"default\"",
		wire.PermissionAsk:    `-c sandbox_mode="workspace-write" -c approval_policy="untrusted" -c service_tier="default"`,
		wire.PermissionPlan:   `-c sandbox_mode="read-only" -c approval_policy="on-request" -c service_tier="default"`,
	}
	for mode, want := range cases {
		line := args(&agents.Launch{Permission: mode})
		if !strings.Contains(line, want) || mode != wire.PermissionBypass && strings.Contains(line, "--yolo") {
			t.Fatalf("%s: %s", mode, line)
		}
	}
	if line := args(&agents.Launch{Fast: true}); !strings.Contains(line, `--yolo -c service_tier="fast"`) {
		t.Fatalf("fast: %s", line)
	}
	if got, _, _ := c.RuntimeArgs("", "", nil); !slices.Contains(got, "--yolo") || slices.ContainsFunc(got, func(a string) bool { return strings.Contains(a, "service_tier") }) {
		t.Fatalf("no mode: %q", got)
	}
	// In a container Codex's sandbox cannot start: plan and ask ask before every command instead.
	c.ContainerSandbox, c.SkipPermissions = true, nil
	if line := args(&agents.Launch{Permission: wire.PermissionPlan}); !strings.Contains(line, `-c sandbox_mode="danger-full-access" -c approval_policy="untrusted"`) {
		t.Fatalf("container: %s", line)
	}
}
