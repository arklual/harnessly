// Package ports lists the dev servers a preview can open (the listeners of this machine grouped by the conversation's
// work folders and the agents' worktrees) and maps a component picked in a preview to its source files.
package ports

import (
	"context"
	"errors"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/preview/devtools"
)

// Port is one listener as the preview lists it.
type Port struct {
	Port       int     `json:"port"`
	PID        *int    `json:"pid"`
	Command    *string `json:"command"`
	Cwd        *string `json:"cwd"`
	Group      string  `json:"group"`
	Owner      *Owner  `json:"owner"`
	Kind       string  `json:"kind"`
	Responding bool    `json:"responding"`
}

type Owner struct {
	Repo string `json:"repo"`
	Task string `json:"task"`
}

// Socket is one listening socket of `ss -ltnpH`; HasPID false for another user's process, which ss does not name.
type Socket struct {
	Port    int
	Host    string
	PID     int
	HasPID  bool
	Command string
}

var userProcess = regexp.MustCompile(`\("((?:[^"\\]|\\.)*)",pid=(\d+)`)

func splitAddress(local string) (string, int, bool) {
	at := strings.LastIndexByte(local, ':')
	if at < 0 {
		return "", 0, false
	}
	n := devtools.JSNumber(local[at+1:])
	if n != math.Trunc(n) || n < 1 || n > 65535 {
		return "", 0, false
	}
	host := strings.TrimSuffix(strings.TrimPrefix(local[:at], "["), "]")
	return host, int(n), true
}

// ParseSockets reads ss output, one socket per port: a named process wins over an unnamed one.
func ParseSockets(out string) []Socket {
	var order []int
	byPort := map[int]Socket{}
	for _, line := range strings.Split(out, "\n") {
		cols := strings.Fields(line)
		if len(cols) < 5 || cols[0] != "LISTEN" {
			continue
		}
		host, port, ok := splitAddress(cols[3])
		if !ok {
			continue
		}
		s := Socket{Port: port, Host: host}
		if m := userProcess.FindStringSubmatch(line); m != nil {
			s.PID, _ = strconv.Atoi(m[2])
			s.HasPID, s.Command = true, m[1]
		}
		known, seen := byPort[port]
		if !seen {
			order = append(order, port)
		}
		if !seen || !known.HasPID && s.HasPID {
			byPort[port] = s
		}
	}
	out2 := make([]Socket, 0, len(order))
	for _, p := range order {
		out2 = append(out2, byPort[p])
	}
	return out2
}

// ListenerOf is the visible listener of a port with a process; nil otherwise.
func ListenerOf(sockets []Socket, port int, hidden map[int]bool) *Socket {
	if hidden[port] {
		return nil
	}
	for _, s := range sockets {
		if s.Port == port {
			if !s.HasPID {
				return nil
			}
			return &s
		}
	}
	return nil
}

func isInside(cwd, root string) bool {
	base := strings.TrimRight(root, "/")
	return base != "" && (cwd == base || strings.HasPrefix(cwd, base+"/"))
}

var hashSuffix = regexp.MustCompile(`-[0-9a-f]{8}$`)

func ownerOfWorktree(cwd, worktrees string) *Owner {
	base := strings.TrimRight(worktrees, "/")
	if !strings.HasPrefix(cwd, base+"/") {
		return nil
	}
	parts := strings.Split(cwd[len(base)+1:], "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return nil
	}
	return &Owner{Repo: hashSuffix.ReplaceAllString(parts[0], ""), Task: parts[1]}
}

var groupOrder = map[string]int{"chat": 0, "agent": 1, "other": 2}

// Classify groups the visible sockets: in a work folder of the conversation (chat), in an agent's worktree (agent),
// or anything else.
func Classify(sockets []Socket, cwdOf func(int) string, workPaths []string, worktrees string, hidden map[int]bool) []Port {
	out := []Port{}
	for _, s := range sockets {
		if hidden[s.Port] {
			continue
		}
		p := Port{Port: s.Port, Kind: "unknown", Group: "other"}
		if s.HasPID {
			pid, cmd := s.PID, s.Command
			p.PID, p.Command = &pid, &cmd
			if cwd := cwdOf(s.PID); cwd != "" {
				p.Cwd = &cwd
				inside := false
				for _, w := range workPaths {
					if isInside(cwd, w) {
						inside = true
						break
					}
				}
				if inside {
					p.Group = "chat"
				} else if owner := ownerOfWorktree(cwd, worktrees); owner != nil {
					p.Group, p.Owner = "agent", owner
				}
			}
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if groupOrder[out[i].Group] != groupOrder[out[j].Group] {
			return groupOrder[out[i].Group] < groupOrder[out[j].Group]
		}
		return out[i].Port < out[j].Port
	})
	return out
}

