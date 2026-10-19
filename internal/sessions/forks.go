package sessions

import (
	"os"
	"strconv"
	"strings"
)

// ForkIDs are the session ids of the conversation forks running on this machine right now (`--fork-session` with
// `--session-id <id>`): a fork answering a side question about a conversation is not a conversation of its own and
// stays out of the lists.
func ForkIDs() map[string]bool {
	out := map[string]bool{}
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		raw, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil || !strings.Contains(string(raw), "--fork-session") {
			continue
		}
		args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		for i, a := range args {
			if a == "--session-id" && i+1 < len(args) {
				out[args[i+1]] = true
			} else if v, ok := strings.CutPrefix(a, "--session-id="); ok {
				out[v] = true
			}
		}
	}
	return out
}
