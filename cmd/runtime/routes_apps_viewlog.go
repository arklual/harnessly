package main

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The errors of the apps' views (internal/apps viewlog.go):
//
//	POST <platform>/apps/view-log   the page: {id, view, lines: [{at, level, text}]} — what a view it runs reported
//	GET  /api/cli/apps/logs?id=&limit=   harnessly app logs <id>: the last lines, oldest first
//
// Every line kept goes to the runtime's own log too, a few a minute for each app, so a view that throws in a loop does
// not fill it.

// viewLogRate is how many lines of one app the runtime's log takes in a minute.
const viewLogRate = 20

func appViewLogRoutes(d deps, reg *apps.Registry) []gate.Route {
	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	var mu sync.Mutex
	logged := map[string][]time.Time{}
	toLog := func(id string, now time.Time) bool {
		mu.Lock()
		defer mu.Unlock()
		recent := logged[id][:0:0]
		for _, at := range logged[id] {
			if now.Sub(at) < time.Minute {
				recent = append(recent, at)
			}
		}
		if len(recent) >= viewLogRate {
			logged[id] = recent
			return false
		}
		logged[id] = append(recent, now)
		return true
	}

	report := func(w http.ResponseWriter, r *http.Request) {
		if sameorigin.Refuse(w, r) {
			return
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		if r.Method != http.MethodPost {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		var b struct {
			ID    string             `json:"id"`
			View  string             `json:"view"`
			Lines []apps.ViewLogLine `json:"lines"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		if _, err := reg.Find(b.ID); err != nil {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		for i := range b.Lines {
			if b.Lines[i].View == "" {
				b.Lines[i].View = b.View
			}
		}
		now := time.Now()
		kept, err := reg.AppendViewLog(b.ID, b.Lines, now)
		if err != nil {
			agent.Log("warn", "apps: view log: "+err.Error())
			secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
			return
		}
		for _, l := range kept {
			if !toLog(b.ID, now) {
				break
			}
			agent.Log("warn", "apps: view "+l.Level, agent.F{K: "id", V: b.ID}, agent.F{K: "view", V: l.View}, agent.F{K: "text", V: clip(l.Text, 300)})
		}
		secretJSON(w, http.StatusOK, map[string]any{"kept": len(kept)})
	}

	logs := func(w http.ResponseWriter, r *http.Request) {
		if d.CLI == nil || !d.CLI.OK(r) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return
		}
		if r.Method != http.MethodGet {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		id := r.URL.Query().Get("id")
		if id == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		if _, err := reg.Find(id); err != nil {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > apps.ViewLogMax {
			limit = apps.ViewLogMax
		}
		lines, err := reg.ViewLog(id, limit)
		if err != nil {
			agent.Log("warn", "apps: view log: "+err.Error())
			secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"id": id, "lines": lines})
	}

	// Written out whole: tools/web-routes-check.mjs reads the served paths from these literals.
	return []gate.Route{
		{Match: is(platformAPI + "/apps/view-log"), Handler: http.HandlerFunc(report)},
		{Match: is("/agents" + platformAPI + "/apps/view-log"), Handler: http.HandlerFunc(report)},
		{Match: is("/api/cli/apps/logs"), Handler: http.HandlerFunc(logs)},
	}
}
