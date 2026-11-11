package tmux

import (
	"context"
	"errors"
	"testing"
	"time"
)

var errHoldOver = errors.New("the hold is over")

// A writer whose hold of the pane ended types nothing: no tmux command is run for it.
func TestKeysAfterTheHoldTypeNothing(t *testing.T) {
	f := newFake()
	f.servers["main"] = [][2]string{{"nw-a", "%1"}}
	tm := f.tmux(Config{Names: fakeNames})
	held, cancel := context.WithCancelCause(context.Background())
	if err := tm.With(held).SendLiteralThenEnter("nw-a", "one"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	before := len(f.calls)
	f.mu.Unlock()
	cancel(errHoldOver)
	k := tm.With(held)
	for _, err := range []error{k.SendKeys("nw-a", "x"), k.SendLiteralThenEnter("nw-a", "x"), k.SendBracketedPaste("nw-a", "x"),
		k.SendKeySequence("nw-a", "Enter"), k.SendRawKeys("nw-a", "Enter")} {
		if !errors.Is(err, errHoldOver) {
			t.Fatalf("a send after the hold: %v", err)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != before {
		t.Fatalf("tmux was run after the hold: %v", f.calls[before:])
	}
}

// A command under way when the hold ends is cut short with it, long before its own timeout.
func TestACommandUnderWayEndsWithTheHold(t *testing.T) {
	held, cancel := context.WithCancelCause(context.Background())
	time.AfterFunc(50*time.Millisecond, func() { cancel(errHoldOver) })
	start := time.Now()
	r := execRunnerCtx(held, writeTimeout, "", "sleep", "20")
	if took := time.Since(start); r.err == nil || r.timedOut || took > 5*time.Second {
		t.Fatalf("err %v, timed out %v, took %s", r.err, r.timedOut, took)
	}
}
