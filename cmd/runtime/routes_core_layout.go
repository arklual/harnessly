package main

import (
	"crypto/subtle"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/coreapi/layoutapi"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/panel"
	"github.com/ly-solutions/harnessly/internal/status"
)

// The core's routes over layouts, workspaces, the CLIs' tabs and the pane's bridges (slice "core"; the GETs without
// side effects also under "core-shadow"). They use the core's one store, sync socket and pane intents.

// cliTokenFirst checks the cli-token the way the previous runtime's handlers did: the first token header present
// decides, whatever the others carry.
func cliTokenFirst(dataDir string, headers []string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		raw, _ := os.ReadFile(filepath.Join(dataDir, "cli-token"))
		token := strings.TrimSpace(string(raw))
		for _, h := range headers {
			vs, present := r.Header[http.CanonicalHeaderKey(strings.TrimSpace(h))]
			if !present {
				continue
			}
			return token != "" && len(vs) > 0 && subtle.ConstantTimeCompare([]byte(vs[0]), []byte(token)) == 1
		}
		return false
	}
}

func panelTargets() panel.Targets {
	t := panel.Targets{
		StandDomain:  os.Getenv("RUNTIME_STAND_DOMAIN"),
		PortalHost:   os.Getenv("RUNTIME_DOCS_HOST"),
		ReaderHost:   os.Getenv("RUNTIME_PLATFORM_HOST"),
		ReaderPath:   env("RUNTIME_DOCS_READER_PATH", "/agents/docs/"),
		GitLabHosts:  splitList(os.Getenv("RUNTIME_GITLAB_HOSTS")),
		TaskExamples: os.Getenv("RUNTIME_TASK_EXAMPLES"),
	}
	if re := os.Getenv("RUNTIME_TASK_KEY_RE"); re != "" {
		if compiled, err := regexp.Compile(re); err == nil {
			t.TaskKey = compiled
		} else {
			agent.Log("warn", "RUNTIME_TASK_KEY_RE does not compile", agent.F{K: "error", V: err.Error()})
		}
	}
	return t
}

// coreSyncHook is the sync socket as the routes use it.
type coreSyncHook struct{ c *core }

func (s coreSyncHook) Broadcast(e *jsvalue.Object) { s.c.sync.Broadcast(e) }
func (s coreSyncHook) ClientCount() int            { return s.c.sync.Hub.Count() }

// coreStatusHook is the tab keeper as the routes use it; nothing happens while the core is off.
type coreStatusHook struct{ c *core }

func optString(v any) status.Opt[string] {
	switch x := v.(type) {
	case nil:
		return status.Null[string]()
	case string:
		return status.Some(x)
	}
	return status.Null[string]()
}

func (s coreStatusHook) RegisterTab(tabID string, e layoutapi.TabEntry) {
	m := s.c.manager()
	if m == nil {
		return
	}
	entry := status.Entry{CliState: e.CliState, WorkspaceID: e.WorkspaceID, TabName: e.TabName, TmuxSession: e.TmuxSession,
		AgentSessionID: optString(e.AgentSessionID), LastEvent: status.Null[status.LastEvent]()}
	if e.PanelType != nil {
		entry.PanelType = optString(e.PanelType)
	}
	if e.AgentProviderID != "" {
		entry.AgentProviderID = status.Some(e.AgentProviderID)
	}
	m.RegisterTab(tabID, entry)
}

func (s coreStatusHook) RemoveTab(tabID string) {
	if m := s.c.manager(); m != nil {
		m.RemoveTab(tabID)
	}
}

func (s coreStatusHook) MarkAgentLaunch(tabID string, reset bool) {
	if m := s.c.manager(); m != nil {
		m.MarkAgentLaunch(tabID, reset)
	}
}

func (s coreStatusHook) HooksMissing(tabID string) bool {
	m := s.c.manager()
	return m != nil && m.HooksMissing(tabID)
}

func (s coreStatusHook) AgentTab(session string) string {
	if m := s.c.manager(); m != nil {
		return m.TabOfSession(session)
	}
	return ""
}

func (s coreStatusHook) Rescan() {
	if m := s.c.manager(); m != nil {
		m.Rescan()
	}
}

// layoutRoutes are the core's routes over layouts, workspaces and the CLIs' tabs.
func (c *core) layoutRoutes(f *gate.Gate, d deps) []gate.Route {
	deps := &layoutapi.Deps{
		Store:       c.store,
		Tmux:        layoutapi.FromTmux(c.cfg.Tmux),
		Status:      coreStatusHook{c},
		Agents:      c.agents,
		Sync:        coreSyncHook{c},
		Intents:     c.intents,
		Panel:       c.panel,
		Vault:       c.vault,
		Targets:     panelTargets(),
		Session:     d.SessionOK,
		CLIToken:    cliTokenFirst(d.DataDir, d.TokenHeaders),
		PlatformAPI: platformAPI,
		Home:        d.Home,
	}
	if c.agents == nil {
		deps.Agents = nil
	}
	main, shadow := layoutapi.Routes(deps)
	routes := []gate.Route{{Slice: "core", Match: main.Match, Handler: main.Handler}}
	return append(routes, f.AsBodyRoutes(gate.BodyRoute{Slice: "core", Match: shadow.Match, Serve: shadow.Handler.ServeHTTP})...)
}

// sessionMaxAge is the page session's lifetime, the previous runtime's MAX_AGE.
const sessionMaxAge = 7 * 24 * time.Hour

// openOrToken: the API paths the previous runtime let in without a session (its proxy's open paths), and a request
// carrying the cli-token, are never asked for a session, so theirs is not refreshed either.
func openOrToken(cliToken func(*http.Request) bool) func(*http.Request, string) bool {
	open := []string{"/api/auth", "/api/install", "/api/cli/", "/api/status/hook", platformAPI + "/share-read/",
		platformAPI + "/design/library/import", "/api/health", "/api/manifest"}
	return func(r *http.Request, p string) bool {
		for _, o := range open {
			if strings.HasPrefix(p, o) {
				return true
			}
		}
		return cliToken(r)
	}
}
