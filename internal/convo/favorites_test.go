package convo

import (
	"context"
	"reflect"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// fav is a rig with three imported tabs, none pinned.
func favRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	r.rt.Import([]ImportTab{
		{ID: "tab-a00001", Session: "s1", Provider: wire.ProviderClaude, Cwd: r.dir},
		{ID: "tab-b00002", Session: "s2", Provider: wire.ProviderClaude, Cwd: r.dir},
		{ID: "tab-c00003", Session: "s3", Provider: wire.ProviderClaude, Cwd: r.dir},
	}, nil)
	return r
}

func (r *rig) favs() wire.Favorites {
	r.t.Helper()
	evs := r.events(wire.EvFavoritesUpsert)
	if len(evs) == 0 {
		r.t.Fatal("no favorites_upsert")
	}
	return evs[len(evs)-1].e.(wire.FavoritesUpsertEv).Favorites
}

func (r *rig) pinned(id string) bool {
	r.t.Helper()
	snap, err := r.rt.Conversations(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	for _, c := range snap.Conversations {
		if c.ID == id {
			return c.Pinned
		}
	}
	r.t.Fatalf("no %s", id)
	return false
}

func sptr(s string) *string { return &s }

func TestAGroupIsMadeRenamedMovedAndDeleted(t *testing.T) {
	r := favRig(t)
	one := r.do(wire.FavGroupCreateCmd{OpID: "g1", Name: "  Релизы "})
	two := r.do(wire.FavGroupCreateCmd{OpID: "g2", Name: "Идеи"})
	if one.R != wire.ResultAccepted || one.ID == nil || two.ID == nil || one.Ch != wire.ChannelConvs {
		t.Fatalf("create: %+v %+v", one, two)
	}
	if evs := r.events(wire.EvFavoritesUpsert); len(evs) != 2 || evs[1].seq != two.Seq {
		t.Fatalf("answer at %d, events %+v", two.Seq, evs)
	}
	f := r.favs()
	if len(f.Groups) != 2 || f.Groups[0].Name != "Релизы" || f.Groups[0].ID != *one.ID {
		t.Fatalf("groups: %+v", f.Groups)
	}
	if err := (wire.FavoritesUpsertEv{Favorites: f}).Validate(); err != nil {
		t.Fatal(err)
	}

	r.do(wire.FavGroupRenameCmd{OpID: "g3", ID: *one.ID, Name: "Выкаты"})
	r.do(wire.FavGroupMoveCmd{OpID: "g4", ID: *two.ID, BeforeID: one.ID})
	f = r.favs()
	if f.Groups[0].ID != *two.ID || f.Groups[1].Name != "Выкаты" {
		t.Fatalf("renamed and moved: %+v", f.Groups)
	}

	r.do(wire.FavGroupDeleteCmd{OpID: "g5", ID: *two.ID})
	if f = r.favs(); len(f.Groups) != 1 || f.Groups[0].ID != *one.ID {
		t.Fatalf("deleted: %+v", f.Groups)
	}
	for _, cmd := range []wire.Command{
		wire.FavGroupRenameCmd{OpID: "x1", ID: "fg-nonexist", Name: "x"},
		wire.FavGroupDeleteCmd{OpID: "x2", ID: *two.ID},
		wire.FavGroupMoveCmd{OpID: "x3", ID: *one.ID, BeforeID: sptr("fg-nonexist")},
	} {
		if res := r.do(cmd); res.R != wire.ResultRefused || res.Code != wire.RefuseNotFound {
			t.Fatalf("%T: %+v", cmd, res)
		}
	}
}

func TestPlacingAChatPinsItAndPutsItInTheGroup(t *testing.T) {
	r := favRig(t)
	g := r.do(wire.FavGroupCreateCmd{OpID: "g1", Name: "Релизы"})
	if r.pinned("tab-a00001") {
		t.Fatal("pinned already")
	}
	res := r.do(wire.FavPlaceCmd{OpID: "p1", ID: "tab-a00001", GroupID: g.ID})
	if res.R != wire.ResultAccepted || res.Ch != wire.ChannelConvs {
		t.Fatalf("place: %+v", res)
	}
	if !r.pinned("tab-a00001") {
		t.Fatal("placing does not pin")
	}
	// The pin goes out before the layout, so the page that waits for the layout's seq has both.
	ups := r.events(wire.EvConversationUpsert)
	favs := r.events(wire.EvFavoritesUpsert)
	if len(ups) == 0 || ups[len(ups)-1].seq >= favs[len(favs)-1].seq || favs[len(favs)-1].seq != res.Seq {
		t.Fatalf("upserts %+v, favorites %+v, answer %d", ups, favs, res.Seq)
	}
	if f := r.favs(); len(f.Groups[0].Chats) != 1 || f.Groups[0].Chats[0] != "tab-a00001" || len(f.Loose) != 0 {
		t.Fatalf("layout: %+v", f)
	}

	r.do(wire.FavPlaceCmd{OpID: "p2", ID: "tab-b00002", GroupID: g.ID, BeforeID: sptr("tab-a00001")})
	r.do(wire.FavPlaceCmd{OpID: "p3", ID: "tab-c00003"})
	f := r.favs()
	if !reflect.DeepEqual(f.Groups[0].Chats, []string{"tab-b00002", "tab-a00001"}) || !reflect.DeepEqual(f.Loose, []string{"tab-c00003"}) {
		t.Fatalf("layout: %+v", f)
	}

	if res := r.do(wire.FavPlaceCmd{OpID: "p4", ID: "tab-a00001", GroupID: sptr("fg-nonexist")}); res.Code != wire.RefuseNotFound {
		t.Fatalf("a group that is not there: %+v", res)
	}
	if res := r.do(wire.FavPlaceCmd{OpID: "p5", ID: "tab-nobody"}); res.Code != wire.RefuseNotFound {
		t.Fatalf("a chat that is not there: %+v", res)
	}
}

// A chat pinned by the old conversation.pin is a favorite without being placed; an unpinned one leaves the layout at
// the next change, and deleting a group keeps its chats pinned, ungrouped.
func TestThePinOfOlderPagesAndDeletingAGroupKeepTheChats(t *testing.T) {
	r := favRig(t)
	g := r.do(wire.FavGroupCreateCmd{OpID: "g1", Name: "Релизы"})
	r.do(wire.FavPlaceCmd{OpID: "p1", ID: "tab-a00001", GroupID: g.ID})
	r.do(wire.FavPlaceCmd{OpID: "p2", ID: "tab-b00002", GroupID: g.ID})
	r.do(wire.ConversationPinCmd{OpID: "pin", ID: "tab-c00003", Pinned: true})
	r.do(wire.ConversationPinCmd{OpID: "unpin", ID: "tab-a00001", Pinned: false})

	r.do(wire.FavGroupRenameCmd{OpID: "g2", ID: *g.ID, Name: "Выкаты"})
	f := r.favs()
	if !reflect.DeepEqual(f.Groups[0].Chats, []string{"tab-b00002"}) || !reflect.DeepEqual(f.Loose, []string{"tab-c00003"}) {
		t.Fatalf("after the older pins: %+v", f)
	}

	r.do(wire.FavGroupDeleteCmd{OpID: "g3", ID: *g.ID})
	f = r.favs()
	if len(f.Groups) != 0 || !reflect.DeepEqual(f.Loose, []string{"tab-b00002", "tab-c00003"}) {
		t.Fatalf("after the delete: %+v", f)
	}
	if !r.pinned("tab-b00002") {
		t.Fatal("deleting a group unpins its chats")
	}
}

func TestTheLayoutIsKeptAndRidesTheSnapshot(t *testing.T) {
	r := favRig(t)
	if snap, _ := r.rt.Conversations(context.Background()); snap.Favorites != nil {
		t.Fatalf("nothing laid out, nothing sent: %+v", snap.Favorites)
	}
	g := r.do(wire.FavGroupCreateCmd{OpID: "g1", Name: "Релизы"})
	r.do(wire.FavPlaceCmd{OpID: "p1", ID: "tab-a00001", GroupID: g.ID})
	r.do(wire.FavPlaceCmd{OpID: "p2", ID: "tab-b00002"})
	want := r.favs()

	r.restart()
	snap, err := r.rt.Conversations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Favorites == nil || !reflect.DeepEqual(*snap.Favorites, want) {
		t.Fatalf("after the restart: %+v, want %+v", snap.Favorites, want)
	}
	if err := snap.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTheGroupsAreFew(t *testing.T) {
	r := newRig(t)
	for i := 0; i < wire.FavGroupsMax; i++ {
		if res := r.do(wire.FavGroupCreateCmd{OpID: "g" + string(rune('A'+i/26)) + string(rune('a'+i%26)), Name: "g"}); res.R != wire.ResultAccepted {
			t.Fatalf("group %d: %+v", i, res)
		}
	}
	if res := r.do(wire.FavGroupCreateCmd{OpID: "over", Name: "g"}); res.R != wire.ResultRefused || res.Code != wire.RefuseConflict {
		t.Fatalf("over the limit: %+v", res)
	}
}
