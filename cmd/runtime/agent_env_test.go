package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
)

func writeEnvFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for scope, body := range files {
		if err := os.WriteFile(agents.EnvFile(dir, scope), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// The CLIs the runtime starts itself get what the machine agent's env files give a launch line.
func TestWithAgentEnv(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HTTPS_PROXY=http://old:1"}
	dir := filepath.Join(t.TempDir(), "agent-env")
	if got := withAgentEnv(base, "", agents.Claude); !slices.Equal(got, base) {
		t.Errorf("no folder: %v", got)
	}
	if got := withAgentEnv(base, dir, agents.Claude); !slices.Equal(got, base) {
		t.Errorf("a folder that is not there: %v", got)
	}
	writeEnvFiles(t, dir, map[string]string{
		agents.EnvScopeAll: "GH_TOKEN='gh'\nHTTPS_PROXY='http://127.0.0.1:3128'\n",
		agents.Claude:      "ANTHROPIC_API_KEY='it'\\''s a key'\nGH_TOKEN='own'\n",
		agents.Codex:       "OPENAI_API_KEY='sk'\n",
	})
	want := []string{"PATH=/usr/bin", "HTTPS_PROXY=http://old:1", "GH_TOKEN=own", "HTTPS_PROXY=http://127.0.0.1:3128", "ANTHROPIC_API_KEY=it's a key"}
	got := withAgentEnv(base, dir, agents.Claude)
	slices.Sort(want[2:])
	if !slices.Equal(got[:2], want[:2]) || !slices.Equal(got[2:], want[2:]) {
		t.Errorf("claude:\n got %v\nwant %v", got, want)
	}
	if got := withAgentEnv(base, dir, agents.Codex); !slices.Contains(got, "OPENAI_API_KEY=sk") || slices.Contains(got, "ANTHROPIC_API_KEY=it's a key") || !slices.Contains(got, "GH_TOKEN=gh") {
		t.Errorf("codex: %v", got)
	}
	if len(base) != 2 {
		t.Errorf("the base was changed: %v", base)
	}
}

func TestAgentEnvDir(t *testing.T) {
	t.Setenv("AGENT_ENV_DIR", "")
	if got := agentEnvDir("/data"); got != "/data/"+agents.EnvDirName {
		t.Errorf("%q", got)
	}
	t.Setenv("AGENT_ENV_DIR", "/elsewhere/env")
	if got := agentEnvDir("/data"); got != "/elsewhere/env" {
		t.Errorf("%q", got)
	}
}
