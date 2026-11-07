package main

import (
	"net/http"
	"slices"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

// An app's view puts a value into the safe and never reads it back:
//
//	POST <platform>/apps/secrets  {name, value}               secrets.write: the app's own secret (app/<id>/<name>),
//	                                                          one of those it holds secrets:<name> for → {created}
//	POST <platform>/apps/secrets  {name, value, shared: true} secrets.write:<name>: the person's secret of that name,
//	                                                          the one the agent uses; while the agent uses the view only
//	                                                          with the person's yes → {created}
//
// A shared secret keeps its limits: one other projects alone may use is not replaced from this chat's, and a new one is
// for this chat's project only. The agent's waiting cards are the person's to answer: a put fulfils none of them.
//
// Neither the value nor anything of it reaches the journal or the log: only the name and whose it is.
func appSecretRoutes(d deps, reg *apps.Registry, chats appChats, live appLive) []gate.Route {
	put := func(w http.ResponseWriter, r *http.Request) {
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
			Secret string `json:"value"`
			Shared bool   `json:"shared"`
		}
		if !readSecretBody(r, &b) {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		refuse := func(err error) {
			st, out := bridgeRefused(err)
			secretJSON(w, st, out)
		}
		sess, app, err := live(b.bridgeBody)
		if err != nil {
			refuse(err)
			return
		}
		if err := app.Manifest.Authorize("harnessly/secrets.put", sess.Target, nil); err != nil {
			refuse(err)
			return
		}
		m := app.Manifest
		class, whose := apps.ClassMedium, "own"
		if b.Shared {
			class, whose = apps.ClassHigh, "shared"
		}
		journal := func(ok bool) {
			_ = reg.Journal(sess.App, apps.JournalEntry{What: "secrets.put", Class: class, Chat: sess.Conversation, OK: ok, Detail: whose + " " + clipDetail(b.Name)})
		}
		switch {
		case !b.Shared && !slices.Contains(m.Permissions, "secrets.write"):
			journal(false)
			refuse(&apps.BridgeError{Code: apps.CodePermissionDenied, Message: "the app's own secrets need the permission secrets.write"})
			return
		case !b.Shared && !m.Allows("secrets:"+b.Name):
			journal(false)
			refuse(&apps.BridgeError{Code: apps.CodePermissionDenied, Message: "the app's own secret is one it holds secrets:<name> for: " + clipDetail(b.Name) + " is not"})
			return
		case b.Shared && !slices.Contains(m.Permissions, "secrets.write:"+b.Name):
			journal(false)
			refuse(&apps.BridgeError{Code: apps.CodePermissionDenied, Message: "the person's secret " + clipDetail(b.Name) + " needs the permission secrets.write:" + clipDetail(b.Name)})
			return
		case b.Shared && !b.Confirmed && appPages.Driving(sess.Conversation, sess.App):
			journal(false)
			refuse(&apps.BridgeError{Code: apps.CodePermissionDenied, Message: "the agent is using the app: this needs the person's yes"})
			return
		}
		sf := safeFor(d)
		var created bool
		if b.Shared {
			rec, ok := chats.record(sess.Conversation)
			if !ok {
				journal(false)
				refuse(&apps.BridgeError{Code: apps.CodeHostUnavailable, Message: "the chat is not on this machine"})
				return
			}
			ask := secrets.Ask{Name: b.Name}
			if rec.ProjectID != nil {
				ask.Project = *rec.ProjectID
			}
			_, created, err = sf.store.PutAsked(ask, b.Secret)
			if secrets.ReasonOf(err) == secrets.ReasonNotFound {
				journal(false)
				refuse(&apps.BridgeError{Code: apps.CodePermissionDenied, Message: "the secret " + clipDetail(b.Name) + " is for other projects than this chat's"})
				return
			}
		} else {
			created = !sf.store.Local.HasApp(sess.App, b.Name)
			err = sf.store.Local.PutApp(sess.App, b.Name, b.Secret) // an app's secrets stay in the file
		}
		journal(err == nil)
		if err != nil {
			status := http.StatusInternalServerError
			switch secrets.ReasonOf(err) {
			case secrets.ReasonEmpty, secrets.ReasonTooLarge, secrets.ReasonBadName, secrets.ReasonReadOnly:
				status = http.StatusBadRequest
			}
			secretJSON(w, status, map[string]string{"error": apps.CodeInvalidParams, "message": "the safe did not take it: " + secrets.ReasonOf(err)})
			return
		}
		agent.Log("info", "apps: secret put", agent.F{K: "app", V: sess.App}, agent.F{K: "name", V: b.Name}, agent.F{K: "whose", V: whose})
		secretJSON(w, http.StatusOK, map[string]bool{"created": created})
	}
	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	return []gate.Route{
		{Match: is(platformAPI + "/apps/secrets"), Handler: http.HandlerFunc(put)},
		{Match: is("/agents" + platformAPI + "/apps/secrets"), Handler: http.HandlerFunc(put)},
	}
}
