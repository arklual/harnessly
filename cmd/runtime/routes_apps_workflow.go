package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/push"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The statuses of an app's documents (internal/apps/workflow.go), for the views:
//
//	POST <platform>/apps/workflow  {session, op: get}                          → the document's state for this person
//	POST <platform>/apps/workflow  {session, op: move, to, version, hash, note} → the move, checked, then the state
//
// The document is the session's (a view of a sinopia target is bound to one); the role is the person's in the project,
// as the library tells it, so the page cannot claim another. Every read follows the document: a change of a version
// that may not be edited starts the next version first.

type workflowBody struct {
	Session string `json:"session"`
	Op      string `json:"op"`
	To      string `json:"to"`
	Version int    `json:"version"`
	Hash    string `json:"hash"`
	Note    string `json:"note"`
}

// workflowRefused is a refused move in the bridge's words.
func workflowRefused(err error) (int, any) {
	codes := map[error]string{
		apps.ErrNoWorkflow:    "the app has no workflow in its manifest",
		apps.ErrWorkflowMove:  "there is no such move from the current status",
		apps.ErrWorkflowRole:  "your role in the project does not allow this move",
		apps.ErrWorkflowStale: "the document changed since it was read: read the status again",
		apps.ErrWorkflowNote:  "the note is too long",
	}
	for e, msg := range codes {
		if errors.Is(err, e) {
			code := apps.CodeInvalidParams
			status := http.StatusBadRequest
			switch e {
			case apps.ErrWorkflowRole:
				code, status = apps.CodePermissionDenied, http.StatusForbidden
			case apps.ErrWorkflowStale:
				code, status = apps.CodeConflict, http.StatusConflict
			}
			return status, map[string]string{"error": code, "message": msg}
		}
	}
	return bridgeRefused(err)
}

// docVersionHash is what a document is now: its pages and what the app keeps of it in its doc area.
func docVersionHash(lib *design.Library, storage *apps.Storage, app, doc string) (string, error) {
	area, err := storage.ContentHash(app, apps.ScopeDoc, doc)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(lib.DocContentHash(doc) + "/" + area))
	return hex.EncodeToString(sum[:]), nil
}

// workflowStep reads (and follows) a document's record and, with move, makes a move in the same write.
func workflowStep(w *apps.Workflow, storage *apps.Storage, app, doc, hash string,
	move func(r *apps.WorkflowRecord) error) (apps.WorkflowRecord, error) {
	var rec apps.WorkflowRecord
	_, err := storage.HostUpdate(app, apps.ScopeDoc, doc, apps.WorkflowKey, func(old json.RawMessage) (json.RawMessage, error) {
		r, err := apps.ParseWorkflowRecord(old)
		if err != nil {
			r = apps.WorkflowRecord{}
		}
		changed := w.Follow(&r, hash, time.Now())
		if move != nil {
			if err := move(&r); err != nil {
				return nil, err
			}
			changed = true
		}
		rec = r
		if !changed {
			return nil, nil
		}
		return json.Marshal(r)
	})
	return rec, err
}

