package resaccess

// UI kits as a resource. A kit is shared like a design project: the same dialog, the same rows, the same
// levels, and one more rule: a kit has one maker. Only the owner and the people the owner (or an editor the owner lets
// share) names one by one may edit it. A team or the company can be given view and nothing more: «edit for everyone» does
// not exist for a kit. The ceilings are checked where a grant is written, and again where a role is folded, so a row that
// got past (an older one, a hand-made one) does not lift them.

import (
	"context"
	"strings"
)

// kitPerson is the role a person's own grant counts for in a kit: view or edit.
func kitPerson(role string) string {
	switch role {
	case roleManage, roleEdit:
		return roleEdit
	case roleComment, roleView:
		return roleView
	}
	return ""
}

// kitGroup is the role a team or the company counts for in a kit: view, whatever it was given.
func kitGroup(role string) string {
	if role == "" || role == levelNone {
		return ""
	}
	return roleView
}

func kitTeamLevel(ref Ref, level string) error {
	if ref.Kind == KindKit && level != levelNone && level != roleView {
		return fail("bad_request", "Команде или компании кит открывается только на просмотр: править его могут владелец и люди, названные поимённо")
	}
	return nil
}

func kitPersonRole(ref Ref, role string) error {
	if ref.Kind == KindKit && role != roleView && role != roleEdit {
		return fail("bad_request", "Человеку кит открывается на просмотр (view) или правку (edit)")
	}
	return nil
}

func kitGroupRole(ref Ref, role string, canShare bool) error {
	if ref.Kind == KindKit && (role != roleView || canShare) {
		return fail("bad_request", "Команде кит открывается только на просмотр: правку выдают человеку поимённо")
	}
	return nil
}

// KitRef is the resource of a kit of a space. The kit belongs to the space: its rows carry the space, and the id keeps
// the space too, so the same kit id in two spaces names two kits under the table's key.
func KitRef(spaceID, kit string) Ref {
	return Ref{Kind: KindKit, ID: spaceID + "/" + kit, Space: spaceID}
}

// KitGrant is what one person may do with one kit.
type KitGrant struct{ See, Edit, Own bool }

// ClaimKit records a kit no one has recorded: the person is its owner and it is private (legacy: the team may view it,
// for the kits that were on the machines before kits were shared). It is one insert under the table's key, so two
// machines racing for the same id get one owner. It returns the owner's address, whoever that is.
func (s *Service) ClaimKit(ctx context.Context, spaceID, kit, email, name string, legacy bool) (string, error) {
	ref := KitRef(spaceID, kit)
	email = normEmail(email)
	var owner string
	err := s.write(ctx, func(t *txn) error {
		if r, err := t.stored(ref, true); err != nil {
			return err
		} else if r != nil {
			owner = r.Owner
			return nil
		}
		level := levelNone
		if legacy {
			level = roleView
		}
		now := ms(t.now)
		if _, err := t.q.Exec(t.ctx, `INSERT INTO resource_access (kind, id, space_id, owner_email, owner_name, home_team, team_level, editors_can_share, title, created_at, updated_at)
			VALUES (?, ?, NULLIF(?, ''), ?, ?, '', ?, 0, ?, ?, ?) ON CONFLICT (kind, id) DO NOTHING`, ref.Kind, ref.ID, ref.Space, email, name, level, kit, now, now); err != nil {
			return err
		}
		r, err := t.stored(ref, true)
		if err != nil || r == nil {
			return err
		}
		owner = r.Owner
		if owner == email {
			return t.log(ref, email, "claim", kit, map[string]any{"legacy": legacy})
		}
		return nil
	})
	return owner, err
}

// KitView is what a person may do with each recorded kit of a space. A role given by name counts as everywhere else:
// held by the person's account, or an invitation to an address they have proved.
func (s *Service) KitView(ctx context.Context, spaceID string, a Actor) (map[string]KitGrant, error) {
	t := s.read(ctx)
	prefix := spaceID + "/"
	rows, err := t.q.Query(t.ctx, `SELECT id FROM resource_access WHERE kind = ? AND COALESCE(space_id, '') = ?`, KindKit, spaceID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := make(map[string]KitGrant, len(ids))
	for _, id := range ids {
		if !strings.HasPrefix(id, prefix) {
			continue
		}
		r, people, grants, err := t.load(Ref{Kind: KindKit, ID: id, Space: spaceID}, false)
		if err != nil {
			return nil, err
		}
		f, err := t.foldFor(r, people, grants, a)
		if err != nil {
			return nil, err
		}
		out[strings.TrimPrefix(id, prefix)] = KitGrant{See: f.Role != "", Edit: Rank(f.Role) >= Rank(roleEdit), Own: f.Role == roleOwner}
	}
	return out, nil
}

// OpenLegacyKit lets the team view a kit its owner has just claimed and not shared: nothing has been granted, the team
// level is still «none». A kit that was shared in any way is left as the owner made it.
func (s *Service) OpenLegacyKit(ctx context.Context, spaceID, kit, email string) error {
	ref := KitRef(spaceID, kit)
	email = normEmail(email)
	return s.write(ctx, func(t *txn) error {
		r, err := t.stored(ref, true)
		if err != nil || r == nil || r.Owner != email || r.TeamLevel != levelNone {
			return err
		}
		var n int
		if err := t.q.QueryRow(t.ctx, `SELECT COUNT(*) FROM resource_access_people WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ?`, ref.Kind, ref.ID, ref.Space).Scan(&n); err != nil || n > 0 {
			return err
		}
		if err := t.q.QueryRow(t.ctx, `SELECT COUNT(*) FROM resource_access_teams WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ?`, ref.Kind, ref.ID, ref.Space).Scan(&n); err != nil || n > 0 {
			return err
		}
		var logged int
		if err := t.q.QueryRow(t.ctx, `SELECT COUNT(*) FROM resource_access_log WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ? AND action <> 'claim'`, ref.Kind, ref.ID, ref.Space).Scan(&logged); err != nil || logged > 0 {
			return err // the owner has already changed something: it is theirs now
		}
		return t.setTeam(ref, email, roleView)
	})
}
