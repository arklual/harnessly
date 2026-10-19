// Package sleep puts idle agents to sleep: an idle Claude holds a few hundred megabytes with its MCP bridges whatever it
// does, and a machine that never lets one go runs out of memory. A slept conversation stays a chat; its next prompt
// resumes it. Every five minutes the sweep looks at the agent tabs, picks Claude agents quiet for longer than a
// threshold (shorter under memory pressure) and stops their process trees, keeping the pane's shell, the tab and the
// transcript.
package sleep

import (
	"context"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"harnessly/internal/agents"
	"harnessly/internal/agents/claude"
	"harnessly/internal/background"
	"harnessly/internal/jscompat"
	"harnessly/internal/proc"
	"harnessly/internal/tmux"
)

const (
	minute = 60_000.0
	// SweepEvery is how often the sweep runs; the first runs one period after the start.
	SweepEvery = 5 * time.Minute
	// sweepConcurrency: a tmux call per tab, all at once, for 170 tabs kept tmux and the runtime busy long enough that
	// the status poll running alongside timed out.
	sweepConcurrency = 4
	// PressureRatio: memory is short when less than this share of it is available.
	PressureRatio = 0.15
	// termGrace and killPoll: how long the agent gets to exit on SIGTERM, and how often survivors are looked at.
	termGrace = 3 * time.Second
	killPoll  = 150 * time.Millisecond

	defaultIdleMin     = 360
	defaultPressureMin = 20
	defaultResumeMin   = 0
)

// EnvNames are the variables the thresholds are read from, in minutes; the deployment chooses the names.
type EnvNames struct {
	// IdleMin: an idle agent sleeps after this long (default 360; 0 never).
	IdleMin string
	// PressureMin: under memory pressure the threshold drops to this, and unread answers may sleep too (default 20).
	PressureMin string
	// ResumeRecentMin: at startup a conversation that was not mid-turn is resumed only if it was active this recently
	// (default 0: none).
	ResumeRecentMin string
}

// Config are the thresholds in milliseconds; 0 turns a rule off.
type Config struct {
	IdleMs, PressureIdleMs, ResumeRecentMs float64
}

// jsNumber is JavaScript's Number(s) for what an environment variable may hold.
func jsNumber(s string) float64 {
	t := jscompat.Trim(s)
	if t == "" {
		return 0
	}
	if len(t) > 2 && t[0] == '0' {
		base := map[byte]int{'x': 16, 'X': 16, 'o': 8, 'O': 8, 'b': 2, 'B': 2}[t[1]]
		if base != 0 {
			n, err := strconv.ParseUint(t[2:], base, 64)
			if err != nil {
				return math.NaN()
			}
			return float64(n)
		}
	}
	switch strings.TrimLeft(t, "+-") {
	case "Infinity":
		return math.Inf(1)
	}
	if strings.Trim(t, "0123456789+-.eE") != "" {
		return math.NaN()
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return math.NaN()
	}
	return f
}

// envMinutes reads minutes: unset, empty, not a number or negative takes the default.
func envMinutes(getenv func(string) string, name string, fallback float64) float64 {
	v := math.NaN()
	if name != "" {
		if raw := getenv(name); raw != "" {
			v = jsNumber(raw)
		}
	}
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		v = fallback
	}
	return v * minute
}

// ReadConfig reads the thresholds; the sweep calls it every time, so a change needs no restart.
func ReadConfig(getenv func(string) string, names EnvNames) Config {
	return Config{
		IdleMs:         envMinutes(getenv, names.IdleMin, defaultIdleMin),
		PressureIdleMs: envMinutes(getenv, names.PressureMin, defaultPressureMin),
		ResumeRecentMs: envMinutes(getenv, names.ResumeRecentMin, defaultResumeMin),
	}
}

// Candidate is an agent tab the sweep may put to sleep.
type Candidate struct {
	TabID    string
	Provider string
	CliState string
	// LastActivityAt is the newest of the last user or assistant message and the last hook event other than
	// SessionStart, in epoch ms.
	LastActivityAt  float64
	BackgroundTasks int
}

// Pick is which agents to put to sleep, oldest first. Working, asking and background-running agents never sleep, nor
// Codex; an unread answer (ready-for-review) keeps its agent unless memory is short.
func Pick(candidates []Candidate, now float64, underPressure bool, c Config) []string {
	threshold := c.IdleMs
	if underPressure && c.PressureIdleMs > 0 {
		idle := c.IdleMs
		if idle == 0 {
			idle = math.Inf(1)
		}
		threshold = math.Min(c.PressureIdleMs, idle)
	}
	if threshold == 0 {
		return []string{}
	}
	var keep []Candidate
	for _, x := range candidates {
		if x.Provider == agents.Claude && (x.CliState == "idle" || underPressure && x.CliState == "ready-for-review") &&
			x.BackgroundTasks == 0 && now-x.LastActivityAt >= threshold {
			keep = append(keep, x)
		}
	}
	slices.SortStableFunc(keep, func(a, b Candidate) int {
		switch {
		case a.LastActivityAt < b.LastActivityAt:
			return -1
		case a.LastActivityAt > b.LastActivityAt:
			return 1
		}
		return 0
	})
	out := make([]string, len(keep))
	for i, x := range keep {
		out[i] = x.TabID
	}
	return out
}

