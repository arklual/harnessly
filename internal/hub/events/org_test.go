package events

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)

func drain(s *OrgSub) []OrgEvent {
	var out []OrgEvent
	for {
		select {
		case e := <-s.ch:
			out = append(out, e)
		default:
			return out
		}
	}
}

func TestOrgBusIDsGrowAndStartFromTheClock(t *testing.T) {
	b := NewMemoryBus(func() time.Time { return t0 })
	s := b.Subscribe("o1", Filter{UserID: "u", Role: RoleAdmin}, nil)
	b.Publish("o1", "a", map[string]int{"n": 1})
	b.Publish("o1", "b", map[string]int{"n": 2})
	got := drain(s)
	if len(got) != 2 || got[0].ID != t0.UnixMilli()*1000+1 || got[1].ID != got[0].ID+1 || got[0].Kind != "a" || string(got[0].Data) != `{"n":1}` {
		t.Fatalf("events = %+v", got)
	}
}

func TestOrgBusFansOutPerOrganization(t *testing.T) {
	b := NewMemoryBus(func() time.Time { return t0 })
	one := b.Subscribe("o1", Filter{Role: RoleAdmin}, nil)
	two := b.Subscribe("o2", Filter{Role: RoleAdmin}, nil)
	b.Publish("o1", "x", 1)
	if len(drain(one)) != 1 || len(drain(two)) != 0 {
		t.Fatal("an event crossed organizations")
	}
	if b.Count("o1") != 1 || b.Count("o2") != 1 {
		t.Fatal("count")
	}
	b.Unsubscribe(one)
	if b.Count("o1") != 0 {
		t.Fatal("not unsubscribed")
	}
}

