package fleetdriver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ExitError is a command on a machine that ended with a code.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit %d", e.Code) }

// Runner runs a shell command on a machine. stdin and stdout may be nil.
type Runner interface {
	Run(ctx context.Context, target, command string, stdin io.Reader, stdout io.Writer) error
}

// LocalTarget is a target that is this machine itself: the command runs here with `sh -c`, no ssh. A fleet whose
// machine is the one the driver runs on (one pair of a developer's machine, a single-host install) names it so.
const LocalTarget = "local"

// ExecRunner runs `<Cmd...> <target> <command>` on this machine (ssh, tsh ssh, ...); the target "local" is `sh -c`.
type ExecRunner struct {
	Cmd    []string
	Stderr io.Writer
}

// Run implements Runner. A command that ended with a code comes back as *ExitError.
func (r ExecRunner) Run(ctx context.Context, target, command string, stdin io.Reader, stdout io.Writer) error {
	var c *exec.Cmd
	if target == LocalTarget {
		c = exec.CommandContext(ctx, "sh", "-c", command)
	} else {
		if len(r.Cmd) == 0 {
			return errors.New("no ssh command")
		}
		args := append(append([]string(nil), r.Cmd[1:]...), target, command)
		c = exec.CommandContext(ctx, r.Cmd[0], args...)
	}
	c.Stdin, c.Stdout, c.Stderr = stdin, stdout, r.Stderr
	err := c.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return &ExitError{Code: ee.ExitCode()}
	}
	return err
}

// exitCodeOf is the code of a command's error: 0 for nil, -1 for an error that is not an exit code.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	return -1
}

// Plan is one rollout.
type Plan struct {
	Fleet   *Fleet
	Runner  Runner
	Version string
	// Tarball is the whole release; Delta (with DeltaBase, the commit it was cut against) is optional, see
	// docs/hub/fleet-rollout.md: a machine that runs the base takes the delta, any other gets the whole release.
	Tarball   string
	Delta     string
	DeltaBase string
	Only      []string
	// Force passes --force to activate (the machine's window between rollouts, RUNTIME_FLEET_MIN_GAP).
	Force bool
	// StageJobs caps the machines staged at once (default 4), ActivateJobs those of a wave activated at once
	// (default: the whole wave).
	StageJobs, ActivateJobs int
	StageTimeout            time.Duration // one machine, upload included (default 25 min)
	ActivateTimeout         time.Duration // one machine (default 30 min)
	// Soak is the pause after a wave whose machines all passed, before the next wave starts.
	Soak         time.Duration
	UploadDelay  time.Duration // between upload attempts (default 10 s)
	Log          func(format string, a ...any)
	DryRun       bool
	sleep        func(time.Duration)
	transfersDir string
}

// Result says what happened to each machine.
type Result struct {
	Skipped    []string `json:"skipped,omitempty"`    // already ran the version
	Activated  []string `json:"activated,omitempty"`  // runs the version now
	Failed     []string `json:"failed,omitempty"`     // failed and went back to its own release by itself
	RolledBack []string `json:"rolledBack,omitempty"` // moved, then put back after another machine failed
	NotTouched []string `json:"notTouched,omitempty"` // waves after the failed one
}

func (p *Plan) logf(f string, a ...any) {
	if p.Log != nil {
		p.Log(f, a...)
	}
}

func (p *Plan) bin(h Host) string {
	switch {
	case h.Binary != "":
		return h.Binary
	case p.Fleet.Binary != "":
		return p.Fleet.Binary
	}
	return "harnessly"
}

// remote builds the command that runs a fleet subcommand on a machine: the machine's settings file first.
func (p *Plan) remote(h Host, args ...string) string {
	var b strings.Builder
	b.WriteString("set -e; ")
	if h.EnvFile != "" {
		fmt.Fprintf(&b, "set -a; . %s; set +a; ", h.EnvFile)
	}
	b.WriteString(p.bin(h))
	b.WriteString(" fleet ")
	b.WriteString(args[0])
	if h.Root != "" {
		b.WriteString(" --root " + h.Root)
	}
	for _, a := range args[1:] {
		b.WriteString(" " + a)
	}
	return b.String()
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (p *Plan) check() error {
	if p.Fleet == nil || p.Runner == nil {
		return errors.New("no fleet or runner")
	}
	if !reVersion.MatchString(p.Version) {
		return fmt.Errorf("not a release version: %q", p.Version)
	}
	if p.Tarball == "" {
		return errors.New("no tarball")
	}
	if p.Delta != "" && !reCommit.MatchString(p.DeltaBase) {
		return fmt.Errorf("a delta needs the commit it was cut against (40 hex), not %q", p.DeltaBase)
	}
	if p.StageJobs <= 0 {
		p.StageJobs = 4
	}
	if p.StageTimeout <= 0 {
		p.StageTimeout = 25 * time.Minute
	}
	if p.ActivateTimeout <= 0 {
		p.ActivateTimeout = 30 * time.Minute
	}
	if p.UploadDelay <= 0 {
		p.UploadDelay = 10 * time.Second
	}
	if p.sleep == nil {
		p.sleep = time.Sleep
	}
	p.transfersDir = p.Fleet.Transfers
	if p.transfersDir == "" {
		p.transfersDir = "/var/lib/harnessly/transfers"
	}
	return nil
}

type statusAnswer struct {
	Version string `json:"version"`
}

// runs reports the release a machine serves now.
func (p *Plan) runs(ctx context.Context, h Host) (string, error) {
	var out strings.Builder
	if err := p.Runner.Run(ctx, h.Target, p.remote(h, "status", "--json"), nil, &out); err != nil {
		return "", fmt.Errorf("status of %s: %w", h.Name, err)
	}
	var st statusAnswer
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &st); err != nil {
		// The command may print lines of its own before the answer: the answer is the last line.
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if err2 := json.Unmarshal([]byte(lines[len(lines)-1]), &st); err2 != nil {
			return "", fmt.Errorf("status of %s is not JSON: %q", h.Name, out.String())
		}
	}
	return st.Version, nil
}