// headOnce is one HEAD / of a port; ok false when nothing answered within the budget.
func headOnce(host string, port int, budget time.Duration) (kind string, ok bool) {
	if budget <= 0 {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodHead, "http://"+joinHostPort(host, port)+"/", nil)
	req.Header["User-Agent"] = []string{""}
	res, err := probeClient.Do(req)
	if err != nil {
		return "", false
	}
	_ = res.Body.Close()
	if strings.Contains(res.Header.Get("Content-Type"), "text/html") {
		return "site", true
	}
	return "api", true
}

var probeClient = &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}}

func joinHostPort(host string, port int) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]:" + strconv.Itoa(port)
	}
	return host + ":" + strconv.Itoa(port)
}

// Probe asks a port what it serves: ::1 gets only what 127.0.0.1 left of the budget.
func Probe(port int, budget time.Duration) (string, bool) {
	start := time.Now()
	if kind, ok := headOnce("127.0.0.1", port, budget); ok {
		return kind, true
	}
	if kind, ok := headOnce("::1", port, budget-time.Since(start)); ok {
		return kind, true
	}
	return "unknown", false
}

func trimSlash(v string) string {
	v = strings.TrimRight(v, "/")
	if v == "" {
		return "/"
	}
	return v
}

func isAncestorOrSelf(candidate, of string) bool {
	return candidate == "/" || of == candidate || strings.HasPrefix(of, candidate+"/")
}

// NormaliseWorkRoots turns a conversation's work paths into the roots its dev servers run in. Dropped: "/", home and
// the worktrees folder with everything above them, a repository folder of worktrees, and a root another kept root lies
// inside — the most specific one wins.
func NormaliseWorkRoots(paths []string, resolveRoot func(string) string, worktrees, home string) []string {
	wt, homeDir := trimSlash(worktrees), trimSlash(home)
	var roots []string
	seen := map[string]bool{}
	for _, p := range paths {
		r := trimSlash(resolveRoot(p))
		if seen[r] {
			continue
		}
		seen[r] = true
		if isAncestorOrSelf(r, wt) || isAncestorOrSelf(r, homeDir) {
			continue
		}
		if strings.HasPrefix(r, wt+"/") && !strings.Contains(r[len(wt)+1:], "/") {
			continue
		}
		roots = append(roots, r)
	}
	out := []string{}
	for _, r := range roots {
		inner := false
		for _, o := range roots {
			if o != r && strings.HasPrefix(o, r+"/") {
				inner = true
				break
			}
		}
		if !inner {
			out = append(out, r)
		}
	}
	return out
}

// Scanner lists and probes ports, remembering scans, probes and repository roots briefly.
type Scanner struct {
	Home      string
	Worktrees string
	Hidden    func() map[int]bool
	// SS runs `ss -ltnpH`; tests give their own.
	SS    func() (string, error)
	Cwd   func(pid int) string
	Probe func(port int) (string, bool)

	mu     sync.Mutex
	scans  map[string]scanEntry
	probes map[int]probeEntry
	roots  map[string]rootEntry
}

type scanEntry struct {
	at    time.Time
	ports []Port
	err   error
	done  chan struct{}
}

type probeEntry struct {
	at   time.Time
	kind string
	ok   bool
	done chan struct{}
}

type rootEntry struct {
	at   time.Time
	root string
}

const (
	scanTTL      = 2 * time.Second
	rootTTL      = 60 * time.Second
	ssTimeout    = 2 * time.Second
	gitTimeout   = 2 * time.Second
	probeTimeout = 800 * time.Millisecond
)

func RunSS() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ssTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ss", "-ltnpH").Output()
	return string(out), err
}

func ProcCwd(pid int) string {
	cwd, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
	if err != nil {
		return ""
	}
	return cwd
}

func (s *Scanner) init() {
	if s.scans == nil {
		s.scans, s.probes, s.roots = map[string]scanEntry{}, map[int]probeEntry{}, map[string]rootEntry{}
	}
	if s.SS == nil {
		s.SS = RunSS
	}
	if s.Cwd == nil {
		s.Cwd = ProcCwd
	}
	if s.Probe == nil {
		s.Probe = func(port int) (string, bool) { return Probe(port, probeTimeout) }
	}
}

func (s *Scanner) hidden() map[int]bool {
	if s.Hidden == nil {
		return map[int]bool{}
	}
	return s.Hidden()
}

