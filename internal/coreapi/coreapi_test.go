package coreapi

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"harnessly/internal/agents"
	"harnessly/internal/gate"
	"harnessly/internal/status"
)

// --- fakes of the keeper's collaborators

type call struct {
	what string
	args []any
}

type fakeKeeper struct {
	mu       sync.Mutex
	calls    []call
	applied  bool
	cliState string
	recover  func(string) (bool, string)
	sources  []status.AgentSource
}

func (k *fakeKeeper) note(what string, args ...any) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.calls = append(k.calls, call{what, args})
}

func (k *fakeKeeper) HandleProviderEvent(p, s string, ev agents.Event, at *float64) bool {
	var a any
	if at != nil {
		a = *at
	}
	k.note("event", p, s, ev, a)
	return true
}

func (k *fakeKeeper) ApplyHookMeta(p, s string, meta status.HookMeta) (string, string, bool) {
	k.note("meta", p, s, meta)
	return "tab-1", k.cliState, k.applied
}

func (k *fakeKeeper) MarkAgentLaunch(tabID string, reset bool)   { k.note("launch", tabID, reset) }
func (k *fakeKeeper) MarkAgentGone(tabID string, endLaunch bool) { k.note("gone", tabID, endLaunch) }
func (k *fakeKeeper) RecoverUnknown(tabID string) (bool, string) { return k.recover(tabID) }
func (k *fakeKeeper) Poll()                                      { k.note("poll") }
func (k *fakeKeeper) SynthesizeEvent(session, event string)      { k.note("synth", session, event) }

func (k *fakeKeeper) AgentSources() []status.AgentSource {
	k.mu.Lock()
	defer k.mu.Unlock()
	return slices.Clone(k.sources)
}

func (k *fakeKeeper) named(what string) []call {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []call
	for _, c := range k.calls {
		if c.what == what {
			out = append(out, c)
		}
	}
	return out
}

func (k *fakeKeeper) waitFor(t *testing.T, what string) []call {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c := k.named(what); len(c) > 0 {
			return c
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no %s", what)
	return nil
}

type fakeSink struct {
	mu    sync.Mutex
	infos []agents.SessionInfo
	clear []string
}

func (s *fakeSink) CodexSession(session string, info agents.SessionInfo, clear bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.infos = append(s.infos, info)
	if clear {
		s.clear = append(s.clear, session)
	}
}

type fakePanes struct {
	mu       sync.Mutex
	sessions map[string]int // session → pane pid
	safe     bool
	created  []string
	sent     []string
	createE  error
}

func (p *fakePanes) HasSession(n string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.sessions[n]
	return ok
}

func (p *fakePanes) PanePID(n string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessions[n]
}

func (p *fakePanes) CreateSession(n string, cols, rows int, cwd string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.createE != nil {
		return p.createE
	}
	p.created = append(p.created, n+"@"+cwd)
	p.sessions[n] = 42
	return nil
}

func (p *fakePanes) CheckTerminalProcess(string) (bool, string) { return p.safe, "bash" }

func (p *fakePanes) SendKeys(n, line string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, n+": "+line)
	return nil
}

type fakeTabs map[string]FoundTab

func (f fakeTabs) FindTab(id string) (FoundTab, bool) { t, ok := f[id]; return t, ok }

type fakeVisibility struct{ marks []string }

func (v *fakeVisibility) MarkVisible(id string) { v.marks = append(v.marks, "+"+id) }
func (v *fakeVisibility) MarkHidden(id string)  { v.marks = append(v.marks, "-"+id) }

// --- the rig: the routes behind a real gate with the core slice on

const token = "tok"

type rig struct {
	t       *testing.T
	cfg     *Config
	keeper  *fakeKeeper
	sink    *fakeSink
	panes   *fakePanes
	vis     *fakeVisibility
	running map[int]bool
	srv     *httptest.Server
	mu      sync.Mutex
	mirror  []string
}

