package machines

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"harnessly/internal/nodejson"
)

type loadGolden struct {
	Trees map[string]map[string]json.RawMessage `json:"trees"`
	Scans []struct {
		Advance float64 `json:"advance"`
		Reuse   bool    `json:"reuse"`
		At      float64 `json:"at"`
		Tree    string  `json:"tree"`
		Ports   []struct {
			Port       int    `json:"port"`
			PID        int    `json:"pid"`
			Cwd        string `json:"cwd"`
			Responding bool   `json:"responding"`
			Fresh      bool   `json:"fresh"`
		} `json:"ports"`
		Gen    uint64     `json:"gen"`
		Statfs [3]float64 `json:"statfs"`
		Result string     `json:"result"`
	} `json:"scans"`
	UID int `json:"uid"`
}

// materialize writes a /proc of the test's own: files, and links ({"link": target}).
func materialize(t *testing.T, dir string, files map[string]json.RawMessage) {
	t.Helper()
	for name, raw := range files {
		file := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		var link struct{ Link string }
		var text string
		if json.Unmarshal(raw, &text) == nil {
			_ = os.WriteFile(file, []byte(text), 0o644)
		} else if json.Unmarshal(raw, &link) == nil {
			_ = os.Symlink(link.Link, file)
		}
	}
}

func TestGoldenMachineLoad(t *testing.T) {
	g := loadGoldenFile(t).Load
	root := t.TempDir()
	steps := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	for _, s := range steps {
		materialize(t, root, prefixed("proc-"+s, g.Trees[s]))
	}
	procLink := filepath.Join(root, "proc")
	point := func(name string) {
		_ = os.Remove(procLink)
		if err := os.Symlink("proc-"+name, procLink); err != nil {
			t.Fatal(err)
		}
	}
	next := 0
	point(steps[next])
	now := time.UnixMilli(int64(loadGoldenFile(t).Fixed))
	scan := 0
	m := NewLoadMonitor(LoadDeps{
		ProcRoot: procLink, UID: g.UID, Cores: 4, HomeDir: "/home/agent", ReleaseRoot: goldenReleaseRoot,
		RuntimePIDs: func() []int { return []int{1000} },
		OwnerOf: func(pid int) (int, bool) {
			raw, err := os.ReadFile(filepath.Join(procLink, strconv.Itoa(pid), "owner"))
			if err != nil {
				return 0, false
			}
			n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			return n, err == nil
		},
		Now:   func() time.Time { return now },
		Sleep: func(d time.Duration) { now = now.Add(d); next++; point(steps[next]) },
		Statfs: func(string) (float64, float64, float64, error) {
			s := g.Scans[scan].Statfs
			return s[0], s[1], s[2], nil
		},
		ListPorts: func(map[int]bool) ([]LoadPort, uint64) {
			var out []LoadPort
			for _, p := range g.Scans[scan].Ports {
				out = append(out, LoadPort{Port: p.Port, PID: p.PID, Cwd: p.Cwd, Responding: p.Responding, Fresh: p.Fresh})
			}
			return out, g.Scans[scan].Gen
		},
		PanePIDs: func() []int { return []int{1501, 1502} },
	})
	for i, s := range g.Scans {
		scan = i
		now = now.Add(time.Duration(s.Advance) * time.Millisecond)
		if s.Advance > 0 {
			next++
			point(steps[next])
		}
		l, err := m.collect()
		if err != nil {
			t.Fatalf("scan %d: %v", i, err)
		}
		if got := string(nodejson.Marshal(l)); got != s.Result {
			t.Errorf("scan %d:\n got %s\nwant %s", i, got, s.Result)
		}
		if steps[next] != s.Tree || float64(now.UnixMilli()) != s.At {
			t.Errorf("scan %d: at %d on %s, want %v on %s", i, now.UnixMilli(), steps[next], s.At, s.Tree)
		}
	}
}

func prefixed(dir string, files map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(files))
	for k, v := range files {
		out[filepath.Join(dir, k)] = v
	}
	return out
}
