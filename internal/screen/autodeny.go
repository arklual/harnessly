package screen

import (
	"strconv"
	"strings"
)

// autoDenyRE is the warning of Claude Code 2.1.311 on a permission prompt of an unattended session: «Claude Code will
// automatically deny this request in 1:17, to avoid blocking progress on an unattended session». The pane wraps it
// anywhere, so it is read off the text with its lines joined.
var autoDenyRE = re(`automatically` + sp + `+deny` + sp + `+this` + sp + `+request` + sp + `+in` + sp + `+(?:(\d+):)?(\d{1,2})`)

// AutoDenySeconds is how long Claude says it waits before it denies the request on the screen by itself ("1:17" is 77,
// "45" is 45); false when the screen says nothing of the kind. Only the last 40 lines are read.
func AutoDenySeconds(plain string) (int, bool) {
	lines := strings.Split(plain, "\n")
	m := autoDenyRE.FindStringSubmatch(strings.Join(lines[max(0, len(lines)-40):], "\n"))
	if m == nil {
		return 0, false
	}
	secs, _ := strconv.Atoi(m[2])
	if m[1] != "" {
		mins, _ := strconv.Atoi(m[1])
		secs += mins * 60
	}
	return secs, true
}
