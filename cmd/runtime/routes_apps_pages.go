package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

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
//	GET  …/apps/page-calls?conversation=<id>&page=<page>&ack=1   the same for a page that acknowledges: also the calls
//	                                         it took and did not acknowledge in time; the answer says "ack": true
//	POST <platform>/apps/page-calls/ack      {conversation, id, page}: the page has the call; {status, dead}
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
	opens := apps.OpenWindowOpens(d.DataDir)
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
		names := map[string]string{}
		for _, a := range b.Apps {
			m := apps.PageApp(a.App, rec.Apps, reg)
			if m == nil {
				continue
			}
			kept = append(kept, apps.PageReportAllowed(m, a))
			// An app that works in the background has its view up without the person opening it: no event for it.
			if !m.Allows("background") {
				names[a.App] = m.Name
			}
		}
		if err := pages.Report(b.Conversation, kept); err != nil {
			bad(w)
			return
		}
		// The first report of an app in the chat: its window is up, and the agent at rest is given a turn (v2_appopen.go).
		// The send waits for the chat's actor, so the page's report does not wait for it.
		for _, a := range kept {
			name, greets := names[a.App]
			if !greets {
				continue
			}
			if cmd, ok := appOpens.opened(opens, rec, name, a); ok {
				go appOpens.send(chats, cmd)
			}
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
		// A page that acknowledges what it takes says so (ack=1) and names itself (page): it is given a command again
		// when its acknowledgement did not come. An older page takes each command once, as before. "ack": true tells
		// the page this runtime hears acknowledgements; a page that does not see it works the old way.
		q := r.URL.Query()
		page := q.Get("page")
		if len(page) > 64 {
			bad(w)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"calls": pages.TakeBy(conv, page, q.Get("ack") == "1" && page != ""), "ack": true})
	}
	// ack: the page has the command and is about to run it. {status, dead}: dead means the page must not run it: the
	// command is over (its term ran out, the agent was told so), or nobody waits for its answer now.
	ack := func(w http.ResponseWriter, r *http.Request) {
		if !guard(http.MethodPost, w, r) {
			return
		}
		var b struct {
			Conversation string `json:"conversation"`
			ID           string `json:"id"`
			Page         string `json:"page"`
		}
		if !read(r, &b) || b.ID == "" || len(b.Page) > 64 || !apps.ValidOwner(b.Conversation) {
			bad(w)
			return
		}
		status, dead, err := pages.Ack(b.Conversation, b.ID, b.Page)
		if err != nil {
			// A command this runtime does not know is over for the page all the same.
			secretJSON(w, http.StatusOK, map[string]any{"status": "unknown", "dead": true})
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"status": status, "dead": dead})
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
		case errors.Is(err, apps.ErrPageCallDead):
			// The command ended before the answer came (its term ran out): the answer is dropped.
			secretJSON(w, http.StatusConflict, map[string]string{"error": "dead"})
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
			gate.Route{Match: is(prefix + platformAPI + "/apps/page-calls/ack"), Handler: http.HandlerFunc(ack)},
		)
	}
	return out
}
