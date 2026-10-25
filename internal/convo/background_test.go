package convo

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// fakeBackground is the agent's background work, as the test sets it, and the stops asked of it.
type fakeBackground struct {
	mu      sync.Mutex
	tasks   []wire.BackgroundTask
	panes   []string
	stopped []string
	wake    wire.Ms
	failing error
}

func (f *fakeBackground) Tasks(_ wire.Conversation, pane string) []wire.BackgroundTask {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.panes = append(f.panes, pane)
	return slices.Clone(f.tasks)
}

func (f *fakeBackground) Stop(_ wire.Conversation, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing != nil {
		return f.failing
	}
	f.stopped = append(f.stopped, id)
	return nil
}

func (f *fakeBackground) set(tasks ...wire.BackgroundTask) {
	f.mu.Lock()
	f.tasks = tasks
	f.mu.Unlock()
}

func shell(id, title string, at wire.Ms) wire.BackgroundTask {
	return wire.BackgroundTask{ID: id, ConversationID: convID, Kind: wire.BgCommand, Title: title, StartedAt: at,
		Status: wire.BgStatusRunning, Stoppable: true}
}

func (r *rig) withBackground(f *fakeBackground) {
	r.rt.d.Background = f
}

func (r *rig) bgEvents() []string {
	var out []string
	for _, p := range r.pub.all {
		switch e := p.e.(type) {
		case wire.BackgroundUpsertEv:
			out = append(out, "upsert:"+e.Task.ID+":"+string(e.Task.Status))
		case wire.BackgroundRemovedEv:
			out = append(out, "removed:"+e.ID)
		}
	}
	return out
}

func TestBackgroundTasksComeAndGo(t *testing.T) {
	r := newRig(t)
	f := &fakeBackground{}
	r.withBackground(f)
	r.open()
	r.settle(wire.StateReady)

	f.set(shell("b1", "npm run dev", 1_800_000_000_500))
	r.tick(bgEvery)
	if got := r.snap().Background; len(got) != 1 || got[0].ID != "b1" || got[0].Status != wire.BgStatusRunning {
		t.Fatalf("%+v", got)
	}
	// The same list again says nothing; a second task goes out whole, ahead of nothing it already had.
	r.tick(bgEvery)
	f.set(shell("b1", "npm run dev", 1_800_000_000_500), shell("b2", "tail -f log", 1_800_000_000_900))
	r.tick(bgEvery)
	if got := r.bgEvents(); !slices.Equal(got, []string{"upsert:b1:running", "upsert:b2:running"}) {
		t.Fatalf("%v", got)
	}
	f.set(shell("b2", "tail -f log", 1_800_000_000_900))
	r.tick(bgEvery)
	if got := r.bgEvents(); !slices.Equal(got, []string{"upsert:b1:running", "upsert:b2:running", "removed:b1"}) {
		t.Fatalf("%v", got)
	}
	if got := r.snap().Background; len(got) != 1 || got[0].ID != "b2" {
		t.Fatalf("%+v", got)
	}
	if len(f.panes) == 0 || f.panes[len(f.panes)-1] == "" {
		t.Fatalf("the task read was not given the screen: %q", f.panes)
	}
}

func TestATaskOnlyTheFooterKnowsKeepsItsFirstTime(t *testing.T) {
	r := newRig(t)
	f := &fakeBackground{}
	r.withBackground(f)
	r.open()
	r.settle(wire.StateReady)
	unnamed := shell("shell-0", "", 0)
	f.set(unnamed)
	r.tick(bgEvery)
	first := r.snap().Background[0].StartedAt
	if first == 0 {
		t.Fatalf("%+v", r.snap().Background)
	}
	r.tick(bgEvery)
	f.set(unnamed)
	r.tick(bgEvery)
	if got := r.snap().Background[0].StartedAt; got != first || len(r.bgEvents()) != 1 {
		t.Fatalf("started %d then %d; %v", first, got, r.bgEvents())
	}
}

