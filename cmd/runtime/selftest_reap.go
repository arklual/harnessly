package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// What keeps an agent of the self-test from outliving it.
//
// The scenario starts a real Claude in a pane of its own. The pane is closed when the scenario ends, but a runtime
// that is stopped or restarted in the middle never gets there, and a Claude that does not die of its terminal's
// hangup stays for good. So:
//   - the scenario has one term for the whole of it (agentSelftestLimit), whoever asked for it;
//   - one scenario runs at a time per runtime;
//   - the agent is started with a mark in its environment (selftestMarkVar, the runtime's data folder as its value),
//     and every process of this account that carries the mark is ended when the scenario ends, when the runtime
//     stops, and when it starts — with the self-test's panes left by a previous run.
//
// Nothing else carries the mark: a person's chat is never one of these.

const (
	selftestMarkVar    = "HARNESSLY_AGENT_SELFTEST"
	agentSelftestLimit = 4 * time.Minute
	reapGrace          = 2 * time.Second
)

// selftestMark is the mark's value for this runtime: two runtimes of one account do not end each other's agents.
func selftestMark(dataDir string) string {
	if abs, err := filepath.Abs(dataDir); err == nil {
		return abs
	}
	return dataDir
}

// markedPIDs are the processes of uid under procRoot (/proc) whose environment has the mark with this value.
func markedPIDs(procRoot, mark string, uid int) []int {
	if mark == "" {
		return nil
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil
	}
	want := []byte(selftestMarkVar + "=" + mark)
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 || pid == os.Getpid() {
			continue
		}
		dir := filepath.Join(procRoot, e.Name())
		if st, err := os.Stat(dir); err != nil || !ownedBy(st, uid) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, "environ"))
		if err != nil {
			continue
		}
		for _, kv := range bytes.Split(raw, []byte{0}) {
			if bytes.Equal(kv, want) {
				pids = append(pids, pid)
				break
			}
		}
	}
	return pids
}

func ownedBy(st os.FileInfo, uid int) bool {
	sys, ok := st.Sys().(*syscall.Stat_t)
	return ok && int(sys.Uid) == uid
}

// reapMarked ends the marked processes: asked first, killed after reapGrace. It returns how many it found.
func reapMarked(mark string) int {
	pids := markedPIDs("/proc", mark, os.Getuid())
	if len(pids) == 0 {
		return 0
	}
	for _, pid := range pids {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	until := time.Now().Add(reapGrace)
	for time.Now().Before(until) && len(markedPIDs("/proc", mark, os.Getuid())) > 0 {
		time.Sleep(100 * time.Millisecond)
	}
	for _, pid := range markedPIDs("/proc", mark, os.Getuid()) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	return len(pids)
}

// selftestSessions are the self-test's panes among names: the runtime's prefix, then "selftest-".
func selftestSessions(names []string, prefix string) []string {
	var out []string
	for _, n := range names {
		if strings.HasPrefix(n, prefix+"selftest-") {
			out = append(out, n)
		}
	}
	return out
}

// sweep ends what a self-test left: its panes and its marked agents. At the start nothing of this runtime's can be
// running yet; at the stop whatever runs is cut short.
func (k *keeper) sweep(when string) {
	if k.d.Tmux == nil {
		return
	}
	left := selftestSessions(k.d.Tmux.ListSessions(), k.d.Tmux.Names().Prefix)
	for _, name := range left {
		k.d.Tmux.KillSession(name)
	}
	if n := reapMarked(selftestMark(k.d.DataDir)); n > 0 || len(left) > 0 {
		agent.Log("warn", "selftest leftovers ended", agent.F{K: "when", V: when}, agent.F{K: "panes", V: len(left)},
			agent.F{K: "processes", V: n})
	}
}

// stop is the runtime going down: a running scenario is cut short and its agent ended.
func (k *keeper) stop() {
	k.mu.Lock()
	cancel := k.cancelAgent
	k.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	k.sweep("stop")
}

// agentOnce runs the agent's scenario unless one is running: wait says whether to wait for that one to end (the
// keeper's own tries, which must not turn into a verdict unrun) or to give up (started false: a person's request).
func (k *keeper) agentOnce(ctx context.Context, wait bool, run func(context.Context) []machineCheck) (checks []machineCheck, started bool) {
	if wait {
		k.agentRun.Lock()
	} else if !k.agentRun.TryLock() {
		return nil, false
	}
	defer k.agentRun.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	k.mu.Lock()
	k.cancelAgent = cancel
	k.mu.Unlock()
	defer func() {
		k.mu.Lock()
		k.cancelAgent = nil
		k.mu.Unlock()
	}()
	k.run.Lock()
	defer k.run.Unlock()
	return run(ctx), true
}
