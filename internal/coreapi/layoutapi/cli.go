package layoutapi

import (
	"errors"
	"regexp"

	"harnessly/internal/agent"
	"harnessly/internal/agents"
	"harnessly/internal/jsvalue"
	"harnessly/internal/paneintents"
	"harnessly/internal/panel"
	"harnessly/internal/state"
	"harnessly/internal/vault"
)

var (
	validPanelTypes = []string{"terminal", "claude-code", "codex-cli", "agent-sessions", "web-browser", "diff"}
	agentSessionRe  = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
)

type tabLocation struct {
	paneID string
	tab    *jsvalue.Object
}

// findTab is a tab of a workspace that exists; the workspace's layout is created when it has none.
func (a *api) findTab(wsID, tabID string) *tabLocation {
	ws := a.d.Store.Workspace(wsID)
	if ws == nil {
		return nil
	}
	l, err := a.d.Store.GetLayout(wsID, "")
	must(err)
	for _, pane := range state.Panes(l) {
		for _, t := range state.Tabs(pane) {
			if t.String("id") == tabID {
				return &tabLocation{paneID: pane.String("id"), tab: t}
			}
		}
	}
	return nil
}

func (c *call) cliToken() bool {
	if !c.a.d.CLIToken(c.r) {
		c.fail(403, "Forbidden")
		return false
	}
	return true
}

// workspaceParam is ?workspaceId= as one string; answers 400 when there is none.
func (c *call) workspaceParam() (string, bool) {
	ws, ok := c.qString("workspaceId")
	if !ok || ws == "" {
		c.fail(400, "workspaceId is required")
		return "", false
	}
	return ws, true
}

// GET /api/cli/workspaces
func (a *api) cliWorkspaces(c *call) {
	if c.r.Method != "GET" {
		c.methodNotAllowed("GET")
		return
	}
	if !c.cliToken() {
		return
	}
	list := []any{}
	for _, w := range a.d.Store.Workspaces().Array("workspaces") {
		ws, _ := w.(*jsvalue.Object)
		id, _ := ws.Get("id")
		name, _ := ws.Get("name")
		dirs, _ := ws.Get("directories")
		o := jsvalue.NewObject("id", id, "name", name)
		if dirs != nil || ws.Has("directories") {
			o.Set("directories", dirs)
		}
		list = append(list, o)
	}
	c.reply(200, jsvalue.NewObject("workspaces", list))
}

func tabJSON(wsID string, tab *jsvalue.Object, paneID string) *jsvalue.Object {
	panelType, _ := tab.Get("panelType")
	p := providerOf(panelType)
	o := jsvalue.NewObject("tabId", tab.String("id"), "workspaceId", wsID)
	if paneID != "" {
		o.Set("paneId", paneID)
	}
	name, _ := tab.Get("name")
	o.Set("name", name)
	session, _ := tab.Get("sessionName")
	o.Set("sessionName", session)
	if panelType != nil || tab.Has("panelType") {
		o.Set("panelType", panelType)
	}
	o.Set("agentProviderId", providerID(p))
	o.Set("agentSessionId", agentSessionID(p, tab))
	return o
}

// GET, POST /api/cli/tabs
func (a *api) cliTabs(c *call) {
	if !c.cliToken() {
		return
	}
	switch c.r.Method {
	case "GET":
		var ids []string
		if ws, ok := c.qString("workspaceId"); ok && ws != "" {
			ids = []string{ws}
		} else {
			for _, w := range a.d.Store.Workspaces().Array("workspaces") {
				ids = append(ids, w.(*jsvalue.Object).String("id"))
			}
		}
		tabs := []any{}
		for _, id := range ids {
			if a.d.Store.Workspace(id) == nil {
				continue
			}
			l, err := a.d.Store.GetLayout(id, "")
			must(err)
			for _, pane := range state.Panes(l) {
				for _, t := range state.Tabs(pane) {
					o := tabJSON(id, t, "")
					tabs = append(tabs, o)
				}
			}
		}
		c.reply(200, jsvalue.NewObject("tabs", tabs))
	case "POST":
		a.cliTabCreate(c)
	default:
		c.methodNotAllowed("GET, POST")
	}
}

