package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The registry of projects: the runtime tells its agent about each change over the loopback (POST
// /projects/event), the agent keeps the last list and passes the events to the hubs that subscribed. A hub that
// subscribes later (a new link, a restarted hub) gets the whole list at once, so its registry never needs the runtime.

const projectsEventTO = 3 * time.Second

// projectsCache is the machine's list as the runtime last told it: the full list with the events applied after it.
type projectsCache struct {
	mu    sync.Mutex
	known bool
	list  []hubproto.Project
}

// apply takes one event; it reports whether the event is well formed.
func (c *projectsCache) apply(e hubproto.ProjectsEvent) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch e.Kind {
	case hubproto.ProjectsFull:
		c.list, c.known = slices.Clone(e.Projects), true
		return true
	case hubproto.ProjectsCreated, hubproto.ProjectsRenamed, hubproto.ProjectsPath, hubproto.ProjectsDeleted:
		if e.Project == nil || e.Project.ID == "" {
			return false
		}
		i := slices.IndexFunc(c.list, func(p hubproto.Project) bool { return p.ID == e.Project.ID })
		switch {
		case e.Kind == hubproto.ProjectsDeleted:
			if i >= 0 {
				c.list = slices.Delete(c.list, i, i+1)
			}
		case i >= 0:
			c.list[i] = *e.Project
		default:
			c.list = append(c.list, *e.Project)
		}
		return true
	}
	return false
}

// full is the list to tell a hub that has just subscribed; ok is false until the runtime told it once.
func (c *projectsCache) full() (hubproto.ProjectsEvent, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return hubproto.ProjectsEvent{Kind: hubproto.ProjectsFull, Projects: slices.Clone(c.list)}, c.known
}

// projectsServed: an observing link is not told the registry.
func (h *hub) projectsServed() bool { return !h.observe }

// sendProjectsFull tells one link the whole list.
func (h *hub) sendProjectsFull(l *link) {
	e, ok := h.projects.full()
	if !ok || !l.subscribed(hubproto.EventProjects) {
		return
	}
	frame, err := marshal(map[string]any{"event": hubproto.EventProjects, "projects": e})
	if err != nil {
		return
	}
	l.sendEvent(hubproto.EventProjects, frame)
}

// projectsEventRoute is POST /projects/event {kind, project | projects}, from the runtime of this machine.
func (a *Agent) projectsEventRoute(w http.ResponseWriter, r *http.Request) {
	if !tokenMatches(r.Header.Get(a.cfg.LocalTokenHeader), a.localToken) {
		reply(w, 403, map[string]string{"error": "no local agent token (agent.json in the data directory)"})
		return
	}
	var e hubproto.ProjectsEvent
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&e) != nil {
		reply(w, 400, map[string]string{"error": "kind and project (or projects) are required"})
		return
	}
	h := a.hub
	if h == nil {
		reply(w, 202, map[string]bool{"ok": true, "sent": false})
		return
	}
	if !h.projects.apply(e) {
		reply(w, 400, map[string]string{"error": "kind (projects.full, project.created, project.renamed, project.path or project.deleted) and a project id are required"})
		return
	}
	sent := h.projectsServed() && h.emitSubscribed(hubproto.EventProjects, "projects", e)
	reply(w, 202, map[string]bool{"ok": true, "sent": sent})
}

// EmitProjectsEvent asks the machine agent whose agent.json lies in dataDir to pass a registry event to the hubs. It
// never blocks the caller and never reports a failure: no agent, no event.
func EmitProjectsEvent(dataDir, tokenHeader string, e hubproto.ProjectsEvent) {
	if dataDir == "" {
		return
	}
	go func() {
		port, token, ok := localAgent(dataDir)
		if !ok {
			return
		}
		body, _ := json.Marshal(e)
		ctx, cancel := context.WithTimeout(context.Background(), projectsEventTO)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:"+port+"/projects/event", bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("content-type", "application/json")
		if tokenHeader == "" {
			tokenHeader = "x-agent-local-token"
		}
		req.Header.Set(tokenHeader, token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			Log("warn", "projects event not handed to the agent", F{"kind", e.Kind}, F{"error", err.Error()})
			return
		}
		res.Body.Close()
	}()
}
