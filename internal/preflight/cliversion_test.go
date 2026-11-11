package preflight

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/cliversion"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// machineWith is a checker whose login PATH holds tmux, git and a claude that counts its starts (the sign-in probe
// is not one) and prints version.
func machineWith(t *testing.T, version string) (c *Checker, bin, starts string) {
	t.Helper()
	cliversion.Reset()
	t.Cleanup(cliversion.Reset)
	home := t.TempDir()
	bin = t.TempDir()
	shell := filepath.Join(t.TempDir(), "loginsh")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf '%s' \""+bin+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	starts = filepath.Join(t.TempDir(), "starts")
	script(t, bin, "tmux", `echo "tmux 3.4"`)
	script(t, bin, "git", `echo "git version 2.43.0"`)
	script(t, bin, "node", `echo "v22.1.0"`)
	script(t, bin, "claude", `[ "$1" = auth ] && exit 0; echo x >> "`+starts+`"; echo "`+version+` (Claude Code)"`)
	return &Checker{Home: home, Env: []string{"PATH=/nowhere", "HOME=" + home}, Shell: shell}, bin, starts
}

func startsOf(t *testing.T, file string) int {
	t.Helper()
	raw, _ := os.ReadFile(file)
	return strings.Count(string(raw), "x")
}

func TestTheCLIIsStartedOncePerBinary(t *testing.T) {
	c, bin, starts := machineWith(t, "99.0.0")
	for i := 0; i < 4; i++ { // four launches, each past the half-minute answer
		c.Invalidate()
		if r := c.Runtime(); !r.Claude.Installed || r.Claude.Version == nil || *r.Claude.Version != "99.0.0" {
			t.Fatalf("claude %+v", r.Claude)
		}
	}
	if n := startsOf(t, starts); n != 1 {
		t.Fatalf("claude --version ran %d times for one binary, want 1", n)
	}
	// An update puts another file there: read again, once.
	script(t, bin, "claude", `[ "$1" = auth ] && exit 0; echo x >> "`+starts+`"; echo "99.0.1 (Claude Code) updated"`)
	later := time.Now().Add(time.Hour)
	_ = os.Chtimes(filepath.Join(bin, "claude"), later, later)
	for i := 0; i < 2; i++ {
		c.Invalidate()
		if r := c.Runtime(); *r.Claude.Version != "99.0.1" {
			t.Fatalf("after the update: %+v", r.Claude)
		}
	}
	if n := startsOf(t, starts); n != 2 {
		t.Fatalf("starts after an update: %d, want 2", n)
	}
}

func cliCheck(rep hubproto.PreflightReport) *hubproto.Check {
	for i := range rep.Checks {
		if rep.Checks[i].ID == "cli_version" {
			return &rep.Checks[i]
		}
	}
	return nil
}

func TestAnUncheckedVersionIsAWarningOfTheMachine(t *testing.T) {
	c, _, _ := machineWith(t, "99.0.0")
	var told []string
	cliversion.OnUnchecked(func(provider, version string) { told = append(told, provider+" "+version) })
	opts := ReportOptions{Disk: func(string) (int64, int64, error) { return 500 * gb, 100 * gb, nil }}
	rep := c.Report(context.Background(), opts)
	if r := c.Runtime(); !r.Claude.Unchecked {
		t.Fatalf("the tools' answer does not say the version is unchecked: %+v", r.Claude)
	}
	got := cliCheck(rep)
	if got == nil || got.Level != hubproto.LevelWarn || !strings.Contains(got.Message, "99.0.0") {
		t.Fatalf("the report's check: %+v", got)
	}
	if !cliversion.Cautious(cliversion.Claude) {
		t.Fatal("the cautious mode is off for an unchecked version")
	}
	// Reports and launches go on; the journal has one line for the version.
	c.Report(context.Background(), opts)
	c.Invalidate()
	c.Runtime()
	if len(told) != 1 || told[0] != "claude 99.0.0" {
		t.Fatalf("told %v, want one line", told)
	}
}

func TestACheckedVersionIsNotWarnedOf(t *testing.T) {
	c, _, _ := machineWith(t, cliversion.Table()[0].To)
	rep := c.Report(context.Background(), ReportOptions{Disk: func(string) (int64, int64, error) { return 500 * gb, 100 * gb, nil }})
	if got := cliCheck(rep); got != nil {
		t.Fatalf("a checked version got a check: %+v", got)
	}
	if r := c.Runtime(); r.Claude.Unchecked || cliversion.Cautious(cliversion.Claude) {
		t.Fatalf("a checked version is treated as unchecked: %+v", r.Claude)
	}
}
