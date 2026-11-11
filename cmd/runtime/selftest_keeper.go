package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/cliversion"
)

// The keeper of the self-test: when the tests run, what the machine is told, and the Claude Code gate.
//
// The terminal's self-test runs a while after the start and every 30 minutes; its state is in /api/health for the
// rollout, which takes a release back when it fails (host-activate.sh).
//
// The Claude gate talks to the deploy's Claude updater (claude-update.py, root) through two places only:
//   - its state file (world-readable) names a `candidate` {version, path}: a new Claude it downloaded and has not
//     switched new sessions to yet. The keeper runs the agent's scenario on that binary and writes a verdict.
//   - the request folder (write-only for the accounts, sticky) takes the verdicts: verdict-<version>-<uid>.json
//     {version, ok, detail, at, rollback}. The updater switches only to a version with a passing verdict, and back to
//     a previous one when the current one fails and the previous one passes (rollback names it).
//
// The current Claude is tried when it changes and every 6 hours; a failure tries the previous installed copy, and only
// a passing previous one makes it the Claude's fault (a rollback). Otherwise it is the machine's, and the person is told.

const (
	keeperTerminalEvery = 30 * time.Minute
	keeperTerminalRetry = 30 * time.Second
	keeperAgentEvery    = 6 * time.Hour
	keeperClaudePoll    = time.Minute
	keeperAlertQuiet    = 6 * time.Hour
)

type keeper struct {
	d   deps
	run sync.Mutex // one test at a time: they share the machine with people
	// agentRun is held by the agent's scenario from before it waits for run: a second one does not start over it.
	agentRun    sync.Mutex
	cancelAgent context.CancelFunc // cuts the running scenario short (the runtime stops)
	mu          sync.Mutex
	state       string // pending | pass | fail
	checks      []machineCheck
	judged      map[string]bool // Claude versions this runtime gave a verdict on
	current     string          // the current Claude's version as last tried
	lastAt      time.Time       // the current Claude's last try
	alerted     map[string]time.Time
	// updater's places; empty: no managed Claude on this machine, no gate
	statePath, requests string
	store               string // the folder of the installed copies, claude-<version>
	// try runs the agent's scenario on a Claude binary (agentOn); a test brings its own.
	try   func(binary string) []machineCheck
	alarm func(key, text string) // alert; a test brings its own
	// runTerminal runs the terminal's self-test once, sleep waits before it is tried again; nil: the real ones.
	runTerminal func() []machineCheck
	sleep       func(time.Duration)
	// readScreens reads the reference screens and returns the misread ones; nil: the typist's.
	readScreens func() []string
}

var selfKeeper = &keeper{state: "pending", judged: map[string]bool{}, alerted: map[string]time.Time{}}

// Selftest is the terminal self-test's state for /api/health.
func (k *keeper) Selftest() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.state
}

func (k *keeper) setTerminal(checks []machineCheck) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.checks = checks
	k.state = "pass"
	if selftestFailed(checks) != "" {
		k.state = "fail"
	}
}

// alert tells the machine's person once per key in keeperAlertQuiet; the log has it always.
func (k *keeper) alert(key, text string) {
	agent.Log("error", "selftest alert", agent.F{K: "key", V: key}, agent.F{K: "text", V: text})
	k.mu.Lock()
	if at, ok := k.alerted[key]; ok && time.Since(at) < keeperAlertQuiet {
		k.mu.Unlock()
		return
	}
	k.alerted[key] = time.Now()
	k.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	host, _ := os.Hostname()
	if _, err := notifySharedFor(k.d.DataDir).Send(ctx, "⚠️ "+host+": "+text, false); err != nil {
		agent.Log("warn", "selftest alert not sent", agent.F{K: "err", V: err.Error()})
	}
}

// terminalOnce runs the terminal's self-test once.
func (k *keeper) terminalOnce() []machineCheck {
	if k.runTerminal != nil {
		return k.screens(k.runTerminal())
	}
	k.run.Lock()
	defer k.run.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return k.screens(newSelftest(k.d.Tmux).run(ctx))
}

// screens adds the screen's part of the self-test to the terminal's.
func (k *keeper) screens(checks []machineCheck) []machineCheck {
	if k.readScreens != nil {
		return append(checks, screenChecks(k.readScreens(), cliversion.Seen())...)
	}
	return withScreenChecks(checks)
}

func (k *keeper) terminal() {
	checks := k.terminalOnce()
	if bad := selftestFailed(checks); bad != "" {
		// A failed run is tried again after a pause before the state says fail: the rollout takes a release back on
		// it, and the first run after a start shares the machine with every tab coming back.
		agent.Log("warn", "selftest failed once; trying again", agent.F{K: "failed", V: bad})
		sleep := k.sleep
		if sleep == nil {
			sleep = time.Sleep
		}
		sleep(keeperTerminalRetry)
		checks = k.terminalOnce()
	}
	prev := k.Selftest()
	k.setTerminal(checks)
	if bad := selftestFailed(checks); bad != "" {
		// One failure may be a loaded moment; the person hears of the second in a row.
		agent.Log("error", "selftest failed", agent.F{K: "failed", V: bad})
		if prev == "fail" {
			k.alert("terminal", "самопроверка терминала не проходит два раза подряд — "+bad)
		}
		return
	}
	agent.Log("info", "selftest passed")
}

