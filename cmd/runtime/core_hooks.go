package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/claude"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/coreapi"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/nodejson"
	"github.com/ly-solutions/harnessly/internal/proc"
	"github.com/ly-solutions/harnessly/internal/prompts"
	"github.com/ly-solutions/harnessly/internal/push"
	"github.com/ly-solutions/harnessly/internal/sleep"
	"github.com/ly-solutions/harnessly/internal/state"
	"github.com/ly-solutions/harnessly/internal/status"
)

// The core's hook and agent routes (internal/coreapi), its web push sender and its idle sleep, wired to the core's
// keeper, store and tmux. The routes and the sender exist with the core; they answer and send while it runs.

// hookConfig is what the deployment says about them; see the README's runtime table.
type hookConfig struct {
	// ConfigFile is the runtime's config: its networkAccess is the access rule of the hook route.
	ConfigFile string
	// PushSubject is the VAPID subject and PushTagPrefix the start of a notification's tag; push is off without both.
	PushSubject, PushTagPrefix string
	// Sleep names the variables of the idle sleep's thresholds.
	Sleep sleep.EnvNames
}

func hookConfigFromEnv(configFile string) hookConfig {
	return hookConfig{
		ConfigFile:    configFile,
		PushSubject:   os.Getenv("RUNTIME_PUSH_SUBJECT"),
		PushTagPrefix: os.Getenv("RUNTIME_PUSH_TAG_PREFIX"),
		Sleep: sleep.EnvNames{
			IdleMin:         "RUNTIME_AGENT_SLEEP_IDLE_MIN",
			PressureMin:     "RUNTIME_AGENT_SLEEP_PRESSURE_MIN",
			ResumeRecentMin: "RUNTIME_AGENT_RESUME_RECENT_MIN",
		},
	}
}

// keeperRef is the keeper of the running core for the routes; with the core off every call is a no-op (the routes
// answer 503 before they get here).
type keeperRef struct{ c *core }

func (k keeperRef) HandleProviderEvent(p, s string, ev agents.Event, at *float64) bool {
	m := k.c.manager()
	if m == nil {
		return false
	}
	handled := m.HandleProviderEvent(p, s, ev, at)
	// v2's conversation of the session hears the hook too.
	if v := k.c.v2.Load(); v != nil {
		v.hook(s, ev)
	}
	return handled
}

func (k keeperRef) ApplyHookMeta(p, s string, meta status.HookMeta) (string, string, bool) {
	if m := k.c.manager(); m != nil {
		return m.ApplyHookMeta(p, s, meta)
	}
	return "", "", false
}

func (k keeperRef) MarkAgentLaunch(tabID string, reset bool) {
	if m := k.c.manager(); m != nil {
		m.MarkAgentLaunch(tabID, reset)
	}
}

func (k keeperRef) MarkAgentGone(tabID string, endLaunch bool) {
	if m := k.c.manager(); m != nil {
		m.MarkAgentGone(tabID, endLaunch)
	}
}

func (k keeperRef) RecoverUnknown(tabID string) (bool, string) {
	if m := k.c.manager(); m != nil {
		return m.RecoverUnknown(tabID)
	}
	return false, "no-entry"
}

func (k keeperRef) Poll() {
	if m := k.c.manager(); m != nil {
		m.Poll()
	}
}

func (k keeperRef) AgentSources() []status.AgentSource {
	if m := k.c.manager(); m != nil {
		return m.AgentSources()
	}
	return nil
}

func (k keeperRef) SynthesizeEvent(session, event string) {
	if m := k.c.manager(); m != nil {
		m.SynthesizeEvent(session, event)
	}
}

// codexSink hands the session a Codex SessionStart names to the timeline.
type codexSink struct{ c *core }

func (s codexSink) CodexSession(session string, info agents.SessionInfo, _ bool) {
	if s.c.timelineOn() {
		s.c.timeline.CodexSessionInfo(session, info)
	}
}

// FindTab is a layout tab by id, with its workspace (coreapi.TabLookup).
func (c *core) FindTab(tabID string) (coreapi.FoundTab, bool) {
	layouts := coreLayouts{store: c.store}
	for _, ws := range layouts.Workspaces() {
		layout := c.store.Layout(ws.ID)
		if layout == nil {
			continue
		}
		for _, t := range state.AllTabs(layout) {
			if t.String("id") == tabID {
				return coreapi.FoundTab{WorkspaceID: ws.ID, Directories: ws.Directories, Tab: status.TabFromLayout(t), Cwd: t.String("cwd")}, true
			}
		}
	}
	return coreapi.FoundTab{}, false
}

