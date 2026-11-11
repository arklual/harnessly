package storehub

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// ResumeRoom is the room of a person's chat resume snapshots: one per person, whatever space the machine is
// in. A machine nobody made has none.
func ResumeRoom(userID string) (string, error) {
	room := filestore.KindResume + "-" + userID
	if userID == "" || !filestore.ValidRoom(room) {
		return "", filestore.ErrForbidden
	}
	return room, nil
}

// resumeAccess lets only the person's own machines at their resume snapshots. A snapshot holds a chat's whole
// history, so it is refused (403) to anything that is not one of them:
//   - a request without a machine's session (the web, a script with a person's cookie): not a 401, there is no session
//     that would let it in;
//   - a machine of another person naming a snapshot of this one's machine: the room is the caller's own anyway, the
//     answer says out loud that the snapshot is not theirs.
//
// A machine writes only under its own id (`<machine>/<chat>.json`) and reads its person's other machines' snapshots,
// those of a machine that is off or revoked too: that is the point, the chat goes on while it is gone.
func (s *Store) resumeAccess(r *http.Request) (filestore.Access, error) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		return filestore.Access{}, filestore.ErrForbidden
	}
	m, err := s.sessionMachine(r)
	if err != nil {
		return filestore.Access{}, err
	}
	room, err := ResumeRoom(m.CreatedBy)
	if err != nil {
		return filestore.Access{}, err
	}
	q := r.URL.Query()
	for _, p := range []string{q.Get("path"), q.Get("prefix")} {
		if err := s.resumeOwner(r, m, p); err != nil {
			return filestore.Access{}, err
		}
	}
	own := m.ID + "/"
	return filestore.Access{Member: memberName(m), Room: room,
		Writes: func(p string) bool { return strings.HasPrefix(p, own) }}, nil
}

// resumeOwner refuses a path whose machine (its first segment) is known and is somebody else's. A machine the hub no
// longer knows is let through: the room is the caller's own, nothing of another person is in it.
func (s *Store) resumeOwner(r *http.Request, caller store.Machine, p string) error {
	id, _, _ := strings.Cut(p, "/")
	if id == "" || id == caller.ID {
		return nil
	}
	src, err := s.machine(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if src.CreatedBy != caller.CreatedBy {
		return filestore.ErrForbidden
	}
	return nil
}
