package designaccess

import (
	"context"
	"net/http"
	"regexp"
	"sort"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The apps' document types and their documents from the hub's database: GET <platform api>/apps/kinds and
// /apps/kinds/docs, the shape of the machine's own answers. The types are read from the packages of the space's team in
// the store; the documents are the projects the person has a role in (the library's access, HRN-1411 included) whose kind
// is one of those types. With the switch off the hub leaves both to the machine, so the page's old path is the one it had.

// AppsCatalogSwitch is the rollout switch of the apps' types and documents from the hub's database.
const AppsCatalogSwitch = "hub-apps-catalog"

var appsKindsPath = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/apps/kinds(/docs)?$`)

// AppKinder lists the document types of the apps installed for a space (appsview.View).
type AppKinder interface {
	Kinds(ctx context.Context, spaceID string) ([]hubproto.AppKind, error)
}

// UseAppsCatalog connects the types' reader and the switch; set once, before the hub serves.
func (s *Service) UseAppsCatalog(k AppKinder, on func(ctx context.Context, email, machineID string) bool) {
	s.appKinds, s.appsCatalogOn = k, on
}

// ownKinds are the kinds of Sinopia's own: a project of one of them is not a document of an app's type.
var ownKinds = map[string]bool{"": true, "design": true, "site": true, "slides": true, "deck": true, "ds": true, "video": true}

// appsCatalogRoute answers the two routes; false leaves them to the machine.
func (s *Service) appsCatalogRoute(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine, docs bool) bool {
	email := normEmail(u.Email)
	if s.appKinds == nil || s.appsCatalogOn == nil || m.SpaceID == "" || !s.appsCatalogOn(r.Context(), email, m.ID) {
		return false
	}
	ctx := r.Context()
	kinds, err := s.appKinds.Kinds(ctx, m.SpaceID)
	if err != nil {
		s.writeError(w, err)
		return true
	}
	w.Header().Set("Cache-Control", "no-store")
	if !docs {
		writeJSON(w, http.StatusOK, map[string]any{"kinds": kinds})
		return true
	}
	titles := map[string]string{}
	extends := map[string]bool{}
	for _, k := range kinds {
		titles[k.ID] = k.Title
		extends[k.ID] = k.Extends != ""
	}
	t, err := s.read(ctx, m)
	if err != nil {
		s.writeError(w, err)
		return true
	}
	rows, err := t.projects()
	if err != nil {
		s.writeError(w, err)
		return true
	}
	type doc struct {
		Document  string `json:"document"`
		Title     string `json:"title"`
		UpdatedAt int64  `json:"updatedAt"`
		Kind      string `json:"kind"`
		KindTitle string `json:"kindTitle"`
		NeedsApp  bool   `json:"needsApp"`
	}
	out := []doc{}
	for _, p := range rows {
		f, err := t.fold(p, email)
		if err != nil {
			s.writeError(w, err)
			return true
		}
		if f.Role == "" {
			continue
		}
		mr, err := t.metaOf(p.ID)
		if err != nil || ownKinds[mr.Kind] || extends[mr.Kind] {
			continue
		}
		title, installed := titles[mr.Kind]
		if !installed {
			title = mr.Kind
		}
		name := mr.Title
		if name == "" {
			name = p.ID
		}
		out = append(out, doc{Document: p.ID, Title: name, UpdatedAt: mr.UpdatedAt, Kind: mr.Kind, KindTitle: title, NeedsApp: !installed})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	writeJSON(w, http.StatusOK, map[string]any{"docs": out})
	return true
}
