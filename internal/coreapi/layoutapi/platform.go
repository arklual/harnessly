package layoutapi

import (
	"regexp"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/panel"
	"github.com/ly-solutions/harnessly/internal/vault"
)

var (
	uuidRe       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	intentTabRe  = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	maxIntentIDs = 20
)

// POST /<platform>/state-changed: the machine agent wrote a layout or the workspace list the cluster changed.
func (a *api) stateChanged(c *call) {
	if c.r.Method != "POST" {
		c.methodNotAllowed("POST")
		return
	}
	if !a.d.CLIToken(c.r) {
		c.fail(403, "Forbidden")
		return
	}
	name, _ := c.str("name")
	if err := a.d.Store.StateChanged(name); err != nil {
		c.fail(400, "unknown state file")
		return
	}
	c.reply(200, jsvalue.NewObject("ok", true))
}

// POST, DELETE /<platform>/pane-shell: the shell of a conversation's right panel, in the conversation's folder.
func (a *api) paneShell(c *call) {
	if !a.d.Session(c.r) {
		c.end(403)
		return
	}
	var tabID string
	var isStr bool
	if c.r.Method == "DELETE" {
		tabID, isStr = c.qString("tabId")
	} else {
		tabID, isStr = c.str("tabId")
	}
	name, ok := "", false
	if isStr {
		name, ok = a.d.Tmux.PaneShellName(tabID, false)
	}
	if !ok {
		c.fail(400, msgNeedTab.String())
		return
	}
	legacy, hasLegacy := a.d.Tmux.PaneShellName(tabID, true)
	if !hasLegacy {
		legacy = name
	}
	if c.r.Method == "DELETE" {
		for _, n := range unique(name, legacy) {
			if a.d.Tmux.HasSession(n) {
				a.d.Tmux.KillSession(n)
			}
		}
		c.end(204)
		return
	}
	if c.r.Method != "POST" {
		c.header.Set("Allow", "POST, DELETE")
		c.end(405)
		return
	}
	if a.d.Tmux.HasSession(name) {
		c.reply(200, jsvalue.NewObject("sessionName", name))
		return
	}
	if legacy != name && a.d.Tmux.HasSession(legacy) {
		c.reply(200, jsvalue.NewObject("sessionName", legacy))
		return
	}
	var found *jsvalue.Object
	if tabID != "" {
		for _, wt := range a.d.Store.WorkspaceTabs() {
			if wt.Tab.String("id") == tabID {
				found = wt.Tab
				break
			}
		}
	}
	if found == nil {
		c.fail(404, msgNoConversation.String())
		return
	}
	cwd := a.d.Tmux.SessionCwd(found.String("sessionName"))
	if cwd == "" {
		cwd = found.String("cwd")
	}
	must(a.d.Tmux.CreateSession(name, 80, 24, a.d.Tmux.ResolveExistingDir(cwd)))
	c.reply(200, jsvalue.NewObject("sessionName", name))
}

func unique(a, b string) []string {
	if a == b {
		return []string{a}
	}
	return []string{a, b}
}

func intentsJSON(list []*jsvalue.Object) *jsvalue.Object {
	arr := make([]any, len(list))
	for i, it := range list {
		arr[i] = it
	}
	return jsvalue.NewObject("intents", arr)
}

// GET, POST /<platform>/pane-intents: what the agent asked the pane of a conversation, and which of it a page applied.
func (a *api) paneIntents(c *call) {
	if !a.d.Session(c.r) {
		c.fail(403, "Forbidden")
		return
	}
	c.header.Set("Cache-Control", "no-store")
	switch c.r.Method {
	case "GET":
		tabID, ok := c.qString("tabId")
		if !ok || !intentTabRe.MatchString(tabID) {
			c.fail(400, msgNeedTab.String())
			return
		}
		c.reply(200, intentsJSON(a.d.Intents.List(tabID)))
	case "POST":
		if !c.contentTypeJSON() {
			c.fail(415, msgNeedJSON.String())
			return
		}
		tabID, ok := c.str("tabId")
		if !ok || !intentTabRe.MatchString(tabID) {
			c.fail(400, msgNeedTab.String())
			return
		}
		v, _ := c.field("ids")
		ids, isArr := v.([]any)
		valid := isArr && len(ids) <= maxIntentIDs
		var strs []string
		for _, id := range ids {
			s, isStr := id.(string)
			valid = valid && isStr && uuidRe.MatchString(s)
			strs = append(strs, s)
		}
		if !valid {
			c.fail(400, msgNeedIntentIDs.String())
			return
		}
		device, _ := c.field("device")
		if device != "desktop" && device != "phone" {
			c.fail(400, msgBadDevice.String())
			return
		}
		c.reply(200, intentsJSON(a.d.Intents.MarkApplied(tabID, strs, device.(string))))
	default:
		c.header.Set("Allow", "GET, POST")
		c.end(405)
	}
}

// resultGuard is what both bridges' result routes check before reading the answer; false when it already answered.
func (c *call) resultGuard() bool {
	if !c.a.d.Session(c.r) {
		c.fail(403, "Forbidden")
		return false
	}
	c.header.Set("Cache-Control", "no-store")
	if c.r.Method != "POST" {
		c.header.Set("Allow", "POST")
		c.end(405)
		return false
	}
	if !c.contentTypeJSON() {
		c.fail(415, msgNeedJSON.String())
		return false
	}
	return true
}

// POST /<platform>/panel-result: the person's page reports what it did with a pane command.
func (a *api) panelResult(c *call) {
	if !c.resultGuard() {
		return
	}
	id, _ := c.str("id")
	result := panel.ParseResult(c.body)
	if !uuidRe.MatchString(id) || result == nil {
		c.fail(400, msgBadResult.String())
		return
	}
	if !a.d.Panel.Answer(id, *result) {
		c.fail(404, msgCommandGone.String())
		return
	}
	c.reply(200, jsvalue.NewObject("accepted", true))
}

// POST /<platform>/vault-result: the person's page reports how a login the agent asked for went.
func (a *api) vaultResult(c *call) {
	if !c.resultGuard() {
		return
	}
	id, _ := c.str("id")
	result := vault.ParseResult(c.body)
	if !uuidRe.MatchString(id) || result == nil {
		c.fail(400, msgBadResult.String())
		return
	}
	if !a.d.Vault.Answer(id, *result) {
		c.fail(404, msgVaultAskGone.String())
		return
	}
	c.reply(200, jsvalue.NewObject("accepted", true))
}

// POST /api/tmux/reset: every tab's session ends, and the stores and statuses start again.
func (a *api) tmuxReset(c *call) {
	if c.r.Method != "POST" {
		c.methodNotAllowed("POST")
		return
	}
	sessions := a.d.Tmux.ListSessions()
	agent.Log("info", "tmux reset requested", agent.F{K: "sessions", V: len(sessions)})
	a.d.Tmux.KillServer()
	a.d.Tmux.ApplyConfig("")
	if err := a.d.Store.Init(); err != nil {
		agent.Log("error", "tmux reset failed", agent.F{K: "error", V: err.Error()})
		c.fail(500, "tmux reset failed")
		return
	}
	if a.d.Agents != nil {
		a.d.Agents.AutoResume()
	}
	if a.d.Status != nil {
		a.d.Status.Rescan()
	}
	c.reply(200, jsvalue.NewObject("killed", len(sessions)))
}
