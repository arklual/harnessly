package convo

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// slowMachine has one idle pane; its probe waits while hold is set, so the actor is busy for that long.
type slowMachine struct {
	mu   sync.Mutex
	hold time.Duration
	pane *lockedPane
}

func (m *slowMachine) Pane(string) typist.Pane { return m.pane }
func (m *slowMachine) Probe(string) Probe {
	m.mu.Lock()
	d := m.hold
	m.mu.Unlock()
	time.Sleep(d)
	return Probe{Session: true, Foreground: true}
}

type lockedPane struct {
	mu     sync.Mutex
	screen string
}

func (p *lockedPane) CaptureStyled() (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.screen, true
}
func (p *lockedPane) Capture() (string, bool) { return p.CaptureStyled() }
func (p *lockedPane) Paste(string) error      { return nil }
func (p *lockedPane) TypeLine(string) error   { return nil }
func (p *lockedPane) Keys(...string) error    { return nil }

// The runtime on its goroutines: commands from many callers at once answer in order per conversation, a command
// stuck behind a busy actor answers refused{timeout} within the budget, and its retry gets the answer it had.
func TestActorsOnTheirGoroutines(t *testing.T) {
	dir := t.TempDir()
	pub := &fakePub{t: t, seqs: map[wire.Channel]int64{}}
	m := &slowMachine{pane: &lockedPane{screen: tui(t, "claude-idle-placeholder.ansi.txt")}}
	limits := wire.DefaultLimits
	limits.CmdBudgetMs = 200
	rt := New(Deps{MachineID: "m1", Publisher: pub, Machine: m, Store: NewStore(filepath.Join(dir, "v2")), Limits: limits})
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "t.jsonl")
	writeFile(path, "")
	rt.Import([]ImportTab{{ID: "tab-a", Session: "s", Provider: wire.ProviderClaude, TranscriptPath: path},
		{ID: "tab-b", Session: "s2", Provider: wire.ProviderClaude, TranscriptPath: path}}, nil)

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := "tab-a"
			if i%2 == 1 {
				id = "tab-b"
			}
			res := rt.Do(context.Background(), "d", wire.ConversationPinCmd{OpID: "pin-" + itoa(int64(i)), ID: id, Pinned: i%4 < 2})
			if res.R != wire.ResultAccepted {
				t.Errorf("pin %d: %+v", i, res)
			}
			if _, err := rt.Conversation(context.Background(), id); err != nil {
				t.Errorf("snapshot: %v", err)
			}
		}()
	}
	wg.Wait()

	// A look at the pane stuck on a loaded machine does not hold the actor's mail: the command answers at once.
	m.mu.Lock()
	m.hold = 1500 * time.Millisecond
	m.mu.Unlock()
	time.Sleep(LookEvery + 100*time.Millisecond)
	start := time.Now()
	res := rt.Do(context.Background(), "d", wire.ConversationRenameCmd{OpID: "slow", ID: "tab-a", Title: "x"})
	if took := time.Since(start); took > 150*time.Millisecond || res.R != wire.ResultAccepted || res.Rev == nil {
		t.Fatalf("behind a stuck look: %+v after %v", res, took)
	}
	if _, err := rt.Conversations(context.Background()); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.hold = 0
	m.mu.Unlock()
	rt.Stop()

	files, err := NewStore(filepath.Join(dir, "v2")).loadConvs()
	if err != nil || len(files) != 2 {
		t.Fatalf("saved %d conversations: %v", len(files), err)
	}
	for _, f := range files {
		if f.Conv.ID == "tab-a" && f.Conv.Title != "x" {
			t.Fatalf("saved title %q", f.Conv.Title)
		}
	}
}

// slowLauncher keeps the actor for hold: a command that does not finish within the budget answers refused{timeout},
// and its retry gets the answer the first one came to.
type slowLauncher struct{ hold time.Duration }

func (l slowLauncher) Launch(LaunchRequest) error     { time.Sleep(l.hold); return nil }
func (l slowLauncher) Resume(wire.Conversation) error { return nil }

