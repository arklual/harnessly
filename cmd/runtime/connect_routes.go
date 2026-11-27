package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/connect"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/hlcli"
)

// connectRoutes serve Harnessly Connect: devices pair and dial in on /connect/v1 (their own Ed25519 auth), the
// agent's shell wrapper runs a command on one (cli-token), and the person issues codes and lists devices.
//
//	POST /connect/v1/register           a pairing code and a public key make a device
//	GET  /connect/v1/dial               a device's link (WebSocket)
//	GET  /api/cli/connect/exec          the wrapper's socket: one command on a device
//	GET  /api/cli/connect/fs            a mount's socket: file operations on a device
//	POST /api/cli/connect/code          a new pairing code
//	GET  /api/cli/connect/devices       the devices, online first
//	POST /api/cli/connect/revoke        {"device": id or name}
//	GET  /api/cli/connect/folders       device folders chats start in; POST {device, dir} adds, {op:"remove", dir}
//	GET  /api/cli/connect/route         ?session=<tmux> or ?cwd=: the device a chat's folder lives on, if any
func connectRoutes(d deps, tabCwd func(session string) string) []gate.Route {
	srv := &connect.Server{Dir: filepath.Join(d.DataDir, "connect"), Logf: log.Printf}
	srv.Load()
	// The shim on a device: the chat's harnessly commands run here, as if the agent had typed them in its own pane.
	cliEnv := func(ctx map[string]string) hlcli.Env {
		env := hlcli.OSEnv()
		env.Getenv = func(k string) string {
			if v, ok := ctx[k]; ok {
				return v
			}
			return os.Getenv(k)
		}
		env.Session = func() string { return ctx["session"] }
		if cwd := ctx["cwd"]; cwd != "" {
			env.Cwd = cwd
		}
		return env
	}
	srv.RunCLI = func(ctx map[string]string, args []string, stdin []byte) (int, []byte, []byte) {
		var out, errb bytes.Buffer
		env := cliEnv(ctx)
		env.Stdin, env.Stdout, env.Stderr = bytes.NewReader(stdin), &out, &errb
		code := hlcli.Run(args, env)
		return code, out.Bytes(), errb.Bytes()
	}
	srv.Secrets = func(ctx map[string]string, names []string) (map[string]string, error) {
		vals := map[string]string{}
		for _, n := range names {
			var out, errb bytes.Buffer
			env := cliEnv(ctx)
			env.Stdout, env.Stderr = &out, &errb
			if code := hlcli.Run([]string{"secret", "file", n}, env); code != 0 {
				return nil, fmt.Errorf("секрет %s: %s", n, strings.TrimSpace(errb.String()))
			}
			p := strings.TrimSpace(out.String())
			raw, err := os.ReadFile(p)
			_ = os.Remove(p)
			if err != nil {
				return nil, err
			}
			vals[n] = string(raw)
		}
		return vals, nil
	}
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
		route("/api/cli/connect/fs", d.CLI.OK, srv.ServeFS),
		route("/api/cli/connect/code", person, post(func(w http.ResponseWriter, r *http.Request) {
			code, exp := srv.IssueCode()
			peerJSON(w, http.StatusOK, map[string]any{"code": code, "expires": exp.UTC().Format(time.RFC3339)})
		})),
		route("/api/cli/connect/devices", person, func(w http.ResponseWriter, r *http.Request) {
			peerJSON(w, http.StatusOK, map[string]any{"devices": srv.Devices()})
		}),
		route("/api/cli/connect/folders", person, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				peerJSON(w, http.StatusOK, map[string]any{"folders": srv.Folders()})
				return
			}
			var req struct{ Device, Dir, Op string }
			if r.Method != http.MethodPost || json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
				peerJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
				return
			}
			if req.Op == "remove" {
				if !srv.RemoveFolder(req.Dir) {
					peerJSON(w, http.StatusNotFound, map[string]string{"error": "no such folder"})
					return
				}
				peerJSON(w, http.StatusOK, map[string]any{"ok": true})
				return
			}
			f, err := srv.AddFolder(req.Device, req.Dir)
			if err != nil {
				peerJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			peerJSON(w, http.StatusOK, map[string]any{"folder": f})
		}),
		// The Claude launcher (RUNTIME_CLAUDE_LAUNCHER=connect-chat) asks where the chat of its tmux session runs.
		route("/api/cli/connect/route", d.CLI.OK, func(w http.ResponseWriter, r *http.Request) {
			cwd := r.URL.Query().Get("cwd")
			if s := r.URL.Query().Get("session"); s != "" && tabCwd != nil {
				cwd = tabCwd(s)
			}
			dev, dir, ok := srv.Route(cwd)
			if !ok {
				peerJSON(w, http.StatusOK, map[string]any{})
				return
			}
			peerJSON(w, http.StatusOK, map[string]any{"device": dev.ID, "name": dev.Name, "os": dev.OS, "online": dev.Online,
				"dir": dir, "cwd": cwd})
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