func (p *Plan) upload(ctx context.Context, h Host, file, name string) error {
	t := p.transfersDir
	cmd := fmt.Sprintf("mkdir -p %s && cat >%s/%s.part && mv -f %s/%s.part %s/%s", t, t, name, t, name, t, name)
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		f, oerr := os.Open(file)
		if oerr != nil {
			return oerr
		}
		uctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
		err = p.Runner.Run(uctx, h.Target, cmd, f, nil)
		cancel()
		f.Close()
		if err == nil || ctx.Err() != nil {
			return err
		}
		if attempt < 3 {
			p.logf("upload to %s failed (%v), retrying", h.Name, err)
			p.sleep(p.UploadDelay)
		}
	}
	return fmt.Errorf("could not upload the release to %s: %w", h.Name, err)
}

// stage puts the release on one machine and returns the checksum of what was staged.
func (p *Plan) stage(ctx context.Context, h Host, wholeSHA, deltaSHA string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, p.StageTimeout)
	defer cancel()
	if p.Delta != "" {
		name := p.Version + ".delta.tgz"
		if err := p.upload(ctx, h, p.Delta, name); err != nil {
			return "", err
		}
		err := p.Runner.Run(ctx, h.Target, p.remote(h, "stage", "--version", p.Version,
			"--tarball", p.transfersDir+"/"+name, "--sha256", deltaSHA, "--delta-base", p.DeltaBase), nil, nil)
		if err == nil {
			return deltaSHA, nil
		}
		if exitCodeOf(err) != 3 { // 3: the machine runs another commit, nothing changed
			return "", fmt.Errorf("stage on %s: %w", h.Name, err)
		}
		p.logf("%s does not run %s: the whole release goes", h.Name, p.DeltaBase)
	}
	name := p.Version + ".tgz"
	if err := p.upload(ctx, h, p.Tarball, name); err != nil {
		return "", err
	}
	if err := p.Runner.Run(ctx, h.Target, p.remote(h, "stage", "--version", p.Version,
		"--tarball", p.transfersDir+"/"+name, "--sha256", wholeSHA), nil, nil); err != nil {
		return "", fmt.Errorf("stage on %s: %w", h.Name, err)
	}
	return wholeSHA, nil
}

func (p *Plan) activate(ctx context.Context, h Host, sha string) error {
	ctx, cancel := context.WithTimeout(ctx, p.ActivateTimeout)
	defer cancel()
	args := []string{"activate", "--version", p.Version, "--sha256", sha}
	if p.Force {
		args = append(args, "--force")
	}
	if err := p.Runner.Run(ctx, h.Target, p.remote(h, args...), nil, nil); err != nil {
		if exitCodeOf(err) == 4 {
			return fmt.Errorf("%s refuses a release so soon after the last one (RUNTIME_FLEET_MIN_GAP); --force goes through: %w", h.Name, err)
		}
		return fmt.Errorf("activate on %s: %w", h.Name, err)
	}
	// The machine's own check has passed; the version it serves is asked once more so a rollout never ends on a
	// machine that answers with another release.
	got, err := p.runs(ctx, h)
	if err != nil {
		return err
	}
	if got != p.Version {
		return fmt.Errorf("%s serves %q after the activation, not %s", h.Name, got, p.Version)
	}
	return nil
}

// each runs fn for every item, up to jobs at once, and returns the errors in the order of the items.
func each[T any](jobs int, items []T, fn func(T) error) []error {
	if jobs <= 0 || jobs > len(items) {
		jobs = len(items)
	}
	errs := make([]error, len(items))
	sem := make(chan struct{}, max(jobs, 1))
	var wg sync.WaitGroup
	for i, it := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			errs[i] = fn(it)
		}()
	}
	wg.Wait()
	return errs
}