// configString is a string field of the runtime's config file ("" when missing).
func configString(file, key string) string {
	raw, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	v, err := jsvalue.Parse(raw)
	o, _ := v.(*jsvalue.Object)
	if err != nil || o == nil {
		return ""
	}
	s, _ := o.Get(key)
	str, _ := s.(string)
	return str
}

// access is the previous runtime's access rule: an explicit host (RUNTIME_ACCESS_HOST, the previous runtime's HOST;
// HOST itself is still read), else the config's networkAccess (localhost, tailscale or all; all when unset).
func (c *core) access() coreapi.Access {
	if host := strings.TrimSpace(env("RUNTIME_ACCESS_HOST", os.Getenv("HOST"))); host != "" {
		return coreapi.ParseAccess(host)
	}
	return coreapi.AccessFor(configString(c.cfg.Hooks.ConfigFile, "networkAccess"))
}

// resumeAgents are the agent CLIs as resuming a conversation needs them: the session a tab keeps, whether the CLI
// runs under a pane, and its resume line.
func (c *core) resumeAgents() map[string]coreapi.Agent {
	out := map[string]coreapi.Agent{}
	if c.agents == nil {
		return out
	}
	line := func(provider string) func(string, string, *agents.Launch) (string, error) {
		return func(id, ws string, l *agents.Launch) (string, error) { return c.agents.ResumeLine(provider, id, ws, l) }
	}
	codexDet := codex.NewDetector(c.cfg.Home)
	out[agents.Claude] = coreapi.Agent{ReadSessionID: claude.Tabs{}.ReadSessionID, ResumeCommand: line(agents.Claude),
		Running: func(pid int) bool { return c.claude.IsRunning(proc.ChildPIDs(pid)) }}
	out[agents.Codex] = coreapi.Agent{ReadSessionID: codex.Tabs{}.ReadSessionID, ResumeCommand: line(agents.Codex),
		Running: func(pid int) bool { return codexDet.IsRunning(proc.ChildPIDs(pid)) }}
	return out
}

// newHookAPI builds the route configuration over the core; the token check and the API refusal come from main.
func (c *core) newHookAPI() *coreapi.Config {
	return &coreapi.Config{
		Keeper:        keeperRef{c},
		Access:        c.access,
		CodexSessions: codexSink{c},
		Tabs:          c,
		Panes:         c.cfg.Tmux,
		Agents:        c.resumeAgents(),
		RecoverSession: func(session, _ string) (string, bool) {
			m := c.manager()
			if m == nil {
				return "", false
			}
			tabID := m.TabOfSession(session)
			if tabID == "" {
				if _, _, id, ok := c.cfg.Tmux.ParseSessionName(session); ok {
					tabID = id
				}
			}
			if tabID == "" || !m.RecoverSession(tabID) {
				return "", false
			}
			info, _ := m.Tab(tabID)
			return info.AgentSessionID, info.AgentSessionID != ""
		},
		LaunchOf: func(session string) *agents.Launch {
			l := c.tabLaunch(session)
			return &l
		},
		KillTree:           func(pid int) int { return proc.KillTree(pid, 3*time.Second, 150*time.Millisecond, nil) },
		Visibility:         c.visibility(),
		SessionProcessPath: platformAPI + "/session-process",
		Log:                coreLog,
	}
}

// visibility is where the page's device visibility goes: the push sender's store, or nowhere without push.
func (c *core) visibility() coreapi.Visibility {
	if c.push == nil {
		return nil
	}
	return c.push.Store
}

// newPusher is the core's web push sender (the process's shared one when given), nil when the deployment named no
// subject or tag prefix.
func newPusher(dataDir string, shared *push.Sender, h hookConfig) *push.Sender {
	if h.PushSubject == "" || h.PushTagPrefix == "" {
		agent.Log("info", "core: web push is off (RUNTIME_PUSH_SUBJECT or RUNTIME_PUSH_TAG_PREFIX unset)")
		return nil
	}
	if shared != nil {
		return shared
	}
	return newSharedPusher(dataDir, h)
}

// newSharedPusher is the process's one web push sender: one subscription store with one lock for every writer.
func newSharedPusher(dataDir string, h hookConfig) *push.Sender {
	return push.New(push.Config{Dir: dataDir, Subject: h.PushSubject, TagPrefix: h.PushTagPrefix, Log: coreLog})
}

