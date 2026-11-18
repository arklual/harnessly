// Package notebooks keeps explicitly started Python kernels. It never reads a notebook's
// kernelspec, runs a cell on open, or writes the notebook back to disk.
package notebooks

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	MaxSessions      = 16
	MaxOwnerSessions = 4
	MaxCodeBytes     = 256 << 10
	MaxOutputBytes   = 4 << 20
	MaxOutputs       = 1000
	ExecuteTimeout   = 120 * time.Second
	StartupTimeout   = 30 * time.Second
	IdleTimeout      = 30 * time.Minute
)

// Error is an actionable refusal, not a Python cell exception (those are outputs).
type Error struct{ Code, Message string }

func (e *Error) Error() string           { return e.Message }
func failure(code, message string) error { return &Error{code, message} }

// Key must be made by the caller after authorizing the owner and resolving Path
// to an accessible, canonical notebook in the conversation's writable folder.
type Key struct{ Owner, Conversation, Path string }

type State struct {
	State  string `json:"state"`
	Kernel string `json:"kernel,omitempty"`
}

type Result struct {
	Outputs        []json.RawMessage `json:"outputs"`
	ExecutionCount *int              `json:"execution_count"`
	Status         string            `json:"status"`
}

type kernel interface {
	Execute(context.Context, string) (Result, error)
	Interrupt(context.Context) error
	Close()
	Alive() bool
}

type session struct {
	kernel kernel
	life   context.Context
	cancel context.CancelFunc
	busy   bool
	used   time.Time
}

// Manager's lock protects only bookkeeping; interrupt/shutdown never wait for an
// execution lock. A failed or timed-out helper is discarded, not reused out of sync.
type Manager struct {
	mu       sync.Mutex
	sessions map[Key]*session
	closed   bool
	stop     chan struct{}
	done     chan struct{}
	wg       sync.WaitGroup
	start    func(context.Context, string) (kernel, error)
	now      func() time.Time
	idle     time.Duration
}

func New() *Manager {
	m := &Manager{sessions: make(map[Key]*session), stop: make(chan struct{}), done: make(chan struct{}), start: startPython, now: time.Now, idle: IdleTimeout}
	go func() {
		defer close(m.done)
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-m.stop:
				return
			case <-t.C:
				m.expire()
			}
		}
	}()
	return m
}

func state(s *session) State {
	if s == nil {
		return State{State: "stopped"}
	}
	if s.busy {
		return State{State: "busy", Kernel: "python3"}
	}
	return State{State: "idle", Kernel: "python3"}
}

// Status does not create a session, probe Python, or extend its idle lifetime.
func (m *Manager) Status(key Key) State {
	m.mu.Lock()
	s := m.sessions[key]
	if s != nil && !s.busy && (m.now().Sub(s.used) >= m.idle || s.kernel == nil || !s.kernel.Alive()) {
		delete(m.sessions, key)
	} else {
		s = nil
	}
	out := state(m.sessions[key])
	m.mu.Unlock()
	m.stopSession(s)
	return out
}

func (m *Manager) stopSession(s *session) {
	if s != nil {
		m.mu.Lock()
		p := s.kernel
		m.mu.Unlock()
		s.cancel()
		if p != nil {
			p.Close()
		}
	}
}

// reserve claims both a session slot and its single execute/restart operation.
func (m *Manager) reserve(key Key) (*session, context.Context, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, nil, failure("closed", "Notebook execution is shutting down.")
	}
	s := m.sessions[key]
	if s != nil && s.busy {
		return nil, nil, failure("busy", "This notebook is busy. Interrupt it or wait for the cell to finish.")
	}
	if s == nil {
		n := 0
		for k := range m.sessions {
			if k.Owner == key.Owner {
				n++
			}
		}
		if len(m.sessions) >= MaxSessions || n >= MaxOwnerSessions {
			return nil, nil, failure("limit", "Too many notebook kernels. Shut down another notebook and try again.")
		}
		life, cancel := context.WithCancel(context.Background())
		s = &session{life: life, cancel: cancel}
		m.sessions[key] = s
	}
	s.busy = true
	s.used = m.now()
	m.wg.Add(1)
	return s, s.life, nil
}

func (m *Manager) finish(key Key, s *session, err error) {
	m.mu.Lock()
	if m.sessions[key] == s {
		if err != nil {
			delete(m.sessions, key)
		} else {
			s.busy = false
			s.used = m.now()
		}
	}
	m.mu.Unlock()
	if err != nil {
		m.stopSession(s)
	}
	m.wg.Done()
}

