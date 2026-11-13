package machines

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type cpuHost struct {
	t    *testing.T
	proc string
	cg   string
	now  time.Time
}

func newCPUHost(t *testing.T) *cpuHost {
	dir := t.TempDir()
	h := &cpuHost{t: t, proc: filepath.Join(dir, "proc"), cg: filepath.Join(dir, "cg"), now: time.Unix(1_800_000_000, 0)}
	for _, d := range []string{h.proc, h.cg} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func (h *cpuHost) write(dir, name, text string) {
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

// stat writes /proc/stat with busy and idle ticks on its cpu line and n cpuN lines.
func (h *cpuHost) stat(busy, idle int, n int) {
	text := "cpu  " + itoa(busy) + " 0 0 " + itoa(idle) + " 0 0 0 0 0 0\n"
	for i := 0; i < n; i++ {
		text += "cpu" + itoa(i) + " 0 0 0 0 0 0 0 0 0 0\n"
	}
	h.write(h.proc, "stat", text)
}

func itoa(n int) string { return string(appendInt(nil, n)) }

func appendInt(b []byte, n int) []byte {
	if n >= 10 {
		b = appendInt(b, n/10)
	}
	return append(b, byte('0'+n%10))
}

func (h *cpuHost) meter(withCgroup bool) *CPUMeter {
	m := &CPUMeter{Proc: h.proc, Now: func() time.Time { return h.now }}
	if withCgroup {
		m.Cgroup = h.cg
	}
	return m
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// gVisor: /proc/loadavg is always zeros, /proc/stat counts the work.
func TestCPUMeterUnderGVisorTakesProcStat(t *testing.T) {
	h := newCPUHost(t)
	h.write(h.proc, "loadavg", "0.00 0.00 0.00 0/0 0\n")
	h.stat(100, 900, 16)
	m := h.meter(false)
	if frac, src := m.Read(16); frac != 0 || src != CPUFromProcStat {
		t.Fatalf("first read = %v %q, want 0 from proc-stat (no delta yet, the zeros are no load)", frac, src)
	}
	h.now = h.now.Add(2 * time.Second)
	h.stat(130, 1170, 16) // 30 busy of 300
	if frac, src := m.Read(16); !approx(frac, 0.1) || src != CPUFromProcStat {
		t.Fatalf("second read = %v %q, want 0.1 from proc-stat", frac, src)
	}
}

// A container limited to 2 of the host's 16 cores: its own usage over the 2.
func TestCPUMeterInLimitedContainerTakesCgroup(t *testing.T) {
	h := newCPUHost(t)
	h.write(h.proc, "loadavg", "12.00 10.00 8.00 3/400 1\n")
	h.stat(100, 900, 16)
	h.write(h.cg, "cpu.max", "200000 100000\n")
	h.write(h.cg, "cpu.stat", "usage_usec 1000000\nuser_usec 1\n")
	m := h.meter(true)
	if _, src := m.Read(16); src != CPUFromLoadavg {
		t.Fatalf("first read from %q, want loadavg before any delta", src)
	}
	h.now = h.now.Add(time.Second)
	h.stat(900, 1100, 16)                             // the host's work, not the container's
	h.write(h.cg, "cpu.stat", "usage_usec 2000000\n") // 1 s of CPU in 1 s over 2 cores
	if frac, src := m.Read(16); !approx(frac, 0.5) || src != CPUFromCgroup {
		t.Fatalf("read = %v %q, want 0.5 from cgroup", frac, src)
	}
}

// A VM: no quota (cpu.max "max"), /proc/stat deltas.
func TestCPUMeterOnVMTakesProcStat(t *testing.T) {
	h := newCPUHost(t)
	h.write(h.proc, "loadavg", "2.00 1.00 1.00 1/100 1\n")
	h.stat(0, 0, 4)
	h.write(h.cg, "cpu.max", "max 100000\n")
	h.write(h.cg, "cpu.stat", "usage_usec 0\n")
	m := h.meter(true)
	if frac, src := m.Read(4); !approx(frac, 0.5) || src != CPUFromLoadavg {
		t.Fatalf("first read = %v %q, want 0.5 from loadavg", frac, src)
	}
	h.now = h.now.Add(time.Second)
	h.stat(300, 100, 4)
	if frac, src := m.Read(4); !approx(frac, 0.75) || src != CPUFromProcStat {
		t.Fatalf("read = %v %q, want 0.75 from proc-stat", frac, src)
	}
	// A read right after: the last figure, not a rate over a few ticks.
	h.now = h.now.Add(100 * time.Millisecond)
	h.stat(310, 100, 4)
	if frac, _ := m.Read(4); !approx(frac, 0.75) {
		t.Fatalf("read within the gap = %v, want the last 0.75", frac)
	}
}

func TestHostStatsCPUShareFallsBackToLoad(t *testing.T) {
	if got := (HostStats{CPUs: 4, Load1: 2}).CPUShare(); !approx(got, 0.5) {
		t.Fatalf("no meter: %v, want 0.5", got)
	}
	v := 0.25
	if got := (HostStats{CPUs: 4, Load1: 0, CPU: &v}).CPUShare(); !approx(got, 0.25) {
		t.Fatalf("meter: %v, want 0.25", got)
	}
}
