package main

import (
	"encoding/json"
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

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/terminal"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

func TestTrackerKeysFromTheTaskPattern(t *testing.T) {
	t.Setenv("RUNTIME_TRACKER_KEYS", "")
	t.Setenv("RUNTIME_TASK_KEY_RE", `^(ABC|EXP|OPS)-[0-9]{1,6}$`)
	if got := strings.Join(trackerKeys(), ","); got != "ABC,EXP,OPS" {
		t.Fatalf("keys %q", got)
	}
	t.Setenv("RUNTIME_TRACKER_KEYS", "XY, Z")
	if got := strings.Join(trackerKeys(), ","); got != "XY,Z" {
		t.Fatalf("keys %q", got)
	}
	t.Setenv("RUNTIME_TRACKER_KEYS", "")
	t.Setenv("RUNTIME_TASK_KEY_RE", `[A-Z]+-\d+`)
	if keys := trackerKeys(); keys != nil {
		t.Fatalf("keys %q", keys)
	}
}

// transcriptTestGate is the transcripts slice behind a gate, switched on, with its routes.
func transcriptTestGate(t *testing.T, c *core, dir, home string) (*httptest.Server, []gate.Route) {
	_ = os.WriteFile(filepath.Join(dir, "cli-token"), []byte("tok"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":["transcripts"]}`), 0o600)
	f, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: "127.0.0.1:1", DataDir: dir, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	sessionOK := func(r *http.Request) bool {
		ck, err := r.Cookie("session-token")
		return err == nil && ck.Value == "good"
	}
	apiRefusal := func(r *http.Request) (int, any, http.Header, bool) {
		if sessionOK(r) || r.Header.Get("x-cli-token") == "tok" {
			return 0, nil, nil, false
		}
		return http.StatusUnauthorized, map[string]string{"error": "Unauthorized"}, http.Header{"Content-Type": {"application/json"}}, true
	}
	routes := c.transcriptRoutes(f, deps{Home: home, DataDir: dir, TokenHeaders: []string{"x-cli-token"}, SessionOK: sessionOK,
		APIRefusal: apiRefusal, Secret: []byte("pair-secret"),
		Claims: func(r *http.Request) map[string]any {
			if !sessionOK(r) {
				return nil
			}
			return map[string]any{"sub": "ann@example.com"}
		}})
	f.Add(routes...)
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return srv, routes
}

func transcriptGet(t *testing.T, srv *httptest.Server, path string, h map[string]string) (int, string) {
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	for k, v := range h {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestTranscriptRoutesThroughTheGate(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	for k, v := range map[string]string{"RUNTIME_SHARE_BASE_HEADER": "x-share-base", "RUNTIME_SHARE_TOKEN_HEADER": "x-share-token",
		"RUNTIME_SHARE_TOKEN_SALT": "acme", "RUNTIME_SHARE_TOKEN_AUDIENCE": "acme-share-read", "RUNTIME_SHARE_FILE_PREFIX": "acme",
		"RUNTIME_APP_NAME": "Acme"} {
		t.Setenv(k, v)
	}
	c := &core{cfg: coreConfig{DataDir: dir, Home: home, Timeline: timelineConfigFromEnv(dir)}}
	srv, routes := transcriptTestGate(t, c, dir, home)
	for _, rt := range routes {
		if rt.Match(httptest.NewRequest("POST", platformAPI+"/snapshots", nil)) {
			t.Fatal("snapshots taken without the core")
		}
	}
	for _, p := range []string{platformAPI + "/sources", "/agents" + platformAPI + "/sources//", "/api/timeline/message-counts/", platformAPI + "/stats"} {
		r := httptest.NewRequest("GET", p, nil)
		taken := false
		for _, rt := range routes {
			taken = taken || rt.Slice == "transcripts" && rt.Match(r)
		}
		if !taken {
			t.Fatalf("%s is not taken", p)
		}
	}
	for _, rt := range routes {
		if rt.Match(httptest.NewRequest("GET", "/api/stats/daily-report/cache", nil)) {
			t.Fatal("the dead daily report cache route is taken")
		}
	}
	session := filepath.Join(home, ".claude", "projects", "-srv", "s.jsonl")
	_ = os.MkdirAll(filepath.Dir(session), 0o755)
	_ = os.WriteFile(session, []byte(`{"type":"assistant","timestamp":"2026-10-18T10:00:00.000Z","uuid":"a","message":{"id":"m","role":"assistant",`+
		`"content":[{"type":"text","text":"see https://example.org/x"},{"type":"tool_use","id":"t","name":"Read","input":{"file_path":"/srv/a.go"}}]}}`+"\n"), 0o644)
	ses := map[string]string{"cookie": "session-token=good"}
	if code, body := transcriptGet(t, srv, platformAPI+"/sources?jsonlPath="+session, nil); code != 401 || body != `{"error":"Unauthorized"}` {
		t.Fatalf("sources without a session %d %s", code, body)
	}
	if code, body := transcriptGet(t, srv, platformAPI+"/sources?jsonlPath="+session, map[string]string{"x-cli-token": "tok"}); code != 403 {
		t.Fatalf("sources with the cli-token %d %s", code, body)
	}
	if code, body := transcriptGet(t, srv, platformAPI+"/sources?jsonlPath="+session, ses); code != 200 ||
		!strings.Contains(body, `"url":"https://example.org/x"`) || !strings.Contains(body, `"workPaths":["/srv/a.go"]`) {
		t.Fatalf("sources %d %s", code, body)
	}
	if code, body := transcriptGet(t, srv, "/api/timeline/message-counts?jsonlPath="+session, map[string]string{"x-cli-token": "tok"}); code != 200 ||
		body != `{"userCount":0,"assistantCount":1,"toolCount":1,"toolBreakdown":{"Read":1}}` {
		t.Fatalf("message counts %d %s", code, body)
	}
	if code, body := transcriptGet(t, srv, platformAPI+"/shares", ses); code != 200 || body != `{"enabled":false,"shares":[]}` {
		t.Fatalf("shares %d %s", code, body)
	}
	if code, body := transcriptGet(t, srv, platformAPI+"/shares", map[string]string{"x-cli-token": "tok"}); code != 403 {
		t.Fatalf("shares with the cli-token %d %s", code, body)
	}
	if code, _ := transcriptGet(t, srv, platformAPI+"/snapshots/"+strings.Repeat("a", 43)+"/preview", ses); code != 404 {
		t.Fatalf("preview of no draft %d", code)
	}
	if code, _ := transcriptGet(t, srv, platformAPI+"/share-read/"+strings.Repeat("a", 43), nil); code != 401 {
		t.Fatalf("share read without a token %d", code)
	}
	if code, body := transcriptGet(t, srv, "/agents/api/stats/cache-status/", map[string]string{"x-cli-token": "tok"}); code != 200 ||
		body != `{"exists":false,"fileCount":1}` {
		t.Fatalf("stats cache status %d %s", code, body)
	}
	if code, _ := transcriptGet(t, srv, "/api/stats/overview", nil); code != 401 {
		t.Fatalf("stats overview without a session %d", code)
	}
	if code, body := transcriptGet(t, srv, platformAPI+"/stats?shape=report&wait=0", map[string]string{"x-cli-token": "tok"}); code != 403 {
		t.Fatalf("stats with the cli-token %d %s", code, body)
	}
	if code, body := transcriptGet(t, srv, platformAPI+"/stats?shape=report", ses); code != 200 || !strings.Contains(body, `"person":"ann@example.com"`) {
		t.Fatalf("stats %d %s", code, body)
	}
}

func TestSnapshotsWhileTheCoreRuns(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	dir, home := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	for k, v := range map[string]string{"RUNTIME_SHARE_BASE_HEADER": "x-share-base", "RUNTIME_SHARE_TOKEN_HEADER": "x-share-token",
		"RUNTIME_SHARE_TOKEN_SALT": "acme", "RUNTIME_SHARE_TOKEN_AUDIENCE": "acme-share-read", "RUNTIME_SHARE_FILE_PREFIX": "acme",
		"RUNTIME_APP_NAME": "Acme"} {
		t.Setenv(k, v)
	}
	socket := "hs-test-transcripts-" + strconv.Itoa(os.Getpid())
	tm := tmux.NewConfig(tmux.Config{Names: tmux.Names{Socket: socket, Prefix: "hs-"}, Home: home})
	t.Cleanup(func() {
		tm.KillServer()
		_ = os.Remove(filepath.Join(os.TempDir(), "tmux-"+strconv.Itoa(os.Getuid()), socket))
	})
	tmuxNames = tm
	sid := "7c6b5a49-3827-4615-a4b3-c2d1e0f9a8b7"
	session := filepath.Join(home, ".claude", "projects", "-srv", sid+".jsonl")
	_ = os.MkdirAll(filepath.Dir(session), 0o755)
	_ = os.WriteFile(session, []byte(`{"type":"user","timestamp":"2026-10-18T10:00:00.000Z","uuid":"u","message":{"role":"user","content":"hello"}}`+"\n"+
		`{"type":"assistant","timestamp":"2026-10-18T10:00:01.000Z","uuid":"a","message":{"id":"m","role":"assistant","content":[{"type":"text","text":"Hi **there**"}]}}`+"\n"), 0o644)
	files := map[string]string{
		"workspaces.json": `{"workspaces":[{"id":"ws-a","name":"A","directories":["` + home + `"]}],"sidebarCollapsed":false,"sidebarWidth":240}`,
		"workspaces/ws-a/layout.json": `{"root":{"type":"pane","id":"pane-a","activeTabId":"tab-1","tabs":[{"id":"tab-1","sessionName":"hs-ws-a-pane-a-tab-1",
			"name":"","order":0,"cwd":"` + home + `","panelType":"claude-code","agentState":{"providerId":"claude","sessionId":"` + sid + `","jsonlPath":"` + session + `"}}]},"activePaneId":"pane-a"}`,
	}
	for name, content := range files {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		_ = os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
	}
	c := newCore(coreConfig{DataDir: dir, Home: home, Tmux: tm, Term: terminal.New(terminal.Config{Tmux: tm, Home: home}), Timeline: timelineConfigFromEnv(dir)})
	srv, _ := transcriptTestGate(t, c, dir, home)
	c.start()
	defer c.stop()
	req, _ := http.NewRequest("POST", srv.URL+platformAPI+"/snapshots", strings.NewReader(`{"tabId":"tab-1","options":{"tools":"summary","thinking":false,"diffs":false,"economics":true}}`))
	req.Header.Set("cookie", "session-token=good")
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated || !strings.Contains(string(body), `"draft"`) {
		t.Fatalf("snapshot %d %s", res.StatusCode, body)
	}
	var created struct {
		Draft struct {
			ID string `json:"id"`
		} `json:"draft"`
	}
	_ = json.Unmarshal(body, &created)
	code, page := transcriptGet(t, srv, platformAPI+"/snapshots/"+created.Draft.ID+"/preview", map[string]string{"cookie": "session-token=good"})
	if code != 200 || !strings.Contains(page, "<strong>there</strong>") || !strings.Contains(page, "Acme") {
		t.Fatalf("preview %d %.300s", code, page)
	}
}
