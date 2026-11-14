// Package searchsync keeps the hub's search index (internal/searchindex) as the store is written: a write
// to a project's folder marks the project, a write to a space's team file marks its apps, and a short while later the
// marked ones are read once, with the same code the library and the apps list use, and put into the index. Nothing is
// read when someone searches.
package searchsync

import (
	"context"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"log/slog"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/designaccess"
	"github.com/ly-solutions/harnessly/internal/hub/storehub"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/searchindex"
)

// teamStateFile is apps.TeamStateFile: the hub reads the name, not the apps package, which brings the design code
// with it and closes an import cycle with internal/agent's tests.
const teamStateFile = "team/state.json"

// Library lists projects of a space (designview.View).
type Library interface {
	Library(ctx context.Context, spaceID string, ids []string) ([]designaccess.LibraryItem, error)
}

// Apps lists a space's apps (appsview.View).
type Apps interface {
	SpaceApps(ctx context.Context, spaceID string) ([]hubproto.SpaceApp, error)
}

// Sync is the writer of the index.
type Sync struct {
	Index   searchindex.Index
	Library Library // nil: designs are not indexed
	Apps    Apps    // nil: apps are not indexed
	Log     *slog.Logger
	// Delay is how long a mark waits for more writes of the same project; 0 is two seconds.
	Delay time.Duration
	// Projects lists the projects of a space for the first build (the hub's rows); nil skips it.
	Projects func(ctx context.Context, spaceID string) ([]string, error)
	// Record reads the record of a library entity (a system, a video, a deck; HRN-1419): such an entity is listed by its
	// title even when the store has none of its files. Nil: the records are not read.
	Record func(ctx context.Context, spaceID, id string) (filestore.Record, bool, error)

	mu    sync.Mutex
	dirty map[string]*marks
	wake  chan struct{}
}

type marks struct {
	projects map[string]bool
	apps     bool
}

func (s *Sync) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Hear is the store's observer (storehub.Store.Observe).
func (s *Sync) Hear(spaceID, kind, path string, _ bool) {
	switch kind {
	case "design":
		if s.Library == nil {
			return
		}
		if id, ok := storehub.ProjectOf(path); ok {
			s.mark(spaceID, id, false)
		}
	case filestore.KindSystems, filestore.KindVideos, filestore.KindDecks:
		if s.Library == nil || s.Record == nil {
			return
		}
		if id, ok := filestore.RecordID(path); ok {
			s.mark(spaceID, id, false)
		}
	case "apps":
		if s.Apps != nil && path == teamStateFile {
			s.mark(spaceID, "", true)
		}
	}
}

func (s *Sync) mark(spaceID, project string, app bool) {
	s.mu.Lock()
	if s.dirty == nil {
		s.dirty = map[string]*marks{}
		s.wake = make(chan struct{}, 1)
	}
	m := s.dirty[spaceID]
	if m == nil {
		m = &marks{projects: map[string]bool{}}
		s.dirty[spaceID] = m
	}
	if project != "" {
		m.projects[project] = true
	}
	m.apps = m.apps || app
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run puts the marked things into the index until ctx ends.
func (s *Sync) Run(ctx context.Context) {
	s.mu.Lock()
	if s.dirty == nil {
		s.dirty = map[string]*marks{}
		s.wake = make(chan struct{}, 1)
	}
	s.mu.Unlock()
	delay := s.Delay
	if delay <= 0 {
		delay = 2 * time.Second
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		s.Flush(ctx)
	}
}

// Flush writes what is marked now.
func (s *Sync) Flush(ctx context.Context) {
	s.mu.Lock()
	work := s.dirty
	s.dirty = map[string]*marks{}
	s.mu.Unlock()
	for space, m := range work {
		if len(m.projects) > 0 {
			ids := make([]string, 0, len(m.projects))
			for id := range m.projects {
				ids = append(ids, id)
			}
			if err := s.Designs(ctx, space, ids); err != nil {
				s.log().Warn("search index: designs not updated", "space", space, "error", err)
				s.requeue(space, ids, false)
			}
		}
		if m.apps {
			if err := s.AppsOf(ctx, space); err != nil {
				s.log().Warn("search index: apps not updated", "space", space, "error", err)
				s.requeue(space, nil, true)
			}
		}
	}
}

func (s *Sync) requeue(space string, ids []string, app bool) {
	for _, id := range ids {
		s.mark(space, id, false)
	}
	if app {
		s.mark(space, "", true)
	}
}

// Designs reads the projects from the store and puts them into the index; one the store has no files of any more is
// taken out.
func (s *Sync) Designs(ctx context.Context, space string, ids []string) error {
	items, err := s.Library.Library(ctx, space, ids)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, it := range items {
		have[it.ID] = true
		title := it.Title
		if title == "" {
			title = it.ID
		}
		body := it.Kind
		for _, b := range it.Boards {
			body += " " + b.Title
		}
		if err := s.Index.Put(ctx, searchindex.Doc{Space: space, Kind: searchindex.KindDesign, ID: it.ID, Parent: it.ID,
			Title: title, Body: body, UpdatedAt: it.UpdatedAt}); err != nil {
			return err
		}
	}
	for _, id := range ids {
		if !have[id] && s.Record != nil {
			// A system, a video or a deck the store keeps a record of, with or without its files.
			if rec, ok, err := s.Record(ctx, space, id); err != nil {
				return err
			} else if ok {
				title := rec.Title
				if title == "" {
					title = id
				}
				if err := s.Index.Put(ctx, searchindex.Doc{Space: space, Kind: searchindex.KindDesign, ID: id, Parent: id,
					Title: title, Body: rec.Kind, UpdatedAt: rec.At}); err != nil {
					return err
				}
				continue
			}
		}
		if !have[id] {
			if err := s.Index.Remove(ctx, space, searchindex.KindDesign, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// AppsOf puts the space's apps into the index, replacing what it had.
func (s *Sync) AppsOf(ctx context.Context, space string) error {
	list, err := s.Apps.SpaceApps(ctx, space)
	if err != nil {
		return err
	}
	docs := make([]searchindex.Doc, 0, len(list))
	for _, a := range list {
		docs = append(docs, searchindex.Doc{Kind: searchindex.KindApp, ID: a.ID, Title: a.Name, Body: a.ID + " " + a.Version, UpdatedAt: time.Now().UnixMilli()})
	}
	return s.Index.Replace(ctx, space, searchindex.KindApp, "", docs)
}

// Backfill builds the index of the spaces from what the store has: at the hub's start, in the background, for a space
// with nothing in the index yet.
func (s *Sync) Backfill(ctx context.Context, designSpaces, appsSpaces []string) {
	for _, space := range designSpaces {
		if ctx.Err() != nil {
			return
		}
		if s.Library == nil || s.Projects == nil {
			break
		}
		if n, err := s.Index.Count(ctx, space); err != nil || n > 0 {
			continue
		}
		ids, err := s.Projects(ctx, space)
		if err != nil {
			s.log().Warn("search index: projects not listed", "space", space, "error", err)
			continue
		}
		for len(ids) > 0 {
			n := min(len(ids), 50)
			if err := s.Designs(ctx, space, ids[:n]); err != nil {
				s.log().Warn("search index: first build of designs failed", "space", space, "error", err)
				break
			}
			ids = ids[n:]
		}
	}
	for _, space := range appsSpaces {
		if ctx.Err() != nil || s.Apps == nil {
			return
		}
		if err := s.AppsOf(ctx, space); err != nil {
			s.log().Warn("search index: first build of apps failed", "space", space, "error", err)
		}
	}
}
