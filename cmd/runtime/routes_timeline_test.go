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

	"harnessly/internal/gate"
	"harnessly/internal/terminal"
	"harnessly/internal/tmux"
)

func TestTimelineRoutesAnswerWhileTheCoreRuns(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	dir, home := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	socket := "hs-test-timeline-" + strconv.Itoa(os.Getpid())
	tm := tmux.NewConfig(tmux.Config{Names: tmux.Names{Socket: socket, Prefix: "hs-"}, Home: home})
	t.Cleanup(func() {
		tm.KillServer()
		_ = os.Remove(filepath.Join(os.TempDir(), "tmux-"+strconv.Itoa(os.Getuid()), socket))
	})
	tmuxNames = tm
	sid := "7c6b5a49-3827-4615-a4b3-c2d1e0f9a8b7"
	files := map[string]string{
		"cli-token":       "tok",
		"state-mode.json": `{"goSlices":["timeline"]}`,
		"workspaces.json": `{"workspaces":[{"id":"ws-a","name":"A","directories":["` + home + `"]}],"sidebarCollapsed":false,"sidebarWidth":240}`,
		"workspaces/ws-a/layout.json": `{"root":{"type":"pane","id":"pane-a","activeTabId":"tab-1","tabs":[{"id":"tab-1","sessionName":"hs-ws-a-pane-a-tab-1",
			"name":"","order":0,"cwd":"` + home + `","panelType":"claude-code","agentState":{"providerId":"claude","sessionId":"` + sid + `","jsonlPath":null},
			"meta":{"launch":{"model":"opus","effort":"ultracode"}}}]},"activePaneId":"pane-a"}`,
	}
	for name, content := range files {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		_ = os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
	}
	c := newCore(coreConfig{DataDir: dir, Home: home, Tmux: tm, Term: terminal.New(terminal.Config{Tmux: tm, Home: home}), MetaKey: "meta"})
	f, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: "127.0.0.1:1", DataDir: dir, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	routes := c.timelineRoutes(f, deps{Home: home, DataDir: dir, TokenHeaders: []string{"x-cli-token"}, SessionOK: func(r *http.Request) bool {
		ck, err := r.Cookie("session-token")
		return err == nil && ck.Value == "good"
	}})
	f.Add(routes...)
	srv := httptest.NewServer(f)
	defer srv.Close()
	matched := func(method, path string) bool {
		r := httptest.NewRequest(method, path, nil)
		for _, rt := range routes {
			if rt.Slice == "timeline" && rt.Match(r) {
				return true
			}
		}
		return false
	}
	if matched("POST", "/api/status/statusline") || matched("GET", platformAPI+"/subagents") || matched("GET", platformAPI+"/subagents/a1") ||
		matched("GET", "/api/timeline/entries?jsonlPath=x&beforeByte=1") {
		t.Fatal("a timeline route matched with the core off")
	}
	c.start()
	defer c.stop()
	if !matched("POST", "/agents/api/status/statusline/") || !matched("GET", platformAPI+"/subagents") || !matched("GET", "/agents"+platformAPI+"/subagents/a1/") ||
		!matched("GET", "/api/timeline/entries?jsonlPath=x&beforeByte=1") {
		t.Fatal("the timeline routes do not match with the core running")
	}
	if matched("GET", platformAPI+"/subagents/a1/more") || matched("GET", "/api/timeline/entries?beforeByte=1&beforeByte=2") {
		t.Fatal("a timeline route matched what the old runtime answers")
	}
	savedLimits := providerLimits // set by the machines routes when another test built them
	providerLimits = nil
	if matched("GET", platformAPI+"/economics") {
		t.Fatal("economics matched without the machine's limits")
	}
	providerLimits = savedLimits
	req, _ := http.NewRequest("POST", srv.URL+"/api/status/statusline", strings.NewReader(`{"session_id":"`+sid+`","effort":{"level":"xhigh"}}`))
	req.Header.Set("x-cli-token", "tok")
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("statusline %d", res.StatusCode)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "session-stats", sid+".json"))
	if !strings.Contains(string(raw), `"effort":"ultracode"`) {
		t.Fatalf("stats %s", raw)
	}
	providerLimits = func(string) any { return nil }
	defer func() { providerLimits = nil }()
	get := func(path string) (int, string) {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.Header.Set("cookie", "session-token=good")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if code, body := get(platformAPI + "/economics?tab=tab-x"); code != 200 || body != `{"conversation":null,"limits":null}` {
		t.Fatalf("economics %d %s", code, body)
	}
	if code, body := get(platformAPI + "/economics?tab=tab-1"); code != 200 || !strings.Contains(body, `"status":"no-transcript"`) {
		t.Fatalf("economics %d %s", code, body)
	}
	projects := filepath.Join(home, ".claude", "projects", "-srv")
	_ = os.MkdirAll(projects, 0o755)
	if code, body := get(platformAPI + "/subagents?jsonlPath=" + filepath.Join(projects, sid+".jsonl")); code != 200 || body != `{"subagents":[],"supported":true}` {
		t.Fatalf("subagents %d %s", code, body)
	}
	session := filepath.Join(projects, sid+".jsonl")
	_ = os.MkdirAll(filepath.Join(projects, sid, "subagents"), 0o755)
	_ = os.WriteFile(filepath.Join(projects, sid, "subagents", "agent-a1.jsonl"), []byte(`{"type":"summary","summary":"S"}`+"\n"), 0o644)
	if code, body := get(platformAPI + "/subagents/a1?jsonlPath=" + session); code != 200 || body != `{"entries":[]}` {
		t.Fatalf("subagent %d %s", code, body)
	}
	_ = os.WriteFile(session, []byte(`{"type":"summary","summary":"S"}`+"\n"), 0o644)
	if code, body := get("/api/timeline/entries?beforeByte=33&jsonlPath=" + session); code != 200 || body != `{"entries":[],"startByteOffset":0,"hasMore":false,"replaceEntries":false}` {
		t.Fatalf("entries %d %s", code, body)
	}
}
