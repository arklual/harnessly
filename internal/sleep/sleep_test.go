package sleep

import (
	"io"
	"math"
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

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/claude"
	"github.com/ly-solutions/harnessly/internal/background"
	"github.com/ly-solutions/harnessly/internal/proc"
	"github.com/ly-solutions/harnessly/internal/sessions"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

const (
	mn    = 60_000.0
	now   = 1_000 * mn
	clean = "idle"
)

var config = Config{IdleMs: 120 * mn, PressureIdleMs: 20 * mn, ResumeRecentMs: 120 * mn}

func agent(tabID string, idleMinutes float64, patch ...func(*Candidate)) Candidate {
	c := Candidate{TabID: tabID, Provider: "claude", CliState: clean, LastActivityAt: now - idleMinutes*mn}
	for _, p := range patch {
		p(&c)
	}
	return c
}

func state(s string) func(*Candidate) { return func(c *Candidate) { c.CliState = s } }

// agent-sleep: puts to sleep only idle Claude agents past the threshold, oldest first.
func TestPickOldestFirst(t *testing.T) {
	got := Pick([]Candidate{agent("fresh", 30), agent("old", 600), agent("older", 1500), agent("just-over", 121)}, now, false, config)
	if !slices.Equal(got, []string{"older", "old", "just-over"}) {
		t.Fatalf("%v", got)
	}
}

// agent-sleep: never touches a working, asking or background-running agent, nor Codex.
func TestPickNeverTouchesWorking(t *testing.T) {
	got := Pick([]Candidate{
		agent("busy", 900, state("busy")),
		agent("asks", 900, state("needs-input")),
		agent("unknown", 900, state("unknown")),
		agent("monitor", 900, func(c *Candidate) { c.BackgroundTasks = 1 }),
		agent("codex", 900, func(c *Candidate) { c.Provider = "codex" }),
		agent("gone", 900, state("inactive")),
	}, now, true, config)
	if len(got) != 0 {
		t.Fatalf("%v", got)
	}
}

// agent-sleep: keeps an unread answer awake unless memory is short.
func TestPickUnreadAnswer(t *testing.T) {
	c := []Candidate{agent("unread", 900, state("ready-for-review"))}
	if got := Pick(c, now, false, config); len(got) != 0 {
		t.Fatal(got)
	}
	if got := Pick(c, now, true, config); !slices.Equal(got, []string{"unread"}) {
		t.Fatal(got)
	}
}

// agent-sleep: shortens the threshold under memory pressure.
func TestPickPressureThreshold(t *testing.T) {
	c := []Candidate{agent("half-hour", 30), agent("ten-minutes", 10)}
	if got := Pick(c, now, false, config); len(got) != 0 {
		t.Fatal(got)
	}
	if got := Pick(c, now, true, config); !slices.Equal(got, []string{"half-hour"}) {
		t.Fatal(got)
	}
	// Idle off, pressure on: only pressure puts agents to sleep.
	if got := Pick(c, now, true, Config{PressureIdleMs: 20 * mn}); !slices.Equal(got, []string{"half-hour"}) {
		t.Fatal(got)
	}
	if got := Pick(c, now, false, Config{PressureIdleMs: 20 * mn}); len(got) != 0 {
		t.Fatal(got)
	}
}

// agent-sleep: is off when both thresholds are zero.
func TestPickOff(t *testing.T) {
	if got := Pick([]Candidate{agent("old", 9000)}, now, true, Config{}); got == nil || len(got) != 0 {
		t.Fatal(got)
	}
}

func at(v float64) *float64 { return &v }

// agent-sleep: resume at startup.
func TestShouldResumeAtStartup(t *testing.T) {
	midTurnOnly := config
	midTurnOnly.ResumeRecentMs = 0
	if !ShouldResumeAtStartup("busy", nil, now, midTurnOnly) || !ShouldResumeAtStartup("needs-input", at(now-900*mn), now, midTurnOnly) {
		t.Fatal("a conversation that was working or asking comes back, however old")
	}
	for _, s := range []string{"idle", "inactive", "ready-for-review", "unknown", ""} {
		if ShouldResumeAtStartup(s, at(now-1*mn), now, midTurnOnly) {
			t.Errorf("%q resumed by default", s)
		}
	}
	if !ShouldResumeAtStartup("idle", at(now-30*mn), now, config) || ShouldResumeAtStartup("inactive", at(now-600*mn), now, config) ||
		ShouldResumeAtStartup("", nil, now, config) {
		t.Fatal("an opted-in window brings back the recently touched ones only")
	}
}

var names = EnvNames{IdleMin: "APP_SLEEP_IDLE_MIN", PressureMin: "APP_SLEEP_PRESSURE_MIN", ResumeRecentMin: "APP_RESUME_RECENT_MIN"}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// agent-sleep: the default window is none; the thresholds are read from the environment.
func TestReadConfig(t *testing.T) {
	c := ReadConfig(env(nil), names)
	if c != (Config{IdleMs: 360 * mn, PressureIdleMs: 20 * mn, ResumeRecentMs: 0}) {
		t.Fatalf("defaults %+v", c)
	}
	for raw, want := range map[string]float64{
		"30": 30, " 45 ": 45, "0": 0, "1.5": 1.5, "0x10": 16, "1e1": 10, "  ": 0,
		"-5": 360, "abc": 360, "Infinity": 360, "NaN": 360, "1_0": 360, "inf": 360, "5m": 360,
	} {
		if got := ReadConfig(env(map[string]string{names.IdleMin: raw}), names).IdleMs; got != want*mn {
			t.Errorf("%q: %v minutes, want %v", raw, got/mn, want)
		}
	}
	if ReadConfig(env(map[string]string{"": "5"}), EnvNames{}).IdleMs != 360*mn {
		t.Fatal("unnamed variables are not read")
	}
}

// agent-sleep: memory pressure reads the available share from /proc/meminfo; conversation activity.
func TestPressureAndActivityHelpers(t *testing.T) {
	r, ok := proc.ParseMemAvailableRatio("MemTotal:       24000000 kB\nMemFree:          400000 kB\nMemAvailable:    2400000 kB\n")
	if !ok || math.Abs(r-0.1) > 1e-9 {
		t.Fatal(r)
	}
	if _, ok := proc.ParseMemAvailableRatio("garbage"); ok {
		t.Fatal("garbage")
	}
	lines := []string{
		`{"type":"user","timestamp":"2026-09-28T08:00:00.000Z"}`,
		`{"type":"assistant","timestamp":"2026-09-28T08:05:00.000Z"}`,
		`{"type":"assistant","isSidechain":true,"timestamp":"2026-09-28T09:00:00.000Z"}`,
		`{"type":"cost-state","sessionId":"s"}`,
		`{"type":"bridge-session","sessionId":"s"}`,
		`{"type":"assis`,
	}
	var raw [][]byte
	for _, l := range lines {
		raw = append(raw, []byte(l))
	}
	want, _ := time.Parse(time.RFC3339, "2026-09-28T08:05:00Z")
	if got := claude.LastConversationAt(claude.Parse(raw)); got == nil || *got != float64(want.UnixMilli()) {
		t.Fatal(got)
	}
	if claude.LastConversationAt(claude.Parse([][]byte{[]byte(`{"type":"cost-state"}`)})) != nil {
		t.Fatal("nothing in the tail")
	}
	s := &Sleeper{MemRatio: func() (float64, bool) { return 0.149, true }}
	if !s.underPressure() {
		t.Fatal("below 0.15")
	}
	s.MemRatio = func() (float64, bool) { return 0, false }
	if s.underPressure() {
		t.Fatal("unreadable is no pressure")
	}
}

// --- the sweep with a fake state machine and tmux

type fakeTabs struct {
	mu      sync.Mutex
	tabs    []Tab
	current map[string]string // tabID → state now, when it differs
}

func (f *fakeTabs) AgentTabs() []Tab { return f.tabs }

func (f *fakeTabs) Current(id string) (Tab, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tabs {
		if t.TabID == id {
			if s, ok := f.current[id]; ok {
				if s == "" {
					return Tab{}, false
				}
				t.CliState = s
			}
			return t, true
		}
	}
	return Tab{}, false
}

type fakePanes struct {
	panes    map[string]tmux.PaneInfo
	screens  map[string]string
	silent   bool
	captures int
	mu       sync.Mutex
}

func (f *fakePanes) ReadAllPanes() (map[string]tmux.PaneInfo, bool) { return f.panes, !f.silent }

func (f *fakePanes) Capture(name string) (string, bool) {
	f.mu.Lock()
	f.captures++
	f.mu.Unlock()
	s, ok := f.screens[name]
	return s, ok
}

func (f *fakePanes) HasSession(name string) bool { _, ok := f.panes[name]; return ok }
func (f *fakePanes) PanePID(name string) int     { return f.panes[name].PID }

type rig struct {
	s       *Sleeper
	tabs    *fakeTabs
	panes   *fakePanes
	killed  []int
	gone    []string
	home    string
	nowTime time.Time
	argsOf  map[int]string
}

const footer = "\n╭────╮\n│ >  │\n╰────╯\n  ⏵⏵ bypass permissions on (shift+tab to cycle)\n"

// newRig: pane pids 100+i, each with a child 1000+i that is Claude unless told otherwise.
func newRig(t *testing.T) *rig {
	r := &rig{home: t.TempDir(), nowTime: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}
	r.tabs = &fakeTabs{current: map[string]string{}}
	r.panes = &fakePanes{panes: map[string]tmux.PaneInfo{}, screens: map[string]string{}}
	args := map[int]string{}
	r.s = &Sleeper{
		Tabs:  r.tabs,
		Panes: r.panes,
		Claude: &claude.Detector{Dir: filepath.Join(r.home, ".claude"), Match: agents.MatchArgv, Procs: proc.Procs{
			Children: func(pid int) []int {
				if _, ok := args[pid+900]; ok {
					return []int{pid + 900}
				}
				return nil
			},
			Args: func(pid int) (string, bool) { a, ok := args[pid]; return a, ok },
		}},
		Activity:   &claude.ActivityReader{},
		Background: &background.Reader{TempDir: r.home, Now: func() float64 { return float64(r.nowTime.UnixMilli()) }},
		Getenv:     env(map[string]string{names.IdleMin: "120"}),
		Env:        names,
		MemRatio:   func() (float64, bool) { return 0.5, true },
		KillTree: func(pid int) int {
			r.killed = append(r.killed, pid)
			return 3
		},
		MarkGone: func(id string) { r.gone = append(r.gone, id) },
		Now:      func() time.Time { return r.nowTime },
	}
	r.argsOf = args
	return r
}

func (r *rig) addTab(t *testing.T, i int, tab Tab, command, procArgs string, idleMinutes float64, screen string) Tab {
	t.Helper()
	pane := 100 + i
	tab.TmuxSession = "s" + strconv.Itoa(i)
	if tab.TabID == "" {
		tab.TabID = "t" + strconv.Itoa(i)
	}
	if tab.PanelType == "" {
		tab.PanelType = agents.ClaudePanel
	}
	if tab.Provider == "" {
		tab.Provider = agents.Claude
	}
	if tab.CliState == "" {
		tab.CliState = "idle"
	}
	r.panes.panes[tab.TmuxSession] = tmux.PaneInfo{Command: command, PID: pane}
	if screen != "" {
		r.panes.screens[tab.TmuxSession] = screen
	}
	if procArgs != "" {
		r.argsOf[pane+900] = procArgs
	}
	if idleMinutes >= 0 {
		ts := r.nowTime.Add(-time.Duration(idleMinutes * float64(time.Minute))).UTC().Format(time.RFC3339Nano)
		path := filepath.Join(r.home, "t"+strconv.Itoa(i)+".jsonl")
		if tab.JSONLPath == "" && tab.AgentSessionID != "" {
			path = filepath.Join(r.home, ".claude", "projects", sessions.ProjectName("/w"), tab.AgentSessionID+".jsonl")
			_ = os.MkdirAll(filepath.Dir(path), 0o700)
		} else if tab.JSONLPath == "" {
			tab.JSONLPath = path
		}
		body := `{"type":"user","timestamp":"` + ts + `","message":{"content":"go"}}` + "\n" + `{"type":"cost-state"}` + "\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r.tabs.tabs = append(r.tabs.tabs, tab)
	return tab
}

func TestSweepPicksAndKills(t *testing.T) {
	r := newRig(t)
	r.addTab(t, 1, Tab{}, "claude-2.1", "claude --settings x", 600, footer)
	r.addTab(t, 2, Tab{}, "claude", "claude", 30, footer)                                                    // too fresh
	r.addTab(t, 3, Tab{}, "bash", "claude", 600, footer)                                                     // shell in the foreground
	r.addTab(t, 4, Tab{}, "node", "vim notes.md", 600, footer)                                               // no Claude
	r.addTab(t, 5, Tab{}, "claude", "claude", 600, "  ✻ Thinking… (12s · esc to interrupt)"+footer)          // a turn runs
	r.addTab(t, 6, Tab{}, "claude", "claude", 600, footer+"  · 2 shells ·\n")                                // background work
	r.addTab(t, 7, Tab{CliState: "busy"}, "claude", "claude", 600, footer)                                   // working
	r.addTab(t, 8, Tab{PanelType: agents.CodexPanel, Provider: agents.Codex}, "codex", "codex", 600, footer) // Codex
	r.addTab(t, 9, Tab{CliState: "inactive", AgentSessionID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}, "claude", "claude", 1000, "")
	r.addTab(t, 10, Tab{LastEventName: "session-start", LastEventAt: float64(r.nowTime.UnixMilli())}, "claude", "claude", 700, footer)
	r.addTab(t, 11, Tab{LastEventName: "stop", LastEventAt: float64(r.nowTime.UnixMilli())}, "claude", "claude", 700, footer)
	r.addTab(t, 12, Tab{}, "claude", "claude", -1, footer)                              // no transcript
	r.addTab(t, 13, Tab{CliState: "ready-for-review"}, "claude", "claude", 800, footer) // unread, no pressure
	r.addTab(t, 14, Tab{}, "claude", "claude", 900, footer)                             // moves to busy before its turn
	r.tabs.current["t14"] = "busy"
	r.addTab(t, 15, Tab{}, "claude", "claude", 10, "  esc to interrupt (an old line)\n"+footer) // seven lines up: not the footer

	cands := r.s.Candidates()
	ids := []string{}
	for _, c := range cands {
		ids = append(ids, c.TabID)
		if c.TabID == "t9" && c.CliState != "idle" {
			t.Errorf("inactive reads as idle: %+v", c)
		}
		if c.TabID == "t10" && c.LastActivityAt != float64(r.nowTime.Add(-700*time.Minute).UnixMilli()) {
			t.Errorf("a SessionStart is not activity: %+v", c)
		}
	}
	slices.Sort(ids)
	if want := []string{"t1", "t10", "t11", "t13", "t14", "t15", "t2", "t6", "t9"}; !slices.Equal(ids, want) {
		t.Fatalf("candidates %v, want %v", ids, want)
	}
	slept, pressure := r.s.Sweep()
	if pressure || !slices.Equal(slept, []string{"t9", "t10", "t1"}) {
		t.Fatalf("slept %v pressure %v", slept, pressure)
	}
	if !slices.Equal(r.killed, []int{109, 110, 101}) || !slices.Equal(r.gone, []string{"t9", "t10", "t1"}) {
		t.Fatalf("killed %v gone %v", r.killed, r.gone)
	}
	// Under pressure the threshold drops and the unread answer may sleep too.
	r.killed, r.gone = nil, nil
	r.s.MemRatio = func() (float64, bool) { return 0.05, true }
	slept, pressure = r.s.Sweep()
	if !pressure || !slices.Equal(slept, []string{"t9", "t13", "t10", "t1", "t2"}) || slices.Contains(slept, "t15") {
		t.Fatalf("pressure: slept %v", slept)
	}
}

func TestSweepOffAndSilentTmux(t *testing.T) {
	r := newRig(t)
	r.addTab(t, 1, Tab{}, "claude", "claude", 600, footer)
	r.s.Getenv = env(map[string]string{names.IdleMin: "0", names.PressureMin: "0"})
	if slept, _ := r.s.Sweep(); len(slept) != 0 || r.panes.captures != 0 {
		t.Fatal("off: nothing is looked at")
	}
	r.s.Getenv = env(nil)
	r.panes.silent = true
	if c := r.s.Candidates(); len(c) != 0 {
		t.Fatal("tmux did not answer: no candidates")
	}
	r.panes.silent = false
	r.tabs.current["t1"] = ""
	if slept, _ := r.s.Sweep(); len(slept) != 0 || len(r.killed) != 0 {
		t.Fatal("a tab gone before its turn is not touched")
	}
	delete(r.panes.panes, "s1")
	if n := r.s.Kill("t1", "s1"); n != 0 || len(r.gone) != 0 {
		t.Fatal("no session, nothing to kill, nothing marked")
	}
}

func TestForgetEvictsCaches(t *testing.T) {
	r := newRig(t)
	tab := r.addTab(t, 1, Tab{}, "claude", "claude", 600, footer)
	r.s.Candidates()
	if r.s.Activity.Len() != 1 {
		t.Fatal("cached")
	}
	r.s.Forget(tab.JSONLPath)
	if r.s.Activity.Len() != 0 {
		t.Fatal("forgotten")
	}
	r.s.Candidates()
	r.s.ForgetTab("t1")
	if r.s.Activity.Len() != 0 {
		t.Fatal("a removed tab's transcript is forgotten")
	}
	r.s.ForgetTab("unknown")
}

// --- the kill, on a private process tree

func gone(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return true
	}
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	s := string(raw)
	i := strings.LastIndexByte(s, ')')
	return i > 0 && len(s) > i+2 && s[i+2] == 'Z'
}

// startPane is a stand-in for a pane's shell running an agent: the agent has a background shell, and a grandchild
// that ignores SIGTERM and outlives its parent.
func startPane(t *testing.T) (*exec.Cmd, io.WriteCloser) {
	t.Helper()
	dir := t.TempDir()
	agentScript := filepath.Join(dir, "agent.sh")
	_ = os.WriteFile(agentScript, []byte("#!/bin/sh\nsleep 310 &\n(trap '' TERM; exec sleep 311) &\nwait\n"), 0o700)
	pane := exec.Command("sh", "-c", agentScript+" & read x")
	pane.Dir = dir
	stdin, _ := pane.StdinPipe()
	if err := pane.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, p := range proc.Descendants(pane.Process.Pid) {
			_ = syscall.Kill(p, syscall.SIGKILL)
		}
		_ = stdin.Close()
		_ = pane.Process.Kill()
		_ = pane.Wait()
	})
	deadline := time.Now().Add(3 * time.Second)
	for len(proc.Descendants(pane.Process.Pid)) < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("tree not up: %v", proc.Descendants(pane.Process.Pid))
		}
		time.Sleep(20 * time.Millisecond)
	}
	return pane, stdin
}

