package prompts

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/tmux"
)

func tui(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "tui", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func transcript(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "transcripts", name))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(string(raw), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// fakeClock is a clock that moves only when told: Sleep advances it, Advance also fires the timers that fall due.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
	slept  []time.Duration
}

type fakeTimer struct {
	c       *fakeClock
	at      time.Time
	f       func()
	stopped bool
}

func (t *fakeTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	was := !t.stopped
	t.stopped = true
	return was
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.slept = append(c.slept, d)
	c.mu.Unlock()
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{c: c, at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return t
}

// pending are the live timers, earliest first.
func (c *fakeClock) pending() []*fakeTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	var live []*fakeTimer
	for _, t := range c.timers {
		if !t.stopped {
			live = append(live, t)
		}
	}
	sort.SliceStable(live, func(i, j int) bool { return live[i].at.Before(live[j].at) })
	return live
}

// Advance moves the clock by d, firing each timer that falls due at its own time.
func (c *fakeClock) Advance(d time.Duration) {
	target := c.Now().Add(d)
	for {
		var next *fakeTimer
		for _, t := range c.pending() {
			if !t.at.After(target) {
				next = t
			}
			break
		}
		if next == nil {
			break
		}
		c.mu.Lock()
		next.stopped = true
		if next.at.After(c.now) {
			c.now = next.at
		}
		c.mu.Unlock()
		next.f()
	}
	c.mu.Lock()
	if target.After(c.now) {
		c.now = target
	}
	c.mu.Unlock()
}

// fakePanes is one tmux session whose screen the test sets; every key and text sent is recorded.
type fakePanes struct {
	mu       sync.Mutex
	session  string
	exists   bool
	styled   func() (string, bool)
	plain    func() (string, bool)
	current  string
	detail   tmux.PaneDetail
	paneID   string
	panePID  int
	socket   string
	failOn   string
	calls    []string
	onSend   func(call string)
	atWidths int
}

func newFakePanes(session string, screen string) *fakePanes {
	p := &fakePanes{session: session, exists: true, current: "claude", paneID: "%7", socket: "hs-test", detail: tmux.PaneDetail{Width: 120, Height: 40}}
	p.setScreen(screen)
	return p
}

func (p *fakePanes) setScreen(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.styled = func() (string, bool) { return s, true }
	p.plain = func() (string, bool) { return stripForTest(s), true }
}

func stripForTest(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !(s[j] >= '@' && s[j] <= '~') {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func (p *fakePanes) record(call string) error {
	p.mu.Lock()
	failOn, onSend := p.failOn, p.onSend
	if failOn != "" && strings.HasPrefix(call, failOn) {
		p.mu.Unlock()
		return os.ErrClosed
	}
	p.calls = append(p.calls, call)
	p.mu.Unlock()
	if onSend != nil {
		onSend(call)
	}
	return nil
}

func (p *fakePanes) sent() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string{}, p.calls...)
}

func (p *fakePanes) HasSession(name string) bool   { return p.exists && name == p.session }
func (p *fakePanes) IsAppSession(name string) bool { return strings.HasPrefix(name, "hs-") }
func (p *fakePanes) SocketOf(string) string        { return p.socket }
func (p *fakePanes) Capture(string) (string, bool) {
	p.mu.Lock()
	f := p.plain
	p.mu.Unlock()
	return f()
}
func (p *fakePanes) CaptureStyled(string) (string, bool) {
	p.mu.Lock()
	f := p.styled
	p.mu.Unlock()
	return f()
}
func (p *fakePanes) CaptureAtWidth(name string, cols, rows int) (string, bool) {
	p.mu.Lock()
	p.atWidths++
	p.mu.Unlock()
	return p.Capture(name)
}
func (p *fakePanes) SendRawKeys(_, keys string) error { return p.record("keys:" + keys) }
func (p *fakePanes) SendKeySequence(_ string, keys ...string) error {
	return p.record("seq:" + strings.Join(keys, " "))
}
func (p *fakePanes) SendLiteral(_, text string) error            { return p.record("literal:" + text) }
func (p *fakePanes) SendLiteralThenEnter(_, text string) error   { return p.record("line:" + text) }
func (p *fakePanes) SendBracketedPasteOnly(_, text string) error { return p.record("paste:" + text) }
func (p *fakePanes) ExitCopyMode(string)                         {}
func (p *fakePanes) PaneID(string) string                        { return p.paneID }
func (p *fakePanes) PanePID(string) int                          { return p.panePID }
func (p *fakePanes) PaneCurrentCommand(string) string            { return p.current }
func (p *fakePanes) PaneDetail(string) tmux.PaneDetail           { return p.detail }

