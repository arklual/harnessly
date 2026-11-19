package agent

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// A registration is worth something only with an agent running for it: the hub shows the machine as connecting until
// the agent links. register therefore makes sure one runs, the way the machine allows: the installer's unit when there
// is one (systemd restarts it with the new keys), otherwise the agent in the background of this account, its output in
// agent.log next to its data. A machine without a service manager (a container) gets no autostart: the status line and
// the output say how to start it again after a reboot.

// agentUnit is the unit the installer leaves (deploy/install/install.sh).
const agentUnit = "harnessly-agent.service"

// unitScope says whether the installer's unit is there: "--user" for the account's own, "" for the system one, and
// false for none.
func unitScope(home string) (string, bool) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return "", false
	}
	if home != "" {
		if _, err := os.Stat(filepath.Join(home, ".config", "systemd", "user", agentUnit)); err == nil {
			return "--user", true
		}
	}
	for _, d := range []string{"/etc/systemd/system", "/lib/systemd/system", "/usr/lib/systemd/system"} {
		if _, err := os.Stat(filepath.Join(d, agentUnit)); err == nil {
			return "", true
		}
	}
	return "", false
}

// agentPort is the port the agent of this account listens on: the environment, an agent.env, else the default.
func agentPort() int {
	if n, err := strconv.Atoi(env("AGENT_PORT", "")); err == nil && n > 0 {
		return n
	}
	return DefaultAgentPort
}

func listening(port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// startCommand is how a person starts the agent by hand on this machine.
func startCommand(dataDir string) string {
	exe, err := os.Executable()
	if err != nil {
		exe = "harnessly-agent"
	}
	return fmt.Sprintf("nohup %s >>%s 2>&1 &", exe, filepath.Join(dataDir, "agent.log"))
}

// startAfterRegister is startAgent; tests replace it.
var startAfterRegister = startAgent

// startAgent runs the agent for a fresh registration (see the top of the file).
func startAgent(paths cliPaths, stdout, stderr io.Writer) {
	loadEnvKeys(systemEnvFiles(paths.home), runEnvKeys, func(k string) (string, bool) { v, ok := os.LookupEnv(k); return v, ok && v != "" },
		func(k, v string) { _ = os.Setenv(k, v) })
	if scope, ok := unitScope(paths.home); ok {
		args := []string{"restart", agentUnit}
		if scope != "" {
			args = append([]string{scope}, args...)
		}
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err == nil {
			fmt.Fprintf(stdout, "restarted %s: it links with the new registration\n", agentUnit)
			return
		} else {
			fmt.Fprintf(stderr, "harnessly-agent register: systemctl %v: %v %s\n", args, err, out)
		}
	}
	port := agentPort()
	if listening(port) {
		fmt.Fprintf(stdout, "an agent already runs on port %d: restart it to link with the new registration\n", port)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, "harnessly-agent register: cannot start the agent: %v; start it with: %s\n", err, startCommand(paths.dataDir))
		return
	}
	if err := os.MkdirAll(paths.dataDir, 0o700); err != nil {
		fmt.Fprintf(stderr, "harnessly-agent register: cannot start the agent: %v\n", err)
		return
	}
	logPath := filepath.Join(paths.dataDir, "agent.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintf(stderr, "harnessly-agent register: cannot start the agent: %v\n", err)
		return
	}
	defer f.Close()
	cmd := exec.Command(exe)
	cmd.Stdout, cmd.Stderr = f, f
	cmd.Env = append(os.Environ(), "AGENT_PORT="+strconv.Itoa(port))
	detach(cmd)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "harnessly-agent register: cannot start the agent: %v; start it with: %s\n", err, startCommand(paths.dataDir))
		return
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	fmt.Fprintf(stdout, "started the agent in the background (pid %d, port %d), its log: %s\n", pid, port, logPath)
	fmt.Fprintf(stdout, "this machine has no service manager to start it after a reboot; then run: %s\n", startCommand(paths.dataDir))
}
