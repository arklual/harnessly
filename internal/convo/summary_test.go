package convo

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func TestTheLastTurnIsSummedUpOnceAndTheSummaryIsTheRecap(t *testing.T) {
	r := newRig(t)
	var asked []string
	r.rt.d.Summarizer = func(_ wire.Provider, message string, replies []string) (string, error) {
		asked = append(asked, message+" | "+strings.Join(replies, " / "))
		return "Сделал форму оплаты.\nЖдёт проверки.", nil
	}
	r.named("first")
	r.settle(wire.StateReady)
	r.user("Сделай форму оплаты")
	r.say("Начинаю")
	r.say("Форма готова")
	r.turnEnd()
	r.tick(time.Second)
	turn := r.record().LastTurn
	if turn == nil || turn.Status != wire.TurnDone || turn.EndedAt == nil {
		t.Fatalf("last turn: %+v", turn)
	}
	if res := r.do(wire.ConversationSummarizeCmd{OpID: "sm1", ID: r.id, TurnID: turn.ID}); res.R != wire.ResultAccepted {
		t.Fatalf("result: %+v", res)
	}
	if len(asked) != 1 || asked[0] != "Сделай форму оплаты | Начинаю / Форма готова" {
		t.Fatalf("asked: %v", asked)
	}
	rec := r.record()
	if rec.Recap == nil || rec.Recap.Text != "Сделал форму оплаты.\nЖдёт проверки." || rec.Recap.At < *turn.EndedAt {
		t.Fatalf("recap: %+v", rec.Recap)
	}
	// Another page asks too: the model is not asked again.
	if res := r.do(wire.ConversationSummarizeCmd{OpID: "sm2", ID: r.id, TurnID: turn.ID}); res.R != wire.ResultAccepted || len(asked) != 1 {
		t.Fatalf("again: %+v %v", res, asked)
	}
	// The person writes again: the summary of the turn before is no longer what is happening.
	r.tick(time.Second)
	r.user("Теперь возвраты")
	r.tick(metaEvery)
	r.tick(metaEvery)
	if rec := r.record(); rec.Recap != nil {
		t.Fatalf("the recap stays after the person wrote: %+v", rec.Recap)
	}
}

func TestSummarizeRefusesWhatIsNotTheLastFinishedTurn(t *testing.T) {
	r := newRig(t)
	r.named("first")
	r.settle(wire.StateReady)
	r.user("ask")
	r.say("answer")
	r.turnEnd()
	r.tick(time.Second)
	turn := r.record().LastTurn
	if res := r.do(wire.ConversationSummarizeCmd{OpID: "sm0", ID: r.id, TurnID: turn.ID}); res.R != wire.ResultRefused || res.Code != wire.RefuseUnsupported {
		t.Fatalf("no summarizer: %+v", res)
	}
	calls := 0
	r.rt.d.Summarizer = func(wire.Provider, string, []string) (string, error) { calls++; return "", errors.New("no claude") }
	other := strings.TrimSuffix(turn.ID, "1") + "9"
	if res := r.do(wire.ConversationSummarizeCmd{OpID: "sm1", ID: r.id, TurnID: other}); res.R != wire.ResultRefused || res.Code != wire.RefuseBadRequest {
		t.Fatalf("not the last turn: %+v", res)
	}
	if res := r.do(wire.ConversationSummarizeCmd{OpID: "sm2", ID: r.id, TurnID: turn.ID}); res.R != wire.ResultAccepted || calls != 1 {
		t.Fatalf("a failed call is accepted: %+v %d", res, calls)
	}
	if rec := r.record(); rec.Recap != nil {
		t.Fatalf("no recap without a summary: %+v", rec.Recap)
	}
}
