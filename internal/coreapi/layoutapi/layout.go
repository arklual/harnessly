package layoutapi

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"harnessly/internal/agent"
	"harnessly/internal/agents"
	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
	"harnessly/internal/state"
)

// must turns an unexpected store error into the 500 the previous runtime gave an exception.
func must(err error) {
	if err != nil {
		panic(err)
	}
}

// refusal is the route's answer for a store refusal; false for any other error.
func (c *call) refusal(err error) bool {
	var e *state.Error
	if errors.As(err, &e) {
		c.fail(e.Status, e.Message)
		return true
	}
	return false
}

// wsID is `?workspace=` or, without one, the active workspace; "" when there is none.
func (c *call) wsID() string {
	if p := c.q("workspace"); p.Truthy() {
		return p.String()
	}
	return c.a.d.Store.ActiveWorkspaceID()
}

func (c *call) needWorkspace() (string, bool) {
	ws := c.wsID()
	if ws == "" {
		c.fail(400, "No workspace found")
		return "", false
	}
	return ws, true
}

func firstDir(ws *jsvalue.Object) string {
	if dirs := ws.Array("directories"); len(dirs) > 0 {
		s, _ := dirs[0].(string)
		return s
	}
	return ""
}

// stringOr is v when it is a string, else "".
func stringOr(v any) string { s, _ := v.(string); return s }

func launchOf(o *jsvalue.Object) *agents.Launch {
	if o == nil {
		return nil
	}
	l := &agents.Launch{}
	if m, ok := func() (string, bool) { v, _ := o.Get("model"); s, ok := v.(string); return s, ok }(); ok {
		l.Model = &m
	}
	if e, ok := func() (string, bool) { v, _ := o.Get("effort"); s, ok := v.(string); return s, ok }(); ok {
		l.Effort = &e
	}
	return l
}

func launchJSON(l *agents.Launch) *jsvalue.Object {
	if l == nil {
		return nil
	}
	o := jsvalue.NewObject("model", nil, "effort", nil)
	if l.Model != nil {
		o.Set("model", *l.Model)
	}
	if l.Effort != nil {
		o.Set("effort", *l.Effort)
	}
	return o
}

// launchForAgentSession is the launch of the newest tab that ran this agent session and remembers one.
func (a *api) launchForAgentSession(sessionID string) *agents.Launch {
	tabs := a.d.Store.WorkspaceTabs()
	for i := len(tabs) - 1; i >= 0; i-- {
		t := tabs[i].Tab
		var stored any
		if st := t.Object("agentState"); st != nil {
			stored, _ = st.Get("sessionId")
		}
		if stored == nil {
			stored, _ = t.Get("claudeSessionId")
		}
		if stored != sessionID {
			continue
		}
		if l := a.d.Store.TabLaunch(t); l != nil {
			return launchOf(l)
		}
	}
	return nil
}

func (a *api) availability(panelType string) *Unavailable {
	if a.d.Agents == nil {
		return nil
	}
	return a.d.Agents.Availability(panelType)
}

func (c *call) unavailable(u *Unavailable) {
	c.reply(409, jsvalue.NewObject("error", u.Code, "providerId", u.ProviderID, "providerDisplayName", u.ProviderDisplayName,
		"panelType", u.PanelType, "suggestedCommand", u.SuggestedCommand))
}

func providerOf(panelType any) state.Provider { return state.ProviderOfPanel(panelType) }

func providerID(p state.Provider) any {
	if p == "" {
		return nil
	}
	return string(p)
}

func agentSessionID(p state.Provider, tab *jsvalue.Object) any {
	if p == "" {
		return nil
	}
	return p.SessionID(tab)
}

// register tells the status keeper of a new tab with a terminal.
func (a *api) register(wsID string, tab *jsvalue.Object) {
	panelType, _ := tab.Get("panelType")
	if a.d.Status == nil || panelType == "web-browser" {
		return
	}
	p := providerOf(panelType)
	a.d.Status.RegisterTab(tab.String("id"), TabEntry{CliState: "inactive", WorkspaceID: wsID, TabName: tab.String("name"),
		TmuxSession: tab.String("sessionName"), PanelType: panelType, AgentProviderID: string(p), AgentSessionID: agentSessionID(p, tab)})
}

func (a *api) markLaunch(tabID string, reset bool) {
	if a.d.Status != nil {
		a.d.Status.MarkAgentLaunch(tabID, reset)
	}
}

// resumeLater types the resume line once the new tab's shell is up.
func (a *api) resumeLater(p state.Provider, sessionID, wsID string, tab *jsvalue.Object, launch *agents.Launch) {
	if a.d.Agents == nil {
		return
	}
	time.AfterFunc(a.d.ShellReady, func() {
		line, err := a.d.Agents.ResumeLine(string(p), sessionID, wsID, launch)
		if err == nil {
			err = a.d.Tmux.SendKeys(tab.String("sessionName"), line)
		}
		if err != nil {
			agent.Log("warn", "resume sendKeys failed", agent.F{K: "error", V: err.Error()})
			return
		}
		a.markLaunch(tab.String("id"), false)
	})
}

