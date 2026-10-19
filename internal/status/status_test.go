package status

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"harnessly/internal/agents"
	"harnessly/internal/history"
	"harnessly/internal/nodejson"
	"harnessly/internal/sockets"
	"harnessly/internal/tmux"
)

type fakeLayouts struct {
	mu        sync.Mutex
	ws        []Workspace
	tabs      map[string][]Tab
	persisted []string
	agent     []string
}

func (f *fakeLayouts) Workspaces() []Workspace { return f.ws }
func (f *fakeLayouts) Tabs(ws string) ([]Tab, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tabs[ws]
	return append([]Tab(nil), t...), ok
}
func (f *fakeLayouts) UpdateTabCliStatus(session, state string, dismissed Opt[float64], event string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.persisted = append(f.persisted, session+" "+state+" "+event)
	return nil
}
func (f *fakeLayouts) UpdateTabAgentState(session, provider string, sid, path, summary, last Opt[string]) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agent = append(f.agent, session+" "+path.Value())
	return nil
}
func (f *fakeLayouts) RecoverSession(string) (string, bool) { return "", false }

type fakeTmux struct {
	mu      sync.Mutex
	panes   map[string]tmux.PaneInfo
	silent  bool
	screens map[string]string
	keys    []string
	titles  map[string]string
	detail  tmux.PaneDetail
}

func (f *fakeTmux) ReadAllPanes() (map[string]tmux.PaneInfo, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.silent {
		return nil, false
	}
	out := map[string]tmux.PaneInfo{}
	for k, v := range f.panes {
		out[k] = v
	}
	return out, true
}
func (f *fakeTmux) PaneTitle(s string) string         { f.mu.Lock(); defer f.mu.Unlock(); return f.titles[s] }
func (f *fakeTmux) PaneDetail(string) tmux.PaneDetail { return f.detail }
func (f *fakeTmux) PanePID(s string) int              { f.mu.Lock(); defer f.mu.Unlock(); return f.panes[s].PID }
func (f *fakeTmux) Capture(s string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.screens[s]
	return c, ok
}
func (f *fakeTmux) CaptureAtWidth(s string, _, _ int) (string, bool) { return f.Capture(s) }
func (f *fakeTmux) SessionCwd(string) string                         { return "/srv/app" }
func (f *fakeTmux) SendRawKeys(s, k string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, s+" "+k)
	return nil
}
func (f *fakeTmux) ParseSessionName(n string) (string, string, string, bool) {
	p := strings.Split(n, "-")
	if len(p) != 4 {
		return "", "", "", false
	}
	return p[1], p[2], p[3], true
}
func (f *fakeTmux) set(session string, pane tmux.PaneInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.panes[session] = pane
}

type fakeProcs struct{}

func (fakeProcs) Children(pid int) []int       { return []int{pid + 1} }
func (fakeProcs) ListeningPorts(pid int) []int { return nil }

type fakeProvider struct {
	mu       sync.Mutex
	id       string
	running  map[int]bool
	snaps    map[string]agents.Snapshot
	detected string
}

func (p *fakeProvider) ID() string { return p.id }
func (p *fakeProvider) Running(pid int, _ []int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running[pid]
}
func (p *fakeProvider) Detect(pid int, _ []int) agents.SessionInfo {
	if p.Running(pid, nil) && p.detected != "" {
		return agents.SessionInfo{Status: agents.Running, JSONLPath: p.detected}
	}
	return agents.SessionInfo{Status: agents.NotRunning}
}
func (p *fakeProvider) Snapshot(path string, _ bool) (agents.Snapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snaps[path], nil
}
func (p *fakeProvider) HistoryStats(string) (*agents.HistoryStats, error) { return nil, nil }
func (p *fakeProvider) ParsePaneTitle(string) *string                     { return nil }
func (p *fakeProvider) SessionIDFromPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".jsonl")
}
func (p *fakeProvider) ReadSessionID(t agents.TabAgent) string { return "" }
func (p *fakeProvider) ReadJSONLPath(t agents.TabAgent) string { return "" }
func (p *fakeProvider) ReadSummary(t agents.TabAgent) string   { return "" }
func (p *fakeProvider) FindSession(string) string              { return "" }
func (p *fakeProvider) TranscriptIn(string, string) string     { return "" }
func (p *fakeProvider) RunsWithHooks([]int) (bool, bool)       { return true, true }
func (p *fakeProvider) setRunning(pid int, r bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running[pid] = r
}
func (p *fakeProvider) setSnap(path string, s agents.Snapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.snaps[path] = s
}

