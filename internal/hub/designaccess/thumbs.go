package designaccess

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The cards of the library are the pictures of the projects' first boards, which the machine draws and serves
// (GET <platform>/design/library/thumb). While the machine is off the hub's library still lists its projects from the
// store, and the hub answers their pictures too: the newest one the machine drew, which it put into the store's thumbs
// collection. A machine that is on is always asked: it draws the picture of the board as it is now.

// Thumber is the part of a Viewer that gives the board pictures kept in the store (designview.View).
type Thumber interface {
	// Thumb is the picture of a board, at most limit bytes, with its sha; nil when there is none.
	Thumb(ctx context.Context, spaceID, project, boardPath string, limit int64) ([]byte, string, error)
}

// thumbPath is the machine's route of a board's picture under a platform prefix.
var thumbPath = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/design/library/thumb$`)

// maxThumb caps a board picture the hub serves.
const maxThumb = 1 << 20

// maxThumbBoard caps the board path of a picture asked for (design.LimitBoardPath).
const maxThumbBoard = 512

// serveThumb answers a board's picture from the store when the machine is off and the hub owns the design facts of its
// person; false leaves the request to the machine.
func (s *Service) serveThumb(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine) bool {
	th, ok := s.view.(Thumber)
	if !ok || m.SpaceID == "" || (s.reg != nil && s.reg.Get(m.ID) != nil) || !s.OwnerOn(r.Context(), m) {
		return false
	}
	q := r.URL.Query()
	id, board := q.Get("artifact"), q.Get("board")
	none := func() bool {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNotFound)
		return true
	}
	if !projectIDRe.MatchString(id) || board == "" || len(board) > maxThumbBoard || strings.ContainsRune(board, 0) {
		return none()
	}
	if may, err := s.mayOpen(r.Context(), u, m, id); err != nil || !may {
		return none()
	}
	b, sha, err := th.Thumb(r.Context(), m.SpaceID, id, board, maxThumb)
	if err != nil || len(b) == 0 {
		if err != nil {
			s.log.Warn("design: a board picture was not read from the store", "project", id, "error", err)
		}
		return none()
	}
	etag := `"` + sha[:16] + `"`
	w.Header().Set("ETag", etag)
	// The picture may be of an earlier version of the board: it is checked again each time, as the machine's earlier one.
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	w.Header().Set("Content-Type", "image/jpeg")
	_, _ = w.Write(b)
	return true
}
