package wire

import (
	"encoding/json"
	"reflect"
	"testing"
)

func sp(s string) *string { return &s }

func layout(groups []FavGroup, loose ...string) Favorites {
	if groups == nil {
		groups = []FavGroup{}
	}
	if loose == nil {
		loose = []string{}
	}
	return Favorites{Groups: groups, Loose: loose}
}

func grp(id string, chats ...string) FavGroup {
	if chats == nil {
		chats = []string{}
	}
	return FavGroup{ID: id, Name: id, Chats: chats}
}

func TestFavoritesNormalized(t *testing.T) {
	f := layout([]FavGroup{grp("fg-aaaa", "tab-a", "tab-gone", "tab-b")}, "tab-c", "tab-a", "tab-unpinned")
	// pinned, newest first: d is new to the layout, e too.
	got := f.Normalized([]string{"tab-d", "tab-a", "tab-b", "tab-c", "tab-e"})
	want := layout([]FavGroup{grp("fg-aaaa", "tab-a", "tab-b")}, "tab-d", "tab-e", "tab-c")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if len(f.Groups[0].Chats) != 3 {
		t.Fatal("normalizing changed the original")
	}
}

func TestFavoritesPlaced(t *testing.T) {
	base := layout([]FavGroup{grp("fg-aaaa", "tab-a", "tab-b"), grp("fg-bbbb")}, "tab-c", "tab-d")
	cases := []struct {
		name          string
		id            string
		group, before *string
		want          Favorites
	}{
		{"into a group before a chat", "tab-c", sp("fg-aaaa"), sp("tab-b"),
			layout([]FavGroup{grp("fg-aaaa", "tab-a", "tab-c", "tab-b"), grp("fg-bbbb")}, "tab-d")},
		{"into an empty group", "tab-d", sp("fg-bbbb"), nil,
			layout([]FavGroup{grp("fg-aaaa", "tab-a", "tab-b"), grp("fg-bbbb", "tab-d")}, "tab-c")},
		{"out of a group to the loose, first", "tab-b", nil, sp("tab-c"),
			layout([]FavGroup{grp("fg-aaaa", "tab-a"), grp("fg-bbbb")}, "tab-b", "tab-c", "tab-d")},
		{"within the loose to the end", "tab-c", nil, nil,
			layout([]FavGroup{grp("fg-aaaa", "tab-a", "tab-b"), grp("fg-bbbb")}, "tab-d", "tab-c")},
		{"before a chat of another place goes last", "tab-c", sp("fg-aaaa"), sp("tab-d"),
			layout([]FavGroup{grp("fg-aaaa", "tab-a", "tab-b", "tab-c"), grp("fg-bbbb")}, "tab-d")},
		{"before itself stays", "tab-a", sp("fg-aaaa"), sp("tab-a"),
			layout([]FavGroup{grp("fg-aaaa", "tab-a", "tab-b"), grp("fg-bbbb")}, "tab-c", "tab-d")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := base.Placed(c.id, c.group, c.before)
			if !ok || !reflect.DeepEqual(got, c.want) {
				t.Fatalf("ok=%v got %+v, want %+v", ok, got, c.want)
			}
		})
	}
	if _, ok := base.Placed("tab-c", sp("fg-zzzz"), nil); ok {
		t.Fatal("a group that is not there takes no one")
	}
}

func TestFavoritesGroups(t *testing.T) {
	base := layout([]FavGroup{grp("fg-aaaa", "tab-a"), grp("fg-bbbb", "tab-b"), grp("fg-cccc")}, "tab-c")

	moved, ok := base.GroupMoved("fg-cccc", sp("fg-aaaa"))
	if !ok || moved.Groups[0].ID != "fg-cccc" || moved.Groups[1].ID != "fg-aaaa" || moved.Groups[2].ID != "fg-bbbb" {
		t.Fatalf("move to the front: %+v", moved.Groups)
	}
	moved, ok = base.GroupMoved("fg-aaaa", nil)
	if !ok || moved.Groups[2].ID != "fg-aaaa" || moved.Groups[0].ID != "fg-bbbb" {
		t.Fatalf("move last: %+v", moved.Groups)
	}
	moved, ok = base.GroupMoved("fg-bbbb", sp("fg-bbbb"))
	if !ok || !reflect.DeepEqual(moved, base) {
		t.Fatalf("before itself stays: %+v", moved.Groups)
	}
	if _, ok := base.GroupMoved("fg-aaaa", sp("fg-zzzz")); ok {
		t.Fatal("before a group that is not there is refused")
	}

	renamed, ok := base.Renamed("fg-bbbb", "Релизы")
	if !ok || renamed.Groups[1].Name != "Релизы" || base.Groups[1].Name != "fg-bbbb" {
		t.Fatalf("rename: %+v", renamed.Groups)
	}

	gone, ok := base.Without("fg-aaaa")
	want := layout([]FavGroup{grp("fg-bbbb", "tab-b"), grp("fg-cccc")}, "tab-a", "tab-c")
	if !ok || !reflect.DeepEqual(gone, want) {
		t.Fatalf("delete keeps its chats, loose first: %+v", gone)
	}
	if _, ok := base.Without("fg-zzzz"); ok {
		t.Fatal("a group that is not there")
	}
}

func TestFavoritesEncodeAsArrays(t *testing.T) {
	b, err := json.Marshal(Favorites{})
	if err != nil || string(b) != `{"groups":[],"loose":[]}` {
		t.Fatalf("%s %v", b, err)
	}
}

func TestFavoritesCommandsAreChecked(t *testing.T) {
	bad := map[Op]string{
		OpFavGroupCreate: `{"opId":"o","name":"  "}`,
		OpFavGroupRename: `{"opId":"o","id":"bad","name":"x"}`,
		OpFavGroupDelete: `{"opId":"o","id":""}`,
		OpFavGroupMove:   `{"opId":"o","id":"fg-aaaa","beforeId":"nope"}`,
		OpFavPlace:       `{"opId":"o","id":"tab-a","groupId":"nope","beforeId":null}`,
	}
	for op, body := range bad {
		if _, err := DecodeCommand(op, []byte(body)); err == nil {
			t.Errorf("%s accepted %s", op, body)
		}
	}
	if _, err := DecodeCommand(OpFavPlace, []byte(`{"opId":"o","id":"tab-a","groupId":"fg-aaaa","beforeId":"tab-b"}`)); err != nil {
		t.Errorf("a good place: %v", err)
	}
}