func (m *Manager) ensure(ctx context.Context, key Key, s *session) error {
	m.mu.Lock()
	p := s.kernel
	m.mu.Unlock()
	if p != nil && p.Alive() {
		return nil
	}
	if p != nil {
		p.Close()
	}
	p, err := m.start(s.life, key.Path)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if m.closed || m.sessions[key] != s || ctx.Err() != nil {
		m.mu.Unlock()
		p.Close()
		return failure("closed", "Notebook execution was stopped.")
	}
	s.kernel = p
	m.mu.Unlock()
	return nil
}

func (m *Manager) Execute(ctx context.Context, key Key, code string) (out Result, err error) {
	if len(code) > MaxCodeBytes || !utf8.ValidString(code) {
		return out, failure("code-limit", "Cell code must be UTF-8 and at most 256 KiB.")
	}
	s, life, err := m.reserve(key)
	if err != nil {
		return out, err
	}
	defer func() { m.finish(key, s, err) }()
	// Canceling an HTTP request also tears down its executing kernel, so code
	// cannot continue invisibly after the client disconnects.
	work, cancel := context.WithTimeout(ctx, StartupTimeout+ExecuteTimeout)
	defer cancel()
	stop := context.AfterFunc(life, cancel)
	defer stop()
	stopWork := context.AfterFunc(work, s.cancel)
	defer stopWork()
	if err = m.ensure(work, key, s); err != nil {
		return out, err
	}
	m.mu.Lock()
	p := s.kernel
	m.mu.Unlock()
	execCtx, execCancel := context.WithTimeout(work, ExecuteTimeout)
	defer execCancel()
	out, err = p.Execute(execCtx, code)
	return out, err
}

func (m *Manager) Restart(ctx context.Context, key Key) (out State, err error) {
	s, life, err := m.reserve(key)
	if err != nil {
		return out, err
	}
	defer func() { m.finish(key, s, err) }()
	m.mu.Lock()
	old := s.kernel
	s.kernel = nil
	m.mu.Unlock()
	if old != nil {
		old.Close()
	}
	work, cancel := context.WithTimeout(ctx, StartupTimeout)
	defer cancel()
	stop := context.AfterFunc(life, cancel)
	defer stop()
	stopWork := context.AfterFunc(work, s.cancel)
	defer stopWork()
	err = m.ensure(work, key, s)
	return State{State: "idle", Kernel: "python3"}, err
}

func (m *Manager) Interrupt(ctx context.Context, key Key) (State, error) {
	m.mu.Lock()
	s := m.sessions[key]
	var p kernel
	if s != nil {
		p = s.kernel
	}
	out := state(s)
	m.mu.Unlock()
	if p == nil {
		// An execute can still be starting Python. Cancel that reservation too;
		// otherwise an interrupt on editor close could run the cell afterwards.
		if s != nil {
			m.mu.Lock()
			if m.sessions[key] == s {
				delete(m.sessions, key)
			}
			m.mu.Unlock()
			m.stopSession(s)
			return State{State: "stopped"}, nil
		}
		return out, nil
	}
	work, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := p.Interrupt(work); err != nil {
		return out, err
	}
	return m.Status(key), nil
}

func (m *Manager) Shutdown(key Key) State {
	m.mu.Lock()
	s := m.sessions[key]
	delete(m.sessions, key)
	m.mu.Unlock()
	m.stopSession(s)
	return State{State: "stopped"}
}

func (m *Manager) CloseConversation(conv string) {
	m.remove(func(k Key, _ *session) bool { return k.Conversation == conv })
}

func (m *Manager) expire() {
	m.remove(func(_ Key, s *session) bool {
		return !s.busy && (m.now().Sub(s.used) >= m.idle || s.kernel == nil || !s.kernel.Alive())
	})
}

func (m *Manager) remove(match func(Key, *session) bool) {
	m.mu.Lock()
	var stopped []*session
	for k, s := range m.sessions {
		if match(k, s) {
			delete(m.sessions, k)
			stopped = append(stopped, s)
		}
	}
	m.mu.Unlock()
	for _, s := range stopped {
		m.stopSession(s)
	}
}

// Close waits for every started helper (including one still starting) to be reaped.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		<-m.done
		m.wg.Wait()
		return
	}
	m.closed = true
	close(m.stop)
	m.mu.Unlock()
	m.remove(func(Key, *session) bool { return true })
	<-m.done
	m.wg.Wait()
}

func contextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return failure("timeout", "Notebook execution timed out; the kernel was stopped. Run the cell again to start a fresh kernel.")
	}
	return failure("closed", "Notebook execution was stopped.")
}
