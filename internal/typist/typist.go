// Package typist types into an agent's tmux pane: it reads the screen before any key, decides whether a message may
// go in now, pastes its pictures and its text, presses Enter and watches the text leave the input box. It owns the
// pane lock, one per pane for the whole process, that every writer of a pane takes — v2's outbox and Legacy alike —
// so two of them never type into the same box.
//
// The package keeps no state of messages: it answers what one try did, and the caller decides what comes next. It
// reaches tmux only through Pane, so the tests run on screens from testdata and a fake clock.
package typist

import (
	"sync"
	"time"
)

// Clock is time for a try: now and a sleep that tests make instant.
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

// SystemClock is the real clock.
type SystemClock struct{}

func (SystemClock) Now() time.Time        { return time.Now() }
func (SystemClock) Sleep(d time.Duration) { time.Sleep(d) }

// Pane is one agent pane in tmux.
type Pane interface {
	// CaptureStyled is the pane with its styles, Capture without; false when it cannot be read.
	CaptureStyled() (string, bool)
	Capture() (string, bool)
	// Paste puts text in as one bracketed paste, without submitting it.
	Paste(text string) error
	// TypeLine types a short line as keys with its Enter right behind it.
	TypeLine(text string) error
	// Keys sends tmux key names in one go, in order.
	Keys(keys ...string) error
}

// LineTyper is a pane that can type a line's keys without its Enter: the typist then presses the Enter once the box has
// read the line to its end (a loaded Claude reads a burst of keys as a paste, slowly).
type LineTyper interface {
	TypeOnly(text string) error
}

var (
	locksMu sync.Mutex
	locks   = map[string]*sync.Mutex{}
)

func paneLock(pane string) *sync.Mutex {
	locksMu.Lock()
	defer locksMu.Unlock()
	l := locks[pane]
	if l == nil {
		l = &sync.Mutex{}
		locks[pane] = l
	}
	return l
}

// Lock takes the pane's lock, waiting for the writer that holds it; unlock lets it go and is safe to call twice.
func Lock(pane string) (unlock func()) {
	l := paneLock(pane)
	l.Lock()
	return onceUnlock(l)
}

// TryLock takes the pane's lock when nobody holds it; ok false otherwise, and nothing is held.
func TryLock(pane string) (unlock func(), ok bool) {
	l := paneLock(pane)
	if !l.TryLock() {
		return func() {}, false
	}
	return onceUnlock(l), true
}

func onceUnlock(l *sync.Mutex) func() {
	var once sync.Once
	return func() { once.Do(l.Unlock) }
}
