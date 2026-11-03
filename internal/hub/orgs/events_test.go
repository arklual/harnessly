package orgs_test

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/events"
	"github.com/ly-solutions/harnessly/internal/hub/features"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// frame is one event read from a stream.
type frame struct{ id, kind, data string }

// stream is an open organization stream of a test person.
type stream struct {
	t      *testing.T
	frames chan frame
	res    *http.Response
	cancel context.CancelFunc
}

func openStream(t *testing.T, p *hubtest.Person, path string, lastID string) (*stream, int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", p.S.URL+path, nil)
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	res, err := p.Client.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	s := &stream{t: t, frames: make(chan frame, 64), res: res, cancel: cancel}
	t.Cleanup(func() { cancel(); res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		return s, res.StatusCode
	}
	go func() {
		sc := bufio.NewScanner(res.Body)
		var f frame
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if f.kind != "" {
					s.frames <- f
				}
				f = frame{}
			case strings.HasPrefix(line, "id: "):
				f.id = line[4:]
			case strings.HasPrefix(line, "event: "):
				f.kind = line[7:]
			case strings.HasPrefix(line, "data: "):
				f.data = line[6:]
			}
		}
	}()
	return s, res.StatusCode
}

func (s *stream) next() (frame, bool) {
	select {
	case f := <-s.frames:
		return f, true
	case <-time.After(700 * time.Millisecond):
		return frame{}, false
	}
}

func (s *stream) expectNone() bool {
	select {
	case <-s.frames:
		return false
	case <-time.After(150 * time.Millisecond):
		return true
	}
}

func waitListeners(t *testing.T, bus *events.MemoryBus, org string, n int) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if bus.Count(org) == n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%d streams open, want %d", bus.Count(org), n)
}

func TestOrgEventsAreDarkUntilTheFlagIsOn(t *testing.T) {
	v := newVera(t)
	for _, p := range []*hubtest.Person{v.anna, v.boris, v.vera} {
		r := p.Do("GET", v.url("/events"), nil)
		if r.Code != http.StatusNotFound || r.Err() != "not_enabled" {
			t.Errorf("%s with the flag off: %s", p.Email, r)
		}
	}
}

func TestOrgEventsStreamByRoleAndScope(t *testing.T) {
	v := newVera(t)
	ctx := context.Background()
	if err := v.s.Hub.Features.Set(ctx, v.org, features.OrgEvents, true, ""); err != nil {
		t.Fatal(err)
	}
	bus := v.s.Hub.OrgEvents

	// Somebody outside the organization does not learn it exists; a wrong scope is a bad request.
	outsider := v.s.SignIn("zoe@example.com")
	if r := outsider.Do("GET", v.url("/events"), nil); r.Code != http.StatusNotFound {
		t.Errorf("outsider: %s", r)
	}
	if r := v.anna.Do("GET", v.url("/events?scope=nope"), nil); r.Code != http.StatusBadRequest {
		t.Errorf("bad scope: %s", r)
	}

	admin, code := openStream(t, v.anna, v.url("/events"), "")
	if code != http.StatusOK {
		t.Fatalf("admin: %d", code)
	}
	if ct := admin.res.Header.Get("Content-Type"); ct != "text/event-stream" || admin.res.Header.Get("X-Accel-Buffering") != "no" {
		t.Errorf("headers = %v", admin.res.Header)
	}
	lead, _ := openStream(t, v.boris, v.url("/events?scope=tenant:"+v.tenant), "")
	member, _ := openStream(t, v.vera, v.url("/events"), "")
	otherLead, code := openStream(t, v.gleb, v.url("/events?scope=tenant:"+v.otherTenant), "")
	if code != http.StatusOK {
		t.Fatalf("gleb on his tenant: %d", code)
	}
	waitListeners(t, bus, v.org, 4)

	bus.Publish(v.org, "usage.tick", events.For(events.Audience{Role: events.RoleAdmin}, map[string]int{"chatsNow": 1}))
	bus.Publish(v.org, "signal.new", events.For(events.Audience{Role: events.RoleLead, Space: v.tenant}, map[string]string{"n": "t"}))
	bus.Publish(v.org, "signal.new", events.For(events.Audience{User: v.vera.ID}, map[string]string{"n": "mine"}))
	bus.Publish(v.org, "machine.status", events.For(events.Audience{Role: events.RoleMember}, map[string]string{"n": "all"}))

	got := func(s *stream) string {
		var kinds []string
		for {
			f, ok := s.next()
			if !ok {
				break
			}
			kinds = append(kinds, f.kind+":"+f.data)
			if len(kinds) >= 4 {
				break
			}
		}
		return strings.Join(kinds, " ")
	}
	if g := got(admin); g != `usage.tick:{"chatsNow":1} signal.new:{"n":"t"} machine.status:{"n":"all"}` {
		t.Errorf("admin: %s", g)
	}
	// A lead of the tenant has the tenant's events, not the organization's totals nor events of no tenant.
	if g := got(lead); g != `signal.new:{"n":"t"}` {
		t.Errorf("lead: %s", g)
	}
	// A member has what is addressed to her and to everybody.
	if g := got(member); g != `signal.new:{"n":"mine"} machine.status:{"n":"all"}` {
		t.Errorf("member: %s", g)
	}
	// The other tenant's stream sees none of this tenant's events.
	if g := got(otherLead); g != "" {
		t.Errorf("other tenant: %s", g)
	}
}

func TestOrgEventsReplayAndResync(t *testing.T) {
	v := newVera(t)
	if err := v.s.Hub.Features.Set(context.Background(), v.org, features.OrgEvents, true, ""); err != nil {
		t.Fatal(err)
	}
	bus := v.s.Hub.OrgEvents
	first, _ := openStream(t, v.anna, v.url("/events"), "")
	waitListeners(t, bus, v.org, 1)
	for i := 0; i < 3; i++ {
		bus.Publish(v.org, "usage.tick", map[string]int{"i": i})
	}
	var ids []string
	for i := 0; i < 3; i++ {
		f, ok := first.next()
		if !ok {
			t.Fatal("event missing")
		}
		ids = append(ids, f.id)
	}
	first.cancel()

	// Back after the first event: the other two come again, and nothing else.
	again, _ := openStream(t, v.anna, v.url("/events"), ids[0])
	for i := 1; i < 3; i++ {
		f, ok := again.next()
		if !ok || f.id != ids[i] {
			t.Fatalf("replay %d = %+v (%v)", i, f, ok)
		}
	}
	if !again.expectNone() {
		t.Error("more than the missed events came")
	}

	// A position the hub does not know (another run) says resync.
	foreign, _ := openStream(t, v.anna, v.url("/events"), "12345")
	if f, ok := foreign.next(); !ok || f.kind != "resync" {
		t.Errorf("foreign position = %+v (%v)", f, ok)
	}
}
