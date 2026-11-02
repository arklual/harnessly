package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The token of an agent's launch, over real HTTP: the routes that change the apps refuse it with 403
// agent_cannot_change_apps where the person's cli-token passes; the routes the agent's CLI uses take it; a forged or an
// expired one is no token.
func TestAgentTokenCannotChangeApps(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cli-token"), []byte("person"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cli := &gate.CLIToken{DataDir: dir, Headers: []string{"x-cli-token"}, Now: func() time.Time { return now }}
	d := deps{DataDir: dir, Home: t.TempDir(), CLI: cli, SessionOK: func(*http.Request) bool { return false }}
	reg := apps.Open(dir)
	chats := appChats{
		ofSession: func(string) string { return "" },
		record:    func(string) (wire.Conversation, bool) { return wire.Conversation{}, false },
		do: func(context.Context, wire.Command) wire.CmdResult {
			return wire.Refused(wire.RefuseUnsupported)
		},
	}
	routes := appRouteList(d, reg, "0.17.0", chats)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, rt := range routes {
			if rt.Match(r) {
				rt.Handler.ServeHTTP(w, r)
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	call := func(method, path, body, token string) (int, string) {
		req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("x-cli-token", token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(raw)
	}

	agent, err := cli.IssueAgent("launch-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(t.TempDir(), "demo")
	if err := apps.Scaffold(pkg, "acme.demo", "Демо", "chat"); err != nil {
		t.Fatal(err)
	}
	install := `{"dir":"` + pkg + `"}`
	if code, body := call("POST", "/api/cli/apps/install", install, agent); code != http.StatusForbidden || !strings.Contains(body, errAgentCannotChangeApps) {
		t.Fatalf("agent install: %d %s", code, body)
	}
	if list, _ := reg.List(); len(list) != 0 {
		t.Fatalf("the agent's install went through: %v", list)
	}
	if code, body := call("POST", "/api/cli/apps/install", install, "person"); code != http.StatusOK {
		t.Fatalf("the person's install: %d %s", code, body)
	}
	for _, p := range agentRefusedAppRoutes {
		if code, body := call("POST", p, `{"id":"acme.demo"}`, agent); code != http.StatusForbidden || !strings.Contains(body, errAgentCannotChangeApps) {
			t.Errorf("%s: %d %s", p, code, body)
		}
	}
	// Publishing: dev and beta are the agent's as in the hook; anything else, and any body the route cannot read as
	// exactly its fields, is refused to the agent.
	const publish = "/api/cli/apps/publish"
	dirField := `"dir":"` + pkg + `"`
	for name, body := range map[string]string{
		"stable":           `{` + dirField + `,"channel":"stable"}`,
		"the team":         `{` + dirField + `,"channel":"beta","team":true}`,
		"no channel":       `{` + dirField + `}`,
		"an unknown field": `{` + dirField + `,"channel":"beta","space":"sp1"}`,
		"trailing data":    `{` + dirField + `,"channel":"beta"} {"channel":"stable"}`,
		"not json":         `channel=beta`,
		"a second key":     `{` + dirField + `,"channel":"beta","channel":"stable"}`,
	} {
		if code, out := call("POST", publish, body, agent); code != http.StatusForbidden || !strings.Contains(out, errAgentCannotChangeApps) {
			t.Errorf("agent publish, %s: %d %s", name, code, out)
		}
	}
	for _, ch := range []string{"dev", "beta"} {
		if code, out := call("POST", publish, `{`+dirField+`,"channel":"`+ch+`"}`, agent); code == http.StatusForbidden {
			t.Errorf("agent publish to %s: %d %s", ch, code, out)
		}
	}
	if code, out := call("POST", publish, `{`+dirField+`,"channel":"stable","extra":1}`, "person"); code == http.StatusForbidden {
		t.Errorf("the person's publish is read as before: %d %s", code, out)
	}
	// What the agent's CLI reads stays open to it.
	if code, body := call("GET", "/api/cli/apps", "", agent); code != http.StatusOK || !strings.Contains(body, "acme.demo") {
		t.Fatalf("agent list: %d %s", code, body)
	}
	// A forged or an expired agent token is no token: the CLI routes answer it as they answer none.
	parts := strings.Split(agent, ".")
	forged := strings.Join([]string{parts[0], "launch-2", parts[2], parts[3]}, ".")
	if code, _ := call("GET", "/api/cli/apps", "", forged); code != http.StatusForbidden {
		t.Fatalf("forged: %d", code)
	}
	now = now.Add(2 * time.Hour)
	if code, _ := call("GET", "/api/cli/apps", "", agent); code != http.StatusForbidden {
		t.Fatalf("expired: %d", code)
	}
	if code, _ := call("POST", "/api/cli/apps/remove", `{"id":"acme.demo"}`, "person"); code != http.StatusOK {
		t.Fatalf("the person's remove: %d", code)
	}
}

// The tabs and panes the agent's CLI opens (`harnessly open`, `harnessly tab …`) take its token as the cli-token.
func TestCLITokenFirstTakesAgentTokens(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cli-token"), []byte("person"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent, err := (&gate.CLIToken{DataDir: dir}).IssueAgent("launch-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ok := cliTokenFirst(dir, []string{"x-cli-token", "x-other"})
	for token, want := range map[string]bool{"person": true, agent: true, agent + "x": false, "": false} {
		r := httptest.NewRequest("POST", "/api/cli/open", nil)
		r.Header.Set("x-cli-token", token)
		if got := ok(r); got != want {
			t.Errorf("%q: %v", token, got)
		}
	}
}
