package main

import (
	"bytes"
	"context"
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
	"github.com/ly-solutions/harnessly/internal/wire"
)

// connectRoutes serve Harnessly Connect: devices pair and dial in on /connect/v1 (their own Ed25519 auth), the
// agent's shell wrapper runs a command on one (cli-token), and the person issues codes and lists devices.
//
//	GET  /connect/install/<code>[.ps1]  the one-command installer for a pairing code (sh, PowerShell)
//	POST /connect/v1/register           a pairing code and a public key make a device
//	GET  /connect/v1/dial               a device's link (WebSocket)
//	GET  /api/cli/connect/exec          the wrapper's socket: one command on a device
//	GET  /api/cli/connect/fs            a mount's socket: file operations on a device
//	POST /api/cli/connect/code          a new pairing code
//	GET  /api/cli/connect/devices       the devices, online first
//	POST /api/cli/connect/revoke        {"device": id or name}
//	GET  /api/cli/connect/folders       device folders chats start in; POST {device, dir} adds, {op:"remove", dir}
//	GET  /api/cli/connect/route         ?session=<tmux> or ?cwd=: the device a chat's folder lives on, if any
//
// connectChats is what the switch of one chat to a device needs of the conversations.
type connectChats struct {
	// record is the conversation; false when there is none.
	record func(conv string) (wire.Conversation, bool)
	// convOf is the conversation running in a tmux session, "" for none.
	convOf func(session string) string
	// relaunch starts the conversation's agent again at its next rest, wherever its route now says.
	relaunch func(ctx context.Context, conv string) error
}

func connectRoutes(d deps, tabCwd func(session string) string, chats connectChats) []gate.Route {
	srv := &connect.Server{Dir: filepath.Join(d.DataDir, "connect"), Logf: log.Printf,
		URL: os.Getenv("RUNTIME_CONNECT_URL"), Releases: os.Getenv("RUNTIME_CONNECT_RELEASES"),
		Download: os.Getenv("RUNTIME_CONNECT_DOWNLOAD")}
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
		{Match: func(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/connect/install/") },
			Handler: http.HandlerFunc(srv.ServeInstall)},
		route("/connect/v1/register", nil, srv.ServeRegister),
		route("/connect/v1/dial", nil, srv.ServeDial),
		route("/api/cli/connect/exec", d.CLI.OK, srv.ServeExec),
		route("/api/cli/connect/fs", d.CLI.OK, srv.ServeFS),
		route("/api/cli/connect/code", person, post(func(w http.ResponseWriter, r *http.Request) {
			code, exp := srv.IssueCode()
			base := srv.PublicBase(r)
			if u := r.URL.Query().Get("url"); u != "" {
				base = u
			}
			unix, win := connect.InstallCommands(base, code)
			peerJSON(w, http.StatusOK, map[string]any{"code": code, "expires": exp.UTC().Format(time.RFC3339),
				"install": unix, "installWindows": win})
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
			cwd, conv := r.URL.Query().Get("cwd"), ""
			if s := r.URL.Query().Get("session"); s != "" {
				if tabCwd != nil {
					cwd = tabCwd(s)
				}
				if chats.convOf != nil {
					conv = chats.convOf(s)
				}
			}
			dev, dir, start, ok := srv.RouteChat(conv, cwd)
			if !ok {
				peerJSON(w, http.StatusOK, map[string]any{})
				return
			}
			out := map[string]any{"device": dev.ID, "name": dev.Name, "os": dev.OS, "online": dev.Online, "dir": dir, "cwd": start}
			if _, switched := srv.ChatOf(conv); switched && conv != "" {
				out["from"] = cwd // the chat's folder here: what its earlier turns name
			}
			peerJSON(w, http.StatusOK, out)
		}),
		// One chat on a device or back here: GET ?conv= says where it is and where it could go, POST {conv, device, dir}
		// switches it (device "" puts it back), its agent starting again there once its turn is over.
		route("/api/cli/connect/chat", person, func(w http.ResponseWriter, r *http.Request) {
			serveConnectChat(w, r, srv, chats)
		}),
		// The same for the page, under its own API (with or without the app's /agents base), on the person's session.
		{
			Match: func(r *http.Request) bool { return pathIs(r, platformAPI+"/connect/chat") },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !person(r) {
					peerJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
					return
				}
				serveConnectChat(w, r, srv, chats)
			}),
		},
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

// connectChatState is what the page's «Где работает» shows of one chat.
type connectChatState struct {
	// On is the device the chat was switched to; nil: it runs here (or in a device folder of its own, Folder).
	On *connect.ChatRoute `json:"on"`
	// Folder: the chat's own folder is a device folder, so it runs there whatever is switched.
	Folder bool `json:"folder,omitempty"`
	// Cwd is the chat's folder on this machine, where it goes back to.
	Cwd string `json:"cwd"`
	// Claude: only Claude chats run through Connect.
	Claude bool `json:"claude"`
	Busy   bool `json:"busy"`
	// Devices are the paired devices, with a folder on each that looks like the chat's project.
	Devices []connectChatDevice `json:"devices"`
}

type connectChatDevice struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	OS      string `json:"os"`
	Online  bool   `json:"online"`
	Suggest string `json:"suggest,omitempty"`
}

