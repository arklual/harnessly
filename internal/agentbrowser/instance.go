package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/cdp"
)

// Profile identifies the browser process or external connection shared by conversations.
type Profile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"-"`
}

// Viewport describes the requested content area in CSS pixels.
type Viewport struct {
	Mode string  `json:"mode"`
	W    int     `json:"w"`
	H    int     `json:"h"`
	DPR  float64 `json:"dpr"`
}

// Tab is persistent metadata; Order records creation order independently of Chrome's target IDs.
type Tab struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	URL          string   `json:"url"`
	Conversation string   `json:"conversation,omitempty"`
	By           string   `json:"by"`
	Viewport     Viewport `json:"viewport"`
	Crashed      bool     `json:"crashed"`
	Order        uint64   `json:"order"`
	WindowID     int      `json:"windowId"`
}

type page struct {
	Tab
	gate         chan struct{}
	binding      binding
	preparing    bool
	viewers      map[*Viewer]bool
	last         *Frame
	delivered    time.Time
	framePending bool
	provisional  bool
}

type binding struct {
	conn    *cdp.Conn
	session string
	window  int
	target  string
}

// Instance keeps registry locks independent of browser replies and disk writes.
type Instance struct {
	manager                   *Manager
	profile                   Profile
	dir                       string
	mu                        sync.Mutex
	closing                   bool
	creating                  int
	conn                      *cdp.Conn
	pages                     map[string]*page
	current                   map[string]string
	hidden                    map[string]bool
	state, failure            string
	changes                   map[chan struct{}]bool
	refresh                   chan struct{}
	revision, saved, sequence uint64
	writes                    uint64
	saveError                 error
	tasks                     sync.WaitGroup
	ackQueue                  []ack
	ackNext                   map[string]time.Time
}

// Profile returns this instance's identity.
func (i *Instance) Profile() Profile { return i.profile }

// State reports connection or persistence failures without discarding healthy tabs.
func (i *Instance) State() (string, string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.saveError != nil {
		return "error", "Cannot save browser tabs: " + i.saveError.Error()
	}
	return i.state, i.failure
}

func (i *Instance) notifyLocked() {
	for c := range i.changes {
		select {
		case c <- struct{}{}:
		default:
		}
	}
}

func (i *Instance) changedLocked() {
	i.revision++
	i.notifyLocked()
}

func (i *Instance) setState(s string, err error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.state, i.failure = s, ""
	if err != nil {
		i.failure = err.Error()
	}
	i.notifyLocked()
}

// Changes coalesces notifications; the caller reads current snapshots after each wakeup.
func (i *Instance) Changes() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	i.mu.Lock()
	i.changes[ch] = true
	i.mu.Unlock()
	return ch, func() { i.mu.Lock(); delete(i.changes, ch); i.mu.Unlock() }
}

func (i *Instance) connection() *cdp.Conn {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.conn
}

func (i *Instance) beginOperation() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closing || i.manager.ctx.Err() != nil {
		return cdp.ErrClosed
	}
	i.tasks.Add(1)
	return nil
}

func (i *Instance) task(fn func()) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closing || i.manager.ctx.Err() != nil {
		return
	}
	i.tasks.Add(1)
	go func() { defer i.tasks.Done(); fn() }()
}

func (i *Instance) run() {
	ctx := i.manager.ctx
	workers := make(chan struct{})
	flush := make(chan struct{})
	go func() { defer close(workers); i.persist(flush) }()
	i.task(func() { i.acknowledge(ctx) })
	defer func() {
		// Pair with task's mutex before Wait so cancellation cannot race a final Add.
		i.mu.Lock()
		i.closing = true
		i.mu.Unlock()
		i.tasks.Wait()
		close(flush)
		<-workers
		i.setState("down", nil)
	}()
	delay := time.Second
	for ctx.Err() == nil {
		i.setState("starting", nil)
		attempt, cancel := context.WithTimeout(ctx, cdp.DefaultTimeout)
		c, err := i.connect(attempt)
		cancel()
		started := time.Now()
		up := false
		if err == nil {
			i.mu.Lock()
			i.conn = c
			i.ackQueue = nil
			i.ackNext = make(map[string]time.Time)
			for _, p := range i.pages {
				p.last = nil
			}
			i.mu.Unlock()
			readerDone := make(chan struct{})
			go func() { defer close(readerDone); _ = c.Run(ctx) }()
			err = i.reconcile(ctx, c, true)
			if err == nil {
				_, err = c.Call(ctx, "Target.setDiscoverTargets", map[string]any{"discover": true}, "")
			}
			if err == nil {
				i.setState("up", nil)
				up = true
				err = i.connected(ctx, c)
			}
			_ = c.Close()
			<-readerDone
		}
		if ctx.Err() != nil {
			return
		}
		if up {
			i.setState("down", err)
		} else {
			i.setState("error", i.startupError(err))
		}
		delay = retryDelay(delay, up && time.Since(started) >= time.Minute)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(30*time.Second, delay*2)
	}
}

func retryDelay(delay time.Duration, stable bool) time.Duration {
	if stable {
		return time.Second
	}
	return min(30*time.Second, max(time.Second, delay))
}

func (i *Instance) connected(ctx context.Context, c *cdp.Conn) error {
	// Some Chrome versions update getTargets titles without emitting targetInfoChanged.
	// This inexpensive snapshot never reattaches an already bound target or forces a disk write.
	metadata := time.NewTicker(time.Second)
	defer metadata.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.Done():
			return cdp.ErrClosed
		case <-metadata.C:
			_ = i.reconcile(ctx, c, false)
		case <-i.refresh:
			// A transient target error must not disconnect every conversation in the profile.
			_ = i.reconcile(ctx, c, false)
		}
	}
}

func (i *Instance) lookup(id string) (*page, *cdp.Conn, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	p := i.pages[id]
	if p == nil {
		return nil, nil, fmt.Errorf("unknown browser tab %q", id)
	}
	if i.conn == nil {
		return nil, nil, cdp.ErrClosed
	}
	return p, i.conn, nil
}

func lockPage(ctx context.Context, p *page) error {
	select {
	case p.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func unlockPage(p *page) { <-p.gate }

// prepare binds and restores only this tab. Its queue never holds the registry mutex.
func (i *Instance) prepare(ctx context.Context, p *page, c *cdp.Conn) (binding, error) {
	ctx, cancel := cdp.WithTimeout(ctx)
	defer cancel()
	if err := lockPage(ctx, p); err != nil {
		return binding{}, err
	}
	defer unlockPage(p)
	return i.bind(ctx, p, c)
}

func (i *Instance) bind(ctx context.Context, p *page, c *cdp.Conn) (binding, error) {
	i.mu.Lock()
	b, v := p.binding, p.Viewport
	present := i.pages[p.ID] == p && i.conn == c
	i.mu.Unlock()
	if !present {
		return binding{}, errors.New("browser tab is no longer available")
	}
	if b.conn == c && b.session != "" {
		return b, nil
	}
	raw, err := c.Call(ctx, "Target.attachToTarget", map[string]any{"targetId": p.ID, "flatten": true}, "")
	if err != nil {
		return binding{}, err
	}
	var a struct {
		Session string `json:"sessionId"`
	}
	if err = json.Unmarshal(raw, &a); err != nil {
		return binding{}, err
	}
	b = binding{conn: c, session: a.Session, target: p.ID}
	if _, err = c.Call(ctx, "Emulation.setFocusEmulationEnabled", map[string]any{"enabled": true}, b.session); err != nil {
		return binding{}, err
	}
	if _, err = c.Call(ctx, "Inspector.enable", nil, b.session); err != nil {
		return binding{}, err
	}
	raw, err = c.Call(ctx, "Browser.getWindowForTarget", map[string]any{"targetId": p.ID}, "")
	if err != nil {
		return binding{}, err
	}
	var window struct {
		ID int `json:"windowId"`
	}
	if err = json.Unmarshal(raw, &window); err != nil {
		return binding{}, err
	}
	b.window = window.ID
	i.mu.Lock()
	p.binding, p.WindowID = b, b.window
	p.Crashed = false
	viewers := len(p.viewers)
	i.changedLocked()
	i.mu.Unlock()
	// An unresponsive renderer may reject restoration; other tabs remain usable.
	if v.W > 0 {
		err = i.viewport(ctx, b, v)
	}
	if viewers > 0 {
		castErr := i.startCast(ctx, p.ID, b)
		if err == nil {
			err = castErr
		}
	}
	return b, err
}

// Call sends a tab command with a bounded lifetime, independently of other tabs.
func (i *Instance) Call(ctx context.Context, id, method string, params any) (json.RawMessage, error) {
	if err := i.beginOperation(); err != nil {
		return nil, err
	}
	defer i.tasks.Done()
	ctx, cancel := cdp.WithTimeout(ctx)
	defer cancel()
	p, c, err := i.lookup(id)
	if err != nil {
		return nil, err
	}
	b, err := i.prepare(ctx, p, c)
	if err != nil {
		return nil, err
	}
	return c.Call(ctx, method, params, b.session)
}
