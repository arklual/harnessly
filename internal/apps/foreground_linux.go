package apps

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

// ForegroundExec becomes argv (looked up on PATH) in a process group of its own that it makes the terminal's
// foreground first, when the terminal's foreground is its group: bwrap stays in the group the shell started, so the
// pane's foreground program (#{pane_current_command}, what tells a running agent) is the CLI and not bwrap, and the
// terminal's Ctrl-C and window signals go to the CLI. Off a terminal, or not in its foreground, it only execs. The
// managed launcher does the same for the CLI it starts.
func ForegroundExec(argv []string) error {
	if len(argv) == 0 {
		return errors.New("usage: sandbox-exec -- <command> [args]")
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	// A kernel without one of these says so at the machine check (Boundary.Gaps); the chat starts, as it does without the wall.
	if err := harden(); err != nil {
		fmt.Fprintln(os.Stderr, "sandbox-exec:", err)
	}
	if fg, err := unix.IoctlGetInt(0, unix.TIOCGPGRP); err == nil && fg == unix.Getpgrp() {
		if err := unix.Setpgid(0, 0); err != nil {
			return err
		}
		// A group not in the foreground that sets the foreground is stopped by SIGTTOU unless it ignores it.
		signal.Ignore(syscall.SIGTTOU)
		err := unix.IoctlSetPointerInt(0, unix.TIOCSPGRP, unix.Getpgrp())
		signal.Reset(syscall.SIGTTOU)
		if err != nil {
			return err
		}
	}
	return syscall.Exec(path, argv, os.Environ())
}
