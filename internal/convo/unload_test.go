package convo

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/sessionmeta"
	"github.com/ly-solutions/harnessly/internal/shots"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// forgettingMachine is the rig's machine that also keeps what it was told to forget.
type forgettingMachine struct {
	*fakeMachine
	screens, sessions []string
}

func (m *forgettingMachine) ForgetScreen(s string)  { m.screens = append(m.screens, s) }
func (m *forgettingMachine) ForgetSession(s string) { m.sessions = append(m.sessions, s) }

// forgettingPub is the rig's publisher that also keeps the channels it was told to forget.
type forgettingPub struct {
	*fakePub
	forgot []wire.Channel
}

func (p *forgettingPub) Forget(ch wire.Channel) bool { p.forgot = append(p.forgot, ch); return true }
func (p *forgettingPub) Seq(ch wire.Channel) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seqs[ch]
}

type forgettingBg struct{ forgot []string }

func (b *forgettingBg) Tasks(wire.Conversation, string) []wire.BackgroundTask { return nil }
func (b *forgettingBg) Stop(wire.Conversation, string) error                  { return nil }
func (b *forgettingBg) Forget(c wire.Conversation)                            { b.forgot = append(b.forgot, c.TranscriptPath) }

type unloadRig struct {
	*reapRig
	machine *forgettingMachine
	pub     *forgettingPub
	bg      *forgettingBg
	meta    *sessionmeta.Reader
	shots   *shots.Reader
}

func newUnloadRig(t *testing.T) *unloadRig {
	t.Helper()
	base := newRig(t)
	r := &unloadRig{reapRig: &reapRig{rig: base, terms: &fakeTerms{owned: true, tree: idleTree()}, log: &fakeLog{}},
		machine: &forgettingMachine{fakeMachine: base.machine}, pub: &forgettingPub{fakePub: base.pub}, bg: &forgettingBg{},
		meta: &sessionmeta.Reader{}, shots: &shots.Reader{}}
	r.terms.onStop = func() { base.machine.probes["s1"] = Probe{} }
	r.rt = r.unloadRuntime()
	r.open()
	r.settle(wire.StateReady)
	return r
}

func (r *unloadRig) unloadRuntime() *Runtime {
	return NewInline(Deps{MachineID: "m1", Publisher: r.pub, Machine: r.machine, Launcher: r.launcher, Clock: r.clock,
		Store: NewStore(filepath.Join(r.dir, "v2")), Limits: wire.DefaultLimits, Log: r.log, Terminals: r.terms,
		Background: r.bg, Meta: r.meta, Shots: r.shots})
}

// stopAndRest stops the chat's terminal and lets the clock run until nobody has asked for it in UnloadAfter.
func (r *unloadRig) stopAndRest() {
	r.t.Helper()
	if res := r.do(wire.TerminalStopCmd{OpID: "stop-" + itoa(int64(r.clock.Now().UnixMilli())), ID: r.id}); res.R != wire.ResultAccepted {
		r.t.Fatalf("terminal.stop: %+v", res)
	}
	r.settle(wire.StateStopped)
	for range 13 {
		r.tick(UnloadAfter / 12)
	}
}

// listedRecord is the conversation's record as the list has it: reading it asks for no conversation by its id, so it
// is no use of one and wakes none.
func (r *unloadRig) listedRecord() wire.Conversation { r.t.Helper(); return r.rig.record() }

// why is what keeps the conversation's actor, "" when nothing does (or it has none).
func (r *unloadRig) why() string {
	a := r.rt.loaded(r.id)
	if a == nil {
		return ""
	}
	return a.c.notAtRest(r.clock.Now())
}

