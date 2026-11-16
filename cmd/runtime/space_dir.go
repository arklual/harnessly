package main

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The chat of the boards and the documents (the spaces) works in a folder of its own: not the folder of the
// person's last chat, whose CLAUDE.md and project memory are another project's (15.11: a board's chat read the memory of
// an analytics page). The folder is empty but for what the chat makes there.
//
//	GET <platform>/spaces/dir -> {dir}

// spaceDirName is the folder under the person's home.
const spaceDirName = "harnessly-spaces"

// spaceChatDir is the spaces' folder, made when missing; "" when it cannot be.
func spaceChatDir(home string) string {
	if home == "" {
		return ""
	}
	dir := filepath.Join(home, spaceDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return dir
}

func spaceDirRoutes(d deps) []gate.Route {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sameorigin.Refuse(w, r) {
			return
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		dir := spaceChatDir(d.Home)
		if dir == "" {
			secretJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no-dir"})
			return
		}
		secretJSON(w, http.StatusOK, map[string]string{"dir": dir})
	})
	var routes []gate.Route
	for _, path := range []string{platformAPI + "/spaces/dir", "/agents" + platformAPI + "/spaces/dir"} {
		routes = append(routes, gate.Route{Match: func(r *http.Request) bool { return r.URL.Path == path }, Handler: handler})
	}
	return routes
}
