package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/integrations"
)

// The page's side of the boards and artifacts integrations: where the services are, a sign-in hand-off for the board
// pane, and the documents of a conversation. The calls that need a service token run here, so tokens stay on the
// machine. In front of the previous runtime the routes exist only when a service is configured, so a machine without
// one answers exactly as before.

func integrationRoutes(d deps, c integrations.Config) []gate.Route {
	if !d.Standalone && !c.Enabled() {
		return nil
	}
	write := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	refused := func(w http.ResponseWriter, r *http.Request) bool {
		if status, v, h, refused := d.APIRefusal(r); refused {
			for k, vs := range h {
				w.Header()[k] = vs
			}
			write(w, status, v)
			return true
		}
		return false
	}
	failed := func(w http.ResponseWriter, service string, err error) {
		var ue *integrations.UpstreamError
		switch {
		case errors.Is(err, integrations.ErrNotConfigured):
			write(w, http.StatusNotFound, map[string]string{"error": service + " is not configured"})
		case errors.Is(err, integrations.ErrBadPath):
			write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		case errors.As(err, &ue):
			write(w, http.StatusBadGateway, map[string]any{"error": service + ": " + ue.Text, "status": ue.Status})
		default:
			agent.Log("warn", "integrations: "+service+" unreachable", agent.F{K: "err", V: err.Error()})
			write(w, http.StatusBadGateway, map[string]string{"error": service + " is unreachable"})
		}
	}
	return []gate.Route{
		{
			Match: func(r *http.Request) bool { return pathIs(r, "/api/integrations") },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if refused(w, r) {
					return
				}
				if r.Method != http.MethodGet {
					w.Header().Set("Allow", "GET")
					write(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
					return
				}
				write(w, 200, c.Public())
			}),
		},
		{
			Match: func(r *http.Request) bool { return pathIs(r, "/api/integrations/boards/sign-in") },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if refused(w, r) {
					return
				}
				if r.Method != http.MethodPost {
					w.Header().Set("Allow", "POST")
					write(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
					return
				}
				if !d.SessionOK(r) { // a hand-off signs a person in; a cli-token is not one
					write(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
					return
				}
				var body struct {
					Next string `json:"next"`
				}
				if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); err != nil {
					write(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
					return
				}
				u, err := c.BoardSignIn(r.Context(), personOf(d.Claims(r)), body.Next)
				if err != nil {
					failed(w, "boards", err)
					return
				}
				write(w, 200, map[string]string{"url": u})
			}),
		},
		{
			// The boards to attach to a message: listed with the runtime's token, which never reaches the page.
			Match: func(r *http.Request) bool { return pathIs(r, "/api/integrations/boards") },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if refused(w, r) {
					return
				}
				if r.Method != http.MethodGet {
					w.Header().Set("Allow", "GET")
					write(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
					return
				}
				boards, err := c.ListBoards(r.Context())
				if err != nil {
					failed(w, "boards", err)
					return
				}
				write(w, 200, map[string]any{"boards": boards})
			}),
		},
		{
			Match: func(r *http.Request) bool { return pathIs(r, "/api/integrations/artifacts/documents") },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if refused(w, r) {
					return
				}
				if r.Method != http.MethodGet {
					w.Header().Set("Allow", "GET")
					write(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
					return
				}
				session := r.URL.Query().Get("session")
				if session == "" || len(session) > 200 {
					write(w, http.StatusBadRequest, map[string]string{"error": "session parameter required"})
					return
				}
				docs, err := c.Documents(r.Context(), session)
				if err != nil {
					failed(w, "artifacts", err)
					return
				}
				write(w, 200, map[string]any{"documents": docs})
			}),
		},
	}
}

// personOf is who a board hand-off signs in: the page session's subject, named by the deployment when it knows the
// owner (RUNTIME_OWNER_NAME, RUNTIME_OWNER_EMAIL).
func personOf(claims map[string]any) integrations.User {
	str := func(k string) string { s, _ := claims[k].(string); return strings.TrimSpace(s) }
	u := integrations.User{ID: str("sub"), Name: str("name"), Email: str("email")}
	owner := os.Getenv("RUNTIME_OWNER_EMAIL")
	if u.Email == "" && strings.Contains(u.ID, "@") {
		u.Email = u.ID
	}
	if u.Email == "" && strings.Contains(owner, "@") {
		u.Email = owner
	}
	if u.ID == "" || u.ID == "user" && u.Email != "" {
		u.ID = u.Email
	}
	if u.ID == "" {
		u.ID = "user"
	}
	if u.Name == "" {
		u.Name = env("RUNTIME_OWNER_NAME", u.ID)
	}
	return u
}
