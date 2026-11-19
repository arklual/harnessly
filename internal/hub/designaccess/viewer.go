package designaccess

import (
	"errors"
	"strings"
)

// What the hub's view of the store (internal/hub/designview, which implements Viewer) answers with. They live here and
// not there: the hub's packages are imported by the agent's tests, and the view runs the runtime's design code, which
// imports the agent; designaccess must not.

var (
	// ErrConflict is a write made on top of a revision that is not the file's newest one.
	ErrConflict = errors.New("designview: the file changed since its base revision")
	// ErrBadPath is a path outside the project, or one the store does not keep.
	ErrBadPath = errors.New("designview: not a file of the project")
	// ErrReadOnly is a store the hub only reads.
	ErrReadOnly = errors.New("designview: the store is not writable here")
)

// Written is the outcome of a write: the file's revision now, and the project's newest one.
type Written struct {
	Rev, Head int64
}

// EditRoutes are the editor's saves the hub makes itself, by their path under <platform api>/design/; the view runs
// each with the runtime's handler of the same name (designview.EditRoutes, which a test keeps equal to this).
var EditRoutes = map[string]bool{
	"deck": true, "deck/settings": true, "deck/notes": true, "deck/patch": true, "deck/comments": true, "deck/pdf": true,
	"video": true, "video/settings": true, "video/scene": true, "video/overlay": true, "video/comments": true,
	"scene3d/comments": true,
}

// servedSegment: a first segment of a path the public route answers. The names that begin with two underscores are the
// library's own, and only the guest script, the vendor files and the motion kit with the video runtime (HRN-1258:
// what a snapshot needs to play without its machine) are the route's to serve; a board itself may not be named so.
func servedSegment(first string) bool {
	if !strings.HasPrefix(first, "__") {
		return true
	}
	return first == "__guest" || first == "__vendor" || first == "__lib"
}
