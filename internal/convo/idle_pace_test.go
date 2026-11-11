package convo

import (
	"reflect"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// turnsIn counts the clock turns and the looks of one conversation's actor over d, by the times wake and lookEvery
// give it, the way loop waits on them with nothing else happening.
func turnsIn(c *conv, start time.Time, d time.Duration) (turns, looks int) {
	now, end := start, start.Add(d)
	next := start.Add(c.lookEvery())
	for {
		w := c.wake(now)
		if next.Before(w) {
			w = next
		}
		if w.After(end) {
			return turns, looks
		}
		now = w
		if !now.Before(next) {
			looks++
			next = now.Add(c.lookEvery())
		}
		turns++
	}
}

// An agent at rest at its prompt costs a turn and a look every LookResting, not a turn every second and a look every
// three: a runtime holds a hundred and more such chats.
func TestARestingChatTurnsSeldom(t *testing.T) {
	r := newRig(t)
	r.open()
	c := r.rt.actor(convID).c
	now := r.clock.Now()
	c.recDirty, c.stateDirty, c.titleDueAt = false, false, time.Time{}

	for _, k := range []wire.AgentStateKind{wire.StateIdle, wire.StateReady} {
		c.rec.State = wire.AgentState{K: k}
		if got := c.wake(now).Sub(now); got != LookResting {
			t.Fatalf("%s: the clock turns in %v, want %v", k, got, LookResting)
		}
		turns, looks := turnsIn(c, now, 10*time.Minute)
		// Before: 600 turns and 200 looks in ten minutes.
		if turns != 40 || looks != 40 {
			t.Fatalf("%s: %d turns and %d looks in ten minutes, want 40 and 40", k, turns, looks)
		}
	}

	// A background task still running is watched as before: its end is read from its files.
	c.rec.State = wire.AgentState{K: wire.StateIdle}
	c.bg = []wire.BackgroundTask{{ID: "b1", Status: wire.BgStatusRunning}}
	if got := c.lookEvery(); got != 3*LookEvery {
		t.Fatalf("an idle agent with a running task is looked at every %v", got)
	}
	c.bg = []wire.BackgroundTask{{ID: "b1", Status: wire.BgStatusDone}}
	if got := c.lookEvery(); got != LookResting {
		t.Fatalf("an idle agent whose tasks ended is looked at every %v", got)
	}

	// A model change waiting to be typed keeps its pace.
	c.rec.PendingModel = &wire.PendingModel{ID: "opus"}
	if got := c.lookEvery(); got != 3*LookEvery {
		t.Fatalf("an idle agent with a model change waiting is looked at every %v", got)
	}
	c.rec.PendingModel = nil

	// A real time sooner than the rest still wakes it on time.
	c.titleDueAt = now.Add(2 * time.Second)
	if got := c.wake(now).Sub(now); got != 2*time.Second {
		t.Fatalf("a title due in 2 s wakes the resting clock in %v", got)
	}
	c.titleDueAt = time.Time{}

	// An agent at work keeps the second.
	c.rec.State = wire.AgentState{K: wire.StateWorking}
	if got := c.wake(now).Sub(now); got != LookEvery {
		t.Fatalf("a working agent's clock turns in %v", got)
	}
}

// sameState tells apart every field of the agent state, so a field added later is not missed by the comparison.
func TestSameStateSeesEveryField(t *testing.T) {
	turn, other := "t1", "t2"
	base := wire.AgentState{K: wire.StateWorking, Since: 1, Wait: &wire.Wait{SlowAt: 1, DeadlineAt: 2}, TurnID: &turn}
	same := base
	same.Wait, same.TurnID = &wire.Wait{SlowAt: 1, DeadlineAt: 2}, &other
	*same.TurnID = turn
	if !sameState(base, same) {
		t.Fatal("equal states with other pointers differ")
	}
	typ := reflect.TypeOf(base)
	for i := range typ.NumField() {
		changed := base
		f := reflect.ValueOf(&changed).Elem().Field(i)
		switch f.Kind() {
		case reflect.Pointer:
			if f.IsNil() {
				t.Fatalf("set %s in base", typ.Field(i).Name)
			}
			f.Set(reflect.Zero(f.Type()))
		case reflect.String:
			f.SetString(f.String() + "x")
		case reflect.Bool:
			f.SetBool(!f.Bool())
		case reflect.Int, reflect.Int64:
			f.SetInt(f.Int() + 1)
		default:
			t.Fatalf("field %s of kind %s: teach sameState and this test about it", typ.Field(i).Name, f.Kind())
		}
		if sameState(base, changed) {
			t.Fatalf("a change of %s is not seen", typ.Field(i).Name)
		}
	}
	waitMoved := base
	waitMoved.Wait = &wire.Wait{SlowAt: 1, DeadlineAt: 3}
	if sameState(base, waitMoved) {
		t.Fatal("a moved deadline is not seen")
	}
}