func serveConnectChat(w http.ResponseWriter, r *http.Request, srv *connect.Server, chats connectChats) {
	if chats.record == nil || chats.relaunch == nil {
		peerJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "чаты ещё не готовы"})
		return
	}
	if r.Method == http.MethodGet {
		conv := r.URL.Query().Get("conv")
		if s := r.URL.Query().Get("session"); conv == "" && s != "" && chats.convOf != nil {
			conv = chats.convOf(s)
		}
		rec, ok := chats.record(conv)
		if !ok {
			peerJSON(w, http.StatusNotFound, map[string]string{"error": "нет такого чата"})
			return
		}
		st := connectChatState{Cwd: rec.Cwd, Claude: rec.Provider == wire.ProviderClaude,
			Busy: rec.State.K == wire.StateWorking || rec.State.K == wire.StateWaitingQuestion || rec.State.K == wire.StateWaitingPermission}
		if c, ok := srv.ChatOf(conv); ok {
			st.On = &c
		}
		_, _, st.Folder = srv.Route(rec.Cwd)
		for _, dev := range srv.Devices() {
			cd := connectChatDevice{ID: dev.ID, Name: dev.Name, OS: dev.OS, Online: dev.Online}
			if dev.Online {
				cd.Suggest = suggestDeviceDir(r.Context(), srv, dev.ID, rec.Cwd)
			}
			st.Devices = append(st.Devices, cd)
		}
		peerJSON(w, http.StatusOK, st)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		peerJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var req struct{ Conv, Session, Device, Dir string }
	ok := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) == nil
	if req.Conv == "" && req.Session != "" && chats.convOf != nil {
		req.Conv = chats.convOf(req.Session)
	}
	if !ok || req.Conv == "" {
		peerJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	rec, found := chats.record(req.Conv)
	if !found {
		peerJSON(w, http.StatusNotFound, map[string]string{"error": "нет такого чата"})
		return
	}
	if req.Device == "" {
		if !srv.ClearChat(req.Conv) {
			peerJSON(w, http.StatusOK, map[string]any{"ok": true, "changed": false})
			return
		}
	} else {
		if rec.Provider != wire.ProviderClaude {
			peerJSON(w, http.StatusBadRequest, map[string]string{"error": "на устройстве работают только чаты Claude"})
			return
		}
		dev, found := connectDevice(srv, req.Device)
		if !found {
			peerJSON(w, http.StatusNotFound, map[string]string{"error": "нет такого устройства"})
			return
		}
		if !dev.Online {
			peerJSON(w, http.StatusConflict, map[string]string{"error": dev.Name + " офлайн: чат остаётся где был"})
			return
		}
		st := srv.FS(r.Context(), dev.ID, &connect.FSReq{Op: "stat", Path: req.Dir})
		if st.Errno != "" || st.Stat == nil || st.Stat.Mode&connect.SIFMT != connect.SIFDIR {
			peerJSON(w, http.StatusBadRequest, map[string]string{"error": "на " + dev.Name + " нет папки " + req.Dir})
			return
		}
		if _, err := srv.SetChat(req.Conv, dev.ID, req.Dir); err != nil {
			peerJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	if err := chats.relaunch(r.Context(), req.Conv); err != nil {
		peerJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	peerJSON(w, http.StatusOK, map[string]any{"ok": true, "changed": true})
}

func connectDevice(srv *connect.Server, idOrName string) (connect.DeviceRec, bool) {
	for _, d := range srv.Devices() {
		if d.ID == idOrName || d.Name == idOrName {
			return d, true
		}
	}
	return connect.DeviceRec{}, false
}

// suggestDeviceDir is a folder on the device that looks like the chat's project: one of the same name under a
// device folder (or its Projects, src, code), else the device folder itself.
func suggestDeviceDir(ctx context.Context, srv *connect.Server, device, cwd string) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	base := filepath.Base(cwd)
	isDir := func(p string) bool {
		st := srv.FS(ctx, device, &connect.FSReq{Op: "stat", Path: p})
		return st.Errno == "" && st.Stat != nil && st.Stat.Mode&connect.SIFMT == connect.SIFDIR
	}
	first := ""
	for _, f := range srv.Folders() {
		if f.Device != device {
			continue
		}
		if first == "" {
			first = f.Dir
		}
		if base == "" || base == "/" || base == "." {
			continue
		}
		for _, sub := range []string{"", "Projects", "projects", "src", "code", "work"} {
			if p := filepath.Join(f.Dir, sub, base); isDir(p) {
				return p
			}
		}
	}
	return first
}
