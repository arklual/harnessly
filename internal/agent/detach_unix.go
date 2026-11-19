//go:build linux || darwin

package agent

import (
	"os/exec"
	"syscall"
)

// detach puts the started agent in a session of its own, so it outlives the terminal that registered it.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
