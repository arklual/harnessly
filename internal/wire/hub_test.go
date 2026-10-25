package wire

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestRingReplaysWhatItHolds(t *testing.T) {
	var r ring
	now := time.Unix(1000, 0)
	for seq := int64(1); seq <= 10; seq++ {
		r.push(ringItem{seq: seq, at: now, frame: []byte(fmt.Sprint(seq))})
	}
	if frames, ok := r.since(7, 10, now, AllCaps); !ok || len(frames) != 3 || string(frames[0]) != "8" {
		t.Fatalf("after 7: %q %v", frames, ok)
	}
	if frames, ok := r.since(10, 10, now, AllCaps); !ok || len(frames) != 0 {
		t.Fatalf("after the last: %q %v", frames, ok)
	}
	if _, ok := r.since(11, 10, now, AllCaps); ok {
		t.Fatal("a seq past the channel's is not a cursor of it")
	}
	if frames, ok := r.since(0, 10, now, AllCaps); !ok || len(frames) != 10 {
		t.Fatalf("from the start: %d %v", len(frames), ok)
	}
}

func TestRingForgetsPastItsSizeAndAge(t *testing.T) {
	var r ring
	now := time.Unix(1000, 0)
	for seq := int64(1); seq <= RingSize+5; seq++ {
		r.push(ringItem{seq: seq, at: now, frame: []byte{'x'}})
	}
	if _, ok := r.since(4, RingSize+5, now, AllCaps); ok {
		t.Fatal("seq 5 was pushed out: a snapshot is due")
	}
	if frames, ok := r.since(5, RingSize+5, now, AllCaps); !ok || len(frames) != RingSize {
		t.Fatalf("from the oldest kept: %d %v", len(frames), ok)
	}
	if _, ok := r.since(RingSize, RingSize+5, now.Add(RingAge+time.Second), AllCaps); ok {
		t.Fatal("events older than the ring's age do not replay")
	}
	if frames, ok := r.since(RingSize+5, RingSize+5, now.Add(time.Hour), AllCaps); !ok || len(frames) != 0 {
		t.Fatal("a page that missed nothing needs nothing, however old the ring")
	}
}

// testClient is a client without a socket: what it would write stays in its queue.
func testClient() *client {
	return &client{subs: map[Channel]*subscription{}, wake: make(chan struct{}, 1), done: make(chan struct{})}
}

func (c *client) frames(t *testing.T) []ServerFrame {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []ServerFrame
	for _, b := range c.queue {
		f, err := DecodeServerFrame(b)
		if err != nil {
			t.Fatalf("%v: %s", err, b)
		}
		out = append(out, f)
	}
	c.queue, c.queued = nil, 0
	return out
}

func seen(id string, at int64) Event { return ConversationSeenEv{ID: id, SeenAt: at, Rev: at} }

func TestHubNumbersEachChannel(t *testing.T) {
	h := NewHub(nil)
	if s := h.Publish(ChannelConvs, seen("tab-a", 1)); s != 1 {
		t.Fatalf("first seq %d", s)
	}
	if s := h.Publish(ChannelConvs, seen("tab-a", 2)); s != 2 {
		t.Fatalf("second seq %d", s)
	}
	if s := h.Publish(ConvChannel("tab-a"), BackgroundRemovedEv{ID: "x"}); s != 1 {
		t.Fatalf("another channel starts at 1: %d", s)
	}
	if s := h.Publish(ChannelMachine, seen("tab-a", 3)); s != 0 || h.Seq(ChannelMachine) != 0 {
		t.Fatal("an event of another channel is dropped, not numbered")
	}
}

// The snapshot goes first and carries the seq at subscribe time; what was published while it was taken follows it.
func TestHubHoldsEventsUntilTheSnapshot(t *testing.T) {
	h := NewHub(nil)
	h.Publish(ChannelConvs, seen("tab-a", 1))
	c := testClient()
	sub, seq, replayed := h.subscribe(c, ChannelConvs, "", nil)
	if replayed || seq != 1 {
		t.Fatalf("seq %d replayed %v", seq, replayed)
	}
	h.Publish(ChannelConvs, seen("tab-a", 2)) // between subscribe and snapshot
	if f := c.frames(t); len(f) != 0 {
		t.Fatalf("an event before the snapshot: %v", f)
	}
	snap, _ := json.Marshal(SnapshotFrame{Ch: ChannelConvs, Seq: seq, Mode: SnapshotReplace, Convs: &ConvsSnapshot{Conversations: []Conversation{}}})
	h.release(sub, snap)
	h.Publish(ChannelConvs, seen("tab-a", 3))
	f := c.frames(t)
	if len(f) != 3 {
		t.Fatalf("frames %v", f)
	}
	if s, ok := f[0].(SnapshotFrame); !ok || s.Seq != 1 {
		t.Fatalf("first %#v", f[0])
	}
	for i, want := range []int64{2, 3} {
		if ev := f[i+1].(EvFrame); ev.Seq != want {
			t.Fatalf("event %d seq %d", i, ev.Seq)
		}
	}
}

func TestHubResumesFromTheRing(t *testing.T) {
	h := NewHub(nil)
	for i := int64(1); i <= 5; i++ {
		h.Publish(ChannelConvs, seen("tab-a", i))
	}
	c := testClient()
	after := int64(3)
	_, seq, replayed := h.subscribe(c, ChannelConvs, h.Epoch(), &after)
	if !replayed || seq != 5 {
		t.Fatalf("replayed %v seq %d", replayed, seq)
	}
	f := c.frames(t)
	if len(f) != 2 || f[0].(EvFrame).Seq != 4 || f[1].(EvFrame).Seq != 5 {
		t.Fatalf("replay %v", f)
	}
	// Another epoch, a cursor the ring does not hold: a snapshot.
	c2 := testClient()
	if _, _, replayed := h.subscribe(c2, ChannelConvs, "ep-old", &after); replayed {
		t.Fatal("a cursor of another epoch replayed")
	}
	h.Reset()
	if h.Seq(ChannelConvs) != 0 {
		t.Fatal("a new epoch starts every channel again")
	}
}

func TestHubUnderLoad(t *testing.T) {
	h := NewHub(nil)
	const writers, each = 8, 200
	var clients []*client
	for i := 0; i < 4; i++ {
		c := testClient()
		_, _, _ = h.subscribe(c, ChannelConvs, h.Epoch(), new(int64))
		clients = append(clients, c)
	}
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				h.Publish(ChannelConvs, seen("tab-a", int64(i+1)))
			}
		}()
	}
	// Subscribers come and go meanwhile.
	for i := 0; i < 50; i++ {
		c := testClient()
		s, seq, _ := h.subscribe(c, ChannelConvs, "", nil)
		snap, _ := json.Marshal(SnapshotFrame{Ch: ChannelConvs, Seq: seq, Mode: SnapshotReplace, Convs: &ConvsSnapshot{Conversations: []Conversation{}}})
		h.release(s, snap)
		h.unsubscribe(c, ChannelConvs)
	}
	wg.Wait()
	for _, c := range clients {
		f := c.frames(t)
		if len(f) != writers*each {
			t.Fatalf("got %d events", len(f))
		}
		for i, fr := range f {
			if fr.(EvFrame).Seq != int64(i+1) {
				t.Fatalf("event %d has seq %d: out of order", i, fr.(EvFrame).Seq)
			}
		}
	}
}
