//go:build linux

package preflight

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func diskUsage(path string) (total, free int64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	return int64(st.Blocks) * int64(st.Bsize), int64(st.Bavail) * int64(st.Bsize), nil
}

func osName(releaseFile string) string {
	if releaseFile == "" {
		releaseFile = "/etc/os-release"
	}
	f, err := os.Open(releaseFile)
	if err != nil {
		return "Linux"
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "PRETTY_NAME="); ok {
			if name := strings.Trim(strings.TrimSpace(v), `"'`); name != "" {
				return name
			}
		}
	}
	return "Linux"
}

// memory is the total and the available memory in bytes: /proc/meminfo, held down by the cgroup's limit when the
// account runs in a container with one.
func memory(meminfoFile, cgroupDir string) (total, available int64) {
	if meminfoFile == "" {
		meminfoFile = "/proc/meminfo"
	}
	if cgroupDir == "" {
		cgroupDir = "/sys/fs/cgroup"
	}
	if f, err := os.Open(meminfoFile); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			name, rest, ok := strings.Cut(sc.Text(), ":")
			if !ok {
				continue
			}
			fields := strings.Fields(rest)
			if len(fields) == 0 {
				continue
			}
			kb, err := strconv.ParseInt(fields[0], 10, 64)
			if err != nil {
				continue
			}
			switch name {
			case "MemTotal":
				total = kb * 1024
			case "MemAvailable":
				available = kb * 1024
			}
		}
		f.Close()
	}
	if limit, ok := readInt(cgroupDir + "/memory.max"); ok && limit > 0 && (total == 0 || limit < total) {
		total = limit
		if used, ok := readInt(cgroupDir + "/memory.current"); ok && limit-used < available {
			available = max(limit-used, 0)
		}
		if available > total {
			available = total
		}
	}
	return total, available
}

func readInt(path string) (int64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	return n, err == nil
}
