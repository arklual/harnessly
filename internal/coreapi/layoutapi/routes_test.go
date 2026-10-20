package layoutapi

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/state"
)

func TestAuthAndBodyParsing(t *testing.T) {
	r := newRig(t)
	a := r.do("", "GET", "/api/workspace", "")
	r.want(a, 401, `{"error":"Unauthorized"}`)
	if a.header.Get("Content-Type") != "application/json" || a.header.Get("ETag") != "" {
		t.Fatal(a.header)
	}
	r.want(r.do("t", "GET", "/api/workspace", ""), 200, "*")
	r.want(r.do("s", "GET", "/agents/api/workspace/", ""), 200, "*")
	// A wrong token under the first header decides, even with the right one under the old name.
	r.want(r.do("", "GET", "/api/workspace", "", "X-Tok", "nope", "X-Old-Tok", token), 401, "*")
	r.want(r.do("", "GET", "/api/workspace", "", "X-Old-Tok", token), 200, "*")

	a = r.do("s", "PATCH", "/api/workspace/active", `{"sidebarWidth":`)
	r.want(a, 400, "Invalid JSON")
	if a.header.Get("Content-Type") != "text/plain" {
		t.Fatal(a.header)
	}
	r.want(r.do("s", "PATCH", "/api/workspace/active", `{"x":"`+strings.Repeat("a", 1<<20)+`"}`), 413, "Body exceeded 1mb limit")
	r.want(r.do("s", "PATCH", "/api/workspace/active", ""), 200, `{"ok":true}`)

	a = r.do("s", "GET", "/api/workspace", "")
	if a.header.Get("Content-Type") != "application/json; charset=utf-8" || a.header.Get("ETag") == "" {
		t.Fatal(a.header)
	}
	b := r.do("s", "GET", "/api/workspace", "", "If-None-Match", a.header.Get("ETag"))
	r.want(b, 304, "")
	r.want(r.do("s", "GET", "/api/workspace", "", "If-None-Match", a.header.Get("ETag"), "Cache-Control", "no-cache"), 200, "*")

	// The CLIs' routes are open to the dispatcher and checked by their handlers.
	r.want(r.do("", "GET", "/api/cli/workspaces", ""), 403, `{"error":"Forbidden"}`)
	r.want(r.do("s", "GET", "/api/cli/workspaces", ""), 403, `{"error":"Forbidden"}`)
	// The page's routes want a session: the token alone is not a person.
	r.want(r.do("t", "GET", "/api/platform/pane-intents?tabId=x", ""), 403, `{"error":"Forbidden"}`)
	r.want(r.do("t", "POST", "/api/platform/pane-shell", `{"tabId":"x"}`), 403, "")
	// The route precedence: a fixed name beats a parameter.
	r.want(r.do("s", "PATCH", "/api/layout/pane/p/tabs/order", `{}`), 400, `{"error":"No workspace found"}`)
}

