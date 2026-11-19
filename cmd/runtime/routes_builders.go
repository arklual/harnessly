package main

import (
	"net/http"
	"os"

	"github.com/ly-solutions/harnessly/internal/builders"
	"github.com/ly-solutions/harnessly/internal/gate"
)

// The pool of the hub's builder machines for the page (docs/hub/builders.md): the list a «render» button offers
// beside «auto», and a job's status by id. The machine agent signs for the hub (internal/builders); a machine that is not
// in a hub has no pool and the list says so, so the button just runs on this machine's own slots.

// builderClient is the client of this machine's agent, or nil when it has none.
func builderClient(d deps) *builders.Client {
	c, err := builders.NewLocal(d.DataDir, os.Getenv("RUNTIME_AGENT_TOKEN_HEADER"))
	if err != nil {
		return nil
	}
	return c
}

func builderRoutes(d deps) []gate.Route {
	pageOK := func(w http.ResponseWriter, r *http.Request) bool {
		if !d.SessionOK(r) {
			peerJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return false
		}
		return true
	}
	route := func(p string, h http.HandlerFunc) gate.Route {
		return gate.Route{Match: func(r *http.Request) bool { return pathIs(r, p) }, Handler: h}
	}
	return []gate.Route{
		route(platformAPI+"/builders", func(w http.ResponseWriter, r *http.Request) {
			if !pageOK(w, r) {
				return
			}
			c := builderClient(d)
			if c == nil {
				peerJSON(w, http.StatusOK, map[string]any{"hub": false, "builders": []builders.Builder{}})
				return
			}
			list, err := c.Builders(r.Context())
			if err != nil {
				peerJSON(w, http.StatusOK, map[string]any{"hub": true, "builders": []builders.Builder{}, "error": err.Error()})
				return
			}
			if list == nil {
				list = []builders.Builder{}
			}
			peerJSON(w, http.StatusOK, map[string]any{"hub": true, "builders": list})
		}),
		route(platformAPI+"/jobs", func(w http.ResponseWriter, r *http.Request) {
			if !pageOK(w, r) {
				return
			}
			c := builderClient(d)
			id := r.URL.Query().Get("id")
			if c == nil || id == "" {
				peerJSON(w, http.StatusBadRequest, map[string]string{"error": "GET ?id=<job id> on a machine of a hub"})
				return
			}
			j, err := c.Get(r.Context(), id)
			if err != nil {
				peerJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
			peerJSON(w, http.StatusOK, map[string]any{"job": j})
		}),
	}
}
