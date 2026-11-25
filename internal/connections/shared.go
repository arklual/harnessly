package connections

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"
)

// Shared is the person's record of their connections in a safe every machine of theirs reads (the hub's):
// the list without credentials, and when each forgotten one was removed. The credentials stay where they are (conn-<id>
// in the same safe), so a connection taken from the record works on the new machine at once.
type Shared interface {
	// ReadIndex is the record as it was written; "" when there is none yet.
	ReadIndex() (string, error)
	WriteIndex(value string) error
}

const (
	// pullEvery is how often the list looks for what the person's other machines changed.
	pullEvery = 30 * time.Second
	// deniedWait is how long a record the hub refuses (its switch is off for the person) is not asked for again.
	deniedWait = 10 * time.Minute
	// removedKeep is how long a removal is remembered: a machine that was off longer than that brings the connection back.
	removedKeep = 180 * 24 * time.Hour
)

type sharedRecord struct {
	Version     int                  `json:"version"`
	Connections []Conn               `json:"connections"`
	Removed     map[string]time.Time `json:"removed,omitempty"`
}

// SetShared keeps the person's connections in sh as well, and takes from it the ones their other machines made, changed
// or removed. Without it (nil) the list is this machine's alone.
func (s *Service) SetShared(sh Shared) {
	s.nextMu.Lock()
	s.shared = sh
	s.nextMu.Unlock()
}

func (s *Service) sharedSafe() Shared {
	s.nextMu.Lock()
	defer s.nextMu.Unlock()
	return s.shared
}

// pullSoon syncs when the last sync is old enough: in the background, so the list answers at once, unless wait (a
// machine with no list of its own yet waits for the person's).
func (s *Service) pullSoon(wait bool) {
	if s.sharedSafe() == nil {
		return
	}
	s.nextMu.Lock()
	due := !s.now().Before(s.next)
	if due {
		s.next = s.now().Add(pullEvery)
	}
	s.nextMu.Unlock()
	switch {
	case !due:
	case wait:
		_ = s.Sync()
	default:
		s.kick(func() { _ = s.Sync() })
	}
}

// push gives a change of this machine to the record, in the background; a hub that refused the record lately is not
// asked again for each change, the next pull brings it.
func (s *Service) push() {
	if s.sharedSafe() == nil {
		return
	}
	s.nextMu.Lock()
	off := s.now().Before(s.deniedUntil)
	s.nextMu.Unlock()
	if !off {
		s.kick(func() { _ = s.Sync() })
	}
}

func (s *Service) held(err error) {
	s.nextMu.Lock()
	defer s.nextMu.Unlock()
	if errors.Is(err, ErrDenied) {
		s.deniedUntil = s.now().Add(deniedWait)
		s.next = s.deniedUntil
	}
}

// Sync merges this machine's connections with the person's record: what another machine made or changed later comes
// here, what was removed there is removed here, and the record gets this machine's. When the record cannot be read (the
// hub is away, its switch is off) the list stays as it is, and the next sync tries again.
func (s *Service) Sync() error {
	sh := s.sharedSafe()
	if sh == nil {
		return nil
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	raw, err := sh.ReadIndex()
	if err != nil {
		s.held(err)
		return err
	}
	var rec sharedRecord
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &rec); err != nil {
			return fmt.Errorf("the shared list of connections: %w", err)
		}
	}
	s.mu.Lock()
	f, err := s.readAll()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	list, removed, dropped := merge(f.Connections, f.Removed, rec.Connections, rec.Removed, s.now().UTC())
	if !sameJSON(f.Connections, list) || !sameJSON(f.Removed, removed) {
		f.Connections, f.Removed = list, removed
		err = s.writeAll(f)
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	// The credentials of a duplicate the newer connection replaced are of no use to any machine.
	for _, id := range dropped {
		_ = s.secrets.Remove(secretName(id))
	}
	if !sameJSON(shareable(rec.Connections), shareable(list)) || !sameJSON(rec.Removed, removed) {
		out, err := json.Marshal(sharedRecord{Version: 1, Connections: list, Removed: removed})
		if err != nil {
			return err
		}
		if err := sh.WriteIndex(string(out)); err != nil {
			s.held(err)
			return err
		}
	}
	return nil
}

// changed is when the person last changed the connection (connected, renamed, made primary).
func (c Conn) changed() time.Time {
	if !c.UpdatedAt.IsZero() {
		return c.UpdatedAt
	}
	return c.CreatedAt
}

func sameAccount(a, b Conn) bool {
	return a.Kind == b.Kind && a.BaseURL == b.BaseURL && a.Login == b.Login
}

// merge is the connections of two lists: of the same one the later change, none removed at or after its last change, and
// one per instance and login (the later). dropped are the duplicates it forgot.
func merge(a []Conn, aRemoved map[string]time.Time, b []Conn, bRemoved map[string]time.Time, now time.Time) ([]Conn, map[string]time.Time, []string) {
	removed := map[string]time.Time{}
	for _, m := range []map[string]time.Time{aRemoved, bRemoved} {
		for id, at := range m {
			if now.Sub(at) < removedKeep && at.After(removed[id]) {
				removed[id] = at
			}
		}
	}
	var list []Conn
	at := map[string]int{}
	for _, c := range slices.Concat(a, b) {
		if r, ok := removed[c.ID]; ok && !c.changed().After(r) {
			continue
		}
		if i, ok := at[c.ID]; ok {
			if c.changed().After(list[i].changed()) {
				list[i] = c
			}
			continue
		}
		at[c.ID] = len(list)
		list = append(list, c)
	}
	var out []Conn
	var dropped []string
	for _, c := range list {
		newer := slices.ContainsFunc(list, func(x Conn) bool {
			return x.ID != c.ID && sameAccount(x, c) &&
				(x.changed().After(c.changed()) || x.changed().Equal(c.changed()) && x.ID > c.ID)
		})
		if newer {
			removed[c.ID] = now
			dropped = append(dropped, c.ID)
			continue
		}
		out = append(out, c)
	}
	if len(removed) == 0 {
		removed = nil
	}
	return out, removed, dropped
}

// shareable is a list without what each machine finds out for itself (the last check), so two machines that checked
// at different times do not rewrite the record in turn.
func shareable(list []Conn) []Conn {
	out := make([]Conn, len(list))
	for i, c := range list {
		c.State, c.CheckedAt, c.Me = "", time.Time{}, nil
		out[i] = c
	}
	return out
}

func sameJSON(a, b any) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(x) == string(y)
}

// fresh says whether this machine has no list of its own yet.
func (s *Service) fresh() bool {
	_, err := os.Stat(s.file)
	return errors.Is(err, os.ErrNotExist)
}
