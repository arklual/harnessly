package convo

import (
	"context"
	"errors"
	"hash/fnv"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentstate"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A chat's terminal is its tmux session: the pane, the agent in it and everything the agent started. An idle Claude
// holds hundreds of megabytes with its MCP servers whatever it does, so a terminal nobody uses is stopped — by the
// person (terminal.stop) or by the runtime once it has stood idle long enough (the sweep below). The chat stays, its
// history too; the next message resumes the agent on its session the way a stopped agent always resumes.
//
// The sweep runs every ReapEvery and is cheap: the actor answers from what it holds (state, queue, gates, background
// work, its last screen), and only a chat that passes all of that costs a tmux call and a read of /proc. A chat is idle
// when every one of these holds at every sweep of the window:
//
//   - the agent is at its prompt (idle or ready), no turn, no gate, nothing queued, typed or waiting to be applied;
//   - no background work: no subagent, command, monitor or teammate the agent waits for, no scheduled wakeup;
//   - nobody has the chat or its terminal open (a page subscribed to it, a terminal attached to the pane);
//   - the pane's process tree holds the agent, its helpers and shells only — no command the agent runs, nothing the
//     person started in the pane (treeActivity);
//   - the tree used next to no processor time since the last sweep (helpers under 2 % of a core, the agent under 5 %);
//   - the agent's screen did not change (digits aside: a clock or a counter in the footer is no work);
//   - the record shows no activity (a prompt, a turn's end, the state's change, a read) after the window began.
//
// Any of them failing starts the window again from the next sweep. Never stopped: a session the runtime does not own
// (Terminals.Owned) and a chat whose agent has no session to resume on.

const (
	// ReapEvery is how often the sweep looks at the chats.
	ReapEvery = 5 * time.Minute
	// helperCPUShare and agentCPUShare: the share of one core the agent's helpers and the agent itself may use between
	// two sweeps and still count as idle. An MCP server waiting for a call, a Claude at its prompt redrawing nothing,
	// use far less; a build, a test, a browser at work or a turn use far more.
	helperCPUShare = 0.02
	agentCPUShare  = 0.05
	// reapAsk is how long the sweep waits for one actor's view.
	reapAsk = 2 * time.Second
)

var errNotOwned = errors.New("convo: the session is not a chat terminal of this runtime")

// --- the stop ---

// stoppable is why the conversation's terminal cannot be stopped now, "" when it can.
func (c *conv) stoppable(force bool) wire.RefuseCode {
	switch {
	case c.d.Terminals == nil:
		return wire.RefuseUnsupported
	case c.stoppingTerm || c.rec.Session == "" || c.rec.TerminalStop != nil:
		return wire.RefuseAgentGone
	}
	switch c.rec.State.K {
	case wire.StateStopped, wire.StateGone, wire.StateLaunchFailed, wire.StateStarting:
		return wire.RefuseAgentGone
	case wire.StateWorking, wire.StateWaitingQuestion, wire.StateWaitingPermission:
		if !force {
			return wire.RefuseAgentWorking
		}
	}
	if c.rec.AgentSessionID == "" && c.rec.TranscriptPath == "" {
		// Nothing to resume on: stopped, the chat could never go on.
		return wire.RefuseUnsupported
	}
	return ""
}

// stopTerminal ends the terminal off the actor, under the pane's lock (nothing is typed into it meanwhile); finish gets
// the answer once it is over: accepted on convs from the record's upsert, or the refusal.
func (c *conv) stopTerminal(by wire.TerminalStopBy, idle time.Duration, force bool, finish func(wire.CmdResult)) {
	if code := c.stoppable(force); code != "" {
		finish(wire.Refused(code))
		return
	}
	c.stoppingTerm = true
	session, terms := c.rec.Session, c.d.Terminals
	type stopped struct {
		freed int64
		err   error
	}
	c.spawn(func() any {
		if !terms.Owned(session) {
			return stopped{err: errNotOwned}
		}
		unlock := typist.Lock(session)
		defer unlock()
		freed, err := terms.Stop(session)
		return stopped{freed, err}
	}, func(r any) {
		c.stoppingTerm = false
		res := r.(stopped)
		switch {
		case errors.Is(res.err, errNotOwned):
			finish(wire.Refused(wire.RefuseUnsupported))
		case res.err != nil:
			c.d.Log.Warn("terminal not stopped", "conv", c.rec.ID, "err", res.err)
			finish(wire.Refused(wire.RefuseInternal))
		default:
			now := c.d.Clock.Now()
			c.mark, c.markConvs = 0, 0
			c.terminalStopped(by, idle, res.freed, now)
			finish(wire.Accepted(wire.ChannelConvs, c.convsSeq()))
		}
	})
}

// terminalStopped: the session is gone. The agent is stopped and resumable — the next message brings it back — and
// the record says who stopped it; a look begun before the stop saw the pane still there and is dropped.
func (c *conv) terminalStopped(by wire.TerminalStopBy, idle time.Duration, freed int64, now time.Time) {
	c.lookSeq++
	c.looking, c.lookAgain = false, false
	c.input(agentstate.Input{K: agentstate.Shell, Resumable: true}, now)
	ts := &wire.TerminalStop{At: ms(now), By: by}
	if by == wire.TerminalStopIdle {
		ts.IdleMs = max(idle.Milliseconds(), 1)
	}
	c.rec.TerminalStop = ts
	c.touch()
	c.flush(now, true)
	c.save(now, true)
	kv := []any{"conv", c.rec.ID, "by", string(by), "session", c.rec.Session, "freedMB", freed >> 20}
	if by == wire.TerminalStopIdle {
		kv = append(kv, "idleFor", idle.Round(time.Minute).String(),
			"why", "at its prompt, no background work, no process at work in the pane, screen unchanged, nobody watching")
	}
	c.d.Log.Info("terminal stopped", kv...)
}

// --- the actor's side of the sweep ---

// reapView is what the actor knows of whether its chat is idle: why not (reason, "" when it is), its session and
// agent, its last screen, and the newest activity its record shows.
type reapView struct {
	reason   string
	session  string
	provider wire.Provider
	screen   string
	since    time.Time
}

func (c *conv) reapView(now time.Time) reapView {
	v := reapView{session: c.rec.Session, provider: c.rec.Provider}
	if c.look != nil {
		v.screen = c.look.Plain
	}
	for _, at := range []wire.Ms{c.rec.State.Since, c.rec.LastActivityAt, c.rec.LastPromptAt, c.rec.SeenAt} {
		if t := time.UnixMilli(at); t.After(v.since) {
			v.since = t
		}
	}
	if t := c.rec.LastTurn; t != nil && t.EndedAt != nil && time.UnixMilli(*t.EndedAt).After(v.since) {
		v.since = time.UnixMilli(*t.EndedAt)
	}
	v.reason = c.notIdle(now)
	return v
}

// notIdle is why the chat is not idle in what the actor holds, "" when nothing it holds says so.
func (c *conv) notIdle(now time.Time) string {
	switch {
	case c.deletedAt != 0:
		return "deleted"
	case c.rec.Session == "" || c.rec.TerminalStop != nil || c.stoppingTerm:
		return "no terminal"
	case c.rec.State.K != wire.StateIdle && c.rec.State.K != wire.StateReady:
		return "agent " + string(c.rec.State.K)
	case c.rec.Gate != nil:
		return "gate"
	case c.rec.Queue.Count > 0 || c.rec.Queue.Paused != nil || c.typing || c.switching || c.busyGate || c.interrupting:
		return "queue"
	case c.rec.PendingModel != nil:
		return "model change"
	case c.rec.BgAgents > 0 || c.bgWake > ms(now):
		return "background"
	}
	for _, t := range c.bg {
		if t.Status == wire.BgStatusRunning {
			return "background"
		}
	}
	for _, g := range c.gates {
		if k := g.state().K; k == wire.GateStateOpen || k == wire.GateStateAnswering {
			return "gate"
		}
	}
	if c.rec.AgentSessionID == "" && c.rec.TranscriptPath == "" {
		return "nothing to resume"
	}
	return ""
}

// --- the pane's processes ---

var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "fish": true, "ksh": true, "tcsh": true, "csh": true}