type recorder struct {
	mu   sync.Mutex
	msgs []map[string]any
}

func (r *recorder) Broadcast(v any, _ *sockets.Client) {
	var m map[string]any
	_ = json.Unmarshal(nodejson.Marshal(v), &m)
	r.mu.Lock()
	r.msgs = append(r.msgs, m)
	r.mu.Unlock()
}
func (r *recorder) Shutdown() {}
func (r *recorder) take() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.msgs
	r.msgs = nil
	return out
}
func (r *recorder) kinds() []string {
	var out []string
	for _, m := range r.take() {
		k, _ := m["type"].(string)
		if k == "status:update" {
			if s, ok := m["cliState"].(string); ok {
				k += ":" + s
			}
		}
		if k == "status:hook-event" {
			k += ":" + m["event"].(map[string]any)["name"].(string)
		}
		out = append(out, k)
	}
	return out
}

type env struct {
	m      *Manager
	rec    *recorder
	tm     *fakeTmux
	claude *fakeProvider
	lay    *fakeLayouts
	hist   *history.Store
	dir    string
	cancel context.CancelFunc
}

const session = "tk-ws1-p1-t1"

func newEnv(t *testing.T, tab Tab, pane *tmux.PaneInfo) *env {
	t.Helper()
	dir := t.TempDir()
	tm := &fakeTmux{panes: map[string]tmux.PaneInfo{}, screens: map[string]string{}, titles: map[string]string{}, detail: tmux.PaneDetail{Width: 120, Height: 40}}
	if pane != nil {
		tm.panes[tab.SessionName] = *pane
	}
	claude := &fakeProvider{id: agents.Claude, running: map[int]bool{}, snaps: map[string]agents.Snapshot{}}
	lay := &fakeLayouts{ws: []Workspace{{ID: "ws1", Name: "One", Directories: []string{"/srv/app"}}}, tabs: map[string][]Tab{"ws1": {tab}}}
	rec := &recorder{}
	hist := &history.Store{File: filepath.Join(dir, "session-history.json")}
	m := New(Deps{Layouts: lay, Tmux: tm, Procs: fakeProcs{}, Providers: map[string]Provider{agents.Claude: claude}, History: hist,
		NewID: func() string { return "hid" }}, rec)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m.Start(ctx)
	return &env{m: m, rec: rec, tm: tm, claude: claude, lay: lay, hist: hist, dir: dir, cancel: cancel}
}

func claudeTab(state string) Tab {
	return Tab{ID: "t1", SessionName: session, PanelType: Some(agents.ClaudePanel), CliState: state}
}

