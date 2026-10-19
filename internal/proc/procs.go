package proc

import (
	"strings"
	"time"
)

// Procs is the process table as the agent detectors read it: the machine's by default, a table of a test's own
// otherwise. A field left nil reads as "nothing there", never as the machine.
type Procs struct {
	Children func(pid int) []int
	// Args is the command line as ps prints it.
	Args func(pid int) (string, bool)
	// Argv is the arguments one by one; when nil they are the space-separated words of Args.
	Argv      func(pid int) ([]string, bool)
	Cwd       func(pid int) string
	Environ   func(pid int) ([]string, bool)
	StartTime func(pid int) (time.Time, bool)
}

// System reads this machine's /proc.
func System() Procs {
	return Procs{Children: ChildPIDs, Args: Cmdline, Argv: Argv, Cwd: Cwd, Environ: Environ, StartTime: StartTime}
}

// ChildrenOf is p.Children, empty when unset.
func (p Procs) ChildrenOf(pid int) []int {
	if p.Children == nil {
		return nil
	}
	return p.Children(pid)
}

// ArgsOf is p.Args, false when unset.
func (p Procs) ArgsOf(pid int) (string, bool) {
	if p.Args == nil {
		return "", false
	}
	return p.Args(pid)
}

// ArgvOf is p.Argv, or the words of p.Args when Argv is unset.
func (p Procs) ArgvOf(pid int) ([]string, bool) {
	if p.Argv != nil {
		return p.Argv(pid)
	}
	args, ok := p.ArgsOf(pid)
	if !ok {
		return nil, false
	}
	return strings.Fields(args), true
}

// CwdOf is p.Cwd, "" when unset.
func (p Procs) CwdOf(pid int) string {
	if p.Cwd == nil {
		return ""
	}
	return p.Cwd(pid)
}

// EnvironOf is p.Environ, false when unset.
func (p Procs) EnvironOf(pid int) ([]string, bool) {
	if p.Environ == nil {
		return nil, false
	}
	return p.Environ(pid)
}

// StartTimeOf is p.StartTime, false when unset.
func (p Procs) StartTimeOf(pid int) (time.Time, bool) {
	if p.StartTime == nil {
		return time.Time{}, false
	}
	return p.StartTime(pid)
}

// WithGrandchildren is the given children followed by each one's children.
func (p Procs) WithGrandchildren(children []int) []int {
	return WithGrandchildren(children, p.ChildrenOf)
}
