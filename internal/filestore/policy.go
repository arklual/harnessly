package filestore

import (
	"path"
	"strings"
)

// Policy is which paths of a collection sync. The hub checks the same policy for the collection's kind, so a writer
// cannot slip a path the collection never shares.
type Policy interface {
	Synced(rel string) bool
	// SkippedDir tells whether a folder holds nothing that syncs, so a walk or a watch need not enter it.
	SkippedDir(rel string) bool
}

// Design is a Sinopia library: project folders, nothing hidden, no notes for agents (see Synced).
type Design struct{}

func (Design) Synced(rel string) bool     { return Synced(rel) }
func (Design) SkippedDir(rel string) bool { return SkippedDir(rel) }

// Files is a collection of files that are not a folder anybody edits in place: uploads of a chat, files an agent
// sent, handoff material, application bundles. Any clean relative path is taken; what it means is the writer's.
type Files struct{}

func (Files) Synced(rel string) bool { return SafeRel(rel) }

func (Files) SkippedDir(rel string) bool { return false }

// SafeRel is the shape every synced path must have whatever the collection: relative, clean, no way out of the root.
func SafeRel(rel string) bool {
	if rel == "" || len(rel) > 1024 || strings.ContainsAny(rel, "\x00\\\n\r") {
		return false
	}
	if strings.HasPrefix(rel, "/") || path.Clean(rel) != rel || rel == "." {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// Collection kinds. A collection is one kind of a person's or a space's data; the hub keeps one room per space and
// kind. Design is a folder that syncs both ways; the other kinds take files by path and serve them by hash.
const (
	KindDesign  = "design"
	KindUploads = "uploads"
	KindFiles   = "files"
	KindHandoff = "handoff"
	KindApps    = "apps"
	// KindProfile is a person's Claude Code profile (Profile): one room per person, not per space.
	KindProfile = "profile"
)

// Kinds are the collection kinds a hub serves, in a stable order.
var Kinds = []string{KindDesign, KindUploads, KindFiles, KindHandoff, KindApps, KindProfile}

// KnownKind tells whether a collection kind is one the hub serves.
func KnownKind(kind string) bool {
	for _, k := range Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// PolicyFor is the policy of a room by its name, `<kind>` or `<kind>-<space>`: a design library is Design, every
// other room takes any clean path.
func PolicyFor(room string) Policy {
	if room == KindDesign || strings.HasPrefix(room, KindDesign+"-") {
		return Design{}
	}
	if room == KindProfile || strings.HasPrefix(room, KindProfile+"-") {
		return Profile{}
	}
	return Files{}
}

// ValidRoom is a room name: lowercase letters, digits, dashes.
func ValidRoom(room string) bool {
	if room == "" || len(room) > 64 {
		return false
	}
	for _, c := range room {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}
