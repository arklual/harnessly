package convo

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func (r *rig) phase() Phase {
	r.t.Helper()
	p, ok := r.rt.Phase(r.id)
	if !ok {
		r.t.Fatalf("no conversation %s", r.id)
	}
	return p
}

// sessionBack plays the machine after a resume: the session is there again with the agent at its prompt.
func (r *rig) sessionBack() {
	r.machine.panes["s1"] = r.pane
	r.machine.probes["s1"] = Probe{Session: true, Foreground: true}
	r.idle()
}

// sessionGone plays a machine that lost the chat's tmux session (a reboot).
func (r *rig) sessionGone() {
	r.machine.probes["s1"] = Probe{}
	delete(r.machine.panes, "s1")
}

// A chat's terminal goes round its whole life with the conversations as its only owner: stopped it is cold, the next
// message resumes it (warm) and is typed (hot), the turn's end leaves it warm, and the idle window makes it cold.
func TestSessionLifeGoesColdWarmHotWarmCold(t *testing.T) {
	r := newReapRig(t)
	if res := r.do(wire.TerminalStopCmd{OpID: "s1", ID: r.id}); res.R != wire.ResultAccepted {
		t.Fatalf("%+v", res)
	}
	r.sessionGone()
	if p := r.phase(); p != PhaseCold {
		t.Fatalf("after the stop: %s", p)
	}
	if spare, owned := r.rt.SessionSpare("s1"); !owned || !spare {
		t.Fatalf("a cold chat's session: spare %v owned %v", spare, owned)
	}

	// cold → warm: the message resumes the agent.
	r.agentTakes()
	r.send("m1", "go on", wire.ModeQueue)
	r.tick(time.Second)
	if !slices.Equal(r.launcher.resumed, []string{r.id}) {
		t.Fatalf("resumed %v", r.launcher.resumed)
	}
	if p := r.phase(); p != PhaseWarm {
		t.Fatalf("after the resume: %s (%s)", p, r.state().K)
	}
	if spare, owned := r.rt.SessionSpare("s1"); !owned || spare {
		t.Fatalf("a chat on its way up: spare %v owned %v", spare, owned)
	}

	// warm → hot: the agent is back and takes the message.
	r.sessionBack()
	r.settle(wire.StateWorking)
	if p := r.phase(); p != PhaseHot {
		t.Fatalf("in the turn: %s", p)
	}
	if got := r.rt.Reap(); len(got) != 0 {
		t.Fatalf("a hot chat was stopped: %v", got)
	}

	// hot → warm: the turn ends.
	r.say("done")
	r.turnEnd()
	r.idle()
	r.hook("stop")
	r.tick(time.Second)
	if p := r.phase(); p != PhaseWarm {
		t.Fatalf("after the turn: %s (%s)", p, r.state().K)
	}
	if rec := r.record(); rec.TerminalStop != nil {
		t.Fatalf("the stop is history once the agent runs: %+v", rec.TerminalStop)
	}

	// warm → cold: nobody uses it for the window.
	stops := len(r.terms.stopped)
	if took := r.sweeps(4*time.Hour, nil); took == 0 {
		t.Fatalf("never stopped")
	}
	if len(r.terms.stopped) != stops+1 || r.phase() != PhaseCold {
		t.Fatalf("stopped %v, phase %s", r.terms.stopped, r.phase())
	}
	if len(r.launcher.resumed) != 1 {
		t.Fatalf("resumed without a message: %v", r.launcher.resumed)
	}
}

