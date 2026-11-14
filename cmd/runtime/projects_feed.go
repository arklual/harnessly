package main

import (
	"os"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// projectsFeed tells the machine agent about the machine's projects: each create, rename, folder change and delete as
// one event, and the whole list when the runtime starts and every few minutes (a restarted agent or hub learns it
// from the list). Without a machine agent nothing is sent; the runtime's own store stays the truth of a runtime
// that has no hub.
type projectsFeed struct {
	source func() []wire.Project
	dir    string
	token  string

	mu   sync.Mutex
	last map[string]hubproto.Project
	sent bool
}

// projectsResend is how often the whole list is told again.
const projectsResend = 5 * time.Minute

func newProjectsFeed(source func() []wire.Project, dataDir, tokenHeader string) *projectsFeed {
	return &projectsFeed{source: source, dir: dataDir, token: tokenHeader}
}

// changed is convo.Deps.ProjectsChanged: it only starts the diff, off the caller's goroutine.
func (f *projectsFeed) changed() {
	if f == nil || os.Getenv("RUNTIME_PROJECTS_FEED") == "off" {
		return
	}
	go f.sync(false)
}

// run tells the whole list at the start and then every projectsResend.
func (f *projectsFeed) run() {
	if f == nil || os.Getenv("RUNTIME_PROJECTS_FEED") == "off" {
		return
	}
	f.sync(true)
	for range time.Tick(projectsResend) {
		f.sync(true)
	}
}

func (f *projectsFeed) list() []hubproto.Project {
	var out []hubproto.Project
	for _, p := range f.source() {
		if p.ID == "" {
			continue
		}
		hp := hubproto.Project{ID: p.ID, Name: p.Name}
		if p.Path != "" {
			hp.Directories = []string{p.Path}
		}
		out = append(out, hp)
	}
	return out
}

func same(a, b hubproto.Project) bool {
	if a.Name != b.Name || len(a.Directories) != len(b.Directories) {
		return false
	}
	for i := range a.Directories {
		if a.Directories[i] != b.Directories[i] {
			return false
		}
	}
	return true
}

// sync sends what changed since the last call; with full, the whole list.
func (f *projectsFeed) sync(full bool) {
	list := f.list()
	f.mu.Lock()
	defer f.mu.Unlock()
	now := map[string]hubproto.Project{}
	for _, p := range list {
		now[p.ID] = p
	}
	if full || !f.sent {
		f.last, f.sent = now, true
		agent.EmitProjectsEvent(f.dir, f.token, hubproto.ProjectsEvent{Kind: hubproto.ProjectsFull, Projects: list})
		return
	}
	for _, e := range projectDiff(f.last, list) {
		agent.EmitProjectsEvent(f.dir, f.token, e)
	}
	f.last = now
}

// projectDiff is the events that turn the list `last` into `list`.
func projectDiff(last map[string]hubproto.Project, list []hubproto.Project) []hubproto.ProjectsEvent {
	var out []hubproto.ProjectsEvent
	seen := map[string]bool{}
	for _, p := range list {
		seen[p.ID] = true
		old, had := last[p.ID]
		switch {
		case !had:
			out = append(out, hubproto.ProjectsEvent{Kind: hubproto.ProjectsCreated, Project: &p})
		case old.Name != p.Name:
			out = append(out, hubproto.ProjectsEvent{Kind: hubproto.ProjectsRenamed, Project: &p})
		case !same(old, p):
			out = append(out, hubproto.ProjectsEvent{Kind: hubproto.ProjectsPath, Project: &p})
		}
	}
	for id := range last {
		if !seen[id] {
			out = append(out, hubproto.ProjectsEvent{Kind: hubproto.ProjectsDeleted, Project: &hubproto.Project{ID: id}})
		}
	}
	return out
}
