package agents

import (
	"os"
	"path/filepath"
	"strings"
)

// The env files the machine agent keeps for the agent CLIs (docs/hub/machine.md): keys and secrets the person gave
// through the hub, and the proxy. A launch line sources them first, so the CLI sees them and nothing else needs the
// person's shell profile.
const (
	// EnvScopeAll is for every agent: delivered secrets, an all-agents proxy.
	EnvScopeAll = "all"
	// EnvDirName is the folder in the agent's data folder.
	EnvDirName = "agent-env"
)

// EnvFile is the path of a scope's env file in dir: all.env, claude.env, codex.env.
func EnvFile(dir, scope string) string { return filepath.Join(dir, scope+".env") }

// EnvSourcePrefix is what goes before a launch line so the CLI starts with the env files of dir: `set -a; . 'all.env';
// . 'claude.env'; set +a; `, for the files that exist; "" when none does (or dir is empty), so a machine that never
// met the hub starts its agents exactly as before. Later files win, so an agent's own proxy beats the shared one.
func EnvSourcePrefix(dir, provider string) string {
	if dir == "" {
		return ""
	}
	var parts []string
	for _, scope := range []string{EnvScopeAll, provider} {
		file := EnvFile(dir, scope)
		if st, err := os.Stat(file); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
			parts = append(parts, ". "+ShellQuote(file)+";")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "set -a; " + strings.Join(parts, " ") + " set +a; "
}
