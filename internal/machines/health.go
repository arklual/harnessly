package machines

import (
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The machine's health: whether its disk, memory or CPU is about to break the work on it. The runtime judges it itself,
// so every page and the cluster read one verdict: the smart notice, the sidebar's card and the Machine tab say the same.
// Each metric keeps its own level with a margin to leave it, so a value on the line does not flap; CPU and swap-in are
// judged over a window, never one sample; and a level is said only once it held: a short burst (the load of a busy
// machine jumps from 9 to 24 within a minute) comes and goes unsaid.

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
	// Swap-in is the rate over the last swapWindow.
	swapWindow time.Duration
	// A metric comes to a worse level once it held riseAfter, and back to a better one once that held fallAfter.
	riseAfter, fallAfter time.Duration
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
	swapWindow:     30 * time.Second,
	riseAfter:      20 * time.Second,
	fallAfter:      time.Minute,
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

type swapAt struct {
	at    time.Time
	pages float64
}

// heldLevel is a metric's level as said, and the level its figures point to since when, until that held long enough.
type heldLevel struct {
	said     wire.HealthLevel
	next     wire.HealthLevel
	nextFrom time.Time
}

// HealthMonitor samples the metrics and keeps their levels; one goroutine calls Sample.
type HealthMonitor struct {
	d HealthDeps
	// levels are what the figures say now (with their margins); said is what held and is told.
	levels map[wire.HealthMetric]wire.HealthLevel
	said   map[wire.HealthMetric]*heldLevel
	cpu    []cpuAt
	swap   []swapAt
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
	return &HealthMonitor{d: d, levels: map[wire.HealthMetric]wire.HealthLevel{}, said: map[wire.HealthMetric]*heldLevel{}}
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
	for _, m := range wire.HealthMetrics {
		h.hold(m, now)
	}
	return h.verdict(now)
}

var levelRankOf = map[wire.HealthLevel]int{wire.HealthOK: 0, wire.HealthWarn: 1, wire.HealthCritical: 2}

// hold brings the said level of a metric to what its figures say once they said it for riseAfter (worse) or fallAfter
// (better), all the while.
func (h *HealthMonitor) hold(m wire.HealthMetric, now time.Time) {
	s := h.said[m]
	if s == nil {
		s = &heldLevel{said: wire.HealthOK, next: wire.HealthOK}
		h.said[m] = s
	}
	at := h.levels[m]
	if at == "" {
		at = wire.HealthOK
	}
	if at == s.said {
		s.next = s.said
		return
	}
	if at != s.next {
		s.next, s.nextFrom = at, now
	}
	after := HealthLimits.fallAfter
	if levelRankOf[at] > levelRankOf[s.said] {
		after = HealthLimits.riseAfter
	}
	if now.Sub(s.nextFrom) >= after {
		s.said = at
	}
}

// swapInRate is the pages swapped in per second over the last swapWindow; nil until the history is that long.
func (h *HealthMonitor) swapInRate(now time.Time) *float64 {
	raw, ok := h.read("vmstat")
	if !ok {
		return nil
	}
	v, ok := parseVmstatSwapIn(raw)
	if !ok {
		return nil
	}
	h.swap = append(h.swap, swapAt{now, v})
	edge := now.Add(-HealthLimits.swapWindow)
	base := -1
	for i, s := range h.swap {
		if !s.at.After(edge) {
			base = i
		}
	}
	if base < 0 {
		return nil
	}
	h.swap = h.swap[base:]
	seconds := now.Sub(h.swap[0].at).Seconds()
	if seconds <= 0 {
		return nil
	}
	rate := max(0, v-h.swap[0].pages) / seconds
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
			if s := h.said[m]; s != nil && s.said == worst {
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
