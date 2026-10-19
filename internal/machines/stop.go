package machines

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
	"time"

	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
)

// Stopping a heavy process from the machine load list: every check runs on a fresh read of /proc, nothing from the
// page is trusted but the pid and its start time. A process may be stopped only while the list offers it, and it is
// classified here exactly as the list classified it.

const (
	termWait = 5 * time.Second
	termPoll = 100 * time.Millisecond
	pidMax   = 4_194_304
)

// StopRequest names one process for its whole life: its pid and field 22 of its stat, as decimal text.
type StopRequest struct {
	PID       int
	StartTime string
}

var startTimeText = regexp.MustCompile(`^(0|[1-9]\d*)$`)

// ParseStopRequest reads {pid, starttime} from a parsed JSON body; false for anything else.
func ParseStopRequest(body any) (StopRequest, bool) {
	o, ok := body.(*jsvalue.Object)
	if !ok {
		return StopRequest{}, false
	}
	pidValue, _ := o.Get("pid")
	pid, ok := pidValue.(float64)
	if !ok || pid != math.Trunc(pid) || math.IsInf(pid, 0) || pid <= 1 || pid > pidMax {
		return StopRequest{}, false
	}
	switch st, _ := o.Get("starttime"); v := st.(type) {
	case float64:
		if v != math.Trunc(v) || math.Abs(v) > 1<<53-1 || v < 0 {
			return StopRequest{}, false
		}
		return StopRequest{PID: int(pid), StartTime: jscompat.Number(v)}, true
	case string:
		if startTimeText.MatchString(v) {
			return StopRequest{PID: int(pid), StartTime: v}, true
		}
	}
	return StopRequest{}, false
}

// StopResult is the answer: a status and {stopped, signal} or {error}.
type StopResult struct {
	Status int
	Body   *jsvalue.Object
}

func refuseStop(status int, reason string) StopResult {
	return StopResult{status, jsvalue.NewObject("error", reason)}
}

func stopped(signal string) StopResult {
	return StopResult{200, jsvalue.NewObject("stopped", true, "signal", signal)}
}

// errProcRead is a /proc read that failed for another reason than the process being gone or closed to the user.
var errProcRead = errors.New("proc read failed")

// Stopper stops processes the load monitor lists.
type Stopper struct {
	// Listed is what the load list offers now: only these may be stopped.
	Listed      func() ([]LoadProcess, error)
	UID         int
	RuntimePIDs func() []int
	ReleaseRoot string
	PanePIDs    func() []int
	ProcRoot    string
	// Kill sends a signal; Sleep waits between the checks after SIGTERM. Tests replace both.
	Kill  func(pid int, sig syscall.Signal) error
	Sleep func(time.Duration)
}

type freshProc struct {
	procStat
	state string
}

func (s *Stopper) root() string {
	if s.ProcRoot == "" {
		return "/proc"
	}
	return s.ProcRoot
}

// read is a /proc file of a process; false when the process is gone or the file is closed to the user.
func (s *Stopper) read(pid int, name string) (string, bool, error) {
	raw, err := os.ReadFile(filepath.Join(s.root(), strconv.Itoa(pid), name))
	if err != nil {
		switch {
		case errors.Is(err, syscall.ENOENT), errors.Is(err, syscall.ESRCH), errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
			return "", false, nil
		}
		return "", false, errProcRead
	}
	return string(raw), true, nil
}

func (s *Stopper) link(pid int, name string) string {
	target, err := os.Readlink(filepath.Join(s.root(), strconv.Itoa(pid), name))
	if err != nil {
		return ""
	}
	return target
}

func (s *Stopper) stat(pid int) (*freshProc, error) {
	text, ok, err := s.read(pid, "stat")
	if err != nil || !ok || text == "" {
		return nil, err
	}
	p, ok := parseProcStat(text)
	if !ok {
		return nil, nil
	}
	state := ""
	if i := lastIndexByte(text, ')'); i+3 <= len(text) {
		state = text[i+2 : i+3]
	}
	return &freshProc{p, state}, nil
}

func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// same: alive and still the process the person clicked (a zombie or a reused pid is not).
func same(p *freshProc, startTime string) bool {
	return p != nil && p.state != "Z" && jscompat.Number(p.starttime) == startTime
}

// own: both the real and the effective uid are the user's; a setuid child (sudo) relays signals to root.
func (s *Stopper) own(pid int) (bool, error) {
	status, ok, err := s.read(pid, "status")
	if err != nil || !ok {
		return false, err
	}
	real, effective, ok := parseProcUids(status)
	return ok && real == s.UID && effective == s.UID, nil
}

func (s *Stopper) args(pid int) (string, error) {
	raw, _, err := s.read(pid, "cmdline")
	return jscompat.Trim(stringsReplaceNUL(raw)), err
}

func stringsReplaceNUL(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] == 0 {
			b[i] = ' '
		}
	}
	return string(b)
}

func (s *Stopper) ancestors(from int) ([]*freshProc, error) {
	var chain []*freshProc
	seen := map[int]bool{from: true}
	first, err := s.stat(from)
	if err != nil {
		return nil, err
	}
	pid := 0
	if first != nil {
		pid = first.ppid
	}
	for pid > 0 && !seen[pid] && len(chain) < maxLineage {
		seen[pid] = true
		p, err := s.stat(pid)
		if err != nil {
			return nil, err
		}
		if p == nil {
			break
		}
		chain = append(chain, p)
		pid = p.ppid
	}
	return chain, nil
}

