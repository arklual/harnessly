package hubproto

// The work of a chat (docs/hub/history.md, «Work of a chat»): the git repositories, worktrees and branches a chat changed
// or committed to, as the machine's runtime registered them. The runtime hands the records to the agent over its
// replica socket ({"k":"work","entries":[…]}): all of them when the socket opens, then each one that changed. The agent
// forwards them to the hub as `chatwork` events and sends what it holds again when a link subscribes to the event. A
// record carries no text of the conversation: paths, a branch, shas and a merge request's address.
const (
	// EventChatWork carries ChatWork.
	EventChatWork = "chatwork"
	// ChatWorkFrameMax is the most records one event may carry.
	ChatWorkFrameMax = 200
	// ChatWorkCommitsMax is the most commits a record lists, newest first.
	ChatWorkCommitsMax = 50
)

// Work sources: registered while the chat ran, or found afterwards in its transcript.
const (
	ChatWorkLive = "live"
	ChatWorkScan = "scan"
)

// ChatWork is the payload of EventChatWork.
type ChatWork struct {
	Entries []ChatWorkEntry `json:"entries"`
}

// ChatWorkEntry is one worktree a chat worked in. Conv and Worktree name it on its machine; a newer At replaces an older
// record of the same name.
type ChatWorkEntry struct {
	Conv string `json:"conv"`
	// Parent is the chat that delegated Conv ("" for none): the hub shows a parent the work of its delegated chats.
	Parent string `json:"parent,omitempty"`
	// Repo is the top folder of the repository's main worktree; Worktree the top folder the work is in (the same for
	// work in the main one).
	Repo     string `json:"repo"`
	Worktree string `json:"worktree"`
	Branch   string `json:"branch,omitempty"`
	// Base is the commit the worktree stood on when the chat first touched it; Head where it stands now.
	Base string `json:"base,omitempty"`
	Head string `json:"head,omitempty"`
	// Commits are the commits the chat made there, newest first, at most ChatWorkCommitsMax.
	Commits []string `json:"commits,omitempty"`
	// Remote is the address of the repository's origin without credentials; MR a merge request of the branch
	// (https://host/group/project/-/merge_requests/<iid>) when one is known.
	Remote string `json:"remote,omitempty"`
	MR     string `json:"mr,omitempty"`
	// Dirty: the worktree had uncommitted changes when it was last looked at.
	Dirty  bool   `json:"dirty,omitempty"`
	Since  int64  `json:"since"` // epoch ms, the first touch
	At     int64  `json:"at"`    // epoch ms, the last look
	Source string `json:"source"`
}
