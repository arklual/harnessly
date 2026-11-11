package spaces

import (
	"context"

	"github.com/ly-solutions/harnessly/internal/hub/events"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// peopleChanged tells the organization's open pages that the members (events.KindMembers) or the invitations
// (events.KindInvites) of a space changed; the space's own stream is told by its `members` event beside every call.
//
// userID is the person whose place changed, "" when it is nobody's (an invitation made or revoked): the workspace
// pages they have open are told to read their machines again. lost says they left or their role changed: the
// organization's streams they hold were opened with what they could see before, and are ended; the page's reconnect is
// given what the person may see now, or is refused.
func (s *Service) peopleChanged(ctx context.Context, spaceID, kind, userID string, lost bool) {
	if s.people != nil && userID != "" {
		s.people(userID)
	}
	if s.orgBus == nil {
		return
	}
	sp, err := s.db.S().SpaceByID(ctx, spaceID)
	if err != nil {
		return
	}
	org := store.OrgKey(sp)
	events.PublishPeople(s.orgBus, org, kind, spaceID)
	if lost && userID != "" {
		s.orgBus.Kick(org, userID)
	}
}
