package proc

import (
	"os"
	"regexp"
	"strconv"
)

var memLine = map[string]*regexp.Regexp{
	"MemTotal":     regexp.MustCompile(`(?m)^MemTotal:\s+(\d+)`),
	"MemAvailable": regexp.MustCompile(`(?m)^MemAvailable:\s+(\d+)`),
}

func memValue(meminfo, key string) (float64, bool) {
	m := memLine[key].FindStringSubmatch(meminfo)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	return v, err == nil
}

// ParseMemAvailableRatio is MemAvailable / MemTotal of a /proc/meminfo text; false when either is missing.
func ParseMemAvailableRatio(meminfo string) (float64, bool) {
	total, okTotal := memValue(meminfo, "MemTotal")
	avail, okAvail := memValue(meminfo, "MemAvailable")
	if !okTotal || !okAvail || total <= 0 {
		return 0, false
	}
	return avail / total, true
}

// MemAvailableRatio is the share of memory still available: from /proc/meminfo, else from sysinfo(2)'s free and total
// memory. False only when neither can be read.
func MemAvailableRatio() (float64, bool) {
	if raw, err := os.ReadFile("/proc/meminfo"); err == nil {
		if r, ok := ParseMemAvailableRatio(string(raw)); ok {
			return r, true
		}
	}
	return sysMemRatio()
}
