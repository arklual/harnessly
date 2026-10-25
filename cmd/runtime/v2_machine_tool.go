package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/machines"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The Machine tool of the page's right pane (/api/v2/machine/*) is the Legacy machine panel's look at the machine and
// the same stopper, with the hour of history and the disk's layout added. Both read through one kit, so the machine is
// scanned once for whoever asks.

// machineKit is what looks at the machine: the heavy processes and the stopper (also Legacy's), the last hour, the disk.
type machineKit struct {
	load    *machines.LoadMonitor
	stopper *machines.Stopper
	history *machines.History
	disk    *machines.DiskScanner
}

// machineKitRef hands the kit to the v2 once the machine slice has made it.
type machineKitRef struct {
	mu  sync.Mutex
	kit *machineKit
}

func (r *machineKitRef) set(k *machineKit) {
	r.mu.Lock()
	r.kit = k
	r.mu.Unlock()
}

func (r *machineKitRef) get() *machineKit {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.kit
}

// newMachineKit looks at the machine from now on: the history is read every few seconds for the life of the process.
func newMachineKit(d deps, load *machines.LoadMonitor, stopper *machines.Stopper) *machineKit {
	k := &machineKit{load: load, stopper: stopper, history: machines.NewHistory(machines.HistoryDeps{})}
	go k.history.Run(context.Background())
	home := filepath.Clean(d.Home)
	under := func(rel ...string) []string {
		out := make([]string, len(rel))
		for i, r := range rel {
			out[i] = filepath.Join(home, r)
		}
		return out
	}
	k.disk = machines.NewDiskScanner(machines.DiskDeps{Path: home, Specs: []machines.DiskSpec{
		{ID: machines.DiskWorkspace, Roots: []string{filepath.Clean(d.WorkspaceRoot)}, SkipHiddenTop: filepath.Clean(d.WorkspaceRoot) == home},
		{ID: machines.DiskCaches, Roots: under(".cache", ".npm", ".pnpm-store", ".local/share/pnpm", ".gradle", ".m2", ".cargo", ".bun", "go/pkg")},
		{ID: machines.DiskAgents, Roots: append(under(".claude", ".codex"), filepath.Clean(d.DataDir))},
	}})
	return k
}

// v2MachineTool is wire.MachineToolSource over the kit.
type v2MachineTool struct{ ref *machineKitRef }

var _ wire.MachineToolSource = v2MachineTool{}

var errMachineNotReady = errors.New("the machine tool is not up yet")

// chartPoints and chartStep: the hour as the page draws it, a point every two minutes.
const (
	chartPoints = 30
	chartStep   = 2 * time.Minute
)

func (t v2MachineTool) Stats(ctx context.Context) (wire.MachineStats, error) {
	kit := t.ref.get()
	if kit == nil {
		return wire.MachineStats{}, errMachineNotReady
	}
	// The first look after the start has nothing to compare with: take two a second apart.
	if _, ok := kit.history.Latest(); !ok {
		kit.history.Sample()
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			return wire.MachineStats{}, ctx.Err()
		}
		kit.history.Sample()
	}
	now, ok := kit.history.Latest()
	if !ok {
		return wire.MachineStats{}, errors.New("/proc could not be read")
	}
	load, err := kit.load.Load()
	if errors.Is(err, machines.ErrScanTimeout) {
		return wire.MachineStats{}, fmt.Errorf("%w: %w", context.DeadlineExceeded, err)
	}
	if err != nil {
		return wire.MachineStats{}, err
	}
	out := wire.MachineStats{At: now.At, CPU: now.CPU, Cores: now.Cores,
		Mem: wire.MachineMem{Used: now.MemUsed, Total: now.MemTotal}, Net: wire.MachineNet{Rx: now.RxPerSec, Tx: now.TxPerSec},
		History: []wire.MachineSample{}, Procs: []wire.MachineProc{}}
	for _, p := range kit.history.Series(chartPoints, chartStep) {
		out.History = append(out.History, wire.MachineSample{At: p.At, CPU: p.CPU, Mem: p.Mem, Rx: p.RxPerSec, Tx: p.TxPerSec})
	}
	layout := kit.disk.Layout()
	out.Disk = wire.MachineDisk{Path: layout.Path, Total: layout.Total, Free: layout.Free, Partial: layout.Partial, Parts: []wire.MachineDiskPart{}}
	if layout.MeasuredAt != 0 {
		out.Disk.MeasuredAt = &layout.MeasuredAt
	}
	for _, p := range layout.Parts {
		out.Disk.Parts = append(out.Disk.Parts, wire.MachineDiskPart{ID: wire.DiskPartID(p.ID), Bytes: p.Bytes})
	}
	for _, p := range load.Top {
		out.Procs = append(out.Procs, machineProc(p))
	}
	return out, nil
}

