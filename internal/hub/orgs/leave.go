package orgs

import (
	"errors"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/tenant"
)

var (
	errLastOrgAdmin = httpx.Conflict("last_admin", "Вы единственный админ организации: сначала дайте роль админа другому")
	errLastOwner    = httpx.Conflict("last_owner", "Вы единственный владелец одной из команд организации: сначала передайте её другому")
)

// leave takes the viewer out of the organization (the profile's «Мои организации»): out of every tenant of it, with what
// being there gave them (RemoveMember: roles, teams, access lists, limits), the machines they registered there revoked,
// and their organization role taken away. The last administrator of the organization or the last owner of a tenant
// cannot leave: the organization would be left without anyone who can manage it.
func (s *Service) leave(w http.ResponseWriter, r *http.Request, tc *tenant.Ctx) error {
	p := tc.Person
	ctx, me := r.Context(), p.User.ID
	o := *tc.Org
	at := s.now().UTC()
	var left []string
	var machines []store.Machine
	err := s.db.Tx(ctx, func(st store.Store) error {
		grants, err := st.GrantsOn(ctx, store.ScopeOrg, o.ID)
		if err != nil {
			return err
		}
		admins, mine := 0, false
		for _, g := range grants {
			if g.Role == store.RoleAdmin {
				admins++
				mine = mine || g.UserID == me
			}
		}
		if mine && admins <= 1 {
			return errLastOrgAdmin
		}
		spaces, err := st.SpacesByOrg(ctx, o.ID)
		if err != nil {
			return err
		}
		for _, sp := range spaces {
			cur, err := st.MemberByUser(ctx, sp.ID, me)
			if store.IsNotFound(err) {
				continue
			}
			if err != nil {
				return err
			}
			if cur.Role == store.RoleOwner {
				if n, err := st.CountOwners(ctx, sp.ID); err != nil {
					return err
				} else if n <= 1 {
					return errLastOwner
				}
			}
			if err := st.RemoveMember(ctx, sp.ID, me); err != nil {
				return err
			}
			ms, err := st.RevokeMachinesOf(ctx, sp.ID, me, at)
			if err != nil {
				return err
			}
			machines = append(machines, ms...)
			left = append(left, sp.ID)
		}
		if mine {
			if _, err := st.Revoke(ctx, me, store.ScopeOrg, o.ID, store.RoleAdmin); err != nil {
				return err
			}
		}
		if len(left) == 0 && !mine {
			return errors.New("nothing to leave")
		}
		chain := []authz.Scope{{Kind: store.ScopeOrg, ID: o.ID}}
		return st.AddAudit(ctx, event(chain, me, "member.leave", "user", me, map[string]any{"tenants": left, "admin": mine}, nil, at))
	})
	if err != nil && err.Error() == "nothing to leave" {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	s.auth.Audit(ctx, me, "org_left", r, o.ID)
	for _, sp := range left {
		if s.gone != nil {
			s.gone(sp, me)
		}
	}
	for _, m := range machines {
		s.auth.Audit(ctx, me, "machine_revoked", r, m.ID+" member_left")
		if s.mGone != nil {
			s.mGone(m)
		}
	}
	s.peopleChanged(o.ID, "", me, true)
	httpx.NoContent(w)
	return nil
}
