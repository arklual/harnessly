package builders

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/heavy"
)

// Spec is a heavy job as a caller states it.
type Spec struct {
	Title string
	// Caps are the labels the builder must carry (blender, gpu, ffmpeg...).
	Caps []string
	// Builder is a builder by name or id; "" or "auto" lets the hub choose.
	Builder string
	// Argv is the command, program first (a bare name: blender, ffmpeg).
	Argv []string
	Env  map[string]string
	// InDir is the folder the command runs in: packed and sent to a builder, used in place by a local run. "" is none.
	InDir string
	// Outputs are globs, relative to InDir, of the files the job hands back.
	Outputs []string
	// Dest is where a builder's result files are written, keeping their paths; "" is InDir.
	Dest string
	// Timeout ends the command; 0 is an hour.
	Timeout time.Duration
	// Log gets progress lines and, for a local run, the command's output.
	Log io.Writer
}

// Result is how a job ended.
type Result struct {
	Job      Job
	ExitCode int
	// Files are the result files, paths relative to Dest.
	Files []string
}

func (s Spec) logf(format string, args ...any) {
	if s.Log != nil {
		fmt.Fprintf(s.Log, format+"\n", args...)
	}
}

// Run submits the job, follows it to its end and brings the result files back. With no online builder that fits the hub
// says so and the command runs here, on the machine's heavy slots, with draft limits. A cancelled ctx cancels the job.
func (c *Client) Run(ctx context.Context, spec Spec) (Result, error) {
	if len(spec.Argv) == 0 {
		return Result{}, errors.New("no command")
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = time.Hour
	}
	job, err := c.Submit(ctx, Request{Title: spec.Title, Need: Need{Kind: "heavy", Caps: spec.Caps, Machine: spec.Builder}, Argv: spec.Argv, Env: spec.Env,
		Outputs: spec.Outputs, TimeoutSec: int(timeout / time.Second), HasInput: spec.InDir != ""})
	if err != nil {
		if NoPool(err) {
			return RunStandalone(ctx, spec, "this machine has no pool of builders to ask ("+err.Error()+")")
		}
		return Result{}, err
	}
	spec.logf("job %s: %s", job.ID, job.Reason)
	uploaded := false
	var res Result
	for {
		switch {
		case job.Local && !job.Final():
			return c.runLocal(ctx, job, spec)
		case !job.Local && !uploaded && spec.InDir != "" && job.Status == "queued":
			uploaded = true
			if err := c.upload(ctx, job.ID, spec.InDir); err != nil {
				_, _ = c.Cancel(context.Background(), job.ID)
				return Result{Job: job}, fmt.Errorf("sending the files to the hub: %w", err)
			}
			spec.logf("job %s: files sent", job.ID)
		case job.Final():
			res.Job = job
			if job.ExitCode != nil {
				res.ExitCode = *job.ExitCode
			}
			if job.Local {
				return res, jobError(job)
			}
			dest := spec.Dest
			if dest == "" {
				dest = spec.InDir
			}
			if dest != "" {
				for _, f := range job.Outputs {
					if err := c.fetch(ctx, job.ID, f.Path, dest); err != nil {
						return res, fmt.Errorf("taking %s back: %w", f.Path, err)
					}
					res.Files = append(res.Files, f.Path)
				}
			}
			return res, jobError(job)
		}
		select {
		case <-ctx.Done():
			cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, _ = c.Cancel(cctx, job.ID)
			cancel()
			return Result{Job: job}, ctx.Err()
		case <-time.After(PollEvery):
		}
		prev := job.Status + job.Reason
		if job, err = c.Get(ctx, job.ID); err != nil {
			if ctx.Err() != nil {
				continue
			}
			return Result{Job: job}, err
		}
		if now := job.Status + job.Reason; now != prev {
			where := job.Reason
			if job.Builder != nil && !job.Local {
				where = job.Builder.Name + ": " + job.Reason
			}
			spec.logf("job %s: %s — %s", job.ID, job.Status, where)
		}
	}
}

func jobError(j Job) error {
	switch j.Status {
	case "done":
		return nil
	case "cancelled":
		return errors.New("the job was cancelled")
	}
	if j.Error != "" {
		return errors.New(j.Error)
	}
	return fmt.Errorf("the job failed (status %s)", j.Status)
}

