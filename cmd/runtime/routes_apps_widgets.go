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

// The widgets of a chat (internal/apps/widgets.go): what an agent shows in the feed, kept on the machine.
//
//	GET  <platform>/apps/widgets?conversation=<id>   the person's page: {widgets: [...]}, oldest first
//	POST /api/cli/apps/widgets                       `harnessly widget show|render`: {session, kind, app, view, id,
//	                                                 title, data, html, css, script} → {id, rev}
//	GET  /api/cli/apps/widgets?session=<tmux>        `harnessly widget list`
//	POST /api/cli/apps/widgets/remove                `harnessly widget remove`: {session, id}
//
// An app's widget is shown only where the app is on in the chat, and only a view of the app for the chat. The agent's
// own widget (kind html) needs no app: it runs in the views' sandbox with no network, so any chat may draw one.

// widgetBodyMax bounds a CLI call: the files of an html widget and its data, with room for JSON's quoting.
const widgetBodyMax = 1 << 20

type widgetBody struct {
	Session string          `json:"session"`
	Kind    string          `json:"kind"`
	App     string          `json:"app"`
	View    string          `json:"view"`
	ID      string          `json:"id"`
	Title   string          `json:"title"`
	Data    json.RawMessage `json:"data"`
	HTML    string          `json:"html"`
	CSS     string          `json:"css"`
	Script  string          `json:"script"`
}

func widgetRefusal(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, apps.ErrWidgetArgs):
		secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-widget"})
	case errors.Is(err, apps.ErrWidgetsFull):
		secretJSON(w, http.StatusConflict, map[string]string{"error": "too-many-widgets"})
	case errors.Is(err, apps.ErrWidgetKind):
		secretJSON(w, http.StatusConflict, map[string]string{"error": "another-widget"})
	case errors.Is(err, apps.ErrWidgetGone):
		secretJSON(w, http.StatusNotFound, map[string]string{"error": "no-widget"})
	default:
		secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
	}
}

func appWidgetRoutes(d deps, reg *apps.Registry, widgets *apps.Widgets, chats appChats) []gate.Route {
	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	cli := func(w http.ResponseWriter, r *http.Request, method string) bool {
		if d.CLI == nil || !d.CLI.OK(r) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return false
		}
		if r.Method != method {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return false
		}
		return true
	}
	chatOf := func(w http.ResponseWriter, session string) string {
		conv := chats.ofSession(session)
		if conv == "" {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "no-chat"})
		}
		return conv
	}

	page := func(w http.ResponseWriter, r *http.Request) {
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
		conv := r.URL.Query().Get("conversation")
		if !apps.ValidOwner(conv) {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		list, err := widgets.List(conv)
		if err != nil {
			widgetRefusal(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"widgets": list})
	}

	show := func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if !cli(w, r, http.MethodGet) {
				return
			}
			conv := chatOf(w, r.URL.Query().Get("session"))
			if conv == "" {
				return
			}
			list, err := widgets.List(conv)
			if err != nil {
				widgetRefusal(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]any{"conversation": conv, "widgets": list})
			return
		}
		if !cli(w, r, http.MethodPost) {
			return
		}
		var b widgetBody
		raw, err := io.ReadAll(io.LimitReader(r.Body, widgetBodyMax+1))
		if err != nil || len(raw) > widgetBodyMax || json.Unmarshal(raw, &b) != nil {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		conv := chatOf(w, b.Session)
		if conv == "" {
			return
		}
		in := apps.WidgetInput{ID: b.ID, Kind: b.Kind, Title: b.Title, Data: b.Data}
		switch b.Kind {
		case apps.WidgetHTML:
			in.App, in.HTML, in.Style, in.Script = apps.WidgetsApp, b.HTML, b.CSS, b.Script
		case apps.WidgetView:
			rec, ok := chats.record(conv)
			if !ok || !slices.Contains(rec.Apps, b.App) {
				secretJSON(w, http.StatusForbidden, map[string]string{"error": "not-on", "message": "the app is not turned on in this chat"})
				return
			}
			app, err := reg.Get(b.App)
			if err != nil || app.Manifest == nil {
				secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
				return
			}
			found := false
			for _, v := range app.Manifest.Views {
				found = found || (v.ID == b.View && slices.Contains(v.Targets, apps.TargetChat))
			}
			if !found {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "no-view", "message": "the app has no such view for the chat"})
				return
			}
			in.App, in.View = b.App, b.View
		default:
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-widget"})
			return
		}
		made, err := widgets.Show(conv, in)
		if err != nil {
			widgetRefusal(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"conversation": conv, "id": made.ID, "rev": made.Rev})
	}

	remove := func(w http.ResponseWriter, r *http.Request) {
		if !cli(w, r, http.MethodPost) {
			return
		}
		var b widgetBody
		if !readSecretBody(r, &b) {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		conv := chatOf(w, b.Session)
		if conv == "" {
			return
		}
		if err := widgets.Remove(conv, b.ID); err != nil {
			widgetRefusal(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"removed": b.ID})
	}

	return []gate.Route{
		{Match: is(platformAPI + "/apps/widgets"), Handler: http.HandlerFunc(page)},
		{Match: is("/agents" + platformAPI + "/apps/widgets"), Handler: http.HandlerFunc(page)},
		{Match: is("/api/cli/apps/widgets"), Handler: http.HandlerFunc(show)},
		{Match: is("/api/cli/apps/widgets/remove"), Handler: http.HandlerFunc(remove)},
	}
}
