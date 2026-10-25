package convo

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The idle stop and terminal.stop on the rig: a fake process tree under the pane, a fake clock, and a Terminals that
// records what it was asked. Nothing touches tmux or /proc.

type fakeTerms struct {
	mu      sync.Mutex
	owned   bool
	watched bool
	tree    PaneSample
	failed  bool
	stopped []string
	// onStop plays the machine once the session is gone.
	onStop func()
}

func (f *fakeTerms) Owned(string) bool { return f.owned }
func (f *fakeTerms) Sample(string) (PaneSample, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return PaneSample{Root: f.tree.Root, Procs: slices.Clone(f.tree.Procs)}, !f.failed
}
func (f *fakeTerms) Watched(string, string) bool { return f.watched }
func (f *fakeTerms) Stop(session string) (int64, error) {
	f.stopped = append(f.stopped, session)
	if f.onStop != nil {
		f.onStop()
	}
	return 300 << 20, nil
}

// cpu adds processor time to a process of the tree.
func (f *fakeTerms) cpu(pid int, d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.tree.Procs {
		if f.tree.Procs[i].PID == pid {
			f.tree.Procs[i].CPU += d
		}
	}
}

func (f *fakeTerms) add(p Proc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tree.Procs = append(f.tree.Procs, p)
}

func proc(pid, ppid int, argv ...string) Proc {
	return Proc{PID: pid, PPID: ppid, Comm: filepath.Base(argv[0]), Argv: argv, RSS: 100 << 20}
}

// idleTree is a Claude at its prompt in a zsh pane, with two MCP servers (one started through a shell) and the status
// line's helper.
func idleTree() PaneSample {
	return PaneSample{Root: 100, Procs: []Proc{
		proc(100, 1, "-zsh"),
		proc(101, 100, "claude", "--resume", "abc"),
		proc(102, 101, "node", "/home/u/.npm/_npx/x/node_modules/.bin/mcp-server-git"),
		proc(103, 101, "bash", "-c", "exec uvx mcp-server-fetch"),
		proc(104, 103, "python3", "/home/u/.cache/uv/mcp_server_fetch"),
	}}
}

type logLine struct {
	msg string
	kv  []any
}

type fakeLog struct {
	mu    sync.Mutex
	lines []logLine
}

func (l *fakeLog) Info(msg string, kv ...any) {
	l.mu.Lock()
	l.lines = append(l.lines, logLine{msg, kv})
	l.mu.Unlock()
}
func (l *fakeLog) Warn(msg string, kv ...any) { l.Info(msg, kv...) }

type reapRig struct {
	*rig
	terms *fakeTerms
	log   *fakeLog
	after time.Duration
}

func newReapRig(t *testing.T) *reapRig {
	t.Helper()
	r := &reapRig{rig: newRig(t), terms: &fakeTerms{owned: true, tree: idleTree()}, log: &fakeLog{}, after: 2 * time.Hour}
	r.terms.onStop = func() { r.machine.probes["s1"] = Probe{} }
	r.rt = r.reapRuntime()
	r.open()
	r.settle(wire.StateReady)
	return r
}

func (r *reapRig) reapRuntime() *Runtime {
	return NewInline(Deps{MachineID: "m1", Publisher: r.pub, Machine: r.machine, Launcher: r.launcher, Clock: r.clock,
		Store: NewStore(filepath.Join(r.dir, "v2")), Limits: wire.DefaultLimits, Log: r.log, Terminals: r.terms,
		IdleStop: func() time.Duration { return r.after }})
}

// sweeps runs the sweep every ReapEvery for d, the actors looking every few seconds between, and calls each (if any)
// before each sweep. It returns when the terminal was first stopped (zero: never).
func (r *reapRig) sweeps(d time.Duration, each func(i int)) time.Duration {
	start := r.clock.Now()
	for i := 0; r.clock.Now().Sub(start) < d; i++ {
		for range 5 {
			r.tick(ReapEvery / 5)
		}
		if each != nil {
			each(i)
		}
		if got := r.rt.Reap(); len(got) > 0 {
			return r.clock.Now().Sub(start)
		}
	}
	return 0
}

func (r *reapRig) record() wire.Conversation {
	r.t.Helper()
	var rec wire.Conversation
	r.poke(func(c *conv) { rec = c.rec })
	return rec
}

func (r *rig) poke(f func(c *conv)) {
	r.t.Helper()
	if err := r.rt.call(context.Background(), r.id, func(c *conv, _ time.Time) { f(c) }); err != nil {
		r.t.Fatal(err)
	}
}

