package api

import (
	"net/http"
	"os"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
)

// extUIOn says whether the hub draws the views and the branding of its extensions: HARNESSLY_UI_SLOTS=on, the flag of
// the runtime's slots (docs/apps.md), and an extensions registry to take them from.
func (h *Hub) extUIOn() bool {
	return h.Ext != nil && strings.EqualFold(os.Getenv("HARNESSLY_UI_SLOTS"), "on")
}

// extUI answers GET /api/ext/ui: the views and the branding the extensions file lists. Like /api/capabilities it is
// open, because the branding shows before sign-in; it names no person and no secret (the file is the operator's, and a
// view's address is the one the page frames). With the flag off the route is not there.
func (h *Hub) extUI(w http.ResponseWriter, r *http.Request) error {
	if !h.extUIOn() {
		http.NotFound(w, r)
		return nil
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, h.Ext.UI())
	return nil
}
