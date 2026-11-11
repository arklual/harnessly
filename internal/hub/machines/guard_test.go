package machines_test

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/api"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// Every route under a space refuses a person of another space before it reads or changes anything, and the routes of
// the owners refuse a plain member as well; the space's own owner is let through.
func TestSpaceRoutesRefuseOtherSpaces(t *testing.T) {
	c := &cloud{}
	s := hubtest.New(t, hubtest.Env("HUB_PROVIDER_PROTOCOL", "v1"), hubtest.WithOptions(func(o *api.Options) { o.Provider = c }))
	s.SignIn("root@example.com") // the first account administers the installation; the others are plain people
	owner, mate, stranger := s.SignIn("owner@example.com"), s.SignIn("mate@example.com"), s.SignIn("stranger@example.com")
	spaceID, _ := owner.NewSpace("North")
	owner.Join(spaceID, mate)
	stranger.NewSpace("South")

	var made struct{ Op string }
	if r := owner.Do("POST", "/api/spaces/"+spaceID+"/machines/provision", map[string]any{"name": "box"}); r.Code != 202 {
		t.Fatalf("provision: %s", r)
	} else {
		r.JSON(&made)
	}
	base := "/api/spaces/" + spaceID + "/machines"
	type route struct {
		method, path string
		body         any
		ownersOnly   bool
	}
	routes := []route{
		{"GET", base, nil, false},
		{"POST", base + "/codes", map[string]any{"profile": "container"}, true},
		{"POST", base + "/local", map[string]any{}, true},
		{"POST", base + "/provision", map[string]any{"name": "box"}, true},
		{"GET", base + "/provision/" + made.Op, nil, true},
	}
	for _, rt := range routes {
		if r := stranger.Do(rt.method, rt.path, rt.body); r.Code != 403 {
			t.Errorf("another space %s %s: %s", rt.method, rt.path, r)
		}
		if !rt.ownersOnly {
			if r := mate.Do(rt.method, rt.path, rt.body); r.Code != 200 {
				t.Errorf("member %s %s: %s", rt.method, rt.path, r)
			}
			continue
		}
		if r := mate.Do(rt.method, rt.path, rt.body); r.Code != 403 {
			t.Errorf("member %s %s: %s", rt.method, rt.path, r)
		}
	}
	if c.made != 1 {
		t.Errorf("the provider made %d servers, want only the owner's one", c.made)
	}
	if r := owner.Do("GET", base+"/provision/"+made.Op, nil); r.Code != 200 {
		t.Errorf("owner reads the operation: %s", r)
	}
}

