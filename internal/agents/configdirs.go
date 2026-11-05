package agents

import (
	"github.com/ly-solutions/harnessly/internal/agentcfg"
	"github.com/ly-solutions/harnessly/internal/proc"
)

// LiveDirs are the folders the running processes of the CLI named binary were told to keep their state in by the
// variable env (CLAUDE_CONFIG_DIR, CODEX_HOME): what the runtime's or the agent's own environment says need not be
// what the person's shell gave the CLI. Processes of other accounts do not show their environment and are skipped.
func LiveDirs(p proc.Procs, binary, env string) []string {
	var dirs []string
	for _, pid := range p.AllPids() {
		if !MatchArgv.Is(p, pid, binary) {
			continue
		}
		if environ, ok := p.EnvironOf(pid); ok {
			dirs = append(dirs, agentcfg.DirOfEnv(environ, env))
		}
	}
	return agentcfg.Unique(dirs...)
}
