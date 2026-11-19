//go:build !(linux || darwin)

package agent

import "os/exec"

func detach(*exec.Cmd) {}
