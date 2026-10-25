package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/machines"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func TestProcName(t *testing.T) {
	for command, want := range map[string]string{
		"node /home/u/app/node_modules/.bin/vite --port 4400":        "node · vite",
		"/usr/bin/node --max-old-space-size=4096 server.js":          "node · server",
		"node -r ts-node/register src/main.ts":                       "node · main",
		"java -Xmx4g -jar /opt/gradle/lib/gradle-launcher-8.5.jar x": "java · gradle-launcher-8.5",
		"java -cp /a:/b com.acme.app.Main arg":                       "java · Main",
		"python3 -m http.server 8000":                                "python3 · http.server",
		"npm run dev":                                                "npm · dev",
		"pnpm exec vitest --watch":                                   "pnpm · vitest",
		"npm":                                                        "npm",
		"claude --resume 1b2c":                                       "claude",
		"/usr/bin/dockerd -H fd://":                                  "dockerd",
		"":                                                           "?",
	} {
		if got := procName(command); got != want {
			t.Errorf("%q: %q, want %q", command, got, want)
		}
	}
}

func TestMachineProcGuardsWhatTheListWillNotStop(t *testing.T) {
	port, stuck := 4400, true
	dev := machineProc(machines.LoadProcess{PID: 10, StartTime: 123456, Command: "node vite", CPUPct: 22.5, RSSBytes: 1 << 30, Kind: "dev", Stoppable: true, Port: &port, Stuck: &stuck})
	if dev.Guard || !dev.Stuck || dev.StartTime != "123456" || dev.Name != "node · vite" || dev.Port == nil || *dev.Port != 4400 {
		t.Fatalf("a dev server: %+v", dev)
	}
	agent := machineProc(machines.LoadProcess{PID: 11, StartTime: 5, Command: "claude", Kind: "agent"})
	service := machineProc(machines.LoadProcess{PID: 12, StartTime: 6, Command: "tmux new-session", Kind: "service"})
	if !agent.Guard || !service.Guard {
		t.Fatalf("an agent and a service are guarded: %+v %+v", agent, service)
	}
	if err := dev.Validate(); err != nil {
		t.Fatal(err)
	}
}

func startTimeOf(t *testing.T, pid int) string {
	t.Helper()
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		t.Fatal(err)
	}
	rest := string(raw)[strings.LastIndexByte(string(raw), ')')+2:]
	return strings.Fields(rest)[19]
}

// toolOnThisMachine is the Machine tool over this machine's /proc, for the user running the test.
func toolOnThisMachine(t *testing.T, runtimePIDs func() []int, listed func() []machines.LoadProcess) v2MachineTool {
	t.Helper()
	load := machines.NewLoadMonitor(machines.LoadDeps{UID: os.Getuid(), Cores: runtime.NumCPU(), HomeDir: t.TempDir(), RuntimePIDs: runtimePIDs})
	stopper := &machines.Stopper{UID: os.Getuid(), RuntimePIDs: runtimePIDs,
		Listed: func() ([]machines.LoadProcess, error) { return listed(), nil },
		Kill:   func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }, Sleep: time.Sleep}
	ref := &machineKitRef{}
	ref.set(newMachineKit(deps{Home: t.TempDir(), WorkspaceRoot: t.TempDir(), DataDir: t.TempDir()}, load, stopper))
	return v2MachineTool{ref: ref}
}

func TestMachineToolStatsAreInContract(t *testing.T) {
	tool := toolOnThisMachine(t, func() []int { return []int{os.Getpid()} }, func() []machines.LoadProcess { return nil })
	stats, err := tool.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := stats.Validate(); err != nil {
		t.Fatalf("%v\n%+v", err, stats)
	}
	if len(stats.Cores) != runtime.NumCPU() || stats.Mem.Total <= 0 || stats.Disk.Total <= 0 || len(stats.History) == 0 {
		t.Fatalf("a machine has cores, memory, a disk and a last reading: %+v", stats)
	}
	if n := len(stats.Disk.Parts); n < 2 || stats.Disk.Parts[n-1].ID != wire.DiskPartFree {
		t.Fatalf("the disk ends with its free part: %+v", stats.Disk.Parts)
	}
}

// sleeper starts a `sleep` that is nobody's child but init's, as a dev server the person started from a terminal is not the
// runtime's child; it reports the pid and ends when the test does.
func sleeper(t *testing.T) int {
	t.Helper()
	out, err := exec.Command("sh", "-c", "sleep 60 >/dev/null 2>&1 </dev/null & echo $!").Output()
	if err != nil {
		t.Skip("no shell:", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("%q", out)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	return pid
}

func running(pid int) bool {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	rest := string(raw)[strings.LastIndexByte(string(raw), ')')+2:]
	return !strings.HasPrefix(rest, "Z")
}

func TestMachineToolStopsAProcessOfTheUserAndNotTheRuntime(t *testing.T) {
	pid := sleeper(t)
	start := startTimeOf(t, pid)
	startNum, _ := strconv.ParseFloat(start, 64)
	self := os.Getpid()
	selfStart, _ := strconv.ParseFloat(startTimeOf(t, self), 64)
	listed := []machines.LoadProcess{
		{PID: pid, StartTime: machines.Num(startNum), Command: "sleep 60", Kind: "other", Stoppable: true},
		{PID: self, StartTime: machines.Num(selfStart), Command: "runtime", Kind: "service"},
	}
	// The test process stands for the runtime.
	tool := toolOnThisMachine(t, func() []int { return []int{self} }, func() []machines.LoadProcess { return listed })

	var refusal *wire.MachineStopError
	if _, err := tool.Stop(context.Background(), wire.MachineStop{PID: self, StartTime: startTimeOf(t, self)}); !errors.As(err, &refusal) || refusal.Reason != wire.StopProtected {
		t.Fatalf("the runtime itself must be protected: %v", err)
	}
	if _, err := tool.Stop(context.Background(), wire.MachineStop{PID: pid, StartTime: "1"}); !errors.As(err, &refusal) || refusal.Reason != wire.StopChanged {
		t.Fatalf("a start time that is not the process's: %v", err)
	}
	if !running(pid) {
		t.Fatal("a refused stop must leave the process alone")
	}

	got, err := tool.Stop(context.Background(), wire.MachineStop{PID: pid, StartTime: start})
	if err != nil || !got.Stopped || got.Signal != "SIGTERM" {
		t.Fatalf("stop: %+v %v", got, err)
	}
	for i := 0; running(pid) && i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if running(pid) {
		t.Fatal("the process did not end")
	}
	if _, err := tool.Stop(context.Background(), wire.MachineStop{PID: pid, StartTime: start}); !errors.As(err, &refusal) || refusal.Reason != wire.StopGone {
		t.Fatalf("a process that is gone: %v", err)
	}
}
