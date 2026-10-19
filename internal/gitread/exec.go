package gitread

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

const mib = 1024 * 1024

// run is one git invocation with the limits the previous runtime gave it.
type run struct {
	dir       string        // working directory of the process; "" — the runtime's
	timeout   time.Duration // then SIGTERM
	maxBuffer int           // bytes of stdout (and of stderr) past which the process is stopped and the run fails
}

// execError is a failed run: the exit code (-1 when the process did not exit by itself: a timeout, too much output, no
// such folder) and what it wrote to stdout until then.
type execError struct {
	code   int
	stdout string
	err    error
}

func (e *execError) Error() string { return fmt.Sprintf("git: exit %d: %v", e.code, e.err) }

// exitCode is the exit code of a failed run, -1 for any other failure.
func exitCode(err error) int {
	var ee *execError
	if errors.As(err, &ee) {
		return ee.code
	}
	return -1
}

// stdoutOf is what a failed run wrote before it failed.
func stdoutOf(err error) string {
	var ee *execError
	if errors.As(err, &ee) {
		return ee.stdout
	}
	return ""
}

var errMaxBuffer = errors.New("output exceeded the buffer")

// capped keeps at most max bytes and stops the process once more arrives.
type capped struct {
	buf  []byte
	max  int
	over bool
	stop func()
}

func (c *capped) Write(p []byte) (int, error) {
	if c.over {
		return len(p), nil
	}
	if len(c.buf)+len(p) > c.max {
		c.buf = append(c.buf, p[:c.max-len(c.buf)]...)
		c.over = true
		c.stop()
		return len(p), nil
	}
	c.buf = append(c.buf, p...)
	return len(p), nil
}

// git runs git with args and returns its stdout, decoded as the previous runtime decoded it.
func (o run) git(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = o.dir
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 2 * time.Second
	limit := o.maxBuffer
	if limit <= 0 {
		limit = mib
	}
	stdout := &capped{max: limit, stop: cancel}
	stderr := &capped{max: limit, stop: cancel}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	out := decodeUTF8(stdout.buf)
	if stdout.over || stderr.over {
		return out, &execError{code: -1, stdout: out, err: errMaxBuffer}
	}
	if err != nil {
		code := -1
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.Exited() && ctx.Err() == nil {
			code = ee.ExitCode()
		}
		return out, &execError{code: code, stdout: out, err: err}
	}
	return out, nil
}
