package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
)

// the guard of an agent's launch asks the views that hear its calls; a stop wins, a context comes back, a
// strict app with no view stops the call, and the person's switch turns the hook off.
func TestAppHookRoute(t *testing.T) {
	// A hook marks the agent's window on the app (appPages.Driving): a page tools of its own keeps it from the tests after.
	shared := appPages
	pages := apps.NewPageTools()
	appPages = pages
	t.Cleanup(func() { appPages = shared })
	call, conv := appRig(t)
	conv.Apps = []string{"acme.demo"}
	page := map[string]string{"X-Test-Session": "yes"}
	pkg := filepath.Join(t.TempDir(), "demo")
	if err := apps.Scaffold(pkg, "acme.demo", "Демо", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(pkg, apps.ManifestPath))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["permissions"] = []string{"hooks.agent", "hooks.strict"}
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(filepath.Join(pkg, apps.ManifestPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, map[string]string{"x-cli-token": "tok"}); code != http.StatusOK {
		t.Fatalf("install: %d %s", code, body)
	}
	agreeRig(t)
	tok, err := (&gate.CLIToken{DataDir: decRigDir}).IssueAgent("launch-h", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	grant := filepath.Join(filepath.Dir(decRigRoot), "app-launch", "launch-h")
	_ = os.MkdirAll(grant, 0o700)
	g, _ := json.Marshal(map[string]any{"apps": []string{"acme.demo"}, "conversation": "tab-1"})
	_ = os.WriteFile(filepath.Join(grant, "grant.json"), g, 0o600)
	agent := map[string]string{"x-cli-token": tok}
	line := `{"session":"hs-agent","tool":"Bash","input":{"command":"git push"}}`
	verdict := func(h map[string]string) (int, apps.HookVerdict) {
		code, body := call("POST", "/api/cli/apps/hook", line, h)
		var v apps.HookVerdict
		_ = json.Unmarshal([]byte(body), &v)
		return code, v
	}
	if code, _ := verdict(map[string]string{"x-cli-token": "tok"}); code != http.StatusForbidden {
		t.Fatalf("the person's token: %d", code)
	}
	// Strict, and no view hears: the call is stopped by the app.
	if code, v := verdict(agent); code != http.StatusOK || !v.Deny || v.Name != "Демо" {
		t.Fatalf("strict with no view: %d %+v", code, v)
	}
	// A view hears and stops it, in its words.
	if err := appPages.Report("tab-1", []apps.PageReport{{App: "acme.demo", Hooks: true}}); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			for _, c := range pages.Take("tab-1") {
				var got struct {
					Tool  string
					Input map[string]string
				}
				_ = json.Unmarshal(c.Arguments, &got)
				word := `{"context":"main is protected"}`
				if got.Input["command"] == "git push" {
					word = `{"decision":"deny","reason":"no pushes on Friday"}`
				}
				_ = pages.Answer("tab-1", c.ID, apps.PageAnswer{Result: json.RawMessage(word)})
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	if code, v := verdict(agent); code != http.StatusOK || !v.Deny || v.Reason != "no pushes on Friday" {
		t.Fatalf("a view's stop: %d %+v", code, v)
	}
	code, body := call("POST", "/api/cli/apps/hook", strings.Replace(line, "git push", "ls", 1), agent)
	if code != http.StatusOK || !strings.Contains(body, "[Демо] main is protected") || strings.Contains(body, `"deny":true`) {
		t.Fatalf("a view's context: %d %s", code, body)
	}
	// The person turns the hook off: the app is not asked, strict or not.
	if code, body := call("POST", platformAPI+"/apps/hooks-off", `{"id":"acme.demo","off":true}`, agent); code < 400 {
		t.Fatalf("the agent turned a hook off: %d %s", code, body)
	}
	if code, body := call("POST", platformAPI+"/apps/hooks-off", `{"id":"acme.demo","off":true}`, page); code != http.StatusOK {
		t.Fatalf("hooks off: %d %s", code, body)
	}
	if code, v := verdict(agent); code != http.StatusOK || v.Deny {
		t.Fatalf("a hook turned off still stops: %d %+v", code, v)
	}
	list, _ := apps.Open(decRigDir).ReadJournal("acme.demo", 50)
	var denied, off bool
	for _, e := range list {
		denied = denied || (e.What == "hook.deny" && strings.Contains(e.Detail, "Friday"))
		off = off || e.What == "hooks.off"
	}
	if !denied || !off {
		t.Fatalf("the journal: %+v", list)
	}
}
