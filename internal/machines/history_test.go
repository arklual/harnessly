package machines

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeHost writes the /proc files the history reads: two cores and one real link, a loopback and a container's pair.
func fakeHost(t *testing.T, root string, c0, c1 [2]float64, avail, rx, tx int) {
	t.Helper()
	line := func(name string, busy, idle float64) string {
		return fmt.Sprintf("%s %.0f 0 0 %.0f 0 0 0 0 0 0\n", name, busy, idle)
	}
	stat := line("cpu", c0[0]+c1[0], c0[1]+c1[1]) + line("cpu0", c0[0], c0[1]) + line("cpu1", c1[0], c1[1]) + "intr 1\n"
	mem := fmt.Sprintf("MemTotal: 1000 kB\nMemFree: 1 kB\nMemAvailable: %d kB\n", avail)
	net := "Inter-|   Receive\n face |bytes\n" +
		fmt.Sprintf("    lo: 999 0 0 0 0 0 0 0 999 0 0 0 0 0 0 0\n  eth0: %d 0 0 0 0 0 0 0 %d 0 0 0 0 0 0 0\nveth1: 5000 0 0 0 0 0 0 0 5000 0 0 0 0 0 0 0\n", rx, tx)
	for name, body := range map[string]string{"stat": stat, "meminfo": mem, "net/dev": net} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestHistoryReadsCoresMemoryAndNetwork(t *testing.T) {
	root := t.TempDir()
	now := time.UnixMilli(1_700_000_000_000)
	h := NewHistory(HistoryDeps{ProcRoot: root, Now: func() time.Time { return now }})
	fakeHost(t, root, [2]float64{0, 100}, [2]float64{0, 100}, 600, 1000, 500)
	h.Sample()
	if _, ok := h.Latest(); ok {
		t.Fatal("the first reading has nothing to compare with")
	}
	now = now.Add(5 * time.Second)
	// core 0 did 75 ticks of work in 100, core 1 25; 5 s of traffic: 5000 B in, 2500 B out.
	fakeHost(t, root, [2]float64{75, 125}, [2]float64{25, 175}, 400, 6000, 3000)
	h.Sample()
	s, ok := h.Latest()
	if !ok {
		t.Fatal("no reading")
	}
	if !near(s.CPU, 0.5) || len(s.Cores) != 2 || !near(s.Cores[0], 0.75) || !near(s.Cores[1], 0.25) {
		t.Fatalf("cpu %v cores %v", s.CPU, s.Cores)
	}
	if s.MemTotal != 1000*1024 || s.MemUsed != 600*1024 {
		t.Fatalf("memory %d of %d", s.MemUsed, s.MemTotal)
	}
	if !near(s.RxPerSec, 1000) || !near(s.TxPerSec, 500) {
		t.Fatalf("network %v/%v (loopback and veth must not count)", s.RxPerSec, s.TxPerSec)
	}
}

func TestHistorySeriesThinsTheHour(t *testing.T) {
	root := t.TempDir()
	now := time.UnixMilli(1_700_000_000_000)
	h := NewHistory(HistoryDeps{ProcRoot: root, Now: func() time.Time { return now }})
	busyTicks, idleTicks := 0.0, 0.0
	step := func(busy float64) {
		busyTicks += busy
		idleTicks += 100 - busy
		fakeHost(t, root, [2]float64{busyTicks, idleTicks}, [2]float64{busyTicks, idleTicks}, 500, 0, 0)
		h.Sample()
	}
	step(0)
	// 30 minutes at 20% then 30 minutes at 80%, a reading every 5 s.
	for i := 0; i < 360; i++ {
		now = now.Add(HistoryEvery)
		step(20)
	}
	for i := 0; i < 360; i++ {
		now = now.Add(HistoryEvery)
		step(80)
	}
	got := h.Series(30, 2*time.Minute)
	if len(got) != 30 {
		t.Fatalf("%d points", len(got))
	}
	if !near(got[0].CPU, 0.2) || !near(got[29].CPU, 0.8) {
		t.Fatalf("first %v last %v (cpu is the busy share of the cores)", got[0].CPU, got[29].CPU)
	}
	if got[29].At != now.UnixMilli() || got[28].At != now.UnixMilli()-120_000 {
		t.Fatalf("points end at %d and %d, now is %d", got[28].At, got[29].At, now.UnixMilli())
	}
	if !near(got[15].Mem, 0.5) {
		t.Fatalf("memory %v", got[15].Mem)
	}
}

func TestHistorySeriesSkipsStepsNobodyWatched(t *testing.T) {
	root := t.TempDir()
	now := time.UnixMilli(1_700_000_000_000)
	h := NewHistory(HistoryDeps{ProcRoot: root, Now: func() time.Time { return now }})
	fakeHost(t, root, [2]float64{0, 100}, [2]float64{0, 100}, 500, 0, 0)
	h.Sample()
	// Ten minutes in: points exist only for the steps the runtime was running.
	for i := 0; i < 120; i++ {
		now = now.Add(HistoryEvery)
		fakeHost(t, root, [2]float64{float64(i+1) * 10, float64(i+1) * 90}, [2]float64{0, float64(i+1) * 100}, 500, 0, 0)
		h.Sample()
	}
	got := h.Series(30, 2*time.Minute)
	if len(got) < 5 || len(got) > 6 {
		t.Fatalf("%d points for ten minutes", len(got))
	}
	if h.Series(0, time.Minute) == nil {
		t.Fatal("an empty series is [], not null")
	}
}

func TestHistoryKeepsAnHour(t *testing.T) {
	root := t.TempDir()
	now := time.UnixMilli(1_700_000_000_000)
	h := NewHistory(HistoryDeps{ProcRoot: root, Now: func() time.Time { return now }})
	for i := 0; i < 900; i++ {
		fakeHost(t, root, [2]float64{float64(i), float64(i) * 2}, [2]float64{0, float64(i)}, 500, 0, 0)
		h.Sample()
		now = now.Add(HistoryEvery)
	}
	h.mu.Lock()
	n := len(h.ring)
	h.mu.Unlock()
	if n > 721 || n < 719 {
		t.Fatalf("%d readings kept for an hour at 5 s", n)
	}
}
