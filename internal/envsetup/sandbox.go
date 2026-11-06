package envsetup

import (
	"os"
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// A sandbox run checks a list «from scratch»: it does the whole list as a person with a fresh machine would. It always
// has a home of its own (a folder under the link's, removed after the run) with HOME and the XDG folders pointing into
// it, the git config in it, the repositories cloned into it and the logins landing in it. Nothing of the real home is
// read or written by the runtime, and the ledger and the setup file are left alone.
//
// Where bubblewrap works (probed once per run) every program also runs inside it: the system is bound read-only, the
// real home is hidden under the sandbox's folder, /tmp is empty, the process tree is its own. Where it does not (many
// machines refuse unprivileged user namespaces) the run goes on without it and says so in every step's log. The scripts
// are the organization's own and run on the machine anyway: the sandbox is for finding what a fresh machine lacks,
// not for containing a hostile script.

const (
	sandboxWrapped   = "Песочница: отдельная домашняя папка + bwrap\n"
	sandboxUnwrapped = "Песочница: отдельная домашняя папка (bwrap на этой машине недоступен)\n"
)

// prepareSandbox makes the folder that stands for the home and finds out whether bubblewrap can be used. False: the run
// has ended in an error (no room, no folder).
func (ex *execution) prepareSandbox() bool {
	if msg := ex.lowDisk(ex.r.LinkDir); msg != "" {
		ex.sandboxFailed(msg)
		return false
	}
	ex.tmpHome = filepath.Join(ex.r.LinkDir, "environment", "sandbox", ex.run.RunID)
	if err := os.MkdirAll(ex.tmpHome, 0o700); err != nil {
		ex.sandboxFailed("песочница не создана: " + err.Error())
		return false
	}
	// The probe is the one program that runs outside the wall.
	exit, err := ex.r.exec(ex.ctx, Cmd{Name: "bwrap", Args: []string{"--ro-bind", "/", "/", "--dev-bind", "/dev", "/dev", "--unshare-pid", "--die-with-parent", "true"},
		Env: ex.baseEnv(), Timeout: loginTimeout})
	ex.wrapped = err == nil && exit == 0
	ex.home, ex.logPrefix = ex.tmpHome, sandboxUnwrapped
	if ex.wrapped {
		ex.home, ex.logPrefix = ex.r.Home, sandboxWrapped
	}
	for _, l := range ex.logs {
		l.reserve = len(ex.logPrefix)
	}
	return true
}

// sandboxFailed ends the run before its first step with a step of its own that says why.
func (ex *execution) sandboxFailed(msg string) {
	steps := []hubproto.EnvStep{{ItemID: "sandbox", Kind: hubproto.EnvKindScript, Name: "Песочница", Status: hubproto.EnvError, Message: msg}}
	for _, s := range ex.run.Steps {
		s.Status, s.Message = hubproto.EnvPending, "ждёт песочницу"
		steps = append(steps, s)
	}
	ex.run.Steps = steps
}

func (ex *execution) removeSandbox() {
	if ex.tmpHome != "" {
		if err := os.RemoveAll(ex.tmpHome); err != nil {
			ex.r.logf("warn", "envsetup: sandbox not removed", "dir", ex.tmpHome, "error", err.Error())
		}
	}
}

// wrap runs c inside bubblewrap.
func (ex *execution) wrap(c Cmd) Cmd {
	args := []string{"--die-with-parent", "--unshare-pid", "--ro-bind", "/", "/", "--dev-bind", "/dev", "/dev", "--tmpfs", "/tmp", "--bind", ex.tmpHome, ex.r.Home}
	if c.Bind != "" {
		args = append(args, "--bind", c.Bind, c.Bind)
	}
	if c.Dir != "" {
		args = append(args, "--chdir", c.Dir)
	}
	args = append(args, "--", c.Name)
	args = append(args, c.Args...)
	c.Name, c.Args, c.Dir, c.Bind = "bwrap", args, "", ""
	return c
}
