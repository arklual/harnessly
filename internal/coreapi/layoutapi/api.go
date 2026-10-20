// Package layoutapi answers the core slice's routes over layouts, workspaces, the CLIs' tab commands and the right
// pane's bridges, the way the previous runtime's API answered them: its route table and precedence, its sign-in check,
// its body parsing (1 MB, "Invalid JSON", 413), its JSON with ETag and 304, and each handler's statuses and texts.
package layoutapi

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/machine"
	"github.com/ly-solutions/harnessly/internal/paneintents"
	"github.com/ly-solutions/harnessly/internal/panel"
	"github.com/ly-solutions/harnessly/internal/state"
	"github.com/ly-solutions/harnessly/internal/tmux"
	"github.com/ly-solutions/harnessly/internal/vault"
)

// Tmux is what the routes ask of tmux beyond the stores. FromTmux adapts the runtime's own.
type Tmux interface {
	HasSession(name string) bool
	Capture(name string) (string, bool)
	SendKeys(name, line string) error
	SendBracketedPaste(name, text string) error
	PaneCurrentCommand(name string) string
	CheckTerminalProcess(name string) (safe bool, process string)
	ListSessions() []string
	KillServer()
	ApplyConfig(socket string)
	CreateSession(name string, cols, rows int, cwd string) error
	KillSession(name string)
	PaneShellName(tabID string, legacy bool) (string, bool)
	SessionCwd(name string) string
	// CwdAndLastCommand is the session's folder and the newest command its shell runs (nil for none).
	CwdAndLastCommand(name string) (string, *string)
	ResolveExistingDir(preferred string) string
}

type tmuxAdapter struct{ *tmux.Tmux }

func (t tmuxAdapter) SessionCwd(name string) string { return machine.SessionDir(t.Tmux, name) }

func (t tmuxAdapter) CwdAndLastCommand(name string) (string, *string) {
	return machine.SessionCwd(t.Tmux, name)
}

// FromTmux is the runtime's tmux layer as the routes use it.
func FromTmux(t *tmux.Tmux) Tmux { return tmuxAdapter{t} }

// TabEntry is what the status keeper learns of a new tab.
type TabEntry struct {
	CliState, WorkspaceID, TabName, TmuxSession string
	PanelType                                   any
	AgentProviderID                             string // "" for a tab without an agent
	AgentSessionID                              any
}

// Status is the status keeper.
type Status interface {
	RegisterTab(tabID string, e TabEntry)
	RemoveTab(tabID string)
	MarkAgentLaunch(tabID string, resetAgentSession bool)
	// HooksMissing: the tab's agent was started without the platform's hooks.
	HooksMissing(tabID string) bool
	// AgentTab is the agent tab (Claude Code or Codex) whose tmux session this is, "" for none.
	AgentTab(tmuxSession string) string
	// Rescan rebuilds the tab statuses after a tmux reset.
	Rescan()
}

// Unavailable is why an agent CLI cannot start: the 409 answer.
type Unavailable = agents.Unavailable

// StartRequest starts an agent conversation the platform's way (launch line, hooks, prompt).
type StartRequest struct {
	Provider, WorkspaceID string
	Prompt                any // string or nil
	Launch                *agents.Launch
	TabName               any // string or nil
}

// StartResult is the started conversation, or the status and JSON body of the refusal.
type StartResult struct {
	ConversationID string
	Status         int
	Body           []byte
}

// Agents is what the routes need of the agent CLIs: availability, launch lines and starting a conversation.
type Agents interface {
	// Availability is nil when the agent of panelType (if any) can run.
	Availability(panelType string) *Unavailable
	// ValidateLaunch checks a requested model and effort against what the CLI offers; the text is the 400 error.
	ValidateLaunch(provider string, model, effort any) (*agents.Launch, string)
	LaunchLine(provider, workspaceID string, launch *agents.Launch) (string, error)
	ResumeLine(provider, sessionID, workspaceID string, launch *agents.Launch) (string, error)
	Start(StartRequest) StartResult
	// AutoResume starts again the agents that ran before a reset.
	AutoResume()
}

