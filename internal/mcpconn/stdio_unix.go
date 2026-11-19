//go:build !windows

package mcpconn

import (
	"os/exec"
	"syscall"
)

// setProcGroup puts the server in a group of its own, so npx and the node it starts stop together.
func setProcGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func killProcGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	_ = cmd.Process.Kill()
}
