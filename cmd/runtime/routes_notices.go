package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/noticeslot"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The smart notice's open slot: what apps and agents ask the person in the black plate. Three ways in — an
// app's view (hl.notice, through its view session), the `harnessly notice` command and the agent's notice_* MCP tools
// (noticeslot.Tools) — and the page's way out: the list, the answer, the person turning a source off.

// noticeSlot is the machine's slot; noticeMutes the sources turned off (set up with the routes, kept in the data dir).
var (
	noticeSlot  = noticeslot.New()
	noticeMutes = &noticeslot.Mutes{}
)

// noticeWaitMax is how long one wait call holds (the CLI asks again until the notice ends).
const noticeWaitMax = 50 * time.Second

// noticeAppWait is one wait of an app's view, under the page's own term for a call.
const noticeAppWait = 15 * time.Second

func noticeRefused(w http.ResponseWriter, err error) {
	code := map[error]string{
		noticeslot.ErrArgs: "bad-notice", noticeslot.ErrSecret: "no-secrets", noticeslot.ErrBusy: "busy",
		noticeslot.ErrRate: "too-often", noticeslot.ErrMuted: "muted", noticeslot.ErrGone: "not-found",
		noticeslot.ErrAnswer: "bad-answer", noticeslot.ErrAnswerSecret: "looks-like-key",
	}
	status := http.StatusBadRequest
	for e, c := range code {
		if errors.Is(err, e) {
			switch e {
			case noticeslot.ErrGone:
				status = http.StatusNotFound
			case noticeslot.ErrRate, noticeslot.ErrBusy:
				status = http.StatusTooManyRequests
			case noticeslot.ErrMuted:
				status = http.StatusForbidden
			}
			secretJSON(w, status, map[string]string{"error": c, "message": noticeslot.Refusal(err)})
			return
		}
	}
	secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
}