// A message that comes while the idle stop is ending the terminal is neither lost nor typed into the dying pane: it
// waits for the stop, then resumes the agent once and is typed.
func TestMessageDuringTheIdleStopResumesTheAgent(t *testing.T) {
	r := newReapRig(t)
	r.agentTakes()
	var sent wire.CmdResult
	typedBefore := 0
	r.terms.onStop = func() {
		// The stop's work runs off the actor, and the actor takes a turn meanwhile.
		c := r.rt.actors[r.id].c
		typedBefore = len(r.pane.calls)
		sent = c.send(wire.SendCmd{OpID: "m1", ConversationID: r.id, ClientID: "m1", Text: "one more thing", Mode: wire.ModeQueue,
			Kind: wire.KindPrompt, AttachmentIDs: []string{}}, r.clock.Now())
		r.sessionGone()
	}
	if took := r.sweeps(4*time.Hour, nil); took == 0 {
		t.Fatalf("never stopped")
	}
	if sent.R == wire.ResultRefused || sent.R == "" {
		t.Fatalf("the message: %+v", sent)
	}
	if len(r.pane.calls) != typedBefore {
		t.Fatalf("typed into the terminal being stopped: %v", r.pane.calls[typedBefore:])
	}
	for range 5 {
		r.tick(time.Second)
	}
	if !slices.Equal(r.launcher.resumed, []string{r.id}) {
		t.Fatalf("resumed %v", r.launcher.resumed)
	}
	r.sessionBack()
	r.settle(wire.StateWorking)
	if lines := r.typedLines(typedBefore); !slices.Equal(lines, []string{"one more thing"}) {
		t.Fatalf("typed %v", lines)
	}
	if len(r.launcher.resumed) != 1 {
		t.Fatalf("resumed %d times", len(r.launcher.resumed))
	}
}

// After a reboot (no tmux session left) a chat that was at work resumes its own agent at once and is told to go on,
// with no one else's startup resume to wait for.
func TestRestartResumesAChatThatWasAtWork(t *testing.T) {
	r := newRig(t)
	r.working("big job")
	r.rt.Stop()
	r.sessionGone()
	r.restart()
	r.tick(time.Second)
	if !slices.Equal(r.launcher.resumed, []string{r.id}) {
		t.Fatalf("resumed %v", r.launcher.resumed)
	}
	if rec := r.record(); len(rec.AutoContinues) != 1 || rec.AutoContinues[0].Why != wire.AutoAfterRestart {
		t.Fatalf("autoContinues: %+v", rec.AutoContinues)
	}
	typed := len(r.pane.calls)
	r.sessionBack()
	r.settle(wire.StateWorking)
	if lines := r.typedLines(typed); !slices.Equal(lines, []string{RestartContinueText}) {
		t.Fatalf("typed %v", lines)
	}
	for range 60 {
		r.tick(time.Second)
	}
	if len(r.launcher.resumed) != 1 {
		t.Fatalf("resumed %d times", len(r.launcher.resumed))
	}
}

// With the automatic continuation off the agent of a chat that was at work still comes back, as it did when the
// tabs' startup resume brought it: at its prompt, with no word typed.
func TestRestartBringsTheAgentBackWithoutTheWord(t *testing.T) {
	r := newRig(t)
	r.deps = func(d *Deps) { d.AutoContinue = func() (bool, bool) { return true, false } }
	r.rt = r.runtime()
	r.working("big job")
	r.rt.Stop()
	r.sessionGone()
	r.restart()
	for range 5 {
		r.tick(time.Second)
	}
	if !slices.Equal(r.launcher.resumed, []string{r.id}) {
		t.Fatalf("resumed %v", r.launcher.resumed)
	}
	if rec := r.record(); len(rec.AutoContinues) != 0 {
		t.Fatalf("autoContinues: %+v", rec.AutoContinues)
	}
}

// A chat that stood at its prompt, or was stopped, when the runtime went down has no session after a reboot and gets
// none: it is cold and resumable, and only its next message brings the agent back.
func TestRestartLeavesAQuietChatCold(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		r := newReapRig(t)
		if stopped {
			if res := r.do(wire.TerminalStopCmd{OpID: "s1", ID: r.id}); res.R != wire.ResultAccepted {
				t.Fatalf("%+v", res)
			}
		}
		r.rt.Stop()
		r.sessionGone()
		r.rt = r.reapRuntime()
		r.restart0()
		for range 60 {
			r.tick(time.Second)
		}
		if st := r.state(); st.K != wire.StateStopped || !st.Resumable {
			t.Fatalf("stopped %v: state %+v", stopped, st)
		}
		if q := r.snap().Queue; q.Paused != nil {
			t.Fatalf("stopped %v: queue paused: %+v", stopped, q.Paused)
		}
		if len(r.launcher.resumed) != 0 {
			t.Fatalf("stopped %v: resumed %v", stopped, r.launcher.resumed)
		}
		if spare, owned := r.rt.SessionSpare("s1"); !owned || !spare {
			t.Fatalf("stopped %v: spare %v owned %v", stopped, spare, owned)
		}
		r.send("m1", "hello again", wire.ModeQueue)
		r.tick(time.Second)
		if !slices.Equal(r.launcher.resumed, []string{r.id}) {
			t.Fatalf("stopped %v: after a message resumed %v", stopped, r.launcher.resumed)
		}
	}
}