// Sync is the sync socket: an event to every page, and how many sockets are open (the agent's relay included).
type Sync interface {
	Broadcast(event *jsvalue.Object)
	ClientCount() int
}

// Deps is everything the routes use. Status and Agents may be nil until they exist: tabs are then not registered,
// every agent counts as available and a conversation start is refused.
type Deps struct {
	Store   *state.Store
	Tmux    Tmux
	Status  Status
	Agents  Agents
	Sync    Sync
	Intents *paneintents.Store
	Panel   *panel.Bridge
	Vault   *vault.Bridge
	Targets panel.Targets
	// Session: the request carries a page session; CLIToken: it carries the runtime's cli-token (the first token
	// header present decides).
	Session  func(*http.Request) bool
	CLIToken func(*http.Request) bool
	// PlatformAPI is the prefix of the platform's own routes (state-changed, pane-shell, pane intents, bridges).
	PlatformAPI string
	Home        string
	// ShellReady is how long a new tab's shell gets before a resume line is typed (500 ms).
	ShellReady time.Duration
	// PanelWait is how long a pane command waits for a page (1.5 s).
	PanelWait time.Duration
}

// Route is one gate route: Match the paths, Handler the answer.
type Route struct {
	Match   func(*http.Request) bool
	Handler http.Handler
}

type handler func(c *call)

type entry struct {
	segments []string
	open     bool // under the CLIs' open path: the handler checks the token itself
	shadow   bool // a GET without side effects
	handle   handler
}

// Routes are the answering route and the route of the GETs that may run in the shadow.
func Routes(d *Deps) (main, shadow Route) {
	if d.ShellReady == 0 {
		d.ShellReady = 500 * time.Millisecond
	}
	if d.PanelWait == 0 {
		d.PanelWait = 1500 * time.Millisecond
	}
	a := &api{d: d}
	return a.routesFor(a.table())
}

func (a *api) routesFor(table []entry) (main, shadow Route) {
	sort.SliceStable(table, func(i, j int) bool { return precedes(table[i].segments, table[j].segments) })
	find := func(r *http.Request) (*entry, map[string]string) {
		p, ok := apiPath(r)
		if !ok {
			return nil, nil
		}
		parts := splitPath(p)
		for i := range table {
			if params, ok := match(table[i].segments, parts); ok {
				return &table[i], params
			}
		}
		return nil, nil
	}
	serve := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e, params := find(r)
		if e == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		a.serve(w, r, e, params)
	})
	main = Route{Match: func(r *http.Request) bool { e, _ := find(r); return e != nil }, Handler: serve}
	shadow = Route{Match: func(r *http.Request) bool {
		e, _ := find(r)
		return e != nil && e.shadow && r.Method == http.MethodGet
	}, Handler: serve}
	return main, shadow
}

func isParam(s string) bool { return strings.HasPrefix(s, "[") }

// precedes is the previous runtime's order: at the first segment where two routes differ, a fixed name beats a
// parameter.
func precedes(a, b []string) bool {
	for i := 0; i < max(len(a), len(b)); i++ {
		if i >= len(a) || i >= len(b) {
			return i >= len(a)
		}
		if isParam(a[i]) != isParam(b[i]) {
			return !isParam(a[i])
		}
	}
	return false
}

func match(segments, parts []string) (map[string]string, bool) {
	if len(segments) != len(parts) {
		return nil, false
	}
	params := map[string]string{}
	for i, s := range segments {
		if isParam(s) {
			params[strings.Trim(s, "[]")] = parts[i]
		} else if s != parts[i] {
			return nil, false
		}
	}
	return params, true
}

// apiPath is the path the API sees: without the app's mount point and trailing slashes.
func apiPath(r *http.Request) (string, bool) {
	p := r.URL.EscapedPath()
	if rest, ok := strings.CutPrefix(p, "/agents"); ok && (rest == "" || strings.HasPrefix(rest, "/")) {
		p = rest
	}
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	return p, p == "/api" || strings.HasPrefix(p, "/api/")
}

func splitPath(p string) []string {
	var parts []string
	for _, s := range strings.Split(p, "/") {
		if s == "" {
			continue
		}
		if d, err := url.PathUnescape(s); err == nil {
			s = d
		}
		parts = append(parts, s)
	}
	return parts
}

