package main

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/chatwork"
	"github.com/ly-solutions/harnessly/internal/connections"
	"github.com/ly-solutions/harnessly/internal/gate"
)

func init() { routeGroups = append(routeGroups, codeReviewRoutes) }

// The review of a merge request (a pull request on GitHub) in the «Ревью кода» app, through the person's connections
// (Settings → Connections):
//
//	GET  <platform>/code-review?url=<merge request>        its discussions, the person's draft notes, approvals
//	POST <platform>/code-review {url, act}                 one act of the review (reply, resolve, draft, publish, approve…)
//	POST <platform>/code-review/open {remote, source, target, title, draft}  a merge request of a pushed branch
//	GET  <platform>/code-review/github                     the open pull requests of the person's GitHub, for the start
//
// Reading may go through the deployment's GitLab (env-gitlab) when the person has no connection of their own on the
// host. Writing never does: every write goes only through a connection the person made themselves, so nothing is
// sent in another's name; without one the answer is {error: "no-personal"} and the page offers to connect.

const codeReviewTimeout = 30 * time.Second

// reviewConn is the connection to read a host's review with: the person's own GitLab or GitHub connection first
// (writable), else the deployment's (read only).
func reviewConn(cs *connections.Service, host string) (connections.Conn, bool, bool) {
	list, err := cs.List()
	if err != nil {
		return connections.Conn{}, false, false
	}
	return pickReviewConn(list, host)
}

// pickReviewConn is reviewConn over a list: the connection, whether it is the person's own, and whether any fits.
func pickReviewConn(list []connections.Conn, host string) (connections.Conn, bool, bool) {
	var env *connections.Conn
	var own []connections.Conn
	for _, c := range list {
		u, err := url.Parse(c.BaseURL)
		if err != nil || (c.Kind != connections.GitLab && c.Kind != connections.GitHub) || !sameHost(u.Hostname(), host) {
			continue
		}
		if c.Env {
			env = &c
			continue
		}
		own = append(own, c)
	}
	for _, c := range own {
		if c.Primary {
			return c, true, true
		}
	}
	if len(own) > 0 {
		return own[0], true, true
	}
	if env != nil {
		return *env, false, true
	}
	return connections.Conn{}, false, false
}