func procName(p Proc) string {
	if len(p.Argv) > 0 {
		if b := filepath.Base(p.Argv[0]); b != "" && b != "." {
			return strings.TrimPrefix(b, "-")
		}
	}
	return p.Comm
}

func isShell(p Proc) bool { return shells[procName(p)] || shells[p.Comm] }

// commandShell: a shell running a command (-c, -lc, -ic …) rather than waiting at its own prompt.
func commandShell(p Proc) bool {
	if !isShell(p) {
		return false
	}
	for _, a := range p.Argv[min(1, len(p.Argv)):] {
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.ContainsRune(a, 'c') {
			return true
		}
	}
	return false
}

// isAgent: the process is the agent CLI of the provider — its own binary, or the node that runs its script.
func isAgent(p Proc, provider wire.Provider) bool {
	name := "claude"
	if provider == wire.ProviderCodex {
		name = "codex"
	}
	if p.Comm == name || procName(p) == name {
		return true
	}
	if len(p.Argv) > 1 && (procName(p) == "node" || p.Comm == "node") {
		return strings.Contains(filepath.Base(p.Argv[1]), name) || strings.Contains(p.Argv[1], "/"+name)
	}
	return false
}

// toolShell: a shell the agent runs a command of its own in (a Bash call, run_in_background included). Claude runs
// them as `<shell> -c -l source …/shell-snapshots/… && eval '…'`; Codex as `bash -lc '…'`. A shell that merely
// starts an MCP server is no such shell: it is a helper like the server.
func toolShell(p Proc, provider wire.Provider) bool {
	if !commandShell(p) {
		return false
	}
	if provider == wire.ProviderCodex {
		return true
	}
	line := strings.Join(p.Argv, " ")
	return strings.Contains(line, "shell-snapshots") || strings.Contains(line, "eval ")
}

