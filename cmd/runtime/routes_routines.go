package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The person's routines — recurring agent tasks the hub schedules and runs (docs/hub/routines.md). The
// runtime only carries the calls: the page (a session) and the `harnessly routine` command and tools (the CLI token)
// ask here, the machine's agent signs the call and the hub acts for the machine's owner.
//
//	GET    <p>/routines              the routines with their last runs and next starts
//	POST   <p>/routines              make one {name, promptTemplate, trigger, routine, model, …}
//	GET    <p>/routines/{id}         one routine
//	PATCH  <p>/routines/{id}         change it (enabled, routine, state: "" clears the memory, …)
//	DELETE <p>/routines/{id}
//	POST   <p>/routines/{id}/run     run it now
//	GET    <p>/routines/{id}/runs    its runs, newest first (?limit=)

const routinesTimeout = 25 * time.Second

var routinesPathRe = regexp.MustCompile(`^/api/[^/]+/routines(?:/([^/]+))?(?:/(run|runs))?$`)

func routinesCall(r *http.Request) (hubproto.RoutinesCall, bool) {
	p := strings.TrimPrefix(r.URL.Path, "/agents")
	if !strings.HasPrefix(p, platformAPI+"/routines") && !strings.HasPrefix(p, "/api/cli/routines") {
		return hubproto.RoutinesCall{}, false
	}
	m := routinesPathRe.FindStringSubmatch(p)
	if m == nil {
		return hubproto.RoutinesCall{}, false
	}
	id, tail := m[1], m[2]
	c := hubproto.RoutinesCall{ID: id}
	switch {
	case id == "" && r.Method == http.MethodGet:
		c.Op = "list"
	case id == "" && r.Method == http.MethodPost:
		c.Op = "create"
	case tail == "" && r.Method == http.MethodGet:
		c.Op = "get"
	case tail == "" && r.Method == http.MethodPatch:
		c.Op = "update"
	case tail == "" && r.Method == http.MethodDelete:
		c.Op = "delete"
	case tail == "run" && r.Method == http.MethodPost:
		c.Op = "run"
	case tail == "runs" && r.Method == http.MethodGet:
		c.Op = "runs"
		c.Limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	default:
		return hubproto.RoutinesCall{}, false
	}
	return c, true
}

func routinesRoutes(d deps) []gate.Route {
	return []gate.Route{{
		Match: func(r *http.Request) bool { _, ok := routinesCall(r); return ok },
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !(d.SessionOK != nil && d.SessionOK(r)) && !(d.CLI != nil && d.CLI.OK(r)) {
				peerJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
				return
			}
			call, _ := routinesCall(r)
			if call.Op == "create" || call.Op == "update" {
				raw, err := io.ReadAll(io.LimitReader(r.Body, 200<<10))
				if err != nil || !json.Valid(raw) {
					peerJSON(w, http.StatusBadRequest, map[string]string{"error": "Тело задачи — JSON"})
					return
				}
				call.Body = raw
			}
			status, raw, err := agent.CallLocal(nil, d.DataDir, os.Getenv("RUNTIME_AGENT_TOKEN_HEADER"), agent.RoutinesRoute, call, routinesTimeout)
			if err != nil {
				peerJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Регулярные задачи живут в хабе, а эта машина не подключена к нему: " + err.Error()})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(status)
			_, _ = w.Write(raw)
		}),
	}}
}