func (r *unloadRig) listed() string {
	r.t.Helper()
	snap, err := r.rt.Conversations(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	raw, _ := json.Marshal(snap.Conversations)
	return string(raw)
}

// A deleted conversation lets go of what was kept by its id: the readers' cursors in its transcript, what the looks
// kept of its session, its channel and its pane's lock.
func TestDeletedConversationFreesWhatWasKeptForIt(t *testing.T) {
	locks := typist.Locks()
	r := newUnloadRig(t)
	for range 5 {
		r.tick(metaEvery)
	}
	if r.meta.Cursors() == 0 {
		t.Fatal("the transcript was never read: the test would prove nothing")
	}
	if res := r.do(wire.ConversationDeleteCmd{OpID: "del-1", ID: r.id}); res.R != wire.ResultAccepted {
		t.Fatalf("delete: %+v", res)
	}
	if n := r.meta.Cursors() + r.shots.Cursors(); n != 0 {
		t.Fatalf("%d cursors kept for a deleted conversation", n)
	}
	if !slices.Contains(r.bg.forgot, r.path) {
		t.Fatal("the background reader keeps its place in the transcript")
	}
	if !slices.Contains(r.machine.sessions, "s1") {
		t.Fatal("the looks keep the session")
	}
	if !slices.Contains(r.pub.forgot, wire.ConvChannel(r.id)) {
		t.Fatal("the conversation's channel is kept")
	}
	if got := typist.Locks(); got != locks {
		t.Fatalf("%d pane locks left behind", got-locks)
	}
	// Nothing reads the deleted conversation again: the cursors stay gone.
	for range 5 {
		r.tick(metaRestEvery)
	}
	if n := r.meta.Cursors() + r.shots.Cursors(); n != 0 {
		t.Fatalf("%d cursors came back", n)
	}
}

// A stopped terminal takes the session's tall capture with it; the rest stays while the conversation is looked at.
func TestStoppedTerminalFreesItsScreen(t *testing.T) {
	r := newUnloadRig(t)
	if res := r.do(wire.TerminalStopCmd{OpID: "stop-1", ID: r.id}); res.R != wire.ResultAccepted {
		t.Fatalf("terminal.stop: %+v", res)
	}
	if !slices.Contains(r.machine.screens, "s1") {
		t.Fatal("the screen kept of a stopped terminal was not let go")
	}
	if len(r.machine.sessions) != 0 {
		t.Fatal("the session is still looked at: its probe must stay")
	}
}

// A stopped conversation nobody asked for in UnloadAfter loses its actor and stays on the list as it was; asked for by
// its id it has an actor again, with the state it was parked in.
func TestStoppedConversationIsParkedAndComesBack(t *testing.T) {
	r := newUnloadRig(t)
	r.do(wire.ConversationRenameCmd{OpID: "rename-1", ID: r.id, Title: "kept"})

	// A working, an idle and a freshly stopped conversation keep their actors.
	if got := r.rt.Unload(); len(got) != 0 {
		t.Fatalf("an idle conversation was parked: %v", got)
	}
	r.stopAndRest()

	r.terms.watched = true
	if got := r.rt.Unload(); len(got) != 0 {
		t.Fatalf("a conversation a page holds open was parked: %v", got)
	}
	r.terms.watched = false
	if got := r.rt.Unload(); len(got) != 0 {
		t.Fatal("a watched conversation counts as used: it waits UnloadAfter again")
	}
	for range 13 {
		r.tick(UnloadAfter / 12)
	}
	before, list := r.listedRecord(), r.listed()
	if before.State.K != wire.StateStopped || before.Title != "kept" {
		t.Fatalf("before: %+v %q", before.State, before.Title)
	}
	if got := r.rt.Unload(); !slices.Equal(got, []string{r.id}) {
		t.Fatalf("parked %v (kept by: %s)", got, r.why())
	}
	if r.rt.Loaded() != 0 || r.rt.Parked() != 1 {
		t.Fatalf("loaded %d, parked %d", r.rt.Loaded(), r.rt.Parked())
	}
	if r.meta.Cursors()+r.shots.Cursors() != 0 || !slices.Contains(r.machine.sessions, "s1") {
		t.Fatal("a parked conversation keeps its cursors or its session's probe")
	}

	// The lists and the reads that wake nothing answer as before.
	if got := r.listed(); got != list {
		t.Fatalf("the list changed:\n%s\n%s", list, got)
	}
	if rec := r.listedRecord(); rec.Rev != before.Rev || rec.State.K != wire.StateStopped {
		t.Fatalf("record of the parked conversation: rev %d state %s", rec.Rev, rec.State.K)
	}
	if !r.rt.Has(r.id) || !slices.Contains(r.rt.Transcripts(), r.path) || !slices.Contains(r.rt.MoveIDs(), r.id) {
		t.Fatal("the parked conversation is missing from what lists every conversation")
	}
	r.tick(time.Minute)
	r.rt.Reap()
	if r.rt.Loaded() != 0 {
		t.Fatal("a list, a tick or a sweep woke the parked conversation")
	}

	// A page opens it: the actor is back, with what it was parked with.
	snap := r.snap()
	if r.rt.Loaded() != 1 || r.rt.Parked() != 0 {
		t.Fatalf("after a read: loaded %d, parked %d", r.rt.Loaded(), r.rt.Parked())
	}
	after := r.listedRecord()
	if after.State.K != wire.StateStopped || !after.State.Resumable || after.Title != "kept" || after.Rev != before.Rev ||
		after.TerminalStop == nil || after.Session != before.Session || after.TranscriptPath != before.TranscriptPath {
		t.Fatalf("came back as %+v (rev %d, was %d)", after.State, after.Rev, before.Rev)
	}
	if len(snap.Questions) != 0 || len(snap.Permissions) != 0 {
		t.Fatalf("snapshot: %+v", snap)
	}
	if got := r.listed(); got != list {
		t.Fatalf("the list changed with the wake:\n%s\n%s", list, got)
	}

	// It works as any conversation: a command changes it, and the change is written.
	if res := r.do(wire.ConversationRenameCmd{OpID: "rename-2", ID: r.id, Title: "after"}); res.R != wire.ResultAccepted {
		t.Fatalf("rename after the wake: %+v", res)
	}
	r.tick(time.Second)
	if f, err := r.rt.d.Store.loadConv(r.id); err != nil || f.Conv.Title != "after" {
		t.Fatalf("written: %q, %v", f.Conv.Title, err)
	}
}

// A hook and a message wake a parked conversation too; one that has a message waiting is never parked.
func TestParkedConversationWakesForAHookAndAMessage(t *testing.T) {
	r := newUnloadRig(t)
	r.stopAndRest()
	if got := r.rt.Unload(); len(got) != 1 {
		t.Fatalf("parked %v (kept by: %s)", got, r.why())
	}
	r.hook("session-start")
	if r.rt.Loaded() != 1 {
		t.Fatal("the hook did not wake the conversation")
	}

	r2 := newUnloadRig(t)
	r2.stopAndRest()
	if got := r2.rt.Unload(); len(got) != 1 {
		t.Fatalf("parked %v (kept by: %s)", got, r2.why())
	}
	if res := r2.send("m1", "go on", wire.ModeQueue); res.R == wire.ResultRefused {
		t.Fatalf("send to a parked conversation: %+v", res)
	}
	if r2.rt.Loaded() != 1 {
		t.Fatal("the message did not wake the conversation")
	}
	for range 13 {
		r2.tick(UnloadAfter / 12)
	}
	if rec := r2.listedRecord(); rec.Queue.Count > 0 {
		if got := r2.rt.Unload(); len(got) != 0 {
			t.Fatal("a conversation with a message waiting was parked")
		}
	}
}

// A parked conversation survives a restart of the runtime as any other: its file is all there is of it.
func TestParkedConversationSurvivesARestart(t *testing.T) {
	r := newUnloadRig(t)
	r.stopAndRest()
	if got := r.rt.Unload(); len(got) != 1 {
		t.Fatalf("parked %v (kept by: %s)", got, r.why())
	}
	r.rt.Stop()
	r.rt = r.unloadRuntime()
	if err := r.rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !r.rt.Has(r.id) || r.rt.Loaded() != 1 {
		t.Fatalf("after the restart: has %v, loaded %d", r.rt.Has(r.id), r.rt.Loaded())
	}
}

// On its goroutines: a parked actor's goroutine ends, a read wakes the conversation while lists go on answering, and
// the runtime stops with parked conversations.
func TestParkOnGoroutines(t *testing.T) {
	r := newUnloadRig(t)
	r.stopAndRest()
	r.rt.Stop()

	rt := New(Deps{MachineID: "m1", Publisher: r.pub, Machine: r.machine, Launcher: r.launcher, Clock: r.clock,
		Store: NewStore(filepath.Join(r.dir, "v2")), Limits: wire.DefaultLimits, Log: r.log, Terminals: r.terms})
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	defer func() {
		go func() { rt.Stop(); close(stopped) }()
		select {
		case <-stopped:
		case <-time.After(30 * time.Second):
			t.Error("the runtime did not stop")
		}
	}()
	state := func() wire.AgentStateKind {
		rec, _ := rt.Record(r.id)
		return rec.State.K
	}
	// The start looks at the conversation once and finds it stopped, as it was.
	for deadline := time.Now().Add(20 * time.Second); state() != wire.StateStopped; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("state %s after the start", state())
		}
	}
	r.clock.Sleep(UnloadAfter + time.Hour)
	// The rig's clock never fires by itself: the actor turns only for Advance, and what a turn sends off its goroutine
	// (a look at the pane) comes back as mail of its own. The sweep waits for that, or it finds the work under way
	// and keeps the actor — every pass, on a runner slow enough that the look is never back first.
	why := ""
	for pass := 0; rt.Parked() == 0; pass++ {
		if pass == 10 {
			t.Fatalf("the conversation was not parked: %s", why)
		}
		rt.Advance() // the clock's turn: the record's change goes out, the feed lets go
		r.clock.Sleep(FeedKept + time.Minute)
		why = workDone(t, rt, r.id)
		rt.Unload()
	}
	if rt.Loaded() != 0 {
		t.Fatalf("loaded %d", rt.Loaded())
	}
	list, err := rt.Conversations(context.Background())
	if err != nil || len(list.Conversations) != 1 || list.Conversations[0].State.K != wire.StateStopped {
		t.Fatalf("the list with a parked conversation: %+v, %v", list.Conversations, err)
	}

	// Readers that come at once wake one actor, and every one of them is answered.
	errs := make(chan error, 8)
	for range cap(errs) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, err := rt.Conversation(ctx, r.id)
			errs <- err
		}()
	}
	for range cap(errs) {
		if err := <-errs; err != nil {
			t.Fatalf("a read of a parked conversation: %v", err)
		}
	}
	if rt.Loaded() != 1 || rt.Parked() != 0 || state() != wire.StateStopped {
		t.Fatalf("after the reads: loaded %d, parked %d, state %s", rt.Loaded(), rt.Parked(), state())
	}
}

