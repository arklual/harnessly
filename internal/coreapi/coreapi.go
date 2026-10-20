// Package coreapi answers the HTTP routes of the core slice that drive the tab state keeper: the agents' hook reports,
// a launch typed by the page, the recovery of a tab whose state is unknown, killing and resuming a conversation's
// agent, and the device visibility the web push sender reads. Status codes and bodies are the previous runtime's.
package coreapi

import (
	"encoding/json"
	"math"
	"net/http"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/claude"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/nodejson"
	"github.com/ly-solutions/harnessly/internal/status"
)

// Slice is the switch these routes answer under.
const Slice = "core"

// Keeper is the part of the state keeper (*status.Manager) the routes drive.
type Keeper interface {
	HandleProviderEvent(providerID, session string, ev agents.Event, firedAt *float64) bool
	ApplyHookMeta(providerID, session string, meta status.HookMeta) (tabID, cliState string, ok bool)
	MarkAgentLaunch(tabID string, reset bool)
	MarkAgentGone(tabID string, endLaunch bool)
	RecoverUnknown(tabID string) (bool, string)
	Poll()
	AgentSources() []status.AgentSource
	SynthesizeEvent(session, event string)
}

// CodexSessionSink hears the session a Codex SessionStart hook names (clear: it was a /clear), for the session
// watchers of the timeline; the deployment forwards it to the runtime that still holds the timeline.
type CodexSessionSink interface {
	CodexSession(tmuxSession string, info agents.SessionInfo, clear bool)
}

// FoundTab is a layout tab with what resuming it needs.
type FoundTab struct {
	WorkspaceID string
	Directories []string
	Tab         status.Tab
	Cwd         string
}

// TabLookup finds a tab by id in the layouts on disk.
type TabLookup interface {
	FindTab(tabID string) (FoundTab, bool)
}

// Panes is what the routes ask tmux; *tmux.Tmux has it.
type Panes interface {
	HasSession(name string) bool
	PanePID(name string) int
	CreateSession(name string, cols, rows int, cwd string) error
	CheckTerminalProcess(name string) (safe bool, process string)
	SendKeys(name, line string) error
}

// Agent is one agent CLI as resuming a conversation needs it.
type Agent struct {
	// ReadSessionID is the tab's stored session (claude.Tabs or codex.Tabs).
	ReadSessionID func(t agents.TabAgent) string
	// Running tells whether the agent runs under a pane's shell.
	Running func(panePID int) bool
	// ResumeCommand is the line that resumes a session (claude/codex LaunchConfig).
	ResumeCommand func(sessionID, workspaceID string, launch *agents.Launch) (string, error)
}

// Visibility is where the page's device visibility goes (the push sender's store).
type Visibility interface {
	MarkVisible(deviceID string)
	MarkHidden(deviceID string)
}

// Timings of a resume; zero values take the previous runtime's.
type Timings struct {
	// ShellReady: a fresh tmux session's shell needs a moment before it takes keys.
	ShellReady time.Duration
	// BackstopPoll and BackstopWait: how often and how long the agent is looked for after the resume line; BackstopGrace
	// is how long it may then run without a SessionStart hook before one is synthesized.
	BackstopPoll, BackstopWait, BackstopGrace time.Duration
}

func (t Timings) withDefaults() Timings {
	def := func(v *time.Duration, d time.Duration) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&t.ShellReady, 500*time.Millisecond)
	def(&t.BackstopPoll, 500*time.Millisecond)
	def(&t.BackstopWait, 30*time.Second)
	def(&t.BackstopGrace, 5*time.Second)
	return t
}

// Config is what the routes use.
type Config struct {
	Keeper Keeper
	// TokenOK checks the runtime's cli-token in the hook's headers.
	TokenOK func(*http.Request) bool
	// APIRefusal is the dispatcher's answer to a caller with neither a page session nor the cli-token (refused false
	// lets the request in); the hook route is open and checks the token itself.
	APIRefusal func(*http.Request) (status int, body any, h http.Header, refused bool)
	// Access is the network access rule, asked per hook (the setting may change).
	Access func() Access
	// RemoteAddr is the caller's address (r.RemoteAddr by default).
	RemoteAddr    func(*http.Request) string
	CodexSessions CodexSessionSink
	Tabs          TabLookup
	Panes         Panes
	// Agents by provider id.
	Agents map[string]Agent
	// RecoverSession finds and stores the session of a tab that lost its id (from its transcript or history).
	RecoverSession func(session string, providerID string) (string, bool)
	// LaunchOf is the model and effort the tab's conversation was started with, nil for the CLI's default.
	LaunchOf func(session string) *agents.Launch
	// KillTree stops a pane's process tree, keeping the shell, and returns how many processes it stopped.
	KillTree   func(panePID int) int
	Visibility Visibility
	// SessionProcessPath is the route of killing and resuming a conversation's agent (under the deployment's platform
	// prefix); empty leaves it out.
	SessionProcessPath string
	// MirrorVisibility also tells the previous runtime, which still sends some pushes of its own.
	MirrorVisibility func(deviceID string, visible bool)
	Timings          Timings
	Log              func(level, msg string, kv ...any)
}

