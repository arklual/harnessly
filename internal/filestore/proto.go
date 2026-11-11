// The wire format between the store and the machines that sync with it.
package filestore

// Entry is one revision of one file of the library. Rev numbers are global to the hub: every accepted write takes
// the next one, so "everything after rev N" is a complete catch-up.
type Entry struct {
	Path    string `json:"path"`
	Rev     int64  `json:"rev"`
	Sha     string `json:"sha,omitempty"`
	Tag     string `json:"tag,omitempty"`
	Size    int64  `json:"size"`
	Exec    bool   `json:"exec,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
	Author  string `json:"author,omitempty"`
	Machine string `json:"machine,omitempty"`
	At      int64  `json:"at"`
}

// Same tells whether two revisions hold the same contents. Sha addresses the stored blob; Tag is the fingerprint
// of the plain contents the agents compare (in an encrypted room a keyed one, and the blob differs every time the
// same contents are sealed). A revision written before tags existed compares by Sha.
func (e Entry) Same(tag string) bool {
	if e.Tag != "" {
		return e.Tag == tag
	}
	return e.Sha == tag
}

// CommitRequest asks for a new revision of Path. Base is the revision the writer started from: 0 for a file it has
// never seen. The hub accepts the write only when Base is the file's current revision (or the file is absent or
// deleted and Base is 0), so a write made on top of a stale copy is refused instead of silently replacing a newer one.
type CommitRequest struct {
	Path    string `json:"path"`
	Base    int64  `json:"base"`
	Sha     string `json:"sha,omitempty"`
	Tag     string `json:"tag,omitempty"`
	Size    int64  `json:"size"`
	Exec    bool   `json:"exec,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
	Author  string `json:"author,omitempty"`
}

// CommitResponse is the accepted revision, or, with Conflict, the current one that the write was stale against.
type CommitResponse struct {
	Entry    *Entry `json:"entry,omitempty"`
	Conflict bool   `json:"conflict,omitempty"`
}

// Changes is the latest revision of every file changed after the asked one, oldest first. Rev is the cursor to ask
// from next time; More says the answer was cut and the next ask returns the rest at once.
type Changes struct {
	Rev     int64   `json:"rev"`
	Entries []Entry `json:"entries"`
	More    bool    `json:"more,omitempty"`
	// Scope names what of the room the caller sees (a design library shows a machine only the projects its
	// people have a role in). It changes when that does; "" for a caller who sees the whole room.
	Scope string `json:"scope,omitempty"`
}

// Snapshot is a folder of the room as it was at a revision: the newest revision of every path under the prefix that
// is not newer than At, deleted paths left out (a design's history and its pinned versions).
type Snapshot struct {
	At      int64   `json:"at"`
	Entries []Entry `json:"entries"`
}

// Member is one machine (or one person's agent on a machine) in a room, with the age recipient its room key is
// wrapped for. HasKey says the room key has been wrapped for it already.
type Member struct {
	Name      string `json:"name"`
	Recipient string `json:"recipient"`
	HasKey    bool   `json:"has_key"`
	At        int64  `json:"at"`
}

// WrappedKey is a room key sealed with age for one member's recipient: only that member's machine opens it.
type WrappedKey struct {
	Member  string `json:"member"`
	Wrapped string `json:"wrapped"`
	By      string `json:"by,omitempty"`
}

// Lease is a short claim on one thing in a room (a board of a project), kept by the hub so every machine sees the
// same holder. Machine is the member the holder's token belongs to (filled by the hub, never trusted from the body);
// Chat is the holder's chat on that machine. The hub does not read Key: it is the name the holders agree on.
type Lease struct {
	Key       string `json:"key"`
	Machine   string `json:"machine"`
	Chat      string `json:"chat"`
	User      string `json:"user,omitempty"`
	ChatTitle string `json:"chat_title,omitempty"`
	// Turn is what the holder's runtime says the chat is doing: "working" while an agent turn runs, "idle" after.
	Turn  string `json:"turn,omitempty"`
	Since int64  `json:"since"`
	// Beat is the last sign of life; a lease is live for LeaseTTL after it. Edit is the last time something was
	// written under it.
	Beat int64 `json:"beat"`
	Edit int64 `json:"edit"`
}

// LeaseTTLMs is how long a lease nobody refreshes stays live (a machine that went away frees its boards by itself).
const LeaseTTLMs = 2 * 60 * 1000

// LeaseRequest takes a lease or refreshes the caller's own. Edit says something was just written under it.
type LeaseRequest struct {
	Key       string `json:"key"`
	Chat      string `json:"chat"`
	User      string `json:"user,omitempty"`
	ChatTitle string `json:"chat_title,omitempty"`
	Turn      string `json:"turn,omitempty"`
	Edit      bool   `json:"edit,omitempty"`
}

// LeaseResponse is the lease the caller now holds, or, with Held, the live lease of someone else.
type LeaseResponse struct {
	Lease Lease `json:"lease"`
	Held  bool  `json:"held,omitempty"`
}

// LeaseRelease lets go of the caller's lease on Key; Force drops whoever's it is (a hung chat: "take it off").
type LeaseRelease struct {
	Key   string `json:"key"`
	Chat  string `json:"chat,omitempty"`
	Force bool   `json:"force,omitempty"`
}

// Leases is every live lease of the room.
type Leases struct {
	Leases []Lease `json:"leases"`
}

// WrappedKeys is the room key of a profile room as wrapped for each of its members (GET …/keys).
type WrappedKeys struct {
	Keys []WrappedKey `json:"keys"`
}

// MaxBlobCheck is the most hashes one MissingBlobsRequest names.
const MaxBlobCheck = 500

// MissingBlobsRequest asks which of the named contents the collection does not hold: one request before a batch of
// uploads instead of a look at each.
type MissingBlobsRequest struct {
	Shas []string `json:"shas"`
}

// MissingBlobsResponse lists, in the order asked, the hashes the collection does not hold.
type MissingBlobsResponse struct {
	Missing []string `json:"missing"`
}
