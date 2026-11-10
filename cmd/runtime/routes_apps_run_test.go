package main

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// HRN-851: a view runs commands of the chat's folder only with terminal.run and inside its masks, processes only with
// process, in a chat the app is on in; every run is journaled.
func TestAppRunRoute(t *testing.T) {
	call, conv := appRig(t)
	conv.Apps = []string{"acme.demo"}
	conv.Cwd = t.TempDir()
	cli := map[string]string{"x-cli-token": "tok"}
	page := map[string]string{"X-Test-Session": "yes"}
	pkg := filepath.Join(t.TempDir(), "demo")
	if err := apps.Scaffold(pkg, "acme.demo", "Демо", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(pkg, apps.ManifestPath))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["permissions"] = []string{"terminal.run:echo *"}
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(filepath.Join(pkg, apps.ManifestPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, cli); code != http.StatusOK {
		t.Fatalf("install: %d %s", code, body)
	}
	agreeRig(t)
	code, body := call("POST", platformAPI+"/apps/session", `{"app":"acme.demo","view":"main","target":"pane","conversation":"tab-1"}`, page)
	var sess struct{ Session string }
	if code != http.StatusOK || json.Unmarshal([]byte(body), &sess) != nil {
		t.Fatalf("session: %d %s", code, body)
	}
	run := func(rest string) (int, string) {
		return call("POST", platformAPI+"/apps/run", `{"session":"`+sess.Session+`",`+rest+`}`, page)
	}
	if code, _ := call("POST", platformAPI+"/apps/run", `{"session":"`+sess.Session+`","op":"run","command":"echo hi"}`, nil); code != http.StatusUnauthorized {
		t.Errorf("no page session: %d", code)
	}
	for _, c := range []struct{ name, rest, want string }{
		{"outside the mask", `"op":"run","command":"rm -rf ."`, "masks"},
		{"the shell", `"op":"run","command":"echo hi; id"`, "shell"},
		{"a process without process", `"op":"start","command":"echo hi"`, "process"},
		{"the pane without process", `"op":"preview","port":5173`, "process"},
		{"cwd out of the folder", `"op":"run","command":"echo hi","cwd":"../x"`, "cwd"},
	} {
		if code, body := run(c.rest); code < 400 || !strings.Contains(body, c.want) {
			t.Errorf("%s: %d %s", c.name, code, body)
		}
	}
	if _, err := exec.LookPath("bwrap"); err == nil && appBoundary().Ready(t.TempDir()) == nil {
		code, body := run(`"op":"run","command":"echo hi"`)
		var res apps.RunResult
		if code != http.StatusOK || json.Unmarshal([]byte(body), &res) != nil || res.Code != 0 || res.Output != "hi\n" {
			t.Errorf("a command of the mask: %d %s", code, body)
		}
	}
	list, err := apps.Open(decRigDir).ReadJournal("acme.demo", 50)
	if err != nil {
		t.Fatal(err)
	}
	var refused bool
	for _, e := range list {
		refused = refused || (e.What == "terminal.run" && !e.OK && strings.Contains(e.Detail, "rm -rf"))
	}
	if !refused {
		t.Errorf("a refused command is journaled: %+v", list)
	}
	conv.Apps = nil
	if code, body := run(`"op":"run","command":"echo hi"`); code != http.StatusForbidden || !strings.Contains(body, "not-on") {
		t.Errorf("an app turned off in the chat: %d %s", code, body)
	}
}
