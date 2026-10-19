package machines

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The listening ports of the runtime user's own processes, for the machine load: nobody else's service is probed.
// Each probe gets 400 ms and all of them 1.5 s together, so a few silent ports cannot stall a scan.

const (
	portsCacheTTL    = 2 * time.Second
	ownedProbeBudget = 400 * time.Millisecond
	ownedProbeCap    = 1500 * time.Millisecond
	probeConcurrency = 16
)

// OwnedPorts lists and probes the user's listeners; one listing serves every caller for two seconds.
type OwnedPorts struct {
	UID      int
	ProcRoot string
	// Hidden are the platform's own ports: never listed.
	Hidden  map[int]bool
	OwnerOf func(pid int) (int, bool)
	// Probe says whether something answers HTTP on the port within the budget.
	Probe func(port int, budget time.Duration) bool
	Now   func() time.Time

	mu       sync.Mutex
	at       time.Time
	ports    []LoadPort
	gen      uint64
	running  chan struct{}
	listener map[int]int // every listening port of the user's processes, hidden ones included
	probes   map[int]*ownedProbe
}

type ownedProbe struct {
	last    *bool
	pending chan struct{}
	result  bool
}

func (o *OwnedPorts) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// listeningInodes maps the inode of every listening TCP socket to its port, IPv4 first.
func listeningInodes(procRoot string) (map[string]int, []string) {
	ports := map[string]int{}
	var order []string
	for _, table := range []string{"net/tcp", "net/tcp6"} {
		raw, err := os.ReadFile(filepath.Join(procRoot, table))
		if err != nil {
			continue
		}
		lines := strings.Split(string(raw), "\n")
		for _, line := range lines[min(1, len(lines)):] {
			f := strings.Fields(line)
			if len(f) < 10 || f[3] != "0A" {
				continue
			}
			i := strings.LastIndexByte(f[1], ':')
			if i < 0 {
				continue
			}
			port, err := strconv.ParseInt(f[1][i+1:], 16, 32)
			if err != nil || port < 1 {
				continue
			}
			if _, seen := ports[f[9]]; !seen {
				order = append(order, f[9])
			}
			ports[f[9]] = int(port)
		}
	}
	return ports, order
}

// scan finds which of the user's processes holds each listening socket. Several holders (a forked server) are named
// by the newest pid, as ss names them first.
func (o *OwnedPorts) scan() map[int]int {
	root := o.ProcRoot
	if root == "" {
		root = "/proc"
	}
	inodes, order := listeningInodes(root)
	holder := map[string]int{}
	if len(inodes) > 0 {
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			if !isDigits(e.Name()) {
				continue
			}
			pid, _ := strconv.Atoi(e.Name())
			if o.OwnerOf != nil {
				if owner, ok := o.OwnerOf(pid); !ok || owner != o.UID {
					continue
				}
			}
			fdDir := filepath.Join(root, e.Name(), "fd")
			fds, err := os.ReadDir(fdDir)
			if err != nil {
				continue
			}
			for _, fd := range fds {
				target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
				if err != nil {
					continue
				}
				inode, ok := strings.CutPrefix(target, "socket:[")
				if !ok {
					continue
				}
				inode = strings.TrimSuffix(inode, "]")
				if _, listening := inodes[inode]; listening && pid > holder[inode] {
					holder[inode] = pid
				}
			}
		}
	}
	out := map[int]int{}
	for _, inode := range order {
		port := inodes[inode]
		if pid, ok := holder[inode]; ok {
			if _, known := out[port]; !known {
				out[port] = pid
			}
		}
	}
	return out
}

// ListenerPID is the user's process listening on port as of the last listing; 0 when none.
func (o *OwnedPorts) ListenerPID(port int) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.listener[port]
}

