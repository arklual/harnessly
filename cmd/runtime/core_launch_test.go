package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/preflight"
	"github.com/ly-solutions/harnessly/internal/terminal"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

// A conversation started through the gate with the launch slice on: the core makes the tab on a real tmux server, types
// the launch line with the prompt, and its keeper holds the tab as launching. With the slice off the route is the old
// runtime's again.
func TestLaunchSliceStartsAConversation(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	dir, home := t.TempDir(), t.TempDir()
	socket := "hs-test-launch-" + strconv.Itoa(os.Getpid())
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}
	tm := tmux.NewConfig(tmux.Config{Names: tmux.Names{Socket: socket, Prefix: "hs-"}, ConfigFile: "/dev/null", Home: home,
		Env: []string{"HOME=" + home, "SHELL=" + shell}})
	t.Cleanup(func() {
		tm.KillServer()
		_ = os.Remove(filepath.Join(os.TempDir(), "tmux-"+strconv.Itoa(os.Getuid()), socket))
	})
	tmuxNames = tm
	agentBin := filepath.Join(home, "fake-agent")
	_ = os.WriteFile(agentBin, []byte("#!/bin/sh\necho \"fake agent started: $*\"\necho \"key=[$HL_TEST_KEY]\"\nsleep 30\n"), 0o700)
	// A machine agent's env file: what the hub delivered reaches the launched CLI, on a start and on a resume.
	envDir := filepath.Join(dir, "agent-env")
	_ = os.MkdirAll(envDir, 0o700)
	_ = os.WriteFile(filepath.Join(envDir, "all.env"), []byte("HL_TEST_KEY='it'\\''s secret'\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "workspaces.json"), []byte(`{"workspaces":[{"id":"ws-a","name":"A","directories":["`+home+`"]}],"sidebarCollapsed":false,"sidebarWidth":240}`), 0o600)
	mode := filepath.Join(dir, "state-mode.json")
	_ = os.WriteFile(mode, []byte(`{"owned":false,"goSlices":["core","launch"]}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "cli-token"), []byte("tok"), 0o600)

	ag := newCoreAgents(coreAgentsConfig{DataDir: dir, Home: home, ConfigFile: filepath.Join(dir, "config.json"),
		ClaudeDir: filepath.Join(home, ".claude"), CodexDir: filepath.Join(home, ".codex"), DefaultsKey: "launchDefaults", Launcher: agentBin, EnvDir: envDir})
	ag.preflight = func() preflight.Result {
		return preflight.Result{Claude: preflight.Claude{Installed: true}, Codex: preflight.Codex{Installed: true}}
	}
	c := newCore(coreConfig{DataDir: dir, Home: home, Tmux: tm, Term: terminal.New(terminal.Config{Tmux: tm, Home: home}), MetaKey: "meta",
		Agents: ag, Launch: launchConfig{EnvPrefix: "HARNESS_", DataDirLabel: "~/data", FirstWorkspaceName: "Workspace"}})
	f, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: "127.0.0.1:1", DataDir: dir, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	ag.launchOn = func() bool { return f.SliceOn("launch") }
	token := func(r *http.Request) bool { return r.Header.Get("x-test-token") == "tok" }
	session := func(r *http.Request) bool {
		ck, err := r.Cookie("session-token")
		return err == nil && ck.Value == "good"
	}
	d := deps{Home: home, DataDir: dir, TokenHeaders: []string{"x-test-token"}, SessionOK: session}
	f.Add(c.routes(token, token)...)
	f.Add(c.launchRoutes(f, d, func(*http.Request) bool { return false })...)
	f.Add(c.layoutRoutes(f, d)...)
	srv := httptest.NewServer(f)
	defer srv.Close()
	// An answer of the old runtime, which is not there: the gate drops the connection (-1).
	post := func(path, body string) (int, *jsvalue.Object) {
		t.Helper()
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session-token", Value: "good"})
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return -1, nil
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		v, _ := jsvalue.Parse(raw)
		o, _ := v.(*jsvalue.Object)
		return res.StatusCode, o
	}

	if code, _ := post(platformAPI+"/conversations", `{"provider":"claude"}`); code != -1 {
		t.Fatalf("before the core runs the route is the old runtime's: %d", code)
	}
	c.start()
	defer c.stop()
	m := c.manager()

	code, conv := post(platformAPI+"/conversations", `{"provider":"claude","workspaceId":"ws-a","prompt":"hello there"}`)
	if code != http.StatusCreated || conv.String("title") != "hello there" || conv.String("provider") != "claude" {
		t.Fatalf("%d %s", code, jsvalue.Stringify(conv, ""))
	}
	id := conv.String("id")
	info, ok := m.Tab(id)
	if !ok || !info.Launching || info.PanelType != "claude-code" || info.Provider != "claude" || !strings.HasPrefix(info.TmuxSession, "hs-ws-a-") {
		t.Fatalf("the keeper does not hold the tab as launching: %+v %v", info, ok)
	}
	var screen string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		screen, _ = tm.Capture(info.TmuxSession)
		if strings.Contains(screen, "key=[") {
			break
		}
	}
	flat := paneText(screen)
	// The line is typed as `sh -c '…'`, so each quote of the sh line shows as '\''.
	if !strings.Contains(flat, paneText(`exec env DISABLE_AUTOUPDATER=1 '\''`+agentBin+`'\'' --settings `+filepath.Join(dir, "hooks.json"))) ||
		!strings.Contains(flat, paneText("fake agent started:")) || !strings.Contains(flat, paneText(`-- "$(printf %b '\''hello there'\'')"`)) ||
		!strings.Contains(flat, paneText("key=[it's secret]")) {
		t.Fatalf("the launch line was not typed and run:\n%s", screen)
	}
	tab := func() *jsvalue.Object {
		for _, wt := range c.store.WorkspaceTabs() {
			if wt.Tab.String("id") == id {
				return wt.Tab
			}
		}
		return nil
	}()
	if tab == nil || tab.String("panelType") != "claude-code" || tab.String("lastUserMessage") != "hello there" || tab.String("name") != "Claude Code" {
		t.Fatalf("the layout does not hold the tab: %s", jsvalue.Stringify(tab, ""))
	}

	// A tmux reset resumes the agent that was working, here: the tab's session is made again and the resume line typed.
	const sid = "425055ec-bbc8-4e3c-87d0-36fa74c1cccb"
	if err := c.store.UpdateTabAgentSessionID(info.TmuxSession, "claude", sid); err != nil {
		t.Fatal(err)
	}
	if err := c.store.UpdateTabCliStatus(info.TmuxSession, "busy", nil, nil); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", srv.URL+"/api/tmux/reset", nil)
	req.Header.Set("x-test-token", "tok")
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != 200 {
		t.Fatalf("reset: %v %v", res, err)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		screen, _ = tm.Capture(info.TmuxSession)
		if strings.Contains(paneText(screen), paneText("fake agent started: --resume "+sid)) {
			break
		}
	}
	time.Sleep(300 * time.Millisecond)
	screen, _ = tm.Capture(info.TmuxSession)
	if flat := paneText(screen); !strings.Contains(flat, paneText(`exec env DISABLE_AUTOUPDATER=1 '\''`+agentBin+`'\'' --resume `+sid+" --settings")) ||
		!strings.Contains(flat, paneText("fake agent started: --resume "+sid)) || !strings.Contains(flat, paneText("key=[it's secret]")) {
		t.Fatalf("the reset did not resume the agent:\n%s", screen)
	}

	// The slice off: the route goes back to the old runtime.
	_ = os.WriteFile(mode, []byte(`{"owned":false,"goSlices":["core"]}`), 0o600)
	time.Sleep(1100 * time.Millisecond)
	if code, _ := post(platformAPI+"/conversations", `{"provider":"claude"}`); code != -1 {
		t.Fatalf("with the slice off the route is the old runtime's: %d", code)
	}
}

// The machine agent's own calls are told from a page's by the session's subject, and only over loopback.
func TestMachineAgentRequest(t *testing.T) {
	secret := []byte("s3cret")
	ok := machineAgent(secret, "session-token")
	req := func(remote, sub string) *http.Request {
		r := httptest.NewRequest("POST", "/api/platform/automations/start", nil)
		r.RemoteAddr = remote
		r.AddCookie(&http.Cookie{Name: "session-token", Value: agent.SignHS256(secret, sub, "", time.Minute)})
		return r
	}
	if !ok(req("127.0.0.1:5000", "machine-agent")) || !ok(req("[::1]:5000", "machine-agent")) {
		t.Fatal("the machine agent is refused")
	}
	if ok(req("127.0.0.1:5000", "user")) || ok(req("10.0.0.2:5000", "machine-agent")) || ok(httptest.NewRequest("POST", "/", nil)) {
		t.Fatal("a page session, a remote caller or no session passes")
	}
}

// paneText drops every space and line break. The pane wraps at 80 columns and its capture trims a line's trailing
// space, so a space that lands on the wrap is lost: where it lands depends on the prompt, the runner's host name.
func paneText(s string) string {
	return strings.Join(strings.Fields(s), "")
}
