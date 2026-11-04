package main

import (
	"net/http"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The panel exchanges status/tabs JSON, generation-tagged JPEG frames and Input
// commands; internal/agentbrowser/protocol.go is the shared panel contract.
func agentBrowserRoutes(d deps, m *agentbrowser.Manager, exists func(string) bool) []gate.Route {
	return []gate.Route{{Match: func(r *http.Request) bool { return pathIs(r, "/api/agent-browser") }, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sameorigin.Refuse(w, r) {
			return
		}
		if !d.SessionOK(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !exists(r.URL.Query().Get("conv")) {
			http.NotFound(w, r)
			return
		}
		if !m.Enabled() {
			http.Error(w, "Agent browser is disabled", http.StatusServiceUnavailable)
			return
		}
		m.ServeHTTP(w, r)
	})}}
}
