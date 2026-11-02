package main

import (
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/usagefeed"
	"github.com/ly-solutions/harnessly/internal/usagereport"
)

// usageFeed reads the transcripts of the account's Claude Code and Codex and writes the spend facts the hub asks for.
// It lives here, not in internal/agent: the usage report imports the agent's package.
func usageFeed(d agent.UsageDeps) (agent.UsageFeed, error) {
	claudeDir := d.Getenv("claude", "CLAUDE_CONFIG_DIR")
	if claudeDir == "" {
		claudeDir = filepath.Join(d.Home, ".claude")
	}
	codexDir := d.Getenv("codex", "CODEX_HOME")
	if codexDir == "" {
		codexDir = filepath.Join(d.Home, ".codex")
	}
	col := usagereport.NewCollector(usagereport.Config{
		ClaudeRoot: filepath.Join(claudeDir, "projects"),
		CodexRoot:  filepath.Join(codexDir, "sessions"),
		// The agent's own facts file: the runtime names projects by its own settings and the two must not overwrite
		// each other's reading.
		CacheFile: filepath.Join(d.DataDir, "stats", "usage-agent-v2.json"),
		Projects:  &usagereport.Projects{},
		Timezone:  "UTC",
		Machine:   d.Hostname,
	})
	local := usagefeed.Local{Home: d.Home, Getenv: d.Getenv}
	return usagefeed.New(usagefeed.Config{
		Outbox:    d.Box,
		Source:    col,
		StatePath: usagefeed.StatePath(d.Dir),
		Identity:  local.Identity,
	})
}