func appWorkflowRoute(d deps, reg *apps.Registry, storage *apps.Storage, views *apps.ViewServer) http.HandlerFunc {
	handle := func(ctx context.Context, b workflowBody) (int, any) {
		sess, ok := views.Sessions.Get(b.Session)
		if !ok {
			return bridgeRefused(&apps.BridgeError{Code: apps.CodeHostUnavailable, Message: "no such view session"})
		}
		app, err := reg.Get(sess.App)
		if err != nil {
			return bridgeRefused(err)
		}
		if app.Manifest == nil || app.Manifest.Workflow == nil || len(app.Manifest.Workflow.Statuses) == 0 {
			return workflowRefused(apps.ErrNoWorkflow)
		}
		if sess.Document == "" {
			return bridgeRefused(&apps.BridgeError{Code: apps.CodeInvalidParams, Message: "this view is bound to no document"})
		}
		lib := designAPI(d).L
		person := design.PersonIn(ctx)
		role := string(lib.RoleFor(sess.Document, person))
		hash, err := docVersionHash(lib, storage, sess.App, sess.Document)
		if err != nil {
			return bridgeRefused(err)
		}
		w := app.Manifest.Workflow
		var move func(r *apps.WorkflowRecord) error
		switch b.Op {
		case "get":
		case "move":
			who := ""
			if id, err := lib.IdentityOf(person); err == nil {
				who = id.Person
			}
			move = func(r *apps.WorkflowRecord) error {
				return w.Move(r, b.To, who, role, b.Version, b.Hash, b.Note, time.Now())
			}
		default:
			return bridgeRefused(apps.ErrUnknownMethod)
		}
		rec, err := workflowStep(w, storage, sess.App, sess.Document, hash, move)
		if err != nil {
			return workflowRefused(err)
		}
		if move != nil && len(rec.Journal) > 0 {
			rec = workflowDecided(d, lib, storage, w, sess.App, sess.Document, person, rec)
		}
		// The chat the view is in works on this document: its agent is told the status and its rules by status hold.
		if cur := rec.Current(); sess.Conversation != "" {
			if _, err := reg.NoteChatStatus(sess.Conversation, apps.ChatDocStatus{App: sess.App, Doc: sess.Document,
				Version: cur.N, Status: cur.Status, At: time.Now().UnixMilli()}); err != nil {
				agent.Log("warn", "apps: workflow: the chat's status: "+err.Error())
			}
		}
		state := w.State(rec, role)
		for i, e := range state.Journal {
			state.Journal[i].Link = lib.VersionLink(sess.Document, e.Rev)
		}
		return http.StatusOK, state
	}
	return func(rw http.ResponseWriter, r *http.Request) {
		if sameorigin.Refuse(rw, r) {
			return
		}
		if !d.SessionOK(r) {
			secretJSON(rw, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		if r.Method != http.MethodPost {
			secretJSON(rw, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		var b workflowBody
		if !readSecretBody(r, &b) {
			secretJSON(rw, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		status, out := handle(design.WithPerson(r.Context(), designAPI(d).PersonOf(r)), b)
		secretJSON(rw, status, out)
	}
}

// workflowDecided follows a move: the project as it was decided on is kept as a named version of Sinopia's history
// ("v2 · На ревью"), its point goes into the move's journal entry, so the decision links to what was decided on, and
// the machine's owner is told by a push when another person of the project made the move. Neither failing undoes the
// move: it is logged.
func workflowDecided(d deps, lib *design.Library, storage *apps.Storage, w *apps.Workflow, app, doc, person string,
	rec apps.WorkflowRecord) apps.WorkflowRecord {
	last := rec.Journal[len(rec.Journal)-1]
	to, _ := w.Status(last.To)
	from, _ := w.Status(last.From)
	who, err := lib.IdentityOf(person)
	if err != nil {
		agent.Log("warn", "apps: workflow: who moved: "+err.Error())
	}
	// One named version per version of the document: a move back and forth on the same content reuses the point
	// kept before, and only a person who may edit the project adds one to its named versions (they are bounded).
	rev := ""
	for _, e := range rec.Journal {
		if e.Version == last.Version && e.Hash == last.Hash && e.Rev != "" {
			rev = e.Rev
		}
	}
	if rev == "" && lib.RoleFor(doc, person).AtLeast(design.RoleEdit) {
		name := "v" + strconv.Itoa(last.Version) + " · " + to.Title
		if v, err := lib.SaveProjectVersion(doc, name, who.Person); err != nil {
			agent.Log("warn", "apps: workflow: the decided version: "+err.Error())
		} else {
			rev = v.ID
		}
	}
	if rev != "" {
		_, err := storage.HostUpdate(app, apps.ScopeDoc, doc, apps.WorkflowKey, func(old json.RawMessage) (json.RawMessage, error) {
			r, err := apps.ParseWorkflowRecord(old)
			if err != nil || !r.MarkRev(last.At, last.Version, rev) {
				return nil, nil
			}
			rec = r
			return json.Marshal(r)
		})
		if err != nil {
			agent.Log("warn", "apps: workflow: the decided version in the journal: "+err.Error())
		}
	}
	own, err := lib.Identity()
	if err != nil || who.PersonID == "" || who.PersonID == own.PersonID {
		return rec
	}
	h := hookConfigFromEnv(d.ConfigFile)
	if h.PushSubject == "" || h.PushTagPrefix == "" {
		return rec
	}
	title := doc
	if a := lib.ReadArtifact(doc); a != nil && a.Title != "" {
		title = a.Title
	}
	sender := newSharedPusher(d.DataDir, h)
	body := who.Person + ": «" + from.Title + "» → «" + to.Title + "», v" + strconv.Itoa(last.Version)
	if last.Note != "" {
		body += ". " + last.Note
	}
	msg := push.Message{Kind: "design", Title: push.Clip(title, 90), Body: push.Clip(body, 180), ConversationTitle: title,
		Detail: push.Clip(body, 180), Tag: sender.TagPrefix() + "workflow-" + doc, At: time.Now().UnixMilli(),
		URL: "/designly/doc/" + url.PathEscape(doc)}
	go func() {
		for _, sub := range push.SelectTargets(sender.Store.Subscriptions(), sender.Store.Visible()) {
			if st, _ := sender.Send(sub, msg); st == http.StatusGone || st == http.StatusNotFound {
				_ = sender.Store.Remove(sub.Endpoint)
			}
		}
	}()
	return rec
}
