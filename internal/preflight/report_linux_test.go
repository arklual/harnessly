//go:build linux

package preflight

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTo(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOSName(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "os-release")
	writeTo(t, f, "NAME=\"Ubuntu\"\nVERSION_ID=\"24.04\"\nPRETTY_NAME=\"Ubuntu 24.04.1 LTS\"\nID=ubuntu\n")
	if got := osName(f); got != "Ubuntu 24.04.1 LTS" {
		t.Errorf("%q", got)
	}
	writeTo(t, f, "PRETTY_NAME=Alpine Linux v3.20\n")
	if got := osName(f); got != "Alpine Linux v3.20" {
		t.Errorf("%q", got)
	}
	writeTo(t, f, "NAME=x\n")
	if got := osName(f); got != "Linux" {
		t.Errorf("%q", got)
	}
	if got := osName(filepath.Join(dir, "missing")); got != "Linux" {
		t.Errorf("%q", got)
	}
}

func TestMemory(t *testing.T) {
	dir := t.TempDir()
	meminfo := filepath.Join(dir, "meminfo")
	writeTo(t, meminfo, "MemTotal:       65536000 kB\nMemFree:         1000000 kB\nMemAvailable:   40960000 kB\nBuffers: 5 kB\n")
	cg := filepath.Join(dir, "cgroup")
	if err := os.MkdirAll(cg, 0o755); err != nil {
		t.Fatal(err)
	}
	total, avail := memory(meminfo, cg)
	if total != 65536000*1024 || avail != 40960000*1024 {
		t.Fatalf("%d %d", total, avail)
	}
	// A container limit below the host's holds both numbers down.
	writeTo(t, filepath.Join(cg, "memory.max"), "2147483648\n")
	writeTo(t, filepath.Join(cg, "memory.current"), "1610612736\n")
	total, avail = memory(meminfo, cg)
	if total != 2147483648 || avail != 536870912 {
		t.Fatalf("limited: %d %d", total, avail)
	}
	// "max" means no limit.
	writeTo(t, filepath.Join(cg, "memory.max"), "max\n")
	if total, _ = memory(meminfo, cg); total != 65536000*1024 {
		t.Fatalf("unlimited: %d", total)
	}
	if total, avail = memory(filepath.Join(dir, "missing"), cg); total != 0 || avail != 0 {
		t.Fatalf("no meminfo: %d %d", total, avail)
	}
}

func TestDiskUsage(t *testing.T) {
	total, free, err := diskUsage(t.TempDir())
	if err != nil || total <= 0 || free < 0 || free > total {
		t.Fatalf("%d %d %v", total, free, err)
	}
	if _, _, err := diskUsage("/definitely/not/here"); err == nil {
		t.Fatal("a missing path")
	}
}
