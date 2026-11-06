package envsetup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// DefaultExec runs a program as a process of the runtime, in a group of its own: on the timeout the group gets SIGTERM,
// and SIGKILL five seconds later, so a script's children do not outlive it.
func DefaultExec(ctx context.Context, c Cmd) (int, error) {
	runCtx := ctx
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	// Not found is said before anything is wrapped: the wrapper would answer 127 instead.
	path, err := exec.LookPath(c.Name)
	if err != nil {
		return -1, err
	}
	name, args := path, c.Args
	if io, err := exec.LookPath("ionice"); err == nil {
		// Setup is background work on the person's machine: the disk goes to the others first.
		name, args = io, append([]string{"-c", "3", path}, c.Args...)
	}
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = c.Dir
	cmd.Env = append([]string{}, c.Env...)
	if c.Stdin != "" {
		cmd.Stdin = strings.NewReader(c.Stdin)
	}
	out := c.Out
	if out == nil {
		out = io.Discard
	}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		pid := cmd.Process.Pid
		time.AfterFunc(5*time.Second, func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
		return syscall.Kill(-pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 8 * time.Second
	err = cmd.Start()
	if err == nil {
		// Lower priority for the program and what it starts (children inherit it).
		_ = syscall.Setpriority(syscall.PRIO_PROCESS, cmd.Process.Pid, 10)
		err = cmd.Wait()
	}
	if err == nil {
		return 0, nil
	}
	if ctx.Err() == nil && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return -1, ErrTimeout
	}
	if ctx.Err() != nil {
		return -1, ctx.Err()
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return -1, err
}

// freeBytes is the space free for a user on the disk of path.
func freeBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}

// defaultFetch downloads a script: http or https, 30 seconds, at most 1 MiB.
func defaultFetch(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if r.URL.Scheme != "https" {
			return errors.New("переадресация не на https")
		}
		if len(via) >= 5 {
			return errors.New("слишком много переадресаций")
		}
		return nil
	}}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ответ %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, fetchMax+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > fetchMax {
		return nil, errors.New("скрипт больше 1 МиБ")
	}
	return raw, nil
}
