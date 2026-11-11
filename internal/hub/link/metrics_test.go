package link

import (
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/metrics"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The registry says what its links hold, and how long the oldest unhandled history frame has waited.
func TestStatsCountLinksQueuesAndTheOldestEvent(t *testing.T) {
	h := newTestHub(t)
	release := make(chan struct{})
	h.dialer.Hooks.Bulk = func(name string) bool { return name == "history.events" }
	h.dialer.Hooks.Event = func(_ *Link, _ store.Machine, ev Event) {
		if ev.Name == "history.events" {
			<-release
		}
	}
	connects, reconnects := metrics.LinkConnects.Value(), metrics.LinkReconnects.Value()
	a, _ := h.agent(dialOpts{})
	if st := h.reg.Stats(); st.Links != 1 || st.Bulk != 0 || st.OldestBulk != 0 {
		t.Fatalf("stats of an idle link: %+v", st)
	}
	for range 3 {
		a.emit(t, `{"event":"history.events"}`)
	}
	deadline := time.Now().Add(5 * time.Second)
	var st Stats
	for time.Now().Before(deadline) {
		// One frame is with the handler, two wait behind it.
		if st = h.reg.Stats(); st.Bulk == 2 && st.OldestBulk > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st.Bulk != 2 || st.OldestBulk <= 0 || st.OldestEvent != 0 {
		t.Fatalf("stats with frames waiting: %+v", st)
	}
	close(release)
	for time.Now().Before(deadline) {
		if st = h.reg.Stats(); st.Bulk == 0 && st.OldestBulk == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st.Bulk != 0 || st.OldestBulk != 0 {
		t.Fatalf("stats after the queue was taken: %+v", st)
	}

	disconnects := metrics.LinkDisconnects.Value()
	h.agent(dialOpts{}) // the same machine dials again
	if got := metrics.LinkConnects.Value() - connects; got != 2 {
		t.Errorf("connects grew by %d, want 2", got)
	}
	if got := metrics.LinkReconnects.Value() - reconnects; got != 1 {
		t.Errorf("reconnects grew by %d, want 1", got)
	}
	if got := metrics.LinkDisconnects.Value() - disconnects; got != 1 {
		t.Errorf("disconnects grew by %d, want 1 (the replaced link)", got)
	}
	if st := h.reg.Stats(); st.Links != 1 {
		t.Errorf("links = %d, want 1", st.Links)
	}
}
