package machines

import (
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The machine's health: whether its disk, memory or CPU is about to break the work on it. The runtime judges it itself,
// so every page and the cluster read one verdict. Each metric keeps its own level with a margin to leave it, so a value
// on the line does not flap; CPU is judged over a window, never one sample.

const gib = float64(1 << 30)

// HealthEvery is how often the health is sampled.
const HealthEvery = 5 * time.Second

type diskLine struct{ freeShare, freeBytes float64 }

type healthTable struct {
	// Disk: a level when the free part is below either line; it is left once both are exitFactor above it.
	diskWarn, diskCritical diskLine
	diskExitFactor         float64
	// Memory: used share (MemAvailable) in percent, and swap-in in pages per second; a level is left memExitPct below
	// its line and below half its swap-in rate.
	memWarnPct, memCriticalPct float64
	memExitPct                 float64
	swapInWarn, swapInCritical float64
	// CPU: the busy share over the last cpuWindow, warn only; left below cpuExitPct.
	cpuWarnPct, cpuExitPct float64
	cpuWindow              time.Duration
}

// HealthLimits are the lines of the health, the only table of them: the pages show what the runtime judged.
var HealthLimits = healthTable{
	diskWarn:       diskLine{freeShare: 0.10, freeBytes: 10 * gib},
	diskCritical:   diskLine{freeShare: 0.05, freeBytes: 3 * gib},
	diskExitFactor: 1.2,
	memWarnPct:     85,
	memCriticalPct: 95,
	memExitPct:     3,
	swapInWarn:     50,
	swapInCritical: 500,
	cpuWarnPct:     90,
	cpuExitPct:     80,
	cpuWindow:      2 * time.Minute,
}

// HealthDeps is what the health reads; tests give it a /proc and a disk of their own.
type HealthDeps struct {
	ProcRoot string
	// Path is a folder on the disk to judge (the runtime's data folder).
	Path string
	Now  func() time.Time
	// Disk is the total and free (available to the user) bytes of the filesystem of path.
	Disk func(path string) (total, free float64, err error)
}

type cpuAt struct {
	at    time.Time
	ticks cpuTicks
}

// HealthMonitor samples the metrics and keeps their levels; one goroutine calls Sample.
type HealthMonitor struct {
	d      HealthDeps
	levels map[wire.HealthMetric]wire.HealthLevel
	cpu    []cpuAt
	swapIn *float64
	swapAt time.Time
	level  wire.HealthLevel
	since  time.Time
}

func NewHealthMonitor(d HealthDeps) *HealthMonitor {
	if d.ProcRoot == "" {
		d.ProcRoot = "/proc"
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Disk == nil {
		d.Disk = diskBytes
	}
	return &HealthMonitor{d: d, levels: map[wire.HealthMetric]wire.HealthLevel{}}
}

func diskBytes(path string) (total, free float64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(filepath.Clean(path), &st); err != nil {
		return 0, 0, err
	}
	return float64(st.Blocks) * float64(st.Bsize), float64(st.Bavail) * float64(st.Bsize), nil
}

func (h *HealthMonitor) read(name string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(h.d.ProcRoot, name))
	return string(raw), err == nil
}

// Sample reads the metrics now and answers the health. A metric that cannot be read keeps its level.
func (h *HealthMonitor) Sample() wire.MachineHealth {
	now := h.d.Now()
	if total, free, err := h.d.Disk(h.d.Path); err == nil && total > 0 {
		h.levels[wire.HealthDisk] = diskLevel(free, total, h.levels[wire.HealthDisk])
	}
	if raw, ok := h.read("meminfo"); ok {
		if mem, ok := parseMeminfo(raw); ok {
			h.levels[wire.HealthMem] = memLevel(mem.usedPct, h.swapInRate(now), h.levels[wire.HealthMem])
		}
	}
	if raw, ok := h.read("stat"); ok {
		if ticks, ok := parseCPUStat(raw); ok {
			if pct, ok := h.cpuOverWindow(now, ticks); ok {
				h.levels[wire.HealthCPU] = cpuLevel(pct, h.levels[wire.HealthCPU])
			}
		}
	}
	return h.verdict(now)
}

// swapInRate is the pages swapped in per second since the previous sample; nil without one.
func (h *HealthMonitor) swapInRate(now time.Time) *float64 {
	raw, ok := h.read("vmstat")
	if !ok {
		return nil
	}
	v, ok := parseVmstatSwapIn(raw)
	if !ok {
		return nil
	}
	prev, prevAt := h.swapIn, h.swapAt
	h.swapIn, h.swapAt = &v, now
	seconds := now.Sub(prevAt).Seconds()
	if prev == nil || seconds <= 0 {
		return nil
	}
	rate := max(0, v-*prev) / seconds
	return &rate
}

// cpuOverWindow is the busy share since the newest sample at least cpuWindow old; false until the history is that long.
func (h *HealthMonitor) cpuOverWindow(now time.Time, ticks cpuTicks) (float64, bool) {
	h.cpu = append(h.cpu, cpuAt{now, ticks})
	edge := now.Add(-HealthLimits.cpuWindow)
	base := -1
	for i, s := range h.cpu {
		if !s.at.After(edge) {
			base = i
		}
	}
	if base < 0 {
		return 0, false
	}
	h.cpu = h.cpu[base:]
	return cpuPctFromDelta(h.cpu[0].ticks, ticks), true
}

func (h *HealthMonitor) verdict(now time.Time) wire.MachineHealth {
	out := wire.MachineHealth{Level: wire.HealthOK, Issues: []wire.HealthIssue{}}
	for _, worst := range []wire.HealthLevel{wire.HealthCritical, wire.HealthWarn} {
		for _, m := range wire.HealthMetrics {
			if h.levels[m] == worst {
				out.Issues = append(out.Issues, wire.HealthIssue{Metric: m, Level: worst})
			}
		}
	}
	if len(out.Issues) > 0 {
		out.Level = out.Issues[0].Level
	}
	if h.since.IsZero() || out.Level != h.level {
		h.level, h.since = out.Level, now
	}
	out.Since = h.since.UnixMilli()
	return out
}

func held(prev, at wire.HealthLevel) bool {
	return prev == at || (at == wire.HealthWarn && prev == wire.HealthCritical)
}

func diskLevel(free, total float64, prev wire.HealthLevel) wire.HealthLevel {
	share := free / total
	below := func(l diskLine, factor float64) bool { return share < l.freeShare*factor || free < l.freeBytes*factor }
	t := HealthLimits
	switch {
	case below(t.diskCritical, 1), prev == wire.HealthCritical && below(t.diskCritical, t.diskExitFactor):
		return wire.HealthCritical
	case below(t.diskWarn, 1), held(prev, wire.HealthWarn) && below(t.diskWarn, t.diskExitFactor):
		return wire.HealthWarn
	}
	return wire.HealthOK
}

func memLevel(usedPct float64, swapIn *float64, prev wire.HealthLevel) wire.HealthLevel {
	t := HealthLimits
	swap := 0.0
	if swapIn != nil {
		swap = *swapIn
	}
	over := func(pct, rate float64, holding bool) bool {
		if holding {
			return usedPct >= pct-t.memExitPct || swap >= rate/2
		}
		return usedPct >= pct || swap >= rate
	}
	switch {
	case over(t.memCriticalPct, t.swapInCritical, prev == wire.HealthCritical):
		return wire.HealthCritical
	case over(t.memWarnPct, t.swapInWarn, held(prev, wire.HealthWarn)):
		return wire.HealthWarn
	}
	return wire.HealthOK
}

func cpuLevel(pct float64, prev wire.HealthLevel) wire.HealthLevel {
	t := HealthLimits
	if pct >= t.cpuWarnPct || (prev == wire.HealthWarn && pct >= t.cpuExitPct) {
		return wire.HealthWarn
	}
	return wire.HealthOK
}