func (a *api) cliTabCreate(c *call) {
	if c.body == nil {
		panic("body is null")
	}
	wsID, _ := c.field("workspaceId")
	name, _ := c.field("name")
	panelType, hasType := c.field("panelType")
	prompt, _ := c.field("prompt")
	model, hasModel := c.field("model")
	effort, hasEffort := c.field("effort")
	if !jsvalue.Truthy(wsID) {
		c.fail(400, "workspaceId is required")
		return
	}
	id := stringOr(wsID)
	ws := a.d.Store.Workspace(id)
	if ws == nil {
		c.fail(404, "Workspace not found")
		return
	}
	l, err := a.d.Store.GetLayout(id, "")
	must(err)
	paneID := ""
	if ps := state.Panes(l); len(ps) > 0 {
		paneID = ps[0].String("id")
	}
	if paneID == "" {
		c.fail(500, "No pane available in workspace")
		return
	}
	resolved := "terminal"
	if hasType {
		s, _ := panelType.(string)
		valid := false
		for _, v := range validPanelTypes {
			valid = valid || s == v
		}
		if !valid {
			c.reply(400, jsvalue.NewObject("error", "Invalid panelType", "validPanelTypes", validPanelTypes))
			return
		}
		resolved = s
	}
	if u := a.availability(resolved); u != nil {
		c.unavailable(u)
		return
	}
	provider := agents.ProviderOfPanel(resolved)
	_, promptIsString := prompt.(string)
	wantsAgent := promptIsString || hasModel || hasEffort
	if wantsAgent && provider == "" {
		c.fail(400, "prompt, model and effort need a claude-code or codex-cli tab")
		return
	}
	if provider != "" && wantsAgent {
		if a.d.Agents == nil {
			c.fail(503, "Agent start is not available")
			return
		}
		launch, refusal := a.d.Agents.ValidateLaunch(provider, model, effort)
		if refusal != "" {
			c.fail(400, refusal)
			return
		}
		var promptArg any
		if promptIsString {
			promptArg = prompt
		}
		var tabName any
		if name != nil {
			tabName = name
		}
		started := a.d.Agents.Start(StartRequest{Provider: provider, WorkspaceID: id, Prompt: promptArg, Launch: launch, TabName: tabName})
		if started.ConversationID == "" {
			c.raw(started.Status, started.Body)
			return
		}
		launched := a.findTab(id, started.ConversationID)
		out := jsvalue.NewObject("tabId", started.ConversationID, "workspaceId", id, "paneId", nil, "sessionName", nil, "name", nil)
		if launched != nil {
			out.Set("paneId", launched.paneID)
			out.Set("sessionName", launched.tab.String("sessionName"))
			if n, _ := launched.tab.Get("name"); n != nil {
				out.Set("name", n)
			} else if name != nil {
				out.Set("name", name)
			}
		} else if name != nil {
			out.Set("name", name)
		}
		out.Set("panelType", resolved)
		out.Set("agentProviderId", provider)
		out.Set("agentSessionId", nil)
		out.Set("launched", true)
		c.reply(201, out)
		return
	}
	tab, err := a.d.Store.AddTab(id, paneID, state.TabOptions{Name: stringOr(name), Cwd: firstDir(ws), PanelType: resolved})
	var e *state.Error
	if errors.As(err, &e) && e.Status == 404 {
		c.fail(500, "Failed to create tab")
		return
	}
	if err != nil {
		agent.Log("error", "create tab failed", agent.F{K: "error", V: err.Error()})
		c.fail(500, err.Error())
		return
	}
	// The previous runtime left a CLI tab to the next poll; it is registered at once here.
	a.register(id, tab)
	pt, _ := tab.Get("panelType")
	c.reply(201, jsvalue.NewObject("tabId", tab.String("id"), "workspaceId", id, "paneId", paneID, "sessionName", tab.String("sessionName"),
		"name", tab.String("name"), "panelType", pt, "agentProviderId", nil, "agentSessionId", nil))
}

// GET, DELETE /api/cli/tabs/<tabId>
func (a *api) cliTab(c *call) {
	if !c.cliToken() {
		return
	}
	tabID := c.param("tabId")
	ws, ok := c.workspaceParam()
	if !ok {
		return
	}
	switch c.r.Method {
	case "GET":
		found := a.findTab(ws, tabID)
		if found == nil {
			c.fail(404, "Tab not found")
			return
		}
		c.reply(200, tabJSON(ws, found.tab, found.paneID))
	case "DELETE":
		found := a.findTab(ws, tabID)
		if found == nil {
			c.fail(404, "Tab not found")
			return
		}
		err := a.d.Store.RemoveTab(ws, found.paneID, tabID)
		var e *state.Error
		if err != nil && !errors.As(err, &e) {
			panic(err)
		}
		// The previous runtime relied on the reconciler; the status keeper hears it at once here.
		if err == nil && a.d.Status != nil {
			a.d.Status.RemoveTab(tabID)
		}
		c.reply(200, jsvalue.NewObject("ok", err == nil))
	default:
		c.methodNotAllowed("GET, DELETE")
	}
}

