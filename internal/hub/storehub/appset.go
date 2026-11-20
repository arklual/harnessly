package storehub

import (
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// AppSetRoom is the room of a person's installed apps: one per person, whatever space the machine is in. A
// machine nobody made has none.
func AppSetRoom(userID string) (string, error) {
	room := filestore.KindAppSet + "-" + userID
	if userID == "" || !filestore.ValidRoom(room) {
		return "", filestore.ErrForbidden
	}
	return room, nil
}

// appSetAccess lets only the person's own machines at their app set: a request without a machine's session (the web, a
// script with a person's cookie) is a 403, and a machine always gets the room of its own person, never another's. Every
// machine of the person reads and writes all of it: a record is the person's whole set, and the packed packages are
// content-addressed.
func (s *Store) appSetAccess(r *http.Request) (filestore.Access, error) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		return filestore.Access{}, filestore.ErrForbidden
	}
	m, err := s.sessionMachine(r)
	if err != nil {
		return filestore.Access{}, err
	}
	room, err := AppSetRoom(m.CreatedBy)
	if err != nil {
		return filestore.Access{}, err
	}
	return filestore.Access{Member: memberName(m), Room: room}, nil
}
