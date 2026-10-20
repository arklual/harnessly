//go:build linux

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// A runtime installed the way the README says (Docker, compose, Helm, systemd) names nothing of the product in its
// environment. Its Codex and Claude conversations start all the same: the real binary on a private tmux server, with
// stand-ins for the agent CLIs, a first workspace made through the API, and a conversation of each opened with and
// without a first message. A CLI route refuses a page session with a body that names the token header, and a control
// key reaches the pane as a key.
func TestStandaloneAgentsStartWithNoProductEnv(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the runtime")
	}
	for _, tool := range []string{"tmux", "bash", "git", "curl", "sed", "node"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("no " + tool)
		}
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "runtime")
	if out, err := exec.Command(goBin, "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	home := filepath.Join(root, "home")
	data := filepath.Join(home, ".harnessly")
	fakes := filepath.Join(home, "bin")
	project := filepath.Join(home, "project")
	tmuxDir := filepath.Join(root, "tmux")
	for _, d := range []string{fakes, project, tmuxDir, filepath.Join(home, ".claude"), filepath.Join(home, ".codex")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(fakes, "claude"), []byte(fakeClaude), 0o755)
	_ = os.WriteFile(filepath.Join(fakes, "codex"), []byte(fakeCodex), 0o755)
	// The panes' shells are login shells that build their own PATH, where a real Codex may come first.
	_ = os.WriteFile(filepath.Join(home, ".profile"), []byte("PATH=\"$HOME/bin:$PATH\"\n"), 0o644)
	socket := "hs-test-bare-" + strconv.Itoa(os.Getpid())
	port := freePort(t)
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	// Only what a container has: no RUNTIME_SCRIPT_DATA_DIRS, RUNTIME_MCP_TOKEN_ENV, RUNTIME_CLI_TOKEN_HEADERS, no data
	// folder, no prompt templates, no release file.
	env := []string{
		"HOME=" + home, "USER=agent", "LOGNAME=agent", "SHELL=/bin/bash", "LANG=C.UTF-8", "TMUX_TMPDIR=" + tmuxDir,
		"PATH=" + fakes + ":/usr/local/bin:/usr/bin:/bin",
		"RUNTIME_GATE_LISTEN=127.0.0.1:" + strconv.Itoa(port), "RUNTIME_GATE_UPSTREAM=",
		"RUNTIME_TMUX_SOCKET=" + socket, "RUNTIME_WORKSPACE_DIR=" + project, "RUNTIME_AGENT_RESUME_RECENT_MIN=0",
		"RUNTIME_CLAUDE_LAUNCHER=" + filepath.Join(fakes, "claude"),
	}
	tmuxCmd := func(args ...string) *exec.Cmd {
		c := exec.Command("tmux", append([]string{"-L", socket}, args...)...)
		c.Env = env
		return c
	}
	logFile, _ := os.Create(filepath.Join(root, "runtime.log"))
	defer logFile.Close()
	gateCmd := exec.Command(bin)
	gateCmd.Env = env
	gateCmd.Stdout, gateCmd.Stderr = logFile, logFile
	gateCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := gateCmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			raw, _ := os.ReadFile(filepath.Join(root, "runtime.log"))
			t.Logf("runtime log:\n%s", tail(string(raw), 6000))
			out, _ := tmuxCmd("list-sessions", "-F", "#{session_name}").Output()
			for _, s := range strings.Fields(string(out)) {
				pane, _ := tmuxCmd("capture-pane", "-p", "-t", s).Output()
				t.Logf("pane %s:\n%s", s, strings.TrimSpace(string(pane)))
			}
		}
		_ = gateCmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = gateCmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = gateCmd.Process.Kill()
			<-done
		}
		out, _ := tmuxCmd("list-panes", "-a", "-F", "#{pane_pid}").Output()
		_ = tmuxCmd("kill-server").Run()
		for deadline, pids := time.Now().Add(10*time.Second), strings.Fields(string(out)); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			alive := false
			for _, pid := range pids {
				if _, err := os.Stat("/proc/" + pid); err == nil {
					alive = true
				}
			}
			if !alive {
				return
			}
		}
	})
	client := &http.Client{Timeout: 20 * time.Second}
	var token, cookie string
	call := func(method, path, body string, headers map[string]string) (int, string) {
		req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		res, err := client.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(raw)
	}
	api := func(method, path, body string) (int, string) {
		return call(method, path, body, map[string]string{"x-cli-token": token})
	}
	waitFor := func(what string, limit time.Duration, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(limit)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	read := func(p string) string {
		raw, _ := os.ReadFile(p)
		return string(raw)
	}

	waitFor("health", 30*time.Second, func() bool {
		code, _ := call("GET", "/api/health", "", nil)
		return code == 200
	})
	for _, f := range []string{"cli-token", "config.json", "hooks.json", "codex-hook.sh", "codex-launcher.js", "preview-mcp.json"} {
		if _, err := os.Stat(filepath.Join(data, f)); err != nil {
			t.Fatalf("%s not written at start: %v", f, err)
		}
	}
	token = strings.TrimSpace(read(filepath.Join(data, "cli-token")))
	var config struct {
		AuthSecret string `json:"authSecret"`
	}
	_ = json.Unmarshal([]byte(read(filepath.Join(data, "config.json"))), &config)
	cookie = "session-token=" + agent.SignHS256([]byte(config.AuthSecret), "user", "", time.Hour)

	code, body := api("POST", "/api/workspace", `{"directory":"`+project+`","name":"Project"}`)
	var ws struct{ ID string }
	if code/100 != 2 || json.Unmarshal([]byte(body), &ws) != nil || ws.ID == "" {
		t.Fatalf("workspace: %d %s", code, body)
	}

	// A page session is not a cli-token: the refusal says what the route takes.
	if code, body := call("GET", "/api/cli/workspaces", "", map[string]string{"Cookie": cookie}); code != 403 ||
		!strings.Contains(body, "x-cli-token request header") || !strings.Contains(body, "session cookie is not accepted") {
		t.Fatalf("a session on a CLI route: %d %s", code, body)
	}
	if code, body := call("GET", "/api/cli/workspaces", "", nil); code != 403 || !strings.Contains(body, "x-cli-token") {
		t.Fatalf("no credentials on a CLI route: %d %s", code, body)
	}

	// Codex, with a first message.
	code, body = api("POST", "/api/cli/tabs", `{"workspaceId":"`+ws.ID+`","panelType":"codex-cli","prompt":"do the codex task"}`)
	if code != 201 {
		t.Fatalf("codex tab: %d %s", code, body)
	}
	waitFor("codex to be started with the prompt", 30*time.Second, func() bool {
		return strings.Contains(read(filepath.Join(home, "codex-calls.log")), "do the codex task")
	})
	calls := read(filepath.Join(home, "codex-calls.log"))
	if !strings.Contains(calls, "mcp_servers.harnessly.env_http_headers=") || !strings.Contains(calls, `"`+defaultMCPTokenEnv+`"`) {
		t.Fatalf("codex was not given the MCP server and its token variable:\n%s", calls)
	}
	// Codex, no first message: it starts all the same, in a tab of its own.
	code, body = api("POST", "/api/cli/tabs", `{"workspaceId":"`+ws.ID+`","panelType":"codex-cli"}`)
	var bare struct {
		TabID    string `json:"tabId"`
		Launched bool   `json:"launched"`
	}
	if code != 201 || json.Unmarshal([]byte(body), &bare) != nil || !bare.Launched || bare.TabID == "" {
		t.Fatalf("codex tab without a prompt: %d %s", code, body)
	}
	waitFor("the second codex", 30*time.Second, func() bool {
		return strings.Count(read(filepath.Join(home, "codex-calls.log")), "\n") >= 2
	})

	// Claude needs no more than that, with and without a first message.
	for _, req := range []string{`,"prompt":"do the task"`, ``} {
		code, body = api("POST", "/api/cli/tabs", `{"workspaceId":"`+ws.ID+`","panelType":"claude-code"`+req+`}`)
		if code != 201 || !strings.Contains(body, `"launched":true`) {
			t.Fatalf("claude tab (%q): %d %s", req, code, body)
		}
	}
	waitFor("both claudes to start with the hooks", 30*time.Second, func() bool {
		return strings.Count(read(filepath.Join(home, "claude-calls.log")), "--settings "+filepath.Join(data, "hooks.json")) >= 2
	})

	// Control keys: Ctrl-C ends a running command, so the next line runs at once; as text it would be typed.
	code, body = api("POST", "/api/cli/tabs", `{"workspaceId":"`+ws.ID+`","name":"shell"}`)
	var shell struct {
		TabID string `json:"tabId"`
	}
	if code != 201 || json.Unmarshal([]byte(body), &shell) != nil {
		t.Fatalf("shell tab: %d %s", code, body)
	}
	send := func(payload string) {
		t.Helper()
		if code, body := api("POST", "/api/cli/tabs/"+shell.TabID+"/send?workspaceId="+ws.ID, payload); code != 200 {
			t.Fatalf("send %s: %d %s", payload, code, body)
		}
	}
	screen := func() string {
		_, body := api("GET", "/api/cli/tabs/"+shell.TabID+"/result?workspaceId="+ws.ID, "")
		var res struct{ Content string }
		_ = json.Unmarshal([]byte(body), &res)
		return res.Content
	}
	waitFor("the shell", 20*time.Second, func() bool { return strings.TrimSpace(screen()) != "" })
	send(`{"content":"sleep 600"}`)
	time.Sleep(500 * time.Millisecond)
	send(`{"keys":["C-c"]}`)
	send(`{"content":"echo interrupted-$((20+22))"}`)
	waitFor("the command after Ctrl-C", 15*time.Second, func() bool {
		return strings.Contains(screen(), "interrupted-42")
	})
}