func TestACommandPastTheBudgetTimesOutAndItsRetryGetsTheAnswer(t *testing.T) {
	pub := &fakePub{t: t, seqs: map[wire.Channel]int64{}}
	limits := wire.DefaultLimits
	limits.CmdBudgetMs = 200
	rt := New(Deps{MachineID: "m1", Publisher: pub, Machine: &fakeMachine{}, Launcher: slowLauncher{hold: 700 * time.Millisecond}, Limits: limits})
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer rt.Stop()
	cmd := wire.ConversationCreateCmd{OpID: "create-1", Provider: wire.ProviderClaude, Cwd: "/tmp"}
	start := time.Now()
	first := rt.Do(context.Background(), "d", cmd)
	if took := time.Since(start); took > 400*time.Millisecond || first.R != wire.ResultRefused || first.Code != wire.RefuseTimeout {
		t.Fatalf("past the budget: %+v after %v", first, took)
	}
	var again wire.CmdResult
	for range 20 {
		if again = rt.Do(context.Background(), "d", cmd); again.R == wire.ResultAccepted {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if again.R != wire.ResultAccepted || again.ID == nil {
		t.Fatalf("the retry: %+v", again)
	}
	if third := rt.Do(context.Background(), "d", cmd); third.ID == nil || *third.ID != *again.ID {
		t.Fatalf("the answer moved: %+v then %+v", again, third)
	}
}

// An actor stuck in a turn does not hold a list: the list takes the record the actor left after its last turn, and a
// page asking for its conversation waits only for its own budget.
func TestAListDoesNotWaitForAStuckActor(t *testing.T) {
	pub := &fakePub{t: t, seqs: map[wire.Channel]int64{}}
	limits := wire.DefaultLimits
	limits.CmdBudgetMs = 300
	rt := New(Deps{MachineID: "m1", Publisher: pub, Machine: &fakeMachine{}, Launcher: slowLauncher{hold: 1500 * time.Millisecond}, Limits: limits})
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer rt.Stop()
	rt.Import([]ImportTab{{ID: "tab-ok", Provider: wire.ProviderClaude}}, nil)
	res := rt.Do(context.Background(), "d", wire.ConversationCreateCmd{OpID: "create-2", Provider: wire.ProviderClaude, Cwd: "/tmp"})
	if res.R != wire.ResultRefused {
		t.Fatalf("create: %+v", res)
	}
	// The created conversation's actor is inside the launcher now.
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	snap, err := rt.Conversations(context.Background())
	if took := time.Since(start); err != nil || took > 600*time.Millisecond || len(snap.Conversations) != 2 {
		t.Fatalf("list: %d conversations, %v, after %v", len(snap.Conversations), err, took)
	}
}

// A message for an actor that is stuck is not taken, and its sender is told: a hook and an attach get ErrBusy, a command
// is refused{timeout} with nothing left in flight, and the retry of its opId, once the actor is free, runs.
func TestAFullMailboxRefusesWhatItDoesNotTake(t *testing.T) {
	old := MailWait
	MailWait = 100 * time.Millisecond
	defer func() { MailWait = old }()
	dir := t.TempDir()
	pub := &fakePub{t: t, seqs: map[wire.Channel]int64{}}
	m := &slowMachine{pane: &lockedPane{screen: tui(t, "claude-idle-placeholder.ansi.txt")}}
	rt := New(Deps{MachineID: "m1", Publisher: pub, Machine: m, Store: NewStore(filepath.Join(dir, "v2")), Limits: wire.DefaultLimits})
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)
	path := filepath.Join(dir, "t.jsonl")
	writeFile(path, "")
	rt.Import([]ImportTab{{ID: "tab-a", Session: "s", Provider: wire.ProviderClaude, TranscriptPath: path}}, nil)
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	a := rt.actor("tab-a")
	stuck := make(chan struct{})
	if err := rt.on(a, func(*conv, time.Time) { close(stuck); <-release }); err != nil {
		t.Fatal(err)
	}
	// The actor has the blocker in hand before the box is filled: until then the blocker may still sit in the box, and
	// so may the done of a look the import started (it is posted from its own goroutine, whenever it ends).
	<-stuck
	for full := false; !full; {
		select {
		case a.mail <- func() {}:
		default:
			full = true
		}
	}

	start := time.Now()
	if err := rt.Hook("tab-a", Hook{Kind: "stop"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("the hook: %v", err)
	}
	if took := time.Since(start); took < 50*time.Millisecond || took > 3*time.Second {
		t.Fatalf("the hook waited %v for a full mailbox", took)
	}
	if err := rt.Attach("tab-a", "s", path, ""); !errors.Is(err, ErrBusy) {
		t.Fatalf("the attach: %v", err)
	}
	pin := wire.ConversationPinCmd{OpID: "pin-1", ID: "tab-a", Pinned: true}
	if res := rt.Do(context.Background(), "d", pin); res.R != wire.ResultRefused || res.Code != wire.RefuseTimeout {
		t.Fatalf("the command: %+v", res)
	}
	rt.opsMu.Lock()
	left, kept := len(rt.inflight), len(rt.ops)
	rt.opsMu.Unlock()
	if left != 0 || kept != 0 {
		t.Fatalf("after the refusal %d commands in flight, %d answers kept", left, kept)
	}

	free()
	if res := rt.Do(context.Background(), "d", pin); res.R != wire.ResultAccepted {
		t.Fatalf("the retry: %+v", res)
	}
}