// Stop sends SIGTERM to a listed process of the user, and SIGKILL when it is still there five seconds later. An error
// is a failed read of /proc or of the load list (ErrScanTimeout).
func (s *Stopper) Stop(req StopRequest) (StopResult, error) {
	proc, err := s.stat(req.PID)
	if err != nil {
		return StopResult{}, err
	}
	if proc == nil || proc.state == "Z" {
		return refuseStop(404, "gone"), nil
	}
	if jscompat.Number(proc.starttime) != req.StartTime {
		return refuseStop(409, "changed"), nil
	}
	if own, err := s.own(req.PID); err != nil {
		return StopResult{}, err
	} else if !own {
		return refuseStop(403, "foreign"), nil
	}

	runtimePIDs := s.RuntimePIDs()
	runtimeSet, lineage := map[int]bool{}, map[int]bool{}
	for _, pid := range runtimePIDs {
		runtimeSet[pid], lineage[pid] = true, true
		up, err := s.ancestors(pid)
		if err != nil {
			return StopResult{}, err
		}
		for _, a := range up {
			lineage[a.pid] = true
		}
	}
	chain, err := s.ancestors(req.PID)
	if err != nil {
		return StopResult{}, err
	}
	isRuntime := lineage[req.PID]
	for _, a := range chain {
		isRuntime = isRuntime || runtimeSet[a.pid]
	}
	if isRuntime {
		return refuseStop(403, "protected"), nil
	}

	listed, err := s.Listed()
	if err != nil {
		return StopResult{}, err
	}
	var entry *LoadProcess
	for i := range listed {
		if p := &listed[i]; p.PID == req.PID && jscompat.Number(float64(p.StartTime)) == req.StartTime {
			entry = p
			break
		}
	}
	units := map[string]bool{}
	for _, pid := range runtimePIDs {
		cg, _, err := s.read(pid, "cgroup")
		if err != nil {
			return StopResult{}, err
		}
		if u := systemServiceUnit(cg); u != "" {
			units[u] = true
		}
	}
	isService := func(pid int) (bool, error) {
		args, err := s.args(pid)
		if err != nil {
			return false, err
		}
		cg, _, err := s.read(pid, "cgroup")
		if err != nil {
			return false, err
		}
		return isSystemService(serviceInput{cgroup: cg, exe: s.link(pid, "exe"), cwd: s.link(pid, "cwd"), args: args, runtimeUnits: units}, s.ReleaseRoot), nil
	}
	service, err := isService(req.PID)
	if err != nil {
		return StopResult{}, err
	}
	// The user's own nearest parents: a foreign one ends the walk, as in the list. Anything a service spawned is part of
	// it, up to a tmux server: whatever runs in a pane is the user's work.
	var own []*freshProc
	for _, a := range chain {
		ok, err := s.own(a.pid)
		if err != nil {
			return StopResult{}, err
		}
		if !ok {
			break
		}
		own = append(own, a)
	}
	for _, a := range own {
		if service || isTmuxComm(a.comm) {
			break
		}
		if service, err = isService(a.pid); err != nil {
			return StopResult{}, err
		}
	}
	agentAncestor := false
	for _, a := range own[:min(agentAncestorDepth, len(own))] {
		args, err := s.args(a.pid)
		if err != nil {
			return StopResult{}, err
		}
		if isAgentProcess(a.comm, args) {
			agentAncestor = true
			break
		}
	}
	args, err := s.args(req.PID)
	if err != nil {
		return StopResult{}, err
	}
	isPane := false
	if s.PanePIDs != nil {
		for _, pid := range s.PanePIDs() {
			isPane = isPane || pid == req.PID
		}
	}
	kind := classifyKind(kindInput{comm: proc.comm, args: args, hasPort: entry != nil && entry.Port != nil, isPanePid: isPane,
		hasAgentAncestor: agentAncestor, isSystemService: service})
	if kind == "service" {
		return refuseStop(403, "protected"), nil
	}
	if !isStoppableKind(kind) {
		return refuseStop(403, "not-stoppable"), nil
	}
	if entry == nil {
		return refuseStop(403, "not-listed"), nil
	}

	before, err := s.stat(req.PID)
	if err != nil {
		return StopResult{}, err
	}
	if before == nil || before.state == "Z" {
		return refuseStop(404, "gone"), nil
	}
	if !same(before, req.StartTime) {
		return refuseStop(409, "changed"), nil
	}
	switch err := s.Kill(req.PID, syscall.SIGTERM); {
	case errors.Is(err, syscall.ESRCH):
		return refuseStop(404, "gone"), nil
	case errors.Is(err, syscall.EPERM):
		return refuseStop(403, "foreign"), nil
	case err != nil:
		return StopResult{}, err
	}
	for waited := time.Duration(0); waited < termWait; waited += termPoll {
		s.Sleep(termPoll)
		p, err := s.stat(req.PID)
		if err != nil {
			return StopResult{}, err
		}
		if !same(p, req.StartTime) {
			return stopped("SIGTERM"), nil
		}
	}
	switch err := s.Kill(req.PID, syscall.SIGKILL); {
	case errors.Is(err, syscall.ESRCH):
		return stopped("SIGTERM"), nil
	case errors.Is(err, syscall.EPERM):
		return refuseStop(403, "foreign"), nil
	case err != nil:
		return StopResult{}, err
	}
	return stopped("SIGKILL"), nil
}
