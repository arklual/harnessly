package machine

import (
	"os"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/proc"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

// ProcessArgs is a process's command line as ps prints it (arguments joined by spaces); false when it cannot be read.
func ProcessArgs(pid int) (string, bool) { return proc.Cmdline(pid) }

// ProcessCwd is a process's working folder, or "" when it cannot be read.
func ProcessCwd(pid int) string { return proc.Cwd(pid) }

// ChildPIDs are a process's children in pid order, listed per thread (see proc.ChildPIDs).
func ChildPIDs(parent int) []int { return proc.ChildPIDs(parent) }

// SessionDir is a tab session's working folder: its shell's, else tmux's idea of the pane; "" when neither is known.
func SessionDir(t *tmux.Tmux, session string) string {
	if pid := t.PanePID(session); pid > 0 {
		if c := ProcessCwd(pid); c != "" {
			return c
		}
	}
	return t.PanePath(session)
}

// DirCheck is the answer to "can this folder be a workspace".
type DirCheck struct {
	Valid         bool    `json:"valid"`
	Error         *string `json:"error,omitempty"`
	SuggestedName *string `json:"suggestedName,omitempty"` // "" for the root folder
}

func dirRefused(why string) DirCheck { return DirCheck{Error: &why} }

// DirMissing is the answer for a path that cannot be looked at.
func DirMissing() DirCheck { return dirRefused("Directory does not exist") }

// ValidateDirectory checks that dir exists and is a folder, and names a workspace after it.
func ValidateDirectory(dir string) DirCheck {
	st, err := os.Stat(dir)
	if err != nil {
		return DirMissing()
	}
	if !st.IsDir() {
		return dirRefused("Please enter a directory path, not a file")
	}
	name := jscompat.Basename(dir, "")
	return DirCheck{Valid: true, SuggestedName: &name}
}
