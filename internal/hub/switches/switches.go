// Package switches is the rollout switches: a feature turned on or off for a person on a machine without a release
// (docs/hub/rollout-switches.md). A row names a feature, a person's email and a machine id; "*" in either matches anyone.
// The most specific row wins (both named, then the person, then the machine, then both "*"); no row means off, so a
// new feature ships dark and is turned on pair by pair, then with a "*" row for everyone.
package switches

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// CacheTTL is how long an answer is kept; a change made by this process is seen at once, one made by another replica
// within this time.
const CacheTTL = 5 * time.Second

// Gate answers whether a feature is on for a person on a machine.
type Gate interface {
	On(ctx context.Context, feature, person, machine string) bool
}

type entry struct {
	on    bool
	names []string
	at    time.Time
}

// Store is the Gate over the rollout_switches table.
type Store struct {
	db    *store.DB
	now   func() time.Time
	mu    sync.Mutex
	cache map[string]entry
}

// New makes the gate; now is the clock, nil means time.Now.
func New(db *store.DB, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, now: now, cache: map[string]entry{}}
}

// rank orders the rows of one feature: the higher, the more specific (the person counts more than the machine).
func rank(w store.Switch) int {
	r := 0
	if w.Person != store.Wildcard {
		r += 2
	}
	if w.Machine != store.Wildcard {
		r++
	}
	return r
}

// decide is the answer of the most specific row; ok is false when no row speaks.
func decide(rows []store.Switch) (on, ok bool) {
	best := -1
	for _, w := range rows {
		if r := rank(w); r > best {
			best, on, ok = r, w.Enabled, true
		}
	}
	return on, ok
}

func (s *Store) get(key string) (entry, bool) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, hit := s.cache[key]
	if hit && now.Sub(e.at) < CacheTTL && now.After(e.at.Add(-CacheTTL)) {
		return e, true
	}
	return entry{}, false
}

func (s *Store) put(key string, e entry) {
	e.at = s.now()
	s.mu.Lock()
	s.cache[key] = e
	s.mu.Unlock()
}

// On is whether the feature is on for the person on the machine. A table that cannot be read means off.
func (s *Store) On(ctx context.Context, feature, person, machine string) bool {
	person = strings.ToLower(person)
	key := "f|" + feature + "|" + person + "|" + machine
	if e, ok := s.get(key); ok {
		return e.on
	}
	rows, err := s.db.S().SwitchesFor(ctx, feature, false, person, machine)
	if err != nil {
		return false
	}
	on, _ := decide(rows)
	s.put(key, entry{on: on})
	return on
}

// ByPrefix are the features named <prefix><name> that are on for the person on the machine, as the sorted names after
// the prefix; the most specific row of each feature wins.
func (s *Store) ByPrefix(ctx context.Context, prefix, person, machine string) []string {
	person = strings.ToLower(person)
	key := "p|" + prefix + "|" + person + "|" + machine
	if e, ok := s.get(key); ok {
		return e.names
	}
	rows, err := s.db.S().SwitchesFor(ctx, prefix, true, person, machine)
	if err != nil {
		return nil
	}
	by := map[string][]store.Switch{}
	for _, w := range rows {
		by[w.Feature] = append(by[w.Feature], w)
	}
	out := []string{}
	for f, rs := range by {
		if on, _ := decide(rs); on {
			out = append(out, strings.TrimPrefix(f, prefix))
		}
	}
	sort.Strings(out)
	s.put(key, entry{names: out})
	return out
}

// Set keeps a row and drops the cached answers.
func (s *Store) Set(ctx context.Context, feature, person, machine string, enabled bool, note, by string) error {
	err := s.db.S().SetSwitch(ctx, store.Switch{Feature: feature, Person: person, Machine: machine, Enabled: enabled, Note: note, UpdatedBy: by, UpdatedAt: s.now()})
	s.reset()
	return err
}

// Remove drops a row and the cached answers.
func (s *Store) Remove(ctx context.Context, feature, person, machine string) error {
	err := s.db.S().DeleteSwitch(ctx, feature, person, machine)
	s.reset()
	return err
}

// List are the rows, of the features that start with prefix ("" for all).
func (s *Store) List(ctx context.Context, prefix string) ([]store.Switch, error) {
	return s.db.S().ListSwitches(ctx, prefix)
}

func (s *Store) reset() {
	s.mu.Lock()
	s.cache = map[string]entry{}
	s.mu.Unlock()
}

// Static is a Gate with fixed answers for tests of other packages: Static{"feature": true} is on for everyone.
type Static map[string]bool

// On implements Gate.
func (g Static) On(_ context.Context, feature, _, _ string) bool { return g[feature] }
