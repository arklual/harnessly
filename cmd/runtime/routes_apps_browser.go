package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	browsertools "github.com/ly-solutions/harnessly/internal/agentbrowser/tools"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The agent's browser of the chat through an app: browser.read looks, browser.drive acts. Both go through
// the agent's own browser tools for the chat (internal/agentbrowser/tools), so a tab the person holds stays theirs and
// a page's text comes marked untrusted. What reaches a password, a local file or a script is not given to an app.

// browserTools are the agent's tools an app may call, by the right that allows each.
var browserTools = map[string]map[string]string{
	"browser.read": {
		"tabs": "chrome_tabs", "snapshot": "chrome_snapshot", "screenshot": "chrome_screenshot", "wait_for": "chrome_wait_for",
	},
	"browser.drive": {
		"open": "chrome_open", "navigate": "chrome_navigate", "close_tab": "chrome_close_tab", "resize": "chrome_resize",
		"click": "chrome_click", "hover": "chrome_hover", "type": "chrome_type", "press_key": "chrome_press_key",
		"select": "chrome_select", "dialog": "chrome_dialog",
	},
}

// browserJournaled are the arguments a journal line keeps: where it went, never what was typed.
var browserJournaled = []string{"url", "tab", "ref", "key"}

// maxBrowserTime bounds one call of an app.
const maxBrowserTime = 60 * time.Second

func appBrowserRoutes(d deps, reg *apps.Registry, live appLive, workspaceOf appWorkspace) []gate.Route {
	call := func(w http.ResponseWriter, r *http.Request) {
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
			Right string          `json:"right"`
			Tool  string          `json:"tool"`
			Args  json.RawMessage `json:"args"`
		}
		refuse := func(err error) {
			st, out := bridgeRefused(err)
			secretJSON(w, st, out)
		}
		if !readSecretBody(r, &b) {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		tools, ok := browserTools[b.Right]
		if !ok {
			refuse(apps.ErrUnknownMethod)
			return
		}
		sess, app, err := live(b.bridgeBody)
		if err != nil {
			refuse(err)
			return
		}
		if err := app.Manifest.Authorize("harnessly/"+b.Right, sess.Target, nil); err != nil {
			refuse(err)
			return
		}
		if _, status, out := workspaceOf(sess, app); status != 0 {
			secretJSON(w, status, out)
			return
		}
		name, ok := tools[b.Tool]
		if !ok {
			refuse(&apps.BridgeError{Code: apps.CodeInvalidParams, Message: "no such tool for " + b.Right})
			return
		}
		args := jsvalue.NewObject()
		if len(b.Args) > 0 && string(b.Args) != "null" {
			v, err := jsvalue.Parse(b.Args)
			o, isObject := v.(*jsvalue.Object)
			if err != nil || !isObject {
				refuse(&apps.BridgeError{Code: apps.CodeInvalidParams, Message: "args is an object"})
				return
			}
			args = o
		}
		detail := b.Tool
		for _, k := range browserJournaled {
			if v := args.String(k); v != "" {
				detail += " " + k + "=" + clipDetail(v)
			}
		}
		journal := func(ok bool) {
			_ = reg.Journal(sess.App, apps.JournalEntry{What: b.Right, Class: apps.ClassHigh, Chat: sess.Conversation, OK: ok, Detail: detail})
		}
		// The agent gets no more through an app than the person allowed: while it uses the app's view, the browser is
		// reached only with the person's yes, asked by the page.
		if !b.Confirmed && appPages.Driving(sess.Conversation, sess.App) {
			journal(false)
			refuse(&apps.BridgeError{Code: apps.CodePermissionDenied, Message: "the agent is using the app: this needs the person's yes"})
			return
		}
		if d.BrowserTools == nil || d.BrowserTools.Manager == nil || !d.BrowserTools.Manager.Enabled() {
			refuse(&apps.BridgeError{Code: apps.CodeHostUnavailable, Message: "this machine has no agent browser"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), maxBrowserTime)
		defer cancel()
		out := browsertools.Tools{S: d.BrowserTools, Caller: sess.Conversation}.Call(ctx, name, args)
		v, _ := out.Get("isError")
		failed, _ := v.(bool)
		journal(!failed)
		secretJSON(w, http.StatusOK, map[string]any{"content": out.Array("content"), "isError": failed})
	}
	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	return []gate.Route{
		{Match: is(platformAPI + "/apps/browser"), Handler: http.HandlerFunc(call)},
		{Match: is("/agents" + platformAPI + "/apps/browser"), Handler: http.HandlerFunc(call)},
	}
}

// clipDetail keeps a journal line short.
func clipDetail(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
