package machines

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// The machine load: CPU, memory, swap-in, disk, and the heaviest processes of the runtime user with what each one is
// (a dev server, an agent or its tool, a service) and whether it may be stopped.

const (
	loadCacheTTL       = 2 * time.Second  // any number of callers within this window share one scan
	loadServeStale     = 10 * time.Second // a result this young is answered at once while the next scan runs behind it
	bootstrapGap       = 250 * time.Millisecond
	baselineMaxAge     = 10 * time.Second
	agentAncestorDepth = 4
	topLimit           = 12
	maxLineage         = 64
	scanTimeout        = 4 * time.Second
)

// ErrScanTimeout: the scan gave up (a hung mount under statfs, /proc reads stuck in a swap storm); the next request
// tries again.
var ErrScanTimeout = errors.New("scan-timeout")

// LoadProcess is one process of the answer; the keys are in the previous runtime's order.
type LoadProcess struct {
	PID       int    `json:"pid"`
	StartTime Num    `json:"starttime"` // with the pid it identifies one process for its whole life
	Command   string `json:"command"`
	Cwd       string `json:"cwd,omitempty"`
	CPUPct    Num    `json:"cpuPct"` // share of one core, like top
	RSSBytes  Num    `json:"rssBytes"`
	Port      *int   `json:"port,omitempty"`
	Stuck     *bool  `json:"stuck,omitempty"`
	Kind      string `json:"kind"`
	Stoppable bool   `json:"stoppable"`
}

// MachineLoad is the answer.
type MachineLoad struct {
	At  Num `json:"at"`
	CPU struct {
		Pct   Num `json:"pct"`
		Cores int `json:"cores"`
		Load1 Num `json:"load1"`
	} `json:"cpu"`
	Mem struct {
		UsedPct       Num  `json:"usedPct"`
		SwapUsedBytes Num  `json:"swapUsedBytes"`
		SwapInPerSec  *Num `json:"swapInPerSec"` // nil: no baseline yet
	} `json:"mem"`
	Disk struct {
		UsedPct Num    `json:"usedPct"`
		Path    string `json:"path"`
	} `json:"disk"`
	Level  string        `json:"level"`
	Reason *string       `json:"reason"`
	Top    []LoadProcess `json:"top"`
}

// LoadDeps is what the monitor reads; tests give it a /proc of their own.
type LoadDeps struct {
	ProcRoot string
	UID      int
	// RuntimePIDs are the runtime's processes (the previous runtime first, then this one): they, everything above them
	// and everything they spawned are the platform's.
	RuntimePIDs func() []int
	Cores       int
	HomeDir     string
	// ReleaseRoot is where the runtime is installed; anything run from there is the platform's.
	ReleaseRoot string
	OwnerOf     func(pid int) (int, bool)
	Now         func() time.Time
	Sleep       func(time.Duration)
	Statfs      func(path string) (blocks, bfree, bavail float64, err error)
	// ListPorts are the listeners of the user's processes; stuck are ports already marked stuck, whose last probe may
	// be reused. gen changes whenever the listing is a new one (a cached listing must not count twice).
	ListPorts func(stuck map[int]bool) (ports []LoadPort, gen uint64)
	PanePIDs  func() []int
	// ScanTimeout is how long a scan may take before it is given up (4 s).
	ScanTimeout time.Duration
}

// LoadPort is a listening port of one of the user's processes and whether it answered.
type LoadPort struct {
	Port       int
	PID        int
	Cwd        string
	Responding bool
	Fresh      bool // false: an old probe result
}

type procSample struct {
	pid       int
	comm      string
	ppid      int
	ticks     float64
	starttime float64
	rssBytes  float64
}

type sample struct {
	at     time.Time
	swapIn *float64
	cpu    cpuTicks
	procs  map[int]*procSample
	order  []int
}

// LoadMonitor scans at most once per two seconds and serves a result up to ten seconds old while the next scan runs.
type LoadMonitor struct {
	d LoadDeps

	mu       sync.Mutex
	previous *sample
	levels   map[string]string
	misses   map[int]int
	lastGen  uint64
	hasGen   bool
	cached   *MachineLoad
	cachedAt time.Time
	inflight chan struct{}
	result   *MachineLoad
	err      error

	served     *MachineLoad
	servedAt   time.Time
	refreshing bool
}

