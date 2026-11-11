package main

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/chatwork"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func init() { routeGroups = append(routeGroups, workRoutes) }

// The work of a chat (internal/chatwork), for the «Changes» tab:
//
//	POST <platform>/work {conversationId}            the records of the chat and of the chats it delegated on this
//	                                                 machine; a chat older than the registry has its transcript looked
//	                                                 through first, once
//	POST <platform>/work/remote {remote, branch, base} a pushed branch read from its code host by the connection on
//	                                                 the remote's host: commits, diff against base, its merge request
//
// Both are the page's (a page session); they only read.

var workConvID = regexp.MustCompile(`^[A-Za-z0-9:_.#-]{1,200}$`)

const workRemoteTimeout = 30 * time.Second

func workRoutes(d deps) []gate.JSONRoute {
	refuse := func(r *http.Request) (int, any, http.Header, bool) {
		if status, v, h, refused := d.APIRefusal(r); refused {
			return status, v, h, true
		}
		if !d.SessionOK(r) {
			s, v, h := answerJSON(r, http.StatusForbidden, map[string]string{"error": "Forbidden"}, nil)
			return s, v, h, true
		}
		return 0, nil, nil, false
	}
	body := func(r *http.Request, out any) *gateAnswer {
		raw, refusal := readPromptBody(r)
		if refusal != nil {
			return refusal
		}
		if json.Unmarshal(raw, out) != nil {
			return &gateAnswer{status: http.StatusBadRequest, text: "Invalid JSON"}
		}
		return nil
	}
	text := func(a *gateAnswer) (int, any, http.Header) {
		return a.status, gate.Raw{ContentType: "text/plain", Body: []byte(a.text)}, nil
	}
	noStore := func() http.Header { return http.Header{"Cache-Control": {"no-store"}} }
	return []gate.JSONRoute{{
		Slice: "fsread",
		Match: func(r *http.Request) bool { return r.Method == http.MethodPost && pathIs(r, platformAPI+"/work") },
		Compute: func(r *http.Request) (int, any, http.Header) {
			if s, v, h, refused := refuse(r); refused {
				return s, v, h
			}
			var in struct {
				ConversationID string `json:"conversationId"`
			}
			if a := body(r, &in); a != nil {
				return text(a)
			}
			if !workConvID.MatchString(in.ConversationID) {
				return answerJSON(r, http.StatusBadRequest, map[string]string{"error": "bad-request"}, nil)
			}
			var cw chatWork
			if d.ChatWork != nil {
				cw = d.ChatWork()
			}
			if cw == nil {
				return answerJSON(r, http.StatusServiceUnavailable, map[string]string{"error": "not-ready"}, noStore())
			}
			entries, complete := chatWorkOf(r.Context(), cw, in.ConversationID, chatWorkScanWait)
			return answerJSON(r, http.StatusOK, map[string]any{"entries": entries, "complete": complete}, noStore())
		},
	}, {
		Slice: "fsread",
		Match: func(r *http.Request) bool {
			return r.Method == http.MethodPost && pathIs(r, platformAPI+"/work/remote")
		},
		Compute: func(r *http.Request) (int, any, http.Header) {
			if s, v, h, refused := refuse(r); refused {
				return s, v, h
			}
			var in struct {
				Remote string `json:"remote"`
				Branch string `json:"branch"`
				Base   string `json:"base"`
			}
			if a := body(r, &in); a != nil {
				return text(a)
			}
			host, project := chatwork.RemoteProject(in.Remote)
			if host == "" || project == "" || strings.TrimSpace(in.Branch) == "" {
				return answerJSON(r, http.StatusBadRequest, map[string]string{"error": "bad-request"}, nil)
			}
			cs := connectionsFor(d)
			conn, ok := cs.ForHost(host)
			if !ok {
				return answerJSON(r, http.StatusNotFound, map[string]string{"error": "no-connection", "host": host}, noStore())
			}
			src, err := cs.Branches(conn)
			if err != nil {
				return answerJSON(r, connStatus(connReason(err)), map[string]string{"error": connReason(err)}, noStore())
			}
			ctx, cancel := context.WithTimeout(r.Context(), workRemoteTimeout)
			defer cancel()
			b, err := src.Branch(ctx, project, in.Base, in.Branch)
			if err != nil {
				agent.Log("info", "work: a branch not read", agent.F{K: "id", V: conn.ID}, agent.F{K: "reason", V: connReason(err)})
				return answerJSON(r, connStatus(connReason(err)), map[string]string{"error": connReason(err)}, noStore())
			}
			return answerJSON(r, http.StatusOK, map[string]any{"connection": conn.ID, "branch": b, "diff": b.GitDiff()}, noStore())
		},
	}}
}

// chatWorkOf is the records of a chat and of the chats delegated from it on this machine. The transcripts of the
// ones not scanned yet are looked through first, within wait; complete is false when some were not finished then.
func chatWorkOf(ctx context.Context, cw chatWork, conv string, wait time.Duration) ([]hubproto.ChatWorkEntry, bool) {
	o := cw.workObserver()
	ids := chatwork.Subtree(conv, cw.localChats(ctx))
	deadline := time.Now().Add(wait)
	complete := true
	for _, id := range ids {
		left := time.Until(deadline)
		if left <= 0 || !o.Scan(id, left) {
			complete = false
			break
		}
	}
	entries := o.Reg.Of(ids...)
	if entries == nil {
		entries = []hubproto.ChatWorkEntry{}
	}
	return entries, complete
}
