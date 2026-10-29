package main

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/limits"
	"github.com/ly-solutions/harnessly/internal/machines"
	"github.com/ly-solutions/harnessly/internal/push"
	"github.com/ly-solutions/harnessly/internal/updatecheck"
)

// The machines slice: the machine snapshot the machine agent polls, the machine load panel, health and the manifest,
// and the web push subscription routes. They read files, /proc and tmux directly, so they answer with the core on or
// off; the push routes write through the process's one sender, the core's.

func portOf(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(p)
	return n
}

// upstreamAlive says whether the previous runtime accepts connections, remembered for a second.
func upstreamAlive(upstream string) func() bool {
	var mu sync.Mutex
	var at time.Time
	var alive bool
	return func() bool {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(at) < time.Second {
			return alive
		}
		c, err := net.DialTimeout("tcp", upstream, 300*time.Millisecond)
		alive = err == nil
		if c != nil {
			_ = c.Close()
		}
		at = time.Now()
		return alive
	}
}

func machineSliceRoutes(d deps, listen, upstream string, sender *push.Sender, hooks hookConfig) []gate.JSONRoute {
	tz, _ := strconv.Atoi(os.Getenv("RUNTIME_USAGE_TZ_OFFSET_MINUTES"))
	usage := &machines.Usage{
		ClaudeRoot: filepath.Join(env("CLAUDE_CONFIG_DIR", filepath.Join(d.Home, ".claude")), "projects"),
		CodexRoot:  filepath.Join(limits.CodexHome(os.Getenv("CODEX_HOME"), d.Home), "sessions"),
		TZOffset:   float64(tz) * 60,
		CacheFile:  filepath.Join(d.DataDir, "limits-spend-cache.gob"),
	}
	releaseFile := os.Getenv("RUNTIME_RELEASE_FILE")
	snapshot := machines.NewSnapshot(machines.SnapshotConfig{
		DataDir:     d.DataDir,
		NamePrefix:  os.Getenv("RUNTIME_MACHINE_NAME_PREFIX"),
		Codexbar:    filepath.IsAbs(os.Getenv("RUNTIME_CODEXBAR_BIN")),
		ReleaseFile: releaseFile,
		Activity:    usage.Activity,
	})
	// The economics of a conversation shows its provider's limits as the snapshot has them.
	providerLimits = func(provider string) any {
		l := snapshot.Limits(float64(time.Now().UnixMilli()) / 1000)
		switch provider {
		case "claude":
			return l.Claude
		case "codex":
			return l.Codex
		}
		return nil
	}
	hidden := map[int]bool{}
	for _, p := range splitList(os.Getenv("RUNTIME_RESERVED_PORTS")) {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			hidden[n] = true
		}
	}
	upstreamPort := portOf(upstream)
	for _, p := range []int{portOf(listen), upstreamPort} {
		if p > 0 {
			hidden[p] = true
		}
	}
	uid := os.Getuid()
	ports := &machines.OwnedPorts{UID: uid, Hidden: hidden, Probe: machines.ProbeHTTP, OwnerOf: procOwner}
	self := os.Getpid()
	panePIDs := func() []int {
		var pids []int
		for _, p := range d.Tmux.AllPanes() {
			if p.PID > 0 {
				pids = append(pids, p.PID)
			}
		}
		return pids
	}
	// The previous runtime (whoever listens on its port) and this process: theirs are the platform's processes.
	runtimePIDs := func() []int {
		if upstreamPort == 0 {
			return []int{self}
		}
		if pid := ports.ListenerPID(upstreamPort); pid > 0 && pid != self {
			return []int{pid, self}
		}
		return []int{self}
	}
	load := machines.NewLoadMonitor(machines.LoadDeps{
		UID:         uid,
		Cores:       runtime.NumCPU(),
		HomeDir:     d.Home,
		ReleaseRoot: os.Getenv("RUNTIME_RELEASE_ROOT"),
		OwnerOf:     procOwner,
		ListPorts:   ports.List,
		PanePIDs:    panePIDs,
		RuntimePIDs: runtimePIDs,
	})
	stopper := newStopper(load, runtimePIDs, panePIDs)
	if d.MachineKit != nil {
		d.MachineKit.set(newMachineKit(d, load, stopper))
	}
	appName := os.Getenv("RUNTIME_APP_NAME")
	// In front of the old runtime the machine is up while the old runtime is; alone, while this process answers.
	var alive func() bool
	if upstream != "" {
		alive = upstreamAlive(upstream)
	}
	return append(machines.Routes(machines.Config{
		PlatformAPI: platformAPI,
		DataDir:     d.DataDir,
		SessionOK:   d.SessionOK,
		APIRefusal:  d.APIRefusal,
		Snapshot:    snapshot,
		Load:        load,
		Push:        sender,
		CanSend:     hooks.PushSubject != "" && hooks.PushTagPrefix != "" && appName != "",
		AppName:     appName,
		TagPrefix:   hooks.PushTagPrefix,
		Health: machines.Health{
			App:             env("RUNTIME_HEALTH_APP", "harnessly"),
			FallbackVersion: healthFallbackVersion(),
			ReleaseFile:     releaseFile,
			Alive:           alive,
		},
		ManifestFile:     releaseStatic("RUNTIME_MANIFEST_FILE", "manifest.json"),
		MachineAgent:     d.MachineAgent,
		DocsReaderPath:   env("RUNTIME_DOCS_READER_PATH", "/agents/docs/"),
		DesignReaderPath: env("RUNTIME_DESIGN_READER_PATH", "/agents/design/"),
		Log:              machinesLog,
	}), stopRoute(d, stopper))
}

func machinesLog(level, msg string, kv ...any) {
	fs := make([]agent.F, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		k, _ := kv[i].(string)
		fs = append(fs, agent.F{K: k, V: kv[i+1]})
	}
	agent.Log(level, "machines: "+msg, fs...)
}

// procOwner is the owner uid of /proc/<pid>; false when the process is gone.
func procOwner(pid int) (int, bool) {
	st, err := os.Stat("/proc/" + strconv.Itoa(pid))
	if err != nil {
		return 0, false
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(sys.Uid), true
}

// releaseStatic is a payload the release ships for the runtime to serve as it is (the app manifest, the CLI's API
// guide): the file the variable names, else <release>/runtime-static/<name> beside the release file.
func releaseStatic(variable, name string) string {
	if v := os.Getenv(variable); v != "" {
		return v
	}
	if rel := os.Getenv("RUNTIME_RELEASE_FILE"); rel != "" {
		return filepath.Join(filepath.Dir(rel), "runtime-static", name)
	}
	return ""
}

// healthFallbackVersion is the version the health route answers with when there is no release file: the one the build
// was linked with (a release archive, an image), or "0.0.0-development" for a build from source.
func healthFallbackVersion() string {
	if updatecheck.Comparable(agent.BuildVersion) {
		return agent.BuildVersion
	}
	return "0.0.0-development"
}
