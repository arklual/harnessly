// Package projreg keeps the registry of the projects (workspaces) the machines told the hub of: a full list
// when a machine connects and an event on each create, rename, delete and change of folders. The sidebar «Проекты»
// reads it through the proxy's GET /api/me/projects, filtered by what the person may open.
package projreg

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Switch is the rollout switch of the hub's sidebar «Проекты».
const Switch = "hub-projects"

// Service keeps the registry.
type Service struct {
	DB  *store.DB
	Log *slog.Logger
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func clip(v string, n int) string {
	v = strings.TrimSpace(v)
	if r := []rune(v); len(r) > n {
		return string(r[:n])
	}
	return v
}

// clean makes a project the hub keeps: bounded, no empty folder, nil for a project with no id.
func clean(machineID string, p hubproto.Project, at int64) *store.MachineProject {
	id := clip(p.ID, 128)
	if id == "" {
		return nil
	}
	out := &store.MachineProject{MachineID: machineID, ID: id, Name: clip(p.Name, hubproto.MaxProjectNameLen), UpdatedAt: at}
	for _, d := range p.Directories {
		if d = clip(d, hubproto.MaxProjectDirLen); d != "" && len(out.Directories) < hubproto.MaxProjectDirs {
			out.Directories = append(out.Directories, d)
		}
	}
	return out
}

// Event applies a machine's event. A machine tells only of its own projects: the machine is the link's, never named by
// the event.
func (s *Service) Event(m store.Machine, e hubproto.ProjectsEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := s.Apply(ctx, m.ID, e); err != nil && s.Log != nil {
		s.Log.Warn("projects event not kept", "machine", m.ID, "kind", e.Kind, "error", err)
	}
}

// Apply keeps one event of the machine.
func (s *Service) Apply(ctx context.Context, machineID string, e hubproto.ProjectsEvent) error {
	at := store.Ms(s.now())
	switch e.Kind {
	case hubproto.ProjectsFull:
		var list []store.MachineProject
		for i, p := range e.Projects {
			if i >= hubproto.MaxProjects {
				break
			}
			if c := clean(machineID, p, at); c != nil {
				list = append(list, *c)
			}
		}
		return s.DB.Tx(ctx, func(tx store.Store) error { return tx.ReplaceMachineProjects(ctx, machineID, list) })
	case hubproto.ProjectsCreated, hubproto.ProjectsRenamed, hubproto.ProjectsPath:
		if e.Project == nil {
			return nil
		}
		if c := clean(machineID, *e.Project, at); c != nil {
			return s.DB.S().PutMachineProject(ctx, *c)
		}
	case hubproto.ProjectsDeleted:
		if e.Project != nil && e.Project.ID != "" {
			return s.DB.S().DeleteMachineProject(ctx, machineID, clip(e.Project.ID, 128))
		}
	}
	return nil
}
