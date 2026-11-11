package events

// The two kinds that say the people of an organization changed. Like every event of the organization's stream they
// carry no copy of anything: a page that shows members or invitations reads them again.
const (
	// KindMembers: somebody joined or left a space, a team or a role.
	KindMembers = "members.changed"
	// KindInvites: an invitation was made, accepted or revoked.
	KindInvites = "invites.changed"
)

// PeopleChange is the data of KindMembers and KindInvites: the space (tenant) it happened in, "" for the organization
// as a whole. It does not name the person: the event goes to every member of the organization.
type PeopleChange struct {
	SpaceID string `json:"spaceId,omitempty"`
}

// PublishPeople says on the organization's stream that the members or the invitations of a space changed. Every
// member of the organization hears it, on the organization's stream and on the space's own.
//
// The organization's bus is one process's (MemoryBus): on a hub of several replicas the pages connected to another
// replica do not hear it, and neither does Kick reach the streams they hold. Both need the shared bus the OrgBus
// interface leaves room for before replicas are turned on.
func PublishPeople(p Publisher, orgID, kind, spaceID string) {
	if p == nil || orgID == "" {
		return
	}
	p.Publish(orgID, kind, For(Audience{Role: RoleMember, Space: spaceID}, PeopleChange{SpaceID: spaceID}))
}
