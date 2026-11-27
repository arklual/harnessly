// connect-bash is the agent's shell when its chat runs on a Harnessly Connect device: Claude Code is started with
// CLAUDE_CODE_SHELL pointing here (the name must hold "bash") and HARNESSLY_CONNECT_DEVICE naming the device. Without
// a device it is plain bash.
package main

import (
	"os/exec"
	"strings"

	"fmt"
	"os"
	"syscall"

	"github.com/ly-solutions/harnessly/internal/connect"
)

func main() {
	args := os.Args[1:]
	// Claude Code checks a shell with --version before taking it.
	if len(args) == 1 && args[0] == "--version" && os.Getenv(connect.DeviceEnv) != "" {
		fmt.Println("GNU bash, version 5 (harnessly connect)")
		return
	}
	cwd, _ := os.Getwd()
	os.Exit(connect.RunBash(args, connect.BashEnv{
		Getenv: os.Getenv, Environ: os.Environ(), Cwd: cwd,
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		Endpoint: func() (string, string, string, error) { return connect.Endpoint(os.Getenv) },
		Session:  tmuxSession,
		Fallback: func(args []string) int {
			bash := "/bin/bash"
			err := syscall.Exec(bash, append([]string{"bash"}, args...), os.Environ())
			fmt.Fprintf(os.Stderr, "connect-bash: %v\n", err)
			return 127
		},
	}))
}

// tmuxSession is the chat's tmux session: the runtime runs the agent's harnessly commands from the device for it.
func tmuxSession() string {
	if os.Getenv("TMUX") == "" {
		return ""
	}
	out, err := exec.Command("tmux", "display-message", "-p", "#{session_name}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
