package main

import (
	"io/fs"
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
	"github.com/ly-solutions/harnessly/web"
)

// The design library (slice "design"): the library's listing, its loopback server, board edits, imports and new
// projects, checks and pictures in a headless browser, claims, history, and the dialogs and messages to agents. The
// routes that need the tab keeper (the listing, claims, messages, dialogs) are answered only while the core runs; so
// are the CLI's live log, page state and page control, which belong to the server whose port the listing gave the panel.

var (
	designMu   sync.Mutex
	designAPIs = map[*gate.CLIToken]*design.API{}
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

// designEngineDir is where this machine keeps the board engine its owner fetched (`harnessly design engine fetch`);
// laid out as a mirror of releases, it is the releases folder unless the deployment names a mirror of its own.
func designEngineDir(d deps) string {
	return env("RUNTIME_DESIGN_ENGINE_DIR", design.DefaultEngineDir(d.DataDir))
}

func designConfig(d deps) design.Config {
	port, _ := strconv.Atoi(os.Getenv("RUNTIME_DESIGN_LIBRARY_PORT"))
	return design.Config{
		DataDir:          d.DataDir,
		AgentTokenHeader: os.Getenv("RUNTIME_AGENT_TOKEN_HEADER"),
		ConfigFile:       d.ConfigFile,
		LibraryDir:       os.Getenv("RUNTIME_DESIGN_LIBRARY_DIR"),
		WorkspaceDir:     os.Getenv("RUNTIME_WORKSPACE_DIR"),
		AltWorkspaceDir:  os.Getenv("RUNTIME_WORKSPACE"),
		WorkspaceConfig:  env("RUNTIME_WORKSPACE_CONFIG", os.Getenv("RUNTIME_SIDE_CHAT_CONFIG")),
		HistoryDir:       os.Getenv("RUNTIME_DESIGN_HISTORY_DIR"),
		KitsDir:          os.Getenv("RUNTIME_DESIGN_KITS_DIR"),
		KitOrigins:       splitList(os.Getenv("RUNTIME_DESIGN_KIT_ORIGINS")),
		ReleasesDir:      env("RUNTIME_DESIGN_RELEASES_DIR", designEngineDir(d)),
		Port:             port,
		Chrome:           os.Getenv("RUNTIME_DESIGN_CHROME"),
		PlatformAPI:      platformAPI,
		OwnerEmail:       os.Getenv("RUNTIME_OWNER_EMAIL"),
		CanvasKey:        os.Getenv("RUNTIME_DESIGN_CANVAS_KEY"),
		EngineFile:       os.Getenv("RUNTIME_DESIGN_ENGINE_FILE"),
		Leases: design.LeaseConfig{
			Mode:      os.Getenv("RUNTIME_DESIGN_LEASES"),
			HubURL:    os.Getenv("RUNTIME_DESIGN_HUB_URL"),
			Token:     os.Getenv("RUNTIME_DESIGN_HUB_TOKEN"),
			TokenFile: os.Getenv("RUNTIME_DESIGN_HUB_TOKEN_FILE"),
		},
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

// webFiles are the built front-end the runtime serves: RUNTIME_WEB_DIR, else the files compiled in (nil without either).
func webFiles() fs.FS {
	if dir := os.Getenv("RUNTIME_WEB_DIR"); dir != "" {
		return os.DirFS(dir)
	}
	return web.Dist()
}

// designAPI is one library per runtime, so every route shares its caches, server and background.
func designAPI(d deps) *design.API {
	designMu.Lock()
	defer designMu.Unlock()
	if a := designAPIs[d.CLI]; a != nil {
		return a
	}
	a := &design.API{
		L:            design.New(designConfig(d)),
		Session:      d.SessionOK,
		MachineAgent: d.MachineAgent,
		CLIToken:     cliTokenFirst(d.DataDir, d.TokenHeaders),
		APIRefusal:   d.APIRefusal,
		PlatformAPI:  platformAPI,
		GuestApp:     webFiles(),
		PublicToken:  designPublic(d.Secret),
		Log: func(level, msg string, err error) {
			fields := []agent.F{{K: "module", V: "design-library"}}
			if err != nil {
				fields = append(fields, agent.F{K: "error", V: err.Error()})
			}
			agent.Log(level, msg, fields...)
		},
	}
	designAPIs[d.CLI] = a
	return a
}

// designRoute is a JSON route of the slice at one path of the platform's API (or the CLI's when cli).
func designRoute(path string, compute func(*http.Request) design.Answer) gate.JSONRoute {
	return gate.JSONRoute{Slice: "design", Match: func(r *http.Request) bool { return files.RouteIs(r, path) },
		Compute: func(r *http.Request) (int, any, http.Header) { return compute(r).Split() }}
}

func init() {
	routeGroups = append(routeGroups, designRoutes)
	bodyRouteGroups = append(bodyRouteGroups, designPublicRoutes)
}

// designPublicRoutes: the guest links' read of a design, which the platform's gateway makes with a token, no session.
func designPublicRoutes(d deps) []gate.BodyRoute {
	a := designAPI(d)
	return []gate.BodyRoute{{Slice: "design", Match: a.PublicMatch, Serve: a.Public}}
}

// designRoutes are the library's routes that need neither the tab keeper nor the library server's memory.
func designRoutes(d deps) []gate.JSONRoute {
	a := designAPI(d)
	return []gate.JSONRoute{
		designRoute(platformAPI+"/design/library/boards", a.Boards),
		designRoute(platformAPI+"/design/library/locate", a.Locate),
		designRoute(platformAPI+"/design/library/send", a.Send),
		designRoute("/api/cli/design/pages", a.CLIPages),
		designRoute("/api/cli/design/notes", a.CLINotes),
		designRoute(platformAPI+"/design/library/releases", a.Releases),
		designRoute(platformAPI+"/design/access", a.PostAccess),
		designRoute(platformAPI+"/design/publish", a.Publish),
		designRoute(platformAPI+"/design/library/claim", a.ClaimProject),
		designRoute(platformAPI+"/design/library/delete", a.DeleteProject),
		designRoute(platformAPI+"/design/engine", a.EngineStatus),
	}
}

// startDesign runs the library's background (claim keeper, history) when the slice is on, as the previous runtime ran
// it from its start.
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
	if f.SliceOn("design") {
		a.L.WriteRootHint()
		a.L.StartBackground()
	}
}

// designPublic is how a guest link's token is checked. The audience and the header follow the design tag, the salt is the
// share salt, so a deployment that names its design tag and share salt needs no other setting.
func designPublic(secret []byte) design.PublicConfig {
	tag := env("RUNTIME_DESIGN_TAG", "harnessly-design")
	return design.PublicConfig{
		Secret:   secret,
		Salt:     env("RUNTIME_SHARE_TOKEN_SALT", "harnessly"),
		Audience: tag + "-public",
		Header:   "x-" + tag + "-token",
	}
}
