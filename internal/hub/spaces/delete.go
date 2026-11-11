package spaces

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/events"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Deleting a space with everything in it: its documents, machines, history, spend, teams, invitations and members
// (store.SpaceCascade is the list). An owner of the space does it, or an administrator of the installation.
//
// It cannot be undone, so the request repeats the space's name. A space other people are in goes only when the request
// also says so (withMembers): they lose their place in it as if each had been removed, and a person for whom it was the
// only space is left signed in with no space, the state of an account that has not joined one yet. Servers rented from
// a provider for the space are not given back by this: the line of the deletion says how many the space had.
// The organization of the space stays, with its administrators, also when this was its last space.
//
// The deletion's own line is written where it outlives the space: in the organization's journal when there is one,
// and in the installation's journal of sign-in events always.

func (s *Service) deleteSpace(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	spaceID := r.PathValue("spaceId")
	var req struct {
		Confirm     string `json:"confirm"`
		WithMembers bool   `json:"withMembers"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if !p.Admin {
		if err := s.Owner(ctx, p.User.ID, spaceID); err != nil {
			return err
		}
	}
	var sp store.Space
	var members []store.Member
	var machines []store.Machine
	var removed map[string]int64
	err := s.db.Tx(ctx, func(st store.Store) error {
		if err := lockMembers(ctx, st, spaceID); err != nil {
			return err
		}
		var err error
		if sp, err = st.SpaceByID(ctx, spaceID); err != nil {
			return err
		}
		if strings.TrimSpace(req.Confirm) != sp.Name {
			return httpx.Invalid("confirm", "Чтобы удалить пространство, повторите его название")
		}
		servers, err := st.CountProviderServers(ctx, spaceID)
		if err != nil {
			return err
		}
		if members, err = st.Members(ctx, spaceID); err != nil {
			return err
		}
		others := 0
		ids := make([]string, 0, len(members))
		for _, m := range members {
			ids = append(ids, m.UserID)
			if m.UserID != p.User.ID {
				others++
			}
		}
		if others > 0 && !req.WithMembers {
			alone, err := st.MembersWithoutOtherSpace(ctx, spaceID, p.User.ID)
			if err != nil {
				return err
			}
			return httpx.Conflict("space_has_members", "В пространстве есть другие участники: подтвердите удаление вместе с ними").
				With("members", others).With("withoutOtherSpace", alone)
		}
		if machines, err = st.MachinesBySpace(ctx, spaceID); err != nil {
			return err
		}
		if removed, err = st.DeleteSpaceData(ctx, sp); err != nil {
			return err
		}
		// The line names the space by target only: it is not a row of the space, so it stays.
		was, _ := json.Marshal(map[string]any{"name": sp.Name, "slug": sp.Slug, "members": ids, "machines": len(machines), "providerServers": servers, "byOperator": p.Admin})
		return st.AddAudit(ctx, &store.AuditEvent{OrgID: sp.OrgID, ActorID: p.User.ID, ScopeKind: store.ScopeTenant, ScopeID: sp.ID,
			Action: "tenant.purge", TargetKind: "tenant", TargetID: sp.ID, Before: string(was), At: s.now().UTC()})
	})
	if err != nil {
		return err
	}
	s.auth.Audit(ctx, p.User.ID, "space_purged", r, sp.ID+" "+sp.Slug)
	s.afterSpaceDeleted(ctx, sp, members, machines)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"deleted": true, "removed": removed})
	return nil
}

// afterSpaceDeleted ends what was open in a space that is gone: the machines' links, the people's streams and
// whatever they had open on its machines.
func (s *Service) afterSpaceDeleted(ctx context.Context, sp store.Space, members []store.Member, machines []store.Machine) {
	for _, m := range machines {
		if s.machineGone != nil {
			s.machineGone(m)
		}
	}
	org := store.OrgKey(sp)
	for _, m := range members {
		s.events.Kick(sp.ID, m.UserID)
		if s.gone != nil {
			s.gone(sp.ID, m.UserID)
		}
		if s.people != nil {
			s.people(m.UserID)
		}
		if s.orgBus != nil {
			s.orgBus.Kick(org, m.UserID)
		}
	}
	if s.orgBus != nil {
		events.PublishPeople(s.orgBus, org, events.KindMembers, sp.ID)
	}
}
