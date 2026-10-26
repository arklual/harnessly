package outbox

import (
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The message of 26.10 that stood «в очереди агента» for 18 minutes while the agent had answered it: typed into a
// working Claude, it was queued by Claude cut short (its first 102 characters), so the entry did not carry the whole text.
const (
	cutWhole = "Да, и ещё важно интеграцию КиМИ, ГЛМ и так далее. Ну, вот КиМИ— это самое важное. А и кодексы катить надо осаб прям Вот, как только будет доступно. Сразу раскатываем на прод."
	cutTook  = "Да, и ещё важно интеграцию КиМИ, ГЛМ и так далее. Ну, вот КиМИ— это самое важное. А и кодексы катить н"
)

func steered(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.leaves()
	r.send("m1", cutWhole, wire.ModeSteer)
	r.pump()
	if st := r.state("m1"); st.K != wire.MsgInInput {
		t.Fatalf("not handed: %s", r.trail("m1"))
	}
	return r
}

// The user entry of a steered message is its: the whole text, the text with its white space changed, or its start that
// Claude cut short. It is accepted with that entry and joins the running turn, and its status says where it went.
func TestASteeredMessageIsTakenByItsEntryWholeOrCutShort(t *testing.T) {
	for name, entry := range map[string]string{
		"whole":                cutWhole,
		"white space":          strings.ReplaceAll(cutWhole, " ", "\n  "),
		"cut short":            cutTook,
		"cut short, re-spaced": strings.ReplaceAll(cutTook, ". ", ".\n"),
	} {
		t.Run(name, func(t *testing.T) {
			r := steered(t)
			turn := "c:aa#0"
			id, ch := r.box.UserEntryText("c:bb#3", entry, 1<<40, &turn, r.clock.Now())
			r.record(ch)
			if id != "m1" {
				t.Fatalf("not taken: %q (%s)", id, r.trail("m1"))
			}
			if st := r.state("m1"); st.K != wire.MsgInTurn || st.TurnID != turn {
				t.Fatalf("state: %+v", st)
			}
			got := r.box.Status([]string{"m1"})[0]
			if got.K != wire.StatusKnown || got.Handed == nil || got.Handed.Where != wire.HandedTranscript ||
				*got.Handed.EntryID != "c:bb#3" || got.Handed.At == 0 {
				t.Fatalf("status: %+v %+v", got, got.Handed)
			}
			if err := got.Validate(); err != nil {
				t.Fatal(err)
			}
			r.advance(time.Hour)
			if st := r.state("m1"); st.K != wire.MsgInTurn {
				t.Fatalf("moved on its own: %s", r.trail("m1"))
			}
		})
	}
}

// Somebody else's entry is not taken for it: a short start any message could have, or other words.
func TestASteeredMessageIsNotTakenByAnotherEntry(t *testing.T) {
	for name, entry := range map[string]string{
		"short start": "Да, и ещё важно",
		"other words": "Посмотри логи оплаты, пожалуйста, и скажи, что там",
	} {
		t.Run(name, func(t *testing.T) {
			r := steered(t)
			if id, ch := r.box.UserEntryText("c:bb#3", entry, 1<<40, nil, r.clock.Now()); id != "" || len(ch.Events) != 0 {
				t.Fatalf("taken by %q: %+v", entry, ch)
			}
			if st := r.state("m1"); st.K != wire.MsgInInput {
				t.Fatalf("state: %s", r.trail("m1"))
			}
		})
	}
}
