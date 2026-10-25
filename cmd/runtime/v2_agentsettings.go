package main

import (
	"os"
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/agentcfg"
)

// The Settings screen (/api/v2/agent-settings/<provider>) reads and writes the files the agent CLIs keep on this
// machine, in the folders the runtime already looks in for them.
func newV2AgentSettings(d deps) *agentcfg.Store {
	claudeDir := env("CLAUDE_CONFIG_DIR", filepath.Join(d.Home, ".claude"))
	// With CLAUDE_CONFIG_DIR Claude Code keeps its account file inside that folder, otherwise beside it in the home.
	claudeJSON := filepath.Join(d.Home, ".claude.json")
	if os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		claudeJSON = filepath.Join(claudeDir, ".claude.json")
	}
	return agentcfg.New(agentcfg.Config{
		Home:       d.Home,
		ClaudeDir:  claudeDir,
		ClaudeJSON: claudeJSON,
		CodexDir:   env("CODEX_HOME", filepath.Join(d.Home, ".codex")),
	})
}
