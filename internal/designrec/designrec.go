// Package designrec keeps the records of a library's entities in the store (docs/hub/store.md): a project, a
// design system, a video, a deck, each a small file `<id>.json` of the collection of its kind. A runtime writes them on
// every change and once on connect; the same code serves a hub's store and the embedded store of a runtime without a
// hub (SQLite or Postgres), since both are filesync clients of a collection. Reconcile says whether the store has what
// the machine has.
package designrec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/filesync"
)

// RefusedRetry is how long a write the store refused (a project none of the machine's people may edit) is not
// asked again while it stays as it was: a refused record was written again on every change of the library, about twice
// a minute, each a 403 at the hub. A role given later is picked up within this time.
const RefusedRetry = time.Hour

// Store is a collection of the store as a client has it (filesync.Client).
type Store interface {
	Put(ctx context.Context, path string, data []byte, author string) (filestore.Entry, error)
	Current(ctx context.Context, path string) (*filestore.Entry, error)
	Changes(ctx context.Context, since int64, wait time.Duration) (filestore.Changes, error)
}

// Clients gives the client of a collection by its kind.
type Clients func(kind string) Store

// CollectionOf is the collection that holds the records of a project of the library's kind (wire.DesignKind and the
// hub's own words: ds, video, slides/deck; anything else is a project).
func CollectionOf(kind string) string {
	switch kind {
	case "ds", "system":
		return filestore.KindSystems
	case "video":
		return filestore.KindVideos
	case "slides", "deck":
		return filestore.KindDecks
	}
	return filestore.KindProjects
}

// Syncer writes records and remembers what it wrote, so a record that did not change is not written again; a record the
// store refused is not asked again for RefusedRetry while it stays the same.
type Syncer struct {
	Clients Clients
	Author  string
	// Now is the clock (time.Now when nil).
	Now func() time.Time

	mu      sync.Mutex
	sent    map[string]string  // collection/id → sha of the record written
	refused map[string]refusal // collection/id → the write the store refused
}

type refusal struct {
	sum string
	at  time.Time
}

func (s *Syncer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// settled: what is under key now was written, or was refused as it is a short while ago.
func (s *Syncer) settled(key, sum string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sent[key] == sum {
		return true
	}
	r, ok := s.refused[key]
	return ok && r.sum == sum && s.now().Sub(r.at) < RefusedRetry
}

func (s *Syncer) remember(key, sum string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sent == nil {
		s.sent = map[string]string{}
	}
	s.sent[key] = sum
	delete(s.refused, key)
}

func (s *Syncer) refuse(key, sum string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refused == nil {
		s.refused = map[string]refusal{}
	}
	s.refused[key] = refusal{sum: sum, at: s.now()}
}

// Refused is how many writes the store refuses now (collection/id).
func (s *Syncer) Refused() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.refused)
}

func body(r filestore.Record) ([]byte, string, error) {
	b, err := r.Marshal()
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:]), nil
}

// Push writes the records the store does not have as they are now and returns how many it wrote. A record the store
// refuses is skipped and remembered; any other failure ends the push (the caller asks again); what was written stays
// written.
func (s *Syncer) Push(ctx context.Context, recs []filestore.Record) (int, error) {
	n := 0
	for _, r := range recs {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		if !(filestore.Records{}).Synced(filestore.RecordPath(r.ID)) {
			continue
		}
		coll := CollectionOf(r.Kind)
		b, sum, err := body(r)
		if err != nil {
			return n, err
		}
		key := coll + "/" + r.ID
		if s.settled(key, sum) {
			continue
		}
		c := s.Clients(coll)
		cur, err := c.Current(ctx, filestore.RecordPath(r.ID))
		if err != nil {
			return n, err
		}
		if cur == nil || cur.Deleted || cur.Sha != sum {
			if _, err := c.Put(ctx, filestore.RecordPath(r.ID), b, s.Author); errors.Is(err, filesync.ErrRefused) {
				// Not this machine's to write: the others go on, this one waits for a change or RefusedRetry.
				s.refuse(key, sum)
				continue
			} else if err != nil {
				return n, fmt.Errorf("record %s: %w", key, err)
			}
			n++
		}
		s.remember(key, sum)
	}
	return n, nil
}

