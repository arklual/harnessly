package wire

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// The Machine tool of the page's right pane: how busy the machine is now and was in the last hour, per core, over the
// network and on the disk, and the heaviest processes of the runtime's user, one of which the person may stop.

// DiskPartID names a part of the disk layout. Other is the used part none of the named folders accounts for.
type DiskPartID string

const (
	DiskPartWorkspace DiskPartID = "workspace"
	DiskPartCaches    DiskPartID = "caches"
	DiskPartAgents    DiskPartID = "agents"
	DiskPartOther     DiskPartID = "other"
	DiskPartFree      DiskPartID = "free"
)

// DiskPartIDs is every DiskPartID, in the schema's order (the order the parts are drawn).
var DiskPartIDs = []DiskPartID{DiskPartWorkspace, DiskPartCaches, DiskPartAgents, DiskPartOther, DiskPartFree}

// MachineDiskPart is one part of the disk with its size in bytes.
type MachineDiskPart struct {
	ID    DiskPartID `json:"id"`
	Bytes int64      `json:"bytes"`
}

func (p MachineDiskPart) Validate() error {
	return errors.Join(in("id", p.ID, DiskPartIDs), nonNegative("bytes", p.Bytes))
}

// MachineDisk is the filesystem of the runtime user's home and what its used part is made of. The folders are measured
// in the background, so the layout is as old as `measuredAt` (null: not measured yet, all that is used is `other`);
// `partial`: the walk ran out of time, the named parts are lower bounds.
type MachineDisk struct {
	Path       string            `json:"path"`
	Total      int64             `json:"total"`
	Free       int64             `json:"free"`
	Parts      []MachineDiskPart `json:"parts"`
	MeasuredAt *int64            `json:"measuredAt"`
	Partial    bool              `json:"partial"`
}

func (d MachineDisk) Validate() error {
	return errors.Join(need("path", d.Path), nonNegative("total", d.Total), nonNegative("free", d.Free), each("parts", d.Parts))
}

// MachineSample is the machine at one moment of the last hour: the busy share of the processor and the share of the
// memory in use (0…1), the bytes over the network per second.
type MachineSample struct {
	At  int64   `json:"at"`
	CPU float64 `json:"cpu"`
	Mem float64 `json:"mem"`
	Rx  float64 `json:"rx"`
	Tx  float64 `json:"tx"`
}

func (s MachineSample) Validate() error {
	return errors.Join(nonNegative("at", s.At), share("cpu", s.CPU), share("mem", s.Mem), rate("rx", s.Rx), rate("tx", s.Tx))
}

func share(name string, v float64) error {
	if !(v >= 0 && v <= 1) {
		return fmt.Errorf("%s: %v is not in 0…1", name, v)
	}
	return nil
}

func rate(name string, v float64) error {
	if !(v >= 0) {
		return fmt.Errorf("%s: %v is below 0", name, v)
	}
	return nil
}

// MachineProcKind is what a listed process is to the platform: a service of the platform or the runtime itself, an agent
// or its tool, a dev server, any other process of the user.
type MachineProcKind string

const (
	ProcService MachineProcKind = "service"
	ProcAgent   MachineProcKind = "agent"
	ProcDev     MachineProcKind = "dev"
	ProcOther   MachineProcKind = "other"
)

// MachineProcKinds is every MachineProcKind, in the schema's order.
var MachineProcKinds = []MachineProcKind{ProcService, ProcAgent, ProcDev, ProcOther}

// MachineProc is a heavy process of the runtime's user. `startTime` with `pid` names it for its whole life (a stop names
// both); `name` is what it is called ("node · vite"), `command` the command line with secrets hidden, cut at 160
// characters. `cpu` is the share of one core like top shows it (a busy multi-threaded process passes 100), `rss` the
// resident memory in bytes. `guard`: the runtime, a service of the platform, tmux or an agent of a chat; it is shown and
// never stopped. `stuck`: it listens on `port` and has not answered for several looks.
type MachineProc struct {
	PID       int             `json:"pid"`
	StartTime string          `json:"startTime"`
	Name      string          `json:"name"`
	Command   string          `json:"command"`
	CPU       float64         `json:"cpu"`
	RSS       int64           `json:"rss"`
	Kind      MachineProcKind `json:"kind"`
	Port      *int            `json:"port"`
	Stuck     bool            `json:"stuck"`
	Guard     bool            `json:"guard"`
}

