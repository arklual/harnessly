package agentcfg

import (
	"path/filepath"
	"strings"
)

// The variables that move the CLIs' folders away from the home. On Coder workspaces and other managed machines they
// are often set in the person's shell, not in the service that runs the runtime and the machine's agent.
const (
	ClaudeDirEnv = "CLAUDE_CONFIG_DIR"
	CodexDirEnv  = "CODEX_HOME"
)

// ClaudeDirIn is the folder Claude Code keeps its state in when it runs with this environment: $CLAUDE_CONFIG_DIR,
// else <home>/.claude.
func ClaudeDirIn(getenv func(string) string, home string) string {
	if d := absDir(getenv(ClaudeDirEnv)); d != "" {
		return d
	}
	return filepath.Join(home, ".claude")
}

// CodexDirIn is Codex's home when it runs with this environment: $CODEX_HOME, else <home>/.codex.
func CodexDirIn(getenv func(string) string, home string) string {
	if d := absDir(getenv(CodexDirEnv)); d != "" {
		return d
	}
	return filepath.Join(home, ".codex")
}

// EnvOf reads a process's environment (its KEY=value entries, as /proc/<pid>/environ holds them) as os.Getenv does.
func EnvOf(environ []string) func(string) string {
	return func(name string) string {
		prefix := name + "="
		v := ""
		for _, e := range environ {
			if strings.HasPrefix(e, prefix) {
				v = e[len(prefix):]
			}
		}
		return v
	}
}

// DirOfEnv is the folder an environment variable of a running CLI names, "" when it is not set. A relative value
// depends on the folder the CLI was started in and is not followed.
func DirOfEnv(environ []string, name string) string {
	return absDir(EnvOf(environ)(name))
}

func absDir(d string) string {
	d = strings.TrimSpace(d)
	if d == "" || !filepath.IsAbs(d) {
		return ""
	}
	return filepath.Clean(d)
}

// Unique is the folders without repeats and empty ones, in the order given.
func Unique(dirs ...string) []string {
	out := make([]string, 0, len(dirs))
	seen := map[string]bool{}
	for _, d := range dirs {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}
