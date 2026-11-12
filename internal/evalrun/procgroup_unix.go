//go:build unix

package evalrun

import (
	"os/exec"
	"syscall"
)

// ownGroup puts the child in a process group of its own, so that a kill takes the whole tree with it.
func ownGroup(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// killTree kills the child and everything it started.
func killTree(c *exec.Cmd) {
	if c.Process == nil {
		return
	}
	_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	_ = c.Process.Kill()
}
