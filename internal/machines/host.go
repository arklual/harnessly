package machines

import (
	"os"
	"regexp"
	"strconv"
	"strings"
)

var meminfoLine = regexp.MustCompile(`(?m)^(MemTotal|MemAvailable):\s+(\d+)\s*kB`)

// ReadHostStats reads the figures from /proc as Node's os module did: CPUs as /proc/stat lists them, the load from
// /proc/loadavg, total and available memory from /proc/meminfo, the uptime from /proc/uptime. Zero where unreadable.
func ReadHostStats() HostStats { return readHostStats("/proc") }

func readHostStats(root string) HostStats {
	var h HostStats
	if raw, err := os.ReadFile(root + "/stat"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if len(line) > 3 && strings.HasPrefix(line, "cpu") && line[3] >= '0' && line[3] <= '9' {
				h.CPUs++
			}
		}
	}
	if raw, err := os.ReadFile(root + "/loadavg"); err == nil {
		if f := strings.Fields(string(raw)); len(f) > 0 {
			h.Load1, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	if raw, err := os.ReadFile(root + "/meminfo"); err == nil {
		for _, m := range meminfoLine.FindAllStringSubmatch(string(raw), -1) {
			kb, _ := strconv.ParseFloat(m[2], 64)
			if m[1] == "MemTotal" {
				h.TotalMem = kb * 1024
			} else {
				h.FreeMem = kb * 1024
			}
		}
	}
	if raw, err := os.ReadFile(root + "/uptime"); err == nil {
		if f := strings.Fields(string(raw)); len(f) > 0 {
			h.Uptime, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	return h
}
