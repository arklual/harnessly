package claude

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/preflight"
)

// statusTimeout bounds the probe: the CLI may reach for the network to refresh a token.
const statusTimeout = 25 * time.Second

// OverrideVars are the variables that beat Claude Code's subscription login, in the order it takes them: a cloud
// provider, an auth token, an API key, a key helper's token (docs: the spec's «Приоритет окружения»).
var OverrideVars = []string{"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY",
	"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY"}

// statusScript prints, in labelled parts: the version, which override variables are set (names only), the output and
// exit code of `claude auth status`, and — when something overrides — the same status with the overrides unset, to see
// whether a subscription login is hiding behind them.
func statusScript(envPrefix string) string {
	names := strings.Join(OverrideVars, " ")
	unset := ""
	for _, v := range OverrideVars {
		unset += " -u " + v
	}
	return envPrefix + agents.FindBinary(Binary) + `if [ -z "$bin" ]; then echo "HL-NOBIN"; exit 0; fi
echo "HL-VERSION"; "$bin" --version 2>/dev/null | head -n 1
echo "HL-ENV"; for v in ` + names + `; do eval "val=\${$v-}"; if [ -n "$val" ]; then echo "$v"; fi; done
echo "HL-STATUS"; "$bin" auth status 2>/dev/null; echo "HL-EXIT $?"
if [ -n "$(for v in ` + names + `; do eval "val=\${$v-}"; [ -n "$val" ] && echo x; done)" ]; then
echo "HL-STATUS2"; env` + unset + ` "$bin" auth status 2>/dev/null; echo "HL-EXIT2 $?"
fi
`
}

type authJSON struct {
	LoggedIn    *bool  `json:"loggedIn"`
	AuthMethod  string `json:"authMethod"`
	APIProvider string `json:"apiProvider"`
}

func parseAuth(raw string) (authJSON, bool) {
	var a authJSON
	if json.Unmarshal([]byte(raw), &a) != nil || a.LoggedIn == nil {
		return a, false
	}
	return a, true
}

// authMode says how the CLI is signed in from `claude auth status`.
func authMode(a authJSON) string {
	if p := strings.ToLower(a.APIProvider); p != "" && p != "firstparty" {
		return hubproto.AuthCloud
	}
	m := strings.ToLower(a.AuthMethod)
	switch {
	case m == "third_party" || strings.Contains(m, "bedrock") || strings.Contains(m, "vertex") || strings.Contains(m, "foundry"):
		return hubproto.AuthCloud
	case strings.Contains(m, "claude.ai") || strings.HasPrefix(m, "oauth"):
		return hubproto.AuthSubscription
	case strings.Contains(m, "key") || strings.Contains(m, "console") || strings.Contains(m, "token"):
		return hubproto.AuthAPIKey
	}
	return hubproto.AuthSubscription
}

// Status is how Claude Code stands on the machine: whether it is there, its version, how it is signed in, and whether
// a variable hides a subscription login. It runs `claude auth status` through the login shell with the env files
// sourced, the way a tab would start. envDir is the agent's env folder ("" for none).
func Status(ctx context.Context, sh agents.LoginRunner, envDir string) hubproto.AgentStatus {
	st := hubproto.AgentStatus{Provider: hubproto.ProviderClaude, Login: hubproto.LoginUnknown}
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	res, err := sh.RunLogin(ctx, statusScript(agents.EnvSourcePrefix(envDir, agents.Claude)))
	if err != nil {
		return st
	}
	parts := agents.Sections(res.Stdout)
	if _, none := parts["NOBIN"]; none {
		st.Login = hubproto.LoggedOut
		return st
	}
	st.Installed = true
	if v := preflight.Version(parts["VERSION"]); v != nil {
		st.Version = v
	}
	status, ok := parseAuth(parts["STATUS"])
	exit, _ := strconv.Atoi(parts["EXIT"])
	switch {
	case ok && *status.LoggedIn:
		st.Login = hubproto.LoggedIn
	case ok:
		st.Login = hubproto.LoggedOut
	case parts["STATUS"] == "" && parts["EXIT"] == "1":
		st.Login = hubproto.LoggedOut
	case exit == 0 && parts["STATUS"] != "":
		st.Login = hubproto.LoggedIn
	}
	if st.Login != hubproto.LoggedIn {
		return st
	}
	mode := authMode(status)
	st.AuthMode = &mode
	if mode != hubproto.AuthSubscription {
		// Something beats the subscription. Is there a subscription login behind it?
		if hidden, ok := parseAuth(parts["STATUS2"]); ok && *hidden.LoggedIn && authMode(hidden) == hubproto.AuthSubscription {
			if name := firstSet(parts["ENV"]); name != "" {
				st.OverriddenBy = &name
			}
		}
	}
	return st
}

// firstSet is the first of OverrideVars named in the probe's ENV part.
func firstSet(env string) string {
	set := map[string]bool{}
	for _, line := range strings.Fields(env) {
		set[line] = true
	}
	for _, v := range OverrideVars {
		if set[v] {
			return v
		}
	}
	return ""
}
