package typist

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The pane lock has two terms. A writer holds a pane for HoldLimit at most: past it the lock is let go for it, so a
// writer that hangs (a tmux that never answers, a wait that never ends) does not keep every other writer out for good.
// A writer waits for a pane for WaitLimit at most (or its context's end, whichever is first) and is told who is in the
// way, instead of hanging behind it.
const (
	// HoldLimit is longer than the longest honest hold: a message with pictures typed into a loaded agent takes tens of
	// seconds, a dialog driven by keys about as much.
	HoldLimit = 2 * time.Minute
	// WaitLimit is how long LockContext waits for a pane when its context names no sooner end.
	WaitLimit = 45 * time.Second
	// ProbeLimit is how long the terminal server may take to answer before anything is typed: a server slower than
	// this takes each key later than the typist's own waits, and the keys of one message arrive apart.
	ProbeLimit = 2 * time.Second

	lockPoll = 20 * time.Millisecond
)

// holdLimit and waitLimit are the terms in force; the tests shorten them.
var (
	holdLimit = HoldLimit
	waitLimit = WaitLimit
)

// OnExpired, when set, is told of a pane whose writer held it past HoldLimit: the lock was let go for it.
var OnExpired func(pane string, held time.Duration)

var expired atomic.Int64

// Expired counts the holds that ended by their term.
func Expired() int64 { return expired.Load() }

// BusyError is a pane another writer holds: the wait for it ended, nothing was typed.
type BusyError struct {
	Pane   string
	Waited time.Duration
	// Cause is the context's error when it ended the wait, nil when WaitLimit did.
	Cause error
}

func (e *BusyError) Error() string {
	return fmt.Sprintf("pane %s is held by another writer: waited %s, nothing was typed", e.Pane, e.Waited.Round(10*time.Millisecond))
}

func (e *BusyError) Unwrap() error { return e.Cause }

// SlowError is a terminal server that did not answer in ProbeLimit: nothing was typed, the write is to be tried later.
type SlowError struct {
	Pane  string
	Cause error
}

func (e *SlowError) Error() string {
	return fmt.Sprintf("the terminal of pane %s answers too slowly to type into it: %v", e.Pane, e.Cause)
}

func (e *SlowError) Unwrap() error { return e.Cause }

// IsSlow says the error is a write put off because the terminal server answers too slowly.
func IsSlow(err error) bool {
	var slow *SlowError
	return errors.As(err, &slow)
}

// ErrHoldExpired and ErrPreempted are why a writer's hold of a pane ended before the writer let it go: its term
// passed, or a writer that does not wait (the person's Stop) took the pane from it.
var (
	ErrHoldExpired = errors.New("typist: the hold of the pane ended at its term")
	ErrPreempted   = errors.New("typist: the pane was taken for the person's key")
)

// hold is one writer's hold of a pane. Its context lives as long as the hold: the writer's keys are bound to it, so
// nothing of the writer reaches the pane after the hold ended, however it ended.
type hold struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	term   *time.Timer
	// unlock lets the pane's lock go, once.
	unlock func()
}

var (
	holdsMu sync.Mutex
	holds   = map[string]*hold{}
)

// leased makes the hold of a pane just locked: unlock is called when the writer lets go, at HoldLimit, or when the
// pane is taken from it, whichever is first.
func leased(pane string, unlock func()) func() {
	_, release := held(pane, unlock)
	return release
}

func held(pane string, unlock func()) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(context.Background())
	h := &hold{ctx: ctx, cancel: cancel, unlock: unlock}
	limit := holdLimit
	holdsMu.Lock()
	holds[pane] = h
	h.term = time.AfterFunc(limit, func() {
		if h.end(pane, ErrHoldExpired) {
			expired.Add(1)
			if OnExpired != nil {
				OnExpired(pane, limit)
			}
		}
	})
	holdsMu.Unlock()
	return ctx, func() { h.end(pane, context.Canceled) }
}

// end ends the hold for cause and lets the pane go; false when it had ended already.
func (h *hold) end(pane string, cause error) bool {
	holdsMu.Lock()
	if holds[pane] != h {
		holdsMu.Unlock()
		return false
	}
	delete(holds, pane)
	holdsMu.Unlock()
	h.term.Stop()
	h.cancel(cause)
	h.unlock()
	return true
}

// tryHold is TryLock that also gives the hold's context.
func tryHold(pane string) (context.Context, func(), bool) {
	l := lockOf(pane)
	if !l.mu.TryLock() {
		leave(pane, l)
		return nil, nil, false
	}
	ctx, unlock := held(pane, onceUnlock(pane, l))
	return ctx, unlock, true
}

// holdOf is the hold of the pane now, nil when nobody holds it.
func holdOf(pane string) *hold {
	holdsMu.Lock()
	defer holdsMu.Unlock()
	return holds[pane]
}

// LockContext takes the pane's lock like Lock, but waits no longer than ctx and WaitLimit let it: a *BusyError says
// the pane stayed with another writer, and nothing is held.
func LockContext(ctx context.Context, pane string) (unlock func(), err error) {
	_, unlock, err = Hold(ctx, pane)
	return unlock, err
}