// ShouldResumeAtStartup: only a conversation that was mid-turn when the runtime stopped gets its agent back, plus,
// when the window is set, the ones active within it. lastActivityAt is nil when unknown.
func ShouldResumeAtStartup(persistedState string, lastActivityAt *float64, now float64, c Config) bool {
	if persistedState == "busy" || persistedState == "needs-input" {
		return true
	}
	if c.ResumeRecentMs == 0 {
		return false
	}
	return lastActivityAt != nil && now-*lastActivityAt < c.ResumeRecentMs
}

// Tab is the state machine's view of an agent tab.
type Tab struct {
	TabID       string
	PanelType   string
	Provider    string
	CliState    string
	TmuxSession string
	// JSONLPath is the transcript when known; AgentSessionID finds it otherwise.
	JSONLPath      string
	AgentSessionID string
	// LastEventName and LastEventAt (epoch ms, 0 when none) are the last work-state event.
	LastEventName string
	LastEventAt   float64
}

// Tabs is where the sweep reads the agent tabs from.
type Tabs interface {
	// AgentTabs are the tabs whose panel runs an agent.
	AgentTabs() []Tab
	// Current is a tab's state now (false when it is gone), asked again right before its agent is stopped.
	Current(tabID string) (Tab, bool)
}

// Panes is what the sweep asks tmux; *tmux.Tmux has it.
type Panes interface {
	ReadAllPanes() (map[string]tmux.PaneInfo, bool)
	Capture(name string) (string, bool)
	HasSession(name string) bool
	PanePID(name string) int
}

// Sleeper runs the sweep.
type Sleeper struct {
	Tabs  Tabs
	Panes Panes
	// Claude tells whether Claude runs under a pane and where its transcripts are.
	Claude *claude.Detector
	// Activity and Background read transcripts; both keep a cache per transcript (see Forget).
	Activity   *claude.ActivityReader
	Background *background.Reader
	// Getenv and Env read the thresholds at every sweep.
	Getenv func(string) string
	Env    EnvNames
	// MemRatio is the available share of memory (proc.MemAvailableRatio by default); unreadable is no pressure.
	MemRatio func() (float64, bool)
	// KillTree stops a pane's processes (proc.KillTree with a 3 s grace by default) and returns how many it stopped.
	KillTree func(panePID int) int
	// MarkGone tells the state machine the tab's agent is gone (it goes inactive silently, its launch ends).
	MarkGone func(tabID string)
	Now      func() time.Time

	seenMu sync.Mutex
	seen   map[string]string // tab → the transcript the sweep read for it
}

// remember notes the transcript read for a tab, so ForgetTab can drop it.
func (s *Sleeper) remember(tabID, path string) {
	s.seenMu.Lock()
	defer s.seenMu.Unlock()
	if s.seen == nil {
		s.seen = map[string]string{}
	}
	if old, ok := s.seen[tabID]; ok && old != path {
		s.Forget(old)
	}
	s.seen[tabID] = path
}

// ForgetTab drops what is cached of the transcript the sweep read for a tab that is gone.
func (s *Sleeper) ForgetTab(tabID string) {
	s.seenMu.Lock()
	path, ok := s.seen[tabID]
	delete(s.seen, tabID)
	s.seenMu.Unlock()
	if ok {
		s.Forget(path)
	}
}

func (s *Sleeper) now() float64 {
	if s.Now != nil {
		return float64(s.Now().UnixMilli())
	}
	return float64(time.Now().UnixMilli())
}

// sleepable: inactive counts too, as a runtime restart restores the persisted state and an agent idle through it stays
// inactive until its next hook; such a tab sleeps only when its Claude really runs and its transcript is found.
func sleepable(state string) bool {
	return state == "idle" || state == "ready-for-review" || state == "inactive"
}

func isAgentPanel(p string) bool { return p == agents.ClaudePanel || p == agents.CodexPanel }

var turnRunning = regexp.MustCompile(`(?i)esc to interrupt`)

// lastLines is the last n lines of a capture, as split('\n').slice(-n) gives them.
func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}

func (s *Sleeper) transcript(t Tab) string {
	if t.JSONLPath != "" {
		return t.JSONLPath
	}
	if t.AgentSessionID != "" {
		return claude.FindTranscript(s.Claude.ProjectsDir(), t.AgentSessionID)
	}
	return ""
}

