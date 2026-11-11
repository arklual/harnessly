package events

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/metrics"
)

// wire is a Transport in memory: what one replica notifies, every listener hears, the sender included.
type wire struct {
	mu        sync.Mutex
	listeners []func(string)
	sent      int
}

func (w *wire) Notify(_ context.Context, payload string) error {
	w.mu.Lock()
	ls := append([]func(string){}, w.listeners...)
	w.sent++
	w.mu.Unlock()
	for _, fn := range ls {
		fn(payload)
	}
	return nil
}

func (w *wire) Listen(ctx context.Context, up func(), fn func(string), _ func(error)) error {
	w.mu.Lock()
	w.listeners = append(w.listeners, fn)
	w.mu.Unlock()
	up()
	<-ctx.Done()
	return nil
}

func (w *wire) notified() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sent
}

func (w *wire) listening() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.listeners)
}

type replica struct {
	broker *Broker
	fleet  *FleetBus
	org    *MemoryBus
}

func twoReplicas(t *testing.T) (a, b replica, w *wire) {
	t.Helper()
	w = &wire{}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	t.Cleanup(func() { cancel(); wg.Wait() })
	mk := func(id string) replica {
		r := replica{broker: NewBroker(), fleet: NewFleetBus(), org: NewMemoryBus(nil)}
		relay := NewRelay(id, w, r.broker, r.fleet, r.org, nil)
		wg.Add(1)
		go func() { defer wg.Done(); relay.Run(ctx) }()
		return r
	}
	a, b = mk("r-a"), mk("r-b")
	for deadline := time.Now().Add(3 * time.Second); w.listening() < 2; {
		if time.Now().After(deadline) {
			t.Fatal("the relays did not start to listen")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return a, b, w
}

func within[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatalf("%s did not arrive", what)
		panic("unreachable")
	}
}

// An event of a space published on one replica reaches a stream open on the other, once, with who may see it; the
// replica it happened on does not get it a second time.
func TestRelayCarriesSpaceEventsToTheOtherReplica(t *testing.T) {
	a, b, _ := twoReplicas(t)
	here := a.broker.Subscribe("sp", "u1", true)
	owner := b.broker.Subscribe("sp", "u1", true)
	member := b.broker.Subscribe("sp", "u2", false)
	taps := 0
	b.broker.SetTap(func(string, Event) { taps++ })

	a.broker.PublishMachine("sp", "m1", "machine", map[string]string{"id": "m1"}, true)
	e := within(t, owner.ch, "the event on the other replica")
	if e.Name != "machine" || e.MachineID != "m1" || !e.OwnersOnly || string(e.Data) != `{"id":"m1"}` {
		t.Errorf("event carried as %+v", e)
	}
	within(t, here.ch, "the event on its own replica")
	a.broker.PublishJSON("sp", "note", 1, false)
	within(t, member.ch, "the event for everybody")
	select {
	case e := <-here.ch:
		if e.Name != "note" {
			t.Errorf("the first event came back to its own replica: %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the second event did not arrive on its own replica")
	}
	if len(member.ch) != 0 {
		t.Error("a member got the owners' event through the relay")
	}
	if taps != 0 {
		t.Errorf("the tap heard %d events of another replica: their fleet pokes are carried by themselves", taps)
	}
}

// A kick, a fleet poke and an organization's event cross as well.
func TestRelayCarriesKicksFleetAndOrganizationEvents(t *testing.T) {
	a, b, _ := twoReplicas(t)

	gone := b.broker.Subscribe("sp", "u1", false)
	a.broker.Kick("sp", "u1")
	within(t, gone.done, "the kick")

	watcher := b.fleet.Subscribe("u1", []string{"sp"})
	a.fleet.TouchSpace("sp")
	within(t, watcher.wake, "the fleet poke")
	a.fleet.TouchUser("u1")
	within(t, watcher.wake, "the person's poke")

	lead := b.org.Subscribe("org", Filter{UserID: "u1", Role: RoleLead}, nil)
	plain := b.org.Subscribe("org", Filter{UserID: "u2", Role: RoleMember}, nil)
	a.org.Publish("org", "signal.new", map[string]int{"n": 1})
	a.org.Publish("org", "note", For(Audience{Role: RoleMember}, map[string]int{"n": 2}))
	if e := within(t, lead.ch, "the leads' event"); e.Kind != "signal.new" || string(e.Data) != `{"n":1}` {
		t.Errorf("organization event carried as %+v", e)
	}
	if e := within(t, plain.ch, "the members' event"); e.Kind != "note" {
		t.Errorf("a member got %+v: the audience did not cross", e)
	}
	a.org.Kick("org", "u2")
	within(t, plain.done, "the organization kick")
}

// An event too long for a notification stays with its replica and is counted; the streams there still get it.
func TestRelayDropsWhatDoesNotFit(t *testing.T) {
	a, b, w := twoReplicas(t)
	here := a.broker.Subscribe("sp", "u1", true)
	there := b.broker.Subscribe("sp", "u1", true)
	before := metrics.EventsRelayDropped.Value()
	a.broker.PublishJSON("sp", "big", strings.Repeat("x", RelayMax), false)
	within(t, here.ch, "the long event on its own replica")
	a.broker.PublishJSON("sp", "small", 1, false)
	if e := within(t, there.ch, "the next event"); e.Name != "small" {
		t.Errorf("the long event crossed: %s", e.Name)
	}
	if metrics.EventsRelayDropped.Value() != before+1 {
		t.Errorf("dropped %d, want 1", metrics.EventsRelayDropped.Value()-before)
	}
	if n := w.notified(); n != 1 {
		t.Errorf("%d notifications sent, want 1", n)
	}
}