// Hold is LockContext for a writer that types more than one key: held is the hold's context — it ends when the
// writer lets go, at HoldLimit (ErrHoldExpired) or when the pane is taken from it (ErrPreempted) — and the writer's
// keys are to be bound to it.
func Hold(ctx context.Context, pane string) (held context.Context, unlock func(), err error) {
	start := time.Now()
	tick := time.NewTicker(lockPoll)
	defer tick.Stop()
	for {
		if held, unlock, ok := tryHold(pane); ok {
			return held, unlock, nil
		}
		waited := time.Since(start)
		if waited >= waitLimit {
			return nil, nil, &BusyError{Pane: pane, Waited: waited}
		}
		select {
		case <-ctx.Done():
			return nil, nil, &BusyError{Pane: pane, Waited: time.Since(start), Cause: ctx.Err()}
		case <-tick.C:
		}
	}
}

// Preempt takes the pane at once, from whoever holds it: the holder's context ends with ErrPreempted — its keys stop
// there, the one under way is cut short — and the pane is the caller's. It is for the person's own key that must not
// wait behind a message being typed: Stop.
func Preempt(pane string) (held context.Context, unlock func()) {
	for {
		if held, unlock, ok := tryHold(pane); ok {
			return held, unlock
		}
		if h := holdOf(pane); h != nil {
			h.end(pane, ErrPreempted)
			continue
		}
		// Between a lock and its hold, or a writer that waits in Lock got in first: it is asked again in a moment.
		time.Sleep(time.Millisecond)
	}
}

// Responder is a terminal server that can say whether it answers in time for a pane: nil when it does, an error
// when it took longer than ProbeLimit.
type Responder interface {
	Responsive(pane string) error
}

// Prober is a Pane that can say the same of itself; Type asks it before the first key.
type Prober interface {
	Responsive() error
}

// Write is how anything but a message puts keys into a pane: write runs with the pane's lock held, after the terminal
// server answered in time, and is given the hold's context to bind its keys to. server is what the keys go through;
// one that is a Responder is asked first, and a slow answer is a *SlowError with nothing typed. A pane of "" is
// nobody's: write runs as it is.
func Write(ctx context.Context, server any, pane string, write func(held context.Context) error) error {
	if pane == "" {
		return write(ctx)
	}
	held, unlock, err := Hold(ctx, pane)
	if err != nil {
		return err
	}
	defer unlock()
	if r, ok := server.(Responder); ok {
		if err := r.Responsive(pane); err != nil {
			return &SlowError{Pane: pane, Cause: err}
		}
	}
	return write(held)
}

// WriteNow is Write for the person's key that does not wait: the pane is taken from the writer that holds it
// (Preempt), and the key goes in without asking the server how fast it is.
func WriteNow(pane string, write func(held context.Context) error) error {
	if pane == "" {
		return write(context.Background())
	}
	held, unlock := Preempt(pane)
	defer unlock()
	return write(held)
}

// Binder is a Pane whose keys can be bound to a hold: the pane it returns types nothing once held has ended, and a
// key under way then is cut short.
type Binder interface {
	Bind(held context.Context) Pane
}

// TypeHeld is TypeChecked (Type, or its cautious form for an unchecked CLI version) under the pane's lock: it takes the lock, binds the pane's keys to the hold and types. A hold that
// ended under the try (the person's Stop took the pane, or its term passed) ends the try there: nothing more is typed,
// and the result says Lost — the message was not delivered by this try and keeps its place.
func TypeHeld(lock string, m Message, provider wire.Provider, p Pane, c Clock) Result {
	held, unlock, err := Hold(context.Background(), lock)
	if err != nil {
		return Result{Outcome: Wait, Err: err}
	}
	defer unlock()
	if b, ok := p.(Binder); ok {
		p = b.Bind(held)
	}
	res := TypeChecked(m, provider, guard(held, p), c)
	if held.Err() != nil {
		res.Lost, res.Err = true, context.Cause(held)
		if res.Outcome == Sent || res.Outcome == Stuck {
			res.Outcome = Failed
		}
	}
	return res
}

// guarded is a pane for the length of a hold: its keys are refused once the hold has ended.
type guarded struct {
	Pane
	held context.Context
}

func (g guarded) ended() error {
	if g.held.Err() == nil {
		return nil
	}
	return context.Cause(g.held)
}

func (g guarded) Paste(text string) error {
	if err := g.ended(); err != nil {
		return err
	}
	return g.Pane.Paste(text)
}

func (g guarded) TypeLine(text string) error {
	if err := g.ended(); err != nil {
		return err
	}
	return g.Pane.TypeLine(text)
}

func (g guarded) Keys(keys ...string) error {
	if err := g.ended(); err != nil {
		return err
	}
	return g.Pane.Keys(keys...)
}

func (g guarded) GrowForDialog() bool {
	d, ok := g.Pane.(DialogGrower)
	return ok && g.ended() == nil && d.GrowForDialog()
}

func (g guarded) CaptureTall(short string) (string, bool) {
	if t, ok := g.Pane.(TallCapturer); ok {
		return t.CaptureTall(short)
	}
	return short, false
}

func (g guarded) Responsive() error {
	if pr, ok := g.Pane.(Prober); ok {
		return pr.Responsive()
	}
	return nil
}

// guardedLine is guarded for a pane that types a line without its Enter.
type guardedLine struct{ guarded }

func (g guardedLine) TypeOnly(text string) error {
	if err := g.ended(); err != nil {
		return err
	}
	return g.Pane.(LineTyper).TypeOnly(text)
}

func guard(held context.Context, p Pane) Pane {
	g := guarded{Pane: p, held: held}
	if _, ok := p.(LineTyper); ok {
		return guardedLine{g}
	}
	return g
}