// cliTabPrelude is what result, send and status check first; nil when it already answered.
func (c *call) cliTabPrelude(method string) *tabLocation {
	if c.r.Method != method {
		c.methodNotAllowed(method)
		return nil
	}
	if !c.cliToken() {
		return nil
	}
	ws, ok := c.workspaceParam()
	if !ok {
		return nil
	}
	if method == "POST" {
		if c.body == nil {
			panic("body is null")
		}
		if content, _ := c.field("content"); !jsvalue.Truthy(content) {
			c.fail(400, "content is required")
			return nil
		}
	}
	found := c.a.findTab(ws, c.param("tabId"))
	if found == nil {
		c.fail(404, "Tab not found")
	}
	return found
}

// GET /api/cli/tabs/<tabId>/result: the visible screen of the tab.
func (a *api) cliTabResult(c *call) {
	found := c.cliTabPrelude("GET")
	if found == nil {
		return
	}
	session := found.tab.String("sessionName")
	if !a.d.Tmux.HasSession(session) {
		c.fail(409, "Tab session is not running")
		return
	}
	var content any
	if s, ok := a.d.Tmux.Capture(session); ok {
		content = s
	}
	c.reply(200, jsvalue.NewObject("content", content))
}

// POST /api/cli/tabs/<tabId>/send: text pasted into the tab; a bare agent command into its idle shell starts the
// agent the platform's way.
func (a *api) cliTabSend(c *call) {
	found := c.cliTabPrelude("POST")
	if found == nil {
		return
	}
	ws, _ := c.qString("workspaceId")
	v, _ := c.field("content")
	content, isStr := v.(string)
	if !isStr {
		panic("content is not a string")
	}
	session := found.tab.String("sessionName")
	if !a.d.Tmux.HasSession(session) {
		c.fail(409, "Tab session is not running")
		return
	}
	panelType, _ := found.tab.Get("panelType")
	args, bare := agents.BareAgentArgs(content, stringOr(panelType))
	provider := agents.ProviderOfPanel(stringOr(panelType))
	if bare && provider != "" && a.d.Agents != nil {
		if safe, _ := a.d.Tmux.CheckTerminalProcess(session); safe {
			line, err := a.d.Agents.LaunchLine(provider, ws, launchOf(a.d.Store.TabLaunch(found.tab)))
			must(err)
			must(a.d.Tmux.SendBracketedPaste(session, line+args))
			a.markLaunch(found.tab.String("id"), true)
			agent.Log("info", "tab send of a bare agent command started it the platform way", agent.F{K: "tabId", V: found.tab.String("id")})
			c.reply(200, jsvalue.NewObject("status", "sent", "launched", "platform"))
			return
		}
	}
	must(a.d.Tmux.SendBracketedPaste(session, content))
	c.reply(200, jsvalue.NewObject("status", "sent"))
}

// GET /api/cli/tabs/<tabId>/status
func (a *api) cliTabStatus(c *call) {
	found := c.cliTabPrelude("GET")
	if found == nil {
		return
	}
	ws, _ := c.qString("workspaceId")
	tabID := c.param("tabId")
	panelType, _ := found.tab.Get("panelType")
	p := providerOf(panelType)
	sid := agentSessionID(p, found.tab)
	session := found.tab.String("sessionName")
	if !a.d.Tmux.HasSession(session) {
		c.reply(200, jsvalue.NewObject("tabId", tabID, "workspaceId", ws, "alive", false, "agentProviderId", providerID(p),
			"agentSessionId", sid, "claudeSessionId", sid))
		return
	}
	var command any
	if cmd := a.d.Tmux.PaneCurrentCommand(session); cmd != "" {
		command = cmd
	}
	cliState, _ := found.tab.Get("cliState")
	hooksMissing := a.d.Status != nil && a.d.Status.HooksMissing(tabID)
	c.reply(200, jsvalue.NewObject("tabId", tabID, "workspaceId", ws, "alive", true, "command", command, "cliState", cliState,
		"agentProviderId", providerID(p), "agentSessionId", sid, "claudeSessionId", sid, "hooksMissing", hooksMissing))
}

