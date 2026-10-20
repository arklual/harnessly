package main

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/economics"
	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/history"
	"github.com/ly-solutions/harnessly/internal/hooksetup"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/state"
	"github.com/ly-solutions/harnessly/internal/statusline"
	"github.com/ly-solutions/harnessly/internal/subagents"
	"github.com/ly-solutions/harnessly/internal/timeline"
	"github.com/ly-solutions/harnessly/internal/transcript"
)

// providerLimits is a provider's subscription limits on this machine with their forecasts, as the machine snapshot
// shows them (a JSON value, nil for none). Until it is set the economics route stays with the previous runtime: an
// answer without the forecasts would not be the one the page knows.
var providerLimits func(provider string) any

// timelineRoutes are the timeline slice's HTTP routes: the statusline sink, the economics and subagents of a
// conversation, and the page of a transcript the cluster's copy lacks. They answer only while the core runs: the sink looks tabs up in its layouts and tells its timeline
// sockets.
func (c *core) timelineRoutes(f *gate.Gate, d deps) []gate.Route {
	claudeDir := env("CLAUDE_CONFIG_DIR", filepath.Join(d.Home, ".claude"))
	codexDir := env("CODEX_HOME", filepath.Join(d.Home, ".codex"))
	token := cliTokenFirst(d.DataDir, d.TokenHeaders)
	// The previous runtime's API let in the cli-token (the first of its headers present) or a page session.
	authorized := func(r *http.Request) bool { return token(r) || d.SessionOK(r) }
	stats := &history.Stats{Dir: filepath.Join(d.DataDir, "session-stats")}
	sink := &statusline.Sink{Authorized: authorized, TokenOK: token, Home: d.Home, ClaudeDir: claudeDir, Limits: c.limits, Stats: stats,
		Ultracode: c.ultracodeSession, Broadcast: c.broadcastSessionStats, Script: filepath.Join(d.DataDir, hooksetup.StatuslineFile)}
	econ := &economics.Route{Store: &economics.Store{Dir: filepath.Join(d.DataDir, "economics")}, Stats: stats, Authorized: authorized,
		SessionOK: d.SessionOK, FindTab: c.layoutTabByID, Limits: func(p string) any { return providerLimits(p) }}
	subs := &subagents.Route{Lister: &subagents.Lister{}, Authorized: authorized, SessionOK: d.SessionOK,
		Paths: subagents.Paths{ClaudeProjects: filepath.Join(claudeDir, "projects"), CodexSessions: filepath.Join(codexDir, "sessions")}}
	entries := &timeline.EntriesRoute{Paths: subs.Paths, Authorized: authorized,
		Codex: transcript.CodexOptions{ImageURL: transcript.NewCodexImageURL(c.cfg.Timeline.UploadsDir, c.cfg.Timeline.AppBase)}}
	at := func(r *http.Request, p string) bool { return c.manager() != nil && (pathIs(r, p) || pathIs(r, p+"/")) }
	return append([]gate.Route{{
		Slice:   "timeline",
		Match:   func(r *http.Request) bool { return at(r, "/api/status/statusline") },
		Handler: sink,
	}}, f.AsRoutes(
		gate.JSONRoute{
			Slice:   "timeline",
			Match:   func(r *http.Request) bool { return providerLimits != nil && at(r, platformAPI+"/economics") },
			Compute: econ.Answer,
			Volatile: []string{"conversation.computedAt", "limits.fiveHour.forecast", "limits.sevenDay.forecast", "limits.extra.*.forecast",
				"limits.fiveHour.sampledAt", "limits.sevenDay.sampledAt", "limits.extra.*.sampledAt"},
		},
		gate.JSONRoute{
			Slice:   "timeline",
			Match:   func(r *http.Request) bool { return at(r, platformAPI+"/subagents") },
			Compute: subs.Answer,
		},
		gate.JSONRoute{
			Slice: "timeline",
			Match: func(r *http.Request) bool { _, ok := subagentOf(r); return ok && c.manager() != nil },
			Compute: func(r *http.Request) (int, any, http.Header) {
				id, _ := subagentOf(r)
				return subs.AnswerTranscript(r, id)
			},
		},
		gate.JSONRoute{
			Slice:   "timeline",
			Match:   func(r *http.Request) bool { return at(r, "/api/timeline/entries") && entries.Takes(r) },
			Compute: entries.Answer,
		},
	)...)
}

// subagentOf is the id in <platform API>/subagents/<id>, decoded as the previous runtime's route table did.
func subagentOf(r *http.Request) (string, bool) {
	parts, _, ok := files.APIParts(r)
	want := strings.Split(strings.Trim(platformAPI, "/"), "/")
	if !ok || len(parts) != len(want)+2 || parts[len(want)] != "subagents" {
		return "", false
	}
	for i, p := range want {
		if parts[i] != p {
			return "", false
		}
	}
	return parts[len(parts)-1], true
}

// layoutTabByID is a tab of the layouts by its id, as stored, in workspace order.
func (c *core) layoutTabByID(tabID string) (*jsvalue.Object, bool) {
	for _, ws := range (coreLayouts{store: c.store}).Workspaces() {
		for _, t := range state.AllTabs(c.store.Layout(ws.ID)) {
			if t.String("id") == tabID {
				return t, true
			}
		}
	}
	return nil, false
}

// ultracodeSession says whether an agent session belongs to a tab whose remembered launch asked for ultracode.
func (c *core) ultracodeSession(sessionID any) bool {
	key := c.cfg.MetaKey
	if key == "" {
		return false
	}
	for _, ws := range (coreLayouts{store: c.store}).Workspaces() {
		for _, t := range state.AllTabs(c.store.Layout(ws.ID)) {
			id, _ := t.Object("agentState").Get("sessionId")
			if !jsvalue.Truthy(id) || !sameKey(id, sessionID) {
				continue
			}
			if effort, _ := t.Object(key).Object("launch").Get("effort"); effort == "ultracode" {
				return true
			}
		}
	}
	return false
}

// sameKey is how a JavaScript Set compares two JSON values: objects only to themselves.
func sameKey(a, b any) bool {
	switch a.(type) {
	case string, float64, bool:
		return a == b
	}
	return false
}
