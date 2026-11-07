package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The rig's data folder and registry root, for the tests that issue agent tokens against it.
var decRigDir, decRigRoot string

// rigDeps are the deps of the last appRig, for a test that reads the safe behind the routes.
var rigDeps deps

// rigRoutes are the routes of the last appRig, for a test that needs a real server (a socket).
var rigRoutes []gate.Route

// rigCreated are the chats the rig was asked to start (agents.run).
var rigCreated []wire.ConversationCreateCmd

func appRig(t *testing.T) (func(method, path, body string, h map[string]string) (int, string), *wire.Conversation) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cli-token"), []byte("tok"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := deps{DataDir: dir, Home: t.TempDir(), CLI: &gate.CLIToken{DataDir: dir, Headers: []string{"x-cli-token"}},
		SessionOK: func(r *http.Request) bool { return r.Header.Get("X-Test-Session") == "yes" }}
	reg := apps.Open(dir)
	decRigDir, decRigRoot, rigDeps = dir, reg.Root, d
	conv := &wire.Conversation{ID: "tab-1"}
	chats := appChats{
		ofSession: func(session string) string {
			if session == "hs-agent" {
				return "tab-1"
			}
			if session == "hs-second" {
				return "tab-2"
			}
			return ""
		},
		record: func(id string) (wire.Conversation, bool) {
			if id == "tab-2" {
				return wire.Conversation{ID: id, Apps: []string{"acme.demo"}}, true
			}
			return *conv, id == conv.ID
		},
		do: func(_ context.Context, cmd wire.Command) wire.CmdResult {
			if c, ok := cmd.(wire.ConversationCreateCmd); ok {
				rigCreated = append(rigCreated, c)
				id := "tab-new"
				return wire.CmdResult{R: wire.ResultAccepted, ID: &id}
			}
			m := cmd.(wire.ConversationAppsCmd)
			if bad := appsOK(reg)(m.Apps); len(m.Apps) > 0 && bad != "" {
				return wire.Refused(wire.RefuseNotFound)
			}
			conv.Apps = m.Apps
			return wire.Accepted(wire.ChannelConvs, 1)
		},
	}
	rigCreated = nil
	routes := appRouteList(d, reg, "0.17.0", chats)
	rigRoutes = routes
	return func(method, path, body string, h map[string]string) (int, string) {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		for k, v := range h {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		for _, rt := range routes {
			if rt.Match(req) {
				rt.Handler.ServeHTTP(w, req)
				return w.Code, w.Body.String()
			}
		}
		t.Fatalf("no route for %s", path)
		return 0, ""
	}, conv
}