// resumeForQueue brings a gone agent back for a queued message: the session-process resume. A line that cannot be built
// gives the launch up; a tmux call that failed is tried again.
func (c *core) resumeForQueue(tabID string) string {
	found, ok := c.FindTab(tabID)
	if !ok {
		return prompts.ResumeNoSession
	}
	result, err := c.api.Resume(found)
	if err != nil {
		agent.Log("warn", "core: agent of a queued message not resumed", agent.F{K: "tabId", V: tabID}, agent.F{K: "err", V: err.Error()})
		if errors.Is(err, coreapi.ErrResumeLine) {
			return prompts.ResumeNoSession
		}
		return "busy-shell"
	}
	return result
}

// hookRoutes are the coreapi routes under slice "core": while the core is starting they answer 503 like the prompt
// routes.
func (c *core) hookRoutes(tokenOK func(*http.Request) bool, apiRefusal func(*http.Request) (int, any, http.Header, bool)) []gate.Route {
	c.api.TokenOK, c.api.APIRefusal = tokenOK, apiRefusal
	var routes []gate.Route
	for _, jr := range c.api.Routes() {
		compute := jr.Compute
		routes = append(routes, gate.Route{Slice: jr.Slice, Match: jr.Match, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c.manager() == nil {
				code, v, h := nodejson.Answer(r, http.StatusServiceUnavailable, errorBody("the Go core is starting"), nil)
				writeAnswer(w, code, v, h)
				return
			}
			code, v, h := compute(r)
			writeAnswer(w, code, v, h)
		})})
	}
	return routes
}

// writeAnswer writes a route's answer: JSON, a gate.Raw body, or none.
func writeAnswer(w http.ResponseWriter, status int, v any, h http.Header) {
	for k, vs := range h {
		w.Header()[k] = vs
	}
	switch x := v.(type) {
	case nil:
		w.WriteHeader(status)
	case gate.Raw:
		w.Header().Set("Content-Type", x.ContentType)
		w.WriteHeader(status)
		_, _ = w.Write(x.Body)
	case json.RawMessage:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write(x)
	default:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write(gate.Encode(v))
	}
}

// sleepTabs is the keeper as the idle sleep reads it.
type sleepTabs struct{ m func() *status.Manager }

func (s sleepTabs) AgentTabs() []sleep.Tab {
	m := s.m()
	if m == nil {
		return nil
	}
	var out []sleep.Tab
	for _, a := range m.AgentSources() {
		t := sleep.Tab{TabID: a.TabID, PanelType: a.PanelType, Provider: a.Provider, CliState: a.CliState, TmuxSession: a.TmuxSession,
			JSONLPath: a.Transcript, AgentSessionID: a.AgentSessionID, LastEventName: a.LastEventName}
		if a.LastEventAt != nil {
			t.LastEventAt = *a.LastEventAt
		}
		out = append(out, t)
	}
	return out
}

func (s sleepTabs) Current(tabID string) (sleep.Tab, bool) {
	m := s.m()
	if m == nil {
		return sleep.Tab{}, false
	}
	info, ok := m.Tab(tabID)
	if !ok {
		return sleep.Tab{}, false
	}
	return sleep.Tab{TabID: tabID, PanelType: info.PanelType, Provider: info.Provider, CliState: info.CliState, TmuxSession: info.TmuxSession,
		JSONLPath: info.Transcript, AgentSessionID: info.AgentSessionID}, true
}

// newSleeper is the idle sleep over the keeper m gives once the core runs.
func (c *core) newSleeper(m func() *status.Manager) *sleep.Sleeper {
	return &sleep.Sleeper{
		Tabs:       sleepTabs{m},
		Panes:      c.cfg.Tmux,
		Claude:     c.claude,
		Activity:   &claude.ActivityReader{},
		Background: c.bg,
		Getenv:     os.Getenv,
		Env:        c.cfg.Hooks.Sleep,
		MarkGone: func(tabID string) {
			if k := m(); k != nil {
				k.MarkAgentGone(tabID, true)
			}
		},
	}
}

// sleepReport logs a sweep that put agents to sleep.
func sleepReport(slept []string, underPressure bool) {
	if len(slept) > 0 {
		agent.Log("info", "core: idle agents put to sleep", agent.F{K: "slept", V: slept}, agent.F{K: "underPressure", V: underPressure})
	}
}

// tabRemoved chains what forgets a closed tab.
func tabRemoved(fns ...func(string)) func(string) {
	return func(tabID string) {
		for _, f := range fns {
			if f != nil {
				f(tabID)
			}
		}
	}
}
