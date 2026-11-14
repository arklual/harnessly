package designaccess

import (
	"context"
	"net/http"
	"regexp"
	"strconv"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/searchindex"
)

// ⌘K from the hub's database: GET <platform api>/search?q= under any machine of the space the person may open
// (it only names the space). What the index holds is read by no machine: the projects are the ones the person has a role
// in, folded exactly as the library does (so a project of a machine closed to them, HRN-1411, is not found), the rest of
// the space's things (apps, their documents) are open to its members.

// SearchSwitch is the rollout switch of the search from the database on the hub. A runtime without a hub always has it.
const SearchSwitch = "hub-search"

var searchPath = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/search$`)

// UseSearch connects the index and the switch; set once, before the hub serves. A nil index answers 404 search_off.
func (s *Service) UseSearch(ix searchindex.Index, on func(ctx context.Context, email, machineID string) bool) {
	s.search, s.searchOn = ix, on
}

func (s *Service) searchRoute(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine) {
	email := normEmail(u.Email)
	if s.search == nil || s.searchOn == nil || m.SpaceID == "" || !s.searchOn(r.Context(), email, m.ID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "search_off", "error": "Поиск из базы выключен"})
		return
	}
	ctx := r.Context()
	t, err := s.read(ctx, m)
	if err != nil {
		s.writeError(w, err)
		return
	}
	rows, err := t.projects()
	if err != nil {
		s.writeError(w, err)
		return
	}
	open := make(map[string]bool, len(rows))
	for _, p := range rows {
		f, err := t.fold(p, email)
		if err != nil {
			s.writeError(w, err)
			return
		}
		if f.Role != "" {
			open[p.ID] = true
		}
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	hits, err := s.search.Search(ctx, searchindex.Query{Space: m.SpaceID, Text: r.URL.Query().Get("q"), Limit: limit,
		Allow: func(h searchindex.Hit) bool { return h.Kind != searchindex.KindDesign || open[h.Parent] }})
	if err != nil {
		s.writeError(w, err)
		return
	}
	if hits == nil {
		hits = []searchindex.Hit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
}
