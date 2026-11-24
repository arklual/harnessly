package hubproto

// The hub sends the machine owner's verified identity on the authenticated link.
// An empty author clears a previously managed default on ownership changes.
const OpGitAuthor = "setup.git-author"

type GitAuthor struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}
