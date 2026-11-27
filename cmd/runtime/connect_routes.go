package main

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/ly-solutions/harnessly/internal/connect"
	"github.com/ly-solutions/harnessly/internal/gate"
)

// connectRoutes serve Harnessly Connect: devices pair and dial in on /connect/v1 (their own Ed25519 auth), the
// agent's shell wrapper runs a command on one (cli-token), and the person issues codes and lists devices.
//
//	POST /connect/v1/register           a pairing code and a public key make a device
//	GET  /connect/v1/dial               a device's link (WebSocket)
//	GET  /api/cli/connect/exec          the wrapper's socket: one command on a device
//	POST /api/cli/connect/code          a new pairing code
//	GET  /api/cli/connect/devices       the devices, online first
//	POST /api/cli/connect/revoke        {"device": id or name}
func connectRoutes(d deps) []gate.Route {
	srv := &connect.Server{Dir: filepath.Join(d.DataDir, "connect"), Logf: log.Printf}
	person := func(r *http.Request) bool { return d.CLI.OK(r) || d.SessionOK(r) }
	route := func(path string, auth func(*http.Request) bool, h http.HandlerFunc) gate.Route {
		return gate.Route{
			Match: func(r *http.Request) bool { return r.URL.Path == path },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if auth != nil && !auth(r) {
					peerJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
					return
				}
				h(w, r)
			}),
		}
	}
	post := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				peerJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
				return
			}
			h(w, r)
		}
	}
	return []gate.Route{
		route("/connect/v1/register", nil, srv.ServeRegister),
		route("/connect/v1/dial", nil, srv.ServeDial),
		route("/api/cli/connect/exec", d.CLI.OK, srv.ServeExec),
		route("/api/cli/connect/code", person, post(func(w http.ResponseWriter, r *http.Request) {
			code, exp := srv.IssueCode()
			peerJSON(w, http.StatusOK, map[string]any{"code": code, "expires": exp.UTC().Format(time.RFC3339)})
		})),
		route("/api/cli/connect/devices", person, func(w http.ResponseWriter, r *http.Request) {
			peerJSON(w, http.StatusOK, map[string]any{"devices": srv.Devices()})
		}),
		route("/api/cli/connect/revoke", person, post(func(w http.ResponseWriter, r *http.Request) {
			var req struct{ Device string }
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil || req.Device == "" {
				peerJSON(w, http.StatusBadRequest, map[string]string{"error": "device required"})
				return
			}
			if !srv.Revoke(req.Device) {
				peerJSON(w, http.StatusNotFound, map[string]string{"error": "no such device"})
				return
			}
			peerJSON(w, http.StatusOK, map[string]any{"ok": true})
		})),
	}
}
