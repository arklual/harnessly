//go:build linux

package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type linuxReader struct {
	root string
	cpu  *cpuMeter
}

func newSysReader() sysReader { return linuxReader{root: "/", cpu: &cpuMeter{}} }

func (r linuxReader) read(rel string) string {
	b, err := os.ReadFile(filepath.Join(r.root, rel))
	if err != nil {
		return ""
	}
	return string(b)
}

func (r linuxReader) CPUPct() (float64, error) {
	busy, total, err := parseProcStat(r.read("proc/stat"))
	if err != nil {
		return 0, err
	}
	return r.cpu.pct(busy, total)
}

func (r linuxReader) Memory() (uint64, uint64, error) { return parseMeminfo(r.read("proc/meminfo")) }

func (linuxReader) Disk(path string) (uint64, uint64, error) { return statfsDisk(path) }

func (r linuxReader) Hardware(path string) hwFacts {
	h := hwFacts{Cores: runtime.NumCPU()}
	if total, _, err := r.Memory(); err == nil {
		h.RAMBytes = int64(total)
	}
	if total, _, err := statfsDisk(path); err == nil {
		h.DiskBytes = int64(total)
	}
	h.Model = cpuModel(r.read("proc/cpuinfo"))
	if t, err := strconv.Atoi(strings.TrimSpace(r.read("sys/class/dmi/id/chassis_type"))); err == nil && laptopChassis(t) {
		h.Laptop = true
	} else if m, _ := filepath.Glob(filepath.Join(r.root, "sys/class/power_supply/BAT*")); len(m) > 0 {
		h.Laptop = true
	}
	return h
}

// cpuModel is the first "model name" (x86) or "Model"/"Hardware" (arm boards) of /proc/cpuinfo.
func cpuModel(text string) string {
	for _, key := range []string{"model name", "Model", "Hardware"} {
		for _, line := range strings.Split(text, "\n") {
			k, v, ok := strings.Cut(line, ":")
			if ok && strings.TrimSpace(k) == key {
				if v = strings.Join(strings.Fields(v), " "); v != "" {
					return v
				}
			}
		}
	}
	return ""
}
