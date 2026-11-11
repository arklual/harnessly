package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The person's settings (GET /api/v2/me, PATCH /api/v2/me/settings) are the hub user's, kept by the hub in
// person_settings (GitHub #61). A runtime keeps one record for the whole machine, so a hub that sent these
// paths to a runtime let the users of that runtime overwrite each other. Every way in answers them here and never
// forwards them: the fleet (/api/v2/…, with or without ?machine=), the workspace (/agents/api/v2/…) and a machine's
// own address (/m/<id>/…).

// mePath says whether a path under /api/v2 is a settings document the person owns (wire.Settings), which the hub
// answers itself: settings is the PATCH one.
func mePath(p string) (me, settings bool) {
	rest, ok := strings.CutPrefix(p, "/api/v2/")
	if !ok {
		return false, false
	}
	if _, owner, ok := wire.SettingsDocOf(rest); !ok || owner != wire.OwnerPerson {
		return false, false
	}
	return true, strings.Trim(rest, "/") == "me/settings"
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
	st := &hubMe{s: s, r: r, user: u}
	if settings {
		wire.ServeMeSettings(w, r, st)
		return
	}
	wire.ServeMe(w, r, st)
}

// hubMe is the wire.MeStore of one hub user, for one request.
type hubMe struct {
	s    *Service
	r    *http.Request
	user store.User
}

func (h *hubMe) load() (wire.Me, error) {
	ctx := h.r.Context()
	me := wire.Me{Settings: wire.DefaultSettings(), Rev: 1}
	ps, err := h.s.db.S().PersonSettingsOf(ctx, h.user.ID)
	if errors.Is(err, store.ErrNotFound) {
		// The person has no record here yet: what they had on a machine before the hub kept settings is taken over,
		// once, and the hub's record is the truth from then on.
		if legacy, ok := h.s.legacyMe(h.r, h.user); ok {
			if raw, merr := json.Marshal(legacy); merr == nil {
				if _, serr := h.s.db.S().SavePersonSettings(ctx, h.user.ID, string(raw), 1, h.s.now()); serr != nil {
					return me, serr
				}
				ps, err = h.s.db.S().PersonSettingsOf(ctx, h.user.ID)
			}
		}
	}
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
		if _, err := h.s.admit(h.r.Context(), h.user.ID, *next.DefaultMachine); err != nil {
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
	ok, err := h.s.db.S().SavePersonSettings(h.r.Context(), h.user.ID, string(raw), rev, h.s.now())
	if err != nil {
		return me, err
	}
	if !ok {
		return me, wire.ErrMeConflict
	}
	me.Settings, me.Rev = next, rev+1
	return me, nil
}

// Taking over the settings a person had before the hub kept them. A runtime keeps one record for its machine
// (<data>/v2/me.json, GET /api/v2/me on it); a person without a record in the hub gets the first changed record among
// the machines they may open, as their starting values. On a machine several people share, each of them starts from
// the same record and they part from there. Machines that are away are asked again later, until the person has a
// record: taken over, or made by their own first change.

const (
	// legacyMeEvery is how often the machines of a person without a record are asked.
	legacyMeEvery = 10 * time.Minute
	// legacyMeWait is how long one machine is waited for, and legacyMeMachines how many are asked at a time.
	legacyMeWait     = 1500 * time.Millisecond
	legacyMeMachines = 4
	maxLegacyMe      = 1 << 20
)

// legacyMe is the settings the person changed on a machine before the hub kept them, and whether there are any.
func (s *Service) legacyMe(r *http.Request, u store.User) (wire.PersonSettings, bool) {
	s.mu.Lock()
	last, asked := s.legacy[u.ID]
	due := !asked || !s.now().Before(last.Add(legacyMeEvery))
	if due {
		if len(s.legacy) >= maxAuthz {
			s.legacy = map[string]time.Time{}
		}
		s.legacy[u.ID] = s.now()
	}
	s.mu.Unlock()
	if !due {
		return wire.PersonSettings{}, false
	}
	// The machine the request names first, then the person's own, then the others they may open.
	var ids []string
	add := func(id string) {
		for _, have := range ids {
			if have == id {
				return
			}
		}
		if id != "" && wire.ValidMachineID(id) && s.reg.Get(id) != nil {
			ids = append(ids, id)
		}
	}
	add(r.URL.Query().Get("machine"))
	if rest, ok := strings.CutPrefix(r.URL.Path, "/m/"); ok {
		id, _, _ := strings.Cut(rest, "/")
		add(id)
	}
	if list, err := s.fleet(r.Context(), u.ID, false); err == nil {
		for _, mine := range []bool{true, false} {
			for _, f := range list {
				if f.Mine == mine {
					add(f.ID)
				}
			}
		}
	}
	if len(ids) > legacyMeMachines {
		ids = ids[:legacyMeMachines]
	}
	for _, id := range ids {
		if st, ok := s.machineMe(r, u, id); ok {
			return st, true
		}
	}
	return wire.PersonSettings{}, false
}

// machineMe reads the machine's own record of its person and gives its settings when somebody changed them there and
// they are settings the hub would take from a page.
func (s *Service) machineMe(r *http.Request, u store.User, id string) (wire.PersonSettings, bool) {
	none := wire.PersonSettings{}
	ctx, cancel := context.WithTimeout(r.Context(), legacyMeWait)
	defer cancel()
	m, err := s.admit(ctx, u.ID, id)
	if err != nil {
		return none, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v2/me", nil)
	if err != nil {
		return none, false
	}
	req.Host = r.Host
	req.Header.Set("Accept", "application/json")
	rec := &pageRecorder{header: http.Header{}, status: http.StatusOK}
	s.forward(rec, req, u, m)
	if ctx.Err() != nil || rec.status != http.StatusOK || rec.body.Len() > maxLegacyMe {
		return none, false
	}
	saved := wire.Me{Settings: wire.DefaultSettings()}
	if json.Unmarshal(rec.body.Bytes(), &saved) != nil || saved.Rev <= 1 {
		return none, false
	}
	st := saved.Settings
	wire.NormalizeSettings(&st)
	// A preference is not a grant: the main machine is kept only when the person may open it.
	if st.DefaultMachine != nil {
		if _, err := s.admit(ctx, u.ID, *st.DefaultMachine); err != nil {
			st.DefaultMachine = nil
		}
	}
	// What a machine says goes through the same check as what a page writes.
	raw, err := json.Marshal(st)
	if err != nil {
		return none, false
	}
	next, err := wire.ApplySettingsPatch(wire.DefaultSettings(), raw)
	if err != nil {
		s.log.Warn("person settings: the machine's record is not settings", "machine", id)
		return none, false
	}
	return next, true
}
