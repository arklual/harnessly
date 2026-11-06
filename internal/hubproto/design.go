package hubproto

// Who may see and change a machine's designs (Sinopia) when the machine is linked to a hub, not to the closed cluster:
// the hub keeps the access records, the runtime enforces the snapshot it is pushed. The page's own access API is
// answered by the hub, the runtime is only told the result.

const (
	// FeatureDesignAccess is in `hello` when the agent serves design.access and design.public.token and sends the
	// `design` event to a link that subscribed to it by name.
	FeatureDesignAccess = "design-access"

	// OpDesignAccess pushes the access snapshot to the runtime: the body is the snapshot as the runtime takes it
	// (enforce, owner, projects, people…); the answer is {ok}.
	OpDesignAccess = "design.access"
	// OpDesignPublicToken asks the runtime for a short read-only token over one project, for a guest of a share link:
	// DesignPublicTokenRequest in, DesignPublicToken out.
	OpDesignPublicToken = "design.public.token"

	// EventDesign is a project made, taken or deleted on the machine: {"event": "design", "design": DesignEvent}.
	EventDesign = "design"
)

// DesignEvent is a project's change: kind is design.created, design.claim or design.deleted; person is the address the
// project's owner file names ("" when it names none). The hub believes the person only when they may open the machine.
type DesignEvent struct {
	Kind    string `json:"kind"`
	Project string `json:"project"`
	Person  string `json:"person,omitempty"`
}

// DesignPublicTokenRequest is what a guest's token opens: a project, and at most one of a pinned revision (Rev, 40
// hex), the project's pinned version (Pin), or a list of pages.
type DesignPublicTokenRequest struct {
	Project string   `json:"project"`
	Rev     string   `json:"rev,omitempty"`
	Pin     bool     `json:"pin,omitempty"`
	Pages   []string `json:"pages,omitempty"`
}

// DesignPublicToken is the token, the request header the runtime reads it from and its expiry in unix milliseconds.
type DesignPublicToken struct {
	Token     string `json:"token"`
	Header    string `json:"header"`
	ExpiresAt int64  `json:"expiresAt"`
}
