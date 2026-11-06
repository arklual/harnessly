// Package supervise keeps a machine's processes alive where there is no service manager: a container, WSL without
// systemd, a plain shell. It starts the runtime and the agent, starts one again when it ends (waiting longer after each
// quick failure), says why it ended, and, as the first process of a container, passes signals on and reaps what is
// left over. It does not touch the machine's registration: the agent reads the same machine.json at every start, so
// the machine stays in its hub under the same id.
package supervise

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Child is one process to keep running.
type Child struct {
	Name string
	Path string
	Args []string
}

// Options configure Run. Zero values take the defaults noted on each field.
type Options struct {
	Children []Child
	Stdout   io.Writer // the children's output; default os.Stdout
	Stderr   io.Writer // the children's errors and the supervisor's own lines; default os.Stderr
	// Env is the children's environment; default the supervisor's own.
	Env []string
	// BackoffBase is the wait after the first quick failure (default 1s); each further quick failure doubles it up to
	// BackoffMax (default 30s).
	BackoffBase, BackoffMax time.Duration
	// Stable is how long a child must run to count as healthy: after that the next failure waits BackoffBase again
	// (default 30s).
	Stable time.Duration
	// Grace is how long children get to end after SIGTERM before SIGKILL (default 10s).
	Grace time.Duration
	// StatusFile, when set, receives a JSON note of every child's restarts and last end after each change.
	StatusFile string
	// Reap makes the supervisor collect orphaned processes; default: only when it is process 1.
	Reap *bool
}

func (o *Options) defaults() {
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.Env == nil {
		o.Env = os.Environ()
	}
	if o.BackoffBase <= 0 {
		o.BackoffBase = time.Second
	}
	if o.BackoffMax <= 0 {
		o.BackoffMax = 30 * time.Second
	}
	if o.Stable <= 0 {
		o.Stable = 30 * time.Second
	}
	if o.Grace <= 0 {
		o.Grace = 10 * time.Second
	}
}

// Backoff is the wait after the n-th quick failure in a row (n starts at 1).
func Backoff(n int, base, max time.Duration) time.Duration {
	d := base
	for i := 1; i < n; i++ {
		d *= 2
		if d >= max {
			return max
		}
	}
	if d > max {
		return max
	}
	return d
}

// ChildStatus is what the status file says about one child.
type ChildStatus struct {
	Name      string `json:"name"`
	Pid       int    `json:"pid,omitempty"`
	Running   bool   `json:"running"`
	Restarts  int    `json:"restarts"`
	LastEnd   string `json:"lastEnd,omitempty"`
	LastEndAt string `json:"lastEndAt,omitempty"`
}

type supervisor struct {
	o      Options
	reaper *reaper
	mu     sync.Mutex
	status map[string]*ChildStatus
	order  []string
}

// Run starts every child and keeps them running until ctx ends; then it stops them (SIGTERM, then SIGKILL after the
// grace) and returns nil. It returns an error only when nothing could be started at all.
func Run(ctx context.Context, o Options) error {
	if len(o.Children) == 0 {
		return errors.New("nothing to supervise")
	}
	o.defaults()
	s := &supervisor{o: o, status: map[string]*ChildStatus{}}
	reap := os.Getpid() == 1
	if o.Reap != nil {
		reap = *o.Reap
	}
	if reap {
		s.reaper = startReaper()
		defer s.reaper.stop()
	}
	for _, c := range o.Children {
		s.status[c.Name] = &ChildStatus{Name: c.Name}
		s.order = append(s.order, c.Name)
	}
	var wg sync.WaitGroup
	for _, c := range o.Children {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.keep(ctx, c)
		}()
	}
	wg.Wait()
	return nil
}

func (s *supervisor) say(format string, a ...any) {
	fmt.Fprintf(s.o.Stderr, "harnessly-supervise: "+format+"\n", a...)
}

// keep runs one child over and over.
func (s *supervisor) keep(ctx context.Context, c Child) {
	quick := 0
	for ctx.Err() == nil {
		started := time.Now()
		end, err := s.runOnce(ctx, c)
		if ctx.Err() != nil {
			return
		}
		ran := time.Since(started).Round(time.Millisecond)
		if err != nil {
			end = "did not start: " + err.Error()
		}
		if ran >= s.o.Stable {
			quick = 0
		}
		quick++
		wait := Backoff(quick, s.o.BackoffBase, s.o.BackoffMax)
		s.note(c.Name, func(st *ChildStatus) {
			st.Running, st.Pid = false, 0
			st.Restarts++
			st.LastEnd, st.LastEndAt = end, time.Now().UTC().Format(time.RFC3339)
		})
		s.say("%s ended after %s: %s; starting it again in %s", c.Name, ran, end, wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// runOnce starts the child and waits for it. On ctx it stops the child. It returns how the child ended.
func (s *supervisor) runOnce(ctx context.Context, c Child) (string, error) {
	cmd := exec.Command(c.Path, c.Args...)
	cmd.Env = s.o.Env
	cmd.Stdout, cmd.Stderr = s.o.Stdout, s.o.Stderr
	var done chan waitResult
	if s.reaper != nil {
		// With a reaper every exit is collected there, so cmd.Wait would race it: hand the pid over instead.
		if err := cmd.Start(); err != nil {
			return "", err
		}
		done = s.reaper.watch(cmd.Process.Pid)
	} else {
		if err := cmd.Start(); err != nil {
			return "", err
		}
		done = make(chan waitResult, 1)
		go func() {
			err := cmd.Wait()
			done <- exitOf(cmd.ProcessState, err)
		}()
	}
	pid := cmd.Process.Pid
	s.note(c.Name, func(st *ChildStatus) { st.Running, st.Pid = true, pid })
	select {
	case r := <-done:
		return r.String(), nil
	case <-ctx.Done():
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(s.o.Grace):
			_ = cmd.Process.Kill()
			<-done
		}
		return "stopped", nil
	}
}

// note changes one child's status and writes the status file.
func (s *supervisor) note(name string, change func(*ChildStatus)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(s.status[name])
	if s.o.StatusFile == "" {
		return
	}
	out := make([]ChildStatus, 0, len(s.order))
	for _, n := range s.order {
		out = append(out, *s.status[n])
	}
	b, err := json.MarshalIndent(map[string]any{"children": out}, "", "  ")
	if err != nil {
		return
	}
	tmp := s.o.StatusFile + ".tmp"
	if os.MkdirAll(filepath.Dir(tmp), 0o755) == nil && os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, s.o.StatusFile)
	}
}

// waitResult is how a process ended.
type waitResult struct {
	Code   int
	Signal syscall.Signal
}

func (r waitResult) String() string {
	if r.Signal != 0 {
		return fmt.Sprintf("killed by signal %d (%s)", int(r.Signal), r.Signal)
	}
	if r.Code == 0 {
		return "exited with status 0"
	}
	return fmt.Sprintf("exited with status %d", r.Code)
}

func exitOf(ps *os.ProcessState, err error) waitResult {
	if ps == nil {
		return waitResult{Code: -1}
	}
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok {
		if ws.Signaled() {
			return waitResult{Signal: ws.Signal()}
		}
		return waitResult{Code: ws.ExitStatus()}
	}
	_ = err
	return waitResult{Code: ps.ExitCode()}
}
