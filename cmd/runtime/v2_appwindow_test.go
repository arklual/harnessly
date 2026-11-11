package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// a window the person opens in the pane of a chat turns the app on in that chat, so its bridge, tools and
// context work with no `app enable`; the consent the app waits for still holds, and a view of another target does not
// turn anything on.
func TestOpeningAWindowTurnsTheAppOn(t *testing.T) {
	call, conv := appRig(t)
	cli := map[string]string{"x-cli-token": "tok"}
	page := map[string]string{"X-Test-Session": "yes"}
	pkg := filepath.Join(t.TempDir(), "demo")
	if err := apps.Scaffold(pkg, "acme.demo", "Демо", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, cli); code != http.StatusOK {
		t.Fatalf("install: %d %s", code, body)
	}
	open := func(target string) (int, string) {
		return call("POST", platformAPI+"/apps/session", `{"app":"acme.demo","view":"main","target":"`+target+`","conversation":"tab-1"}`, page)
	}
	if code, body := open("pane"); code == http.StatusOK || len(conv.Apps) != 0 {
		t.Fatalf("an app that waits for consent stays off: %d %s %v", code, body, conv.Apps)
	}
	agreeRig(t)
	if code, body := open("app"); code != http.StatusForbidden || !strings.Contains(body, "not-on") || len(conv.Apps) != 0 {
		t.Fatalf("only the pane's window turns the app on: %d %s %v", code, body, conv.Apps)
	}
	code, body := open("pane")
	var got struct{ Session, Path string }
	_ = json.Unmarshal([]byte(body), &got)
	if code != http.StatusOK || got.Session == "" || len(conv.Apps) != 1 || conv.Apps[0] != "acme.demo" {
		t.Fatalf("the pane's window: %d %s, apps %v", code, body, conv.Apps)
	}
	if code, _ := open("pane"); code != http.StatusOK || len(conv.Apps) != 1 {
		t.Fatalf("a second window of an app that is on: %d %v", code, conv.Apps)
	}
}

// The live hook tells of an open window once, again when its block changed, and when it was closed.
func TestLiveWindowNotice(t *testing.T) {
	reg := &apps.Registry{Root: filepath.Join(t.TempDir(), "apps")}
	installLiveApp(t, reg, "acme.cash", "Кэшбэк", `{}`)
	l := &liveApps{reg: reg}
	block := func(text string) []apps.WindowBriefOf {
		return []apps.WindowBriefOf{{App: "acme.cash", Name: "Кэшбэк", Text: text}}
	}
	if first := l.windowNotice("c1", block("## Open window: Кэшбэк (acme.cash)\nstep 1"), false); !strings.Contains(first, "# Open app windows") || !strings.Contains(first, "step 1") {
		t.Fatalf("first: %q", first)
	}
	// a service turn is told what changed but spends nothing.
	if service := l.windowNotice("c1", block("## Open window: Кэшбэк (acme.cash)\nstep 1b"), true); !strings.Contains(service, "step 1b") {
		t.Fatalf("service turn: %q", service)
	}
	if again := l.windowNotice("c1", block("## Open window: Кэшбэк (acme.cash)\nstep 1"), false); again != "" {
		t.Fatalf("told once: %q", again)
	}
	if other := l.windowNotice("c2", block("## Open window: Кэшбэк (acme.cash)\nstep 1"), false); other == "" {
		t.Fatal("another chat is told of its own window")
	}
	if changed := l.windowNotice("c1", block("## Open window: Кэшбэк (acme.cash)\nstep 2"), false); !strings.Contains(changed, "step 2") {
		t.Fatalf("changed: %q", changed)
	}
	if closed := l.windowNotice("c1", nil, false); !strings.Contains(closed, "# Closed app windows") || !strings.Contains(closed, "Кэшбэк (acme.cash)") {
		t.Fatalf("closed: %q", closed)
	}
	if quiet := l.windowNotice("c1", nil, false); quiet != "" {
		t.Fatalf("a closed window is told once: %q", quiet)
	}
}

// The first opening of an app's window in a chat gives an agent at rest one turn: once for the app in the chat, never
// to a busy agent, at most one a minute in a chat, and not at all when the deployment turned it off.
func TestAppOpenEvent(t *testing.T) {
	opens := apps.OpenWindowOpens(t.TempDir())
	now := time.Unix(1000, 0)
	e := &appOpenEvents{now: func() time.Time { return now }}
	rest := wire.Conversation{ID: "tab-c1", State: wire.AgentState{K: wire.StateIdle}}
	report := func(app string) apps.PageReport {
		return apps.PageReport{App: app, Context: json.RawMessage(`{"screen":{"title":"Форма"}}`)}
	}
	cmd, ok := e.opened(opens, rest, "Кэшбэк", report("acme.cash"))
	if !ok || cmd.ConversationID != "tab-c1" || cmd.Mode != wire.ModeQueue || !strings.HasPrefix(cmd.ClientID, appOpenClientPrefix) || cmd.Validate() != nil {
		t.Fatalf("the first opening: %v %+v", ok, cmd)
	}
	if !strings.Contains(cmd.Text, `<app-event app="acme.cash" event="opened">`) || !strings.Contains(cmd.Text, `on the screen "Форма"`) {
		t.Fatalf("text: %q", cmd.Text)
	}
	if _, ok := e.opened(opens, rest, "Кэшбэк", report("acme.cash")); ok {
		t.Fatal("once for an app in a chat")
	}
	if _, ok := e.opened(opens, rest, "Другое", report("acme.other")); ok {
		t.Fatal("at most one event a minute in a chat")
	}
	now = now.Add(appOpenEventEvery)
	if _, ok := e.opened(opens, rest, "Третье", report("acme.third")); !ok {
		t.Fatal("a minute later another app's first opening is told")
	}
	busy := wire.Conversation{ID: "tab-c2", State: wire.AgentState{K: wire.StateWorking}}
	if _, ok := e.opened(opens, busy, "Кэшбэк", report("acme.cash")); ok {
		t.Fatal("a busy agent is not interrupted")
	}
	busy.State.K = wire.StateIdle
	if _, ok := e.opened(opens, busy, "Кэшбэк", report("acme.cash")); ok {
		t.Fatal("an opening the agent was busy at is dropped, not kept for later")
	}
	if _, ok := e.opened(opens, wire.Conversation{ID: "tab-c3", State: wire.AgentState{K: wire.StateIdle}}, "Sinopia", report("harnessly.sinopia")); ok {
		t.Fatal("the host's own app gives no turn")
	}
	t.Setenv(appOpenEventEnv, "0")
	if _, ok := e.opened(opens, wire.Conversation{ID: "tab-c4", State: wire.AgentState{K: wire.StateIdle}}, "Кэшбэк", report("acme.cash")); ok {
		t.Fatal("turned off by the environment")
	}
}