func TestAnIdleTerminalIsStoppedAfterTheWindow(t *testing.T) {
	r := newReapRig(t)
	took := r.sweeps(4*time.Hour, nil)
	if took < r.after || took > r.after+2*ReapEvery {
		t.Fatalf("stopped after %v, the window is %v", took, r.after)
	}
	if !slices.Equal(r.terms.stopped, []string{"s1"}) {
		t.Fatalf("stopped %v", r.terms.stopped)
	}
	rec := r.record()
	if rec.State.K != wire.StateStopped || !rec.State.Resumable {
		t.Fatalf("state %+v, want stopped and resumable", rec.State)
	}
	ts := rec.TerminalStop
	if ts == nil || ts.By != wire.TerminalStopIdle || ts.IdleMs < r.after.Milliseconds() {
		t.Fatalf("terminalStop %+v", ts)
	}
	var line *logLine
	for i := range r.log.lines {
		if r.log.lines[i].msg == "terminal stopped" {
			line = &r.log.lines[i]
		}
	}
	if line == nil {
		t.Fatalf("no log line of the stop: %+v", r.log.lines)
	}
	got := fmt.Sprint(line.kv...)
	for _, want := range []string{r.id, "idle", "idleFor", "freedMB300"} {
		if !strings.Contains(strings.ReplaceAll(got, " ", ""), strings.ReplaceAll(want, " ", "")) {
			t.Errorf("log %q lacks %q", got, want)
		}
	}
	// The pane is gone, which a stopped chat has: the look keeps it stopped, not gone.
	for range 5 {
		r.tick(10 * time.Second)
	}
	if k := r.state().K; k != wire.StateStopped {
		t.Fatalf("after the looks: %s", k)
	}
	// A second sweep has nothing to stop.
	if got := r.rt.Reap(); len(got) != 0 {
		t.Fatalf("stopped again: %v", got)
	}
}

