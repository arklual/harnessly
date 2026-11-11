package typist

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// boundPane is a pane whose keys know their hold, as the tmux pane does: a line typed into a server that does not
// answer waits until the hold ends, and is cut short with it.
type boundPane struct {
	*fakePane
	held    context.Context
	hangs   bool
	started chan struct{}
}

func (p boundPane) Bind(held context.Context) Pane {
	p.held = held
	return p
}

func (p boundPane) TypeLine(text string) error {
	if !p.hangs {
		return p.fakePane.TypeLine(text)
	}
	close(p.started)
	<-p.held.Done()
	return context.Cause(p.held)
}

// Stop does not wait behind a message being typed: it takes the pane at once, the writer's try ends there with
// nothing more typed, and its result says the message was not delivered.
func TestStopTakesThePaneFromAMessageBeingTyped(t *testing.T) {
	locks := Locks()
	p := boundPane{fakePane: newPane(tui(t, "claude-idle-placeholder.ansi.txt")), hangs: true, started: make(chan struct{})}
	done := make(chan Result, 1)
	go func() { done <- TypeHeld("preempt-test", Message{Text: "hello"}, claude, p, newClock()) }()
	select {
	case <-p.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the message was never typed")
	}

	start := time.Now()
	stopped := false
	err := WriteNow("preempt-test", func(held context.Context) error {
		stopped = held.Err() == nil
		return p.fakePane.Keys("Escape")
	})
	if took := time.Since(start); err != nil || !stopped || took > time.Second {
		t.Fatalf("Stop: err %v, held %v, took %s", err, stopped, took)
	}
	var res Result
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the writer did not end with its hold")
	}
	if !res.Lost || res.Outcome != Failed || !errors.Is(res.Err, ErrPreempted) {
		t.Fatalf("the try that lost the pane: %+v", res)
	}
	if sent := p.sent(); len(sent) != 1 || sent[0] != "keys:Escape" {
		t.Fatalf("keys in the pane: %v", sent)
	}
	if Locks() != locks {
		t.Fatalf("%d locks left behind", Locks()-locks)
	}
}

// slowLook is a pane whose screen takes long to read: the writer hangs before its first key.
type slowLook struct {
	*fakePane
	wait time.Duration
}

func (p slowLook) CaptureStyled() (string, bool) {
	time.Sleep(p.wait)
	return p.fakePane.CaptureStyled()
}

// A writer that hung past its term lost the pane, and what it would have typed when it woke up is not typed: the
// next writer's keys are the only ones in the pane.
func TestAHungWriterTypesNothingAfterItsTerm(t *testing.T) {
	shortTerms(t, 60*time.Millisecond, 5*time.Second)
	locks := Locks()
	p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
	done := make(chan Result, 1)
	go func() {
		done <- TypeHeld("expire-test", Message{Text: "hello"}, claude, slowLook{p, 300 * time.Millisecond}, newClock())
	}()
	time.Sleep(20 * time.Millisecond) // the writer has the pane and hangs on its first look
	err := Write(context.Background(), nil, "expire-test", func(context.Context) error { return p.Keys("next") })
	if err != nil {
		t.Fatalf("the next writer: %v", err)
	}
	res := <-done
	if !res.Lost || !errors.Is(res.Err, ErrHoldExpired) || res.Outcome == Sent {
		t.Fatalf("the hung writer's try: %+v", res)
	}
	if sent := p.sent(); len(sent) != 1 || sent[0] != "keys:next" {
		t.Fatalf("keys in the pane: %s", strings.Join(sent, ", "))
	}
	if Locks() != locks {
		t.Fatalf("%d locks left behind", Locks()-locks)
	}
}

// A pane without a hold-aware sender is guarded all the same: its keys are refused once the hold has ended, and it
// keeps what else it can do.
func TestAGuardedPaneRefusesKeysAfterItsHold(t *testing.T) {
	held, cancel := context.WithCancelCause(context.Background())
	p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
	g := guard(held, p)
	if _, line := g.(LineTyper); line {
		t.Fatal("a pane that cannot type a line without its Enter became one that can")
	}
	if err := g.Keys("Enter"); err != nil {
		t.Fatal(err)
	}
	cancel(ErrPreempted)
	for _, err := range []error{g.Keys("Enter"), g.Paste("x"), g.TypeLine("x")} {
		if !errors.Is(err, ErrPreempted) {
			t.Fatalf("a key after the hold: %v", err)
		}
	}
	if sent := p.sent(); len(sent) != 1 {
		t.Fatalf("keys: %v", sent)
	}
}
