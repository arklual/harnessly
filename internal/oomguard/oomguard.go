// Package oomguard keeps the OOM killer's protection of the runtime from passing to the work it starts.
//
// A unit may protect the runtime with a negative oom_score_adj, so that a machine running out of memory takes an agent
// rather than the runtime every page of its person goes through. Children inherit the value: without this package every
// terminal and agent the runtime starts would be protected as well, and the kernel would have nothing to take but the
// runtime again.
package oomguard

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

// selfFile is where this process's adjustment is read.
var selfFile = "/proc/self/oom_score_adj"

var protected = sync.OnceValue(isProtected)

func isProtected() bool {
	raw, err := os.ReadFile(selfFile)
	if err != nil {
		return false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	return err == nil && n < 0
}

// reset raises the adjustment back to the kernel's default, which an unprivileged process may do, and runs the program.
const reset = `echo 0 >/proc/self/oom_score_adj 2>/dev/null; exec "$@"`

// Unprotected returns the program and arguments that run program with the kernel's default adjustment when this process
// is protected, and program itself otherwise.
func Unprotected(program string, args ...string) (string, []string) {
	if !protected() {
		return program, args
	}
	return "/bin/sh", append([]string{"-c", reset, "sh", program}, args...)
}
