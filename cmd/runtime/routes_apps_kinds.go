package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The document types apps add to Sinopia (internal/apps/kinds.go). The person's page only.
//
//	GET  <platform>/apps/kinds          the types of the apps that are on: the list of new documents
//	GET  <platform>/apps/kinds/docs     the documents of those types the person may see: for the library's list
//	POST <platform>/apps/kinds/create   a new document of a type: the host makes the project, the app's editor fills it
//	POST <platform>/apps/kinds/open     what to open a document with: its type, and the editor, or that the app is missing;
//	                                    a document of an older format is migrated first, or comes back read-only with the reason
//	POST <platform>/apps/kinds/revert   puts a migrated document back to the version it was
//
// The host owns the project (folder, history, rights, links); the app's editor view is shown with target sinopia.<type>
// and the document, and reaches it only through the SDK.
func kindRoutes(d deps, reg *apps.Registry) []gate.Route {
	storage := apps.OpenStorage(d.DataDir)
	page := func(method string, handle func(ctx context.Context, b kindBody) (int, any)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if sameorigin.Refuse(w, r) {
				return
			}
			if !d.SessionOK(r) {
				secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
				return
			}
			if r.Method != method {
				secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
				return
			}
			var b kindBody
			if method == http.MethodPost && !readSecretBody(r, &b) {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
				return
			}
			status, out := handle(design.WithPerson(r.Context(), designAPI(d).PersonOf(r)), b)
			secretJSON(w, status, out)
		}
	}
	notFound := map[string]string{"error": "not-found"}

	list := page(http.MethodGet, func(context.Context, kindBody) (int, any) {
		kinds, err := reg.Kinds()
		if err != nil {
			agent.Log("warn", "apps: kinds: "+err.Error())
			return http.StatusInternalServerError, map[string]string{"error": "failed"}
		}
		return http.StatusOK, map[string]any{"kinds": kinds}
	})

	// docs are the documents of the apps' types that the person may see, for the library's list beside the others.
	docs := page(http.MethodGet, func(ctx context.Context, _ kindBody) (int, any) {
		type doc struct {
			Document  string `json:"document"`
			Title     string `json:"title"`
			UpdatedAt int64  `json:"updatedAt"`
			Kind      string `json:"kind"`
			KindTitle string `json:"kindTitle"`
			NeedsApp  bool   `json:"needsApp"`
		}
		kinds, err := reg.Kinds()
		if err != nil {
			agent.Log("warn", "apps: kinds: "+err.Error())
			return http.StatusInternalServerError, map[string]string{"error": "failed"}
		}
		titles := map[string]string{}
		for _, k := range kinds {
			titles[k.ID] = k.Title
		}
		lib := designAPI(d).L
		have := reg.DocKinds()
		out := []doc{}
		for _, a := range lib.VisibleToFor(lib.ListArtifacts(), design.PersonIn(ctx)) {
			kind, ok := have[a.ID]
			if !ok {
				continue
			}
			title, installed := titles[kind]
			if !installed {
				title = kind
			}
			out = append(out, doc{Document: a.ID, Title: a.Title, UpdatedAt: a.UpdatedAt, Kind: kind, KindTitle: title, NeedsApp: !installed})
		}
		return http.StatusOK, map[string]any{"docs": out}
	})

	create := page(http.MethodPost, func(ctx context.Context, b kindBody) (int, any) {
		info, _, err := reg.Kind(b.Kind)
		if errors.Is(err, apps.ErrNotInstalled) {
			return http.StatusNotFound, notFound
		}
		if err != nil {
			agent.Log("warn", "apps: kinds: "+err.Error())
			return http.StatusInternalServerError, map[string]string{"error": "failed"}
		}
		lib := designAPI(d).L
		title := strings.TrimSpace(b.Title)
		if title == "" {
			title = info.Title
		}
		project, err := lib.CreateProject(title)
		var ce *design.CreateError
		switch {
		case errors.As(err, &ce) && ce.Conflict:
			return http.StatusConflict, map[string]string{"error": "conflict", "message": ce.Msg}
		case errors.As(err, &ce):
			return http.StatusBadRequest, map[string]string{"error": "bad-request", "message": ce.Msg}
		case err != nil:
			agent.Log("warn", "apps: kinds: create: "+err.Error())
			return http.StatusInternalServerError, map[string]string{"error": "failed"}
		}
		lib.AnnounceNewBy(project.ID, design.PersonIn(ctx))
		if err := reg.SetDocKind(project.ID, info.ID); err != nil {
			agent.Log("warn", "apps: kinds: record: "+err.Error())
			return http.StatusInternalServerError, map[string]string{"error": "failed"}
		}
		if err := apps.RecordDocVersion(storage, info.App, project.ID, info.SchemaVersion); err != nil {
			agent.Log("warn", "apps: kinds: version: "+err.Error())
			return http.StatusInternalServerError, map[string]string{"error": "failed"}
		}
		return http.StatusOK, map[string]any{"document": project.ID, "title": project.Title, "kind": info}
	})

	open := page(http.MethodPost, func(ctx context.Context, b kindBody) (int, any) {
		if !design.IsLibraryID(b.Document) {
			return http.StatusBadRequest, map[string]string{"error": "bad-request"}
		}
		kind, ok := reg.DocKind(b.Document)
		if !ok {
			return http.StatusNotFound, notFound
		}
		info, k, err := reg.Kind(kind)
		if errors.Is(err, apps.ErrNotInstalled) {
			// The document stays readable; the app that edits it is not on here.
			return http.StatusOK, map[string]any{"kind": kind, "needsApp": true}
		}
		if err != nil {
			agent.Log("warn", "apps: kinds: "+err.Error())
			return http.StatusInternalServerError, map[string]string{"error": "failed"}
		}
		// A document of an older format is brought to the app's, all or nothing; one that cannot be is shown read-only with
		// the reason.
		// A person who may only view the project is shown the editor too, told the document is for reading; opening does
		// not migrate it then, for a migration writes.
		var opened apps.KindOpen
		if designAPI(d).L.CanFor(b.Document, design.RoleEdit, design.PersonIn(ctx)) {
			opened = reg.OpenDocument(storage, b.Document, info, k)
		} else {
			opened = apps.KindOpen{ReadOnly: true, Code: apps.ReadOnlyViewOnly}
		}
		return http.StatusOK, map[string]any{"kind": kind, "needsApp": false, "app": info.App, "editor": info.Editor, "target": "sinopia." + kind,
			"schema": opened, "readOnly": opened.ReadOnly, "code": opened.Code, "reason": opened.Reason}
	})

	// revert puts a migrated document back to the version it was, from the copy the migration kept.
	revert := page(http.MethodPost, func(ctx context.Context, b kindBody) (int, any) {
		if !design.IsLibraryID(b.Document) || b.To < 1 {
			return http.StatusBadRequest, map[string]string{"error": "bad-request"}
		}
		// Putting a document back writes it: only a person who may edit it. Anyone else is told it is not there.
		if !designAPI(d).L.CanFor(b.Document, design.RoleEdit, design.PersonIn(ctx)) {
			return http.StatusNotFound, notFound
		}
		kind, ok := reg.DocKind(b.Document)
		if !ok {
			return http.StatusNotFound, notFound
		}
		info, k, err := reg.Kind(kind)
		if errors.Is(err, apps.ErrNotInstalled) {
			return http.StatusNotFound, notFound
		}
		if err != nil {
			agent.Log("warn", "apps: kinds: "+err.Error())
			return http.StatusInternalServerError, map[string]string{"error": "failed"}
		}
		if err := reg.RevertMigration(storage, b.Document, info, k, b.To); err != nil {
			return http.StatusConflict, map[string]string{"error": "no-copy", "message": err.Error()}
		}
		return http.StatusOK, map[string]any{"ok": true}
	})

	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	var routes []gate.Route
	for _, prefix := range []string{platformAPI, "/agents" + platformAPI} {
		routes = append(routes,
			gate.Route{Match: is(prefix + "/apps/kinds"), Handler: list},
			gate.Route{Match: is(prefix + "/apps/kinds/docs"), Handler: docs},
			gate.Route{Match: is(prefix + "/apps/kinds/create"), Handler: create},
			gate.Route{Match: is(prefix + "/apps/kinds/open"), Handler: open},
			gate.Route{Match: is(prefix + "/apps/kinds/revert"), Handler: revert},
		)
	}
	return routes
}

// kindBody is a call of the kinds routes.
type kindBody struct {
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Document string `json:"document"`
	// To is the version a document is put back to.
	To int `json:"to"`
}
