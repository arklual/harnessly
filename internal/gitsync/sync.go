// Package gitsync brings a tab's repository in line with its upstream, as the diff panel's sync button asks: fetch
// with prune, a fast-forward pull when behind, a push when ahead; a failed step is classified for the page.
package gitsync

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

const (
	stepTimeout = 30 * time.Second
	maxBuffer   = 2 * 1024 * 1024
)

// result is one git run: what it wrote, and whether it was stopped (a timeout, or more output than the buffer holds).
type result struct {
	ok             bool
	stdout, stderr string
	killed         bool
}

type capped struct {
	buf  []byte
	over bool
	stop func()
}

func (c *capped) Write(p []byte) (int, error) {
	if c.over {
		return len(p), nil
	}
	if len(c.buf)+len(p) > maxBuffer {
		c.buf = append(c.buf, p[:maxBuffer-len(c.buf)]...)
		c.over = true
		c.stop()
		return len(p), nil
	}
	c.buf = append(c.buf, p...)
	return len(p), nil
}

// git runs git with the previous runtime's limits: 30 s, then SIGTERM; 2 MiB of output.
var git = func(cwd string, args ...string) result {
	ctx, cancel := context.WithTimeout(context.Background(), stepTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 2 * time.Second
	stdout, stderr := &capped{stop: cancel}, &capped{stop: cancel}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	r := result{ok: err == nil && !stdout.over && !stderr.over, stdout: jscompat.DecodeUTF8(stdout.buf), stderr: jscompat.DecodeUTF8(stderr.buf)}
	r.killed = stdout.over || stderr.over || ctx.Err() != nil
	return r
}

// classify is the kind of a failed step the page explains.
func classify(r result) string {
	if r.killed {
		return "timeout"
	}
	s := strings.ToLower(r.stderr + "\n" + r.stdout)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(s, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("no upstream", "no tracking information"):
		return "no-upstream"
	case has("authentication failed", "permission denied", "could not read from remote", "invalid credentials"):
		return "auth"
	case has("would be overwritten by merge", "would be overwritten by checkout", "commit your changes or stash them"):
		return "local-changes"
	case has("divergent", "not possible to fast-forward", "non-fast-forward"):
		if strings.Contains(strings.ToLower(r.stderr), "push") {
			return "rejected"
		}
		return "diverged"
	case has("rejected", "updates were rejected"):
		return "rejected"
	}
	return "unknown"
}

var spaces = regexp.MustCompile(jscompat.SpaceClass + `+`)

func aheadBehind(cwd string) (ahead, behind float64) {
	r := git(cwd, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if !r.ok {
		return 0, 0
	}
	parts := spaces.Split(jscompat.Trim(r.stdout), -1)
	num := func(i int) float64 {
		if i >= len(parts) {
			return 0
		}
		v := jscompat.ParseInt(parts[i], 10)
		if v != v || v == 0 {
			return 0
		}
		return v
	}
	return num(0), num(1)
}

func step(name string, r result) *jsvalue.Object {
	return jsvalue.NewObject("name", name, "ok", r.ok, "skipped", false, "stdout", r.stdout, "stderr", r.stderr)
}

func skipped(name string) *jsvalue.Object {
	return jsvalue.NewObject("name", name, "ok", false, "skipped", true, "stdout", "", "stderr", "")
}

func summary(pulled, pushed float64) *jsvalue.Object {
	return jsvalue.NewObject("pulled", pulled, "pushed", pushed)
}

// Sync syncs the repository at cwd and gives the status and answer of the previous runtime's handler.
func Sync(cwd string) (int, *jsvalue.Object) {
	if !git(cwd, "rev-parse", "--is-inside-work-tree").ok {
		return 400, jsvalue.NewObject("error", "Not a git repository")
	}
	up := git(cwd, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	upstream := ""
	if up.ok {
		upstream = jscompat.Trim(up.stdout)
	}
	if upstream == "" {
		branch := "HEAD"
		if b := git(cwd, "rev-parse", "--abbrev-ref", "HEAD"); b.ok {
			branch = jscompat.Trim(b.stdout)
		}
		return 200, jsvalue.NewObject("ok", false, "steps", []any{}, "summary", summary(0, 0), "upstream", nil, "branch", branch,
			"errorKind", "no-upstream")
	}
	failed := func(steps []any, pulled float64, r result) (int, *jsvalue.Object) {
		return 200, jsvalue.NewObject("ok", false, "steps", steps, "summary", summary(pulled, 0), "upstream", upstream, "errorKind", classify(r))
	}
	fetch := git(cwd, "fetch", "--prune")
	steps := []any{step("fetch", fetch)}
	if !fetch.ok {
		return failed(append(steps, skipped("pull"), skipped("push")), 0, fetch)
	}
	pulled := 0.0
	if _, behind := aheadBehind(cwd); behind > 0 {
		pull := git(cwd, "pull", "--ff-only")
		steps = append(steps, step("pull", pull))
		if !pull.ok {
			return failed(append(steps, skipped("push")), 0, pull)
		}
		pulled = behind
	} else {
		steps = append(steps, skipped("pull"))
	}
	pushed := 0.0
	if ahead, _ := aheadBehind(cwd); ahead > 0 {
		push := git(cwd, "push")
		steps = append(steps, step("push", push))
		if !push.ok {
			return failed(steps, pulled, push)
		}
		pushed = ahead
	} else {
		steps = append(steps, skipped("push"))
	}
	return 200, jsvalue.NewObject("ok", true, "steps", steps, "summary", summary(pulled, pushed), "upstream", upstream)
}
