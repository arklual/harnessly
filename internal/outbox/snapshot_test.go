package outbox

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// four messages with pictures, sent while the agent was busy, were delivered and answered — on disk they are
// done with the turn of their transcript line — yet a page that had cached them as queued showed them in the queue for
// hours. A snapshot lists every message not over and the finished ones of the last FinalKept only; the page takes a
// message of the runtime that the snapshot no longer names as ended. These are the records of prod's tab-dBSEBV (texts
// cut), and the contract the page now leans on.
func TestASnapshotNamesEveryMessageNotOverAndDropsTheLongFinished(t *testing.T) {
	b, st := stuckBox(t)

	var lastEnd int64
	open := map[string]bool{}
	for _, r := range st.Records {
		if final(r.Message.State.K) {
			if len(r.Message.Attachments) == 0 || r.Message.State.TurnID == "" {
				t.Fatalf("%s: a delivered message with pictures and its turn expected, got %+v", r.Message.ClientID, r.Message)
			}
			lastEnd = max(lastEnd, r.EndedAt)
		} else {
			open[r.Message.ClientID] = true
		}
	}
	if len(open) == 0 || lastEnd == 0 {
		t.Fatal("fixture must hold both finished and open messages")
	}

	ids := func(now time.Time) map[string]wire.MessageStateKind {
		out := map[string]wire.MessageStateKind{}
		for _, m := range b.Messages(now) {
			out[m.ClientID] = m.State.K
		}
		return out
	}
	soon := ids(time.UnixMilli(lastEnd).Add(FinalKept / 2))
	if len(soon) != len(st.Records) {
		t.Fatalf("within FinalKept every message is listed, got %v", soon)
	}
	for id, k := range soon {
		if !open[id] && k != wire.MsgDone {
			t.Fatalf("%s listed as %s, want done", id, k)
		}
	}
	later := ids(time.UnixMilli(lastEnd).Add(FinalKept + time.Minute))
	if len(later) != len(open) {
		t.Fatalf("after FinalKept only the open messages are listed, got %v", later)
	}
	for id := range open {
		if _, ok := later[id]; !ok {
			t.Fatalf("open message %s missing from the snapshot", id)
		}
	}
}

func stuckBox(t *testing.T) (*Box, State) {
	t.Helper()
	raw, err := os.ReadFile("testdata/delivered-with-pictures.json")
	if err != nil {
		t.Fatal(err)
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	b := New("tab-dBSEBV", wire.DefaultLimits, nil)
	b.Restore(st)
	return b, st
}

// The stuck rows' Delete, «Сразу», move and Изменить were refused too-late («не получилось, попробуйте ещё раз»). A
// command on a message that is over is accepted and answers with the message as it is (done, its turn), so the page
// drops the row; the line, the record and the box's revision stay as they were — nothing is sent again.
func TestACommandOnADeliveredMessageShowsItAsItIsInsteadOfRefusing(t *testing.T) {
	const delivered = "01a170db-b45b-769c-a4ab-0236cf95cf75" // «Вот четыре макета карточки…», 4 pictures
	now := time.UnixMilli(1792917400000)
	cmds := map[string]func(b *Box) (Change, wire.RefuseCode){
		"cancel": func(b *Box) (Change, wire.RefuseCode) { return b.Cancel(delivered, now) },
		"now": func(b *Box) (Change, wire.RefuseCode) {
			return b.Promote(delivered, wire.PromoteToNow, wire.AgentState{K: wire.StateIdle}, now)
		},
		"steer": func(b *Box) (Change, wire.RefuseCode) {
			return b.Promote(delivered, wire.PromoteToSteer, wire.AgentState{K: wire.StateIdle}, now)
		},
		"move": func(b *Box) (Change, wire.RefuseCode) { return b.Move(delivered, "", now) },
		"moveTo": func(b *Box) (Change, wire.RefuseCode) {
			return b.Move(delivered, "01a1722f-9735-7006-ad75-bffbd3983ccd", now)
		},
	}
	for name, cmd := range cmds {
		t.Run(name, func(t *testing.T) {
			b, _ := stuckBox(t)
			before, _ := b.Message(delivered)
			q := b.Queue()
			ch, code := cmd(b)
			if code != "" {
				t.Fatalf("refused %q", code)
			}
			if len(ch.Events) != 1 {
				t.Fatalf("want one message_upsert, got %+v", ch.Events)
			}
			up, ok := ch.Events[0].(wire.MessageUpsertEv)
			if !ok || up.Message.ClientID != delivered || up.Message.State.K != wire.MsgDone || up.Message.State.TurnID == "" {
				t.Fatalf("%+v", ch.Events[0])
			}
			after, _ := b.Message(delivered)
			if after.Rev != before.Rev || after.State != before.State || after.Mode != before.Mode {
				t.Fatalf("the record moved: %+v -> %+v", before, after)
			}
			if got := b.Queue(); got.Rev != q.Rev || len(got.Items) != len(q.Items) {
				t.Fatalf("the line moved: %+v -> %+v", q, got)
			}
		})
	}
}

// A message the person took back is over too: Delete again or «Сразу» on it shows it cancelled, not «не получилось».
func TestACommandOnACancelledMessageShowsItAsItIs(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.send("m1", "x", wire.ModeQueue)
	ch, _ := r.box.Cancel("m1", r.clock.Now())
	r.record(ch)
	for _, cmd := range []func() (Change, wire.RefuseCode){
		func() (Change, wire.RefuseCode) { return r.box.Cancel("m1", r.clock.Now()) },
		func() (Change, wire.RefuseCode) {
			return r.box.Promote("m1", wire.PromoteToNow, r.agent.State, r.clock.Now())
		},
		func() (Change, wire.RefuseCode) { return r.box.Move("m1", "", r.clock.Now()) },
	} {
		ch, code := cmd()
		if code != "" || len(ch.Events) != 1 {
			t.Fatalf("%q %+v", code, ch.Events)
		}
		if up, ok := ch.Events[0].(wire.MessageUpsertEv); !ok || up.Message.State.K != wire.MsgCancelled {
			t.Fatalf("%+v", ch.Events[0])
		}
	}
}