// candidate looks at one tab; nil when it cannot sleep (no pane, a shell in the foreground, no Claude, no transcript
// or no conversation in it, a turn visibly running).
func (s *Sleeper) candidate(t Tab, pane tmux.PaneInfo, ok bool) *Candidate {
	if !ok || pane.Command == "" || tmux.SafeShells[pane.Command] || pane.PID <= 0 {
		return nil
	}
	if !s.Claude.IsRunning(s.Claude.Procs.ChildrenOf(pane.PID)) {
		return nil
	}
	path := s.transcript(t)
	if path == "" {
		return nil
	}
	s.remember(t.TabID, path)
	conversationAt := s.Activity.At(path)
	if conversationAt == nil {
		return nil
	}
	var tasks []background.Task
	var screen string
	var captured bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if s.Background != nil {
			tasks = s.Background.Read(path)
		}
	}()
	screen, captured = s.Panes.Capture(t.TmuxSession)
	wg.Wait()
	// A stale state can hide a turn in progress; Claude's own footer cannot.
	if captured && turnRunning.MatchString(lastLines(screen, 6)) {
		return nil
	}
	var counts *background.Counts
	if captured {
		counts = background.ParseFooter(screen)
	}
	state := t.CliState
	if state == "inactive" {
		state = "idle"
	}
	event := t.LastEventAt
	if t.LastEventName == string(agents.SessionStart) {
		event = 0
	}
	return &Candidate{
		TabID:           t.TabID,
		Provider:        t.Provider,
		CliState:        state,
		LastActivityAt:  math.Max(*conversationAt, event),
		BackgroundTasks: len(background.Reconcile(tasks, counts)),
	}
}

// Candidates are the agent tabs that may sleep, looked at a few at a time against one tmux snapshot.
func (s *Sleeper) Candidates() []Candidate {
	var tabs []Tab
	for _, t := range s.Tabs.AgentTabs() {
		if isAgentPanel(t.PanelType) && t.Provider == agents.Claude && sleepable(t.CliState) {
			tabs = append(tabs, t)
		}
	}
	if len(tabs) == 0 {
		return []Candidate{}
	}
	panes, ok := s.Panes.ReadAllPanes()
	if !ok {
		return []Candidate{}
	}
	found := make([]*Candidate, len(tabs))
	next := make(chan int)
	var wg sync.WaitGroup
	for range min(sweepConcurrency, len(tabs)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				p, ok := panes[tabs[i].TmuxSession]
				found[i] = s.candidate(tabs[i], p, ok)
			}
		}()
	}
	for i := range tabs {
		next <- i
	}
	close(next)
	wg.Wait()
	out := []Candidate{}
	for _, c := range found {
		if c != nil {
			out = append(out, *c)
		}
	}
	return out
}

func (s *Sleeper) underPressure() bool {
	ratio := s.MemRatio
	if ratio == nil {
		ratio = proc.MemAvailableRatio
	}
	r, ok := ratio()
	return ok && r < PressureRatio
}

// Kill stops the agent of a tmux session and everything it started, keeping the pane's shell, then marks the tab's
// agent gone. It returns how many processes it stopped; 0 without the session or its pane.
func (s *Sleeper) Kill(tabID, session string) int {
	if !s.Panes.HasSession(session) {
		return 0
	}
	pane := s.Panes.PanePID(session)
	if pane <= 0 {
		return 0
	}
	kill := s.KillTree
	if kill == nil {
		kill = func(root int) int { return proc.KillTree(root, termGrace, killPoll, nil) }
	}
	n := kill(pane)
	if s.MarkGone != nil {
		s.MarkGone(tabID)
	}
	return n
}

// Sweep puts the agents picked now to sleep and returns their tabs, and whether memory was short.
func (s *Sleeper) Sweep() (slept []string, underPressure bool) {
	c := ReadConfig(s.Getenv, s.Env)
	if c.IdleMs == 0 && c.PressureIdleMs == 0 {
		return []string{}, false
	}
	underPressure = s.underPressure()
	slept = []string{}
	for _, tabID := range Pick(s.Candidates(), s.now(), underPressure, c) {
		// The state may have moved while the sweep looked at the others.
		cur, ok := s.Tabs.Current(tabID)
		if !ok || !sleepable(cur.CliState) {
			continue
		}
		if s.Kill(tabID, cur.TmuxSession) > 0 {
			slept = append(slept, tabID)
		}
	}
	return slept, underPressure
}

// Forget drops what is cached of a transcript whose tab is gone.
func (s *Sleeper) Forget(path string) {
	if s.Activity != nil {
		s.Activity.Forget(path)
	}
	if s.Background != nil {
		s.Background.Forget(path)
	}
}

// Run sweeps every period (SweepEvery when 0) until ctx ends; report gets each sweep's result.
func (s *Sleeper) Run(ctx context.Context, period time.Duration, report func(slept []string, underPressure bool)) {
	if period <= 0 {
		period = SweepEvery
	}
	t := time.NewTicker(period)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			slept, pressure := s.Sweep()
			if report != nil {
				report(slept, pressure)
			}
		}
	}
}