func newRig(t *testing.T) *rig {
	r := &rig{t: t, keeper: &fakeKeeper{applied: true, cliState: "inactive"}, sink: &fakeSink{}, vis: &fakeVisibility{},
		panes: &fakePanes{sessions: map[string]int{}, safe: true}, running: map[int]bool{}}
	r.keeper.recover = func(string) (bool, string) { return false, "no-entry" }
	sid := "11111111-2222-4333-8444-555555555555"
	r.cfg = &Config{
		Keeper:  r.keeper,
		TokenOK: func(req *http.Request) bool { return req.Header.Get("x-app-token") == token },
		APIRefusal: func(req *http.Request) (int, any, http.Header, bool) {
			if req.Header.Get("x-app-token") == token {
				return 0, nil, nil, false
			}
			return 401, map[string]string{"error": "Unauthorized"}, http.Header{"Content-Type": {"application/json"}}, true
		},
		Access:        func() Access { return ParseAccess("localhost") },
		CodexSessions: r.sink,
		Tabs: fakeTabs{
			"tab-1":  {WorkspaceID: "ws", Directories: []string{"/w"}, Tab: tab("tab-1", "s1", agents.ClaudePanel, &sid)},
			"tab-2":  {WorkspaceID: "ws", Directories: []string{"/w"}, Tab: tab("tab-2", "s2", agents.ClaudePanel, nil)},
			"tab-3":  {WorkspaceID: "ws", Directories: []string{"/w1", "/w2"}, Cwd: "", Tab: tab("tab-3", "s3", agents.ClaudePanel, &sid)},
			"tab-sh": {WorkspaceID: "ws", Tab: tab("tab-sh", "s4", "terminal", nil)},
		},
		Panes: r.panes,
		Agents: map[string]Agent{agents.Claude: {
			ReadSessionID: func(t agents.TabAgent) string {
				if t.AgentState != nil && t.AgentState.SessionID != nil {
					return *t.AgentState.SessionID
				}
				return ""
			},
			Running: func(pid int) bool { r.mu.Lock(); defer r.mu.Unlock(); return r.running[pid] },
			ResumeCommand: func(id, ws string, l *agents.Launch) (string, error) {
				if !agents.IsUUID(id) {
					return "", errors.New("Invalid session ID format: " + id)
				}
				model := ""
				if l != nil && l.Model != nil {
					model = " --model '" + *l.Model + "'"
				}
				return "claude --resume " + id + model + " # " + ws, nil
			},
		}},
		RecoverSession: func(session, provider string) (string, bool) {
			if session == "s2" {
				return "22222222-2222-4222-8222-222222222222", true
			}
			return "", false
		},
		LaunchOf:           func(session string) *agents.Launch { m := "opus"; return &agents.Launch{Model: &m} },
		KillTree:           func(pid int) int { return 3 },
		Visibility:         r.vis,
		MirrorVisibility:   func(id string, v bool) { r.mu.Lock(); defer r.mu.Unlock(); r.mirror = append(r.mirror, id) },
		SessionProcessPath: "/api/app/session-process",
		Timings:            Timings{ShellReady: time.Millisecond, BackstopPoll: 2 * time.Millisecond, BackstopWait: 20 * time.Millisecond, BackstopGrace: 5 * time.Millisecond},
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":["core"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	g, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: "127.0.0.1:1", DataDir: dir, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	g.Add(g.AsRoutes(r.cfg.Routes()...)...)
	r.srv = httptest.NewServer(g)
	t.Cleanup(r.srv.Close)
	return r
}

func tab(id, session, panel string, sid *string) status.Tab {
	t := status.Tab{ID: id, SessionName: session, PanelType: status.Some(panel)}
	t.Agent = agents.TabAgent{ID: id}
	if sid != nil {
		t.Agent.AgentState = &agents.AgentState{ProviderID: agents.Claude, SessionID: sid}
	}
	return t
}

type reply struct {
	code   int
	body   string
	header http.Header
}

func (r *rig) do(method, path, contentType, body string, authed bool) reply {
	r.t.Helper()
	req, _ := http.NewRequest(method, r.srv.URL+path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if authed {
		req.Header.Set("x-app-token", token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return reply{res.StatusCode, string(raw), res.Header}
}

func (r *rig) post(path, body string) reply {
	return r.do("POST", path, "application/json", body, true)
}

func expect(t *testing.T, got reply, code int, body string) {
	t.Helper()
	if got.code != code || got.body != body {
		t.Fatalf("got %d %q, want %d %q", got.code, got.body, code, body)
	}
}

// --- hook

func TestHookAuthAndMethod(t *testing.T) {
	r := newRig(t)
	got := r.do("GET", "/api/status/hook", "", "", true)
	expect(t, got, 405, `{"error":"method not allowed"}`)
	if got.header.Get("Allow") != "POST" || got.header.Get("ETag") == "" || got.header.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("%v", got.header)
	}
	expect(t, r.do("POST", "/api/status/hook", "application/json", `{}`, false), 403, `{"error":"forbidden"}`)
	r.cfg.Access = func() Access { return ParseAccess("tailscale") }
	expect(t, r.post("/api/status/hook", `{}`), 403, `{"error":"forbidden"}`)
	r.cfg.Access = func() Access { return ParseAccess("localhost") }
	expect(t, r.post("/api/status/hook", `{bad`), 400, "Invalid JSON")
	expect(t, r.post("/api/status/hook", `"`+strings.Repeat("x", bodyLimit)+`"`), 413, "Body exceeded 1mb limit")
	if len(r.keeper.calls) != 0 {
		t.Fatalf("%v", r.keeper.calls)
	}
}

func TestClaudeHook(t *testing.T) {
	r := newRig(t)
	expect(t, r.post("/api/status/hook", `{"event":"prompt-submit","session":"s1","at":1700000000123}`), 204, "")
	expect(t, r.post("/agents/api/status/hook", `{"event":"notification","session":"s1","notificationType":"permission_prompt","at":"x"}`), 204, "")
	ev := r.keeper.named("event")
	if len(ev) != 2 || ev[0].args[0] != "claude" || ev[0].args[1] != "s1" || ev[0].args[2].(agents.Event).Kind != agents.PromptSubmit || ev[0].args[3] != 1700000000123.0 {
		t.Fatalf("%+v", ev)
	}
	if e := ev[1].args[2].(agents.Event); e.Kind != agents.Notification || e.NotificationType != "permission_prompt" || ev[1].args[3] != nil {
		t.Fatalf("%+v", ev[1])
	}
	expect(t, r.post("/api/status/hook", `{"event":"bogus","session":"s1"}`), 204, "")
	if len(r.keeper.named("event")) != 2 || len(r.keeper.named("poll")) != 0 {
		t.Fatal("an unknown event is ignored")
	}
	for _, body := range []string{`{"event":"poll","session":"s1"}`, `{"event":"stop","session":""}`, `{}`, `[1]`} {
		expect(t, r.post("/api/status/hook", body), 204, "")
	}
	expect(t, r.do("POST", "/api/status/hook", "text/plain", "hello", true), 204, "")
	deadline := time.Now().Add(2 * time.Second)
	for len(r.keeper.named("poll")) < 5 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := len(r.keeper.named("poll")); n != 5 {
		t.Fatalf("%d polls", n)
	}
}

func TestCodexHook(t *testing.T) {
	r := newRig(t)
	expect(t, r.post("/api/status/hook?provider=codex", `{}`), 400, `{"error":"missing tmuxSession"}`)
	expect(t, r.post("/api/status/hook?provider=codex&tmuxSession=a&tmuxSession=b", `{}`), 400, `{"error":"missing tmuxSession"}`)
	sid := "12345678-aaaa-bbbb-cccc-1234567890ab"
	expect(t, r.post("/api/status/hook?provider=codex&tmuxSession=s1", `{"hook_event_name":"UserPromptSubmit","session_id":"`+sid+`","transcript_path":"/t.jsonl","prompt":"Сделай"}`), 204, "")
	meta := r.keeper.named("meta")[0].args[2].(status.HookMeta)
	if meta.SessionID.Value() != sid || meta.JSONLPath.Value() != "/t.jsonl" || meta.LastUserMessage.Value() != "Сделай" || meta.AgentSummary.Value() != "Сделай" || meta.PermissionRequest != nil {
		t.Fatalf("%+v", meta)
	}
	if ev := r.keeper.named("event"); len(ev) != 1 || ev[0].args[0] != "codex" || ev[0].args[2].(agents.Event).Kind != agents.PromptSubmit || ev[0].args[3] != nil {
		t.Fatalf("%+v", ev)
	}
	// SessionStart of a /clear goes to the session watchers and is applied in any state.
	r.keeper.cliState = "idle"
	expect(t, r.post("/api/status/hook?provider=codex&tmuxSession=s1", `{"hook_event_name":"SessionStart","session_id":"`+sid+`","cwd":"/w","source":"clear"}`), 204, "")
	if len(r.sink.infos) != 1 || r.sink.infos[0].SessionID != sid || r.sink.infos[0].Cwd != "/w" || !slices.Equal(r.sink.clear, []string{"s1"}) {
		t.Fatalf("%+v %v", r.sink.infos, r.sink.clear)
	}
	if !r.keeper.named("meta")[1].args[2].(status.HookMeta).ClearMessages || len(r.keeper.named("event")) != 2 {
		t.Fatal("clear")
	}
	// A startup SessionStart moves only an inactive or unknown tab.
	expect(t, r.post("/api/status/hook?provider=codex&tmuxSession=s1", `{"hook_event_name":"SessionStart","source":"startup"}`), 204, "")
	if len(r.keeper.named("event")) != 2 || len(r.sink.clear) != 1 {
		t.Fatal("not emitted for an idle tab")
	}
	if m := r.keeper.named("meta")[2].args[2].(status.HookMeta); m.SessionID != nil {
		t.Fatal("a payload without session_id keeps the stored id")
	}
	expect(t, r.post("/api/status/hook?provider=codex&tmuxSession=s1", `{"hook_event_name":"PermissionRequest","request_type":"ExecApprovalRequest","exec_command":{"command":"pnpm test"}}`), 204, "")
	if m := r.keeper.named("meta")[3].args[2].(status.HookMeta); m.PermissionRequest == nil || string(*m.PermissionRequest) != `{"type":"ExecApprovalRequest","command":"pnpm test"}` {
		t.Fatalf("%+v", m)
	}
	expect(t, r.post("/api/status/hook?provider=codex&tmuxSession=s1", `{"hook_event_name":"Stop"}`), 204, "")
	if m := r.keeper.named("meta")[4].args[2].(status.HookMeta); m.PermissionRequest == nil || string(*m.PermissionRequest) != "null" {
		t.Fatal("Stop clears the request")
	}
	// A session no tab has: nothing else happens.
	r.keeper.applied = false
	n := len(r.keeper.named("event"))
	expect(t, r.post("/api/status/hook?provider=codex&tmuxSession=nope", `{"hook_event_name":"Stop"}`), 204, "")
	if len(r.keeper.named("event")) != n {
		t.Fatal("unknown session")
	}
}

// --- agent-launch, recover-unknown, visibility

func TestAgentLaunch(t *testing.T) {
	r := newRig(t)
	expect(t, r.do("POST", "/api/status/agent-launch", "application/json", `{"tabId":"t"}`, false), 401, `{"error":"Unauthorized"}`)
	expect(t, r.do("GET", "/api/status/agent-launch", "", "", true), 405, `{"error":"Method not allowed"}`)
	expect(t, r.post("/api/status/agent-launch", `{}`), 400, `{"error":"tabId is required"}`)
	expect(t, r.post("/api/status/agent-launch", `{"tabId":"tab-1"}`), 204, "")
	expect(t, r.post("/api/status/agent-launch", `{"tabId":"tab-1","resetAgentSession":true}`), 204, "")
	expect(t, r.post("/api/status/agent-launch", `{"tabId":"tab-1","resetAgentSession":"yes"}`), 204, "")
	expect(t, r.post("/api/status/agent-launch", `{"tabId":7}`), 204, "")
	l := r.keeper.named("launch")
	if len(l) != 3 || l[0].args[1] != false || l[1].args[1] != true || l[2].args[1] != false {
		t.Fatalf("%+v", l)
	}
}

func TestRecoverUnknown(t *testing.T) {
	r := newRig(t)
	expect(t, r.post("/api/tmux/recover-unknown", `{"tabId":1}`), 400, `{"error":"tabId required"}`)
	expect(t, r.post("/api/tmux/recover-unknown", `{"tabId":"t"}`), 200, `{"recovered":false,"reason":"no-entry"}`)
	r.keeper.recover = func(string) (bool, string) { return true, "" }
	expect(t, r.post("/api/tmux/recover-unknown", `{"tabId":"t"}`), 200, `{"recovered":true}`)
	r.keeper.recover = func(string) (bool, string) { panic("tmux") }
	expect(t, r.post("/api/tmux/recover-unknown", `{"tabId":"t"}`), 500, `{"error":"recover-unknown failed"}`)
}

func TestVisibility(t *testing.T) {
	r := newRig(t)
	expect(t, r.post("/api/push/visibility", `{"deviceId":"d1"}`), 400, `{"error":"Invalid body"}`)
	expect(t, r.post("/api/push/visibility", `{"visible":true}`), 400, `{"error":"Invalid body"}`)
	expect(t, r.post("/api/push/visibility", `{"deviceId":"d1","visible":true}`), 200, `{"ok":true}`)
	expect(t, r.post("/api/push/visibility", `{"deviceId":"d1","visible":false}`), 200, `{"ok":true}`)
	if !slices.Equal(r.vis.marks, []string{"+d1", "-d1"}) {
		t.Fatal(r.vis.marks)
	}
	deadline := time.Now().Add(time.Second)
	for {
		r.mu.Lock()
		n := len(r.mirror)
		r.mu.Unlock()
		if n == 2 || time.Now().After(deadline) {
			if n != 2 {
				t.Fatalf("mirrored %d", n)
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// --- session-process

func TestSessionProcessAnswers(t *testing.T) {
	r := newRig(t)
	got := r.do("GET", "/api/app/session-process", "", "", true)
	if got.code != 405 || got.body != "" || got.header.Get("Allow") != "POST" {
		t.Fatalf("%+v", got)
	}
	expect(t, r.do("POST", "/api/app/session-process", "application/json", `{}`, false), 401, `{"error":"Unauthorized"}`)
	expect(t, r.post("/api/app/session-process", `{"tabId":"tab-1","action":"stop"}`), 400, `{"error":"Нужны tabId и action"}`)
	expect(t, r.post("/api/app/session-process", `{"tabId":"nope","action":"kill"}`), 404, `{"error":"Разговор не найден"}`)
	// No tmux session: nothing to kill, nothing marked.
	expect(t, r.post("/api/app/session-process", `{"tabId":"tab-1","action":"kill"}`), 200, `{"killed":0}`)
	r.panes.sessions["s1"] = 100
	expect(t, r.post("/api/app/session-process", `{"tabId":"tab-1","action":"kill"}`), 200, `{"killed":3}`)
	if g := r.keeper.named("gone"); len(g) != 1 || g[0].args[0] != "tab-1" || g[0].args[1] != true {
		t.Fatalf("%+v", g)
	}
}

func TestSessionProcessResume(t *testing.T) {
	r := newRig(t)
	expect(t, r.post("/api/app/session-process", `{"tabId":"tab-sh","action":"resume"}`), 409, `{"error":"У разговора нет сессии, которую можно поднять"}`)
	// The machine rebooted: the session is made again in the tab's folder (the workspace's first without one).
	r.keeper.sources = []status.AgentSource{{TabID: "tab-3", CliState: "inactive", EventSeq: 4}}
	expect(t, r.post("/api/app/session-process", `{"tabId":"tab-3","action":"resume"}`), 200, `{"result":"resumed"}`)
	if !slices.Equal(r.panes.created, []string{"s3@/w1"}) || !slices.Equal(r.panes.sent, []string{"s3: claude --resume 11111111-2222-4333-8444-555555555555 --model 'opus' # ws"}) {
		t.Fatalf("%v %v", r.panes.created, r.panes.sent)
	}
	if l := r.keeper.named("launch"); len(l) != 1 || l[0].args[0] != "tab-3" || l[0].args[1] != false {
		t.Fatalf("%+v", l)
	}
	// No SessionStart came while the agent ran: one is synthesized.
	r.mu.Lock()
	r.running[42] = true
	r.mu.Unlock()
	if s := r.keeper.waitFor(t, "synth"); s[0].args[0] != "s3" || s[0].args[1] != "session-start" {
		t.Fatalf("%+v", s)
	}
	// Already running.
	expect(t, r.post("/api/app/session-process", `{"tabId":"tab-3","action":"resume"}`), 200, `{"result":"running"}`)
	// A tab that lost its id recovers it; a busy terminal refuses.
	r.panes.sessions["s2"] = 7
	r.panes.safe = false
	expect(t, r.post("/api/app/session-process", `{"tabId":"tab-2","action":"resume"}`), 409, `{"error":"Во вкладке занят терминал — сначала освободите его"}`)
	r.panes.safe = true
	expect(t, r.post("/api/app/session-process", `{"tabId":"tab-2","action":"resume"}`), 200, `{"result":"resumed"}`)
	if last := r.panes.sent[len(r.panes.sent)-1]; !strings.Contains(last, "22222222-2222-4222-8222-222222222222") {
		t.Fatal(last)
	}
}

func TestSessionProcessBackstopStaysQuietAfterAHook(t *testing.T) {
	r := newRig(t)
	r.panes.sessions["s1"] = 5
	r.keeper.sources = []status.AgentSource{{TabID: "tab-1", CliState: "inactive", EventSeq: 1}}
	expect(t, r.post("/api/app/session-process", `{"tabId":"tab-1","action":"resume"}`), 200, `{"result":"resumed"}`)
	r.keeper.mu.Lock()
	r.keeper.sources = []status.AgentSource{{TabID: "tab-1", CliState: "idle", EventSeq: 2}}
	r.keeper.mu.Unlock()
	time.Sleep(60 * time.Millisecond)
	if len(r.keeper.named("synth")) != 0 {
		t.Fatal("the hook came: nothing synthesized")
	}
}

func TestSessionProcessErrors(t *testing.T) {
	r := newRig(t)
	bad := "not-a-uuid"
	r.cfg.Tabs.(fakeTabs)["tab-bad"] = FoundTab{WorkspaceID: "ws", Tab: tab("tab-bad", "s9", agents.ClaudePanel, &bad)}
	r.panes.sessions["s9"] = 9
	got := r.post("/api/app/session-process", `{"tabId":"tab-bad","action":"resume"}`)
	if got.code != 500 || got.body != "Internal Server Error" || got.header.Get("Content-Type") != "text/plain" {
		t.Fatalf("%+v", got)
	}
	r.panes.createE = errors.New("tmux")
	got = r.post("/api/app/session-process", `{"tabId":"tab-1","action":"resume"}`)
	if got.code != 500 {
		t.Fatalf("%+v", got)
	}
}

func TestRoutesOnlyWithTheSlice(t *testing.T) {
	c := (&Config{}).Routes()
	paths := map[string]bool{}
	for _, rt := range c {
		if rt.Slice != Slice {
			t.Fatal(rt.Slice)
		}
		req := httptest.NewRequest("POST", "/api/status/hook", nil)
		paths["hook"] = paths["hook"] || rt.Match(req)
	}
	if len(c) != 4 || !paths["hook"] {
		t.Fatalf("%d routes", len(c))
	}
}

func TestAccess(t *testing.T) {
	for _, c := range []struct {
		spec, addr string
		ok         bool
	}{
		{"all", "8.8.8.8:1", true},
		{"localhost", "127.0.0.1:5555", true},
		{"localhost", "[::1]:80", true},
		{"localhost", "[::ffff:127.0.0.1]:80", true},
		{"localhost", "10.0.0.1:80", false},
		{"localhost,tailscale", "100.100.1.2:80", true},
		{"localhost,tailscale", "[fd7a:115c:a1e0::5]:1", true},
		{"lan", "192.168.1.5", true},
		{"10.1.0.0/16, bogus", "10.1.2.3:4", true},
		{"10.1.0.0/16", "10.2.0.1:4", false},
		{"localhost", "", false},
		{"", "127.0.0.1:1", false},
	} {
		if got := ParseAccess(c.spec).Allowed(c.addr); got != c.ok {
			t.Errorf("%q %q: %v", c.spec, c.addr, got)
		}
	}
	if !AccessFor("").Allowed("1.2.3.4:5") || AccessFor("localhost").Allowed("100.64.0.1:1") || !AccessFor("tailscale").Allowed("100.64.0.1:1") {
		t.Fatal("network access settings")
	}
}
