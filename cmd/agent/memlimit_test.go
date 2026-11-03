package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCgroupMemoryMax(t *testing.T) {
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
	if got := cgroupMemoryMax(self); got != 134217728 {
		t.Errorf("cap %d, want 134217728", got)
	}
	write(filepath.Join(unit, "memory.max"), "max\n")
	if got := cgroupMemoryMax(self); got != 0 {
		t.Errorf("no cap read as %d", got)
	}
	write(self, "12:memory:/x\n")
	if got := cgroupMemoryMax(self); got != 0 {
		t.Errorf("cgroup v1 read as %d", got)
	}
	if got := cgroupMemoryMax(filepath.Join(dir, "missing")); got != 0 {
		t.Errorf("missing file read as %d", got)
	}
}