// Each condition that keeps a terminal: while it holds, three hours pass without a stop.
func TestWhatKeepsATerminal(t *testing.T) {
	cases := []struct {
		name string
		set  func(r *reapRig)
		each func(r *reapRig, i int)
	}{
		{name: "the setting is off", set: func(r *reapRig) { r.after = 0 }},
		{name: "a turn in progress", set: func(r *reapRig) {
			r.hook("prompt-submit")
			r.busy()
		}},
		{name: "background subagents", set: func(r *reapRig) { r.poke(func(c *conv) { c.rec.BgAgents = 2 }) }},
		{name: "a background command", set: func(r *reapRig) {
			r.poke(func(c *conv) {
				c.bg = []wire.BackgroundTask{{ID: "b1", ConversationID: r.id, Kind: wire.BgCommand, Status: wire.BgStatusRunning}}
			})
		}},
		{name: "a scheduled wakeup", set: func(r *reapRig) {
			r.poke(func(c *conv) { c.bgWake = ms(r.clock.Now().Add(24 * time.Hour)) })
		}},
		{name: "an open gate", set: func(r *reapRig) {
			r.poke(func(c *conv) { c.rec.Gate = &wire.GateRef{Kind: wire.GateQuestion, ID: "q:toolu_1"} })
		}},
		{name: "a queued message", each: func(r *reapRig, _ int) {
			r.poke(func(c *conv) { c.rec.Queue = wire.QueueSummary{Count: 1} })
		}},
		{name: "a model change waiting", each: func(r *reapRig, _ int) {
			r.poke(func(c *conv) { c.rec.PendingModel = &wire.PendingModel{ID: "opus"} })
		}},
		{name: "someone has the chat open", set: func(r *reapRig) { r.terms.watched = true }},
		{name: "a session the runtime does not own", set: func(r *reapRig) { r.terms.owned = false }},
		{name: "a tree that cannot be read", set: func(r *reapRig) { r.terms.failed = true }},
		{name: "a command the agent runs in the background", set: func(r *reapRig) {
			r.terms.add(proc(110, 101, "/bin/zsh", "-c", "-l", "source /home/u/.claude/shell-snapshots/snapshot-zsh-1.sh && eval 'npm run dev'"))
			r.terms.add(proc(111, 110, "node", "/w/node_modules/.bin/vite"))
		}},
		{name: "a dev server the person started in the pane", set: func(r *reapRig) {
			r.terms.add(proc(120, 100, "node", "/w/node_modules/.bin/next", "dev"))
		}},
		{name: "an ssh session in the pane", set: func(r *reapRig) {
			r.terms.add(proc(121, 100, "ssh", "build-host"))
		}},
		{name: "a helper at work", each: func(r *reapRig, _ int) { r.terms.cpu(104, 30*time.Second) }},
		{name: "the agent at work", each: func(r *reapRig, _ int) { r.terms.cpu(101, time.Minute) }},
		{name: "output in the pane", each: func(r *reapRig, i int) {
			if i%2 == 0 {
				r.pane.screen = withInput(t, "a draft")
			} else {
				r.idle()
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newReapRig(t)
			if tc.set != nil {
				tc.set(r)
			}
			var each func(int)
			if tc.each != nil {
				each = func(i int) { tc.each(r, i) }
			}
			if took := r.sweeps(3*time.Hour, each); took != 0 {
				t.Fatalf("stopped after %v", took)
			}
			if len(r.terms.stopped) != 0 {
				t.Fatalf("stopped %v", r.terms.stopped)
			}
		})
	}
}

// What an idle agent has around it is no work: its MCP servers (one through a shell), an idle CPU, a clock in the
// footer.
func TestWhatIsNoWork(t *testing.T) {
	r := newReapRig(t)
	took := r.sweeps(3*time.Hour, func(i int) {
		r.terms.cpu(101, 200*time.Millisecond)
		r.terms.cpu(102, 100*time.Millisecond)
		r.pane.screen = tui(t, "claude-idle-placeholder.ansi.txt") + fmt.Sprintf("\n  %02d:%02d", i/12, i*5%60)
	})
	if took == 0 {
		t.Fatal("an idle terminal was kept")
	}
}

// One sign of work in the middle of the window starts it again.
func TestWorkInTheWindowStartsItAgain(t *testing.T) {
	r := newReapRig(t)
	took := r.sweeps(5*time.Hour, func(i int) {
		if i == 12 { // an hour in: a build ran for a while
			r.terms.cpu(102, 2*time.Minute)
		}
	})
	if took < r.after+time.Hour {
		t.Fatalf("stopped after %v, an hour of work should have moved the window", took)
	}
	r2 := newReapRig(t)
	took = r2.sweeps(5*time.Hour, func(i int) {
		if i == 12 { // an hour in: the person sent a message and the turn finished before the next sweep
			r2.poke(func(c *conv) { c.rec.LastPromptAt = ms(r2.clock.Now()) })
		}
	})
	if took < r2.after+time.Hour {
		t.Fatalf("stopped after %v, the prompt should have moved the window", took)
	}
}

func TestTerminalStopCommand(t *testing.T) {
	r := newReapRig(t)
	stop := func(op string, force bool) wire.CmdResult {
		var f *bool
		if force {
			f = &force
		}
		return r.do(wire.TerminalStopCmd{OpID: op, ID: r.id, Force: f})
	}
	r.terms.owned = false
	if res := stop("s0", false); res.R != wire.ResultRefused || res.Code != wire.RefuseUnsupported {
		t.Fatalf("a session the runtime does not own: %+v", res)
	}
	r.terms.owned = true
	r.hook("prompt-submit")
	if res := stop("s1", false); res.R != wire.ResultRefused || res.Code != wire.RefuseAgentWorking {
		t.Fatalf("a working agent: %+v", res)
	}
	res := stop("s2", true)
	if res.R != wire.ResultAccepted || res.Ch != wire.ChannelConvs || res.Seq == 0 {
		t.Fatalf("forced: %+v", res)
	}
	rec := r.record()
	if rec.State.K != wire.StateStopped || !rec.State.Resumable || rec.TerminalStop == nil || rec.TerminalStop.By != wire.TerminalStopPerson || rec.TerminalStop.IdleMs != 0 {
		t.Fatalf("after the stop: %+v %+v", rec.State, rec.TerminalStop)
	}
	if !slices.Equal(r.terms.stopped, []string{"s1"}) {
		t.Fatalf("stopped %v", r.terms.stopped)
	}
	up := false
	for _, p := range r.pub.all {
		if u, ok := p.e.(wire.ConversationUpsertEv); ok && p.seq == res.Seq && u.Conv.TerminalStop != nil {
			up = true
		}
	}
	if !up {
		t.Fatal("the answer's seq is not the upsert that says the terminal stopped")
	}
	if res := stop("s3", false); res.R != wire.ResultRefused || res.Code != wire.RefuseAgentGone {
		t.Fatalf("a stopped terminal: %+v", res)
	}
	if again := stop("s2", true); again != res && again.R != wire.ResultAccepted {
		t.Fatalf("the same opId again: %+v", again)
	}
	for range 5 {
		r.tick(10 * time.Second)
	}
	if k := r.state().K; k != wire.StateStopped {
		t.Fatalf("the look made it %s", k)
	}

	// The next message resumes the agent on its session; once it is up, the stop is history.
	r.send("m1", "go on", wire.ModeQueue)
	if !slices.Equal(r.launcher.resumed, []string{r.id}) {
		t.Fatalf("resumed %v", r.launcher.resumed)
	}
	if k := r.state().K; k != wire.StateStarting {
		t.Fatalf("after the message: %s", k)
	}
	r.machine.probes["s1"] = Probe{Session: true, Foreground: true}
	r.idle()
	r.agentTakes()
	r.settle(wire.StateWorking)
	if ts := r.record().TerminalStop; ts != nil {
		t.Fatalf("the agent is up and the record still says %+v", ts)
	}
}

func TestAStoppedTerminalStaysStoppedAfterARestart(t *testing.T) {
	r := newReapRig(t)
	if res := r.do(wire.TerminalStopCmd{OpID: "s1", ID: r.id}); res.R != wire.ResultAccepted {
		t.Fatalf("%+v", res)
	}
	r.rt.Stop()
	r.rt = r.reapRuntime()
	if err := r.rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		r.tick(10 * time.Second)
	}
	rec := r.record()
	if rec.State.K != wire.StateStopped || !rec.State.Resumable || rec.TerminalStop == nil {
		t.Fatalf("after the restart: %+v %+v", rec.State, rec.TerminalStop)
	}
}

