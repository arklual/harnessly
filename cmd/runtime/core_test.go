package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"harnessly/internal/gate"
	"harnessly/internal/terminal"
	"harnessly/internal/tmux"
)

func TestCoreStartsServesTheOldRuntimeAndStops(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	dir, home := t.TempDir(), t.TempDir()
	socket := "hs-test-core-" + strconv.Itoa(os.Getpid())
	tm := tmux.NewConfig(tmux.Config{Names: tmux.Names{Socket: socket, Prefix: "hs-"}, Home: home})
	t.Cleanup(func() {
		tm.KillServer()
		_ = os.Remove(filepath.Join(os.TempDir(), "tmux-"+strconv.Itoa(os.Getuid()), socket))
	})
	tmuxNames = tm
	_ = os.WriteFile(filepath.Join(dir, "workspaces.json"), []byte(`{"workspaces":[{"id":"ws-a","name":"A","directories":["`+home+`"]}],"sidebarCollapsed":false,"sidebarWidth":240}`), 0o600)
	c := newCore(coreConfig{DataDir: dir, Home: home, Tmux: tm, Term: terminal.New(terminal.Config{Tmux: tm, Home: home}), MetaKey: "meta"})
	f, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: "127.0.0.1:1", DataDir: dir, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	token := func(r *http.Request) bool { return r.Header.Get("x-test-token") == "ok" }
	f.Add(c.routes(token, token)...)
	srv := httptest.NewServer(f)
	defer srv.Close()
	call := func(path, body string) (int, map[string]any) {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
		if body == "" {
			req.Method = http.MethodGet
		}
		req.Header.Set("x-test-token", "ok")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var m map[string]any
		_ = json.NewDecoder(res.Body).Decode(&m)
		return res.StatusCode, m
	}
	if code, _ := call("/api/internal/core/tabs", ""); code != http.StatusConflict {
		t.Fatalf("an off core answered %d", code)
	}
	c.start()
	defer c.stop()
	code, body := call("/api/internal/core/tabs", "")
	if code != 200 {
		t.Fatalf("%d %v", code, body)
	}
	tabs, _ := body["tabs"].([]any)
	if len(tabs) != 1 {
		t.Fatalf("the default layout's tab is not kept: %v", body)
	}
	tab := tabs[0].(map[string]any)
	session := tab["tmuxSession"].(string)
	if !tm.HasSession(session) {
		t.Fatalf("Init did not create the tab's session %q", session)
	}
	if code, body := call("/api/internal/core/layout", `{"op":"lastUserMessage","session":"`+session+`","message":"hello"}`); code != 200 {
		t.Fatalf("%d %v", code, body)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "workspaces", "ws-a", "layout.json"))
	if !strings.Contains(string(raw), `"lastUserMessage": "hello"`) {
		t.Fatalf("the operation did not reach the layout:\n%s", raw)
	}
	if code, body := call("/api/internal/core/layout", `{"op":"nope"}`); code != 400 {
		t.Fatalf("%d %v", code, body)
	}
	if code, _ := call("/api/internal/core/agent-launch", `{"tabId":"`+tab["tabId"].(string)+`"}`); code != 204 {
		t.Fatalf("agent-launch %d", code)
	}
	c.stop()
	if code, _ := call("/api/internal/core/tabs", ""); code != http.StatusConflict {
		t.Fatalf("a stopped core answered %d", code)
	}
}
