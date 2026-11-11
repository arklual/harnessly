package orgs_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/events"
	"github.com/ly-solutions/harnessly/internal/hub/features"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// kinds reads the events about people a stream got, as "kind:data" joined by spaces: it waits for the first one, then
// for the stream to go quiet. The organization's other events (its numbers tick on their own) are passed over.
func (s *stream) kinds() string {
	var out []string
	wait := 3 * time.Second
	for {
		select {
		case f := <-s.frames:
			if f.kind == events.KindMembers || f.kind == events.KindInvites || f.kind == "signal.new" {
				out = append(out, f.kind+":"+f.data)
				wait = 200 * time.Millisecond
			}
		case <-time.After(wait):
			return strings.Join(out, " ")
		}
	}
}

func TestEveryChangeOfMembersAndInvitesIsPublished(t *testing.T) {
	v := newVera(t)
	if err := v.s.Hub.Features.Set(context.Background(), v.org, features.OrgEvents, true, ""); err != nil {
		t.Fatal(err)
	}
	bus := v.s.Hub.OrgEvents
	admin, code := openStream(t, v.anna, v.url("/events"), "")
	if code != http.StatusOK {
		t.Fatalf("admin: %d", code)
	}
	// The other tenant's stream hears nothing of this tenant's people.
	other, _ := openStream(t, v.gleb, v.url("/events?scope=tenant:"+v.otherTenant), "")
	waitListeners(t, bus, v.org, 2)

	members := events.KindMembers + `:{"spaceId":"` + v.tenant + `"}`
	invites := events.KindInvites + `:{"spaceId":"` + v.tenant + `"}`
	expect := func(what, want string) {
		t.Helper()
		if got := admin.kinds(); got != want {
			t.Errorf("%s: the stream got %q, want %q", what, got, want)
		}
	}
	space := "/api/spaces/" + v.tenant

	// An invitation is made, by link and by mail.
	var made struct{ Invite struct{ ID, URL string } }
	r := v.anna.Do("POST", space+"/invites/link", map[string]any{"maxUses": 5})
	if r.Code != http.StatusCreated {
		t.Fatal(r)
	}
	r.JSON(&made)
	expect("an invitation by link", invites)
	if r := v.anna.Do("POST", space+"/invites", map[string]any{"emails": []string{"new@example.com"}}); r.Code != http.StatusCreated {
		t.Fatal(r)
	}
	expect("an invitation by mail", invites)

	// It is accepted: the invitations and the members changed.
	zoe := v.s.SignIn("zoe@example.com")
	token := made.Invite.URL[strings.LastIndex(made.Invite.URL, "/")+1:]
	if r := zoe.Do("POST", "/api/invites/"+token+"/accept", map[string]any{}); r.Code != http.StatusOK {
		t.Fatal(r)
	}
	expect("an invitation accepted", invites+" "+members)

	// It is revoked.
	if r := v.anna.Do("DELETE", space+"/invites/"+made.Invite.ID, nil); r.Code != http.StatusNoContent {
		t.Fatal(r)
	}
	expect("an invitation revoked", invites)

	// A role in the space, a team, a role in the organization.
	if r := v.anna.Do("PATCH", space+"/members/"+zoe.ID, map[string]any{"role": store.RoleOwner}); r.Code != http.StatusOK {
		t.Fatal(r)
	}
	expect("a role in the space", members)
	if r := v.anna.Do("PUT", v.url("/teams/"+v.team.ID+"/members/"+zoe.ID), map[string]any{}); r.Code/100 != 2 {
		t.Fatal(r)
	}
	expect("into a team", members)
	if r := v.grant(v.anna, zoe, "team", v.team.ID, "lead", true); r.Code/100 != 2 {
		t.Fatal(r)
	}
	expect("a role given", members)
	if r := v.grant(v.anna, zoe, "team", v.team.ID, "lead", false); r.Code/100 != 2 {
		t.Fatal(r)
	}
	expect("a role taken", members)
	if r := v.anna.Do("DELETE", v.url("/teams/"+v.team.ID+"/members/"+zoe.ID), nil); r.Code != http.StatusNoContent {
		t.Fatal(r)
	}
	expect("out of a team", members)

	// A member is removed.
	if r := v.anna.Do("DELETE", space+"/members/"+zoe.ID, nil); r.Code != http.StatusNoContent {
		t.Fatal(r)
	}
	expect("a member removed", members)

	if !other.expectNone() {
		t.Error("another tenant's stream got an event")
	}
}

