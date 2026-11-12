package evalrun

import (
	"bytes"
	"context"
	"os/exec"
	"sync"
	"time"
)

// outCap bounds what a command's output keeps in memory.
const outCap = 4 << 20

type capped struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (c *capped) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.b.Len() < outCap {
		c.b.Write(p)
	}
	return len(p), nil
}

func (c *capped) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.String()
}

// execResult is how a command ended. code is -1 when it did not run or was killed.
type execResult struct {
	code     int
	stdout   string
	stderr   string
	ms       int
	timedOut bool
}

type execOpts struct {
	dir     string
	env     []string // nil: the process's own
	timeout time.Duration
}

// run starts a command in a process group of its own and waits for it; it is killed when the timeout passes or ctx ends.
func run(ctx context.Context, name string, args []string, o execOpts) execResult {
	started := time.Now()
	cmd := exec.Command(name, args...)
	cmd.Dir = o.dir
	if o.env != nil {
		cmd.Env = o.env
	}
	ownGroup(cmd)
	var out, errb capped
	cmd.Stdout, cmd.Stderr = &out, &errb
	res := execResult{code: -1}
	if err := cmd.Start(); err != nil {
		res.stderr = err.Error()
		return res
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var timer <-chan time.Time
	if o.timeout > 0 {
		t := time.NewTimer(o.timeout)
		defer t.Stop()
		timer = t.C
	}
	var err error
	select {
	case err = <-done:
	case <-timer:
		res.timedOut = true
		killTree(cmd)
		err = <-done
	case <-ctx.Done():
		killTree(cmd)
		err = <-done
	}
	res.ms = int(time.Since(started).Milliseconds())
	res.stdout, res.stderr = out.String(), res.stderr+errb.String()
	if cmd.ProcessState != nil && !res.timedOut && ctx.Err() == nil {
		res.code = cmd.ProcessState.ExitCode()
	} else if err == nil && cmd.ProcessState != nil {
		res.code = cmd.ProcessState.ExitCode()
	}
	return res
}

// tail is the last n bytes of s.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
