package main

import (
	"math"
	"net/http"
	"sync"

	"github.com/ly-solutions/harnessly/internal/agentdetect"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/machine"
	"github.com/ly-solutions/harnessly/internal/preflight"
	"github.com/ly-solutions/harnessly/internal/sessions"
	"github.com/ly-solutions/harnessly/internal/settings"
)

func init() { routeGroups = append(routeGroups, sessionRoutes) }

// handlerFailed is the old runtime's answer when a handler throws.
var handlerFailed = gate.Raw{ContentType: "text/plain", Body: []byte("Internal Server Error")}

func getAt(r *http.Request, p string) bool { return r.Method == http.MethodGet && pathIs(r, p) }

// sessionRoutes: read-only answers about the agents in a tab, the conversations of a folder, a folder chosen for a
// workspace and the tools on the machine, checked again on POST (slice fsread). The other methods' refusals are in
// routes_methods.go.
func sessionRoutes(d deps) []gate.JSONRoute {
	detector := agentdetect.New(d.Home, d.Tmux)
	claude := sessions.NewClaudeLister(d.Home)
	codex := sessions.NewCodexLister(d.Home)
	tools := d.Tools
	if tools == nil {
		tools = preflight.New(d.Home)
	}
	query := func(r *http.Request, name string) jscompat.Param {
		return jscompat.Param{Values: jscompat.Query(r.URL.RawQuery)[name]}
	}
	// Each answer carries the ETag the old dispatcher gave a JSON answer.
	guard := func(compute func(*http.Request) (int, any, http.Header)) func(*http.Request) (int, any, http.Header) {
		return func(r *http.Request) (int, any, http.Header) {
			if status, v, h, refused := d.APIRefusal(r); refused {
				return status, v, h
			}
			if status, v, refused := bodyRefusal(r); refused {
				return status, v, nil
			}
			status, v, h := compute(r)
			if _, raw := v.(gate.Raw); raw || v == nil {
				return status, v, h
			}
			return settings.Answer(r, status, v, h)
		}
	}
	return []gate.JSONRoute{
		{
			Slice: "fsread",
			// The old runtime's check-claude was the same handler under its first name.
			Match:    func(r *http.Request) bool { return getAt(r, "/api/check-agent") || getAt(r, "/api/check-claude") },
			Volatile: []string{"checkedAt"},
			Compute: guard(func(r *http.Request) (int, any, http.Header) {
				s := query(r, "session")
				if !s.Truthy() {
					return 400, map[string]string{"error": "session parameter required"}, nil
				}
				if s.Array() {
					return 500, handlerFailed, nil
				}
				a, err := detector.Check(s.Values[0])
				if err != nil {
					return 500, handlerFailed, nil
				}
				return 200, a.JSON(), nil
			}),
		},
		{
			Slice: "fsread",
			Match: func(r *http.Request) bool { return getAt(r, "/api/codex/sessions") },
			Compute: guard(func(r *http.Request) (int, any, http.Header) {
				cwd := query(r, "cwd")
				if !cwd.Truthy() {
					return 400, map[string]string{"error": "missing-param", "message": "cwd parameter required"}, nil
				}
				days := 30
				if n := jscompat.ParseInt(query(r, "daysBack").String(), 10); !math.IsNaN(n) {
					days = int(math.Min(math.Max(1, n), 365))
				}
				want := cwd.Values[0]
				list, dirs, files := codex.List(want, days)
				if cwd.Array() {
					list = []sessions.CodexEntry{} // no session's folder equals a list of folders
				}
				return 200, map[string]any{"sessions": list, "scannedDirs": dirs, "scannedFiles": files}, nil
			}),
		},
		{
			Slice: "fsread",
			Match: func(r *http.Request) bool { return getAt(r, "/api/timeline/sessions") },
			Compute: guard(func(r *http.Request) (int, any, http.Header) {
				name := query(r, "tmuxSession")
				if !name.Truthy() {
					return 400, map[string]string{"error": "missing-param", "message": "tmuxSession parameter required"}, nil
				}
				if name.Array() {
					return 500, handlerFailed, nil
				}
				if !d.Tmux.HasSession(name.Values[0]) {
					return 404, map[string]string{"error": "tmux-session-not-found", "message": "tmux session '" + name.Values[0] + "' not found"}, nil
				}
				limit := jscompat.ParseInt(query(r, "limit").String(), 10)
				if math.IsNaN(limit) || limit == 0 {
					limit = 50
				}
				limit = math.Max(1, limit)
				offset := jscompat.ParseInt(query(r, "offset").String(), 10)
				if math.IsNaN(offset) {
					offset = 0
				}
				offset = math.Max(0, offset)
				hint := query(r, "cwd")
				if hint.Array() {
					return 500, map[string]string{"error": "internal-error", "message": "dirPath.replace is not a function"}, nil
				}
				cwd := hint.String()
				if cwd == "" {
					cwd = machine.SessionDir(d.Tmux, name.Values[0])
				}
				if cwd == "" {
					return 500, map[string]string{"error": "cwd-lookup-failed", "message": "Failed to get cwd from tmux session"}, nil
				}
				all := claude.List(cwd, forkFilter())
				total := len(all)
				from := int(math.Min(offset, float64(total)))
				to := int(math.Min(offset+limit, float64(total)))
				return 200, map[string]any{"sessions": all[from:to], "total": total, "hasMore": offset+limit < float64(total)}, nil
			}),
		},
		{
			Slice: "fsread",
			Match: func(r *http.Request) bool { return getAt(r, "/api/workspace/validate") },
			Compute: guard(func(r *http.Request) (int, any, http.Header) {
				dir := query(r, "directory")
				if !dir.Truthy() {
					return 400, map[string]string{"error": "directory parameter required"}, nil
				}
				if dir.Array() {
					return 200, machine.DirMissing(), nil
				}
				return 200, machine.ValidateDirectory(dir.Values[0]), nil
			}),
		},
		{
			Slice: "fsread",
			Match: func(r *http.Request) bool { return getAt(r, "/api/preflight/runtime") },
			Compute: guard(func(r *http.Request) (int, any, http.Header) {
				return 200, tools.Runtime(), nil
			}),
		},
		{
			// POST checks again now: the cached answer and the Codex probe are dropped first.
			Slice: "fsread",
			Match: func(r *http.Request) bool { return r.Method == http.MethodPost && pathIs(r, "/api/preflight/runtime") },
			Compute: guard(func(r *http.Request) (int, any, http.Header) {
				tools.Invalidate()
				return 200, tools.Runtime(), nil
			}),
		},
	}
}

// forkFilter leaves out the forks running now, reading the process table once and only when a folder has transcripts.
func forkFilter() func(string) bool {
	var once sync.Once
	var forks map[string]bool
	return func(id string) bool {
		once.Do(func() { forks = sessions.ForkIDs() })
		return forks[id]
	}
}
