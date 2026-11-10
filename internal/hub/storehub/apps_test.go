package storehub

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// in the apps collection a machine sees its own person's user and chat areas and nobody else's, a document's
// area goes as the design project, the team's apps are written only by an owner's machine, and nothing else is written.
func TestAppsCollectionRules(t *testing.T) {
	me, other := PersonName("user-1"), PersonName("user-2")
	if me == "" || me == other || PersonName("") != "" || me != PersonName("user-1") {
		t.Fatalf("names: %q %q", me, other)
	}
	const hash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	member := appsRules{person: me, view: map[string]Grant{"seen": {See: true}, "edited": {See: true, Edit: true}, "hidden": {}, "guest-hidden": {}}}
	owner := member
	owner.owner = true
	type want struct{ sees, writes bool }
	for p, w := range map[string]want{
		"storage/acme.board/user/" + me + ".json":           {true, true},
		"storage/acme.board/user/" + other + ".json":        {false, false},
		"storage/acme.board/chat/" + me + ".chat-1.json":    {true, true},
		"storage/acme.board/chat/" + other + ".chat-1.json": {false, false},
		"storage/acme.board/chat/chat-1.json":               {false, false},
		"storage/acme.board/space/area.json":                {true, true},
		"storage/acme.board/space/other.json":               {false, false},
		"storage/acme.board/doc/seen.json":                  {true, false},
		"storage/acme.board/doc/edited.json":                {true, true},
		"storage/acme.board/doc/hidden.json":                {false, false},
		"storage/acme.board/doc/unrecorded.json":            {true, true},
		"storage/acme.board/doc/guest-abc.json":             {true, true},
		"storage/acme.board/doc/guest-hidden.json":          {false, false},
		"storage/acme.board/install/area.json":              {false, false},
		"storage/not-an-app/space/area.json":                {false, false},
		"team/state.json":                                   {true, false},
		"team/archives/acme.board/1.0.0.tgz":                {true, false},
		"elsewhere/file.txt":                                {true, false},
		"guests/" + me + "/" + hash + ".json":               {true, true},
		"guests/" + other + "/" + hash + ".json":            {false, false},
		"guests/" + me + "/not-a-hash.json":                 {false, false},
		"bundles/acme.board/" + hash + ".tgz":               {true, true},
		"bundles/acme.board/1.0.0.tgz":                      {false, false},
	} {
		if got := (want{member.sees(p), member.writes(p)}); got != w {
			t.Errorf("member %s: %+v, want %+v", p, got, w)
		}
	}
	if !owner.writes("team/state.json") || !owner.writes("team/archives/acme.board/1.0.0.tgz") || owner.writes("team/other.json") {
		t.Error("an owner's machine and the team's files")
	}
	if (appsRules{}).sees("storage/acme.board/user/" + me + ".json") {
		t.Error("a machine nobody made sees a person's user area")
	}
	big := filestore.CommitRequest{Path: "storage/acme.board/doc/edited.json", Size: 100<<10 + 1}
	if member.admits(big) == nil {
		t.Error("an area over its scope's limit is admitted")
	}
	big.Size = 100 << 10
	if member.admits(big) != nil {
		t.Error("an area at its scope's limit is refused")
	}
}
