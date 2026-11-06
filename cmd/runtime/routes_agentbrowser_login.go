package main

import (
	"context"
	"net/http"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

func agentBrowserLoginRoutes(d deps, a *agentbrowser.ProfileAccess) []gate.Route {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sameorigin.Refuse(w, r) {
			return
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		if a == nil || a.Manager == nil || !a.Manager.Enabled() {
			secretJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Agent browser is disabled"})
			return
		}
		switch r.Method {
		case http.MethodGet:
			secretJSON(w, http.StatusOK, map[string]any{"asks": a.Logins.Pending()})
		case http.MethodPost:
			var body struct {
				ID     string `json:"id"`
				Action string `json:"action"`
			}
			if !readSecretBody(r, &body) {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request"})
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			if err := a.AnswerLogin(ctx, body.ID, body.Action); err != nil {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			secretJSON(w, http.StatusOK, map[string]bool{"ok": true})
		default:
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		}
	})
	return []gate.Route{{Match: func(r *http.Request) bool { return pathIs(r, "/api/agent-browser/logins") }, Handler: handler}}
}