// GET, PATCH /api/layout
func (a *api) layout(c *call) {
	ws, ok := c.needWorkspace()
	if !ok {
		return
	}
	switch c.r.Method {
	case "GET":
		l, err := a.d.Store.GetLayout(ws, firstDir(a.d.Store.Workspace(ws)))
		if err != nil {
			agent.Log("error", "layout GET failed", agent.F{K: "error", V: err.Error()})
			c.fail(500, "Failed to load layout")
			return
		}
		c.reply(200, l)
	case "PATCH":
		body, _ := c.body.(*jsvalue.Object)
		l, err := a.d.Store.PatchLayout(ws, state.ParseLayoutPatch(body))
		if !c.refusal(err) {
			must(err)
			c.reply(200, l)
		}
	default:
		c.methodNotAllowed("GET, PATCH")
	}
}

// POST /api/layout/pane: split a pane, or start the session of a pane the page adds.
func (a *api) pane(c *call) {
	if c.r.Method != "POST" {
		c.methodNotAllowed("POST")
		return
	}
	ws, ok := c.needWorkspace()
	if !ok {
		return
	}
	source, _ := c.field("sourcePaneId")
	orientation, _ := c.field("orientation")
	cwd, _ := c.field("cwd")
	panelType, _ := c.field("panelType")
	if jsvalue.Truthy(source) && jsvalue.Truthy(orientation) {
		l, err := a.d.Store.SplitPane(ws, jscompat.String(source), jscompat.String(orientation), stringOr(cwd), stringOr(panelType))
		var e *state.Error
		switch {
		case errors.As(err, &e) && e.Status == 404:
			c.fail(404, "Target pane not found")
		case err != nil:
			c.fail(500, "Failed to create pane")
		default:
			c.reply(200, l)
		}
		return
	}
	paneID, tab, err := a.d.Store.CreatePane(ws, stringOr(cwd))
	if err != nil {
		c.fail(500, "Failed to create pane")
		return
	}
	c.reply(200, jsvalue.NewObject("paneId", paneID, "tab", tab))
}

// DELETE, PATCH /api/layout/pane/<paneId>
func (a *api) paneByID(c *call) {
	ws := c.wsID()
	paneID := c.param("paneId")
	switch c.r.Method {
	case "DELETE":
		if ws == "" {
			var sessions []string
			if v, _ := c.field("sessions"); v != nil {
				sessions = jsvalue.Strings(func() []any { a, _ := v.([]any); return a }())
			}
			a.d.Store.KillPaneSessions(sessions)
			c.end(204)
			return
		}
		l, err := a.d.Store.ClosePane(ws, paneID)
		if !c.refusal(err) {
			must(err)
			c.reply(200, l)
		}
	case "PATCH":
		if ws == "" {
			c.fail(400, "No workspace found")
			return
		}
		var active *string
		if v, ok := c.field("activeTabId"); ok {
			s := stringOr(v)
			active = &s
		}
		l, err := a.d.Store.PatchPane(ws, paneID, active)
		if !c.refusal(err) {
			must(err)
			c.reply(200, l)
		}
	default:
		c.methodNotAllowed("DELETE, PATCH")
	}
}

// POST /api/layout/pane/<paneId>/tabs: a new tab, optionally continuing an agent conversation.
func (a *api) tabs(c *call) {
	if c.r.Method != "POST" {
		c.methodNotAllowed("POST")
		return
	}
	ws, ok := c.needWorkspace()
	if !ok {
		return
	}
	name, _ := c.field("name")
	cwd, _ := c.field("cwd")
	panelType, _ := c.field("panelType")
	command, _ := c.field("command")
	resume, _ := c.field("resumeSessionId")

	var p state.Provider
	resumeID := ""
	if jsvalue.Truthy(resume) {
		pt := panelType
		if pt == nil {
			pt = "claude-code"
		}
		if p = providerOf(pt); p == "" {
			c.fail(400, "Unknown panel type for resume")
			return
		}
		if resumeID = stringOr(resume); !agents.IsUUID(resumeID) {
			c.fail(400, "Invalid session ID format")
			return
		}
	}
	check := stringOr(panelType)
	if p != "" {
		check = panelOf(p)
	}
	if u := a.availability(check); u != nil {
		c.unavailable(u)
		return
	}
	resuming := p != "" && !jsvalue.Truthy(command)
	var launch *agents.Launch
	if resuming {
		launch = a.launchForAgentSession(resumeID)
	}
	tab, err := a.d.Store.AddTab(ws, c.param("paneId"), state.TabOptions{Name: stringOr(name), Cwd: stringOr(cwd), PanelType: stringOr(panelType), Command: stringOr(command)})
	var e *state.Error
	if errors.As(err, &e) && e.Status == 404 {
		c.fail(404, "Pane not found")
		return
	}
	if err != nil {
		c.fail(500, "Failed to create tab")
		return
	}
	if resuming {
		p.WriteSessionID(tab, resumeID)
		if err := a.d.Store.UpdateTabAgentSessionID(tab.String("sessionName"), p, resumeID); err != nil {
			c.fail(500, "Failed to create tab")
			return
		}
		if launch != nil {
			if _, err := a.d.Store.UpdateTabMeta(tab.String("sessionName"), jsvalue.NewObject("launch", launchJSON(launch))); err != nil {
				c.fail(500, "Failed to create tab")
				return
			}
		}
	}
	a.register(ws, tab)
	if pt, _ := tab.Get("panelType"); pt != "web-browser" && jsvalue.Truthy(command) {
		a.markLaunch(tab.String("id"), false)
	}
	if resuming {
		a.resumeLater(p, resumeID, ws, tab, launch)
	}
	c.reply(200, tab)
}

