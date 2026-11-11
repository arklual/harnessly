package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// styledPanes is the terminal's fake with the styled capture the agent's scenario reads.
type styledPanes struct{ *fakePanes }

func (s styledPanes) CaptureStyled(string) (string, bool) { return "", true }

// An agent that starts and never asks its question: the scenario ends at its term, closes its pane and ends the agent.
func TestTheAgentScenarioEndsAtItsTerm(t *testing.T) {
	panes := &fakePanes{}
	reaped := 0
	a := agentSelftest{selftest: selftest{panes: panes, sleep: func(time.Duration) { time.Sleep(time.Millisecond) }, now: time.Now},
		panes: styledPanes{panes}, Binary: "claude", Dir: t.TempDir(), Mark: "mark", Limit: 150 * time.Millisecond,
		reap: func() { reaped++ }}
	// The agent takes the foreground as soon as its script is typed, and stays there.
	go func() {
		for i := 0; i < 2000; i++ {
			panes.mu.Lock()
			if panes.typed != "" || len(panes.queued) > 0 {
				panes.fg = "claude"
			}
			panes.mu.Unlock()
			time.Sleep(time.Millisecond)
		}
	}()
	done := make(chan []machineCheck, 1)
	started := time.Now()
	go func() { done <- a.run(context.Background()) }()
	select {
	case checks := <-done:
		if selftestFailed(checks) == "" {
			t.Fatalf("a scenario cut by its term passed: %+v", checks)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the scenario outlived its term")
	}
	if took := time.Since(started); took > 10*time.Second {
		t.Fatalf("the scenario took %s with a term of 150ms", took)
	}
	panes.mu.Lock()
	killed := panes.killed
	panes.mu.Unlock()
	if !killed || reaped != 1 {
		t.Fatalf("pane closed: %v, agents ended: %d times", killed, reaped)
	}
}

func TestASecondAgentScenarioDoesNotStartOverARunningOne(t *testing.T) {
	k := &keeper{state: "pending", judged: map[string]bool{}, alerted: map[string]time.Time{}}
	running, release := make(chan struct{}), make(chan struct{})
	first := make(chan bool, 1)
	go func() {
		_, started := k.agentOnce(context.Background(), false, func(context.Context) []machineCheck {
			close(running)
			<-release
			return nil
		})
		first <- started
	}()
	<-running
	ran := false
	if _, started := k.agentOnce(context.Background(), false, func(context.Context) []machineCheck { ran = true; return nil }); started || ran {
		t.Fatal("a second scenario started while one was running")
	}
	close(release)
	if !<-first {
		t.Fatal("the first scenario did not run")
	}
	// The line is free again, and a keeper's own try waits instead of giving up.
	if _, started := k.agentOnce(context.Background(), true, func(context.Context) []machineCheck { return nil }); !started {
		t.Fatal("the scenario after the first one did not start")
	}
}

func TestTheRuntimeStoppingCutsTheScenarioShort(t *testing.T) {
	k := &keeper{state: "pending", judged: map[string]bool{}, alerted: map[string]time.Time{}}
	running := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		k.agentOnce(context.Background(), true, func(ctx context.Context) []machineCheck {
			close(running)
			<-ctx.Done()
			done <- ctx.Err()
			return nil
		})
	}()
	<-running
	k.stop() // no tmux in this keeper: only the running scenario is cut
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the scenario's context was not cancelled")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the scenario went on after the stop")
	}
}

func TestOnlyMarkedProcessesOfThisRuntimeAreFound(t *testing.T) {
	proc := t.TempDir()
	put := func(pid int, env ...string) {
		dir := filepath.Join(proc, strconv.Itoa(pid))
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "environ"), []byte(strings.Join(env, "\x00")+"\x00"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	put(4001, "HOME=/home/a", selftestMarkVar+"=/data/one")         // ours
	put(4002, "HOME=/home/a", selftestMarkVar+"=/data/two")         // another runtime's self-test
	put(4003, "HOME=/home/a", "CLAUDE_CODE_ENTRYPOINT=cli")         // a person's chat
	put(4004, "NOTE="+selftestMarkVar+"=/data/one", "HOME=/home/a") // the mark's text inside another variable
	put(4005, selftestMarkVar+"=/data/one/deeper")                  // a longer value
	if err := os.Mkdir(filepath.Join(proc, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := markedPIDs(proc, "/data/one", os.Getuid())
	if len(got) != 1 || got[0] != 4001 {
		t.Fatalf("marked %v, want [4001]", got)
	}
	if got := markedPIDs(proc, "/data/one", os.Getuid()+1); len(got) != 0 {
		t.Fatalf("another account's processes were found: %v", got)
	}
	if got := markedPIDs(proc, "", os.Getuid()); len(got) != 0 {
		t.Fatalf("an empty mark matched %v", got)
	}
}

func TestTheAgentIsStartedWithItsMark(t *testing.T) {
	a := agentSelftest{Binary: "/opt/claude", Mark: "/data/one"}
	if !strings.Contains(a.script(), "export "+selftestMarkVar+"='/data/one'\n") {
		t.Fatalf("the script does not set the mark:\n%s", a.script())
	}
	if got := selftestSessions([]string{"hs-selftest-ab12", "hs-chat-1", "other-selftest-1", "hs-selftestx"}, "hs-"); len(got) != 1 || got[0] != "hs-selftest-ab12" {
		t.Fatalf("self-test panes: %v", got)
	}
}
