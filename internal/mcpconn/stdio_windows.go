//go:build windows

package mcpconn

import "os/exec"

func setProcGroup(*exec.Cmd) {}

func killProcGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
