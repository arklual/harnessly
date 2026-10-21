package design

// Claims of a project with meta/ are keyed by the page id, so that renaming a page or its file keeps them. A claim
// under the board's old key (a file name without the suffix, which a runtime without meta/ still writes) is honoured
// beside it: it holds the same page. What people and agents read names the board as before.

// boardOfKey is the board a claim key names: by page id, or by its old key.
func boardOfKey(a *Artifact, key string) (Board, bool) {
	if a == nil {
		return Board{}, false
	}
	for _, b := range a.Boards {
		if b.ID != "" && b.ID == key {
			return b, true
		}
	}
	for _, b := range a.Boards {
		if BoardKeyOf(b) == key {
			return b, true
		}
	}
	return Board{}, false
}

// legacyKeyOf is the old key of the board a page id names; "" when key is no page of the project.
func legacyKeyOf(a *Artifact, key string) string {
	if !IsPageID(key) {
		return ""
	}
	if b, ok := boardOfKey(a, key); ok && b.ID == key {
		return BoardKeyOf(b)
	}
	return ""
}

// claimShown is how a claim key is named to people and agents, and the page id behind it ("" for none).
func claimShown(a *Artifact, key string) (board, page string) {
	if b, ok := boardOfKey(a, key); ok {
		return BoardKeyOf(b), b.ID
	}
	return key, ""
}

// claimNamesFile: a claim key holds the board file of a project (its old key or its page's id).
func (l *Library) claimNamesFile(project, key, file string) bool {
	if key == BoardTitleOf(file) {
		return true
	}
	if !IsPageID(key) {
		return false
	}
	m := l.ReadModel(project)
	return m != nil && m.PageOfFile(file) == key
}
