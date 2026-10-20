package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/proc"
	"github.com/ly-solutions/harnessly/internal/push"
	"github.com/ly-solutions/harnessly/internal/sessions"
	"github.com/ly-solutions/harnessly/internal/sleep"
	"github.com/ly-solutions/harnessly/internal/terminal"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

// recorder is a fake HTTP peer (the old runtime, a push service) that keeps what it was sent.
type recorder struct {
	mu   sync.Mutex
	got  []recorded
	code int
}

type recorded struct {
	path   string
	header http.Header
	body   []byte
}

func (rec *recorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	rec.mu.Lock()
	rec.got = append(rec.got, recorded{r.URL.Path, r.Header.Clone(), body})
	code := rec.code
	rec.mu.Unlock()
	if code == 0 {
		code = http.StatusNoContent
	}
	w.WriteHeader(code)
}

func (rec *recorder) waitFor(t *testing.T, path string) recorded {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		rec.mu.Lock()
		for _, g := range rec.got {
			if g.path == path {
				rec.mu.Unlock()
				return g
			}
		}
		rec.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("nothing posted to %s", path)
	return recorded{}
}

func TestCoreHooksPushSleepAndKill(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	t.Setenv("HOST", "")
	dir, home := t.TempDir(), t.TempDir()
	socket := "hs-test-hooks-" + strconv.Itoa(os.Getpid())
	tm := tmux.NewConfig(tmux.Config{Names: tmux.Names{Socket: socket, Prefix: "hs-"}, Home: home})
	t.Cleanup(func() {
		tm.KillServer()
		_ = os.Remove(filepath.Join(os.TempDir(), "tmux-"+strconv.Itoa(os.Getuid()), socket))
	})
	tmuxNames = tm
	shSession, clSession := tm.SessionName("ws-a", "pane-p1", "tab-sh"), tm.SessionName("ws-a", "pane-p1", "tab-cl")

	// A Claude transcript, where Claude keeps it, whose last conversation is two days old.
	transcript := filepath.Join(home, ".claude", "projects", sessions.ProjectName(home), "11111111-2222-4333-8444-555555555555.jsonl")
	_ = os.MkdirAll(filepath.Dir(transcript), 0o700)
	old := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339Nano)
	_ = os.WriteFile(transcript, []byte(`{"type":"user","timestamp":"`+old+`","message":{"content":"go"}}`+"\n"+
		`{"type":"assistant","timestamp":"`+old+`","message":{"content":[{"type":"text","text":"Готово."}],"stop_reason":"end_turn"}}`+"\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "workspaces.json"), []byte(`{"workspaces":[{"id":"ws-a","name":"A","directories":["`+home+`"]}],"sidebarCollapsed":false,"sidebarWidth":240}`), 0o600)
	_ = os.MkdirAll(filepath.Join(dir, "workspaces", "ws-a"), 0o700)
	layout := `{"root":{"type":"pane","id":"pane-p1","tabs":[` +
		`{"id":"tab-sh","sessionName":"` + shSession + `","name":"","order":0,"cwd":"` + home + `"},` +
		`{"id":"tab-cl","sessionName":"` + clSession + `","name":"Claude","order":1,"cwd":"` + home + `","panelType":"claude-code","cliState":"idle",` +
		`"agentState":{"providerId":"claude","sessionId":"11111111-2222-4333-8444-555555555555","jsonlPath":"` + transcript + `","summary":null}}` +
		`],"activeTabId":"tab-sh"},"activePaneId":"pane-p1"}`
	_ = os.WriteFile(filepath.Join(dir, "workspaces", "ws-a", "layout.json"), []byte(layout), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"networkAccess":"localhost"}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "cli-token"), []byte("ok"), 0o600)

	// The old runtime and a push service.
	oldRT, pushSvc := &recorder{}, &recorder{code: http.StatusCreated}
	oldSrv, pushSrv := httptest.NewServer(oldRT), httptest.NewServer(pushSvc)
	defer oldSrv.Close()
	defer pushSrv.Close()
	devKey, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	b64 := base64.RawURLEncoding
	subs, _ := json.Marshal([]any{map[string]any{"endpoint": pushSrv.URL + "/dev", "keys": map[string]string{"p256dh": b64.EncodeToString(devKey.PublicKey().Bytes()), "auth": b64.EncodeToString(auth)}}})
	_ = os.WriteFile(filepath.Join(dir, push.SubscriptionsFile), subs, 0o600)

	sleepNames := sleep.EnvNames{IdleMin: "HS_TEST_SLEEP_IDLE_MIN", PressureMin: "HS_TEST_SLEEP_PRESSURE_MIN"}
	c := newCore(coreConfig{DataDir: dir, Home: home, Tmux: tm, Term: terminal.New(terminal.Config{Tmux: tm, Home: home}), MetaKey: "meta",
		Old:    &gate.OldRuntime{Upstream: strings.TrimPrefix(oldSrv.URL, "http://"), DataDir: dir, TokenHeaders: []string{"x-cli-token"}},
		OldAPI: "/api/internal", Prompts: promptConfigFromEnv(dir),
		Hooks: hookConfig{ConfigFile: filepath.Join(dir, "config.json"), PushSubject: "mailto:ops@example.org", PushTagPrefix: "app:", Sleep: sleepNames}})
	_ = os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":["core"]}`), 0o600)
	f, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: "127.0.0.1:1", DataDir: dir, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	token := func(r *http.Request) bool { return r.Header.Get("x-cli-token") == "ok" }
	refusal := func(r *http.Request) (int, any, http.Header, bool) {
		if token(r) {
			return 0, nil, nil, false
		}
		return 401, map[string]string{"error": "Unauthorized"}, http.Header{"Content-Type": {"application/json"}}, true
	}
	f.Add(c.routes(token, token)...)
	f.Add(c.hookRoutes(cliTokenFirst(dir, []string{"x-cli-token"}), refusal)...)
	srv := httptest.NewServer(f)
	defer srv.Close()
	post := func(path, body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-cli-token", "ok")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(raw)
	}
	stateOf := func(tabID string) string {
		if m := c.manager(); m != nil {
			info, _ := m.Tab(tabID)
			return info.CliState
		}
		return ""
	}
	waitState := func(tabID, want string) {
		t.Helper()
		for deadline := time.Now().Add(8 * time.Second); stateOf(tabID) != want; time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%s stayed %q, want %q", tabID, stateOf(tabID), want)
			}
		}
	}

	if code, body := post("/api/status/hook", `{"event":"stop","session":"x"}`); code != http.StatusServiceUnavailable || !strings.Contains(body, "starting") {
		t.Fatalf("an off core answered %d %s", code, body)
	}
	// Init starts only the agent tabs' sessions again; the shell tab's is the test's.
	if err := tm.CreateSession(shSession, 80, 24, home); err != nil {
		t.Fatal(err)
	}
	c.start()
	defer c.stop()

	// A Claude hook moves the tab through the core; the turn's end pushes to the subscribed device.
	at := strconv.FormatInt(time.Now().UnixMilli(), 10)
	if code, _ := post("/api/status/hook", `{"event":"prompt-submit","session":"`+shSession+`","at":`+at+`}`); code != 204 {
		t.Fatalf("hook %d", code)
	}
	waitState("tab-sh", "busy")
	if code, _ := post("/api/status/hook", `{"event":"stop","session":"`+shSession+`","at":`+at+`}`); code != 204 {
		t.Fatalf("hook %d", code)
	}
	waitState("tab-sh", "ready-for-review")
	got := pushSvc.waitFor(t, "/dev")
	if got.header.Get("Topic") != push.Topic("app:tab-sh") || got.header.Get("Urgency") != "normal" || !strings.HasPrefix(got.header.Get("Authorization"), "vapid t=") {
		t.Fatalf("push headers %v", got.header)
	}
	plain, err := push.Decrypt(got.body, devKey, auth)
	var msg push.Message
	if err != nil || json.Unmarshal(plain, &msg) != nil || msg.Kind != "done" || msg.TabID != "tab-sh" || msg.Tag != "app:tab-sh" {
		t.Fatalf("push body %s %v", plain, err)
	}

	// A Codex SessionStart reaches the old runtime's timeline.
	if code, _ := post("/api/status/hook?provider=codex&tmuxSession="+shSession, `{"hook_event_name":"SessionStart","session_id":"12345678-aaaa-bbbb-cccc-1234567890ab","cwd":"/w","source":"clear"}`); code != 204 {
		t.Fatalf("codex hook %d", code)
	}
	sent := oldRT.waitFor(t, "/api/internal/core/codex-session")
	var sink struct {
		TmuxSession  string         `json:"tmuxSession"`
		SessionInfo  map[string]any `json:"sessionInfo"`
		ClearSession bool           `json:"clearSession"`
	}
	if json.Unmarshal(sent.body, &sink) != nil || sink.TmuxSession != shSession || sink.SessionInfo["sessionId"] != "12345678-aaaa-bbbb-cccc-1234567890ab" ||
		sink.SessionInfo["status"] != "running" || !sink.ClearSession || sent.header.Get("x-cli-token") != "ok" {
		t.Fatalf("codex session %s", sent.body)
	}

	// Visibility goes to the sender and to the old runtime.
	if code, body := post("/api/push/visibility", `{"deviceId":"d1","visible":true}`); code != 200 || body != `{"ok":true}` {
		t.Fatalf("visibility %d %s", code, body)
	}
	if v := c.push.Store.Visible(); !v["d1"] {
		t.Fatal("not marked visible")
	}
	if g := oldRT.waitFor(t, "/api/push/visibility"); string(g.body) != `{"deviceId":"d1","visible":true}` {
		t.Fatalf("%s", g.body)
	}

	// The access rule comes from the config (an explicit HOST wins).
	t.Setenv("HOST", "10.0.0.0/8")
	if code, _ := post("/api/status/hook", `{}`); code != 403 {
		t.Fatalf("a caller outside the rule got %d", code)
	}
	t.Setenv("HOST", "")

	// session-process kill stops what runs under the tab's shell and keeps the shell.
	pane := tm.PanePID(shSession)
	if err := tm.SendKeys(shSession, "sleep 300 & sleep 301 &"); err != nil {
		out, _ := exec.Command("tmux", "-L", socket, "ls").CombinedOutput()
		t.Fatalf("%v; has %v; sessions:\n%s", err, tm.HasSession(shSession), out)
	}
	for deadline := time.Now().Add(5 * time.Second); len(proc.ChildPIDs(pane)) < 2; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("no background sleeps under %d", pane)
		}
	}
	kids := proc.ChildPIDs(pane)
	if code, body := post(platformAPI+"/session-process", `{"tabId":"tab-sh","action":"kill"}`); code != 200 || body != `{"killed":2}` {
		t.Fatalf("kill %d %s", code, body)
	}
	for _, k := range kids {
		for deadline := time.Now().Add(3 * time.Second); syscall.Kill(k, 0) == nil && !zombie(k); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("pid %d survived", k)
			}
		}
	}
	if syscall.Kill(pane, 0) != nil {
		t.Fatal("the tab's shell must stay")
	}
	waitState("tab-sh", "inactive")

	// One sleep sweep: the Claude tab, quiet for two days, has its agent stopped.
	clPane := tm.PanePID(clSession)
	if err := tm.SendKeys(clSession, "bash -c 'exec -a claude sleep 300'"); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); !c.claude.IsRunning(proc.ChildPIDs(clPane)); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the stand-in Claude did not start")
		}
	}
	c.mu.Lock()
	sl := c.sleeper
	c.mu.Unlock()
	sl.Getenv = func(k string) string { return map[string]string{"HS_TEST_SLEEP_IDLE_MIN": "120"}[k] }
	sl.MemRatio = func() (float64, bool) { return 0.5, true }
	sl.Now = func() time.Time { return time.Now().Add(time.Hour) }
	sl.KillTree = func(root int) int { return proc.KillTree(root, 500*time.Millisecond, 50*time.Millisecond, nil) }
	slept, pressure := sl.Sweep()
	if !slices.Equal(slept, []string{"tab-cl"}) || pressure {
		t.Fatalf("slept %v (state %s)", slept, stateOf("tab-cl"))
	}
	waitState("tab-cl", "inactive")
	if c.claude.IsRunning(proc.ChildPIDs(clPane)) {
		t.Fatal("the agent still runs")
	}
	// The removed tab's caches go with it.
	if sl.Activity.Len() == 0 {
		t.Fatal("the sweep read the transcript")
	}
	sl.ForgetTab("tab-cl")
	if sl.Activity.Len() != 0 {
		t.Fatal("not forgotten")
	}
}

func zombie(pid int) bool {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	s := string(raw)
	i := strings.LastIndexByte(s, ')')
	return i > 0 && len(s) > i+2 && s[i+2] == 'Z'
}