// treeLook is a sample read: the agent's pid (0 when none runs), why the tree is at work ("" when it is not), and the
// processor time of the agent and of the rest by pid, for the next sample to compare.
type treeLook struct {
	agent  int
	busy   string
	cpu    map[int]time.Duration
	rss    int64
	agentP Proc
}

// treeActivity reads a pane's tree: the agent is the topmost process of its CLI; its ancestors (the pane's shell, a
// wrapper) and its helpers (what it started, an MCP server, its children) are expected; a tool shell under it, or
// anything but a shell outside it, is work going on.
func treeActivity(s PaneSample, provider wire.Provider) treeLook {
	look := treeLook{cpu: map[int]time.Duration{}}
	byPID := map[int]Proc{}
	kids := map[int][]int{}
	for _, p := range s.Procs {
		byPID[p.PID] = p
		look.cpu[p.PID] = p.CPU
		look.rss += p.RSS
		if p.PID != s.Root {
			kids[p.PPID] = append(kids[p.PPID], p.PID)
		}
	}
	// Breadth first from the root: the agent found first is the topmost.
	order := []int{s.Root}
	for i := 0; i < len(order); i++ {
		ks := kids[order[i]]
		slices.Sort(ks)
		order = append(order, ks...)
	}
	for _, pid := range order {
		if p, ok := byPID[pid]; ok && isAgent(p, provider) {
			look.agent, look.agentP = pid, p
			break
		}
	}
	if look.agent == 0 {
		look.busy = "no agent in the pane"
		return look
	}
	under := map[int]bool{}
	var walk func(pid int, inTool bool)
	walk = func(pid int, inTool bool) {
		for _, k := range kids[pid] {
			p := byPID[k]
			tool := inTool || toolShell(p, provider)
			under[k] = true
			if tool && look.busy == "" {
				look.busy = "the agent runs: " + strings.Join(p.Argv, " ")
			}
			walk(k, tool)
		}
	}
	walk(look.agent, false)
	ancestors := map[int]bool{}
	for pid := byPID[look.agent].PPID; pid != 0 && !ancestors[pid]; pid = byPID[pid].PPID {
		ancestors[pid] = true
		if pid == s.Root {
			break
		}
	}
	for _, pid := range order {
		p, ok := byPID[pid]
		if !ok || pid == look.agent || under[pid] || ancestors[pid] || pid == s.Root || isShell(p) {
			continue
		}
		if look.busy == "" {
			look.busy = "the pane runs: " + strings.Join(p.Argv, " ")
		}
	}
	return look
}

// --- the sweep ---