// resolveRoot is the git toplevel of the path's directory (the directory itself outside a repository).
func (s *Scanner) resolveRoot(target string) string {
	dir := filepath.Dir(target)
	if st, err := os.Stat(target); err == nil && st.IsDir() {
		dir = target
	}
	s.mu.Lock()
	if e, ok := s.roots[dir]; ok && time.Since(e.at) < rootTTL {
		s.mu.Unlock()
		return e.root
	}
	s.mu.Unlock()
	root := dir
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	if out, err := cmd.Output(); err == nil {
		if t := strings.TrimSpace(string(out)); t != "" {
			root = t
		}
	}
	cancel()
	s.mu.Lock()
	s.roots[dir] = rootEntry{time.Now(), root}
	for k, e := range s.roots {
		if time.Since(e.at) >= rootTTL {
			delete(s.roots, k)
		}
	}
	s.mu.Unlock()
	return root
}

func (s *Scanner) probe(port int) (string, bool) {
	s.mu.Lock()
	if e, ok := s.probes[port]; ok && time.Since(e.at) < scanTTL {
		s.mu.Unlock()
		<-e.done
		s.mu.Lock()
		e = s.probes[port]
		s.mu.Unlock()
		return e.kind, e.ok
	}
	e := probeEntry{at: time.Now(), done: make(chan struct{})}
	s.probes[port] = e
	s.mu.Unlock()
	kind, ok := s.Probe(port)
	s.mu.Lock()
	e.kind, e.ok = kind, ok
	s.probes[port] = e
	for k, x := range s.probes {
		if time.Since(x.at) >= scanTTL && k != port {
			delete(s.probes, k)
		}
	}
	s.mu.Unlock()
	close(e.done)
	return kind, ok
}

func (s *Scanner) scan(workPaths []string) ([]Port, error) {
	out, err := s.SS()
	if err != nil {
		return nil, err
	}
	ports := Classify(ParseSockets(out), s.Cwd, workPaths, s.Worktrees, s.hidden())
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range ports {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			ports[i].Kind, ports[i].Responding = s.probe(ports[i].Port)
		}(i)
	}
	wg.Wait()
	return ports, nil
}

// List is the ports a preview of a conversation with these work paths can open.
func (s *Scanner) List(paths []string) ([]Port, error) {
	s.mu.Lock()
	s.init()
	s.mu.Unlock()
	roots := NormaliseWorkRoots(paths, s.resolveRoot, s.Worktrees, s.Home)
	sorted := append([]string(nil), roots...)
	sort.Strings(sorted)
	key := strings.Join(sorted, "\x00")
	s.mu.Lock()
	if e, ok := s.scans[key]; ok && time.Since(e.at) < scanTTL {
		s.mu.Unlock()
		<-e.done
		s.mu.Lock()
		e = s.scans[key]
		s.mu.Unlock()
		return e.ports, e.err
	}
	e := scanEntry{at: time.Now(), done: make(chan struct{})}
	s.scans[key] = e
	s.mu.Unlock()
	ports, err := s.scan(roots)
	s.mu.Lock()
	e.ports, e.err = ports, err
	if err != nil {
		delete(s.scans, key)
	} else {
		s.scans[key] = e
	}
	for k, x := range s.scans {
		if time.Since(x.at) >= scanTTL && k != key {
			delete(s.scans, k)
		}
	}
	s.mu.Unlock()
	close(e.done)
	return ports, err
}

// PortRoot is the repository root of the process listening on port; "" for a port that is not a visible dev server.
func (s *Scanner) PortRoot(port int) (string, error) {
	s.mu.Lock()
	s.init()
	s.mu.Unlock()
	out, err := s.SS()
	if err != nil {
		return "", err
	}
	sock := ListenerOf(ParseSockets(out), port, s.hidden())
	if sock == nil {
		return "", nil
	}
	cwd := s.Cwd(sock.PID)
	if cwd == "" {
		return "", nil
	}
	root := s.resolveRoot(cwd)
	if root == "/" || root == trimSlash(s.Home) {
		return "", nil
	}
	return root, nil
}

// Listener is whoever listens on port, for the dev server log.
func (s *Scanner) Listener(port int) (*devtools.Listener, error) {
	s.mu.Lock()
	s.init()
	s.mu.Unlock()
	out, err := s.SS()
	if err != nil {
		return nil, err
	}
	for _, sock := range ParseSockets(out) {
		if sock.Port == port {
			return &devtools.Listener{Port: sock.Port, Host: sock.Host, PID: sock.PID, Command: sock.Command, HasPID: sock.HasPID}, nil
		}
	}
	return nil, errors.New("no listener")
}