func (p MachineProc) Validate() error {
	var port error
	if p.Port != nil && (*p.Port < 1 || *p.Port > 65535) {
		port = fmt.Errorf("port: %d", *p.Port)
	}
	// Init is listed (in a container it is a shell), never stopped: pid 1 comes only guarded.
	var pid error
	if p.PID < 1 || p.PID == 1 && !p.Guard {
		pid = fmt.Errorf("pid: %d", p.PID)
	}
	var start error
	if !startTimeText.MatchString(p.StartTime) {
		start = fmt.Errorf("startTime: %q is not a start time", p.StartTime)
	}
	return errors.Join(pid, start, need("name", p.Name), rate("cpu", p.CPU), nonNegative("rss", p.RSS),
		in("kind", p.Kind, MachineProcKinds), port)
}

// MachineMem is the memory in bytes: in use (what a new program cannot have) of the whole.
type MachineMem struct {
	Used  int64 `json:"used"`
	Total int64 `json:"total"`
}

// MachineNet is the bytes per second over the machine's own links, in and out, now.
type MachineNet struct {
	Rx float64 `json:"rx"`
	Tx float64 `json:"tx"`
}

// MachineStats is the answer of GET /api/v2/machine/stats. `cpu`, `cores[]` are the busy share (0…1) of the processor and
// of each core now; `history` is the last hour thinned to a point every two minutes, oldest first, the last one being
// now; a point is left out for a stretch the runtime was not running. `procs` are the heaviest processes by memory and
// processor together, the heaviest first.
type MachineStats struct {
	At      int64           `json:"at"`
	CPU     float64         `json:"cpu"`
	Cores   []float64       `json:"cores"`
	Mem     MachineMem      `json:"mem"`
	Net     MachineNet      `json:"net"`
	Disk    MachineDisk     `json:"disk"`
	History []MachineSample `json:"history"`
	Procs   []MachineProc   `json:"procs"`
}

func (s MachineStats) Validate() error {
	var cores error
	for i, c := range s.Cores {
		cores = errors.Join(cores, share(fmt.Sprintf("cores[%d]", i), c))
	}
	last := int64(-1)
	var order error
	for _, h := range s.History {
		if h.At <= last {
			order = errors.New("history: not oldest first")
		}
		last = h.At
	}
	return errors.Join(nonNegative("at", s.At), share("cpu", s.CPU), cores, nonNegative("mem.used", s.Mem.Used), nonNegative("mem.total", s.Mem.Total),
		rate("net.rx", s.Net.Rx), rate("net.tx", s.Net.Tx), under("disk", s.Disk.Validate()), each("history", s.History), order, each("procs", s.Procs))
}

// MachineStop is the body of POST /api/v2/machine/processes/stop: the process the page was shown.
type MachineStop struct {
	PID       int    `json:"pid"`
	StartTime string `json:"startTime"`
}

// startTimeText is the process's start time as the kernel counts it (field 22 of its stat), in decimal.
var startTimeText = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

func (m MachineStop) Validate() error {
	var pid, start error
	if m.PID < 2 {
		pid = fmt.Errorf("pid: %d", m.PID)
	}
	if !startTimeText.MatchString(m.StartTime) {
		start = fmt.Errorf("startTime: %q is not a start time", m.StartTime)
	}
	return errors.Join(pid, start)
}

// MachineStopped is the answer of a stop: the signal that ended the process (SIGTERM, or SIGKILL when it was still
// there five seconds later).
type MachineStopped struct {
	Stopped bool   `json:"stopped"`
	Signal  string `json:"signal"`
}

func (m MachineStopped) Validate() error {
	var stopped error
	if !m.Stopped {
		stopped = errors.New("stopped: false")
	}
	return errors.Join(stopped, in("signal", m.Signal, []string{"SIGTERM", "SIGKILL"}))
}

// MachineStopReason says why a process was not stopped.
type MachineStopReason string