func noticeRoutes(d deps, reg *apps.Registry, views *apps.ViewServer, chats appChats) []gate.Route {
	noticeMutes.Path = filepath.Join(d.DataDir, "notice-mutes.json")
	noticeSlot.Muted = noticeMutes.Has

	page := func(method string, w http.ResponseWriter, r *http.Request) bool {
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
	base := platformAPI + "/notices"

	// The page: what is open, and the sources turned off.
	list := func(w http.ResponseWriter, r *http.Request) {
		if !page(http.MethodGet, w, r) {
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"notices": noticeSlot.Pending(), "muted": noticeMutes.List()})
	}
	answer := func(w http.ResponseWriter, r *http.Request) {
		if !page(http.MethodPost, w, r) {
			return
		}
		var b struct {
			ID      string          `json:"id"`
			Value   json.RawMessage `json:"value"`
			Dismiss bool            `json:"dismiss"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		var err error
		if b.Dismiss {
			err = noticeSlot.Dismiss(b.ID)
		} else {
			err = noticeSlot.Answer(b.ID, b.Value)
		}
		if err != nil {
			noticeRefused(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]string{})
	}
	mute := func(w http.ResponseWriter, r *http.Request) {
		if !page(http.MethodPost, w, r) {
			return
		}
		var b struct {
			Source noticeslot.Source `json:"source"`
			Off    bool              `json:"off"`
		}
		if !readSecretBody(r, &b) {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		if err := noticeMutes.Set(b.Source, b.Off); err != nil {
			noticeRefused(w, err)
			return
		}
		if b.Off {
			noticeSlot.MuteSource(b.Source)
		}
		secretJSON(w, http.StatusOK, map[string]any{"muted": noticeMutes.List()})
	}

	// An app's view: its session names the app and the chat; the app needs the notices permission.
	appCall := func(w http.ResponseWriter, r *http.Request) {
		if !page(http.MethodPost, w, r) {
			return
		}
		var b struct {
			Session  string          `json:"session"`
			Op       string          `json:"op"` // ask, show, update, close
			Spec     noticeslot.Spec `json:"spec"`
			ID       string          `json:"id"`
			Progress *float64        `json:"progress"`
			Title    *string         `json:"title"`
			Text     *string         `json:"text"`
		}
		if !readSecretBody(r, &b) {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		sess, ok := views.Sessions.Get(b.Session)
		if !ok || sess.App == apps.WidgetsApp {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "no-session"})
			return
		}
		app, err := reg.Get(sess.App)
		if err != nil || app.Manifest == nil {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "not-installed"})
			return
		}
		if !app.Manifest.Allows("notices") {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "permission", "message": "the app has no notices permission in its manifest"})
			return
		}
		src := noticeslot.Source{Kind: noticeslot.SourceApp, ID: app.ID, Name: app.Manifest.Name}
		switch b.Op {
		case "ask":
			// The page's calls are short: ask opens the notice and gives its id, wait holds a while and is called again.
			if !b.Spec.Kind.Waits() {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-notice", "message": "ask shows a kind that waits; status and progress go through show"})
				return
			}
			n, err := noticeSlot.Open(src, sess.Conversation, b.Spec)
			if err != nil {
				noticeRefused(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]string{"id": n.ID})
		case "wait":
			if owner, ok := noticeSlot.SourceOf(b.ID); ok && owner.Key() != src.Key() {
				noticeRefused(w, noticeslot.ErrGone)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), noticeAppWait)
			defer cancel()
			res, done := noticeSlot.Wait(ctx, b.ID)
			secretJSON(w, http.StatusOK, map[string]any{"done": done, "result": res})
		case "show":
			if b.Spec.Kind.Waits() {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-notice", "message": "show is for status and progress"})
				return
			}
			n, err := noticeSlot.Open(src, sess.Conversation, b.Spec)
			if err != nil {
				noticeRefused(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]string{"id": n.ID})
		case "update":
			if err := noticeSlot.Update(src, b.ID, b.Progress, b.Text, b.Title); err != nil {
				noticeRefused(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]string{})
		case "close":
			if err := noticeSlot.Close(src, b.ID); err != nil {
				noticeRefused(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]string{})
		default:
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
		}
	}

	// The `harnessly notice` command: the agent's session names its chat.
	cliSource := func(session string) (noticeslot.Source, string, bool) {
		if chats.ofSession == nil || session == "" {
			return noticeslot.Source{}, "", false
		}
		conv := chats.ofSession(session)
		if conv == "" {
			return noticeslot.Source{}, "", false
		}
		name := "Агент"
		if rec, ok := chats.record(conv); ok && strings.TrimSpace(rec.Title) != "" {
			name = rec.Title
		}
		return noticeslot.Source{Kind: noticeslot.SourceAgent, ID: conv, Name: name}, conv, true
	}
	cli := func(w http.ResponseWriter, r *http.Request) {
		if d.CLI == nil || !d.CLI.OK(r) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return
		}
		rest := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/api/cli/notices"), "/")
		id, action, _ := strings.Cut(rest, "/")
		if id == "" {
			if r.Method != http.MethodPost {
				secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
				return
			}
			var b struct {
				Session string          `json:"session"`
				Spec    noticeslot.Spec `json:"spec"`
			}
			if !readSecretBody(r, &b) {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
				return
			}
			src, conv, ok := cliSource(b.Session)
			if !ok {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "no-chat", "message": "run it from an agent's chat (HARNESSLY_SESSION is not a chat here)"})
				return
			}
			if b.Spec.Kind == noticeslot.KindCode || b.Spec.Kind == noticeslot.KindCustom {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-notice", "message": noticeslot.AgentCodeRefusal})
				return
			}
			n, err := noticeSlot.Open(src, conv, b.Spec)
			if err != nil {
				noticeRefused(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]string{"id": n.ID})
			return
		}
		session := r.URL.Query().Get("session")
		var body struct {
			Session  string   `json:"session"`
			Progress *float64 `json:"progress"`
			Title    *string  `json:"title"`
			Text     *string  `json:"text"`
		}
		if r.Method == http.MethodPost {
			if !readSecretBody(r, &body) {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
				return
			}
			session = body.Session
		}
		src, _, ok := cliSource(session)
		if !ok {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "no-chat"})
			return
		}
		switch {
		case action == "wait" && r.Method == http.MethodGet:
			if owner, ok := noticeSlot.SourceOf(id); ok && owner.Key() != src.Key() {
				noticeRefused(w, noticeslot.ErrGone)
				return
			}
			hold := noticeWaitMax
			if s, err := strconv.Atoi(r.URL.Query().Get("timeout")); err == nil && s >= 0 && time.Duration(s)*time.Second < hold {
				hold = time.Duration(s) * time.Second
			}
			ctx, cancel := context.WithTimeout(r.Context(), hold)
			defer cancel()
			res, done := noticeSlot.Wait(ctx, id)
			secretJSON(w, http.StatusOK, map[string]any{"done": done, "result": res})
		case action == "update" && r.Method == http.MethodPost:
			if err := noticeSlot.Update(src, id, body.Progress, body.Text, body.Title); err != nil {
				noticeRefused(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]string{})
		case action == "close" && r.Method == http.MethodPost:
			if err := noticeSlot.Close(src, id); err != nil {
				noticeRefused(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]string{})
		default:
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		}
	}

	is := func(p string) func(*http.Request) bool {
		return func(r *http.Request) bool { return pathIs(r, p) || pathIs(r, "/agents"+p) }
	}
	return []gate.Route{
		{Match: is(base), Handler: http.HandlerFunc(list)},
		{Match: is(base + "/answer"), Handler: http.HandlerFunc(answer)},
		{Match: is(base + "/mute"), Handler: http.HandlerFunc(mute)},
		{Match: is(base + "/app"), Handler: http.HandlerFunc(appCall)},
		{Match: func(r *http.Request) bool {
			return pathIs(r, "/api/cli/notices") || strings.HasPrefix(r.URL.Path, "/api/cli/notices/")
		}, Handler: http.HandlerFunc(cli)},
	}
}
