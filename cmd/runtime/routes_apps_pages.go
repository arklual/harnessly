package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// appPages are the tools and the context the views of a chat's apps offer from the page (internal/apps/pagetools.go).
var appPages = apps.NewPageTools()

// The page's side of the page tools; the person's page session only:
//
//	POST <platform>/apps/page-tools          {conversation, apps: [{app, tools: [{name, description, inputSchema}], context}]}
//	                                         what the chat's open views offer now; the page renews it every few seconds
//	GET  <platform>/apps/page-calls?conversation=<id>   the agent's calls nobody took yet: {calls: [{id, app, tool, arguments}]}
//	POST <platform>/apps/page-calls/answer   {conversation, id, result | error}: the view's answer
//
// Only an app that is on in the chat is heard, and of it only what its rights open: tools with tools.page, the context
// with context.publish.
func appPageRoutes(d deps, reg *apps.Registry, pages *apps.PageTools, chats appChats) []gate.Route {
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
	read := func(r *http.Request, out any) bool {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 2<<20+1))
		return err == nil && len(raw) <= 2<<20 && json.Unmarshal(raw, out) == nil
	}
	bad := func(w http.ResponseWriter) {
		secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
	}
	report := func(w http.ResponseWriter, r *http.Request) {
		if !guard(http.MethodPost, w, r) {
			return
		}
		var b struct {
			Conversation string            `json:"conversation"`
			Apps         []apps.PageReport `json:"apps"`
		}
		if !read(r, &b) || !apps.ValidOwner(b.Conversation) || len(b.Apps) > 16 {
			bad(w)
			return
		}
		rec, ok := chats.record(b.Conversation)
		if !ok {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "no-chat"})
			return
		}
		var kept []apps.PageReport
		for _, a := range b.Apps {
			if !slices.Contains(rec.Apps, a.App) {
				continue
			}
			in, err := reg.Get(a.App)
			if err != nil || in.Manifest == nil {
				continue
			}
			kept = append(kept, apps.PageReportAllowed(in.Manifest, a))
		}
		if err := pages.Report(b.Conversation, kept); err != nil {
			bad(w)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"kept": len(kept)})
	}
	take := func(w http.ResponseWriter, r *http.Request) {
		if !guard(http.MethodGet, w, r) {
			return
		}
		conv := r.URL.Query().Get("conversation")
		if !apps.ValidOwner(conv) {
			bad(w)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"calls": pages.Take(conv)})
	}
	answer := func(w http.ResponseWriter, r *http.Request) {
		if !guard(http.MethodPost, w, r) {
			return
		}
		var b struct {
			Conversation string          `json:"conversation"`
			ID           string          `json:"id"`
			Result       json.RawMessage `json:"result"`
			Error        string          `json:"error"`
		}
		if !read(r, &b) || b.ID == "" || !apps.ValidOwner(b.Conversation) {
			bad(w)
			return
		}
		switch err := pages.Answer(b.Conversation, b.ID, apps.PageAnswer{Result: b.Result, Error: b.Error}); {
		case errors.Is(err, apps.ErrPageCallGone):
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
		case err != nil:
			bad(w)
		default:
			secretJSON(w, http.StatusOK, map[string]string{})
		}
	}
	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	var out []gate.Route
	for _, prefix := range []string{"", "/agents"} {
		out = append(out,
			gate.Route{Match: is(prefix + platformAPI + "/apps/page-tools"), Handler: http.HandlerFunc(report)},
			gate.Route{Match: is(prefix + platformAPI + "/apps/page-calls"), Handler: http.HandlerFunc(take)},
			gate.Route{Match: is(prefix + platformAPI + "/apps/page-calls/answer"), Handler: http.HandlerFunc(answer)},
		)
	}
	return out
}
