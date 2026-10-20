package prompts

import (
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// Routes a message goes into a pane by.
const (
	RouteComposer   = "composer"
	RoutePromptText = "prompt-text"
	RouteQueue      = "queue"
	RouteSteer      = "steer"
	RouteSendNow    = "send-now"
)

// DuplicateWindow is how long a message id that went into a pane is refused again.
const DuplicateWindow = 30 * time.Second

// Registry remembers, for DuplicateWindow, the ids of messages that went into a pane by any route, so a retry through
// another path is not typed twice. Every dispatch is logged with a fingerprint of the text, never the text.
type Registry struct {
	clock Clock
	log   func(level, msg string, kv ...any)

	mu   sync.Mutex
	seen map[string]dispatch
}

type dispatch struct {
	route string
	at    time.Time
}

// NewRegistry is an empty registry.
func NewRegistry(clock Clock, log func(level, msg string, kv ...any)) *Registry {
	if log == nil {
		log = func(string, string, ...any) {}
	}
	return &Registry{clock: clock, log: log, seen: map[string]dispatch{}}
}

// Claim is false when the id went in within the window: the caller must not type the message again. A message without
// an id is never refused.
func (r *Registry) Claim(id, route, target, text string) bool {
	now := r.clock.Now()
	r.mu.Lock()
	for k, d := range r.seen {
		if now.Sub(d.at) >= DuplicateWindow {
			delete(r.seen, k)
		}
	}
	earlier, dup := r.seen[id]
	if id != "" && !dup {
		r.seen[id] = dispatch{route: route, at: now}
	}
	r.mu.Unlock()

	fields := []any{"promptId", nullable(id), "route", route, "target", target, "chars", jscompat.Len(text), "digest", sha1Hex(text, 10)}
	if id != "" && dup {
		r.log("warn", "duplicate prompt refused", append(fields, "firstRoute", earlier.route, "sinceMs", now.Sub(earlier.at).Milliseconds())...)
		return false
	}
	r.log("info", "prompt "+route+" → "+target, fields...)
	return true
}

// Release forgets an id whose dispatch did not reach the pane, so a retry may go.
func (r *Registry) Release(id string) {
	if id == "" {
		return
	}
	r.mu.Lock()
	delete(r.seen, id)
	r.mu.Unlock()
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// EnterFresh is how old the runtime's last Enter in a pane may be for a prompt-submit hook to take it.
const EnterFresh = 30 * time.Second

// EnterClock remembers when the runtime last pressed or passed on Enter in each pane, so Claude's prompt-submit hook
// can tell how long Claude took to take the prompt.
type EnterClock struct {
	clock Clock
	mu    sync.Mutex
	at    map[string]time.Time
}

// NewEnterClock is an empty Enter clock.
func NewEnterClock(clock Clock) *EnterClock {
	return &EnterClock{clock: clock, at: map[string]time.Time{}}
}

// Note records an Enter in the session now.
func (e *EnterClock) Note(session string) {
	e.mu.Lock()
	e.at[session] = e.clock.Now()
	e.mu.Unlock()
}

// Take is the milliseconds since the session's last Enter, taken once; false without one in the last 30 s.
func (e *EnterClock) Take(session string) (int64, bool) {
	now := e.clock.Now()
	e.mu.Lock()
	at, ok := e.at[session]
	delete(e.at, session)
	e.mu.Unlock()
	if !ok || now.Sub(at) > EnterFresh || now.Before(at) {
		return 0, false
	}
	return now.Sub(at).Milliseconds(), true
}

// flights lets one typing job run per tab at a time: the queue pump, a steer and "send now" share it.
type flights struct {
	mu   sync.Mutex
	busy map[string]bool
}

func newFlights() *flights { return &flights{busy: map[string]bool{}} }

// run runs job unless one runs for the tab; false when it was refused. The tab is freed however job ends.
func (f *flights) run(tabID string, job func()) bool {
	f.mu.Lock()
	if f.busy[tabID] {
		f.mu.Unlock()
		return false
	}
	f.busy[tabID] = true
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		delete(f.busy, tabID)
		f.mu.Unlock()
	}()
	job()
	return true
}