// machineProc is a heavy process of the Legacy list in the wire's words. A process the list does not offer to stop is
// guarded, whatever the reason (the runtime, tmux, a service, an agent or its tool).
func machineProc(p machines.LoadProcess) wire.MachineProc {
	out := wire.MachineProc{PID: p.PID, StartTime: strconv.FormatInt(int64(p.StartTime), 10), Name: procName(p.Command),
		Command: p.Command, CPU: max(0, float64(p.CPUPct)), RSS: int64(math.Max(0, float64(p.RSSBytes))), Kind: wire.MachineProcKind(p.Kind),
		Port: p.Port, Guard: !p.Stoppable}
	if p.Stuck != nil {
		out.Stuck = *p.Stuck
	}
	return out
}

// interpreters run a script or a program that names the process better than they do.
var interpreters = map[string]bool{"node": true, "nodejs": true, "bun": true, "deno": true, "python": true, "python3": true, "ruby": true, "perl": true, "php": true}

// packageManagers name the script they run: "npm run dev" is "npm · dev".
var packageManagers = map[string]bool{"npm": true, "pnpm": true, "yarn": true, "npx": true, "bunx": true}

// procName is what a process is called on the page: its program, and what it runs when that tells more ("node · vite",
// "java · gradle-launcher", "npm · dev"). The command line stays whole beside it.
func procName(command string) string {
	f := strings.Fields(command)
	if len(f) == 0 {
		return "?"
	}
	prog := filepath.Base(f[0])
	args := f[1:]
	// The words of the command line that are not flags, with the values of the flags that take one skipped.
	positional := func(skipValue ...string) []string {
		var out []string
		for i := 0; i < len(args); i++ {
			if strings.HasPrefix(args[i], "-") {
				if slices.Contains(skipValue, args[i]) {
					i++
				}
				continue
			}
			out = append(out, args[i])
		}
		return out
	}
	base := func(path string) string {
		b := filepath.Base(path)
		if ext := filepath.Ext(b); ext != "" && ext != b {
			b = strings.TrimSuffix(b, ext)
		}
		return b
	}
	var what string
	switch {
	case prog == "java":
		if i := slices.Index(args, "-jar"); i >= 0 && i+1 < len(args) {
			what = base(args[i+1])
		} else if words := positional("-cp", "-classpath", "--class-path", "-p", "--module-path"); len(words) > 0 {
			what = words[0][strings.LastIndexByte(words[0], '.')+1:]
		}
	case interpreters[prog]:
		if i := slices.Index(args, "-m"); i >= 0 && i+1 < len(args) {
			what = args[i+1]
		} else if words := positional("-e", "-p", "-r", "--require", "--import", "--loader"); len(words) > 0 {
			what = base(words[0])
		}
	case packageManagers[prog]:
		words := positional()
		if len(words) > 1 && slices.Contains([]string{"run", "run-script", "exec", "dlx", "x"}, words[0]) {
			words = words[1:]
		}
		if len(words) > 0 {
			what = words[0]
		}
	}
	if what == "" || what == prog {
		return prog
	}
	return prog + " · " + what
}

func (t v2MachineTool) Stop(_ context.Context, req wire.MachineStop) (wire.MachineStopped, error) {
	kit := t.ref.get()
	if kit == nil {
		return wire.MachineStopped{}, errMachineNotReady
	}
	res, err := kit.stopper.Stop(machines.StopRequest{PID: req.PID, StartTime: req.StartTime})
	if errors.Is(err, machines.ErrScanTimeout) {
		return wire.MachineStopped{}, &wire.MachineStopError{Reason: wire.StopBusy, Message: "The machine is too busy to answer. Try again in a moment."}
	}
	if err != nil {
		return wire.MachineStopped{}, err
	}
	if res.Status == 200 {
		signal := "SIGTERM"
		if s := res.Body.String("signal"); s == "SIGKILL" {
			signal = s
		}
		return wire.MachineStopped{Stopped: true, Signal: signal}, nil
	}
	reason, message := stopReason(res.Body.String("error"))
	return wire.MachineStopped{}, &wire.MachineStopError{Reason: reason, Message: message}
}

// stopReason words a refusal of the stopper.
func stopReason(why string) (wire.MachineStopReason, string) {
	switch why {
	case "gone":
		return wire.StopGone, "The process has ended already."
	case "changed":
		return wire.StopChanged, "The process is another one now."
	case "foreign":
		return wire.StopForeign, "The process is not the runtime user's."
	case "protected":
		return wire.StopProtected, "The process belongs to the platform."
	case "not-stoppable":
		return wire.StopNotStoppable, "The process is an agent of a chat or its tool."
	case "not-listed":
		return wire.StopNotListed, "The process is not among the heavy ones."
	}
	return wire.StopFailed, "The process could not be stopped."
}