func (c *Config) log(level, msg string, kv ...any) {
	if c.Log != nil {
		c.Log(level, msg, kv...)
	}
}

// pathIs matches a route's path, also behind the machine agent's /agents prefix.
func pathIs(r *http.Request, p string) bool { return r.URL.Path == p || r.URL.Path == "/agents"+p }

// answer is a JSON answer as the previous runtime's dispatcher sent it (with its ETag and 304).
func answer(r *http.Request, code int, v any, h http.Header) (int, any, http.Header) {
	return nodejson.Answer(r, code, v, h)
}

func errorBody(msg string) map[string]string { return map[string]string{"error": msg} }

func textAnswer(code int, text string) (int, any, http.Header) {
	return code, gate.Raw{ContentType: "text/plain", Body: []byte(text)}, nil
}

var internalError = func() (int, any, http.Header) { return textAnswer(500, "Internal Server Error") }

// guarded runs a route behind the dispatcher's authorization and body parsing, then the handler.
func (c *Config) guarded(open bool, handle func(*http.Request, any) (int, any, http.Header)) func(*http.Request) (int, any, http.Header) {
	return func(r *http.Request) (code int, v any, h http.Header) {
		if !open && c.APIRefusal != nil {
			if s, body, hh, refused := c.APIRefusal(r); refused {
				return s, body, hh
			}
		}
		body, err := readBody(r)
		if err != nil {
			if be, ok := asErrBody(err); ok {
				return textAnswer(be.status, be.text)
			}
			return internalError()
		}
		defer func() {
			if p := recover(); p != nil {
				c.log("error", "api handler failed", "path", r.URL.Path, "panic", p)
				code, v, h = internalError()
			}
		}()
		return handle(r, body)
	}
}

// Routes are the core slice's routes for the gate.
func (c *Config) Routes() []gate.JSONRoute {
	route := func(path string, open bool, h func(*http.Request, any) (int, any, http.Header)) gate.JSONRoute {
		return gate.JSONRoute{Slice: Slice, Match: func(r *http.Request) bool { return pathIs(r, path) }, Compute: c.guarded(open, h)}
	}
	routes := []gate.JSONRoute{
		route("/api/status/hook", true, c.hook),
		route("/api/status/agent-launch", false, c.agentLaunch),
		route("/api/tmux/recover-unknown", false, c.recoverUnknown),
		route("/api/push/visibility", false, c.visibility),
	}
	if c.SessionProcessPath != "" {
		routes = append(routes, route(c.SessionProcessPath, false, c.sessionProcess))
	}
	return routes
}

func methodNotAllowed(r *http.Request, msg string) (int, any, http.Header) {
	return answer(r, http.StatusMethodNotAllowed, errorBody(msg), http.Header{"Allow": {"POST"}})
}

// hook: Claude's status hook script reports {event, session, notificationType?, at?}; Codex's posts its payload with
// ?provider=codex&tmuxSession=. Every success is a 204.
func (c *Config) hook(r *http.Request, body any) (int, any, http.Header) {
	if r.Method != http.MethodPost {
		return methodNotAllowed(r, "method not allowed")
	}
	if c.TokenOK == nil || !c.TokenOK(r) {
		return answer(r, http.StatusForbidden, errorBody("forbidden"), nil)
	}
	remote := r.RemoteAddr
	if c.RemoteAddr != nil {
		remote = c.RemoteAddr(r)
	}
	if c.Access != nil && !c.Access().Allowed(remote) {
		return answer(r, http.StatusForbidden, errorBody("forbidden"), nil)
	}
	if p, ok := queryString(r, "provider"); ok && p == "codex" {
		return c.codexHook(r, body)
	}
	return c.claudeHook(body)
}

func (c *Config) claudeHook(body any) (int, any, http.Header) {
	f := fields(body)
	event, isEvent := f["event"].(string)
	session, _ := f["session"].(string)
	if !isEvent || event == "poll" || session == "" {
		go c.Keeper.Poll()
		return http.StatusNoContent, nil, nil
	}
	nt, _ := f["notificationType"].(string)
	ev, ok := claude.TranslateHook(event, nt)
	if !ok {
		c.log("debug", "unknown claude hook event, ignoring", "event", event, "session", session)
		return http.StatusNoContent, nil, nil
	}
	var firedAt *float64
	if at, isNum := f["at"].(float64); isNum && !math.IsInf(at, 0) && !math.IsNaN(at) {
		firedAt = &at
	}
	c.Keeper.HandleProviderEvent(claude.ProviderID, session, ev, firedAt)
	return http.StatusNoContent, nil, nil
}

