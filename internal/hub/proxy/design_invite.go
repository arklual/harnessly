package proxy

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"regexp"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

var inviteToken = regexp.MustCompile("^[a-zA-Z0-9_-]{43}$")

// The link is a navigation only. The signed-in design access API consumes it.
func (s *Service) designInvite(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	token := r.PathValue("token")
	if !inviteToken.MatchString(token) {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	p, err := s.auth.Authenticate(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if p == nil {
		http.Redirect(w, r, "/signin?next="+url.QueryEscape(r.URL.Path), http.StatusFound)
		return
	}
	var machine, space string
	err = s.db.QueryRow(r.Context(), `SELECT m.id,m.space_id FROM design_invites i JOIN machines m ON m.id=i.machine_id WHERE i.token_hash=? AND m.revoked_at IS NULL`, store.HashToken(token)).Scan(&machine, &space)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := s.spaces.Access(r.Context(), p.User.ID, space); err != nil {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	sp, err := s.db.S().SpaceByID(r.Context(), space)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	target := workspaceBase + "/designly/invite/" + token
	if !s.Workspace() || !s.fleetOn(r.Context(), sp) {
		if _, _, err := s.admitView(r.Context(), p.User.ID, machine); err != nil {
			s.fail(w, r, err)
			return
		}
		target = "/m/" + machine + "/designly/invite/" + token + "?runtime=1"
	}
	http.Redirect(w, r, target, http.StatusFound)
}
