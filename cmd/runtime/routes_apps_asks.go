package main

import (
	"errors"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The choices an agent puts to the person through app.ask (internal/apps/asks.go). The person's page session only:
//
//	GET  <platform>/apps/asks?conversation=<id>   the chat's open choices: {asks: [{id, app, appName, question, options, createdAt}]}
//	POST <platform>/apps/asks/answer              {id, answer: <one of the options> | null}: null cancels
func appAskRoutes(d deps, asks *apps.Asks) []gate.Route {
	guard := func(method string, w http.ResponseWriter, r *http.Request) bool {
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
	list := func(w http.ResponseWriter, r *http.Request) {
		if !guard(http.MethodGet, w, r) {
			return
		}
		conv := r.URL.Query().Get("conversation")
		if conv == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"asks": asks.Pending(conv)})
	}
	answer := func(w http.ResponseWriter, r *http.Request) {
		if !guard(http.MethodPost, w, r) {
			return
		}
		var b struct {
			ID     string  `json:"id"`
			Answer *string `json:"answer"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		switch err := asks.Answer(b.ID, b.Answer); {
		case errors.Is(err, apps.ErrAskGone):
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
		case errors.Is(err, apps.ErrAskAnswer):
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-answer"})
		default:
			secretJSON(w, http.StatusOK, map[string]string{})
		}
	}
	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	return []gate.Route{
		{Match: is(platformAPI + "/apps/asks"), Handler: http.HandlerFunc(list)},
		{Match: is("/agents" + platformAPI + "/apps/asks"), Handler: http.HandlerFunc(list)},
		{Match: is(platformAPI + "/apps/asks/answer"), Handler: http.HandlerFunc(answer)},
		{Match: is("/agents" + platformAPI + "/apps/asks/answer"), Handler: http.HandlerFunc(answer)},
	}
}
