package machines

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The machine's last hour for the Machine tool of the page: how busy the processor and each of its cores were, how much
// memory was in use, how much went over the network. The runtime reads /proc every few seconds into a ring in memory and
// serves it thinned to a point every two minutes; a restart starts it empty (the page draws what there is).

const (
	// HistoryEvery is how often /proc is read; HistoryKeep how long a reading is kept.
	HistoryEvery = 5 * time.Second
	HistoryKeep  = time.Hour
)

// Sample is one reading: the busy share of all cores and of each (0…1) since the reading before, the memory in use, the
// bytes over the network per second since then.
type Sample struct {
	At                 int64 // ms
	CPU                float64
	Cores              []float64
	MemUsed, MemTotal  int64
	RxPerSec, TxPerSec float64
}

// Point is a sample thinned for a chart: the processor and the memory as shares of the whole (0…1).
type Point struct {
	At                 int64
	CPU, Mem           float64
	RxPerSec, TxPerSec float64
}

type cpuRaw struct{ idle, total float64 }

type rawReading struct {
	at     time.Time
	cpu    cpuRaw
	cores  []cpuRaw
	rx, tx float64
}

// HistoryDeps is what the history reads; tests give it a /proc of their own and a clock.
type HistoryDeps struct {
	ProcRoot string
	Now      func() time.Time
}

// History keeps the readings of the last hour.
type History struct {
	d HistoryDeps

	mu   sync.Mutex
	prev *rawReading
	ring []Sample
}

func NewHistory(d HistoryDeps) *History {
	if d.ProcRoot == "" {
		d.ProcRoot = "/proc"
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &History{d: d}
}

// Run reads /proc every HistoryEvery until ctx ends; the first reading is at once.
func (h *History) Run(ctx context.Context) {
	h.Sample()
	t := time.NewTicker(HistoryEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.Sample()
		}
	}
}

// parseCores reads the "cpu" lines of /proc/stat: the total first, then each core in order. Idle counts iowait: a core
// waiting for the disk is not working.
func parseCores(text string) (total cpuRaw, cores []cpuRaw, ok bool) {
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "cpu") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		var sum, idle float64
		for i, s := range f[1:min(len(f), 9)] {
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return cpuRaw{}, nil, false
			}
			sum += v
			if i == 3 || i == 4 {
				idle += v
			}
		}
		if f[0] == "cpu" {
			total, ok = cpuRaw{idle, sum}, true
			continue
		}
		cores = append(cores, cpuRaw{idle, sum})
	}
	return total, cores, ok
}

func busy(prev, cur cpuRaw) float64 {
	total, idle := cur.total-prev.total, cur.idle-prev.idle
	if total <= 0 || idle < 0 {
		return 0
	}
	return min(1, max(0, (total-idle)/total))
}