func TestKillTreeOnRealProcesses(t *testing.T) {
	pane, _ := startPane(t)
	tree := proc.Descendants(pane.Process.Pid)
	start := time.Now()
	n := proc.KillTree(pane.Process.Pid, 600*time.Millisecond, 50*time.Millisecond, nil)
	if n != len(tree) || n < 3 {
		t.Fatalf("stopped %d of %v", n, tree)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Fatal("the process ignoring SIGTERM must have kept the grace period running")
	}
	deadline := time.Now().Add(2 * time.Second)
	for _, p := range tree {
		for !gone(p) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if !gone(p) {
			t.Errorf("pid %d survived", p)
		}
	}
	if gone(pane.Process.Pid) {
		t.Fatal("the pane's shell stays")
	}
	for _, p := range proc.Descendants(pane.Process.Pid) {
		if !gone(p) {
			t.Errorf("left below the shell: %d (only an unreaped zombie may stay)", p)
		}
	}
}

func TestSweepKillsARealTree(t *testing.T) {
	pane, _ := startPane(t)
	r := newRig(t)
	r.s.KillTree = func(root int) int { return proc.KillTree(root, 400*time.Millisecond, 50*time.Millisecond, nil) }
	tab := r.addTab(t, 1, Tab{}, "claude", "claude", 600, footer)
	info := r.panes.panes[tab.TmuxSession]
	// The fake Claude child hangs off the real pane pid; the kill reads the real tree below it.
	info.PID = pane.Process.Pid
	r.panes.panes[tab.TmuxSession] = info
	r.argsOf[pane.Process.Pid+900] = "claude"
	tree := proc.Descendants(pane.Process.Pid)
	slept, _ := r.s.Sweep()
	if !slices.Equal(slept, []string{"t1"}) || !slices.Equal(r.gone, []string{"t1"}) {
		t.Fatalf("slept %v gone %v", slept, r.gone)
	}
	time.Sleep(200 * time.Millisecond)
	for _, p := range tree {
		if !gone(p) {
			t.Errorf("pid %d survived", p)
		}
	}
	if gone(pane.Process.Pid) {
		t.Fatal("the pane's shell stays")
	}
}
