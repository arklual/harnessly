package agents

import (
	"context"
	"strings"

	"github.com/ly-solutions/harnessly/internal/preflight"
)

// LoginRunner runs an sh script the way a tab's login shell would see the machine: the PATH and variables of the
// account's profile. preflight.Checker is one.
type LoginRunner interface {
	RunLogin(ctx context.Context, script string, extraEnv ...string) (preflight.LoginResult, error)
}

// Sections splits what a probe script printed into the parts it labelled: a line that starts with "HL-" begins a part,
// named by that word (without the "HL-"), whose body is what follows up to the next label, trimmed. Text before the
// first label is dropped.
func Sections(out string) map[string]string {
	parts := map[string]string{}
	name := ""
	var body []string
	flush := func() {
		if name != "" {
			parts[name] = strings.TrimSpace(strings.Join(body, "\n"))
		}
		body = nil
	}
	for _, line := range strings.Split(out, "\n") {
		if label, ok := strings.CutPrefix(line, "HL-"); ok && !strings.ContainsAny(label, "{}\"") {
			flush()
			label, rest, _ := strings.Cut(strings.TrimSpace(label), " ")
			name = label
			if rest != "" {
				body = append(body, rest)
			}
			continue
		}
		body = append(body, line)
	}
	flush()
	return parts
}

// FindBinary is the sh snippet that sets $bin to the agent CLI: on the PATH, else where the installers put it
// (~/.local/bin). Empty when there is none.
func FindBinary(name string) string {
	return `bin=$(command -v ` + name + ` 2>/dev/null)
if [ -z "$bin" ] && [ -x "$HOME/.local/bin/` + name + `" ]; then bin="$HOME/.local/bin/` + name + `"; fi
`
}