// idleTrack is what the sweep remembers of a chat it saw idle: since when, and the sample to compare the next with.
type idleTrack struct {
	since  time.Time
	at     time.Time
	agent  int
	cpu    map[int]time.Duration
	screen uint64
}

func screenKey(s string) uint64 {
	h := fnv.New64a()
	for _, line := range strings.Split(s, "\n") {
		line = strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return '#'
			}
			return r
		}, strings.TrimRight(line, "  "))
		h.Write([]byte(line))
		h.Write([]byte{'\n'})
	}
	return h.Sum64()
}

// quietBetween: the tree used next to no processor time from the last sample to this one.
func quietBetween(prev *idleTrack, look treeLook, elapsed time.Duration) bool {
	if elapsed <= 0 {
		return true
	}
	var agent, rest time.Duration
	for pid, cpu := range look.cpu {
		before, ok := prev.cpu[pid]
		if !ok || cpu < before {
			continue // new since the last sample: its time is counted from the next one
		}
		if pid == look.agent {
			agent += cpu - before
		} else {
			rest += cpu - before
		}
	}
	return float64(agent) <= agentCPUShare*float64(elapsed) && float64(rest) <= helperCPUShare*float64(elapsed)
}

func (r *Runtime) idleAfter() time.Duration {
	if r.d.IdleStop == nil {
		return 0
	}
	return r.d.IdleStop()
}

// Reap is one sweep: it stops the terminals of the chats that stood idle for the whole window and returns their ids
// (the stops finish off the sweep's goroutine; the inline runtime of a test has finished them on return).
func (r *Runtime) Reap() []string {
	r.reapMu.Lock()
	defer r.reapMu.Unlock()
	after := r.idleAfter()
	if after <= 0 || r.d.Terminals == nil {
		r.idle = nil
		return nil
	}
	if r.idle == nil {
		r.idle = map[string]*idleTrack{}
	}
	var stopped []string
	seen := map[string]bool{}
	for _, a := range r.all() {
		last := a.last.Load()
		if last == nil || last.deleted {
			continue
		}
		id := last.rec.ID
		seen[id] = true
		if r.reapOne(id, after) {
			stopped = append(stopped, id)
		}
	}
	for id := range r.idle {
		if !seen[id] {
			delete(r.idle, id)
		}
	}
	return stopped
}

// reapOne samples one chat and stops its terminal when it has stood idle for after.
func (r *Runtime) reapOne(id string, after time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), reapAsk)
	var v reapView
	err := r.call(ctx, id, func(c *conv, now time.Time) { v = c.reapView(now) })
	cancel()
	terms := r.d.Terminals
	if err != nil || v.reason != "" || !terms.Owned(v.session) || terms.Watched(id, v.session) {
		delete(r.idle, id)
		return false
	}
	s, ok := terms.Sample(v.session)
	if !ok {
		delete(r.idle, id)
		return false
	}
	look := treeActivity(s, v.provider)
	if look.busy != "" {
		delete(r.idle, id)
		return false
	}
	now := r.d.Clock.Now()
	key := screenKey(v.screen)
	t := r.idle[id]
	if t == nil {
		t = &idleTrack{since: now}
		r.idle[id] = t
	} else if t.agent != look.agent || t.screen != key || !quietBetween(t, look, now.Sub(t.at)) {
		t.since = now
	}
	if v.since.After(t.since) {
		t.since = v.since
	}
	t.at, t.agent, t.cpu, t.screen = now, look.agent, look.cpu, key
	idle := now.Sub(t.since)
	if idle < after {
		return false
	}
	delete(r.idle, id)
	started := false
	_ = r.call(context.Background(), id, func(c *conv, now time.Time) {
		// The state may have moved since the view: the actor asks again before it stops anything.
		if c.notIdle(now) != "" {
			return
		}
		started = true
		c.stopTerminal(wire.TerminalStopIdle, idle, false, func(res wire.CmdResult) {
			if res.R != wire.ResultAccepted {
				c.d.Log.Warn("idle terminal not stopped", "conv", id, "code", string(res.Code))
			}
		})
	})
	return started
}

// reaper sweeps every ReapEvery until the runtime stops.
func (r *Runtime) reaper() {
	defer r.wg.Done()
	for {
		select {
		case <-r.stop:
			return
		case <-r.d.Clock.After(ReapEvery):
			r.Reap()
		}
	}
}
