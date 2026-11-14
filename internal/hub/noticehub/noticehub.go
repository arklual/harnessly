// Package noticehub answers the notifications API of a machine from the hub's database: the person's
// notices, their reads and their switches live in Postgres, one set for all their machines and devices, and a read on
// one device reaches the others over a stream. It only answers where the rollout switch hub-notices is on for the
// person and the machine; otherwise the request goes on to the machine, whose own store answers as it always does.
package noticehub

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/notices"
)

// Switch is the rollout switch of the hub's notices.
const Switch = "hub-notices"

var path = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/notifications(/.*)?$`)

// Service is the hub's notices.
type Service struct {
	Notices *notices.Service
	// On tells whether the switch is on for the person's email on the machine; nil is off.
	On func(r *http.Request, email, machineID string) bool
}

// New makes the service over the hub's database.
func New(db notices.DB, on func(r *http.Request, email, machineID string) bool) *Service {
	return &Service{Notices: notices.New(notices.NewSQL(db)), On: on}
}

// ServeMachine answers the notifications routes of a machine the person may open.
func (s *Service) ServeMachine(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine) bool {
	p := strings.TrimPrefix(r.URL.EscapedPath(), "/m/"+m.ID)
	mt := path.FindStringSubmatch(p)
	if mt == nil || s.Notices == nil {
		return false
	}
	if s.On == nil || !s.On(r, strings.ToLower(strings.TrimSpace(u.Email)), m.ID) {
		return false
	}
	rest := mt[1]
	if rest == "" {
		rest = "/"
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path, r2.URL.RawPath = rest, ""
	s.Notices.Handler(func(*http.Request) (string, bool) { return u.ID, u.ID != "" }).ServeHTTP(w, r2)
	return true
}

// Chain asks each interceptor in turn; the first that answers ends it.
type Chain []interface {
	ServeMachine(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine) bool
}

func (c Chain) ServeMachine(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine) bool {
	for _, i := range c {
		if i != nil && i.ServeMachine(w, r, u, m) {
			return true
		}
	}
	return false
}
