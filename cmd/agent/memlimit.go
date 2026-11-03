package main

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// memShare is the part of the unit's memory cap the Go heap may grow to before the collector works harder. The usage
// feed reads transcripts of hundreds of megabytes: its garbage, not what it keeps, is what reaches a small cap first.
const memShare = 0.7

// cgroupRoot is where the cgroup v2 hierarchy is mounted (tests move it).
var cgroupRoot = "/sys/fs/cgroup"

// softMemoryLimit keeps the heap under the unit's MemoryMax, so the kernel does not kill the agent for garbage the
// collector would have freed. GOMEMLIMIT, when set, wins; a unit without a cap is left as it is.
func softMemoryLimit() {
	if os.Getenv("GOMEMLIMIT") != "" {
		return
	}
	max := cgroupMemoryMax("/proc/self/cgroup")
	if max <= 0 {
		return
	}
	limit := int64(float64(max) * memShare)
	debug.SetMemoryLimit(limit)
	agent.Log("info", "memory limit set from the unit's cap", agent.F{K: "capBytes", V: max}, agent.F{K: "limitBytes", V: limit})
}

// cgroupMemoryMax is memory.max of this process's cgroup v2 group, 0 when there is none or it is "max".
func cgroupMemoryMax(selfCgroup string) int64 {
	raw, err := os.ReadFile(selfCgroup)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		// cgroup v2: "0::/system.slice/x.service"
		rest, ok := strings.CutPrefix(line, "0::")
		if !ok {
			continue
		}
		v, err := os.ReadFile(filepath.Join(cgroupRoot, filepath.Clean("/"+rest), "memory.max"))
		if err != nil {
			return 0
		}
		n, err := strconv.ParseInt(strings.TrimSpace(string(v)), 10, 64)
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}
