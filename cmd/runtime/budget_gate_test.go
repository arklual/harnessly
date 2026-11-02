package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/budget"
	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/launch"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// budgetCore is a core over a data folder with two Claude tabs: one launched on Opus, one on Sonnet. The folder's
// budget.json holds one stop for the machine's owner on Opus, in the mode given.
func budgetCore(t *testing.T, mode string) *core {
	t.Helper()
	t.Setenv("RUNTIME_LOCALE", "en")
	t.Setenv("RUNTIME_OWNER_EMAIL", "ann@x.ru")
	dir, home := t.TempDir(), t.TempDir()
	tab := func(id, session, model string) string {
		return `{"id":"` + id + `","sessionName":"` + session + `","name":"Claude Code","order":0,"cwd":"/x","panelType":"claude-code",` +
			`"meta":{"launch":{"model":"` + model + `","effort":null}}}`
	}
	write := func(name, body string) {
		p := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("workspaces.json", `{"workspaces":[{"id":"ws-a","name":"A","directories":["/x"]}],"groups":[]}`)
	write("workspaces/ws-a/layout.json", `{"root":{"type":"pane","id":"pane-a","tabs":[`+
		tab("t-opus", "s-opus", "claude-opus-4-1")+`,`+tab("t-sonnet", "s-sonnet", "claude-sonnet-4-5")+
		`],"activeTabId":"t-opus"},"activePaneId":"pane-a"}`)
	err := budget.Save(budget.Path(dir), hubproto.BudgetSet{Version: 1, Mode: mode, Entries: []hubproto.BudgetEntry{
		{User: "ann@x.ru", ModelPattern: "opus", Status: hubproto.BudgetStop, Pct: 100, RuleID: "r1", Reason: "Opus limit reached, resets Monday"}}})
	if err != nil {
		t.Fatal(err)
	}
	return newCore(coreConfig{DataDir: dir, Home: home, MetaKey: "meta"})
}

func budgetHook(c *core, token, query string) *httptest.ResponseRecorder {
	routes := c.budgetRoutes(func(r *http.Request) bool { return r.Header.Get("x-test-token") == "tok" })
	req := httptest.NewRequest("POST", "/api/budget/hook?"+query, nil)
	req.Header.Set("x-test-token", token)
	w := httptest.NewRecorder()
	if !routes[0].Match(req) {
		w.WriteHeader(http.StatusNotFound)
		return w
	}
	routes[0].Handler.ServeHTTP(w, req)
	return w
}

func TestBudgetHookRouteStopsTheChatOnTheModelItWasLaunchedWith(t *testing.T) {
	c := budgetCore(t, "stop")
	w := budgetHook(c, "tok", "provider=claude&event=UserPromptSubmit&session=s-opus")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"decision":"block"`) || !strings.Contains(w.Body.String(), "Opus limit reached, resets Monday.") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := budgetHook(c, "tok", "provider=claude&event=PreToolUse&session=s-opus"); !strings.Contains(w.Body.String(), `"continue":false`) {
		t.Errorf("%d %s", w.Code, w.Body)
	}
	// Sonnet is not in the entry; an unknown session has no model and so is not either.
	for _, session := range []string{"s-sonnet", "s-none"} {
		if w := budgetHook(c, "tok", "provider=claude&event=UserPromptSubmit&session="+session); w.Code != http.StatusNoContent || w.Body.Len() != 0 {
			t.Errorf("%s: %d %s", session, w.Code, w.Body)
		}
	}
	// The route is the CLI's: no token, no answer; a provider that is none is refused.
	if w := budgetHook(c, "bad", "provider=claude&event=UserPromptSubmit&session=s-opus"); w.Code != http.StatusUnauthorized {
		t.Errorf("%d", w.Code)
	}
	if w := budgetHook(c, "tok", "provider=gemini&event=UserPromptSubmit&session=s-opus"); w.Code != http.StatusBadRequest {
		t.Errorf("%d", w.Code)
	}
}

func TestBudgetHookRouteInWarnAndOffMode(t *testing.T) {
	c := budgetCore(t, "warn")
	w := budgetHook(c, "tok", "provider=claude&event=UserPromptSubmit&session=s-opus")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "systemMessage") || strings.Contains(w.Body.String(), "block") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if c.budgetAdmit("claude", &agents.Launch{Model: ptrTo("claude-opus-4-1")}) != "" {
		t.Error("a new chat refused in warn mode")
	}
	c = budgetCore(t, "off")
	if w := budgetHook(c, "tok", "provider=claude&event=UserPromptSubmit&session=s-opus"); w.Code != http.StatusNoContent {
		t.Errorf("%d %s", w.Code, w.Body)
	}
}

func ptrTo(s string) *string { return &s }

func TestNewChatIsRefusedInStopMode(t *testing.T) {
	c := budgetCore(t, "stop")
	opus, sonnet := ptrTo("claude-opus-4-1"), ptrTo("claude-sonnet-4-5")
	if r := c.budgetAdmit("claude", &agents.Launch{Model: opus}); !strings.HasPrefix(r, "Opus limit reached, resets Monday.") {
		t.Errorf("%q", r)
	}
	if c.budgetAdmit("claude", &agents.Launch{Model: sonnet}) != "" || c.budgetAdmit("claude", nil) != "" {
		t.Error("a chat the entry is not about was refused")
	}

	// The starter refuses the same chat, and the conversation's launcher says why before it makes a tab.
	c.newLauncher()
	res, err := c.launcher.Start(launch.Options{Provider: agents.Claude, Launch: &agents.Launch{Model: opus}})
	if err != nil || res.OK || res.Status != 403 || !strings.Contains(res.Body.String("error"), "Opus limit reached") {
		t.Fatalf("%+v %v", res, err)
	}
	c.agents = &coreAgents{}
	l := &v2Launcher{v: &v2Live{c: c}}
	_ = os.WriteFile(budget.Path(c.cfg.DataDir), []byte(`{"version":2,"mode":"stop","entries":[{"user":"ann@x.ru","status":"stop","reason":"All Claude chats are stopped","ruleId":"r2"}]}`), 0o600)
	err = l.Launch(convo.LaunchRequest{ID: "c1", Cwd: "/x", Provider: wire.ProviderClaude})
	if err == nil || !strings.Contains(err.Error(), "All Claude chats are stopped") {
		t.Fatalf("%v", err)
	}
	if l.starting.Load() != 0 {
		t.Error("a start began")
	}
}
