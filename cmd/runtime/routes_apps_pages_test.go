package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// The page reports a view's tools for a chat the app is on in; the runtime keeps only what the app's rights open, takes
// the agent's call to the page once and brings the view's answer back (HRN-808).
func TestAppPageRoutes(t *testing.T) {
	call, _ := appRig(t)
	cli := map[string]string{"x-cli-token": "tok"}
	page := map[string]string{"X-Test-Session": "yes"}
	pkg := filepath.Join(t.TempDir(), "demo")
	if err := apps.Scaffold(pkg, "acme.demo", "Демо", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(pkg, "harnessly", "app.json")
	raw, _ := os.ReadFile(manifest)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["permissions"] = append(m["permissions"].([]any), "tools.page")
	raw, _ = json.Marshal(m)
	_ = os.WriteFile(manifest, raw, 0o600)
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, cli); code != http.StatusOK {
		t.Fatalf("install: %d %s", code, body)
	}
	agreeRig(t)
	report := `{"conversation":"tab-2","apps":[{"app":"acme.demo","tools":[{"name":"fill","description":"Fill it"}],"context":{"step":1}},{"app":"acme.other","tools":[{"name":"x"}]}]}`
	if code, _ := call("POST", platformAPI+"/apps/page-tools", report, nil); code != http.StatusUnauthorized {
		t.Fatalf("no session: %d", code)
	}
	if code, body := call("POST", platformAPI+"/apps/page-tools", report, page); code != http.StatusOK || !strings.Contains(body, `"kept":1`) {
		t.Fatalf("report: %d %s", code, body)
	}
	offered := appPages.Offered("tab-2")
	if len(offered) != 1 || offered[0].App != "acme.demo" || len(offered[0].Tools) != 1 || offered[0].Context != nil {
		t.Fatalf("offered (no context.publish, no app that is off): %+v", offered)
	}
	got := make(chan apps.PageAnswer, 1)
	go func() {
		a, err := appPages.Call(context.Background(), "tab-2", "acme.demo", "fill", json.RawMessage(`{"name":"Ann"}`))
		if err != nil {
			t.Error(err)
		}
		got <- a
	}()
	var calls struct {
		Calls []apps.PageCall `json:"calls"`
	}
	for range 200 {
		_, body := call("GET", platformAPI+"/apps/page-calls?conversation=tab-2", "", page)
		if json.Unmarshal([]byte(body), &calls) == nil && len(calls.Calls) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(calls.Calls) != 1 || calls.Calls[0].Tool != "fill" {
		t.Fatalf("calls: %+v", calls)
	}
	id := calls.Calls[0].ID
	if code, _ := call("POST", platformAPI+"/apps/page-calls/answer", `{"conversation":"tab-1","id":"`+id+`","result":1}`, page); code != http.StatusNotFound {
		t.Fatalf("another chat answers: %d", code)
	}
	if code, _ := call("POST", platformAPI+"/apps/page-calls/answer", `{"conversation":"tab-2","id":"`+id+`","result":{"ok":true}}`, page); code != http.StatusOK {
		t.Fatalf("answer: %d", code)
	}
	if a := <-got; string(a.Result) != `{"ok":true}` {
		t.Fatalf("the agent got %+v", a)
	}
	if code, _ := call("POST", platformAPI+"/apps/page-tools", `{"conversation":"tab-9","apps":[]}`, page); code != http.StatusNotFound {
		t.Fatalf("unknown chat: %d", code)
	}
	// Sinopia is the host's own (HRN-808): its page offers tools and its screen in a chat it is on in no list of.
	builtin := `{"conversation":"tab-2","apps":[{"app":"harnessly.sinopia","tools":[{"name":"show_board","description":"Show"}],"context":{"board":{"number":2}}}]}`
	if code, body := call("POST", platformAPI+"/apps/page-tools", builtin, page); code != http.StatusOK || !strings.Contains(body, `"kept":1`) {
		t.Fatalf("builtin report: %d %s", code, body)
	}
	var sinopia *apps.PageReport
	for _, r := range appPages.Offered("tab-2") {
		if r.App == "harnessly.sinopia" {
			sinopia = &r
		}
	}
	if sinopia == nil || len(sinopia.Tools) != 1 || string(sinopia.Context) != `{"board":{"number":2}}` {
		t.Fatalf("builtin offered: %+v", sinopia)
	}
}

// A view the app's own server hosts gets a session and its address, not a path on the view server (delivery mode remote).
func TestRemoteViewSession(t *testing.T) {
	call, _ := appRig(t)
	cli := map[string]string{"x-cli-token": "tok"}
	page := map[string]string{"X-Test-Session": "yes"}
	pkg := filepath.Join(t.TempDir(), "demo")
	if err := apps.Scaffold(pkg, "acme.demo", "Демо", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(pkg, "harnessly", "app.json")
	raw, _ := os.ReadFile(manifest)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["views"] = append(m["views"].([]any), map[string]any{"id": "remote", "entry": "https://admin.acme.test/app", "targets": []string{"pane"}})
	m["network"] = map[string]any{"frames": []string{"admin.acme.test"}}
	raw, _ = json.Marshal(m)
	_ = os.WriteFile(manifest, raw, 0o600)
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, cli); code != http.StatusOK {
		t.Fatalf("install: %d %s", code, body)
	}
	agreeRig(t)
	code, body := call("POST", platformAPI+"/apps/session", `{"app":"acme.demo","view":"remote","target":"pane","conversation":""}`, page)
	var got struct{ Session, Nonce, URL, Path string }
	_ = json.Unmarshal([]byte(body), &got)
	if code != http.StatusOK || got.Session == "" || got.Nonce == "" || got.URL != "https://admin.acme.test/app" || got.Path != "" {
		t.Fatalf("remote session: %d %s", code, body)
	}
}
