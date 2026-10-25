package wirefake

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// fakeProcs are the heavy processes of the fake machine: a hung build and a dev server the person may stop, the agent
// of a chat and the container service, which are guarded.
func fakeProcs() []wire.MachineProc {
	port := 4400
	return []wire.MachineProc{
		{PID: 4211, StartTime: "88120411", Name: "java · gradle", Command: "java -jar gradle-launcher.jar --daemon", CPU: 61, RSS: 4509715456, Kind: wire.ProcOther},
		{PID: 4102, StartTime: "88119902", Name: "node · vite", Command: "node node_modules/.bin/vite --port 4400", CPU: 22, RSS: 1932735283, Kind: wire.ProcDev, Port: &port},
		{PID: 4390, StartTime: "88121100", Name: "node · vitest", Command: "node node_modules/.bin/vitest --watch", CPU: 9, RSS: 966367641, Kind: wire.ProcOther},
		{PID: 977, StartTime: "1200", Name: "claude", Command: "claude --resume", CPU: 4, RSS: 644245094, Kind: wire.ProcAgent, Guard: true},
		{PID: 612, StartTime: "800", Name: "dockerd", Command: "/usr/bin/dockerd -H fd://", CPU: 3, RSS: 2576980377, Kind: wire.ProcService, Guard: true},
	}
}

// machineTool is the fake machine's Machine tool: loads on slow waves, so the page sees them move, and processes that go
// when stopped.
type machineTool struct {
	mu    sync.Mutex
	procs []wire.MachineProc
}

var _ wire.MachineToolSource = (*machineTool)(nil)

// Tool is the fake machine's Machine tool: eight cores, an hour of history, the processes of fakeProcs. A stop ends a
// process for good (until the fake is reset); a guarded one, or one not listed, is refused as the runtime refuses it.
func (m *Machine) Tool() wire.MachineToolSource { return m.tool }

func wave(t time.Time, base, amp, seed float64) float64 {
	x := float64(t.Unix()) / 60
	return base + amp*math.Sin(x/3.1+seed) + amp*0.4*math.Sin(x*1.7+seed)
}

func clamp01(x float64) float64 { return math.Min(1, math.Max(0, x)) }

func (f *machineTool) Stats(_ context.Context) (wire.MachineStats, error) {
	now := time.Now()
	const total, free = int64(128) << 30, int64(59) << 30
	out := wire.MachineStats{
		At: now.UnixMilli(), CPU: clamp01(wave(now, 0.4, 0.1, 0)),
		Mem: wire.MachineMem{Used: int64(clamp01(wave(now, 0.55, 0.04, 2)) * float64(int64(32)<<30)), Total: int64(32) << 30},
		Net: wire.MachineNet{Rx: math.Max(5e4, wave(now, 1.4e6, 5e5, 5)), Tx: math.Max(2e4, wave(now, 4e5, 1.5e5, 1))},
		Disk: wire.MachineDisk{Path: "/home/coder", Total: total, Free: free, MeasuredAt: new(now.UnixMilli()), Parts: []wire.MachineDiskPart{
			{ID: wire.DiskPartWorkspace, Bytes: int64(34) << 30}, {ID: wire.DiskPartCaches, Bytes: int64(15) << 30},
			{ID: wire.DiskPartAgents, Bytes: int64(4) << 30}, {ID: wire.DiskPartOther, Bytes: total - free - int64(53)<<30}, {ID: wire.DiskPartFree, Bytes: free}}},
	}
	for i := range 8 {
		out.Cores = append(out.Cores, clamp01(wave(now, 0.4, 0.2, float64(i)*2.3)))
	}
	for i := range 30 {
		at := now.Add(-time.Duration(29-i) * 2 * time.Minute)
		out.History = append(out.History, wire.MachineSample{At: at.UnixMilli(), CPU: clamp01(wave(at, 0.4, 0.1, 0)), Mem: clamp01(wave(at, 0.55, 0.04, 2)),
			Rx: math.Max(5e4, wave(at, 1.4e6, 5e5, 5)), Tx: math.Max(2e4, wave(at, 4e5, 1.5e5, 1))})
	}
	f.mu.Lock()
	out.Procs = append([]wire.MachineProc{}, f.procs...)
	f.mu.Unlock()
	return out, nil
}

func (f *machineTool) Stop(_ context.Context, req wire.MachineStop) (wire.MachineStopped, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, p := range f.procs {
		if p.PID != req.PID {
			continue
		}
		switch {
		case p.StartTime != req.StartTime:
			return wire.MachineStopped{}, &wire.MachineStopError{Reason: wire.StopChanged, Message: "The process is another one now."}
		case p.Guard:
			return wire.MachineStopped{}, &wire.MachineStopError{Reason: wire.StopProtected, Message: "The process belongs to the platform."}
		}
		f.procs = append(f.procs[:i:i], f.procs[i+1:]...)
		return wire.MachineStopped{Stopped: true, Signal: "SIGTERM"}, nil
	}
	return wire.MachineStopped{}, &wire.MachineStopError{Reason: wire.StopGone, Message: "The process has ended already."}
}

func (f *machineTool) reset() {
	f.mu.Lock()
	f.procs = fakeProcs()
	f.mu.Unlock()
}
