package wire

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// FavGroupNameMax is the longest name of a group in the favorites, in characters.
const FavGroupNameMax = 60

// FavGroupsMax is how many groups the favorites hold.
const FavGroupsMax = 50

// FavGroup is a named group inside the favorites; Chats are the ids of its conversations in the order shown.
type FavGroup struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Chats []string `json:"chats"`
}

// Favorites is how the pinned conversations are laid out: the groups in their order, then the conversations that
// belong to none (Loose) in theirs. Whether a conversation is a favorite is its own `pinned`; this is only where it
// stands. A pinned conversation the layout does not name shows first among the loose, newest first, and an id the
// layout names for a conversation that is not pinned (or gone) is ignored: both ways the pin of an older page stays
// right without the layout being told.
type Favorites struct {
	Groups []FavGroup `json:"groups"`
	Loose  []string   `json:"loose"`
}

var favGroupID = regexp.MustCompile(`^fg-[A-Za-z0-9]{4,40}$`)

// ValidFavGroupID reports whether id has the shape of a favorites group's id.
func ValidFavGroupID(id string) bool { return favGroupID.MatchString(id) }

// EmptyFavorites is a layout with no groups and no one placed, as a machine starts with.
func EmptyFavorites() Favorites { return Favorites{Groups: []FavGroup{}, Loose: []string{}} }

// Clone is a copy that shares nothing with f; its lists are never nil.
func (f Favorites) Clone() Favorites {
	out := Favorites{Groups: make([]FavGroup, 0, len(f.Groups)), Loose: slices.Clone(f.Loose)}
	if out.Loose == nil {
		out.Loose = []string{}
	}
	for _, g := range f.Groups {
		g.Chats = slices.Clone(g.Chats)
		if g.Chats == nil {
			g.Chats = []string{}
		}
		out.Groups = append(out.Groups, g)
	}
	return out
}

// Normalized keeps only the conversations of pinned (newest first), once each, where the layout had them, and puts the
// pinned ones it did not name at the front of the loose.
func (f Favorites) Normalized(pinned []string) Favorites {
	out := f.Clone()
	is := make(map[string]bool, len(pinned))
	for _, id := range pinned {
		is[id] = true
	}
	seen := map[string]bool{}
	keep := func(ids []string) []string {
		kept := ids[:0]
		for _, id := range ids {
			if is[id] && !seen[id] {
				seen[id] = true
				kept = append(kept, id)
			}
		}
		return kept
	}
	for i := range out.Groups {
		out.Groups[i].Chats = keep(out.Groups[i].Chats)
	}
	out.Loose = keep(out.Loose)
	var fresh []string
	for _, id := range pinned {
		if !seen[id] {
			seen[id] = true
			fresh = append(fresh, id)
		}
	}
	out.Loose = append(fresh, out.Loose...)
	return out
}

// MarshalJSON writes the lists as arrays even when nil.
func (f Favorites) MarshalJSON() ([]byte, error) {
	type plain Favorites
	c := f.Clone()
	return json.Marshal(plain(c))
}

func indexOf(ids []string, id string) int { return slices.Index(ids, id) }

// insertBefore puts id into ids before `before`, or last when before is nil or not there.
func insertBefore(ids []string, id string, before *string) []string {
	at := len(ids)
	if before != nil {
		if i := indexOf(ids, *before); i >= 0 {
			at = i
		}
	}
	return slices.Insert(ids, at, id)
}

// WithGroup adds a group last.
func (f Favorites) WithGroup(g FavGroup) Favorites {
	out := f.Clone()
	if g.Chats == nil {
		g.Chats = []string{}
	}
	out.Groups = append(out.Groups, g)
	return out
}

func (f Favorites) groupAt(id string) int {
	return slices.IndexFunc(f.Groups, func(g FavGroup) bool { return g.ID == id })
}

// HasGroup reports whether the layout has the group.
func (f Favorites) HasGroup(id string) bool { return f.groupAt(id) >= 0 }

// Renamed is f with a group's name changed; false when there is no such group.
func (f Favorites) Renamed(id, name string) (Favorites, bool) {
	out := f.Clone()
	i := out.groupAt(id)
	if i < 0 {
		return f, false
	}
	out.Groups[i].Name = name
	return out, true
}

// Without removes a group; its conversations become the first of the loose, in their order.
func (f Favorites) Without(id string) (Favorites, bool) {
	out := f.Clone()
	i := out.groupAt(id)
	if i < 0 {
		return f, false
	}
	out.Loose = append(slices.Clone(out.Groups[i].Chats), out.Loose...)
	out.Groups = slices.Delete(out.Groups, i, i+1)
	return out, true
}

