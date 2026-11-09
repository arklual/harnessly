package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ly-solutions/harnessly/internal/secrets"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The calls of an app's views that reach the machine. The page's bridge (web/app/next/features/apps) takes a view's
// message, checks it against the app's permissions and sends it here; the route checks the same table once more, so a
// page that forgot is not a way round the manifest.
//
//	POST <platform>/apps/storage   hl.storage.<scope>.get|set|delete|keys
//	POST <platform>/apps/tool      hl.tools.call: a tool of the app's own remote MCP server
//	POST <platform>/apps/files     hl.files.read|write|list|search|watch|unwatch: the chat's workspace, inside the app's masks
//	POST <platform>/apps/git       hl.git.status|diff|log: the repository of the chat's folder, read only
//	GET  <platform>/apps/file-events  the files that changed for the views that watch (hl.files.watch), by session
//	GET  <platform>/apps/events    a stream of the keys the app storage changed (no values), for the page's views
//
// Both are the person's page session only; a view never reaches them itself (it has no cookie of the page's origin).

// bridgeBody is a view call as the page sends it. Which app, which target and which chat it is are not in it: the
// session's token names them, and the runtime reads them from the session it issued.
type bridgeBody struct {
	Session string          `json:"session"`
	Op      string          `json:"op"`
	Scope   string          `json:"scope"`
	Key     string          `json:"key"`
	Value   json.RawMessage `json:"value"`
	// IfVersion makes a set conditional: it is made only if the key is at that version (hl.storage.<scope>.set).
	IfVersion *int64         `json:"ifVersion"`
	Name      string         `json:"name"`
	Args      map[string]any `json:"args"`
	// Path and Text are a file of the chat's workspace (hl.files): a path relative to it, and what to write.
	Path string  `json:"path"`
	Text *string `json:"text"`
	// Query, Glob, Case and Limit are a search of the workspace (hl.files.search); Limit is git log's length too.
	Query string `json:"query"`
	Glob  string `json:"glob"`
	Case  bool   `json:"case"`
	Limit int    `json:"limit"`
	// Staged and Ref narrow a diff (hl.git.diff).
	Staged bool   `json:"staged"`
	Ref    string `json:"ref"`
	// Command, Cwd and Timeout (seconds) are a command of hl.terminal.run or hl.process.start; ID and Since a process
	// and an offset of its output; Port a local port for hl.preview.open.
	Command string `json:"command"`
	Cwd     string `json:"cwd"`
	Timeout int    `json:"timeout"`
	ID      string `json:"id"`
	Since   int64  `json:"since"`
	Port    int    `json:"port"`
	// Confirmed: the person said yes in the page's own dialog to a loud call made while the agent uses the view.
	Confirmed bool `json:"confirmed"`
	// The session's issue (POST .../apps/session) names these.
	App          string `json:"app"`
	View         string `json:"view"`
	Target       string `json:"target"`
	Conversation string `json:"conversation"`
	// Document is the Sinopia document a view of a sinopia target is shown in.
	Document string `json:"document"`
}

// bridgeRefused is the answer to a refused call: the status and the code the page and the view read.
func bridgeRefused(err error) (int, any) {
	var be *apps.BridgeError
	var conflict *apps.ConflictError
	switch {
	case errors.As(err, &be):
		if be.Detail != "" {
			agent.Log("warn", "apps: bridge: "+be.Code+": "+be.Detail)
		}
		status := http.StatusForbidden
		if be.Code == apps.CodeInvalidParams {
			status = http.StatusBadRequest
		}
		if be.Code == apps.CodeHostUnavailable || be.Code == apps.CodeIdPUnreachable {
			status = http.StatusBadGateway
		}
		// A refused sign-in stays a 403: a 401 is the page's own session, which the page answers by signing out.
		if be.SignIn != nil {
			return status, map[string]any{"error": be.Code, "message": be.Message, "signIn": be.SignIn}
		}
		return status, map[string]string{"error": be.Code, "message": be.Message}
	case errors.Is(err, apps.ErrNotInstalled):
		return http.StatusNotFound, map[string]string{"error": "not-found"}
	case errors.Is(err, apps.ErrUnknownMethod), errors.Is(err, apps.ErrStorageArgs):
		return http.StatusBadRequest, map[string]string{"error": apps.CodeInvalidParams}
	case errors.As(err, &conflict):
		return http.StatusConflict, map[string]any{"error": apps.CodeConflict, "message": "the key changed since it was read", "version": conflict.Current}
	case errors.Is(err, apps.ErrQuota):
		return http.StatusRequestEntityTooLarge, map[string]string{"error": apps.CodeQuotaExceeded, "message": "the storage area is full"}
	}
	agent.Log("warn", "apps: bridge: "+err.Error())
	return http.StatusInternalServerError, map[string]string{"error": "failed"}
}

