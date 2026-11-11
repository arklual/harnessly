// Package safego is the fault boundary of a goroutine: a panic in one piece of work (one conversation's actor, one
// read of a transcript, one socket) is counted and written to the log with its stack, and the process goes on with
// the rest.
package safego

import (
	"log/slog"
	"runtime/debug"
	"sync/atomic"
)

// Panic is a panic that was held: its value and the stack it was raised on.
type Panic struct {
	Value any
	Stack []byte
}

var (
	panics atomic.Int64
	sink   atomic.Pointer[func(name string, p Panic)]
)

// SetLog makes report the place held panics are written to (the process journal until then). Nil puts that back.
func SetLog(report func(name string, p Panic)) {
	if report == nil {
		sink.Store(nil)
		return
	}
	sink.Store(&report)
}

// Panics is how many panics were held since the process started.
func Panics() int64 { return panics.Load() }

// Try calls fn on the caller's goroutine. A panic of fn is counted and logged under name, and returned; nil when fn
// returned.
func Try(name string, fn func()) (held *Panic) {
	defer func() {
		v := recover()
		if v == nil {
			return
		}
		held = &Panic{Value: v, Stack: debug.Stack()}
		panics.Add(1)
		report(name, *held)
	}()
	fn()
	return nil
}

// Run is Try that only says whether fn returned.
func Run(name string, fn func()) bool { return Try(name, fn) == nil }

// Go runs fn on a goroutine of its own; a panic of fn ends that goroutine only.
func Go(name string, fn func()) {
	go Try(name, fn)
}

func report(name string, p Panic) {
	// The reporter is someone else's code: a panic of its own must not undo the boundary.
	defer func() { _ = recover() }()
	if f := sink.Load(); f != nil {
		(*f)(name, p)
		return
	}
	slog.Error("background task panicked", "task", name, "panic", p.Value, "stack", string(p.Stack))
}
