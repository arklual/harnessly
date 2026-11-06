package main

import (
	"context"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// Sinopia for the apps (internal/apps/sinopia*.go) on the machine's library. Who may call what:
//
//	POST <platform>/apps/sinopia          a view's Document API call (hl.sinopia.*): the view's session names the document
//	POST <platform>/apps/sinopia/state    the editor says which document and chat it shows and what is picked; it gets
//	                                      the marks, the checks' results and what the apps on in the document ask for
//	POST <platform>/apps/sinopia/checks   the editor asks for the checks of a trigger (before sharing) and waits for them
//
// All three are the person's page only. The agent's calls come through the harnessly-app server (v2_apptools.go) and
// share the state kept here.

// appSinopia is the selection, marks and results of the open documents: the routes and the agent's tools share it.
var appSinopia = apps.NewSinopiaState()

// libraryDocs is the Document API over the machine's library.
type libraryDocs struct {
	lib  *design.Library
	kind func(project string) string
}

var _ apps.SinopiaDocs = libraryDocs{}

// sinopiaDocs is the Document API of this runtime's library.
func sinopiaDocs(d deps) libraryDocs {
	lib := designAPI(d).L
	file := filepath.Join(d.DataDir, "v2", "designly.json")
	reg := apps.Open(d.DataDir)
	return libraryDocs{lib: lib, kind: func(project string) string {
		// A document of a type an app adds is told by the apps' own record; the rest by the library.
		if k, ok := reg.DocKind(project); ok {
			return k
		}
		return string(kindOf((&v2Designly{file: file}).state(), lib, project))
	}}
}

func (l libraryDocs) Document(project string) (apps.SinopiaDoc, error) {
	pages, err := l.lib.DocPages(project)
	if err != nil {
		return apps.SinopiaDoc{}, err
	}
	doc := apps.SinopiaDoc{ID: project, Kind: l.kind(project), Rev: l.lib.DocRev(project), Boards: []design.DocPageRef{}}
	if m := l.lib.ReadModel(project); m != nil {
		doc.Title = m.Meta.Title
	}
	for _, p := range pages {
		doc.Boards = append(doc.Boards, pageRef(p))
	}
	return doc, nil
}

func pageRef(p design.Page) design.DocPageRef {
	return design.DocPageRef{ID: p.ID, Title: p.Title, File: p.File, W: p.W, H: p.H}
}

func (l libraryDocs) Nodes(project, page string) ([]design.DocNode, error) {
	return l.lib.DocNodes(project, page)
}

func (l libraryDocs) Query(project, selector, page string) ([]design.DocNode, error) {
	return l.lib.DocQuery(project, selector, page)
}

func (l libraryDocs) Node(project, id string) (design.DocNode, error) {
	return l.lib.DocNodeByID(project, id)
}

func (l libraryDocs) Fields(project string) ([]design.DocField, error) {
	return l.lib.DocFields(project)
}

func (l libraryDocs) Patch(project string, ops []design.DocOp, who design.DocActor) (design.DocPatched, error) {
	return l.lib.DocPatch(project, ops, who, time.Now())
}

func (l libraryDocs) AddBoard(project, title string, src []byte, tpl *design.DocTemplate, who design.DocActor) (design.DocPageRef, error) {
	p, err := l.lib.DocAddBoard(project, title, src, tpl, who, time.Now())
	if err != nil {
		return design.DocPageRef{}, err
	}
	return pageRef(p), nil
}

// installLock gives the library the apps' lock on free hand edits: a project is locked when an app on in it asks for it.
func installLock(d deps, reg *apps.Registry) {
	docs := sinopiaDocs(d)
	docs.lib.LockUnbound = func(project string) bool {
		list, err := reg.List()
		return err == nil && apps.LocksUnbound(list, docs.kind(project))
	}
}

// installShareGate makes the machine check a project before a link to it is made: the platform asks design-public
// (<project>/__share, and __rev for a pinned link) and a check of an app on in the document that blocks sharing
// refuses it with its findings. The editor's own call (POST <platform>/apps/sinopia/checks) stays a convenience: this
// is the check that holds.
func installShareGate(d deps, reg *apps.Registry) {
	api := designAPI(d)
	api.ShareGate = func(ctx context.Context, project string) *design.ShareRefusal {
		docs := sinopiaDocs(d)
		doc, err := docs.Document(project)
		if err != nil {
			return nil
		}
		list, _ := reg.List()
		active := apps.ActiveInDocument(list, doc.Kind)
		if len(active) == 0 {
			return nil
		}
		pkgOf := func(id string) *apps.Package {
			pkg, err := reg.Package(id)
			if err != nil {
				return nil
			}
			return pkg
		}
		ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		results := apps.RunChecks(ctx, docs, appSinopia, project, doc.Kind, apps.CheckOnShare, active, pkgOf)
		if !slices.ContainsFunc(results, func(r apps.CheckResult) bool { return r.Blocks }) {
			return nil
		}
		return &design.ShareRefusal{Code: design.ShareBlocked, Error: "The checks of the apps keep this document from being shared as it is", Blocked: true, Results: results}
	}
}

// sinopiaBody is a page call of the Sinopia routes.
type sinopiaBody struct {
	Session  string         `json:"session"`
	Method   string         `json:"method"`
	Params   map[string]any `json:"params"`
	Document string         `json:"document"`
	// Conversation is the chat the editor shows the document beside; "" for none.
	Conversation string `json:"conversation"`
	// Selection is what is picked; absent leaves the last report as it is.
	Selection *[]apps.SelectedNode `json:"selection"`
	On        string               `json:"on"`
}

func sinopiaRoutes(d deps, reg *apps.Registry, views *apps.ViewServer) []gate.Route {
	page := func(handle func(ctx context.Context, b sinopiaBody) (int, any)) http.HandlerFunc {
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
			var b sinopiaBody
			if !readSecretBody(r, &b) {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
				return
			}
			status, out := handle(r.Context(), b)
			secretJSON(w, status, out)
		}
	}
	pkgOf := func(id string) *apps.Package {
		pkg, err := reg.Package(id)
		if err != nil {
			return nil
		}
		return pkg
	}
	// document is the document a page call names, with its kind.
	document := func(id string) (apps.SinopiaDoc, error) {
		if !design.IsLibraryID(id) {
			return apps.SinopiaDoc{}, &apps.BridgeError{Code: apps.CodeInvalidParams, Message: "no such document"}
		}
		doc, err := sinopiaDocs(d).Document(id)
		if err != nil {
			return apps.SinopiaDoc{}, &apps.BridgeError{Code: apps.CodeInvalidParams, Message: err.Error()}
		}
		return doc, nil
	}

	call := page(func(_ context.Context, b sinopiaBody) (int, any) {
		sess, ok := views.Sessions.Get(b.Session)
		if !ok {
			return bridgeRefused(&apps.BridgeError{Code: apps.CodeHostUnavailable, Message: "no such view session"})
		}
		app, err := reg.Get(sess.App)
		if err != nil || app.Manifest == nil {
			return bridgeRefused(apps.ErrNotInstalled)
		}
		if err := app.Manifest.Authorize("harnessly/sinopia."+b.Method, sess.Target, nil); err != nil {
			return bridgeRefused(err)
		}
		if sess.Document == "" {
			return bridgeRefused(&apps.BridgeError{Code: apps.CodeInvalidParams, Message: "this view is bound to no document"})
		}
		c := apps.SinopiaCall{Docs: sinopiaDocs(d), State: appSinopia, App: app, Pkg: pkgOf(app.ID), Project: sess.Document, Who: app.Manifest.Name}
		res, err := c.Call(b.Method, b.Params)
		if err != nil {
			return bridgeRefused(err)
		}
		return http.StatusOK, map[string]any{"result": res}
	})

	state := page(func(_ context.Context, b sinopiaBody) (int, any) {
		doc, err := document(b.Document)
		if err != nil {
			return bridgeRefused(err)
		}
		if b.Conversation != "" && !apps.ValidOwner(b.Conversation) {
			return bridgeRefused(apps.ErrStorageArgs)
		}
		appSinopia.Open(b.Conversation, b.Document, doc.Kind)
		if b.Selection != nil {
			appSinopia.SetSelection(b.Document, *b.Selection)
		}
		list, _ := reg.List()
		active := apps.ActiveInDocument(list, doc.Kind)
		// The save checks run when the boards changed since they last did, and not while they run.
		if len(active) > 0 && appSinopia.ClaimCheck(b.Document, doc.Rev) {
			go func() {
				defer appSinopia.DoneCheck(b.Document, doc.Rev)
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				defer cancel()
				apps.RunChecks(ctx, sinopiaDocs(d), appSinopia, b.Document, doc.Kind, apps.CheckOnSave, active, pkgOf)
			}()
		}
		ids := make([]string, len(active))
		for i, a := range active {
			ids[i] = a.ID
		}
		return http.StatusOK, map[string]any{
			"kind": doc.Kind, "rev": doc.Rev, "boards": doc.Boards, "apps": ids, "lockUnbound": apps.LocksUnbound(list, doc.Kind),
			"marks": appSinopia.Marks(b.Document), "results": appSinopia.Results(b.Document),
		}
	})

	checks := page(func(ctx context.Context, b sinopiaBody) (int, any) {
		doc, err := document(b.Document)
		if err != nil {
			return bridgeRefused(err)
		}
		if !slices.Contains([]string{apps.CheckOnSave, apps.CheckOnShare}, b.On) {
			return bridgeRefused(&apps.BridgeError{Code: apps.CodeInvalidParams, Message: "on is save or share"})
		}
		list, _ := reg.List()
		ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		results := apps.RunChecks(ctx, sinopiaDocs(d), appSinopia, b.Document, doc.Kind, b.On, apps.ActiveInDocument(list, doc.Kind), pkgOf)
		blocked := slices.ContainsFunc(results, func(r apps.CheckResult) bool { return r.Blocks })
		for _, r := range results {
			if r.Failed != "" {
				agent.Log("warn", "apps: sinopia check failed: "+r.App+"/"+r.Check+": "+r.Failed)
			}
		}
		return http.StatusOK, map[string]any{"results": results, "blocked": blocked, "marks": appSinopia.Marks(b.Document)}
	})

	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	var out []gate.Route
	for path, h := range map[string]http.HandlerFunc{"/apps/sinopia": call, "/apps/sinopia/state": state, "/apps/sinopia/checks": checks} {
		out = append(out, gate.Route{Match: is(platformAPI + path), Handler: h})
	}
	return out
}

// viewDocumentKind checks that a document can be the one a view of target sinopia.<kind> is shown in.
func viewDocumentKind(d deps, target, document string) error {
	kind, ok := strings.CutPrefix(target, "sinopia.")
	if !ok || !design.IsLibraryID(document) {
		return &apps.BridgeError{Code: apps.CodeInvalidParams, Message: "a Sinopia view names its document"}
	}
	doc, err := sinopiaDocs(d).Document(document)
	if err != nil || doc.Kind != kind {
		return &apps.BridgeError{Code: apps.CodeNotInTarget, Message: "the document is not a " + kind + " one"}
	}
	return nil
}