// agentGuard is what the CLI's pane and vault routes check first: the token, POST with JSON, a session name, and the
// agent tab owning that session. It answers and returns "" when the request goes no further.
func (c *call) agentGuard(parse func() bool, notOwned string) string {
	if !c.cliToken() {
		return ""
	}
	c.header.Set("Cache-Control", "no-store")
	if c.r.Method != "POST" {
		c.header.Set("Allow", "POST")
		c.end(405)
		return ""
	}
	if !c.contentTypeJSON() {
		c.fail(415, "Нужен application/json")
		return ""
	}
	session, ok := c.str("session")
	if !ok || !agentSessionRe.MatchString(session) {
		c.fail(400, "Нужна сессия вкладки агента (tmux)")
		return ""
	}
	if !parse() {
		return ""
	}
	tabID := ""
	if c.a.d.Status != nil {
		tabID = c.a.d.Status.AgentTab(session)
	}
	if tabID == "" {
		c.fail(404, notOwned)
	}
	return tabID
}

const tooOften = "Слишком часто: не больше 12 команд панели в минуту"

// POST /api/cli/panel: the agent's command to the right pane of its own tab (the tab owning its tmux session).
func (a *api) cliPanel(c *call) {
	var command panel.Command
	tabID := c.agentGuard(func() bool {
		v, _ := c.field("command")
		var refusal string
		if command, refusal = a.d.Targets.Parse(v); refusal != "" {
			c.fail(400, refusal)
			return false
		}
		return true
	}, "Эта сессия не принадлежит вкладке агента: панелью управляет агент из своей вкладки")
	if tabID == "" {
		return
	}
	k := command.Kind
	switch k {
	case "open", "show":
		if !a.d.Panel.RateAllowed(tabID) {
			c.fail(429, tooOften)
			return
		}
		intent := a.d.Intents.Add(tabID, command.JSON())
		a.d.Sync.Broadcast(jsvalue.NewObject("type", "pane-intents", "tabId", tabID))
		applied := a.d.Sync.ClientCount() > 0 && a.d.Intents.WaitForApplied(tabID, intent.String("id"), a.d.PanelWait)
		what := "Панель покажется"
		if k == "open" {
			what = "Вкладка «" + command.Tab + "»"
			if command.Target != "" {
				what += " (" + command.Target + ")"
			}
		}
		msg := what + ": записано за разговором, откроется, когда человек придёт в разговор"
		if applied {
			msg = what + ": открыто у человека"
		}
		c.reply(200, jsvalue.NewObject("ok", true, "id", intent.String("id"), "applied", applied, "message", msg))
	case "status":
		intents := a.d.Intents.List(tabID)
		live := a.d.Panel.Dispatch(tabID, command, a.d.PanelWait)
		if live.Kind == "rate-limited" {
			c.fail(429, tooOften)
			return
		}
		out := jsvalue.NewObject("ok", true, "message", "Страница с этим разговором сейчас не открыта")
		if live.Kind == "answered" {
			out.Set("message", live.Result.Message)
			if live.Result.State != nil {
				out.Set("state", live.Result.State)
			}
		}
		lines := paneintents.Describe(intents)
		if len(lines) == 0 {
			lines = []string{"Команд панели за сутки не было"}
		}
		out.Set("intents", lines)
		c.reply(200, out)
	default:
		outcome := a.d.Panel.Dispatch(tabID, command, a.d.PanelWait)
		switch outcome.Kind {
		case "no-ui", "timeout":
			c.reply(200, jsvalue.NewObject("ok", true, "message", "Страница с этим разговором сейчас не открыта: скрывать нечего"))
		case "busy":
			c.fail(409, "Предыдущая команда панели ещё выполняется")
		case "rate-limited":
			c.fail(429, tooOften)
		default:
			r := outcome.Result
			out := jsvalue.NewObject("ok", r.OK)
			status := 200
			if r.OK {
				out.Set("message", r.Message)
			} else {
				out.Set("error", r.Message)
				status = 409
			}
			if r.State != nil {
				out.Set("state", r.State)
			}
			c.reply(status, out)
		}
	}
}

// POST /api/cli/vault: the agent asks the person's page to log in on a site with the safe.
func (a *api) cliVault(c *call) {
	origin := ""
	tabID := c.agentGuard(func() bool {
		v, _ := c.field("origin")
		if origin = vault.Origin(v); origin == "" {
			c.fail(400, "Адрес сайта: только https://хост[:порт], без пути и логина")
			return false
		}
		return true
	}, "Эта сессия не принадлежит вкладке агента: входить просит агент из своей вкладки")
	if tabID == "" {
		return
	}
	outcome := a.d.Vault.Dispatch(tabID, origin)
	status, body := vault.CLIReply(outcome)
	agent.Log("info", "vault login request", agent.F{K: "tabId", V: tabID}, agent.F{K: "origin", V: origin}, agent.F{K: "outcome", V: outcome.Kind})
	c.reply(status, body)
}