// List is the listing for the machine load (LoadDeps.ListPorts).
func (o *OwnedPorts) List(stuck map[int]bool) ([]LoadPort, uint64) {
	o.mu.Lock()
	if ch := o.running; ch != nil {
		o.mu.Unlock()
		<-ch
		o.mu.Lock()
		defer o.mu.Unlock()
		return o.ports, o.gen
	}
	if o.gen > 0 && o.now().Sub(o.at) < portsCacheTTL {
		defer o.mu.Unlock()
		return o.ports, o.gen
	}
	ch := make(chan struct{})
	o.running, o.at = ch, o.now()
	o.mu.Unlock()

	listener := o.scan()
	root := o.ProcRoot
	if root == "" {
		root = "/proc"
	}
	var visible []LoadPort
	for port, pid := range listener {
		if o.Hidden[port] {
			continue
		}
		cwd, _ := os.Readlink(filepath.Join(root, strconv.Itoa(pid), "cwd"))
		visible = append(visible, LoadPort{Port: port, PID: pid, Cwd: cwd})
	}
	sort.Slice(visible, func(i, j int) bool { return visible[i].Port < visible[j].Port })
	ports := o.probeAll(visible, stuck)

	o.mu.Lock()
	o.ports, o.listener, o.running = ports, listener, nil
	o.gen++
	gen := o.gen
	o.mu.Unlock()
	close(ch)
	return ports, gen
}

func (o *OwnedPorts) startProbe(port int) *ownedProbe {
	o.mu.Lock()
	defer o.mu.Unlock()
	p := o.probes[port]
	if p.pending != nil {
		return p
	}
	ch := make(chan struct{})
	p.pending = ch
	go func() {
		r := o.Probe != nil && o.Probe(port, ownedProbeBudget)
		o.mu.Lock()
		p.last, p.result = &r, r
		if p.pending == ch {
			p.pending = nil
		}
		o.mu.Unlock()
		close(ch)
	}()
	return p
}

func (o *OwnedPorts) probeAll(ports []LoadPort, stuck map[int]bool) []LoadPort {
	o.mu.Lock()
	if o.probes == nil {
		o.probes = map[int]*ownedProbe{}
	}
	listed := map[int]bool{}
	for _, p := range ports {
		listed[p.Port] = true
		if o.probes[p.Port] == nil {
			o.probes[p.Port] = &ownedProbe{}
		}
	}
	for port := range o.probes {
		if !listed[port] {
			delete(o.probes, port)
		}
	}
	o.mu.Unlock()

	capAt := time.Now().Add(ownedProbeCap)
	out := make([]LoadPort, len(ports))
	sem := make(chan struct{}, probeConcurrency)
	var wg sync.WaitGroup
	for i, p := range ports {
		sem <- struct{}{}
		wg.Add(1)
		go func(i int, p LoadPort) {
			defer func() { <-sem; wg.Done() }()
			o.mu.Lock()
			last := o.probes[p.Port].last
			o.mu.Unlock()
			stale := func() LoadPort {
				q := p
				if last != nil {
					q.Responding = *last
				}
				return q
			}
			if time.Now().After(capAt) || (stuck[p.Port] && last != nil) {
				o.startProbe(p.Port)
				out[i] = stale()
				return
			}
			pr := o.startProbe(p.Port)
			o.mu.Lock()
			ch := pr.pending
			o.mu.Unlock()
			if ch != nil {
				t := time.NewTimer(time.Until(capAt))
				select {
				case <-ch:
					t.Stop()
				case <-t.C:
					out[i] = stale()
					return
				}
			}
			o.mu.Lock()
			r := pr.result
			o.mu.Unlock()
			q := p
			q.Responding, q.Fresh = r, true
			out[i] = q
		}(i, p)
	}
	wg.Wait()
	return out
}

var probeTransport = &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true}

func headOnce(host string, port int, budget time.Duration) bool {
	if budget <= 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, "http://"+net.JoinHostPort(host, strconv.Itoa(port))+"/", nil)
	if err != nil {
		return false
	}
	resp, err := probeTransport.RoundTrip(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

// ProbeHTTP is whether an HTTP server answers a HEAD on the port: 127.0.0.1 first, then ::1 with what is left of the
// budget (a refused connection costs nothing).
func ProbeHTTP(port int, budget time.Duration) bool {
	start := time.Now()
	if headOnce("127.0.0.1", port, budget) {
		return true
	}
	return headOnce("::1", port, budget-time.Since(start))
}