func NewLoadMonitor(d LoadDeps) *LoadMonitor {
	if d.ProcRoot == "" {
		d.ProcRoot = "/proc"
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Sleep == nil {
		d.Sleep = time.Sleep
	}
	if d.OwnerOf == nil {
		root := d.ProcRoot
		d.OwnerOf = func(pid int) (int, bool) {
			st, err := os.Stat(filepath.Join(root, strconv.Itoa(pid)))
			if err != nil {
				return 0, false
			}
			if sys, ok := st.Sys().(*syscall.Stat_t); ok {
				return int(sys.Uid), true
			}
			return 0, false
		}
	}
	if d.Statfs == nil {
		d.Statfs = statfs
	}
	if d.ListPorts == nil {
		d.ListPorts = func(map[int]bool) ([]LoadPort, uint64) { return nil, 0 }
	}
	if d.PanePIDs == nil {
		d.PanePIDs = func() []int { return nil }
	}
	if d.ScanTimeout == 0 {
		d.ScanTimeout = scanTimeout
	}
	if d.RuntimePIDs == nil {
		self := os.Getpid()
		d.RuntimePIDs = func() []int { return []int{self} }
	}
	return &LoadMonitor{d: d, levels: initialLevels(), misses: map[int]int{}}
}

func (m *LoadMonitor) read(pid int, name string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(m.d.ProcRoot, strconv.Itoa(pid), name))
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func (m *LoadMonitor) readProcess(pid int) *procSample {
	if owner, ok := m.d.OwnerOf(pid); !ok || owner != m.d.UID {
		return nil
	}
	status, ok1 := m.read(pid, "status")
	stat, ok2 := m.read(pid, "stat")
	if !ok1 || !ok2 {
		return nil
	}
	// A setuid program (sudo) acts as its owner: it is not the user's to show or stop.
	real, effective, ok := parseProcUids(status)
	if !ok || real != m.d.UID || effective != m.d.UID {
		return nil
	}
	p, ok := parseProcStat(stat)
	if !ok {
		return nil
	}
	rss, _ := parseVmRSSBytes(status) // none: a zombie or a kernel thread, nothing resident
	return &procSample{pid: p.pid, comm: p.comm, ppid: p.ppid, ticks: p.ticks, starttime: p.starttime, rssBytes: rss}
}

func (m *LoadMonitor) sample() (*sample, error) {
	s := &sample{at: m.d.Now(), procs: map[int]*procSample{}}
	raw, err := os.ReadFile(filepath.Join(m.d.ProcRoot, "stat"))
	if err != nil {
		return nil, err
	}
	cpu, ok := parseCPUStat(string(raw))
	if !ok {
		return nil, errors.New("unreadable /proc/stat")
	}
	s.cpu = cpu
	if raw, err := os.ReadFile(filepath.Join(m.d.ProcRoot, "vmstat")); err == nil {
		if v, ok := parseVmstatSwapIn(string(raw)); ok {
			s.swapIn = &v
		}
	}
	entries, err := os.ReadDir(m.d.ProcRoot)
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, e := range entries {
		if isDigits(e.Name()) {
			pids = append(pids, int(jsNumber(e.Name())))
		}
	}
	for _, pid := range pids {
		if p := m.readProcess(pid); p != nil {
			if _, dup := s.procs[p.pid]; !dup {
				s.order = append(s.order, p.pid)
			}
			s.procs[p.pid] = p
		}
	}
	return s, nil
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// lineage is a runtime process and everything above it (its shell, tmux or systemd): killing any of them takes it
// down.
func (m *LoadMonitor) lineage(roots []int) map[int]bool {
	out := map[int]bool{}
	for _, pid := range roots {
		seen := map[int]bool{}
		for pid > 0 && !seen[pid] && len(seen) < maxLineage {
			seen[pid] = true
			out[pid] = true
			stat, ok := m.read(pid, "stat")
			if !ok {
				break
			}
			p, ok := parseProcStat(stat)
			if !ok {
				break
			}
			pid = p.ppid
		}
	}
	return out
}

func diskUsedPct(blocks, bfree, bavail float64) float64 {
	used := blocks - bfree
	if used+bavail > 0 {
		return (used / (used + bavail)) * 100
	}
	return 0
}

func (m *LoadMonitor) stuckPorts() map[int]bool {
	out := map[int]bool{}
	for port, n := range m.misses {
		if n >= stuckScans {
			out[port] = true
		}
	}
	return out
}

type procFacts struct{ args, cwd, cgroup, exe string }

// scanOnce computes on locals and writes back only at the end, so a scan abandoned on timeout leaves no trace.
func (m *LoadMonitor) scanOnce(abandoned func() bool) (*MachineLoad, error) {
	m.mu.Lock()
	previous, levels, misses, lastGen, hasGen := m.previous, m.levels, m.misses, m.lastGen, m.hasGen
	stuckNow := m.stuckPorts()
	m.mu.Unlock()

	rebaseline := previous == nil || m.d.Now().Sub(previous.at) > baselineMaxAge
	before := previous
	if rebaseline {
		s, err := m.sample()
		if err != nil {
			return nil, err
		}
		before = s
		m.d.Sleep(bootstrapGap)
	}
	current, err := m.sample()
	if err != nil {
		return nil, err
	}
	at := m.d.Now()

	rawMem, err := os.ReadFile(filepath.Join(m.d.ProcRoot, "meminfo"))
	if err != nil {
		return nil, err
	}
	rawLoad, err := os.ReadFile(filepath.Join(m.d.ProcRoot, "loadavg"))
	if err != nil {
		return nil, err
	}
	blocks, bfree, bavail, err := m.d.Statfs(m.d.HomeDir)
	if err != nil {
		return nil, err
	}
	ports, gen := m.d.ListPorts(stuckNow)
	panes := m.d.PanePIDs()
	runtimePIDs := m.d.RuntimePIDs()
	lineage := m.lineage(runtimePIDs)
	units := map[string]bool{}
	for _, pid := range runtimePIDs {
		if cg, ok := m.read(pid, "cgroup"); ok {
			if u := systemServiceUnit(cg); u != "" {
				units[u] = true
			}
		}
	}
	mem, okMem := parseMeminfo(string(rawMem))
	load1, okLoad := parseLoadavg(string(rawLoad))
	if !okMem || !okLoad {
		return nil, errors.New("unreadable /proc/meminfo or /proc/loadavg")
	}
	cores := float64(m.d.Cores)

	cpuPct := cpuPctFromDelta(before.cpu, current.cpu)
	// The short bootstrap window is too noisy for a rate: no swap-in signal until a real baseline exists.
	seconds := float64(current.at.UnixMilli()-before.at.UnixMilli()) / 1000
	var swapIn *float64
	if !rebaseline && before.swapIn != nil && current.swapIn != nil && seconds > 0 {
		v := max(0, *current.swapIn-*before.swapIn) / seconds
		swapIn = &v
	}
	diskPct := diskUsedPct(blocks, bfree, bavail)
	loadPerCore := load1 / cores
	state := classifyLevel(map[string]*float64{"ram": &mem.usedPct, "swap": swapIn, "load": &loadPerCore, "disk": &diskPct}, levels)

	// Listeners of the runtime user only: the processes of anyone else are not read.
	var owned []LoadPort
	for _, p := range ports {
		if p.PID > 0 && current.procs[p.PID] != nil {
			owned = append(owned, p)
		}
	}
	freshProbe := !hasGen || gen != lastGen
	newMisses, stuck := misses, stuckNow
	if freshProbe {
		probes := make([]portProbe, len(owned))
		for i, p := range owned {
			probes[i] = portProbe{port: p.Port}
			if p.Fresh {
				r := p.Responding
				probes[i].responding = &r
			}
		}
		newMisses, stuck = stuckAfter(misses, probes)
	}
	sortedOwned := append([]LoadPort(nil), owned...)
	sort.SliceStable(sortedOwned, func(i, j int) bool { return sortedOwned[i].Port < sortedOwned[j].Port })
	portOf := map[int]LoadPort{}
	for _, p := range sortedOwned {
		if _, has := portOf[p.PID]; !has {
			portOf[p.PID] = p
		}
	}

	// Delta of a process's ticks against the machine's, so no clock tick constant is needed.
	perCoreTicks := (current.cpu.total - before.cpu.total) / cores
	cpuOf := func(p *procSample) float64 {
		old := before.procs[p.pid]
		if old == nil || old.starttime != p.starttime || perCoreTicks <= 0 {
			return 0
		}
		return max(0, ((p.ticks-old.ticks)/perCoreTicks)*100)
	}
	type scoredProc struct {
		p           *procSample
		cpu, weight float64
	}
	scored := make([]scoredProc, 0, len(current.order))
	for _, pid := range current.order {
		p := current.procs[pid]
		c := cpuOf(p)
		scored = append(scored, scoredProc{p, c, (p.rssBytes/mem.totalBytes)*100 + c/cores})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].weight != scored[j].weight {
			return scored[i].weight > scored[j].weight
		}
		return scored[i].p.pid < scored[j].p.pid
	})
	picked := append([]scoredProc(nil), scored[:min(topLimit, len(scored))]...)
	chosen := map[int]bool{}
	for _, s := range picked {
		chosen[s.p.pid] = true
	}
	for _, s := range scored {
		if _, has := portOf[s.p.pid]; has && !chosen[s.p.pid] {
			picked = append(picked, s)
		}
	}

	paneSet := map[int]bool{}
	for _, pid := range panes {
		paneSet[pid] = true
	}
	agentOf := map[int]bool{}
	isAgentPid := func(pid int) bool {
		if v, has := agentOf[pid]; has {
			return v
		}
		v := false
		if p := current.procs[pid]; p != nil {
			args, _ := m.args(pid)
			v = isAgentProcess(p.comm, args)
		}
		agentOf[pid] = v
		return v
	}
	ancestorsOf := func(p *procSample) []*procSample {
		var chain []*procSample
		seen := map[int]bool{}
		for parent := current.procs[p.ppid]; parent != nil && !seen[parent.pid] && len(chain) < maxLineage; parent = current.procs[parent.ppid] {
			seen[parent.pid] = true
			chain = append(chain, parent)
		}
		return chain
	}
	factsOf := map[int]procFacts{}
	facts := func(pid int) procFacts {
		if f, has := factsOf[pid]; has {
			return f
		}
		var f procFacts
		f.args, _ = m.args(pid)
		f.cwd = m.link(pid, "cwd")
		f.cgroup, _ = m.read(pid, "cgroup")
		f.exe = m.link(pid, "exe")
		factsOf[pid] = f
		return f
	}
	isService := func(f procFacts) bool {
		return isSystemService(serviceInput{cgroup: f.cgroup, exe: f.exe, cwd: f.cwd, args: f.args, runtimeUnits: units}, m.d.ReleaseRoot)
	}
	runtimeSet := map[int]bool{}
	for _, pid := range runtimePIDs {
		runtimeSet[pid] = true
	}
	top := make([]LoadProcess, 0, len(picked))
	for _, s := range picked {
		p := s.p
		listener, hasPort := portOf[p.pid]
		ancestors := ancestorsOf(p)
		f := facts(p.pid)
		agentAncestor := false
		parent := p.ppid
		for depth := 0; depth < agentAncestorDepth && current.procs[parent] != nil; depth++ {
			if isAgentPid(parent) {
				agentAncestor = true
				break
			}
			parent = current.procs[parent].ppid
		}
		// Anything a service spawned is part of it; the walk stops at a tmux server, whatever runs in a pane is the
		// user's work.
		serviceAncestor := false
		for _, a := range ancestors {
			if isTmuxComm(a.comm) {
				break
			}
			if isService(facts(a.pid)) {
				serviceAncestor = true
				break
			}
		}
		isRuntime := lineage[p.pid]
		for _, a := range ancestors {
			isRuntime = isRuntime || runtimeSet[a.pid]
		}
		kind := classifyKind(kindInput{comm: p.comm, args: f.args, hasPort: hasPort, isPanePid: paneSet[p.pid],
			hasAgentAncestor: agentAncestor, isRuntime: isRuntime, isSystemService: serviceAncestor || isService(f)})
		command := f.args
		if command == "" {
			command = p.comm
		}
		lp := LoadProcess{PID: p.pid, StartTime: Num(p.starttime), Command: redactCommand(command), CPUPct: Num(s.cpu),
			RSSBytes: Num(p.rssBytes), Kind: kind, Stoppable: isStoppableKind(kind)}
		lp.Cwd = f.cwd
		if hasPort {
			if listener.Cwd != "" {
				lp.Cwd = listener.Cwd
			}
			port, st := listener.Port, stuck[listener.Port]
			lp.Port, lp.Stuck = &port, &st
		}
		top = append(top, lp)
	}

	if abandoned() {
		return nil, ErrScanTimeout
	}
	m.mu.Lock()
	m.previous, m.levels, m.misses, m.lastGen, m.hasGen = current, state.metrics, newMisses, gen, true
	m.mu.Unlock()
	out := &MachineLoad{At: Num(at.UnixMilli()), Level: state.level, Reason: state.reason, Top: top}
	out.CPU.Pct, out.CPU.Cores, out.CPU.Load1 = Num(cpuPct), m.d.Cores, Num(load1)
	out.Mem.UsedPct, out.Mem.SwapUsedBytes = Num(mem.usedPct), Num(mem.swapUsedBytes)
	if swapIn != nil {
		out.Mem.SwapInPerSec = num(*swapIn)
	}
	out.Disk.UsedPct, out.Disk.Path = Num(diskPct), m.d.HomeDir
	return out, nil
}