// workDone waits until the conversation's actor has no work off its goroutine, and says what still keeps it then.
func workDone(t *testing.T, rt *Runtime, id string) string {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(time.Millisecond) {
		a := rt.loaded(id)
		if a == nil {
			return "no actor"
		}
		jobs, why := 0, ""
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := rt.callOn(ctx, a, func(c *conv, now time.Time) { jobs, why = c.jobs, c.notAtRest(now) })
		cancel()
		if err == nil && jobs == 0 {
			return why
		}
		if time.Now().After(deadline) {
			t.Fatalf("the actor's work did not end: %d jobs, %v", jobs, err)
		}
	}
}

// What turns by the clock or lists every conversation leaves a parked one parked: the sweeps, the lists, the
// take-over of the Legacy tabs (which names every tab at every pass) and marking everything read.
func TestTimersAndListsDoNotWakeAParkedConversation(t *testing.T) {
	r := newUnloadRig(t)
	r.stopAndRest()
	if got := r.rt.Unload(); len(got) != 1 {
		t.Fatalf("parked %v (kept by: %s)", got, r.why())
	}
	rec := r.listedRecord()
	tab := ImportTab{ID: r.id, Session: "s1", Title: rec.Title, Provider: wire.ProviderClaude, Cwd: "/w", TranscriptPath: r.path}
	projects := []LegacyProject{{ID: "legacy-p1", Name: "work"}}
	for pass := range 30 {
		r.tick(ReapEvery)
		r.rt.Reap()
		r.rt.Unload()
		r.rt.RetitleImported(tab)
		r.rt.Import([]ImportTab{tab}, nil)
		r.rt.ImportProjects(projects, map[string]string{r.id: "legacy-p1"}) // its folder is not there: never placed
		r.listed()
		r.rt.Record(r.id)
		r.rt.Has(r.id)
		r.rt.Transcripts()
		r.rt.AttachedSessions()
		r.rt.MoveIDs()
		r.rt.Busy()
		r.rt.ConvOfSession("s1")
		r.rt.TerminalOutput("s1", []byte("noise"))
		r.do(wire.SeenCmd{OpID: "seen-" + itoa(int64(pass)), ConversationID: wire.SeenAll, UpTo: ms(r.clock.Now())})
		if r.rt.Loaded() != 0 || r.rt.Parked() != 1 {
			t.Fatalf("pass %d: loaded %d, parked %d", pass, r.rt.Loaded(), r.rt.Parked())
		}
	}
	// A title the agent wrote meanwhile is a change: that does wake it, once.
	tab.Title = "named by the agent"
	r.rt.RetitleImported(tab)
	if after := r.listedRecord(); after.TitleSource == wire.TitleFromAgent && (r.rt.Loaded() != 1 || after.Title != tab.Title) {
		t.Fatalf("a new title: loaded %d, title %q", r.rt.Loaded(), after.Title)
	}
}
