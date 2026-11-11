package convo

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/safego"
	"github.com/ly-solutions/harnessly/internal/wire"
)

type warnLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *warnLog) Info(string, ...any) {}
func (l *warnLog) Warn(msg string, kv ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, msg+" "+fmt.Sprint(kv...))
}
func (l *warnLog) has(parts ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		all := true
		for _, p := range parts {
			all = all && strings.Contains(line, p)
		}
		if all {
			return true
		}
	}
	return false
}

// A panic inside one conversation's actor, or inside work it spawned, ends neither the process nor the other
// conversations: it is logged with its stack and the conversation's id, the conversation goes unknown, and its actor
// runs again after a pause.
func TestAPanicInOneActorLeavesTheRuntimeRunning(t *testing.T) {
	safego.SetLog(func(string, safego.Panic) {})
	defer safego.SetLog(nil)
	old := ActorRestartAfter
	ActorRestartAfter = 30 * time.Millisecond
	defer func() { ActorRestartAfter = old }()

	dir := t.TempDir()
	pub := &fakePub{t: t, seqs: map[wire.Channel]int64{}}
	m := &slowMachine{pane: &lockedPane{screen: tui(t, "claude-idle-placeholder.ansi.txt")}}
	log := &warnLog{}
	rt := New(Deps{MachineID: "m1", Publisher: pub, Machine: m, Store: NewStore(filepath.Join(dir, "v2")), Log: log})
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer rt.Stop()
	path := filepath.Join(dir, "t.jsonl")
	if err := writeFile(path, ""); err != nil {
		t.Fatal(err)
	}
	rt.Import([]ImportTab{{ID: "tab-a", Session: "s", Provider: wire.ProviderClaude, TranscriptPath: path},
		{ID: "tab-b", Session: "s2", Provider: wire.ProviderClaude, TranscriptPath: path}}, nil)
	before := safego.Panics()

	// Work off the actor panics: its end is still told on the actor.
	told := make(chan any, 1)
	if err := rt.call(context.Background(), "tab-a", func(c *conv, _ time.Time) {
		c.spawn(func() any { panic("odd transcript line") }, func(r any) { told <- r })
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-told:
		if r != nil {
			t.Fatalf("panicked work handed over %v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the end of panicked work was never told")
	}
	if !log.has("conversation work panicked", "tab-a", "panic_test.go") {
		t.Fatalf("no log line with the stack and the conversation: %q", log.lines)
	}

	// The actor itself panics.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	_ = rt.call(ctx, "tab-a", func(*conv, time.Time) { panic("odd screen") })
	cancel()
	if !log.has("conversation actor panicked", "tab-a", "odd screen", "panic_test.go") {
		t.Fatalf("no log line with the stack and the conversation: %q", log.lines)
	}
	if got := safego.Panics() - before; got != 2 {
		t.Fatalf("held panics = %d, want 2", got)
	}
	if rec, ok := rt.Record("tab-a"); !ok || rec.State.K != wire.StateUnknown {
		t.Fatalf("the panicked conversation shows %+v, want unknown", rec.State)
	}
	// The other conversation answers meanwhile.
	if _, err := rt.Conversation(context.Background(), "tab-b"); err != nil {
		t.Fatalf("the other conversation: %v", err)
	}
	// The panicked one answers again once its actor is back, and is looked at again.
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := rt.Conversation(ctx, "tab-a"); err != nil {
		t.Fatalf("the panicked conversation did not come back: %v", err)
	}
}
