package convo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The favorites are the pinned conversations; what the runtime keeps besides each conversation's `pinned` is how they
// are laid out: the groups, their order and the order of the chats in them (wire.Favorites). Every change publishes
// the whole layout as favorites_upsert, and the answer names that event's seq.

// pinnedIDs are the pinned conversations, the newest first, as each actor holds them now (the list a snapshot is made of).
func (r *Runtime) pinnedIDs() []string {
	snap, err := r.Conversations(context.Background())
	if err != nil {
		return nil
	}
	ids := []string{}
	for _, c := range snap.Conversations {
		if c.Pinned {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// favEdit changes the layout under the lock: edit gets the layout kept down to the pinned conversations (and also
// `extra`, one just pinned) and answers the new layout, or false to refuse with RefuseNotFound.
func (r *Runtime) favEdit(extra string, edit func(wire.Favorites) (wire.Favorites, bool)) wire.CmdResult {
	pinned := r.pinnedIDs()
	r.favMu.Lock()
	defer r.favMu.Unlock()
	if extra != "" {
		rest := pinned[:0:0]
		for _, id := range pinned {
			if id != extra {
				rest = append(rest, id)
			}
		}
		pinned = append([]string{extra}, rest...)
	}
	next, ok := edit(r.favs.Normalized(pinned))
	if !ok {
		return wire.Refused(wire.RefuseNotFound)
	}
	r.favs = next.Normalized(pinned)
	seq := r.d.Publisher.Publish(wire.ChannelConvs, wire.FavoritesUpsertEv{Favorites: r.favs.Clone()})
	if r.d.Store != nil {
		if err := r.d.Store.saveFavorites(r.favs); err != nil {
			r.d.Log.Warn("favorites not saved", "err", err)
		}
	}
	return wire.Accepted(wire.ChannelConvs, seq)
}

func newFavGroupID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "fg-" + hex.EncodeToString(b[:])
}

func (r *Runtime) favGroup(cmd wire.Command) wire.CmdResult {
	switch m := cmd.(type) {
	case wire.FavGroupCreateCmd:
		id := newFavGroupID()
		full := false
		res := r.favEdit("", func(f wire.Favorites) (wire.Favorites, bool) {
			full = len(f.Groups) >= wire.FavGroupsMax
			if full {
				return f, false
			}
			return f.WithGroup(wire.FavGroup{ID: id, Name: strings.TrimSpace(m.Name)}), true
		})
		if full {
			return wire.Refused(wire.RefuseConflict)
		}
		if res.R != wire.ResultRefused {
			res.ID = &id
		}
		return res
	case wire.FavGroupRenameCmd:
		return r.favEdit("", func(f wire.Favorites) (wire.Favorites, bool) { return f.Renamed(m.ID, strings.TrimSpace(m.Name)) })
	case wire.FavGroupDeleteCmd:
		return r.favEdit("", func(f wire.Favorites) (wire.Favorites, bool) { return f.Without(m.ID) })
	case wire.FavGroupMoveCmd:
		return r.favEdit("", func(f wire.Favorites) (wire.Favorites, bool) { return f.GroupMoved(m.ID, m.BeforeID) })
	}
	return wire.Refused(wire.RefuseUnsupported)
}

// favPlace pins the conversation when it is not pinned (the actor does, as conversation.pin does) and puts it in its
// place in the layout. When the second step cannot go (the group is gone) the conversation stays pinned, ungrouped.
func (r *Runtime) favPlace(m wire.FavPlaceCmd) wire.CmdResult {
	if r.actor(m.ID) == nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	if m.GroupID != nil {
		r.favMu.Lock()
		has := r.favs.HasGroup(*m.GroupID)
		r.favMu.Unlock()
		if !has {
			return wire.Refused(wire.RefuseNotFound)
		}
	}
	gone := false
	err := r.call(context.Background(), m.ID, func(c *conv, now time.Time) {
		if c.deletedAt != 0 {
			gone = true
			return
		}
		if !c.rec.Pinned {
			c.do("", wire.ConversationPinCmd{OpID: m.OpID, ID: m.ID, Pinned: true}, now)
		}
	})
	if err != nil {
		return wire.Refused(wire.RefuseTimeout)
	}
	if gone {
		return wire.Refused(wire.RefuseNotFound)
	}
	return r.favEdit(m.ID, func(f wire.Favorites) (wire.Favorites, bool) { return f.Placed(m.ID, m.GroupID, m.BeforeID) })
}
