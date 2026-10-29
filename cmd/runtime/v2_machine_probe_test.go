package main

import (
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

type probeFake struct {
	mu       sync.Mutex
	panes    map[string]tmux.PaneInfo
	ok       bool
	reads    int
	has      map[string]bool
	hasAsked []string
}

func (f *probeFake) ReadAllPanes() (map[string]tmux.PaneInfo, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	return f.panes, f.ok
}

func (f *probeFake) HasSession(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hasAsked = append(f.hasAsked, name)
	return f.has[name]
}

// Many conversations look at once: one list-panes answers them all, and a conversation whose session is gone stops
// costing a tmux call once its absence is confirmed.
func TestProbeSharesOneSnapshot(t *testing.T) {
	f := &probeFake{ok: true, panes: map[string]tmux.PaneInfo{"tk-a": {Command: "claude"}, "tk-b": {Command: "bash"}}}
	m := &v2Tmux{probe: f}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); m.Probe("tk-a") }()
	}
	wg.Wait()
	if f.reads != 1 {
		t.Fatalf("%d list-panes for 50 looks", f.reads)
	}
	if got := m.Probe("tk-a"); got != (convo.Probe{Session: true, Foreground: true}) {
		t.Fatalf("agent %+v", got)
	}
	if got := m.Probe("tk-b"); got != (convo.Probe{Session: true, Foreground: true}) {
		t.Fatalf("a shell seen once %+v", got)
	}
	m.expire()
	if got := m.Probe("tk-b"); got != (convo.Probe{Session: true, Shell: true}) {
		t.Fatalf("shell %+v", got)
	}
	// Gone: asked once on its own (it may be a pane between programs), then believed from the snapshot.
	m.Probe("tk-gone")
	m.Probe("tk-gone")
	if got := m.Probe("tk-gone"); got != (convo.Probe{}) || len(f.hasAsked) != 1 {
		t.Fatalf("gone %+v, has-session %v", got, f.hasAsked)
	}
}

// A tmux that did not answer says nothing: the agent is taken as still running.
func TestProbeOnASilentTmux(t *testing.T) {
	f := &probeFake{ok: false}
	m := &v2Tmux{probe: f}
	m.Probe("tk-a")
	if got := m.Probe("tk-a"); got != (convo.Probe{Session: true, Foreground: true}) || len(f.hasAsked) != 0 {
		t.Fatalf("%+v %v", got, f.hasAsked)
	}
}

// expire makes the next look read tmux again.
func (m *v2Tmux) expire() {
	m.snapMu.Lock()
	m.snap.at = time.Time{}
	m.snapMu.Unlock()
}

// A shell is believed on the second snapshot that shows it, not on the second look of one: a pane that ran its shell
// when the snapshot was taken, with the agent up and its first hook in before the next snapshot, is not an agent that
// exited.
func TestProbeShellNeedsTwoSnapshots(t *testing.T) {
	f := &probeFake{ok: true, panes: map[string]tmux.PaneInfo{"tk-a": {Command: "bash"}}}
	m := &v2Tmux{probe: f}
	for i := 0; i < 5; i++ {
		if got := m.Probe("tk-a"); got != (convo.Probe{Session: true, Foreground: true}) {
			t.Fatalf("look %d of one snapshot: %+v", i, got)
		}
	}
	f.mu.Lock()
	f.panes = map[string]tmux.PaneInfo{"tk-a": {Command: "claude"}}
	f.mu.Unlock()
	m.expire()
	if got := m.Probe("tk-a"); got != (convo.Probe{Session: true, Foreground: true}) {
		t.Fatalf("agent up: %+v", got)
	}
	if f.reads != 2 {
		t.Fatalf("%d reads", f.reads)
	}
}
