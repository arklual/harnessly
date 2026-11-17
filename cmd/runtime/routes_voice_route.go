package main

// The voice's router: a task the voice handed over goes here first, with the screen, the person's chats
// and what the page can do; one short run of a fast model answers the actions the page carries out (a page, the theme,
// the sidebar, a message to another chat, an answer to an agent's question) and the line the voice says. Work for the
// chat's agent comes back as an "agent" action, so nothing is lost when the router is not there.
//
//	POST <platform>/voice/route   router.Request -> router.Plan

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/journal"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/voice/router"
)

// voiceRouter asks the runtime's own ChatGPT sign-in first (the plan's lightest model, as quick edits do), and the
// Codex CLI signed in with the same plan when the runtime has none; RUNTIME_VOICE_ROUTER_MODEL names another model.
func voiceRouter(d deps) *router.Router {
	model := strings.TrimSpace(os.Getenv("RUNTIME_VOICE_ROUTER_MODEL"))
	gpt := chatgptService(d)
	own := func(ctx context.Context, instructions, prompt string) (string, error) {
		return gpt.Complete(ctx, model, instructions, prompt)
	}
	projects := filepath.Join(env("CLAUDE_CONFIG_DIR", filepath.Join(d.Home, ".claude")), "projects")
	return &router.Router{
		Ask:    router.First(own, router.CodexAsker("", model, "")),
		Recall: router.MemoryRecaller(projects),
	}
}

func voiceRouteRoutes(d deps) []gate.Route {
	var routes []gate.Route
	for _, base := range []string{platformAPI + "/voice/route", "/agents" + platformAPI + "/voice/route"} {
		routes = append(routes, gate.Route{
			Match: func(r *http.Request) bool { return r.URL.Path == base },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if sameorigin.Refuse(w, r) {
					return
				}
				if !d.SessionOK(r) {
					secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
					return
				}
				if r.Method != http.MethodPost {
					secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST"})
					return
				}
				var req router.Request
				if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil ||
					strings.TrimSpace(req.Task) == "" {
					secretJSON(w, http.StatusBadRequest, map[string]string{"error": "route_bad_request"})
					return
				}
				plan, err := voiceRouter(d).Route(r.Context(), req)
				voiceRouted(plan, err)
				if err != nil {
					secretJSON(w, http.StatusBadGateway, map[string]string{"error": "route_failed"})
					return
				}
				secretJSON(w, http.StatusOK, plan)
			}),
		})
	}
	return routes
}

// voiceRouted journals a plan (voice.route): its actions' kinds and its time, never the words.
func voiceRouted(p router.Plan, err error) {
	level, text := journal.Info, ""
	if err != nil {
		level, text = journal.Warn, "no plan: "+clip(err.Error(), 160)
	} else {
		kinds := make([]string, 0, len(p.Actions))
		for _, a := range p.Actions {
			kinds = append(kinds, a.Do)
		}
		text = "actions: " + strings.Join(kinds, ",")
		if len(kinds) == 0 {
			text = "say only"
		}
	}
	journal.Add(journal.Entry{At: time.Now().UnixMilli(), Level: level, Source: journal.SourceRuntime,
		Event: "voice.route", Text: text, Dur: p.Ms})
}
