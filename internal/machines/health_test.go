package machines

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

type healthHost struct {
	t           *testing.T
	root        string
	now         time.Time
	total, free float64
	busy, idle  float64 // cumulative ticks
	usedPct     float64
	swapIn      float64
}

func newHealthHost(t *testing.T) *healthHost {
	return &healthHost{t: t, root: t.TempDir(), now: time.UnixMilli(1792656000000), total: 100 * gib, free: 50 * gib, usedPct: 40}
}

func (f *healthHost) write() {
	f.t.Helper()
	const totalKB = 16000000
	files := map[string]string{
		"stat":    fmt.Sprintf("cpu  %.0f 0 0 %.0f 0 0 0 0 0 0\n", f.busy, f.idle),
		"meminfo": fmt.Sprintf("MemTotal: %d kB\nMemAvailable: %.0f kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n", totalKB, totalKB*(1-f.usedPct/100)),
		"vmstat":  fmt.Sprintf("pswpin %.0f\n", f.swapIn),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(f.root, name), []byte(body), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
}

// step advances the clock by d with the CPU this busy over it, and samples.
func (f *healthHost) step(h *HealthMonitor, d time.Duration, busyShare float64) wire.MachineHealth {
	f.now = f.now.Add(d)
	ticks := d.Seconds() * 100
	f.busy += ticks * busyShare
	f.idle += ticks * (1 - busyShare)
	f.write()
	return h.Sample()
}

func (f *healthHost) monitor() *HealthMonitor {
	f.write()
	return NewHealthMonitor(HealthDeps{ProcRoot: f.root, Path: "/", Now: func() time.Time { return f.now },
		Disk: func(string) (float64, float64, error) { return f.total, f.free, nil }})
}

func levelOf(h wire.MachineHealth, m wire.HealthMetric) wire.HealthLevel {
	for _, i := range h.Issues {
		if i.Metric == m {
			return i.Level
		}
	}
	return wire.HealthOK
}

func TestHealthDisk(t *testing.T) {
	f := newHealthHost(t)
	f.total = 260 * gib
	h := f.monitor()
	cases := []struct {
		free float64
		want wire.HealthLevel
	}{
		{100 * gib, wire.HealthOK},
		{25 * gib, wire.HealthWarn},     // under 10 %
		{12 * gib, wire.HealthCritical}, // under 5 %
		{14 * gib, wire.HealthCritical}, // within the margin: stays
		{2.6 * gib, wire.HealthCritical},
		{20 * gib, wire.HealthWarn}, // past the critical margin, under the warn line
		{27 * gib, wire.HealthWarn}, // over 10 %, within the margin
		{40 * gib, wire.HealthOK},   // past the margin
		{2.9 * gib, wire.HealthCritical},
	}
	for i, c := range cases {
		f.free = c.free
		got := f.step(h, HealthEvery, 0.1)
		if levelOf(got, wire.HealthDisk) != c.want || got.Level != c.want {
			t.Fatalf("case %d (%.1f GiB free): %+v, want %s", i, c.free/gib, got, c.want)
		}
		if err := got.Validate(); err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	// A small disk: the absolute line rules.
	f.total, f.free = 20*gib, 2.5*gib
	if got := f.step(h, HealthEvery, 0.1); levelOf(got, wire.HealthDisk) != wire.HealthCritical {
		t.Fatalf("2.5 of 20 GiB: %+v", got)
	}
}

func TestHealthMemoryAndSwap(t *testing.T) {
	f := newHealthHost(t)
	h := f.monitor()
	steps := []struct {
		used, swapPages float64
		want            wire.HealthLevel
	}{
		{50, 0, wire.HealthOK},
		{86, 0, wire.HealthWarn},
		{96, 0, wire.HealthCritical},
		{93, 0, wire.HealthCritical}, // within 3 points: stays
		{90, 0, wire.HealthWarn},
		{83, 0, wire.HealthWarn},
		{80, 0, wire.HealthOK},
		{60, 5 * 600, wire.HealthCritical}, // 600 pages/s swapped in over 5 s
		{60, 0, wire.HealthOK},
	}
	for i, s := range steps {
		f.usedPct = s.used
		f.swapIn += s.swapPages
		if got := levelOf(f.step(h, HealthEvery, 0.1), wire.HealthMem); got != s.want {
			t.Fatalf("step %d: %s, want %s", i, got, s.want)
		}
	}
}

func TestHealthCPUNeedsTwoMinutes(t *testing.T) {
	f := newHealthHost(t)
	h := f.monitor()
	for i := 0; i < 23; i++ { // 115 s at full load: not yet
		if got := levelOf(f.step(h, HealthEvery, 1), wire.HealthCPU); got != wire.HealthOK {
			t.Fatalf("after %d s: %s", (i+1)*5, got)
		}
	}
	for i := 0; i < 2; i++ {
		f.step(h, HealthEvery, 1)
	}
	if got := f.step(h, HealthEvery, 1); levelOf(got, wire.HealthCPU) != wire.HealthWarn || got.Level != wire.HealthWarn {
		t.Fatalf("after 2 min busy: %+v", got)
	}
	// One quiet sample does not end it; a quiet window does.
	if got := levelOf(f.step(h, HealthEvery, 0), wire.HealthCPU); got != wire.HealthWarn {
		t.Fatalf("one quiet sample: %s", got)
	}
	for i := 0; i < 30; i++ {
		f.step(h, HealthEvery, 0.2)
	}
	if got := levelOf(f.step(h, HealthEvery, 0.2), wire.HealthCPU); got != wire.HealthOK {
		t.Fatalf("after a quiet window: %s", got)
	}
	if len(h.cpu) > 30 {
		t.Fatalf("the history keeps %d samples", len(h.cpu))
	}
}

func TestHealthIssuesWorstFirst(t *testing.T) {
	f := newHealthHost(t)
	h := f.monitor()
	f.usedPct, f.free = 88, 2*gib
	got := f.step(h, HealthEvery, 0.1)
	want := []wire.HealthIssue{{Metric: wire.HealthDisk, Level: wire.HealthCritical}, {Metric: wire.HealthMem, Level: wire.HealthWarn}}
	if got.Level != wire.HealthCritical || fmt.Sprint(got.Issues) != fmt.Sprint(want) || got.Since != f.now.UnixMilli() {
		t.Fatalf("%+v", got)
	}
	// The level holds: since stays where it came to it.
	came := got.Since
	if again := f.step(h, HealthEvery, 0.1); again.Since != came {
		t.Fatalf("since moved: %+v", again)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	f.usedPct, f.free = 40, 50*gib
	if got := f.step(h, HealthEvery, 0.1); got.Level != wire.HealthOK || got.Issues == nil || len(got.Issues) != 0 {
		t.Fatalf("%+v", got)
	}
}
