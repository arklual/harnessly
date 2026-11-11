package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/memsync"
)

// The person's Claude Code memory, the same on all of the person's machines (docs/hub/memory-sync.md). It is on when
// RUNTIME_MEMORY_SYNC_URL names the memory relay of this machine's agent (http://127.0.0.1:<agent port>/memory) and
// the hub keeps memory (HUB_MEMORY_SYNC=on).
//
//	before a Claude conversation starts   the memory of its project is pulled and pushed (5 s at most)
//	GET  /api/cli/memory                  the last sync (harnessly memory status)
//	POST /api/cli/memory/sync {cwd}       sync again: the project in cwd, or every project synced before

type memoryService struct {
	syncer   *memsync.Syncer
	projects string
	report   string
	// localFolders: a folder without a git remote syncs under its path (RUNTIME_MEMORY_LOCAL_FOLDERS=on).
	localFolders bool
}

// memorySync is the service when memory sync is on; the launcher reads it before each start.
var memorySync atomic.Pointer[memoryService]

const memorySyncAtStart = 5 * time.Second

func newMemoryService(d deps) *memoryService {
	u := strings.TrimSpace(os.Getenv("RUNTIME_MEMORY_SYNC_URL"))
	if u == "" || len(d.Secret) == 0 {
		return nil
	}
	return &memoryService{
		syncer:   &memsync.Syncer{Client: &memsync.HTTPClient{URL: u, Secret: d.Secret}},
		projects: filepath.Join(env("CLAUDE_CONFIG_DIR", filepath.Join(d.Home, ".claude")), "projects"),
		report:   filepath.Join(d.DataDir, "v2", "memory-sync.json"),

		localFolders: env("RUNTIME_MEMORY_LOCAL_FOLDERS", "") == "on",
	}
}

func (m *memoryService) save(rep memsync.Report) {
	raw, _ := json.MarshalIndent(rep, "", "  ")
	_ = os.MkdirAll(filepath.Dir(m.report), 0o700)
	tmp := m.report + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, m.report)
	}
}

func (m *memoryService) last() *memsync.Report {
	raw, err := os.ReadFile(m.report)
	if err != nil {
		return nil
	}
	var rep memsync.Report
	if json.Unmarshal(raw, &rep) != nil {
		return nil
	}
	return &rep
}

// sync syncs the project in cwd, or every project synced before when cwd is "".
func (m *memoryService) sync(ctx context.Context, cwd string) memsync.Report {
	var projects []memsync.Project
	if cwd == "" {
		projects = memsync.Known(m.projects)
	} else if id := memsync.FolderID(ctx, cwd, m.localFolders); id != "" {
		projects = []memsync.Project{{ID: id, Dir: memsync.MemoryDir(m.projects, cwd)}}
	}
	rep := m.syncer.Sync(ctx, projects)
	m.save(rep)
	return rep
}

// memoryBeforeAgent is the launcher's BeforeAgent: a Claude conversation starts with its person's memory.
func memoryBeforeAgent(provider, cwd string) {
	m := memorySync.Load()
	if m == nil || provider != agents.Claude || cwd == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), memorySyncAtStart)
	defer cancel()
	rep := m.sync(ctx, cwd)
	if len(rep.Errors) > 0 || len(rep.Conflicts) > 0 {
		coreLog("warn", "memory sync", "cwd", cwd, "conflicts", len(rep.Conflicts), "errors", rep.Errors)
	}
}

type memoryStatus struct {
	Enabled bool            `json:"enabled"`
	Last    *memsync.Report `json:"last"`
}

func memoryRoutes(d deps) []gate.Route {
	m := newMemoryService(d)
	if m != nil {
		memorySync.Store(m)
	}
	route := func(p string, h http.HandlerFunc) gate.Route {
		return gate.Route{Match: func(r *http.Request) bool { return pathIs(r, p) }, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if d.CLI == nil || !d.CLI.OK(r) {
				peerJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
				return
			}
			h(w, r)
		})}
	}
	return []gate.Route{
		route("/api/cli/memory", func(w http.ResponseWriter, r *http.Request) {
			st := memoryStatus{Enabled: m != nil}
			if m != nil {
				st.Last = m.last()
			}
			peerJSON(w, http.StatusOK, st)
		}),
		route("/api/cli/memory/sync", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				peerJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST {cwd}"})
				return
			}
			if m == nil {
				peerJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "memory sync is off on this machine (RUNTIME_MEMORY_SYNC_URL is not set)"})
				return
			}
			var b struct {
				Cwd string `json:"cwd"`
			}
			_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&b)
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
			defer cancel()
			peerJSON(w, http.StatusOK, m.sync(ctx, b.Cwd))
		}),
	}
}
