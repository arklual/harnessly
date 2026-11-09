package agentlogin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// CLIsFile is the name of the file in the agent's data folder that describes the machine's other CLIs to sign in (#58).
const CLIsFile = "cli-logins.json"

// CLI is one command-line tool the machine's owner signs in from the hub's page, the way the agents are signed in by
// console: a terminal of the runtime runs Login, the person follows it, and Status says when it worked.
//
// The file is a JSON array of these. An organization's extension pack writes it; the hub learns of the CLIs only from
// what the agent reports, so no core change and no hub setting is needed.
type CLI struct {
	// ID names the CLI in the hub's routes: lower-case letters, digits and dashes, not an agent's name.
	ID string `json:"id"`
	// Label is what the page shows.
	Label string `json:"label"`
	// Login is the sign-in command, as arguments; it runs as it is, nothing from the browser is added to it.
	Login []string `json:"login"`
	// Status is a command that exits 0 when this account is signed in. Its output is not read and not kept.
	Status []string `json:"status"`
	// Timeout is how long a sign-in may take, in seconds: 900 when 0, at most 3600.
	Timeout int `json:"timeout,omitempty"`
}

var cliID = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

const (
	maxCLIs       = 16
	maxCLILabel   = 60
	maxCLIArgs    = 32
	maxCLIArg     = 1024
	maxCLITimeout = 3600
	cliStatusWait = 20 * time.Second
)

// TTL is how long a sign-in of this CLI may take.
func (c CLI) TTL(def time.Duration) time.Duration {
	if c.Timeout <= 0 {
		return def
	}
	return time.Duration(min(c.Timeout, maxCLITimeout)) * time.Second
}

// Reserved says the id belongs to an agent, which has its own sign-in.
func Reserved(id string) bool {
	return id == hubproto.ProviderClaude || id == hubproto.ProviderCodex || id == hubproto.ProviderGemini
}

func (c CLI) check() error {
	if !cliID.MatchString(c.ID) || Reserved(c.ID) {
		return fmt.Errorf("id %q: lower-case letters, digits and dashes, 2 to 32, not an agent's name", c.ID)
	}
	if strings.TrimSpace(c.Label) == "" || utf8.RuneCountInString(c.Label) > maxCLILabel {
		return fmt.Errorf("%s: label must be 1 to %d characters", c.ID, maxCLILabel)
	}
	for name, argv := range map[string][]string{"login": c.Login, "status": c.Status} {
		if len(argv) == 0 || len(argv) > maxCLIArgs || strings.TrimSpace(argv[0]) == "" {
			return fmt.Errorf("%s: %s must be a command with up to %d arguments", c.ID, name, maxCLIArgs)
		}
		for _, a := range argv {
			if len(a) > maxCLIArg || strings.ContainsFunc(a, func(r rune) bool { return r == 0 || r == '\n' || r == '\r' }) {
				return fmt.Errorf("%s: an argument of %s is too long or has a line break", c.ID, name)
			}
		}
	}
	if c.Timeout < 0 {
		return fmt.Errorf("%s: timeout must not be negative", c.ID)
	}
	return nil
}

// LoadCLIs reads the file: the entries that are right, and what is wrong with the others. A missing file is no CLIs.
// It is read on each use, so a pack that changes it needs no restart of the agent.
func LoadCLIs(path string) ([]CLI, []error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []error{err}
	}
	var list []CLI
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, []error{fmt.Errorf("%s: %w", CLIsFile, err)}
	}
	var out []CLI
	var bad []error
	seen := map[string]bool{}
	for _, c := range list {
		c.Label = strings.TrimSpace(c.Label)
		if err := c.check(); err != nil {
			bad = append(bad, err)
			continue
		}
		if seen[c.ID] {
			bad = append(bad, fmt.Errorf("%s: described twice", c.ID))
			continue
		}
		if len(out) == maxCLIs {
			bad = append(bad, fmt.Errorf("%s: more than %d CLIs", c.ID, maxCLIs))
			continue
		}
		seen[c.ID] = true
		out = append(out, c)
	}
	return out, bad
}

// FindCLI is the CLI the file describes under id.
func FindCLI(path, id string) (CLI, bool) {
	list, _ := LoadCLIs(path)
	for _, c := range list {
		if c.ID == id {
			return c, true
		}
	}
	return CLI{}, false
}

func quoteArgs(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = agents.ShellQuote(a)
	}
	return strings.Join(q, " ")
}

// cliEnv is what a CLI's commands run with: no display, and a "browser" that prints the address it is given, so the
// address shows in the console instead of a browser opening on the machine, where nobody is.
const cliEnv = "BROWSER=echo DISPLAY= WAYLAND_DISPLAY= "

// LoginLine is the line the console runs for the CLI's sign-in, with the machine's shared env file (a proxy).
func (c CLI) LoginLine(envDir string) string {
	return agents.ForAnyShell(agents.EnvSourcePrefix(envDir, c.ID) + cliEnv + quoteArgs(c.Login))
}

// CLIStatus asks the CLI whether this account is signed in. Neither its output nor its errors are kept: a status
// command may print a token.
func CLIStatus(ctx context.Context, sh agents.LoginRunner, envDir string, c CLI) hubproto.AgentStatus {
	st := hubproto.AgentStatus{Provider: c.ID, Kind: hubproto.KindCLI, Label: c.Label, Login: hubproto.LoginUnknown}
	ctx, cancel := context.WithTimeout(ctx, cliStatusWait)
	defer cancel()
	script := agents.EnvSourcePrefix(envDir, c.ID) + "command -v " + agents.ShellQuote(c.Status[0]) +
		" >/dev/null 2>&1 || exit 127\n" + "exec env " + cliEnv + quoteArgs(c.Status) + " </dev/null >/dev/null 2>&1\n"
	res, err := sh.RunLogin(ctx, script)
	if err != nil {
		return st
	}
	switch res.Exit {
	case 0:
		st.Installed, st.Login = true, hubproto.LoggedIn
	case 127:
		st.Login = hubproto.LoggedOut
	default:
		st.Installed, st.Login = true, hubproto.LoggedOut
	}
	return st
}