func (e *env) entry() Entry {
	e.m.mu.Lock()
	defer e.m.mu.Unlock()
	return *e.m.tabs["t1"]
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(6 * time.Second); !cond(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func TestHooksMoveTheStateAndAnnounceTheEventFirst(t *testing.T) {
	e := newEnv(t, claudeTab(Idle), &tmux.PaneInfo{Command: "claude", Path: "/srv/app", PID: 10})
	e.rec.take()
	for _, step := range []struct{ event, notif, state string }{
		{EventPromptSubmit, "", Busy},
		{EventNotification, "idle_prompt", Busy}, // not a question: no state change, no event
		{EventNotification, "permission_prompt", NeedsInput},
		{EventStop, "", ReadyForReview},
		{EventSessionStart, "", Idle},
	} {
		e.m.updateFromHook(session, step.event, step.notif)
		if got := e.entry().CliState; got != step.state {
			t.Fatalf("%s(%s): %s, want %s", step.event, step.notif, got, step.state)
		}
	}
	got := strings.Join(e.rec.kinds(), " ")
	want := "status:hook-event:prompt-submit status:update:busy status:hook-event:notification status:update:needs-input status:hook-event:stop status:update:ready-for-review status:hook-event:session-start status:update:idle"
	if got != want {
		t.Fatalf("messages:\n got  %s\n want %s", got, want)
	}
	if seq := e.entry().EventSeq; seq != 4 {
		t.Fatalf("seq %d", seq)
	}
}

func TestALateHookReportIsDropped(t *testing.T) {
	e := newEnv(t, claudeTab(Idle), &tmux.PaneInfo{Command: "claude", PID: 10})
	at := func(f float64) *float64 { return &f }
	e.m.HandleProviderEvent(agents.Claude, session, agents.Event{Kind: agents.Stop}, at(2000))
	e.m.HandleProviderEvent(agents.Claude, session, agents.Event{Kind: agents.PromptSubmit}, at(1000))
	if got := e.entry().CliState; got != ReadyForReview {
		t.Fatalf("a prompt fired before the stop moved the tab to %s", got)
	}
	if e.m.HandleProviderEvent(agents.Codex, session, agents.Event{Kind: agents.PromptSubmit}, nil) {
		t.Fatal("a Codex event reached a Claude tab")
	}
}

func TestClientEntryLeavesAbsentFieldsOut(t *testing.T) {
	e := newEnv(t, claudeTab(Idle), nil)
	var m map[string]any
	_ = json.Unmarshal(e.m.tabs["t1"].clientJSON(), &m)
	if _, has := m["currentProcess"]; has {
		t.Fatal("a tab without a pane has no currentProcess key")
	}
	for _, k := range []string{"agentSummary", "lastAssistantMessage", "readyForReviewAt", "busySince", "dismissedAt"} {
		if v, has := m[k]; !has || v != nil {
			t.Fatalf("%s should be null at the start: %v %v", k, v, has)
		}
	}
	if m["launchingSince"] != nil || m["launchFailed"] != false || m["hooksMissing"] != false {
		t.Fatalf("%v", m)
	}
}

func TestBusyAtRestartIsUnknownUntilSomethingDefinite(t *testing.T) {
	e := newEnv(t, claudeTab(Busy), &tmux.PaneInfo{Command: "zsh", PID: 10})
	waitFor(t, "resolve unknown", func() bool { return e.entry().CliState == Idle })
}

func TestDismissAndAck(t *testing.T) {
	e := newEnv(t, claudeTab(ReadyForReview), &tmux.PaneInfo{Command: "claude", PID: 10})
	e.rec.take()
	e.m.Dismiss("t1", nil)
	if en := e.entry(); en.CliState != Idle || !en.DismissedAt.Has() {
		t.Fatalf("%+v", en)
	}
	e.m.updateFromHook(session, EventNotification, "")
	seq := e.entry().EventSeq
	e.m.Ack("t1", float64(seq-1))
	if e.entry().CliState != NeedsInput {
		t.Fatal("an acknowledgement of an older question was taken")
	}
	e.m.Ack("t1", float64(seq))
	if e.entry().CliState != Busy {
		t.Fatal("the acknowledgement did not move the tab to busy")
	}
}

func TestGoneNeedsThreeWitnessesAndHealComesBack(t *testing.T) {
	transcript := filepath.Join(t.TempDir(), "s1.jsonl")
	_ = os.WriteFile(transcript, nil, 0o600)
	e := newEnv(t, claudeTab(Idle), &tmux.PaneInfo{Command: "claude", PID: 10})
	e.m.mu.Lock()
	e.m.tabs["t1"].JSONLPath = transcript
	e.m.mu.Unlock()
	e.tm.set(session, tmux.PaneInfo{Command: "zsh", PID: 10})
	e.tm.mu.Lock()
	e.tm.silent = false
	e.tm.mu.Unlock()
	e.claude.setRunning(10, true) // the fresh look still sees the agent
	e.m.Poll()
	if e.entry().CliState != Idle {
		t.Fatal("marked gone while a fresh look found the agent")
	}
	e.claude.setRunning(10, false)
	e.m.Poll()
	if e.entry().CliState != Inactive {
		t.Fatalf("not marked gone: %s", e.entry().CliState)
	}
	e.claude.setRunning(10, true)
	e.claude.setSnap(transcript, agents.Snapshot{Idle: true})
	e.tm.set(session, tmux.PaneInfo{Command: "claude", PID: 10})
	e.m.Poll()
	if e.entry().CliState != Idle {
		t.Fatalf("an inactive tab whose agent runs was not healed: %s", e.entry().CliState)
	}
}

func TestLaunchEndsWhenThePromptBoxIsReady(t *testing.T) {
	e := newEnv(t, claudeTab(Inactive), &tmux.PaneInfo{Command: "zsh", PID: 10})
	e.m.MarkAgentLaunch("t1", false)
	if e.entry().LaunchingSince == nil {
		t.Fatal("no launch under way")
	}
	e.claude.setRunning(10, true)
	e.tm.mu.Lock()
	e.tm.screens[session] = strings.Join([]string{" ▐▛███▛█   Claude Code v2.1.296", "❯ Try \"fix typecheck errors\"", "────────────────", "  ⏵⏵ bypass permissions on (shift+tab to cycle)", ""}, "\n")
	e.tm.mu.Unlock()
	waitFor(t, "synthetic session-start", func() bool { en := e.entry(); return en.LaunchingSince == nil && en.CliState == Idle })
}

func TestLaunchFailsWhenTheAgentWentAway(t *testing.T) {
	e := newEnv(t, claudeTab(Inactive), &tmux.PaneInfo{Command: "zsh", PID: 10})
	e.claude.setRunning(10, true)
	e.m.MarkAgentLaunch("t1", false)
	time.Sleep(900 * time.Millisecond)
	e.claude.setRunning(10, false)
	waitFor(t, "failed launch", func() bool { return e.entry().LaunchFailed })
}

func TestTranscriptStartsATurnWithoutAHook(t *testing.T) {
	transcript := filepath.Join(t.TempDir(), "s1.jsonl")
	_ = os.WriteFile(transcript, []byte("{}\n"), 0o600)
	e := newEnv(t, claudeTab(ReadyForReview), &tmux.PaneInfo{Command: "claude", PID: 10})
	e.m.mu.Lock()
	e.m.tabs["t1"].JSONLPath = transcript
	e.m.watch.start("t1", transcript)
	e.m.mu.Unlock()
	ts := e.m.now() + 5000
	e.claude.setSnap(transcript, agents.Snapshot{Idle: false, LastEntryTs: &ts})
	f, _ := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("{\"type\":\"user\"}\n")
	f.Close()
	waitFor(t, "synthetic prompt-submit", func() bool { return e.entry().CliState == Busy })
}

func TestStopSavesTheTurnToHistory(t *testing.T) {
	transcript := filepath.Join(t.TempDir(), "s1.jsonl")
	_ = os.WriteFile(transcript, nil, 0o600)
	e := newEnv(t, claudeTab(Busy), &tmux.PaneInfo{Command: "claude", PID: 10})
	e.claude.setRunning(10, true)
	e.m.mu.Lock()
	en := e.m.tabs["t1"]
	en.JSONLPath, en.CliState, en.LastUserMessage = transcript, Busy, Some("do it")
	e.m.mu.Unlock()
	e.m.updateFromHook(session, EventStop, "")
	waitFor(t, "history entry", func() bool { return len(e.hist.All()) == 1 })
	h := e.hist.All()[0]
	if *h.Prompt != "do it" || h.TabID != "t1" || h.WorkspaceName != "One" {
		t.Fatalf("%+v", h)
	}
}

func TestPollAddsAndRemovesTabsAndCoalesces(t *testing.T) {
	e := newEnv(t, claudeTab(Idle), &tmux.PaneInfo{Command: "claude", PID: 10})
	e.rec.take()
	e.lay.mu.Lock()
	e.lay.tabs["ws1"] = []Tab{{ID: "t2", SessionName: "tk-ws1-p1-t2", Name: "shell"}}
	e.lay.mu.Unlock()
	e.m.Poll()
	got := e.rec.take()
	var removed, added bool
	for _, msg := range got {
		if msg["tabId"] == "t1" && msg["cliState"] == nil {
			removed = true
		}
		if msg["tabId"] == "t2" {
			added = true
		}
	}
	if !removed || !added {
		t.Fatalf("%v", got)
	}
	e.tm.mu.Lock()
	e.tm.silent = true
	e.tm.mu.Unlock()
	e.m.Poll() // tmux silent: nothing judged, nothing removed
	if len(e.m.AgentSources()) != 0 || e.m.Tabs() == nil {
		t.Fatal("unexpected")
	}
	e.m.mu.Lock()
	n := len(e.m.tabs)
	e.m.mu.Unlock()
	if n != 1 {
		t.Fatalf("a silent tmux removed tabs: %d", n)
	}
}
