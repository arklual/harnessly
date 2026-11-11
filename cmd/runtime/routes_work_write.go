package main

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/gitread"
)

func init() { routeGroups = append(routeGroups, workWriteRoutes) }

// workWriteScanWait bounds the look through old transcripts a write may wait for (the review has read them already).
const workWriteScanWait = 5 * time.Second

// The person's own hand in the review of a chat's work («Ревью кода»):
//
//	PUT  <platform>/work/file {conversationId, root, path, text, expect}   writes a file of the repository at root from the diff's new
//	                                                      side; refused (409) when the file changed since the page
//	                                                      read it (expect is the hash of what it read)
//	POST <platform>/work/commit {conversationId, root, message, paths}    commits those files
//
// Both are the page's (a page session, same origin, a JSON body); an agent's cli-token is not a person. The root must
// be a worktree the chat (or a chat it delegated to) registered as its work: the review writes nowhere else.

func workWriteRoutes(d deps) []gate.JSONRoute {
	refuse := func(r *http.Request) (int, any, http.Header, bool) {
		if status, v, h, refused := d.APIRefusal(r); refused {
			return status, v, h, true
		}
		if !d.SessionOK(r) {
			s, v, h := answerJSON(r, http.StatusForbidden, map[string]string{"error": "Forbidden"}, nil)
			return s, v, h, true
		}
		if site, _ := fetchSite(r); site == "cross-site" {
			s, v, h := answerJSON(r, http.StatusForbidden, map[string]string{"error": "Forbidden"}, nil)
			return s, v, h, true
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			s, v, h := answerJSON(r, http.StatusUnsupportedMediaType, map[string]string{"error": "json-only"}, nil)
			return s, v, h, true
		}
		return 0, nil, nil, false
	}
	ownWork := func(r *http.Request, conv, root string) bool {
		if d.ChatWork == nil {
			return false
		}
		return isChatWorktree(r.Context(), d.ChatWork(), conv, root)
	}
	notOwn := func(r *http.Request) (int, any, http.Header) {
		return answerJSON(r, http.StatusForbidden, map[string]string{"error": "not-chat-work"}, http.Header{"Cache-Control": {"no-store"}})
	}
	noStore := http.Header{"Cache-Control": {"no-store"}}
	read := func(r *http.Request, out any) bool {
		raw, refusal := readPromptBody(r)
		return refusal == nil && json.Unmarshal(raw, out) == nil
	}
	bad := func(r *http.Request) (int, any, http.Header) {
		return answerJSON(r, http.StatusBadRequest, map[string]string{"error": "bad-request"}, noStore)
	}
	return []gate.JSONRoute{{
		Slice: "fswrite",
		Match: func(r *http.Request) bool { return r.Method == http.MethodPut && pathIs(r, platformAPI+"/work/file") },
		Compute: func(r *http.Request) (int, any, http.Header) {
			if s, v, h, refused := refuse(r); refused {
				return s, v, h
			}
			var in struct {
				Conv   string `json:"conversationId"`
				Root   string `json:"root"`
				Path   string `json:"path"`
				Text   string `json:"text"`
				Expect string `json:"expect"`
			}
			if !read(r, &in) {
				return bad(r)
			}
			if !ownWork(r, in.Conv, in.Root) {
				return notOwn(r)
			}
			status, v := gitread.WriteFile(in.Root, in.Path, in.Text, in.Expect)
			agent.Log("info", "work: a file written from the review", agent.F{K: "status", V: status})
			return answerJSON(r, status, v, noStore)
		},
	}, {
		Slice: "fswrite",
		Match: func(r *http.Request) bool {
			return r.Method == http.MethodPost && pathIs(r, platformAPI+"/work/commit")
		},
		Compute: func(r *http.Request) (int, any, http.Header) {
			if s, v, h, refused := refuse(r); refused {
				return s, v, h
			}
			var in struct {
				Conv    string   `json:"conversationId"`
				Root    string   `json:"root"`
				Message string   `json:"message"`
				Paths   []string `json:"paths"`
			}
			if !read(r, &in) {
				return bad(r)
			}
			if !ownWork(r, in.Conv, in.Root) {
				return notOwn(r)
			}
			status, v := gitread.CommitFiles(in.Root, in.Message, in.Paths)
			agent.Log("info", "work: a commit from the review", agent.F{K: "status", V: status})
			return answerJSON(r, status, v, noStore)
		},
	}}
}

// isChatWorktree: root is one of the worktrees the chat's work (its own and its delegated chats') lists.
func isChatWorktree(ctx context.Context, cw chatWork, conv, root string) bool {
	if cw == nil || !workConvID.MatchString(conv) || !filepath.IsAbs(root) {
		return false
	}
	entries, _ := chatWorkOf(ctx, cw, conv, workWriteScanWait)
	want := filepath.Clean(root)
	for _, e := range entries {
		if e.Worktree != "" && filepath.Clean(e.Worktree) == want {
			return true
		}
	}
	return false
}
