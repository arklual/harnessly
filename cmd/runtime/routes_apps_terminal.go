package main

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The terminals of the apps: a shell of an app's own on a pty in the chat's folder inside the agent's wall
// (terminal.pty, internal/apps pty.go), and what the chat's terminals show (terminal.read).
//
// The person's page holds the socket of an app's terminal and carries its bytes to the view and back: binary frames
// are what is typed one way and what the shell prints the other; a text frame {"cols","rows"} sets the size, and the
// runtime's last text frame {"exit": code} says how the shell ended. What is typed is never journaled; the opening and
// the end are.

const (
	// maxTerminalWrite is the most one frame of typing may carry.
	maxTerminalWrite = 64 << 10
	// terminalAliveEvery is how often a terminal checks that its app is still on in the chat.
	terminalAliveEvery = 5 * time.Second
	// readLines are the lines of history terminal.read gives by default, and at most.
	readLines    = 200
	maxReadLines = 2000
)

type appLive func(bridgeBody) (apps.Session, apps.Installed, error)
type appWorkspace func(apps.Session, apps.Installed) (apps.FileScope, int, any)

func appTerminalRoutes(d deps, reg *apps.Registry, chats appChats, live appLive, workspaceOf appWorkspace) []gate.Route {
	slots := &apps.TerminalSlots{}
	refuse := func(w http.ResponseWriter, err error) {
		st, out := bridgeRefused(err)
		secretJSON(w, st, out)
	}

	socket := func(w http.ResponseWriter, r *http.Request) {
		if sameorigin.Refuse(w, r) {
			return
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		if r.Method != http.MethodGet {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		q := r.URL.Query()
		sess, app, err := live(bridgeBody{Session: q.Get("session")})
		if err != nil {
			refuse(w, err)
			return
		}
		if err := app.Manifest.Authorize("harnessly/terminal.open", sess.Target, nil); err != nil {
			refuse(w, err)
			return
		}
		scope, status, out := workspaceOf(sess, app)
		if status != 0 {
			secretJSON(w, status, out)
			return
		}
		appID, chat := sess.App, sess.Conversation
		journal := func(what, detail string, ok bool) {
			_ = reg.Journal(appID, apps.JournalEntry{What: what, Class: apps.ClassCritical, Chat: chat, OK: ok, Detail: detail})
		}
		// The agent gets no more through an app than the person allowed: a terminal opened while it uses the app's view
		// opens only with the person's yes, asked by the page.
		if q.Get("confirmed") != "1" && appPages.Driving(chat, appID) {
			journal("terminal.open", "the agent", false)
			refuse(w, &apps.BridgeError{Code: apps.CodePermissionDenied, Message: "the agent is using the app: this needs the person's yes"})
			return
		}
		cols, _ := strconv.Atoi(q.Get("cols"))
		rows, _ := strconv.Atoi(q.Get("rows"))
		dir, err := apps.RunDir(scope.Workspace, q.Get("cwd"))
		if err != nil {
			refuse(w, err)
			return
		}
		box, err := reg.Sandbox(appBoundary())
		if err != nil {
			refuse(w, err)
			return
		}
		release, err := slots.Take(appID, chat)
		if err != nil {
			refuse(w, err)
			return
		}
		defer release()
		term, err := box.Shell(dir, cols, rows)
		if err != nil {
			journal("terminal.open", relDir(scope.Workspace, dir), false)
			refuse(w, err)
			return
		}
		defer term.Close()
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
		if err != nil {
			return
		}
		ws.SetReadLimit(maxTerminalWrite + 1024)
		journal("terminal.open", relDir(scope.Workspace, dir), true)
		serveTerminal(r.Context(), ws, term, func() bool {
			if rec, ok := chats.record(chat); !ok || !slices.Contains(rec.Apps, appID) {
				return false
			}
			now, err := reg.Get(appID)
			return err == nil && now.Manifest != nil && now.Manifest.Allows("terminal.pty")
		}, func(code int) { journal("terminal.close", strconv.Itoa(code), true) })
	}

	// read: what the chat's terminals show, as text; the agent's own ("agent") and the chat's shells.
	read := func(w http.ResponseWriter, r *http.Request) {
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
			bridgeBody
			Terminal string `json:"terminal"`
			Lines    int    `json:"lines"`
		}
		if !readSecretBody(r, &b) {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		sess, app, err := live(b.bridgeBody)
		if err != nil {
			refuse(w, err)
			return
		}
		if err := app.Manifest.Authorize("harnessly/terminal.read", sess.Target, nil); err != nil {
			refuse(w, err)
			return
		}
		if _, status, out := workspaceOf(sess, app); status != 0 {
			secretJSON(w, status, out)
			return
		}
		if d.Tmux == nil {
			refuse(w, &apps.BridgeError{Code: apps.CodeHostUnavailable, Message: "this machine has no terminals"})
			return
		}
		lines := b.Lines
		if lines <= 0 {
			lines = readLines
		}
		lines = min(lines, maxReadLines)
		rec, _ := chats.record(sess.Conversation)
		type shown struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}
		list := []shown{}
		add := func(id, session string) {
			if b.Terminal != "" && b.Terminal != id {
				return
			}
			if text, ok := d.Tmux.CaptureHistory(session, lines); ok {
				list = append(list, shown{ID: id, Text: strings.TrimRight(text, "\n")})
			}
		}
		if rec.Session != "" {
			add("agent", rec.Session)
		}
		for _, name := range d.Tmux.ListSessions() {
			if id, ok := d.Tmux.OwnShellID(sess.Conversation, name); ok {
				add(id, name)
			}
		}
		_ = reg.Journal(sess.App, apps.JournalEntry{What: "terminal.read", Class: apps.ClassHigh, Chat: sess.Conversation, OK: true,
			Detail: strconv.Itoa(len(list)) + " " + b.Terminal})
		secretJSON(w, http.StatusOK, map[string]any{"terminals": list})
	}

	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	return []gate.Route{
		{Match: is(platformAPI + "/apps/terminal"), Handler: http.HandlerFunc(socket)},
		{Match: is("/agents" + platformAPI + "/apps/terminal"), Handler: http.HandlerFunc(socket)},
		{Match: is(platformAPI + "/apps/terminal/read"), Handler: http.HandlerFunc(read)},
		{Match: is("/agents" + platformAPI + "/apps/terminal/read"), Handler: http.HandlerFunc(read)},
	}
}

// relDir is dir as the journal writes it: inside the chat's folder.
func relDir(workspace, dir string) string {
	rel, err := filepath.Rel(workspace, dir)
	if err != nil {
		return "."
	}
	return rel
}

// serveTerminal carries a terminal's bytes over the socket until the shell ends, the page goes, or alive says no; ended
// hears how the shell ended before the socket's closing handshake (which waits for the page).
func serveTerminal(parent context.Context, ws *websocket.Conn, term *apps.Terminal, alive func() bool, ended func(code int)) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	printed := make(chan struct{})
	go func() {
		defer close(printed)
		buf := make([]byte, 32<<10)
		for {
			n, err := term.Pty.Read(buf)
			if n > 0 {
				if ws.Write(ctx, websocket.MessageBinary, buf[:n]) != nil {
					cancel()
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		for {
			kind, data, err := ws.Read(ctx)
			if err != nil {
				cancel()
				return
			}
			if kind == websocket.MessageBinary {
				if _, err := term.Pty.Write(data); err != nil {
					cancel()
					return
				}
				continue
			}
			var size struct{ Cols, Rows int }
			if json.Unmarshal(data, &size) == nil {
				_ = term.Resize(size.Cols, size.Rows)
			}
		}
	}()
	go func() {
		tick := time.NewTicker(terminalAliveEvery)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if !alive() {
					cancel()
					return
				}
			}
		}
	}()
	select {
	case <-printed:
	case <-ctx.Done():
	}
	term.Close()
	<-printed
	ended(term.Code())
	end, _ := json.Marshal(map[string]int{"exit": term.Code()})
	wctx, wcancel := context.WithTimeout(context.Background(), time.Second)
	defer wcancel()
	_ = ws.Write(wctx, websocket.MessageText, end)
	_ = ws.Close(websocket.StatusNormalClosure, "")
}