func panelOf(p state.Provider) string {
	if p == state.Codex {
		return "codex-cli"
	}
	return "claude-code"
}

// POST (restart), DELETE, PATCH /api/layout/pane/<paneId>/tabs/<tabId>
func (a *api) tab(c *call) {
	ws, ok := c.needWorkspace()
	if !ok {
		return
	}
	paneID, tabID := c.param("paneId"), c.param("tabId")
	switch c.r.Method {
	case "DELETE":
		err := a.d.Store.RemoveTab(ws, paneID, tabID)
		if c.refusal(err) {
			return
		}
		must(err)
		if a.d.Status != nil {
			a.d.Status.RemoveTab(tabID)
		}
		c.end(204)
	case "POST":
		command, _ := c.field("command")
		err := a.d.Store.RestartTab(ws, paneID, tabID, stringOr(command))
		if !c.refusal(err) {
			if err != nil {
				c.fail(500, "Failed to restart session")
				return
			}
			c.reply(200, jsvalue.NewObject("ok", true))
		}
	case "PATCH":
		body, _ := c.body.(*jsvalue.Object)
		p, err := state.ParseTabPatch(body)
		if c.refusal(err) {
			return
		}
		l, err := a.d.Store.PatchTab(ws, paneID, tabID, p)
		if !c.refusal(err) {
			must(err)
			c.reply(200, l)
		}
	default:
		c.methodNotAllowed("POST, DELETE, PATCH")
	}
}

// POST /api/layout/pane/<paneId>/tabs/<tabId>/move
func (a *api) tabMove(c *call) {
	if c.r.Method != "POST" {
		c.methodNotAllowed("POST")
		return
	}
	ws, ok := c.needWorkspace()
	if !ok {
		return
	}
	to, _ := c.field("toPaneId")
	index, hasIndex := c.field("toIndex")
	if !jsvalue.Truthy(to) || !hasIndex {
		c.fail(400, "toPaneId and toIndex required")
		return
	}
	toPane, isStr := to.(string)
	if !isStr {
		c.fail(404, "Target not found")
		return
	}
	l, err := a.d.Store.MoveTab(ws, c.param("tabId"), c.param("paneId"), toPane, toNumber(index))
	if !c.refusal(err) {
		must(err)
		c.reply(200, l)
	}
}

// toNumber is JavaScript's Number(v) for what a JSON body holds.
func toNumber(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case bool:
		if x {
			return 1
		}
		return 0
	case nil:
		return 0
	case string:
		s := jsvalue.TrimJS(x)
		if s == "" {
			return 0
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil && !strings.ContainsAny(s, "_") {
			return f
		}
	}
	return math.NaN()
}

// PATCH /api/layout/pane/<paneId>/tabs/order
func (a *api) tabsOrder(c *call) {
	if c.r.Method != "PATCH" {
		c.methodNotAllowed("PATCH")
		return
	}
	ws, ok := c.needWorkspace()
	if !ok {
		return
	}
	v, _ := c.field("tabIds")
	ids, isArr := v.([]any)
	if !isArr || len(ids) == 0 {
		c.fail(400, "tabIds array required")
		return
	}
	strs := jsvalue.Strings(ids)
	if len(strs) == 0 {
		c.fail(404, "Target not found")
		return
	}
	l, err := a.d.Store.ReorderTabs(ws, c.param("paneId"), strs)
	if !c.refusal(err) {
		must(err)
		c.reply(200, l)
	}
}

// GET /api/layout/cwd?session=: a session's folder and the command its shell runs.
func (a *api) layoutCwd(c *call) {
	if c.r.Method != "GET" {
		c.methodNotAllowed("GET")
		return
	}
	session := c.q("session")
	if !session.Truthy() {
		c.fail(400, "session parameter required")
		return
	}
	if !a.d.Tmux.HasSession(session.String()) {
		c.fail(404, "Session not found")
		return
	}
	cwd, last := a.d.Tmux.CwdAndLastCommand(session.String())
	if cwd == "" {
		c.fail(500, "Failed to get CWD")
		return
	}
	var lastCommand any
	if last != nil {
		lastCommand = *last
	}
	c.reply(200, jsvalue.NewObject("cwd", cwd, "lastCommand", lastCommand))
}
