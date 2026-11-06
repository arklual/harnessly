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
	stat, err := os.ReadFile(procRoot + "/" + strconv.Itoa(panePID) + "/stat")
	if err != nil {
		return ""
	}
	// pid (comm) state ppid pgrp session tty_nr tpgid …; comm may hold spaces and parentheses.
	end := strings.LastIndexByte(string(stat), ')')
	if end < 0 {
		return ""
	}
	f := strings.Fields(string(stat)[end+1:])
	if len(f) < 6 {
		return ""
	}
	session, _ := strconv.Atoi(f[3])
	tty, _ := strconv.Atoi(f[4])
	tpgid, _ := strconv.Atoi(f[5])
	// The pane's shell leads its own session on the pane's terminal; anything else is not the pane this pid names in
	// our namespace, and saying nothing is better than describing another process.
	if session != panePID || tty == 0 || tpgid <= 0 {
		return ""
	}
	cmdline, err := os.ReadFile(procRoot + "/" + strconv.Itoa(tpgid) + "/cmdline")
	if err != nil {
		return ""
	}
	argv0, _, _ := strings.Cut(string(cmdline), "\x00")
	return windowName(argv0)
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
