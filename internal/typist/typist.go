// Package typist types into an agent's tmux pane: it reads the screen before any key, decides whether a message may
// go in now, pastes its pictures and its text, presses Enter and watches the text leave the input box. It owns the
// pane lock, one per pane for the whole process, that every writer of a pane takes — v2's outbox and Legacy alike —
// so two of them never type into the same box. A hold has a term and a wait for it has one too (panelock.go): Write is
// the way in for everything that is not a message.
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

// DialogGrower is a pane whose window can grow for a dialog that does not fit. GrowForDialog answers true when the
// window grew and the program had time to redraw.
type DialogGrower interface {
	GrowForDialog() bool
}

// TallCapturer is a pane with a terminal attached, whose size it may change only for a moment: CaptureTall is the styled
// screen at a size a dialog fits in. short is the screen now, as the look read it; false leaves that as it is.
type TallCapturer interface {
	CaptureTall(short string) (string, bool)
}

// A pane's lock lives while someone holds it or waits for it: the panes of the sessions that ended leave nothing.
type paneLock struct {
	mu sync.Mutex
	// users are the holder and those waiting, under locksMu.
	users int
}

var (
	locksMu sync.Mutex
	locks   = map[string]*paneLock{}
)

func lockOf(pane string) *paneLock {
	locksMu.Lock()
	defer locksMu.Unlock()
	l := locks[pane]
	if l == nil {
		l = &paneLock{}
		locks[pane] = l
	}
	l.users++
	return l
}

// leave is the end of a use of the pane's lock; the last one takes the lock off the list.
func leave(pane string, l *paneLock) {
	locksMu.Lock()
	defer locksMu.Unlock()
	if l.users--; l.users == 0 && locks[pane] == l {
		delete(locks, pane)
	}
}

// Lock takes the pane's lock, waiting for the writer that holds it; unlock lets it go and is safe to call twice.
func Lock(pane string) (unlock func()) {
	l := lockOf(pane)
	l.mu.Lock()
	return leased(pane, onceUnlock(pane, l))
}

// TryLock takes the pane's lock when nobody holds it; ok false otherwise, and nothing is held.
func TryLock(pane string) (unlock func(), ok bool) {
	l := lockOf(pane)
	if !l.mu.TryLock() {
		leave(pane, l)
		return func() {}, false
	}
	return leased(pane, onceUnlock(pane, l)), true
}

func onceUnlock(pane string, l *paneLock) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Unlock()
			leave(pane, l)
		})
	}
}

// Locks is how many panes have a lock held or waited for now.
func Locks() int {
	locksMu.Lock()
	defer locksMu.Unlock()
	return len(locks)
}