func TestTreeActivity(t *testing.T) {
	codex := PaneSample{Root: 1, Procs: []Proc{
		proc(1, 0, "bash"),
		proc(2, 1, "node", "/usr/lib/node_modules/@openai/codex/bin/codex.js"),
		proc(3, 2, "/usr/lib/node_modules/@openai/codex/vendor/codex"),
		proc(4, 3, "node", "/opt/mcp/server.js"),
	}}
	cases := []struct {
		name     string
		s        PaneSample
		provider wire.Provider
		busy     bool
		agent    int
	}{
		{"an idle Claude and its MCP servers", idleTree(), wire.ProviderClaude, false, 101},
		{"an idle Codex behind its node wrapper", codex, wire.ProviderCodex, false, 2},
		{"Codex runs a command", PaneSample{Root: 1, Procs: append(slices.Clone(codex.Procs), proc(5, 3, "bash", "-lc", "go test ./..."))},
			wire.ProviderCodex, true, 2},
		{"Claude runs a Bash call", PaneSample{Root: 100, Procs: append(idleTree().Procs, proc(9, 101, "zsh", "-c", "-l", "source ~/.claude/shell-snapshots/s.sh && eval 'make'"), proc(10, 9, "make"))},
			wire.ProviderClaude, true, 101},
		{"no agent in the pane", PaneSample{Root: 1, Procs: []Proc{proc(1, 0, "zsh"), proc(2, 1, "vim", "x")}}, wire.ProviderClaude, true, 0},
		{"a shell of the person waits beside the agent", PaneSample{Root: 100, Procs: append(idleTree().Procs, proc(30, 100, "bash"))},
			wire.ProviderClaude, false, 101},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := treeActivity(tc.s, tc.provider)
			if (got.busy != "") != tc.busy || got.agent != tc.agent {
				t.Fatalf("busy %q agent %d, want busy %v agent %d", got.busy, got.agent, tc.busy, tc.agent)
			}
		})
	}
}

func TestStopFailureKeepsTheChatRunning(t *testing.T) {
	r := newReapRig(t)
	r.terms.onStop = nil
	failing := &failingTerms{fakeTerms: r.terms}
	r.rt = NewInline(Deps{MachineID: "m1", Publisher: r.pub, Machine: r.machine, Launcher: r.launcher, Clock: r.clock,
		Limits: wire.DefaultLimits, Log: r.log, Terminals: failing})
	r.open()
	r.settle(wire.StateReady)
	if res := r.do(wire.TerminalStopCmd{OpID: "s1", ID: r.id}); res.R != wire.ResultRefused || res.Code != wire.RefuseInternal {
		t.Fatalf("%+v", res)
	}
	if rec := r.record(); rec.State.K == wire.StateStopped || rec.TerminalStop != nil {
		t.Fatalf("%+v %+v", rec.State, rec.TerminalStop)
	}
}

type failingTerms struct{ *fakeTerms }

func (failingTerms) Stop(string) (int64, error) { return 0, errors.New("tmux did not answer") }
