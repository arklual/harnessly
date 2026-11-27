//go:build !windows

package connect

import (
	"os"
	"os/exec"
	"syscall"
)

func setGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

var signals = map[string]syscall.Signal{"INT": syscall.SIGINT, "TERM": syscall.SIGTERM, "HUP": syscall.SIGHUP, "KILL": syscall.SIGKILL}

// signalGroup signals the command's whole process group: what the agent's shell started goes with it.
func signalGroup(cmd *exec.Cmd, sig string) {
	s, ok := signals[sig]
	if !ok || cmd.Process == nil {
		return
	}
	if syscall.Kill(-cmd.Process.Pid, s) != nil {
		_ = cmd.Process.Signal(s)
	}
}

func exitCode(ee *exec.ExitError) int {
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ee.ExitCode()
}

func findShell() string {
	for _, p := range []string{"/bin/bash", "/usr/bin/bash", "/usr/local/bin/bash", "/opt/homebrew/bin/bash"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	return ""
}

func shellMissing() string { return "на устройстве нет bash" }

func toShellPath(p string) string   { return p }
func fromShellPath(p string) string { return p }
