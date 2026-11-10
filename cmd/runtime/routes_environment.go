package main

import (
	"errors"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/envsetup"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The start page's «Настройка машины» card: GET <platform>/environment is how the setup of the hub's list
// stands on this machine; POST <platform>/environment/run puts a run of the list in the queue («Повторить»). Both are
// the page's, under its session; the agent's CLI has its own way in.

func envSetupRoutes(d deps, runners []*envsetup.Runner) []gate.Route {
	base := platformAPI + "/environment"
	ok := func(w http.ResponseWriter, r *http.Request, method string) bool {
		if sameorigin.Refuse(w, r) {
			return false
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return false
		}
		if r.Method != method {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return false
		}
		return true
	}
	// The runner of the link with a list: an own link and an observe link do not both hold one on a machine.
	pick := func() (*envsetup.Runner, envsetup.View) {
		var first *envsetup.Runner
		var firstView envsetup.View
		for _, rn := range runners {
			v := rn.View()
			if v.Hash != "" {
				return rn, v
			}
			if first == nil {
				first, firstView = rn, v
			}
		}
		return first, firstView
	}
	view := func(w http.ResponseWriter, r *http.Request) {
		if !ok(w, r, http.MethodGet) {
			return
		}
		_, v := pick()
		if v.Items == nil {
			v = envsetup.View{Items: []envsetup.ViewItem{}}
		}
		secretJSON(w, http.StatusOK, v)
	}
	run := func(w http.ResponseWriter, r *http.Request) {
		if !ok(w, r, http.MethodPost) {
			return
		}
		rn, v := pick()
		if rn == nil || v.Hash == "" {
			secretJSON(w, http.StatusConflict, map[string]string{"error": "no-list"})
			return
		}
		id, err := rn.Ask("")
		switch {
		case errors.Is(err, envsetup.ErrSetupOff):
			secretJSON(w, http.StatusConflict, map[string]string{"error": "setup-off"})
		case err != nil:
			secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
		default:
			secretJSON(w, http.StatusOK, map[string]string{"runId": id})
		}
	}
	return []gate.Route{
		{Match: func(r *http.Request) bool { return pathIs(r, base) }, Handler: http.HandlerFunc(view)},
		{Match: func(r *http.Request) bool { return pathIs(r, base+"/run") }, Handler: http.HandlerFunc(run)},
	}
}
