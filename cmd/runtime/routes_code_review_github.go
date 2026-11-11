package main

import (
	"context"
	"net/http"
	"strconv"

	"github.com/ly-solutions/harnessly/internal/connections"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The open pull requests of the person's GitHub for the start of the «Ревью кода» app, as rows of the same shape as
// GitLab's merge requests, so the lists hold both:
//
//	GET <platform>/code-review/github → {state: "ready", host, me, rows} | {state: "off"|"failed", reason, host?}
//
// Read through the person's own GitHub connection first, else the deployment's; without one the answer is off and the
// start stays as it is.

// githubListConn is the GitHub connection to list with: the person's own (the primary first), else the deployment's.
func githubListConn(list []connections.Conn) (connections.Conn, bool) {
	var own, env []connections.Conn
	for _, c := range list {
		switch {
		case c.Kind != connections.GitHub:
		case c.Env:
			env = append(env, c)
		case c.Primary:
			own = append([]connections.Conn{c}, own...)
		default:
			own = append(own, c)
		}
	}
	if all := append(own, env...); len(all) > 0 {
		return all[0], true
	}
	return connections.Conn{}, false
}

// pullRow is a pull request as a row of the lists, with what it waits for, as GitLab's rows say it.
func pullRow(p connections.PullSummary) (wire.AppRow, bool) {
	if p.URL == "" || p.Number < 1 || p.Title == "" {
		return wire.AppRow{}, false
	}
	state := "opened"
	m := wire.AppMR{Draft: p.Draft, Conflicts: p.Conflicts, Comments: max(0, p.Comments)}
	if p.Source != "" {
		m.Source = &p.Source
	}
	switch p.Checks {
	case "success":
		m.Pipeline = rowPtr(wire.PipelineSuccess)
	case "failed":
		m.Pipeline = rowPtr(wire.PipelineFailed)
	case "pending":
		m.Pipeline = rowPtr(wire.PipelinePending)
	}
	row := wire.AppRow{Key: p.URL, ID: "#" + strconv.Itoa(p.Number), Title: p.Title, URL: p.URL, State: &state,
		CreatedAt: max(0, p.Created.UnixMilli()), UpdatedAt: max(0, p.Updated.UnixMilli()), Assignees: p.Assignees,
		Reviewers: p.Reviewers, Me: []wire.AppRole{}, Labels: p.Labels, Children: []wire.AppChild{}, MR: &m}
	if p.Project != "" {
		row.Project = &p.Project
	}
	if p.Target != "" {
		row.Target = &p.Target
	}
	if p.Author != nil {
		row.Author = &p.Author.Name
		row.Person = row.Author
	}
	if p.Mine {
		row.Me = append(row.Me, wire.RoleAuthor)
	}
	if p.Assigned {
		row.Me = append(row.Me, wire.RoleAssignee)
	}
	if p.Asked {
		row.Me = append(row.Me, wire.RoleReviewer)
	}
	var flag wire.AppFlag
	switch {
	case p.Asked && !p.Mine:
		flag = wire.FlagReview
	case p.Conflicts:
		flag = wire.FlagConflict
	case p.Checks == "failed":
		flag = wire.FlagCIFailed
	case len(p.Reviewers) == 0 && !p.Draft:
		flag = wire.FlagNoReviewer
	default:
		flag = wire.FlagOpen
	}
	row.Flag = &flag
	switch flag {
	case wire.FlagReview:
		row.Tone = rowPtr(wire.ToneAccent)
	case wire.FlagConflict, wire.FlagCIFailed:
		row.Tone = rowPtr(wire.ToneWarn)
	}
	return row, true
}

func rowPtr[T any](v T) *T { return &v }

func githubPullsRoute(d deps, refuse func(*http.Request) (int, any, http.Header, bool), noStore http.Header) gate.JSONRoute {
	return gate.JSONRoute{
		Slice: "services",
		Match: func(r *http.Request) bool {
			return r.Method == http.MethodGet && pathIs(r, platformAPI+"/code-review/github")
		},
		Compute: func(r *http.Request) (int, any, http.Header) {
			if s, v, h, refused := refuse(r); refused {
				return s, v, h
			}
			cs := connectionsFor(d)
			list, err := cs.List()
			if err != nil {
				return answerJSON(r, http.StatusOK, map[string]any{"state": "failed", "reason": "unreachable"}, noStore)
			}
			conn, ok := githubListConn(list)
			if !ok {
				return answerJSON(r, http.StatusOK, map[string]any{"state": "off", "reason": "not-configured"}, noStore)
			}
			host := reviewHost(conn.BaseURL)
			off := func(err error) (int, any, http.Header) {
				reason := "unreachable"
				if connReason(err) == "denied" {
					reason = "denied"
				}
				return answerJSON(r, http.StatusOK, map[string]any{"state": "failed", "reason": reason, "host": host}, noStore)
			}
			cl, err := cs.Open(conn)
			if err != nil {
				return off(err)
			}
			lister, ok := cl.(connections.PullLister)
			if !ok {
				return answerJSON(r, http.StatusOK, map[string]any{"state": "off", "reason": "not-configured"}, noStore)
			}
			ctx, cancel := context.WithTimeout(r.Context(), codeReviewTimeout)
			defer cancel()
			me, pulls, err := lister.OpenPulls(ctx)
			if err != nil {
				return off(err)
			}
			rows := []wire.AppRow{}
			for _, p := range pulls {
				if row, ok := pullRow(p); ok {
					rows = append(rows, row)
				}
			}
			var name *string
			if me != nil {
				name = &me.Name
			}
			return answerJSON(r, http.StatusOK, map[string]any{"state": "ready", "host": host, "me": name, "rows": rows}, noStore)
		},
	}
}
