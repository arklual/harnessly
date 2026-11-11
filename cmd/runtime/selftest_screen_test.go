package main

import (
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/cliversion"
	"github.com/ly-solutions/harnessly/internal/typist"
)

func terminalPasses() []machineCheck {
	return []machineCheck{{ID: "pane_shell", OK: true}, {ID: "pane_program", OK: true}, {ID: "pane_keys", OK: true}}
}

func screenKeeper(misread []string) *keeper {
	k := &keeper{state: "pending", judged: map[string]bool{}, alerted: map[string]time.Time{}}
	k.runTerminal = terminalPasses
	k.sleep = func(time.Duration) {}
	k.alarm = func(string, string) {}
	k.readScreens = func() []string { return misread }
	return k
}

func checkOf(checks []machineCheck, id string) machineCheck {
	for _, c := range checks {
		if c.ID == id {
			return c
		}
	}
	return machineCheck{}
}

// The state the rollout reads in /api/health (keeper.Selftest) fails when the release no longer reads its screens.
func TestSelftestFailsWhenTheReferenceScreensAreMisread(t *testing.T) {
	k := screenKeeper([]string{"2.1.322 question: menu \"\", not \"question\""})
	k.terminal()
	if k.Selftest() != "fail" {
		t.Fatalf("selftest %s, want fail", k.Selftest())
	}
	if c := checkOf(k.checks, "screen_reading"); c.OK || !strings.Contains(c.Detail, "2.1.322 question") {
		t.Fatalf("screen_reading %+v", c)
	}
}

// An unchecked version of the CLI is said, and does not fail the self-test: the vendor's update must not stop rollouts.
func TestSelftestOnlyWarnsOfAnUncheckedCLIVersion(t *testing.T) {
	cliversion.Reset()
	t.Cleanup(cliversion.Reset)
	cliversion.Note(cliversion.Claude, "99.0.0")
	k := screenKeeper(nil)
	k.terminal()
	if k.Selftest() != "pass" {
		t.Fatalf("selftest %s, want pass", k.Selftest())
	}
	c := checkOf(k.checks, "cli_version")
	if !c.OK || !strings.Contains(c.Detail, "claude 99.0.0 — не проверена") {
		t.Fatalf("cli_version %+v", c)
	}
	cliversion.Note(cliversion.Claude, cliversion.Table()[0].To)
	k.terminal()
	if c := checkOf(k.checks, "cli_version"); !c.OK || !strings.Contains(c.Detail, "— проверена") {
		t.Fatalf("cli_version of a checked one %+v", c)
	}
}

// The self-test of this build reads the screens it carries.
func TestSelftestReadsTheCarriedScreens(t *testing.T) {
	cliversion.Reset()
	t.Cleanup(cliversion.Reset)
	checks := withScreenChecks(terminalPasses())
	if bad := selftestFailed(checks); bad != "" {
		t.Fatalf("failed: %s", bad)
	}
	if len(typist.Goldens()) == 0 || len(checks) != 5 {
		t.Fatalf("%d reference screens, %d checks", len(typist.Goldens()), len(checks))
	}
}
