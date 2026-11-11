package memlimit

import (
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
)

func TestCgroupMax(t *testing.T) {
	dir := t.TempDir()
	old := cgroupRoot
	cgroupRoot = dir
	t.Cleanup(func() { cgroupRoot = old })

	unit := filepath.Join(dir, "system.slice", "a.service")
	if err := os.MkdirAll(unit, 0o755); err != nil {
		t.Fatal(err)
	}
	self := filepath.Join(dir, "self")
	write := func(path, s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(self, "0::/system.slice/a.service\n")

	write(filepath.Join(unit, "memory.max"), "134217728\n")
	if got := CgroupMax(self); got != 134217728 {
		t.Errorf("cap %d, want 134217728", got)
	}
	write(filepath.Join(unit, "memory.max"), "max\n")
	if got := CgroupMax(self); got != 0 {
		t.Errorf("no cap read as %d", got)
	}
	write(self, "12:memory:/x\n")
	if got := CgroupMax(self); got != 0 {
		t.Errorf("cgroup v1 read as %d", got)
	}
	if got := CgroupMax(filepath.Join(dir, "missing")); got != 0 {
		t.Errorf("missing file read as %d", got)
	}
}

func TestMachineTotal(t *testing.T) {
	file := filepath.Join(t.TempDir(), "meminfo")
	if err := os.WriteFile(file, []byte("MemFree:  1 kB\nMemTotal:       24608564 kB\nMemAvailable: 2 kB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := MachineTotal(file); got != 24608564<<10 {
		t.Errorf("total %d", got)
	}
	if got := MachineTotal(filepath.Join(t.TempDir(), "missing")); got != 0 {
		t.Errorf("missing file read as %d", got)
	}
}

func TestLimit(t *testing.T) {
	const mib, gib = int64(1) << 20, int64(1) << 30
	unit := Policy{CapShare: 0.5}
	machine := Policy{CapShare: 0.5, MachineDiv: 48, Min: 384 * mib, Max: 2 * gib}
	for _, c := range []struct {
		name       string
		p          Policy
		cap, mem   int64
		want       int64
		wantSource string
	}{
		{"a unit's cap alone", unit, 512 * mib, 24 * gib, 256 * mib, "unit"},
		{"no cap, no limit", unit, 0, 24 * gib, 0, ""},
		{"the machine's share", machine, 0, 24 * gib, 512 * mib, "machine"},
		{"a small machine keeps the floor", machine, 0, 2 * gib, 384 * mib, "machine"},
		{"a large machine stops at the ceiling", machine, 0, 512 * gib, 2 * gib, "machine"},
		{"a tight cap wins over the machine", machine, 400 * mib, 24 * gib, 200 * mib, "unit"},
		{"a loose cap loses to the machine", machine, 64 * gib, 24 * gib, 512 * mib, "machine"},
		{"nothing known", machine, 0, 0, 0, ""},
	} {
		if got, source := c.p.Limit(c.cap, c.mem); got != c.want || source != c.wantSource {
			t.Errorf("%s: %d from %q, want %d from %q", c.name, got, source, c.want, c.wantSource)
		}
	}
}

func TestApply(t *testing.T) {
	dir := t.TempDir()
	oldRoot, oldSelf, oldInfo := cgroupRoot, selfCgroup, meminfo
	before := debug.SetMemoryLimit(-1)
	t.Cleanup(func() {
		cgroupRoot, selfCgroup, meminfo = oldRoot, oldSelf, oldInfo
		debug.SetMemoryLimit(before)
	})
	cgroupRoot, selfCgroup, meminfo = dir, filepath.Join(dir, "self"), filepath.Join(dir, "meminfo")
	if err := os.WriteFile(meminfo, []byte("MemTotal: 8388608 kB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Policy{MachineDiv: 8}

	// GOMEMLIMIT is the person's word: the policy does not touch the limit.
	t.Setenv("GOMEMLIMIT", "123MiB")
	debug.SetMemoryLimit(123 << 20)
	Apply(p)
	if got := debug.SetMemoryLimit(-1); got != 123<<20 {
		t.Fatalf("GOMEMLIMIT overridden: %d", got)
	}

	t.Setenv("GOMEMLIMIT", "")
	debug.SetMemoryLimit(math.MaxInt64)
	Apply(p)
	if got := debug.SetMemoryLimit(-1); got != 1<<30 {
		t.Fatalf("limit %d, want an eighth of the machine", got)
	}

	// Nothing to go by leaves the limit alone.
	debug.SetMemoryLimit(math.MaxInt64)
	meminfo = filepath.Join(dir, "missing")
	Apply(p)
	if got := debug.SetMemoryLimit(-1); got != math.MaxInt64 {
		t.Fatalf("limit %d set from nothing", got)
	}
}