// Every route of one machine refuses a person of another space before it reads or changes anything, and the routes of
// the owners refuse a plain member as well.
func TestMachineRoutesRefuseOtherSpaces(t *testing.T) {
	c := &cloud{}
	s := hubtest.New(t, hubtest.Env("HUB_PROVIDER_PROTOCOL", "v1"), hubtest.WithOptions(func(o *api.Options) { o.Provider = c }))
	s.SignIn("root@example.com")
	owner, mate, stranger := s.SignIn("owner@example.com"), s.SignIn("mate@example.com"), s.SignIn("stranger@example.com")
	spaceID, _ := owner.NewSpace("North")
	owner.Join(spaceID, mate)
	stranger.NewSpace("South")
	id := owner.Machine(spaceID, nil).MachineID
	m := "/api/machines/" + id
	type route struct {
		method, path string
		body         any
		ownersOnly   bool
	}
	routes := []route{
		{"GET", m, nil, false},
		{"PATCH", m, map[string]any{"name": "Чужая"}, true},
		{"DELETE", m, nil, true},
		{"GET", m + "/open", nil, false},
		{"POST", m + "/trust", nil, true},
		{"DELETE", m + "/trust", nil, true},
		{"POST", m + "/preflight", nil, true},
		{"POST", m + "/owner", map[string]any{"userId": stranger.ID}, true},
		{"POST", m + "/provider/attach", map[string]any{"externalId": "srv-1"}, true},
		{"GET", m + "/provider", nil, false},
		{"POST", m + "/provider/power", map[string]any{"action": "off"}, true},
		{"GET", m + "/provider/ops/op-1", nil, false},
		{"GET", m + "/agents", nil, true},
		{"POST", m + "/agents/claude/install", map[string]any{}, true},
		{"POST", m + "/agents/claude/check", map[string]any{}, true},
		{"POST", m + "/agents/claude/login", map[string]any{"method": "oauth"}, true},
		{"POST", m + "/logins/l1/code", map[string]any{"code": "x"}, true},
		{"DELETE", m + "/logins/l1", nil, true},
		{"POST", m + "/agents/claude/key", map[string]any{"envelope": "x"}, true},
		{"POST", m + "/agents/claude/key/verify", map[string]any{}, true},
		{"GET", m + "/proxy", nil, true},
		{"PUT", m + "/proxy", map[string]any{"scope": "all"}, true},
		{"DELETE", m + "/proxy", nil, true},
		{"POST", m + "/env", map[string]any{"name": "X", "envelope": "x"}, true},
		{"DELETE", m + "/env/X", nil, true},
		{"POST", m + "/gitkey", map[string]any{}, true},
	}
	for _, rt := range routes {
		if r := stranger.Do(rt.method, rt.path, rt.body); r.Code != 403 {
			t.Errorf("another space %s %s: %s", rt.method, rt.path, r)
		}
		if rt.ownersOnly {
			if r := mate.Do(rt.method, rt.path, rt.body); r.Code != 403 {
				t.Errorf("member %s %s: %s", rt.method, rt.path, r)
			}
		}
	}
	if r := owner.Do("GET", m, nil); r.Code != 200 {
		t.Errorf("the machine is gone or changed: %s", r)
	}
	if c.made != 0 || len(c.attached) != 0 {
		t.Errorf("the provider was asked: made %d, attached %v", c.made, c.attached)
	}
}

// A member the machine's access list leaves out does not find the machine: not its card, not its server at the
// provider, not its resources; a member on the list does.
func TestHiddenMachineIsNotFoundOnItsRoutes(t *testing.T) {
	c := &cloud{}
	s := hubtest.New(t, hubtest.Env("HUB_PROVIDER_PROTOCOL", "v1"), hubtest.Env("HUB_ORGS", "on"),
		hubtest.Env("HUB_ACCESS_ENFORCE", "on"), hubtest.WithOptions(func(o *api.Options) { o.Provider = c }))
	s.SignIn("root@example.com")
	owner, listed, other := s.SignIn("owner@example.com"), s.SignIn("listed@example.com"), s.SignIn("other@example.com")
	spaceID, _ := owner.NewSpace("North")
	owner.Join(spaceID, listed)
	owner.Join(spaceID, other)
	id := owner.Machine(spaceID, nil).MachineID
	m := "/api/machines/" + id
	if r := owner.Do("PATCH", m, map[string]any{"access": []map[string]string{{"kind": "user", "id": listed.ID}}}); r.Code != 200 {
		t.Fatalf("share: %s", r)
	}
	for _, path := range []string{m, m + "/provider", m + "/provider/ops/op-1"} {
		if r := other.Do("GET", path, nil); r.Code != 404 {
			t.Errorf("left out, %s: %s", path, r)
		}
	}
	// The resources need the tenant's view, which a plain member has not; either way they are not given.
	for _, path := range []string{m + "/resources", m + "/ops", m + "/metrics"} {
		if r := other.Do("GET", path, nil); r.Code != 404 && r.Code != 403 {
			t.Errorf("left out, %s: %s", path, r)
		}
	}
	if r := listed.Do("GET", m, nil); r.Code != 200 {
		t.Errorf("listed, the machine: %s", r)
	}
	if r := listed.Do("GET", m+"/provider", nil); r.Code == 404 || r.Code == 403 {
		t.Errorf("listed, provider: %s", r)
	}
}