func appBridgeRoutes(d deps, reg *apps.Registry, storage *apps.Storage, views *apps.ViewServer, chats appChats) []gate.Route {
	// session: the person's page, a POST with a JSON body.
	session := func(handle func(ctx context.Context, b bridgeBody) (int, any)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
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
			var b bridgeBody
			if !readSecretBody(r, &b) {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
				return
			}
			status, out := handle(r.Context(), b)
			secretJSON(w, status, out)
		}
	}
	refused := bridgeRefused

	// live is the session of a call and its app, read from the machine's own state.
	live := func(b bridgeBody) (apps.Session, apps.Installed, error) {
		sess, ok := views.Sessions.Get(b.Session)
		if !ok {
			return apps.Session{}, apps.Installed{}, &apps.BridgeError{Code: apps.CodeHostUnavailable, Message: "no such view session"}
		}
		// An agent's own widget has no package: no storage, tools or files of an app.
		if sess.App == apps.WidgetsApp {
			return apps.Session{}, apps.Installed{}, apps.ErrNotInstalled
		}
		app, err := reg.Get(sess.App)
		if err != nil {
			return apps.Session{}, apps.Installed{}, err
		}
		if app.Manifest == nil {
			return apps.Session{}, apps.Installed{}, apps.ErrNotInstalled
		}
		return sess, app, nil
	}

	// open issues the session of a view the page mounts: the app has to be installed and the view has to be one for the
	// target. The reply is the address to open on the view server.
	open := session(func(_ context.Context, b bridgeBody) (int, any) {
		// The agent's own widget: no package, the chat's widget names it, and only its files are served.
		if b.App == apps.WidgetsApp {
			if b.Target != apps.TargetChat || b.Conversation == "" || views.Widgets == nil {
				return refused(&apps.BridgeError{Code: apps.CodeNotInTarget, Message: "a widget is shown in a chat"})
			}
			wd, err := views.Widgets.Get(b.Conversation, b.View)
			if err != nil || wd.Kind != apps.WidgetHTML {
				return http.StatusNotFound, map[string]string{"error": "not-found"}
			}
			sess, err := views.Sessions.IssueIn(apps.WidgetsApp, wd.ID, apps.TargetChat, b.Conversation, "")
			if err != nil {
				return refused(err)
			}
			return http.StatusOK, map[string]any{"session": sess.Token, "nonce": sess.Nonce,
				"path": "/" + sess.Token + "/" + apps.WidgetsApp + "/" + wd.ID + "/index.html"}
		}
		app, err := reg.Get(b.App)
		if err != nil {
			return refused(err)
		}
		m := app.Manifest
		if apps.IsSlot(b.Target) && !uiSlotsOn() {
			return http.StatusForbidden, map[string]string{"error": "slots-off", "message": "the interface slots are off (HARNESSLY_UI_SLOTS)"}
		}
		if m == nil || !m.HasTarget(b.Target) {
			return refused(&apps.BridgeError{Code: apps.CodeNotInTarget, Message: "the app does not work in " + b.Target})
		}
		var inTarget bool
		for _, v := range m.Views {
			if v.ID == b.View {
				for _, t := range v.Targets {
					inTarget = inTarget || apps.TargetCovers(t, b.Target)
				}
			}
		}
		// A view the app's own server hosts gets a session too (the page holds it; the view never sees it), and its address
		// instead of a path on the view server.
		remote, isRemote := m.RemoteView(b.View)
		path, err := m.ViewPath(b.View)
		if isRemote {
			err = nil
		}
		if !inTarget || err != nil {
			return refused(&apps.BridgeError{Code: apps.CodeNotInTarget, Message: "no such view for " + b.Target})
		}
		if b.Conversation != "" && !apps.ValidOwner(b.Conversation) {
			return refused(apps.ErrStorageArgs)
		}
		// An app gets a chat only where the person turned it on there: the server is the check, not the page.
		if b.Conversation != "" {
			if rec, ok := chats.record(b.Conversation); !ok || !slices.Contains(rec.Apps, b.App) {
				return http.StatusForbidden, map[string]string{"error": "not-on", "message": "the app is not turned on in this chat"}
			}
		}
		// A view of a Sinopia target is shown in a document of that kind, and no other view is shown in any.
		if apps.IsSinopiaTarget(b.Target) {
			if err := viewDocumentKind(d, b.Target, b.Document); err != nil {
				return refused(err)
			}
		} else if b.Document != "" {
			return refused(apps.ErrStorageArgs)
		}
		sess, err := views.Sessions.IssueIn(b.App, b.View, b.Target, b.Conversation, b.Document)
		if err != nil {
			return refused(err)
		}
		if isRemote {
			return http.StatusOK, map[string]any{"session": sess.Token, "nonce": sess.Nonce, "url": remote}
		}
		return http.StatusOK, map[string]any{"session": sess.Token, "nonce": sess.Nonce, "path": "/" + sess.Token + path}
	})

	// renew keeps the session of a view that stays mounted: the same app, view, target and chat, only a later end.
	renew := session(func(_ context.Context, b bridgeBody) (int, any) {
		left, ok := views.Sessions.Renew(b.Session)
		if !ok {
			return refused(&apps.BridgeError{Code: apps.CodeHostUnavailable, Message: "no such view session"})
		}
		return http.StatusOK, map[string]any{"expiresIn": int(left.Seconds())}
	})

	store := session(func(_ context.Context, b bridgeBody) (int, any) {
		sess, app, err := live(b)
		if err != nil {
			return refused(err)
		}
		method := "harnessly/storage." + b.Op
		if err := app.Manifest.Authorize(method, sess.Target, map[string]any{"scope": b.Scope}); err != nil {
			return refused(err)
		}
		scope := apps.Scope(b.Scope)
		// The settings are written by the form, which checks them against the schema; a view reads them with hl.settings.
		if scope == apps.ScopeInstall && b.Key == apps.SettingsKey && (b.Op == "set" || b.Op == "delete") {
			return refused(apps.ErrStorageArgs)
		}
		// An area of a chat is the chat the view is mounted in; a view with no chat has none, and no view is bound to a
		// document yet.
		owner := ""
		switch scope {
		case apps.ScopeChat:
			if sess.Conversation == "" {
				return refused(apps.ErrStorageArgs)
			}
			owner = sess.Conversation
		case apps.ScopeDoc:
			if sess.Document == "" {
				return refused(&apps.BridgeError{Code: apps.CodeInvalidParams, Message: "this view is bound to no document"})
			}
			owner = sess.Document
		}
		switch b.Op {
		case "get":
			v, version, ok, err := storage.GetVersioned(sess.App, scope, owner, b.Key)
			if err != nil {
				return refused(err)
			}
			if !ok {
				return http.StatusOK, map[string]any{"value": nil, "version": version}
			}
			return http.StatusOK, map[string]any{"value": v, "version": version}
		case "set":
			if b.IfVersion != nil && *b.IfVersion < 0 {
				return refused(&apps.BridgeError{Code: apps.CodeInvalidParams, Message: "ifVersion is 0 or more"})
			}
			version, err := storage.SetIf(sess.App, scope, owner, b.Key, b.Value, b.IfVersion)
			if err != nil {
				return refused(err)
			}
			return http.StatusOK, map[string]any{"ok": true, "version": version}
		case "delete":
			if err := storage.Delete(sess.App, scope, owner, b.Key); err != nil {
				return refused(err)
			}
		case "keys":
			keys, err := storage.Keys(sess.App, scope, owner)
			if err != nil {
				return refused(err)
			}
			return http.StatusOK, map[string]any{"keys": keys}
		default:
			return refused(apps.ErrUnknownMethod)
		}
		return http.StatusOK, map[string]any{"ok": true}
	})

	tool := session(func(ctx context.Context, b bridgeBody) (int, any) {
		sess, app, err := live(b)
		if err != nil {
			return refused(err)
		}
		if err := app.Manifest.Authorize("tools/call", sess.Target, nil); err != nil {
			return refused(err)
		}
		result, err := callAppTool(ctx, d, reg, sess.App, b.Name, b.Args)
		if err != nil {
			return refused(err)
		}
		return http.StatusOK, map[string]any{"result": result}
	})

	// settings: hl.settings.get(), the values the person saved in the app's form over the schema's defaults.
	settings := session(func(_ context.Context, b bridgeBody) (int, any) {
		sess, app, err := live(b)
		if err != nil {
			return refused(err)
		}
		if err := app.Manifest.Authorize("harnessly/settings.get", sess.Target, nil); err != nil {
			return refused(err)
		}
		values, err := storage.Settings(app.Manifest, sess.App)
		if err != nil {
			return refused(err)
		}
		return http.StatusOK, map[string]any{"value": values}
	})

	watches := apps.NewFileWatches()
	// workspaceOf is the reach of a view's app into the workspace of its chat: the chat's own folder, read from the
	// machine's record of the chat, never from the call. A status other than 0 is the refusal to answer.
	workspaceOf := func(sess apps.Session, app apps.Installed) (apps.FileScope, int, any) {
		if sess.Conversation == "" {
			st, out := refused(&apps.BridgeError{Code: apps.CodeInvalidParams, Message: "this view is in no chat, so it has no workspace"})
			return apps.FileScope{}, st, out
		}
		rec, ok := chats.record(sess.Conversation)
		if !ok || !slices.Contains(rec.Apps, sess.App) {
			return apps.FileScope{}, http.StatusForbidden, map[string]string{"error": "not-on", "message": "the app is not turned on in this chat"}
		}
		protected, err := reg.ProtectedDirs()
		if err != nil {
			st, out := refused(err)
			return apps.FileScope{}, st, out
		}
		// The host's data folder, the registry and every installed package (any app's) are out of reach.
		return app.Manifest.FileScope(rec.Cwd, append(protected, filepath.Dir(reg.Root))...), 0, nil
	}

	// files: a file of the workspace of the chat the view is in. The workspace is the chat's own folder, read from the
	// machine's record of the chat, never from the call; the host's data folder (the registry, the cli-token) is out of
	// reach whatever the masks say.
	files := session(func(_ context.Context, b bridgeBody) (int, any) {
		sess, app, err := live(b)
		if err != nil {
			return refused(err)
		}
		method := "harnessly/files." + b.Op
		if !slices.Contains([]string{"read", "write", "list", "search", "watch", "unwatch"}, b.Op) {
			return refused(apps.ErrUnknownMethod)
		}
		if err := app.Manifest.Authorize(method, sess.Target, nil); err != nil {
			return refused(err)
		}
		if b.Op == "unwatch" {
			watches.Stop(b.Session)
			return http.StatusOK, map[string]any{"ok": true}
		}
		scope, status, out := workspaceOf(sess, app)
		if status != 0 {
			return status, out
		}
		switch b.Op {
		case "read":
			raw, err := scope.Read(b.Path)
			if err != nil {
				return refused(err)
			}
			if !utf8.Valid(raw) {
				return refused(&apps.BridgeError{Code: apps.CodeInvalidParams, Message: "the file is not text"})
			}
			return http.StatusOK, map[string]any{"text": string(raw)}
		case "list":
			list, err := scope.List(b.Path)
			if err != nil {
				return refused(err)
			}
			return http.StatusOK, map[string]any{"entries": list}
		case "search":
			hits, cut, err := scope.Search(b.Query, apps.SearchOptions{Glob: b.Glob, Case: b.Case, Limit: b.Limit})
			if err != nil {
				return refused(err)
			}
			return http.StatusOK, map[string]any{"hits": hits, "cut": cut}
		case "watch":
			token, appID, chat := b.Session, sess.App, sess.Conversation
			// The watch lives while its session does, the app stays on in the chat and keeps files.watch: turned off in
			// the chat, switched off or updated without the right, it stops at the next check.
			alive := func() bool {
				if _, ok := views.Sessions.Get(token); !ok {
					return false
				}
				if rec, ok := chats.record(chat); !ok || !slices.Contains(rec.Apps, appID) {
					return false
				}
				now, err := reg.Get(appID)
				return err == nil && now.Manifest != nil && now.Manifest.Allows("files.watch")
			}
			if err := watches.Start(token, scope, alive); err != nil {
				return refused(err)
			}
			return http.StatusOK, map[string]any{"ok": true}
		}
		if b.Text == nil {
			return refused(&apps.BridgeError{Code: apps.CodeInvalidParams, Message: "text is what to write"})
		}
		if err := scope.Write(b.Path, []byte(*b.Text)); err != nil {
			return refused(err)
		}
		return http.StatusOK, map[string]any{"ok": true}
	})

	// git: the repository of the chat's folder, read only (internal/apps gitread.go).
	git := session(func(ctx context.Context, b bridgeBody) (int, any) {
		sess, app, err := live(b)
		if err != nil {
			return refused(err)
		}
		if !slices.Contains([]string{"status", "diff", "log"}, b.Op) {
			return refused(apps.ErrUnknownMethod)
		}
		if err := app.Manifest.Authorize("harnessly/git."+b.Op, sess.Target, nil); err != nil {
			return refused(err)
		}
		scope, status, out := workspaceOf(sess, app)
		if status != 0 {
			return status, out
		}
		repo := apps.GitScope{Workspace: scope.Workspace, Forbidden: scope.Forbidden}
		switch b.Op {
		case "status":
			st, err := repo.Status(ctx)
			if err != nil {
				return refused(err)
			}
			return http.StatusOK, st
		case "diff":
			diff, cut, err := repo.Diff(ctx, apps.GitDiffOptions{Path: b.Path, Staged: b.Staged, Ref: b.Ref})
			if err != nil {
				return refused(err)
			}
			return http.StatusOK, map[string]any{"diff": diff, "cut": cut}
		}
		log, err := repo.Log(ctx, b.Limit, b.Path)
		if err != nil {
			return refused(err)
		}
		return http.StatusOK, map[string]any{"commits": log}
	})

	// run: the commands and the processes of an app in the chat's folder, inside the agent's wall (internal/apps run.go).
	// Every one is journaled: what ran, where, how it ended.
	procs := apps.NewProcesses()
	procs.OnEnd = func(app, chat string, p apps.Process) {
		code := -1
		if p.Code != nil {
			code = *p.Code
		}
		_ = reg.Journal(app, apps.JournalEntry{What: "process.end", Class: apps.ClassHigh, Chat: chat, OK: code == 0,
			Detail: p.ID + " " + strconv.Itoa(code) + ": " + p.Command})
	}
	run := session(func(ctx context.Context, b bridgeBody) (int, any) {
		sess, app, err := live(b)
		if err != nil {
			return refused(err)
		}
		method := map[string]string{"run": "harnessly/terminal.run", "start": "harnessly/process.start", "stop": "harnessly/process.stop",
			"list": "harnessly/process.list", "output": "harnessly/process.output", "preview": "harnessly/preview.open"}[b.Op]
		if method == "" {
			return refused(apps.ErrUnknownMethod)
		}
		if err := app.Manifest.Authorize(method, sess.Target, nil); err != nil {
			return refused(err)
		}
		scope, status, out := workspaceOf(sess, app)
		if status != 0 {
			return status, out
		}
		appID, chat := sess.App, sess.Conversation
		journal := func(what, detail string, ok bool) {
			_ = reg.Journal(appID, apps.JournalEntry{What: what, Class: apps.ClassHigh, Chat: chat, OK: ok, Detail: detail})
		}
		// The agent gets no more through an app than the person allowed: while it uses the app's view, what changes
		// something runs only with the person's yes, which the page asks in a dialog of its own.
		if b.Op != "list" && b.Op != "output" && !b.Confirmed && appPages.Driving(chat, appID) {
			journal(strings.TrimPrefix(method, "harnessly/"), "the agent: "+b.Command, false)
			return refused(&apps.BridgeError{Code: apps.CodePermissionDenied, Message: "the agent is using the app: this needs the person's yes"})
		}
		switch b.Op {
		case "list":
			return http.StatusOK, map[string]any{"processes": procs.List(appID, chat)}
		case "output":
			got, err := procs.Output(appID, chat, b.ID, b.Since)
			if err != nil {
				return refused(err)
			}
			return http.StatusOK, got
		case "stop":
			got, err := procs.Stop(appID, chat, b.ID)
			if err != nil {
				return refused(err)
			}
			journal("process.stop", got.ID+": "+got.Command, true)
			return http.StatusOK, got
		case "preview":
			if b.Port < 1024 || b.Port > 65535 {
				return refused(&apps.BridgeError{Code: apps.CodeInvalidParams, Message: "port is a local port from 1024 to 65535"})
			}
			journal("preview.open", "localhost:"+strconv.Itoa(b.Port), true)
			return http.StatusOK, map[string]any{"ok": true}
		}
		// A command and a process are each held to their own right: alone any command, with masks only theirs.
		right := "terminal.run"
		if b.Op == "start" {
			right = "process"
		}
		argv, err := app.Manifest.RunRightsOf(right).Argv(b.Command)
		if err != nil {
			journal(strings.TrimPrefix(method, "harnessly/"), b.Command, false)
			return refused(err)
		}
		dir, err := apps.RunDir(scope.Workspace, b.Cwd)
		if err != nil {
			return refused(err)
		}
		box, err := reg.Sandbox(appBoundary())
		if err != nil {
			return refused(err)
		}
		if b.Op == "start" {
			alive := func() bool {
				if rec, ok := chats.record(chat); !ok || !slices.Contains(rec.Apps, appID) {
					return false
				}
				now, err := reg.Get(appID)
				return err == nil && now.Manifest != nil && now.Manifest.Allows("process")
			}
			got, err := procs.Start(box, appID, chat, dir, b.Command, argv, alive)
			journal("process.start", b.Command, err == nil)
			if err != nil {
				return refused(err)
			}
			return http.StatusOK, got
		}
		res, err := box.Run(ctx, dir, argv, time.Duration(b.Timeout)*time.Second)
		if err != nil {
			journal("terminal.run", b.Command, false)
			return refused(err)
		}
		journal("terminal.run", strconv.Itoa(res.Code)+": "+b.Command, res.Code == 0)
		return http.StatusOK, res
	})

	// fileEvents: which files changed for the views that watch, for the person's page to hand to each view by its
	// session (app-views/file-events.ts). Names only, never what is in the files.
	fileEvents := func(w http.ResponseWriter, r *http.Request) {
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		if r.Method != http.MethodGet {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
			return
		}
		changes := make(chan apps.FileChange, 64)
		stop := watches.OnChange(func(c apps.FileChange) {
			select {
			case changes <- c:
			default:
			}
		})
		defer stop()
		h := w.Header()
		h.Set("Content-Type", "text/event-stream; charset=utf-8")
		h.Set("Cache-Control", "no-store, no-transform")
		h.Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(": ok\n\n"))
		flusher.Flush()
		beat := time.NewTicker(15 * time.Second)
		defer beat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case c := <-changes:
				raw, _ := json.Marshal(c)
				_, _ = w.Write(append(append([]byte("data: "), raw...), '\n', '\n'))
				flusher.Flush()
			case <-beat.C:
				_, _ = w.Write([]byte(": ping\n\n"))
				flusher.Flush()
			}
		}
	}

	// events: the changes of the app storage as they happen, for the person's page to tell the views that are open. A
	// change carries where it happened (app, scope, the chat or document of the area, key) and never the value; the
	// page hands it only to the views of that app, and an area per chat or document only to the views of that chat or
	// document (app-views/storage-events.ts).
	events := func(w http.ResponseWriter, r *http.Request) {
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		if r.Method != http.MethodGet {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
			return
		}
		// A page too slow to keep up loses changes rather than holding the writers up.
		changes := make(chan apps.Change, 256)
		stop := storage.OnChange(func(c apps.Change) {
			select {
			case changes <- c:
			default:
			}
		})
		defer stop()
		h := w.Header()
		h.Set("Content-Type", "text/event-stream; charset=utf-8")
		h.Set("Cache-Control", "no-store, no-transform")
		h.Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(": ok\n\n"))
		flusher.Flush()
		beat := time.NewTicker(15 * time.Second)
		defer beat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case c := <-changes:
				raw, _ := json.Marshal(map[string]string{"app": c.App, "scope": string(c.Scope), "owner": c.Owner, "key": c.Key})
				_, _ = w.Write(append(append([]byte("data: "), raw...), '\n', '\n'))
				flusher.Flush()
			case <-beat.C:
				_, _ = w.Write([]byte(": ping\n\n"))
				flusher.Flush()
			}
		}
	}

	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	routes := append(appTerminalRoutes(d, reg, chats, live, workspaceOf), appBrowserRoutes(d, reg, live, workspaceOf)...)
	routes = append(routes, appAIRoutes(d, reg, chats, live)...)
	routes = append(routes, appSecretRoutes(d, reg, chats, live)...)
	return append(routes, []gate.Route{
		{Match: is(platformAPI + "/apps/files"), Handler: files},
		{Match: is("/agents" + platformAPI + "/apps/files"), Handler: files},
		{Match: is(platformAPI + "/apps/git"), Handler: git},
		{Match: is("/agents" + platformAPI + "/apps/git"), Handler: git},
		{Match: is(platformAPI + "/apps/run"), Handler: run},
		{Match: is("/agents" + platformAPI + "/apps/run"), Handler: run},
		{Match: is(platformAPI + "/apps/file-events"), Handler: http.HandlerFunc(fileEvents)},
		{Match: is("/agents" + platformAPI + "/apps/file-events"), Handler: http.HandlerFunc(fileEvents)},
		{Match: is(platformAPI + "/apps/session"), Handler: open},
		{Match: is("/agents" + platformAPI + "/apps/session"), Handler: open},
		{Match: is(platformAPI + "/apps/session/renew"), Handler: renew},
		{Match: is("/agents" + platformAPI + "/apps/session/renew"), Handler: renew},
		{Match: is(platformAPI + "/apps/settings"), Handler: settings},
		{Match: is("/agents" + platformAPI + "/apps/settings"), Handler: settings},
		{Match: is(platformAPI + "/apps/storage"), Handler: store},
		{Match: is(platformAPI + "/apps/events"), Handler: http.HandlerFunc(events)},
		{Match: is(platformAPI + "/apps/tool"), Handler: tool},
		{Match: is("/agents" + platformAPI + "/apps/storage"), Handler: store},
		{Match: is("/agents" + platformAPI + "/apps/tool"), Handler: tool},
	}...)
}

