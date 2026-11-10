package main

import (
	"net/http"
	"strconv"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// chatRoutes is the CLI's `harnessly chat test` (HRN-842) and `harnessly chat delete` (HRN-520): the mark of a test chat, set through the same command the
// page sends. The chat is named by its id, or by the tmux session of the calling tab ("current").
func chatRoutes(d deps, chats appChats) []gate.Route {
	test := func(w http.ResponseWriter, r *http.Request) {
		if d.CLI == nil || !d.CLI.OK(r) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return
		}
		if r.Method != http.MethodPost {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		var b struct {
			Session string `json:"session"`
			ID      string `json:"id"`
			Test    bool   `json:"test"`
		}
		if !readSecretBody(r, &b) || (b.Session == "" && b.ID == "") {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		conv := b.ID
		if conv == "" {
			conv = chats.ofSession(b.Session)
		}
		if _, ok := chats.record(conv); !ok {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "no-chat"})
			return
		}
		res := chats.do(r.Context(), wire.ConversationTestCmd{OpID: "cli-test-" + strconv.FormatInt(time.Now().UnixNano(), 36), ID: conv, Test: b.Test})
		if res.R != wire.ResultAccepted {
			status := http.StatusConflict
			if res.Code == wire.RefuseNotFound {
				status = http.StatusNotFound
			}
			secretJSON(w, status, map[string]string{"error": string(res.Code)})
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"conversation": conv, "test": b.Test})
	}
	// `harnessly chat delete ID` removes a chat the calling chat delegated, as the sidebar's «Удалить» does (it can be
	// restored from the bin): the limit of delegated chats counts them until they go. Only the caller's own children,
	// and only one that is not working: the command is not forced.
	del := func(w http.ResponseWriter, r *http.Request) {
		if d.CLI == nil || !d.CLI.OK(r) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return
		}
		if r.Method != http.MethodPost {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		var b struct {
			Session string `json:"session"`
			ID      string `json:"id"`
		}
		if !readSecretBody(r, &b) || b.Session == "" || b.ID == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		caller := chats.ofSession(b.Session)
		if caller == "" {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "no-chat"})
			return
		}
		rec, ok := chats.record(b.ID)
		if !ok {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		if rec.ParentID == nil || *rec.ParentID != caller {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "not-yours"})
			return
		}
		res := chats.do(r.Context(), wire.ConversationDeleteCmd{OpID: "cli-delete-" + strconv.FormatInt(time.Now().UnixNano(), 36), ID: b.ID})
		if res.R != wire.ResultAccepted {
			secretJSON(w, http.StatusConflict, map[string]string{"error": string(res.Code)})
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"conversation": b.ID, "deleted": true})
	}
	return []gate.Route{
		{Match: func(r *http.Request) bool { return pathIs(r, "/api/cli/chat/test") }, Handler: http.HandlerFunc(test)},
		{Match: func(r *http.Request) bool { return pathIs(r, "/api/cli/chat/delete") }, Handler: http.HandlerFunc(del)},
	}
}
