package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// keeperRig: the updater's state file, its request folder and store, and Claude copies that pass or fail as told.
type keeperRig struct {
	k       *keeper
	dir     string
	broken  map[string]bool // version → the scenario fails on it
	tried   []string
	alerted []string
}

func newKeeperRig(t *testing.T, installed []string, state map[string]any) *keeperRig {
	t.Helper()
	dir := t.TempDir()
	r := &keeperRig{dir: dir, broken: map[string]bool{}}
	for _, sub := range []string{"store", "requests"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range installed {
		if err := os.WriteFile(filepath.Join(dir, "store", "claude-"+v), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	r.k = &keeper{state: "pending", judged: map[string]bool{}, alerted: map[string]time.Time{},
		statePath: filepath.Join(dir, "state.json"), requests: filepath.Join(dir, "requests"), store: filepath.Join(dir, "store")}
	r.k.try = func(binary string) []machineCheck {
		v := strings.TrimPrefix(filepath.Base(binary), "claude-")
		r.tried = append(r.tried, v)
		if r.broken[v] {
			return []machineCheck{{ID: "agent_question", Detail: "вопрос не распознан"}}
		}
		return []machineCheck{{ID: "agent_question", OK: true}, {ID: "agent_answer", OK: true}}
	}
	r.k.alarm = func(key, _ string) { r.alerted = append(r.alerted, key) }
	return r
}

func (r *keeperRig) verdict(t *testing.T, version string) map[string]any {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(r.dir, "requests", "verdict-"+version+"-*.json"))
	if len(matches) != 1 {
		return nil
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestKeeperJudgesACandidateOnce(t *testing.T) {
	r := newKeeperRig(t, []string{"2.1.318", "2.1.319"}, map[string]any{
		"installed": "2.1.318", "candidate": map[string]any{"version": "2.1.319", "path": "/x/claude-2.1.319"}})
	r.broken["2.1.319"] = true
	r.k.claudeTick()
	v := r.verdict(t, "2.1.319")
	if v == nil || v["ok"] != false || !strings.Contains(v["detail"].(string), "не распознан") {
		t.Fatalf("verdict %v", v)
	}
	if len(r.alerted) != 1 || r.alerted[0] != "candidate-2.1.319" {
		t.Fatalf("alerts %v", r.alerted)
	}
	r.tried = nil
	r.k.claudeTick()
	for _, v := range r.tried {
		if v == "2.1.319" {
			t.Fatal("a judged candidate was tried again")
		}
	}
}

func TestKeeperPassesAGoodCandidate(t *testing.T) {
	r := newKeeperRig(t, []string{"2.1.318", "2.1.319"}, map[string]any{
		"installed": "2.1.318", "candidate": map[string]any{"version": "2.1.319", "path": "/x/claude-2.1.319"}})
	r.k.claudeTick()
	if v := r.verdict(t, "2.1.319"); v == nil || v["ok"] != true {
		t.Fatalf("verdict %v", v)
	}
	if len(r.alerted) != 0 {
		t.Fatalf("alerts %v", r.alerted)
	}
}

// The current Claude breaks and the previous one works: the Claude's fault, a rollback to the previous one.
func TestKeeperRollsBackABrokenCurrentClaude(t *testing.T) {
	r := newKeeperRig(t, []string{"2.1.316", "2.1.317", "2.1.318"}, map[string]any{"installed": "2.1.318"})
	r.broken["2.1.318"] = true
	r.k.claudeTick()
	v := r.verdict(t, "2.1.318")
	if v == nil || v["ok"] != false || v["rollback"] != "2.1.317" {
		t.Fatalf("verdict %v", v)
	}
	if len(r.alerted) != 1 || r.alerted[0] != "rollback-2.1.318" {
		t.Fatalf("alerts %v", r.alerted)
	}
}

// Both fail: the machine's fault, no rollback, the person is told.
func TestKeeperDoesNotBlameClaudeForTheMachine(t *testing.T) {
	r := newKeeperRig(t, []string{"2.1.317", "2.1.318"}, map[string]any{"installed": "2.1.318"})
	r.broken["2.1.318"], r.broken["2.1.317"] = true, true
	r.k.claudeTick()
	if v := r.verdict(t, "2.1.318"); v != nil {
		t.Fatalf("a verdict was written: %v", v)
	}
	if len(r.alerted) != 1 || r.alerted[0] != "agent-2.1.318" {
		t.Fatalf("alerts %v", r.alerted)
	}
}

// A blocked previous version is skipped; the current one is tried again only after keeperAgentEvery.
func TestKeeperSkipsBlockedAndWaits(t *testing.T) {
	r := newKeeperRig(t, []string{"2.1.316", "2.1.317", "2.1.318"}, map[string]any{"installed": "2.1.318", "blocked": []string{"2.1.317"}})
	r.broken["2.1.318"] = true
	r.k.claudeTick()
	if v := r.verdict(t, "2.1.318"); v == nil || v["rollback"] != "2.1.316" {
		t.Fatalf("verdict %v", v)
	}
	r.tried = nil
	r.k.claudeTick()
	if len(r.tried) != 0 {
		t.Fatalf("tried again at once: %v", r.tried)
	}
}

func TestVersionOrder(t *testing.T) {
	if !versionLess("2.1.99", "2.1.318") || versionLess("2.1.318", "2.1.318") || !isVersion("2.1.318") || isVersion("2.1") {
		t.Fatal("version order")
	}
}

// one failed run of the terminal's self-test is tried again before the state the rollout reads says fail;
// a machine that fails twice is failing.
func TestKeeperTriesTheTerminalAgainBeforeFailing(t *testing.T) {
	bad := []machineCheck{{ID: "pane_program", Detail: "вместо sleep видно «bash»"}}
	good := []machineCheck{{ID: "pane_shell", OK: true}, {ID: "pane_program", OK: true}, {ID: "pane_keys", OK: true}}
	for name, tc := range map[string]struct {
		runs  [][]machineCheck
		state string
		tries int
	}{
		"passes":          {[][]machineCheck{good}, "pass", 1},
		"a loaded moment": {[][]machineCheck{bad, good}, "pass", 2},
		"fails twice":     {[][]machineCheck{bad, bad}, "fail", 2},
	} {
		t.Run(name, func(t *testing.T) {
			k := &keeper{state: "pending", judged: map[string]bool{}, alerted: map[string]time.Time{}}
			var slept []time.Duration
			tries := 0
			k.runTerminal = func() []machineCheck { tries++; return tc.runs[tries-1] }
			k.sleep = func(d time.Duration) { slept = append(slept, d) }
			k.alarm = func(string, string) {}
			k.terminal()
			if k.Selftest() != tc.state || tries != tc.tries {
				t.Fatalf("state %s after %d runs, want %s after %d", k.Selftest(), tries, tc.state, tc.tries)
			}
			if tc.tries == 2 && (len(slept) != 1 || slept[0] != keeperTerminalRetry) {
				t.Fatalf("slept %v before trying again", slept)
			}
		})
	}
}
