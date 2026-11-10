package api

import (
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/switches"
)

// The rollout switches' administration (docs/hub/rollout-switches.md, HRN-979). A switch belongs to the installation,
// not to an organization, so only an administrator of the installation reads and changes them.

type switchWire struct {
	Feature   string `json:"feature"`
	Person    string `json:"person"`  // an email, or "*"
	Machine   string `json:"machine"` // a machine id, or "*"
	Enabled   bool   `json:"enabled"`
	Note      string `json:"note"`
	UpdatedBy string `json:"updatedBy"`
	UpdatedAt string `json:"updatedAt"`
}

func (h *Hub) mountSwitches(mux *http.ServeMux) {
	admin := func(f func(http.ResponseWriter, *http.Request, *auth.Principal) error) httpx.Handler {
		return h.Auth.Require(func(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
			if !p.Admin {
				return httpx.ErrForbidden
			}
			return f(w, r, p)
		})
	}
	mux.Handle("GET /api/admin/switches", admin(func(w http.ResponseWriter, r *http.Request, _ *auth.Principal) error {
		rows, err := h.Switches.List(r.Context(), r.URL.Query().Get("prefix"))
		if err != nil {
			return err
		}
		out := make([]switchWire, 0, len(rows))
		for _, s := range rows {
			out = append(out, switchWire{s.Feature, s.Person, s.Machine, s.Enabled, s.Note, s.UpdatedBy, httpx.Time(s.UpdatedAt)})
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"switches": out})
		return nil
	}))
	mux.Handle("PUT /api/admin/switches", admin(func(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
		var in switchWire
		if err := httpx.Decode(r, &in); err != nil {
			return err
		}
		if strings.TrimSpace(in.Feature) == "" {
			return httpx.Invalid("feature", "У переключателя нужно имя")
		}
		if err := h.Switches.Set(r.Context(), strings.TrimSpace(in.Feature), in.Person, in.Machine, in.Enabled, in.Note, p.User.Email); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	mux.Handle("DELETE /api/admin/switches", admin(func(w http.ResponseWriter, r *http.Request, _ *auth.Principal) error {
		q := r.URL.Query()
		if strings.TrimSpace(q.Get("feature")) == "" {
			return httpx.Invalid("feature", "У переключателя нужно имя")
		}
		if err := h.Switches.Remove(r.Context(), q.Get("feature"), q.Get("person"), q.Get("machine")); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
}

var _ switches.Gate = (*switches.Store)(nil)