// restart0 starts the rig's current runtime over what the last one wrote.
func (r *reapRig) restart0() {
	r.t.Helper()
	if err := r.rt.Start(context.Background()); err != nil {
		r.t.Fatal(err)
	}
}

// A restart that finds the agent still running (tmux outlived the runtime) resumes nothing.
func TestRestartWithTheAgentRunningResumesNothing(t *testing.T) {
	r := newRig(t)
	r.working("big job")
	r.rt.Stop()
	r.restart()
	for range 10 {
		r.tick(time.Second)
	}
	if len(r.launcher.resumed) != 0 {
		t.Fatalf("resumed %v", r.launcher.resumed)
	}
}

// Deps.LegacyLife: the chats' sessions are the tab keepers' again, and the owner answers for none.
func TestLegacyLifeOwnsNoSession(t *testing.T) {
	r := newRig(t)
	r.deps = func(d *Deps) { d.LegacyLife = true }
	r.rt = r.runtime()
	r.open()
	r.settle(wire.StateReady)
	if r.rt.OwnsSession("s1") {
		t.Fatalf("a chat owns its session with the owner off")
	}
	if _, owned := r.rt.SessionSpare("s1"); owned {
		t.Fatalf("the owner answers for a session with the owner off")
	}
}

// The owner answers for the sessions of live chats only.
func TestOwnsSession(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	if !r.rt.OwnsSession("s1") {
		t.Fatalf("the chat's session is not owned")
	}
	if r.rt.OwnsSession("someone-elses") || r.rt.OwnsSession("") {
		t.Fatalf("a session of no chat is owned")
	}
	if spare, owned := r.rt.SessionSpare("s1"); !owned || spare {
		t.Fatalf("a warm chat's session: spare %v owned %v", spare, owned)
	}
	if _, owned := r.rt.SessionSpare("someone-elses"); owned {
		t.Fatalf("the owner answers for a session of no chat")
	}
}

// The floor the tabs' sleep kept for every chat holds under the owner: a Claude chat with no activity for six hours
// is stopped with the idle stop off and with a page holding it open, a machine short of memory stops it after the
// short window, and with neither the setting alone counts. Codex has no floor, as it had no sleep.
func TestIdleFloorHoldsWhateverTheSetting(t *testing.T) {
	const floor, pressure = 6 * time.Hour, 20 * time.Minute
	for _, tc := range []struct {
		name     string
		setting  time.Duration
		floor    time.Duration
		short    bool
		watched  bool
		provider wire.Provider
		legacy   bool
		want     time.Duration // 0: never within eight hours
	}{
		{"setting off", 0, floor, false, false, wire.ProviderClaude, false, floor},
		{"setting off, a page open", 0, floor, false, true, wire.ProviderClaude, false, floor},
		{"setting 2h, a page open", 2 * time.Hour, floor, false, true, wire.ProviderClaude, false, floor},
		{"setting 2h, nobody", 2 * time.Hour, floor, false, false, wire.ProviderClaude, false, 2 * time.Hour},
		{"setting off, codex", 0, floor, false, false, wire.ProviderCodex, false, 0},
		{"setting off, no floor", 0, 0, false, false, wire.ProviderClaude, false, 0},
		{"pressure, setting off", 0, floor, true, false, wire.ProviderClaude, false, pressure},
		{"pressure, setting 2h, a page open", 2 * time.Hour, floor, true, true, wire.ProviderClaude, false, pressure},
		{"pressure, no floor", 0, 0, true, false, wire.ProviderClaude, false, pressure},
		{"the tabs' keepers own the sessions", 0, floor, true, false, wire.ProviderClaude, true, 0},
	} {
		r := &reapRig{rig: newRig(t), terms: &fakeTerms{owned: true, watched: tc.watched, tree: idleTree()}, log: &fakeLog{}, after: tc.setting}
		r.terms.onStop = func() { r.machine.probes["s1"] = Probe{} }
		r.rt = NewInline(Deps{MachineID: "m1", Publisher: r.pub, Machine: r.machine, Launcher: r.launcher, Clock: r.clock,
			Store: NewStore(filepath.Join(r.dir, "v2")), Limits: wire.DefaultLimits, Log: r.log, Terminals: r.terms,
			IdleStop: func() time.Duration { return r.after }, IdleFloor: func() time.Duration { return tc.floor },
			MemoryShort: func() bool { return tc.short }, PressureStop: func() time.Duration { return pressure },
			LegacyLife: tc.legacy})
		r.rt.Import([]ImportTab{{ID: r.id, Session: "s1", Title: "work", Provider: tc.provider, Cwd: "/w", TranscriptPath: r.path}}, nil)
		for range 30 {
			r.tick(time.Second)
		}
		took := r.sweeps(8*time.Hour, nil)
		if tc.want == 0 && took != 0 || tc.want != 0 && (took < tc.want-time.Minute || took > tc.want+2*ReapEvery) {
			t.Errorf("%s: stopped after %v, want %v", tc.name, took, tc.want)
		}
	}
}

