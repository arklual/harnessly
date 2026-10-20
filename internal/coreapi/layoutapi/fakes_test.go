package layoutapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/paneintents"
	"github.com/ly-solutions/harnessly/internal/panel"
	"github.com/ly-solutions/harnessly/internal/state"
	"github.com/ly-solutions/harnessly/internal/tmux"
	"github.com/ly-solutions/harnessly/internal/vault"
)

// fakeTmux is tmux as a set of session names; what was asked of it is recorded.
type fakeTmux struct {
	mu       sync.Mutex
	names    *tmux.Tmux
	home     string
	sessions map[string]bool
	calls    [][]string
	screen   string
	command  string
	cwd      string
}

func newFakeTmux(home string) *fakeTmux {
	return &fakeTmux{names: tmux.New(tmux.Names{Socket: "unused", Prefix: "tk-", LegacyPrefix: "pt-"}), home: home,
		sessions: map[string]bool{}, screen: "$ ", command: "bash"}
}

func (f *fakeTmux) note(c ...string) {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
}

func (f *fakeTmux) took() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

func (f *fakeTmux) CreateSession(name string, cols, rows int, cwd string) error {
	f.note("create", name, cwd)
	f.mu.Lock()
	f.sessions[name] = true
	f.mu.Unlock()
	return nil
}
func (f *fakeTmux) KillSession(name string) {
	f.note("kill", name)
	f.mu.Lock()
	delete(f.sessions, name)
	f.mu.Unlock()
}
func (f *fakeTmux) HasSession(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions[name]
}
func (f *fakeTmux) SendKeys(name, line string) error { f.note("send", name, line); return nil }
func (f *fakeTmux) SendBracketedPaste(name, text string) error {
	f.note("paste", name, text)
	return nil
}
func (f *fakeTmux) SendKeySequence(name string, keys ...string) error {
	f.note(append([]string{"keys", name}, keys...)...)
	return nil
}
func (f *fakeTmux) ListSessions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for s := range f.sessions {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
func (f *fakeTmux) KillServer() {
	f.note("kill-server")
	f.mu.Lock()
	f.sessions = map[string]bool{}
	f.mu.Unlock()
}
func (f *fakeTmux) ApplyConfig(string)                { f.note("config") }
func (f *fakeTmux) Names() tmux.Names                 { return f.names.Names() }
func (f *fakeTmux) SessionName(w, p, t string) string { return f.names.SessionName(w, p, t) }
func (f *fakeTmux) ParseSessionName(n string) (string, string, string, bool) {
	return f.names.ParseSessionName(n)
}
func (f *fakeTmux) PaneShellName(id string, legacy bool) (string, bool) {
	return f.names.PaneShellName(id, legacy)
}
func (f *fakeTmux) ResolveExistingDir(p string) string {
	if p == "" {
		return f.home
	}
	for c := p; ; {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
		parent := filepath.Dir(c)
		if parent == c {
			return f.home
		}
		c = parent
	}
}
func (f *fakeTmux) SessionCwd(string) string         { return f.cwd }
func (f *fakeTmux) PaneCurrentCommand(string) string { return f.command }
func (f *fakeTmux) Capture(name string) (string, bool) {
	return f.screen, true
}
func (f *fakeTmux) CheckTerminalProcess(name string) (bool, string) {
	return f.command == "bash" || f.command == "zsh", f.command
}

type fakeStatus struct {
	mu         sync.Mutex
	registered map[string]TabEntry
	removed    []string
	launched   []string
	agentTabs  map[string]string
	hooks      map[string]bool
	rescans    int
}

func newFakeStatus() *fakeStatus {
	return &fakeStatus{registered: map[string]TabEntry{}, agentTabs: map[string]string{}, hooks: map[string]bool{}}
}

func (s *fakeStatus) RegisterTab(id string, e TabEntry) {
	s.mu.Lock()
	s.registered[id] = e
	s.mu.Unlock()
}
func (s *fakeStatus) RemoveTab(id string) {
	s.mu.Lock()
	s.removed = append(s.removed, id)
	s.mu.Unlock()
}
func (s *fakeStatus) MarkAgentLaunch(id string, reset bool) {
	s.mu.Lock()
	s.launched = append(s.launched, id+":"+strconv.FormatBool(reset))
	s.mu.Unlock()
}
func (s *fakeStatus) HooksMissing(id string) bool    { return s.hooks[id] }
func (s *fakeStatus) AgentTab(session string) string { return s.agentTabs[session] }
func (s *fakeStatus) Rescan()                        { s.rescans++ }

type fakeAgents struct {
	unavailable map[string]*Unavailable
	started     []StartRequest
	start       StartResult
	autoResumed int
}

func (a *fakeAgents) Availability(panelType string) *Unavailable { return a.unavailable[panelType] }
func (a *fakeAgents) ValidateLaunch(provider string, model, effort any) (*agents.Launch, string) {
	if model == "bad" {
		return nil, "Недопустимое имя модели"
	}
	l, _ := agents.ReadLaunchShape(model, effort)
	return l, ""
}
func (a *fakeAgents) LaunchLine(provider, ws string, l *agents.Launch) (string, error) {
	line := provider + "-launch --ws " + ws
	if l != nil && l.Model != nil {
		line += " --model " + *l.Model
	}
	return line, nil
}
func (a *fakeAgents) ResumeLine(provider, id, ws string, l *agents.Launch) (string, error) {
	line := provider + " --resume " + id
	if l != nil && l.Model != nil {
		line += " --model " + *l.Model
	}
	return line, nil
}
func (a *fakeAgents) Start(r StartRequest) StartResult {
	a.started = append(a.started, r)
	return a.start
}
func (a *fakeAgents) AutoResume() { a.autoResumed++ }

type fakeSync struct {
	mu      sync.Mutex
	events  []*jsvalue.Object
	clients int
	onEvent func(*jsvalue.Object)
}

func (s *fakeSync) Broadcast(e *jsvalue.Object) {
	s.mu.Lock()
	s.events = append(s.events, e)
	on := s.onEvent
	s.mu.Unlock()
	if on != nil {
		on(e)
	}
}
func (s *fakeSync) ClientCount() int { return s.clients }

const token = "cli-secret"

type rig struct {
	t      *testing.T
	home   string
	data   string
	tm     *fakeTmux
	status *fakeStatus
	agents *fakeAgents
	sync   *fakeSync
	deps   *Deps
	h      http.Handler
	shadow Route
}

func newRig(t *testing.T) *rig {
	t.Helper()
	home, _ := filepath.EvalSymlinks(t.TempDir())
	data := filepath.Join(home, "data")
	tm := newFakeTmux(home)
	st := newFakeStatus()
	ag := &fakeAgents{unavailable: map[string]*Unavailable{}}
	sy := &fakeSync{clients: 1}
	store := state.New(state.Config{DataDir: data, Tmux: tm, Home: home, MetaKey: "meta"})
	publish := func(e *jsvalue.Object) { sy.Broadcast(e) }
	d := &Deps{
		Store: store, Tmux: tm, Status: st, Agents: ag, Sync: sy,
		Intents: paneintents.New(filepath.Join(data, "pane-intents.json"), nil, false),
		Panel:   panel.NewBridge(publish, sy.ClientCount),
		Vault:   vault.NewBridge(publish, sy.ClientCount),
		Targets: panel.Targets{PortalHost: "a.example.org", ReaderHost: "ai.example.org", GitLabHosts: []string{"gitlab.example.org"}},
		Session: func(r *http.Request) bool {
			c, err := r.Cookie("session-token")
			return err == nil && c.Value == "good"
		},
		CLIToken: func(r *http.Request) bool {
			for _, h := range []string{"X-Tok", "X-Old-Tok"} {
				if vs, ok := r.Header[h]; ok {
					return vs[0] == token
				}
			}
			return false
		},
		PlatformAPI: "/api/platform", Home: home, ShellReady: 10 * time.Millisecond, PanelWait: 100 * time.Millisecond,
	}
	main, shadow := Routes(d)
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	return &rig{t: t, home: home, data: data, tm: tm, status: st, agents: ag, sync: sy, deps: d, h: main.Handler, shadow: shadow}
}

type answer struct {
	status int
	header http.Header
	body   string
}

func (a answer) json(t *testing.T) *jsvalue.Object {
	t.Helper()
	v, err := jsvalue.Parse([]byte(a.body))
	if err != nil {
		t.Fatalf("not JSON: %q", a.body)
	}
	o, _ := v.(*jsvalue.Object)
	return o
}

// who: "s" a page session, "t" the cli-token, "" nobody.
func (r *rig) do(who, method, path, body string, extra ...string) answer {
	r.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	switch who {
	case "s":
		req.Header.Set("Cookie", "session-token=good")
	case "t":
		req.Header.Set("X-Tok", token)
	}
	for i := 0; i+1 < len(extra); i += 2 {
		req.Header.Set(extra[i], extra[i+1])
	}
	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, req)
	raw, _ := io.ReadAll(rec.Result().Body)
	return answer{status: rec.Code, header: rec.Result().Header, body: string(raw)}
}

func (r *rig) want(a answer, status int, body string) {
	r.t.Helper()
	if a.status != status || (body != "*" && a.body != body) {
		r.t.Fatalf("got %d %s, want %d %s", a.status, a.body, status, body)
	}
}

func (r *rig) mkdir(name string) string {
	p := filepath.Join(r.home, name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		r.t.Fatal(err)
	}
	return p
}

// workspace creates a workspace in a new folder and returns its id, its first pane and tab.
func (r *rig) workspace(name string) (ws, pane, tab string) {
	r.t.Helper()
	dir := r.mkdir(name)
	a := r.do("s", "POST", "/api/workspace", `{"directory":"`+dir+`","name":"`+name+`"}`)
	r.want(a, 200, "*")
	ws = a.json(r.t).String("id")
	l := r.deps.Store.Layout(ws)
	p := state.Panes(l)[0]
	return ws, p.String("id"), state.Tabs(p)[0].String("id")
}

func (f *fakeTmux) CwdAndLastCommand(string) (string, *string) {
	if f.cwd == "" {
		return "", nil
	}
	last := "npm run dev"
	return f.cwd, &last
}
