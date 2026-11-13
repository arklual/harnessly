package machines

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CPU sources, as CPUMeter.Read names the one it used.
const (
	// CPUFromCgroup: the cgroup's own usage (cpu.stat usage_usec) over the cores its cpu.max allows — a container
	// limited to fewer cores than the host shows, where /proc/stat would count the host's.
	CPUFromCgroup = "cgroup"
	// CPUFromProcStat: the busy share of the ticks in /proc/stat between two reads, as top and vmstat count it. Works
	// on a VM, on bare metal and under gVisor, whose /proc/loadavg is always zeros.
	CPUFromProcStat = "proc-stat"
	// CPUFromLoadavg: the 1-minute load over the cores; only for the first read, before there is a delta, and only
	// when the load is not the zeros of a sandbox that does not count it.
	CPUFromLoadavg = "loadavg"
)

// cpuMinGap is the shortest interval a delta is taken over: two reads closer than that (a snapshot right after a tick)
// answer the last figure instead of a rate of a few ticks.
const cpuMinGap = 500 * time.Millisecond

// CPUMeter is the machine's CPU busy share, 0..1, from the best source the machine has. It keeps the last read of each
// source to take a delta from; one meter serves every caller of the process.
type CPUMeter struct {
	Proc   string // /proc
	Cgroup string // the process's cgroup v2 folder; empty: found from /proc/self/cgroup under /sys/fs/cgroup
	Now    func() time.Time

	mu      sync.Mutex
	found   bool
	cgDir   string
	at      time.Time
	ticks   cpuTicks
	hasTick bool
	usage   float64 // cgroup usage_usec
	hasUse  bool
	last    float64
	lastSrc string
}

// DefaultCPU is the meter of this process over the real /proc and /sys/fs/cgroup.
var DefaultCPU = &CPUMeter{Proc: "/proc"}

// Read is the busy share now and the source it came from. cpus is the host's CPU count (the /proc/stat lines).
func (m *CPUMeter) Read(cpus int) (float64, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if m.Now != nil {
		now = m.Now()
	}
	if m.hasTick && now.Sub(m.at) < cpuMinGap {
		return m.last, m.lastSrc
	}
	if !m.found {
		m.cgDir, m.found = m.cgroupDir(), true
	}
	gap := now.Sub(m.at)
	prevTicks, hadTick := m.ticks, m.hasTick
	prevUse, hadUse := m.usage, m.hasUse
	m.at = now

	ticks, okTicks := m.readTicks()
	m.ticks, m.hasTick = ticks, okTicks
	usage, okUse := m.readUsage()
	m.usage, m.hasUse = usage, okUse

	frac, src := 0.0, ""
	if quota := m.quotaCores(); okUse && hadUse && quota > 0 && quota < float64(max(cpus, 1)) && gap > 0 {
		frac, src = (usage-prevUse)/(float64(gap.Microseconds())*quota), CPUFromCgroup
	} else if okTicks && hadTick && ticks.total > prevTicks.total {
		frac, src = cpuPctFromDelta(prevTicks, ticks)/100, CPUFromProcStat
	} else if load, ok := m.readLoad(); ok && load > 0 && cpus > 0 {
		frac, src = load/float64(cpus), CPUFromLoadavg
	} else if okTicks {
		// The first read under gVisor: the delta comes with the next one.
		src = CPUFromProcStat
	}
	m.last, m.lastSrc = min(1, max(0, frac)), src
	return m.last, m.lastSrc
}

func (m *CPUMeter) readTicks() (cpuTicks, bool) {
	raw, err := os.ReadFile(filepath.Join(m.Proc, "stat"))
	if err != nil {
		return cpuTicks{}, false
	}
	return parseCPUStat(string(raw))
}

func (m *CPUMeter) readLoad() (float64, bool) {
	raw, err := os.ReadFile(filepath.Join(m.Proc, "loadavg"))
	if err != nil {
		return 0, false
	}
	return parseLoadavg(string(raw))
}

// cgroupDir is the cgroup v2 folder of the process ("0::/path" in /proc/self/cgroup); empty without cgroup v2.
func (m *CPUMeter) cgroupDir() string {
	if m.Cgroup != "" {
		return m.Cgroup
	}
	raw, err := os.ReadFile(filepath.Join(m.Proc, "self", "cgroup"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if rel, ok := strings.CutPrefix(line, "0::"); ok {
			for _, dir := range []string{filepath.Join("/sys/fs/cgroup", rel), "/sys/fs/cgroup"} {
				if _, err := os.Stat(filepath.Join(dir, "cpu.max")); err == nil {
					return dir
				}
			}
		}
	}
	return ""
}

// quotaCores is the cores cpu.max allows ("<quota> <period>"); 0 when unlimited ("max") or unreadable.
func (m *CPUMeter) quotaCores() float64 {
	if m.cgDir == "" {
		return 0
	}
	raw, err := os.ReadFile(filepath.Join(m.cgDir, "cpu.max"))
	if err != nil {
		return 0
	}
	f := strings.Fields(string(raw))
	if len(f) != 2 || f[0] == "max" {
		return 0
	}
	quota, err1 := strconv.ParseFloat(f[0], 64)
	period, err2 := strconv.ParseFloat(f[1], 64)
	if err1 != nil || err2 != nil || quota <= 0 || period <= 0 {
		return 0
	}
	return quota / period
}

// readUsage is usage_usec of the cgroup's cpu.stat.
func (m *CPUMeter) readUsage() (float64, bool) {
	if m.cgDir == "" {
		return 0, false
	}
	raw, err := os.ReadFile(filepath.Join(m.cgDir, "cpu.stat"))
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(line, "usage_usec "); ok {
			n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			return n, err == nil
		}
	}
	return 0, false
}