func TestAppRoutes(t *testing.T) {
	call, conv := appRig(t)
	cli := map[string]string{"x-cli-token": "tok"}
	pkg := filepath.Join(t.TempDir(), "demo")
	if err := apps.Scaffold(pkg, "acme.demo", "Демо", "chat"); err != nil {
		t.Fatal(err)
	}

	if code, _ := call("GET", "/api/cli/apps", "", nil); code != http.StatusForbidden {
		t.Fatalf("no token: %d", code)
	}
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"demo"}`, cli); code != http.StatusBadRequest {
		t.Fatalf("relative folder: %d %s", code, body)
	}
	code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, cli)
	if code != http.StatusOK || !strings.Contains(body, `"id":"acme.demo"`) {
		t.Fatalf("install: %d %s", code, body)
	}
	agreeRig(t)

	// The page lists it without the folder.
	code, body = call("GET", platformAPI+"/installed-apps", "", map[string]string{"X-Test-Session": "yes"})
	var page struct {
		Apps []appView `json:"apps"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &page) != nil || len(page.Apps) != 1 || page.Apps[0].Dir != "" || page.Apps[0].Name != "Демо" {
		t.Fatalf("page list: %d %s", code, body)
	}
	if code, _ := call("GET", platformAPI+"/installed-apps", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("page without a session: %d", code)
	}

	// On and off in the chat of the calling tab.
	if code, body := call("POST", "/api/cli/apps/enable", `{"session":"hs-agent","id":"acme.demo","on":true}`, cli); code != http.StatusOK {
		t.Fatalf("enable: %d %s", code, body)
	}
	if len(conv.Apps) != 1 || conv.Apps[0] != "acme.demo" {
		t.Fatalf("chat apps: %v", conv.Apps)
	}
	if code, _ := call("POST", "/api/cli/apps/enable", `{"session":"hs-agent","id":"acme.nope","on":true}`, cli); code != http.StatusNotFound {
		t.Fatalf("enable an app not installed: %d", code)
	}
	if code, body := call("POST", "/api/cli/apps/enable", `{"session":"other","id":"acme.demo","on":true}`, cli); code != http.StatusNotFound || !strings.Contains(body, "no-chat") {
		t.Fatalf("enable outside a chat: %d %s", code, body)
	}
	// Off from the tab is the page's: refused while the chat has apps, and nothing changes.
	if code, body := call("POST", "/api/cli/apps/enable", `{"session":"hs-agent","id":"acme.demo","on":false}`, cli); code != http.StatusConflict || !strings.Contains(body, "use-page") || len(conv.Apps) != 1 {
		t.Fatalf("disable from the tab: %d %s %v", code, body, conv.Apps)
	}
	// explain: a read, layer by layer; the chat of the tab is the one named.
	if code, body := call("GET", "/api/cli/apps/explain?id=acme.demo&session=hs-agent", "", cli); code != http.StatusOK ||
		!strings.Contains(body, `"layer":"machine"`) || !strings.Contains(body, "on in this chat") {
		t.Fatalf("explain: %d %s", code, body)
	}
	if code, body := call("GET", "/api/cli/apps/explain?id=acme.nope", "", cli); code != http.StatusOK || !strings.Contains(body, "not installed") {
		t.Fatalf("explain an app not installed: %d %s", code, body)
	}
	if code, _ := call("GET", "/api/cli/apps/explain?id=acme.demo", "", nil); code != http.StatusForbidden {
		t.Fatalf("explain without a token: %d", code)
	}
	// The page's own command (conversation.apps) takes it out; off in a chat without apps is a no-op that still answers.
	conv.Apps = nil
	if code, _ := call("POST", "/api/cli/apps/enable", `{"session":"hs-agent","id":"acme.demo","on":false}`, cli); code != http.StatusOK || len(conv.Apps) != 0 {
		t.Fatalf("disable in a chat without apps: %d %v", code, conv.Apps)
	}

	// A broken package is refused with its problems.
	if err := os.WriteFile(filepath.Join(pkg, apps.ManifestPath), []byte(`{"id":"acme.demo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, body = call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, cli)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, `"path":"name"`) {
		t.Fatalf("broken package: %d %s", code, body)
	}
	agreeRig(t)

	if code, _ := call("POST", "/api/cli/apps/remove", `{"id":"acme.demo"}`, cli); code != http.StatusOK {
		t.Fatalf("remove: %d", code)
	}
	if code, _ := call("POST", "/api/cli/apps/remove", `{"id":"acme.demo"}`, cli); code != http.StatusNotFound {
		t.Fatalf("remove twice: %d", code)
	}
}

func TestAppViewsAndBridgeRoutes(t *testing.T) {
	call, conv := appRig(t)
	conv.Apps = []string{"acme.demo"} // tab-1 has the app on; tab-2 does in the rig; any other chat does not
	cli := map[string]string{"x-cli-token": "tok"}
	page := map[string]string{"X-Test-Session": "yes"}
	pkg := filepath.Join(t.TempDir(), "demo")
	if err := apps.Scaffold(pkg, "acme.demo", "Демо", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, cli); code != http.StatusOK {
		t.Fatalf("install: %d %s", code, body)
	}
	agreeRig(t)

	// The page's list names each view's entry on the view server, and the server's port.
	code, body := call("GET", platformAPI+"/installed-apps", "", page)
	if code != http.StatusOK || !strings.Contains(body, `"path":"/acme.demo/ui/index.html"`) || !strings.Contains(body, `"viewPort":`) || !strings.Contains(body, `"mcpServers":[]`) {
		t.Fatalf("list: %d %s", code, body)
	}

	// A view's session names the app, the target and the chat; the calls that follow carry only its token.
	open := func(body string) (int, string) { return call("POST", platformAPI+"/apps/session", body, page) }
	if code, body := open(`{"app":"acme.demo","view":"main","target":"sinopia.site"}`); code != http.StatusForbidden || !strings.Contains(body, "not_in_target") {
		t.Errorf("a target the app lacks: %d %s", code, body)
	}
	if code, body := open(`{"app":"acme.demo","view":"nope","target":"pane"}`); code != http.StatusForbidden {
		t.Errorf("a view that is not there: %d %s", code, body)
	}
	if code, _ := open(`{"app":"acme.none","view":"main","target":"pane"}`); code != http.StatusNotFound {
		t.Errorf("not installed: %d", code)
	}
	if code, _ := call("POST", platformAPI+"/apps/session", `{"app":"acme.demo","view":"main","target":"pane"}`, nil); code != http.StatusUnauthorized {
		t.Errorf("no page session: %d", code)
	}
	issue := func(conversation string) string {
		code, body := open(`{"app":"acme.demo","view":"main","target":"pane","conversation":"` + conversation + `"}`)
		var out struct{ Session, Path, Nonce string }
		if code != http.StatusOK || json.Unmarshal([]byte(body), &out) != nil || !strings.HasPrefix(out.Path, "/"+out.Session+"/acme.demo/ui/index.html") {
			t.Fatalf("session: %d %s", code, body)
		}
		if len(out.Nonce) < 16 {
			t.Fatalf("session without a nonce: %s", body)
		}
		return out.Session
	}
	one, two := issue("tab-1"), issue("tab-2")

	store := func(session, rest string) (int, string) {
		return call("POST", platformAPI+"/apps/storage", `{"session":"`+session+`",`+rest+`}`, page)
	}
	if code, _ := call("POST", platformAPI+"/apps/storage", `{"session":"`+one+`","op":"get","scope":"install","key":"k"}`, nil); code != http.StatusUnauthorized {
		t.Errorf("no page session: %d", code)
	}
	if code, body := store("nope", `"op":"get","scope":"install","key":"k"`); code != http.StatusBadGateway {
		t.Errorf("an unknown session: %d %s", code, body)
	}
	if code, body := store(one, `"op":"set","scope":"install","key":"k","value":{"n":1}`); code != http.StatusOK {
		t.Fatalf("set: %d %s", code, body)
	}
	if code, body := store(two, `"op":"get","scope":"install","key":"k"`); code != http.StatusOK || !strings.Contains(body, `"n":1`) {
		t.Errorf("get: %d %s", code, body)
	}
	// The space needs storage.space.write, which this app does not hold.
	if code, body := store(one, `"op":"set","scope":"space","key":"k","value":1`); code != http.StatusForbidden || !strings.Contains(body, "permission_denied") {
		t.Errorf("space write: %d %s", code, body)
	}
	// The chat's area is the chat the view is mounted in: the body cannot name another (an owner field is not read).
	if code, body := store(one, `"op":"set","scope":"chat","owner":"tab-2","key":"k","value":"from one"`); code != http.StatusOK {
		t.Fatalf("chat set: %d %s", code, body)
	}
	if code, body := store(two, `"op":"get","scope":"chat","key":"k"`); code != http.StatusOK || strings.Contains(body, "from one") {
		t.Errorf("another chat's area was written: %d %s", code, body)
	}
	if code, body := store(one, `"op":"get","scope":"chat","key":"k"`); code != http.StatusOK || !strings.Contains(body, "from one") {
		t.Errorf("own chat's area: %d %s", code, body)
	}
	// The stream of storage changes is the person's page only.
	if code, _ := call("GET", platformAPI+"/apps/events", "", nil); code != http.StatusUnauthorized {
		t.Errorf("events without a page session: %d", code)
	}
	// Versions: a get says the key's version, a set the new one; ifVersion keeps a stale write out, with its own code.
	if code, body := store(one, `"op":"get","scope":"install","key":"k"`); code != http.StatusOK || !strings.Contains(body, `"version":1`) {
		t.Errorf("get version: %d %s", code, body)
	}
	if code, body := store(one, `"op":"set","scope":"install","key":"k","value":{"n":2},"ifVersion":1`); code != http.StatusOK || !strings.Contains(body, `"version":2`) {
		t.Errorf("set ifVersion: %d %s", code, body)
	}
	if code, body := store(two, `"op":"set","scope":"install","key":"k","value":{"n":3},"ifVersion":1`); code != http.StatusConflict || !strings.Contains(body, `"error":"conflict"`) || !strings.Contains(body, `"version":2`) {
		t.Errorf("stale set: %d %s", code, body)
	}
	if code, body := store(one, `"op":"get","scope":"install","key":"k"`); !strings.Contains(body, `"n":2`) {
		t.Errorf("a refused write changed the key: %d %s", code, body)
	}
	if code, body := store(one, `"op":"set","scope":"chat","key":"big1","value":"`+strings.Repeat("x", 150<<10)+`"`); code != http.StatusOK {
		t.Fatalf("a big write that fits: %d %.80s", code, body)
	}
	if code, body := store(one, `"op":"set","scope":"chat","key":"big2","value":"`+strings.Repeat("x", 150<<10)+`"`); code != http.StatusRequestEntityTooLarge || !strings.Contains(body, `"error":"quota_exceeded"`) {
		t.Errorf("quota: %d %.80s", code, body)
	}
	// A view with no chat has no chat area, and none is bound to a document.
	bare := func() string {
		code, body := open(`{"app":"acme.demo","view":"main","target":"pane"}`)
		var out struct{ Session string }
		if code != http.StatusOK || json.Unmarshal([]byte(body), &out) != nil {
			t.Fatalf("session: %d %s", code, body)
		}
		return out.Session
	}()
	for _, scope := range []string{"chat", "doc"} {
		if code, _ := store(bare, `"op":"get","scope":"`+scope+`","key":"k"`); code != http.StatusBadRequest {
			t.Errorf("scope %s without a binding: %d", scope, code)
		}
	}
	// An app gets a chat only where it is turned on: a chat without it, or one that does not exist, is refused.
	if code, body := open(`{"app":"acme.demo","view":"main","target":"pane","conversation":"tab-9"}`); code != http.StatusForbidden || !strings.Contains(body, "not-on") {
		t.Errorf("a chat the app is not on in: %d %s", code, body)
	}
	if code, body := open(`{"app":"acme.demo","view":"main","target":"pane","conversation":"tab-1"}`); code != http.StatusOK {
		t.Errorf("a chat the app is on in: %d %s", code, body)
	}
	if code, _ := open(`{"app":"acme.demo","view":"main","target":"pane","conversation":"../x"}`); code != http.StatusBadRequest {
		t.Errorf("a conversation that is no name: %d", code)
	}
	// A live session is renewed under its own app, view, target and chat: whatever else the body says changes nothing.
	renew := func(body string) (int, string) { return call("POST", platformAPI+"/apps/session/renew", body, page) }
	if code, body := renew(`{"session":"` + one + `","app":"acme.other","view":"x","target":"sinopia.site","conversation":"tab-2"}`); code != http.StatusOK || !strings.Contains(body, `"expiresIn":`) {
		t.Errorf("renew: %d %s", code, body)
	}
	if code, body := store(one, `"op":"get","scope":"chat","key":"k"`); code != http.StatusOK || !strings.Contains(body, "from one") {
		t.Errorf("a renewed session kept its chat: %d %s", code, body)
	}
	if code, _ := renew(`{"session":"nosuch"}`); code == http.StatusOK {
		t.Error("an unknown session was renewed")
	}
	if code, _ := call("POST", platformAPI+"/apps/session/renew", `{"session":"`+one+`"}`, nil); code != http.StatusUnauthorized {
		t.Errorf("renew without a page session: %d", code)
	}
	// A tool of a server the package does not have.
	if code, body := call("POST", platformAPI+"/apps/tool", `{"session":"`+one+`","name":"bank.create"}`, page); code != http.StatusForbidden || !strings.Contains(body, "policy_blocked") {
		t.Errorf("tool: %d %s", code, body)
	}
}

func TestAppAskRoutes(t *testing.T) {
	dir := t.TempDir()
	d := deps{DataDir: dir, SessionOK: func(r *http.Request) bool { return r.Header.Get("X-Test-Session") == "yes" }}
	asks := apps.NewAsks()
	routes := appAskRoutes(d, asks)
	call := func(method, path, body string, session bool) (int, string) {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if session {
			req.Header.Set("X-Test-Session", "yes")
		}
		w := httptest.NewRecorder()
		for _, rt := range routes {
			if rt.Match(req) {
				rt.Handler.ServeHTTP(w, req)
				return w.Code, w.Body.String()
			}
		}
		t.Fatalf("no route for %s", path)
		return 0, ""
	}
	if code, _ := call("GET", platformAPI+"/apps/asks?conversation=c1", "", false); code != http.StatusUnauthorized {
		t.Fatalf("no session: %d", code)
	}
	if code, _ := call("GET", platformAPI+"/apps/asks", "", true); code != http.StatusBadRequest {
		t.Fatalf("no chat: %d", code)
	}
	got := make(chan string, 1)
	go func() {
		a, cancelled, _ := asks.Ask(context.Background(), "c1", "acme.demo", "Демо", "Какую?", []string{"АЗС", "Кафе"})
		if cancelled {
			a = "cancelled"
		}
		got <- a
	}()
	var list struct {
		Asks []apps.PendingAsk `json:"asks"`
	}
	for range 200 {
		_, body := call("GET", platformAPI+"/apps/asks?conversation=c1", "", true)
		if json.Unmarshal([]byte(body), &list) == nil && len(list.Asks) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(list.Asks) != 1 || list.Asks[0].Question != "Какую?" || list.Asks[0].AppName != "Демо" {
		t.Fatalf("list: %+v", list)
	}
	if _, body := call("GET", platformAPI+"/apps/asks?conversation=c2", "", true); !strings.Contains(body, `"asks":[]`) {
		t.Fatalf("another chat: %s", body)
	}
	id := list.Asks[0].ID
	if code, _ := call("POST", platformAPI+"/apps/asks/answer", `{"id":"`+id+`","answer":"Такси"}`, true); code != http.StatusBadRequest {
		t.Fatalf("bad answer: %d", code)
	}
	if code, _ := call("POST", platformAPI+"/apps/asks/answer", `{"id":"`+id+`","answer":"Кафе"}`, true); code != http.StatusOK {
		t.Fatalf("answer: %d", code)
	}
	if a := <-got; a != "Кафе" {
		t.Fatalf("the agent got %q", a)
	}
	if code, _ := call("POST", platformAPI+"/apps/asks/answer", `{"id":"`+id+`","answer":null}`, true); code != http.StatusNotFound {
		t.Fatalf("answer twice: %d", code)
	}
}

func TestAppEnableWarnsOverTheInstructionsBudget(t *testing.T) {
	call, conv := appRig(t)
	cli := map[string]string{"x-cli-token": "tok"}
	for i := 1; i <= 4; i++ {
		id := "acme.big" + strconv.Itoa(i)
		pkg := filepath.Join(t.TempDir(), "p")
		if err := apps.Scaffold(pkg, id, "Big", "chat"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkg, "instructions.md"), []byte(strings.Repeat("x", 15<<10)), 0o644); err != nil {
			t.Fatal(err)
		}
		if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, cli); code != http.StatusOK {
			t.Fatalf("install: %d %s", code, body)
		}
		agreeRig(t)
		code, body := call("POST", "/api/cli/apps/enable", `{"session":"hs-agent","id":"`+id+`","on":true}`, cli)
		if code != http.StatusOK {
			t.Fatalf("enable %s: %d %s", id, code, body)
		}
		// 15 KB each: the fourth (60 KB) passes 48 KB, and is on all the same.
		if got := strings.Contains(body, `"warnings"`); got != (i == 4) {
			t.Errorf("app %d: warnings=%v: %s", i, got, body)
		}
	}
	if len(conv.Apps) != 4 {
		t.Errorf("an app over the budget is still on: %v", conv.Apps)
	}
}

func TestWithoutSlotsStripsSlotViewsAndBranding(t *testing.T) {
	list := []appView{{
		ID:       "example-corp.slots",
		Targets:  []string{"pane", "settings", "branding"},
		Branding: &apps.Branding{Name: "Example"},
		Views: []appViewEntry{
			{ID: "main", Targets: []string{"pane"}},
			{ID: "s", Targets: []string{"settings"}},
			{ID: "both", Targets: []string{"pane", "onboarding"}},
		},
	}}
	got := withoutSlots(list)[0]
	if got.Branding != nil || len(got.Targets) != 1 || got.Targets[0] != "pane" {
		t.Fatalf("app: %+v", got)
	}
	if len(got.Views) != 2 || got.Views[0].ID != "main" || got.Views[1].ID != "both" || len(got.Views[1].Targets) != 1 {
		t.Fatalf("views: %+v", got.Views)
	}
}

func TestUISlotsFlag(t *testing.T) {
	t.Setenv("HARNESSLY_UI_SLOTS", "")
	if uiSlotsOn() {
		t.Fatal("slots are off by default")
	}
	t.Setenv("HARNESSLY_UI_SLOTS", "ON")
	if !uiSlotsOn() {
		t.Fatal("slots are on")
	}
}

func TestAppDecisionsRoutes(t *testing.T) {
	call, conv := appRig(t)
	page := map[string]string{"X-Test-Session": "yes"}
	conv.Apps = []string{"acme.demo"}
	line := `{"session":"hs-agent","agentSession":"s1","app":"acme.demo","tool":"Bash","rule":"Bash(git push*)","decision":"deny","reason":"no"}`
	person := map[string]string{"x-cli-token": "tok"}
	agentFor := func(launch, chat string, granted ...string) map[string]string {
		tok, err := (&gate.CLIToken{DataDir: decRigDir}).IssueAgent(launch, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		grant := filepath.Join(filepath.Dir(decRigRoot), "app-launch", launch)
		_ = os.MkdirAll(grant, 0o700)
		raw, _ := json.Marshal(map[string]any{"apps": granted, "conversation": chat})
		_ = os.WriteFile(filepath.Join(grant, "grant.json"), raw, 0o600)
		return map[string]string{"x-cli-token": tok}
	}
	agent := agentFor("launch-a", "tab-1", "acme.demo")

	if code, _ := call("POST", "/api/cli/apps/decision", line, nil); code != http.StatusForbidden {
		t.Fatalf("no token: %d", code)
	}
	if code, _ := call("POST", "/api/cli/apps/decision", line, person); code != http.StatusForbidden {
		t.Fatalf("the person's token: %d", code)
	}
	for name, c := range map[string]struct {
		body string
		want int
	}{
		"another chat's session": {strings.Replace(line, "hs-agent", "other", 1), http.StatusForbidden},
		"an app not on":          {strings.Replace(line, "acme.demo", "acme.other", 1), http.StatusForbidden},
		"an empty app":           {strings.Replace(line, `"app":"acme.demo"`, `"app":""`, 1), http.StatusBadRequest},
		"not a refusal":          {strings.Replace(line, `"deny"`, `"allow"`, 1), http.StatusBadRequest},
		"a rule of no one":       {strings.Replace(line, "Bash(git push*)", "Nope", 1), http.StatusBadRequest},
	} {
		if code, body := call("POST", "/api/cli/apps/decision", c.body, agent); code != c.want {
			t.Fatalf("%s: %d %s", name, code, body)
		}
	}
	// A launch not granted the app writes nothing, though the chat has it.
	if code, _ := call("POST", "/api/cli/apps/decision", line, agentFor("launch-b", "tab-1")); code != http.StatusForbidden {
		t.Fatalf("a launch without the grant: %d", code)
	}
	if code, _ := call("POST", "/api/cli/apps/decision", line, agentFor("launch-c", "", "acme.demo")); code != http.StatusForbidden {
		t.Fatalf("a launch whose grant names no chat: %d", code)
	}
	// The launch of chat B (tab-2) has the app, but the body is about chat A.
	if code, _ := call("POST", "/api/cli/apps/decision", line, agentFor("launch-d", "tab-2", "acme.demo")); code != http.StatusForbidden {
		t.Fatalf("a launch of chat B writing about chat A: %d", code)
	}
	q := platformAPI + "/apps/decisions?app=acme.demo&conversation=tab-1"
	_, body := call("GET", q, "", page)
	if !strings.Contains(body, `"decisions":[]`) {
		t.Fatalf("refused lines were written: %s", body)
	}
	if code, body := call("POST", "/api/cli/apps/decision", line, agent); code != http.StatusOK {
		t.Fatalf("journal: %d %s", code, body)
	}
	// Chat A's launch naming chat B's tab writes nothing.
	if code, _ := call("POST", "/api/cli/apps/decision", strings.Replace(line, "hs-agent", "hs-second", 1), agent); code != http.StatusForbidden {
		t.Fatalf("a second chat from one launch: %d", code)
	}
	if _, body := call("GET", platformAPI+"/apps/decisions?app=acme.demo&conversation=tab-2", "", page); !strings.Contains(body, `"decisions":[]`) {
		t.Fatalf("chat B's journal was written: %s", body)
	}

	if code, _ := call("GET", q, "", nil); code != http.StatusUnauthorized {
		t.Fatalf("no session: %d", code)
	}
	code, body := call("GET", q, "", page)
	var got struct {
		Decisions []apps.DecisionEntry `json:"decisions"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &got) != nil || len(got.Decisions) != 1 || got.Decisions[0].Rule != "Bash(git push*)" || got.Decisions[0].At == 0 {
		t.Fatalf("read: %d %s", code, body)
	}
	_, body = call("GET", platformAPI+"/apps/decisions?app=acme.demo&conversation=tab-9", "", page)
	if !strings.Contains(body, `"decisions":[]`) {
		t.Fatalf("a chat with none is an empty list: %s", body)
	}

	// Flood: past decisionRate a minute the line is refused and not written.
	for i := 1; i < decisionRate; i++ {
		if code, body := call("POST", "/api/cli/apps/decision", line, agent); code != http.StatusOK {
			t.Fatalf("line %d: %d %s", i, code, body)
		}
	}
	if code, _ := call("POST", "/api/cli/apps/decision", line, agent); code != http.StatusTooManyRequests {
		t.Fatalf("over the rate: %d", code)
	}
	_, body = call("GET", q, "", page)
	if err := json.Unmarshal([]byte(body), &got); err != nil || len(got.Decisions) != decisionRate {
		t.Fatalf("journal after the flood: %d %v", len(got.Decisions), err)
	}
}
