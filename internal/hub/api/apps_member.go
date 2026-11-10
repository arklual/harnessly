package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/storehub"
)

// HRN-898: the apps installed for a space open at the hub for its members with every machine off
// (/apps/space/<space id>/<app id>/, internal/hub/appsview). The session and the role are checked here; a page asked
// without a session leads to the sign-in and back.

func (h *Hub) appsMember(v storehub.AppsViewer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := h.Auth.Authenticate(r)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		if p == nil {
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				http.Redirect(w, r, h.cfg.BasePath+"/signin?next="+url.QueryEscape(r.URL.Path), http.StatusFound)
				return
			}
			httpx.WriteError(w, r, httpx.ErrUnauthenticated)
			return
		}
		spaceID, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/apps/space/"), "/")
		if spaceID == "" {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
		if _, err := h.Spaces.Access(r.Context(), p.User.ID, spaceID); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		v.ServeMember(w, r, spaceID, storehub.PersonName(p.User.ID))
	}
}

func (h *Hub) spaceApps(v storehub.AppsViewer) func(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	return func(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
		spaceID := r.PathValue("spaceId")
		if _, err := h.Spaces.Access(r.Context(), p.User.ID, spaceID); err != nil {
			return err
		}
		list, err := v.SpaceApps(r.Context(), spaceID)
		if err != nil {
			return err
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"apps": list})
		return nil
	}
}
