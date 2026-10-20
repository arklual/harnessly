// Package proc reads the process table of this machine: a process's children and descendants, its command line,
// environment, working folder and start time, the TCP ports a process tree listens on, and memory pressure. Everything
// is read from /proc; a process that cannot be read (gone, another user's) reads as nothing rather than as an error.
package proc

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

func dir(pid int) string { return "/proc/" + strconv.Itoa(pid) }

func parsePids(s string) []int {
	var out []int
	for _, f := range strings.Fields(s) {
		if n, err := strconv.Atoi(f); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// ChildPIDs are a process's children in pid order. They are listed per thread: a process that spawns from a worker
// thread has them under that thread. Without the per-thread lists (a kernel built without them) the process table is
// read instead, as pgrep -P does. A process that is gone has none.
func ChildPIDs(parent int) []int {
	base := dir(parent) + "/task"
	tasks, err := os.ReadDir(base)
	if err != nil {
		return []int{}
	}
	seen := map[int]bool{}
	out := []int{}
	missing := 0
	for _, t := range tasks {
		raw, err := os.ReadFile(filepath.Join(base, t.Name(), "children"))
		if err != nil {
			if os.IsNotExist(err) {
				missing++
			}
			continue
		}
		for _, pid := range parsePids(string(raw)) {
			if !seen[pid] {
				seen[pid] = true
				out = append(out, pid)
			}
		}
	}
	if len(tasks) > 0 && missing == len(tasks) {
		if _, err := os.Stat(dir(parent)); err != nil {
			return []int{}
		}
		return ChildrenFromTable(parent)
	}
	slices.Sort(out)
	return out
}

// ChildrenFromTable finds a process's children by reading every process's parent, as pgrep -P does.
func ChildrenFromTable(parent int) []int {
	entries, _ := os.ReadDir("/proc")
	out := []int{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if st, ok := readStat(pid); ok && st.ppid == parent {
			out = append(out, pid)
		}
	}
	slices.Sort(out)
	return out
}

// Descendants is every process below root (root itself excluded), level by level: the children, then their children.
func Descendants(root int) []int {
	all := []int{}
	frontier := []int{root}
	for len(frontier) > 0 {
		var next []int
		for _, p := range frontier {
			next = append(next, ChildPIDs(p)...)
		}
		if len(next) == 0 {
			break
		}
		all = append(all, next...)
		frontier = next
	}
	return all
}

// WithGrandchildren is the given children followed by each one's own children: one level more, never deeper.
func WithGrandchildren(children []int, childrenOf func(int) []int) []int {
	all := append([]int(nil), children...)
	for _, c := range children {
		all = append(all, childrenOf(c)...)
	}
	return all
}

// Cmdline is a process's command line as ps prints it: arguments joined by spaces, trimmed; false when it cannot be
// read.
func Cmdline(pid int) (string, bool) {
	raw, err := os.ReadFile(dir(pid) + "/cmdline")
	if err != nil {
		return "", false
	}
	return jscompat.Trim(strings.ReplaceAll(strings.ToValidUTF8(string(raw), "�"), "\x00", " ")), true
}

// Argv is a process's arguments one by one; false when they cannot be read.
func Argv(pid int) ([]string, bool) {
	raw, err := os.ReadFile(dir(pid) + "/cmdline")
	if err != nil {
		return nil, false
	}
	s := strings.TrimSuffix(strings.ToValidUTF8(string(raw), "�"), "\x00")
	if s == "" {
		return []string{}, true
	}
	return strings.Split(s, "\x00"), true
}

// Cwd is a process's working folder, or "" when it cannot be read.
func Cwd(pid int) string {
	c, err := os.Readlink(dir(pid) + "/cwd")
	if err != nil {
		return ""
	}
	return c
}

// Environ is a process's environment as KEY=value entries; false when it cannot be read (gone, another user's).
func Environ(pid int) ([]string, bool) {
	raw, err := os.ReadFile(dir(pid) + "/environ")
	if err != nil {
		return nil, false
	}
	return strings.Split(string(raw), "\x00"), true
}

// HasEnvEntry tells whether one of the exact KEY=value entries is in the environment.
func HasEnvEntry(env []string, entries ...string) bool {
	for _, e := range env {
		if slices.Contains(entries, e) {
			return true
		}
	}
	return false
}

// Running is kill(pid, 0): a process another user owns counts as running.
func Running(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

type stat struct {
	ppid  int
	start int64 // clock ticks after boot
}

// readStat reads a process's parent and start time from /proc/<pid>/stat.
func readStat(pid int) (stat, bool) {
	raw, err := os.ReadFile(dir(pid) + "/stat")
	if err != nil {
		return stat{}, false
	}
	// The command name may hold spaces and parentheses: the fields start after the last ')'.
	s := string(raw)
	end := strings.LastIndexByte(s, ')')
	if end < 0 {
		return stat{}, false
	}
	fields := strings.Fields(s[end+1:])
	if len(fields) < 20 {
		return stat{}, false
	}
	ppid, _ := strconv.Atoi(fields[1])
	start, _ := strconv.ParseInt(fields[19], 10, 64)
	return stat{ppid: ppid, start: start}, true
}

// Parent is a process's parent pid; false when it cannot be read.
func Parent(pid int) (int, bool) {
	st, ok := readStat(pid)
	return st.ppid, ok
}

// userHZ is the unit of the times in /proc/<pid>/stat, fixed by the kernel's ABI.
const userHZ = 100

func bootTime() (int64, bool) {
	raw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			return n, err == nil
		}
	}
	return 0, false
}

// StartTime is when a process started, to the second as ps -o lstart prints it; false when it cannot be read.
func StartTime(pid int) (time.Time, bool) {
	st, ok := readStat(pid)
	if !ok {
		return time.Time{}, false
	}
	boot, ok := bootTime()
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(boot+st.start/userHZ, 0), true
}

// NewestChild is what `pgrep -n -P pid` finds: the most recently started child, or 0.
func NewestChild(parent int) int {
	entries, _ := os.ReadDir("/proc")
	best, bestStart := 0, int64(-1)
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		st, ok := readStat(pid)
		if !ok || st.ppid != parent {
			continue
		}
		if st.start > bestStart || (st.start == bestStart && pid > best) {
			best, bestStart = pid, st.start
		}
	}
	return best
}
