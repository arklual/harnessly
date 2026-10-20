package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
)

// The design library (slice "design"): the library's listing, its loopback server, board edits, imports and new
// projects, checks and pictures in a headless browser, claims, history, and the dialogs and messages to agents. The
// routes that need the tab keeper (the listing, claims, messages, dialogs) are answered only while the core runs; so
// are the CLI's live log, page state and page control, which belong to the server whose port the listing gave the panel.

var (
	designMu   sync.Mutex
	designAPIs = map[*gate.OldRuntime]*design.API{}
)

// people reads "local=Name,local2=Name2".
func people(v string) map[string]string {
	out := map[string]string{}
	for _, pair := range splitList(v) {
		if k, name, ok := strings.Cut(pair, "="); ok && strings.TrimSpace(k) != "" {
			out[strings.TrimSpace(k)] = strings.TrimSpace(name)
		}
	}
	return out
}

func designConfig(d deps) design.Config {
	port, _ := strconv.Atoi(os.Getenv("RUNTIME_DESIGN_LIBRARY_PORT"))
	return design.Config{
		DataDir:         d.DataDir,
		ConfigFile:      d.ConfigFile,
		LibraryDir:      os.Getenv("RUNTIME_DESIGN_LIBRARY_DIR"),
		WorkspaceDir:    os.Getenv("RUNTIME_WORKSPACE_DIR"),
		AltWorkspaceDir: os.Getenv("RUNTIME_WORKSPACE"),
		WorkspaceConfig: env("RUNTIME_WORKSPACE_CONFIG", os.Getenv("RUNTIME_SIDE_CHAT_CONFIG")),
		HistoryDir:      os.Getenv("RUNTIME_DESIGN_HISTORY_DIR"),
		Port:            port,
		Chrome:          os.Getenv("RUNTIME_DESIGN_CHROME"),
		PlatformAPI:     platformAPI,
		OwnerEmail:      os.Getenv("RUNTIME_OWNER_EMAIL"),
		// The transcripts the listing reads to see who works on what: the agents' own folders under the home.
		ClaudeProjects: filepath.Join(d.Home, ".claude", "projects"),
		CodexSessions:  filepath.Join(d.Home, ".codex", "sessions"),
		Texts: design.Texts{
			CLI:          env("RUNTIME_CLI_NAME", "harnessly"),
			Host:         env("RUNTIME_PLATFORM_HOST", "localhost"),
			AppName:      env("RUNTIME_APP_NAME", "harnessly"),
			Tag:          env("RUNTIME_DESIGN_TAG", "harnessly-design"),
			HistoryEmail: env("RUNTIME_DESIGN_HISTORY_EMAIL", "design@localhost"),
			SharedDir:    env("RUNTIME_DESIGN_SHARED_DIR", "/srv/design"),
			KitGlobal:    env("RUNTIME_DESIGN_KIT_GLOBAL", "DesignKit"),
			PagesGlobal:  env("RUNTIME_DESIGN_PAGES_GLOBAL", "DesignPages"),
			ArgsGlobal:   env("RUNTIME_DESIGN_ARGS_GLOBAL", "__DESIGN_ARGS"),
			People:       people(os.Getenv("RUNTIME_PEOPLE")),
		},
	}
}

// designAPI is one library per runtime, so every route shares its caches, server and background.
func designAPI(d deps) *design.API {
	designMu.Lock()
	defer designMu.Unlock()
	if a := designAPIs[d.Old]; a != nil {
		return a
	}
	a := &design.API{
		L:           design.New(designConfig(d)),
		Session:     d.SessionOK,
		CLIToken:    cliTokenFirst(d.DataDir, d.TokenHeaders),
		APIRefusal:  d.APIRefusal,
		PlatformAPI: platformAPI,
		Log: func(level, msg string, err error) {
			fields := []agent.F{{K: "module", V: "design-library"}}
			if err != nil {
				fields = append(fields, agent.F{K: "error", V: err.Error()})
			}
			agent.Log(level, msg, fields...)
		},
	}
	designAPIs[d.Old] = a
	return a
}

// designRoute is a JSON route of the slice at one path of the platform's API (or the CLI's when cli).
func designRoute(path string, compute func(*http.Request) design.Answer) gate.JSONRoute {
	return gate.JSONRoute{Slice: "design", Match: func(r *http.Request) bool { return files.RouteIs(r, path) },
		Compute: func(r *http.Request) (int, any, http.Header) { return compute(r).Split() }}
}

func init() { routeGroups = append(routeGroups, designRoutes) }

// designRoutes are the library's routes that need neither the tab keeper nor the library server's memory.
func designRoutes(d deps) []gate.JSONRoute {
	a := designAPI(d)
	return []gate.JSONRoute{
		designRoute(platformAPI+"/design/library/boards", a.Boards),
		designRoute(platformAPI+"/design/library/locate", a.Locate),
		designRoute(platformAPI+"/design/library/send", a.Send),
	}
}

// startDesign keeps the library's background (claim keeper, history) running while the slice is on, as the previous
// runtime ran it from its start.
func startDesign(f *gate.Gate, d deps) {
	a := designAPI(d)
	// The preview's component lookup leaves the library server alone: this process's once it runs, else the port the
	// old runtime's takes.
	designLibraryPort = func() int {
		if p := a.L.ServerPort(); p > 0 {
			return p
		}
		return a.L.Config().Port
	}
	settle := func(on bool) {
		if on {
			a.L.StartBackground()
		} else {
			a.L.StopBackground()
		}
	}
	f.OnSliceChange("design", settle)
	settle(f.SliceOn("design"))
}
