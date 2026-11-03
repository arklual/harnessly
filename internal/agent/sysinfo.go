package agent

import (
	"bufio"
	"errors"
	"strconv"
	"strings"
	"sync"
)

// What the agent reads of its machine for the hub (docs/hub/machines-resources.md): the hardware once, the load every
// minute. Nothing here needs cgo or a helper program on Linux (/proc, statfs); macOS asks sysctl, vm_stat and ps, each
// once a minute, which is cheap. A number the machine cannot read is absent, never made up.

// sysReader is the machine, as far as the sampler is concerned. Tests give it a fake.
type sysReader interface {
	// CPUPct is the share of the CPU busy since the previous call, 0..100. The first call only takes a baseline and
	// answers an error.
	CPUPct() (float64, error)
	// Memory is the total and the available bytes (what a program can still take without swapping).
	Memory() (total, avail uint64, err error)
	// Disk is the total and the used bytes of the filesystem that holds path.
	Disk(path string) (total, used uint64, err error)
	// Hardware is what the machine is made of; zero fields are unknown.
	Hardware(path string) hwFacts
}

// hwFacts are the hardware facts a reader found.
type hwFacts struct {
	Cores     int
	RAMBytes  int64
	DiskBytes int64
	Model     string
	Laptop    bool
}

var errNoBaseline = errors.New("no earlier reading to compare with")

// cpuMeter turns cumulative busy and total ticks into the share between two reads.
type cpuMeter struct {
	mu          sync.Mutex
	busy, total uint64
	have        bool
}

func (m *cpuMeter) pct(busy, total uint64) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pb, pt, had := m.busy, m.total, m.have
	m.busy, m.total, m.have = busy, total, true
	if !had {
		return 0, errNoBaseline
	}
	if total <= pt || busy < pb {
		return 0, errors.New("cpu times went backwards")
	}
	return pct(float64(busy-pb), float64(total-pt)), nil
}

// parseProcStat reads the first line of /proc/stat: cpu user nice system idle iowait irq softirq steal ...
// Busy is everything but idle and iowait.
func parseProcStat(text string) (busy, total uint64, err error) {
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 5 || f[0] != "cpu" {
			continue
		}
		var v []uint64
		for _, x := range f[1:] {
			n, e := strconv.ParseUint(x, 10, 64)
			if e != nil {
				return 0, 0, e
			}
			v = append(v, n)
		}
		// Fields: user nice system idle iowait irq softirq steal guest guest_nice. The last two are already in user.
		if len(v) > 8 {
			v = v[:8]
		}
		for _, n := range v {
			total += n
		}
		idle := v[3]
		if len(v) > 4 {
			idle += v[4]
		}
		if idle > total {
			return 0, 0, errors.New("cpu times do not add up")
		}
		return total - idle, total, nil
	}
	return 0, 0, errors.New("no cpu line")
}

// parseMeminfo reads /proc/meminfo: MemTotal and MemAvailable in kB (MemFree plus caches when the kernel is too old to
// say).
func parseMeminfo(text string) (total, avail uint64, err error) {
	var free, buffers, cached, sreclaimable uint64
	var haveAvail bool
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			continue
		}
		n, e := strconv.ParseUint(f[0], 10, 64)
		if e != nil {
			continue
		}
		n *= 1024
		switch name {
		case "MemTotal":
			total = n
		case "MemAvailable":
			avail, haveAvail = n, true
		case "MemFree":
			free = n
		case "Buffers":
			buffers = n
		case "Cached":
			cached = n
		case "SReclaimable":
			sreclaimable = n
		}
	}
	if total == 0 {
		return 0, 0, errors.New("no MemTotal")
	}
	if !haveAvail {
		avail = free + buffers + cached + sreclaimable
	}
	if avail > total {
		avail = total
	}
	return total, avail, nil
}

// parseVMStat reads macOS `vm_stat`: the page size and the pages free, inactive, speculative and purgeable are what a
// program can take; the total comes from sysctl hw.memsize.
func parseVMStat(text string) (availBytes uint64, err error) {
	page := uint64(4096)
	var pages uint64
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := sc.Text()
		if strings.Contains(line, "page size of") {
			for _, w := range strings.Fields(line) {
				if n, e := strconv.ParseUint(w, 10, 64); e == nil && n >= 512 {
					page = n
				}
			}
			continue
		}
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(name) {
		case "Pages free", "Pages inactive", "Pages speculative", "Pages purgeable":
			n, e := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(rest), "."), 10, 64)
			if e != nil {
				return 0, e
			}
			pages += n
		}
	}
	if pages == 0 {
		return 0, errors.New("no pages in vm_stat")
	}
	return pages * page, nil
}

// parsePSCPU sums the %cpu column of `ps -A -o %cpu=`. macOS gives no cumulative ticks, so the sum over processes, taken
// as a share of all cores, is the load.
func parsePSCPU(text string) (sum float64, err error) {
	sc := bufio.NewScanner(strings.NewReader(text))
	n := 0
	for sc.Scan() {
		s := strings.TrimSpace(sc.Text())
		if s == "" {
			continue
		}
		v, e := strconv.ParseFloat(s, 64)
		if e != nil {
			return 0, e
		}
		sum += v
		n++
	}
	if n == 0 {
		return 0, errors.New("no processes")
	}
	return sum, nil
}

// laptopChassis are the SMBIOS chassis types of a portable machine: portable, laptop, notebook, hand held, sub notebook.
func laptopChassis(t int) bool {
	switch t {
	case 8, 9, 10, 11, 14, 31, 32:
		return true
	}
	return false
}

func pct(part, whole float64) float64 {
	if whole <= 0 {
		return 0
	}
	v := part / whole * 100
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