func segs(pattern string) []string { return splitPath(pattern) }

type api struct {
	d *Deps
	l *Launch
}

// call is one request and the answer being built.
type call struct {
	a      *api
	r      *http.Request
	params map[string]string
	query  url.Values
	body   any
	header http.Header
	status int
	out    []byte
	json   bool
	sent   bool
	// stream, when set, writes the answer itself after the headers (a server-sent stream).
	stream func(w http.ResponseWriter)
}

func (c *call) param(name string) string { return c.params[name] }

// q is req.query[name] as a handler reading it sees it.
func (c *call) q(name string) jscompat.Param {
	if v, ok := c.params[name]; ok {
		return jscompat.Param{Values: []string{v}}
	}
	return jscompat.Param{Values: c.query[name]}
}

// qString is the parameter when it is one string (typeof === 'string').
func (c *call) qString(name string) (string, bool) {
	p := c.q(name)
	if len(p.Values) == 1 {
		return p.Values[0], true
	}
	return "", false
}

// field is req.body?.name: absent unless the body is an object.
func (c *call) field(name string) (any, bool) {
	if o, ok := c.body.(*jsvalue.Object); ok {
		return o.Get(name)
	}
	return nil, false
}

func (c *call) str(name string) (string, bool) {
	v, _ := c.field(name)
	s, ok := v.(string)
	return s, ok
}

// reply answers JSON as res.status(s).json(v) does: an ETag over the body, 304 when the caller holds it.
func (c *call) reply(status int, v any) {
	c.status, c.out, c.json, c.sent = status, jsvalue.Stringify(v, ""), true, true
}

// raw answers bytes that are already JSON.
func (c *call) raw(status int, body []byte) {
	c.status, c.out, c.json, c.sent = status, body, true, true
}

// end answers with no body (res.status(s).end()).
func (c *call) end(status int) { c.status, c.sent = status, true }

func (c *call) fail(status int, text string) { c.reply(status, errorBody(text)) }

func errorBody(text string) *jsvalue.Object { return jsvalue.NewObject("error", text) }

func (c *call) methodNotAllowed(allow string) {
	c.header.Set("Allow", allow)
	c.fail(405, "Method not allowed")
}

func etag(b []byte) string {
	sum := sha1.Sum(b)
	return `"` + base64.RawURLEncoding.EncodeToString(sum[:])[:27] + `"`
}

var noCache = regexp.MustCompile(`(?:^|,)\s*no-cache\s*(?:,|$)`)

func fresh(r *http.Request, tag string) bool {
	m := strings.Join(r.Header.Values("If-None-Match"), ", ")
	if m == "" || noCache.MatchString(strings.Join(r.Header.Values("Cache-Control"), ", ")) {
		return false
	}
	if m == "*" {
		return true
	}
	for _, t := range strings.Split(m, ",") {
		if strings.TrimPrefix(strings.TrimSpace(t), "W/") == tag {
			return true
		}
	}
	return false
}

func plain(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, text)
}

