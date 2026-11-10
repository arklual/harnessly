package screen

import "strings"

// Claude Code's modes as its screen says them (HRN-945): the permission mode on the footer under the prompt box
// («⏸ plan mode on (shift+tab to cycle)»), and the answers of /fast.

// Claude Code's permission modes as its footer names them; ClaudeModeDefault is a footer that names none.
const (
	ClaudeModeDefault     = "default"
	ClaudeModeAcceptEdits = "acceptEdits"
	ClaudeModePlan        = "plan"
	ClaudeModeAuto        = "auto"
	ClaudeModeBypass      = "bypassPermissions"
)

var (
	claudeModeMarks = []struct{ re, mode string }{
		{`plan mode on`, ClaudeModePlan},
		{`accept edits on`, ClaudeModeAcceptEdits},
		{`auto mode on`, ClaudeModeAuto},
		{`bypass permissions on`, ClaudeModeBypass},
		{`manual mode on`, ClaudeModeDefault},
	}
	fastOnRE      = re(`(?m)^\W*Fast mode ON\b`)
	fastOffRE     = re(`(?m)^\W*(Kept )?Fast mode OFF\b`)
	fastRefusedRE = re(`Fast mode is not available|Fast mode unavailable`)
)

// ClaudePermissionMode is the permission mode the footer under Claude's prompt box names, ClaudeModeDefault when it
// names none; ok is false when no prompt box is drawn (the footer cannot be told from the history then). Only the
// lines under the box's last border are read: the history above may quote any of the words.
func ClaudePermissionMode(plain string) (mode string, ok bool) {
	lines := trimEndLines(plain)
	border := -1
	for i, l := range lines {
		if t := trim(l); strings.Count(t, "─") >= 20 && strings.Trim(t, "─") == "" {
			border = i
		}
	}
	if border < 0 {
		return "", false
	}
	for _, l := range lines[border+1:] {
		for _, m := range claudeModeMarks {
			if strings.Contains(l, m.re) {
				return m.mode, true
			}
		}
	}
	return ClaudeModeDefault, true
}

// CountFast counts Claude's answers of /fast that leave the mode on, or off.
func CountFast(plain string, on bool) int {
	if on {
		return len(fastOnRE.FindAllStringIndex(plain, -1))
	}
	return len(fastOffRE.FindAllStringIndex(plain, -1))
}

// CountFastRefused counts Claude's answers that fast mode cannot run (another model, an account without it).
func CountFastRefused(plain string) int {
	return len(fastRefusedRE.FindAllStringIndex(plain, -1))
}