func (c *Client) upload(ctx context.Context, id, dir string) error {
	pr, pw := io.Pipe()
	go func() {
		_, err := Pack(dir, pw)
		pw.CloseWithError(err)
	}()
	err := c.PutInput(ctx, id, pr)
	pr.CloseWithError(err)
	return err
}

func (c *Client) fetch(ctx context.Context, id, rel, dest string) error {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("a result path outside the folder: %q", rel)
	}
	dst := filepath.Join(dest, clean)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(dst), ".job-*")
	if err != nil {
		return err
	}
	err = c.Download(ctx, id, rel, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	return os.Rename(f.Name(), dst)
}

// tailBuffer keeps the last bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 16<<10 {
		t.buf = t.buf[len(t.buf)-8<<10:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(bytes.ToValidUTF8(t.buf, nil))
}

// RunStandalone is Run on a machine that is in no hub: there is no pool, so the command runs on this machine's own heavy
// slots as a draft, and the first line says why.
func RunStandalone(ctx context.Context, spec Spec, why string) (Result, error) {
	timeout := min(spec.Timeout, localDraft)
	if timeout <= 0 {
		timeout = localDraft
	}
	job := Job{ID: "local", Status: "running", Local: true, Reason: why, TimeoutSec: int(timeout / time.Second)}
	var c *Client
	return c.runLocal(ctx, job, spec)
}

// localDraft is the longest a draft run on this machine's own slots may take.
const localDraft = 10 * time.Minute

// runLocal runs the job on this machine's heavy slots under the draft limits the hub named, and tells the hub the end (a
// nil client has no hub to tell).
func (c *Client) runLocal(ctx context.Context, job Job, spec Spec) (Result, error) {
	limit := time.Duration(job.TimeoutSec) * time.Second
	if job.LocalLimits != nil && job.LocalLimits.TimeoutSec > 0 {
		limit = time.Duration(job.LocalLimits.TimeoutSec) * time.Second
	}
	if limit <= 0 {
		limit = 10 * time.Minute
	}
	spec.logf("job %s: runs on this machine's own heavy slots, a draft: at most %s. %s", job.ID, limit, job.Reason)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// the hub's status is the stop button of a local run too; the pace is read here, not by the goroutine, which may
	// outlive the call by one tick
	every := PollEvery * 2
	go func() {
		for c != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(every):
			}
			if j, err := c.Get(ctx, job.ID); err == nil && j.Final() {
				cancel()
				return
			}
		}
	}()
	release, err := heavy.Hold(spec.Argv[0], 15*time.Minute)
	if err != nil {
		msg := "no heavy slot on this machine freed up in 15 minutes: " + err.Error()
		c.finishLocal(context.Background(), job.ID, 75, msg, "")
		return Result{Job: job, ExitCode: 75}, errors.New(msg)
	}
	defer release()
	runCtx, stop := context.WithTimeout(ctx, limit)
	defer stop()
	argv := heavy.Gentle(spec.Argv)
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = spec.InDir
	cmd.Env = append(os.Environ(), "HARNESSLY_JOB_LOCAL=1", "HARNESSLY_JOB_DRAFT=1")
	for k, v := range spec.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	tail := &tailBuffer{}
	var out io.Writer = tail
	if spec.Log != nil {
		out = io.MultiWriter(tail, spec.Log)
	}
	cmd.Stdout, cmd.Stderr = out, out
	runErr := cmd.Run()
	exit := 0
	errText := ""
	if runErr != nil {
		exit = 1
		var ee *exec.ExitError
		switch {
		case errors.As(runErr, &ee):
			exit = ee.ExitCode()
			errText = fmt.Sprintf("the command ended with exit code %d", exit)
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			errText = fmt.Sprintf("the draft limit of %s ended the command", limit)
		case ctx.Err() != nil:
			errText = "cancelled"
		default:
			errText = runErr.Error()
		}
	}
	fctx, done := context.WithTimeout(context.Background(), 20*time.Second)
	defer done()
	c.finishLocal(fctx, job.ID, exit, errText, tail.String())
	res := Result{Job: job, ExitCode: exit}
	if spec.InDir != "" {
		res.Files, _ = Collect(spec.InDir, spec.Outputs)
	}
	if errText != "" {
		return res, errors.New(errText)
	}
	return res, nil
}

func (c *Client) finishLocal(ctx context.Context, id string, exit int, errText, log string) {
	if c != nil {
		_ = c.Finish(ctx, id, exit, errText, log)
	}
}