func (a *api) serve(w http.ResponseWriter, r *http.Request, e *entry, params map[string]string) {
	if !e.open && !(a.d.CLIToken(r) || a.d.Session(r)) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"Unauthorized"}`)
		return
	}
	c := &call{a: a, r: r, params: params, query: jscompat.Query(r.URL.RawQuery), header: http.Header{}}
	body, status, text := parseBody(r)
	if status != 0 {
		plain(w, status, text)
		return
	}
	c.body = body
	defer func() {
		if p := recover(); p != nil {
			agent.Log("error", "api handler failed", agent.F{K: "path", V: r.URL.Path}, agent.F{K: "error", V: p})
			plain(w, 500, "Internal Server Error")
		}
	}()
	e.handle(c)
	for k, vs := range c.header {
		w.Header()[k] = vs
	}
	if c.stream != nil {
		c.stream(w)
		return
	}
	if !c.json || c.status == 204 || c.status == 304 {
		w.WriteHeader(c.status)
		return
	}
	tag := etag(c.out)
	w.Header().Set("ETag", tag)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if fresh(r, tag) {
		w.WriteHeader(304)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(c.out)))
	w.WriteHeader(c.status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(c.out)
	}
}

const bodyLimit = 1 << 20

// parseBody is Next's: JSON (empty is {}), url-encoded forms as an object, anything else as text; a status and the
// text/plain answer when it cannot be read.
func parseBody(r *http.Request) (any, int, string) {
	if r.ContentLength > bodyLimit {
		return nil, 413, "Body exceeded 1mb limit"
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, bodyLimit+1))
	if err != nil {
		return nil, 400, "Invalid body"
	}
	if len(raw) > bodyLimit {
		return nil, 413, "Body exceeded 1mb limit"
	}
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	text := strings.ToValidUTF8(string(raw), "�")
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		ct = "text/plain"
	}
	kind, _, _ := strings.Cut(ct, ";")
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "application/json", "application/ld+json":
		if text == "" {
			return jsvalue.NewObject(), 0, ""
		}
		v, err := jsvalue.Parse([]byte(text))
		if err != nil {
			return nil, 400, "Invalid JSON"
		}
		return v, 0, ""
	case "application/x-www-form-urlencoded":
		o := jsvalue.NewObject()
		for k, vs := range jscompat.Query(text) {
			if len(vs) == 1 {
				o.Set(k, vs[0])
			} else {
				o.Set(k, jsvalue.StringArray(vs))
			}
		}
		return o, 0, ""
	}
	return text, 0, ""
}

// contentTypeJSON: the content type starts with application/json, as the bridges require.
func (c *call) contentTypeJSON() bool {
	return strings.HasPrefix(c.r.Header.Get("Content-Type"), "application/json")
}

func (a *api) table() []entry {
	p := a.d.PlatformAPI
	if p == "" {
		p = "/api/platform"
	}
	var t []entry
	add := func(pattern string, open, shadow bool, h handler) {
		t = append(t, entry{segments: segs(pattern), open: open, shadow: shadow, handle: h})
	}
	add("/api/layout", false, false, a.layout)
	add("/api/layout/cwd", false, true, a.layoutCwd)
	add("/api/layout/pane", false, false, a.pane)
	add("/api/layout/pane/[paneId]", false, false, a.paneByID)
	add("/api/layout/pane/[paneId]/tabs", false, false, a.tabs)
	add("/api/layout/pane/[paneId]/tabs/order", false, false, a.tabsOrder)
	add("/api/layout/pane/[paneId]/tabs/[tabId]", false, false, a.tab)
	add("/api/layout/pane/[paneId]/tabs/[tabId]/move", false, false, a.tabMove)
	add("/api/workspace", false, true, a.workspaces)
	add("/api/workspace/[workspaceId]", false, false, a.workspace)
	add("/api/workspace/active", false, false, a.workspaceActive)
	add("/api/workspace/reorder", false, false, a.workspaceReorder)
	add("/api/workspace/validate", false, true, a.workspaceValidate)
	add("/api/workspace/group", false, false, a.groupCreate)
	add("/api/workspace/group/[groupId]", false, false, a.group)
	add("/api/workspace/group/reorder", false, false, a.groupReorder)
	add("/api/tmux/reset", false, false, a.tmuxReset)
	add(p+"/state-changed", false, false, a.stateChanged)
	add(p+"/pane-shell", false, false, a.paneShell)
	add(p+"/pane-intents", false, false, a.paneIntents)
	add(p+"/panel-result", false, false, a.panelResult)
	add(p+"/vault-result", false, false, a.vaultResult)
	add("/api/cli/workspaces", true, true, a.cliWorkspaces)
	add("/api/cli/tabs", true, false, a.cliTabs)
	add("/api/cli/tabs/[tabId]", true, false, a.cliTab)
	add("/api/cli/tabs/[tabId]/result", true, false, a.cliTabResult)
	add("/api/cli/tabs/[tabId]/send", true, false, a.cliTabSend)
	add("/api/cli/tabs/[tabId]/status", true, false, a.cliTabStatus)
	add("/api/cli/panel", true, false, a.cliPanel)
	add("/api/cli/vault", true, false, a.cliVault)
	return t
}
