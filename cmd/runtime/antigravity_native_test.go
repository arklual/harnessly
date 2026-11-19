package main

import (
	"os"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/antigravity"
	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func TestAntigravityLaunchBinding(t *testing.T) {
	const first = "11111111-2222-4333-8444-555555555555"
	const other = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	var starts nativeAntigravityStarts
	generation, err := starts.begin("tab-test", "")
	if err != nil {
		t.Fatal(err)
	}
	if starts.accept("other-tab", generation, first) || starts.accept("tab-test", "old-generation", first) || !starts.accept("tab-test", generation, first) || starts.accept("tab-test", generation, other) {
		t.Fatal("native root binding did not fail closed")
	}
	next, err := starts.begin("tab-test", first)
	if err != nil || next == generation {
		t.Fatal("resume did not replace generation")
	}
	if starts.accept("tab-test", generation, first) || starts.accept("tab-test", next, other) || !starts.accept("tab-test", next, first) {
		t.Fatal("stale or mismatched resume accepted")
	}
}

func TestAntigravityLaunchScopesAndModes(t *testing.T) {
	a := &coreAgents{antigravity: antigravity.LaunchConfig{Launcher: "/private/agy"}, nativeToken: func(string) (string, error) { return "/private/launch/.credentials.json", nil }}
	choice := &agents.Launch{Conversation: "tab-test", Permission: wire.PermissionAsk}
	t.Setenv("RUNTIME_ANTIGRAVITY_NATIVE", "")
	if _, err := a.antigravityLine("ws", "", choice, nil); err == nil {
		t.Fatal("native launch enabled without setup")
	}
	t.Setenv("RUNTIME_ANTIGRAVITY_NATIVE", "on")
	line, err := a.antigravityLine("ws", "", choice, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"HARNESSLY_ANTIGRAVITY_CONVERSATION=", "HARNESSLY_ANTIGRAVITY_GENERATION=", "HARNESSLY_ANTIGRAVITY_EXPECTED_UUID=''", "HARNESSLY_CLI_TOKEN=''", "HARNESSLY_CLI_TOKEN_FILE=", "trap "} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing native scope/cleanup %q", want)
		}
	}
	if strings.Contains(line, "--mode") || strings.Contains(line, "dangerously") || strings.Contains(line, "--resume") {
		t.Fatal("foreign flags or permission bypass in native launch")
	}
	if got := defaultPermissionOf(wire.ProviderAntigravity, true, "", ""); got != wire.PermissionAsk {
		t.Fatal("native inherited global bypass setting")
	}
}

func TestAntigravityTokenFileIsPrivate(t *testing.T) {
	dir := t.TempDir()
	path, err := nativeAntigravityTokenIssuer(dir)(strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("native token permissions")
	}
	if !files.NewAccess([]string{dir}, nil).Secret(path) {
		t.Fatal("native token is accessible to file viewer")
	}
}
