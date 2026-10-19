// Package prompts types a person's messages and keys into an agent's tmux pane: the per-tab prompt queue and its
// delivery, steering a message into a running turn, "send now", the composer's text route (through Claude's external
// editor or a paste, or Codex's watched submit), the slash-command runner with its model and effort confirmation, the
// menu mirror, AskUserQuestion answers, and the registry that keeps one message from being typed twice.
//
// The package owns no tab state. It reads tabs, agent processes and transcripts and reports transitions through the
// small interfaces in deps.go, so the status machine wires it and tests use fakes. Every route-facing operation answers
// a Response (status code and JSON body), so an HTTP handler only checks the session and the method.
package prompts

import (
	"sync"
	"time"
)

// Clock is time for the package: now, a blocking sleep and a timer. Tests give a fake one.
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer is a pending AfterFunc.
type Timer interface {
	Stop() bool
}

// SystemClock is the real clock.
type SystemClock struct{}

func (SystemClock) Now() time.Time        { return time.Now() }
func (SystemClock) Sleep(d time.Duration) { time.Sleep(d) }
func (SystemClock) AfterFunc(d time.Duration, f func()) Timer {
	return time.AfterFunc(d, f)
}

// Steps marks how long the parts of one delivery took, in milliseconds since it started, in the order they happened.
type Steps struct {
	clock Clock
	start time.Time

	mu    sync.Mutex
	names []string
	at    map[string]int64
}

// NewSteps starts a step clock now.
func NewSteps(c Clock) *Steps { return &Steps{clock: c, start: c.Now(), at: map[string]int64{}} }

// Mark records a step; a step marked again keeps its place and takes the new time.
func (s *Steps) Mark(name string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.at[name]; !ok {
		s.names = append(s.names, name)
	}
	s.at[name] = s.clock.Now().Sub(s.start).Milliseconds()
}

// Names are the steps marked, in order.
func (s *Steps) Names() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.names...)
}

// MarshalJSON writes the steps as an object in the order they were marked.
func (s *Steps) MarshalJSON() ([]byte, error) {
	if s == nil {
		return []byte("{}"), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b := []byte{'{'}
	for i, name := range s.names {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendJSONString(b, name)
		b = append(b, ':')
		b = appendInt(b, s.at[name])
	}
	return append(b, '}'), nil
}
