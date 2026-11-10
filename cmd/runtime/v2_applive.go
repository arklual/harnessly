package main

import (
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Apps turned on in a chat whose agent already runs (HRN-975, internal/apps live.go): which of them the agent can use
// as it runs, what its next turn is told of them, and whether a change of a chat's apps needs a new start of its agent.

// liveApps answers for the chats of this runtime; liveAppsOf is the one the routes, the host tools and the chats share.
type liveApps struct {
	reg *apps.Registry
	// launched are the apps the agent of a tmux session started with (its tab's launch).
	launched func(session string) []string
	// requireWall: the deployment requires the wall around the apps (RUNTIME_APPS_REQUIRE_WALL=1).
	requireWall bool

	mu sync.Mutex
	// told are the apps each chat's agent was told of by the live hook, by conversation.
	told map[string][]string
}

var liveAppsOf = &liveApps{}

func (l *liveApps) init(reg *apps.Registry, c *core) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reg = reg
	l.launched = func(session string) []string { return c.tabLaunch(session).Apps }
	l.requireWall = os.Getenv("RUNTIME_APPS_REQUIRE_WALL") == "1"
}

func (l *liveApps) startedWith(session string) []string {
	if l.launched == nil || session == "" {
		return nil
	}
	return l.launched(session)
}

// refusal is why the agent of a chat that started with launched cannot use the app as it runs; "" when it can.
func (l *liveApps) refusal(app string, launched []string) string {
	if slices.Contains(launched, app) || l.reg == nil {
		return ""
	}
	pkg, err := l.reg.Package(app)
	if err != nil || pkg.Manifest == nil {
		return app + " is not installed"
	}
	if l.requireWall && len(launched) == 0 {
		// A launch with apps has the wall or does not start (Launcher.RequireWall); one without them has none.
		return apps.WallRefusal(pkg.Manifest.Name)
	}
	if needs := apps.LaunchNeeds(pkg); needs != "" {
		return apps.LiveRefusal(pkg.Manifest.Name, needs)
	}
	return ""
}

// hostRefusal is HostTools.Live for the chat of a session.
func (l *liveApps) hostRefusal(session string) func(app string) string {
	return func(app string) string { return l.refusal(app, l.startedWith(session)) }
}

// changeIsLive says whether a Claude Code chat's agent takes the apps ids as it runs: every app it did not start with
// can go live, and none it started with is turned off (its plugin and words stay with the agent until it starts again).
// false makes the chat stop an agent at rest, so the next message brings it back with the apps.
func (l *liveApps) changeIsLive(rec wire.Conversation, ids []string) bool {
	if rec.Provider != wire.ProviderClaude || rec.Session == "" {
		return false
	}
	launched := l.startedWith(rec.Session)
	for _, id := range launched {
		if !slices.Contains(ids, id) {
			return false
		}
	}
	for _, id := range ids {
		if l.refusal(id, launched) != "" {
			return false
		}
	}
	return true
}

// notice is the live hook's text for the next turn of a chat's agent: the apps turned on since it was last told (their
// words), the ones that wait for a new start (why), and the ones turned off. It remembers what it told.
func (l *liveApps) notice(rec wire.Conversation) string {
	launched := l.startedWith(rec.Session)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.told == nil {
		l.told = map[string][]string{}
	}
	told := l.told[rec.ID]
	var on, waiting, off, now []string
	for _, id := range rec.Apps {
		if slices.Contains(launched, id) {
			continue
		}
		now = append(now, id)
		if slices.Contains(told, id) {
			continue
		}
		if why := l.refusal(id, launched); why != "" {
			waiting = append(waiting, why)
			continue
		}
		a, err := l.reg.Get(id)
		if err != nil || a.Manifest == nil {
			continue
		}
		brief, err := apps.LiveBrief(a)
		if err != nil {
			continue
		}
		on = append(on, brief)
	}
	for _, id := range told {
		if !slices.Contains(rec.Apps, id) {
			off = append(off, l.name(id))
		}
	}
	sort.Strings(now)
	if len(now) == 0 {
		delete(l.told, rec.ID)
	} else {
		l.told[rec.ID] = now
	}
	return apps.LiveNotice(on, waiting, off)
}