func TestOrgBusFilters(t *testing.T) {
	b := NewMemoryBus(func() time.Time { return t0 })
	admin := b.Subscribe("o", Filter{UserID: "a", Role: RoleAdmin}, nil)
	lead := b.Subscribe("o", Filter{UserID: "l", Role: RoleLead}, nil)
	member := b.Subscribe("o", Filter{UserID: "m", Role: RoleMember}, nil)
	tenantLead := b.Subscribe("o", Filter{UserID: "l", Role: RoleLead, Space: "s1"}, nil)
	otherTenant := b.Subscribe("o", Filter{UserID: "l", Role: RoleLead, Space: "s2"}, nil)

	b.Publish("o", "plain", 1)                                                               // leads and admins, whole organization
	b.Publish("o", "everyone", For(Audience{Role: RoleMember}, 2))                           // every role
	b.Publish("o", "adm", For(Audience{Role: RoleAdmin}, 3))                                 // admins only
	b.Publish("o", "tenant", For(Audience{Role: RoleLead, Space: "s1"}, 4))                  // tenant s1
	b.Publish("o", "tenant-only", For(Audience{Role: RoleLead, Space: "s1", Only: true}, 5)) // not on the organization stream
	b.Publish("o", "mine", For(Audience{User: "m"}, 6))                                      // one person

	kinds := func(s *OrgSub) string {
		var k []string
		for _, e := range drain(s) {
			k = append(k, e.Kind)
		}
		return strings.Join(k, ",")
	}
	for name, c := range map[string]struct {
		sub  *OrgSub
		want string
	}{
		"admin":        {admin, "plain,everyone,adm,tenant"},
		"lead":         {lead, "plain,everyone,tenant"},
		"member":       {member, "everyone,mine"},
		"tenant lead":  {tenantLead, "tenant,tenant-only"},
		"other tenant": {otherTenant, ""},
	} {
		if got := kinds(c.sub); got != c.want {
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}
}

func TestOrgBusReplaysFromTheRing(t *testing.T) {
	b := NewMemoryBus(func() time.Time { return t0 })
	f := Filter{UserID: "u", Role: RoleAdmin}
	first := b.Subscribe("o", f, nil)
	for i := 0; i < 5; i++ {
		b.Publish("o", "e", i)
	}
	got := drain(first)
	last := got[1].ID // the client saw two events

	again := b.Subscribe("o", f, &last)
	re := drain(again)
	if len(re) != 3 || re[0].ID != got[2].ID || again.lost.Load() {
		t.Fatalf("replayed %d, lost=%v", len(re), again.lost.Load())
	}

	// An id from another run of the hub, or from the future, means the client cannot be caught up.
	future := b.next + 10
	if s := b.Subscribe("o", f, &future); !s.lost.Load() {
		t.Error("foreign id was not a resync")
	}
	old := t0.UnixMilli()*1000 - 5
	if s := b.Subscribe("o", f, &old); !s.lost.Load() {
		t.Error("id from before the start was not a resync")
	}
}

func TestOrgBusRingForgetsAndResyncs(t *testing.T) {
	b := NewMemoryBus(func() time.Time { return t0 })
	f := Filter{UserID: "u", Role: RoleAdmin}
	b.Publish("o", "first", 0)
	seen := b.next // the client saw the first event
	for i := 0; i < OrgRing+10; i++ {
		b.Publish("o", "e", i)
	}
	b.mu.Lock()
	n := len(b.rings["o"].events)
	b.mu.Unlock()
	if n != OrgRing {
		t.Fatalf("ring holds %d", n)
	}
	if s := b.Subscribe("o", f, &seen); !s.lost.Load() {
		t.Error("an id the ring let go was replayed as if complete")
	}
	// The newest is still known.
	newest := b.next - 3
	if s := b.Subscribe("o", f, &newest); s.lost.Load() || len(drain(s)) != 3 {
		t.Error("recent ids should replay")
	}
}

func TestOrgBusSlowSubscriberIsDroppedNotBlocking(t *testing.T) {
	b := NewMemoryBus(func() time.Time { return t0 })
	slow := b.Subscribe("o", Filter{Role: RoleAdmin}, nil)
	done := make(chan struct{})
	go func() {
		for i := 0; i < OrgBuffer*3; i++ {
			b.Publish("o", "e", i)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a slow subscriber blocked the publisher")
	}
	if len(slow.ch) != OrgBuffer || !slow.lost.Load() {
		t.Errorf("buffer %d, lost %v", len(slow.ch), slow.lost.Load())
	}
}

func TestOrgBusKickAndClose(t *testing.T) {
	b := NewMemoryBus(func() time.Time { return t0 })
	a := b.Subscribe("o", Filter{UserID: "a", Role: RoleAdmin}, nil)
	c := b.Subscribe("o", Filter{UserID: "c", Role: RoleAdmin}, nil)
	b.Kick("o", "a")
	select {
	case <-a.Done():
	default:
		t.Error("kicked stream is open")
	}
	if len(b.Active()) != 1 {
		t.Errorf("active = %v", b.Active())
	}
	b.Close()
	select {
	case <-c.Done():
	default:
		t.Error("closed bus left a stream open")
	}
	if b.Subscribe("o", Filter{}, nil) != nil {
		t.Error("closed bus accepted a subscriber")
	}
	b.Publish("o", "late", 1) // no panic
}

func TestOrgBusActiveListsOrganizationsAndTenants(t *testing.T) {
	b := NewMemoryBus(func() time.Time { return t0 })
	b.Subscribe("b", Filter{Role: RoleAdmin}, nil)
	b.Subscribe("a", Filter{Role: RoleAdmin, Space: "s"}, nil)
	b.Subscribe("a", Filter{Role: RoleAdmin}, nil)
	b.Subscribe("a", Filter{Role: RoleAdmin}, nil)
	got := b.Active()
	want := []Listener{{"a", ""}, {"a", "s"}, {"b", ""}}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("active = %v", got)
	}
}

func TestServeOrgWritesFramesAndHeartbeat(t *testing.T) {
	old := HeartbeatEvery
	HeartbeatEvery = 30 * time.Millisecond
	defer func() { HeartbeatEvery = old }()

	b := NewMemoryBus(func() time.Time { return t0 })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := b.Subscribe("o", Filter{UserID: "u", Role: RoleAdmin}, nil)
		ServeOrg(w, r, b, s)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Errorf("headers = %v", resp.Header)
	}
	for i := 0; i < 100 && b.Count("o") == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	b.Publish("o", "usage.tick", map[string]int{"chatsNow": 2})

	var text strings.Builder
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		text.WriteString(sc.Text() + "\n")
		if strings.Contains(text.String(), "heartbeat") && strings.Contains(text.String(), "event: usage.tick") {
			break
		}
	}
	got := text.String()
	for _, want := range []string{"retry: 3000", "event: usage.tick", `data: {"chatsNow":2}`, ": heartbeat"} {
		if !strings.Contains(got, want) {
			t.Errorf("stream lacks %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "id: ") {
		t.Errorf("no event id:\n%s", got)
	}
}

func TestServeOrgSaysResyncAfterDrop(t *testing.T) {
	b := NewMemoryBus(func() time.Time { return t0 })
	rec := httptest.NewRecorder()
	stale := b.next + 99
	s := b.Subscribe("o", Filter{UserID: "u", Role: RoleAdmin}, &stale)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	ServeOrg(rec, req, b, s)
	if !strings.Contains(rec.Body.String(), "event: resync") {
		t.Errorf("body = %q", rec.Body.String())
	}
}
