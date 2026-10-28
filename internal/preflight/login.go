package preflight

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// A login shell is how the person's tabs start, so a command run through one sees what a tab sees: the PATH their
// profile builds (version managers included) and the variables it exports.
const (
	beginMark = "<<HL-BEGIN>>"
	endMark   = "<<HL-END "
	// scriptVar carries the script into the login shell, which hands it to sh: any login shell (bash, zsh, fish) can
	// take it, and the script needs no quoting for them.
	scriptVar = "HL_LOGIN_SCRIPT"
)

// LoginResult is a script run through the login shell.
type LoginResult struct {
	// Stdout is what the script printed, without what the shell's profile printed before it.
	Stdout string
	// Exit is the script's exit code.
	Exit int
}

// LoginCmd is the login shell set up to run an sh script, not yet started: the shell reads the profile, then hands the
// script to sh. extraEnv (KEY=VALUE) is added to the shell's environment. Whoever starts it decides what its input and
// output are (RunLogin reads the output; a login in a terminal gives it a terminal).
func (c *Checker) LoginCmd(ctx context.Context, script string, extraEnv ...string) (*exec.Cmd, error) {
	shell := c.loginShell()
	env := []string{}
	for _, kv := range c.Env {
		k, _, _ := strings.Cut(kv, "=")
		if (inherited[k] || strings.HasPrefix(k, "LC_")) && k != "TERM" && k != "COLORTERM" && k != "SHELL" {
			env = append(env, kv)
		}
	}
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor", "SHELL="+shell, "DISABLE_AUTO_UPDATE=true", "ZSH_TMUX_AUTOSTARTED=true")
	env = append(env, extraEnv...)
	env = append(env, scriptVar+"="+script)

	bin, err := lookPath(shell, c.getenv("PATH"))
	if err != nil {
		return nil, fmt.Errorf("the login shell %s was not found", shell)
	}
	cmd := exec.CommandContext(ctx, bin, "-ilc", `exec /bin/sh -c "$`+scriptVar+`"`)
	cmd.Args[0] = shell
	cmd.Env = env
	cmd.Dir = c.Home
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = time.Second
	return cmd, nil
}

// RunLogin runs an sh script in the account's login shell: the shell reads the profile, then hands the script to sh.
// extraEnv (KEY=VALUE) is added to the shell's environment. The error is for a shell that did not run the script at
// all, or ran out of time; a script that exits non-zero is a result.
func (c *Checker) RunLogin(ctx context.Context, script string, extraEnv ...string) (LoginResult, error) {
	wrapped := "printf '\\n" + beginMark + "\\n'; (\n" + script + "\n); hl_rc=$?; printf '\\n" + endMark + "%d>>\\n' \"$hl_rc\""
	cmd, err := c.LoginCmd(ctx, wrapped, extraEnv...)
	if err != nil {
		return LoginResult{}, err
	}
	var out capped
	cmd.Stdout = &out
	// The shell may print its rc files' complaints; they are of no use here.
	if devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0); err == nil {
		defer devnull.Close()
		cmd.Stdin, cmd.Stderr = devnull, devnull
	}
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return LoginResult{}, fmt.Errorf("the login shell ran out of time: %w", ctx.Err())
	}
	res, ok := parseMarked(out.String())
	if !ok {
		if runErr != nil {
			return LoginResult{}, fmt.Errorf("the login shell failed: %w", runErr)
		}
		return LoginResult{}, errors.New("the login shell did not run the script")
	}
	return res, nil
}

func parseMarked(out string) (LoginResult, bool) {
	_, rest, ok := strings.Cut(out, "\n"+beginMark+"\n")
	if !ok {
		return LoginResult{}, false
	}
	i := strings.LastIndex(rest, "\n"+endMark)
	if i < 0 {
		return LoginResult{}, false
	}
	code, _, _ := strings.Cut(rest[i+1+len(endMark):], ">>")
	exit, err := strconv.Atoi(strings.TrimSpace(code))
	if err != nil {
		return LoginResult{}, false
	}
	return LoginResult{Stdout: strings.TrimPrefix(rest[:i], "\n"), Exit: exit}, true
}