func (l *liveApps) name(id string) string {
	if a, err := l.reg.Get(id); err == nil && a.Manifest != nil && a.Manifest.Name != "" {
		return a.Manifest.Name + " (" + id + ")"
	}
	return id
}

// appLiveRoutes: POST /api/cli/apps/live-context {session} is the live hook's question (harnessly hook app-context
// --live): {"text": …}, empty when the chat's apps did not change since the agent was last told.
func appLiveRoutes(d deps, reg *apps.Registry, chats appChats) []gate.Route {
	live := func(w http.ResponseWriter, r *http.Request) {
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
		}
		if !readSecretBody(r, &b) || strings.TrimSpace(b.Session) == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		conv := chats.ofSession(b.Session)
		// The agent of a launch with apps asks under that launch's token: only of its own chat (the launch's grant).
		if launch, byAgent := d.CLI.AgentOf(r); byAgent {
			if _, own := reg.Grant(launch); own == "" || own != conv {
				secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
				return
			}
		}
		rec, ok := chats.record(conv)
		if !ok {
			secretJSON(w, http.StatusOK, map[string]string{"text": ""})
			return
		}
		secretJSON(w, http.StatusOK, map[string]string{"text": liveAppsOf.notice(rec)})
	}
	return []gate.Route{{Match: func(r *http.Request) bool { return pathIs(r, "/api/cli/apps/live-context") }, Handler: http.HandlerFunc(live)}}
}

// appToolsWatch is how often the GET stream of the harnessly-app server looks at the apps on in its chat.
const appToolsWatch = time.Second

// appToolStreamRoutes: GET on the runtime's MCP route with ?server=harnessly-app is the Streamable HTTP stream the
// client opens after initialize. The server tells it there that its tools changed (notifications/tools/list_changed)
// when the apps on in the chat change, so the running agent asks tools/list again and has the apps' tools (HRN-975).
// The stream stays open with a comment every 15 s; a caller the runtime cannot place in a chat gets 405, which the
// client takes for "no stream".
func appToolStreamRoutes(d deps) []gate.Route {
	stream := func(w http.ResponseWriter, r *http.Request) {
		if d.CLI == nil || !d.CLI.OK(r) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return
		}
		var host apps.HostTools
		placed := false
		if more := previewOf(d).appTools.Load(); more != nil {
			host, placed = (*more)(r).(apps.HostTools)
		}
		flusher, canFlush := w.(http.Flusher)
		if !placed || host.Enabled == nil || !canFlush {
			w.Header().Set("Allow", "POST")
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/event-stream; charset=utf-8")
		h.Set("Cache-Control", "no-store, no-transform")
		h.Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(": ok\n\n"))
		flusher.Flush()
		seen := host.Offering()
		look := time.NewTicker(appToolsWatch)
		defer look.Stop()
		beat := time.NewTicker(15 * time.Second)
		defer beat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-look.C:
				now := host.Offering()
				if now == seen {
					continue
				}
				seen = now
				_, _ = w.Write(append(append([]byte("event: message\ndata: "), mcpserver.ToolsListChanged()...), '\n', '\n'))
				flusher.Flush()
			case <-beat.C:
				_, _ = w.Write([]byte(": ping\n\n"))
				flusher.Flush()
			}
		}
	}
	match := func(r *http.Request) bool {
		return r.Method == http.MethodGet && r.URL.Query().Get("server") == apps.HostServer && files.RouteIs(r, "/api/cli/preview/mcp")
	}
	return []gate.Route{{Slice: "preview", Match: match, Handler: http.HandlerFunc(stream)}}
}
