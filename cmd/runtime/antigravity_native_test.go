package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/antigravity"
	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func TestAntigravityPreflightUsesLaunchBinary(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		override, missing, local bool
		version, model           string
	}{
		{"custom override", true, false, true, "2.0.0", "custom-model"},
		{"missing override", true, true, true, "", ""},
		{"official local install", false, false, true, "3.0.0", "local-model"},
		{"default PATH", false, false, false, "1.0.0", "path-model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, dir := t.TempDir(), t.TempDir()
			writeCLI := func(binary, version, model string) {
				t.Helper()
				body := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'agy " + version + "'; else printf '" + model + "\\tModel\\n'; fi\n"
				if err := os.WriteFile(binary, []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			writeCLI(filepath.Join(dir, "agy"), "1.0.0", "path-model")
			binary := ""
			if tc.local {
				local := filepath.Join(home, ".local", "bin")
				if err := os.MkdirAll(local, 0o755); err != nil {
					t.Fatal(err)
				}
				binary = filepath.Join(local, "agy")
				writeCLI(binary, "3.0.0", "local-model")
			}
			t.Setenv("RUNTIME_ANTIGRAVITY_BIN", "")
			if tc.override {
				binary = filepath.Join(t.TempDir(), "custom agy")
				if !tc.missing {
					writeCLI(binary, "2.0.0", "custom-model")
				}
				t.Setenv("RUNTIME_ANTIGRAVITY_BIN", binary)
			}
			a := newCoreAgents(coreAgentsConfig{DataDir: t.TempDir(), Home: home})
			if a.antigravity.Launcher != binary {
				t.Fatalf("launch binary: %q, want %q", a.antigravity.Launcher, binary)
			}
			a.checker.Env, a.checker.Shell = []string{"PATH=" + dir}, "/nonexistent/login-shell"
			r := a.runtime().Antigravity
			options := a.antigravityOptions()
			if tc.missing {
				if r.Installed || r.LoggedIn || len(options.Models) != 0 {
					t.Fatalf("missing override fell back: %+v / %+v", r, options)
				}
				return
			}
			if !r.Installed || !r.LoggedIn || r.Version == nil || *r.Version != tc.version || len(options.Models) != 1 || options.Models[0].ID != tc.model {
				t.Fatalf("preflight/catalog binary differs from launch: %+v / %+v", r, options)
			}
		})
	}
}

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