func TestStoppingATaskShowsItStoppedUntilTheAgentDropsIt(t *testing.T) {
	r := newRig(t)
	f := &fakeBackground{}
	r.withBackground(f)
	r.open()
	r.settle(wire.StateReady)
	f.set(shell("b1", "npm run dev", 1_800_000_000_500))
	r.tick(bgEvery)

	res := r.do(wire.BgStopCmd{OpID: "st1", ConversationID: r.id, TaskID: "b1"})
	if res.R != wire.ResultAccepted || res.Ch != wire.ConvChannel(r.id) || !slices.Equal(f.stopped, []string{"b1"}) {
		t.Fatalf("%+v %v", res, f.stopped)
	}
	ups := r.events(wire.EvBackgroundUpsert)
	last := ups[len(ups)-1]
	if task := last.e.(wire.BackgroundUpsertEv).Task; task.Status != wire.BgStatusStopped || task.EndedAt == nil || task.Stoppable || last.seq != res.Seq {
		t.Fatalf("answer seq %d, last upsert %+v", res.Seq, last)
	}
	// The agent still lists it: it stays stopped, and nothing more goes out.
	r.tick(bgEvery)
	if got := r.snap().Background; len(got) != 1 || got[0].Status != wire.BgStatusStopped || len(r.bgEvents()) != 2 {
		t.Fatalf("%+v %v", got, r.bgEvents())
	}
	// Stopped again: accepted, nothing is killed twice.
	if res := r.do(wire.BgStopCmd{OpID: "st2", ConversationID: r.id, TaskID: "b1"}); res.R != wire.ResultAccepted || len(f.stopped) != 1 {
		t.Fatalf("%+v %v", res, f.stopped)
	}
	f.set()
	r.tick(bgEvery)
	if got := r.snap().Background; len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestATaskThatCannotBeStoppedIsRefused(t *testing.T) {
	r := newRig(t)
	f := &fakeBackground{}
	r.withBackground(f)
	r.open()
	r.settle(wire.StateReady)
	sub := wire.BackgroundTask{ID: "a1", ConversationID: convID, Kind: wire.BgSubagent, Title: "review", StartedAt: 1_800_000_000_100,
		Status: wire.BgStatusRunning}
	f.set(sub, shell("b1", "npm run dev", 1_800_000_000_500))
	r.tick(bgEvery)
	for id, want := range map[string]wire.RefuseCode{"a1": wire.RefuseUnsupported, "nope": wire.RefuseNotFound} {
		if res := r.do(wire.BgStopCmd{OpID: "st-" + id, ConversationID: r.id, TaskID: id}); res.R != wire.ResultRefused || res.Code != want {
			t.Errorf("%s: %+v", id, res)
		}
	}
	f.failing = errors.New("no process")
	if res := r.do(wire.BgStopCmd{OpID: "st-b1", ConversationID: r.id, TaskID: "b1"}); res.R != wire.ResultRefused || res.Code != wire.RefuseInternal {
		t.Fatalf("%+v", res)
	}
	if got := r.snap().Background; got[1].Status != wire.BgStatusRunning && got[0].Status != wire.BgStatusRunning {
		t.Fatalf("a task that did not stop shows stopped: %+v", got)
	}
}

func TestTheTasksOfAnAgentThatLeftLeaveWithIt(t *testing.T) {
	r := newRig(t)
	f := &fakeBackground{}
	r.withBackground(f)
	r.open()
	r.settle(wire.StateReady)
	f.set(shell("b1", "npm run dev", 1_800_000_000_500))
	r.tick(bgEvery)
	r.machine.probes["s1"] = Probe{}
	for range 5 {
		r.tick(10 * time.Second)
	}
	if st := r.state(); st.K != wire.StateGone {
		t.Fatalf("%+v", st)
	}
	if got := r.snap().Background; len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func (f *fakeBackground) Wakeup(wire.Conversation) wire.Ms {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.wake
}

func (f *fakeBackground) wakeAt(at wire.Ms) {
	f.mu.Lock()
	f.wake = at
	f.mu.Unlock()
}

// What the agent waits for is every kind of background work: subagents, commands, monitors and its own wakeup.
func TestTheRecordCountsAllTheBackgroundWorkTheAgentWaitsFor(t *testing.T) {
	r := newRig(t)
	f := &fakeBackground{}
	r.withBackground(f)
	r.open()
	r.settle(wire.StateReady)
	rec := func() wire.Conversation {
		cs, err := r.rt.Conversations(t.Context())
		if err != nil || len(cs.Conversations) != 1 {
			t.Fatalf("%v %+v", err, cs.Conversations)
		}
		return cs.Conversations[0]
	}
	sub := func(id string) wire.BackgroundTask {
		task := shell(id, "Explore", 1_800_000_000_500)
		task.Kind = wire.BgSubagent
		return task
	}
	mon := func(id string) wire.BackgroundTask {
		task := shell(id, "watch the build", 1_800_000_000_600)
		task.Monitor = true
		return task
	}
	cmd := shell("b1", "npm test", 1_800_000_000_400)

	// A background command alone keeps the chat at work.
	f.set(cmd)
	r.tick(bgEvery)
	if got := rec(); got.BgAgents != 1 || got.BgWait == nil || got.BgWait.Commands != 1 || got.BgWait.Command != "npm test" {
		t.Fatalf("a command: %d %+v", got.BgAgents, got.BgWait)
	}
	flow := shell("w1", "Review the checkout", 1_800_000_000_700)
	flow.Kind, flow.Workflow = wire.BgWorkflow, &wire.WorkflowRun{Name: "review", Phases: []wire.WorkflowPhase{}}
	// Everything at once: the total is the sum, the parts say what it is made of.
	f.set(cmd, shell("b2", "tail -f log", 1_800_000_000_450), sub("a1"), sub("a2"), mon("m1"), flow)
	r.tick(bgEvery)
	got := rec()
	if got.BgAgents != 6 || got.BgWait == nil || *got.BgWait != (wire.BgWait{Agents: 2, Commands: 2, Monitors: 1, Workflows: 1}) {
		t.Fatalf("a mix: %d %+v", got.BgAgents, got.BgWait)
	}
	// A wakeup counts too, with its time; only the subagents' count is kept when they alone are left.
	at := ms(r.clock.Now()) + 600_000
	f.set(sub("a2"))
	f.wakeAt(at)
	r.tick(bgEvery)
	got = rec()
	if got.BgAgents != 2 || got.BgWait == nil || got.BgWait.WakeupAt == nil || *got.BgWait.WakeupAt != at || got.BgWait.Agents != 1 {
		t.Fatalf("a wakeup: %d %+v", got.BgAgents, got.BgWait)
	}
	// Ended work is not waited for.
	f.set(ended("a2", "Explore", 1_800_000_000_100, 1_800_000_001_000, wire.BgStatusDone))
	r.tick(bgEvery)
	if got := rec(); got.BgAgents != 1 || got.BgWait == nil || got.BgWait.Agents != 0 {
		t.Fatalf("an ended subagent: %d %+v", got.BgAgents, got.BgWait)
	}
	// The wakeup comes: nothing is waited for, and the record says so.
	f.wakeAt(0)
	r.tick(bgEvery)
	if got := rec(); got.BgAgents != 0 || got.BgWait != nil {
		t.Fatalf("none is left: %d %+v", got.BgAgents, got.BgWait)
	}
}

// A wakeup whose time has passed is not waited for although the read still lists it.
func TestAWakeupThatCameIsNotWaitedFor(t *testing.T) {
	r := newRig(t)
	f := &fakeBackground{}
	r.withBackground(f)
	r.open()
	r.settle(wire.StateReady)
	f.wakeAt(ms(r.clock.Now()) + 5_000)
	r.tick(bgEvery)
	cs, _ := r.rt.Conversations(t.Context())
	if cs.Conversations[0].BgAgents != 1 {
		t.Fatalf("a wakeup in 5s: %+v", cs.Conversations[0])
	}
	r.tick(bgEvery)
	r.tick(bgEvery)
	cs, _ = r.rt.Conversations(t.Context())
	if cs.Conversations[0].BgAgents != 0 || cs.Conversations[0].BgWait != nil {
		t.Fatalf("it came: %+v", cs.Conversations[0])
	}
}

func ended(id, title string, started, at wire.Ms, status wire.BackgroundStatus) wire.BackgroundTask {
	task := wire.BackgroundTask{ID: id, ConversationID: convID, Kind: wire.BgSubagent, Title: title, StartedAt: started, Status: status}
	task.EndedAt = &at
	return task
}

// The snapshot a person opens the chat with lists the subagents that ended beside the ones that run, and a finished one
// is no agent the chat waits for.
func TestTheSnapshotListsTheSubagentsThatEnded(t *testing.T) {
	r := newRig(t)
	f := &fakeBackground{}
	r.withBackground(f)
	r.open()
	r.settle(wire.StateReady)
	run := shell("a1", "audit backend", 1_800_000_000_100)
	run.Kind, run.Stoppable = wire.BgSubagent, false
	f.set(run, ended("a2", "audit shell", 1_800_000_000_200, 1_800_000_000_900, wire.BgStatusDone))
	r.tick(bgEvery)
	got := r.snap().Background
	if len(got) != 2 || got[0].ID != "a1" || got[0].Status != wire.BgStatusRunning || got[1].ID != "a2" || got[1].Status != wire.BgStatusDone || got[1].EndedAt == nil {
		t.Fatalf("%+v", got)
	}
	cs, err := r.rt.Conversations(t.Context())
	if err != nil || cs.Conversations[0].BgAgents != 1 {
		t.Fatalf("%v %+v", err, cs.Conversations)
	}
	// It finishes: the same task, done, goes out whole, and nothing is waited for.
	f.set(ended("a1", "audit backend", 1_800_000_000_100, 1_800_000_001_000, wire.BgStatusDone), ended("a2", "audit shell", 1_800_000_000_200, 1_800_000_000_900, wire.BgStatusDone))
	r.tick(bgEvery)
	if got := r.bgEvents(); !slices.Equal(got, []string{"upsert:a1:running", "upsert:a2:done", "upsert:a1:done"}) {
		t.Fatalf("%v", got)
	}
	if cs, _ := r.rt.Conversations(t.Context()); cs.Conversations[0].BgAgents != 0 {
		t.Fatalf("%+v", cs.Conversations)
	}
}

// An agent that left takes what ran with it, but the subagents that ended stay for a chat somebody opens; one nobody
// opens is not read.
func TestTheEndedSubagentsOutliveTheAgent(t *testing.T) {
	r := newRig(t)
	f := &fakeBackground{}
	r.withBackground(f)
	r.open()
	r.settle(wire.StateReady)
	run := shell("a1", "audit backend", 1_800_000_000_100)
	run.Kind, run.Stoppable = wire.BgSubagent, false
	f.set(run, shell("b1", "npm run dev", 1_800_000_000_300), ended("a2", "audit shell", 1_800_000_000_200, 1_800_000_000_900, wire.BgStatusDone))
	r.tick(bgEvery)
	r.machine.probes["s1"] = Probe{}
	for range 5 {
		r.tick(10 * time.Second)
	}
	if st := r.state(); st.K != wire.StateGone {
		t.Fatalf("%+v", st)
	}
	_ = r.snap() // the first look at the chat since the agent left: the read follows
	r.tick(bgEvery)
	got := r.snap().Background
	if len(got) != 1 || got[0].ID != "a2" || got[0].Status != wire.BgStatusDone {
		t.Fatalf("%+v", got)
	}
}

// A teammate at work keeps the chat at work and is counted as one; an idle one waits for a message and nothing waits
// for it, until a message sends it back to work. With the agent gone its teammates are over and stay as done.
func TestTeammatesAtWorkAreWaitedForAndIdleOnesAreNot(t *testing.T) {
	r := newRig(t)
	f := &fakeBackground{}
	r.withBackground(f)
	r.open()
	r.settle(wire.StateReady)
	rec := func() wire.Conversation {
		cs, err := r.rt.Conversations(t.Context())
		if err != nil || len(cs.Conversations) != 1 {
			t.Fatalf("%v %+v", err, cs.Conversations)
		}
		return cs.Conversations[0]
	}
	mate := func(id string, status wire.BackgroundStatus) wire.BackgroundTask {
		task := wire.BackgroundTask{ID: id, ConversationID: convID, Kind: wire.BgSubagent, Teammate: true, Title: id,
			StartedAt: 1_800_000_000_100, Status: status}
		if status != wire.BgStatusRunning {
			end := wire.Ms(1_800_000_000_900)
			task.EndedAt = &end
		}
		return task
	}
	sub := shell("a1", "Explore", 1_800_000_000_500)
	sub.Kind = wire.BgSubagent

	f.set(mate("impl-g1", wire.BgStatusRunning), mate("impl-h1", wire.BgStatusRunning), mate("rev-t4", wire.BgStatusIdle), sub)
	r.tick(bgEvery)
	if got := rec(); got.BgAgents != 3 || got.BgWait == nil || *got.BgWait != (wire.BgWait{Agents: 1, Teammates: 2}) {
		t.Fatalf("two at work: %d %+v", got.BgAgents, got.BgWait)
	}
	f.set(mate("impl-g1", wire.BgStatusIdle), mate("impl-h1", wire.BgStatusIdle), mate("rev-t4", wire.BgStatusIdle))
	r.tick(bgEvery)
	if got := rec(); got.BgAgents != 0 || got.BgWait != nil {
		t.Fatalf("all idle: %d %+v", got.BgAgents, got.BgWait)
	}
	f.set(mate("impl-g1", wire.BgStatusRunning), mate("impl-h1", wire.BgStatusIdle), mate("rev-t4", wire.BgStatusDone))
	r.tick(bgEvery)
	if got := rec(); got.BgAgents != 1 || got.BgWait == nil || got.BgWait.Teammates != 1 {
		t.Fatalf("one sent a message: %d %+v", got.BgAgents, got.BgWait)
	}
	r.machine.probes["s1"] = Probe{}
	for range 5 {
		r.tick(10 * time.Second)
	}
	if st := r.state(); st.K != wire.StateGone {
		t.Fatalf("%+v", st)
	}
	_ = r.snap()
	r.tick(bgEvery)
	got := r.snap().Background
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	for _, task := range got {
		if task.Status != wire.BgStatusDone {
			t.Errorf("%s is %s with the agent gone", task.ID, task.Status)
		}
	}
	if c := rec(); c.BgAgents != 0 || c.BgWait != nil {
		t.Fatalf("the agent gone: %d %+v", c.BgAgents, c.BgWait)
	}
}