// GroupMoved puts a group before another (before), or last when before is nil; false when either is not there.
func (f Favorites) GroupMoved(id string, before *string) (Favorites, bool) {
	out := f.Clone()
	i := out.groupAt(id)
	if i < 0 || before != nil && !out.HasGroup(*before) {
		return f, false
	}
	g := out.Groups[i]
	out.Groups = slices.Delete(out.Groups, i, i+1)
	at := len(out.Groups)
	if before != nil && *before != id {
		at = out.groupAt(*before)
	}
	if before != nil && *before == id {
		at = i
	}
	out.Groups = slices.Insert(out.Groups, at, g)
	return out, true
}

// Placed puts the conversation id, which is pinned, into group (nil: the loose) before another of that place or last.
// It is taken out of the place it had; false when the group is not there.
func (f Favorites) Placed(id string, group, before *string) (Favorites, bool) {
	if group != nil && !f.HasGroup(*group) {
		return f, false
	}
	out := f.Clone()
	if before != nil && *before == id {
		// Dropped on itself: it stays where it stands in that place, else it goes last.
		here := out.Loose
		if group != nil {
			here = out.Groups[out.groupAt(*group)].Chats
		}
		if indexOf(here, id) >= 0 {
			return out, true
		}
	}
	if i := indexOf(out.Loose, id); i >= 0 {
		out.Loose = slices.Delete(out.Loose, i, i+1)
	}
	for k := range out.Groups {
		if i := indexOf(out.Groups[k].Chats, id); i >= 0 {
			out.Groups[k].Chats = slices.Delete(out.Groups[k].Chats, i, i+1)
		}
	}
	if before != nil && *before == id {
		before = nil
	}
	if group == nil {
		out.Loose = insertBefore(out.Loose, id, before)
		return out, true
	}
	k := out.groupAt(*group)
	out.Groups[k].Chats = insertBefore(out.Groups[k].Chats, id, before)
	return out, true
}

// ValidFavName reports whether a group's name is one line of at most FavGroupNameMax characters, not blank.
func ValidFavName(name string) bool {
	return strings.TrimSpace(name) != "" && utf8.RuneCountInString(name) <= FavGroupNameMax && !strings.ContainsAny(name, "\r\n")
}

func (g FavGroup) Validate() error {
	errs := []error{}
	if !ValidFavGroupID(g.ID) {
		errs = append(errs, fmt.Errorf("id: %q is not a group id", g.ID))
	}
	if !ValidFavName(g.Name) {
		errs = append(errs, errors.New("name: one line of at most 60 characters"))
	}
	for _, id := range g.Chats {
		errs = append(errs, convID("chats", id))
	}
	return errors.Join(errs...)
}

func (f Favorites) Validate() error {
	errs := []error{each("groups", f.Groups)}
	for _, id := range f.Loose {
		errs = append(errs, convID("loose", id))
	}
	return errors.Join(errs...)
}

func (e FavoritesUpsertEv) Validate() error { return under("favorites", e.Favorites.Validate()) }

func favGroupRef(name, id string) error {
	if !ValidFavGroupID(id) {
		return fmt.Errorf("%s: %q is not a group id", name, id)
	}
	return nil
}

func favGroupRefPtr(name string, id *string) error {
	if id == nil {
		return nil
	}
	return favGroupRef(name, *id)
}

func (c FavGroupCreateCmd) Validate() error {
	errs := []error{need("opId", c.OpID)}
	if !ValidFavName(c.Name) {
		errs = append(errs, errors.New("name: one line of at most 60 characters"))
	}
	return errors.Join(errs...)
}

func (c FavGroupRenameCmd) Validate() error {
	errs := []error{need("opId", c.OpID), favGroupRef("id", c.ID)}
	if !ValidFavName(c.Name) {
		errs = append(errs, errors.New("name: one line of at most 60 characters"))
	}
	return errors.Join(errs...)
}

func (c FavGroupDeleteCmd) Validate() error {
	return errors.Join(need("opId", c.OpID), favGroupRef("id", c.ID))
}

func (c FavGroupMoveCmd) Validate() error {
	return errors.Join(need("opId", c.OpID), favGroupRef("id", c.ID), favGroupRefPtr("beforeId", c.BeforeID))
}

func (c FavPlaceCmd) Validate() error {
	errs := []error{byID(c.OpID, c.ID), favGroupRefPtr("groupId", c.GroupID)}
	if c.BeforeID != nil {
		errs = append(errs, convID("beforeId", *c.BeforeID))
	}
	return errors.Join(errs...)
}
