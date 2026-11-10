package main

import (
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// The runtime's memory, seen from inside: on SIGUSR1 it writes its heap, allocations and goroutines as pprof
// profiles into <data dir>/debug and logs its memory statistics. A machine's root reads them with `go tool pprof`
// (`kill -USR1 <pid>`), without a port to keep closed and without restarting what people work in.

// profileSets is how many sets of profiles the folder keeps; older ones go.
const profileSets = 3

var profileNames = []string{"heap", "allocs", "goroutine"}

// profileOnSignal writes the profiles into dir on every SIGUSR1.
func profileOnSignal(dir string) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1)
	go func() {
		for range ch {
			stamp, err := writeProfiles(dir, time.Now())
			if err != nil {
				agent.Log("warn", "profiles not written", agent.F{K: "error", V: err.Error()})
				continue
			}
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			agent.Log("info", "profiles written", agent.F{K: "dir", V: dir}, agent.F{K: "stamp", V: stamp},
				agent.F{K: "heapAllocMB", V: m.HeapAlloc >> 20}, agent.F{K: "heapInuseMB", V: m.HeapInuse >> 20},
				agent.F{K: "heapReleasedMB", V: m.HeapReleased >> 20}, agent.F{K: "sysMB", V: m.Sys >> 20},
				agent.F{K: "numGC", V: m.NumGC}, agent.F{K: "goroutines", V: runtime.NumGoroutine()})
		}
	}()
}

// writeProfiles writes one set of profiles named by the time and drops the sets beyond profileSets; the stamp.
func writeProfiles(dir string, at time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	stamp := at.UTC().Format("20060102-150405")
	for _, name := range profileNames {
		f, err := os.OpenFile(filepath.Join(dir, name+"-"+stamp+".pprof"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return "", err
		}
		err = pprof.Lookup(name).WriteTo(f, 0)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return "", err
		}
	}
	pruneProfiles(dir)
	return stamp, nil
}

// pruneProfiles keeps the newest profileSets sets.
func pruneProfiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var stamps []string
	for _, e := range entries {
		if s, ok := strings.CutPrefix(e.Name(), "heap-"); ok && strings.HasSuffix(s, ".pprof") {
			stamps = append(stamps, strings.TrimSuffix(s, ".pprof"))
		}
	}
	slices.Sort(stamps)
	for len(stamps) > profileSets {
		for _, name := range profileNames {
			_ = os.Remove(filepath.Join(dir, name+"-"+stamps[0]+".pprof"))
		}
		stamps = stamps[1:]
	}
}