func optPtr(p *string) *status.Opt[string] {
	if p == nil {
		return nil
	}
	o := status.Some(*p)
	return &o
}

// hookMeta turns a Codex translation into the keeper's terms.
func hookMeta(m codex.Meta) status.HookMeta {
	meta := status.HookMeta{
		SessionID:       optPtr(m.SessionID),
		JSONLPath:       optPtr(m.JSONLPath),
		LastUserMessage: optPtr(m.LastUserMessage),
		AgentSummary:    optPtr(m.AgentSummary),
		ClearMessages:   m.ClearMessages,
	}
	if m.SetPermission {
		raw := json.RawMessage("null")
		if m.Permission != nil {
			raw = nodejson.Marshal(m.Permission)
		}
		meta.PermissionRequest = &raw
	}
	return meta
}

func (c *Config) codexHook(r *http.Request, body any) (int, any, http.Header) {
	session, ok := queryString(r, "tmuxSession")
	if !ok || session == "" {
		return answer(r, http.StatusBadRequest, errorBody("missing tmuxSession"), nil)
	}
	payload := fields(body)
	t := codex.ProcessHook(payload)
	_, cliState, applied := c.Keeper.ApplyHookMeta(codex.ProviderID, session, hookMeta(t.Meta))
	if !applied {
		return http.StatusNoContent, nil, nil
	}
	if t.SessionInfo != nil && c.CodexSessions != nil {
		c.CodexSessions.CodexSession(session, *t.SessionInfo, t.ClearSession)
	}
	if t.Event != nil && codex.ShouldEmit(payload, cliState) {
		c.Keeper.HandleProviderEvent(codex.ProviderID, session, *t.Event, nil)
	}
	return http.StatusNoContent, nil, nil
}

// agentLaunch: the page typed a launch line into a tab over the terminal socket.
func (c *Config) agentLaunch(r *http.Request, body any) (int, any, http.Header) {
	if r.Method != http.MethodPost {
		return methodNotAllowed(r, "Method not allowed")
	}
	f := fields(body)
	if !truthy(f["tabId"]) {
		return answer(r, http.StatusBadRequest, errorBody("tabId is required"), nil)
	}
	if tabID, ok := f["tabId"].(string); ok {
		c.Keeper.MarkAgentLaunch(tabID, f["resetAgentSession"] == true)
	}
	return http.StatusNoContent, nil, nil
}

// recoverUnknown: a tab in the unknown state whose screen shows a permission prompt is waiting for an answer.
func (c *Config) recoverUnknown(r *http.Request, body any) (code int, v any, h http.Header) {
	if r.Method != http.MethodPost {
		return methodNotAllowed(r, "Method not allowed")
	}
	tabID, _ := fields(body)["tabId"].(string)
	if tabID == "" {
		return answer(r, http.StatusBadRequest, errorBody("tabId required"), nil)
	}
	defer func() {
		if p := recover(); p != nil {
			c.log("error", "recover-unknown failed", "panic", p)
			code, v, h = answer(r, http.StatusInternalServerError, errorBody("recover-unknown failed"), nil)
		}
	}()
	recovered, reason := c.Keeper.RecoverUnknown(tabID)
	if recovered {
		return answer(r, 200, map[string]bool{"recovered": true}, nil)
	}
	return answer(r, 200, struct {
		Recovered bool   `json:"recovered"`
		Reason    string `json:"reason"`
	}{false, reason}, nil)
}

// visibility: a device the person is looking at gets no system notification; the page reports when it shows or hides.
func (c *Config) visibility(r *http.Request, body any) (int, any, http.Header) {
	if r.Method != http.MethodPost {
		return methodNotAllowed(r, "Method not allowed")
	}
	f := fields(body)
	visible, isBool := f["visible"].(bool)
	if !truthy(f["deviceId"]) || !isBool {
		return answer(r, http.StatusBadRequest, errorBody("Invalid body"), nil)
	}
	if id, ok := f["deviceId"].(string); ok {
		if c.Visibility != nil {
			if visible {
				c.Visibility.MarkVisible(id)
			} else {
				c.Visibility.MarkHidden(id)
			}
		}
		if c.MirrorVisibility != nil {
			go c.MirrorVisibility(id, visible)
		}
	}
	return answer(r, 200, map[string]bool{"ok": true}, nil)
}
