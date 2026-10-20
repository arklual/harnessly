//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// fakeClaude stands in for Claude Code: it answers --version, notes its arguments, writes its pid file and transcript
// as Claude does, and runs as a process named claude. Beside it the hooks and the status line it was given in its
// --settings file are played, as Claude runs them: the session start, the prompt, the status line, and the end of the
// turn unless the mode file says the agent keeps working.
const fakeClaude = `#!/bin/bash
if [ "$1" = "--version" ]; then echo "2.1.99 (Claude Code)"; exit 0; fi
echo "$*" >> "$HOME/claude-calls.log"
env | grep -E '^(BOARDS|ARTIFACTS|RUNTIME|HARNESSLY)_' | sort >> "$HOME/claude-env.log"
settings=""; sid=""
while [ $# -gt 0 ]; do
  case "$1" in
    --settings) settings="$2"; shift ;;
    --resume) sid="$2"; shift ;;
  esac
  shift
done
[ -n "$sid" ] || sid=$(cat /proc/sys/kernel/random/uuid)
mode=$(cat "$HOME/fake-claude-mode" 2>/dev/null)
cwd=$(pwd)
project="$HOME/.claude/projects/$(printf '%s' "$cwd" | sed 's/[^A-Za-z0-9]/-/g')"
mkdir -p "$project" "$HOME/.claude/sessions"
transcript="$project/$sid.jsonl"
now=$(date -u +%Y-%m-%dT%H:%M:%S.000Z)
printf '{"type":"user","message":{"role":"user","content":"do the task"},"uuid":"u-%s","timestamp":"%s","sessionId":"%s","cwd":"%s"}\n' "$sid" "$now" "$sid" "$cwd" >> "$transcript"
printf '{"pid":%s,"sessionId":"%s","cwd":"%s","startedAt":%s000}\n' "$$" "$sid" "$cwd" "$(date +%s)" > "$HOME/.claude/sessions/$$.json"
hook() {
  sed -n 's/^ *"command": "\(.*\)",\{0,1\}$/\1/p' "$settings" | sed 's/\\"/"/g' | grep -- "$1\$" | head -1
}
(
  sleep 1
  echo "{\"session_id\":\"$sid\"}" | eval "$(hook session-start)"
  sleep 0.5
  echo "{\"session_id\":\"$sid\",\"prompt\":\"do the task\"}" | eval "$(hook prompt-submit)"
  sleep 0.5
  printf '{"session_id":"%s","transcript_path":"%s","cwd":"%s","model":{"id":"claude-test-1","display_name":"Test"},"workspace":{"current_dir":"%s"},"version":"2.1.99","cost":{"total_cost_usd":0.01},"context_window":{"context_window_size":200000,"current_usage":{"input_tokens":1000,"output_tokens":10,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}' \
    "$sid" "$transcript" "$cwd" "$cwd" | eval "$(hook statusline.sh\")" > "$HOME/statusline-$sid.out"
  if [ "$mode" != "work" ]; then
    printf '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]},"uuid":"a-%s","timestamp":"%s","sessionId":"%s","cwd":"%s"}\n' "$sid" "$now" "$sid" "$cwd" >> "$transcript"
    sleep 0.5
    echo "{\"session_id\":\"$sid\"}" | eval "$(hook stop)"
  fi
) </dev/null >/dev/null 2>&1 &
echo "fake claude ready $sid"
exec -a claude sleep 3600
`

const fakeCodex = `#!/bin/sh
if [ "$1" = "--version" ]; then echo "codex-cli 0.50.0"; exit 0; fi
echo "$*" >> "$HOME/codex-calls.log"
exec sleep 3600
`

