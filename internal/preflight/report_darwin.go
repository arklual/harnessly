//go:build darwin

package preflight

import (
	"os/exec"
	"regexp"
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

func osName(string) string {
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if v := strings.TrimSpace(string(out)); err == nil && v != "" {
		return "macOS " + v
	}
	return "macOS"
}

var vmStatPage = regexp.MustCompile(`page size of (\d+) bytes`)
var vmStatCount = regexp.MustCompile(`^Pages (free|inactive|speculative):\s+(\d+)\.`)

// memory: hw.memsize for the total; free, inactive and speculative pages of vm_stat for what is available.
func memory(string, string) (total, available int64) {
	if out, err := exec.Command("sysctl", "-n", "hw.memsize").Output(); err == nil {
		total, _ = strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	}
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return total, 0
	}
	page := int64(4096)
	if m := vmStatPage.FindStringSubmatch(string(out)); m != nil {
		page, _ = strconv.ParseInt(m[1], 10, 64)
	}
	var pages int64
	for _, line := range strings.Split(string(out), "\n") {
		if m := vmStatCount.FindStringSubmatch(line); m != nil {
			n, _ := strconv.ParseInt(m[2], 10, 64)
			pages += n
		}
	}
	return total, pages * page
}
