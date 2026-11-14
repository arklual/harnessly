// Package noticehub answers the notifications API of a machine from the hub's database: the person's
// notices, their reads and their switches live in Postgres, one set for all their machines and devices, and a read on
// one device reaches the others over a stream. It only answers where the rollout switch hub-notices is on for the
// person and the machine; otherwise the request goes on to the machine, whose own store answers as it always does.
package noticehub

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

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
	// OnFor is On for news the hub itself sends (no request): the same switch, asked by email and machine.
	OnFor func(ctx context.Context, email, machineID string) bool
	// UserID finds the hub user of an email; not found is "" and false.
	UserID func(ctx context.Context, email string) (string, bool)
}

// Ring puts a news of the hub into the bell of the person with this email: kind `access`, no chat, a link to
// the page it is about. Only where the switch is on for them on the machine, and the same key never shows twice. It
// reports whether the notice was put.
func (s *Service) Ring(ctx context.Context, machineID, email, key, title, text, link string) bool {
	if s == nil || s.Notices == nil || s.OnFor == nil || s.UserID == nil {
		return false
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || key == "" || !s.OnFor(ctx, email, machineID) {
		return false
	}
	id, ok := s.UserID(ctx, email)
	if !ok || id == "" {
		return false
	}
	now := time.Now()
	if s.Notices.Now != nil {
		now = s.Notices.Now()
	}
	at := now.UnixMilli()
	doc, err := json.Marshal(map[string]any{"key": key, "chatId": "", "kind": "access", "icon": "key-round",
		"title": title, "text": text, "at": at, "link": link})
	if err != nil {
		return false
	}
	fresh, err := s.Notices.Add(ctx, id, notices.Item{Key: key, At: at, Doc: doc}, "")
	return err == nil && fresh
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
