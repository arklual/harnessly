package storehub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// KitGrant is what a machine's person may do with one recorded kit: see it (its files come to the machine), edit it.
type KitGrant struct{ See, Edit, Own bool }

// Kits is the hub's access to UI kits as the store needs it. Without it the kits collection is open to every
// machine of the space, as before.
type Kits interface {
	// KitView is what the machine's person may do with each recorded kit of its space.
	KitView(ctx context.Context, m store.Machine) (map[string]KitGrant, error)
	// ClaimKit records a kit nobody recorded, for the machine's person; owner says whether it is theirs.
	ClaimKit(ctx context.Context, m store.Machine, kit string) (owner bool, err error)
	// OpenLegacyKit lets the team view a kit the person just claimed and has not shared (it was on their machine before
	// kits were shared); anything else is left as it is.
	OpenLegacyKit(ctx context.Context, m store.Machine, kit string) error
}

type kitViews struct {
	mu sync.Mutex
	m  map[string]cachedKitView
}

type cachedKitView struct {
	v  map[string]KitGrant
	at int64
}

// kitOf is the kit a path of the kits collection is in; false for a path that is in none.
func kitOf(p string) (string, bool) {
	id, _, ok := strings.Cut(p, "/")
	return id, ok && id != ""
}

func (s *Store) kitView(ctx context.Context, m store.Machine) (map[string]KitGrant, error) {
	s.kv.mu.Lock()
	c, ok := s.kv.m[m.ID]
	s.kv.mu.Unlock()
	if ok && s.now().UnixMilli()-c.at < viewCache.Milliseconds() {
		return copyKits(c.v), nil
	}
	v, err := s.kits.KitView(ctx, m)
	if err != nil {
		return nil, err
	}
	s.kv.mu.Lock()
	if s.kv.m == nil {
		s.kv.m = map[string]cachedKitView{}
	}
	s.kv.m[m.ID] = cachedKitView{v: copyKits(v), at: s.now().UnixMilli()}
	s.kv.mu.Unlock()
	return v, nil
}

func (s *Store) forgetKitViews() {
	s.kv.mu.Lock()
	s.kv.m = map[string]cachedKitView{}
	s.kv.mu.Unlock()
}

func copyKits(v map[string]KitGrant) map[string]KitGrant {
	out := make(map[string]KitGrant, len(v))
	for k, g := range v {
		out[k] = g
	}
	return out
}

// kitsFilter limits a machine to the kits its person may see and takes writes only to those they may edit. A
// kit nobody recorded is claimed by the first machine to put a file of it in the room: its person becomes the owner and
// it is private to them; the claim is one insert under the table's key, so two machines racing get one owner. The kit
// access of the hub decides, not any file in the room. The seals and the immutability of the versions apply on top.
func (s *Store) kitsFilter(ctx context.Context, m store.Machine, acc *filestore.Access) error {
	view, err := s.kitView(ctx, m)
	if err != nil {
		return err
	}
	acc.Scope = kitScope(view)
	acc.Sees = func(p string) bool {
		id, ok := kitOf(p)
		return ok && view[id].See
	}
	acc.Writes = func(p string) bool {
		id, ok := kitOf(p)
		if !ok {
			return false
		}
		g, known := view[id]
		if !known {
			owner, err := s.kits.ClaimKit(ctx, m, id)
			if err != nil {
				s.log.Warn("store: a kit could not be claimed", "kit", id, "err", err)
				return false
			}
			if owner {
				g = KitGrant{See: true, Edit: true, Own: true}
			}
			view[id] = g
			s.forgetKitViews()
		}
		return g.Edit
	}
	room := acc.Room
	acc.Committed = func(ctx context.Context, e filestore.Entry) {
		id, ok := kitOf(e.Path)
		if !ok || e.Deleted || !view[id].Own || !strings.HasSuffix(e.Path, "/"+filestore.KitSealFile) {
			return
		}
		// A seal that says the version was on the owner's machine before kits were shared opens the kit to the team, for
		// viewing, if the owner has not shared it yet.
		rc, err := s.reader.Open(ctx, room, e.Sha)
		if err != nil {
			return
		}
		raw, err := io.ReadAll(io.LimitReader(rc, filestore.MaxKitSealBytes+1))
		rc.Close()
		if err != nil {
			return
		}
		var doc struct {
			Legacy bool `json:"legacy"`
		}
		if json.Unmarshal(raw, &doc) == nil && doc.Legacy {
			if err := s.kits.OpenLegacyKit(ctx, m, id); err != nil {
				s.log.Warn("store: a legacy kit was not opened to the team", "kit", id, "err", err)
			} else {
				s.forgetKitViews()
			}
		}
	}
	return nil
}

func kitScope(v map[string]KitGrant) string {
	ids := make([]string, 0, len(v))
	for id := range v {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	h := sha256.New()
	_, _ = h.Write([]byte("kit-view/v1\x00"))
	for _, id := range ids {
		fmt.Fprintf(h, "%s:%t:%t\n", id, v[id].See, v[id].Edit)
	}
	return "k1." + hex.EncodeToString(h.Sum(nil)[:12])
}
