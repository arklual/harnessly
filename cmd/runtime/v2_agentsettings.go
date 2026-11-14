package main

import (
	"os"
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/agentcfg"
	"github.com/ly-solutions/harnessly/internal/preflight"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The Settings screen (/api/v2/agent-settings/<provider>) reads and writes the files the agent CLIs keep on this
// machine, in the folders the runtime already looks in for them.
func newV2AgentSettings(d deps) *agentcfg.Store { return agentFiles(d.Home) }

// agentFiles is the store of the agent CLIs' files under a home.
func agentFiles(home string) *agentcfg.Store {
	claudeDir := env("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	// With CLAUDE_CONFIG_DIR Claude Code keeps its account file inside that folder, otherwise beside it in the home.
	claudeJSON := filepath.Join(home, ".claude.json")
	if os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		claudeJSON = filepath.Join(claudeDir, ".claude.json")
	}
	return agentcfg.New(agentcfg.Config{
		Home:       home,
		ClaudeDir:  claudeDir,
		ClaudeJSON: claudeJSON,
		CodexDir:   env("CODEX_HOME", filepath.Join(home, ".codex")),
		AuthDir:    preflight.AuthDirOf(os.Getenv("RUNTIME_SIDE_CHAT_CONFIG")),
	})
}

// firstRunBeforeAgent answers the CLI's first-run screens (theme, onboarding, folder trust, the bypass warning) in its
// files before a conversation's agent starts: nobody is at the terminal of a platform chat to press Enter.
func firstRunBeforeAgent(home string) func(provider, cwd string) {
	files := agentFiles(home)
	return func(provider, cwd string) {
		if err := files.FirstRun(wire.Provider(provider), cwd); err != nil {
			coreLog("warn", "agent first run", "provider", provider, "cwd", cwd, "err", err)
		}
	}
}