// Count is how many entities of a collection the machine has and the store has.
type Count struct {
	Machine int `json:"machine"`
	Store   int `json:"store"`
}

// Report is the check «the store has as many as the machine»: the counts per collection and the entities the store
// lacks (Missing, `<collection>/<id>`) or has that the machine does not (Extra: another machine's, or one deleted here).
type Report struct {
	OK      bool             `json:"ok"`
	Counts  map[string]Count `json:"counts"`
	Missing []string         `json:"missing,omitempty"`
	Extra   []string         `json:"extra,omitempty"`
}

// Reconcile compares the machine's records with what the store holds. Missing decides OK: the store may hold more (a
// teammate's projects reach it too), never less than the machine has.
func (s *Syncer) Reconcile(ctx context.Context, recs []filestore.Record) (Report, error) {
	rep := Report{Counts: map[string]Count{}}
	want := map[string]map[string]bool{}
	for _, k := range filestore.RecordKinds {
		want[k] = map[string]bool{}
	}
	for _, r := range recs {
		want[CollectionOf(r.Kind)][r.ID] = true
	}
	for _, k := range filestore.RecordKinds {
		have, err := held(ctx, s.Clients(k))
		if err != nil {
			return rep, fmt.Errorf("%s: %w", k, err)
		}
		rep.Counts[k] = Count{Machine: len(want[k]), Store: len(have)}
		for id := range want[k] {
			if !have[id] {
				rep.Missing = append(rep.Missing, k+"/"+id)
			}
		}
		for id := range have {
			if !want[k][id] {
				rep.Extra = append(rep.Extra, k+"/"+id)
			}
		}
	}
	sort.Strings(rep.Missing)
	sort.Strings(rep.Extra)
	rep.OK = len(rep.Missing) == 0
	return rep, nil
}

// held is the ids of the records a collection has now.
func held(ctx context.Context, c Store) (map[string]bool, error) {
	newest := map[string]filestore.Entry{}
	var since int64
	for {
		ch, err := c.Changes(ctx, since, 0)
		if err != nil {
			return nil, err
		}
		for _, e := range ch.Entries {
			if old, ok := newest[e.Path]; !ok || e.Rev >= old.Rev {
				newest[e.Path] = e
			}
			since = max(since, e.Rev)
		}
		if !ch.More || len(ch.Entries) == 0 {
			break
		}
	}
	out := map[string]bool{}
	for p, e := range newest {
		if id, ok := filestore.RecordID(p); ok && !e.Deleted {
			out[id] = true
		}
	}
	return out, nil
}

// PushCover puts a project's cover picture into the covers collection when it is not there as it is now. It
// returns whether it wrote; a picture over the cap is left out.
func (s *Syncer) PushCover(ctx context.Context, id string, data []byte) (bool, error) {
	if len(data) == 0 || len(data) > filestore.MaxCoverBytes || !(filestore.Covers{}).Synced(filestore.CoverPath(id)) {
		return false, nil
	}
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])
	key := filestore.KindCovers + "/" + id
	if s.settled(key, hexSum) {
		return false, nil
	}
	c := s.Clients(filestore.KindCovers)
	cur, err := c.Current(ctx, filestore.CoverPath(id))
	if err != nil {
		return false, err
	}
	wrote := false
	if cur == nil || cur.Deleted || cur.Sha != hexSum {
		if _, err := c.Put(ctx, filestore.CoverPath(id), data, s.Author); errors.Is(err, filesync.ErrRefused) {
			s.refuse(key, hexSum)
			return false, nil
		} else if err != nil {
			return false, fmt.Errorf("cover %s: %w", id, err)
		}
		wrote = true
	}
	s.remember(key, hexSum)
	return wrote, nil
}
