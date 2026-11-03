//go:build darwin

package agent

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// errNoReader is what a reading this system cannot do answers.
var errNoReader = errors.New("this system is not read")

type darwinReader struct {
	run func(name string, args ...string) (string, error)
}

func newSysReader() sysReader { return darwinReader{run: runOut} }

func runOut(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}

func (r darwinReader) sysctl(key string) string {
	out, err := r.run("sysctl", "-n", key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// CPUPct: macOS has no cumulative ticks without cgo, so the load is the sum of the processes' %cpu (ps keeps its own
// average) as a share of all cores.
func (r darwinReader) CPUPct() (float64, error) {
	out, err := r.run("ps", "-A", "-o", "%cpu=")
	if err != nil {
		return 0, err
	}
	sum, err := parsePSCPU(out)
	if err != nil {
		return 0, err
	}
	return pct(sum, float64(runtime.NumCPU())*100), nil
}

func (r darwinReader) Memory() (uint64, uint64, error) {
	total, err := strconv.ParseUint(r.sysctl("hw.memsize"), 10, 64)
	if err != nil || total == 0 {
		return 0, 0, errNoReader
	}
	out, err := r.run("vm_stat")
	if err != nil {
		return 0, 0, err
	}
	avail, err := parseVMStat(out)
	if err != nil {
		return 0, 0, err
	}
	if avail > total {
		avail = total
	}
	return total, avail, nil
}

func (darwinReader) Disk(path string) (uint64, uint64, error) { return statfsDisk(path) }

func (r darwinReader) Hardware(path string) hwFacts {
	h := hwFacts{Cores: runtime.NumCPU()}
	if n, err := strconv.ParseInt(r.sysctl("hw.memsize"), 10, 64); err == nil {
		h.RAMBytes = n
	}
	if total, _, err := statfsDisk(path); err == nil {
		h.DiskBytes = int64(total)
	}
	h.Model = r.sysctl("machdep.cpu.brand_string")
	if h.Model == "" {
		h.Model = r.sysctl("hw.model")
	}
	h.Laptop = strings.Contains(r.sysctl("hw.model"), "Book")
	return h
}