func TestWorkspaceRoutes(t *testing.T) {
	r := newRig(t)
	r.want(r.do("s", "POST", "/api/workspace", `{"directory":"/nowhere/at/all"}`), 400, `{"error":"Directory does not exist"}`)
	_ = os.WriteFile(filepath.Join(r.home, "file"), nil, 0o644)
	r.want(r.do("s", "POST", "/api/workspace", `{"directory":"`+filepath.Join(r.home, "file")+`"}`), 400, `{"error":"Please enter a directory path, not a file"}`)
	ws, _, tab := r.workspace("Alpha")
	if e := r.status.registered[tab]; e.CliState != "inactive" || e.WorkspaceID != ws || e.AgentProviderID != "" {
		t.Fatalf("%+v", e)
	}
	// Without a folder, the home.
	a := r.do("s", "POST", "/api/workspace", `{}`)
	r.want(a, 200, "*")
	if dirs := a.json(t).Array("directories"); dirs[0] != r.home {
		t.Fatal(dirs)
	}
	ws2 := a.json(t).String("id")

	r.want(r.do("s", "PATCH", "/api/workspace/"+ws, `{"groupId":5}`), 400, `{"error":"Invalid groupId"}`)
	r.want(r.do("s", "PATCH", "/api/workspace/ws-nope", `{"groupId":null}`), 404, `{"error":"Workspace not found"}`)
	r.want(r.do("s", "PATCH", "/api/workspace/"+ws, `{"groupId":null}`), 200, `{"ok":true}`)
	r.want(r.do("s", "PATCH", "/api/workspace/"+ws, `{"name":"  "}`), 400, `{"error":"name field required"}`)
	r.want(r.do("s", "PATCH", "/api/workspace/"+ws, `{}`), 400, `{"error":"name or groupId required"}`)
	a = r.do("s", "PATCH", "/api/workspace/"+ws, `{"name":" Beta ","groupId":null}`)
	r.want(a, 200, "*")
	if a.json(t).String("name") != "Beta" {
		t.Fatal(a.body)
	}

	g := r.do("s", "POST", "/api/workspace/group", `{"name":5}`)
	r.want(g, 200, "*")
	gid := g.json(t).String("id")
	if g.json(t).String("name") != "Group 1" {
		t.Fatal(g.body)
	}
	r.want(r.do("s", "PATCH", "/api/workspace/group/"+gid, `{"collapsed":"yes"}`), 400, `{"error":"collapsed must be boolean"}`)
	r.want(r.do("s", "PATCH", "/api/workspace/group/"+gid, `{"collapsed":true}`), 200, `{"ok":true}`)
	r.want(r.do("s", "PATCH", "/api/workspace/group/"+gid, `{"name":""}`), 400, `{"error":"name required"}`)
	r.want(r.do("s", "PATCH", "/api/workspace/group/"+gid, `{}`), 400, `{"error":"name or collapsed required"}`)
	r.want(r.do("s", "PATCH", "/api/workspace/group/grp-x", `{"collapsed":true}`), 404, `{"error":"Group not found"}`)
	r.want(r.do("s", "PATCH", "/api/workspace/group/"+gid, `{"name":" Ops "}`), 200, `{"id":"`+gid+`","name":"Ops","collapsed":true}`)
	r.want(r.do("s", "PATCH", "/api/workspace/group/reorder", `{"groupIds":"x"}`), 400, `{"error":"groupIds array required"}`)
	r.want(r.do("s", "PATCH", "/api/workspace/group/reorder", `{"groupIds":[]}`), 400, `{"error":"Invalid order"}`)
	r.want(r.do("s", "PATCH", "/api/workspace/group/reorder", `{"groupIds":["`+gid+`"]}`), 200, `{"ok":true}`)

	r.want(r.do("s", "PATCH", "/api/workspace/reorder", `{"items":[]}`), 400, `{"error":"items array required"}`)
	r.want(r.do("s", "PATCH", "/api/workspace/reorder", `{"items":[{"id":"`+ws+`"}]}`), 400, `{"error":"Invalid order"}`)
	r.want(r.do("s", "PATCH", "/api/workspace/reorder", `{"items":[{"id":"`+ws2+`","groupId":"`+gid+`"},{"id":"`+ws+`"}]}`), 200, `{"ok":true}`)
	r.want(r.do("s", "PATCH", "/api/workspace/reorder", `{"workspaceIds":["`+ws+`","`+ws2+`"]}`), 200, `{"ok":true}`)
	r.want(r.do("s", "PATCH", "/api/workspace/active", `{"activeWorkspaceId":"`+ws2+`","sidebarWidth":300}`), 200, `{"ok":true}`)
	view := r.do("s", "GET", "/api/workspace", "").json(t)
	if v, _ := view.Get("activeWorkspaceId"); v != ws2 || len(view.Array("groups")) != 1 {
		t.Fatal(string(jsvalue.Stringify(view, "")))
	}

	r.want(r.do("s", "GET", "/api/workspace/validate", ""), 400, `{"error":"directory parameter required"}`)
	r.want(r.do("s", "GET", "/api/workspace/validate?directory="+r.home, ""), 200, `{"valid":true,"suggestedName":"`+filepath.Base(r.home)+`"}`)
	r.want(r.do("s", "POST", "/api/workspace/validate", ""), 405, `{"error":"Method not allowed"}`)

	r.want(r.do("s", "DELETE", "/api/workspace/group/"+gid, ""), 204, "")
	r.want(r.do("s", "DELETE", "/api/workspace/group/"+gid, ""), 404, `{"error":"Group not found"}`)
	r.tm.took()
	r.want(r.do("s", "DELETE", "/api/workspace/"+ws, ""), 204, "")
	if calls := r.tm.took(); len(calls) != 1 || calls[0][0] != "kill" {
		t.Fatal(calls)
	}
	r.want(r.do("s", "DELETE", "/api/workspace/"+ws, ""), 404, `{"error":"Workspace not found"}`)
	a = r.do("s", "PUT", "/api/workspace", "")
	r.want(a, 405, `{"error":"Method not allowed"}`)
	if a.header.Get("Allow") != "GET, POST" {
		t.Fatal(a.header)
	}
}