// virtualLink says whether a network interface only carries the traffic of another (loopback, containers' pairs and
// bridges): counting it would count the same bytes twice.
func virtualLink(name string) bool {
	if name == "lo" {
		return true
	}
	for _, p := range []string{"veth", "docker", "br-", "virbr", "cni", "flannel", "cali"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// parseNetDev sums the bytes received and sent over the machine's own links.
func parseNetDev(text string) (rx, tx float64) {
	for _, line := range strings.Split(text, "\n") {
		name, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		name = strings.TrimSpace(name)
		f := strings.Fields(rest)
		if virtualLink(name) || len(f) < 9 {
			continue
		}
		r, err1 := strconv.ParseFloat(f[0], 64)
		t, err2 := strconv.ParseFloat(f[8], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		rx, tx = rx+r, tx+t
	}
	return rx, tx
}

// parseMemUsed is the memory in use (what is not available to a new program) and the total, in bytes.
func parseMemUsed(text string) (used, total int64, ok bool) {
	var avail int64
	var haveTotal, haveAvail bool
	for _, line := range strings.Split(text, "\n") {
		key, rest, found := strings.Cut(line, ":")
		if !found || (key != "MemTotal" && key != "MemAvailable") {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			continue
		}
		kb, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil {
			continue
		}
		if key == "MemTotal" {
			total, haveTotal = kb*1024, true
		} else {
			avail, haveAvail = kb*1024, true
		}
	}
	if !haveTotal || !haveAvail || total <= 0 {
		return 0, 0, false
	}
	return max(0, total-avail), total, true
}

func (h *History) read(name string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(h.d.ProcRoot, name))
	return string(raw), err == nil
}

// Sample takes one reading. The first has nothing to compare with and only remembers the counters.
func (h *History) Sample() {
	now := h.d.Now()
	stat, ok1 := h.read("stat")
	mem, ok2 := h.read("meminfo")
	if !ok1 || !ok2 {
		return
	}
	total, cores, ok := parseCores(stat)
	used, memTotal, okMem := parseMemUsed(mem)
	if !ok || !okMem {
		return
	}
	cur := &rawReading{at: now, cpu: total, cores: cores}
	if net, ok := h.read("net/dev"); ok {
		cur.rx, cur.tx = parseNetDev(net)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	prev := h.prev
	h.prev = cur
	if prev == nil || len(prev.cores) != len(cores) {
		return
	}
	s := Sample{At: now.UnixMilli(), CPU: busy(prev.cpu, cur.cpu), Cores: make([]float64, len(cores)), MemUsed: used, MemTotal: memTotal}
	for i := range cores {
		s.Cores[i] = busy(prev.cores[i], cores[i])
	}
	if secs := now.Sub(prev.at).Seconds(); secs > 0 {
		s.RxPerSec = max(0, cur.rx-prev.rx) / secs
		s.TxPerSec = max(0, cur.tx-prev.tx) / secs
	}
	h.ring = append(h.ring, s)
	cut := now.Add(-HistoryKeep).UnixMilli()
	drop := 0
	for drop < len(h.ring) && h.ring[drop].At < cut {
		drop++
	}
	h.ring = h.ring[drop:]
}

// Latest is the newest reading; false before the second one.
func (h *History) Latest() (Sample, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.ring) == 0 {
		return Sample{}, false
	}
	s := h.ring[len(h.ring)-1]
	s.Cores = append([]float64(nil), s.Cores...)
	return s, true
}

// Series is the hour thinned to n points, one per step, the last ending at the newest reading. A point is the mean of the
// readings of its step; a step with none (the runtime was not running) has no point. The last point is the newest
// reading itself, so the chart ends where the figures beside it are.
func (h *History) Series(n int, step time.Duration) []Point {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.ring) == 0 || n <= 0 || step <= 0 {
		return []Point{}
	}
	last := h.ring[len(h.ring)-1]
	end := last.At
	stepMs := step.Milliseconds()
	type acc struct {
		cpu, mem, rx, tx float64
		n                int
	}
	buckets := make([]acc, n)
	for _, s := range h.ring[:len(h.ring)-1] {
		// Bucket n−1 ends at `end`; a reading `age` ms old is `age / step` buckets back.
		back := int((end - s.At) / stepMs)
		i := n - 1 - back
		if i < 0 || i >= n || s.MemTotal <= 0 {
			continue
		}
		b := &buckets[i]
		b.cpu += s.CPU
		b.mem += float64(s.MemUsed) / float64(s.MemTotal)
		b.rx += s.RxPerSec
		b.tx += s.TxPerSec
		b.n++
	}
	out := make([]Point, 0, n)
	for i := 0; i < n-1; i++ {
		b := buckets[i]
		if b.n == 0 {
			continue
		}
		f := float64(b.n)
		out = append(out, Point{At: end - int64(n-1-i)*stepMs, CPU: b.cpu / f, Mem: b.mem / f, RxPerSec: b.rx / f, TxPerSec: b.tx / f})
	}
	if last.MemTotal > 0 {
		out = append(out, Point{At: end, CPU: last.CPU, Mem: float64(last.MemUsed) / float64(last.MemTotal), RxPerSec: last.RxPerSec, TxPerSec: last.TxPerSec})
	}
	return out
}