// fakeTabs holds tabs by id.
type fakeTabs struct {
	mu   sync.Mutex
	tabs map[string]Tab
}

func newFakeTabs(tabs ...Tab) *fakeTabs {
	f := &fakeTabs{tabs: map[string]Tab{}}
	for _, t := range tabs {
		f.tabs[t.ID] = t
	}
	return f
}

func (f *fakeTabs) Tab(id string) (Tab, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tabs[id]
	return t, ok
}

func (f *fakeTabs) TabOfSession(session string) (Tab, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tabs {
		if t.Session == session {
			return t, true
		}
	}
	return Tab{}, false
}

func (f *fakeTabs) set(id string, change func(t *Tab)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.tabs[id]
	change(&t)
	f.tabs[id] = t
}

// fakeAgents answers for the agents and records what was asked.
type fakeAgents struct {
	mu        sync.Mutex
	running   bool
	recovers  bool
	resume    string
	resumed   int
	gaveUp    []string
	recovered int
}

func (a *fakeAgents) Running(Tab) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running
}
func (a *fakeAgents) RecoverSession(Tab) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.recovered++
	return a.recovers
}
func (a *fakeAgents) Resume(Tab) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.resumed++
	return a.resume
}
func (a *fakeAgents) GiveUpLaunch(tabID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.gaveUp = append(a.gaveUp, tabID)
}

// fakeEvents records what the package reported.
type fakeEvents struct {
	mu          sync.Mutex
	log         []string
	menuMoves   bool
	stuckChecks chan string
}

func (e *fakeEvents) add(s string) {
	e.mu.Lock()
	e.log = append(e.log, s)
	e.mu.Unlock()
}
func (e *fakeEvents) Interrupted(tabID, session string) {
	e.add("interrupted:" + tabID + ":" + session)
}
func (e *fakeEvents) MenuOnScreen(tabID string) bool {
	e.add("menu:" + tabID)
	return e.menuMoves
}
func (e *fakeEvents) RecheckStuck(tabID string) {
	if e.stuckChecks != nil {
		e.stuckChecks <- tabID
	}
}
func (e *fakeEvents) LaunchChosen(session string, a Applied) {
	s := "launch:" + session
	if a.Model != nil {
		s += ":model=" + *a.Model
	}
	if a.Effort != nil {
		s += ":effort=" + *a.Effort
	}
	e.add(s)
}
func (e *fakeEvents) CommandsReloaded(provider string) { e.add("reloaded:" + provider) }
func (e *fakeEvents) events() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string{}, e.log...)
}

// fakeBroadcast records the messages sent to pages.
type fakeBroadcast struct {
	mu   sync.Mutex
	msgs []any
}

func (b *fakeBroadcast) Broadcast(m any) {
	b.mu.Lock()
	b.msgs = append(b.msgs, m)
	b.mu.Unlock()
}

// rig is a Service over fakes, with one Claude tab "tab-1" in session "hs-a".
type rig struct {
	svc    *Service
	clock  *fakeClock
	panes  *fakePanes
	tabs   *fakeTabs
	agents *fakeAgents
	events *fakeEvents
	bc     *fakeBroadcast
}

const testSession = "hs-a"

func newRig(t *testing.T, screen string, tab Tab) *rig {
	t.Helper()
	if tab.ID == "" {
		tab.ID = "tab-1"
	}
	if tab.Session == "" {
		tab.Session = testSession
	}
	if tab.Provider == "" {
		tab.Provider = ProviderClaude
	}
	if tab.CliState == "" {
		tab.CliState = StateIdle
	}
	r := &rig{clock: newFakeClock(), panes: newFakePanes(tab.Session, screen), tabs: newFakeTabs(tab),
		agents: &fakeAgents{running: true, resume: ResumeResumed}, events: &fakeEvents{}, bc: &fakeBroadcast{}}
	r.svc = New(Config{
		Panes: r.panes, Tabs: r.tabs, Agents: r.agents, Events: r.events, Broadcast: r.bc, Clock: r.clock,
		IsUploadImage: func(p string) bool { return strings.HasPrefix(p, "/uploads/") },
		Procs:         Procs{Children: func(int) []int { return nil }, Args: func(int) (string, bool) { return "", false }, Environ: func(int) ([]string, bool) { return nil, false }},
	})
	return r
}