// agentOn runs the agent's scenario on one Claude binary.
func (k *keeper) agentOn(binary string) []machineCheck {
	a, err := newAgentSelftest(k.d.Tmux, k.d.DataDir, binary)
	if err != nil {
		return []machineCheck{{ID: "agent_question", Detail: err.Error()}}
	}
	checks, _ := k.agentOnce(context.Background(), true, a.run)
	return checks
}

// updaterState is what the keeper reads of claude-update.py's state file.
type updaterState struct {
	Installed string `json:"installed"`
	Current   string `json:"current"`
	Candidate *struct {
		Version string `json:"version"`
		Path    string `json:"path"`
	} `json:"candidate"`
	Blocked []string `json:"blocked"`
}

func (k *keeper) readUpdater() (updaterState, bool) {
	var st updaterState
	raw, err := os.ReadFile(k.statePath)
	if err != nil || json.Unmarshal(raw, &st) != nil {
		return st, false
	}
	return st, true
}

func (k *keeper) verdict(version string, checks []machineCheck, rollback string) {
	bad := selftestFailed(checks)
	v := map[string]any{"version": version, "ok": bad == "", "detail": bad, "at": time.Now().UnixMilli()}
	if rollback != "" {
		v["rollback"] = rollback
	}
	raw, _ := json.Marshal(v)
	name := filepath.Join(k.requests, "verdict-"+version+"-"+strconv.Itoa(os.Getuid())+".json")
	// The folder is write-only and sticky: a new file each time (a verdict of ours may be there from before).
	_ = os.Remove(name)
	if err := os.WriteFile(name, raw, 0o644); err != nil {
		agent.Log("warn", "selftest verdict not written", agent.F{K: "err", V: err.Error()})
	}
	k.mu.Lock()
	k.judged[version] = true
	k.mu.Unlock()
}

// installed is the copies of Claude in the store, newest first, as version → path.
func (k *keeper) installed() []string {
	entries, err := os.ReadDir(k.store)
	if err != nil {
		return nil
	}
	var vs []string
	for _, e := range entries {
		if v, ok := strings.CutPrefix(e.Name(), "claude-"); ok && isVersion(v) {
			vs = append(vs, v)
		}
	}
	sort.Slice(vs, func(i, j int) bool { return versionLess(vs[j], vs[i]) })
	return vs
}

func (k *keeper) claudeTick() {
	st, ok := k.readUpdater()
	if !ok {
		return
	}
	// A candidate: tried once per version.
	if c := st.Candidate; c != nil && isVersion(c.Version) && filepath.IsAbs(c.Path) {
		k.mu.Lock()
		done := k.judged[c.Version]
		k.mu.Unlock()
		if !done {
			checks := k.try(c.Path)
			k.verdict(c.Version, checks, "")
			if bad := selftestFailed(checks); bad != "" {
				k.alarm("candidate-"+c.Version, fmt.Sprintf("Claude %s не прошёл проверку, новые сессии остаются на %s — %s", c.Version, st.Installed, bad))
			} else {
				agent.Log("info", "selftest passed a Claude candidate", agent.F{K: "version", V: c.Version})
			}
		}
	}
	// The current one: when it changed, and every keeperAgentEvery.
	cur := st.Installed
	if !isVersion(cur) {
		return
	}
	k.mu.Lock()
	due := cur != k.current || time.Since(k.lastAt) >= keeperAgentEvery
	k.mu.Unlock()
	if !due {
		return
	}
	checks := k.try(filepath.Join(k.store, "claude-"+cur))
	k.mu.Lock()
	k.current, k.lastAt = cur, time.Now()
	k.mu.Unlock()
	bad := selftestFailed(checks)
	if bad == "" {
		return
	}
	// Whose fault: the previous installed copy that is not blocked decides.
	for _, prev := range k.installed() {
		if !versionLess(prev, cur) || contains(st.Blocked, prev) {
			continue
		}
		if selftestFailed(k.try(filepath.Join(k.store, "claude-"+prev))) == "" {
			k.verdict(cur, checks, prev)
			k.alarm("rollback-"+cur, fmt.Sprintf("Claude %s сломал основной сценарий (%s): новые сессии возвращаются на %s", cur, bad, prev))
			return
		}
		break
	}
	k.alarm("agent-"+cur, "сценарий агента не проходит и на прошлой версии Claude — дело не в версии: "+bad)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func isVersion(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil || p == "" {
			return false
		}
	}
	return true
}

// versionLess compares x.y.z numerically.
func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3 && i < len(pa) && i < len(pb); i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			return x < y
		}
	}
	return false
}

// startKeeper starts the self-test's loops; the Claude gate only where claude-update.py keeps its state.
func startKeeper(d deps) {
	watchCLIVersions(d)
	if d.Tmux == nil {
		return
	}
	k := selfKeeper
	k.d = d
	k.try, k.alarm = k.agentOn, k.alert
	k.statePath = env("CLAUDE_UPDATE_STATE", "/var/lib/harnessly/claude-update/state.json")
	k.requests = env("CLAUDE_UPDATE_REQUESTS", "/run/harnessly-claude-update")
	k.store = env("CLAUDE_UPDATE_STORE", "/opt/harnessly-profile/bin")
	// Before any test of this run: what a runtime stopped in the middle of one left behind.
	k.sweep("start")
	go func() {
		time.Sleep(selftestAfterBoot)
		for {
			k.terminal()
			time.Sleep(keeperTerminalEvery)
		}
	}()
	if _, err := os.Stat(k.statePath); err != nil {
		return
	}
	go func() {
		time.Sleep(selftestAfterBoot + time.Minute)
		for {
			k.claudeTick()
			time.Sleep(keeperClaudePoll)
		}
	}()
}