// revealApp is the values of an app's secrets by their names; one with no value is left out.
func revealApp(store *secrets.Store, app string, names []string) map[string]string {
	out := map[string]string{}
	for _, n := range names {
		if v, err := store.RevealApp(app, []string{n}); err == nil {
			out[n] = v[n]
		}
	}
	return out
}

// callAppTool runs a tool (<server>.<tool>) of an app's own MCP server from the machine: a server through the person's
// connection with that connection's grant, any other with the app's secrets the machine fills into its headers (the
// page never holds one). A server whose secret has no value is not called: the app waits for it.
func callAppTool(ctx context.Context, d deps, reg *apps.Registry, appID, tool string, args map[string]any) (json.RawMessage, error) {
	out, err := callAppToolRaw(ctx, d, reg, appID, tool, args)
	if err != nil {
		// The author reads a backend's refusal in `harnessly app logs`: the tool, the code and the short reason,
		// never the arguments or the answer's body.
		reg.LogEventOnce(appID, "", apps.ViewLogMCP, "tools.call "+clip(tool, 80)+" — "+toolFailure(err))
	}
	return out, err
}

// toolFailure is the code and the short detail of a failed backend call, in one line.
func toolFailure(err error) string {
	var be *apps.BridgeError
	if errors.As(err, &be) {
		return clip(strings.TrimSpace(be.Code+": "+be.Message+" "+be.Detail), 300)
	}
	return clip(err.Error(), 300)
}

func callAppToolRaw(ctx context.Context, d deps, reg *apps.Registry, appID, tool string, args map[string]any) (json.RawMessage, error) {
	pkg, err := reg.Package(appID)
	if err != nil {
		return nil, err
	}
	server, name, err := pkg.CanCallTool(tool)
	if err != nil {
		return nil, err
	}
	if decl := pkg.MCP[server]; decl.Connection != "" {
		return callConnectionTool(ctx, mcpFor(d), reg.ConnectionGranted(appID, decl.Connection), decl, name, args)
	}
	decl := pkg.MCP[server]
	srv, missing := decl.Filled(revealApp(safeFor(d).store.Local, appID, decl.Secrets()))
	if len(missing) > 0 {
		return nil, &apps.BridgeError{Code: apps.CodeHostUnavailable, Message: apps.ToolRefused, Detail: "the app waits for a secret"}
	}
	if decl.Auth != nil && decl.Auth.Type == apps.AuthOAuth {
		return callAppOAuthTool(ctx, mcpFor(d), appID, server, decl, srv, name, args)
	}
	return callWithAppToken(ctx, appTokensOf(d), appID, server, decl, srv, name, args)
}
