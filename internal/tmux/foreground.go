package tmux

import (
	"os"
	"path"
	"strconv"
	"strings"
)

// procRoot is where foregroundOf reads processes; tests point it elsewhere.
var procRoot = "/proc"

// foregroundOf is the program in the foreground of the pane whose shell is panePID, read from /proc the way tmux reads
// it (the terminal's foreground process group, the first word of its leader's command line), or "" when /proc does not
// tell.
//
// tmux's own #{pane_current_command} cannot be trusted alone: when the server cannot read /proc/<pgrp>/cmdline (the
// group is not visible from its pid namespace, /proc is hidden from it, the leader is gone) it silently answers with the
// first word of the command the pane was started with — `nice` or `env` for every pane of this runtime — and a pane
// with nothing but its shell looked busy, so the agent of a restored tab was never resumed.
func foregroundOf(panePID int) string {
	if panePID <= 0 {
		return ""
	}
	shell, ok := readStat(panePID)
	// The pane's shell leads its own session; anything else is not the pane this pid names in our namespace, and saying
	// nothing is better than describing another process.
	if !ok || shell.session != panePID {
		return ""
	}
	fg := shell.tpgid
	if shell.tty == 0 || fg <= 0 {
		// gVisor (GitHub issue 37) leaves tty_nr and tpgid at 0, and tmux's own tcgetpgrp on the pane fails there too;
		// the ioctl answers only on the terminal's own session, which the runtime is not in. The session's processes
		// still say it.
		fg = sessionForeground(panePID, shell.pgrp)
		if fg <= 0 {
			return ""
		}
	}
	cmdline, err := os.ReadFile(procRoot + "/" + strconv.Itoa(fg) + "/cmdline")
	if err != nil {
		return ""
	}
	argv0, _, _ := strings.Cut(string(cmdline), "\x00")
	return windowName(argv0)
}

// procStat is what foregroundOf needs of /proc/<pid>/stat.
type procStat struct {
	state                     byte
	pgrp, session, tty, tpgid int
}

func readStat(pid int) (procStat, bool) {
	stat, err := os.ReadFile(procRoot + "/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return procStat{}, false
	}
	// pid (comm) state ppid pgrp session tty_nr tpgid …; comm may hold spaces and parentheses.
	end := strings.LastIndexByte(string(stat), ')')
	if end < 0 {
		return procStat{}, false
	}
	f := strings.Fields(string(stat)[end+1:])
	if len(f) < 6 || f[0] == "" {
		return procStat{}, false
	}
	var s procStat
	s.state = f[0][0]
	s.pgrp, _ = strconv.Atoi(f[2])
	s.session, _ = strconv.Atoi(f[3])
	s.tty, _ = strconv.Atoi(f[4])
	s.tpgid, _ = strconv.Atoi(f[5])
	return s, true
}

// sessionForeground is the process group in the foreground of the session led by the pane's shell when the kernel does
// not say: the shell's own group when nothing else of the session runs, the one other running group when there is
// exactly one, else 0 (a background job beside a foreground one cannot be told apart; tmux's word then stands, which
// reads as busy rather than idle). Stopped and dead processes hold no foreground.
func sessionForeground(session, shellGroup int) int {
	dir, err := os.ReadDir(procRoot)
	if err != nil {
		return 0
	}
	other := 0
	for _, e := range dir {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == session {
			continue
		}
		s, ok := readStat(pid)
		if !ok || s.session != session || s.pgrp == shellGroup || s.pgrp <= 0 {
			continue
		}
		switch s.state {
		case 'T', 't', 'Z', 'X':
			continue
		}
		if other != 0 && other != s.pgrp {
			return 0
		}
		other = s.pgrp
	}
	if other != 0 {
		return other
	}
	return shellGroup
}

// windowName is tmux's parse_window_name: no quotes, no leading "exec " or dashes, the first word, its base name.
func windowName(s string) string {
	s = strings.TrimPrefix(s, `"`)
	if i := strings.IndexByte(s, '"'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimPrefix(s, "exec ")
	s = strings.TrimLeft(s, " -")
	if i := strings.IndexByte(s, ' '); i >= 0 {
		s = s[:i]
	}
	if strings.HasPrefix(s, "/") {
		s = path.Base(s)
	}
	return s
}

// ForegroundCommand is the foreground program of the pane whose shell is panePID: what /proc says, else what tmux
// said (#{pane_current_command}). Every reader of #{pane_current_command} goes through it.
func ForegroundCommand(tmuxSaid string, panePID int) string {
	if c := foregroundOf(panePID); c != "" {
		return c
	}
	return tmuxSaid
}
