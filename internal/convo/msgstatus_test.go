package convo

import (
	"context"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/outbox"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The page asks where its messages are by their client ids: one the agent took is known with its entry, one the
// runtime never had is unknown, one finished long ago is expired. A conversation that is not here is not-found.
func TestTheStatusOfMessagesByClientID(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	r.agentTakes()
	if res := r.send("m1", "как дела", wire.ModeQueue); res.R != wire.ResultQueued {
		t.Fatalf("%+v", res)
	}
	r.tick(time.Second)
	got, err := r.rt.MessageStatus(context.Background(), r.id, []string{"m1", "never"})
	if err != nil {
		t.Fatal(err)
	}
	m1, never := got.Messages[0], got.Messages[1]
	if m1.K != wire.StatusKnown || m1.Message.Text != "как дела" || m1.Handed == nil || m1.Handed.Where != wire.HandedTranscript {
		t.Fatalf("m1: %+v %+v", m1, m1.Handed)
	}
	if never.K != wire.StatusUnknown {
		t.Fatalf("never: %+v", never)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}

	// The turn ends; a day later the record is let go and the id is expired.
	r.hook("prompt-submit")
	r.say("Хорошо!")
	r.turnEnd()
	r.idle()
	r.hook("stop")
	r.tick(outbox.FinalStored + time.Minute)
	got, err = r.rt.MessageStatus(context.Background(), r.id, []string{"m1"})
	if err != nil || got.Messages[0].K != wire.StatusExpired {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := r.rt.MessageStatus(context.Background(), "tab-nothere1", []string{"m1"}); err != ErrNotFound {
		t.Fatalf("%v", err)
	}
}

// The message of 26.10: steered into a working Claude, queued by it cut short (its first 102 characters), and read at
// its next turn. The user entry is the message's — it carries the client id to the pages, the message leaves the strip
// (in the turn), and its status says the transcript has it, under that entry.
func TestASteeredMessageCutShortByClaudeIsItsEntrys(t *testing.T) {
	const (
		whole = "Да, и ещё важно интеграцию КиМИ, ГЛМ и так далее. Ну, вот КиМИ— это самое важное. А и кодексы катить надо осаб прям Вот, как только будет доступно. Сразу раскатываем на прод."
		took  = "Да, и ещё важно интеграцию КиМИ, ГЛМ и так далее. Ну, вот КиМИ— это самое важное. А и кодексы катить н"
	)
	r := compactTurn(t)
	if res := r.send("m1", whole, wire.ModeSteer); res.R != wire.ResultQueued {
		t.Fatalf("%+v", res)
	}
	r.tick(5 * time.Second)
	if st := r.message("m1").State; st.K != wire.MsgInInput {
		t.Fatalf("not handed: %s", r.trail("m1"))
	}
	r.say("Готово.")
	r.turnEnd()
	r.user(took)
	r.say("Принял, Кими первой.")
	r.tick(2 * time.Second)
	if st := r.message("m1").State; st.K != wire.MsgInTurn && st.K != wire.MsgAccepted {
		t.Fatalf("still waiting: %s", r.trail("m1"))
	}
	var carried bool
	var entryID string
	for _, e := range r.snap().Entries {
		if e.T == wire.EntryUser && e.Text == took {
			carried, entryID = e.ClientID != nil && *e.ClientID == "m1", e.ID
		}
	}
	if !carried {
		t.Fatalf("the entry does not carry the client id: %+v", r.snap().Entries)
	}
	got, err := r.rt.MessageStatus(context.Background(), r.id, []string{"m1"})
	if err != nil {
		t.Fatal(err)
	}
	h := got.Messages[0].Handed
	if h == nil || h.Where != wire.HandedTranscript || h.EntryID == nil || *h.EntryID != entryID || h.At == 0 {
		t.Fatalf("status: %+v %+v (entry %s)", got.Messages[0], h, entryID)
	}
}
