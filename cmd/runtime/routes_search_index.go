package main

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/searchindex"
)

// HRN-1420, stage 3. GET <platform>/search?q= is ⌘K from an index in the runtime's own database (search.db in the data
// dir), the same route and answer the hub gives from its database, so the page asks one path and whoever is there
// answers. A runtime with no hub always serves it. The designs are put into the index as the library changes (its
// watcher), the apps' document types and documents, which only a request can read, when a question comes after they were
// last read ten seconds ago.

const (
	searchSpace    = "local"
	searchKindsTTL = 10 * time.Second
)

type runtimeSearch struct {
	ix  *searchindex.SQL
	lib *design.Library
	// person says whose question it is: the index holds every project of the machine, the answer only what they may see.
	person func(*http.Request) string

	mu      sync.Mutex
	dirty   map[string]bool
	all     bool
	timer   *time.Timer
	kindsAt time.Time
	watched bool
}

func indexSearchRoutes(d deps, f http.Handler) []gate.Route {
	api := designAPI(d)
	s := newRuntimeSearch(d.DataDir, api.L)
	if s != nil {
		s.person = api.PersonOf
	}
	return s.routes(f)
}

func newRuntimeSearch(dataDir string, lib *design.Library) *runtimeSearch {
	ix, _, err := searchindex.OpenSQLite(context.Background(), filepath.Join(dataDir, "search.db"))
	if err != nil {
		return nil
	}
	s := &runtimeSearch{ix: ix, lib: lib, dirty: map[string]bool{}, all: true}
	go s.flush()
	_, s.watched = lib.Subscribe(s.hear)
	return s
}

func (s *runtimeSearch) routes(f http.Handler) []gate.Route {
	if s == nil {
		return nil
	}
	path := platformAPI + "/search"
	return []gate.Route{{Slice: "design", Match: func(r *http.Request) bool {
		return r.Method == http.MethodGet && files.RouteIs(r, path)
	}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.serve(w, r, f) })}}
}

// hear marks the project a change is in; a change at the root (or a watcher that was lost) marks all.
func (s *runtimeSearch) hear(rel string) {
	first, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
	s.mu.Lock()
	if first == "" || first == "." || !design.IsLibraryID(first) {
		if first == "" || first == "." {
			s.all = true
		}
	} else {
		s.dirty[first] = true
	}
	if s.timer == nil {
		s.timer = time.AfterFunc(500*time.Millisecond, s.flush)
	}
	s.mu.Unlock()
}

func (s *runtimeSearch) flush() {
	s.mu.Lock()
	all, ids := s.all, s.dirty
	s.all, s.dirty, s.timer = false, map[string]bool{}, nil
	s.mu.Unlock()
	ctx := context.Background()
	if all {
		docs := []searchindex.Doc{}
		for _, a := range s.lib.ListArtifacts() {
			docs = append(docs, designDoc(a))
		}
		_ = s.ix.Replace(ctx, searchSpace, searchindex.KindDesign, "", docs)
		return
	}
	for id := range ids {
		if a := s.lib.ReadArtifact(id); a != nil {
			_ = s.ix.Put(ctx, designDoc(a))
		} else {
			_ = s.ix.Remove(ctx, searchSpace, searchindex.KindDesign, id)
		}
	}
}

func designDoc(a *design.Artifact) searchindex.Doc {
	title := a.Title
	if title == "" {
		title = a.ID
	}
	body := ""
	for _, b := range a.Boards {
		body += b.Title + " "
	}
	return searchindex.Doc{Space: searchSpace, Kind: searchindex.KindDesign, ID: a.ID, Parent: a.ID, Title: title, Body: body, UpdatedAt: a.UpdatedAt}
}

func (s *runtimeSearch) serve(w http.ResponseWriter, r *http.Request, f http.Handler) {
	if !s.watched {
		// No watcher: what changed on disk is not heard, so the designs are read again now and then.
		s.mu.Lock()
		if time.Since(s.kindsAt) > searchKindsTTL {
			s.all = true
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	pending := s.all || len(s.dirty) > 0
	s.mu.Unlock()
	if pending {
		s.flush()
	}
	s.refreshKinds(r, f)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	hits, err := s.ix.Search(r.Context(), searchindex.Query{Space: searchSpace, Text: r.URL.Query().Get("q"), Limit: limit, Allow: s.allow(r)})
	if err != nil {
		hits = nil
	}
	if hits == nil {
		hits = []searchindex.Hit{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"hits": hits})
}

// allow keeps the hits the person of r may see, as the library's list does (VisibleToFor): a design project or a
// document (a project of the library too) by their role in it; the apps' document types are not anyone's.
func (s *runtimeSearch) allow(r *http.Request) func(searchindex.Hit) bool {
	if s.lib == nil || !s.lib.Enforcing() {
		return nil
	}
	person := ""
	if s.person != nil {
		person = s.person(r)
	}
	return func(h searchindex.Hit) bool {
		switch h.Kind {
		case searchindex.KindDesign, searchindex.KindDoc:
			return s.lib.CanFor(h.ID, design.RoleView, person)
		}
		return true
	}
}

// refreshKinds puts the apps' document types and documents into the index; only a request can read them.
func (s *runtimeSearch) refreshKinds(r *http.Request, f http.Handler) {
	s.mu.Lock()
	if time.Since(s.kindsAt) < searchKindsTTL {
		s.mu.Unlock()
		return
	}
	s.kindsAt = time.Now()
	s.mu.Unlock()
	ctx := r.Context()
	if st, body := askSelf(f, r, homePart{name: "kinds", method: http.MethodGet, path: platformAPI + "/apps/kinds"}); st == 200 {
		var kinds []struct{ ID, Title, App, AppName string }
		if json.Unmarshal(unwrapList(body, "kinds"), &kinds) == nil {
			docs := make([]searchindex.Doc, 0, len(kinds))
			for _, k := range kinds {
				docs = append(docs, searchindex.Doc{Space: searchSpace, Kind: searchindex.KindKind, ID: k.ID, Parent: k.App, Title: k.Title, Body: k.AppName})
			}
			_ = s.ix.Replace(ctx, searchSpace, searchindex.KindKind, "", docs)
		}
	}
	if st, body := askSelf(f, r, homePart{name: "kindDocs", method: http.MethodGet, path: platformAPI + "/apps/kinds/docs"}); st == 200 {
		var list []struct {
			Document  string
			Title     string
			UpdatedAt int64
			Kind      string
			KindTitle string
		}
		if json.Unmarshal(unwrapList(body, "docs"), &list) == nil {
			docs := make([]searchindex.Doc, 0, len(list))
			for _, d := range list {
				docs = append(docs, searchindex.Doc{Space: searchSpace, Kind: searchindex.KindDoc, ID: d.Document, Parent: d.Kind, Title: d.Title, Body: d.KindTitle, UpdatedAt: d.UpdatedAt})
			}
			_ = s.ix.Replace(ctx, searchSpace, searchindex.KindDoc, "", docs)
		}
	}
}

// unwrapList reads a JSON array that may come bare or under a key.
func unwrapList(body []byte, key string) []byte {
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) == nil {
		if v, ok := m[key]; ok {
			return v
		}
		if v, ok := m["items"]; ok {
			return v
		}
	}
	return body
}