// The gate run on its own, as a machine runs once the old runtime is gone: the real binary on a private tmux server,
// with stand-ins for the agent CLIs. A fresh machine is set up and its page loaded, a workspace made, a Claude tab
// opened whose hooks and status line reach the gate through the files it wrote; the conversation and terminal sockets
// show the tab, the tab closes, and after a restart of the machine the agent that was working is resumed.
func TestStandaloneMachineEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the runtime")
	}
	for _, tool := range []string{"tmux", "bash", "git", "curl", "sed"} {
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
	data := filepath.Join(home, ".acme-ai")
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
	// The boards and artifacts servers: a hand-off and a conversation's documents, as the integrations call them.
	services := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/b/api/handoff" && r.Header.Get("Authorization") == "Bearer runtime-token":
			w.WriteHeader(201)
			_, _ = io.WriteString(w, `{"url":"http://`+r.Host+`/b/auth/handoff?t=signed"}`)
		case r.URL.Path == "/api/v1/artifacts" && r.Header.Get("Authorization") == "Bearer viewer-token":
			_, _ = io.WriteString(w, `{"total":1,"items":[{"slug":"plan-4k2m9x","title":"Plan for `+r.URL.Query().Get("session")+`",`+
				`"updatedAt":"2026-10-19T12:00:00.000Z","versionCount":1,"viewUrl":"http://`+r.Host+`/a/plan-4k2m9x","embedUrl":"http://`+r.Host+`/embed/plan-4k2m9x"}]}`)
		default:
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":"unauthorized"}`)
		}
	}))
	defer services.Close()
	release := filepath.Join(root, "release.json")
	_ = os.WriteFile(release, []byte(`{"version":"9.9.9","commit":"abc"}`), 0o644)
	socket := "hs-test-sa-" + strconv.Itoa(os.Getpid())
	port := freePort(t)
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	wd, _ := os.Getwd()
	env := []string{
		"HOME=" + home, "USER=agent", "LOGNAME=agent", "SHELL=/bin/bash", "LANG=C.UTF-8", "TMUX_TMPDIR=" + tmuxDir,
		"PATH=" + fakes + ":/usr/local/bin:/usr/bin:/bin",
		"RUNTIME_GATE_LISTEN=127.0.0.1:" + strconv.Itoa(port), "RUNTIME_GATE_UPSTREAM=", "AGENT_DATA_DIR=" + data, "AGENT_VERSION=9.9.9",
		"RUNTIME_TMUX_SOCKET=" + socket, "RUNTIME_SESSION_PREFIX=hs-", "RUNTIME_CLI_TOKEN_HEADERS=x-acme-token,x-omux-token",
		"RUNTIME_SCRIPT_DATA_DIRS=.acme-ai", "RUNTIME_MCP_SERVER=acme-preview", "RUNTIME_MCP_TOKEN_ENV=ACME_PREVIEW_TOKEN",
		"RUNTIME_PROMPT_EDITOR=" + filepath.Join(data, "bin", "acme-prompt-editor"), "RUNTIME_PROMPT_HUMAN_EDITOR_VAR=ACME_HUMAN_EDITOR",
		"RUNTIME_PROMPT_HANDOFF_UNKEYED_SOCKET=violet", "RUNTIME_LOCK_FILE=acme.lock", "RUNTIME_HEALTH_APP=acme",
		"RUNTIME_RELEASE_FILE=" + release, "RUNTIME_WORKSPACE_DIR=" + project, "RUNTIME_FIRST_WORKSPACE_NAME=First",
		"RUNTIME_WORKSPACE_PROMPTS_FILE=" + filepath.Join(wd, "testdata", "standalone", "workspace-prompts.json"),
		"RUNTIME_AGENT_RESUME_RECENT_MIN=0", "RUNTIME_GATE_COUNT_SECONDS=3600",
		// The panes' login shells build their own PATH, where a real Claude may come first: launches name the stand-in.
		"RUNTIME_CLAUDE_LAUNCHER=" + filepath.Join(fakes, "claude"),
		"RUNTIME_PLUGIN_RELOAD_MARK=" + filepath.Join(home, ".local", "state", "sync", "claude-reload.json"),
		"RUNTIME_BOARDS_URL=" + services.URL, "RUNTIME_BOARDS_TOKEN=runtime-token", "RUNTIME_BOARDS_AGENT_TOKEN=agent-token",
		"RUNTIME_ARTIFACTS_URL=" + services.URL, "RUNTIME_ARTIFACTS_TOKEN=editor-token", "RUNTIME_ARTIFACTS_READ_TOKEN=viewer-token",
		"RUNTIME_OWNER_NAME=Ada",
	}
	tmuxCmd := func(args ...string) *exec.Cmd {
		c := exec.Command("tmux", append([]string{"-L", socket}, args...)...)
		c.Env = env
		return c
	}
	logFile, _ := os.Create(filepath.Join(root, "gate.log"))
	if p := os.Getenv("E2E_LOG"); p != "" {
		logFile, _ = os.Create(p)
	}
	defer logFile.Close()
	var gateCmd *exec.Cmd
	start := func(extra ...string) *exec.Cmd {
		c := exec.Command(bin)
		c.Env = append(append([]string{}, env...), extra...)
		c.Stdout, c.Stderr = logFile, logFile
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		return c
	}
	stop := func(c *exec.Cmd) {
		_ = c.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = c.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = c.Process.Kill()
			<-done
		}
	}
	// The panes' shells write into the temp HOME as they exit (history, the fake agents' logs): the server goes, and
	// then its panes, before the temp dirs are removed.
	killTmux := func() {
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
	}
	t.Cleanup(func() {
		if gateCmd != nil && gateCmd.ProcessState == nil {
			stop(gateCmd)
		}
		if t.Failed() {
			raw, _ := os.ReadFile(filepath.Join(root, "gate.log"))
			t.Logf("gate log:\n%s", tail(string(raw), 6000))
			out, _ := tmuxCmd("list-sessions", "-F", "#{session_name}").Output()
			for _, s := range strings.Fields(string(out)) {
				pane, _ := tmuxCmd("capture-pane", "-p", "-t", s).Output()
				t.Logf("pane %s:\n%s", s, strings.TrimSpace(string(pane)))
			}
			calls, _ := os.ReadFile(filepath.Join(home, "claude-calls.log"))
			t.Logf("claude calls:\n%s", calls)
		}
		killTmux()
	})
	client := &http.Client{Timeout: 20 * time.Second}
	var cookie, token string
	call := func(method, path, body string, page bool) (int, string) {
		req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if page {
			req.Header.Set("Cookie", cookie)
		} else {
			req.Header.Set("x-acme-token", token)
		}
		res, err := client.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(raw)
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

	// The plugin reconciler changed the plugins a while ago: Claudes started from now on load them as they are.
	_ = os.MkdirAll(filepath.Join(home, ".local", "state", "sync"), 0o700)
	_ = os.WriteFile(filepath.Join(home, ".local", "state", "sync", "claude-reload.json"), []byte(`{"plugins":1792400400000}`), 0o600)

	// --- start: the machine's files, written once the gate holds the port ---------------------------------------------
	gateCmd = start()
	started := time.Now()
	waitFor("health", 30*time.Second, func() bool {
		code, body := call("GET", "/api/health", "", true)
		return code == 200 && strings.Contains(body, `"app":"acme"`) && strings.Contains(body, `"version":"9.9.9"`)
	})
	if read(filepath.Join(data, "port")) != strconv.Itoa(port) {
		t.Fatalf("port file: %q", read(filepath.Join(data, "port")))
	}
	for _, f := range []string{"cli-token", "config.json", "hooks.json", "status-hook.sh", "statusline.sh", "codex-hook.sh", "preview-mcp.json",
		"acme.lock", "codex-launcher.js", "bin/acme-prompt-editor"} {
		if _, err := os.Stat(filepath.Join(data, f)); err != nil {
			t.Fatalf("%s not written at start: %v", f, err)
		}
	}
	token = strings.TrimSpace(read(filepath.Join(data, "cli-token")))
	var config struct {
		AuthSecret string `json:"authSecret"`
	}
	_ = json.Unmarshal([]byte(read(filepath.Join(data, "config.json"))), &config)
	if len(config.AuthSecret) != 64 || len(token) != 64 {
		t.Fatalf("secret %q, token %q", config.AuthSecret, token)
	}
	cookie = "session-token=" + agent.SignHS256([]byte(config.AuthSecret), "user", "", time.Hour)
	if !strings.Contains(read(filepath.Join(data, "preview-mcp.json")), "http://127.0.0.1:"+strconv.Itoa(port)+"/api/cli/preview/mcp") {
		t.Fatal("the MCP config names the gate's port")
	}
	// One writer per data folder: a second runtime on it gives up.
	second := start("RUNTIME_GATE_LISTEN=127.0.0.1:" + strconv.Itoa(freePort(t)))
	exited := make(chan error, 1)
	go func() { exited <- second.Wait() }()
	select {
	case err := <-exited:
		if err == nil {
			t.Fatal("a second runtime on the same data folder started")
		}
	case <-time.After(15 * time.Second):
		_ = second.Process.Kill()
		t.Fatal("a second runtime on the same data folder kept running")
	}
	if code, body := call("GET", "/api/stats/overview", "", true); code != 503 && code != 200 {
		t.Fatalf("a route not answered here: %d %s", code, body)
	}
	if code, body := call("GET", "/api/nothing", "", true); code != 404 || body != `{"error":"Not found"}` {
		t.Fatalf("an unknown route: %d %s", code, body)
	}

	// --- first page load: the machine's first workspace, its agents' prompts ---------------------------------------------
	var boot struct {
		Workspaces struct {
			Workspaces []struct{ ID, Name string } `json:"workspaces"`
		} `json:"initialWorkspace"`
	}
	var last string
	defer func() {
		if t.Failed() {
			t.Log("last answer: " + tail(last, 2000))
		}
	}()
	waitFor("the first page load", 30*time.Second, func() bool {
		code, body := call("GET", "/api/platform/bootstrap", "", true)
		last = strconv.Itoa(code) + " " + body
		return code == 200 && json.Unmarshal([]byte(body), &boot) == nil && len(boot.Workspaces.Workspaces) == 1
	})
	first := boot.Workspaces.Workspaces[0].ID
	waitFor("the first workspace's prompts", 10*time.Second, func() bool {
		return strings.Contains(read(filepath.Join(data, "workspaces", first, "claude-prompt.md")), "`"+first+"`")
	})

	// --- a workspace -------------------------------------------------------------------------------------------------------
	code, body := call("POST", "/api/workspace", `{"directory":"`+project+`","name":"Project"}`, true)
	var ws struct{ ID string }
	if code/100 != 2 || json.Unmarshal([]byte(body), &ws) != nil || ws.ID == "" {
		t.Fatalf("workspace: %d %s", code, body)
	}
	waitFor("the new workspace's prompts", 10*time.Second, func() bool {
		return strings.Contains(read(filepath.Join(data, "workspaces", ws.ID, "codex-prompt.md")), "`"+ws.ID+"`")
	})

	// --- a Claude tab: launched with the hooks, which report back through the scripts --------------------------------
	type tabStatus struct {
		Alive          bool    `json:"alive"`
		CliState       *string `json:"cliState"`
		AgentSessionID *string `json:"agentSessionId"`
	}
	status := func(tab string) tabStatus {
		var s tabStatus
		_, body := call("GET", "/api/cli/tabs/"+tab+"/status?workspaceId="+ws.ID, "", false)
		_ = json.Unmarshal([]byte(body), &s)
		return s
	}
	// The last hook event the keeper heard of a tab: its internal view, which only the cli-token reads.
	lastEvent := func(tab string) string {
		_, body := call("POST", "/api/internal/core/tabs", "{}", false)
		var res struct {
			Tabs []struct {
				TabID         string  `json:"tabId"`
				LastEventName *string `json:"lastEventName"`
			} `json:"tabs"`
		}
		_ = json.Unmarshal([]byte(body), &res)
		for _, x := range res.Tabs {
			if x.TabID == tab && x.LastEventName != nil {
				return *x.LastEventName
			}
		}
		return ""
	}
	openClaude := func(mode string) (string, string) {
		t.Helper()
		_ = os.WriteFile(filepath.Join(home, "fake-claude-mode"), []byte(mode), 0o600)
		code, body := call("POST", "/api/cli/tabs", `{"workspaceId":"`+ws.ID+`","panelType":"claude-code","prompt":"do the task"}`, false)
		var tab struct{ TabID, SessionName string }
		if code != 201 || json.Unmarshal([]byte(body), &tab) != nil || tab.TabID == "" {
			t.Fatalf("claude tab: %d %s", code, body)
		}
		return tab.TabID, tab.SessionName
	}
	tab, session := openClaude("finish")
	waitFor("claude started with the hooks", 30*time.Second, func() bool {
		return strings.Contains(read(filepath.Join(home, "claude-calls.log")), "--settings "+filepath.Join(data, "hooks.json"))
	})
	if !strings.Contains(read(filepath.Join(home, "claude-calls.log")), "--append-system-prompt-file "+filepath.Join(data, "workspaces", ws.ID, "claude-prompt.md")) {
		t.Fatal("claude started without its workspace prompt")
	}
	// The workspace's skills: a Claude plugin loaded with --plugin-dir, Codex user skills, and the launchers on the PATH.
	if !strings.Contains(read(filepath.Join(home, "claude-calls.log")), "--plugin-dir "+filepath.Join(data, "agent-skills", "claude")) {
		t.Fatal("claude started without the skills plugin")
	}
	for _, f := range []string{
		filepath.Join(data, "agent-skills", "claude", ".claude-plugin", "plugin.json"),
		filepath.Join(data, "agent-skills", "claude", "skills", "harnessly", "SKILL.md"),
		filepath.Join(data, "agent-skills", "claude", "skills", "boards", "SKILL.md"),
		filepath.Join(data, "agent-skills", "claude", "skills", "artifacts", "SKILL.md"),
		filepath.Join(home, ".codex", "skills", "harnessly", "harnessly", "SKILL.md"),
		filepath.Join(home, ".codex", "skills", "harnessly", "boards", "scripts", "boards"),
		filepath.Join(home, ".codex", "skills", "harnessly", "artifacts", "SKILL.md"),
		filepath.Join(home, ".local", "bin", "boards"), filepath.Join(home, ".local", "bin", "artifacts"),
	} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("skills: %v", err)
		}
	}
	if p := read(filepath.Join(data, "workspaces", ws.ID, "claude-prompt.md")); !strings.Contains(p, "Acme platform context") {
		t.Fatalf("the prompts file the deployment names is used, not the built-in one: %.80q", p)
	}
	var sid string
	waitFor("the turn to end through the hooks", 40*time.Second, func() bool {
		s := status(tab)
		if s.AgentSessionID != nil {
			sid = *s.AgentSessionID
		}
		return s.Alive && sid != "" && s.CliState != nil && *s.CliState == "ready-for-review" && lastEvent(tab) == "stop"
	})
	waitFor("the status line's figures", 10*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(data, "session-stats", sid+".json"))
		return err == nil
	})

	// --- the integrations: the agent's environment, and what the page asks the runtime for ------------------------------
	wantEnv := strings.Join([]string{"ARTIFACTS_AUTHOR=Ada (agent)", "ARTIFACTS_SESSION=" + tab, "ARTIFACTS_TOKEN=editor-token",
		"ARTIFACTS_URL=" + services.URL, "BOARDS_TOKEN=agent-token", "BOARDS_URL=" + services.URL, "HARNESSLY_DATA_DIR=" + data}, "\n") + "\n"
	if got := read(filepath.Join(home, "claude-env.log")); got != wantEnv {
		t.Fatalf("the agent's environment:\n%s\nwant\n%s", got, wantEnv)
	}
	if code, body := call("GET", "/api/integrations", "", true); code != 200 ||
		body != `{"boards":{"url":"`+services.URL+`"},"artifacts":{"url":"`+services.URL+`"}}`+"\n" {
		t.Fatalf("integrations: %d %s", code, body)
	}
	if code, body := call("POST", "/agents/api/integrations/boards/sign-in", `{"next":"/b/plan-abc?k=key"}`, true); code != 200 ||
		body != `{"url":"`+services.URL+`/b/auth/handoff?t=signed"}`+"\n" {
		t.Fatalf("board sign-in: %d %s", code, body)
	}
	if code, _ := call("POST", "/api/integrations/boards/sign-in", `{"next":"/b/plan-abc?k=key"}`, false); code != 403 {
		t.Fatalf("a cli-token signs nobody in to boards: %d", code)
	}
	if code, body := call("GET", "/api/integrations/artifacts/documents?session="+tab, "", true); code != 200 ||
		!strings.Contains(body, `"title":"Plan for `+tab+`"`) || !strings.Contains(body, `"embedUrl":"`+services.URL+`/embed/plan-4k2m9x"`) {
		t.Fatalf("documents: %d %s", code, body)
	}

	// --- the conversation and terminal sockets ---------------------------------------------------------------------------
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	header := http.Header{"Cookie": {cookie}}
	tl, _, err := websocket.Dial(ctx, "ws://127.0.0.1:"+strconv.Itoa(port)+"/api/timeline?session="+session+"&panelType=claude-code",
		&websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	tl.SetReadLimit(64 << 20)
	for {
		_, raw, err := tl.Read(ctx)
		if err != nil {
			t.Fatalf("timeline: %v", err)
		}
		var m struct{ Type, SessionID string }
		_ = json.Unmarshal(raw, &m)
		if m.Type == "timeline:init" {
			if m.SessionID != sid {
				t.Fatalf("timeline init names %q, want %q", m.SessionID, sid)
			}
			break
		}
	}
	_ = tl.Close(websocket.StatusNormalClosure, "")
	term, _, err := websocket.Dial(ctx, "ws://127.0.0.1:"+strconv.Itoa(port)+"/api/terminal?session="+session+"&cols=100&rows=30",
		&websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	term.SetReadLimit(64 << 20)
	var screen bytes.Buffer
	for !bytes.Contains(screen.Bytes(), []byte("fake claude ready "+sid)) {
		typ, raw, err := term.Read(ctx)
		if err != nil {
			t.Fatalf("terminal: %v; screen so far %q", err, tail(screen.String(), 400))
		}
		if typ == websocket.MessageBinary && len(raw) > 0 && raw[0] == 0x01 {
			screen.Write(raw[1:])
		}
	}
	_ = term.Close(websocket.StatusNormalClosure, "")

	// --- closing the tab ends its session ----------------------------------------------------------------------------
	if code, body := call("DELETE", "/api/cli/tabs/"+tab+"?workspaceId="+ws.ID, "", false); code != 200 {
		t.Fatalf("close: %d %s", code, body)
	}
	waitFor("the tab's session to end", 15*time.Second, func() bool { return tmuxCmd("has-session", "-t", session).Run() != nil })

	// --- a restart of the machine: the agent that was working comes back --------------------------------------------
	// The reconciler's mark is read on the first sweep, a few seconds after the start.
	time.Sleep(time.Until(started.Add(reloadSweep + time.Second)))
	working, _ := openClaude("work")
	var workingSID string
	waitFor("the second agent to be working", 40*time.Second, func() bool {
		s := status(working)
		if s.AgentSessionID != nil {
			workingSID = *s.AgentSessionID
		}
		return workingSID != "" && s.CliState != nil && *s.CliState == "busy" && lastEvent(working) == "prompt-submit"
	})
	defer func() {
		if t.Failed() {
			t.Logf("applied marks: %s (working %s)", read(filepath.Join(data, "claude-reload-applied.json")), working)
		}
	}()
	waitFor("the started agent to count as having the plugins", 20*time.Second, func() bool {
		return strings.Contains(read(filepath.Join(data, "claude-reload-applied.json")), `"`+working+`":{"plugins":1792400400000}`)
	})
	stop(gateCmd)
	if _, err := os.Stat(filepath.Join(data, "port")); err == nil {
		t.Fatal("port file left by a stopped gate")
	}
	if _, err := os.Stat(filepath.Join(data, "acme.lock")); err == nil {
		t.Fatal("lock left by a stopped gate")
	}
	killTmux()
	gateCmd = start()
	waitFor("the restarted gate", 30*time.Second, func() bool { code, _ := call("GET", "/api/health", "", true); return code == 200 })
	waitFor("the working agent to be resumed", 60*time.Second, func() bool {
		return strings.Contains(read(filepath.Join(home, "claude-calls.log")), "--resume "+workingSID)
	})
	waitFor("the resumed agent's conversation in its environment", 10*time.Second, func() bool {
		return strings.Count(read(filepath.Join(home, "claude-env.log")), "ARTIFACTS_SESSION="+working+"\n") == 2
	})
	waitFor("the resumed tab to show its conversation", 40*time.Second, func() bool {
		s := status(working)
		return s.Alive && s.AgentSessionID != nil && *s.AgentSessionID == workingSID
	})
	t.Logf("agent launches:\n%s", read(filepath.Join(home, "claude-calls.log")))
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