// args is the command line with its arguments joined by spaces; false when unreadable.
func (m *LoadMonitor) args(pid int) (string, bool) {
	raw, ok := m.read(pid, "cmdline")
	if !ok {
		return "", false
	}
	return jscompat.Trim(strings.ReplaceAll(raw, "\x00", " ")), true
}

func (m *LoadMonitor) link(pid int, name string) string {
	target, err := os.Readlink(filepath.Join(m.d.ProcRoot, strconv.Itoa(pid), name))
	if err != nil {
		return ""
	}
	return target
}

// scan runs one scan, given up after scanTimeout.
func (m *LoadMonitor) scan() (*MachineLoad, error) {
	var mu sync.Mutex
	gaveUp := false
	type res struct {
		l   *MachineLoad
		err error
	}
	done := make(chan res, 1)
	go func() {
		l, err := m.scanOnce(func() bool { mu.Lock(); defer mu.Unlock(); return gaveUp })
		done <- res{l, err}
	}()
	t := time.NewTimer(m.d.ScanTimeout)
	defer t.Stop()
	select {
	case r := <-done:
		return r.l, r.err
	case <-t.C:
		mu.Lock()
		gaveUp = true
		mu.Unlock()
		return nil, ErrScanTimeout
	}
}

// collect is one scan shared by every caller within the cache window.
func (m *LoadMonitor) collect() (*MachineLoad, error) {
	m.mu.Lock()
	if ch := m.inflight; ch != nil {
		m.mu.Unlock()
		<-ch
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.result, m.err
	}
	if m.cached != nil && m.d.Now().Sub(m.cachedAt) < loadCacheTTL {
		defer m.mu.Unlock()
		return m.cached, nil
	}
	ch := make(chan struct{})
	m.inflight = ch
	m.mu.Unlock()
	l, err := m.scan()
	m.mu.Lock()
	if err != nil {
		m.cached = nil
	} else {
		m.cached, m.cachedAt = l, m.d.Now()
	}
	m.result, m.err, m.inflight = l, err, nil
	m.mu.Unlock()
	close(ch)
	return l, err
}

// Load is the machine load: a result under ten seconds old is answered at once (a scan takes over a second on a busy
// machine), and one older than two seconds is refreshed behind it.
func (m *LoadMonitor) Load() (*MachineLoad, error) {
	m.mu.Lock()
	if m.served != nil && m.d.Now().Sub(m.servedAt) < loadServeStale {
		l := m.served
		if m.d.Now().Sub(m.servedAt) >= loadCacheTTL && !m.refreshing {
			m.refreshing = true
			go func() { _, _ = m.refresh() }()
		}
		m.mu.Unlock()
		return l, nil
	}
	m.mu.Unlock()
	return m.refresh()
}

func (m *LoadMonitor) refresh() (*MachineLoad, error) {
	l, err := m.collect()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshing = false
	if err == nil {
		m.served, m.servedAt = l, m.d.Now()
	}
	return l, err
}