// ended waits for a stream to be closed by the hub.
func (s *stream) ended() bool {
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 1)
		for {
			if _, err := s.res.Body.Read(buf); err != nil {
				close(done)
				return
			}
		}
	}()
	select {
	case <-done:
		return true
	case <-time.After(5 * time.Second):
		return false
	}
}

func TestARemovedMembersStreamIsClosed(t *testing.T) {
	v := newVera(t)
	if err := v.s.Hub.Features.Set(context.Background(), v.org, features.OrgEvents, true, ""); err != nil {
		t.Fatal(err)
	}
	bus := v.s.Hub.OrgEvents
	admin, _ := openStream(t, v.anna, v.url("/events"), "")
	waitListeners(t, bus, v.org, 1)

	// rawStream opens a stream nobody reads, so that its end can be waited for.
	rawStream := func(path string) *stream {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(ctx, "GET", v.s.URL+path, nil)
		res, err := v.vera.Client.Do(req)
		if err != nil || res.StatusCode != http.StatusOK {
			cancel()
			t.Fatalf("vera's stream: %v %v", res, err)
		}
		t.Cleanup(func() { cancel(); res.Body.Close() })
		return &stream{t: t, res: res, cancel: cancel}
	}
	whole := rawStream(v.url("/events"))
	tenant := rawStream(v.url("/events?scope=tenant:" + v.tenant))
	waitListeners(t, bus, v.org, 3)

	if r := v.anna.Do("DELETE", "/api/spaces/"+v.tenant+"/members/"+v.vera.ID, nil); r.Code != http.StatusNoContent {
		t.Fatal(r)
	}
	if !whole.ended() || !tenant.ended() {
		t.Fatal("the removed member's streams stayed open")
	}
	waitListeners(t, bus, v.org, 1)
	// The page's reconnect is refused: she has no part in the organization any more.
	if r := v.vera.Do("GET", v.url("/events"), nil); r.Code != http.StatusNotFound {
		t.Errorf("the removed member opened a stream again: %s", r)
	}
	if r := v.vera.Do("GET", v.url("/events?scope=tenant:"+v.tenant), nil); r.Code != http.StatusNotFound && r.Code != http.StatusForbidden {
		t.Errorf("the removed member opened the tenant's stream again: %s", r)
	}
	// Those who stay keep their streams.
	if got := admin.kinds(); !strings.Contains(got, events.KindMembers) {
		t.Errorf("the administrator was not told: %q", got)
	}
}

func TestAChangedRoleEndsThePersonsStreams(t *testing.T) {
	v := newVera(t)
	if err := v.s.Hub.Features.Set(context.Background(), v.org, features.OrgEvents, true, ""); err != nil {
		t.Fatal(err)
	}
	bus := v.s.Hub.OrgEvents
	open := func() *stream {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(ctx, "GET", v.s.URL+v.url("/events"), nil)
		res, err := v.vera.Client.Do(req)
		if err != nil || res.StatusCode != http.StatusOK {
			cancel()
			t.Fatalf("vera's stream: %v %v", res, err)
		}
		t.Cleanup(func() { cancel(); res.Body.Close() })
		return &stream{t: t, res: res, cancel: cancel}
	}

	// A role in the space.
	s := open()
	waitListeners(t, bus, v.org, 1)
	if r := v.anna.Do("PATCH", "/api/spaces/"+v.tenant+"/members/"+v.vera.ID, map[string]any{"role": store.RoleOwner}); r.Code != http.StatusOK {
		t.Fatal(r)
	}
	if !s.ended() {
		t.Fatal("the stream outlived a change of the role in the space")
	}
	waitListeners(t, bus, v.org, 0)

	// A role of the organization: the stream opened before it is ended, the next one is given what the role allows.
	s = open()
	waitListeners(t, bus, v.org, 1)
	if r := v.grant(v.anna, v.vera, "team", v.team.ID, "lead", true); r.Code/100 != 2 {
		t.Fatal(r)
	}
	if !s.ended() {
		t.Fatal("the stream outlived a role given in the organization")
	}
	waitListeners(t, bus, v.org, 0)
	again, code := openStream(t, v.vera, v.url("/events"), "")
	if code != http.StatusOK {
		t.Fatalf("the stream does not open again: %d", code)
	}
	waitListeners(t, bus, v.org, 1)
	bus.Publish(v.org, "signal.new", events.For(events.Audience{Role: events.RoleLead, Space: v.tenant}, map[string]string{"n": "t"}))
	if got := again.kinds(); got != `signal.new:{"n":"t"}` {
		t.Errorf("the new stream does not have the new role: %q", got)
	}
}
