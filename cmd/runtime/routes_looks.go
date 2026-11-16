package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/looks"
	"github.com/ly-solutions/harnessly/internal/settingsctl"
)

// An agent changes the person's look from a chat (docs/looks.md). The looks are the person's prefs (hx-looks),
// which on a hub belong to the hub user and which a machine never writes: the runtime holds the agent's request, the
// person's page puts it on and shows «Применено · Отменить», and the CLI waits for that.
//
//	GET  <platform>/look-requests                 {requests: [Request]}: what waits for a page (the person's session)
//	POST <platform>/look-requests/<id>/done       {state: applied|failed, note?}
//	PUT  <platform>/look-state                    {looks}: the page's hx-looks, for `harnessly theme get`
//	GET  /api/cli/looks                           {looks, presets, scenes}: an agent of this machine (cli-token)
//	POST /api/cli/looks/set                       {scope, look?|preset?|reset?, why, wait} -> {status, request}
//
// Pages learn that a request came through the settings' watch (settingsctl.Touch): no watch of its own.

// lookInbox is the machine's one inbox of the agents' looks: the CLI's routes and the agents' MCP tools put in it.
var lookInbox = &looks.Inbox{}

func lookRoutes(inbox *looks.Inbox, actorOf func(*http.Request) settingsctl.Actor,
	pageOK func(http.ResponseWriter, *http.Request) bool,
	cliOK func(http.ResponseWriter, *http.Request, string) bool) []gate.Route {
	bad := func(w http.ResponseWriter, msg string) {
		secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request", "message": msg})
	}
	readJSON := func(w http.ResponseWriter, r *http.Request, limit int64, out any) bool {
		raw, err := io.ReadAll(io.LimitReader(r.Body, limit))
		if err != nil || json.Unmarshal(raw, out) != nil {
			bad(w, "not JSON")
			return false
		}
		return true
	}
	reqBase := platformAPI + "/look-requests"
	requests := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"requests": inbox.Pending()})
	}
	doneOne := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		rest := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/agents"), reqBase+"/")
		id, verb, _ := strings.Cut(rest, "/")
		if r.Method != http.MethodPost || verb != "done" {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		var b struct {
			State string `json:"state"`
			Note  string `json:"note"`
		}
		if !readJSON(w, r, 4<<10, &b) {
			return
		}
		if !inbox.Done(id, b.State, b.Note) {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		secretJSON(w, http.StatusOK, map[string]string{"state": b.State})
	}
	state := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		if r.Method != http.MethodPut {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		var b struct {
			Looks json.RawMessage `json:"looks"`
		}
		if !readJSON(w, r, 64<<10, &b) {
			return
		}
		inbox.SetState(b.Looks)
		secretJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
	cliGet := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodGet) {
			return
		}
		cur := inbox.State()
		if cur == nil {
			cur = json.RawMessage("null")
		}
		secretJSON(w, http.StatusOK, map[string]any{"looks": cur, "presets": looks.Presets(), "scenes": looks.Scenes})
	}
	cliSet := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodPost) {
			return
		}
		var b struct {
			Scope  string          `json:"scope"`
			Look   json.RawMessage `json:"look"`
			Preset string          `json:"preset"`
			Reset  bool            `json:"reset"`
			Why    string          `json:"why"`
			Wait   float64         `json:"wait"`
		}
		if !readJSON(w, r, 16<<10, &b) {
			return
		}
		by := actorOf(r)
		out, status, err := inbox.Submit(r.Context(), looks.Ask{Scope: b.Scope, Conv: by.Conv, Look: b.Look, Preset: b.Preset,
			Reset: b.Reset, Why: b.Why, Wait: time.Duration(b.Wait * float64(time.Second))})
		if err != nil {
			bad(w, err.Error())
			return
		}
		agent.Log("info", "looks: an agent's change", agent.F{K: "scope", V: out.Scope}, agent.F{K: "conv", V: by.Conv}, agent.F{K: "status", V: status})
		secretJSON(w, http.StatusOK, map[string]any{"status": status, "request": out})
	}
	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	return []gate.Route{
		{Match: is(reqBase), Handler: http.HandlerFunc(requests)},
		{Match: func(r *http.Request) bool {
			p := strings.TrimPrefix(r.URL.Path, "/agents")
			return strings.HasPrefix(p, reqBase+"/") && len(p) > len(reqBase)+1
		}, Handler: http.HandlerFunc(doneOne)},
		{Match: is(platformAPI + "/look-state"), Handler: http.HandlerFunc(state)},
		{Match: is("/api/cli/looks"), Handler: http.HandlerFunc(cliGet)},
		{Match: is("/api/cli/looks/set"), Handler: http.HandlerFunc(cliSet)},
	}
}
