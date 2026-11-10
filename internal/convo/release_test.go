package convo

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/feed"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func (r *rig) feed() *feed.Feed {
	r.t.Helper()
	a := r.rt.actor(r.id)
	if a == nil {
		r.t.Fatal("no conversation")
	}
	return a.c.fd
}

func snapJSON(t *testing.T, s wire.ConvSnapshot) string {
	t.Helper()
	raw, err := json.Marshal(struct {
		Turns   []wire.Turn
		Entries []wire.Entry
	}{s.Turns, s.Entries})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// HRN-901: after a start every conversation's feed lets go of what it read, and an idle chat's looks do not read it
// again; the page's snapshot is the one it was, and what the agent writes next goes out as before.
func TestAQuietChatHoldsNoFeedAfterARestart(t *testing.T) {
	r := newRig(t)
	r.open()
	r.user("question")
	r.say("answer")
	r.turnEnd()
	r.done()
	r.settle(wire.StateReady)
	want := snapJSON(t, r.snap())
	r.rt.Stop()

	r.restart()
	if !r.feed().Released() {
		t.Fatal("the feed holds what it read after the start")
	}
	for range 30 {
		r.tick(time.Second)
	}
	if !r.feed().Released() {
		t.Fatal("an idle chat's looks read its feed again")
	}
	if got := snapJSON(t, r.snap()); got != want {
		t.Fatalf("the snapshot after a release differs\n got %s\nwant %s", got, want)
	}

	r.user("next question")
	r.busy()
	r.tick(time.Second)
	if len(r.events(wire.EvStep)) == 0 {
		t.Fatal("the new entry did not go out")
	}
	if s := r.snap(); len(s.Entries) == 0 || s.Entries[len(s.Entries)-1].Text != "next question" {
		t.Fatalf("the snapshot misses the new entry: %+v", s.Entries)
	}
}

// A feed read again is let go of once nobody reads it for FeedKept.
func TestAFeedNobodyReadsIsReleased(t *testing.T) {
	r := newRig(t)
	r.open()
	r.user("question")
	r.say("answer")
	r.turnEnd()
	r.done()
	r.settle(wire.StateReady)
	_ = r.snap()
	if r.feed().Released() {
		t.Fatal("released while it was read")
	}
	for i := 0; i < int(FeedKept/time.Second)+5; i++ {
		r.tick(time.Second)
	}
	if !r.feed().Released() {
		t.Fatalf("not released after %s unread", FeedKept)
	}
}
