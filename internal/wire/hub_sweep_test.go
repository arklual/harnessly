package wire

import (
	"testing"
	"time"
)

// A conversation's channel nobody holds gives its events back once they are past RingAge and is then dropped; when it
// comes back it goes on past its last seq, and a page with a cursor of the old one is given a snapshot.
func TestHubDropsQuietConversationChannels(t *testing.T) {
	h := NewHub(nil)
	now := time.Unix(1000, 0)
	h.now = func() time.Time { return now }
	ch := ConvChannel("tab-a")
	for i := 0; i < 50; i++ {
		h.Publish(ch, BackgroundRemovedEv{ID: "x"})
	}
	h.Publish(ChannelConvs, seen("tab-a", 1))
	if h.Channels() != 2 {
		t.Fatalf("channels %d", h.Channels())
	}

	// Within RingAge the channel keeps its events without a reader.
	now = now.Add(RingAge - time.Second)
	h.Sweep()
	if h.Channels() != 2 || h.chans[ch].ring.n != 50 {
		t.Fatalf("swept early: channels %d", h.Channels())
	}

	// Past it, an event of any other channel turns the sweep: no reader ever came, and nothing is kept.
	now = now.Add(sweepEvery + time.Second)
	h.Publish(ChannelConvs, seen("tab-a", 2))
	if _, held := h.chans[ch]; held {
		t.Fatal("the quiet channel is still held")
	}
	if c := h.chans[ChannelConvs]; c == nil || c.seq != 2 {
		t.Fatal("convs is never dropped")
	}

	// The channel comes back past every seq it gave.
	if got := h.Seq(ch); got != 51 {
		t.Fatalf("seq of the dropped channel %d", got)
	}
	c := testClient()
	after := int64(50)
	if _, seq, replayed := h.subscribe(c, ch, h.Epoch(), &after); replayed || seq != 51 {
		t.Fatalf("a cursor of the dropped channel: replayed %v, seq %d", replayed, seq)
	}
	if s := h.Publish(ch, BackgroundRemovedEv{ID: "y"}); s != 52 {
		t.Fatalf("the next seq %d", s)
	}

	// A channel a socket holds is kept however quiet it is.
	now = now.Add(time.Hour)
	h.Sweep()
	if _, held := h.chans[ch]; !held {
		t.Fatal("a channel with a socket was dropped")
	}
	h.detach(c)
	h.Sweep()
	if _, held := h.chans[ch]; held {
		t.Fatal("the channel outlived its last socket and its events")
	}
}

// Forget drops a deleted conversation's channel at once, unless a page still holds it.
func TestHubForgetsAChannel(t *testing.T) {
	h := NewHub(nil)
	ch := ConvChannel("tab-a")
	h.Publish(ch, BackgroundRemovedEv{ID: "x"})
	c := testClient()
	h.subscribe(c, ch, "", nil)
	if h.Forget(ch) {
		t.Fatal("a channel a page holds was forgotten")
	}
	h.detach(c)
	if !h.Forget(ch) || h.Channels() != 0 {
		t.Fatalf("not forgotten: channels %d", h.Channels())
	}
	if h.Forget(ChannelConvs) {
		t.Fatal("convs is not a conversation's channel")
	}
	if s := h.Publish(ch, BackgroundRemovedEv{ID: "y"}); s != 3 {
		t.Fatalf("the channel came back at seq %d", s)
	}
	h.Reset()
	if s := h.Publish(ch, BackgroundRemovedEv{ID: "z"}); s != 1 {
		t.Fatalf("a new epoch starts at 1: %d", s)
	}
}
