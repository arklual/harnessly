package api

import (
	"context"

	"github.com/ly-solutions/harnessly/internal/hub/designaccess"
	"github.com/ly-solutions/harnessly/internal/hub/searchsync"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/storehub"
	"github.com/ly-solutions/harnessly/internal/searchindex"
)

// newSearch connects the search from the database (docs/hub/search.md): the index in the hub's own database,
// kept up to date from the store's writes, and the route behind the switch hub-search. Without the store's views there
// is nothing to read the designs and the apps from, and the route answers 404 as it does with the switch off.
func (h *Hub) newSearch(view designaccess.Viewer, apps storehub.AppsViewer) {
	ix := &searchindex.SQL{DB: h.db, Postgres: h.db.Driver() == store.Postgres}
	if k, ok := apps.(designaccess.AppKinder); ok {
		h.Design.UseAppsCatalog(k, func(ctx context.Context, email, machineID string) bool {
			return h.Switches != nil && h.Switches.On(ctx, designaccess.AppsCatalogSwitch, email, machineID)
		})
	}
	lib, _ := view.(designaccess.Librarian)
	if lib == nil && apps == nil {
		return
	}
	s := &searchsync.Sync{Index: ix, Log: h.log, Projects: h.designProjectsOf, Record: h.Store.Record}
	if lib != nil {
		s.Library = lib
	}
	if apps != nil {
		s.Apps = apps
	}
	h.search = s
	h.Store.Observe(s.Hear)
	h.Design.UseSearch(ix, func(ctx context.Context, email, machineID string) bool {
		return h.Switches != nil && h.Switches.On(ctx, designaccess.SearchSwitch, email, machineID)
	})
}

// designProjectsOf are the projects the hub has rows for in a space, the ones the search lists.
func (h *Hub) designProjectsOf(ctx context.Context, spaceID string) ([]string, error) {
	rows, err := h.db.Query(ctx, `SELECT DISTINCT p.project_id FROM design_projects p JOIN machines m ON m.id = p.machine_id WHERE m.space_id = ?`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (h *Hub) runSearch(ctx context.Context) {
	if sql, ok := h.search.Index.(*searchindex.SQL); ok {
		if err := sql.Ensure(ctx); err != nil {
			h.log.Warn("search index: no trigram index", "error", err)
		}
	}
	go func() {
		ds, _ := h.Store.DesignSpaces(ctx)
		as, _ := h.Store.AppsSpaces(ctx)
		h.search.Backfill(ctx, ds, as)
	}()
	h.search.Run(ctx)
}