// The floor counts from the record's last activity, which a restart of the runtime does not move: a chat quiet for
// hours before the restart is stopped at the second sweep after it, not a whole window later.
func TestIdleFloorSurvivesARestart(t *testing.T) {
	r := newReapRig(t)
	deps := func() *Runtime {
		return NewInline(Deps{MachineID: "m1", Publisher: r.pub, Machine: r.machine, Launcher: r.launcher, Clock: r.clock,
			Store: NewStore(filepath.Join(r.dir, "v2")), Limits: wire.DefaultLimits, Log: r.log, Terminals: r.terms,
			IdleFloor: func() time.Duration { return 6 * time.Hour }})
	}
	r.rt.Stop()
	r.clock.Sleep(7 * time.Hour)
	r.rt = deps()
	r.restart0()
	if took := r.sweeps(time.Hour, nil); took == 0 || took > 3*ReapEvery {
		t.Fatalf("stopped after %v, want within three sweeps of the start", took)
	}
}

func TestPhaseOf(t *testing.T) {
	for k, want := range map[wire.AgentStateKind]Phase{
		wire.StateWorking: PhaseHot, wire.StateWaitingQuestion: PhaseHot, wire.StateWaitingPermission: PhaseHot,
		wire.StateIdle: PhaseWarm, wire.StateReady: PhaseWarm, wire.StateStarting: PhaseWarm, wire.StateUnknown: PhaseWarm,
		wire.StateStopped: PhaseCold, wire.StateGone: PhaseCold, wire.StateLaunchFailed: PhaseCold,
	} {
		if got := PhaseOf(k); got != want {
			t.Errorf("%s: %s, want %s", k, got, want)
		}
	}
}

// A session not found stays gone only while it may be tmux that failed to answer: one still missing after TmuxLostFor
// is gone for good, and the chat resumes its agent for the messages queued meanwhile (tab-o93cj72iu, 12.11: the queue
// waited under «агента нет» with nothing to start the agent).
func TestLostSessionForGoodResumesTheQueue(t *testing.T) {
	r := newReapRig(t)
	rec := r.record()
	if rec.AgentSessionID == "" && rec.TranscriptPath == "" {
		t.Fatalf("the rig's chat has no session to resume: %+v", rec)
	}
	r.sessionGone()
	r.tick(time.Second)
	if st := r.state(); st.K != wire.StateGone || st.Reason != string(wire.GoneTmuxLost) {
		t.Fatalf("state: %+v", st)
	}
	r.agentTakes()
	r.send("m1", "go on", wire.ModeQueue)
	r.tick(time.Minute)
	if len(r.launcher.resumed) != 0 {
		t.Fatalf("resumed while the pane may come back: %v", r.launcher.resumed)
	}
	for range 10 {
		r.tick(15 * time.Second)
	}
	if !slices.Equal(r.launcher.resumed, []string{r.id}) {
		t.Fatalf("resumed %v, state %+v, queue %+v", r.launcher.resumed, r.state(), r.snap().Queue)
	}
	if q := r.snap().Queue; q.Paused != nil {
		t.Fatalf("queue still paused: %+v", q)
	}
}