// Run rolls the release out. On a failure the machines that were moved go back with `fleet rollback` (the machine that
// failed has put itself back already) and the error says which machine and why; Result tells where everything is.
func (p *Plan) Run(ctx context.Context) (Result, error) {
	var res Result
	if err := p.check(); err != nil {
		return res, err
	}
	hosts, err := p.Fleet.Select(p.Only)
	if err != nil {
		return res, err
	}
	wholeSHA, err := fileSHA(p.Tarball)
	if err != nil {
		return res, err
	}
	var deltaSHA string
	if p.Delta != "" {
		if deltaSHA, err = fileSHA(p.Delta); err != nil {
			return res, err
		}
	}
	if !reSHA.MatchString(wholeSHA) {
		return res, errors.New("bad checksum")
	}
	p.logf("release %s on %d machines, sha256 %s", p.Version, len(hosts), wholeSHA)

	// Machines that run the version already are left alone, so a rollout that was cut short can be run again.
	var todo []Host
	for _, h := range hosts {
		got, err := p.runs(ctx, h)
		if err != nil {
			return res, err
		}
		if got == p.Version {
			p.logf("%s already runs %s", h.Name, p.Version)
			res.Skipped = append(res.Skipped, h.Name)
			continue
		}
		p.logf("%s runs %q", h.Name, got)
		todo = append(todo, h)
	}
	if len(todo) == 0 {
		return res, nil
	}
	if p.DryRun {
		for _, w := range Waves(todo) {
			var names []string
			for _, h := range w {
				names = append(names, h.Name)
			}
			p.logf("wave %d: %s", w[0].Wave, strings.Join(names, " "))
		}
		return res, nil
	}

	// 1. Stage everywhere. Nothing that runs changes, so a failure here leaves every machine as it was.
	p.logf("== staging on %d machines, up to %d at once", len(todo), p.StageJobs)
	shas := map[string]string{}
	var mu sync.Mutex
	errs := each(p.StageJobs, todo, func(h Host) error {
		s, err := p.stage(ctx, h, wholeSHA, deltaSHA)
		if err != nil {
			return err
		}
		mu.Lock()
		shas[h.Name] = s
		mu.Unlock()
		p.logf("staged on %s", h.Name)
		return nil
	})
	if err := errors.Join(errs...); err != nil {
		return res, fmt.Errorf("staging failed, no machine was changed: %w", err)
	}

	// 2. Activate wave by wave.
	waves := Waves(todo)
	var moved []Host
	for wi, w := range waves {
		p.logf("== wave %d: %d machines", w[0].Wave, len(w))
		errs := each(p.ActivateJobs, w, func(h Host) error { return p.activate(ctx, h, shas[h.Name]) })
		var failures []error
		for i, e := range errs {
			if e != nil {
				failures = append(failures, e)
				res.Failed = append(res.Failed, w[i].Name)
				continue
			}
			moved = append(moved, w[i])
			res.Activated = append(res.Activated, w[i].Name)
			p.logf("activated %s", w[i].Name)
		}
		if len(failures) > 0 {
			for _, later := range waves[wi+1:] {
				for _, h := range later {
					res.NotTouched = append(res.NotTouched, h.Name)
				}
			}
			rerr := p.rollBack(ctx, moved, &res)
			return res, errors.Join(append(failures, rerr)...)
		}
		if wi < len(waves)-1 && p.Soak > 0 {
			p.logf("soak %s before the next wave", p.Soak)
			p.sleep(p.Soak)
		}
	}
	p.logf("release %s is active on %d machines", p.Version, len(res.Activated))
	return res, nil
}

// rollBack puts the machines that moved back to the release before, and moves them from Activated to RolledBack.
func (p *Plan) rollBack(ctx context.Context, moved []Host, res *Result) error {
	if len(moved) == 0 {
		return nil
	}
	p.logf("== a machine failed: putting %d back", len(moved))
	// Fresh context: the rollout may have been cut short by the very deadline that makes this necessary.
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.ActivateTimeout)
	defer cancel()
	errs := each(p.ActivateJobs, moved, func(h Host) error {
		if err := p.Runner.Run(rctx, h.Target, p.remote(h, "rollback"), nil, nil); err != nil {
			return fmt.Errorf("rollback on %s: %w", h.Name, err)
		}
		return nil
	})
	var out []error
	back := map[string]bool{}
	for i, e := range errs {
		if e != nil {
			out = append(out, e)
			continue
		}
		back[moved[i].Name] = true
		res.RolledBack = append(res.RolledBack, moved[i].Name)
	}
	kept := res.Activated[:0]
	for _, n := range res.Activated {
		if !back[n] {
			kept = append(kept, n)
		}
	}
	res.Activated = kept
	return errors.Join(out...)
}
