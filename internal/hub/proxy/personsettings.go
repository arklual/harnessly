package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The person's settings (GET /api/v2/me, PATCH /api/v2/me/settings) are the hub user's, kept by the hub in
// person_settings (GitHub #61). A runtime keeps one record for the whole machine, so a hub that sent these
// paths to a runtime let the users of that runtime overwrite each other. Every way in answers them here and never
// forwards them: the fleet (/api/v2/…, with or without ?machine=), the workspace (/agents/api/v2/…) and a machine's
// own address (/m/<id>/…).

// mePath says whether a path under /api/v2 is the person's own: settings is the PATCH one.
func mePath(p string) (me, settings bool) {
	switch strings.TrimSuffix(p, "/") {
	case "/api/v2/me":
		return true, false
	case "/api/v2/me/settings":
		return true, true
	}
	return false, false
}

// machineMePath is mePath for a machine's own address: /m/<id>/api/v2/me or, in the workspace's form,
// /m/<id>/agents/api/v2/me.
func machineMePath(p, id string) (me, settings bool) {
	rest, ok := strings.CutPrefix(p, "/m/"+id)
	if !ok {
		return false, false
	}
	rest = strings.TrimPrefix(rest, workspaceBase)
	return mePath(rest)
}

// serveMe answers the person's settings for the hub user u. A path that is not one of the two is refused 404: the
// caller has told it apart already.
func (s *Service) serveMe(w http.ResponseWriter, r *http.Request, u store.User, settings bool) {
	st := &hubMe{s: s, ctx: r.Context(), user: u}
	if settings {
		wire.ServeMeSettings(w, r, st)
		return
	}
	wire.ServeMe(w, r, st)
}

// hubMe is the wire.MeStore of one hub user, for one request.
type hubMe struct {
	s    *Service
	ctx  context.Context
	user store.User
}

func (h *hubMe) load() (wire.Me, error) {
	me := wire.Me{Settings: wire.DefaultSettings(), Rev: 1}
	ps, err := h.s.db.S().PersonSettingsOf(h.ctx, h.user.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
	case err != nil:
		return me, err
	default:
		saved := wire.DefaultSettings()
		// A record that does not read is as if the person changed nothing.
		if json.Unmarshal([]byte(ps.Settings), &saved) == nil && ps.Rev > 0 {
			me.Settings, me.Rev = saved, ps.Rev
		}
	}
	wire.NormalizeSettings(&me.Settings)
	// The person is the hub's: whatever name the machine has, the account's e-mail and name are shown.
	email := h.user.Email
	me.Email = &email
	if h.user.Name != "" {
		n := h.user.Name
		me.Name = &n
	}
	return me, nil
}

func (h *hubMe) Me() (wire.Me, error) { return h.load() }

func (h *hubMe) Patch(rev int64, patch json.RawMessage) (wire.Me, error) {
	me, err := h.load()
	if err != nil {
		return me, err
	}
	if rev != me.Rev {
		return me, wire.ErrMeConflict
	}
	next, err := wire.ApplySettingsPatch(me.Settings, patch)
	if err != nil {
		return me, err
	}
	// A preference is not a grant: the machine chosen as the main one must be one the person may open now.
	if next.DefaultMachine != nil && (me.Settings.DefaultMachine == nil || *me.Settings.DefaultMachine != *next.DefaultMachine) {
		if _, err := h.s.admit(h.ctx, h.user.ID, *next.DefaultMachine); err != nil {
			var he *httpx.Error
			if errors.As(err, &he) {
				return me, wire.ErrMeBadPatch
			}
			return me, err
		}
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return me, err
	}
	ok, err := h.s.db.S().SavePersonSettings(h.ctx, h.user.ID, string(raw), rev, h.s.now())
	if err != nil {
		return me, err
	}
	if !ok {
		return me, wire.ErrMeConflict
	}
	me.Settings, me.Rev = next, rev+1
	return me, nil
}