const (
	StopGone         MachineStopReason = "gone"          // it has ended already
	StopChanged      MachineStopReason = "changed"       // the pid is another process now
	StopForeign      MachineStopReason = "foreign"       // it is not the runtime user's
	StopProtected    MachineStopReason = "protected"     // the runtime, tmux, or a service of the platform
	StopNotStoppable MachineStopReason = "not-stoppable" // an agent of a chat or its tool
	StopNotListed    MachineStopReason = "not-listed"    // not among the heavy processes the page was shown
	StopBusy         MachineStopReason = "busy"          // the machine did not answer in time
	StopFailed       MachineStopReason = "failed"        // the signal could not be sent
)

// MachineStopReasons is every MachineStopReason, in the schema's order.
var MachineStopReasons = []MachineStopReason{StopGone, StopChanged, StopForeign, StopProtected, StopNotStoppable, StopNotListed, StopBusy, StopFailed}

// MachineRefusal is the answer of a stop that is refused: the code of the protocol, why, and a message worded for the
// person.
type MachineRefusal struct {
	R       ResultKind        `json:"r"`
	Code    RefuseCode        `json:"code"`
	Reason  MachineStopReason `json:"reason"`
	Message string            `json:"message"`
}

func (m MachineRefusal) Validate() error {
	return errors.Join(in("r", m.R, []ResultKind{ResultRefused}), in("code", m.Code, RefuseCodes), in("reason", m.Reason, MachineStopReasons), need("message", m.Message))
}

// MachineStopError is what a MachineToolSource refuses a stop with.
type MachineStopError struct {
	Reason  MachineStopReason
	Message string
}

func (e *MachineStopError) Error() string { return e.Message }

// stopRefusal is the code a reason is refused with.
var stopRefusal = map[MachineStopReason]RefuseCode{
	StopGone: RefuseNotFound, StopChanged: RefuseConflict, StopForeign: RefuseForbidden, StopProtected: RefuseForbidden,
	StopNotStoppable: RefuseForbidden, StopNotListed: RefuseForbidden, StopBusy: RefuseTimeout, StopFailed: RefuseInternal,
}

// MachineToolSource serves the Machine tool. Stats never waits for a look at the machine longer than its own budget; Stop
// refuses with a *MachineStopError, and checks again on a fresh look at /proc that the process is the one named, is the
// runtime user's and is among those Stats lists as stoppable.
type MachineToolSource interface {
	Stats(ctx context.Context) (MachineStats, error)
	Stop(ctx context.Context, req MachineStop) (MachineStopped, error)
}

// MachineStopBodyMax is the largest body a stop may send.
const MachineStopBodyMax = 4 << 10

// machineTool serves GET /api/v2/machine/stats and POST /api/v2/machine/processes/stop.
func (s *Server) machineTool(w http.ResponseWriter, r *http.Request, what string) {
	if s.MachineTool == nil || (what != "stats" && what != "processes/stop") {
		refuse(w, RefuseNotFound)
		return
	}
	want := http.MethodGet
	if what == "processes/stop" {
		want = http.MethodPost
	}
	if r.Method != want {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	if what == "stats" {
		stats, err := s.MachineTool.Stats(ctx)
		if err != nil {
			s.machineToolFailed(w, r, err)
			return
		}
		s.writeChecked(w, r, stats)
		return
	}
	var body MachineStop
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MachineStopBodyMax))
	if err == nil {
		err = decodeStrict(raw, &body)
	}
	if err == nil {
		err = body.Validate()
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, Refused(RefuseBadRequest))
		return
	}
	done, err := s.MachineTool.Stop(ctx, body)
	var se *MachineStopError
	if errors.As(err, &se) {
		code := stopRefusal[se.Reason]
		status := refusalStatus[code]
		writeJSON(w, status, MachineRefusal{R: ResultRefused, Code: code, Reason: se.Reason, Message: se.Message})
		return
	}
	if err != nil {
		s.machineToolFailed(w, r, err)
		return
	}
	s.writeChecked(w, r, done)
}

func (s *Server) machineToolFailed(w http.ResponseWriter, r *http.Request, err error) {
	if r.Context().Err() != nil || errors.Is(err, context.DeadlineExceeded) {
		refuse(w, RefuseTimeout)
		return
	}
	jsonlog.Log("warn", "v2: the machine tool failed", jsonlog.F{K: "path", V: r.URL.Path}, jsonlog.F{K: "error", V: err.Error()})
	refuse(w, RefuseInternal)
}
