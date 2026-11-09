package main

import (
	"net/http"
	"os"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/diskfs"
	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
)

// The Disk tab's file routes: /api/fs/roots, /api/fs/list, /api/fs/favorites, /api/fs/download,
// /api/fs/mkdir and the resumable /api/fs/uploads. Off unless RUNTIME_DISK_ROOTS names the folders the tab may open
// ("Label=/abs/path;Other=/abs/other"); only those folders are reachable, see internal/diskfs.

func init() { bodyRouteGroups = append(bodyRouteGroups, diskBodyRoutes) }

// diskServer is the server for the named roots; nil when none is named or the setting is wrong.
func diskServer(d deps) *diskfs.Server {
	spec := os.Getenv("RUNTIME_DISK_ROOTS")
	if spec == "" {
		return nil
	}
	roots, err := diskfs.ParseRoots(spec)
	if err != nil {
		agent.Log("warn", "disk: RUNTIME_DISK_ROOTS is not valid; the disk routes stay off", agent.F{K: "err", V: err.Error()})
		return nil
	}
	return diskfs.New(diskfs.Config{Roots: roots, StateDir: d.DataDir})
}

func diskBodyRoutes(d deps) []gate.BodyRoute {
	srv := diskServer(d)
	if srv == nil {
		return nil
	}
	serve := func(w http.ResponseWriter, r *http.Request) {
		if _, _, _, refused := d.APIRefusal(r); refused {
			files.WriteUnauthorized(w)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !d.SessionOK(r) { // a cli-token alone is not a person
			w.WriteHeader(http.StatusForbidden)
			return
		}
		parts, _, _ := files.APIParts(r)
		srv.Serve(w, r, parts[2:])
	}
	at := func(read bool) func(*http.Request) bool {
		return func(r *http.Request) bool {
			parts, _, ok := files.APIParts(r)
			if !ok || len(parts) < 3 || parts[0] != "api" || parts[1] != "fs" {
				return false
			}
			isRead := r.Method == http.MethodGet || r.Method == http.MethodHead
			return isRead == read
		}
	}
	return []gate.BodyRoute{
		{Slice: "fsread", Match: at(true), Serve: serve},
		{Slice: "fswrite", Match: at(false), Serve: serve},
	}
}

// diskPaths names the routes in one place for the route check (tools/web-routes-check.mjs) and the docs.
var diskPaths = []string{"/api/fs/roots", "/api/fs/list", "/api/fs/favorites", "/api/fs/download", "/api/fs/mkdir", "/api/fs/uploads"}
