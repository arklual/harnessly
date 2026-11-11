package spaces

import (
	"slices"
	"testing"
)

// Every route under a space refuses a person of another space before it reads or changes anything, the routes of the
// owners refuse a plain member as well, and nothing of the space changes.
func TestSpaceRoutesRefuseOtherSpaces(t *testing.T) {
	e := newEnv(t)
	owner, mate, stranger := e.signIn("owner@example.com"), e.signIn("mate@example.com"), e.signIn("stranger@example.com")
	sp := owner.newSpace("Команда")
	join(t, e, owner, mate, sp.ID)
	stranger.newSpace("Своё")
	var link struct{ Invite InviteView }
	if r := owner.do("POST", "/api/spaces/"+sp.ID+"/invites/link", map[string]any{}); r.Code != 201 && r.Code != 200 {
		t.Fatalf("link: %d %s", r.Code, r.Body)
	} else {
		r.into(&link)
	}

	base := "/api/spaces/" + sp.ID
	type route struct {
		method, path string
		body         any
		ownersOnly   bool
	}
	routes := []route{
		{"GET", base, nil, false},
		{"GET", base + "/members", nil, false},
		{"GET", base + "/events", nil, false},
		{"DELETE", base + "/members/" + mate.id, nil, false},
		{"PATCH", base, map[string]string{"name": "Чужое"}, true},
		{"POST", base + "/delete", map[string]any{"confirm": sp.Name}, true},
		{"PATCH", base + "/members/" + mate.id, map[string]string{"role": "owner"}, true},
		{"POST", base + "/invites", map[string]any{"emails": []string{"x@example.com"}}, true},
		{"POST", base + "/invites/link", map[string]any{}, true},
		{"GET", base + "/invites", nil, true},
		{"DELETE", base + "/invites/" + link.Invite.ID, nil, true},
	}
	for _, rt := range routes {
		if r := stranger.do(rt.method, rt.path, rt.body); r.Code != 403 {
			t.Errorf("another space %s %s: %d %s", rt.method, rt.path, r.Code, r.Body)
		}
		if rt.ownersOnly {
			if r := mate.do(rt.method, rt.path, rt.body); r.Code != 403 {
				t.Errorf("member %s %s: %d %s", rt.method, rt.path, r.Code, r.Body)
			}
		}
	}

	var got struct{ Space SpaceView }
	owner.do("GET", base, nil).into(&got)
	if got.Space.Name != sp.Name {
		t.Errorf("name = %q", got.Space.Name)
	}
	var ms struct{ Members []MemberView }
	owner.do("GET", base+"/members", nil).into(&ms)
	if len(ms.Members) != 2 {
		t.Errorf("members: %+v", ms.Members)
	}
	for _, m := range ms.Members {
		if m.UserID == mate.id && m.Role != "member" {
			t.Errorf("mate's role: %q", m.Role)
		}
	}
	var ivs struct{ Invites []InviteView }
	owner.do("GET", base+"/invites", nil).into(&ivs)
	if !slices.ContainsFunc(ivs.Invites, func(iv InviteView) bool { return iv.ID == link.Invite.ID }) {
		t.Errorf("the link was revoked: %+v", ivs.Invites)
	}
}