func codeReviewRoutes(d deps) []gate.JSONRoute {
	refuse := func(r *http.Request) (int, any, http.Header, bool) {
		if status, v, h, refused := d.APIRefusal(r); refused {
			return status, v, h, true
		}
		if !d.SessionOK(r) {
			s, v, h := answerJSON(r, http.StatusForbidden, map[string]string{"error": "Forbidden"}, nil)
			return s, v, h, true
		}
		// The page calls these from its own origin; a request another site makes is not the person's.
		if site, _ := fetchSite(r); site == "cross-site" {
			s, v, h := answerJSON(r, http.StatusForbidden, map[string]string{"error": "Forbidden"}, nil)
			return s, v, h, true
		}
		if r.Method == http.MethodPost && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			s, v, h := answerJSON(r, http.StatusUnsupportedMediaType, map[string]string{"error": "json-only"}, nil)
			return s, v, h, true
		}
		return 0, nil, nil, false
	}
	noStore := http.Header{"Cache-Control": {"no-store"}}
	fail := func(r *http.Request, err error, extra map[string]any) (int, any, http.Header) {
		reason := connReason(err)
		body := map[string]any{"error": reason}
		if strings.Contains(err.Error(), "api scope") {
			body["scope"] = "api"
		} else if strings.Contains(err.Error(), "repo scope") {
			body["scope"] = "repo"
		}
		for k, v := range extra {
			body[k] = v
		}
		return answerJSON(r, connStatus(reason), body, noStore)
	}
	bad := func(r *http.Request) (int, any, http.Header) {
		return answerJSON(r, http.StatusBadRequest, map[string]string{"error": "bad-request"}, noStore)
	}
	// target is the merge request a link names and the connection to reach it with.
	target := func(cs *connections.Service, raw string) (connections.Conn, bool, string, int, string) {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return connections.Conn{}, false, "", 0, "bad-request"
		}
		conn, personal, ok := reviewConn(cs, u.Hostname())
		if !ok {
			return connections.Conn{}, false, "", 0, "no-connection"
		}
		project, iid, ok := connections.MergeRequestOf(conn, raw)
		if !ok {
			return connections.Conn{}, false, "", 0, "bad-request"
		}
		return conn, personal, project, iid, ""
	}
	about := func(conn connections.Conn, personal bool) map[string]any {
		return map[string]any{"id": conn.ID, "name": conn.Name, "kind": conn.Kind, "personal": personal, "host": reviewHost(conn.BaseURL)}
	}
	routes := []gate.JSONRoute{{
		Slice: "services",
		Match: func(r *http.Request) bool {
			return (r.Method == http.MethodGet || r.Method == http.MethodPost) && pathIs(r, platformAPI+"/code-review")
		},
		Compute: func(r *http.Request) (int, any, http.Header) {
			if s, v, h, refused := refuse(r); refused {
				return s, v, h
			}
			var in struct {
				URL string                `json:"url"`
				Act connections.ReviewAct `json:"act"`
			}
			if r.Method == http.MethodGet {
				in.URL = r.URL.Query().Get("url")
			} else if !readSecretBody(r, &in) {
				return bad(r)
			}
			cs := connectionsFor(d)
			conn, personal, project, iid, why := target(cs, in.URL)
			if why == "no-connection" {
				return answerJSON(r, http.StatusNotFound, map[string]string{"error": why}, noStore)
			}
			if why != "" {
				return bad(r)
			}
			if r.Method == http.MethodPost && !personal {
				// Never written with the deployment's token: the person connects their own first.
				return answerJSON(r, http.StatusConflict, map[string]any{"error": "no-personal", "host": reviewHost(conn.BaseURL)}, noStore)
			}
			src, err := cs.ReviewOf(conn)
			if err != nil {
				return fail(r, err, map[string]any{"connection": about(conn, personal)})
			}
			ctx, cancel := context.WithTimeout(r.Context(), codeReviewTimeout)
			defer cancel()
			var rv *connections.Review
			if r.Method == http.MethodGet {
				rv, err = src.Review(ctx, project, iid)
			} else {
				rv, err = src.ReviewAct(ctx, project, iid, in.Act)
				agent.Log("info", "code review: an act", agent.F{K: "id", V: conn.ID}, agent.F{K: "op", V: in.Act.Op},
					agent.F{K: "ok", V: err == nil})
			}
			if err != nil {
				return fail(r, err, map[string]any{"connection": about(conn, personal)})
			}
			return answerJSON(r, http.StatusOK, map[string]any{"connection": about(conn, personal), "review": rv}, noStore)
		},
	}, {
		Slice: "services",
		Match: func(r *http.Request) bool {
			return r.Method == http.MethodPost && pathIs(r, platformAPI+"/code-review/open")
		},
		Compute: func(r *http.Request) (int, any, http.Header) {
			if s, v, h, refused := refuse(r); refused {
				return s, v, h
			}
			var in struct {
				Remote string `json:"remote"`
				connections.NewMergeRequest
			}
			if !readSecretBody(r, &in) {
				return bad(r)
			}
			host, project := chatwork.RemoteProject(in.Remote)
			if host == "" || project == "" {
				return bad(r)
			}
			cs := connectionsFor(d)
			conn, personal, ok := reviewConn(cs, host)
			if !ok {
				return answerJSON(r, http.StatusNotFound, map[string]string{"error": "no-connection", "host": host}, noStore)
			}
			if !personal {
				return answerJSON(r, http.StatusConflict, map[string]any{"error": "no-personal", "host": host}, noStore)
			}
			src, err := cs.ReviewOf(conn)
			if err != nil {
				return fail(r, err, nil)
			}
			ctx, cancel := context.WithTimeout(r.Context(), codeReviewTimeout)
			defer cancel()
			link, err := src.OpenMergeRequest(ctx, project, in.NewMergeRequest)
			agent.Log("info", "code review: a merge request opened", agent.F{K: "id", V: conn.ID}, agent.F{K: "ok", V: err == nil})
			if err != nil {
				return fail(r, err, nil)
			}
			return answerJSON(r, http.StatusOK, map[string]any{"url": link}, noStore)
		},
	}}
	return append(routes, githubPullsRoute(d, refuse, noStore))
}

// sameHost: two hosts are one (github.com is also www.github.com).
func sameHost(a, b string) bool {
	norm := func(h string) string { return strings.TrimPrefix(strings.ToLower(h), "www.") }
	return norm(a) == norm(b)
}

func reviewHost(base string) string {
	if u, err := url.Parse(base); err == nil {
		return u.Hostname()
	}
	return ""
}
