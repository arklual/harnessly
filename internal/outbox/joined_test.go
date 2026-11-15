package outbox

import (
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// HRN-1470, 14.11 (tab-pwn0vy44r). Two messages steered into a working Claude one after the other came out of its
// queue as one `queued_command` — the second's words, a line, the first's. The entry went to the message on its way
// only; the other stood «в очереди агента» over the field for 30 minutes after the agent had answered it, then failed
// as unconfirmed.
const (
	joinedA = "В смысле, подскажи пожалуйста. У нас же вот типа, ты говоришь, что голосовой режим работать не будет. Почему мы типа не можем так делать? Поясни пожалуйста."
	joinedB = "А ещё, короче, знаешь, переход между досками и документами какой-то супердёрганый, хотя интерфейс у них общий."
)

func twoSteered(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	r.leaves()
	r.size = 100 // the transcript's size when typed: a retry looks in it past that
	r.send("b", joinedB, wire.ModeSteer)
	r.pump()
	r.send("a", joinedA, wire.ModeSteer)
	r.pump()
	for _, id := range []string{"a", "b"} {
		if st := r.state(id); st.K != wire.MsgInInput {
			t.Fatalf("%s not handed: %s", id, r.trail(id))
		}
	}
	return r
}

// One entry, two messages: both are taken by it and join the running turn, in whichever order Claude joined them.
func TestOneEntryOfMessagesReadTogetherTakesThemAll(t *testing.T) {
	for name, entry := range map[string]string{
		"later first":            joinedA + "\n" + joinedB,
		"in order":               joinedB + "\n" + joinedA,
		"white space":            joinedA + "\n\n  " + joinedB + "\n",
		"with a third of others": "Ещё одно сообщение человека из терминала\n" + joinedA + "\n" + joinedB,
	} {
		t.Run(name, func(t *testing.T) {
			r := twoSteered(t)
			turn := "c:aa#0"
			id, ch := r.box.UserEntryText("c:bb#3", entry, 1<<40, &turn, r.clock.Now())
			r.record(ch)
			if id == "" {
				t.Fatalf("not taken: a %s, b %s", r.trail("a"), r.trail("b"))
			}
			for _, c := range []string{"a", "b"} {
				if st := r.state(c); st.K != wire.MsgInTurn || st.TurnID != turn {
					t.Fatalf("%s: %s", c, r.trail(c))
				}
				if got := r.box.Status([]string{c})[0]; got.Handed == nil || got.Handed.EntryID == nil || *got.Handed.EntryID != "c:bb#3" {
					t.Fatalf("%s status: %+v", c, got)
				}
			}
			r.advance(time.Hour)
			for _, c := range []string{"a", "b"} {
				if st := r.state(c); st.K != wire.MsgInTurn {
					t.Fatalf("%s moved on its own: %s", c, r.trail(c))
				}
			}
		})
	}
}

// Only a message the entry holds whole is taken with it: a message that merely shares words with a line stays.
func TestAnEntryDoesNotTakeAMessageItHoldsOnlyPartOf(t *testing.T) {
	r := twoSteered(t)
	entry := joinedB + "\nВ смысле, подскажи пожалуйста. У нас же вот типа, ты говоришь, что голосовой режим работать не будет."
	_, ch := r.box.UserEntryText("c:bb#3", entry, 1<<40, nil, r.clock.Now())
	r.record(ch)
	if st := r.state("b"); st.K != wire.MsgAccepted {
		t.Fatalf("b: %s", r.trail("b"))
	}
	if st := r.state("a"); st.K != wire.MsgInInput {
		t.Fatalf("a taken by part of its words: %s", r.trail("a"))
	}
}

// The entry came and was not matched on the way (read before this runtime knew of it, a lost signal): at the term of
// its wait the message is looked for in the transcript read, once, and accepted rather than failed as unconfirmed.
func TestAtItsTermASteeredMessageIsLookedForInTheTranscriptRead(t *testing.T) {
	r := twoSteered(t)
	var asked []string
	r.box.Transcript = func(_ int64, _ wire.Ms, text string) (string, bool) {
		asked = append(asked, text)
		if screen.JoinedIn(joinedA+"\n"+joinedB, text) {
			return "c:bb#3", true
		}
		return "", false
	}
	r.clock.Sleep(SteerDeadline + time.Second)
	r.record(r.box.Tick(r.clock.Now()))
	for _, c := range []string{"a", "b"} {
		if st := r.state(c); st.K != wire.MsgAccepted || st.EntryID == nil || *st.EntryID != "c:bb#3" {
			t.Fatalf("%s: %s", c, r.trail(c))
		}
	}
	if len(asked) != 2 {
		t.Fatalf("asked %d times: %q", len(asked), asked)
	}
	// Asked once: a second tick asks nothing more.
	r.record(r.box.Tick(r.clock.Now().Add(time.Minute)))
	if len(asked) != 2 {
		t.Fatalf("asked again: %d", len(asked))
	}
}

// Not in the transcript at its term: unconfirmed, as before — and its Повторить finds it in a joined entry later.
func TestAMessageFailedUnconfirmedIsFoundInAJoinedEntryOnRetry(t *testing.T) {
	r := twoSteered(t)
	entry := ""
	r.box.Transcript = func(_ int64, _ wire.Ms, text string) (string, bool) {
		if entry != "" && screen.JoinedIn(entry, text) {
			return "c:bb#3", true
		}
		return "", false
	}
	r.clock.Sleep(SteerDeadline + time.Second)
	r.record(r.box.Tick(r.clock.Now()))
	if st := r.state("a"); st.K != wire.MsgFailed || st.Code != wire.MsgFailedUnconfirmed {
		t.Fatalf("a: %s", r.trail("a"))
	}
	entry = joinedA + "\n" + joinedB
	ch, code := r.box.Retry("a", nil, r.clock.Now())
	r.record(ch)
	if code != "" {
		t.Fatalf("retry refused: %s", code)
	}
	if st := r.state("a"); st.K != wire.MsgAccepted {
		t.Fatalf("a typed again instead: %s", r.trail("a"))
	}
}

// A restart that caught both on their way finds each in the one entry, not only the first.
func TestARestartFindsEveryMessageOfAJoinedEntry(t *testing.T) {
	r := twoSteered(t)
	r.box.Transcript = func(_ int64, _ wire.Ms, text string) (string, bool) {
		if screen.JoinedIn(joinedA+"\n"+joinedB, text) {
			return "c:bb#3", true
		}
		return "", false
	}
	r.record(r.box.Restarted(nil, nil, r.clock.Now()))
	for _, c := range []string{"a", "b"} {
		if st := r.state(c); st.K != wire.MsgAccepted {
			t.Fatalf("%s: %s", c, r.trail(c))
		}
	}
}
