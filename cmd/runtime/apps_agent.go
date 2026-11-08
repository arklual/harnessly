package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// The agent of a chat with apps calls the runtime with a token of its own launch (gate.CLIToken.IssueAgent), not the
// person's cli-token. The guard hook refuses the person's `harnessly app …` commands in the agent's shell; these routes
// refuse the same changes to a request made with an agent token, so a request sent to the runtime by any other means
// (curl, a script) does not pass where the hook's reading of a command line does not reach. Mirrors selfCommands and
// publishesWide of internal/apps/policy.go.

// agentRefusedAppRoutes are the CLI routes that change what is installed, on, shared or published.
var agentRefusedAppRoutes = [...]string{
	platformAPI + "/apps/hooks-off",
	"/agents" + platformAPI + "/apps/hooks-off",
	"/api/cli/apps/install",
	"/api/cli/apps/remove",
	"/api/cli/apps/enable",
	"/api/cli/apps/connect",
	"/api/cli/apps/promote",
	"/api/cli/apps/rollback",
	"/api/cli/apps/follow",
	"/api/cli/apps/sync",
	"/api/cli/apps/team/set",
	"/api/cli/apps/team/publish",
	"/api/cli/apps/team/install",
	"/api/cli/apps/team/uninstall",
	"/api/cli/apps/share",
	"/api/cli/apps/unshare",
	"/api/cli/apps/public/install",
	"/api/cli/apps/public/publish",
}

// What else the token of a launch may reach. Everything else, it is refused, whatever the route's own check says: the
// runtime runs what it is asked outside the wall, so a terminal (`tabs`, `tmux/send-input`, the socket of a tab), a new
// agent, input into another chat's pane or a peer's mailbox would take the agent out of it. The list is of what an agent's
// CLI, hooks and MCP servers call; a new route is shut until it is named here.
var (
	agentRoutes = [...]string{
		"/api/status/hook", "/api/status/statusline",
		"/api/cli/ok", "/api/cli/api-guide", "/api/cli/open", "/api/cli/panel", "/api/cli/deliver",
		"/api/cli/asks", "/api/cli/apps", "/api/cli/design",
		"/api/cli/notify/send", "/api/cli/notify/bots",
		"/api/cli/secrets", "/api/cli/secrets/ask", "/api/cli/secrets/use", "/api/cli/secrets/file",
		"/api/cli/secrets/code", "/api/cli/secrets/code/wait", "/api/cli/secrets/code/cancel",
		"/api/sites-token",
	}
	agentRoutePrefixes = [...]string{"/api/cli/preview/", "/api/cli/asks/", "/api/cli/apps/", "/api/cli/design/", "/api/sites/"}
	// Reading only: the agent's CLI lists these, and sending or creating through them is what the list above keeps out.
	agentReadRoutes = [...]string{"/api/cli/workspaces", "/api/cli/tabs", "/api/cli/peers"}
)

// errAgentOutsideTheWall is the refusal an agent token gets on any other route.
const errAgentOutsideTheWall = "agent_outside_the_wall"

// agentRouteAllowed says whether a request made with an agent token is to a route it may use. Only the plain form of a
// path counts: one the router would read otherwise (`..`, an escape, a doubled slash) is not on the list.
func agentRouteAllowed(r *http.Request) bool {
	path := strings.TrimPrefix(r.URL.Path, "/agents")
	if strings.Contains(path, "..") || strings.Contains(path, "//") || strings.Contains(path, "%") || (r.URL.RawPath != "" && r.URL.RawPath != r.URL.Path) {
		return false
	}
	if slices.Contains(agentRoutes[:], path) {
		return true
	}
	for _, prefix := range agentRoutePrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if slices.Contains(agentReadRoutes[:], path) {
		return true
	}
	// The person's connections (harnessly connections | issues | issue | pages | page): reads through the runtime,
	// which holds the tokens; nothing of them is changed by a GET.
	if path == "/api/cli/connections" || strings.HasPrefix(path, "/api/cli/connections/") {
		return true
	}
	// One tab's status, not what is on its screen (.../result) and not its browser.
	rest, ok := strings.CutPrefix(path, "/api/cli/tabs/")
	return ok && rest != "" && !strings.Contains(rest, "/")
}

// confineAgentToken stands in front of every route.
func confineAgentToken(cli *gate.CLIToken, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, agent := cli.AgentOf(r); agent && !agentRouteAllowed(r) {
			jsonlog.Log("warn", "apps: the agent of a chat with apps asked for a route outside its list", jsonlog.F{K: "path", V: r.URL.Path}, jsonlog.F{K: "method", V: r.Method})
			secretJSON(w, http.StatusForbidden, map[string]string{"error": errAgentOutsideTheWall})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// errAgentCannotChangeApps is the refusal an agent token gets on those routes (and on a publish past dev and beta,
// which the publish route decides from its own reading of the body).
const errAgentCannotChangeApps = "agent_cannot_change_apps"

// agentChangesApps says whether a request made with an agent token is to a route the agent may not use at all.
func agentChangesApps(r *http.Request) bool {
	for _, p := range agentRefusedAppRoutes {
		if pathIs(r, p) {
			return true
		}
	}
	return false
}

// readAgentBody decodes an agent's request body strictly: one JSON object of the struct's fields and nothing after it.
// Anything else is false, which the route takes as a refusal.
func readAgentBody(r *http.Request, out any) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, secretBodyMax+1))
	if err != nil || len(raw) > secretBodyMax {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(out) != nil {
		return false
	}
	_, err = dec.Token()
	return err == io.EOF
}

// refuseAgentAppChanges wraps the apps' routes: a request with an agent token to one that changes the apps gets 403
// agent_cannot_change_apps before the route sees it. The person's cli-token and the page's session pass as before.
func refuseAgentAppChanges(cli *gate.CLIToken, routes []gate.Route) []gate.Route {
	out := make([]gate.Route, len(routes))
	for i, rt := range routes {
		next := rt.Handler
		rt.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, agent := cli.AgentOf(r); agent && agentChangesApps(r) {
				secretJSON(w, http.StatusForbidden, map[string]string{"error": errAgentCannotChangeApps})
				return
			}
			next.ServeHTTP(w, r)
		})
		out[i] = rt
	}
	return out
}
