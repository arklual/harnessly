// Package ticket is the one-minute, single-use pass the hub gives a signed-in person to open a machine's own origin
// (subdomain mode): the hub UI asks for one, the machine's origin trades it for its host-only cookie.
package ticket

import (
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// TTL is how long a ticket can be redeemed.
const TTL = 60 * time.Second

// maxTickets bounds the memory: tickets are made on an owner's or member's request, and a flood of requests is
// stopped by the rate limit before it gets here, but a bound costs nothing.
const maxTickets = 10000

// OpenedFor is how long the hub remembers that a person was given a ticket for a machine.
const OpenedFor = 12 * time.Hour

type entry struct {
	userID, machineID string
	expires           time.Time
}

// Store holds the tickets in memory (the hub is one process, and a ticket lives a minute).
type Store struct {
	mu sync.Mutex
	m  map[string]entry
	// opened is when a person was last given a ticket for a machine: proof that they did sign in to its runtime.
	opened map[[2]string]time.Time
	now    func() time.Time
}

// New makes a store; now may be nil.
func New(now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{m: map[string]entry{}, opened: map[[2]string]time.Time{}, now: now}
}

// Issue makes a ticket for the person to open the machine. Only its hash is kept.
func (s *Store) Issue(userID, machineID string) string {
	token := store.RandomBase32(16)
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.m) >= maxTickets {
		s.sweepLocked(now)
		if len(s.m) >= maxTickets {
			s.m = map[string]entry{}
		}
	}
	s.m[store.HashToken(token)] = entry{userID: userID, machineID: machineID, expires: now.Add(TTL)}
	if len(s.opened) >= maxTickets {
		s.opened = map[[2]string]time.Time{}
	}
	s.opened[[2]string{userID, machineID}] = now
	return token
}

// OpenedRecently says whether the hub gave this person a ticket for the machine within OpenedFor.
func (s *Store) OpenedRecently(userID, machineID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	at, ok := s.opened[[2]string{userID, machineID}]
	return ok && s.now().Sub(at) < OpenedFor
}

// Redeem trades a ticket for the person it was made for, once, and only for the machine it was made for.
func (s *Store) Redeem(token, machineID string) (userID string, ok bool) {
	h := store.HashToken(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	e, found := s.m[h]
	if !found {
		return "", false
	}
	delete(s.m, h)
	if e.machineID != machineID || !s.now().Before(e.expires) {
		return "", false
	}
	return e.userID, true
}

// Sweep drops the tickets that expired.
func (s *Store) Sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(s.now())
}

func (s *Store) sweepLocked(now time.Time) {
	for h, e := range s.m {
		if !now.Before(e.expires) {
			delete(s.m, h)
		}
	}
	for k, at := range s.opened {
		if now.Sub(at) >= OpenedFor {
			delete(s.opened, k)
		}
	}
}

// Len is how many tickets are held; for tests.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m)
}
