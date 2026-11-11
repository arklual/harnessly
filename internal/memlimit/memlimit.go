// Package memlimit gives a process a soft memory limit for the Go collector, so that garbage the collector would have
// freed does not count against the unit's cap or the machine's memory.
package memlimit

import (
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// Policy says how a process's limit follows from the memory around it.
type Policy struct {
	// CapShare is the part of the unit's memory cap the Go heap may grow to before the collector works harder; a unit
	// without a cap gives no limit of its own.
	CapShare float64
	// MachineDiv, when set, also gives a limit without a cap: the machine's memory divided by it, kept between Min
	// and Max. The smaller of the two limits wins.
	MachineDiv int64
	Min, Max   int64
}

// cgroupRoot is where the cgroup v2 hierarchy is mounted, and the files below are the kernel's (tests move them).
var (
	cgroupRoot = "/sys/fs/cgroup"
	selfCgroup = "/proc/self/cgroup"
	meminfo    = "/proc/meminfo"
)

// Limit is the limit for a unit cap and a machine's memory (either 0 when unknown), and which of them gave it; 0 is
// no limit.
func (p Policy) Limit(capBytes, machineBytes int64) (limit int64, source string) {
	if capBytes > 0 && p.CapShare > 0 {
		limit, source = int64(float64(capBytes)*p.CapShare), "unit"
	}
	if machineBytes > 0 && p.MachineDiv > 0 {
		m := machineBytes / p.MachineDiv
		if p.Min > 0 && m < p.Min {
			m = p.Min
		}
		if p.Max > 0 && m > p.Max {
			m = p.Max
		}
		if limit == 0 || m < limit {
			limit, source = m, "machine"
		}
	}
	return limit, source
}

// Apply sets the limit the policy gives and logs it. GOMEMLIMIT, when set, wins: the Go runtime has applied it
// already, and it is logged as it stands.
func Apply(p Policy) {
	if os.Getenv("GOMEMLIMIT") != "" {
		if cur := debug.SetMemoryLimit(-1); cur != math.MaxInt64 {
			agent.Log("info", "memory limit set", agent.F{K: "source", V: "GOMEMLIMIT"}, agent.F{K: "limitBytes", V: cur})
		}
		return
	}
	capBytes := CgroupMax(selfCgroup)
	limit, source := p.Limit(capBytes, MachineTotal(meminfo))
	if limit <= 0 {
		return
	}
	debug.SetMemoryLimit(limit)
	agent.Log("info", "memory limit set", agent.F{K: "source", V: source}, agent.F{K: "capBytes", V: capBytes}, agent.F{K: "limitBytes", V: limit})
}

// CgroupMax is memory.max of the cgroup v2 group a /proc/<pid>/cgroup file names, 0 when there is none or it is "max".
func CgroupMax(selfCgroup string) int64 {
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

// MachineTotal is MemTotal of a /proc/meminfo file in bytes, 0 when it cannot be read (as on a system without one).
func MachineTotal(meminfo string) int64 {
	raw, err := os.ReadFile(meminfo)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(raw), "\n") {
		rest, ok := strings.CutPrefix(line, "MemTotal:")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) != 2 || f[1] != "kB" {
			return 0
		}
		n, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil {
			return 0
		}
		return n << 10
	}
	return 0
}
