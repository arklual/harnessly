package proc

import (
	"os"
	"slices"
	"syscall"
	"time"
)

// KillTree stops every process below root, keeping root itself (a pane's shell) and this process: the tree is taken
// before the first signal, so a background shell reparented to init when its parent exits is still in it. SIGTERM
// first; survivors are polled every poll for up to grace, then SIGKILL goes to them and to anything that appeared below
// root since. It returns how many processes the first tree held. sleep waits between polls (time.Sleep when nil).
func KillTree(root int, grace, poll time.Duration, sleep func(time.Duration)) int {
	if sleep == nil {
		sleep = time.Sleep
	}
	self := os.Getpid()
	treeOf := func() []int {
		return slices.DeleteFunc(Descendants(root), func(p int) bool { return p == self || p == root })
	}
	tree := treeOf()
	if len(tree) == 0 {
		return 0
	}
	signal(tree, syscall.SIGTERM)
	for waited := time.Duration(0); waited < grace && len(alive(tree)) > 0; waited += poll {
		sleep(poll)
	}
	rest := alive(tree)
	for _, p := range treeOf() {
		if !slices.Contains(rest, p) {
			rest = append(rest, p)
		}
	}
	signal(rest, syscall.SIGKILL)
	return len(tree)
}

func signal(pids []int, sig syscall.Signal) {
	for _, p := range pids {
		_ = syscall.Kill(p, sig)
	}
}

// alive are the pids kill(pid, 0) still reaches (a pid owned by another user counts as gone, as for the signal
// itself it would fail).
func alive(pids []int) []int {
	var out []int
	for _, p := range pids {
		if syscall.Kill(p, 0) == nil {
			out = append(out, p)
		}
	}
	return out
}
