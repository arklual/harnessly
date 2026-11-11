package outbox

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// Client ids as the page makes them (UUIDv7): the first 48 bits are when the message was written.
const (
	made1 = "01900000-0001-7000-8000-000000000001"
	made2 = "01900000-0002-7000-8000-000000000002"
	made3 = "01900000-0003-7000-8000-000000000003"
)

func first(ch Change, _ wire.RefuseCode) Change { return ch }

func listed(b *Box, now time.Time) []string {
	ids := []string{}
	for _, m := range b.Messages(now) {
		ids = append(ids, m.ClientID)
	}
	return ids
}

// The first message's send was late on the road and the second came before it: the line is in the order they were
// written, and so are the lists of messages.
func TestASendThatCameLateStandsWhereItWasWritten(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	for _, id := range []string{made2, made3, made1} {
		if code := r.send(id, "text of "+id, wire.ModeQueue); code != "" {
			t.Fatal(code)
		}
	}
	want := []string{made1, made2, made3}
	if got := r.box.Queue().Items; !reflect.DeepEqual(got, want) {
		t.Fatalf("queue %v", got)
	}
	if got := listed(r.box, r.clock.Now()); !reflect.DeepEqual(got, want) {
		t.Fatalf("messages %v", got)
	}
	r.pump()
	if st := r.state(made1); st.Position != 1 {
		t.Fatalf("%+v", st)
	}
	if st := r.state(made3); st.Position != 3 {
		t.Fatalf("%+v", st)
	}
}

// A message already typed into the agent is not overtaken: the late one only goes ahead of what still waits.
func TestALateSendDoesNotOvertakeWhatIsTyped(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	r.leaves()
	r.send(made2, "second", wire.ModeQueue)
	r.pump()
	if st := r.state(made2); st.K != wire.MsgInInput {
		t.Fatalf("%+v", st)
	}
	r.send(made3, "third", wire.ModeQueue)
	r.send(made1, "first", wire.ModeQueue)
	if got := r.box.Queue().Items; !reflect.DeepEqual(got, []string{made1, made3}) {
		t.Fatalf("queue %v", got)
	}
	if st := r.state(made2); st.K != wire.MsgInInput || r.box.InFlight() != made2 {
		t.Fatalf("%+v", st)
	}
}

// Ids that say nothing of their time (an older page, the runtime's own) keep the order they came in, and a late
// message never goes ahead of an urgent one.
func TestALineOfOtherIdsKeepsTheOrderOfArrival(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.send("m2", "second", wire.ModeQueue)
	r.send("m1", "first", wire.ModeQueue)
	r.send(made2, "later", wire.ModeQueue)
	r.send(made1, "earlier", wire.ModeQueue)
	if got := r.box.Queue().Items; !reflect.DeepEqual(got, []string{"m2", "m1", made1, made2}) {
		t.Fatalf("queue %v", got)
	}

	u := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	u.record(first(u.box.Send(Draft{ClientID: made2, Text: "/compact", Mode: wire.ModeQueue, Kind: wire.KindCommand}, u.agent.State, u.clock.Now())))
	u.send(made3, "now", wire.ModeNow)
	u.send(made1, "earlier", wire.ModeQueue)
	if got := u.box.Queue().Items; !reflect.DeepEqual(got, []string{made3, made1, made2}) {
		t.Fatalf("queue %v", got)
	}
}

// The cancel came before the send (the send was late on the road, or waited in a hub): the message is not queued and
// not typed, every page sees it cancelled, and the same send again gets the same record.
func TestACancelBeforeTheSendKeepsItOutOfTheLine(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
	r.leaves()
	ch, code := r.box.Cancel(made1, r.clock.Now())
	r.record(ch)
	if code != wire.RefuseNotFound || len(ch.Events) != 0 {
		t.Fatalf("%s %v", code, ch.Events)
	}
	if code := r.send(made1, "taken back", wire.ModeQueue); code != "" {
		t.Fatal(code)
	}
	if st := r.state(made1); st.K != wire.MsgCancelled || st.By != wire.CancelledByUser {
		t.Fatalf("%+v", st)
	}
	up, ok := r.events[len(r.events)-1].(wire.MessageUpsertEv)
	if !ok || up.Message.State.K != wire.MsgCancelled || up.Message.Text != "taken back" {
		t.Fatalf("%+v", r.events)
	}
	if len(r.box.Queue().Items) != 0 {
		t.Fatalf("queue %v", r.box.Queue().Items)
	}
	r.pump()
	if r.jobs != 0 || len(r.typed()) != 0 {
		t.Fatalf("typed %v", r.typed())
	}
	if code := r.send(made1, "taken back", wire.ModeQueue); code != "" || r.state(made1).K != wire.MsgCancelled {
		t.Fatalf("%s %+v", code, r.state(made1))
	}
	// Another message is queued as ever.
	if code := r.send(made2, "goes", wire.ModeQueue); code != "" || r.state(made2).K != wire.MsgQueued {
		t.Fatalf("%s %+v", code, r.state(made2))
	}
}

// The note of a cancel is written with the box, has a term and a bound; the cancel of a message delivered long ago is
// not noted at all.
func TestTheNoteOfACancelIsKeptForATermAndBounded(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.box.Cancel(made1, r.clock.Now())
	b := New("tab-1", wire.DefaultLimits, nil)
	b.Restore(r.box.State())
	ch, code := b.Send(Draft{ClientID: made1, Text: "taken back", Mode: wire.ModeQueue, Kind: wire.KindPrompt}, r.agent.State, r.clock.Now())
	if m, _ := b.Message(made1); code != "" || len(ch.Events) != 1 || m.State.K != wire.MsgCancelled {
		t.Fatalf("%s %+v", code, m)
	}

	r.box.Cancel(made2, r.clock.Now())
	r.clock.Sleep(CancelKept + time.Minute)
	r.record(r.box.Tick(r.clock.Now()))
	if code := r.send(made2, "after the term", wire.ModeQueue); code != "" || r.state(made2).K != wire.MsgQueued {
		t.Fatalf("%s %+v", code, r.state(made2))
	}

	for i := range CancelMax + 10 {
		r.box.takeBack(fmt.Sprintf("x-%d", i), wire.Ms(i+1))
	}
	if len(r.box.cancelled) != CancelMax {
		t.Fatalf("%d notes", len(r.box.cancelled))
	}

	r.box.forget("long-ago", r.clock.Now().UnixMilli())
	r.box.Cancel("long-ago", r.clock.Now())
	if _, noted := r.box.cancelled["long-ago"]; noted {
		t.Fatal("the cancel of a delivered message was noted")
	}
	if code := r.send("long-ago", "again", wire.ModeQueue); code != wire.RefuseTooLate {
		t.Fatal(code)
	}
}
