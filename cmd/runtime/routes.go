package main

import (
	"net/http"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/machine"
	"github.com/ly-solutions/harnessly/internal/preflight"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

// deps is what a group of routes may use.
type deps struct {
	Home    string
	DataDir string
	// ConfigFile is the runtime's config (the pair's secret, the remembered launch choices).
	ConfigFile string
	// Secret is the pair's secret, which signs page sessions and the tokens derived from it.
	Secret        []byte
	WorkspaceRoot string
	Tmux          *tmux.Tmux
	// TokenHeaders carry the runtime's cli-token, the current name first.
	TokenHeaders []string
	Old          *gate.OldRuntime
	// SessionOK: the request carries a page session.
	SessionOK func(*http.Request) bool
	// Claims are those of the request's session cookie when its token is signed with the pair's secret (any token,
	// audience or not); nil otherwise.
	Claims func(*http.Request) map[string]any
	// APIRefusal is the old runtime API's answer to a caller with neither a page session nor the cli-token; refused is
	// false for anyone else.
	APIRefusal func(*http.Request) (status int, body any, h http.Header, refused bool)
	// MachineAgent: the request is the machine agent's own (loopback, a session it signed with its name).
	MachineAgent func(*http.Request) bool
	// Cookie is the page session's cookie name; SignSession a new page session signed with the pair's secret.
	Cookie      string
	SignSession func() string
	// Tools is the machine's tool check (tmux, git, the agent CLIs), one cache for every route that shows it.
	Tools *preflight.Checker
	// Upstream is the previous runtime's inner address; Standalone: there is none (RUNTIME_GATE_UPSTREAM empty).
	Upstream   string
	Standalone bool
	// SliceOn says whether a slice is on now (all but the shadows on a standalone gate).
	SliceOn func(string) bool
	// RuntimePort is the port the previous runtime reported as its own.
	RuntimePort int
	// CoreRunning says whether the core (the layout store's keeper) runs in this process; FirstWorkspace makes a
	// machine's first workspace there.
	CoreRunning    func() bool
	FirstWorkspace func() error
}

// platformAPI is the prefix of the routes the platform's own pages call (peers, commands, file previews…); the
// deployment names it, as its pages do.
var platformAPI = env("RUNTIME_PLATFORM_API", "/api/platform")

// routeGroups are the JSON routes of the slices; each file adds its group in init.
var routeGroups []func(deps) []gate.JSONRoute

// bodyRouteGroups are the slices' read-only routes that answer more than JSON (files, pictures); added the same way.
var bodyRouteGroups []func(deps) []gate.BodyRoute

func init() { routeGroups = append(routeGroups, machineRoutes) }

// machineRoutes: read-only answers about this machine and a tab's working folder (slice fsread).
func machineRoutes(d deps) []gate.JSONRoute {
	return []gate.JSONRoute{
		{
			Slice: "fsread",
			Match: func(r *http.Request) bool { return pathIs(r, platformAPI+"/peers") },
			Compute: func(r *http.Request) (int, any, http.Header) {
				if status, v, h, refused := d.APIRefusal(r); refused {
					return status, v, h
				}
				if !d.SessionOK(r) { // a cli-token alone is not a person
					return http.StatusForbidden, nil, nil
				}
				return 200, map[string]any{"peers": machine.Peers(d.Home)}, http.Header{"Cache-Control": {"no-store"}}
			},
		},
		{
			Slice: "fsread",
			Match: func(r *http.Request) bool { return pathIs(r, "/api/layout/cwd") },
			Compute: func(r *http.Request) (int, any, http.Header) {
				if status, v, h, refused := d.APIRefusal(r); refused {
					return status, v, h
				}
				if r.Method != http.MethodGet {
					return 405, map[string]string{"error": "Method not allowed"}, http.Header{"Allow": {"GET"}}
				}
				s := r.URL.Query().Get("session")
				if s == "" {
					return 400, map[string]string{"error": "session parameter required"}, nil
				}
				if !d.Tmux.HasSession(s) {
					return 404, map[string]string{"error": "Session not found"}, nil
				}
				cwd, last := machine.SessionCwd(d.Tmux, s)
				if cwd == "" {
					return 500, map[string]string{"error": "Failed to get CWD"}, nil
				}
				return 200, map[string]any{"cwd": cwd, "lastCommand": last}, nil
			},
		},
	}
}
