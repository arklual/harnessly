package codex

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/preflight"
)

const statusTimeout = 25 * time.Second

func statusScript(envPrefix string) string {
	return envPrefix + agents.FindBinary(Binary) + `if [ -z "$bin" ]; then echo "HL-NOBIN"; exit 0; fi
echo "HL-VERSION"; "$bin" --version 2>/dev/null | head -n 1
echo "HL-STATUS"; "$bin" login status 2>&1; echo "HL-EXIT $?"
`
}

// Status is how Codex stands on the machine, from `codex login status` (its exit code says whether it is signed in, its
// text how). The same login shell and env files as a tab's launch.
func Status(ctx context.Context, sh agents.LoginRunner, envDir string) hubproto.AgentStatus {
	st := hubproto.AgentStatus{Provider: hubproto.ProviderCodex, Login: hubproto.LoginUnknown}
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	res, err := sh.RunLogin(ctx, statusScript(agents.EnvSourcePrefix(envDir, agents.Codex)))
	if err != nil {
		return st
	}
	parts := agents.Sections(res.Stdout)
	if _, none := parts["NOBIN"]; none {
		st.Login = hubproto.LoggedOut
		return st
	}
	st.Installed = true
	st.Version = preflight.Version(parts["VERSION"])
	exit, err := strconv.Atoi(parts["EXIT"])
	if err != nil {
		return st
	}
	if exit != 0 {
		st.Login = hubproto.LoggedOut
		return st
	}
	st.Login = hubproto.LoggedIn
	mode := LoginMode(parts["STATUS"])
	st.AuthMode = &mode
	return st
}

// LoginMode reads `codex login status`'s text: "Logged in using ChatGPT", "Logged in using an API key - sk-…".
func LoginMode(text string) string {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "api key") || strings.Contains(lower, "api-key") {
		return hubproto.AuthAPIKey
	}
	return hubproto.AuthSubscription
}