func TestResumeIntoANewWorkspaceAndTab(t *testing.T) {
	r := newRig(t)
	const sid = "425055ec-bbc8-4e3c-87d0-36fa74c1cccb"
	dir := r.mkdir("proj")
	r.want(r.do("s", "POST", "/api/workspace", `{"directory":"`+dir+`","resumeSessionId":"`+sid+`","panelType":"terminal"}`), 400, `{"error":"Unknown panel type for resume"}`)
	r.want(r.do("s", "POST", "/api/workspace", `{"directory":"`+dir+`","resumeSessionId":"x"}`), 400, `{"error":"Invalid session ID format"}`)
	r.agents.unavailable["codex-cli"] = &Unavailable{Code: "agent-not-installed", ProviderID: "codex", ProviderDisplayName: "Codex", PanelType: "codex-cli"}
	r.want(r.do("s", "POST", "/api/workspace", `{"directory":"`+dir+`","resumeSessionId":"`+sid+`","panelType":"codex-cli"}`), 409,
		`{"error":"agent-not-installed","providerId":"codex","providerDisplayName":"Codex","panelType":"codex-cli","suggestedCommand":null}`)
	a := r.do("s", "POST", "/api/workspace", `{"directory":"`+dir+`","resumeSessionId":"`+sid+`"}`)
	r.want(a, 200, "*")
	ws := a.json(t).String("id")
	tab := state.Tabs(state.Panes(r.deps.Store.Layout(ws))[0])[0]
	if tab.String("panelType") != "claude-code" || tab.String("claudeSessionId") != sid {
		t.Fatal(string(jsvalue.Stringify(tab, "")))
	}
	if e := r.status.registered[tab.String("id")]; e.AgentProviderID != "claude" || e.AgentSessionID != sid {
		t.Fatalf("%+v", e)
	}
	waitFor(t, func() bool {
		for _, c := range r.tm.took() {
			if c[0] == "send" && c[2] == "claude --resume "+sid {
				return true
			}
		}
		return false
	})

	// A new tab continuing the same conversation takes the launch it was started with.
	_, _ = r.deps.Store.UpdateTabMeta(tab.String("sessionName"), jsvalue.NewObject("launch", jsvalue.NewObject("model", "opus", "effort", nil)))
	pane := state.Panes(r.deps.Store.Layout(ws))[0].String("id")
	b := r.do("s", "POST", "/api/layout/pane/"+pane+"/tabs?workspace="+ws, `{"resumeSessionId":"`+sid+`","panelType":"claude-code"}`)
	r.want(b, 200, "*")
	nt := b.json(t)
	if st := nt.Object("agentState"); st == nil || st.String("sessionId") != sid || nt.String("claudeSessionId") != sid {
		t.Fatal(b.body)
	}
	waitFor(t, func() bool {
		for _, c := range r.tm.took() {
			if c[0] == "send" && c[2] == "claude --resume "+sid+" --model opus" {
				return true
			}
		}
		return false
	})
	stored := state.Tabs(state.Panes(r.deps.Store.Layout(ws))[0])[1]
	if string(jsvalue.Stringify(stored.Object("meta"), "")) != `{"launch":{"model":"opus","effort":null}}` {
		t.Fatal(string(jsvalue.Stringify(stored, "")))
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLayoutRoutes(t *testing.T) {
	r := newRig(t)
	r.want(r.do("s", "GET", "/api/layout", ""), 400, `{"error":"No workspace found"}`)
	ws, pane, tab := r.workspace("Alpha")
	q := "?workspace=" + ws
	a := r.do("s", "GET", "/api/layout", "")
	r.want(a, 200, "*")
	if a.json(t).String("activePaneId") != pane {
		t.Fatal(a.body)
	}
	r.want(r.do("s", "PATCH", "/api/layout"+q, `{"activePaneId":"nope"}`), 404, `{"error":"Target not found"}`)
	r.want(r.do("s", "PATCH", "/api/layout"+q, `{"diffSettings":{"viewMode":"split"}}`), 200, "*")

	r.want(r.do("s", "POST", "/api/layout/pane"+q, `{"sourcePaneId":"nope","orientation":"vertical"}`), 404, `{"error":"Target pane not found"}`)
	split := r.do("s", "POST", "/api/layout/pane"+q, `{"sourcePaneId":"`+pane+`","orientation":"horizontal"}`)
	r.want(split, 200, "*")
	pane2 := split.json(t).String("activePaneId")
	created := r.do("s", "POST", "/api/layout/pane"+q, `{"cwd":"/tmp"}`)
	r.want(created, 200, "*")
	if created.json(t).Object("tab").String("cwd") != "/tmp" || state.Panes(r.deps.Store.Layout(ws)) == nil {
		t.Fatal(created.body)
	}
	r.want(r.do("s", "GET", "/api/layout/pane"+q, ""), 405, `{"error":"Method not allowed"}`)

	r.want(r.do("s", "PATCH", "/api/layout/pane/"+pane+q, `{"activeTabId":null}`), 404, `{"error":"Target not found"}`)
	r.want(r.do("s", "PATCH", "/api/layout/pane/"+pane+q, `{"activeTabId":"`+tab+`"}`), 200, "*")

	r.want(r.do("s", "POST", "/api/layout/pane/nope/tabs"+q, `{}`), 404, `{"error":"Pane not found"}`)
	r.agents.unavailable["claude-code"] = &Unavailable{Code: "agent-path-missing", ProviderID: "claude", ProviderDisplayName: "Claude Code", PanelType: "claude-code", SuggestedCommand: "claude-path"}
	r.want(r.do("s", "POST", "/api/layout/pane/"+pane+"/tabs"+q, `{"panelType":"claude-code"}`), 409, "*")
	delete(r.agents.unavailable, "claude-code")
	added := r.do("s", "POST", "/api/layout/pane/"+pane+"/tabs"+q, `{"name":" Run ","command":"npm test"}`)
	r.want(added, 200, "*")
	t2 := added.json(t).String("id")
	if added.json(t).String("name") != "Run" || r.status.launched[len(r.status.launched)-1] != t2+":false" {
		t.Fatal(added.body, r.status.launched)
	}

	r.want(r.do("s", "PATCH", "/api/layout/pane/"+pane+"/tabs/"+t2+q, `{}`), 400, `{"error":"No fields to update"}`)
	r.want(r.do("s", "PATCH", "/api/layout/pane/"+pane+"/tabs/"+t2+q, `{"terminalRatio":"x"}`), 400, `{"error":"terminalRatio must be a number"}`)
	r.want(r.do("s", "PATCH", "/api/layout/pane/"+pane+"/tabs/nope"+q, `{"name":"x"}`), 404, `{"error":"Tab not found"}`)
	patched := r.do("s", "PATCH", "/api/layout/pane/"+pane+"/tabs/"+t2+q, `{"terminalRatio":150,"cwd":"/body"}`)
	r.want(patched, 200, "*")
	r.want(r.do("s", "PUT", "/api/layout/pane/"+pane+"/tabs/"+t2+q, ""), 405, "*")

	r.want(r.do("s", "PATCH", "/api/layout/pane/"+pane+"/tabs/order"+q, `{"tabIds":[]}`), 400, `{"error":"tabIds array required"}`)
	r.want(r.do("s", "PATCH", "/api/layout/pane/"+pane+"/tabs/order"+q, `{"tabIds":["x"]}`), 404, `{"error":"Target not found"}`)
	r.want(r.do("s", "PATCH", "/api/layout/pane/"+pane+"/tabs/order"+q, `{"tabIds":["`+t2+`","`+tab+`"]}`), 200, "*")
	r.want(r.do("s", "POST", "/api/layout/pane/"+pane+"/tabs/"+t2+"/move"+q, `{"toPaneId":"`+pane2+`"}`), 400, `{"error":"toPaneId and toIndex required"}`)
	r.want(r.do("s", "POST", "/api/layout/pane/"+pane+"/tabs/"+t2+"/move"+q, `{"toPaneId":"nope","toIndex":0}`), 404, `{"error":"Target not found"}`)
	moved := r.do("s", "POST", "/api/layout/pane/"+pane+"/tabs/"+t2+"/move"+q, `{"toPaneId":"`+pane2+`","toIndex":"0"}`)
	r.want(moved, 200, "*")
	if moved.json(t).String("activePaneId") != pane2 {
		t.Fatal(moved.body)
	}

	// Restart: alive → nothing; gone → started again.
	r.want(r.do("s", "POST", "/api/layout/pane/"+pane+"/tabs/"+tab+q, `{}`), 200, `{"ok":true}`)
	r.tm.KillSession(state.Tabs(state.Panes(r.deps.Store.Layout(ws))[0])[0].String("sessionName"))
	r.tm.took()
	r.want(r.do("s", "POST", "/api/layout/pane/"+pane+"/tabs/"+tab+q, `{"command":"ls"}`), 200, `{"ok":true}`)
	if calls := r.tm.took(); len(calls) != 2 || calls[0][0] != "create" || calls[1][2] != "ls" {
		t.Fatal(calls)
	}
	r.want(r.do("s", "POST", "/api/layout/pane/"+pane+"/tabs/nope"+q, `{}`), 404, `{"error":"Tab not found"}`)

	r.want(r.do("s", "DELETE", "/api/layout/pane/"+pane+"/tabs/"+tab+q, ""), 204, "")
	if !reflect.DeepEqual(r.status.removed, []string{tab}) {
		t.Fatal(r.status.removed)
	}
	r.want(r.do("s", "DELETE", "/api/layout/pane/"+pane+"/tabs/"+tab+q, ""), 404, `{"error":"Tab not found"}`)
	r.want(r.do("s", "DELETE", "/api/layout/pane/nope"+q, ""), 404, `{"error":"Pane not found"}`)
	r.want(r.do("s", "DELETE", "/api/layout/pane/"+pane2+q, ""), 404, `{"error":"Pane not found"}`) // the last one
	third := r.do("s", "POST", "/api/layout/pane"+q, `{"sourcePaneId":"`+pane2+`","orientation":"vertical"}`).json(t).String("activePaneId")
	closed := r.do("s", "DELETE", "/api/layout/pane/"+third+q, "")
	r.want(closed, 200, "*")
	if closed.json(t).String("activePaneId") != pane2 {
		t.Fatal(closed.body)
	}
	r.want(r.do("s", "PUT", "/api/layout/pane/"+pane2+q, ""), 405, "*")

	// Without any workspace, closing a pane ends only the runtime's own sessions named in the body.
	r2 := newRig(t)
	r2.tm.sessions["tk-x"], r2.tm.sessions["mine"] = true, true
	r2.want(r2.do("s", "DELETE", "/api/layout/pane/p", `{"sessions":["tk-x","mine"]}`), 204, "")
	if !r2.tm.sessions["mine"] || r2.tm.sessions["tk-x"] {
		t.Fatal(r2.tm.sessions)
	}
}

func TestPlatformRoutes(t *testing.T) {
	r := newRig(t)
	ws, _, tab := r.workspace("Alpha")
	r.want(r.do("t", "POST", "/api/platform/state-changed", `{"name":"x"}`), 400, `{"error":"unknown state file"}`)
	r.want(r.do("s", "POST", "/api/platform/state-changed", `{"name":"workspaces.json"}`), 403, `{"error":"Forbidden"}`)
	r.sync.events = nil
	r.want(r.do("t", "POST", "/api/platform/state-changed", `{"name":"workspaces/`+ws+`/layout.json"}`), 200, `{"ok":true}`)
	r.want(r.do("t", "GET", "/api/platform/state-changed", ""), 405, "*")

	r.want(r.do("s", "POST", "/api/platform/pane-shell", `{"tabId":"a/b"}`), 400, `{"error":"Нужен tabId"}`)
	r.want(r.do("s", "POST", "/api/platform/pane-shell", `{"tabId":"tab-none"}`), 404, `{"error":"Разговор не найден"}`)
	r.want(r.do("s", "POST", "/api/platform/pane-shell", `{"tabId":"`+tab+`"}`), 200, `{"sessionName":"tk-shell-`+tab+`"}`)
	r.want(r.do("s", "POST", "/api/platform/pane-shell", `{"tabId":"`+tab+`"}`), 200, `{"sessionName":"tk-shell-`+tab+`"}`)
	r.tm.sessions["pt-shell-old"] = true
	r.want(r.do("s", "POST", "/api/platform/pane-shell", `{"tabId":"old"}`), 200, `{"sessionName":"pt-shell-old"}`)
	r.want(r.do("s", "DELETE", "/api/platform/pane-shell?tabId=old", ""), 204, "")
	if r.tm.sessions["pt-shell-old"] {
		t.Fatal("legacy shell kept")
	}
	a := r.do("s", "PUT", "/api/platform/pane-shell", `{"tabId":"x"}`)
	r.want(a, 405, "")
	if a.header.Get("Allow") != "POST, DELETE" {
		t.Fatal(a.header)
	}

	r.tm.sessions["tk-x"] = true
	r.agents.autoResumed = 0
	r.want(r.do("s", "POST", "/api/tmux/reset", ""), 200, "*")
	if r.agents.autoResumed != 1 || r.status.rescans != 1 {
		t.Fatal("reset did not start things again")
	}
	r.want(r.do("s", "GET", "/api/tmux/reset", ""), 405, `{"error":"Method not allowed"}`)
}

func TestCLITabRoutes(t *testing.T) {
	r := newRig(t)
	ws, pane, tab := r.workspace("Alpha")
	r.want(r.do("t", "GET", "/api/cli/workspaces", ""), 200, `{"workspaces":[{"id":"`+ws+`","name":"Alpha","directories":["`+filepath.Join(r.home, "Alpha")+`"]}]}`)
	r.want(r.do("t", "POST", "/api/cli/workspaces", ""), 405, "*")
	session := state.Tabs(state.Panes(r.deps.Store.Layout(ws))[0])[0].String("sessionName")
	list := r.do("t", "GET", "/api/cli/tabs", "")
	r.want(list, 200, `{"tabs":[{"tabId":"`+tab+`","workspaceId":"`+ws+`","name":"","sessionName":"`+session+`","agentProviderId":null,"agentSessionId":null}]}`)
	r.want(r.do("t", "GET", "/api/cli/tabs?workspaceId=ws-none", ""), 200, `{"tabs":[]}`)

	r.want(r.do("t", "POST", "/api/cli/tabs", `{}`), 400, `{"error":"workspaceId is required"}`)
	r.want(r.do("t", "POST", "/api/cli/tabs", `{"workspaceId":"ws-none"}`), 404, `{"error":"Workspace not found"}`)
	r.want(r.do("t", "POST", "/api/cli/tabs", `{"workspaceId":"`+ws+`","panelType":"x"}`), 400,
		`{"error":"Invalid panelType","validPanelTypes":["terminal","claude-code","codex-cli","agent-sessions","web-browser","diff"]}`)
	r.want(r.do("t", "POST", "/api/cli/tabs", `{"workspaceId":"`+ws+`","prompt":"hi"}`), 400, `{"error":"prompt, model and effort need a claude-code or codex-cli tab"}`)
	r.want(r.do("t", "POST", "/api/cli/tabs", `{"workspaceId":"`+ws+`","panelType":"claude-code","model":"bad"}`), 400, `{"error":"Недопустимое имя модели"}`)
	r.agents.start = StartResult{Status: 409, Body: []byte(`{"error":"busy"}`)}
	r.want(r.do("t", "POST", "/api/cli/tabs", `{"workspaceId":"`+ws+`","panelType":"codex-cli","prompt":"go"}`), 409, `{"error":"busy"}`)
	r.agents.start = StartResult{ConversationID: "tab-new"}
	r.want(r.do("t", "POST", "/api/cli/tabs", `{"workspaceId":"`+ws+`","panelType":"claude-code","prompt":"go","model":"opus","name":"N"}`), 201,
		`{"tabId":"tab-new","workspaceId":"`+ws+`","paneId":null,"sessionName":null,"name":"N","panelType":"claude-code","agentProviderId":"claude","agentSessionId":null,"launched":true}`)
	if s := r.agents.started[len(r.agents.started)-1]; s.Provider != "claude" || s.Prompt != "go" || *s.Launch.Model != "opus" || s.TabName != "N" {
		t.Fatalf("%+v", s)
	}
	// An agent tab starts its agent without a prompt too, as the page's new conversation does.
	r.want(r.do("t", "POST", "/api/cli/tabs", `{"workspaceId":"`+ws+`","panelType":"codex-cli"}`), 201, "*")
	if s := r.agents.started[len(r.agents.started)-1]; s.Provider != "codex" || s.Prompt != nil {
		t.Fatalf("%+v", s)
	}
	plain := r.do("t", "POST", "/api/cli/tabs", `{"workspaceId":"`+ws+`","name":"Shell"}`)
	r.want(plain, 201, "*")
	pt := plain.json(t)
	if pt.String("paneId") != pane || pt.String("panelType") != "terminal" || pt.String("name") != "Shell" {
		t.Fatal(plain.body)
	}
	newTab := pt.String("tabId")
	if _, ok := r.status.registered[newTab]; !ok {
		t.Fatal("a CLI tab is registered at once")
	}

	r.want(r.do("t", "GET", "/api/cli/tabs/"+tab, ""), 400, `{"error":"workspaceId is required"}`)
	r.want(r.do("t", "GET", "/api/cli/tabs/nope?workspaceId="+ws, ""), 404, `{"error":"Tab not found"}`)
	r.want(r.do("t", "GET", "/api/cli/tabs/"+tab+"?workspaceId="+ws, ""), 200,
		`{"tabId":"`+tab+`","workspaceId":"`+ws+`","paneId":"`+pane+`","name":"","sessionName":"`+session+`","agentProviderId":null,"agentSessionId":null}`)

	r.tm.screen = "line\n"
	r.want(r.do("t", "GET", "/api/cli/tabs/"+tab+"/result?workspaceId="+ws, ""), 200, `{"content":"line\n"}`)
	r.want(r.do("t", "POST", "/api/cli/tabs/"+tab+"/result?workspaceId="+ws, ""), 405, "*")
	r.want(r.do("t", "POST", "/api/cli/tabs/"+tab+"/send?workspaceId="+ws, `{"content":""}`), 400, `{"error":"content is required"}`)
	r.tm.took()
	r.want(r.do("t", "POST", "/api/cli/tabs/"+tab+"/send?workspaceId="+ws, `{"content":"echo hi"}`), 200, `{"status":"sent"}`)
	if calls := r.tm.took(); len(calls) != 1 || calls[0][0] != "paste" || calls[0][2] != "echo hi" {
		t.Fatal(calls)
	}
	// Control keys are keys, not text: Ctrl-C alone, or after a pasted line.
	r.want(r.do("t", "POST", "/api/cli/tabs/"+tab+"/send?workspaceId="+ws, `{"keys":["C-c"]}`), 200, `{"status":"sent"}`)
	if calls := r.tm.took(); len(calls) != 1 || strings.Join(calls[0], " ") != "keys "+session+" C-c" {
		t.Fatal(calls)
	}
	r.want(r.do("t", "POST", "/api/cli/tabs/"+tab+"/send?workspaceId="+ws, `{"content":"ls","keys":["Escape","Enter"]}`), 200, `{"status":"sent"}`)
	if calls := r.tm.took(); len(calls) != 2 || calls[0][0] != "paste" || strings.Join(calls[1], " ") != "keys "+session+" Escape Enter" {
		t.Fatal(calls)
	}
	for _, bad := range []string{`{"keys":["-t"]}`, `{"keys":[1]}`, `{"keys":"C-c"}`, `{"keys":["C c"]}`} {
		r.want(r.do("t", "POST", "/api/cli/tabs/"+tab+"/send?workspaceId="+ws, bad), 400, "*")
	}
	if calls := r.tm.took(); len(calls) != 0 {
		t.Fatal(calls)
	}
	alive := r.do("t", "GET", "/api/cli/tabs/"+tab+"/status?workspaceId="+ws, "")
	r.want(alive, 200, `{"tabId":"`+tab+`","workspaceId":"`+ws+`","alive":true,"command":"bash","cliState":null,"agentProviderId":null,"agentSessionId":null,"claudeSessionId":null,"hooksMissing":false}`)
	r.tm.KillSession(session)
	r.want(r.do("t", "GET", "/api/cli/tabs/"+tab+"/status?workspaceId="+ws, ""), 200,
		`{"tabId":"`+tab+`","workspaceId":"`+ws+`","alive":false,"agentProviderId":null,"agentSessionId":null,"claudeSessionId":null}`)
	r.want(r.do("t", "GET", "/api/cli/tabs/"+tab+"/result?workspaceId="+ws, ""), 409, `{"error":"Tab session is not running"}`)
	r.want(r.do("t", "DELETE", "/api/cli/tabs/"+newTab+"?workspaceId="+ws, ""), 200, `{"ok":true}`)
	if r.status.removed[len(r.status.removed)-1] != newTab {
		t.Fatal(r.status.removed)
	}
}

func TestBareAgentCommandStartsThePlatformWay(t *testing.T) {
	r := newRig(t)
	ws, pane, _ := r.workspace("Alpha")
	a := r.do("s", "POST", "/api/layout/pane/"+pane+"/tabs?workspace="+ws, `{"panelType":"claude-code"}`)
	r.want(a, 200, "*")
	tab := a.json(t)
	_, _ = r.deps.Store.UpdateTabMeta(tab.String("sessionName"), jsvalue.NewObject("launch", jsvalue.NewObject("model", "opus", "effort", nil)))
	r.tm.took()
	r.want(r.do("t", "POST", "/api/cli/tabs/"+tab.String("id")+"/send?workspaceId="+ws, `{"content":"claude --verbose\n"}`), 200, `{"status":"sent","launched":"platform"}`)
	calls := r.tm.took()
	if len(calls) != 1 || calls[0][2] != "claude-launch --ws "+ws+" --model opus --verbose" {
		t.Fatal(calls)
	}
	if r.status.launched[len(r.status.launched)-1] != tab.String("id")+":true" {
		t.Fatal(r.status.launched)
	}
	// Into a running agent the text goes as it is.
	r.tm.command = "claude"
	r.want(r.do("t", "POST", "/api/cli/tabs/"+tab.String("id")+"/send?workspaceId="+ws, `{"content":"claude"}`), 200, `{"status":"sent"}`)
}

func TestLayoutCwd(t *testing.T) {
	r := newRig(t)
	ws, _, _ := r.workspace("Alpha")
	session := state.Tabs(state.Panes(r.deps.Store.Layout(ws))[0])[0].String("sessionName")
	r.want(r.do("s", "GET", "/api/layout/cwd", ""), 400, `{"error":"session parameter required"}`)
	r.want(r.do("s", "GET", "/api/layout/cwd?session=tk-none", ""), 404, `{"error":"Session not found"}`)
	r.want(r.do("s", "GET", "/api/layout/cwd?session="+session, ""), 500, `{"error":"Failed to get CWD"}`)
	r.tm.cwd = "/work"
	r.want(r.do("s", "GET", "/api/layout/cwd?session="+session, ""), 200, `{"cwd":"/work","lastCommand":"npm run dev"}`)
}

func TestShadowOnlyForSideEffectFreeGETs(t *testing.T) {
	r := newRig(t)
	for path, want := range map[string]bool{"/api/workspace": true, "/api/cli/workspaces": true, "/api/workspace/validate": true,
		"/api/layout": false, "/api/cli/tabs": false, "/api/platform/pane-intents": false} {
		if got := r.shadow.Match(httptest.NewRequest("GET", path, nil)); got != want {
			t.Errorf("%s: %v", path, got)
		}
	}
	if r.shadow.Match(httptest.NewRequest("POST", "/api/workspace", nil)) {
		t.Fatal("POST in the shadow")
	}
}
