package hubproto

// The move of a machine's own data into the hub (docs/migrations): a runtime of a version that keeps its data in
// the hub's store makes an inventory of what it holds only on its disk (the pictures of its chats, the files its agents
// handed over, the handoff notes, the Sinopia library), puts every object into the store, checks the store has it under
// its sha256, and reports each object here. The hub keeps the journal in its database, one row per machine × kind ×
// object, so a run that stopped goes on where it was, a person sees how far it is and what failed, and `hub doctor`
// says it too. The report goes with the machine's store session (StoreSession.Token) as the bearer.
//
//	POST <MigrationPath>/report   MigrationReport → MigrationAnswer
//	GET  <MigrationPath>/status   → MigrationStatus of this machine
//
// A hub without MigrationCapability keeps no journal: the runtime moves its data all the same and keeps the journal
// to itself.

const (
	// MigrationCapability is the id GET /api/capabilities lists the journal under.
	MigrationCapability = "migration"
	// MigrationPath is the root of the journal's machine API on the hub.
	MigrationPath = StoreAPIPath + "/migration/v1"
	// MigrationReportMax is the most objects one report carries.
	MigrationReportMax = 500
	// MigrationFailuresMax is the most failed objects a status lists.
	MigrationFailuresMax = 200
)

// States of an object in the journal.
const (
	// MigrationPending: in the inventory, not in the store yet.
	MigrationPending = "pending"
	// MigrationVerified: the store has it under its sha256, checked after the upload.
	MigrationVerified = "verified"
	// MigrationFailed: the last attempt failed with Code.
	MigrationFailed = "failed"
	// MigrationSkipped: a person (or `harnessly migrate skip`) let it stay on the machine only.
	MigrationSkipped = "skipped"
)

// MigrationObject is one object of a machine's data and how its move went.
type MigrationObject struct {
	// Kind is the store collection it goes to (uploads, files, handoff, design).
	Kind string `json:"kind"`
	// Object names it on the machine: an upload's id, a file's sha256, a path under the library.
	Object string `json:"object"`
	Sha256 string `json:"sha256,omitempty"`
	Size   int64  `json:"size,omitempty"`
	// Mime and Name of an upload: what the chat shows it as once it is read from the hub.
	Mime  string `json:"mime,omitempty"`
	Name  string `json:"name,omitempty"`
	State string `json:"state"`
	// Code is the error code of a failed object (docs/migrations/<id>.md lists every one), Error its words.
	Code     string `json:"code,omitempty"`
	Error    string `json:"error,omitempty"`
	Attempts int    `json:"attempts,omitempty"`
	// ChangedAt is when the hub last wrote it (RFC 3339), in a status only.
	ChangedAt string `json:"changedAt,omitempty"`
}

// MigrationKey names one object of the journal.
type MigrationKey struct {
	Kind   string `json:"kind"`
	Object string `json:"object"`
}

// MigrationReport is what a machine says of its move: the run (Total is the whole inventory) and the objects that
// changed since the last report, at most MigrationReportMax.
type MigrationReport struct {
	// Migration is the id of the move (docs/migrations/<id>.md).
	Migration string `json:"migration"`
	// Version is the runtime's own version.
	Version string            `json:"version"`
	Total   int64             `json:"total"`
	Objects []MigrationObject `json:"objects,omitempty"`
}

// MigrationAnswer is what the hub asks of the machine back: the objects a person asked to try again, and those they
// let stay on the machine (the machine stops trying them).
type MigrationAnswer struct {
	Retry []MigrationKey `json:"retry,omitempty"`
	Skip  []MigrationKey `json:"skip,omitempty"`
}

// MigrationStatus is how far one machine's move is.
type MigrationStatus struct {
	Machine   string `json:"machine"`
	Migration string `json:"migration"`
	Version   string `json:"version,omitempty"`
	// Total is the inventory; Verified + Skipped == Total is a move that is over.
	Total    int64 `json:"total"`
	Verified int64 `json:"verified"`
	Pending  int64 `json:"pending"`
	Failed   int64 `json:"failed"`
	Skipped  int64 `json:"skipped"`
	// Retrying is how many failed objects a person asked to try again that the machine has not tried yet.
	Retrying   int64  `json:"retrying,omitempty"`
	StartedAt  string `json:"startedAt,omitempty"`
	ChangedAt  string `json:"changedAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
	// Failures are the failed objects, at most MigrationFailuresMax, the oldest error first.
	Failures []MigrationObject `json:"failures,omitempty"`
	// Doc is the page of the move: what goes where, every error code, how to check, what not to do.
	Doc string `json:"doc,omitempty"`
}

// MigrationDone tells whether nothing is left to move: every object verified or skipped.
func (s MigrationStatus) MigrationDone() bool {
	return s.Total > 0 && s.Verified+s.Skipped >= s.Total && s.Failed == 0 && s.Pending == 0
}

// The machine's sidebar: its projects and its favorites go into its person's sidebar on the hub, merged as
// the page merges a machine (a project joins the profile's of its name, the favorites once). The object is one, of
// MigrationSidebarKind, named MigrationSidebarObject; its sha256 is that of the body sent, and the hub counts it
// verified once the person's sidebar lists the machine as merged. A hub that keeps no sidebar answers 404.
//
//	POST <MigrationPath>/sidebar   MigrationSidebar → MigrationSidebarAnswer
const (
	MigrationSidebarKind   = "sidebar"
	MigrationSidebarObject = "organizer"
)

// MigrationSidebar is a machine's own projects and favorites.
type MigrationSidebar struct {
	Projects  []MigrationProject  `json:"projects"`
	Favorites *MigrationFavorites `json:"favorites,omitempty"`
}

// MigrationProject is a project as the machine has it.
type MigrationProject struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"createdAt"`
}

// MigrationFavorites is the machine's favorites: the groups, then the loose ones.
type MigrationFavorites struct {
	Groups []MigrationFavGroup `json:"groups"`
	Loose  []string            `json:"loose"`
}

// MigrationFavGroup is a named group of favorites.
type MigrationFavGroup struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Chats []string `json:"chats"`
}

// MigrationSidebarAnswer: Merged is the person's sidebar listing the machine as merged.
type MigrationSidebarAnswer struct {
	Merged bool `json:"merged"`
}
